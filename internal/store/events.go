// internal/store/events.go
package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jasleenkdev/recsys-go/internal/domain"
)

// IsPermanent reports whether err represents a failure that will never
// succeed on retry (bad data, constraint violation) as opposed to a
// transient one (connection dropped, DB temporarily unavailable).
func IsPermanent(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "22P02", // invalid_text_representation (bad input syntax)
			"23503", // foreign_key_violation
			"23502": // not_null_violation
			return true
		}
	}
	return false
}

// InsertEvent writes a RepoEvent to Postgres. Returns nil on success —
// including the case where the event was already inserted, which the
// consumer treats as "safe to commit".
//
// Dedupe is on (event_id, created_at) rather than event_id alone
// because events is partitioned by created_at, and Postgres requires a
// unique constraint to contain the partition key. That makes the
// timestamp part of the identity of an event: it has to travel with the
// event, not be stamped here. Stamping now() on each attempt — as this
// did previously — gave every replay a fresh key, so the ON CONFLICT
// could never fire and retries silently double-counted.
func InsertEvent(ctx context.Context, db *sql.DB, e domain.RepoEvent) error {
	const query = `
		INSERT INTO events (event_id, user_id, item_id, event_type, created_at)
		VALUES ($1, $2, $3, $4, COALESCE($5::timestamptz, now()))
		ON CONFLICT (event_id, created_at) DO NOTHING
	`

	// Producers predating OccurredAt send the zero time; fall back to
	// now() for those rather than inserting year 1, which no partition
	// would accept.
	var occurredAt any
	if !e.OccurredAt.IsZero() {
		occurredAt = e.OccurredAt
	}

	_, err := db.ExecContext(ctx, query, e.EventID, e.UserID, e.RepoID, string(e.EventType), occurredAt)
	return err
}
