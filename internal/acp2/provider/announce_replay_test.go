package acp2

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dhs/internal/acp2/codec"
)

// writeReplayFile drops a .jsonl announce recording into a temp dir.
func writeReplayFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "announces.jsonl")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadAnnounceReplay(t *testing.T) {
	// Valid: two records (an ACP2 announce header + 8-byte body).
	good := `{"dt_ms":0,"slot":1,"hex":"02000008000044230000000008060008deadbeef"}
{"dt_ms":250,"slot":0,"hex":"02000008000044240000000008060008cafef00d"}
`
	items, err := LoadAnnounceReplay(writeReplayFile(t, good))
	if err != nil {
		t.Fatalf("valid file: %v", err)
	}
	if len(items) != 2 || items[0].Slot != 1 || items[1].DtMs != 250 {
		t.Fatalf("items = %+v", items)
	}
	if len(items[0].payload) != 20 {
		t.Fatalf("payload len = %d, want 20", len(items[0].payload))
	}

	cases := []struct{ name, content string }{
		{"bad-json", "{not json\n"},
		{"bad-hex", `{"dt_ms":0,"slot":1,"hex":"zz"}` + "\n"},
		{"short-payload", `{"dt_ms":0,"slot":1,"hex":"0200"}` + "\n"},
		{"empty", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := LoadAnnounceReplay(writeReplayFile(t, c.content)); err == nil {
				t.Fatal("want error")
			}
		})
	}
	if _, err := LoadAnnounceReplay(filepath.Join(t.TempDir(), "missing.jsonl")); err == nil {
		t.Fatal("missing file: want error")
	}

	// A line beyond the 1 MB scanner cap → bufio.ErrTooLong via sc.Err().
	huge := `{"dt_ms":0,"slot":1,"hex":"` + string(make([]byte, 2<<20)) + `"}`
	if _, err := LoadAnnounceReplay(writeReplayFile(t, huge)); err == nil {
		t.Fatal("oversized line: want error")
	}
}

// TestRunAnnounceReplay_LoopsAndFansOut plays a zero-delay recording to
// a session with ACP2 events enabled and asserts frames keep arriving
// across passes (the loop arm), then stops on ctx cancel.
func TestRunAnnounceReplay_LoopsAndFansOut(t *testing.T) {
	sess, peer := newTestSession(t)
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()
	sess.enable(codec.AN2ProtoACP2)
	sess.srv.mu.Lock()
	sess.srv.sessions[sess] = struct{}{}
	sess.srv.mu.Unlock()

	items := []AnnounceItem{{
		DtMs: 0, Slot: 1,
		payload: []byte{0x02, 0x00, 0x00, 0x08, 0, 0, 0x44, 0x23, 0, 0, 0, 0},
	}}

	got := make(chan struct{}, 64)
	go func() {
		for {
			f, err := codec.ReadAN2Frame(peer)
			if err != nil {
				return
			}
			if f.Proto == codec.AN2ProtoACP2 && len(f.Payload) > 0 && f.Payload[0] == 0x02 {
				// Non-blocking post: the reader must NEVER stall. The
				// zero-delay replay pumps frames continuously; if this
				// channel fills while the main goroutine is between its
				// 3rd receive and cancel() (a scheduler pause on a loaded
				// runner), a blocking send wedges the whole machine —
				// reader stops draining the pipe, sess.write blocks
				// mid-frame inside broadcastAnnounce, and replayPass
				// never re-checks ctx. That was the rocky9 main red
				// (run 32427993735; ADR-0029 determinism rule).
				select {
				case got <- struct{}{}:
				default:
				}
			}
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { sess.srv.RunAnnounceReplay(ctx, items); close(done) }()

	// The single-item recording must arrive more than once — proof the
	// stream loops.
	for i := 0; i < 3; i++ {
		select {
		case <-got:
		case <-time.After(5 * time.Second):
			t.Fatalf("announce %d never arrived", i+1)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("replay did not stop on ctx cancel")
	}
}

// TestReplayPass_CtxCancelDuringDelay covers the ctx arm of the
// per-item wait deterministically: the ctx is cancelled before the
// call and the item's delay is an hour, so ctx.Done is the only ready
// arm.
func TestReplayPass_CtxCancelDuringDelay(t *testing.T) {
	sess, peer := newTestSession(t)
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	items := []AnnounceItem{{DtMs: 3_600_000, Slot: 1,
		payload: []byte{0x02, 0x00, 0x00, 0x08, 0, 0, 0, 1, 0, 0, 0, 0}}}
	if done := sess.srv.replayPass(ctx, items); !done {
		t.Fatal("replayPass must report done on cancelled ctx")
	}
}
