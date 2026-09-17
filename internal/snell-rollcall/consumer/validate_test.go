package rollcall

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/transport"
	"dhs/internal/wiretrace"
)

// The committed capture is a real IQ frame's own words: 367 frames taken off
// 10.6.255.113 walking one of its cards. Replaying it here is what makes
// keeping the bytes worth anything — a decoder that changes its mind about
// what hardware actually sent fails on a clean checkout, with no device in the
// room.

const iqCapture = "../testdata/fixtures/iq-frame-IQDBE00/wire.jsonl"

// The controller's own walk: 1489 frames taken off the IQH3UM4-S gateway at
// 10.6.255.113 reading its whole 720-object menu. It is the paged generation —
// the card capture above is one flat menu, this one follows CM_PARTIAL subtrees
// — so replaying it is what keeps the decoder honest about the wire form the
// loopback provider cannot reproduce, on a clean checkout with no device present.
const gatewayCapture = "../testdata/fixtures/iq-frame-IQH3UM4-S/wire.jsonl"

func TestTheCommittedCaptureStillDecodes(t *testing.T) {
	f, err := os.Open(iqCapture)
	if err != nil {
		t.Fatalf("open the capture: %v", err)
	}
	defer func() { _ = f.Close() }()

	trames, err := wiretrace.ReadTrames(f)
	if err != nil {
		t.Fatalf("read the capture: %v", err)
	}
	if len(trames) < 300 {
		t.Fatalf("the capture holds %d frames; it was committed with 367", len(trames))
	}

	p := New(testDeps())
	report, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if len(report.Errors) != 0 {
		for _, e := range report.Errors[:min(3, len(report.Errors))] {
			t.Errorf("frame %d (%s) did not decode: %s — %s",
				e.TrameIndex, e.Direction, e.Err, e.HexPrefix)
		}
		t.Fatalf("%d frames a real device sent no longer decode", len(report.Errors))
	}
	if report.TramesProcessed != len(trames) {
		t.Errorf("%d of %d frames were processed", report.TramesProcessed, len(trames))
	}

	// Both directions, because a capture of one half is a capture of a
	// monologue: what is being checked is a conversation with hardware.
	if report.PerDirection[wiretrace.DirectionTx] == 0 || report.PerDirection[wiretrace.DirectionRx] == 0 {
		t.Errorf("the capture is one-sided: %v", report.PerDirection)
	}

	// The device answered, so a session was opened; a trace with none would
	// mean the walk never got past the handshake.
	for _, inv := range report.Invariants {
		t.Errorf("invariant: %s", inv)
	}
}

func TestTheControllerCaptureStillDecodes(t *testing.T) {
	f, err := os.Open(gatewayCapture)
	if err != nil {
		t.Fatalf("open the capture: %v", err)
	}
	defer func() { _ = f.Close() }()

	trames, err := wiretrace.ReadTrames(f)
	if err != nil {
		t.Fatalf("read the capture: %v", err)
	}
	if len(trames) < 1400 {
		t.Fatalf("the capture holds %d frames; it was committed with 1489", len(trames))
	}

	p := New(testDeps())
	report, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if len(report.Errors) != 0 {
		for _, e := range report.Errors[:min(3, len(report.Errors))] {
			t.Errorf("frame %d (%s) did not decode: %s — %s",
				e.TrameIndex, e.Direction, e.Err, e.HexPrefix)
		}
		t.Fatalf("%d frames the real controller sent no longer decode", len(report.Errors))
	}
	if report.TramesProcessed != len(trames) {
		t.Errorf("%d of %d frames were processed", report.TramesProcessed, len(trames))
	}

	// Both directions, because a capture of one half is a capture of a
	// monologue: what is being checked is a conversation with hardware.
	if report.PerDirection[wiretrace.DirectionTx] == 0 || report.PerDirection[wiretrace.DirectionRx] == 0 {
		t.Errorf("the capture is one-sided: %v", report.PerDirection)
	}

	for _, inv := range report.Invariants {
		t.Errorf("invariant: %s", inv)
	}
}

func TestValidateReportsWhatWillNotDecode(t *testing.T) {
	p := New(testDeps())

	trames := []wiretrace.Trame{
		{Direction: wiretrace.DirectionTx, Hex: "nothexadecimal"},
		{Direction: wiretrace.DirectionRx, Hex: "ffffffffffffffff"},
	}
	report, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(report.Errors) != 2 {
		t.Fatalf("%d frames reported as undecodable, want both", len(report.Errors))
	}
	if report.Errors[0].HexPrefix == "" {
		t.Error("an undecodable frame was reported without its bytes")
	}
	if report.TramesProcessed != 0 {
		t.Errorf("%d frames counted as processed", report.TramesProcessed)
	}
}

// oneFrame renders a frame as a trame, the way a capture holds it.
func oneFrame(t *testing.T, f codec.Frame, dir wiretrace.Direction) wiretrace.Trame {
	t.Helper()
	b, err := f.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return wiretrace.Trame{Direction: dir, Hex: hex.EncodeToString(b)}
}

func TestValidateNoticesWhatCannotBeTrue(t *testing.T) {
	p := New(testDeps())

	// A 32-bit message addressed outside any session: each field is legal on
	// its own, and the combination is a peer talking to nobody.
	stray := oneFrame(t, codec.Frame{
		Type: codec.MsgGetValue,
		Src:  codec.Address{Unit: 1, Index: codec.IndexUnknown},
		Dst:  codec.Address{Unit: 2, Index: codec.IndexUnknown},
	}, wiretrace.DirectionTx)

	report, err := p.Validate(context.Background(), []wiretrace.Trame{stray},
		consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(report.Invariants) == 0 {
		t.Error("a 32-bit message sent outside a session was not noticed")
	}

	// And an acknowledgement that names no session at either end.
	ack := oneFrame(t, codec.Frame{
		Type: codec.MsgAck,
		Src:  codec.Address{Unit: 1, Index: codec.IndexUnknown},
		Dst:  codec.Address{Unit: 2, Index: codec.IndexUnknown},
	}, wiretrace.DirectionRx)

	report, err = p.Validate(context.Background(), []wiretrace.Trame{ack},
		consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	var found bool
	for _, inv := range report.Invariants {
		if len(inv) > 0 {
			found = true
		}
	}
	if !found {
		t.Error("an acknowledgement belonging to no session was not noticed")
	}
}

func TestValidateStopsWhereItIsAsked(t *testing.T) {
	p := New(testDeps())
	trames := []wiretrace.Trame{
		oneFrame(t, codec.Frame{Type: codec.MsgKeepAlive}, wiretrace.DirectionTx),
		{Direction: wiretrace.DirectionRx, Hex: "00", Note: "here"},
		oneFrame(t, codec.Frame{Type: codec.MsgKeepAlive}, wiretrace.DirectionTx),
	}

	report, err := p.Validate(context.Background(), trames,
		consumer.ValidateOpts{StopAt: "here"})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if report.StoppedAt != "here" {
		t.Errorf("it stopped at %q", report.StoppedAt)
	}
	if report.TramesProcessed != 1 {
		t.Errorf("%d frames were processed before the marker", report.TramesProcessed)
	}
}

func TestValidateWritesTheTreeItHolds(t *testing.T) {
	// A trace is frames rather than a tree, so what is written is the export
	// this plugin holds — honest about being a dump rather than a
	// reconstruction, which is the replay capability ADR-0021 defers.
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })
	ctx := context.Background()
	if _, err := h.plugin.Walk(ctx, 1); err != nil {
		t.Fatalf("Walk: %v", err)
	}

	out := filepath.Join(t.TempDir(), "nested", "tree.json")
	if _, err := h.plugin.Validate(ctx, nil, consumer.ValidateOpts{OutTree: out}); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("the tree was not written: %v", err)
	}
	if len(b) == 0 {
		t.Error("the tree was written empty")
	}
}

func TestValidateStopsWithItsContext(t *testing.T) {
	p := New(testDeps())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	trames := []wiretrace.Trame{oneFrame(t, codec.Frame{Type: codec.MsgKeepAlive}, wiretrace.DirectionTx)}
	if _, err := p.Validate(ctx, trames, consumer.ValidateOpts{}); err == nil {
		t.Error("a cancelled context should stop the replay")
	}
}

func TestATrameCarryingMoreThanOneFrame(t *testing.T) {
	// One record holds one frame. A replay that silently dropped the surplus
	// would test half of what was recorded.
	f, err := codec.Frame{Type: codec.MsgKeepAlive}.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	doubled := append(append([]byte(nil), f...), f...)

	p := New(testDeps())
	report, err := p.Validate(context.Background(), []wiretrace.Trame{
		{Direction: wiretrace.DirectionTx, Hex: hex.EncodeToString(doubled)},
	}, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	var noticed bool
	for _, inv := range report.Invariants {
		if len(inv) > 0 {
			noticed = true
		}
	}
	if !noticed {
		t.Error("a record holding two frames was not noticed")
	}
}

func TestARecorderIsAttachedBeforeConnecting(t *testing.T) {
	// A link records from the frame it opens with, so the capture is attached
	// before the connection rather than after: one that began halfway through
	// is a replay that starts in the middle of a conversation.
	dir := t.TempDir()
	rec, err := transport.NewRecorder(filepath.Join(dir, "wire.jsonl"))
	if err != nil {
		t.Fatalf("recorder: %v", err)
	}

	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })
	h.plugin.SetRecorder(rec)

	p := New(testDeps())
	p.SetRecorder(rec)
	if p.recorder == nil {
		t.Error("the recorder was not kept")
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestAShortHexPrefixIsNotTruncated(t *testing.T) {
	// The prefix orients a reader; a frame shorter than the prefix is shown
	// whole rather than padded or cut.
	if got := hexPrefix("00ff"); got != "00ff" {
		t.Errorf("a short frame was rendered as %q", got)
	}
	long := "00112233445566778899aabbccddeeff00112233"
	if got := hexPrefix(long); len(got) != 32 {
		t.Errorf("a long frame was rendered as %d characters", len(got))
	}
}

func TestValidateCannotWriteWhereItIsNotAllowed(t *testing.T) {
	// A path that cannot be created reaches the caller rather than being
	// swallowed: a validate that reports success and wrote nothing is worse
	// than one that fails.
	h := newHarness(t, nil)

	file := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// A file where a directory has to go.
	out := filepath.Join(file, "nested", "tree.json")
	if _, err := h.plugin.Validate(context.Background(), nil,
		consumer.ValidateOpts{OutTree: out}); err == nil {
		t.Error("writing a tree under a file was reported as success")
	}
}

func TestValidateCannotWriteOverADirectory(t *testing.T) {
	h := newHarness(t, nil)
	dir := t.TempDir()

	if _, err := h.plugin.Validate(context.Background(), nil,
		consumer.ValidateOpts{OutTree: dir}); err == nil {
		t.Error("writing a tree onto a directory was reported as success")
	}
}

func TestValidateStopsBeforeWritingWhenCancelled(t *testing.T) {
	// A trace of nothing still reaches the write, and a cancelled context
	// stops it there rather than leaving a half-written tree behind.
	h := newHarness(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	out := filepath.Join(t.TempDir(), "tree.json")
	if _, err := h.plugin.Validate(ctx, nil, consumer.ValidateOpts{OutTree: out}); err == nil {
		t.Error("a cancelled validate wrote a tree anyway")
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("a tree was left behind by a cancelled validate")
	}
}
