// cmd/api/main.go
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"

	"github.com/jasleenkdev/recsys-go/internal/domain"
	"github.com/jasleenkdev/recsys-go/internal/events"
	"github.com/jasleenkdev/recsys-go/internal/session"
	"github.com/jasleenkdev/recsys-go/internal/store"
)

const pageSize = 20

// browsePageDefault is the browse endpoint's page size when the client
// doesn't ask for one. Browse pages off a stored index rather than a
// frozen snapshot, so unlike pageSize it is safe to let clients vary.
const browsePageDefault = 20

const (
	kafkaBroker = "localhost:9092"
	kafkaTopic  = "repo-events"
)

type recommendationItem struct {
	ItemID string  `json:"item_id"`
	Score  float64 `json:"score"`
}

type searchRequest struct {
	Query string `json:"query"`
}

type citationResponse struct {
	ItemID         string  `json:"item_id"`
	ChunkText      string  `json:"chunk_text"`
	SectionHeading string  `json:"section_heading"`
	ChunkIndex     int     `json:"chunk_index"`
	RelevanceScore float64 `json:"relevance_score"`
}

type searchResponse struct {
	Status    string             `json:"status"`
	Answer    *string            `json:"answer"`
	Citations []citationResponse `json:"citations"`
}

type authSyncRequest struct {
	ExternalID string `json:"external_id"`
}

type authSyncResponse struct {
	UserID int64 `json:"user_id"`
}

func authSyncHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req authSyncRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ExternalID == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "external_id is required")
			return
		}

		var userID int64
		err := db.QueryRowContext(r.Context(), `
			INSERT INTO users (external_id)
			VALUES ($1)
			ON CONFLICT (external_id) DO UPDATE SET external_id = EXCLUDED.external_id
			RETURNING id
		`, req.ExternalID).Scan(&userID)
		if err != nil {
			log.Printf("auth sync failed: %v", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to sync user")
			return
		}

		writeJSON(w, http.StatusOK, authSyncResponse{UserID: userID})
	}
}

func searchHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req searchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "malformed JSON body")
			return
		}
		if req.Query == "" {
			writeError(w, http.StatusBadRequest, "missing_query", "query field is required")
			return
		}

		result, err := store.SearchReadmes(r.Context(), db, req.Query)
		if err != nil {
			log.Printf("SearchReadmes failed: %v", err)
			// Even on error, respond with retrieval_error status —
			// per contract, this is still a 200: the server did its
			// job and is reporting the outcome, not a transport failure.
			writeJSON(w, http.StatusOK, searchResponse{
				Status:    string(store.StatusRetrievalError),
				Answer:    nil,
				Citations: []citationResponse{},
			})
			return
		}

		citations := make([]citationResponse, len(result.Citations))
		for i, c := range result.Citations {
			citations[i] = citationResponse{
				ItemID:         strconv.FormatInt(c.ItemID, 10),
				ChunkText:      c.ChunkText,
				SectionHeading: c.SectionHeading,
				ChunkIndex:     c.ChunkIndex,
				RelevanceScore: c.RelevanceScore,
			}
		}

		var answer *string
		if result.Status == store.StatusGrounded {
			answer = &result.Answer
		}

		writeJSON(w, http.StatusOK, searchResponse{
			Status:    string(result.Status),
			Answer:    answer,
			Citations: citations,
		})
	}
}

type recommendationsResponse struct {
	Items          []recommendationItem `json:"items"`
	Cursor         *string              `json:"cursor"`
	ModelVersion   string               `json:"model_version"`
	Fallback       bool                 `json:"fallback"`
	FallbackReason *string              `json:"fallback_reason"`
}

// --- Phase B: events ---

type eventRequest struct {
	// EventID is optional. Supplying one makes the write idempotent:
	// the events table dedupes on (event_id, created_at), so a client
	// retrying an ambiguous request reuses its id rather than logging
	// the same engagement twice.
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	UserID    int64  `json:"user_id"`
	ItemID    int64  `json:"item_id"`

	// OccurredAt is optional and defaults to now(). It is the other
	// half of the dedupe key, so a client retrying an ambiguous request
	// must echo back both this and event_id from the original response
	// to be deduped rather than double-counted.
	OccurredAt *time.Time `json:"occurred_at"`
}

type eventResponse struct {
	EventID    string    `json:"event_id"`
	OccurredAt time.Time `json:"occurred_at"`
	Status     string    `json:"status"`
}

func eventsHandler(db *sql.DB, producer *events.Producer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		var req eventRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "malformed JSON body")
			return
		}

		if req.EventID == "" {
			req.EventID = uuid.NewString()
		} else if _, err := uuid.Parse(req.EventID); err != nil {
			// The column is UUID-typed, so a non-UUID id would fail in
			// the consumer as an undeliverable message rather than here.
			writeError(w, http.StatusBadRequest, "invalid_event_id", "event_id must be a UUID")
			return
		}

		occurredAt := time.Now().UTC()
		if req.OccurredAt != nil {
			occurredAt = req.OccurredAt.UTC()
		}

		// The public API says item_id everywhere; the Kafka schema the
		// consumer already reads says repo_id. Translate here rather
		// than changing either side.
		event := domain.RepoEvent{
			EventID:    req.EventID,
			EventType:  domain.EventType(req.EventType),
			UserID:     req.UserID,
			RepoID:     req.ItemID,
			OccurredAt: occurredAt,
		}
		if err := event.Validate(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_event", err.Error())
			return
		}

		// Both ids are foreign keys. Without this check a bad id is a
		// permanent insert error in the consumer, which drops the
		// message and commits — the client would see 202 for an event
		// that is never stored. One indexed existence probe buys a
		// truthful 404 instead.
		userExists, itemExists, err := checkRefs(ctx, db, event.UserID, event.RepoID)
		if err != nil {
			log.Printf("reference check failed: %v", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to record event")
			return
		}
		if !userExists {
			writeError(w, http.StatusNotFound, "user_not_found", "user_id does not exist")
			return
		}
		if !itemExists {
			writeError(w, http.StatusNotFound, "item_not_found", "item_id does not exist")
			return
		}

		if err := producer.Publish(ctx, event); err != nil {
			log.Printf("failed to publish event: %v", err)
			writeError(w, http.StatusServiceUnavailable, "event_queue_unavailable", "failed to record event, retry with the same event_id")
			return
		}

		// 202, not 201: the event is durably queued, but the Postgres
		// row and the user's embedding recompute happen downstream in
		// the consumer. Claiming 201 Created would be a lie about a
		// resource the client cannot yet read back.
		writeJSON(w, http.StatusAccepted, eventResponse{
			EventID:    event.EventID,
			OccurredAt: event.OccurredAt,
			Status:     "accepted",
		})
	}
}

func checkRefs(ctx context.Context, db *sql.DB, userID, itemID int64) (bool, bool, error) {
	var userExists, itemExists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM users WHERE id = $1),
		       EXISTS(SELECT 1 FROM items WHERE id = $2)
	`, userID, itemID).Scan(&userExists, &itemExists)
	return userExists, itemExists, err
}

// --- Phase B: item detail and browse ---

type itemResponse struct {
	ItemID      string   `json:"item_id"`
	Owner       string   `json:"owner"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Language    string   `json:"language"`
	Topics      []string `json:"topics"`
	Stars       int      `json:"stars"`
	GitHubURL   string   `json:"github_url"`
	CreatedAt   string   `json:"created_at"`
}

type readmeSectionResponse struct {
	ChunkIndex     int    `json:"chunk_index"`
	SectionHeading string `json:"section_heading"`
	ChunkText      string `json:"chunk_text"`
}

type itemDetailResponse struct {
	itemResponse
	Readme []readmeSectionResponse `json:"readme"`
}

type browseResponse struct {
	Items  []itemResponse `json:"items"`
	Cursor *string        `json:"cursor"`
}

type languagesResponse struct {
	Languages []string `json:"languages"`
}

func toItemResponse(it store.Item) itemResponse {
	return itemResponse{
		ItemID:      strconv.FormatInt(it.ItemID, 10),
		Owner:       it.Owner,
		Name:        it.Name,
		Description: it.Description,
		Language:    it.Language,
		Topics:      it.Topics,
		Stars:       it.Stars,
		GitHubURL:   "https://github.com/" + it.Owner + "/" + it.Name,
		CreatedAt:   it.CreatedAt,
	}
}

func itemDetailHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		itemID, err := strconv.ParseInt(r.PathValue("item_id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_item_id", "item_id must be a valid integer")
			return
		}

		detail, err := store.GetItem(r.Context(), db, itemID)
		if errors.Is(err, store.ErrItemNotFound) {
			writeError(w, http.StatusNotFound, "item_not_found", "item_id does not exist")
			return
		}
		if err != nil {
			log.Printf("GetItem failed for item %d: %v", itemID, err)
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to fetch item")
			return
		}

		readme := make([]readmeSectionResponse, len(detail.Readme))
		for i, s := range detail.Readme {
			readme[i] = readmeSectionResponse{
				ChunkIndex:     s.ChunkIndex,
				SectionHeading: s.SectionHeading,
				ChunkText:      s.ChunkText,
			}
		}

		writeJSON(w, http.StatusOK, itemDetailResponse{
			itemResponse: toItemResponse(detail.Item),
			Readme:       readme,
		})
	}
}

func browseHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()

		limit := browsePageDefault
		if raw := query.Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > 100 {
				writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be an integer between 1 and 100")
				return
			}
			limit = parsed
		}

		language := query.Get("language")

		var after *store.BrowseKey
		if raw := query.Get("cursor"); raw != "" {
			key, err := store.DecodeBrowseCursor(raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_cursor", "cursor is malformed")
				return
			}
			// Changing the filter mid-scan would seek into a different
			// result set. Reject rather than silently mixing pages.
			if language != "" && language != key.Language {
				writeError(w, http.StatusBadRequest, "filter_changed", "language does not match the cursor; start a new listing without a cursor")
				return
			}
			language = key.Language
			after = &key
		}

		page, err := store.ListItems(r.Context(), db, language, after, limit)
		if err != nil {
			log.Printf("ListItems failed: %v", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to list items")
			return
		}

		items := make([]itemResponse, len(page.Items))
		for i, it := range page.Items {
			items[i] = toItemResponse(it)
		}

		var cursor *string
		if page.NextCursor != "" {
			c := page.NextCursor
			cursor = &c
		}

		writeJSON(w, http.StatusOK, browseResponse{Items: items, Cursor: cursor})
	}
}

func languagesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		langs, err := store.Languages(r.Context(), db)
		if err != nil {
			log.Printf("Languages failed: %v", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to list languages")
			return
		}
		writeJSON(w, http.StatusOK, languagesResponse{Languages: langs})
	}
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

	producer := events.NewProducer([]string{kafkaBroker}, kafkaTopic)
	defer producer.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/recommendations/{user_id}", recommendationsHandler(db, rdb))
	mux.HandleFunc("POST /v1/recommendations/search", searchHandler(db))
	mux.HandleFunc("POST /v1/auth/sync", authSyncHandler(db))
	mux.HandleFunc("POST /v1/events", eventsHandler(db, producer))
	mux.HandleFunc("GET /v1/items", browseHandler(db))
	mux.HandleFunc("GET /v1/items/{item_id}", itemDetailHandler(db))
	mux.HandleFunc("GET /v1/languages", languagesHandler(db))

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
