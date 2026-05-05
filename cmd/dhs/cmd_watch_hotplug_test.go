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

// firstFrameStatus seeds prev[]. Slots already present at first sight
// are enriched immediately (controller booted before the watch started;
// no boot→present transition will arrive). Slots in any non-present
// state at first sight are recorded silently.
func TestHotPlugEnricher_FirstSight_NonPresentSlotsRecordedSilently(t *testing.T) {
	var buf bytes.Buffer
	enr := newHotPlugEnricher(nil, false, &buf)
	plug := &fakePlugin{}

	// First-sight: slot 0 = no_card → no action.
	transitioned := enr.observe(context.Background(), plug, time.Now(),
		[]protocol.SlotStatus{protocol.SlotNoCard})
	if len(transitioned) != 0 {
		t.Fatalf("first sight no_card returned transitioned=%v, want []", transitioned)
	}
	if buf.Len() != 0 {
		t.Fatalf("first sight no_card emitted output: %q", buf.String())
	}
}

func TestHotPlugEnricher_FirstSight_PresentSlotEnriches(t *testing.T) {
	// When the watch verb starts AFTER the controller has booted, the
	// first frame-status announce shows slots already present.  We
	// must enrich them on first sight — there will be no boot→present
	// transition arriving later.
	var buf bytes.Buffer
	enr := newHotPlugEnricher(nil, false, &buf)
	plug := &fakePlugin{
		identity: protocol.CardIdentity{Model: "RRS18", SwRev: "1601"},
	}

	transitioned := enr.observe(context.Background(), plug, time.Now(),
		[]protocol.SlotStatus{protocol.SlotPresent})
	if len(transitioned) != 1 || transitioned[0] != 0 {
		t.Fatalf("first sight present returned transitioned=%v, want [0]", transitioned)
	}
	out := buf.String()
	if !strings.Contains(out, "no_card -> present") {
		t.Fatalf("missing synthesised transition row: %q", out)
	}
	if !strings.Contains(out, "RRS18@1601") {
		t.Fatalf("missing identity probe result: %q", out)
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
	// First-sight-present triggers enrichment (one SeedFromDM call).
	// The remove cascade after it must NOT add further seed calls —
	// that's what this test pins.
	var buf bytes.Buffer
	enr := newHotPlugEnricher(&fakeResolver{schema: makeSchema(1, 1)}, false, &buf)
	plug := &fakePlugin{identity: protocol.CardIdentity{Model: "RRS18", SwRev: "1601"}}

	t0 := time.Now()
	enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotPresent})
	seedAfterFirstSight := plug.seedCalls
	if seedAfterFirstSight != 1 {
		t.Fatalf("first-sight present should enrich once; seedCalls = %d", seedAfterFirstSight)
	}

	enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotRemoved})
	enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotNoCard})

	if plug.seedCalls != seedAfterFirstSight {
		t.Fatalf("hot-remove cascade should not re-seed; seedCalls = %d, want %d",
			plug.seedCalls, seedAfterFirstSight)
	}
	out := buf.String()
	if !strings.Contains(out, "present -> removed") || !strings.Contains(out, "removed -> no_card") {
		t.Fatalf("missing transitions in output: %q", out)
	}
}

func TestClassifyFingerprintShift(t *testing.T) {
	cases := []struct {
		name string
		prev protocol.CardIdentity
		cur  protocol.CardIdentity
		want string
	}{
		{"first-sight", protocol.CardIdentity{}, protocol.CardIdentity{Model: "RRS18", SwRev: "1601"}, "discovered"},
		{"re-confirmed-same", protocol.CardIdentity{Model: "RRS18", SwRev: "1601"}, protocol.CardIdentity{Model: "RRS18", SwRev: "1601"}, "re-confirmed"},
		{"fw-upgrade", protocol.CardIdentity{Model: "RRS18", SwRev: "1601"}, protocol.CardIdentity{Model: "RRS18", SwRev: "1602"}, "fw-upgrade RRS18 1601 →"},
		{"fw-downgrade", protocol.CardIdentity{Model: "RRS18", SwRev: "1602"}, protocol.CardIdentity{Model: "RRS18", SwRev: "1601"}, "fw-downgrade RRS18 1602 →"},
		{"card-swap", protocol.CardIdentity{Model: "RRS18", SwRev: "1601"}, protocol.CardIdentity{Model: "GJA840", SwRev: "0101"}, "card-swap RRS18@1601 →"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyFingerprintShift(tc.prev, tc.cur, nil)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHotPlugEnricher_FingerprintShifts_LiveDiagnostic(t *testing.T) {
	// Drive a real card-swap sequence and confirm the diagnostic row
	// classifies it correctly.
	var buf bytes.Buffer
	enr := newHotPlugEnricher(&fakeResolver{schema: makeSchema(0, 1)}, false, &buf)
	plug := &fakePlugin{
		identity: protocol.CardIdentity{Model: "RRS18", SwRev: "1601"},
	}

	// 1) Initial discovery.
	t0 := time.Now()
	enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotPresent})
	if !strings.Contains(buf.String(), "discovered RRS18@1601") {
		t.Fatalf("missing 'discovered' for initial; output: %q", buf.String())
	}
	buf.Reset()

	// 2) Same card re-confirmed: cycle through boot then back to present.
	enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotBootMode})
	enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotPresent})
	if !strings.Contains(buf.String(), "re-confirmed RRS18@1601") {
		t.Fatalf("missing 're-confirmed'; output: %q", buf.String())
	}
	buf.Reset()

	// 3) Firmware upgrade: same model, higher sw_rev.
	plug.identity = protocol.CardIdentity{Model: "RRS18", SwRev: "1602"}
	enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotBootMode})
	enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotPresent})
	if !strings.Contains(buf.String(), "fw-upgrade RRS18 1601") {
		t.Fatalf("missing 'fw-upgrade'; output: %q", buf.String())
	}
	buf.Reset()

	// 4) Firmware downgrade.
	plug.identity = protocol.CardIdentity{Model: "RRS18", SwRev: "1500"}
	enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotBootMode})
	enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotPresent})
	if !strings.Contains(buf.String(), "fw-downgrade RRS18 1602") {
		t.Fatalf("missing 'fw-downgrade'; output: %q", buf.String())
	}
	buf.Reset()

	// 5) Card swap to a different model.
	plug.identity = protocol.CardIdentity{Model: "GJA840", SwRev: "0101"}
	enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotBootMode})
	enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotPresent})
	if !strings.Contains(buf.String(), "card-swap RRS18@1500") {
		t.Fatalf("missing 'card-swap'; output: %q", buf.String())
	}
}

func TestHotPlugEnricher_NoCard_DropsCachedFingerprint(t *testing.T) {
	// After the slot goes empty (no_card), the next time it hits
	// present we should classify as 'discovered' even if the same
	// physical card is re-inserted — we cannot assume continuity.
	var buf bytes.Buffer
	enr := newHotPlugEnricher(&fakeResolver{schema: makeSchema(0, 1)}, false, &buf)
	plug := &fakePlugin{identity: protocol.CardIdentity{Model: "RRS18", SwRev: "1601"}}

	t0 := time.Now()
	enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotPresent})
	enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotRemoved})
	enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotNoCard})
	buf.Reset()
	enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotPresent})
	if strings.Contains(buf.String(), "re-confirmed") {
		t.Fatalf("re-plug after no_card should NOT be re-confirmed; output: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "discovered") {
		t.Fatalf("re-plug after no_card should classify as 'discovered'; output: %q", buf.String())
	}
}

// TestHotPlugEnricher_AnyPathToPresent_TriggersEnrichment pins the
// widened trigger introduced after the simulator showed shortcut
// transitions: powerup→present and removed→present can both happen
// without going through boot. Real hardware always passes through
// boot (so the canonical path stays exercised), but the simulator and
// post-controller-boot first-sight cases must enrich too.
func TestHotPlugEnricher_AnyPathToPresent_TriggersEnrichment(t *testing.T) {
	cases := []struct {
		name string
		prev protocol.SlotStatus
	}{
		{"boot-to-present", protocol.SlotBootMode},
		{"powerup-to-present", protocol.SlotPowerUp},
		{"removed-to-present", protocol.SlotRemoved},
		{"error-to-present", protocol.SlotError},
		{"no_card-to-present", protocol.SlotNoCard},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			enr := newHotPlugEnricher(&fakeResolver{schema: makeSchema(0, 1)}, false, &buf)
			plug := &fakePlugin{identity: protocol.CardIdentity{Model: "RRS18", SwRev: "1601"}}

			t0 := time.Now()
			enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{tc.prev})
			before := plug.seedCalls
			enr.observe(context.Background(), plug, t0, []protocol.SlotStatus{protocol.SlotPresent})
			if plug.seedCalls <= before {
				t.Fatalf("%s did not trigger enrichment; seedCalls before=%d after=%d",
					tc.name, before, plug.seedCalls)
			}
			if !strings.Contains(buf.String(), "RRS18@1601") {
				t.Fatalf("%s missing fingerprint in output: %q", tc.name, buf.String())
			}
		})
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
