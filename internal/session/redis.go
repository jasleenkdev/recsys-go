// internal/session/redis.go
package session

import (
	"crypto/tls"

	"github.com/redis/go-redis/v9"

	"github.com/jasleenkdev/recsys-go/internal/config"
)

// NewRedisClient builds the Redis client that backs recommendation
// sessions.
//
// Username, password and TLS are all optional. Unset, the options match
// the old Addr-only client exactly — no AUTH, plaintext — which is what
// local Docker Redis expects. Hosted Redis (Upstash, Redis Cloud, Render
// Key Value's external URL) needs a password and usually TLS.
func NewRedisClient(cfg *config.Config) *redis.Client {
	opts := &redis.Options{
		Addr:     cfg.RedisAddr,
		Username: cfg.RedisUsername,
		Password: cfg.RedisPassword,
	}
	if cfg.RedisTLS {
		// ServerName stays empty: go-redis dials through
		// tls.DialWithDialer, which infers it from Addr for certificate
		// verification.
		opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return redis.NewClient(opts)
}
