package audit

// #852 network-plane checks: each with a fixture that fails and one
// that passes. Addresses are documentation ranges only (233.252.0.0/24
// is itself one of the things detected, so it appears deliberately).

import (
	"encoding/json"
	"os"
	"path/filepath"
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
		"aaaa": ep("224.0.0.55", "192.168.1.10"),   // link-local control → ERROR
		"bbbb": ep("233.252.0.9", "192.168.1.11"),  // doc range → WARN
		"cccc": ep("239.10.0.5", "192.168.1.12"),   // admin-scoped, clean
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
