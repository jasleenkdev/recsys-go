// cmd/promote-qdrant/main.go
package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jasleenkdev/recsys-go/internal/store"
)

// This project's Qdrant runs on host port 6343 (not the default 6333)
// due to a local port conflict with an unrelated Docker stack.
const qdrantURL = "http://localhost:6343"

var httpClient = &http.Client{Timeout: 15 * time.Second}

type qdrantPoint struct {
	ID      int64             `json:"id"`
	Vector  []float64         `json:"vector"`
	Payload map[string]string `json:"payload,omitempty"`
}

type qdrantUpsertRequest struct {
	Points []qdrantPoint `json:"points"`
}

func upsertPoints(collection string, points []qdrantPoint) error {
	body, err := json.Marshal(qdrantUpsertRequest{Points: points})
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/collections/%s/points?wait=true", qdrantURL, collection)
	req, err := http.NewRequest("PUT", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("qdrant returned %d for collection %s", resp.StatusCode, collection)
	}
	return nil
}

func main() {
	db, err := sql.Open("pgx", "postgres://jasleenkaur@localhost:5432/recsys?sslmode=disable")
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	if err := pushRepoEmbeddings(db); err != nil {
		log.Fatalf("repo embedding push failed: %v", err)
	}
	if err := pushReadmeChunks(db); err != nil {
		log.Fatalf("readme chunk push failed: %v", err)
	}

	log.Println("sweep 2 complete: Postgres data pushed to Qdrant")
}

func pushRepoEmbeddings(db *sql.DB) error {
	rows, err := db.Query(`
		SELECT ie.item_id, ie.embedding::text, i.title
		FROM item_embeddings ie
		JOIN items i ON i.id = ie.item_id
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	const batchSize = 50
	var batch []qdrantPoint

	for rows.Next() {
		var itemID int64
		var embeddingText, title string
		if err := rows.Scan(&itemID, &embeddingText, &title); err != nil {
			return err
		}

		vec, err := store.ParsePgvectorText(embeddingText)
		if err != nil {
			log.Printf("  skipping item %d: %v", itemID, err)
			continue
		}

		batch = append(batch, qdrantPoint{
			ID:      itemID,
			Vector:  vec,
			Payload: map[string]string{"title": title},
		})

		if len(batch) >= batchSize {
			if err := upsertPoints("repo_embeddings", batch); err != nil {
				return err
			}
			log.Printf("  pushed batch of %d to repo_embeddings", len(batch))
			batch = nil
		}
	}
	if len(batch) > 0 {
		if err := upsertPoints("repo_embeddings", batch); err != nil {
			return err
		}
		log.Printf("  pushed final batch of %d to repo_embeddings", len(batch))
	}
	return nil
}

func pushReadmeChunks(db *sql.DB) error {
	rows, err := db.Query(`
		SELECT id, embedding::text, item_id
		FROM readme_chunks
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	const batchSize = 50
	var batch []qdrantPoint

	for rows.Next() {
		var chunkID, itemID int64
		var embeddingText string
		if err := rows.Scan(&chunkID, &embeddingText, &itemID); err != nil {
			return err
		}

		vec, err := store.ParsePgvectorText(embeddingText)
		if err != nil {
			log.Printf("  skipping chunk %d: %v", chunkID, err)
			continue
		}

		batch = append(batch, qdrantPoint{
			ID:      chunkID,
			Vector:  vec,
			Payload: map[string]string{"item_id": fmt.Sprintf("%d", itemID)},
		})

		if len(batch) >= batchSize {
			if err := upsertPoints("readme_chunks", batch); err != nil {
				return err
			}
			log.Printf("  pushed batch of %d to readme_chunks", len(batch))
			batch = nil
		}
	}
	if len(batch) > 0 {
		if err := upsertPoints("readme_chunks", batch); err != nil {
			return err
		}
		log.Printf("  pushed final batch of %d to readme_chunks", len(batch))
	}
	return nil
}