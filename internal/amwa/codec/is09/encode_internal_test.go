package is09

import (
	"bytes"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/spec"
)

// Both encoders funnel through Validate: an invalid resource must
// never reach the wire, from either the indented or the compact path.
func TestEncodeRefusesInvalidGlobal(t *testing.T) {
	g := validGlobal()
	g.Label = ""
	if _, err := g.Encode(); err == nil || !strings.Contains(err.Error(), "label: required") {
		t.Fatalf("Encode error = %v, want the label violation", err)
	}
	if _, err := g.EncodeCompact(); err == nil || !strings.Contains(err.Error(), "label: required") {
		t.Fatalf("EncodeCompact error = %v, want the label violation", err)
	}
}

// EncodeCompact is the wire form: one line, same content as Encode.
func TestEncodeCompactIsSingleLine(t *testing.T) {
	g := validGlobal()
	compact, err := g.EncodeCompact()
	if err != nil {
		t.Fatalf("EncodeCompact: %v", err)
	}
	if bytes.Contains(compact, []byte{'\n'}) {
		t.Fatalf("EncodeCompact emitted newlines: %s", compact)
	}
	back, err := Decode(compact)
	if err != nil || back.ID != g.ID {
		t.Fatalf("Decode(compact) = %+v, %v", back, err)
	}
}

// Decode validates after parsing: well-formed JSON with an
// out-of-range value is rejected with the range named.
func TestDecodeRejectsOutOfRangeButWellFormed(t *testing.T) {
	raw := strings.Replace(specExample, `"heartbeat_interval": 8`, `"heartbeat_interval": 0`, 1)
	if raw == specExample {
		t.Fatal("fixture edit did not apply")
	}
	if _, err := Decode([]byte(raw)); err == nil || !strings.Contains(err.Error(), "heartbeat_interval=0") {
		t.Fatalf("Decode error = %v, want the heartbeat range violation", err)
	}
}

func TestSyslogHostnameForms(t *testing.T) {
	cases := []struct {
		name string
		host string
		ok   bool
	}{
		{"IPv4", "192.0.2.10", true},
		{"IPv6", "2001:db8::1", true},
		{"hostname", "logs.example.com", true},
		{"single label", "syslog", true},
		{"label with hyphen inside", "log-01.lan", true},
		{"empty", "", false},
		{"too long", strings.Repeat("a", 254), false},
		{"empty label", "a..b", false},
		{"label too long", strings.Repeat("a", 64) + ".b", false},
		{"leading hyphen", "-a.b", false},
		{"trailing hyphen", "a-.b", false},
		{"underscore", "a_b", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isValidSyslogHostname(tc.host); got != tc.ok {
				t.Fatalf("isValidSyslogHostname(%q) = %v, want %v", tc.host, got, tc.ok)
			}
		})
	}
}

func TestValidateSyslogBlockEdges(t *testing.T) {
	if err := validateSyslogBlock("syslog", nil); err != nil {
		t.Fatalf("nil block must be accepted: %v", err)
	}
	err := validateSyslogBlock("syslogv2", &SyslogConfig{Hostname: "bad_host"})
	if err == nil || !strings.Contains(err.Error(), `syslogv2.hostname "bad_host"`) {
		t.Fatalf("error = %v, want the hostname violation", err)
	}
	g := validGlobal()
	g.Syslog = &SyslogConfig{Hostname: "not valid!"}
	if err := g.Validate(); err == nil || !strings.Contains(err.Error(), "syslog.hostname") {
		t.Fatalf("Validate error = %v, want the syslog hostname violation", err)
	}
}

// Default on an empty registry is the "forgot the blank import" bug; a
// panic that names the missing import is the only useful answer, and
// it is only reachable by swapping the package registry for an empty
// one (every real build registers v1.0 at init).
func TestDefaultPanicsWhenNothingRegistered(t *testing.T) {
	saved := versions
	versions = spec.NewRegistry[Codec]()
	t.Cleanup(func() { versions = saved })

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Default must panic on an empty registry")
		}
		if msg, _ := r.(string); !strings.Contains(msg, "is09/v10") {
			t.Fatalf("panic = %v, want it to name the missing import", r)
		}
	}()
	Default()
}
