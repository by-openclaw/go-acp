package cerebrumnb

import (
	"context"
	"strings"
	"testing"

	"dhs/internal/transport"
)

// A bad TLS posture must fail BEFORE the dial, not during it: the operator
// gets "your CA file is missing" instead of a connection error that says
// nothing about the real cause.
//
// This is also what makes the posture injectable — newSession no longer
// decides its own TLS config, so a test can hand it one that cannot be built.
func TestNewSessionRejectsUnbuildableTLSOptions(t *testing.T) {
	// A host that would fail to dial anyway, to prove the error comes from
	// the config and not from the network.
	_, err := newSession(context.Background(), nil, "wss://198.51.100.1:1/",
		transport.TLSOptions{Enable: true, CAFile: "no-such-ca.pem"}, nil)
	if err == nil {
		t.Fatal("newSession succeeded with an unreadable CA file")
	}
	if !strings.Contains(err.Error(), "tls config") {
		t.Errorf("err = %v, want it to name the tls config", err)
	}
	if !strings.Contains(err.Error(), "no-such-ca.pem") {
		t.Errorf("err = %v, want it to name the offending file", err)
	}
}
