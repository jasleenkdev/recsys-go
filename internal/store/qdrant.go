// internal/store/qdrant.go
package store

import (
	"net/http"

	"github.com/jasleenkdev/recsys-go/internal/config"
)

// SetQdrantAuth adds the api-key header Qdrant Cloud requires when
// QDRANT_API_KEY is set, and nothing otherwise, since local Qdrant runs
// without auth. Every Qdrant request must pass through it: a call site
// that skips it still works locally and fails with 401 only once deployed.
func SetQdrantAuth(req *http.Request) {
	if key := config.Load().QdrantAPIKey; key != "" {
		req.Header.Set("api-key", key)
	}
}
