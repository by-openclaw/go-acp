package provider

// IS-11 constraint re-negotiation surface: applying Active Constraints
// to a Sender must move its Inputs — version bump + a different (but
// still valid) Effective EDID — exactly what IS-11-01 test_02_03_05_*
// observes.

import (
	"bytes"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is11"
	// The node mounts IS-11 for the registered minors only — the test
	// binary needs the v1.0 codec registered like cmd/dhs does.
	_ "dhs/internal/amwa/codec/is11/v10"
)

func ioReader(s string) io.Reader { return strings.NewReader(s) }

func scReadEffective(t *testing.T, ts *httptest.Server, id string) []byte {
	t.Helper()
	resp, err := stdhttp.Get(ts.URL + scBase + "/inputs/" + id + "/edid/effective/")
	if err != nil {
		t.Fatalf("GET effective: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("effective EDID = %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	blob, _ := io.ReadAll(resp.Body)
	return blob
}

func TestIS11ConstraintsMoveInputs(t *testing.T) {
	ts, _ := scServer(t, false)

	var before is11.Input
	scGet(t, ts, scBase+"/inputs/"+scInput+"/properties/", &before)
	edidBefore := scReadEffective(t, ts, scInput)

	body := []byte(`{"constraint_sets":[{"urn:x-nmos:cap:format:grain_rate":{"enum":[{"numerator":25,"denominator":1}]}}]}`)
	resp := scDo(t, ts, "PUT", scBase+"/senders/"+scSender+"/constraints/active/", body, "application/json")
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("PUT constraints = %d", resp.StatusCode)
	}

	var after is11.Input
	scGet(t, ts, scBase+"/inputs/"+scInput+"/properties/", &after)
	if after.Version == before.Version {
		t.Error("input version did not change on constraints PUT")
	}
	edidAfter := scReadEffective(t, ts, scInput)
	if string(edidAfter) == string(edidBefore) {
		t.Error("effective EDID did not change on constraints PUT")
	}
	// Still a structurally valid EDID block: header + checksum.
	if len(edidAfter) != 128 || edidAfter[0] != 0x00 || edidAfter[1] != 0xFF {
		t.Errorf("varied EDID lost its structure: len=%d", len(edidAfter))
	}
	var sum byte
	for _, v := range edidAfter {
		sum += v
	}
	if sum != 0 {
		t.Errorf("varied EDID checksum broken (block sum %d)", sum)
	}

	// DELETE moves them again.
	resp = scDo(t, ts, "DELETE", scBase+"/senders/"+scSender+"/constraints/active/", nil, "")
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("DELETE constraints = %d", resp.StatusCode)
	}
	edidReset := scReadEffective(t, ts, scInput)
	if string(edidReset) == string(edidAfter) {
		t.Error("effective EDID did not move on constraints DELETE")
	}
}

// TestIS11ConstraintsAdaptFlow: applying Active Constraints
// re-configures the sender's Flow (IS-11 essence adaptation), and
// removing them restores the bundle's original parameters.
func TestIS11ConstraintsAdaptFlow(t *testing.T) {
	b := audioBundle()
	sndID := "aaaaaaaa-1111-4111-8111-111111111111"
	flowID := "dddddddd-4444-4444-8444-444444444444"
	inID := "33333333-3333-4333-8333-333333333333" // IS-11 input entity (its own id space)
	adjust := false
	b.StreamCompatibility = &StreamCompatSeed{
		Inputs: []is11.Input{{
			ResourceCore: is11.ResourceCore{ID: inID, Version: "1:0", Label: "IN",
				Description: "x", Tags: map[string][]string{}},
			AdjustToCaps:    &adjust,
			BaseEDIDSupport: true,
			Connected:       true,
			EDIDSupport:     true,
			Status:          is11.Status{State: is11.InputSignalPresent},
			DeviceID:        b.Devices[0].ID,
		}},
		SenderInputs: map[string][]string{sndID: {inID}},
	}
	addr := serveNCPBundleNode(t, b)

	put := func(body string) int {
		req, _ := stdhttp.NewRequest("PUT",
			"http://"+addr+"/x-nmos/streamcompatibility/v1.0/senders/"+sndID+"/constraints/active/",
			ioReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := stdhttp.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("PUT constraints: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode
	}
	flowRate := func() string {
		st, raw := mxlGet(t, "http://"+addr+"/x-nmos/node/v1.3/flows/"+flowID)
		if st != 200 {
			t.Fatalf("flow GET = %d", st)
		}
		var f struct {
			SampleRate struct {
				Numerator int `json:"numerator"`
			} `json:"sample_rate"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatalf("flow decode: %v", err)
		}
		return itoa(f.SampleRate.Numerator)
	}

	if got := flowRate(); got != "48000" {
		t.Fatalf("boot sample_rate = %s, want 48000", got)
	}
	if st := put(`{"constraint_sets":[{"urn:x-nmos:cap:format:sample_rate":{"enum":[{"numerator":44100,"denominator":1}]}}]}`); st != 200 {
		t.Fatalf("PUT constraints = %d", st)
	}
	if got := flowRate(); got != "44100" {
		t.Errorf("constrained sample_rate = %s, want 44100 (flow must adapt)", got)
	}
	req, _ := stdhttp.NewRequest("DELETE",
		"http://"+addr+"/x-nmos/streamcompatibility/v1.0/senders/"+sndID+"/constraints/active/", nil)
	resp, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE constraints: %v", err)
	}
	_ = resp.Body.Close()
	if got := flowRate(); got != "48000" {
		t.Errorf("sample_rate after DELETE = %s, want restored 48000", got)
	}
}

// TestIS11ReceiverStatusThroughTheNode: the whole path the suite's
// live-streaming rounds take — an IS-05 PATCH that stages an SDP and
// activates the receiver, then the IS-11 receiver status and its
// output's properties — through the served node, not the hooks.
func TestIS11ReceiverStatusThroughTheNode(t *testing.T) {
	b := audioBundle()
	rcvID := "bbbbbbbb-2222-4222-8222-222222222222"
	outID := "44444444-4444-4444-8444-444444444444"
	for i := range b.Receivers {
		if b.Receivers[i].ID == rcvID {
			b.Receivers[i].Caps.MediaTypes = []string{"audio/L24"}
		}
	}
	b.StreamCompatibility = &StreamCompatSeed{
		Outputs: []is11.Output{{
			ResourceCore: is11.ResourceCore{ID: outID, Version: "1:0", Label: "OUT",
				Description: "x", Tags: map[string][]string{}},
			Connected:   true,
			EDIDSupport: false,
			Status:      is11.Status{State: is11.OutputNoSignal},
			DeviceID:    b.Devices[0].ID,
		}},
		ReceiverOutputs: map[string][]string{rcvID: {outID}},
	}
	n := startNodeWith(t, b, func(c *IS04NodeConfig) { c.ConnectionAPIVer = "v1.2" })
	addr := n.addr
	status := func() (string, string) {
		_, rs := mxlGet(t, "http://"+addr+"/x-nmos/streamcompatibility/v1.0/receivers/"+rcvID+"/status/")
		_, op := mxlGet(t, "http://"+addr+"/x-nmos/streamcompatibility/v1.0/outputs/"+outID+"/properties/")
		var st is11.Status
		var out is11.Output
		_ = json.Unmarshal(rs, &st)
		_ = json.Unmarshal(op, &out)
		return st.State, out.Status.State
	}
	if r, o := status(); r != is11.ReceiverUnknown || o != is11.OutputNoSignal {
		t.Fatalf("before any connection: receiver %s, output %s", r, o)
	}
	// Connect the receiver to an L24 stream, as a controller does.
	sdp := "v=0\r\no=- 1 1 IN IP4 10.0.0.7\r\ns=ref\r\nt=0 0\r\n" +
		"m=audio 5004 RTP/AVP 97\r\nc=IN IP4 239.1.1.9/64\r\na=rtpmap:97 L24/48000/8\r\n"
	body, _ := json.Marshal(map[string]any{
		"master_enable":  true,
		"activation":     map[string]any{"mode": "activate_immediate"},
		"transport_file": map[string]any{"type": "application/sdp", "data": sdp},
	})
	req, _ := stdhttp.NewRequest("PATCH",
		"http://"+addr+"/x-nmos/connection/v1.2/single/receivers/"+rcvID+"/staged", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("PATCH staged = %d", resp.StatusCode)
	}
	if r, o := status(); r != is11.ReceiverCompliantStream || o != is11.OutputSignalPresent {
		t.Fatalf("connected to an L24 stream: receiver %s, output %s", r, o)
	}
	// A receiver the IS-05 layer does not know answers nothing.
	if active, mt := n.s.streamCompat.receiverStream("ffffffff-ffff-4fff-8fff-ffffffffffff"); active || mt != "" {
		t.Fatalf("unknown receiver = %v %q", active, mt)
	}
}
