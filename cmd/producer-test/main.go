package main

import (
	"context"
	"encoding/json"
	"log"
	"strconv"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"github.com/jasleenkdev/recsys-go/internal/domain"
)
func main() {
	event := domain.RepoEvent{
		EventID:   uuid.NewString(),
		EventType: domain.EventViewed,
		UserID:    1,
		RepoID:    1,
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
		Key: []byte(strconv.FormatInt(event.UserID, 10)),
		Value: data,
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Message sent successfully!")
}