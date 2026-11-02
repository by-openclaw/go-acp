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
	_ = c.Add("PortAes67Output.Multicast", codec.String("239.1.2.3"))
	_ = c.Add("PortAes67Output.MulticastPort", codec.Int(5004))
	_ = c.Add("Alias", codec.String("X"))
	doc, err := codec.EncodeCall("ConfigurationChange", codec.String("C0000000001"), c.Request())
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
	if addr := both.Address[0].Value; len(addr.Members) != 2 || addr.Members[0].Name != "Node" {
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
	_ = c.Add("Ptp.PTP", codec.Int(100))
	_ = c.Add("Nmos.RegistrationIp", codec.String("10.44.55.15"))
	_ = c.Add("Media_1.DefaultGateway", codec.String("10.5.10.185"))
	p := c.Params()
	names := make([]string, 0, len(p.Members))
	for _, m := range p.Members {
		names = append(names, m.Name)
	}
	if strings.Join(names, ",") != "Node,Slot,Nmos,Aes67" || c.ObjectType != "client-card" {
		t.Fatalf("members %v of %s", names, c.ObjectType)
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
	if err := c.Add("Ptp.PTP", codec.Int(1)); err == nil {
		t.Error("a property given twice was accepted")
	}
	if err := c.Add("A.B.C", codec.Int(1)); err == nil {
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

// A write names its target twice or does not happen.
func TestRRCSWriteGuard(t *testing.T) {
	f, changes := rrcsSetFake(t, true)
	ctx := context.Background()
	var val *consumer.ValidationError
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
			if err := c.Add(name, rrcsPropValue(value)); err != nil {
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
		if got := tc.c.NMOSStream(tc.current) != ""; got != tc.refused {
			t.Errorf("%s: refused %v, want %v", name, got, tc.refused)
		}
	}
	card, _ := rrcsChangeFor("net.1.node.60.card.1")
	_ = card.Add("Ptp.PTP", rrcsPropValue("101"))
	if card.NMOSStream(nmos) != "" {
		t.Error("a client card edit was taken for a stream edit")
	}
}

// A link (Mode and Selection alone) is let through on an NMOS stream;
// anything beside them is a stream edit again.
func TestRRCSLinkIsNotAStreamEdit(t *testing.T) {
	nmos := map[string]any{"PortAes67Input": map[string]any{"Protocol": float64(5)}}
	c, _ := rrcsChangeFor("net.1.node.63.port.1073.in")
	_ = c.Add("PortAes67Input.Mode", rrcsPropValue("1072"))
	_ = c.Add("PortAes67Input.Selection", rrcsPropValue("2"))
	if c.NMOSStream(nmos) != "" {
		t.Error("a link was refused")
	}
	doc, err := codec.EncodeCall("ConfigurationChange", codec.String("C0000000001"), c.Request())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<name>IsInput</name><value><boolean>1</boolean>", "<name>Port</name><value><int>1073</int>",
		"<name>Mode</name><value><int>1072</int>", "<name>Selection</name><value><int>2</int>"} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("request lacks %s:\n%s", want, doc)
		}
	}
	_ = c.Add("PortAes67Input.Multicast", rrcsPropValue("239.1.1.1"))
	if c.NMOSStream(nmos) == "" {
		t.Error("a link with an address was let through")
	}
}
