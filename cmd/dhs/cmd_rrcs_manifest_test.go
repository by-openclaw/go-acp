package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"dhs/internal/datastore"
	"dhs/internal/manifest"
	"dhs/internal/rrcs/codec"
)

// rrcsManifestFake is the tree gateway with the types of its ports
// named, a type list, and a configuration that takes creates and
// deletes: a created port is listed with its halves, a deleted one is
// gone. Every ConfigurationChange is kept.
func rrcsManifestFake(t *testing.T) (*rrcsFake, func() []codec.Value) {
	t.Helper()
	var mu sync.Mutex
	var sent []codec.Value
	created := map[int32]int32{} // port → type
	removed := map[int32]bool{}
	channels := map[string]int32{} // "port/block" → the channel count an edit set
	st, mem := codec.Struct, rrcsMember
	stream := func(block string) codec.Member {
		return mem(block, st(mem("Protocol", codec.Int(5)), mem("Channels", codec.Int(1)), mem("Selection", codec.Int(1)),
			mem("Multicast", codec.String("0.0.0.0")), mem("MulticastPort", codec.Int(5004))))
	}
	// withChannels gives a listed port the channel counts the edits set.
	withChannels := func(p codec.Value) codec.Value {
		number, _ := p.Field("Port")
		for i, m := range p.Members {
			n, set := channels[strconv.Itoa(int(number.Int))+"/"+m.Name]
			if !set || m.Value.Kind != codec.KindStruct {
				continue
			}
			block := m.Value
			block.Members = append([]codec.Member(nil), block.Members...)
			for j, f := range block.Members {
				if f.Name == "Channels" {
					block.Members[j].Value = codec.Int(n)
				}
			}
			p.Members[i].Value = block
		}
		return p
	}
	half := func(port int32, in bool, id int32) codec.Value {
		typ, label, block := "Output (AES67)", "O.", "PortAes67Output"
		if in {
			typ, label, block = "Input (AES67)", "I.", "PortAes67Input"
		}
		return st(mem("Net", codec.Int(1)), mem("Node", codec.Int(61)), mem("Port", codec.Int(port)),
			mem("Input", codec.Bool(in)), mem("Output", codec.Bool(!in)), mem("PortType", codec.String(typ)), mem("PortExType", codec.Int(3)),
			mem("Label", codec.String(label+"-7.33")), mem("LongName", codec.String(label+" -7.33")), mem("ObjectID", codec.Int(id)),
			mem("KeyCount", codec.Int(0)), mem("PageCount", codec.Int(0)), stream(block))
	}
	fourWire := func(port int32, id int32) codec.Value {
		return st(mem("Net", codec.Int(1)), mem("Node", codec.Int(61)), mem("Port", codec.Int(port)),
			mem("Input", codec.Bool(true)), mem("Output", codec.Bool(true)), mem("PortType", codec.String("4-Wire (AES67)")), mem("PortExType", codec.Int(2)),
			mem("Label", codec.String("4W")), mem("LongName", codec.String("Four wire")), mem("ObjectID", codec.Int(id)),
			mem("KeyCount", codec.Int(0)), mem("PageCount", codec.Int(0)), stream("PortAes67Input"), stream("PortAes67Output"))
	}
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		mu.Lock()
		defer mu.Unlock()
		k := call.Params[0]
		switch call.Method {
		case "GetPortExTypeList":
			v, err := rrcsValueOfJSON(`[{"PortExType":2,"PortExTypeString":"FOUR_WIRE"},{"PortExType":3,"PortExTypeString":"FOUR_WIRE_SPLIT"},{"PortExType":11,"PortExTypeString":"RSP_1232HL"},{"PortExType":33,"PortExTypeString":"BOLERO_BP"}]`)
			if err != nil {
				t.Fatal(err)
			}
			return codec.Array(k, v), true
		case "GetAllPorts":
			v, ok := rrcsTreeAnswer(call)
			types := map[string]int32{"RSP-1232HL": 11, "Output (AES67)": 3, "Input (AES67)": 3, "4-Wire (AES67)": 2}
			kept := v.Items[1].Items[:0]
			for _, p := range v.Items[1].Items {
				number, _ := p.Field("Port")
				if removed[number.Int] {
					continue
				}
				pt, _ := p.Field("PortType")
				p.Members = append(p.Members, mem("PortExType", codec.Int(types[pt.Str])))
				kept = append(kept, withChannels(p))
			}
			for port, typ := range created {
				switch typ {
				case 3:
					kept = append(kept, withChannels(half(port, true, 7000+port)), withChannels(half(port, false, 8000+port)))
				default:
					kept = append(kept, withChannels(fourWire(port, 9000+port)))
				}
			}
			v.Items[1].Items = kept
			return v, ok
		case "ConfigurationChange":
			change := call.Params[1].Items[0]
			sent = append(sent, change)
			ct, _ := change.Field("ChangeType")
			sp, _ := change.Field("SpecificParams")
			addr, _ := sp.Field("PortAddress")
			port, _ := addr.Field("Port")
			switch ct.Str {
			case "create":
				typ, _ := sp.Field("PortType")
				created[port.Int] = typ.Int
				delete(removed, port.Int)
			case "delete":
				removed[port.Int] = true
				delete(created, port.Int)
			default:
				for _, block := range []string{"PortAes67Input", "PortAes67Output"} {
					if b, ok := sp.Field(block); ok {
						if n, ok := b.Field("Channels"); ok {
							channels[strconv.Itoa(int(port.Int))+"/"+block] = n.Int
						}
					}
				}
			}
			return k, true
		}
		return rrcsTreeAnswer(call)
	})
	return f, func() []codec.Value {
		mu.Lock()
		defer mu.Unlock()
		return append([]codec.Value(nil), sent...)
	}
}

func rrcsDesiredManifest(t *testing.T, dir string) string {
	t.Helper()
	role := func(dir, kind string, kv ...any) map[string]any {
		r := map[string]any{"kind": kind}
		for i := 0; i+1 < len(kv); i += 2 {
			r[kv[i].(string)] = kv[i+1]
		}
		return map[string]any{dir: r}
	}
	mf := manifest.Manifest{
		Device: manifest.Device{Name: "FRAME A", Protocol: "rrcs", IP: "127.0.0.1",
			Endpoints: []manifest.Endpoint{{IP: "127.0.0.1", Port: 8193, Transport: "tcp"}}},
		Frames: []manifest.Frame{{Name: "net.1.node.60", Slots: []manifest.Slot{
			{Addr: map[string]any{"bay": 0, "node": 60, "oid": 9}, DM: "ARTIST_1024@9.0.1"},
			// absent: created as a split port, its input a main of 8 channels
			{Addr: map[string]any{"bay": 1, "node": 61, "port": 1056, "template": "FOUR_WIRE_SPLIT", "role": role("in", "stream-block", "channels", 8)}, DM: "AES67@9.0.1"},
			// present: its output becomes a main of 2 channels
			{Addr: map[string]any{"bay": 1, "node": 61, "port": 7, "type": 3, "role": role("out", "stream-block", "channels", 2)}, DM: "AES67@9.0.1"},
			// present as a panel, wanted as a 4-wire: a retype
			{Addr: map[string]any{"bay": 1, "node": 61, "port": 1026, "type": 2, "role": map[string]any{"kind": "port"}}, DM: "AES67@9.0.1"},
			// present, as wanted
			{Addr: map[string]any{"bay": 1, "node": 61, "port": 1041, "template": "FOUR_WIRE", "role": map[string]any{"kind": "port"}}, DM: "AES67@9.0.1"},
		}}},
	}
	raw, err := json.MarshalIndent(mf, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "desired.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRRCSEnsureManifest(t *testing.T) {
	prev := treeStore
	treeStore = datastore.NewTreeStore(filepath.Join(t.TempDir(), ".cache"))
	t.Cleanup(func() { treeStore = prev })
	f, sent := rrcsManifestFake(t)
	desired := rrcsDesiredManifest(t, t.TempDir())

	// The dry run: the create, the role of the port that exists, the role
	// of the port to create named as such, the retype refused.
	out, err := rrcsStdout(t, func() error {
		return runRRCS(context.Background(), []string{"ensure", f.addr(), "--manifest", desired, "--check"})
	})
	if err == nil || !strings.Contains(err.Error(), "could not be brought") {
		t.Fatalf("a refused retype must fail the run: %v", err)
	}
	rrcsWant(t, out,
		"would change  net.1.node.61.port.1056.PortType", "(absent) -> 3",
		"would change  net.1.node.61.port.7.out.PortAes67Output.Channels", "1 -> 2",
		"would change  net.1.node.61.port.1056.in.PortAes67Input.Channels", "(after create) -> 8",
		"failed        net.1.node.61.port.1026.PortType", "retype RSP_1232HL -> FOUR_WIRE", "--allow-retype",
		"would change 3, failed 1, of the manifest's 4 port entries: create 1, retype 1, role rows 2, unchanged 1, not named 0")
	if n := len(sent()); n != 0 {
		t.Fatalf("--check sent %d requests", n)
	}
	// With the retype allowed, the dry run plans the delete and the create.
	out = rrcsRun(t, "ensure", f.addr(), "--manifest", desired, "--check", "--allow-retype")
	rrcsWant(t, out, "net.1.node.61.port.1026.PortType", "RSP_1232HL (delete) -> FOUR_WIRE (create)", "would change 4, failed 0")
	if n := len(sent()); n != 0 {
		t.Fatalf("--check --allow-retype sent %d requests", n)
	}

	// The write: the delete, the creates with their names, the role of
	// the existing port, then the roles of the created ports.
	out, err = rrcsStdout(t, func() error {
		return runRRCS(context.Background(), []string{"ensure", f.addr(), "--manifest", desired, "--allow-retype", "--write-to", f.addr()})
	})
	if err != nil {
		t.Fatalf("write: %v\n%s", err, out)
	}
	rrcsWant(t, out, "changed       net.1.node.61.port.1026.PortType", "RSP_1232HL (deleted) -> FOUR_WIRE",
		"changed       net.1.node.61.port.1056.PortType", "changed       net.1.node.61.port.1056.in.PortAes67Input.Channels", "1 -> 8",
		// the retype counts twice: its delete and its create
		"changed 5, failed 0, of the manifest's 4 port entries: create 2, retype 1")
	var kinds []string
	for _, c := range sent() {
		ct, _ := c.Field("ChangeType")
		sp, _ := c.Field("SpecificParams")
		addr, _ := sp.Field("PortAddress")
		port, _ := addr.Field("Port")
		kinds = append(kinds, ct.Str+":"+strconv.Itoa(int(port.Int)))
	}
	want := "delete:1026 create:1056 edit:1056 edit:1056 create:1026 edit:1026 edit:1026 edit:7 edit:1056 edit:1056"
	if got := strings.Join(kinds, " "); got != want {
		t.Errorf("requests\n got %s\nwant %s", got, want)
	}
	// The rows of the retyped port were saved; the inventory rewritten.
	saved, _ := filepath.Glob(filepath.Join(filepath.Dir(treeStore.BaseDir()), "snapshots", "rrcs", "127.0.0.1", "retype-61-1026-*.csv"))
	if len(saved) != 1 {
		t.Errorf("saved rows: %v", saved)
	} else if raw, _ := os.ReadFile(saved[0]); !strings.Contains(string(raw), "net.1.node.61.port.1026.LongName") {
		t.Errorf("saved rows lack the port's own rows:\n%s", raw)
	}
	if _, err := manifest.Load(filepath.Join(treeStore.BaseDir(), "manifest", "rrcs", "127.0.0.1.json")); err != nil {
		t.Errorf("inventory after the run: %v", err)
	}
	// A second run changes nothing.
	out = rrcsRun(t, "ensure", f.addr(), "--manifest", desired, "--check", "--allow-retype")
	rrcsWant(t, out, "would change 0, failed 0")
}

func TestRRCSEnsureManifestRefusals(t *testing.T) {
	f, _ := rrcsManifestFake(t)
	dir := t.TempDir()
	desired := rrcsDesiredManifest(t, dir)
	ctx := context.Background()
	for name, args := range map[string][]string{
		"both sources": {"ensure", f.addr(), "--file", desired, "--manifest", desired, "--check"},
		"no source":    {"ensure", f.addr(), "--check"},
		"write guard":  {"ensure", f.addr(), "--manifest", desired},
		"bad file":     {"ensure", f.addr(), "--manifest", filepath.Join(dir, "none.json"), "--check"},
	} {
		if err := runRRCS(ctx, args); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
