// internal/events/producer.go
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/jasleenkdev/recsys-go/internal/domain"
)

// Producer publishes engagement events onto the Kafka topic the
// consumer reads. The API deliberately does not write to Postgres
// directly: the consumer is what recomputes the user's embedding after
// an insert, so a direct write would persist the event but leave the
// user's recommendations frozen at their pre-event state.
type Producer struct {
	writer *kafka.Writer
}

// NewProducer builds a synchronous, fully-acknowledged producer.
//
// Both settings are explicit on purpose. A bare &kafka.Writer{} defaults
// RequiredAcks to RequireNone — fire-and-forget — so the API would
// answer 202 Accepted having guaranteed nothing; only the deprecated
// kafka.NewWriter path quietly substitutes RequireAll. Async stays false
// so WriteMessages surfaces the broker's error to the handler instead of
// swallowing it.
func NewProducer(brokers []string, topic string) *Producer {
	return &Producer{
		writer: &kafka.Writer{
			Addr:         kafka.TCP(brokers...),
			Topic:        topic,
			Balancer:     &kafka.Hash{},
			RequiredAcks: kafka.RequireAll,
			Async:        false,
			WriteTimeout: 5 * time.Second,
		},
	}
}

// Publish writes one event, keyed by user_id. The key matters: hashing
// on it keeps a single user's events on one partition and therefore in
// order, so the consumer never recomputes an embedding from a stale
// prefix of that user's history.
func (p *Producer) Publish(ctx context.Context, e domain.RepoEvent) error {
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshaling event: %w", err)
	}

	err = p.writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(strconv.FormatInt(e.UserID, 10)),
		Value: data,
	})
	if err != nil {
		return fmt.Errorf("publishing event: %w", err)
	}
	return nil
}

func (p *Producer) Close() error {
	return p.writer.Close()
}
