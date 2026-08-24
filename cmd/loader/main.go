
// cmd/loader/main.go
package main

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/jasleenkdev/recsys-go/internal/config"
)

// --- GitHub API response shapes ---

type searchResponse struct {
	Items []repoItem `json:"items"`
}

type repoItem struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	FullName    string   `json:"full_name"`
	Owner       struct {
		Login string `json:"login"`
	} `json:"owner"`
	Description string   `json:"description"`
	Stars       int      `json:"stargazers_count"`
	Language    string   `json:"language"`
	Topics      []string `json:"topics"`
}

type readmeResponse struct {
	Content  string `json:"content"`  // base64-encoded
	Encoding string `json:"encoding"` // should be "base64"
}

// --- category definitions ---

var categories = []string{
	"language:go+topic:backend",
	"language:python+topic:machine-learning",
	"language:javascript+topic:frontend",
	"language:rust+topic:cli",
}

const reposPerCategory = 150
const perPage = 50

var (
	githubToken = os.Getenv("GITHUB_TOKEN")
	httpClient  = &http.Client{Timeout: 15 * time.Second}
)

func main() {
	if githubToken == "" {
		log.Fatal("GITHUB_TOKEN not set")
	}

	db, err := sql.Open("pgx", config.Load().DatabaseURL)
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	for _, category := range categories {
		log.Printf("fetching category: %s", category)
		repos, err := fetchRepos(category, reposPerCategory)
		if err != nil {
			log.Printf("error fetching category %s: %v", category, err)
			continue
		}

		for i, repo := range repos {
			log.Printf("[%s] repo %d/%d: %s", category, i+1, len(repos), repo.FullName)

			stagingID, err := insertStagingRepo(db, repo)
			if err != nil {
				log.Printf("  skipping (staging insert failed): %v", err)
				continue
			}
			if stagingID == 0 {
				log.Printf("  already staged, skipping readme fetch")
				continue
			}

			readme, err := fetchReadme(repo.Owner.Login, repo.Name)
			if err != nil {
				log.Printf("  no readme or fetch failed: %v", err)
				continue
			}

			chunks := chunkReadme(readme)
			if err := insertStagingChunks(db, stagingID, chunks); err != nil {
				log.Printf("  chunk insert failed: %v", err)
			}
		}
	}

	log.Println("loading complete")
}

// fetchRepos paginates GitHub's search API until it has `limit` repos
// or runs out of results, whichever comes first.
func fetchRepos(query string, limit int) ([]repoItem, error) {
	var all []repoItem
	page := 1

	for len(all) < limit {
		url := fmt.Sprintf(
			"https://api.github.com/search/repositories?q=%s&per_page=%d&page=%d",
			query, perPage, page,
		)
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+githubToken)
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := httpClient.Do(req)
		if err != nil {
			return all, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return all, fmt.Errorf("github search returned %d: %s", resp.StatusCode, body)
		}

		var sr searchResponse
		if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
			return all, err
		}
		if len(sr.Items) == 0 {
			break // no more results
		}

		all = append(all, sr.Items...)
		page++

		time.Sleep(2 * time.Second) // stay well under the search rate limit
	}

	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

func fetchReadme(owner, name string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/readme", owner, name)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+githubToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("readme returned %d", resp.StatusCode)
	}

	var rr readmeResponse
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		return "", err
	}

	decoded, err := base64.StdEncoding.DecodeString(
		strings.ReplaceAll(rr.Content, "\n", ""),
	)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

// chunkReadme splits on markdown ## headings. Falls back to one chunk
// if no headings are found.
type readmeChunk struct {
	Heading string
	Text    string
	Index   int
}

func chunkReadme(text string) []readmeChunk {
	lines := strings.Split(text, "\n")
	var chunks []readmeChunk
	var currentHeading string
	var buf strings.Builder
	idx := 0

	flush := func() {
		content := strings.TrimSpace(buf.String())
		if content != "" {
			chunks = append(chunks, readmeChunk{
				Heading: currentHeading,
				Text:    content,
				Index:   idx,
			})
			idx++
		}
		buf.Reset()
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			flush()
			currentHeading = strings.TrimPrefix(line, "## ")
			continue
		}
		buf.WriteString(line)
		buf.WriteString("\n")
	}
	flush()

	if len(chunks) == 0 && strings.TrimSpace(text) != "" {
		chunks = append(chunks, readmeChunk{Text: strings.TrimSpace(text), Index: 0})
	}
	return chunks
}

// insertStagingRepo writes repo metadata, returning 0 (not an error) if
// this repo was already staged in a previous run.
func insertStagingRepo(db *sql.DB, r repoItem) (int64, error) {
	const query = `
		INSERT INTO repo_ingest_staging
			(github_id, owner, name, description, stars, topics, language)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (github_id) DO NOTHING
		RETURNING id
	`
	var id int64
	err := db.QueryRow(
		query, r.ID, r.Owner.Login, r.Name, r.Description, r.Stars,
		pqStringArray(r.Topics), r.Language,
	).Scan(&id)

	if err == sql.ErrNoRows {
		return 0, nil // conflict: already staged
	}
	return id, err
}

func insertStagingChunks(db *sql.DB, stagingRepoID int64, chunks []readmeChunk) error {
	const query = `
		INSERT INTO readme_chunk_staging
			(staging_repo_id, section_heading, chunk_text, chunk_index)
		VALUES ($1, $2, $3, $4)
	`
	for _, c := range chunks {
		if _, err := db.Exec(query, stagingRepoID, nullableString(c.Heading), c.Text, c.Index); err != nil {
			return err
		}
	}
	return nil
}

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// pqStringArray formats a Go string slice as a Postgres array literal.
func pqStringArray(ss []string) string {
	return "{" + strings.Join(ss, ",") + "}"
}