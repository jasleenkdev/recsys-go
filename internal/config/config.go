// internal/config/config.go
package config

import (
	"log"
	"os"
	"strings"
	"sync"

	"github.com/joho/godotenv"
)

// Config holds every externally-configurable endpoint this project talks
// to. Defaults reproduce the local development setup, so an unset
// variable behaves exactly as the previously hardcoded value did.
type Config struct {
	// DatabaseURL is the Postgres DSN used by every binary.
	DatabaseURL string

	// QdrantURL is the base URL of the vector database (no trailing
	// slash). Locally this runs on port 6343 rather than the default
	// 6333 due to a port conflict with an unrelated Docker stack.
	QdrantURL string

	// RedisAddr is the host:port of the Redis used for recommendation
	// session snapshots.
	RedisAddr string

	// KafkaBrokers is the broker list for the events topic.
	KafkaBrokers []string

	// EmbedSidecarURL is the full URL of the embedding sidecar's /embed
	// endpoint.
	EmbedSidecarURL string

	// OllamaURL is the full URL of Ollama's /api/generate endpoint, used
	// to write grounded search answers.
	OllamaURL string

	// APIPort is the port the HTTP API listens on, without a colon.
	APIPort string
}

var (
	once   sync.Once
	loaded *Config
)

// Load returns the process configuration, reading a root .env file on
// first use. It is memoized: main() and library packages alike get the
// same values from the same call, so configuration cannot drift between
// a binary and the packages it links. A missing .env is not an error —
// the app must still run from real environment variables alone.
func Load() *Config {
	once.Do(func() {
		if err := godotenv.Load(); err != nil {
			log.Printf("config: no .env file loaded (%v); using environment variables only", err)
		}

		loaded = &Config{
			DatabaseURL:     env("DATABASE_URL", "postgres://jasleenkaur@localhost:5432/recsys?sslmode=disable"),
			QdrantURL:       strings.TrimSuffix(env("QDRANT_URL", "http://localhost:6343"), "/"),
			RedisAddr:       env("REDIS_ADDR", "localhost:6390"),
			KafkaBrokers:    splitBrokers(env("KAFKA_BROKERS", "localhost:9092")),
			EmbedSidecarURL: env("EMBED_SIDECAR_URL", "http://localhost:8000/embed"),
			OllamaURL:       env("OLLAMA_URL", "http://localhost:11434/api/generate"),
			APIPort:         strings.TrimPrefix(env("API_PORT", "8081"), ":"),
		}
	})
	return loaded
}

// ListenAddr is the address to hand to http.ListenAndServe.
func (c *Config) ListenAddr() string {
	return ":" + c.APIPort
}

// env reads key, falling back to def when unset or empty. Empty is
// treated as unset so an exported-but-blank variable can't silently
// produce an unusable endpoint.
func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// splitBrokers parses a comma-separated broker list, dropping blanks so
// a trailing comma doesn't yield an empty broker address.
func splitBrokers(raw string) []string {
	parts := strings.Split(raw, ",")
	brokers := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			brokers = append(brokers, p)
		}
	}
	return brokers
}
