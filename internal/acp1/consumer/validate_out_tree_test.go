package acp1

import (
	"context"
	"encoding/hex"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/export"
	"dhs/internal/consumer"
	"dhs/internal/wiretrace"
	"dhs/internal/acp1/codec"
)

// buildGetObjectTrame constructs one Trame holding a getObject reply.
// Helper for test fixtures -- no live wire activity.
func buildGetObjectTrame(t *testing.T, mtid uint32, slot uint8, group codec.ObjGroup, id uint8, value []byte) wiretrace.Trame {
	t.Helper()
	m := &codec.Message{
		MTID:     mtid,
		MType:    codec.MTypeReply,
		MAddr:    slot,
		MCode:    byte(codec.MethodGetObject),
		ObjGroup: group,
		ObjID:    id,
		Value:    value,
	}
	raw, err := m.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return wiretrace.Trame{
		Direction: wiretrace.DirectionRx,
		Hex:       hex.EncodeToString(raw),
	}
}

func newPluginForOutTree(t *testing.T) *Plugin {
	t.Helper()
	f := &Factory{}
	plug := f.New(slog.Default()).(*Plugin)
	return plug
}

func TestValidateOutTree_WritesSnapshot(t *testing.T) {
	p := newPluginForOutTree(t)

	// Identity[0]: String "DEMO".
	idValue := []byte{
		0x05, 0x06, 0x01,
		'D', 'E', 'M', 'O', 0x00,
		0x10,
		'C', 'a', 'r', 'd', 0x00,
	}
	// Control[0]: Float gain -6.0.
	ctrlValue := []byte{
		0x03, 0x0A, 0x03,
		0xC0, 0xC0, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x3F, 0x80, 0x00, 0x00,
		0xC2, 0xA0, 0x00, 0x00,
		0x41, 0xA0, 0x00, 0x00,
		'G', 'a', 'i', 'n', 0x00,
		'd', 'B', 0x00,
	}

	// Need one matching request per reply for MTID invariant; provide
	// a request before each reply.
	requestFor := func(mtid uint32, slot uint8, group codec.ObjGroup, id uint8) wiretrace.Trame {
		req := &codec.Message{
			MTID: mtid, MType: codec.MTypeRequest, MAddr: slot,
			MCode: byte(codec.MethodGetObject), ObjGroup: group, ObjID: id,
		}
		raw, _ := req.Encode()
		return wiretrace.Trame{Direction: wiretrace.DirectionTx, Hex: hex.EncodeToString(raw)}
	}

	trames := []wiretrace.Trame{
		requestFor(1, 1, codec.GroupIdentity, 0),
		buildGetObjectTrame(t, 1, 1, codec.GroupIdentity, 0, idValue),
		requestFor(2, 1, codec.GroupControl, 0),
		buildGetObjectTrame(t, 2, 1, codec.GroupControl, 0, ctrlValue),
	}

	out := filepath.Join(t.TempDir(), "tree.json")
	report, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{
		OutTree: out,
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if report.TramesProcessed != 4 {
		t.Fatalf("trames processed = %d, want 4", report.TramesProcessed)
	}

	// Read back the snapshot and verify shape.
	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("open out-tree: %v", err)
	}
	defer func() { _ = f.Close() }()
	snap, err := export.ReadJSON(f)
	if err != nil {
		t.Fatalf("ReadJSON: %v", err)
	}
	if snap.Device.Protocol != "acp1" {
		t.Fatalf("protocol = %q, want acp1", snap.Device.Protocol)
	}
	if len(snap.Slots) != 1 {
		t.Fatalf("slots = %d, want 1", len(snap.Slots))
	}
	if len(snap.Slots[0].Objects) != 2 {
		t.Fatalf("objects = %d, want 2", len(snap.Slots[0].Objects))
	}
	// Order by group (alphabetical) then ID per the writer's
	// deterministic sort: control < identity.
	got := snap.Slots[0].Objects
	if got[0].Group != "control" || got[0].Label != "Gain" {
		t.Errorf("control object: %+v", got[0])
	}
	if got[1].Group != "identity" || got[1].Label != "Card" {
		t.Errorf("identity object: %+v", got[1])
	}
}

func TestValidateOutTree_LatestReplyWins(t *testing.T) {
	p := newPluginForOutTree(t)

	idV1 := []byte{
		0x05, 0x06, 0x01,
		'V', '1', 0x00,
		0x10,
		'C', 'a', 'r', 'd', 0x00,
	}
	idV2 := []byte{
		0x05, 0x06, 0x01,
		'V', '2', 0x00,
		0x10,
		'C', 'a', 'r', 'd', 0x00,
	}
	requestFor := func(mtid uint32, slot uint8, group codec.ObjGroup, id uint8) wiretrace.Trame {
		req := &codec.Message{
			MTID: mtid, MType: codec.MTypeRequest, MAddr: slot,
			MCode: byte(codec.MethodGetObject), ObjGroup: group, ObjID: id,
		}
		raw, _ := req.Encode()
		return wiretrace.Trame{Direction: wiretrace.DirectionTx, Hex: hex.EncodeToString(raw)}
	}
	trames := []wiretrace.Trame{
		requestFor(1, 1, codec.GroupIdentity, 0),
		buildGetObjectTrame(t, 1, 1, codec.GroupIdentity, 0, idV1),
		requestFor(2, 1, codec.GroupIdentity, 0),
		buildGetObjectTrame(t, 2, 1, codec.GroupIdentity, 0, idV2),
	}
	out := filepath.Join(t.TempDir(), "tree.json")
	if _, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{OutTree: out}); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = f.Close() }()
	snap, err := export.ReadJSON(f)
	if err != nil {
		t.Fatalf("ReadJSON: %v", err)
	}
	if got := snap.Slots[0].Objects[0].Value.Str; got != "V2" {
		t.Fatalf("latest-reply-wins broken: got %q, want V2", got)
	}
}

func TestValidateOutTree_NoGetObjectInputProducesEmptySnapshot(t *testing.T) {
	p := newPluginForOutTree(t)

	// Only getValue replies -- out-tree path should still work and emit
	// an empty Snapshot.
	req := &codec.Message{
		MTID: 1, MType: codec.MTypeRequest, MAddr: 1,
		MCode: byte(codec.MethodGetValue), ObjGroup: codec.GroupIdentity, ObjID: 0,
	}
	rep := &codec.Message{
		MTID: 1, MType: codec.MTypeReply, MAddr: 1,
		MCode: byte(codec.MethodGetValue), ObjGroup: codec.GroupIdentity, ObjID: 0,
		Value: []byte("DEMO\x00"),
	}
	reqRaw, _ := req.Encode()
	repRaw, _ := rep.Encode()
	trames := []wiretrace.Trame{
		{Direction: wiretrace.DirectionTx, Hex: hex.EncodeToString(reqRaw)},
		{Direction: wiretrace.DirectionRx, Hex: hex.EncodeToString(repRaw)},
	}
	out := filepath.Join(t.TempDir(), "tree.json")
	if _, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{OutTree: out}); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = f.Close() }()
	snap, err := export.ReadJSON(f)
	if err != nil {
		t.Fatalf("ReadJSON: %v", err)
	}
	if len(snap.Slots) != 0 {
		t.Fatalf("expected empty slots for getValue-only capture, got %d", len(snap.Slots))
	}
}

// TestValidateOutParams_WritesCSVandJSON verifies --out-params is now
// implemented: it writes the captured snapshot as a flat params dump,
// choosing the format from the file extension (.csv vs .json).
func TestValidateOutParams_WritesCSVandJSON(t *testing.T) {
	p := newPluginForOutTree(t)

	// Identity[0]: String "DEMO" with label "Card".
	idValue := []byte{
		0x05, 0x06, 0x01,
		'D', 'E', 'M', 'O', 0x00,
		0x10,
		'C', 'a', 'r', 'd', 0x00,
	}
	requestFor := func(mtid uint32, slot uint8, group codec.ObjGroup, id uint8) wiretrace.Trame {
		req := &codec.Message{
			MTID: mtid, MType: codec.MTypeRequest, MAddr: slot,
			MCode: byte(codec.MethodGetObject), ObjGroup: group, ObjID: id,
		}
		raw, _ := req.Encode()
		return wiretrace.Trame{Direction: wiretrace.DirectionTx, Hex: hex.EncodeToString(raw)}
	}
	trames := []wiretrace.Trame{
		requestFor(1, 1, codec.GroupIdentity, 0),
		buildGetObjectTrame(t, 1, 1, codec.GroupIdentity, 0, idValue),
	}
	dir := t.TempDir()

	// CSV by extension.
	csvOut := filepath.Join(dir, "params.csv")
	if _, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{OutParams: csvOut}); err != nil {
		t.Fatalf("Validate --out-params csv: %v", err)
	}
	csvData, err := os.ReadFile(csvOut)
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	if !strings.Contains(string(csvData), "Card") {
		t.Errorf("csv params dump missing captured label 'Card'; got:\n%s", csvData)
	}

	// JSON by extension (default).
	jsonOut := filepath.Join(dir, "params.json")
	if _, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{OutParams: jsonOut}); err != nil {
		t.Fatalf("Validate --out-params json: %v", err)
	}
	f, err := os.Open(jsonOut)
	if err != nil {
		t.Fatalf("open json: %v", err)
	}
	defer func() { _ = f.Close() }()
	snap, err := export.ReadJSON(f)
	if err != nil {
		t.Fatalf("ReadJSON: %v", err)
	}
	if len(snap.Slots) != 1 || len(snap.Slots[0].Objects) != 1 {
		t.Fatalf("json params dump shape: %d slots", len(snap.Slots))
	}
	if got := snap.Slots[0].Objects[0].Label; got != "Card" {
		t.Errorf("json params label = %q, want Card", got)
	}
}
