package consumer

// ADR-0025 deliverables 4 and 6 for the Controller: a committed plant
// the walk is replayed from without a host. testdata/exports/ holds the
// catalogue `dhs consumer nmos walk --json` printed for the nmos-cpp
// reference registry and for the nmos-cpp reference node (released
// v0.36.2, 2026-10-04). Served back through a Query API, a walk must
// give the same catalogue: every resource of every collection, document
// for document.
//
// Re-capture with:
//
//	dhs consumer nmos walk --registry http://<registry> --json > reference-registry.json
//	dhs consumer nmos walk --node http://<node> --json > reference-node.json

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWalkReplaysTheCommittedExports(t *testing.T) {
	for _, name := range []string{"reference-registry.json", "reference-node.json"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "testdata", "exports", name))
			if err != nil {
				t.Fatalf("the committed export is missing: %v", err)
			}
			var want CatalogueSnapshot
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatalf("%s is not a catalogue: %v", name, err)
			}
			if len(want.Nodes) == 0 || len(want.Senders) == 0 || len(want.Receivers) == 0 {
				t.Fatalf("%s holds %d nodes, %d senders, %d receivers — not a plant", name, len(want.Nodes), len(want.Senders), len(want.Receivers))
			}

			h := newHarness(t)
			h.cat.nodes, h.cat.devices, h.cat.sources = want.Nodes, want.Devices, want.Sources
			h.cat.flows, h.cat.senders, h.cat.receivers = want.Flows, want.Senders, want.Receivers

			got, errs := h.ctrl.Walk(context.Background())
			if len(errs) != 0 {
				t.Fatalf("walk: %v", errs)
			}
			for _, c := range []struct {
				what      string
				got, want any
			}{
				{"nodes", got.Nodes, want.Nodes}, {"devices", got.Devices, want.Devices},
				{"sources", got.Sources, want.Sources}, {"flows", got.Flows, want.Flows},
				{"senders", got.Senders, want.Senders}, {"receivers", got.Receivers, want.Receivers},
			} {
				a, _ := json.Marshal(c.got)
				b, _ := json.Marshal(c.want)
				var x, y any
				_ = json.Unmarshal(a, &x)
				_ = json.Unmarshal(b, &y)
				if !reflect.DeepEqual(x, y) {
					t.Errorf("%s: the walk of the replayed plant is not the committed export", c.what)
				}
			}
		})
	}
}
