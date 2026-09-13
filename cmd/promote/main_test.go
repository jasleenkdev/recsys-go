// cmd/promote/main_test.go
package main

// These are integration tests against a real Postgres with the pgvector
// extension available. They are skipped unless RECSYS_TEST_DATABASE_URL
// is set to a URL-form DSN:
//
//	RECSYS_TEST_DATABASE_URL='postgres://jasleenkaur@localhost:5432/recsys?sslmode=disable' \
//	  go test ./cmd/promote
//
// They never touch existing tables: every migration is applied into a
// throwaway schema, which is dropped when the test ends.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func TestStarsSurvivePromotion(t *testing.T) {
	db := newMigratedTestDB(t)

	// One stub for the whole test: fetchEmbedding reads the sidecar URL
	// through the memoized config.Load(), so the first value set in this
	// test binary is the only one it will ever see.
	stubSidecar(t)

	t.Run("promoteRepos copies stars from staging", func(t *testing.T) {
		const githubID, stars = 900000001, 4242
		mustExec(t, db, `
			INSERT INTO repo_ingest_staging (github_id, owner, name, description, stars, topics, language)
			VALUES ($1, 'octo', 'stars-survive', 'fixture repo', $2, '{go}', 'Go')
		`, githubID, stars)

		modelID, err := getModelID(db, "behavioral")
		if err != nil {
			t.Fatalf("getModelID: %v", err)
		}
		if err := promoteRepos(db, modelID); err != nil {
			t.Fatalf("promoteRepos: %v", err)
		}

		var got int
		if err := db.QueryRow(`SELECT stars FROM items WHERE github_id = $1`, githubID).Scan(&got); err != nil {
			t.Fatalf("item was not promoted: %v", err)
		}
		if got != stars {
			t.Errorf("items.stars = %d, want %d (the staged count)", got, stars)
		}
	})

	t.Run("000016 backfills stars zeroed by the old promote", func(t *testing.T) {
		const githubID, stars = 900000002, 777

		// Reproduce what the buggy promote left behind: an item with
		// stars = 0, linked from a staging row that holds the real count.
		var itemID int64
		if err := db.QueryRow(`
			INSERT INTO items (title, owner, github_id) VALUES ('zeroed', 'octo', $1) RETURNING id
		`, githubID).Scan(&itemID); err != nil {
			t.Fatalf("inserting zeroed item: %v", err)
		}
		mustExec(t, db, `
			INSERT INTO repo_ingest_staging (github_id, owner, name, stars, embedded, item_id)
			VALUES ($1, 'octo', 'zeroed', $2, true, $3)
		`, githubID, stars, itemID)

		backfill, err := os.ReadFile("../../migrations/000016_backfill_item_stars.up.sql")
		if err != nil {
			t.Fatal(err)
		}
		mustExec(t, db, string(backfill))

		var got int
		if err := db.QueryRow(`SELECT stars FROM items WHERE id = $1`, itemID).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != stars {
			t.Errorf("items.stars after backfill = %d, want %d", got, stars)
		}
	})
}

// newMigratedTestDB returns a connection whose search_path is a fresh
// schema with every up migration applied, or skips the test when no test
// database is configured.
func newMigratedTestDB(t *testing.T) *sql.DB {
	t.Helper()
	base := os.Getenv("RECSYS_TEST_DATABASE_URL")
	if base == "" {
		t.Skip("RECSYS_TEST_DATABASE_URL not set; skipping Postgres integration test")
	}

	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatalf("opening test database: %v", err)
	}
	t.Cleanup(func() { admin.Close() })

	schema := fmt.Sprintf("promote_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("creating schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Errorf("dropping schema %s: %v", schema, err)
		}
	})

	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("RECSYS_TEST_DATABASE_URL must be a URL-form DSN: %v", err)
	}
	q := u.Query()
	// public stays on the path so the pgvector type resolves.
	q.Set("search_path", schema+",public")
	u.RawQuery = q.Encode()

	db, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatalf("opening schema connection: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	files, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("finding migrations: %v (found %d)", err, len(files))
	}
	sort.Strings(files)
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		// No arguments, so pgx uses the simple protocol, which accepts
		// multi-statement files and DO blocks.
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatalf("applying %s: %v", filepath.Base(f), err)
		}
	}
	return db
}

// stubSidecar serves a constant 768-dim embedding in place of the Python
// sidecar and points EMBED_SIDECAR_URL at it.
func stubSidecar(t *testing.T) {
	t.Helper()
	vec := make([]float64, 768)
	for i := range vec {
		vec[i] = 0.001
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string][]float64{"embedding": vec})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("EMBED_SIDECAR_URL", srv.URL)
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec failed: %v\n%s", err, query)
	}
}
