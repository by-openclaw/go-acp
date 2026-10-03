package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/export"
	"dhs/internal/plugin"
)

// importFake is the smallest protocol an import can be driven against:
// one slot, one writable object, and a record of every write it got.
type importFake struct {
	consumer.Base
	mu     sync.Mutex
	writes []string
}

type importFakeFactory struct{ p *importFake }

func (f *importFakeFactory) Meta() consumer.ProtocolMeta {
	return consumer.ProtocolMeta{Name: "importfake", DefaultPort: 1, Description: "import dry-run test"}
}
func (f *importFakeFactory) New(plugin.Deps) consumer.Protocol { return f.p }

func (p *importFake) Connect(context.Context, string, int) error { return nil }
func (p *importFake) Disconnect() error                          { return nil }
func (p *importFake) GetDeviceInfo(context.Context) (consumer.DeviceInfo, error) {
	return consumer.DeviceInfo{NumSlots: 1}, nil
}
func (p *importFake) GetSlotInfo(_ context.Context, slot int) (consumer.SlotInfo, error) {
	return consumer.SlotInfo{Slot: slot, Status: consumer.SlotPresent}, nil
}
func (p *importFake) Walk(context.Context, int) ([]consumer.Object, error) {
	return []consumer.Object{p.object()}, nil
}
func (p *importFake) object() consumer.Object {
	return consumer.Object{
		Slot: 0, Group: "control", ID: 7, Path: []string{"ntp", "backup"}, Label: "backup",
		Kind: consumer.KindString, Access: 3,
		Value: consumer.Value{Kind: consumer.KindString, Str: "10.6.224.1"},
	}
}
func (p *importFake) GetValue(context.Context, consumer.ValueRequest) (consumer.Value, error) {
	return p.object().Value, nil
}
func (p *importFake) SetValue(_ context.Context, req consumer.ValueRequest, v consumer.Value) (consumer.Value, error) {
	p.mu.Lock()
	p.writes = append(p.writes, req.Path+"="+v.Str)
	p.mu.Unlock()
	return v, nil
}
func (p *importFake) Subscribe(consumer.ValueRequest, consumer.EventFunc) error { return nil }
func (p *importFake) Unsubscribe(consumer.ValueRequest) error                   { return nil }

func (p *importFake) written() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.writes...)
}

var importFakeOnce sync.Once
var importFakeProto = &importFake{}

func registerImportFake(t *testing.T) *importFake {
	t.Helper()
	importFakeOnce.Do(func() { consumer.Register(&importFakeFactory{p: importFakeProto}) })
	importFakeProto.mu.Lock()
	importFakeProto.writes = nil
	importFakeProto.mu.Unlock()
	return importFakeProto
}

// oneRow writes a snapshot that changes the fake's one object.
func oneRow(t *testing.T) string {
	t.Helper()
	snap := &export.Snapshot{
		Device: export.DeviceInfo{IP: "127.0.0.1", Protocol: "importfake", NumSlots: 1},
		Slots:  []export.SlotDump{{Slot: 0, Objects: []consumer.Object{importFakeProto.object()}}},
	}
	snap.Slots[0].Objects[0].Value.Str = "10.6.240.1"
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "one-row.json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// --check is the canonical dry run (ADR-0007) and what the Ansible plays
// pass. With --file it was accepted and ignored, and the device got
// written (#1203, a Neuron's NTP backup on 2026-10-01).
func TestImportCheckSendsNothing(t *testing.T) {
	p := registerImportFake(t)
	file := oneRow(t)
	for _, flag := range []string{"--check", "--dry-run"} {
		if err := runImport(context.Background(), []string{"--protocol", "importfake", "127.0.0.1", "--file", file, flag}); err != nil {
			t.Fatalf("%s: %v", flag, err)
		}
		if got := p.written(); len(got) != 0 {
			t.Fatalf("%s wrote to the device: %q", flag, got)
		}
	}
	// And without either, the same row is written: the dry run is the
	// only thing standing between the file and the device.
	if err := runImport(context.Background(), []string{"--protocol", "importfake", "127.0.0.1", "--file", file}); err != nil {
		t.Fatal(err)
	}
	if got := p.written(); len(got) != 1 || got[0] != "ntp.backup=10.6.240.1" {
		t.Fatalf("a real import wrote %q", got)
	}
}
