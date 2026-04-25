package probelsw02p

import (
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"acp/internal/probel-sw02p/codec"
)

// TestSubscribeExtendedProtectTallyDump drives a tx 100 across a
// net.Pipe pair and confirms the decoded params reach the listener.
// Covers both the reset sentinel and a populated dump.
func TestSubscribeExtendedProtectTallyDump(t *testing.T) {
	a, b := net.Pipe()
	defer func() {
		_ = a.Close()
		_ = b.Close()
	}()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	disable := false
	client := codec.NewClientFromConn(a, logger, codec.ClientConfig{WireHexLog: &disable})
	p := &Plugin{logger: logger}
	p.client = client

	got := make(chan codec.ExtendedProtectTallyDumpParams, 2)
	_ = p.SubscribeExtendedProtectTallyDump(func(params codec.ExtendedProtectTallyDumpParams) {
		got <- params
	})

	// Frame 1: reset sentinel.
	if _, err := b.Write(codec.Pack(codec.EncodeExtendedProtectTallyDump(codec.ExtendedProtectTallyDumpParams{Reset: true}))); err != nil {
		t.Fatalf("write reset: %v", err)
	}
	// Frame 2: 1 entry.
	entry := codec.ExtendedProtectTallyDumpEntry{Destination: 42, Device: 17, Protect: codec.ProtectOEM}
	if _, err := b.Write(codec.Pack(codec.EncodeExtendedProtectTallyDump(codec.ExtendedProtectTallyDumpParams{
		Entries: []codec.ExtendedProtectTallyDumpEntry{entry},
	}))); err != nil {
		t.Fatalf("write populated: %v", err)
	}

	deadline := time.After(2 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case params := <-got:
			switch i {
			case 0:
				if !params.Reset {
					t.Errorf("frame 0: Reset = false; want true")
				}
			case 1:
				if params.Reset || len(params.Entries) != 1 || params.Entries[0] != entry {
					t.Errorf("frame 1: %+v; want 1 entry %+v", params, entry)
				}
			}
		case <-deadline:
			t.Fatalf("listener did not fire for frame %d", i)
		}
	}
	_ = client.Close()
}
