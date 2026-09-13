// internal/events/conn.go
package events

import (
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl"
	"github.com/segmentio/kafka-go/sasl/plain"
	"github.com/segmentio/kafka-go/sasl/scram"

	"github.com/jasleenkdev/recsys-go/internal/config"
)

// kafka-go secures its two client types through two different types: a
// kafka.Writer connects through a Transport, a kafka.Reader (and its
// consumer group) through a Dialer. Both are built here from the same
// config so the producer and consumer can't drift apart.
//
// With KAFKA_TLS and KAFKA_SASL_MECHANISM unset, both return nil and
// kafka-go uses its own defaults: plaintext and unauthenticated, which is
// exactly what local Docker Kafka expects.

// Transport returns the RoundTripper for a kafka.Writer, or nil for
// kafka-go's default.
//
// It returns the interface rather than *kafka.Transport on purpose:
// Writer.Transport is a RoundTripper, and a nil *kafka.Transport stored in
// it is a non-nil interface, which kafka-go would use instead of falling
// back to its default transport.
func Transport(cfg *config.Config) (kafka.RoundTripper, error) {
	mechanism, tlsConfig, err := security(cfg)
	if err != nil {
		return nil, err
	}
	if mechanism == nil && tlsConfig == nil {
		return nil, nil
	}
	return &kafka.Transport{
		// kafka.DefaultTransport's dial timeout, which a custom Transport
		// would not otherwise inherit.
		Dial: (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
		TLS:  tlsConfig,
		SASL: mechanism,
	}, nil
}

// Dialer returns the dialer for a kafka.Reader, or nil for kafka-go's
// default. The reader hands it to its consumer group as well, so the
// group coordinator connection is secured the same way as fetches.
func Dialer(cfg *config.Config) (*kafka.Dialer, error) {
	mechanism, tlsConfig, err := security(cfg)
	if err != nil {
		return nil, err
	}
	if mechanism == nil && tlsConfig == nil {
		return nil, nil
	}
	return &kafka.Dialer{
		// kafka.DefaultDialer's settings, which a custom Dialer replaces.
		Timeout:       10 * time.Second,
		DualStack:     true,
		TLS:           tlsConfig,
		SASLMechanism: mechanism,
	}, nil
}

// security maps config onto kafka-go's TLS and SASL types. The TLS
// ServerName is left empty on purpose: the Dialer and the Transport both
// fill it in from each broker's own address, which one fixed name could
// not do for a multi-broker cluster.
func security(cfg *config.Config) (sasl.Mechanism, *tls.Config, error) {
	var tlsConfig *tls.Config
	if cfg.KafkaTLS {
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	user, pass := cfg.KafkaSASLUsername, cfg.KafkaSASLPassword
	switch cfg.KafkaSASLMechanism {
	case "":
		return nil, tlsConfig, nil
	case config.KafkaSASLPlain:
		return plain.Mechanism{Username: user, Password: pass}, tlsConfig, nil
	case config.KafkaSASLScramSHA256:
		m, err := scram.Mechanism(scram.SHA256, user, pass)
		if err != nil {
			return nil, nil, fmt.Errorf("configuring %s: %w", config.KafkaSASLScramSHA256, err)
		}
		return m, tlsConfig, nil
	case config.KafkaSASLScramSHA512:
		m, err := scram.Mechanism(scram.SHA512, user, pass)
		if err != nil {
			return nil, nil, fmt.Errorf("configuring %s: %w", config.KafkaSASLScramSHA512, err)
		}
		return m, tlsConfig, nil
	default:
		return nil, nil, fmt.Errorf("unsupported KAFKA_SASL_MECHANISM %q", cfg.KafkaSASLMechanism)
	}
}
