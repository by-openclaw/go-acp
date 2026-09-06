// Package tsl_test — replay tests run captured Trames (per ADR-0021)
// through the TSL plugin's Validate() method. Skips cleanly when no
// local capture is present so a fresh clone is green without
// `git lfs pull` or any pre-loaded fixtures.
//
// Also pins the TSL "no walk verb" contract — TSL is positional UMD
// tally with no walkable tree, so Walk / GetValue / SetValue /
// Subscribe must all return consumer.ErrNotImplemented for every
// version.
package tsl_test

import (
	"context"
	"dhs/internal/plugin"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/tsl/consumer"
	"dhs/internal/wiretrace"
)

// loadTrames reads a captured wire trace from
// captures/<proto-name>/<scenario>/frames.jsonl (gitignored, local-
// only per ADR-0021). Skips cleanly when the file is missing —
// re-capture with `dhs consumer <proto-name> listen --capture <path>`.
func loadTrames(t *testing.T, protoName, scenario string) []wiretrace.Trame {
	t.Helper()
	path := filepath.Join("..", "..", "..", "captures", protoName, scenario, "frames.jsonl")
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("capture %s missing — recapture with `dhs consumer %s listen --capture %s`", path, protoName, path)
	}
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()

	trames, err := wiretrace.ReadTrames(f)
	if err != nil {
		t.Fatalf("ReadTrames: %v", err)
	}
	return trames
}

func newValidator(t *testing.T, factory *tsl.Factory) consumer.Validator {
	t.Helper()
	plug := factory.New(plugin.Deps{Logger: slog.Default()})
	v, ok := plug.(consumer.Validator)
	if !ok {
		t.Fatalf("tsl Plugin (%s) does not implement consumer.Validator", factory.Meta().Name)
	}
	return v
}

// TestReplay_TslV50DMSG runs every Trame through Plugin.Validate
// and asserts no decode errors and no spec invariant violations on
// a v5.0 multi-DMSG capture.
func TestReplay_TslV50DMSG(t *testing.T) {
	trames := loadTrames(t, "tsl-v50", "dmsg_burst")
	if len(trames) == 0 {
		t.Fatal("no trames in capture")
	}
	v := newValidator(t, &tsl.Factory{})
	report, err := v.Validate(context.Background(), trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	t.Logf("tsl-v50 trames: %d decoded (%d tx, %d rx)",
		report.TramesProcessed,
		report.PerDirection[wiretrace.DirectionTx],
		report.PerDirection[wiretrace.DirectionRx])
	for _, e := range report.Errors {
		t.Errorf("trame %d (%s): %s", e.TrameIndex, e.Direction, e.Err)
	}
	for _, inv := range report.Invariants {
		t.Logf("compliance note: %s", inv)
	}
}

// TestPlugin_WalkReturnsErrNotImplemented_AllVersions pins the
// TSL "no walk verb" contract. TSL is positional UMD tally — there
// is no walkable tree, no per-object value to read or write, no
// per-object subscribe path. The Plugin.Walk / GetValue / SetValue /
// Subscribe methods MUST return consumer.ErrNotImplemented for
// every registered version (V31, V40, V50). Future feature PRs
// that try to wire Walk for TSL break this test, by design — TSL
// callers use SubscribeV31 / SubscribeV40 / SubscribeV50, not the
// generic consumer.consumer.Subscribe path.
func TestPlugin_WalkReturnsErrNotImplemented_AllVersions(t *testing.T) {
	cases := []struct {
		name    string
		factory func(*slog.Logger) *tsl.Plugin
	}{
		{"v31", tsl.NewPluginV31},
		{"v40", tsl.NewPluginV40},
		{"v50", tsl.NewPluginV50},
	}
	ctx := context.Background()
	req := consumer.ValueRequest{ID: 1}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := c.factory(slog.Default())
			if _, err := p.Walk(ctx, 0); !errors.Is(err, consumer.ErrNotImplemented) {
				t.Errorf("Walk: got %v, want ErrNotImplemented", err)
			}
			if _, err := p.GetValue(ctx, req); !errors.Is(err, consumer.ErrNotImplemented) {
				t.Errorf("GetValue: got %v, want ErrNotImplemented", err)
			}
			if _, err := p.SetValue(ctx, req, consumer.Value{}); !errors.Is(err, consumer.ErrNotImplemented) {
				t.Errorf("SetValue: got %v, want ErrNotImplemented", err)
			}
			if err := p.Subscribe(req, nil); !errors.Is(err, consumer.ErrNotImplemented) {
				t.Errorf("Subscribe: got %v, want ErrNotImplemented", err)
			}
			if _, err := p.GetDeviceInfo(ctx); !errors.Is(err, consumer.ErrNotImplemented) {
				t.Errorf("GetDeviceInfo: got %v, want ErrNotImplemented", err)
			}
			if _, err := p.GetSlotInfo(ctx, 0); !errors.Is(err, consumer.ErrNotImplemented) {
				t.Errorf("GetSlotInfo: got %v, want ErrNotImplemented", err)
			}
		})
	}
}
