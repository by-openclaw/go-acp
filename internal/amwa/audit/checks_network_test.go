package audit

// #852 network-plane checks: each with a fixture that fails and one
// that passes. Addresses are documentation ranges only (233.252.0.0/24
// is itself one of the things detected, so it appears deliberately).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// netHarvest builds a harvest with connection /active endpoints and an
// optional node self + SDP set, for the network checks.
func netHarvest(senders map[string]activeEndpoint, self json.RawMessage, sdps map[string]string) *Harvest {
	h := &Harvest{Target: "198.51.100.99:3212", Label: "unit", APIs: map[string]API{}, SDP: map[string][]byte{}}
	conn := API{Versions: []string{"v1.1"}, Data: map[string]map[string]json.RawMessage{"v1.1": {}}}
	for id, e := range senders {
		b, _ := json.Marshal(e)
		conn.Data["v1.1"]["senders/"+id+"/active"] = b
	}
	h.APIs["connection"] = conn
	if self != nil {
		h.APIs["node"] = API{Versions: []string{"v1.3"}, Data: map[string]map[string]json.RawMessage{"v1.3": {"self": self}}}
	}
	for k, v := range sdps {
		h.SDP[k] = []byte(v)
	}
	return h
}

func sp(s string) *string { return &s }

func ep(dst, src string) activeEndpoint {
	on := true
	return activeEndpoint{
		MasterEnable:    &on,
		TransportParams: []transportParam{{DestinationIP: sp(dst), SourceIP: sp(src)}},
	}
}

func netCodes(fs []Finding) map[string]int {
	m := map[string]int{}
	for _, f := range fs {
		m[f.Code]++
	}
	return m
}

func TestNetworkMulticastRanges(t *testing.T) {
	h := netHarvest(map[string]activeEndpoint{
		"aaaa": ep("224.0.0.55", "192.168.1.10"),  // link-local control → ERROR
		"bbbb": ep("233.252.0.9", "192.168.1.11"), // doc range → WARN
		"cccc": ep("239.10.0.5", "192.168.1.12"),  // admin-scoped, clean
	}, nil, nil)
	by := netCodes(checkNetworkPlane(h, nil))
	if by["NMOS-NET-MCAST-LINKLOCAL"] != 1 {
		t.Errorf("link-local dst: got %d, want 1", by["NMOS-NET-MCAST-LINKLOCAL"])
	}
	if by["NMOS-NET-MCAST-DOCRANGE"] != 1 {
		t.Errorf("doc-range dst: got %d, want 1", by["NMOS-NET-MCAST-DOCRANGE"])
	}
	// No policy → the class check stands down as a SKIP, never a PASS.
	var classSkip bool
	for _, f := range checkNetworkPlane(h, nil) {
		if f.Code == "NMOS-NET-MCAST-CLASS" {
			if !hasPrefix(f.Detail, "SKIP:") {
				t.Errorf("class finding without policy must be a SKIP, got %q", f.Detail)
			}
			classSkip = true
		}
	}
	if !classSkip {
		t.Errorf("class check must report a SKIP without policy")
	}
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

func TestNetworkUnicastSource(t *testing.T) {
	// leg source_ip link-local (169.254) → ERROR; a clean sender passes.
	bad := netHarvest(map[string]activeEndpoint{
		"dddd": ep("239.10.0.6", "169.254.3.3"),
	}, nil, nil)
	if netCodes(checkNetworkPlane(bad, nil))["NMOS-NET-SRC-LINKLOCAL"] != 1 {
		t.Errorf("link-local source_ip not flagged")
	}
	good := netHarvest(map[string]activeEndpoint{
		"eeee": ep("239.10.0.7", "192.168.9.9"),
	}, nil, nil)
	if netCodes(checkNetworkPlane(good, nil))["NMOS-NET-SRC-INVALID"] != 0 {
		t.Errorf("clean unicast source flagged")
	}
}

func TestNetworkMACAndBindings(t *testing.T) {
	// Node declares eth0 with an uppercase colon MAC (wrong form) and a
	// sender bound to eth7 (undeclared).
	self := json.RawMessage(`{"id":"n1","interfaces":[{"name":"eth0","chassis_id":"AA:BB:CC:DD:EE:01","port_id":"aa-bb-cc-dd-ee-01"}]}`)
	h := netHarvest(map[string]activeEndpoint{}, self, nil)
	// The node API publishes /senders as one JSON array under "senders".
	h.APIs["node"].Data["v1.3"]["senders"] = json.RawMessage(`[{"id":"s1","interface_bindings":["eth7"]}]`)
	by := netCodes(checkNetworkPlane(h, nil))
	if by["NMOS-NET-MAC-FORM"] < 1 {
		t.Errorf("uppercase colon MAC not flagged: %v", by)
	}
	if by["NMOS-NET-BINDING-UNKNOWN"] != 1 {
		t.Errorf("binding to undeclared eth7 not flagged: %v", by)
	}
}

const sdpGM1 = "v=0\r\no=- 1 1 IN IP4 198.51.100.5\r\ns=x\r\nt=0 0\r\nm=video 5004 RTP/AVP 96\r\nc=IN IP4 233.252.0.9/64\r\na=rtpmap:96 raw/90000\r\na=ts-refclk:ptp=IEEE1588-2008:AA-BB-CC-FF-FE-00-00-01:0\r\n"
const sdpGM2 = "v=0\r\no=- 1 1 IN IP4 198.51.100.6\r\ns=y\r\nt=0 0\r\nm=video 5004 RTP/AVP 96\r\nc=IN IP4 233.252.0.10/64\r\na=rtpmap:96 raw/90000\r\na=ts-refclk:ptp=IEEE1588-2008:AA-BB-CC-FF-FE-00-00-02:0\r\n"

func TestNetworkPlantGrandmaster(t *testing.T) {
	// Two nodes, two different grandmasters → one plant-wide finding.
	a := netHarvest(nil, nil, map[string]string{"is05/aaaa.sdp": sdpGM1})
	b := netHarvest(nil, nil, map[string]string{"is05/bbbb.sdp": sdpGM2})
	fs := checkPlantGrandmaster([]*Harvest{a, b}, nil)
	if len(fs) != 1 || fs[0].Code != "NMOS-NET-PTP-MULTIPLE-GM" {
		t.Fatalf("two-GM plant = %+v, want one NMOS-NET-PTP-MULTIPLE-GM", fs)
	}
	// One GM plant-wide → no finding.
	c := netHarvest(nil, nil, map[string]string{"is05/cccc.sdp": sdpGM1})
	if fs := checkPlantGrandmaster([]*Harvest{a, c}, nil); len(fs) != 0 {
		t.Errorf("single-GM plant flagged: %+v", fs)
	}
}

func TestPolicyLoadAndClass(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	body := `{"multicast_classes":[{"range":"239.6.0.0/16","class":"uhd","max_bitrate_gbps":12}],
	          "expected_grandmaster":"aa-bb-cc-ff-fe-00-00-01","expected_domain":0}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	pol, err := LoadPolicy(path)
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}
	// A sender in the uhd range is classed at INFO.
	h := netHarvest(map[string]activeEndpoint{"ffff": ep("239.6.0.5", "192.168.1.20")}, nil, nil)
	by := netCodes(checkNetworkPlane(h, pol))
	if by["NMOS-NET-MCAST-CLASS"] != 1 {
		t.Errorf("policy class not applied: %v", by)
	}

	// A wrong-GM sender under policy → ERROR.
	hg := netHarvest(nil, nil, map[string]string{"is05/gggg.sdp": sdpGM2})
	if netCodes(checkNetworkPlane(hg, pol))["NMOS-NET-PTP-WRONG-GM"] != 1 {
		t.Errorf("wrong-GM under policy not flagged")
	}
}

func TestPolicyRejectsBadRange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(path, []byte(`{"multicast_classes":[{"range":"not-a-cidr","class":"x"}]}`), 0o644)
	if _, err := LoadPolicy(path); err == nil {
		t.Error("a malformed range must be a load error, not silently accepted")
	}
}

// TestMustCIDRPanicsOnBadBlock: the reserved blocks are compiled-in
// constants; a typo there is a programming error and must stop the
// process at init rather than silently disable a check.
func TestMustCIDRPanicsOnBadBlock(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("mustCIDR of a non-CIDR should panic")
		}
	}()
	mustCIDR("not-a-cidr")
}

// TestNetworkUnicastDestinationIsNotMulticast: a unicast destination
// falls in no multicast block and produces no range finding.
func TestNetworkUnicastDestinationIsNotMulticast(t *testing.T) {
	h := netHarvest(map[string]activeEndpoint{"aaaa": ep("198.51.100.7", "192.168.1.10")}, nil, nil)
	for _, f := range h.checkMulticastRanges(nil) {
		if f.Code != "NMOS-NET-MCAST-CLASS" {
			t.Errorf("a unicast destination produced %s: %s", f.Code, f.Detail)
		}
	}
}

const sdpWithFilter = "v=0\r\no=- 1 1 IN IP4 198.51.100.5\r\ns=ssm\r\nt=0 0\r\nm=video 5004 RTP/AVP 96\r\nc=IN IP4 232.1.1.2/64\r\na=source-filter: incl IN IP4 232.1.1.2 198.51.100.5\r\na=rtpmap:96 raw/90000\r\n"

// TestNetworkSSMNeedsSourceFilter (RFC 4607 / RFC 4570): a 232/8 group
// is joinable only with a source-filter in the SDP. The evidence is the
// sender's own SDP; another sender's SDP, an unparseable one, or one
// without the attribute does not count.
func TestNetworkSSMNeedsSourceFilter(t *testing.T) {
	h := netHarvest(map[string]activeEndpoint{
		"nosdp":    ep("232.1.1.1", "192.168.1.10"),
		"filtered": ep("232.1.1.2", "192.168.1.11"),
		"garbage":  ep("232.1.1.3", "192.168.1.12"),
		"plain":    ep("232.1.1.4", "192.168.1.13"),
	}, nil, map[string]string{
		"is05/filtered.sdp": sdpWithFilter,
		"is05/garbage.sdp":  "this is not an sdp\n",
		"is05/plain.sdp":    sdpGM1,
		"is05/other.sdp":    sdpWithFilter, // someone else's filter proves nothing
	})
	flagged := map[string]bool{}
	for _, f := range h.checkMulticastRanges(nil) {
		if f.Code == "NMOS-NET-SSM-NO-FILTER" {
			flagged[f.Resource] = true
		}
	}
	for _, id := range []string{"nosdp", "garbage", "plain"} {
		if !flagged["sender/"+id] {
			t.Errorf("sender %s has no usable source-filter and was not flagged: %v", id, flagged)
		}
	}
	if flagged["sender/filtered"] {
		t.Error("a sender whose SDP carries a=source-filter was flagged")
	}
}

// TestNetworkSourceIPForms walks each shape a source_ip can take that is
// not a real interface address.
func TestNetworkSourceIPForms(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string // "" = no source finding at all
		word string
	}{
		{"absent source is not judged", "", "", ""},
		{"not an address", "eth0", "NMOS-NET-SRC-INVALID", "not an IP address"},
		{"loopback is not an interface", "127.0.0.1", "NMOS-NET-SRC-INVALID", "not a unicast"},
		{"multicast cannot be a source", "239.1.1.1", "NMOS-NET-SRC-INVALID", "not a unicast"},
		{"unspecified is unset", "::", "NMOS-NET-SRC-INVALID", "not a unicast"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := netHarvest(map[string]activeEndpoint{"aaaa": ep("239.10.0.5", tc.src)}, nil, nil)
			fs := h.checkUnicastSourceIPs(nil)
			if tc.want == "" {
				for _, f := range fs {
					if f.Code != "NMOS-NET-SRC-SCOPE" {
						t.Errorf("unexpected %s: %s", f.Code, f.Detail)
					}
				}
				return
			}
			f := has(t, fs, tc.want)
			if !strings.Contains(f.Detail, tc.word) {
				t.Errorf("detail %q should say %q", f.Detail, tc.word)
			}
		})
	}
}

// TestNetworkMACEdgeCases: an interface with no MACs published is not a
// malformed MAC; a sender entry that is not an object is skipped; and a
// registry capture reads its senders from the query API.
func TestNetworkMACEdgeCases(t *testing.T) {
	self := json.RawMessage(`{"id":"n1","interfaces":[{"name":"eth0","chassis_id":"","port_id":""}]}`)
	h := netHarvest(nil, self, nil)
	h.APIs["node"].Data["v1.3"]["senders"] = json.RawMessage(`[{"id":"s1","interface_bindings":["eth0"]}, "junk"]`)
	if got := h.checkInterfaceMACs(); len(got) != 0 {
		t.Errorf("empty MACs and an unreadable sender produced %v", codeList(got))
	}

	reg := netHarvest(nil, self, nil)
	reg.APIs["query"] = API{Versions: []string{"v1.3"}, Data: map[string]map[string]json.RawMessage{
		"v1.3": {"senders": json.RawMessage(`[{"id":"s2","interface_bindings":["eth9"]}]`)},
	}}
	if got := nodeAPIFor(reg); got != "query" {
		t.Fatalf("nodeAPIFor a registry capture = %q, want query", got)
	}
	has(t, reg.checkInterfaceMACs(), "NMOS-NET-BINDING-UNKNOWN")
}

const (
	sdpNoClock   = "v=0\r\no=- 1 1 IN IP4 198.51.100.5\r\ns=x\r\nt=0 0\r\nm=video 5004 RTP/AVP 96\r\nc=IN IP4 233.252.0.9/64\r\na=rtpmap:96 raw/90000\r\n"
	sdpTraceable = "v=0\r\no=- 1 1 IN IP4 198.51.100.5\r\ns=x\r\nt=0 0\r\nm=video 5004 RTP/AVP 96\r\nc=IN IP4 233.252.0.9/64\r\na=rtpmap:96 raw/90000\r\na=ts-refclk:ptp=IEEE1588-2008:traceable\r\n"
	sdpBadGM     = "v=0\r\no=- 1 1 IN IP4 198.51.100.5\r\ns=x\r\nt=0 0\r\nm=video 5004 RTP/AVP 96\r\nc=IN IP4 233.252.0.9/64\r\na=rtpmap:96 raw/90000\r\na=ts-refclk:ptp=IEEE1588-2008:not-a-gmid:200\r\n"
	sdpBadGM2    = "v=0\r\no=- 1 1 IN IP4 198.51.100.5\r\ns=x\r\nt=0 0\r\nm=video 5004 RTP/AVP 96\r\nc=IN IP4 233.252.0.9/64\r\na=rtpmap:96 raw/90000\r\na=ts-refclk:ptp=IEEE1588-2008:zz-zz:5\r\n"
)

// TestSenderPTPForms: the grandmaster id must be EUI-64 and the domain
// 0-127 (IEEE 1588 §7.1); "traceable" and an absent clock are not
// judged; an unparseable SDP is the SDP check's finding, not this one's.
func TestSenderPTPForms(t *testing.T) {
	h := netHarvest(nil, nil, map[string]string{
		"is05/aaaa.sdp": "not an sdp\n",
		"is05/bbbb.sdp": sdpNoClock,
		"is05/cccc.sdp": sdpTraceable,
		"is05/dddd.sdp": sdpBadGM,
		"is05/eeee.sdp": sdpBadGM2,
	})
	fs := h.checkSenderPTP(nil)
	by := netCodes(fs)
	if by["NMOS-NET-PTP-GMID"] != 2 {
		t.Errorf("two malformed grandmaster ids, got %d findings: %v", by["NMOS-NET-PTP-GMID"], by)
	}
	if by["NMOS-NET-PTP-DOMAIN"] != 1 {
		t.Errorf("one domain out of range, got %d findings: %v", by["NMOS-NET-PTP-DOMAIN"], by)
	}
	for _, f := range fs {
		if f.Resource == "sender/aaaa" || f.Resource == "sender/bbbb" || f.Resource == "sender/cccc" {
			t.Errorf("a sender with no judgeable clock was flagged: %+v", f)
		}
	}
	// Findings are ordered by resource so two runs diff cleanly.
	for i := 1; i < len(fs); i++ {
		if fs[i].Resource < fs[i-1].Resource {
			t.Fatalf("PTP findings are not resource-ordered: %v", fs)
		}
	}
}

// TestPlantGrandmasterEdges: unparseable SDPs and SDPs without a PTP
// clock contribute no grandmaster; under a policy the multiple-GM
// finding also names the one the plant expects.
func TestPlantGrandmasterEdges(t *testing.T) {
	noise := netHarvest(nil, nil, map[string]string{
		"is05/aaaa.sdp": "not an sdp\n",
		"is05/bbbb.sdp": sdpNoClock,
		"is05/cccc.sdp": sdpTraceable,
	})
	if fs := checkPlantGrandmaster([]*Harvest{noise}, nil); len(fs) != 0 {
		t.Errorf("no grandmaster is referenced, yet %v", fs)
	}

	a := netHarvest(nil, nil, map[string]string{"is05/aaaa.sdp": sdpGM1})
	b := netHarvest(nil, nil, map[string]string{"is05/bbbb.sdp": sdpGM2})
	pol := &Policy{ExpectedGrandmaster: "aa-bb-cc-ff-fe-00-00-01"}
	fs := checkPlantGrandmaster([]*Harvest{a, b, noise}, pol)
	if len(fs) != 1 {
		t.Fatalf("want one multiple-GM finding, got %v", fs)
	}
	if !strings.Contains(fs[0].Hint, "policy expects aa-bb-cc-ff-fe-00-00-01") {
		t.Errorf("under a policy the hint should name the expected GM: %q", fs[0].Hint)
	}
}
