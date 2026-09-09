package emberplus

import (
	"errors"
	"strings"
	"testing"

	"dhs/internal/emberplus/codec/s101"
)

type failingSink struct{}

func (failingSink) Write([]byte) (int, error) { return 0, errors.New("socket gone") }

// sendEmBER returns the writer's failure as is — the caller decides whether
// to reconnect — and counts nothing on the failed emission. Deterministic
// here because a real socket only fails this way after a peer reset races
// the write, which Linux CI did not always produce.
func TestSendEmBERReturnsWriteError(t *testing.T) {
	s := &Session{logger: discardLogger(), writer: s101.NewWriter(failingSink{})}
	if err := s.sendEmBER([]byte{0x60, 0x00}); err == nil || !strings.Contains(err.Error(), "socket gone") {
		t.Fatalf("sendEmBER over a failing writer = %v, want the sink's error surfaced", err)
	}
	if snap := s.metricsConn(); snap != nil && snap.Snapshot().TxFrames != 0 {
		t.Error("a failed emission must not be counted as tx")
	}
}
