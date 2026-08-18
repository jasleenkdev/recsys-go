// cmd/api/main.go
package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jasleenkdev/recsys-go/internal/store"
)

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

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/recommendations/{user_id}", recommendationsHandler(db))

	log.Println("api server listening on :8081")
	if err := http.ListenAndServe(":8081", mux); err != nil {
		log.Fatal(err)
	}
}

func recommendationsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userIDStr := r.PathValue("user_id")
		userID, err := strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_user_id", "user_id must be a valid integer")
			return
		}

		result, err := store.GetCandidates(r.Context(), db, userID)
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

		items := make([]recommendationItem, len(result.Items))
		for i, r := range result.Items {
			items[i] = recommendationItem{
				ItemID: strconv.FormatInt(r.ItemID, 10),
				Score:  r.Score,
			}
		}

		writeJSON(w, http.StatusOK, recommendationsResponse{
			Items:          items,
			Cursor:         nil, // no pagination yet — Redis snapshot cursor comes next
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