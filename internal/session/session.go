// internal/session/session.go
package session

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/jasleenkdev/recsys-go/internal/store"
)

const sessionTTL = 10 * time.Minute

// Cursor is the decoded form of the opaque pagination cursor.
type Cursor struct {
	SessionID string `json:"session_id"`
	Offset    int    `json:"offset"`
}

// EncodeCursor base64-encodes a Cursor for use as an opaque API token.
func EncodeCursor(c Cursor) string {
	data, _ := json.Marshal(c) // Cursor is always marshalable; error impossible here
	return base64.URLEncoding.EncodeToString(data)
}

// DecodeCursor reverses EncodeCursor. A malformed or tampered cursor
// returns an error — callers should treat this the same as "no
// cursor" (start a fresh session) rather than surfacing it to the user.
func DecodeCursor(raw string) (Cursor, error) {
	data, err := base64.URLEncoding.DecodeString(raw)
	if err != nil {
		return Cursor{}, fmt.Errorf("invalid cursor encoding: %w", err)
	}
	var c Cursor
	if err := json.Unmarshal(data, &c); err != nil {
		return Cursor{}, fmt.Errorf("invalid cursor contents: %w", err)
	}
	return c, nil
}

// CreateSession stores a full ranked candidate pool under a new
// session ID, with a TTL so abandoned sessions don't accumulate.
func CreateSession(ctx context.Context, rdb *redis.Client, items []store.RankedItem) (string, error) {
	sessionID := uuid.NewString()

	data, err := json.Marshal(items)
	if err != nil {
		return "", fmt.Errorf("marshaling session items: %w", err)
	}

	key := "rec_session:" + sessionID
	if err := rdb.Set(ctx, key, data, sessionTTL).Err(); err != nil {
		return "", fmt.Errorf("storing session in redis: %w", err)
	}

	return sessionID, nil
}

// GetSession retrieves a previously stored ranked pool. Returns
// (nil, nil) — not an error — if the session doesn't exist or has
// expired, so callers can treat that as "start fresh" rather than fail.
func GetSession(ctx context.Context, rdb *redis.Client, sessionID string) ([]store.RankedItem, error) {
	key := "rec_session:" + sessionID
	data, err := rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetching session from redis: %w", err)
	}

	var items []store.RankedItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("unmarshaling session items: %w", err)
	}
	return items, nil
}