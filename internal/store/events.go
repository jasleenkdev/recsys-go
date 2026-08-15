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
// including the case where the event was already inserted (idempotent
// via ON CONFLICT), which the consumer treats as "safe to commit".
func InsertEvent(ctx context.Context, db *sql.DB, e domain.RepoEvent) error {
	const query = `
		INSERT INTO events (event_id, user_id, item_id, event_type, created_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (event_id, created_at) DO NOTHING
	`
	_, err := db.ExecContext(ctx, query, e.EventID, e.UserID, e.RepoID, string(e.EventType))
	return err
}
