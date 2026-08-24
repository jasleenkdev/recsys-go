// cmd/promote/main.go
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jasleenkdev/recsys-go/internal/store"
)

const sidecarURL = "http://localhost:8000/embed"

var httpClient = &http.Client{Timeout: 30 * time.Second}

func main() {
	db, err := sql.Open("pgx", "postgres://jasleenkaur@localhost:5432/recsys?sslmode=disable")
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	behavioralModelID, err := getModelID(db, "behavioral")
	if err != nil {
		log.Fatalf("could not find behavioral model: %v", err)
	}
	transcriptModelID, err := getModelID(db, "transcript_search")
	if err != nil {
		log.Fatalf("could not find transcript_search model: %v", err)
	}

	if err := promoteRepos(db, behavioralModelID); err != nil {
		log.Fatalf("repo promotion failed: %v", err)
	}
	if err := promoteChunks(db, transcriptModelID); err != nil {
		log.Fatalf("chunk promotion failed: %v", err)
	}

	log.Println("sweep 1 complete: staging promoted to items/item_embeddings/readme_chunks")
}

func getModelID(db *sql.DB, purpose string) (int64, error) {
	var id int64
	err := db.QueryRow(`SELECT id FROM models WHERE purpose = $1 LIMIT 1`, purpose).Scan(&id)
	return id, err
}

func fetchEmbedding(text string) ([]float64, error) {
	body, _ := json.Marshal(map[string]string{"text": text})
	resp, err := httpClient.Post(sidecarURL, "application/json", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sidecar returned %d", resp.StatusCode)
	}

	var result struct {
		Embedding []float64 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Embedding, nil
}

type stagedRepo struct {
	ID          int64
	GitHubID    int64
	Owner       string
	Name        string
	Description sql.NullString
	Language    sql.NullString
	// TopicsJSON carries topics as JSON rather than a Go slice:
	// database/sql has no array type, so the query casts on the way out
	// and the insert casts back on the way in.
	TopicsJSON string
}

func promoteRepos(db *sql.DB, modelID int64) error {
	rows, err := db.Query(`
		SELECT id, github_id, owner, name, description, language,
		       array_to_json(COALESCE(topics, '{}'))::text
		FROM repo_ingest_staging
		WHERE embedded = false
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var staged []stagedRepo
	for rows.Next() {
		var r stagedRepo
		if err := rows.Scan(&r.ID, &r.GitHubID, &r.Owner, &r.Name, &r.Description,
			&r.Language, &r.TopicsJSON); err != nil {
			return err
		}
		staged = append(staged, r)
	}

	log.Printf("promoting %d repos", len(staged))

	for i, r := range staged {
		text := r.Name
		if r.Description.Valid && r.Description.String != "" {
			text = r.Name + ": " + r.Description.String
		}

		vec, err := fetchEmbedding(text)
		if err != nil {
			log.Printf("  [%d/%d] embed failed for staging id %d: %v", i+1, len(staged), r.ID, err)
			continue
		}

		var itemID int64
		err = db.QueryRow(`
			INSERT INTO items (title, description, owner, language, topics, github_id)
			VALUES ($1, $2, $3, $4,
			        COALESCE((SELECT array_agg(value) FROM json_array_elements_text($5::json)), '{}'),
			        $6)
			RETURNING id
		`, r.Name, r.Description, r.Owner, r.Language, r.TopicsJSON, r.GitHubID).Scan(&itemID)
		if err != nil {
			log.Printf("  [%d/%d] items insert failed for staging id %d: %v", i+1, len(staged), r.ID, err)
			continue
		}

		_, err = db.Exec(`
			INSERT INTO item_embeddings (item_id, model_id, embedding)
			VALUES ($1, $2, $3)
		`, itemID, modelID, store.VectorLiteral(vec))
		if err != nil {
			log.Printf("  [%d/%d] item_embeddings insert failed for staging id %d: %v", i+1, len(staged), r.ID, err)
			continue
		}

		_, err = db.Exec(`
			UPDATE repo_ingest_staging
			SET embedded = true, item_id = $1
			WHERE id = $2
		`, itemID, r.ID)
		if err != nil {
			log.Printf("  [%d/%d] staging update failed for staging id %d: %v", i+1, len(staged), r.ID, err)
			continue
		}

		if (i+1)%50 == 0 {
			log.Printf("  progress: %d/%d repos promoted", i+1, len(staged))
		}
	}

	return nil
}

type stagedChunk struct {
	ID             int64
	StagingRepoID  int64
	SectionHeading sql.NullString
	ChunkText      string
	ChunkIndex     int
}

func promoteChunks(db *sql.DB, modelID int64) error {
	rows, err := db.Query(`
		SELECT c.id, c.staging_repo_id, c.section_heading, c.chunk_text, c.chunk_index
		FROM readme_chunk_staging c
		JOIN repo_ingest_staging r ON r.id = c.staging_repo_id
		WHERE c.embedded = false AND r.item_id IS NOT NULL
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var staged []stagedChunk
	for rows.Next() {
		var c stagedChunk
		if err := rows.Scan(&c.ID, &c.StagingRepoID, &c.SectionHeading, &c.ChunkText, &c.ChunkIndex); err != nil {
			return err
		}
		staged = append(staged, c)
	}

	log.Printf("promoting %d readme chunks", len(staged))

	for i, c := range staged {
		vec, err := fetchEmbedding(c.ChunkText)
		if err != nil {
			log.Printf("  [%d/%d] embed failed for chunk id %d: %v", i+1, len(staged), c.ID, err)
			continue
		}

		var itemID int64
		err = db.QueryRow(`
			SELECT item_id FROM repo_ingest_staging WHERE id = $1
		`, c.StagingRepoID).Scan(&itemID)
		if err != nil {
			log.Printf("  [%d/%d] item_id lookup failed for chunk id %d: %v", i+1, len(staged), c.ID, err)
			continue
		}

		_, err = db.Exec(`
			INSERT INTO readme_chunks (item_id, model_id, chunk_text, section_heading, chunk_index, embedding)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, itemID, modelID, c.ChunkText, c.SectionHeading, c.ChunkIndex, store.VectorLiteral(vec))
		if err != nil {
			log.Printf("  [%d/%d] readme_chunks insert failed for chunk id %d: %v", i+1, len(staged), c.ID, err)
			continue
		}

		_, err = db.Exec(`
			UPDATE readme_chunk_staging SET embedded = true WHERE id = $1
		`, c.ID)
		if err != nil {
			log.Printf("  [%d/%d] staging update failed for chunk id %d: %v", i+1, len(staged), c.ID, err)
			continue
		}

		if (i+1)%200 == 0 {
			log.Printf("  progress: %d/%d chunks promoted", i+1, len(staged))
		}
	}

	return nil
}
