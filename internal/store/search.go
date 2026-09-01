// internal/store/search.go
package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jasleenkdev/recsys-go/internal/config"
)

const (
	relevanceThreshold  = 0.5 // provisional — calibrate against real score distributions
	maxChunksForContext = 5
)

const ollamaModel = "llama3.2:3b"

var ollamaClient = &http.Client{Timeout: 60 * time.Second}

type SearchStatus string

const (
	StatusGrounded       SearchStatus = "grounded"
	StatusNoResults      SearchStatus = "no_results"
	StatusRetrievalError SearchStatus = "retrieval_error"
)

type Citation struct {
	ItemID         int64
	Title          string
	RepoURL        string
	ChunkText      string
	SectionHeading string
	ChunkIndex     int
	RelevanceScore float64
}

type SearchResult struct {
	Status    SearchStatus
	Answer    string // empty unless Status == StatusGrounded
	Citations []Citation
}

type qdrantChunkSearchResponse struct {
	Result []struct {
		ID      int64             `json:"id"`
		Score   float64           `json:"score"`
		Payload map[string]string `json:"payload"`
	} `json:"result"`
}

type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type ollamaResponse struct {
	Response string `json:"response"`
}

// SearchReadmes performs a grounded semantic search over README chunks.
// It never returns an LLM answer without retrieval grounding — if no
// chunk clears the relevance threshold, it returns StatusNoResults
// without ever calling the LLM.
func SearchReadmes(ctx context.Context, db *sql.DB, query string) (SearchResult, error) {
	queryVec, err := fetchEmbedding(ctx, query)
	if err != nil {
		return SearchResult{Status: StatusRetrievalError}, fmt.Errorf("embedding query: %w", err)
	}

	chunks, err := searchReadmeChunks(ctx, db, queryVec, maxChunksForContext)
	if err != nil {
		return SearchResult{Status: StatusRetrievalError}, fmt.Errorf("querying qdrant: %w", err)
	}

	var relevant []Citation
	for _, c := range chunks {
		if c.RelevanceScore >= relevanceThreshold {
			relevant = append(relevant, c)
		}
	}

	if len(relevant) == 0 {
		return SearchResult{Status: StatusNoResults}, nil
	}

	answer, err := generateGroundedAnswer(ctx, query, relevant)
	if err != nil {
		return SearchResult{Status: StatusRetrievalError}, fmt.Errorf("generating answer: %w", err)
	}

	return SearchResult{
		Status:    StatusGrounded,
		Answer:    answer,
		Citations: relevant,
	}, nil
}

func fetchEmbedding(ctx context.Context, text string) ([]float64, error) {
	body, _ := json.Marshal(map[string]string{"text": text})
	req, err := http.NewRequestWithContext(ctx, "POST", config.Load().EmbedSidecarURL, bytes.NewReader(body))
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
		return nil, fmt.Errorf("embedding sidecar returned %d", resp.StatusCode)
	}

	var result struct {
		Embedding []float64 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Embedding, nil
}

func searchReadmeChunks(ctx context.Context, db *sql.DB, vec []float64, limit int) ([]Citation, error) {
	body, err := json.Marshal(qdrantSearchRequest{
		Vector:      vec,
		Limit:       limit,
		WithPayload: true,
	})
	if err != nil {
		return nil, err
	}

	url := config.Load().QdrantURL + "/collections/readme_chunks/points/search"
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

	var result qdrantChunkSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	// Qdrant's payload only stores item_id (set during promotion) —
	// chunk_text, section_heading, and chunk_index live in Postgres,
	// so we need a follow-up lookup by chunk ID (Qdrant point ID).
	chunkIDs := make([]int64, len(result.Result))
	scores := make(map[int64]float64)
	for i, r := range result.Result {
		chunkIDs[i] = r.ID
		scores[r.ID] = r.Score
	}

	return fetchChunkDetails(ctx, db, chunkIDs, scores)
}

// fetchChunkDetails looks up chunk text, section heading, and item_id
// from Postgres for a set of Qdrant point IDs (readme_chunks.id),
// attaching each chunk's similarity score from the Qdrant search.
//
// The JOIN onto items carries the repo's title and owner back with the
// chunk. A citation is only useful if the reader can tell which repo it
// came from, and resolving that client-side meant one detail fetch per
// citation for two columns that sit one foreign key away from the rows
// this query already reads. The join is on items' primary key, and at
// most maxChunksForContext rows reach it.
func fetchChunkDetails(ctx context.Context, db *sql.DB, chunkIDs []int64, scores map[int64]float64) ([]Citation, error) {
	if len(chunkIDs) == 0 {
		return nil, nil
	}

	rows, err := db.QueryContext(ctx, `
		SELECT c.id, c.item_id, c.chunk_text, c.section_heading, c.chunk_index,
		       i.title, i.owner
		FROM readme_chunks c
		JOIN items i ON i.id = c.item_id
		WHERE c.id = ANY($1)
	`, pqInt64Array(chunkIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var citations []Citation
	for rows.Next() {
		var id, itemID int64
		var chunkText string
		var sectionHeading sql.NullString
		var chunkIndex int
		var title, owner string
		if err := rows.Scan(&id, &itemID, &chunkText, &sectionHeading, &chunkIndex, &title, &owner); err != nil {
			return nil, err
		}
		citations = append(citations, Citation{
			ItemID:         itemID,
			Title:          title,
			RepoURL:        GitHubURL(owner, title),
			ChunkText:      chunkText,
			SectionHeading: sectionHeading.String,
			ChunkIndex:     chunkIndex,
			RelevanceScore: scores[id],
		})
	}
	return citations, rows.Err()
}

// generateGroundedAnswer calls the local Ollama model, instructing it
// to answer strictly from the provided chunks and nothing else.
func generateGroundedAnswer(ctx context.Context, query string, citations []Citation) (string, error) {
	var contextBuilder strings.Builder
	for i, c := range citations {
		fmt.Fprintf(&contextBuilder, "[%d] %s\n\n", i+1, c.ChunkText)
	}

	prompt := fmt.Sprintf(`You are answering a question using ONLY the context provided below.
If the context does not contain enough information to answer the question,
say "I don't have enough information to answer that" — do not use any
outside knowledge.

Context:
%s

Question: %s

Answer:`, contextBuilder.String(), query)

	body, err := json.Marshal(ollamaRequest{
		Model:  ollamaModel,
		Prompt: prompt,
		Stream: false,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", config.Load().OllamaURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ollamaClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama returned %d", resp.StatusCode)
	}

	var result ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Response, nil
}