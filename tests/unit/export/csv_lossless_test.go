// CSV lossless round-trip tests (issue #38).
//
// Contract: every writable object exported to CSV must round-trip back
// through ReadCSV + Apply(dryRun=true) with zero Failed rows and every
// writable object marked Applied. Read-only objects must be counted in
// Skipped with reason "read_only".
//
// No device required — pure in-memory flow through WriteCSV → ReadCSV
// → Apply. Covers ACP1, ACP2, Ember+ in one test table.
package export_test

import (
	"bytes"
	"context"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"acp/internal/export"
	"acp/internal/protocol"
)

// dryRunMock is the minimal plugin stub Apply needs to reach its
// dry-run fast path. Walk returns nil (no error, empty tree); SetValue
// is never called because dryRun==true bypasses it. The importer uses
// the snapshot's own object list — the plugin tree is only needed for
// real writes.
type dryRunMock struct{}

func (dryRunMock) Connect(context.Context, string, int) error { return nil }
func (dryRunMock) Disconnect() error                          { return nil }
func (dryRunMock) GetDeviceInfo(context.Context) (protocol.DeviceInfo, error) {
	return protocol.DeviceInfo{}, nil
}
func (dryRunMock) GetSlotInfo(context.Context, int) (protocol.SlotInfo, error) {
	return protocol.SlotInfo{}, nil
}
func (dryRunMock) Walk(context.Context, int) ([]protocol.Object, error) { return nil, nil }
func (dryRunMock) GetValue(context.Context, protocol.ValueRequest) (protocol.Value, error) {
	return protocol.Value{}, nil
}
func (dryRunMock) SetValue(context.Context, protocol.ValueRequest, protocol.Value) (protocol.Value, error) {
	return protocol.Value{}, nil
}
func (dryRunMock) Subscribe(protocol.ValueRequest, protocol.EventFunc) error { return nil }
func (dryRunMock) Unsubscribe(protocol.ValueRequest) error                   { return nil }

var _ protocol.Protocol = dryRunMock{}
var _ = slog.Default // keep import available for future debug

// -----------------------------------------------------------------------
// Sample snapshots — one per protocol, minimal but covering the tricky
// bits each plugin brings to the table.
// -----------------------------------------------------------------------

func acp1Snapshot() *export.Snapshot {
	return &export.Snapshot{
		Device: export.DeviceInfo{
			IP: "10.6.239.113", Port: 2071, Protocol: "acp1", NumSlots: 1,
		},
		CreatedAt: time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC),
		Slots: []export.SlotDump{{
			Slot:     1,
			WalkedAt: time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC),
			Objects: []protocol.Object{
				{ // writable float
					Slot: 1, Group: "control", Path: []string{"control"},
					ID: 7, Label: "GainA", Kind: protocol.KindFloat,
					Access: 3, Unit: "%",
					Min: float64(0), Max: float64(150), Step: float64(1), Def: float64(100),
					Value: protocol.Value{Kind: protocol.KindFloat, Float: 50.8},
				},
				{ // writable enum
					Slot: 1, Group: "control", Path: []string{"control"},
					ID: 4, Label: "Broadcasts", Kind: protocol.KindEnum,
					Access: 3, EnumItems: []string{"Off", "On"},
					Value: protocol.Value{Kind: protocol.KindEnum, Enum: 1, Str: "On"},
				},
				{ // read-only identity string — must land in Skipped
					Slot: 1, Group: "identity", Path: []string{"identity"},
					ID: 0, Label: "Card name", Kind: protocol.KindString,
					Access: 1, MaxLen: 16,
					Value: protocol.Value{Kind: protocol.KindString, Str: "CDV08v06"},
				},
			},
		}},
	}
}

func acp2Snapshot() *export.Snapshot {
	// Deliberate duplicate "Present" label under PSU.1 and PSU.2 — this
	// is the case CSV round-trip must survive via the numeric `id`
	// column (ACP2 obj-ids are globally unique u32).
	return &export.Snapshot{
		Device: export.DeviceInfo{
			IP: "10.41.40.195", Port: 2072, Protocol: "acp2", NumSlots: 1,
		},
		CreatedAt: time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC),
		Slots: []export.SlotDump{{
			Slot:     0,
			WalkedAt: time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC),
			Objects: []protocol.Object{
				{ // writable hierarchical enum
					Slot: 0, Path: []string{"BOARD", "ACP Trace"},
					ID: 47431, Label: "ACP Trace", Kind: protocol.KindEnum,
					Access: 3, EnumItems: []string{"Off", "On"},
					Value: protocol.Value{Kind: protocol.KindEnum, Enum: 0, Str: "Off"},
				},
				{ // duplicate label #1 — different sub-node
					Slot: 0, Path: []string{"STATUS", "PSU", "1", "Present"},
					ID: 60001, Label: "Present", Kind: protocol.KindEnum,
					Access: 1, EnumItems: []string{"no", "yes"},
					Value: protocol.Value{Kind: protocol.KindEnum, Enum: 1, Str: "yes"},
				},
				{ // duplicate label #2 — same label, different ID
					Slot: 0, Path: []string{"STATUS", "PSU", "2", "Present"},
					ID: 60002, Label: "Present", Kind: protocol.KindEnum,
					Access: 1, EnumItems: []string{"no", "yes"},
					Value: protocol.Value{Kind: protocol.KindEnum, Enum: 0, Str: "no"},
				},
				{ // writable string with max length
					Slot: 0, Path: []string{"IDENTITY", "User Label 1"},
					ID: 12345, Label: "User Label 1", Kind: protocol.KindString,
					Access: 3, MaxLen: 17,
					Value: protocol.Value{Kind: protocol.KindString, Str: "Studio A"},
				},
			},
		}},
	}
}

func emberplusSnapshot() *export.Snapshot {
	// Ember+ objects carry OID as the authoritative identifier; labels
	// ("gain") collide freely across channels.
	return &export.Snapshot{
		Device: export.DeviceInfo{
			IP: "127.0.0.1", Port: 9092, Protocol: "emberplus", NumSlots: 1,
		},
		CreatedAt: time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC),
		Slots: []export.SlotDump{{
			Slot:     0,
			WalkedAt: time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC),
			Objects: []protocol.Object{
				{ // writable float — channel 1 gain
					Slot: 0, OID: "1.2.1.3",
					Path: []string{"router", "inputs", "ch1", "gain"},
					ID:   3, Label: "gain", Kind: protocol.KindFloat,
					Access: 3, Unit: "dB",
					Value: protocol.Value{Kind: protocol.KindFloat, Float: -6.0},
				},
				{ // duplicate label — channel 2 gain, disambiguated by OID
					Slot: 0, OID: "1.2.2.3",
					Path: []string{"router", "inputs", "ch2", "gain"},
					ID:   3, Label: "gain", Kind: protocol.KindFloat,
					Access: 3, Unit: "dB",
					Value: protocol.Value{Kind: protocol.KindFloat, Float: -12.0},
				},
				{ // read-only integer — must land in Skipped
					Slot: 0, OID: "1.1",
					Path: []string{"identity", "version"},
					ID:   1, Label: "version", Kind: protocol.KindInt,
					Access: 1,
					Value:  protocol.Value{Kind: protocol.KindInt, Int: 230},
				},
			},
		}},
	}
}

// -----------------------------------------------------------------------
// Tests.
// -----------------------------------------------------------------------

// TestCSV_PreservesOIDAndPath — the writable-object fields that the
// importer's per-protocol resolver depends on must survive a
// WriteCSV → ReadCSV round-trip untouched. Without oid + path in the
// header, Ember+ duplicate-label objects collapse into one row and
// ACP2 hierarchical paths lose their tree location.
func TestCSV_PreservesOIDAndPath(t *testing.T) {
	cases := []struct {
		name string
		snap *export.Snapshot
	}{
		{"acp1", acp1Snapshot()},
		{"acp2", acp2Snapshot()},
		{"emberplus", emberplusSnapshot()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := export.WriteCSV(&buf, c.snap); err != nil {
				t.Fatalf("WriteCSV: %v", err)
			}
			got, err := export.ReadCSV(&buf)
			if err != nil {
				t.Fatalf("ReadCSV: %v", err)
			}
			// Flatten both sides for pair-wise comparison.
			var want, have []protocol.Object
			for _, s := range c.snap.Slots {
				want = append(want, s.Objects...)
			}
			for _, s := range got.Slots {
				have = append(have, s.Objects...)
			}
			if len(want) != len(have) {
				t.Fatalf("object count: got %d, want %d", len(have), len(want))
			}
			// Match by (OID, ID, Label) — survives ordering differences.
			for _, w := range want {
				found := false
				for _, h := range have {
					if h.OID == w.OID && h.ID == w.ID && h.Label == w.Label {
						if !reflect.DeepEqual(h.Path, w.Path) {
							t.Errorf("%s id=%d label=%q: path got %v, want %v",
								c.name, w.ID, w.Label, h.Path, w.Path)
						}
						if h.Kind != w.Kind {
							t.Errorf("%s id=%d: kind got %v, want %v",
								c.name, w.ID, h.Kind, w.Kind)
						}
						if h.Access != w.Access {
							t.Errorf("%s id=%d: access got %d, want %d",
								c.name, w.ID, h.Access, w.Access)
						}
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%s: object (oid=%q id=%d label=%q) missing from round-trip",
						c.name, w.OID, w.ID, w.Label)
				}
			}
		})
	}
}

// TestCSV_DryRunZero — the full contract: snapshot → CSV → read-back →
// Apply(dryRun=true) must classify every writable object as Applied
// and every read-only object as Skipped("read_only") with zero
// Failures, across all three protocols. Any Failed row means the CSV
// carried insufficient info for the importer to address the object.
func TestCSV_DryRunZero(t *testing.T) {
	cases := []struct {
		name string
		snap *export.Snapshot
	}{
		{"acp1", acp1Snapshot()},
		{"acp2", acp2Snapshot()},
		{"emberplus", emberplusSnapshot()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Count writable vs read-only in the source.
			var wantApplied, wantSkipped int
			for _, s := range c.snap.Slots {
				for _, o := range s.Objects {
					if o.HasWrite() {
						wantApplied++
					} else {
						wantSkipped++
					}
				}
			}

			var buf bytes.Buffer
			if err := export.WriteCSV(&buf, c.snap); err != nil {
				t.Fatalf("WriteCSV: %v", err)
			}
			got, err := export.ReadCSV(&buf)
			if err != nil {
				t.Fatalf("ReadCSV: %v", err)
			}
			// ReadCSV strips DeviceInfo.Protocol on older CSVs but ours
			// carries it in every row — verify it survived so the
			// importer's per-protocol switch sees the right value.
			if got.Device.Protocol != c.snap.Device.Protocol {
				t.Fatalf("protocol lost in CSV: got %q, want %q",
					got.Device.Protocol, c.snap.Device.Protocol)
			}

			rep, err := export.Apply(context.Background(), dryRunMock{}, got, true)
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if rep.Failed != 0 {
				t.Errorf("Failed got %d, want 0. Failures: %v",
					rep.Failed, rep.Failures)
			}
			if rep.Applied != wantApplied {
				t.Errorf("Applied got %d, want %d", rep.Applied, wantApplied)
			}
			if rep.Skipped != wantSkipped {
				t.Errorf("Skipped got %d, want %d. Skips: %v",
					rep.Skipped, wantSkipped, rep.Skips)
			}
			// Every skip must carry reason "read_only" — if anything
			// else shows up it means the round-trip dropped a Kind
			// classification and the importer fell through to the
			// unknown_kind branch.
			for _, sk := range rep.Skips {
				if sk.Reason != "read_only" {
					t.Errorf("skip reason got %q, want %q (slot=%d id=%d label=%q)",
						sk.Reason, "read_only", sk.Slot, sk.ID, sk.Label)
				}
			}
		})
	}
}
