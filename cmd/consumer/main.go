// cmd/consumer/main.go
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver
	"github.com/segmentio/kafka-go"

	"github.com/jasleenkdev/recsys-go/internal/config"
	"github.com/jasleenkdev/recsys-go/internal/domain"
	"github.com/jasleenkdev/recsys-go/internal/events"
	"github.com/jasleenkdev/recsys-go/internal/store"
)

// retryBackoff bounds the wait between attempts at a transiently failing
// insert: it starts at initial and doubles up to max. It bounds the delay,
// not the number of attempts — see insertWithRetry for why.
type retryBackoff struct {
	initial time.Duration
	max     time.Duration
}

var defaultBackoff = retryBackoff{initial: 200 * time.Millisecond, max: 30 * time.Second}

// committer is the one method of *kafka.Reader that processMessage needs,
// narrowed to an interface so the retry path is testable without a broker.
type committer interface {
	CommitMessages(ctx context.Context, msgs ...kafka.Message) error
}

func main() {
	ctx := context.Background()
	cfg := config.Load()

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	behavioralModelID, err := getModelID(db, "behavioral")
	if err != nil {
		log.Fatalf("could not find behavioral model: %v", err)
	}

	kafkaDialer, err := events.Dialer(cfg)
	if err != nil {
		log.Fatalf("invalid kafka security config: %v", err)
	}

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: cfg.KafkaBrokers,
		Topic:   "repo-events",
		GroupID: "events-consumer",
		// nil keeps kafka-go's default plaintext, unauthenticated dialer.
		Dialer: kafkaDialer,
	})
	defer reader.Close()

	insert := func(ctx context.Context, e domain.RepoEvent) error {
		return store.InsertEvent(ctx, db, e)
	}

	log.Println("consumer started, waiting for messages...")

	for {
		msg, err := reader.FetchMessage(ctx)
		if err != nil {
			log.Printf("fetch error: %v", err)
			continue
		}

		event, inserted, err := processMessage(ctx, reader, msg, insert, defaultBackoff)
		if err != nil {
			// ctx ended while this message was still failing. It is
			// uncommitted, so the group redelivers it after a restart —
			// but only if we stop here: fetching on would let the next
			// commit on this partition skip past it.
			log.Printf("stopping with partition %d offset %d uncommitted: %v", msg.Partition, msg.Offset, err)
			return
		}
		if !inserted {
			continue
		}

		if err := store.RecomputeUserEmbedding(ctx, db, event.UserID, behavioralModelID); err != nil {
			log.Printf("failed to recompute embedding for user %d: %v", event.UserID, err)
		}
	}
}

// processMessage takes one fetched message through to its commit.
//
// It returns inserted=true once the event is stored, so the caller goes
// on to recompute the user's embedding. It returns inserted=false, with
// the offset committed, for messages that can never succeed: malformed
// JSON, an invalid event, or a permanent insert error.
//
// A transient insert error does not return until it resolves (see
// insertWithRetry). A non-nil error means ctx ended first: the message
// was NOT committed, and the caller must stop rather than fetch past it.
func processMessage(
	ctx context.Context,
	c committer,
	msg kafka.Message,
	insert func(context.Context, domain.RepoEvent) error,
	backoff retryBackoff,
) (domain.RepoEvent, bool, error) {
	var event domain.RepoEvent
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		log.Printf("skipping malformed message: %v", err)
		if cErr := c.CommitMessages(ctx, msg); cErr != nil {
			log.Printf("commit failed after skip: %v", cErr)
		}
		return event, false, nil
	}

	if err := event.Validate(); err != nil {
		log.Printf("skipping invalid event: %v", err)
		if cErr := c.CommitMessages(ctx, msg); cErr != nil {
			log.Printf("commit failed after skip: %v", cErr)
		}
		return event, false, nil
	}

	err := insertWithRetry(ctx, insert, event, backoff)
	switch {
	case err == nil:
		if cErr := c.CommitMessages(ctx, msg); cErr != nil {
			log.Printf("commit failed: %v", cErr)
		}
		return event, true, nil
	case store.IsPermanent(err):
		log.Printf("skipping event with permanent error: %v", err)
		if cErr := c.CommitMessages(ctx, msg); cErr != nil {
			log.Printf("commit failed after skip: %v", cErr)
		}
		return event, false, nil
	default:
		return event, false, err
	}
}

// insertWithRetry calls insert until it succeeds, fails permanently, or
// ctx ends. It deliberately has no attempt limit.
//
// WHY THIS BLOCKS — do not "simplify" it back to log-and-continue:
//
// kafka-go's Reader.FetchMessage advances the reader's in-memory position
// to offset+1 the moment it hands a message over, committed or not, and a
// consumer-group reader has no per-message rewind. CommitMessages then
// writes msg.Offset+1 as the group's offset for that partition. So when a
// transient insert failure was handled by skipping the commit and
// fetching the next message, the next successful commit on the same
// partition moved the group offset past the failed message and it was
// lost for good. The log said "will retry"; nothing ever retried it
// unless the process happened to restart before that next commit.
//
// With this client the only safe option is to not fetch again until the
// current message is resolved. Blocking is also right in practice: a
// transient error means Postgres is unreachable or overloaded, so every
// message behind this one would fail the same way. It does not cost group
// membership either — kafka-go heartbeats from its own goroutine,
// independent of FetchMessage. Retrying is safe because InsertEvent
// dedupes on (event_id, occurred_at), so an attempt that reached Postgres
// but reported an error is not double-counted.
//
// There is no dead-letter fallback on purpose. A DLQ table would live in
// the same Postgres that is failing, and a DLQ topic would store a user's
// events out of the user_id-keyed partition order the producer guarantees.
// If a persistent error is ever misclassified as transient, the attempt
// count in the log makes the stall visible; the fix then belongs in
// store.IsPermanent, not in skipping the message here.
func insertWithRetry(
	ctx context.Context,
	insert func(context.Context, domain.RepoEvent) error,
	event domain.RepoEvent,
	backoff retryBackoff,
) error {
	delay := backoff.initial
	for attempt := 1; ; attempt++ {
		err := insert(ctx, event)
		if err == nil || store.IsPermanent(err) {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		log.Printf("insert failed for event %s (attempt %d), retrying in %s: %v", event.EventID, attempt, delay, err)

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		delay = min(delay*2, backoff.max)
	}
}

func getModelID(db *sql.DB, purpose string) (int64, error) {
	var id int64
	err := db.QueryRow(`SELECT id FROM models WHERE purpose = $1 LIMIT 1`, purpose).Scan(&id)
	return id, err
}
