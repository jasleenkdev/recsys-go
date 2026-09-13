// internal/config/config.go
package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/joho/godotenv"
)

// Kafka SASL mechanisms accepted in KAFKA_SASL_MECHANISM, spelled the way
// Kafka's own sasl.mechanism setting spells them. These are exactly the
// ones kafka-go implements (sasl/plain and sasl/scram).
const (
	KafkaSASLPlain       = "PLAIN"
	KafkaSASLScramSHA256 = "SCRAM-SHA-256"
	KafkaSASLScramSHA512 = "SCRAM-SHA-512"
)

// Config holds every externally-configurable endpoint this project talks
// to. Defaults reproduce the local development setup, so an unset
// variable behaves exactly as the previously hardcoded value did.
//
// The auth fields are all optional and default to off: local Docker
// services run without credentials or TLS, hosted providers need them.
// Secret values are held here but must never be logged.
type Config struct {
	// DatabaseURL is the Postgres DSN used by every binary.
	DatabaseURL string

	// QdrantURL is the base URL of the vector database (no trailing
	// slash). Locally this runs on port 6343 rather than the default
	// 6333 due to a port conflict with an unrelated Docker stack.
	QdrantURL string

	// QdrantAPIKey, when set, is sent as the api-key header on every
	// Qdrant request (Qdrant Cloud). TLS needs no setting of its own: it
	// follows from an https:// QdrantURL.
	QdrantAPIKey string

	// RedisAddr is the host:port of the Redis used for recommendation
	// session snapshots.
	RedisAddr string

	// RedisUsername and RedisPassword authenticate to Redis when set.
	// Username is only for Redis 6+ ACL users; a password alone
	// authenticates as the default user.
	RedisUsername string
	RedisPassword string

	// RedisTLS enables TLS to Redis (REDIS_TLS=true).
	RedisTLS bool

	// KafkaBrokers is the broker list for the events topic.
	KafkaBrokers []string

	// KafkaTLS enables TLS to the brokers (KAFKA_TLS=true).
	KafkaTLS bool

	// KafkaSASLMechanism is one of the KafkaSASL* constants, or empty for
	// no SASL. Username and password are required with it.
	KafkaSASLMechanism string
	KafkaSASLUsername  string
	KafkaSASLPassword  string

	// EmbedSidecarURL is the full URL of the embedding sidecar's /embed
	// endpoint.
	EmbedSidecarURL string

	// OllamaURL is the full URL of Ollama's /api/generate endpoint, used
	// to write grounded search answers.
	OllamaURL string

	// APIPort is the port the HTTP API listens on, without a colon. It
	// comes from PORT when set — the variable Render injects and routes
	// traffic to — then API_PORT, then 8081. PORT wins so a leftover
	// API_PORT can't bind a hosted deploy to a port nothing routes to;
	// the fallbacks keep local dev on 8081, where the frontend's
	// RECSYS_API_URL default expects the API.
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
//
// Auth settings that are present but unusable are fatal here, at
// startup, rather than surfacing later as an opaque connection failure.
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
			APIPort:         strings.TrimPrefix(env("PORT", env("API_PORT", "8081")), ":"),

			// Optional auth. Every default is "off", which is what the
			// unauthenticated local Docker services expect.
			QdrantAPIKey:       env("QDRANT_API_KEY", ""),
			RedisUsername:      env("REDIS_USERNAME", ""),
			RedisPassword:      env("REDIS_PASSWORD", ""),
			RedisTLS:           envBool("REDIS_TLS"),
			KafkaTLS:           envBool("KAFKA_TLS"),
			KafkaSASLMechanism: strings.ToUpper(env("KAFKA_SASL_MECHANISM", "")),
			KafkaSASLUsername:  env("KAFKA_SASL_USERNAME", ""),
			KafkaSASLPassword:  env("KAFKA_SASL_PASSWORD", ""),
		}

		if err := loaded.validateAuth(); err != nil {
			log.Fatalf("config: %v", err)
		}
		if loaded.KafkaSASLMechanism == KafkaSASLPlain && !loaded.KafkaTLS {
			log.Printf("config: KAFKA_SASL_MECHANISM=PLAIN without KAFKA_TLS=true sends the Kafka password in cleartext")
		}
	})
	return loaded
}

// validateAuth rejects auth settings that are present but unusable. Each
// case would otherwise fall back to an unauthenticated connection, which
// a hosted provider rejects with a far less obvious error than this.
// Messages name variables, never their values.
func (c *Config) validateAuth() error {
	switch c.KafkaSASLMechanism {
	case "":
		if c.KafkaSASLUsername != "" || c.KafkaSASLPassword != "" {
			return fmt.Errorf("KAFKA_SASL_USERNAME/KAFKA_SASL_PASSWORD are set but KAFKA_SASL_MECHANISM is not")
		}
	case KafkaSASLPlain, KafkaSASLScramSHA256, KafkaSASLScramSHA512:
		if c.KafkaSASLUsername == "" || c.KafkaSASLPassword == "" {
			return fmt.Errorf("KAFKA_SASL_MECHANISM=%s requires both KAFKA_SASL_USERNAME and KAFKA_SASL_PASSWORD", c.KafkaSASLMechanism)
		}
	default:
		return fmt.Errorf("KAFKA_SASL_MECHANISM=%q is not supported (use %s, %s or %s)",
			c.KafkaSASLMechanism, KafkaSASLPlain, KafkaSASLScramSHA256, KafkaSASLScramSHA512)
	}

	if c.RedisUsername != "" && c.RedisPassword == "" {
		return fmt.Errorf("REDIS_USERNAME is set but REDIS_PASSWORD is not")
	}
	return nil
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

// envBool reads a boolean flag: false when unset or empty. An unparseable
// value is fatal rather than false, because a typo like KAFKA_TLS=ture
// would otherwise quietly dial a TLS-only endpoint in plaintext.
func envBool(key string) bool {
	raw := env(key, "")
	if raw == "" {
		return false
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		log.Fatalf("config: %s=%q is not a boolean (use true or false)", key, raw)
	}
	return v
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
