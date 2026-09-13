// internal/events/conn_test.go
package events

import (
	"testing"

	"github.com/segmentio/kafka-go"

	"github.com/jasleenkdev/recsys-go/internal/config"
)

// TestNoKafkaAuthKeepsLibraryDefaults pins backward compatibility: with no
// security configured, neither builder may hand kafka-go anything.
func TestNoKafkaAuthKeepsLibraryDefaults(t *testing.T) {
	cfg := &config.Config{}

	rt, err := Transport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Compared as an interface: a typed nil would stop kafka.Writer from
	// falling back to its default transport.
	if rt != nil {
		t.Errorf("Transport() = %#v, want a nil interface", rt)
	}

	d, err := Dialer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if d != nil {
		t.Errorf("Dialer() = %#v, want nil", d)
	}
}

func TestKafkaTLSWithoutSASL(t *testing.T) {
	cfg := &config.Config{KafkaTLS: true}

	d, err := Dialer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if d == nil || d.TLS == nil || d.SASLMechanism != nil {
		t.Fatalf("Dialer() = %#v, want TLS and no SASL", d)
	}

	rt, err := Transport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := rt.(*kafka.Transport)
	if !ok || tr.TLS == nil || tr.SASL != nil {
		t.Fatalf("Transport() = %#v, want TLS and no SASL", rt)
	}
}

func TestKafkaSASLMechanisms(t *testing.T) {
	for _, mech := range []string{config.KafkaSASLPlain, config.KafkaSASLScramSHA256, config.KafkaSASLScramSHA512} {
		t.Run(mech, func(t *testing.T) {
			cfg := &config.Config{
				KafkaTLS:           true,
				KafkaSASLMechanism: mech,
				KafkaSASLUsername:  "user",
				KafkaSASLPassword:  "pass",
			}

			d, err := Dialer(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if d == nil || d.TLS == nil || d.SASLMechanism == nil {
				t.Fatalf("Dialer() = %#v, want TLS and SASL", d)
			}
			if got := d.SASLMechanism.Name(); got != mech {
				t.Errorf("Dialer SASL mechanism = %q, want %q", got, mech)
			}

			rt, err := Transport(cfg)
			if err != nil {
				t.Fatal(err)
			}
			tr, ok := rt.(*kafka.Transport)
			if !ok || tr.TLS == nil || tr.SASL == nil {
				t.Fatalf("Transport() = %#v, want TLS and SASL", rt)
			}
			if got := tr.SASL.Name(); got != mech {
				t.Errorf("Transport SASL mechanism = %q, want %q", got, mech)
			}
		})
	}
}

func TestKafkaUnsupportedMechanism(t *testing.T) {
	cfg := &config.Config{KafkaSASLMechanism: "GSSAPI", KafkaSASLUsername: "u", KafkaSASLPassword: "p"}
	if _, err := Dialer(cfg); err == nil {
		t.Error("Dialer() error = nil, want unsupported mechanism error")
	}
	if _, err := Transport(cfg); err == nil {
		t.Error("Transport() error = nil, want unsupported mechanism error")
	}
}
