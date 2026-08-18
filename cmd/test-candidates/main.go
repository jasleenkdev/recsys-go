// cmd/test-candidates/main.go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jasleenkdev/recsys-go/internal/store"
)

func main() {
	ctx := context.Background()

	db, err := sql.Open("pgx", "postgres://jasleenkaur@localhost:5432/recsys?sslmode=disable")
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	const testUserID = 1

	result, err := store.GetCandidates(ctx, db, testUserID)
	if err != nil {
		log.Fatalf("GetCandidates failed: %v", err)
	}

	if result.FallbackReason != "" {
		fmt.Printf("user %d: fallback triggered, reason: %s\n", testUserID, result.FallbackReason)
		return
	}

	fmt.Printf("user %d: %d candidate item IDs\n", testUserID, len(result.ItemIDs))
	for i, id := range result.ItemIDs {
		fmt.Printf("  %d. item_id=%d\n", i+1, id)
	}
}