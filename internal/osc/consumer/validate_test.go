package osc

import (
	"context"
	"encoding/hex"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/osc/codec"
	"dhs/internal/wiretrace"
)

func encMsg(t *testing.T, m codec.Message) string {
	t.Helper()
	b, err := m.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return hex.EncodeToString(b)
}

func tr(dir wiretrace.Direction, hexStr string) wiretrace.Trame {
	return wiretrace.Trame{Direction: dir, Hex: hexStr}
}

// full-tag message set: every oscArgToValue arm.
func allTagTrames(t *testing.T) []wiretrace.Trame {
	t.Helper()
	msgs := []codec.Message{
		{Address: "/int", Args: []codec.Arg{codec.Int32(7)}},
		{Address: "/long", Args: []codec.Arg{{Tag: 'h', Int64: 9}}},
		{Address: "/f", Args: []codec.Arg{codec.Float32(1.5)}},
		{Address: "/d", Args: []codec.Arg{{Tag: 'd', Float64: 2.5}}},
		{Address: "/s", Args: []codec.Arg{codec.String("hi")}},
		{Address: "/sym", Args: []codec.Arg{{Tag: 'S', String: "sym"}}},
		{Address: "/b", Args: []codec.Arg{{Tag: 'b', Blob: []byte{1, 2}}}},
		{Address: "/t", Args: []codec.Arg{{Tag: 't', Uint64: 42}}},
		{Address: "/on", Args: []codec.Arg{{Tag: 'T'}}},
		{Address: "/off", Args: []codec.Arg{{Tag: 'F'}}},
		{Address: "/nil", Args: []codec.Arg{{Tag: 'N'}}},
		{Address: "/bare"}, // no args — Kind stays unknown
	}
	out := make([]wiretrace.Trame, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, tr(wiretrace.DirectionRx, encMsg(t, m)))
	}
	return out
}

func newV10(t *testing.T) *Plugin {
	t.Helper()
	return NewPluginV10(slog.Default())
}

func TestValidate_MessagesAndOutputs(t *testing.T) {
	dir := t.TempDir()
	tree := filepath.Join(dir, "tree.json")
	paramsCSV := filepath.Join(dir, "params.csv")

	trames := allTagTrames(t)
	// Repeat one address: last value wins, ID stays stable.
	trames = append(trames, tr(wiretrace.DirectionTx, encMsg(t, codec.Message{
		Address: "/int", Args: []codec.Arg{codec.Int32(8)},
	})))

	rep, err := newV10(t).Validate(context.Background(), trames, consumer.ValidateOpts{
		OutTree: tree, OutParams: paramsCSV,
	})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if rep.TramesProcessed != len(trames) {
		t.Fatalf("processed %d, want %d", rep.TramesProcessed, len(trames))
	}
	if rep.PerDirection[wiretrace.DirectionRx] != len(trames)-1 || rep.PerDirection[wiretrace.DirectionTx] != 1 {
		t.Fatalf("per-direction = %+v", rep.PerDirection)
	}
	data, err := os.ReadFile(tree)
	if err != nil {
		t.Fatalf("out-tree: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, `"osc-v10"`) || !strings.Contains(s, "/int") {
		t.Fatalf("tree.json missing protocol/address: %s", s[:min(200, len(s))])
	}
	if _, err := os.ReadFile(paramsCSV); err != nil {
		t.Fatalf("out-params: %v", err)
	}
}

func TestValidate_BundleRecursion(t *testing.T) {
	inner, err := (codec.Message{Address: "/a", Args: []codec.Arg{codec.Int32(1)}}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	bundle := codec.Bundle{Timetag: 1}
	msg, err := codec.DecodeMessage(inner)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Elements = []codec.Packet{msg, codec.Bundle{Timetag: 2, Elements: []codec.Packet{msg}}}
	raw, err := bundle.Encode()
	if err != nil {
		t.Fatal(err)
	}
	rep, err := newV10(t).Validate(context.Background(),
		[]wiretrace.Trame{tr(wiretrace.DirectionRx, hex.EncodeToString(raw))},
		consumer.ValidateOpts{OutTree: filepath.Join(t.TempDir(), "t.json")})
	if err != nil {
		t.Fatal(err)
	}
	if rep.TramesProcessed != 1 {
		t.Fatalf("processed = %d", rep.TramesProcessed)
	}
}

func TestValidate_InvariantFromUnknownTag(t *testing.T) {
	// Hand-built message with tag string ",z" — the codec records
	// osc_type_tag_unknown as a ComplianceNote instead of failing.
	raw := append([]byte("/x\x00\x00"), []byte(",z\x00\x00")...)
	rep, err := newV10(t).Validate(context.Background(),
		[]wiretrace.Trame{tr(wiretrace.DirectionRx, hex.EncodeToString(raw))},
		consumer.ValidateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Invariants) != 1 || !strings.Contains(rep.Invariants[0], "osc_type_tag_unknown") {
		t.Fatalf("invariants = %v", rep.Invariants)
	}
}

func TestValidate_Framings(t *testing.T) {
	inner, err := (codec.Message{Address: "/tcp", Args: []codec.Arg{codec.Int32(1)}}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	trames := []wiretrace.Trame{
		tr(wiretrace.DirectionRx, hex.EncodeToString(codec.EncodeLenPrefix(inner))),
		tr(wiretrace.DirectionRx, hex.EncodeToString(codec.EncodeSLIP(inner))),
	}
	rep, err := newV10(t).Validate(context.Background(), trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.TramesProcessed != 2 || len(rep.Errors) != 0 {
		t.Fatalf("report = %+v", rep)
	}
}

func TestValidate_ErrorArms(t *testing.T) {
	trames := []wiretrace.Trame{
		{Direction: wiretrace.DirectionRx, Hex: "zz"},               // hex error
		tr(wiretrace.DirectionRx, "c0ff"),                           // SLIP garbage
		tr(wiretrace.DirectionRx, "00000010deadbeef"),               // len-prefix truncated
		tr(wiretrace.DirectionRx, hex.EncodeToString([]byte("Q"))),  // undecodable, non-framed
		tr(wiretrace.DirectionRx, ""),                               // empty raw
	}
	rep, err := newV10(t).Validate(context.Background(), trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.TramesProcessed != 0 || len(rep.Errors) != len(trames) {
		t.Fatalf("report = %+v", rep)
	}
}

func TestValidate_StopAtAndCancel(t *testing.T) {
	m := encMsg(t, codec.Message{Address: "/a", Args: []codec.Arg{codec.Int32(1)}})
	trames := []wiretrace.Trame{
		{Direction: wiretrace.DirectionRx, Hex: m, Note: "here"},
		tr(wiretrace.DirectionRx, m),
	}
	rep, err := newV10(t).Validate(context.Background(), trames, consumer.ValidateOpts{StopAt: "here"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.StoppedAt != "here" || rep.TramesProcessed != 1 {
		t.Fatalf("stop-at report = %+v", rep)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newV10(t).Validate(ctx, trames, consumer.ValidateOpts{}); err == nil {
		t.Fatal("cancelled ctx must surface")
	}
}

func TestValidate_OutputWriteErrors(t *testing.T) {
	m := encMsg(t, codec.Message{Address: "/a", Args: []codec.Arg{codec.Int32(1)}})
	bad := filepath.Join(t.TempDir(), "no", "such", "dir", "x.json")
	if _, err := newV10(t).Validate(context.Background(),
		[]wiretrace.Trame{tr(wiretrace.DirectionRx, m)},
		consumer.ValidateOpts{OutTree: bad}); err == nil || !strings.Contains(err.Error(), "out-tree") {
		t.Fatalf("want out-tree write error, got %v", err)
	}
	if _, err := newV10(t).Validate(context.Background(),
		[]wiretrace.Trame{tr(wiretrace.DirectionRx, m)},
		consumer.ValidateOpts{OutParams: bad}); err == nil || !strings.Contains(err.Error(), "out-params") {
		t.Fatalf("want out-params write error, got %v", err)
	}
}

func TestSplitOSCAddress(t *testing.T) {
	if got := splitOSCAddress("/mixer/ch/1"); len(got) != 3 || got[0] != "mixer" {
		t.Fatalf("split = %v", got)
	}
	if got := splitOSCAddress("/"); len(got) != 1 || got[0] != "/" {
		t.Fatalf("degenerate split = %v", got)
	}
}

func TestOSCHelperEdges(t *testing.T) {
	if noteDetail("") != "" || noteDetail("x") != " (x)" {
		t.Fatal("noteDetail")
	}
	if len(oscShortHex(make([]byte, 40))) != 32 {
		t.Fatal("oscShortHex clamp")
	}
	if k, _ := oscArgToValue(codec.Arg{Tag: '?'}); k != consumer.KindUnknown {
		t.Fatal("unknown tag kind")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
