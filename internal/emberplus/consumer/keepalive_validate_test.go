package emberplus

import (
	"context"
	"encoding/hex"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/emberplus/codec/glow"
	"dhs/internal/emberplus/codec/s101"
	"dhs/internal/wiretrace"
)

// TestValidate_KeepAliveNotFlagged is the regression for the stream-capture
// finding: S101 keep-alives are encoded as MsgEmBER with command 0x01/0x02
// (the wire has no distinct MsgKeepAlive type). validate used to flag them as
// "EmBER msg with unexpected command" — which made every long / streaming
// capture (keep-alives flow every few seconds) fail validation. They must be
// accepted as keep-alives, not errors.
func TestValidate_KeepAliveNotFlagged(t *testing.T) {
	p := newTreePlugin()

	req := s101.Encode(s101.NewKeepAliveRequest())
	resp := s101.Encode(s101.NewKeepAliveResponse())
	ember := s101.Encode(&s101.Frame{
		Slot: s101.SlotDefault, MsgType: s101.MsgEmBER, Command: s101.CmdEmBER,
		Version: s101.VersionS101, Flags: s101.FlagSingle, DTD: s101.DTDGlow,
		Payload: glow.EncodeGetDirectory(),
	})

	mk := func(b []byte) wiretrace.Trame {
		return wiretrace.Trame{SchemaVersion: 1, Direction: wiretrace.DirectionRx, Hex: hex.EncodeToString(b)}
	}
	rep, err := p.Validate(context.Background(),
		[]wiretrace.Trame{mk(req), mk(resp), mk(ember)}, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if rep.TramesProcessed != 3 {
		t.Errorf("processed %d, want 3", rep.TramesProcessed)
	}
	if len(rep.Invariants) != 0 {
		t.Errorf("keep-alive frames must not be flagged as invariants: %v", rep.Invariants)
	}
	if len(rep.Errors) != 0 {
		t.Errorf("unexpected decode errors: %v", rep.Errors)
	}
}
