package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/codec/router"
	rollcall "dhs/internal/snell-rollcall/consumer"
)

func TestRollcallRouteRequiresADestination(t *testing.T) {
	// A crosspoint is named by its destination, and there is no sensible
	// default: destination zero does not exist, since the command space counts
	// from one.
	err := runRollcallRoute(context.Background(), []string{"127.0.0.1:2050"})
	if err == nil {
		t.Fatal("route without --dest should fail")
	}
	if !strings.Contains(err.Error(), "--dest") {
		t.Errorf("error = %v, want it to name the missing flag", err)
	}
}

func TestRollcallVerbsNeedAHost(t *testing.T) {
	ctx := context.Background()
	for name, run := range map[string]func(context.Context, []string) error{
		"router": runRollcallRouter,
		"route":  runRollcallRoute,
		"tally":  runRollcallTally,
	} {
		t.Run(name, func(t *testing.T) {
			err := run(ctx, nil)
			if err == nil {
				t.Fatal("a verb with no host should fail")
			}
			if !strings.Contains(err.Error(), "usage:") {
				t.Errorf("error = %v, want usage", err)
			}
		})
	}
}

func TestRouterJSONShape(t *testing.T) {
	r := &rollcall.RouterInterface{
		Slot:    14,
		Addr:    codec.Address{Unit: 0x81, Index: codec.IndexUnknown},
		Version: 13,
		Name:    "Sirius",
		Salvos:  4,
		Devices: 100,
		Matrices: []rollcall.RouterMatrix{{
			Number:     1,
			Name:       "Matrix 1",
			Controller: 1,
			SrcAssocs:  router.Table{Count: 10},
			DstAssocs:  router.Table{Count: 12},
			Levels: []rollcall.RouterLevel{{
				Number: 1,
				Name:   "Level 1",
				Srcs:   router.Table{Count: 1024},
				Dsts:   router.Table{Count: 512},
			}},
		}},
	}

	body, err := json.Marshal(routerJSON(r))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got["interface_version"] != float64(13) || got["name"] != "Sirius" {
		t.Errorf("router = %v", got)
	}
	matrices, ok := got["matrices"].([]any)
	if !ok || len(matrices) != 1 {
		t.Fatalf("matrices = %v", got["matrices"])
	}
	m := matrices[0].(map[string]any)
	if m["destination_associations"] != float64(12) {
		t.Errorf("destination associations = %v", m["destination_associations"])
	}
	levels := m["levels"].([]any)
	lv := levels[0].(map[string]any)
	if lv["sources"] != float64(1024) || lv["destinations"] != float64(512) {
		t.Errorf("level = %v", lv)
	}
}

func TestRouteSourceDefaultsToTheDestinationsPlane(t *testing.T) {
	// A source without its own matrix and level is on the destination's, which
	// is the ordinary case: routing within one plane.
	if got := orDefault(0, 3); got != 3 {
		t.Errorf("orDefault(0, 3) = %d, want the fallback", got)
	}
	if got := orDefault(2, 3); got != 2 {
		t.Errorf("orDefault(2, 3) = %d, want what was given", got)
	}
}

func TestNamePlaceholders(t *testing.T) {
	if got := nameOrUnset(""); got != "(unset)" {
		t.Errorf("an unnamed router printed as %q", got)
	}
	if got := nameOrUnset("R1"); got != "R1" {
		t.Errorf("a named router printed as %q", got)
	}
	if got := nameOrBlank(""); got != "-" {
		t.Errorf("a missing name printed as %q", got)
	}
	if got := nameOrBlank("CAM 1"); got != "CAM 1" {
		t.Errorf("a name printed as %q", got)
	}
}
