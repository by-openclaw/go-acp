package export

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/consumer"
)

// scriptedPlugin records every SetValue request and fails Walk or
// SetValue on demand — the device side of the importer contract.
type scriptedPlugin struct {
	walkErr   error
	setErr    error
	walkCalls int
	reqs      []consumer.ValueRequest
}

func (p *scriptedPlugin) Connect(context.Context, string, int) error { return nil }
func (p *scriptedPlugin) Disconnect() error                          { return nil }
func (p *scriptedPlugin) GetDeviceInfo(context.Context) (consumer.DeviceInfo, error) {
	return consumer.DeviceInfo{}, nil
}
func (p *scriptedPlugin) GetSlotInfo(context.Context, int) (consumer.SlotInfo, error) {
	return consumer.SlotInfo{}, nil
}
func (p *scriptedPlugin) Walk(context.Context, int) ([]consumer.Object, error) {
	p.walkCalls++
	return nil, p.walkErr
}
func (p *scriptedPlugin) GetValue(context.Context, consumer.ValueRequest) (consumer.Value, error) {
	return consumer.Value{}, nil
}
func (p *scriptedPlugin) SetValue(_ context.Context, req consumer.ValueRequest, _ consumer.Value) (consumer.Value, error) {
	p.reqs = append(p.reqs, req)
	return consumer.Value{}, p.setErr
}
func (p *scriptedPlugin) Subscribe(consumer.ValueRequest, consumer.EventFunc) error { return nil }
func (p *scriptedPlugin) Unsubscribe(consumer.ValueRequest) error                   { return nil }

func oneObjectSnapshot(proto string, obj consumer.Object) *Snapshot {
	return &Snapshot{Device: DeviceInfo{Protocol: proto}, Slots: []SlotDump{{Slot: 1, Objects: []consumer.Object{obj}}}}
}

// TestApply_RequestAddressing — each protocol's resolver gets the key
// it can act on: ACP1 (group+id+label), ACP2 (id), Ember+ (OID, else
// dotted path, else label), unknown protocols everything we have.
func TestApply_RequestAddressing(t *testing.T) {
	writable := func(o consumer.Object) consumer.Object {
		o.Access = 0x03
		o.Kind = consumer.KindInt
		return o
	}
	cases := []struct {
		name  string
		proto string
		obj   consumer.Object
		want  consumer.ValueRequest
	}{
		{"acp1 uses group id label", "acp1",
			writable(consumer.Object{Group: "control", ID: 7, Label: "GainA", Path: []string{"control"}}),
			consumer.ValueRequest{Slot: 1, Group: "control", ID: 7, Label: "GainA"}},
		{"acp1 derives group from path", "acp1",
			writable(consumer.Object{ID: 7, Label: "GainA", Path: []string{"status"}}),
			consumer.ValueRequest{Slot: 1, Group: "status", ID: 7, Label: "GainA"}},
		{"acp2 uses id only", "acp2",
			writable(consumer.Object{Group: "x", ID: 47431, Label: "ACP Trace", Path: []string{"BOARD", "ACP Trace"}}),
			consumer.ValueRequest{Slot: 1, ID: 47431}},
		{"emberplus prefers oid", "emberplus",
			writable(consumer.Object{OID: "1.2.1.3", ID: 3, Label: "gain", Path: []string{"router", "ch1", "gain"}}),
			consumer.ValueRequest{Slot: 1, Path: "1.2.1.3"}},
		{"emberplus falls back to dotted path", "emberplus",
			writable(consumer.Object{ID: 3, Label: "gain", Path: []string{"router", "ch1", "gain"}}),
			consumer.ValueRequest{Slot: 1, Path: "router.ch1.gain"}},
		{"emberplus last resort is label", "emberplus",
			writable(consumer.Object{ID: 3, Label: "gain"}),
			consumer.ValueRequest{Slot: 1, Label: "gain"}},
		{"unknown protocol sends everything with oid as path", "probel",
			writable(consumer.Object{Group: "g", ID: 4, Label: "l", OID: "1.4", Path: []string{"a", "b"}}),
			consumer.ValueRequest{Slot: 1, Group: "g", ID: 4, Label: "l", Path: "1.4"}},
		{"unknown protocol joins path without oid", "probel",
			writable(consumer.Object{Group: "g", ID: 4, Label: "l", Path: []string{"a", "b"}}),
			consumer.ValueRequest{Slot: 1, Group: "g", ID: 4, Label: "l", Path: "a.b"}},
		{"unknown protocol with neither leaves path empty", "probel",
			writable(consumer.Object{Group: "g", ID: 4, Label: "l"}),
			consumer.ValueRequest{Slot: 1, Group: "g", ID: 4, Label: "l"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := &scriptedPlugin{}
			rep, err := Apply(context.Background(), p, oneObjectSnapshot(c.proto, c.obj), false)
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if rep.Applied != 1 || len(p.reqs) != 1 {
				t.Fatalf("applied=%d reqs=%d, want 1/1 (%+v)", rep.Applied, len(p.reqs), rep)
			}
			if p.reqs[0] != c.want {
				t.Errorf("request got %+v, want %+v", p.reqs[0], c.want)
			}
		})
	}
}

// TestApply_SkipsAndFailures — what the importer refuses at the
// client and how a device rejection is accounted, each with the
// one-word reason the CLI groups by.
func TestApply_SkipsAndFailures(t *testing.T) {
	t.Run("skip reasons", func(t *testing.T) {
		cases := []struct {
			name       string
			obj        consumer.Object
			wantReason string
			wantSkip   SkipRecord
		}{
			{"read-only", consumer.Object{ID: 1, Label: "ro", Kind: consumer.KindInt, Access: 0x01, Path: []string{"a", "ro"}},
				"read_only", SkipRecord{Slot: 1, ID: 1, Label: "ro", Path: "a.ro", Kind: "int", Access: "R--", Reason: "read_only"}},
			{"unknown kind", consumer.Object{ID: 2, Label: "odd", Kind: consumer.KindUnknown, Access: 0x03},
				"unknown_kind", SkipRecord{Slot: 1, ID: 2, Label: "odd", Path: "odd", Kind: "unknown", Access: "RW-", Reason: "unknown_kind"}},
			{"frame is compound", consumer.Object{ID: 3, Label: "frame", Kind: consumer.KindFrame, Access: 0x03},
				"unknown_kind", SkipRecord{Slot: 1, ID: 3, Label: "frame", Path: "frame", Kind: "frame", Access: "RW-", Reason: "unknown_kind"}},
			{"section marker", consumer.Object{ID: 4, Label: "hdr", Kind: consumer.KindString, Access: 0x03, SubGroupMarker: true},
				"marker", SkipRecord{Slot: 1, ID: 4, Label: "hdr", Path: "hdr", Kind: "string", Access: "RW-", Reason: "marker"}},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				p := &scriptedPlugin{}
				rep, err := Apply(context.Background(), p, oneObjectSnapshot("acp2", c.obj), false)
				if err != nil {
					t.Fatalf("Apply: %v", err)
				}
				if len(p.reqs) != 0 || rep.Applied != 0 || rep.Skipped != 1 || len(rep.Skips) != 1 {
					t.Fatalf("report %+v, set calls %d", rep, len(p.reqs))
				}
				if rep.Skips[0] != c.wantSkip {
					t.Errorf("skip got %+v, want %+v", rep.Skips[0], c.wantSkip)
				}
			})
		}
	})
	t.Run("device rejects the write", func(t *testing.T) {
		p := &scriptedPlugin{setErr: errors.New("value out of range")}
		obj := consumer.Object{ID: 7, Label: "GainA", Kind: consumer.KindInt, Access: 0x03}
		rep, err := Apply(context.Background(), p, oneObjectSnapshot("acp2", obj), false)
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if rep.Failed != 1 || rep.Applied != 0 || len(rep.Failures) != 1 || rep.Failures[0] != "slot 1 GainA: value out of range" {
			t.Errorf("report %+v", rep)
		}
	})
}

// TestApply_PreWalk — with the pre-walk switch on, a slot whose walk
// fails is reported once and every row in it counts as failed; a
// successful walk changes nothing about the per-row outcome.
func TestApply_PreWalk(t *testing.T) {
	orig := walkNeeded
	walkNeeded = true
	t.Cleanup(func() { walkNeeded = orig })

	t.Run("walk failure fails the whole slot", func(t *testing.T) {
		p := &scriptedPlugin{walkErr: errors.New("timeout")}
		rep, err := Apply(context.Background(), p, snapshotForTest("acp2"), false)
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if p.walkCalls != 1 || len(p.reqs) != 0 {
			t.Errorf("walk calls %d, set calls %d; want 1/0", p.walkCalls, len(p.reqs))
		}
		if rep.Failed != 3 || len(rep.Failures) != 1 || rep.Failures[0] != "slot 1 walk failed: timeout" {
			t.Errorf("report %+v", rep)
		}
	})
	t.Run("walk success proceeds row by row", func(t *testing.T) {
		p := &scriptedPlugin{}
		rep, err := Apply(context.Background(), p, snapshotForTest("acp2"), false)
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if p.walkCalls != 1 || rep.Applied != 2 || rep.Skipped != 1 || rep.Failed != 0 {
			t.Errorf("walk calls %d, report %+v", p.walkCalls, rep)
		}
	})
}

// TestLoadSnapshot — format is chosen by extension; anything
// unrecognised is read as JSON; a missing file names its path.
func TestLoadSnapshot(t *testing.T) {
	dir := t.TempDir()
	snap := &Snapshot{
		Device: DeviceInfo{IP: "10.6.239.113", Port: 2071, Protocol: "acp1", NumSlots: 1},
		Slots: []SlotDump{{Slot: 1, Objects: []consumer.Object{
			{Group: "control", Path: []string{"control"}, ID: 7, Label: "GainA", Kind: consumer.KindFloat, Access: 3,
				Value: consumer.Value{Kind: consumer.KindFloat, Float: 50.8}},
			{Group: "control", Path: []string{"control"}, ID: 4, Label: "Mode", Kind: consumer.KindEnum, Access: 3,
				EnumItems: []string{"Off", "On"}, Value: consumer.Value{Kind: consumer.KindEnum, Enum: 1, Str: "On"}},
			{Group: "identity", Path: []string{"identity"}, ID: 0, Label: "Card name", Kind: consumer.KindString, Access: 1,
				Value: consumer.Value{Kind: consumer.KindString, Str: "RRS18"}},
		}}},
	}
	write := func(name string, fn func(*os.File) error) string {
		t.Helper()
		path := filepath.Join(dir, name)
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := fn(f); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		return path
	}
	jsonW := func(f *os.File) error { return WriteJSON(f, snap) }
	cases := []struct {
		name string
		path string
		want int // objects read back
	}{
		{"json", write("snap.json", jsonW), 3},
		{"yaml", write("snap.yaml", func(f *os.File) error { return WriteYAML(f, snap) }), 3},
		{"yml", write("snap.YML", func(f *os.File) error { return WriteYAML(f, snap) }), 3},
		{"csv", write("snap.csv", func(f *os.File) error { return WriteCSV(f, snap) }), 3},
		{"unknown extension is json", write("snap.txt", jsonW), 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := LoadSnapshot(c.path)
			if err != nil {
				t.Fatalf("LoadSnapshot: %v", err)
			}
			n := 0
			for _, s := range got.Slots {
				n += len(s.Objects)
			}
			if n != c.want {
				t.Errorf("objects got %d, want %d", n, c.want)
			}
		})
	}
	t.Run("missing file", func(t *testing.T) {
		missing := filepath.Join(dir, "nope.json")
		_, err := LoadSnapshot(missing)
		if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), "open "+missing) {
			t.Errorf("got %v, want open <path>: not exist", err)
		}
	})
}
