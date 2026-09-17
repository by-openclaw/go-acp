package provider

// IS-11 provider tests — expected behaviour from the v1.0.0 RAML
// (route tree, status codes 200/400/404-by-routing/405/423, EDID
// octet-stream rules), not from working code.

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is11"
	httpsession "dhs/internal/amwa/session/http"
)

const (
	scSender   = "11111111-1111-4111-8111-111111111111"
	scReceiver = "22222222-2222-4222-8222-222222222222"
	scInput    = "33333333-3333-4333-8333-333333333333"
	scOutput   = "44444444-4444-4444-8444-444444444444"
	scDevice   = "55555555-5555-4555-8555-555555555555"
)

func scBundle() *NodeConfig {
	adjust := false
	return &NodeConfig{
		Senders: []is04.Sender{{
			ResourceCore: is04.ResourceCore{ID: scSender},
			Transport:    is04.TransportRTP,
		}},
		Receivers: []is04.Receiver{{
			ResourceCore: is04.ResourceCore{ID: scReceiver},
			Transport:    is04.TransportRTP,
		}},
		StreamCompatibility: &StreamCompatSeed{
			Inputs: []is11.Input{{
				ResourceCore: is11.ResourceCore{ID: scInput, Version: "1:0", Label: "SDI-IN-1",
					Description: "x", Tags: map[string][]string{}},
				AdjustToCaps:    &adjust,
				BaseEDIDSupport: true,
				Connected:       true,
				EDIDSupport:     true,
				Status:          is11.Status{State: is11.InputSignalPresent},
				DeviceID:        scDevice,
			}},
			Outputs: []is11.Output{{
				ResourceCore: is11.ResourceCore{ID: scOutput, Version: "1:0", Label: "HDMI-OUT-1",
					Description: "x", Tags: map[string][]string{}},
				Connected:   false,
				EDIDSupport: false,
				Status:      is11.Status{State: is11.OutputNoSignal},
				DeviceID:    scDevice,
			}},
			SenderInputs:    map[string][]string{scSender: {scInput}},
			ReceiverOutputs: map[string][]string{scReceiver: {scOutput}},
		},
	}
}

func scServer(t *testing.T, senderActive bool) (*httptest.Server, *IS11StreamCompatServer) {
	t.Helper()
	s := NewIS11StreamCompatServer(slog.Default(), scBundle(), IS11StreamCompatConfig{APIVer: "v1.0"})
	s.SetSenderActiveFunc(func(string) bool { return senderActive })
	srv := httpsession.NewServer(nil)
	s.Mount(srv)
	ts := httptest.NewServer(srv.MuxHandler())
	t.Cleanup(ts.Close)
	return ts, s
}

func scGet(t *testing.T, ts *httptest.Server, path string, dst any) int {
	t.Helper()
	resp, err := stdhttp.Get(ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if dst != nil && resp.StatusCode == 200 {
		if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}
	return resp.StatusCode
}

func scDo(t *testing.T, ts *httptest.Server, method, path string, body []byte, ct string) *stdhttp.Response {
	t.Helper()
	req, err := stdhttp.NewRequest(method, ts.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	resp, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

const scBase = "/x-nmos/streamcompatibility/v1.0"

func TestIS11TreeAndAssociations(t *testing.T) {
	ts, _ := scServer(t, false)

	var vers []string
	if code := scGet(t, ts, "/x-nmos/streamcompatibility/", &vers); code != 200 || len(vers) != 1 || vers[0] != "v1.0/" {
		t.Fatalf("version list: %d %v", code, vers)
	}
	var base []string
	scGet(t, ts, scBase+"/", &base)
	if len(base) != 4 {
		t.Errorf("base index = %v", base)
	}
	var senders []string
	scGet(t, ts, scBase+"/senders/", &senders)
	if len(senders) != 1 || senders[0] != scSender+"/" {
		t.Errorf("senders = %v", senders)
	}
	var ins []string
	scGet(t, ts, scBase+"/senders/"+scSender+"/inputs/", &ins)
	if len(ins) != 1 || ins[0] != scInput {
		t.Errorf("sender inputs = %v", ins)
	}
	var outs []string
	scGet(t, ts, scBase+"/receivers/"+scReceiver+"/outputs/", &outs)
	if len(outs) != 1 || outs[0] != scOutput {
		t.Errorf("receiver outputs = %v", outs)
	}
	var in is11.Input
	if code := scGet(t, ts, scBase+"/inputs/"+scInput+"/properties/", &in); code != 200 || in.Label != "SDI-IN-1" {
		t.Errorf("input properties: %d %+v", code, in)
	}
	var rs is11.Status
	scGet(t, ts, scBase+"/receivers/"+scReceiver+"/status/", &rs)
	if rs.State != is11.ReceiverUnknown {
		t.Errorf("receiver state = %s", rs.State)
	}
}

func TestIS11ActiveConstraintsLifecycle(t *testing.T) {
	ts, _ := scServer(t, false)
	p := scBase + "/senders/" + scSender + "/constraints/active/"

	// Fresh: unconstrained, empty sets.
	var a is11.ActiveConstraints
	if code := scGet(t, ts, p, &a); code != 200 || len(a.ConstraintSets) != 0 {
		t.Fatalf("fresh active: %d %+v", code, a)
	}
	var st is11.Status
	scGet(t, ts, scBase+"/senders/"+scSender+"/status/", &st)
	if st.State != is11.SenderUnconstrained {
		t.Errorf("fresh state = %s", st.State)
	}

	// PUT a supported constraint → 200 + constrained.
	body := []byte(`{"constraint_sets":[{"urn:x-nmos:cap:format:frame_width":{"enum":[1920]}}]}`)
	resp := scDo(t, ts, stdhttp.MethodPut, p, body, "application/json")
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT: %d %s", resp.StatusCode, b)
	}
	_ = resp.Body.Close()
	scGet(t, ts, scBase+"/senders/"+scSender+"/status/", &st)
	if st.State != is11.SenderConstrained {
		t.Errorf("state after PUT = %s", st.State)
	}

	// Unsupported URN → 400.
	bad := []byte(`{"constraint_sets":[{"urn:x-nmos:cap:format:colorspace":{"enum":["BT2020"]}}]}`)
	resp = scDo(t, ts, stdhttp.MethodPut, p, bad, "application/json")
	if resp.StatusCode != 400 {
		t.Errorf("unsupported URN: %d, want 400", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Garbage → 400.
	resp = scDo(t, ts, stdhttp.MethodPut, p, []byte(`{"bad":1}`), "application/json")
	if resp.StatusCode != 400 {
		t.Errorf("garbage: %d, want 400", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// DELETE → 200 empty shape, unconstrained again.
	resp = scDo(t, ts, stdhttp.MethodDelete, p, nil, "")
	if resp.StatusCode != 200 {
		t.Fatalf("DELETE: %d", resp.StatusCode)
	}
	var cleared is11.ActiveConstraints
	_ = json.NewDecoder(resp.Body).Decode(&cleared)
	_ = resp.Body.Close()
	if cleared.ConstraintSets == nil || len(cleared.ConstraintSets) != 0 {
		t.Errorf("DELETE body = %+v, want empty constraint_sets", cleared)
	}
	scGet(t, ts, scBase+"/senders/"+scSender+"/status/", &st)
	if st.State != is11.SenderUnconstrained {
		t.Errorf("state after DELETE = %s", st.State)
	}
}

func TestIS11ActiveSenderGets423(t *testing.T) {
	ts, _ := scServer(t, true) // IS-05 says the sender is live
	p := scBase + "/senders/" + scSender + "/constraints/active/"
	body := []byte(`{"constraint_sets":[]}`)
	resp := scDo(t, ts, stdhttp.MethodPut, p, body, "application/json")
	if resp.StatusCode != 423 {
		t.Errorf("PUT on active sender: %d, want 423", resp.StatusCode)
	}
	_ = resp.Body.Close()
	resp = scDo(t, ts, stdhttp.MethodDelete, p, nil, "")
	if resp.StatusCode != 423 {
		t.Errorf("DELETE on active sender: %d, want 423", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestIS11EDIDLifecycle(t *testing.T) {
	ts, _ := scServer(t, false)
	base := scBase + "/inputs/" + scInput + "/edid/base/"
	eff := scBase + "/inputs/" + scInput + "/edid/effective/"

	// No Base EDID yet: base is 204, but effective serves the device's
	// own default EDID — an EDID-capable Input always HAS an effective
	// EDID (AMWA IS-11-01 test_01_01/01_02 fail a 204 there).
	resp0 := scDo(t, ts, stdhttp.MethodGet, base, nil, "")
	if resp0.StatusCode != 204 {
		t.Errorf("GET base pre-PUT: %d, want 204", resp0.StatusCode)
	}
	_ = resp0.Body.Close()
	resp0 = scDo(t, ts, stdhttp.MethodGet, eff, nil, "")
	if resp0.StatusCode != 200 {
		t.Errorf("GET effective pre-PUT: %d, want 200 (default EDID)", resp0.StatusCode)
	}
	defBlob, _ := io.ReadAll(resp0.Body)
	_ = resp0.Body.Close()
	if len(defBlob) != 128 {
		t.Errorf("default EDID = %d bytes, want 128", len(defBlob))
	}

	// Bad size → 400.
	resp := scDo(t, ts, stdhttp.MethodPut, base, make([]byte, 100), "application/octet-stream")
	if resp.StatusCode != 400 {
		t.Errorf("PUT 100 bytes: %d, want 400", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 128-byte blob → 204, then GET serves it as octet-stream.
	blob := make([]byte, 128)
	blob[0] = 0x00
	blob[1] = 0xFF
	resp = scDo(t, ts, stdhttp.MethodPut, base+"?adjust_to_caps=true", blob, "application/octet-stream")
	if resp.StatusCode != 204 {
		t.Fatalf("PUT edid: %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp = scDo(t, ts, stdhttp.MethodGet, base, nil, "")
	if resp.StatusCode != 200 {
		t.Fatalf("GET base after PUT: %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("content-type = %q", ct)
	}
	got, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if len(got) != 128 || got[1] != 0xFF {
		t.Errorf("EDID round trip lost bytes: %d", len(got))
	}

	// adjust_to_caps query flipped the property (the input carries it).
	var in is11.Input
	scGet(t, ts, scBase+"/inputs/"+scInput+"/properties/", &in)
	if in.AdjustToCaps == nil || !*in.AdjustToCaps {
		t.Errorf("adjust_to_caps not applied: %+v", in.AdjustToCaps)
	}

	// Effective mirrors Base for the reference node.
	resp = scDo(t, ts, stdhttp.MethodGet, eff, nil, "")
	if resp.StatusCode != 200 {
		t.Errorf("GET effective: %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// DELETE → 204 → back to no content.
	resp = scDo(t, ts, stdhttp.MethodDelete, base, nil, "")
	if resp.StatusCode != 204 {
		t.Fatalf("DELETE edid: %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	resp = scDo(t, ts, stdhttp.MethodGet, base, nil, "")
	if resp.StatusCode != 204 {
		t.Errorf("GET after DELETE: %d, want 204", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Output without EDID support: GET edid → 204, and an input PUT
	// against it is not even routed (404 by absence of the route).
	resp = scDo(t, ts, stdhttp.MethodGet, scBase+"/outputs/"+scOutput+"/edid/", nil, "")
	if resp.StatusCode != 204 {
		t.Errorf("output edid: %d, want 204", resp.StatusCode)
	}
	_ = resp.Body.Close()
}
