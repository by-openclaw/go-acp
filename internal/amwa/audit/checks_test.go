package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mk builds a Harvest in memory. Every check reads only Harvest, so a
// crafted one exercises a single spec rule without needing a device
// that gets it wrong — which is the only way to cover the paths real
// hardware happens not to hit.
func mk(role string, apis map[string]map[string]map[string]any) *Harvest {
	h := &Harvest{
		Target: "198.51.100.99:3212",
		Label:  "unit",
		Role:   role,
		APIs:   map[string]API{},
		SDP:    map[string][]byte{},
	}
	for api, versions := range apis {
		a := API{Data: map[string]map[string]json.RawMessage{}}
		for v, resources := range versions {
			a.Versions = append(a.Versions, v)
			bucket := map[string]json.RawMessage{}
			for name, val := range resources {
				b, err := json.Marshal(val)
				if err != nil {
					panic(err)
				}
				bucket[name] = b
			}
			a.Data[v] = bucket
		}
		h.APIs[api] = a
	}
	return h
}

func has(t *testing.T, fs []Finding, code string) Finding {
	t.Helper()
	for _, f := range fs {
		if f.Code == code {
			return f
		}
	}
	t.Fatalf("expected finding %s; got %v", code, codeList(fs))
	return Finding{}
}

func hasNot(t *testing.T, fs []Finding, code string) {
	t.Helper()
	for _, f := range fs {
		if f.Code == code {
			t.Fatalf("did not expect finding %s: %s", code, f.Detail)
		}
	}
}

func codeList(fs []Finding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Code)
	}
	return out
}

// sender is a minimal valid IS-04 sender, so each test only has to
// state the one field it is about.
func sender(id string, over map[string]any) map[string]any {
	s := map[string]any{
		"id":                 id,
		"version":            "1755000000:0",
		"label":              "s",
		"tags":               map[string]any{},
		"flow_id":            nil,
		"transport":          "urn:x-nmos:transport:rtp.mcast",
		"device_id":          "33333333-3333-4333-8333-333333333333",
		"manifest_href":      "http://h/sdp",
		"interface_bindings": []string{"eth0"},
		"subscription":       map[string]any{"receiver_id": nil, "active": false},
	}
	for k, v := range over {
		s[k] = v
	}
	return s
}

const sID = "44444444-4444-4444-8444-444444444444"

// TestIS05AbsentIsCritical: a node that publishes senders and serves no
// Connection API can be seen by every controller and routed by none.
func TestIS05AbsentIsCritical(t *testing.T) {
	h := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.3": {"senders": []any{sender(sID, nil)}}},
	})
	f := has(t, checkAPISurface(h), "NMOS-IS05-ABSENT")
	if f.Severity != SevCritical {
		t.Errorf("severity = %s, want CRITICAL", f.Severity)
	}

	// No senders, no finding — a receive-only node is legitimately
	// IS-05-free on the sender side.
	h2 := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.3": {"senders": []any{}}},
	})
	hasNot(t, checkAPISurface(h2), "NMOS-IS05-ABSENT")

	// A registry is not a node; the rule does not apply to it.
	h3 := mk("registry", map[string]map[string]map[string]any{
		"query": {"v1.3": {"senders": []any{sender(sID, nil)}}},
	})
	hasNot(t, checkAPISurface(h3), "NMOS-IS05-ABSENT")
}

// TestControlDanglingAndHref covers both halves of the controls[]
// cross-check: an advertised API that does not answer, and an entry
// with no href to follow.
func TestControlDanglingAndHref(t *testing.T) {
	dev := func(controls []any) map[string]any {
		return map[string]any{
			"id": "33333333-3333-4333-8333-333333333333", "version": "1:0",
			"label": "d", "tags": map[string]any{}, "type": "urn:x-nmos:device:generic",
			"node_id": "11111111-1111-4111-8111-111111111111",
			"senders": []any{}, "receivers": []any{}, "controls": controls,
		}
	}

	h := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.3": {"devices": []any{dev([]any{
			map[string]any{"href": "http://h/x-nmos/connection/v1.1/", "type": "urn:x-nmos:control:sr-ctrl/v1.1"},
		})}}},
	})
	f := has(t, checkControlAdvertisement(h), "NMOS-IS04-CONTROL-DANGLING")
	if f.Severity != SevError {
		t.Errorf("severity = %s, want ERROR", f.Severity)
	}

	h2 := mk("node", map[string]map[string]map[string]any{
		"node":       {"v1.3": {"devices": []any{dev([]any{map[string]any{"href": "", "type": "urn:x-nmos:control:events/v1.0"}})}}},
		"events":     {"v1.0": {"root": []any{}}},
		"connection": {"v1.1": {"single_senders": []any{}}},
	})
	fs := checkControlAdvertisement(h2)
	has(t, fs, "NMOS-IS04-CONTROL-HREF")
	has(t, fs, "NMOS-IS04-CONTROL-MISSING")

	// No devices captured — nothing to cross-check, and no finding.
	if got := checkControlAdvertisement(mk("node", nil)); got != nil {
		t.Errorf("want no findings without devices, got %v", codeList(got))
	}
}

// TestManifestRules covers each way manifest_href can be wrong, and the
// version at which null stopped being wrong.
func TestManifestRules(t *testing.T) {
	// v1.2: null is not permitted at all.
	h := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.2": {"senders": []any{sender(sID, map[string]any{"manifest_href": nil})}}},
	})
	has(t, checkIS04Manifest(h), "NMOS-IS04-MANIFEST-NULL")

	// v1.3 + inactive: legal, and must not be reported.
	h2 := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.3": {"senders": []any{sender(sID, map[string]any{"manifest_href": nil})}}},
	})
	fs := checkIS04Manifest(h2)
	hasNot(t, fs, "NMOS-IS04-MANIFEST-NULL")
	hasNot(t, fs, "NMOS-IS04-MANIFEST-ACTIVE-NULL")

	// v1.3 + subscribed: a receiver is consuming a stream whose SDP is
	// published nowhere.
	h3 := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.3": {"senders": []any{sender(sID, map[string]any{
			"manifest_href": nil,
			"subscription":  map[string]any{"receiver_id": nil, "active": true},
		})}}},
	})
	has(t, checkIS04Manifest(h3), "NMOS-IS04-MANIFEST-ACTIVE-NULL")

	// Empty string is not the same as null, and is never legal.
	h4 := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.3": {"senders": []any{sender(sID, map[string]any{"manifest_href": ""})}}},
	})
	has(t, checkIS04Manifest(h4), "NMOS-IS04-MANIFEST-EMPTY")

	// A non-RTP transport has no manifest obligation.
	h5 := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.2": {"senders": []any{sender(sID, map[string]any{
			"manifest_href": nil,
			"transport":     "urn:x-nmos:transport:websocket",
		})}}},
	})
	if got := checkIS04Manifest(h5); len(got) != 0 {
		t.Errorf("a websocket sender needs no manifest: %v", codeList(got))
	}

	if got := checkIS04Manifest(mk("node", nil)); got != nil {
		t.Errorf("want no findings without senders, got %v", codeList(got))
	}
}

// TestGroupHintMalformed: BCP-002-01 splits the hint on its first
// colon. A hint with no colon groups nothing.
func TestGroupHintMalformed(t *testing.T) {
	h := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.3": {"senders": []any{sender(sID, map[string]any{
			"tags": map[string]any{groupHintTag: []string{"camera-one"}},
		})}}},
	})
	has(t, checkBCP002GroupHint(h), "NMOS-BCP002-HINT-MALFORMED")

	// Every sender missing the tag is worse than some missing it: the
	// controller has no grouping at all.
	h2 := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.3": {"senders": []any{sender(sID, nil)}}},
	})
	if f := has(t, checkBCP002GroupHint(h2), "NMOS-BCP002-HINT-ABSENT"); f.Severity != SevError {
		t.Errorf("all-missing severity = %s, want ERROR", f.Severity)
	}

	// A well-formed hint on every sender produces nothing.
	h3 := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.3": {"senders": []any{sender(sID, map[string]any{
			"tags": map[string]any{groupHintTag: []string{"cam-01:video"}},
		})}}},
	})
	if got := checkBCP002GroupHint(h3); len(got) != 0 {
		t.Errorf("a correct hint should produce nothing: %v", codeList(got))
	}
}

// TestBCP004CapsVersion: constraint_sets without a caps.version leaves
// every controller caching stale constraints forever.
func TestBCP004CapsVersion(t *testing.T) {
	rx := func(caps map[string]any) map[string]any {
		return map[string]any{
			"id": "88888888-8888-4888-8888-888888888888", "version": "1:0",
			"label": "r", "tags": map[string]any{},
			"device_id": "33333333-3333-4333-8333-333333333333",
			"transport": "urn:x-nmos:transport:rtp.mcast",
			"format":    "urn:x-nmos:format:video", "caps": caps,
			"subscription": map[string]any{"sender_id": nil, "active": false},
		}
	}

	h := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.3": {"receivers": []any{rx(map[string]any{
			"media_types":     []string{"video/raw"},
			"constraint_sets": []any{map[string]any{"urn:x-nmos:cap:format:width": map[string]any{"enum": []int{1920}}}},
		})}}},
	})
	has(t, checkBCP004ReceiverCaps(h), "NMOS-BCP004-CAPS-VERSION")

	h2 := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.3": {"receivers": []any{rx(map[string]any{
			"media_types": []string{"video/raw"},
			"version":     "not-a-tai-timestamp",
		})}}},
	})
	has(t, checkBCP004ReceiverCaps(h2), "NMOS-BCP004-CAPS-VERSION-FORM")

	if got := checkBCP004ReceiverCaps(mk("node", nil)); got != nil {
		t.Errorf("want no findings without receivers, got %v", codeList(got))
	}
}

// active builds a connection-API bucket for one sender.
func active(id string, body map[string]any) map[string]map[string]map[string]any {
	return map[string]map[string]map[string]any{
		"connection": {"v1.1": {
			"single_senders":            []any{id + "/"},
			"senders/" + id + "/active": body,
		}},
	}
}

// TestIS05ActiveFaults walks every way an active sender can be broken
// while reporting itself healthy.
func TestIS05ActiveFaults(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
		want string
	}{
		{"no params", map[string]any{
			"master_enable": true, "transport_params": []any{},
		}, "NMOS-IS05-NO-PARAMS"},

		{"leg disabled", map[string]any{
			"master_enable": true, "transport_params": []any{
				map[string]any{"destination_ip": "233.252.0.5", "destination_port": 20000, "rtp_enabled": true},
				map[string]any{"destination_ip": "233.252.1.5", "destination_port": 20000, "rtp_enabled": false},
			}}, "NMOS-IS05-LEG-DISABLED"},

		{"all legs disabled", map[string]any{
			"master_enable": true, "transport_params": []any{
				map[string]any{"destination_ip": "233.252.0.5", "destination_port": 20000, "rtp_enabled": false},
			}}, "NMOS-IS05-ALL-LEGS-DISABLED"},

		{"destination not an ip", map[string]any{
			"master_enable": true, "transport_params": []any{
				map[string]any{"destination_ip": "not-an-ip", "destination_port": 20000},
			}}, "NMOS-IS05-BAD-DESTINATION"},

		{"port zero", map[string]any{
			"master_enable": true, "transport_params": []any{
				map[string]any{"destination_ip": "233.252.0.5", "destination_port": 0},
			}}, "NMOS-IS05-PORT-ZERO"},

		{"no port", map[string]any{
			"master_enable": true, "transport_params": []any{
				map[string]any{"destination_ip": "233.252.0.5"},
			}}, "NMOS-IS05-NO-PORT"},

		{"destination auto while enabled", map[string]any{
			"master_enable": true, "transport_params": []any{
				map[string]any{"destination_ip": "auto", "destination_port": 20000},
			}}, "NMOS-IS05-NO-DESTINATION"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := mk("node", active(sID, tc.body))
			has(t, checkIS05Active(h), tc.want)
		})
	}

	// master_enable false: the device is deliberately off, and none of
	// the above are faults.
	off := mk("node", active(sID, map[string]any{
		"master_enable": false, "transport_params": []any{
			map[string]any{"destination_ip": "0.0.0.0", "destination_port": 0},
		}}))
	fs := checkIS05Active(off)
	for _, code := range []string{"NMOS-IS05-NO-DESTINATION", "NMOS-IS05-PORT-ZERO", "NMOS-IS05-ALL-LEGS-DISABLED"} {
		hasNot(t, fs, code)
	}

	// No connection API at all.
	if got := checkIS05Active(mk("node", nil)); got != nil {
		t.Errorf("want no findings without a connection API, got %v", codeList(got))
	}
}

// mkIdlePlant builds a node whose every sender is master-enabled with
// no destination — the shape a real 176-sender device sitting idle has.
func mkIdlePlant(t *testing.T) *Harvest {
	t.Helper()
	bucket := map[string]any{}
	ids := []string{
		"44444444-4444-4444-8444-444444444444",
		"55555555-5555-4555-8555-555555555555",
	}
	list := make([]any, 0, len(ids))
	for _, id := range ids {
		list = append(list, id+"/")
		bucket["senders/"+id+"/active"] = map[string]any{
			"master_enable": true,
			"transport_params": []any{
				map[string]any{"destination_ip": "0.0.0.0", "destination_port": 12700, "rtp_enabled": true},
			},
		}
	}
	bucket["single_senders"] = list
	return mk("node", map[string]map[string]map[string]any{"connection": {"v1.1": bucket}})
}

// TestIS05ReceiverRawSDP: a receiver enabled against a raw SDP is legal
// but leaves the route invisible in the registry, so it is reported at
// INFO rather than dropped.
func TestIS05ReceiverRawSDP(t *testing.T) {
	rxID := "88888888-8888-4888-8888-888888888888"
	h := mk("node", map[string]map[string]map[string]any{
		"connection": {"v1.1": {
			"receivers/" + rxID + "/active": map[string]any{
				"master_enable": true, "sender_id": nil,
				"transport_params": []any{map[string]any{"multicast_ip": "233.252.0.9"}},
			},
		}},
	})
	f := has(t, checkIS05Active(h), "NMOS-IS05-RX-NO-SENDER")
	if f.Severity != SevInfo {
		t.Errorf("severity = %s, want INFO", f.Severity)
	}
	if !strings.Contains(f.Detail, rxID) {
		t.Errorf("the finding should name the receiver: %q", f.Detail)
	}
	if !strings.Contains(f.Detail, "1 of 1 receiver(s)") {
		t.Errorf("the finding should say how many of how many: %q", f.Detail)
	}
}

// TestPortRendering: IS-05 allows destination_port to be an integer or
// the string "auto". Both have to render, or the collision check keys
// on an empty string and reports phantom collisions.
func TestPortRendering(t *testing.T) {
	num, str, weird := any(float64(20000)), any("auto"), any(true)
	for _, tc := range []struct {
		p    transportParam
		want string
	}{
		{transportParam{}, ""},
		{transportParam{DestinationPort: &num}, "20000"},
		{transportParam{DestinationPort: &str}, "auto"},
		{transportParam{DestinationPort: &weird}, "true"},
	} {
		if got := tc.p.port(); got != tc.want {
			t.Errorf("port() = %q, want %q", got, tc.want)
		}
	}
}

// TestSameSubnetRedundancy: two 2022-7 legs on one /24 share a failure
// domain, which is the thing redundancy exists to avoid.
func TestSameSubnetRedundancy(t *testing.T) {
	h := mk("node", active(sID, map[string]any{
		"master_enable": true, "transport_params": []any{
			map[string]any{"destination_ip": "233.252.0.5", "destination_port": 20000},
			map[string]any{"destination_ip": "233.252.0.6", "destination_port": 20000},
		}}))
	has(t, checkIS05TransportParams(h), "NMOS-2022-7-SAME-SUBNET")

	// Distinct /24s are the correct configuration.
	h2 := mk("node", active(sID, map[string]any{
		"master_enable": true, "transport_params": []any{
			map[string]any{"destination_ip": "233.252.0.5", "destination_port": 20000},
			map[string]any{"destination_ip": "233.252.1.5", "destination_port": 20000},
		}}))
	hasNot(t, checkIS05TransportParams(h2), "NMOS-2022-7-SAME-SUBNET")
}

func TestAllSame(t *testing.T) {
	if !allSame([]string{"a", "a"}) {
		t.Error("allSame(a,a) should be true")
	}
	if allSame([]string{"a", "b"}) {
		t.Error("allSame(a,b) should be false")
	}
	if !allSame([]string{"a"}) {
		t.Error("a single element is trivially all-same")
	}
}

// TestRegistrationDirections covers both ways a plant's registration
// state can be wrong.
func TestRegistrationDirections(t *testing.T) {
	node := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.3": {"self": map[string]any{
			"id": "11111111-1111-4111-8111-111111111111", "version": "1:0", "label": "orphan",
		}}},
	})
	node.ID = ""
	reg := mk("registry", map[string]map[string]map[string]any{
		"query": {"v1.3": {"nodes": []any{}}},
	})
	reg.Children = []*Harvest{node}

	fs := checkPlantRegistration([]*Harvest{reg})
	has(t, fs, "NMOS-NODE-UNREGISTERED")
	has(t, fs, "NMOS-REGISTRY-EMPTY")

	// With no registry in the export there is nothing to compare
	// against, so the check must stay silent rather than guess.
	if got := checkPlantRegistration([]*Harvest{node}); got != nil {
		t.Errorf("want no findings without a registry, got %v", codeList(got))
	}
}

// TestReportLineKinds covers the report.txt shapes the fixture does not
// carry.
func TestReportLineKinds(t *testing.T) {
	h := mk("node", nil)
	h.Report = []string{
		"ERR   /x-nmos/connection/v1.1/single/senders/44444444-4444-4444-8444-444444444444/staged",
		"CAPPED 12 node(s) not followed (-MaxNodes 250)",
		"404   /x-nmos/connection/v1.1/single/senders/44444444-4444-4444-8444-444444444444/transportfile",
		"404   /x-nmos/events/",
		"200   /x-nmos/node/v1.3/self",
		"random line the exporter wrote",
	}
	fs := checkTransportReport(h)
	has(t, fs, "NMOS-HTTP-UNREACHABLE")
	has(t, fs, "NMOS-AUDIT-COVERAGE-CAPPED")
	f := has(t, fs, "NMOS-HTTP-MISSING-ENDPOINT")
	if !strings.Contains(f.Resource, "{id}") {
		t.Errorf("endpoint shape not collapsed: %q", f.Resource)
	}
	// A 404 on an API the device simply does not have is not a defect,
	// and must not be reported as one.
	for _, x := range fs {
		if strings.Contains(x.Detail, "/x-nmos/events/") {
			t.Error("a 404 on an absent API was reported as a fault")
		}
	}

	if got := checkTransportReport(mk("node", nil)); got != nil {
		t.Errorf("want no findings without a report, got %v", codeList(got))
	}
}

// TestStuckCursorGroupsAcrossCollections: a registry serving four
// minors × six collections produces 24 identical stuck-cursor lines.
// Twenty-four identical findings bury everything else in the report,
// so they collapse to one that names the count.
func TestStuckCursorGroupsAcrossCollections(t *testing.T) {
	h := mk("registry", nil)
	for _, v := range []string{"v1.0", "v1.1", "v1.2", "v1.3"} {
		for _, kind := range []string{"nodes", "devices", "sources", "flows", "senders", "receivers"} {
			h.Report = append(h.Report, "WARN  paging cursor did not advance for /x-nmos/query/"+v+"/"+kind+
				" - stopping (registry paging defect)")
		}
	}
	fs := checkTransportReport(h)
	stuck := 0
	for _, f := range fs {
		if f.Code == "NMOS-QUERY-PAGING-STUCK" {
			stuck++
		}
	}
	if stuck != 1 {
		t.Fatalf("24 stuck-cursor lines produced %d findings, want 1", stuck)
	}
	f := has(t, fs, "NMOS-QUERY-PAGING-STUCK")
	if !strings.Contains(f.Detail, "24 collection(s)") {
		t.Errorf("the grouped finding should name the count: %q", f.Detail)
	}
}

// TestTruncatedCollectionSuppressesDanglingRefs is the guard that
// removed 97 false findings from a real 44-node capture.
//
// The registry served exactly 100 senders and 100 flows and then an
// empty page. Every sender past that boundary appeared to point at a
// flow that did not exist — not because the flow was missing, but
// because nobody fetched the page it was on.
func TestTruncatedCollectionSuppressesDanglingRefs(t *testing.T) {
	report := []string{
		"200   /x-nmos/query/v1.3/flows  (page 1, +100, total 100)",
		"200   /x-nmos/query/v1.3/flows  (page 2, +0, total 100)",
	}

	h := mk("registry", map[string]map[string]map[string]any{
		"query": {"v1.3": {
			"flows":   []any{},
			"devices": []any{},
			"senders": []any{sender(sID, map[string]any{
				"flow_id": "99999999-9999-4999-8999-999999999999",
			})},
		}},
	})
	h.Report = report

	// Only `flows` was truncated, so only flow references stand down.
	// The device reference is still assertable, and still a finding —
	// suppression is per collection, not blanket.
	for _, f := range checkIS04Graph(h) {
		if f.Code == "NMOS-IS04-DANGLING-REF" && strings.Contains(f.Detail, "flow_id") {
			t.Errorf("a reference into a truncated collection was reported: %q", f.Detail)
		}
	}
	if !strings.Contains(has(t, checkIS04Graph(h), "NMOS-IS04-DANGLING-REF").Detail, "device_id") {
		t.Error("references into a complete collection must still be reported")
	}

	// The suppression must be reported, or a truncated capture reads as
	// a clean plant.
	f := has(t, checkTransportReport(h), "NMOS-AUDIT-COLLECTION-MAYBE-SHORT")
	if !strings.Contains(f.Detail, "flows") {
		t.Errorf("the finding should name the collection: %q", f.Detail)
	}

	// Without the truncation trace, the flow reference IS a finding.
	h2 := mk("registry", map[string]map[string]map[string]any{
		"query": {"v1.3": {
			"flows":   []any{},
			"devices": []any{},
			"senders": []any{sender(sID, map[string]any{
				"flow_id": "99999999-9999-4999-8999-999999999999",
			})},
		}},
	})
	sawFlow := false
	for _, f := range checkIS04Graph(h2) {
		if f.Code == "NMOS-IS04-DANGLING-REF" && strings.Contains(f.Detail, "flow_id") {
			sawFlow = true
		}
	}
	if !sawFlow {
		t.Error("a complete-but-empty collection must still yield a dangling-reference finding")
	}
}

func TestTruncatedKinds(t *testing.T) {
	// A walk that ended on a non-empty page is complete.
	if got := truncatedKinds([]string{
		"200   /x-nmos/query/v1.3/nodes  (page 1, +100, total 100)",
		"200   /x-nmos/query/v1.3/nodes  (page 2, +7, total 107)",
	}); len(got) != 0 {
		t.Errorf("a walk ending on a partial page is complete, got %v", got)
	}
	// A single page is complete.
	if got := truncatedKinds([]string{
		"200   /x-nmos/query/v1.3/nodes  (page 1, +45, total 45)",
	}); len(got) != 0 {
		t.Errorf("a single-page walk is complete, got %v", got)
	}
	if got := truncatedKinds(nil); len(got) != 0 {
		t.Errorf("no pages, no truncation, got %v", got)
	}
}

// TestPagingDupesOnlyWhenRowsExceedUnique guards against reporting a
// clean walk as a defect.
func TestPagingDupesOnlyWhenRowsExceedUnique(t *testing.T) {
	h := mk("registry", nil)
	h.Report = []string{"NOTE  /x-nmos/query/v1.3/nodes returned 40 rows across pages, 40 unique"}
	hasNot(t, checkTransportReport(h), "NMOS-QUERY-PAGING-DUPES")
}

// TestVersionIsolationNeedsTwoMinors: a registry serving one minor
// cannot hide anything behind version isolation.
func TestVersionIsolationNeedsTwoMinors(t *testing.T) {
	h := mk("registry", map[string]map[string]map[string]any{
		"query": {"v1.3": {"nodes": []any{map[string]any{"id": "11111111-1111-4111-8111-111111111111"}}}},
	})
	if got := checkQueryVersionIsolation(h); got != nil {
		t.Errorf("want no findings for a single-minor registry, got %v", codeList(got))
	}
	if got := checkQueryVersionIsolation(mk("node", nil)); got != nil {
		t.Errorf("want no findings without a query API, got %v", codeList(got))
	}
}

// TestUndecodableResource: a collection holding something that is not
// an object is a finding, not a crash.
func TestUndecodableResource(t *testing.T) {
	h := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.3": {"senders": []any{"just a string"}}},
	})
	has(t, checkIS04Core(h), "NMOS-IS04-UNDECODABLE")
}

// TestDuplicateID: two resources sharing an id means one of them is
// unreachable, because the id is the only handle a controller has.
func TestDuplicateID(t *testing.T) {
	h := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.3": {"senders": []any{sender(sID, nil), sender(sID, nil)}}},
	})
	if f := has(t, checkIS04Core(h), "NMOS-IS04-DUPLICATE-ID"); f.Severity != SevCritical {
		t.Errorf("severity = %s, want CRITICAL", f.Severity)
	}
}

// TestGraphSkipsUncapturedCollections: absence of a collection is not
// evidence that an edge dangles. Reporting it would flood a partial
// export with false positives.
func TestGraphSkipsUncapturedCollections(t *testing.T) {
	h := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.3": {"senders": []any{sender(sID, map[string]any{
			"flow_id": "99999999-9999-4999-8999-999999999999",
		})}}},
	})
	if got := checkIS04Graph(h); len(got) != 0 {
		t.Errorf("an uncaptured flows collection must not produce dangling refs: %v", codeList(got))
	}
	if got := checkIS04Graph(mk("unknown", nil)); got != nil {
		t.Errorf("want no findings without an IS-04 surface, got %v", codeList(got))
	}
}

// TestGraphReportsAgainstAnEmptyCapturedCollection is the other half of
// the rule above, and the distinction that decides whether the check is
// useful at all.
//
// `"flows": []` is the device saying it has no flows. A sender pointing
// at one is therefore broken, and a controller will drop it. Treating
// captured-and-empty the same as never-captured silently suppresses the
// finding on every node that publishes empty collections — which is
// most of them.
func TestGraphReportsAgainstAnEmptyCapturedCollection(t *testing.T) {
	h := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.3": {
			"flows":   []any{},
			"devices": []any{},
			"senders": []any{sender(sID, map[string]any{
				"flow_id": "99999999-9999-4999-8999-999999999999",
			})},
		}},
	})
	fs := checkIS04Graph(h)
	has(t, fs, "NMOS-IS04-DANGLING-REF")
	// Both edges dangle against empty collections, and both are real:
	// the sender's flow is not published, and neither is its device.
	want := map[string]bool{"flow_id": false, "device_id": false}
	for _, f := range fs {
		for field := range want {
			if strings.Contains(f.Detail, field) {
				want[field] = true
			}
		}
	}
	for field, found := range want {
		if !found {
			t.Errorf("no dangling-reference finding names %s: %v", field, fs)
		}
	}
}

// TestCapturedDistinguishesEmptyFromAbsent pins the helper the rule
// above rests on.
func TestCapturedDistinguishesEmptyFromAbsent(t *testing.T) {
	h := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.3": {"flows": []any{}, "sources": nil}},
	})
	if !h.captured("node", "flows") {
		t.Error("an empty array is a captured collection")
	}
	if h.captured("node", "sources") {
		t.Error("a null capture is not a captured collection")
	}
	if h.captured("node", "senders") {
		t.Error("a resource that was never fetched is not captured")
	}
	if h.captured("nope", "flows") {
		t.Error("an absent API cannot have captured anything")
	}
}

// TestInferRole covers classification for a capture whose device.json
// never got its role written — an export interrupted mid-run.
func TestInferRole(t *testing.T) {
	for _, tc := range []struct {
		api  string
		want string
	}{
		{"query", "registry"},
		{"registration", "registry"},
		{"node", "node"},
		{"channelmapping", "unknown"},
	} {
		h := mk("", map[string]map[string]map[string]any{tc.api: {"v1.0": {}}})
		if got := inferRole(h); got != tc.want {
			t.Errorf("inferRole(%s) = %q, want %q", tc.api, got, tc.want)
		}
	}
	if got := inferRole(&Harvest{APIs: map[string]API{}}); got != "unknown" {
		t.Errorf("inferRole of an empty surface = %q", got)
	}
}

// TestDecodeArrayTolerance: a capture records `null` where a request
// failed. That must read as absent, never as a decode error that stops
// the audit.
func TestDecodeArrayTolerance(t *testing.T) {
	if got := decodeArray(nil); got != nil {
		t.Error("nil blob should decode to nil")
	}
	if got := decodeArray(json.RawMessage("null")); got != nil {
		t.Error("a null capture should decode to nil")
	}
	// A lone object reads as a one-element array. PowerShell's
	// ConvertTo-Json collapses single-element arrays, so every capture
	// from the original exporter records a node's single Device that
	// way. Read strictly, such a node has no devices and every resource
	// on it dangles — 18104 false findings on one real 7-node capture.
	if got := decodeArray(json.RawMessage(`{"id":"a"}`)); len(got) != 1 {
		t.Errorf("a lone object should read as one element, got %v", got)
	}
	if got := decodeArray(json.RawMessage(`{}`)); got != nil {
		t.Error("an empty object is not a resource")
	}
	if got := decodeArray(json.RawMessage(`"scalar"`)); got != nil {
		t.Error("a scalar is neither an array nor a resource")
	}
	if got := decodeArray(json.RawMessage(`[1,2]`)); len(got) != 2 {
		t.Errorf("array decode = %v", got)
	}
	if got := idOf(json.RawMessage(`"scalar"`)); got != "" {
		t.Errorf("idOf a scalar = %q", got)
	}
}

// TestObjectSkipsNullVersions proves the reader falls through a minor
// that captured nothing to one that did.
func TestObjectSkipsNullVersions(t *testing.T) {
	h := mk("node", map[string]map[string]map[string]any{
		"node": {
			"v1.3": {"self": nil},
			"v1.2": {"self": map[string]any{"id": "11111111-1111-4111-8111-111111111111"}},
		},
	})
	blob, ver := h.object("node", "self")
	if blob == nil || ver != "v1.2" {
		t.Errorf("object() = %s at %q; want the v1.2 capture", blob, ver)
	}
	if blob, _ := h.object("nope", "self"); blob != nil {
		t.Error("object() of an absent API should be nil")
	}
}

// TestLoadReadsSDPAndSkipsNonSDP covers the sdp/ side of the reader.
func TestLoadReadsSDPAndSkipsNonSDP(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("tree.json", `{"target":"h:1","apis":{"_root":["node/"],"node":{"versions":["v1.3"],"data":{}},"broken":"not-an-object"}}`)
	write("sdp/sender_a.sdp", "v=0\r\n")
	write("sdp/notes.txt", "ignore me")

	hs, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := hs[0]
	if len(h.SDP) != 1 {
		t.Errorf("SDP files loaded = %d, want 1 (.txt must be ignored)", len(h.SDP))
	}
	// An API entry whose value is not an object is skipped by the
	// loader; saying so is the checks' job, not the reader's.
	if _, ok := h.APIs["broken"]; ok {
		t.Error("a malformed API entry should not load as an API")
	}
	if h.Role != "node" {
		t.Errorf("role = %q, want node (inferred, no device.json)", h.Role)
	}
}

func TestSplitLinesHandlesBothEndings(t *testing.T) {
	if got := splitLines("a\r\nb\r\n"); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("CRLF split = %q", got)
	}
	if got := splitLines("a\nb"); len(got) != 2 {
		t.Errorf("LF split = %q", got)
	}
	if got := splitLines(""); len(got) != 0 {
		t.Errorf("empty split = %q", got)
	}
}

func TestDedupeAndEmitterString(t *testing.T) {
	if got := dedupe([]string{"a", "a", "b"}); len(got) != 2 {
		t.Errorf("dedupe = %v", got)
	}
	if got := dedupe(nil); len(got) != 0 {
		t.Errorf("dedupe(nil) = %v", got)
	}
	e := emitter{target: "h:1", senderID: "s", leg: 0}
	if !strings.Contains(e.String(), "h:1") {
		t.Errorf("an unlabelled emitter should name its target: %q", e.String())
	}
}

func TestUnmarshalSeverityRejectsGarbage(t *testing.T) {
	var s Severity
	if err := s.UnmarshalText([]byte("nonsense")); err == nil {
		t.Error("an unknown severity must fail to unmarshal")
	}
}

func TestFindHarvestDirsStopsAtFirstHit(t *testing.T) {
	// A nested harvest belongs to its parent as a Child; it must not
	// also load as a second root, or every followed node would be
	// audited twice.
	hs, err := Load(plantDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(hs) != 1 {
		t.Fatalf("nested harvests loaded as %d roots, want 1", len(hs))
	}
}
