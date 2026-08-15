package main

import (
	"context"
	"encoding/json"
	"log"

	"github.com/segmentio/kafka-go"
)

type EventType string

const (
	EventViewed        EventType = "viewed"
	EventStarred       EventType = "starred"
	EventClickedReadme EventType = "clicked_readme"
)

type RepoEvent struct {
	EventType EventType `json:"event_type"`
	UserID    string    `json:"user_id"`
	RepoID    string    `json:"repo_id"`
}

func main() {
	event := RepoEvent{
		EventType: EventViewed,
		UserID:    "user-123",
		RepoID:    "repo-456",
	}

	data, err := json.Marshal(event)
	if err != nil {
		log.Fatal(err)
	}

	writer := kafka.NewWriter(kafka.WriterConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "repo-events",
	})
	defer writer.Close()

	err = writer.WriteMessages(context.Background(), kafka.Message{
		Key:   []byte(event.UserID), // partitions by user_id, per our design
		Value: data,
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Message sent successfully!")
}