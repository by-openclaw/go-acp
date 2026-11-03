package rollcall

import (
	"testing"

	"dhs/internal/consumer"
)

// ADR-0037 P5: this connector names the target of a write twice, with the
// reason every writing connector gives.
func TestWriteGuardReason(t *testing.T) {
	var g consumer.WriteGuard = &Plugin{}
	if g.WriteGuardReason() != consumer.WriteGuardEveryConnector {
		t.Fatalf("reason: %q", g.WriteGuardReason())
	}
}
