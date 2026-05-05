package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"dhs/internal/dmlib"
	"dhs/internal/export"
	"dhs/internal/protocol"
)

// fakePlugin satisfies the subset of protocol.Protocol that the watch
// hot-plug enricher exercises plus protocol.Identifier and seederIface.
type fakePlugin struct {
	identity     protocol.CardIdentity
	identityErr  error
	seedErr      error
	seedCalls    int
	walkCalls    int
	walkErr      error
	walkObjsRet  int
	disconnect   error
	walkSignaled chan struct{}
}

func (f *fakePlugin) Connect(ctx context.Context, ip string, port int) error { return nil }
func (f *fakePlugin) Disconnect() error                                       { return f.disconnect }
func (f *fakePlugin) GetDeviceInfo(ctx context.Context) (protocol.DeviceInfo, error) {
	return protocol.DeviceInfo{}, nil
}
func (f *fakePlugin) GetSlotInfo(ctx context.Context, slot int) (protocol.SlotInfo, error) {
	return protocol.SlotInfo{Slot: slot}, nil
}
func (f *fakePlugin) Walk(ctx context.Context, slot int) ([]protocol.Object, error) {
	f.walkCalls++
	if f.walkSignaled != nil {
		close(f.walkSignaled)
		f.walkSignaled = nil
	}
	if f.walkErr != nil {
		return nil, f.walkErr
	}
	objs := make([]protocol.Object, f.walkObjsRet)
	return objs, nil
}
func (f *fakePlugin) GetValue(ctx context.Context, req protocol.ValueRequest) (protocol.Value, error) {
	return protocol.Value{}, nil
}
func (f *fakePlugin) SetValue(ctx context.Context, req protocol.ValueRequest, v protocol.Value) (protocol.Value, error) {
	return v, nil
}
func (f *fakePlugin) Subscribe(req protocol.ValueRequest, fn protocol.EventFunc) error {
	return nil
}
func (f *fakePlugin) Unsubscribe(req protocol.ValueRequest) error { return nil }

// Identifier
func (f *fakePlugin) GetIdentity(ctx context.Context, slot int) (protocol.CardIdentity, error) {
	if f.identityErr != nil {
		return protocol.CardIdentity{}, f.identityErr
	}
	return f.identity, nil
}

// seederIface (lives in cmd_watch_hotplug.go)
func (f *fakePlugin) SeedFromDM(slot int, snap *export.Snapshot) error {
	f.seedCalls++
	return f.seedErr
}

// fakeResolver hands out a canned schema or ErrNotFound.
type fakeResolver struct {
	schema *dmlib.Schema
	err    error
}

func (r *fakeResolver) Resolve(fp dmlib.Fingerprint) (*dmlib.Schema, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.schema, nil
}
func (r *fakeResolver) LookupAlternate(fp dmlib.Fingerprint) ([]dmlib.Fingerprint, error) {
	return nil, nil
}
func (r *fakeResolver) Persist(s *dmlib.Schema) error { return nil }
func (r *fakeResolver) Diff(prev, cur *dmlib.Schema) dmlib.Diff {
	return dmlib.Diff{}
}

// makeSchema builds a minimal Schema with one slot dump containing N objects.
func makeSchema(slot, nObjs int) *dmlib.Schema {
	objs := make([]protocol.Object, nObjs)
	return &dmlib.Schema{
		Slots: map[int]*export.Snapshot{
			slot: {
				Slots: []export.SlotDump{{Slot: slot, Objects: objs}},
			},
		},
	}
}

// firstFrameStatus seeds prev[]; subsequent calls drive transitions.
func TestHotPlugEnricher_FirstSightDoesNothing(t *testing.T) {
	var buf bytes.Buffer
	enr := newHotPlugEnricher(nil, false, &buf)
	plug := &fakePlugin{}

	transitioned := enr.observe(context.Background(), plug, time.Now(),
		[]protocol.SlotStatus{protocol.SlotPresent, protocol.SlotNoCard})
	if len(transitioned) != 0 {
		t.Fatalf("first sight returned transitioned=%v, want []", transitioned)
	}
	if buf.Len() != 0 {
		t.Fatalf("first sight emitted output: %q", buf.String())
	}
}

func TestHotPlugEnricher_BootToPresent_TriggersEnrichment(t *testing.T) {
	var buf bytes.Buffer
	enr := newHotPlugEnricher(
		&fakeResolver{schema: makeSchema(1, 287)},
		false,
		&buf,
	)
	plug := &fakePlugin{
		identity: protocol.CardIdentity{Model: "RRS18", SwRev: "1601"},
	}

	t0 := time.Date(2026, 5, 5, 18, 42, 0, 0, time.UTC)
	enr.observe(context.Background(), plug, t0,
		[]protocol.SlotStatus{protocol.SlotBootMode}) // first sight, prev=boot
	enr.observe(context.Background(), plug, t0,
		[]protocol.SlotStatus{protocol.SlotPresent}) // boot→present transition

	if plug.seedCalls != 1 {
		t.Fatalf("SeedFromDM calls = %d, want 1", plug.seedCalls)
	}
	out := buf.String()
	if !strings.Contains(out, "boot -> present") {
		t.Fatalf("missing transition tag in output: %q", out)
	}
	if !strings.Contains(out, "RRS18@1601") {
		t.Fatalf("missing fingerprint in output: %q", out)
	}
	if !strings.Contains(out, "seeded(287 objs)") {
		t.Fatalf("missing seed tag in output: %q", out)
	}
}

func TestHotPlugEnricher_BootToPresent_NoDMEntry(t *testing.T) {
	var buf bytes.Buffer
	enr := newHotPlugEnricher(
		&fakeResolver{err: dmlib.ErrNotFound},
		false,
		&buf,
	)
	plug := &fakePlugin{
		identity: protocol.CardIdentity{Model: "RRS19", SwRev: "9999"},
	}

	t0 := time.Date(2026, 5, 5, 18, 42, 0, 0, time.UTC)
	enr.observe(context.Background(), plug, t0,
		[]protocol.SlotStatus{protocol.SlotBootMode})
	enr.observe(context.Background(), plug, t0,
		[]protocol.SlotStatus{protocol.SlotPresent})

	if plug.seedCalls != 0 {
		t.Fatalf("SeedFromDM should not be called on DM miss; got %d", plug.seedCalls)
	}
	out := buf.String()
	if !strings.Contains(out, "no-DM-entry") {
		t.Fatalf("missing no-DM-entry tag in output: %q", out)
	}
}

func TestHotPlugEnricher_IdentityUnresolved(t *testing.T) {
	var buf bytes.Buffer
	enr := newHotPlugEnricher(&fakeResolver{schema: makeSchema(1, 1)}, false, &buf)
	plug := &fakePlugin{
		identityErr: protocol.ErrIdentityUnresolved,
	}

	t0 := time.Now()
	enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotBootMode})
	enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotPresent})

	if plug.seedCalls != 0 {
		t.Fatalf("SeedFromDM should not run on identity-unresolved; got %d", plug.seedCalls)
	}
	out := buf.String()
	if !strings.Contains(out, "id-unresolved") {
		t.Fatalf("missing id-unresolved tag: %q", out)
	}
}

func TestHotPlugEnricher_AutoWalkOnPlug(t *testing.T) {
	var buf bytes.Buffer
	enr := newHotPlugEnricher(
		&fakeResolver{schema: makeSchema(1, 5)},
		true,
		&buf,
	)
	walkSignal := make(chan struct{})
	plug := &fakePlugin{
		identity:     protocol.CardIdentity{Model: "RRS18", SwRev: "1601"},
		walkObjsRet:  5,
		walkSignaled: walkSignal,
	}

	t0 := time.Now()
	enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotBootMode})
	enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotPresent})

	select {
	case <-walkSignal:
		// good — walk goroutine fired
	case <-time.After(time.Second):
		t.Fatal("Walk goroutine never fired with --auto-walk-on-plug")
	}
	if plug.walkCalls != 1 {
		t.Fatalf("Walk calls = %d, want 1", plug.walkCalls)
	}
}

func TestHotPlugEnricher_HotRemove_NoReseed(t *testing.T) {
	var buf bytes.Buffer
	enr := newHotPlugEnricher(&fakeResolver{schema: makeSchema(1, 1)}, false, &buf)
	plug := &fakePlugin{identity: protocol.CardIdentity{Model: "RRS18", SwRev: "1601"}}

	t0 := time.Now()
	enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotPresent})
	enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotRemoved})
	enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotNoCard})

	if plug.seedCalls != 0 {
		t.Fatalf("SeedFromDM called %d times during hot-remove cascade; want 0", plug.seedCalls)
	}
	out := buf.String()
	if !strings.Contains(out, "present -> removed") || !strings.Contains(out, "removed -> no_card") {
		t.Fatalf("missing transitions in output: %q", out)
	}
}

func TestHotPlugEnricher_StableState_NoOutput(t *testing.T) {
	var buf bytes.Buffer
	enr := newHotPlugEnricher(nil, false, &buf)
	plug := &fakePlugin{}
	t0 := time.Now()

	enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotPresent})
	buf.Reset() // ignore first-sight (no output anyway)
	enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotPresent})

	if buf.Len() != 0 {
		t.Fatalf("stable state emitted output: %q", buf.String())
	}
}

// Compile-time guard: *fakePlugin must satisfy the interfaces the
// enricher type-asserts. Drift here breaks live behaviour silently.
var (
	_ protocol.Protocol   = (*fakePlugin)(nil)
	_ protocol.Identifier = (*fakePlugin)(nil)
	_ seederIface         = (*fakePlugin)(nil)
)

func TestHotPlugEnricher_NilResolver_NoSeed(t *testing.T) {
	var buf bytes.Buffer
	enr := newHotPlugEnricher(nil, false, &buf)
	plug := &fakePlugin{identity: protocol.CardIdentity{Model: "X", SwRev: "1"}}

	t0 := time.Now()
	enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotBootMode})
	enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotPresent})

	if plug.seedCalls != 0 {
		t.Fatalf("SeedFromDM called with nil resolver: %d", plug.seedCalls)
	}
	if !strings.Contains(buf.String(), "no-resolver") {
		t.Fatalf("missing no-resolver tag: %q", buf.String())
	}
}

// Silence unused-import warnings if test build is partial.
var _ = errors.New
var _ = slog.LevelInfo
