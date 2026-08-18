// cmd/api/main.go
package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"

	"github.com/jasleenkdev/recsys-go/internal/session"
	"github.com/jasleenkdev/recsys-go/internal/store"
)

const pageSize = 20

type recommendationItem struct {
	ItemID string  `json:"item_id"`
	Score  float64 `json:"score"`
}

type recommendationsResponse struct {
	Items          []recommendationItem `json:"items"`
	Cursor         *string               `json:"cursor"`
	ModelVersion   string                `json:"model_version"`
	Fallback       bool                  `json:"fallback"`
	FallbackReason *string               `json:"fallback_reason"`
}

func main() {
	db, err := sql.Open("pgx", "postgres://jasleenkaur@localhost:5432/recsys?sslmode=disable")
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6390",
	})
	defer rdb.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/recommendations/{user_id}", recommendationsHandler(db, rdb))

	log.Println("api server listening on :8081")
	if err := http.ListenAndServe(":8081", mux); err != nil {
		log.Fatal(err)
	}
}

func recommendationsHandler(db *sql.DB, rdb *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		userIDStr := r.PathValue("user_id")
		userID, err := strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_user_id", "user_id must be a valid integer")
			return
		}

		cursorParam := r.URL.Query().Get("cursor")

		var pool []store.RankedItem
		var sessionID string
		var offset int

		if cursorParam != "" {
			decoded, err := session.DecodeCursor(cursorParam)
			if err == nil {
				existing, err := session.GetSession(ctx, rdb, decoded.SessionID)
				if err == nil && existing != nil {
					pool = existing
					sessionID = decoded.SessionID
					offset = decoded.Offset
				}
				// invalid, expired, or missing session — fall through
				// to the fresh-session path below, same as no cursor.
			}
		}

		if pool == nil {
			result, err := store.GetCandidates(ctx, db, userID)
			if err != nil {
				log.Printf("GetCandidates failed for user %d: %v", userID, err)
				writeError(w, http.StatusInternalServerError, "internal_error", "failed to generate recommendations")
				return
			}

			if result.FallbackReason != "" {
				reason := result.FallbackReason
				writeJSON(w, http.StatusOK, recommendationsResponse{
					Items:          []recommendationItem{},
					Cursor:         nil,
					ModelVersion:   "none",
					Fallback:       true,
					FallbackReason: &reason,
				})
				return
			}

			newSessionID, err := session.CreateSession(ctx, rdb, result.Items)
			if err != nil {
				log.Printf("failed to create session: %v", err)
				writeError(w, http.StatusInternalServerError, "internal_error", "failed to generate recommendations")
				return
			}

			pool = result.Items
			sessionID = newSessionID
			offset = 0
		}

		end := offset + pageSize
		if end > len(pool) {
			end = len(pool)
		}
		page := pool[offset:end]

		items := make([]recommendationItem, len(page))
		for i, item := range page {
			items[i] = recommendationItem{
				ItemID: strconv.FormatInt(item.ItemID, 10),
				Score:  item.Score,
			}
		}

		var nextCursor *string
		if end < len(pool) {
			c := session.EncodeCursor(session.Cursor{SessionID: sessionID, Offset: end})
			nextCursor = &c
		}

		writeJSON(w, http.StatusOK, recommendationsResponse{
			Items:          items,
			Cursor:         nextCursor,
			ModelVersion:   "mpnet-v1",
			Fallback:       false,
			FallbackReason: nil,
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]interface{}{
		"error": map[string]string{"code": code, "message": message},
	})
}