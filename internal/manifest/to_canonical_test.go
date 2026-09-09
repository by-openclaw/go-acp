package manifest

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/export/canonical"
)

// writeDM stores one DM under <cacheDir>/dm/<proto>/<ref>.json, the
// layout DMPath resolves (ADR-0022), so a test can attach it to a
// manifest slot by reference exactly the way a committed fixture does.
func writeDM(t *testing.T, cacheDir, proto, ref, body string) {
	t.Helper()
	p := DMPath(cacheDir, proto, ref)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// flatManifest is the shape ADR-0022 gives an ACP1/ACP2 device: one
// frame, slots addressed by numeric index, DMs by Model@SwRev.
func flatManifest(proto string, slots ...Slot) *Manifest {
	return &Manifest{
		Device: Device{Name: "dev", Protocol: proto,
			Endpoints: []Endpoint{{IP: "0.0.0.0", Port: 2071, Transport: "tcp"}}},
		Frames: []Frame{{Name: "chassis", Slots: slots}},
	}
}

// byPath indexes every element of a canonical tree by Header.Path --
// the key the provider's byIDP index uses -- so assertions read as
// "the parameter at dev.slot-0.identity.Gain is ...".
func byPath(t *testing.T, root canonical.Element) map[string]canonical.Element {
	t.Helper()
	out := map[string]canonical.Element{}
	var walk func(el canonical.Element)
	walk = func(el canonical.Element) {
		h := el.Common()
		if _, seen := out[h.Path]; !seen {
			out[h.Path] = el
		}
		for _, c := range h.Children {
			walk(c)
		}
	}
	walk(root)
	return out
}

func paramAt(t *testing.T, idx map[string]canonical.Element, path string) *canonical.Parameter {
	t.Helper()
	el, ok := idx[path]
	if !ok {
		t.Fatalf("no element at %q", path)
	}
	p, ok := el.(*canonical.Parameter)
	if !ok {
		t.Fatalf("%q is %T, want *canonical.Parameter", path, el)
	}
	return p
}

func nodeAt(t *testing.T, idx map[string]canonical.Element, path string) *canonical.Node {
	t.Helper()
	el, ok := idx[path]
	if !ok {
		t.Fatalf("no element at %q", path)
	}
	n, ok := el.(*canonical.Node)
	if !ok {
		t.Fatalf("%q is %T, want *canonical.Node", path, el)
	}
	return n
}

func childIdents(h *canonical.Header) []string {
	out := make([]string, 0, len(h.Children))
	for _, c := range h.Children {
		out = append(out, c.Common().Identifier)
	}
	return out
}

// acp1CardDM is a flat DM in the ACP1 walker convention: path[] is the
// PARENT chain and label is the leaf's own name. It deliberately mixes
// explicit containers (net, blob), groups that exist only as path
// segments (identity, video/input -> synthetic ancestors), every bound
// shape sanitiseScalar must handle, and the value envelopes the DM
// cache writes (consumer.Value.MarshalJSON).
const acp1CardDM = `{
  "model": "CARD", "sw_rev": "1.0", "protocol": "acp1",
  "objects": [
    {"path": [], "id": 3, "oid": "1.0.9", "label": "net", "kind": "node", "access": 1},
    {"path": ["identity"], "id": 10, "label": "Card name", "kind": "string", "access": 1,
     "meta": {"acp1_type": 5}, "value": {"kind": "string", "str": "CARD"}},
    {"path": ["identity"], "id": 11, "label": "Gain", "kind": "int", "access": 3, "unit": "dB",
     "min": -20, "max": 20, "step": 1, "default": 0, "meta": {"acp1_type": 1},
     "value": {"kind": "int", "int": 3}},
    {"path": ["net"], "id": 4, "label": "IP", "kind": "ipv4", "access": 1,
     "min": 0, "max": 4294967295, "value": {"kind": "ipaddr", "ip": "10.0.0.1"}},
    {"path": ["video", "input"], "id": 5, "label": "Mode", "kind": "enum", "access": 1,
     "enum_items": ["Off", "Auto"], "min": "Off", "max": "Auto", "default": "Off",
     "value": {"kind": "enum", "enum": 1}},
    {"path": [], "id": 6, "label": "blob", "kind": "raw", "access": 1},
    {"path": ["identity"], "id": 12, "label": "Flag", "kind": "enum", "access": 1},
    {"path": ["identity"], "id": 13, "label": "Temp", "kind": "real", "access": 1, "value": null}
  ]
}`

// acp2CardDM is a flat DM in the ACP2 walker convention: path[]
// INCLUDES the object's own label as its last segment. It carries the
// acp2 wire-type meta (objType/numType/optionsMap + raw enum bytes)
// and the Ember+-style element discriminators (matrix / function /
// template) that buildSlotNode dispatches on.
const acp2CardDM = `{
  "model": "NEURON", "sw_rev": "1.0", "protocol": "acp2",
  "objects": [
    {"path": ["Config"], "id": 1, "label": "Config", "kind": "node", "access": 1,
     "meta": {"acp2.objType": 0}},
    {"path": ["Config", "Standard"], "id": 2, "label": "Standard", "kind": "uint", "access": 1,
     "meta": {"acp2.objType": 2, "acp2.optionsMap": {"1271": "Manual", "66": "2SI"}},
     "value": {"kind": "enum", "raw": "AAAE9w==", "enum": 247}},
    {"path": ["Config", "Level"], "id": 3, "label": "Level", "kind": "uint", "access": 1,
     "meta": {"acp2.objType": 3, "acp2.numType": 4}, "min": 0, "max": 255,
     "value": {"kind": "uint", "uint": 7}},
    {"path": ["Config", "xp"], "id": 4, "label": "xp", "kind": "node", "access": 1,
     "meta": {"element": "matrix", "type": "oneToN", "targetCount": 2, "sourceCount": 2,
              "targets": [0, 1], "sources": [0, 1]}},
    {"path": ["Config", "reset"], "id": 5, "label": "reset", "kind": "node", "access": 1,
     "meta": {"element": "function", "arguments": [{"name": "n", "type": "integer"}], "result": []}},
    {"path": ["Config", "tpl"], "id": 6, "label": "tpl", "kind": "node", "access": 1,
     "meta": {"element": "template"}}
  ]
}`

// TestBuildExport_FlatDMs is the flat-shape contract: a manifest with
// three slots (two ACP1-convention, one ACP2-convention, one of them
// without a numeric slot addr) yields a synthetic device root whose
// children are slot-N nodes, each holding the DM's object tree with
// device-rooted paths, synthetic ancestors for group-only path
// segments, and scalar kinds mapped to canonical type/format/access.
func TestBuildExport_FlatDMs(t *testing.T) {
	cache := t.TempDir()
	writeDM(t, cache, "acp1", "CARD@1.0", acp1CardDM)
	writeDM(t, cache, "acp1", "NEURON@1.0", acp2CardDM)
	m := flatManifest("acp1",
		Slot{Addr: map[string]any{"slot": 0}, DM: "CARD@1.0"},
		Slot{Addr: map[string]any{"slot": 1}, DM: "NEURON@1.0"},
		// No numeric slot addr: the slot number falls back to its
		// position under the root (third child -> slot-2).
		Slot{Addr: map[string]any{"oid": "1.4"}, DM: "CARD@1.0"},
	)

	ex, err := BuildExport(m, cache)
	if err != nil {
		t.Fatalf("BuildExport: %v", err)
	}
	if ex.Templates != nil {
		t.Errorf("flat DMs carry no templates, got %+v", ex.Templates)
	}
	root := ex.Root.Common()
	if root.Identifier != "dev" || root.Path != "dev" || root.OID != "1" || root.Number != 1 || root.Access != "read" || !root.IsOnline {
		t.Fatalf("root header = %+v", *root)
	}
	if got := childIdents(root); strings.Join(got, ",") != "slot-0,slot-1,slot-2" {
		t.Fatalf("root children = %v", got)
	}
	idx := byPath(t, ex.Root)

	// slot-0: ACP1 convention.
	slot0 := nodeAt(t, idx, "dev.slot-0")
	if slot0.Number != 0 || slot0.OID != "1.0" || slot0.Identifier != "slot-0" {
		t.Errorf("slot-0 header = %+v", slot0.Header)
	}
	if slot0.Description == nil || *slot0.Description != `dm=CARD@1.0 addr={"slot":0}` {
		t.Errorf("slot-0 description = %v", slot0.Description)
	}
	// Objects are placed shortest-path-first (stable), so explicit
	// root containers come before group-only ancestors.
	if got := childIdents(&slot0.Header); strings.Join(got, ",") != "net,blob,identity,video" {
		t.Errorf("slot-0 children = %v", got)
	}
	net := nodeAt(t, idx, "dev.slot-0.net")
	if net.OID != "1.0.9" || net.Number != 3 || net.Access != "read" {
		t.Errorf("explicit container keeps its DM oid/id: %+v", net.Header)
	}
	if blob := nodeAt(t, idx, "dev.slot-0.blob"); blob.OID != "1.0.6" {
		t.Errorf("kind raw is a container with oid synthesised from slot oid + id: %+v", blob.Header)
	}
	if identity := nodeAt(t, idx, "dev.slot-0.identity"); identity.Access != "read" || !identity.IsOnline ||
		strings.Join(childIdents(&identity.Header), ",") != "Card name,Gain,Flag,Temp" {
		t.Errorf("synthetic ancestor identity = %+v", identity.Header)
	}
	nodeAt(t, idx, "dev.slot-0.video")
	nodeAt(t, idx, "dev.slot-0.video.input")

	name := paramAt(t, idx, "dev.slot-0.identity.Card name")
	if name.Type != "string" || name.Format != nil || name.Value != "CARD" || name.OID != "1.0.10" || name.Access != "read" {
		t.Errorf("acp1_type 5 string = %+v", name)
	}
	gain := paramAt(t, idx, "dev.slot-0.identity.Gain")
	if gain.Type != "integer" || gain.Format == nil || *gain.Format != "int16" || gain.Access != "readWrite" {
		t.Errorf("acp1_type 1 int16 = %+v", gain)
	}
	if gain.Unit == nil || *gain.Unit != "dB" {
		t.Errorf("unit = %v, want dB", gain.Unit)
	}
	if gain.Minimum != int64(-20) || gain.Maximum != int64(20) || gain.Step != int64(1) || gain.Default != int64(0) || gain.Value != int64(3) {
		t.Errorf("integer bounds/value must be int64: min=%#v max=%#v step=%#v def=%#v val=%#v",
			gain.Minimum, gain.Maximum, gain.Step, gain.Default, gain.Value)
	}
	ip := paramAt(t, idx, "dev.slot-0.net.IP")
	if ip.Type != "string" || ip.Format == nil || *ip.Format != "ipv4" || ip.Value != "10.0.0.1" {
		t.Errorf("ipv4 = %+v", ip)
	}
	if ip.Minimum != nil || ip.Maximum != nil {
		t.Errorf("ipv4 bounds encode the address space, must be dropped: min=%#v max=%#v", ip.Minimum, ip.Maximum)
	}
	mode := paramAt(t, idx, "dev.slot-0.video.input.Mode")
	if mode.Type != "enum" || mode.Value != int64(1) {
		t.Errorf("enum = %+v", mode)
	}
	if len(mode.EnumMap) != 2 || mode.EnumMap[0] != (canonical.EnumEntry{Key: "Off", Value: 0}) ||
		mode.EnumMap[1] != (canonical.EnumEntry{Key: "Auto", Value: 1}) {
		t.Errorf("enum_items become sequential EnumMap: %+v", mode.EnumMap)
	}
	if mode.Minimum != nil || mode.Maximum != nil || mode.Default != nil {
		t.Errorf("enum label-string bounds must be dropped: %#v %#v %#v", mode.Minimum, mode.Maximum, mode.Default)
	}
	if flag := paramAt(t, idx, "dev.slot-0.identity.Flag"); flag.EnumMap != nil {
		t.Errorf("enum without items or options map has no EnumMap: %+v", flag.EnumMap)
	}
	if temp := paramAt(t, idx, "dev.slot-0.identity.Temp"); temp.Type != "real" || temp.Value != nil {
		t.Errorf("null envelope leaves Value unset: %+v", temp)
	}

	// slot-1: ACP2 convention + Ember+ element dispatch.
	slot1 := nodeAt(t, idx, "dev.slot-1")
	if slot1.OID != "1.1" || strings.Join(childIdents(&slot1.Header), ",") != "Config" {
		t.Errorf("slot-1 = %+v children %v", slot1.Header, childIdents(&slot1.Header))
	}
	cfg := nodeAt(t, idx, "dev.slot-1.Config")
	if got := childIdents(&cfg.Header); strings.Join(got, ",") != "Standard,Level,xp,reset" {
		t.Errorf("Config children = %v (template must be skipped)", got)
	}
	std := paramAt(t, idx, "dev.slot-1.Config.Standard")
	if std.Type != "enum" || std.Value != int64(1271) {
		t.Errorf("acp2 enum value must come from the 4 raw wire bytes, not the u8 field: %+v", std)
	}
	if len(std.EnumMap) != 2 || std.EnumMap[0] != (canonical.EnumEntry{Key: "2SI", Value: 66}) ||
		std.EnumMap[1] != (canonical.EnumEntry{Key: "Manual", Value: 1271}) {
		t.Errorf("acp2 optionsMap wins over sequential indexes: %+v", std.EnumMap)
	}
	lvl := paramAt(t, idx, "dev.slot-1.Config.Level")
	if lvl.Type != "integer" || lvl.Format == nil || *lvl.Format != "u8" || lvl.Value != int64(7) ||
		lvl.Minimum != int64(0) || lvl.Maximum != int64(255) {
		t.Errorf("acp2 number u8 = %+v", lvl)
	}
	xp, ok := idx["dev.slot-1.Config.xp"].(*canonical.Matrix)
	if !ok || xp.OID != "1.1.4" || xp.Type != canonical.MatrixOneToN || xp.TargetCount != 2 || len(xp.Sources) != 2 {
		t.Errorf("matrix element = %#v", idx["dev.slot-1.Config.xp"])
	}
	reset, ok := idx["dev.slot-1.Config.reset"].(*canonical.Function)
	if !ok || reset.OID != "1.1.5" || len(reset.Arguments) != 1 || reset.Arguments[0].Name != "n" {
		t.Errorf("function element = %#v", idx["dev.slot-1.Config.reset"])
	}
	if _, present := idx["dev.slot-1.Config.tpl"]; present {
		t.Error("template objects are not placed in the tree")
	}

	// slot-2: positional fallback, same DM attached twice.
	slot2 := nodeAt(t, idx, "dev.slot-2")
	if slot2.Number != 2 || slot2.OID != "1.2" || slot2.Description == nil || *slot2.Description != `dm=CARD@1.0 addr={"oid":"1.4"}` {
		t.Errorf("positional slot = %+v", slot2.Header)
	}
	paramAt(t, idx, "dev.slot-2.identity.Gain")
}

// TestBuildExport_CanonicalDMs is the canonical-shape contract (Ember+,
// ADR-0022): each slot DM's root is grafted as a child of the synthetic
// device root, the shared first path segment becomes the root
// identifier so served wire paths equal the cached DM paths, templates
// are collected top-level, and disagreeing prefixes fall back to the
// device name with every path rerooted under it (#456).
func TestBuildExport_CanonicalDMs(t *testing.T) {
	const oneToN = `{"model":"oneToN","sw_rev":"1","protocol":"emberplus","root":{
	  "number":1,"identifier":"oneToN","path":"router.oneToN","oid":"1.1","isOnline":true,"access":"read",
	  "children":[{"number":1,"identifier":"gain","path":"router.oneToN.gain","oid":"1.1.1","isOnline":true,
	               "access":"readWrite","type":"integer","value":3}]}}`
	const nToN = `{"model":"nToN","sw_rev":"1","protocol":"emberplus","root":{
	  "number":2,"identifier":"nToN","path":"router.nToN","oid":"1.2","isOnline":true,"access":"read"},
	  "templates":[{"number":1,"oid":"1.9","identifier":"t","description":null,"template":{
	    "number":1,"identifier":"t","path":"t","oid":"1.9","isOnline":true,"access":"read","type":"string","value":"x"}}]}`
	const identity = `{"model":"identity","sw_rev":"1","protocol":"emberplus","root":{
	  "number":0,"identifier":"identity","path":"identity","oid":"1.0","isOnline":true,"access":"read",
	  "children":[{"number":1,"identifier":"product","path":"identity.product","oid":"1.0.1","isOnline":true,
	               "access":"read","type":"string","value":"p"}]}}`

	t.Run("shared prefix becomes the root identifier", func(t *testing.T) {
		cache := t.TempDir()
		writeDM(t, cache, "emberplus", "oneToN@1", oneToN)
		writeDM(t, cache, "emberplus", "nToN@1", nToN)
		m := flatManifest("emberplus",
			Slot{Addr: map[string]any{"oid": "1.1"}, DM: "oneToN@1"},
			Slot{Addr: map[string]any{"oid": "1.2"}, DM: "nToN@1"})
		ex, err := BuildExport(m, cache)
		if err != nil {
			t.Fatalf("BuildExport: %v", err)
		}
		if h := ex.Root.Common(); h.Identifier != "router" || h.Path != "router" {
			t.Errorf("root = %+v, want identifier/path router (from the DMs, not the device name)", *h)
		}
		idx := byPath(t, ex.Root)
		for _, p := range []string{"router.oneToN", "router.oneToN.gain", "router.nToN"} {
			if _, ok := idx[p]; !ok {
				t.Errorf("missing %q (paths must not be double-prefixed); have %v", p, keys(idx))
			}
		}
		if len(ex.Templates) != 1 || ex.Templates[0].Identifier != "t" || ex.Templates[0].Template.Common().Path != "t" {
			t.Errorf("templates = %+v, want the nToN DM's single template", ex.Templates)
		}
	})

	t.Run("disagreeing prefixes reroot under the device name", func(t *testing.T) {
		cache := t.TempDir()
		writeDM(t, cache, "emberplus", "identity@1", identity)
		writeDM(t, cache, "emberplus", "oneToN@1", oneToN)
		m := flatManifest("emberplus",
			Slot{Addr: map[string]any{"oid": "1.0"}, DM: "identity@1"},
			Slot{Addr: map[string]any{"oid": "1.1"}, DM: "oneToN@1"})
		ex, err := BuildExport(m, cache)
		if err != nil {
			t.Fatalf("BuildExport: %v", err)
		}
		if got := ex.Root.Common().Identifier; got != "dev" {
			t.Errorf("root identifier = %q, want device name", got)
		}
		idx := byPath(t, ex.Root)
		for _, p := range []string{"dev.identity", "dev.identity.product", "dev.router.oneToN", "dev.router.oneToN.gain"} {
			if _, ok := idx[p]; !ok {
				t.Errorf("missing rerooted %q; have %v", p, keys(idx))
			}
		}
		if ex.Templates != nil {
			t.Errorf("no DM carried templates, got %+v", ex.Templates)
		}
	})
}

// jsonNumber is the shape a manifest addr takes when decoded with
// json.Decoder.UseNumber.
func jsonNumber(s string) json.Number { return json.Number(s) }

func keys(m map[string]canonical.Element) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestBuildExport_CommittedFixtures builds from the connectors'
// committed integration-test fixtures (TEMPLATE.md: the provider must
// build its frame from the repo alone). Both shapes: Ember+ canonical
// DMs with bare, disagreeing root paths; ACP1 flat DMs walked from
// real Synapse cards.
func TestBuildExport_CommittedFixtures(t *testing.T) {
	t.Run("emberplus canonical", func(t *testing.T) {
		cache := filepath.Join("..", "emberplus", "testdata", "integration-test")
		m, err := Load(filepath.Join(cache, "manifest", "emberplus-integration.json"))
		if err != nil {
			t.Fatal(err)
		}
		ex, err := BuildExport(m, cache)
		if err != nil {
			t.Fatalf("BuildExport: %v", err)
		}
		if h := ex.Root.Common(); h.Identifier != "dhs-emberplus-integration" || len(h.Children) != 7 {
			t.Fatalf("root = %q with %d slots, want device name with 7 slots", h.Identifier, len(h.Children))
		}
		for p := range byPath(t, ex.Root) {
			if p != "dhs-emberplus-integration" && !strings.HasPrefix(p, "dhs-emberplus-integration.") {
				t.Errorf("path %q not rerooted under the device", p)
			}
		}
	})
	t.Run("acp1 flat", func(t *testing.T) {
		cache := filepath.Join("..", "acp1", "testdata", "integration-test")
		m, err := Load(filepath.Join(cache, "manifest", "synapse-test.json"))
		if err != nil {
			t.Fatal(err)
		}
		ex, err := BuildExport(m, cache)
		if err != nil {
			t.Fatalf("BuildExport: %v", err)
		}
		if got := childIdents(ex.Root.Common()); strings.Join(got, ",") != "slot-0,slot-1,slot-2,slot-3,slot-4,slot-5" {
			t.Fatalf("slots = %v", got)
		}
		idx := byPath(t, ex.Root)
		if name := paramAt(t, idx, "synapse-test.slot-0.identity.Card name"); name.Value != "RRS18" {
			t.Errorf("slot-0 card name = %#v, want the walked RRS18", name.Value)
		}
	})
}

// TestBuildExport_Errors: every failure names the frame and the slot DM
// so an operator can find the offending fixture without the source.
func TestBuildExport_Errors(t *testing.T) {
	t.Run("missing DM", func(t *testing.T) {
		m := flatManifest("acp1", Slot{Addr: map[string]any{"slot": 0}, DM: "GHOST@1"})
		_, err := BuildExport(m, t.TempDir())
		if err == nil || !strings.Contains(err.Error(), `frame="chassis" slot dm="GHOST@1"`) || !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("unparseable DM", func(t *testing.T) {
		cache := t.TempDir()
		writeDM(t, cache, "acp1", "BAD@1", "{not json")
		_, err := BuildExport(flatManifest("acp1", Slot{DM: "BAD@1"}), cache)
		if err == nil || !strings.Contains(err.Error(), "dm parse") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("canonical root that is not an element", func(t *testing.T) {
		cache := t.TempDir()
		writeDM(t, cache, "emberplus", "ODD@1", `{"model":"ODD","root":[1]}`)
		_, err := BuildExport(flatManifest("emberplus", Slot{DM: "ODD@1"}), cache)
		if err == nil || !strings.Contains(err.Error(), "parse canonical root") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("slot assembly failure is wrapped with frame and slot", func(t *testing.T) {
		orig := buildSlot
		buildSlot = func(int, Slot, *dmFile, string) (*canonical.Node, error) {
			return nil, errors.New("boom")
		}
		t.Cleanup(func() { buildSlot = orig })
		cache := t.TempDir()
		writeDM(t, cache, "acp1", "CARD@1.0", acp1CardDM)
		_, err := BuildExport(flatManifest("acp1", Slot{Addr: map[string]any{"slot": 4}, DM: "CARD@1.0"}), cache)
		if err == nil || err.Error() != `manifest frame="chassis" slot=4: boom` {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestLoadDM(t *testing.T) {
	cache := t.TempDir()
	writeDM(t, cache, "acp2", "OK@1", `{"model":"OK","sw_rev":"1","protocol":"acp2","objects":[{"id":1,"label":"x","kind":"string"}]}`)
	writeDM(t, cache, "acp2", "BAD@1", `{"model":`)
	for _, tc := range []struct {
		name, ref, wantErr string
	}{
		{"missing file is a read error", "NOPE@1", "dm read"},
		{"malformed json is a parse error", "BAD@1", "dm parse"},
		{"well-formed DM loads", "OK@1", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := loadDM(DMPath(cache, "acp2", tc.ref))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || d.Model != "OK" || len(d.Objects) != 1 || d.Objects[0].Label != "x" {
				t.Fatalf("d = %+v, err = %v", d, err)
			}
		})
	}
}

// TestDeriveSharedRootIdentifier pins how the synthetic root name is
// chosen from the slot DMs: unreadable, unparseable and flat (no root
// path) DMs are ignored; the first path segment must agree across every
// canonical DM, otherwise the caller's device name wins.
func TestDeriveSharedRootIdentifier(t *testing.T) {
	cache := t.TempDir()
	dms := map[string]string{
		"bad@1":      `{`,
		"flat@1":     `{"model":"flat","objects":[]}`,
		"router-a@1": `{"root":{"path":"router.oneToN"}}`,
		"router-b@1": `{"root":{"path":"router.nToN"}}`,
		"bare@1":     `{"root":{"path":"router"}}`,
		"other@1":    `{"root":{"path":"desk.main"}}`,
		"dotfirst@1": `{"root":{"path":".odd"}}`,
	}
	for ref, body := range dms {
		writeDM(t, cache, "emberplus", ref, body)
	}
	for _, tc := range []struct {
		name string
		refs []string
		want string
	}{
		{"no DM readable", []string{"ghost@1"}, ""},
		{"unparseable and flat DMs are skipped", []string{"bad@1", "flat@1"}, ""},
		{"single canonical DM gives its first segment", []string{"router-a@1"}, "router"},
		{"dotless root path is taken whole", []string{"bare@1"}, "router"},
		{"agreeing DMs share the segment, skipped ones do not break it", []string{"ghost@1", "router-a@1", "flat@1", "router-b@1", "bare@1"}, "router"},
		{"disagreeing DMs yield empty", []string{"router-a@1", "other@1"}, ""},
		{"leading dot keeps the whole path", []string{"dotfirst@1"}, ".odd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			slots := make([]Slot, 0, len(tc.refs))
			for _, r := range tc.refs {
				slots = append(slots, Slot{DM: r})
			}
			if got := deriveSharedRootIdentifier(flatManifest("emberplus", slots...), cache); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestEnsureAncestors pins the synthetic-ancestor shape for a group
// chain that has no container objects of its own: segments already
// indexed are reused, missing ones get a read-only online node whose
// path hangs off the slot and whose oid extends the slot oid by one
// positional digit per depth.
func TestEnsureAncestors(t *testing.T) {
	slot := &canonical.Node{Header: canonical.Header{Identifier: "slot-0", Path: "dev.slot-0", OID: "1.0"}}
	existing := &canonical.Node{Header: canonical.Header{Identifier: "a", Path: "dev.slot-0.a", OID: "1.0.7"}}
	slot.Children = []canonical.Element{existing}
	idx := map[string]*canonical.Node{"": slot, "a": existing}

	deepest := ensureAncestors(slot, "dev", "slot-0", 0, []string{"a", "b", "c"}, idx)

	if len(slot.Children) != 1 {
		t.Fatalf("existing ancestor a must be reused, slot children = %v", childIdents(&slot.Header))
	}
	b, ok := idx["a.b"]
	if !ok || b.Path != "dev.slot-0.a.b" || b.OID != "1.0.0.1" || b.Number != 1 || b.Access != "read" || !b.IsOnline {
		t.Fatalf("a.b = %+v", b)
	}
	if existing.Children[0] != b {
		t.Errorf("b must hang under the existing a node")
	}
	c := idx["a.b.c"]
	if c == nil || c != deepest || c.Path != "dev.slot-0.a.b.c" || c.OID != "1.0.0.1.2" || c.Number != 2 {
		t.Fatalf("a.b.c = %+v, deepest = %+v", c, deepest)
	}
	if b.Children[0] != c {
		t.Errorf("c must hang under b")
	}
}

func TestIsContainerKind(t *testing.T) {
	for kind, want := range map[string]bool{"raw": true, "node": true, "": true, "string": false, "enum": false} {
		if got := isContainerKind(kind); got != want {
			t.Errorf("isContainerKind(%q) = %v, want %v", kind, got, want)
		}
	}
}

// TestSlotIndex covers every shape a manifest addr may arrive in:
// in-memory ints, JSON float64, json.Number and decimal strings
// resolve; anything else means "no numeric slot" (positional fallback).
func TestSlotIndex(t *testing.T) {
	for _, tc := range []struct {
		name   string
		addr   map[string]any
		want   int
		wantOK bool
	}{
		{"missing key", map[string]any{"oid": "1.4"}, 0, false},
		{"int", map[string]any{"slot": 3}, 3, true},
		{"int64", map[string]any{"slot": int64(4)}, 4, true},
		{"float64 from json", map[string]any{"slot": float64(5)}, 5, true},
		{"json.Number", map[string]any{"slot": jsonNumber("6")}, 6, true},
		{"json.Number non-integer", map[string]any{"slot": jsonNumber("6.5")}, 0, false},
		{"decimal string", map[string]any{"slot": "7"}, 7, true},
		{"non-numeric string", map[string]any{"slot": "seven"}, 0, false},
		{"unsupported type", map[string]any{"slot": true}, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := slotIndex(tc.addr)
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("slotIndex = (%d, %v), want (%d, %v)", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// TestParamTypeAndFormat_ACP1AndKinds pins the remaining two rungs of
// the priority ladder: meta.acp1_type (the ACP1 wire type byte, spec
// table: 1=int16 2=ipv4 3=float 4=enum 5=string 6=frame 7=alarm 8=file
// 9=int32 10=uint8), then the cross-protocol ValueKind fallback.
func TestParamTypeAndFormat_ACP1AndKinds(t *testing.T) {
	for _, tc := range []struct {
		name         string
		o            dmObject
		wantT, wantF string
	}{
		{"acp1 1 int16", dmObject{Meta: map[string]any{"acp1_type": float64(1)}}, "integer", "int16"},
		{"acp1 2 ipv4", dmObject{Meta: map[string]any{"acp1_type": float64(2)}}, "string", "ipv4"},
		{"acp1 3 float", dmObject{Meta: map[string]any{"acp1_type": float64(3)}}, "real", ""},
		{"acp1 4 enum", dmObject{Meta: map[string]any{"acp1_type": float64(4)}}, "enum", ""},
		{"acp1 5 string", dmObject{Meta: map[string]any{"acp1_type": float64(5)}}, "string", ""},
		{"acp1 6 frame", dmObject{Meta: map[string]any{"acp1_type": float64(6)}}, "octets", "frame"},
		{"acp1 7 alarm", dmObject{Meta: map[string]any{"acp1_type": float64(7)}}, "boolean", "alarm"},
		{"acp1 8 file", dmObject{Meta: map[string]any{"acp1_type": float64(8)}}, "string", "file"},
		{"acp1 9 int32", dmObject{Meta: map[string]any{"acp1_type": float64(9)}}, "integer", "int32"},
		{"acp1 10 uint8", dmObject{Meta: map[string]any{"acp1_type": float64(10)}}, "integer", "uint8"},
		{"acp1 unknown falls back to kind", dmObject{Kind: "bool", Meta: map[string]any{"acp1_type": float64(11)}}, "boolean", ""},
		{"acp2 node objType falls back to kind", dmObject{Kind: "float", Meta: map[string]any{"acp2.objType": float64(0)}}, "real", ""},
		{"acp2 number s8", dmObject{Meta: map[string]any{"acp2.objType": float64(3), "acp2.numType": float64(0)}}, "integer", "s8"},
		{"acp2 number s16", dmObject{Meta: map[string]any{"acp2.objType": float64(3), "acp2.numType": float64(1)}}, "integer", "s16"},
		{"acp2 number s64", dmObject{Meta: map[string]any{"acp2.objType": float64(3), "acp2.numType": float64(3)}}, "integer", "s64"},
		{"acp2 number u32", dmObject{Meta: map[string]any{"acp2.objType": float64(3), "acp2.numType": float64(6)}}, "integer", "u32"},
		{"kind integer", dmObject{Kind: "integer"}, "integer", ""},
		{"kind number", dmObject{Kind: "number"}, "integer", ""},
		{"kind int", dmObject{Kind: "int"}, "integer", ""},
		{"kind uint", dmObject{Kind: "uint"}, "integer", "uint8"},
		{"kind real", dmObject{Kind: "real"}, "real", ""},
		{"kind float", dmObject{Kind: "float"}, "real", ""},
		{"kind enum", dmObject{Kind: "enum"}, "enum", ""},
		{"kind enumerated", dmObject{Kind: "enumerated"}, "enum", ""},
		{"kind ipv4", dmObject{Kind: "ipv4"}, "string", "ipv4"},
		{"kind ipaddr", dmObject{Kind: "ipaddr"}, "string", "ipv4"},
		{"kind ip", dmObject{Kind: "ip"}, "string", "ipv4"},
		{"kind string", dmObject{Kind: "string"}, "string", ""},
		{"kind bool", dmObject{Kind: "bool"}, "boolean", ""},
		{"kind boolean", dmObject{Kind: "boolean"}, "boolean", ""},
		{"kind alarm", dmObject{Kind: "alarm"}, "boolean", "alarm"},
		{"kind frame", dmObject{Kind: "frame"}, "octets", "frame"},
		{"unknown kind is a string", dmObject{Kind: "mystery"}, "string", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotT, gotF := paramTypeAndFormat(tc.o)
			if gotT != tc.wantT || gotF != tc.wantF {
				t.Fatalf("= (%q, %q), want (%q, %q)", gotT, gotF, tc.wantT, tc.wantF)
			}
		})
	}
}

func TestToUint8(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   any
		want uint8
	}{
		{"float64", float64(9), 9},
		{"int", 10, 10},
		{"int64", int64(11), 11},
		{"uint8", uint8(12), 12},
		{"json.Number", jsonNumber("13"), 13},
		{"json.Number non-integer is 0", jsonNumber("1.5"), 0},
		{"string is 0", "5", 0},
		{"nil is 0", nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := toUint8(tc.in); got != tc.want {
				t.Fatalf("toUint8(%#v) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// TestAccessString maps the ACP access bits (bit0 read, bit1 write)
// onto the four canonical levels; a value with neither bit is served
// read-only rather than hidden.
func TestAccessString(t *testing.T) {
	for _, tc := range []struct {
		bits uint8
		want string
	}{
		{0, "read"}, {1, "read"}, {2, "write"}, {3, "readWrite"}, {7, "readWrite"}, {4, "read"},
	} {
		if got := accessString(tc.bits); got != tc.want {
			t.Errorf("accessString(%d) = %q, want %q", tc.bits, got, tc.want)
		}
	}
}

// TestSanitiseScalar pins which DM bounds survive into the canonical
// parameter: numeric bounds are coerced to the type's Go scalar,
// enum-label strings are dropped, address-space bounds (ipv4/file/
// frame) are dropped, and anything already shaped correctly passes.
func TestSanitiseScalar(t *testing.T) {
	for _, tc := range []struct {
		name        string
		typ, format string
		in, want    any
	}{
		{"nil stays nil", "integer", "", nil, nil},
		{"ipv4 bounds dropped", "string", "ipv4", float64(0), nil},
		{"file bounds dropped", "string", "file", float64(0), nil},
		{"frame bounds dropped", "octets", "frame", float64(0), nil},
		{"integer from json float", "integer", "int16", float64(-20), int64(-20)},
		{"integer from int", "integer", "", 20, int64(20)},
		{"integer from uint64", "integer", "", uint64(21), int64(21)},
		{"boolean numeric", "boolean", "alarm", float64(1), int64(1)},
		{"enum label string dropped", "enum", "", "Off", nil},
		{"integer of unknown shape passes", "integer", "", true, true},
		{"real from int", "real", "", 2, float64(2)},
		{"real from int64", "real", "", int64(3), float64(3)},
		{"real from uint64", "real", "", uint64(4), float64(4)},
		{"real string dropped", "real", "", "1.5", nil},
		{"real float passes", "real", "", 1.5, 1.5},
		{"string passes through", "string", "", "abc", "abc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitiseScalar(tc.typ, tc.format, tc.in); got != tc.want {
				t.Fatalf("sanitiseScalar(%q, %q, %#v) = %#v, want %#v", tc.typ, tc.format, tc.in, got, tc.want)
			}
		})
	}
}

// TestUnwrapValue pins the consumer.Value envelope -> scalar contract
// for every kind the DM cache writes, and that empty / null /
// malformed / incomplete envelopes leave the parameter value unset.
func TestUnwrapValue(t *testing.T) {
	for _, tc := range []struct {
		name   string
		raw    string
		want   any
		wantOK bool
	}{
		{"empty", "", nil, false},
		{"whitespace", "  ", nil, false},
		{"null", "null", nil, false},
		{"malformed", "{oops", nil, false},
		{"string", `{"kind":"string","str":"hello"}`, "hello", true},
		{"int", `{"kind":"int","int":-5}`, int64(-5), true},
		{"uint coerced to int64", `{"kind":"uint","uint":42}`, int64(42), true},
		{"float", `{"kind":"float","float":1.25}`, 1.25, true},
		{"bool", `{"kind":"bool","bool":true}`, true, true},
		{"enum u8 field", `{"kind":"enum","enum":3}`, int64(3), true},
		{"enum raw of wrong length falls back to u8 field", `{"kind":"enum","raw":"AQI=","enum":3}`, int64(3), true},
		{"ipaddr", `{"kind":"ipaddr","ip":"10.0.0.1"}`, "10.0.0.1", true},
		{"kind without payload", `{"kind":"int"}`, nil, false},
		{"empty ip", `{"kind":"ipaddr","ip":""}`, nil, false},
		{"unknown kind", `{"kind":"octets","raw":"AQ=="}`, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := unwrapValue([]byte(tc.raw))
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("unwrapValue(%s) = (%#v, %v), want (%#v, %v)", tc.raw, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}
