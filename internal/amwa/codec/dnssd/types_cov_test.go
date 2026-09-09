package dnssd

import (
	"net"
	"strings"
	"testing"
)

// fullInstance carries every optional field so the announce walks all
// of its record loops.
func fullInstance() Instance {
	return Instance{
		Name:    "dhs registry.1", // a dot inside the label: RFC 6763 §4.3
		Service: "_nmos-register._tcp",
		Host:    "registry.local.",
		Port:    8235,
		IPv4:    []net.IP{net.ParseIP("10.6.239.113"), net.ParseIP("2001:db8::1")},
		IPv6:    []net.IP{net.ParseIP("2001:db8::1"), net.ParseIP("10.6.239.113")},
		TXT:     map[string]string{TXTKeyAPIVer: "v1.3", TXTKeyPriority: "100"},
	}
}

// An instance names itself the same way on the wire whichever domain
// it was given, and a dot inside the instance label is escaped rather
// than becoming a name separator.
func TestInstanceNames(t *testing.T) {
	i := fullInstance()
	if got := i.FullName(); got != `dhs registry\.1._nmos-register._tcp.local` {
		t.Errorf("FullName = %q", got)
	}
	if got := i.PTRName(); got != "_nmos-register._tcp.local" {
		t.Errorf("PTRName = %q", got)
	}

	i.Domain = "example.arpa"
	if got := i.PTRName(); got != "_nmos-register._tcp.example.arpa" {
		t.Errorf("PTRName in a unicast zone = %q", got)
	}
	if !strings.HasSuffix(i.FullName(), "._nmos-register._tcp.example.arpa") {
		t.Errorf("FullName in a unicast zone = %q", i.FullName())
	}
}

// An announcement carries the whole record set, with the cache-flush
// bit on the unique records and never on the shared PTR; a goodbye is
// the same set at TTL 0, which is what makes peers evict.
func TestEncodeAnnounceAndGoodbye(t *testing.T) {
	i := fullInstance()

	for name, tc := range map[string]struct {
		encode func(Instance, bool) ([]byte, error)
		ttl    uint32
	}{
		"an announcement": {EncodeAnnounce, DefaultAnnounceTTL},
		"a goodbye":       {EncodeGoodbye, 0},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := tc.encode(i, true)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			msg, err := Decode(raw)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !msg.Header.IsResponse() || msg.Header.Flags&flagAA == 0 {
				t.Error("an unsolicited announcement is an authoritative response")
			}
			byType := map[uint16][]RR{}
			for _, rr := range msg.Answers {
				byType[rr.Type] = append(byType[rr.Type], rr)
				if rr.TTL != tc.ttl {
					t.Errorf("%d record TTL = %d, want %d", rr.Type, rr.TTL, tc.ttl)
				}
			}
			// One IPv4 and one IPv6 survive: the mismatched addresses
			// in the fixture are skipped, not mis-encoded.
			for typ, want := range map[uint16]int{
				TypePTR: 1, TypeSRV: 1, TypeTXT: 1, TypeA: 1, TypeAAAA: 1,
			} {
				if len(byType[typ]) != want {
					t.Errorf("type %d records = %d, want %d", typ, len(byType[typ]), want)
				}
			}
			if byType[TypePTR][0].Class&ClassFlushBit != 0 {
				t.Error("the shared PTR must not carry the cache-flush bit")
			}
			for _, typ := range []uint16{TypeSRV, TypeTXT, TypeA, TypeAAAA} {
				if byType[typ][0].Class&ClassFlushBit == 0 {
					t.Errorf("the unique record %d must carry the cache-flush bit", typ)
				}
			}
			if srv := byType[TypeSRV][0].SRV; srv == nil || srv.Port != 8235 || srv.Target != "registry.local" {
				t.Errorf("SRV = %+v, want the trailing dot trimmed", srv)
			}
		})
	}

	// An instance missing any of the four required fields cannot be
	// advertised at all.
	for name, mutate := range map[string]func(*Instance){
		"no name":    func(i *Instance) { i.Name = "" },
		"no service": func(i *Instance) { i.Service = "" },
		"no host":    func(i *Instance) { i.Host = "" },
		"no port":    func(i *Instance) { i.Port = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := fullInstance()
			mutate(&bad)
			if _, err := EncodeAnnounce(bad, true); err == nil {
				t.Error("EncodeAnnounce accepted it")
			}
			if _, err := EncodeGoodbye(bad, true); err == nil {
				t.Error("EncodeGoodbye accepted it")
			}
		})
	}

	// A TXT key the encoder refuses fails the whole announcement
	// rather than shipping an instance with no TXT.
	badTXT := fullInstance()
	badTXT.TXT = map[string]string{"a=b": "x"}
	if _, err := EncodeAnnounce(badTXT, true); err == nil {
		t.Error("EncodeAnnounce accepted an unencodable TXT key")
	}
	if _, err := EncodeGoodbye(badTXT, true); err == nil {
		t.Error("EncodeGoodbye accepted an unencodable TXT key")
	}

	// An explicit TTL is honoured by the announce path.
	timed := fullInstance()
	timed.TTL = 30
	raw, err := EncodeAnnounce(timed, false)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.IsResponse() {
		t.Error("asResponse=false must leave QR clear")
	}
	if msg.Answers[0].TTL != 30 {
		t.Errorf("TTL = %d, want the operator's own", msg.Answers[0].TTL)
	}
}

// A browse reply is read back into instances: the SRV is what makes
// one, its TTL comes from the PTR that named it, and records for
// another service are not ours to report.
func TestDecodeInstances(t *testing.T) {
	if got := DecodeInstances(nil, ""); got != nil {
		t.Errorf("no message = %v, want no instances", got)
	}

	raw, err := EncodeAnnounce(fullInstance(), true)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	got := DecodeInstances(msg, "_nmos-register._tcp")
	if len(got) != 1 {
		t.Fatalf("instances = %+v, want the one announced", got)
	}
	ins := got[0]
	if ins.Name != "dhs registry.1" {
		t.Errorf("name = %q, want the escape reversed", ins.Name)
	}
	if ins.Service != "_nmos-register._tcp" || ins.Domain != "local" {
		t.Errorf("service/domain = %q / %q", ins.Service, ins.Domain)
	}
	if ins.Host != "registry.local" || ins.Port != 8235 {
		t.Errorf("target = %s:%d", ins.Host, ins.Port)
	}
	if len(ins.IPv4) != 1 || len(ins.IPv6) != 1 {
		t.Errorf("addresses = %v / %v", ins.IPv4, ins.IPv6)
	}
	if ins.TXT[TXTKeyAPIVer] != "v1.3" || ins.TTL != DefaultAnnounceTTL {
		t.Errorf("TXT = %v, TTL = %d", ins.TXT, ins.TTL)
	}

	// Filtered by another service, the same packet yields nothing.
	if got := DecodeInstances(msg, "_nmos-query._tcp"); len(got) != 0 {
		t.Errorf("another service's browse = %+v, want nothing", got)
	}

	// A goodbye keeps TTL 0 — that zero is the eviction signal, so it
	// must survive the decode.
	raw, err = EncodeGoodbye(fullInstance(), true)
	if err != nil {
		t.Fatal(err)
	}
	msg, err = Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	got = DecodeInstances(msg, "")
	if len(got) != 1 || got[0].TTL != 0 {
		t.Errorf("goodbye = %+v, want TTL 0 preserved", got)
	}

	// Records in the authority and additional sections count too, and
	// an SRV with no PTR takes its own TTL.
	split := &Message{
		Authority: []RR{{
			Name: "solo._nmos-query._tcp.local", Type: TypeSRV, TTL: 45,
			SRV: &SRVData{Port: 8235, Target: "host.local"},
		}},
		Additional: []RR{{
			Name: "solo._nmos-query._tcp.local", Type: TypeTXT, TXT: []string{"pri=0"},
		}},
	}
	got = DecodeInstances(split, "")
	if len(got) != 1 || got[0].TTL != 45 || got[0].TXT["pri"] != "0" {
		t.Errorf("split-section instance = %+v", got)
	}

	// A TXT with no SRV names no reachable instance.
	orphan := &Message{Answers: []RR{{
		Name: "orphan._nmos-query._tcp.local", Type: TypeTXT, TXT: []string{"pri=0"},
	}}}
	if got := DecodeInstances(orphan, ""); len(got) != 0 {
		t.Errorf("a TXT with no SRV = %+v, want nothing", got)
	}

	// A PTR for another service in the same packet is skipped.
	mixed := &Message{Answers: []RR{
		{Name: "_nmos-node._tcp.local", Type: TypePTR, TTL: 10, PTR: "other._nmos-node._tcp.local"},
		{Name: "one._nmos-query._tcp.local", Type: TypeSRV, TTL: 20,
			SRV: &SRVData{Port: 1, Target: "h.local"}},
	}}
	got = DecodeInstances(mixed, "_nmos-query._tcp")
	if len(got) != 1 || got[0].Service != "_nmos-query._tcp" {
		t.Errorf("mixed packet = %+v", got)
	}
}

// A name that does not carry a _tcp/_udp anchor is not a service
// instance name — it is reported as a bare domain rather than being
// split at an arbitrary dot.
func TestSplitFullName(t *testing.T) {
	for full, want := range map[string][3]string{
		"one._nmos-query._tcp.local":  {"one", "_nmos-query._tcp", "local"},
		"a.b._nmos-query._udp.local.": {"a.b", "_nmos-query._udp", "local"},
		"just.a.hostname":             {"", "", "just.a.hostname"},
		"_tcp.local":                  {"", "", "_tcp.local"},
		"nounderscore._tcp.local":     {"", "", "nounderscore._tcp.local"},
	} {
		inst, svc, dom := splitFullName(full)
		if [3]string{inst, svc, dom} != want {
			t.Errorf("splitFullName(%q) = %q / %q / %q, want %v", full, inst, svc, dom, want)
		}
	}

	// The escape round-trips, including a trailing backslash the
	// sender left dangling.
	for _, label := range []string{"plain", "with.dot", `with\backslash`, `trailing\`} {
		if got := unescapeInstanceLabel(escapeInstanceLabel(label)); got != label {
			t.Errorf("round-tripping %q gave %q", label, got)
		}
	}
}
