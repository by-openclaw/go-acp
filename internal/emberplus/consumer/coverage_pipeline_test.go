package emberplus

import (
	"context"
	"encoding/hex"
	"log/slog"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/consumer/compliance"
	"dhs/internal/emberplus/codec/ber"
	"dhs/internal/emberplus/codec/glow"
	"dhs/internal/emberplus/codec/s101"
	"dhs/internal/wiretrace"
)

// newTreePlugin returns a Plugin with its tree indices + profile +
// unknownCTX initialised the same way Connect does, so the element
// processing pipeline can run without a live session.
func newTreePlugin() *Plugin {
	p := &Plugin{logger: slog.Default()}
	p.numIndex = make(map[string]*treeEntry)
	p.pathIndex = make(map[string]*treeEntry)
	p.labelIndex = make(map[string][]*treeEntry)
	p.numPath = make(map[string]string)
	p.subs = make(map[string]consumer.EventFunc)
	p.streamSubs = make(map[string][]int32)
	p.streamIndex = make(map[int64][]string)
	p.templates = make(map[string]*glow.Template)
	p.pendingSets = newPendingSetRegistry()
	p.profile = &compliance.Profile{}
	p.unknownCTX = newUnknownCTXAudit()
	return p
}

// richGlowTree builds a decoded Glow element tree (qualified forms) that
// exercises Node / Parameter (all types) / Matrix / Function / Template
// processing in the consumer.
func richGlowTree() []glow.Element {
	return []glow.Element{
		{Node: &glow.Node{
			Path: []int32{1}, Number: 1, Identifier: "root", Description: "root node",
			IsOnline: true, SchemaIdentifiers: "sch",
		}},
		{Parameter: &glow.Parameter{
			Path: []int32{1, 1}, Number: 1, Identifier: "count", Type: glow.ParamTypeInteger,
			Value: int64(5), Minimum: int64(0), Maximum: int64(10), Step: int64(1),
			Access: glow.AccessReadWrite, Format: "%d", Factor: 10, IsOnline: true,
		}},
		{Parameter: &glow.Parameter{
			Path: []int32{1, 2}, Number: 2, Identifier: "gain", Type: glow.ParamTypeReal,
			Value: float64(-3.0), Access: glow.AccessReadWrite,
			StreamIdentifier: 7, HasStreamIdentifier: true,
			StreamDescriptor: &glow.StreamDescription{Format: glow.StreamFmtFloat64LittleEndian},
		}},
		{Parameter: &glow.Parameter{
			Path: []int32{1, 3}, Number: 3, Identifier: "mode", Type: glow.ParamTypeEnum,
			Value: int64(1), Enumeration: "off\non", Access: glow.AccessReadWrite,
			EnumMap: map[int64]string{0: "off", 1: "on"},
		}},
		{Parameter: &glow.Parameter{
			Path: []int32{1, 4}, Number: 4, Identifier: "name", Type: glow.ParamTypeString,
			Value: "hi", Access: glow.AccessRead,
		}},
		{Parameter: &glow.Parameter{
			Path: []int32{1, 5}, Number: 5, Identifier: "on", Type: glow.ParamTypeBoolean,
			Value: true, Access: glow.AccessReadWrite,
		}},
		{Matrix: &glow.Matrix{
			Path: []int32{1, 6}, Number: 6, Identifier: "mat",
			MatrixType: glow.MatrixTypeNToN, AddressingMode: glow.MatrixAddrNonLinear,
			TargetCount: 2, SourceCount: 2, MaxTotalConnects: 8, MaxConnectsPerTarget: 2,
			Targets: []int32{0, 1}, Sources: []int32{0, 1},
			Labels: []glow.Label{{BasePath: []int32{1, 6, 1}, Description: "lbl"}},
			Connections: []glow.Connection{
				{Target: 0, Sources: []int32{0}, Operation: glow.ConnOpAbsolute, Disposition: glow.ConnDispTally},
			},
		}},
		{Function: &glow.Function{
			Path: []int32{1, 7}, Number: 7, Identifier: "sum", Description: "adds",
			Arguments: []glow.TupleItem{{Type: glow.ParamTypeInteger, Name: "a"}},
			Result:    []glow.TupleItem{{Type: glow.ParamTypeInteger, Name: "r"}},
		}},
		{Template: &glow.Template{
			Qualified: true, Path: []int32{9, 1}, Number: 1, Description: "tpl",
			Element: &glow.TemplateElement{
				Parameter: &glow.Parameter{Number: 1, Identifier: "proto", Type: glow.ParamTypeInteger},
			},
		}},
	}
}

// TestConsumerPipeline drives handleElements through every process*
// handler, then exercises Canonicalize / ExportCanonical / GlowSnapshot
// over the populated tree.
func TestConsumerPipeline(t *testing.T) {
	p := newTreePlugin()
	p.handleElements(richGlowTree())

	if len(p.numIndex) == 0 {
		t.Fatal("tree empty after handleElements")
	}

	ctx := context.Background()
	exp, err := p.Canonicalize(ctx, CanonicalOptions{})
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	if exp.Root == nil {
		t.Error("canonical export has no root")
	}

	// ExportCanonical (labels/gain="both") path.
	if _, err := p.ExportCanonical(ctx); err != nil {
		t.Fatalf("ExportCanonical: %v", err)
	}

	// Canonicalize with inline/both options exercises normMode arms.
	if _, err := p.Canonicalize(ctx, CanonicalOptions{Templates: "inline", Labels: "both", Gain: "INLINE"}); err != nil {
		t.Fatalf("Canonicalize inline: %v", err)
	}

	// GlowSnapshot dumps the same tree losslessly.
	dump, err := p.GlowSnapshot(ctx)
	if err != nil {
		t.Fatalf("GlowSnapshot: %v", err)
	}
	if dump == nil || len(dump.Entries) == 0 {
		t.Error("empty glow dump")
	}

	// Context-cancelled guards.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := p.Canonicalize(cancelled, CanonicalOptions{}); err == nil {
		t.Error("Canonicalize should honour cancelled ctx")
	}
	if _, err := p.GlowSnapshot(cancelled); err == nil {
		t.Error("GlowSnapshot should honour cancelled ctx")
	}
}

// TestProcessParameter_AnnounceMerge feeds the same parameter twice: a
// full walk then a value-only announce, exercising mergeAnnouncedParameter
// and the diff path.
func TestProcessParameter_AnnounceMerge(t *testing.T) {
	p := newTreePlugin()
	full := &glow.Parameter{
		Path: []int32{1, 1}, Number: 1, Identifier: "g", Type: glow.ParamTypeInteger,
		Value: int64(1), Access: glow.AccessReadWrite, Description: "desc",
	}
	p.handleElements([]glow.Element{{Parameter: full}})
	// Announce: only the value changes; other fields absent (zero).
	announce := &glow.Parameter{Path: []int32{1, 1}, Number: 1, Value: int64(2)}
	p.handleElements([]glow.Element{{Parameter: announce}})

	e := p.numIndex["1.1"]
	if e == nil || e.glowParam == nil {
		t.Fatal("entry missing after announce")
	}
	// Merge must preserve the identifier from the walk.
	if e.glowParam.Identifier != "g" {
		t.Errorf("announce merge stripped identifier: %q", e.glowParam.Identifier)
	}
	if e.glowParam.Value != int64(2) {
		t.Errorf("announce value not applied: %v", e.glowParam.Value)
	}
}

// TestValidate drives the offline wire-trace validator across good,
// keep-alive, bad-hex, bad-s101, and invariant-violation frames.
func TestValidate(t *testing.T) {
	p := newTreePlugin()
	ctx := context.Background()

	good := s101.Encode(s101.NewEmBERFrame(glow.EncodeGetDirectory()))
	ka := s101.Encode(s101.NewKeepAliveRequest())

	trames := []wiretrace.Trame{
		{SchemaVersion: 1, Direction: wiretrace.DirectionTx, Hex: hex.EncodeToString(good), Note: "stophere"},
		{SchemaVersion: 1, Direction: wiretrace.DirectionRx, Hex: hex.EncodeToString(ka)},
		{SchemaVersion: 1, Direction: wiretrace.DirectionRx, Hex: "zznothex"}, // bad hex
		{SchemaVersion: 1, Direction: wiretrace.DirectionRx, Hex: "00"},       // bad s101 (no BOF/EOF)
	}
	rep, err := p.Validate(ctx, trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if rep.TramesProcessed < 2 {
		t.Errorf("expected >=2 processed, got %d", rep.TramesProcessed)
	}
	if len(rep.Errors) < 2 {
		t.Errorf("expected bad-hex + bad-s101 errors, got %d", len(rep.Errors))
	}

	// StopAt halts at the matching note.
	repStop, err := p.Validate(ctx, trames, consumer.ValidateOpts{StopAt: "stophere"})
	if err != nil {
		t.Fatalf("Validate stop: %v", err)
	}
	if repStop.StoppedAt != "stophere" {
		t.Errorf("StoppedAt = %q, want stophere", repStop.StoppedAt)
	}

	// --out-tree not implemented → error.
	if _, err := p.Validate(ctx, nil, consumer.ValidateOpts{OutTree: "x.json"}); err == nil {
		t.Error("Validate with OutTree should error (not implemented)")
	}

	// shortHex helper: >16 bytes truncates.
	if len(shortHex(make([]byte, 100))) != 32 {
		t.Error("shortHex should truncate to 16 bytes (32 hex chars)")
	}
}

// TestValidate_Invariants covers the invariant branches: an EmBER frame
// whose command is not CmdEmBER (the keep-alive-over-EmBER case) and a
// frame whose slot is non-zero.
func TestValidate_Invariants(t *testing.T) {
	p := newTreePlugin()
	// Keep-alive uses MsgEmBER msgtype with a non-EmBER command — this
	// trips the "EmBER msg with unexpected command" invariant.
	ka := s101.Encode(s101.NewKeepAliveRequest())
	// A frame with a non-zero slot trips the slot invariant.
	badSlot := s101.Encode(&s101.Frame{
		Slot: 1, MsgType: s101.MsgEmBER, Command: s101.CmdEmBER,
		Version: s101.VersionS101, Flags: s101.FlagSingle, DTD: s101.DTDGlow,
		Payload: glow.EncodeGetDirectory(),
	})
	trames := []wiretrace.Trame{
		{SchemaVersion: 1, Direction: wiretrace.DirectionRx, Hex: hex.EncodeToString(ka)},
		{SchemaVersion: 1, Direction: wiretrace.DirectionRx, Hex: hex.EncodeToString(badSlot)},
	}
	rep, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(rep.Invariants) == 0 {
		t.Error("expected invariant violations")
	}
}

// TestProcessParameter_NonQualified feeds a non-qualified Parameter
// (Number only, no Path) to drive resolveNumPath's derive branch +
// profile.Note(NonQualifiedElement).
func TestProcessParameter_NonQualified(t *testing.T) {
	p := newTreePlugin()
	node := &glow.Node{Number: 1, Identifier: "root", IsOnline: true,
		Children: []glow.Element{
			{Parameter: &glow.Parameter{Number: 2, Identifier: "child", Type: glow.ParamTypeInteger, Value: int64(1)}},
		},
	}
	p.handleElements([]glow.Element{{Node: node}})
	// The non-qualified child should land at numeric path 1.2.
	if _, ok := p.numIndex["1.2"]; !ok {
		t.Errorf("non-qualified child not indexed at 1.2; index=%v", keys(p.numIndex))
	}
	if p.profile.Snapshot()[NonQualifiedElement] == 0 {
		t.Error("NonQualifiedElement compliance event not fired")
	}
}

func keys(m map[string]*treeEntry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestProcessTemplate_NonQualified covers the non-qualified template key
// path + the empty-key guard.
func TestProcessTemplate_NonQualified(t *testing.T) {
	p := newTreePlugin()
	p.processTemplate(&glow.Template{Number: 5, Description: "t"})
	if _, ok := p.templates["5"]; !ok {
		t.Error("non-qualified template not stored under number key")
	}
	// Empty key (qualified with empty path) → dropped.
	p.processTemplate(&glow.Template{Qualified: true})
	if _, ok := p.templates[""]; ok {
		t.Error("empty-key template should be dropped")
	}
}

// TestEncodeRoundTripThroughDecode is a smoke check that the consumer's
// process pipeline accepts a real provider-shaped GetDirectory reply when
// decoded — closing the loop on the encoder/decoder contract offline.
func TestProcessFromDecodedBER(t *testing.T) {
	p := newTreePlugin()
	// Build a QualifiedParameter the way a provider would and decode it.
	qp := ber.AppConstructed(glow.TagRoot,
		ber.AppConstructed(glow.TagRootElementCollection,
			ber.ContextConstructed(0,
				ber.AppConstructed(glow.TagQualifiedParameter,
					ber.ContextConstructed(glow.QParamPath, ber.RelOID(encodeRel([]int32{1, 1}))),
					ber.ContextConstructed(glow.QParamContents, ber.Set(
						ber.ContextConstructed(glow.ParamContentIdentifier, ber.UTF8("p")),
						ber.ContextConstructed(glow.ParamContentValue, ber.Integer(42)),
						ber.ContextConstructed(glow.ParamContentType, ber.Integer(glow.ParamTypeInteger)),
					)),
				),
			),
		),
	)
	els, err := glow.DecodeRoot(ber.EncodeTLV(qp))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	p.handleElements(els)
	if e := p.numIndex["1.1"]; e == nil || e.glowParam == nil || e.glowParam.Value != int64(42) {
		t.Errorf("decoded parameter not processed: %+v", p.numIndex["1.1"])
	}
}

// encodeRel mirrors the glow relative-OID encoding for test fixtures.
func encodeRel(path []int32) []byte {
	var out []byte
	for _, v := range path {
		if v < 128 {
			out = append(out, byte(v))
			continue
		}
		var parts []byte
		for v > 0 {
			parts = append([]byte{byte(v & 0x7F)}, parts...)
			v >>= 7
		}
		for i := 0; i < len(parts)-1; i++ {
			parts[i] |= 0x80
		}
		out = append(out, parts...)
	}
	return out
}
