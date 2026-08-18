// internal/store/user_embeddings.go
package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jasleenkdev/recsys-go/internal/domain"
)

// eventWeights encodes how strongly each event type signals genuine
// user interest, not just casual exposure. Starring is a deliberate
// endorsement; viewing is comparatively weak, ambient signal.
var eventWeights = map[domain.EventType]float64{
	domain.EventStarred:       3.0,
	domain.EventClickedReadme: 2.0,
	domain.EventViewed:        1.0,
}

// RecomputeUserEmbedding rebuilds a user's behavioral embedding as a
// weighted average of the embeddings of every item they've interacted
// with, weighted by event type. It updates the existing row for
// (userID, modelID) in place — this is a data refresh, not a new
// model version, so it deliberately does not create a new row.
func RecomputeUserEmbedding(ctx context.Context, db *sql.DB, userID, modelID int64) error {
	rows, err := db.QueryContext(ctx, `
		SELECT e.event_type, ie.embedding::text
		FROM events e
		JOIN item_embeddings ie ON ie.item_id = e.item_id AND ie.model_id = $2
		WHERE e.user_id = $1
	`, userID, modelID)
	if err != nil {
		return fmt.Errorf("querying user events: %w", err)
	}
	defer rows.Close()

	var (
		weightedSum []float64
		totalWeight float64
	)

	for rows.Next() {
		var eventType domain.EventType
		var embeddingText string
		if err := rows.Scan(&eventType, &embeddingText); err != nil {
			return fmt.Errorf("scanning event row: %w", err)
		}

		weight, ok := eventWeights[eventType]
		if !ok {
			continue // unknown event type, skip rather than fail the whole recompute
		}

		vec, err := ParsePgvectorText(embeddingText)
		if err != nil {
			return fmt.Errorf("parsing embedding: %w", err)
		}

		if weightedSum == nil {
			weightedSum = make([]float64, len(vec))
		}
		for i, v := range vec {
			weightedSum[i] += v * weight
		}
		totalWeight += weight
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating event rows: %w", err)
	}

	if totalWeight == 0 {
		// User has no events with a recognized weight yet — nothing
		// to compute. Not an error; just leave any existing embedding
		// as-is rather than overwriting it with a meaningless zero vector.
		return nil
	}

	for i := range weightedSum {
		weightedSum[i] /= totalWeight
	}

	var newEmbeddingID int64
	err = db.QueryRowContext(ctx, `
		INSERT INTO user_embeddings (user_id, model_id, embedding)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, model_id) DO UPDATE SET embedding = EXCLUDED.embedding, created_at = now()
		RETURNING id
	`, userID, modelID, VectorLiteral(weightedSum)).Scan(&newEmbeddingID)
	if err != nil {
		return fmt.Errorf("upserting user embedding: %w", err)
	}

	// Point users.active_embedding_id at this row if it isn't already —
	// only matters the first time a user gets an embedding, since
	// update-in-place means the row id never changes after that.
	_, err = db.ExecContext(ctx, `
		UPDATE users
		SET active_embedding_id = $1
		WHERE id = $2 AND (active_embedding_id IS NULL OR active_embedding_id != $1)
	`, newEmbeddingID, userID)
	if err != nil {
		return fmt.Errorf("updating active_embedding_id: %w", err)
	}

	return nil
}