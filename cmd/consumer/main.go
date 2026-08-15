// cmd/consumer/main.go
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver
	"github.com/segmentio/kafka-go"

	"github.com/jasleenkdev/recsys-go/internal/domain"
	"github.com/jasleenkdev/recsys-go/internal/store"
)

func main() {
	ctx := context.Background()

	db, err := sql.Open("pgx", "postgres://jasleenkaur@localhost:5432/recsys?sslmode=disable")
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "repo-events",
		GroupID: "events-consumer", // makes this a real consumer group member
	})
	defer reader.Close()

	log.Println("consumer started, waiting for messages...")

	for {
		msg, err := reader.FetchMessage(ctx) // does NOT auto-commit
		if err != nil {
			log.Printf("fetch error: %v", err)
			continue
		}
	
		var event domain.RepoEvent
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			log.Printf("skipping malformed message: %v", err)
			if cErr := reader.CommitMessages(ctx, msg); cErr != nil {
				log.Printf("commit failed after skip: %v", cErr)
			}
			continue
		}
	
		if err := event.Validate(); err != nil {
			log.Printf("skipping invalid event: %v", err)
			if cErr := reader.CommitMessages(ctx, msg); cErr != nil {
				log.Printf("commit failed after skip: %v", cErr)
			}
			continue
		}
	
		if err := store.InsertEvent(ctx, db, event); err != nil {
			if store.IsPermanent(err) {
				log.Printf("skipping event with permanent error: %v", err)
				if cErr := reader.CommitMessages(ctx, msg); cErr != nil {
					log.Printf("commit failed after skip: %v", cErr)
				}
			} else {
				log.Printf("insert failed, will retry: %v", err)
			}
			continue
		}
	
		if err := reader.CommitMessages(ctx, msg); err != nil {
			log.Printf("commit failed: %v", err)
		}
	}

	
}
