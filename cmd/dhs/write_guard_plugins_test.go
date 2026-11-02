package main

import (
	"context"
	"errors"
	"sort"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/plugin"
)

// ADR-0037 P5, extended to every connector on 2026-11-02: a registered
// plugin either declares consumer.WriteGuard or cannot write at all. A
// plugin that could write without naming its target twice fails here, and
// so does a new plugin until it is listed on one side or the other.
func TestEveryWritingPluginGuards(t *testing.T) {
	guarded := map[string]bool{
		"acp1": true, "acp2": true, "ccm": true, "cerebrum-nb": true, "emberplus": true,
		"mnset": true, "rollcall": true, "rrcs": true, "snmp": true,
	}
	cannotWrite := map[string]bool{
		"osc-v10": true, "osc-v11": true, "probel-sw02p": true, "probel-sw08p": true,
		"tsl-v31": true, "tsl-v40": true, "tsl-v50": true,
	}
	names := consumer.List()
	sort.Strings(names)
	seen := 0
	for _, name := range names {
		if name == "importfake" { // registered by a test of this package
			continue
		}
		f, err := consumer.Get(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		plug := f.New(plugin.Deps{})
		g, ok := plug.(consumer.WriteGuard)
		switch {
		case guarded[name]:
			seen++
			if !ok {
				t.Errorf("%s writes but does not declare consumer.WriteGuard", name)
			} else if g.WriteGuardReason() == "" {
				t.Errorf("%s declares the guard without a reason", name)
			}
		case cannotWrite[name]:
			if ok {
				t.Errorf("%s declares the guard but is listed as unable to write", name)
			}
			if _, err := plug.SetValue(context.Background(), consumer.ValueRequest{}, consumer.Value{}); !errors.Is(err, consumer.ErrNotImplemented) {
				t.Errorf("%s: SetValue = %v, want ErrNotImplemented (or declare the guard)", name, err)
			}
		default:
			t.Errorf("%s: a plugin this test does not know; list it as guarded (WriteGuardReason) or as unable to write", name)
		}
	}
	if seen != len(guarded) {
		t.Errorf("%d of %d guarded plugins registered: %v", seen, len(guarded), names)
	}
}
