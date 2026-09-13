// cmd/consumer/main_test.go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/segmentio/kafka-go"

	"github.com/jasleenkdev/recsys-go/internal/domain"
)

// fastBackoff keeps retry tests in the millisecond range.
var fastBackoff = retryBackoff{initial: time.Millisecond, max: 4 * time.Millisecond}

var errTransient = errors.New("dial tcp 127.0.0.1:5432: connect: connection refused")

// recordingCommitter stands in for *kafka.Reader and records every
// committed offset, in order.
type recordingCommitter struct {
	offsets []int64
}

func (r *recordingCommitter) CommitMessages(_ context.Context, msgs ...kafka.Message) error {
	for _, m := range msgs {
		r.offsets = append(r.offsets, m.Offset)
	}
	return nil
}

func eventMessage(t *testing.T, offset int64, eventID string) kafka.Message {
	t.Helper()
	data, err := json.Marshal(domain.RepoEvent{
		EventID:    eventID,
		EventType:  domain.EventStarred,
		UserID:     1,
		RepoID:     1,
		OccurredAt: time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	return kafka.Message{Topic: "repo-events", Partition: 0, Offset: offset, Value: data}
}

// TestTransientFailureIsRetriedBeforeMovingOn is the regression test for
// silent event loss. Offset 10 fails transiently; offset 11 would succeed.
// The old loop logged "will retry", fetched 11 and committed it, moving
// the group offset past 10 without ever storing it. Offset 10 must be
// stored and committed before 11 is touched.
func TestTransientFailureIsRetriedBeforeMovingOn(t *testing.T) {
	ctx := context.Background()
	c := &recordingCommitter{}

	failuresLeft := map[string]int{"event-10": 3}
	var stored []string
	insert := func(_ context.Context, e domain.RepoEvent) error {
		if failuresLeft[e.EventID] > 0 {
			failuresLeft[e.EventID]--
			return errTransient
		}
		stored = append(stored, e.EventID)
		return nil
	}

	// Drive messages the way main's loop does: the next one only after
	// processMessage has returned for the current one.
	for _, msg := range []kafka.Message{
		eventMessage(t, 10, "event-10"),
		eventMessage(t, 11, "event-11"),
	} {
		_, inserted, err := processMessage(ctx, c, msg, insert, fastBackoff)
		if err != nil {
			t.Fatalf("offset %d: unexpected error: %v", msg.Offset, err)
		}
		if !inserted {
			t.Fatalf("offset %d: inserted = false, want true", msg.Offset)
		}
	}

	if want := []string{"event-10", "event-11"}; !slices.Equal(stored, want) {
		t.Errorf("stored = %v, want %v", stored, want)
	}
	if want := []int64{10, 11}; !slices.Equal(c.offsets, want) {
		t.Errorf("committed offsets = %v, want %v", c.offsets, want)
	}
}

// TestCancelDuringRetryLeavesMessageUncommitted checks the shutdown path:
// a message still failing when ctx ends must not be committed, so the
// consumer group redelivers it on restart.
func TestCancelDuringRetryLeavesMessageUncommitted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &recordingCommitter{}

	attempts := 0
	insert := func(context.Context, domain.RepoEvent) error {
		attempts++
		if attempts == 3 {
			cancel()
		}
		return errTransient
	}

	_, inserted, err := processMessage(ctx, c, eventMessage(t, 10, "event-10"), insert, fastBackoff)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if inserted {
		t.Error("inserted = true, want false")
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
	if len(c.offsets) != 0 {
		t.Errorf("committed offsets = %v, want none", c.offsets)
	}
}

// TestPermanentFailureIsCommittedWithoutRetry pins the other branch: an
// error that can never succeed is skipped and committed after one attempt,
// rather than stalling the consumer forever.
func TestPermanentFailureIsCommittedWithoutRetry(t *testing.T) {
	c := &recordingCommitter{}

	attempts := 0
	insert := func(context.Context, domain.RepoEvent) error {
		attempts++
		return &pgconn.PgError{Code: "23503"} // foreign_key_violation
	}

	_, inserted, err := processMessage(context.Background(), c, eventMessage(t, 10, "event-10"), insert, fastBackoff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inserted {
		t.Error("inserted = true, want false")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
	if want := []int64{10}; !slices.Equal(c.offsets, want) {
		t.Errorf("committed offsets = %v, want %v", c.offsets, want)
	}
}
