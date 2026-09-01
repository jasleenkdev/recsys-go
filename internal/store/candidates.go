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

	"github.com/jasleenkdev/recsys-go/internal/config"
)

const (
	qdrantFetchLimit = 150 // over-fetch to leave room for exclusion + a full page pool
	poolSize          = 100 // full ranked pool stored per session, not just one page

	similarityWeight = 0.8
	starsWeight       = 0.2
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// RankedItem is a single candidate with its final blended score, plus
// the metadata a client needs to render it.
//
// The metadata rides along because the ranker already reads these rows
// to compute the star component of the score — carrying the columns it
// has already loaded costs nothing, and not carrying them forced every
// client into one detail fetch per ranked item.
type RankedItem struct {
	ItemID      int64
	Score       float64
	Title       string
	Description string
	Stars       int
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

	meta, err := getCandidateItems(ctx, db, candidateItemIDs(unseen))
	if err != nil {
		return CandidateResult{}, fmt.Errorf("fetching candidate items: %w", err)
	}

	maxStars := 0
	for _, m := range meta {
		if m.Stars > maxStars {
			maxStars = m.Stars
		}
	}

	var rankedList []RankedItem
	for _, c := range unseen {
		m, ok := meta[c.ItemID]
		if !ok {
			// Ranked in Qdrant but gone from Postgres — a stale vector
			// for a deleted repo. Drop it rather than emitting a card
			// with no title.
			continue
		}

		normalizedStars := 0.0
		if maxStars > 0 {
			normalizedStars = float64(m.Stars) / float64(maxStars)
		}
		finalScore := c.Similarity*similarityWeight + normalizedStars*starsWeight
		rankedList = append(rankedList, RankedItem{
			ItemID:      c.ItemID,
			Score:       finalScore,
			Title:       m.Title,
			Description: m.Description,
			Stars:       m.Stars,
		})
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

	url := config.Load().QdrantURL + "/collections/repo_embeddings/points/search"
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

// candidateItem is the per-item data the ranker needs: stars to score
// with, title and description to render with.
type candidateItem struct {
	Title       string
	Description string
	Stars       int
}

// getCandidateItems loads every candidate in one indexed query keyed on
// the primary key. It was previously getStarCounts and selected only
// `stars`; the extra two columns come off the same rows, so the widened
// select adds no round trips and no extra index work.
func getCandidateItems(ctx context.Context, db *sql.DB, itemIDs []int64) (map[int64]candidateItem, error) {
	if len(itemIDs) == 0 {
		return map[int64]candidateItem{}, nil
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, title, COALESCE(description, ''), stars
		FROM items
		WHERE id = ANY($1)
	`, pqInt64Array(itemIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make(map[int64]candidateItem, len(itemIDs))
	for rows.Next() {
		var id int64
		var it candidateItem
		if err := rows.Scan(&id, &it.Title, &it.Description, &it.Stars); err != nil {
			return nil, err
		}
		items[id] = it
	}
	return items, rows.Err()
}

func pqInt64Array(ids []int64) string {
	strs := make([]string, len(ids))
	for i, id := range ids {
		strs[i] = fmt.Sprintf("%d", id)
	}
	return "{" + strings.Join(strs, ",") + "}"
}