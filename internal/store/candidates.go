// internal/store/candidates.go
package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// This project's Qdrant runs on host port 6343 (not the default 6333)
// due to a local port conflict with an unrelated Docker stack.
const qdrantURL = "http://localhost:6343"

const (
	qdrantFetchLimit = 150 // over-fetch to leave room for exclusion + a full page pool
	poolSize          = 100 // full ranked pool stored per session, not just one page

	similarityWeight = 0.8
	starsWeight       = 0.2
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// RankedItem is a single candidate with its final blended score.
type RankedItem struct {
	ItemID int64
	Score  float64
}

// CandidateResult holds up to poolSize ranked candidates for a user,
// or a FallbackReason if none could be produced (e.g. cold_start).
type CandidateResult struct {
	Items          []RankedItem
	FallbackReason string
}

type candidateScore struct {
	ItemID     int64
	Similarity float64
}

type qdrantSearchRequest struct {
	Vector      []float64 `json:"vector"`
	Limit       int       `json:"limit"`
	WithPayload bool      `json:"with_payload"`
}

type qdrantSearchResponse struct {
	Result []struct {
		ID    int64   `json:"id"`
		Score float64 `json:"score"`
	} `json:"result"`
}

// GetCandidates returns up to poolSize ranked candidates for a user,
// blending ANN similarity (80%) with normalized star count (20%),
// excluding items the user has already engaged with. Callers are
// responsible for paginating this pool — it is not pre-sliced to a
// single page.
func GetCandidates(ctx context.Context, db *sql.DB, userID int64) (CandidateResult, error) {
	embeddingText, err := getUserActiveEmbedding(ctx, db, userID)
	if err != nil {
		return CandidateResult{}, fmt.Errorf("fetching user embedding: %w", err)
	}
	if embeddingText == "" {
		return CandidateResult{FallbackReason: "cold_start"}, nil
	}

	vec, err := ParsePgvectorText(embeddingText)
	if err != nil {
		return CandidateResult{}, fmt.Errorf("parsing user embedding: %w", err)
	}

	scored, err := searchRepoEmbeddings(ctx, vec, qdrantFetchLimit)
	if err != nil {
		return CandidateResult{}, fmt.Errorf("querying qdrant: %w", err)
	}

	seenIDs, err := getSeenItemIDs(ctx, db, userID)
	if err != nil {
		return CandidateResult{}, fmt.Errorf("fetching seen items: %w", err)
	}

	var unseen []candidateScore
	for _, c := range scored {
		if !seenIDs[c.ItemID] {
			unseen = append(unseen, c)
		}
	}

	stars, err := getStarCounts(ctx, db, candidateItemIDs(unseen))
	if err != nil {
		return CandidateResult{}, fmt.Errorf("fetching star counts: %w", err)
	}

	maxStars := 0
	for _, s := range stars {
		if s > maxStars {
			maxStars = s
		}
	}

	var rankedList []RankedItem
	for _, c := range unseen {
		normalizedStars := 0.0
		if maxStars > 0 {
			normalizedStars = float64(stars[c.ItemID]) / float64(maxStars)
		}
		finalScore := c.Similarity*similarityWeight + normalizedStars*starsWeight
		rankedList = append(rankedList, RankedItem{ItemID: c.ItemID, Score: finalScore})
	}

	sort.Slice(rankedList, func(i, j int) bool {
		return rankedList[i].Score > rankedList[j].Score
	})

	if len(rankedList) > poolSize {
		rankedList = rankedList[:poolSize]
	}

	return CandidateResult{Items: rankedList}, nil
}

func getUserActiveEmbedding(ctx context.Context, db *sql.DB, userID int64) (string, error) {
	var embeddingText sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT ue.embedding::text
		FROM users u
		JOIN user_embeddings ue ON ue.id = u.active_embedding_id
		WHERE u.id = $1
	`, userID).Scan(&embeddingText)

	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !embeddingText.Valid {
		return "", nil
	}
	return embeddingText.String, nil
}

func getSeenItemIDs(ctx context.Context, db *sql.DB, userID int64) (map[int64]bool, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT item_id FROM events WHERE user_id = $1
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		seen[id] = true
	}
	return seen, rows.Err()
}

func searchRepoEmbeddings(ctx context.Context, vec []float64, limit int) ([]candidateScore, error) {
	body, err := json.Marshal(qdrantSearchRequest{
		Vector:      vec,
		Limit:       limit,
		WithPayload: false,
	})
	if err != nil {
		return nil, err
	}

	url := qdrantURL + "/collections/repo_embeddings/points/search"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("qdrant search returned %d", resp.StatusCode)
	}

	var result qdrantSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	scores := make([]candidateScore, len(result.Result))
	for i, r := range result.Result {
		scores[i] = candidateScore{ItemID: r.ID, Similarity: r.Score}
	}
	return scores, nil
}

func candidateItemIDs(cs []candidateScore) []int64 {
	ids := make([]int64, len(cs))
	for i, c := range cs {
		ids[i] = c.ItemID
	}
	return ids
}

func getStarCounts(ctx context.Context, db *sql.DB, itemIDs []int64) (map[int64]int, error) {
	if len(itemIDs) == 0 {
		return map[int64]int{}, nil
	}
	rows, err := db.QueryContext(ctx, `SELECT id, stars FROM items WHERE id = ANY($1)`, pqInt64Array(itemIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stars := make(map[int64]int)
	for rows.Next() {
		var id int64
		var s int
		if err := rows.Scan(&id, &s); err != nil {
			return nil, err
		}
		stars[id] = s
	}
	return stars, rows.Err()
}

func pqInt64Array(ids []int64) string {
	strs := make([]string, len(ids))
	for i, id := range ids {
		strs[i] = fmt.Sprintf("%d", id)
	}
	return "{" + strings.Join(strs, ",") + "}"
}