// internal/config/config_test.go
package config

import (
	"strings"
	"testing"
)

func TestValidateAuth(t *testing.T) {
	const secret = "s3cret-value"

	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"nothing set (local dev)", Config{}, false},
		{"kafka PLAIN with credentials", Config{KafkaSASLMechanism: KafkaSASLPlain, KafkaSASLUsername: "u", KafkaSASLPassword: secret}, false},
		{"kafka SCRAM-SHA-256 with credentials", Config{KafkaSASLMechanism: KafkaSASLScramSHA256, KafkaSASLUsername: "u", KafkaSASLPassword: secret}, false},
		{"kafka SCRAM-SHA-512 with credentials", Config{KafkaSASLMechanism: KafkaSASLScramSHA512, KafkaSASLUsername: "u", KafkaSASLPassword: secret}, false},
		{"kafka TLS only", Config{KafkaTLS: true}, false},
		{"kafka mechanism without password", Config{KafkaSASLMechanism: KafkaSASLScramSHA256, KafkaSASLUsername: "u"}, true},
		{"kafka mechanism without username", Config{KafkaSASLMechanism: KafkaSASLPlain, KafkaSASLPassword: secret}, true},
		{"kafka credentials without mechanism", Config{KafkaSASLUsername: "u", KafkaSASLPassword: secret}, true},
		{"kafka unsupported mechanism", Config{KafkaSASLMechanism: "GSSAPI", KafkaSASLUsername: "u", KafkaSASLPassword: secret}, true},
		{"redis password only", Config{RedisPassword: secret}, false},
		{"redis username and password", Config{RedisUsername: "u", RedisPassword: secret}, false},
		{"redis username without password", Config{RedisUsername: "u"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.validateAuth()
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateAuth() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), secret) {
				t.Errorf("error message leaks a secret value: %v", err)
			}
		})
	}
}
