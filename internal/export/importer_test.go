package export

import (
	"context"
	"testing"

	"dhs/internal/protocol"
)

// countingPlugin satisfies protocol.Protocol and counts Walk + SetValue
// calls so tests can assert which device-touching paths Apply took.
type countingPlugin struct {
	walkCalls int
	setCalls  int
}

func (p *countingPlugin) Connect(context.Context, string, int) error { return nil }
func (p *countingPlugin) Disconnect() error                          { return nil }
func (p *countingPlugin) GetDeviceInfo(context.Context) (protocol.DeviceInfo, error) {
	return protocol.DeviceInfo{}, nil
}
func (p *countingPlugin) GetSlotInfo(context.Context, int) (protocol.SlotInfo, error) {
	return protocol.SlotInfo{}, nil
}
func (p *countingPlugin) Walk(context.Context, int) ([]protocol.Object, error) {
	p.walkCalls++
	return nil, nil
}
func (p *countingPlugin) GetValue(context.Context, protocol.ValueRequest) (protocol.Value, error) {
	return protocol.Value{}, nil
}
func (p *countingPlugin) SetValue(context.Context, protocol.ValueRequest, protocol.Value) (protocol.Value, error) {
	p.setCalls++
	return protocol.Value{}, nil
}
func (p *countingPlugin) Subscribe(protocol.ValueRequest, protocol.EventFunc) error { return nil }
func (p *countingPlugin) Unsubscribe(protocol.ValueRequest) error                   { return nil }

func snapshotForTest() *Snapshot {
	return &Snapshot{
		Device: DeviceInfo{Protocol: "acp2"},
		Slots: []SlotDump{
			{
				Slot: 1,
				Objects: []protocol.Object{
					{ID: 67604, Path: []string{"OUTPUT", "IP", "VIDEO", "STREAM 1", "LEG 1", "Destination IP"},
						Kind: protocol.KindString, Access: 0x03,
						Value: protocol.Value{Kind: protocol.KindString, Str: "239.129.1.20"}},
					{ID: 67605, Path: []string{"OUTPUT", "IP", "VIDEO", "STREAM 1", "LEG 1", "Destination Port"},
						Kind: protocol.KindInt, Access: 0x03,
						Value: protocol.Value{Kind: protocol.KindInt, Int: 12700}},
					{ID: 99, Path: []string{"READ_ONLY"}, Kind: protocol.KindString, Access: 0x01},
				},
			},
		},
	}
}

func TestApply_DryRunSkipsWalk(t *testing.T) {
	p := &countingPlugin{}
	rep, err := Apply(context.Background(), p, snapshotForTest(), true)
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

func TestApply_NonDryRunStillWalks(t *testing.T) {
	p := &countingPlugin{}
	_, err := Apply(context.Background(), p, snapshotForTest(), false)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if p.walkCalls != 1 {
		t.Errorf("apply must call Walk once per slot; got %d", p.walkCalls)
	}
	if p.setCalls != 2 {
		t.Errorf("apply must call SetValue for each writable row; got %d, want 2", p.setCalls)
	}
}
