package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/rrcs/codec"
)

func TestRRCSPropValue(t *testing.T) {
	tests := map[string]codec.Value{
		"true":       codec.Bool(true),
		"false":      codec.Bool(false),
		"5004":       codec.Int(5004),
		"-3":         codec.Int(-3),
		"239.1.2.3":  codec.String("239.1.2.3"),
		`"5004"`:     codec.String("5004"),
		"":           codec.String(""),
		"9999999999": codec.String("9999999999"),
	}
	for in, want := range tests {
		if got := rrcsPropValue(in); !reflect.DeepEqual(got, want) {
			t.Errorf("rrcsPropValue(%q) = %+v, want %+v", in, got, want)
		}
	}
}

// What leaves for a sender on a port that shares its number with an
// input: the §8.10.1 envelope, the §8.10.4.6 address with IsInput, the
// block of the stream.
func TestRRCSChangeRequestPort(t *testing.T) {
	c, err := rrcsChangeFor("net.1.node.61.port.7.out")
	if err != nil {
		t.Fatal(err)
	}
	_ = c.add("PortAes67Output.Multicast", codec.String("239.1.2.3"))
	_ = c.add("PortAes67Output.MulticastPort", codec.Int(5004))
	_ = c.add("Alias", codec.String("X"))
	doc, err := codec.EncodeCall("ConfigurationChange", codec.String("C0000000001"), c.request())
	if err != nil {
		t.Fatal(err)
	}
	want := `<?xml version="1.0"?><methodCall><methodName>ConfigurationChange</methodName><params>` +
		`<param><value><string>C0000000001</string></value></param>` +
		`<param><value><array><data><value><struct>` +
		`<member><name>ChangeType</name><value><string>edit</string></value></member>` +
		`<member><name>ObjectType</name><value><string>portex</string></value></member>` +
		`<member><name>SpecificParams</name><value><struct>` +
		`<member><name>PortAddress</name><value><struct>` +
		`<member><name>IsInput</name><value><boolean>0</boolean></value></member>` +
		`<member><name>Node</name><value><int>61</int></value></member>` +
		`<member><name>Port</name><value><int>7</int></value></member>` +
		`</struct></value></member>` +
		`<member><name>PortAes67Output</name><value><struct>` +
		`<member><name>Multicast</name><value><string>239.1.2.3</string></value></member>` +
		`<member><name>MulticastPort</name><value><int>5004</int></value></member>` +
		`</struct></value></member>` +
		`<member><name>Alias</name><value><string>X</string></value></member>` +
		`</struct></value></member>` +
		`</struct></value></data></array></value></param></params></methodCall>`
	if string(doc) != want {
		t.Errorf("got  %s\nwant %s", doc, want)
	}
	// A port with both directions carries no IsInput.
	both, _ := rrcsChangeFor("net.1.node.61.port.1026")
	if addr := both.address[0].Value; len(addr.Members) != 2 || addr.Members[0].Name != "Node" {
		t.Errorf("address of a two-way port: %+v", addr)
	}
}

// A client card: Node and Slot, the PTP and media blocks inside Aes67,
// Nmos beside it (§8.10.4.34).
func TestRRCSChangeRequestCard(t *testing.T) {
	c, err := rrcsChangeFor("net.1.node.60.card.4")
	if err != nil {
		t.Fatal(err)
	}
	_ = c.add("Ptp.PTP", codec.Int(100))
	_ = c.add("Nmos.RegistrationIp", codec.String("10.44.55.15"))
	_ = c.add("Media_1.DefaultGateway", codec.String("10.5.10.185"))
	p := c.params()
	names := make([]string, 0, len(p.Members))
	for _, m := range p.Members {
		names = append(names, m.Name)
	}
	if strings.Join(names, ",") != "Node,Slot,Nmos,Aes67" || c.objectType != "client-card" {
		t.Fatalf("members %v of %s", names, c.objectType)
	}
	if slot, _ := p.Field("Slot"); slot.Int != 4 {
		t.Errorf("Slot = %d", slot.Int)
	}
	aes, _ := p.Field("Aes67")
	ptp, ok1 := aes.Field("Ptp")
	_, ok2 := aes.Field("Media_1")
	if dom, _ := ptp.Field("PTP"); !ok1 || !ok2 || dom.Int != 100 {
		t.Errorf("Aes67 = %+v", aes)
	}
	if err := c.add("Ptp.PTP", codec.Int(1)); err == nil {
		t.Error("a property given twice was accepted")
	}
	if err := c.add("A.B.C", codec.Int(1)); err == nil {
		t.Error("a three-level name was accepted")
	}
}

func TestRRCSChangeForRefusals(t *testing.T) {
	for _, bad := range []string{"group.200", "net.1.node.61", "net.1.node.61.port.7.sideways",
		"net.1.node.61.port.1026.key.0.1.1", "net.1.node.x.card.1", "net.1.node.60.card.y"} {
		if _, err := rrcsChangeFor(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// rrcsSetFake is the tree stand-in with a sender whose multicast address
// changes when a ConfigurationChangeEx says so, the way a gateway that
// applies a change answers afterwards.
func rrcsSetFake(t *testing.T, obey bool) (*rrcsFake, *[]codec.Call) {
	t.Helper()
	var mu sync.Mutex
	var changes []codec.Call
	applied := ""
	appliedPort := int32(-1)
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		mu.Lock()
		defer mu.Unlock()
		switch call.Method {
		case "ConfigurationChange":
			changes = append(changes, call)
			sp, _ := call.Params[1].Items[0].Field("SpecificParams")
			if out, ok := sp.Field("PortAes67Output"); ok && obey {
				if m, ok := out.Field("Multicast"); ok {
					applied = m.Str
					addr, _ := sp.Field("PortAddress")
					port, _ := addr.Field("Port")
					appliedPort = port.Int
				}
			}
			return call.Params[0], true
		case "GetAllPorts":
			v, ok := rrcsTreeAnswer(call)
			if applied != "" {
				for i, p := range v.Items[1].Items {
					out, has := p.Field("PortAes67Output")
					if number, _ := p.Field("Port"); !has || number.Int != appliedPort {
						continue
					}
					for j, m := range out.Members {
						if m.Name == "Multicast" {
							out.Members[j].Value = codec.String(applied)
						}
					}
					_ = i
				}
			}
			return v, ok
		}
		return rrcsTreeAnswer(call)
	})
	return f, &changes
}

func TestRRCSSetShowsAndSendsNothing(t *testing.T) {
	f, changes := rrcsSetFake(t, true)
	out := rrcsRun(t, "set", f.addr(), "--path", "net.1.node.61.port.7.out",
		"--prop", "PortAes67Output.Multicast=239.9.9.9", "--prop", "PortAes67Output.MulticastPort=5006")
	rrcsWant(t, out,
		"net.1.node.61.port.7.out  portex  Out seven",
		"PortAes67Output.Multicast          239.1.2.3              wanted \"239.9.9.9\"",
		"PortAes67Output.MulticastPort      5004                   wanted 5006",
		"<methodName>ConfigurationChange</methodName>",
		"nothing sent")
	if len(*changes) != 0 {
		t.Errorf("a change was sent without --apply yes")
	}
}

func TestRRCSSetApplies(t *testing.T) {
	f, changes := rrcsSetFake(t, true)
	out := rrcsRun(t, "set", f.addr(), "--path", "net.1.node.61.port.7.out",
		"--prop", "PortAes67Output.Multicast=239.9.9.9", "--apply", "yes", "--write-to", f.addr())
	rrcsWant(t, out, "read back:", "PortAes67Output.Multicast          239.9.9.9", "done: every wanted value reads back")
	if len(*changes) != 1 {
		t.Fatalf("%d changes sent, want 1", len(*changes))
	}
	change := (*changes)[0].Params[1].Items[0]
	if typ, _ := change.Field("ChangeType"); typ.Str != "edit" {
		t.Errorf("change %+v", change)
	}
}

// RRCS says yes and the value does not move: that is a failure.
func TestRRCSSetNotTaken(t *testing.T) {
	f, _ := rrcsSetFake(t, false)
	_, err := rrcsStdout(t, func() error {
		return runRRCS(context.Background(), []string{"set", f.addr(), "--path", "net.1.node.61.port.7.out",
			"--prop", "PortAes67Output.Multicast=239.9.9.9", "--apply", "yes", "--write-to", f.addr()})
	})
	if err == nil || !strings.Contains(err.Error(), "still differ: PortAes67Output.Multicast") {
		t.Errorf("got %v", err)
	}
}

func TestRRCSSetRefusals(t *testing.T) {
	ctx := context.Background()
	var val *consumer.ValidationError
	for name, args := range map[string][]string{
		"no host":     {"set", "--path", "net.1.node.61.port.7.out", "--prop", "Alias=x"},
		"no path":     {"set", "h", "--prop", "Alias=x"},
		"no prop":     {"set", "h", "--path", "net.1.node.61.port.7.out"},
		"bad apply":   {"set", "h", "--path", "net.1.node.61.port.7.out", "--prop", "Alias=x", "--apply", "maybe"},
		"bad path":    {"set", "h", "--path", "group.200", "--prop", "Label=x"},
		"prop no =":   {"set", "h", "--path", "net.1.node.61.port.7.out", "--prop", "Alias"},
		"prop twice":  {"set", "h", "--path", "net.1.node.61.port.7.out", "--prop", "Alias=x", "--prop", "Alias=y"},
		"prop 3 deep": {"set", "h", "--path", "net.1.node.61.port.7.out", "--prop", "A.B.C=1"},
	} {
		if err := runRRCS(ctx, args); !errors.As(err, &val) {
			t.Errorf("%s: got %v, want a validation error", name, err)
		}
	}
	f, changes := rrcsSetFake(t, true)
	if err := runRRCS(ctx, []string{"set", f.addr(), "--path", "net.1.node.61.port.999", "--prop", "Alias=x", "--apply", "yes", "--write-to", f.addr()}); err == nil || len(*changes) != 0 {
		t.Errorf("a port that does not exist: %v, %d changes sent", err, len(*changes))
	}
	// A gateway that refuses the change.
	refuse := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		if call.Method == "ConfigurationChange" {
			return codec.Value{}, false
		}
		return rrcsTreeAnswer(call)
	})
	_, err := rrcsStdout(t, func() error {
		return runRRCS(ctx, []string{"set", refuse.addr(), "--path", "net.1.node.60.card.1", "--prop", "Ptp.PTP=100", "--apply", "yes", "--write-to", refuse.addr()})
	})
	var fault *codec.Fault
	if !errors.As(err, &fault) {
		t.Errorf("refused change: %v", err)
	}
}

// A write names its target twice or does not happen.
func TestRRCSWriteGuard(t *testing.T) {
	f, changes := rrcsSetFake(t, true)
	ctx := context.Background()
	var val *consumer.ValidationError
	set := []string{"set", f.addr(), "--path", "net.1.node.61.port.7.out", "--prop", "PortAes67Output.Multicast=239.9.9.9", "--apply", "yes"}
	if err := runRRCS(ctx, set); !errors.As(err, &val) || !strings.Contains(err.Error(), "--write-to "+f.addr()) {
		t.Errorf("set without --write-to: %v", err)
	}
	if err := runRRCS(ctx, append(set, "--write-to", "10.12.0.12")); !errors.As(err, &val) || !strings.Contains(err.Error(), "does not match") {
		t.Errorf("set with another host: %v", err)
	}
	file := filepath.Join(t.TempDir(), "x.csv")
	_ = os.WriteFile(file, []byte("path,value\nnet.1.node.61.port.7.out.PortAes67Output.Multicast,239.9.9.9\n"), 0o644)
	if err := runRRCS(ctx, []string{"import", f.addr(), "--file", file}); !errors.As(err, &val) {
		t.Errorf("import without --write-to: %v", err)
	}
	if len(f.methods()) != 0 || len(*changes) != 0 {
		t.Errorf("a refused write reached the gateway: %v", f.methods())
	}
	// The dry runs need no guard.
	rrcsWant(t, rrcsRun(t, "import", f.addr(), "--file", file, "--dry-run"), "would apply 1,")
}

// An alias is read with a request of its own; set shows it and reads it
// back.
func TestRRCSSetAlias(t *testing.T) {
	alias := "OLD"
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		k := call.Params[0]
		switch call.Method {
		case "GetPortAlias":
			return codec.Array(k, codec.Int(0), codec.String(alias)), true
		case "GetPortLabel":
			return codec.Array(k, codec.Int(0), codec.String("LBL")), true
		case "GetInputGain", "GetOutputGain", "GetAllKeyConfiguration":
			return codec.Array(k, codec.Int(0), codec.Int(0)), true
		case "ConfigurationChange":
			sp, _ := call.Params[1].Items[0].Field("SpecificParams")
			if a, ok := sp.Field("Alias"); ok {
				alias = a.Str
			}
			return k, true
		}
		return rrcsTreeAnswer(call)
	})
	out := rrcsRun(t, "set", f.addr(), "--path", "net.1.node.61.port.1026", "--prop", "Alias=TEST1", "--apply", "yes", "--write-to", f.addr())
	rrcsWant(t, out, "Alias                              OLD", "done: every wanted value reads back")
	// Emptying it, the way a shell passes an empty value.
	rrcsWant(t, rrcsRun(t, "set", f.addr(), "--path", "net.1.node.61.port.1026", "--prop", "Alias=", "--apply", "yes", "--write-to", f.addr()),
		"done: every wanted value reads back")
	// Only the addressed port is asked for its values.
	asked := 0
	for _, m := range f.methods() {
		if m == "GetPortAlias" {
			asked++
		}
	}
	if asked != 4 {
		t.Errorf("GetPortAlias sent %d times, want 4 (before and after, twice)", asked)
	}
}

// An edit that gives stream fields to a stream that is, or becomes, NMOS
// stopped a production RRCS twice: it is never sent.
func TestRRCSNMOSStreamGuard(t *testing.T) {
	change := func(props ...string) *rrcsChange {
		c, err := rrcsChangeFor("net.1.node.63.port.1064.out")
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range props {
			name, value, _ := strings.Cut(p, "=")
			if err := c.add(name, rrcsPropValue(value)); err != nil {
				t.Fatal(err)
			}
		}
		return c
	}
	nmos := map[string]any{"PortAes67Output": map[string]any{"Protocol": float64(5)}}
	manual := map[string]any{"PortAes67Output": map[string]any{"Protocol": float64(2)}}
	for name, tc := range map[string]struct {
		c       *rrcsChange
		current map[string]any
		refused bool
	}{
		"address on an NMOS sender (2026-10-06)":  {change("PortAes67Output.Multicast=239.1.1.1"), nmos, true},
		"manual back to NMOS (2026-10-08)":        {change("PortAes67Output.Protocol=5", "PortAes67Output.Multicast=0.0.0.0"), manual, true},
		"NMOS to manual with an address (passed)": {change("PortAes67Output.Protocol=2", "PortAes67Output.Multicast=239.1.1.64"), nmos, false},
		"address on a manual sender":              {change("PortAes67Output.Multicast=239.1.1.1"), manual, false},
		"a label on an NMOS sender":               {change("Label=X"), nmos, false},
	} {
		if got := tc.c.nmosStream(tc.current) != ""; got != tc.refused {
			t.Errorf("%s: refused %v, want %v", name, got, tc.refused)
		}
	}
	card, _ := rrcsChangeFor("net.1.node.60.card.1")
	_ = card.add("Ptp.PTP", rrcsPropValue("101"))
	if card.nmosStream(nmos) != "" {
		t.Error("a client card edit was taken for a stream edit")
	}
}

// A link (Mode and Selection alone) is let through on an NMOS stream;
// anything beside them is a stream edit again.
func TestRRCSLinkIsNotAStreamEdit(t *testing.T) {
	nmos := map[string]any{"PortAes67Input": map[string]any{"Protocol": float64(5)}}
	c, _ := rrcsChangeFor("net.1.node.63.port.1073.in")
	_ = c.add("PortAes67Input.Mode", rrcsPropValue("1072"))
	_ = c.add("PortAes67Input.Selection", rrcsPropValue("2"))
	if c.nmosStream(nmos) != "" {
		t.Error("a link was refused")
	}
	doc, err := codec.EncodeCall("ConfigurationChange", codec.String("C0000000001"), c.request())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<name>IsInput</name><value><boolean>1</boolean>", "<name>Port</name><value><int>1073</int>",
		"<name>Mode</name><value><int>1072</int>", "<name>Selection</name><value><int>2</int>"} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("request lacks %s:\n%s", want, doc)
		}
	}
	_ = c.add("PortAes67Input.Multicast", rrcsPropValue("239.1.1.1"))
	if c.nmosStream(nmos) == "" {
		t.Error("a link with an address was let through")
	}
}

// One request reads the object a set is about; the lists are asked only
// when RRCS does not answer it.
func TestRRCSCurrentOne(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		mu.Lock()
		methods = append(methods, call.Method)
		mu.Unlock()
		k := call.Params[0]
		if call.Method == "GetPort" {
			// §8.9: net, node, port, is input, pool port.
			if len(call.Params) != 6 || call.Params[2].Int != 63 || call.Params[3].Int != 1073 || !call.Params[4].Bool || call.Params[5].Int != -1 {
				return codec.Array(k, codec.Int(4)), true
			}
			return codec.Array(k, codec.Int(0), codec.Struct(
				rrcsMember("LongName", codec.String("In. -7.50")),
				rrcsMember("PortAes67Input", codec.Struct(rrcsMember("Protocol", codec.Int(5)), rrcsMember("Selection", codec.Int(2)))))), true
		}
		return rrcsTreeAnswer(call)
	})
	out := rrcsRun(t, "set", f.addr(), "--path", "net.1.node.63.port.1073.in", "--prop", "PortAes67Input.Selection=1")
	rrcsWant(t, out, "In. -7.50", "PortAes67Input.Selection           2                      wanted 1", "nothing sent")
	mu.Lock()
	defer mu.Unlock()
	for _, m := range methods {
		if m == "GetAllPorts" {
			t.Errorf("the lists were read although GetPort answered: %v", methods)
		}
	}
	// The types are those of a snapshot: the guard still reads the protocol.
	mu.Unlock()
	guarded := rrcsRun(t, "set", f.addr(), "--path", "net.1.node.63.port.1073.in", "--prop", "PortAes67Input.Multicast=239.1.1.1")
	mu.Lock()
	rrcsWant(t, guarded, "PortAes67Input: refused: this edit gives stream fields")
}

// --nmos-test lets the mode alone through, and nothing with an address.
func TestRRCSSetNMOSTest(t *testing.T) {
	var sent []codec.Value
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		k := call.Params[0]
		switch call.Method {
		case "GetPort":
			return codec.Array(k, codec.Int(0), codec.Struct(rrcsMember("LongName", codec.String("Out. -7.41")),
				rrcsMember("PortAes67Output", codec.Struct(rrcsMember("Protocol", codec.Int(2)), rrcsMember("Multicast", codec.String("239.1.1.64")))))), true
		case "ConfigurationChange":
			sent = append(sent, call.Params[1])
			return k, true
		}
		return rrcsTreeAnswer(call)
	})
	path := "net.1.node.63.port.1064.out"
	var val *consumer.ValidationError
	// Without the flag: refused.
	if err := runRRCS(context.Background(), []string{"set", f.addr(), "--path", path, "--prop", "PortAes67Output.Protocol=5", "--apply", "yes", "--write-to", f.addr()}); !errors.As(err, &val) {
		t.Errorf("without --nmos-test: %v", err)
	}
	// With the flag and an address: refused.
	if err := runRRCS(context.Background(), []string{"set", f.addr(), "--path", path, "--prop", "PortAes67Output.Protocol=5",
		"--prop", "PortAes67Output.Multicast=0.0.0.0", "--nmos-test", "yes", "--apply", "yes", "--write-to", f.addr()}); !errors.As(err, &val) {
		t.Errorf("with an address: %v", err)
	}
	if len(sent) != 0 {
		t.Fatalf("%d requests sent by refused edits", len(sent))
	}
	// With the flag and the mode alone: sent, with one field in the block.
	_, _ = rrcsStdout(t, func() error {
		return runRRCS(context.Background(), []string{"set", f.addr(), "--path", path, "--prop", "PortAes67Output.Protocol=5", "--nmos-test", "yes", "--apply", "yes", "--write-to", f.addr()})
	})
	if len(sent) != 1 {
		t.Fatalf("%d requests sent, want 1", len(sent))
	}
	sp, _ := sent[0].Items[0].Field("SpecificParams")
	out, _ := sp.Field("PortAes67Output")
	if len(out.Members) != 1 || out.Members[0].Name != "Protocol" || out.Members[0].Value.Int != 5 {
		t.Errorf("request: %s", rrcsCompact(sp))
	}
}
