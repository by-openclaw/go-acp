package export

import (
	"context"
	"errors"
	"testing"

	"dhs/internal/consumer"
)

// countingPlugin satisfies consumer.Protocol and counts Walk + SetValue
// calls so tests can assert which device-touching paths Apply took.
// validateErr (if set) is returned by ValidateValue — used by tests
// that exercise the ValueValidator skip path.
type countingPlugin struct {
	walkCalls     int
	setCalls      int
	validateCalls int
	validateErr   error // nil = always valid
}

func (p *countingPlugin) Connect(context.Context, string, int) error { return nil }
func (p *countingPlugin) Disconnect() error                          { return nil }
func (p *countingPlugin) GetDeviceInfo(context.Context) (consumer.DeviceInfo, error) {
	return consumer.DeviceInfo{}, nil
}
func (p *countingPlugin) GetSlotInfo(context.Context, int) (consumer.SlotInfo, error) {
	return consumer.SlotInfo{}, nil
}
func (p *countingPlugin) Walk(context.Context, int) ([]consumer.Object, error) {
	p.walkCalls++
	return nil, nil
}
func (p *countingPlugin) GetValue(context.Context, consumer.ValueRequest) (consumer.Value, error) {
	return consumer.Value{}, nil
}
func (p *countingPlugin) SetValue(context.Context, consumer.ValueRequest, consumer.Value) (consumer.Value, error) {
	p.setCalls++
	return consumer.Value{}, nil
}
func (p *countingPlugin) Subscribe(consumer.ValueRequest, consumer.EventFunc) error { return nil }
func (p *countingPlugin) Unsubscribe(consumer.ValueRequest) error                   { return nil }

// ValidateValue makes countingPlugin satisfy consumer.ValueValidator.
func (p *countingPlugin) ValidateValue(context.Context, consumer.ValueRequest, consumer.Value) error {
	p.validateCalls++
	return p.validateErr
}

// nonValidatorPlugin is a Protocol with no ValidateValue method —
// used to assert the importer's graceful fallback when a plugin
// doesn't implement ValueValidator.
type nonValidatorPlugin struct {
	walkCalls int
	setCalls  int
}

func (p *nonValidatorPlugin) Connect(context.Context, string, int) error { return nil }
func (p *nonValidatorPlugin) Disconnect() error                          { return nil }
func (p *nonValidatorPlugin) GetDeviceInfo(context.Context) (consumer.DeviceInfo, error) {
	return consumer.DeviceInfo{}, nil
}
func (p *nonValidatorPlugin) GetSlotInfo(context.Context, int) (consumer.SlotInfo, error) {
	return consumer.SlotInfo{}, nil
}
func (p *nonValidatorPlugin) Walk(context.Context, int) ([]consumer.Object, error) {
	p.walkCalls++
	return nil, nil
}
func (p *nonValidatorPlugin) GetValue(context.Context, consumer.ValueRequest) (consumer.Value, error) {
	return consumer.Value{}, nil
}
func (p *nonValidatorPlugin) SetValue(context.Context, consumer.ValueRequest, consumer.Value) (consumer.Value, error) {
	p.setCalls++
	return consumer.Value{}, nil
}
func (p *nonValidatorPlugin) Subscribe(consumer.ValueRequest, consumer.EventFunc) error { return nil }
func (p *nonValidatorPlugin) Unsubscribe(consumer.ValueRequest) error                   { return nil }

func snapshotForTest(proto string) *Snapshot {
	return &Snapshot{
		Device: DeviceInfo{Protocol: proto},
		Slots: []SlotDump{
			{
				Slot: 1,
				Objects: []consumer.Object{
					{ID: 67604, Path: []string{"OUTPUT", "IP", "VIDEO", "STREAM 1", "LEG 1", "Destination IP"},
						Kind: consumer.KindString, Access: 0x03,
						Value: consumer.Value{Kind: consumer.KindString, Str: "239.129.1.20"}},
					{ID: 67605, Path: []string{"OUTPUT", "IP", "VIDEO", "STREAM 1", "LEG 1", "Destination Port"},
						Kind: consumer.KindInt, Access: 0x03,
						Value: consumer.Value{Kind: consumer.KindInt, Int: 12700}},
					{ID: 99, Path: []string{"READ_ONLY"}, Kind: consumer.KindString, Access: 0x01},
				},
			},
		},
	}
}

func TestApply_DryRunSkipsWalk(t *testing.T) {
	p := &countingPlugin{}
	rep, err := Apply(context.Background(), p, snapshotForTest("acp2"), true)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if p.walkCalls != 0 {
		t.Errorf("dry-run must not call Walk; got %d Walk call(s)", p.walkCalls)
	}
	if p.setCalls != 0 {
		t.Errorf("dry-run must not call SetValue; got %d SetValue call(s)", p.setCalls)
	}
	if rep.Applied != 2 {
		t.Errorf("dry-run report: applied=%d, want 2 (writable rows)", rep.Applied)
	}
	if rep.Skipped != 1 {
		t.Errorf("dry-run report: skipped=%d, want 1 (read_only row)", rep.Skipped)
	}
	if !rep.DryRun {
		t.Errorf("rep.DryRun must be true")
	}
}

// ACP2 apply skips the walk — the CSV's obj-id is enough; SetValue's
// fetchObjectMeta fallback supplies type metadata per row.
func TestApply_ACP2_ApplySkipsWalk(t *testing.T) {
	p := &countingPlugin{}
	_, err := Apply(context.Background(), p, snapshotForTest("acp2"), false)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if p.walkCalls != 0 {
		t.Errorf("acp2 apply must not call Walk; got %d", p.walkCalls)
	}
	if p.setCalls != 2 {
		t.Errorf("acp2 apply must call SetValue per writable row; got %d, want 2", p.setCalls)
	}
}

// EmberPlus apply also skips the walk — path/OID resolution is per-row.
func TestApply_EmberPlus_ApplySkipsWalk(t *testing.T) {
	p := &countingPlugin{}
	_, err := Apply(context.Background(), p, snapshotForTest("emberplus"), false)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if p.walkCalls != 0 {
		t.Errorf("emberplus apply must not call Walk; got %d", p.walkCalls)
	}
	if p.setCalls != 2 {
		t.Errorf("emberplus apply must call SetValue per writable row; got %d, want 2", p.setCalls)
	}
}

// ACP1 apply also skips the walk now (per #423). Plugin.SetValue
// falls back to fetchObjectMeta on cache miss (#421), so the importer
// no longer needs to pre-walk to populate type metadata.
func TestApply_ACP1_ApplySkipsWalk(t *testing.T) {
	p := &countingPlugin{}
	_, err := Apply(context.Background(), p, snapshotForTest("acp1"), false)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if p.walkCalls != 0 {
		t.Errorf("acp1 apply must not call Walk; got %d", p.walkCalls)
	}
	if p.setCalls != 2 {
		t.Errorf("acp1 apply must call SetValue per writable row; got %d, want 2", p.setCalls)
	}
}

// When a plugin returns ErrObjectNotFound from ValidateValue, the
// importer must skip the row with reason "not_found" and never call
// SetValue.
func TestApply_ValidateNotFound_SkipsRow(t *testing.T) {
	p := &countingPlugin{validateErr: consumer.ErrObjectNotFound}
	rep, err := Apply(context.Background(), p, snapshotForTest("acp2"), false)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if p.setCalls != 0 {
		t.Errorf("not_found rows must not reach SetValue; got %d set call(s)", p.setCalls)
	}
	// 2 writable + 1 read_only; both writables become not_found, the
	// read-only stays read_only.
	if rep.Skipped != 3 {
		t.Errorf("rep.Skipped=%d, want 3", rep.Skipped)
	}
	notFoundCount := 0
	for _, s := range rep.Skips {
		if s.Reason == "not_found" {
			notFoundCount++
		}
	}
	if notFoundCount != 2 {
		t.Errorf("not_found skip count=%d, want 2", notFoundCount)
	}
}

// When a plugin returns ErrValidationFailed from ValidateValue, the
// importer must skip the row with reason "validation_failed".
func TestApply_ValidateFailed_SkipsRow(t *testing.T) {
	wrapped := errors.New("enum not in options")
	p := &countingPlugin{
		// wrap so errors.Is works the same way the importer expects.
		validateErr: errors.Join(consumer.ErrValidationFailed, wrapped),
	}
	rep, err := Apply(context.Background(), p, snapshotForTest("acp2"), true)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if p.setCalls != 0 {
		t.Errorf("validation_failed rows must not reach SetValue; got %d", p.setCalls)
	}
	failedCount := 0
	for _, s := range rep.Skips {
		if s.Reason == "validation_failed" {
			failedCount++
		}
	}
	if failedCount != 2 {
		t.Errorf("validation_failed skip count=%d, want 2", failedCount)
	}
}

// Plugins that don't implement ValueValidator should fall through
// the importer untouched — SetValue still runs for writable rows.
func TestApply_NoValidator_GracefulFallback(t *testing.T) {
	p := &nonValidatorPlugin{}
	rep, err := Apply(context.Background(), p, snapshotForTest("acp2"), false)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if rep.Applied != 2 {
		t.Errorf("no-validator path must still apply writable rows; got %d", rep.Applied)
	}
	if p.setCalls != 2 {
		t.Errorf("SetValue should fire per writable row; got %d", p.setCalls)
	}
}
