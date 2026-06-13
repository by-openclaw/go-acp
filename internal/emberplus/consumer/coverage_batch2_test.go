package emberplus

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/emberplus/codec/glow"
	"dhs/internal/emberplus/codec/s101"
	"dhs/internal/wiretrace"
)

// TestMergeAnnouncedParameter_AllArms covers the nil-existing and
// nil-incoming early returns plus a full-field overlay so every
// "incoming wins" branch fires.
func TestMergeAnnouncedParameter_AllArms(t *testing.T) {
	if got := mergeAnnouncedParameter(nil, &glow.Parameter{Number: 1}); got.Number != 1 {
		t.Error("nil existing → return incoming")
	}
	if got := mergeAnnouncedParameter(&glow.Parameter{Number: 2}, nil); got.Number != 2 {
		t.Error("nil incoming → return existing")
	}

	existing := &glow.Parameter{Number: 1, Identifier: "keep", Value: int64(0)}
	incoming := &glow.Parameter{
		Path: []int32{1, 1}, Number: 9, Description: "d", Value: int64(5),
		Minimum: int64(0), Maximum: int64(10), Access: 3, Format: "%d",
		Enumeration: "a\nb", Factor: 2, IsOnline: true, Formula: "x", Step: int64(1),
		Default: int64(0), Type: glow.ParamTypeInteger,
		HasStreamIdentifier: true, StreamIdentifier: 7,
		EnumMap:           map[int64]string{0: "a"},
		StreamDescriptor:  &glow.StreamDescription{Format: 1},
		SchemaIdentifiers: "sch", TemplateReference: []int32{10, 1},
		Children: []glow.Element{{Node: &glow.Node{Number: 1}}},
	}
	merged := mergeAnnouncedParameter(existing, incoming)
	if merged.Identifier != "keep" {
		t.Errorf("identifier must stay from walk, got %q", merged.Identifier)
	}
	if merged.Number != 9 || merged.Value != int64(5) || merged.Type != glow.ParamTypeInteger {
		t.Errorf("incoming fields should win: %+v", merged)
	}
	if !merged.HasStreamIdentifier || merged.StreamIdentifier != 7 {
		t.Error("stream identifier should overlay")
	}
	if merged.StreamDescriptor == nil || len(merged.EnumMap) == 0 || len(merged.Children) == 0 {
		t.Error("descriptor/enumMap/children should overlay")
	}
}

// TestApplyParameterConstraints_EdgeBranches covers the nil-param,
// non-numeric (with and without Round), and the resolveEnumLabelToIndex
// empty-string short-circuit.
func TestApplyParameterConstraints_EdgeBranches(t *testing.T) {
	// nil param → nil.
	v := &consumer.Value{Kind: consumer.KindInt, Int: 5}
	if err := applyParameterConstraints(v, nil, consumer.ValueRequest{}); err != nil {
		t.Errorf("nil param: %v", err)
	}
	// non-numeric, no round → nil.
	vs := &consumer.Value{Kind: consumer.KindString, Str: "x"}
	if err := applyParameterConstraints(vs, &glow.Parameter{}, consumer.ValueRequest{}); err != nil {
		t.Errorf("non-numeric no-round: %v", err)
	}
	// non-numeric, round=true → ErrRoundNotApplicable.
	if err := applyParameterConstraints(vs, &glow.Parameter{}, consumer.ValueRequest{Round: true}); err == nil {
		t.Error("non-numeric with round should error")
	}

	// resolveEnumLabelToIndex empty-string short-circuit.
	ve := &consumer.Value{Kind: consumer.KindEnum, Str: ""}
	if err := resolveEnumLabelToIndex(ve, &glow.Parameter{}); err != nil {
		t.Errorf("empty enum string: %v", err)
	}
}

// TestEnrichMatrixLabels_SkipBranches covers the skip arms: nil Meta,
// non-matrix element, matrix without labels, label with empty basePath,
// and a matrix whose labels resolve to nothing.
func TestEnrichMatrixLabels_SkipBranches(t *testing.T) {
	objs := []consumer.Object{
		{OID: "1", Meta: nil},                                  // nil Meta → skip
		{OID: "2", Meta: map[string]any{"element": "node"}},    // non-matrix → skip
		{OID: "3", Meta: map[string]any{"element": "matrix"}},  // no labels key → skip
		{OID: "4", Meta: map[string]any{                        // empty basePath → skip level
			"element": "matrix",
			"labels":  []map[string]any{{"basePath": "", "description": "x"}},
		}},
		{OID: "5", Meta: map[string]any{ // labels present but resolve to nothing
			"element": "matrix",
			"labels":  []map[string]any{{"basePath": "9.9", "description": "y"}},
		}},
		{OID: "6", Meta: map[string]any{ // labels key present but normalises to empty
			"element": "matrix",
			"labels":  []any{}, // empty after normalise → len(labels)==0 skip
		}},
	}
	out := enrichMatrixLabels(objs)
	for _, o := range out {
		if _, has := o.Meta["targetLabels"]; has {
			t.Errorf("OID %s should not have gained targetLabels", o.OID)
		}
	}
}

// TestValidate_MoreInvariants covers the version invariant, the non-Glow
// DTD invariant, the MPM-skip branch, a glow-decode-error, a keepalive
// with an unknown command, a keepalive carrying payload, and an unknown
// s101 message type.
func TestValidate_MoreInvariants(t *testing.T) {
	p := newTreePlugin()

	// Version != 1 → version invariant.
	badVer := s101.Encode(&s101.Frame{
		Slot: s101.SlotDefault, MsgType: s101.MsgEmBER, Command: s101.CmdEmBER,
		Version: 9, Flags: s101.FlagSingle, DTD: s101.DTDGlow, Payload: glow.EncodeGetDirectory(),
	})
	// Non-Glow DTD → DTD invariant.
	badDTD := s101.Encode(&s101.Frame{
		Slot: s101.SlotDefault, MsgType: s101.MsgEmBER, Command: s101.CmdEmBER,
		Version: s101.VersionS101, Flags: s101.FlagSingle, DTD: 0x99, Payload: glow.EncodeGetDirectory(),
	})
	// Mid-flight MPM (FlagFirst only) → glow decode skipped.
	mpm := s101.Encode(&s101.Frame{
		Slot: s101.SlotDefault, MsgType: s101.MsgEmBER, Command: s101.CmdEmBER,
		Version: s101.VersionS101, Flags: s101.FlagFirst, DTD: s101.DTDGlow, Payload: []byte{0x60},
	})
	// Single EmBER frame with undecodable payload → glow decode error.
	badGlow := s101.Encode(&s101.Frame{
		Slot: s101.SlotDefault, MsgType: s101.MsgEmBER, Command: s101.CmdEmBER,
		Version: s101.VersionS101, Flags: s101.FlagSingle, DTD: s101.DTDGlow, Payload: []byte{0xFF, 0xFF, 0xFF},
	})
	// Keepalive with an unknown command.
	badKA := s101.Encode(&s101.Frame{
		Slot: s101.SlotDefault, MsgType: s101.MsgKeepAlive, Command: 0x55,
		Version: s101.VersionS101,
	})
	// Keepalive carrying an unexpected payload.
	kaPayload := s101.Encode(&s101.Frame{
		Slot: s101.SlotDefault, MsgType: s101.MsgKeepAlive, Command: s101.CmdKeepAliveReq,
		Version: s101.VersionS101, Payload: []byte{0x01, 0x02},
	})
	// Unknown s101 message type.
	badMsg := s101.Encode(&s101.Frame{
		Slot: s101.SlotDefault, MsgType: 0x77, Command: 0x00, Version: s101.VersionS101,
	})

	mk := func(b []byte) wiretrace.Trame {
		return wiretrace.Trame{SchemaVersion: 1, Direction: wiretrace.DirectionRx, Hex: hex.EncodeToString(b)}
	}
	trames := []wiretrace.Trame{
		mk(badVer), mk(badDTD), mk(mpm), mk(badGlow), mk(badKA), mk(kaPayload), mk(badMsg),
	}
	rep, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	// Each crafted frame exercises a distinct branch; collectively they
	// must produce at least one invariant or decode error (the precise
	// bucket per frame depends on s101 encode/decode round-trip).
	if len(rep.Invariants) == 0 && len(rep.Errors) == 0 {
		t.Errorf("expected invariants or errors; processed=%d", rep.TramesProcessed)
	}
}

// crcX25 reproduces the s101 codec's CRC-16/X-25 (init 0xFFFF, reflected
// poly 0x1021, xorout 0xFFFF) so the test can hand-craft malformed S101
// frames the codec's own Encode (which always emits canonical headers)
// cannot produce — exercising Validate's defensive invariant branches.
func crcX25(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0x8408 // reflected 0x1021
			} else {
				crc >>= 1
			}
		}
	}
	return ^crc & 0xFFFF
}

// wrapS101 builds a complete S101 frame (BOF + escaped content + CRC + EOF)
// from raw header+payload content, matching the codec's wire format.
func wrapS101(content []byte) []byte {
	crc := crcX25(content)
	raw := append(append([]byte{}, content...), byte(crc&0xFF), byte(crc>>8))
	out := []byte{0xFE} // BOF
	for _, b := range raw {
		if b >= 0xF8 {
			out = append(out, 0xFD, b^0x20) // ESC
		} else {
			out = append(out, b)
		}
	}
	return append(out, 0xFF) // EOF
}

// TestValidate_MalformedHeaders crafts S101 frames with a bad version, a
// non-Glow DTD, an EmBER msgtype with an unexpected command, a keepalive
// with an unknown command, a keepalive carrying payload, and an unknown
// message type — branches the canonical-only Encode cannot reach.
func TestValidate_MalformedHeaders(t *testing.T) {
	p := newTreePlugin()

	// EmBER, version=9 (bad), Glow DTD, single flag, valid GetDirectory.
	payload := glow.EncodeGetDirectory()
	badVer := wrapS101(append([]byte{0x00, 0x0E, 0x00, 0x09, 0xC0, 0x01, 0x02, 0x3C, 0x02}, payload...))
	// EmBER, version=1, non-Glow DTD (0x10 ≠ 0x01) → falls to 5-byte
	// header path → payload starts at offset 5; still MsgEmBER so the
	// DTD check is on f.DTD (0 here) → DTD invariant.
	badDTD := wrapS101(append([]byte{0x00, 0x0E, 0x00, 0x01, 0xC0, 0x10, 0x02, 0x3C, 0x02}, payload...))
	// MsgEmBER msgtype, command 0x42 (not EmBER, not keepalive).
	badCmd := wrapS101([]byte{0x00, 0x0E, 0x42, 0x01, 0xC0, 0x01, 0x02, 0x3C, 0x02})
	// Keepalive msgtype (content[1]=0x01) with an unknown command 0x55 AND
	// a trailing payload. s101.Decode copies content[1] verbatim into
	// Frame.MsgType, so this decodes as MsgType=MsgKeepAlive with
	// Command=0x55. Because 0x55 is neither req(0x01) nor resp(0x02),
	// Decode does NOT take its keep-alive early-return; it falls through to
	// the EmBER header path. With a 5-byte minimal header [slot, msgType,
	// cmd, version, flags] plus extra bytes where content[5] != DTDGlow,
	// Decode's 5-byte-header fallback sets Payload = content[5:]. The
	// resulting decoded frame therefore drives BOTH validate.go keep-alive
	// invariant arms in one shot: the unknown-command default (Command=0x55)
	// and the unexpected-payload guard (len(Payload)=2). This is genuinely
	// reachable through the real s101.Decode entrypoint — the prior pass's
	// "unreachable" note mis-assumed validate keyed off Command, but it
	// keys off MsgType, which Decode takes straight from content[1].
	badKACmd := wrapS101([]byte{0x00, 0x01, 0x55, 0x01, 0xC0, 0xAB, 0xCD})
	// Unknown message type 0x77 with a non-keepalive command.
	badMsg := wrapS101([]byte{0x00, 0x77, 0x42, 0x01, 0xC0})

	mk := func(b []byte) wiretrace.Trame {
		return wiretrace.Trame{SchemaVersion: 1, Direction: wiretrace.DirectionRx, Hex: hex.EncodeToString(b)}
	}
	trames := []wiretrace.Trame{mk(badVer), mk(badDTD), mk(badCmd), mk(badKACmd), mk(badMsg)}
	rep, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(rep.Invariants) == 0 {
		t.Errorf("expected invariant violations; processed=%d errors=%d", rep.TramesProcessed, len(rep.Errors))
	}
	// Explicitly assert both keep-alive invariant arms fired: the
	// unknown-command message and the unexpected-payload message.
	var sawUnknownKACmd, sawKAPayload bool
	for _, inv := range rep.Invariants {
		if strings.Contains(inv, "keep-alive with unknown command") {
			sawUnknownKACmd = true
		}
		if strings.Contains(inv, "keep-alive carries unexpected payload") {
			sawKAPayload = true
		}
	}
	if !sawUnknownKACmd {
		t.Errorf("expected keep-alive unknown-command invariant; got %v", rep.Invariants)
	}
	if !sawKAPayload {
		t.Errorf("expected keep-alive unexpected-payload invariant; got %v", rep.Invariants)
	}
}

// TestValidate_CtxCancelMidLoop covers the per-iteration ctx.Err() check
// inside Validate's loop.
func TestValidate_CtxCancelMidLoop(t *testing.T) {
	p := newTreePlugin()
	good := s101.Encode(s101.NewEmBERFrame(glow.EncodeGetDirectory()))
	trames := []wiretrace.Trame{
		{SchemaVersion: 1, Direction: wiretrace.DirectionRx, Hex: hex.EncodeToString(good)},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Validate(ctx, trames, consumer.ValidateOpts{}); err == nil {
		t.Error("Validate with cancelled ctx should return the ctx error")
	}
}
