package consumer

import (
	"testing"

	dhsc "dhs/internal/consumer"
)

// ADR-0037 P5: this connector names the target of a write twice, with the
// reason every writing connector gives.
func TestWriteGuardReason(t *testing.T) {
	var g dhsc.WriteGuard = &Plugin{}
	if g.WriteGuardReason() != dhsc.WriteGuardEveryConnector {
		t.Fatalf("reason: %q", g.WriteGuardReason())
	}
}
