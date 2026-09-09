package provider

// IS-11 v1.0.0 contract tests for the routes and seed paths the
// lifecycle tests leave untouched: the RAML index resources, the
// constraints/supported list (seeded vs. BCP-004-01 default), the
// "0..n Inputs/Outputs" association shape, the EDID rules for
// Inputs/Outputs that do not support one, the bundle-seeded EDIDs,
// and the Interoperability.md version-bump duties as seen from the
// hooks the IS-04 side installs.

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is11"
	httpsession "dhs/internal/amwa/session/http"
)

const (
	// scCovSenderNarrow advertises a seeded (narrow) supported list and
	// feeds no Input.
	scCovSenderNarrow = "66666666-6666-4666-8666-666666666666"
	// scCovReceiverBare drives no Output.
	scCovReceiverBare = "77777777-7777-4777-8777-777777777777"
	// scCovInputNoEDID is an Input without EDID support at all.
	scCovInputNoEDID = "88888888-8888-4888-8888-888888888888"
	// scCovInputBadSeed carries an undecodable EDID in the bundle.
	scCovInputBadSeed = "99999999-9999-4999-8999-999999999999"
	// scCovInputOrphan supports Base EDID but feeds no Sender.
	scCovInputOrphan = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	// scCovOutputEDID exposes a bundle-seeded EDID downstream.
	scCovOutputEDID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	// scCovOutputBadSeed claims EDID support but its seed is undecodable.
	scCovOutputBadSeed = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	// scCovGhostInput is associated with scSender but never declared
	// as an Input — a dangling association the server must tolerate.
	scCovGhostInput = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	// scCovDeviceOrphan owns scCovInputOrphan.
	scCovDeviceOrphan = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
)

// scCovInputEDID is the bundle-seeded Base EDID of scInput: the
// default block with a manufacturer id, so "served verbatim" is
// distinguishable from "served the device default".
func scCovInputEDID() []byte {
	b := defaultEDID()
	b[8], b[9] = 0x10, 0xAC
	return edidVariant(b, 1) // epoch 1 only to recompute the checksum
}

// scCovOutputEDIDBlob is the EDID scCovOutputEDID exposes downstream.
func scCovOutputEDIDBlob() []byte {
	b := defaultEDID()
	b[8], b[9] = 0x22, 0xF0
	return edidVariant(b, 1)
}

func scCovInput(id string, edid, base bool, dev string) is11.Input {
	return is11.Input{
		ResourceCore: is11.ResourceCore{ID: id, Version: "1:0", Label: "IN-" + id[:4],
			Description: "x", Tags: map[string][]string{}},
		BaseEDIDSupport: base,
		Connected:       true,
		EDIDSupport:     edid,
		Status:          is11.Status{State: is11.InputSignalPresent},
		DeviceID:        dev,
	}
}

func scCovOutput(id string, edid bool) is11.Output {
	return is11.Output{
		ResourceCore: is11.ResourceCore{ID: id, Version: "1:0", Label: "OUT-" + id[:4],
			Description: "x", Tags: map[string][]string{}},
		Connected:   true,
		EDIDSupport: edid,
		Status:      is11.Status{State: is11.OutputSignalPresent},
		DeviceID:    scDevice,
	}
}

// scCovBundle widens scBundle with the resources the contract needs:
// a second Sender/Receiver with no associations, Inputs/Outputs with
// and without EDID support, and seeded EDIDs (one of each undecodable).
func scCovBundle() *NodeConfig {
	b := scBundle()
	b.Senders = append(b.Senders, is04.Sender{
		ResourceCore: is04.ResourceCore{ID: scCovSenderNarrow}, Transport: is04.TransportRTP})
	b.Receivers = append(b.Receivers, is04.Receiver{
		ResourceCore: is04.ResourceCore{ID: scCovReceiverBare}, Transport: is04.TransportRTP})
	seed := b.StreamCompatibility
	seed.Inputs = append(seed.Inputs,
		scCovInput(scCovInputNoEDID, false, false, scDevice),
		scCovInput(scCovInputBadSeed, true, true, scDevice),
		scCovInput(scCovInputOrphan, true, true, scCovDeviceOrphan),
	)
	seed.Outputs = append(seed.Outputs,
		scCovOutput(scCovOutputEDID, true),
		scCovOutput(scCovOutputBadSeed, true),
	)
	seed.SenderInputs[scSender] = []string{scCovGhostInput, scInput}
	seed.SenderSupported = map[string][]string{
		scCovSenderNarrow: {"urn:x-nmos:cap:format:frame_width"},
	}
	seed.EDIDs = map[string]string{
		scInput:            base64.StdEncoding.EncodeToString(scCovInputEDID()),
		scCovInputBadSeed:  "not*base64*",
		scCovOutputEDID:    base64.StdEncoding.EncodeToString(scCovOutputEDIDBlob()),
		scCovOutputBadSeed: "not*base64*",
	}
	return b
}

// scCovServer serves scCovBundle and returns the log the constructor
// wrote, so seed diagnostics can be asserted.
func scCovServer(t *testing.T) (*httptest.Server, *IS11StreamCompatServer, *bytes.Buffer) {
	t.Helper()
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	s := NewIS11StreamCompatServer(logger, scCovBundle(), IS11StreamCompatConfig{APIVer: "v1.0"})
	s.SetSenderActiveFunc(func(string) bool { return false })
	srv := httpsession.NewServer(nil)
	s.Mount(srv)
	ts := httptest.NewServer(srv.MuxHandler())
	t.Cleanup(ts.Close)
	return ts, s, &logBuf
}

// scCovReadBody drains a response and returns status + bytes.
func scCovReadBody(t *testing.T, resp *stdhttp.Response) (int, []byte) {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, b
}

func scCovEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestScRootIndexServesVersionsWithAndWithoutSlash: the API root is
// reachable both as /x-nmos/streamcompatibility and with the trailing
// slash, each listing the mounted minors as "v1.0/".
func TestScRootIndexServesVersionsWithAndWithoutSlash(t *testing.T) {
	ts, _, _ := scCovServer(t)
	for _, p := range []string{"/x-nmos/streamcompatibility", "/x-nmos/streamcompatibility/"} {
		var vers []string
		if code := scGet(t, ts, p, &vers); code != 200 || !scCovEqual(vers, []string{"v1.0/"}) {
			t.Errorf("GET %s = %d %v, want 200 [v1.0/]", p, code, vers)
		}
	}
}

// TestScIndexResourcesFollowRAML: every branch node of the v1.0.0
// route tree answers the child list the RAML defines, sorted, with
// trailing slashes.
func TestScIndexResourcesFollowRAML(t *testing.T) {
	ts, _, _ := scCovServer(t)
	cases := []struct {
		name string
		path string
		want []string
	}{
		{"receivers list every IS-04 receiver sorted", "/receivers/",
			[]string{scReceiver + "/", scCovReceiverBare + "/"}},
		{"inputs list every seeded input sorted", "/inputs/",
			[]string{scInput + "/", scCovInputNoEDID + "/", scCovInputBadSeed + "/", scCovInputOrphan + "/"}},
		{"outputs list every seeded output sorted", "/outputs/",
			[]string{scOutput + "/", scCovOutputEDID + "/", scCovOutputBadSeed + "/"}},
		{"sender exposes constraints inputs status", "/senders/" + scSender + "/",
			[]string{"constraints/", "inputs/", "status/"}},
		{"sender constraints exposes active supported", "/senders/" + scSender + "/constraints/",
			[]string{"active/", "supported/"}},
		{"receiver exposes outputs status", "/receivers/" + scReceiver + "/",
			[]string{"outputs/", "status/"}},
		{"input exposes edid properties", "/inputs/" + scInput + "/",
			[]string{"edid/", "properties/"}},
		{"input edid exposes base effective", "/inputs/" + scInput + "/edid/",
			[]string{"base/", "effective/"}},
		{"output exposes edid properties", "/outputs/" + scOutput + "/",
			[]string{"edid/", "properties/"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			if code := scGet(t, ts, scBase+tc.path, &got); code != 200 || !scCovEqual(got, tc.want) {
				t.Errorf("GET %s = %d %v, want 200 %v", tc.path, code, got, tc.want)
			}
		})
	}
}

// TestScSupportedConstraintsSeededOrDefault: a Sender seeded with
// sender_supported advertises exactly that list; an unseeded Sender
// advertises the BCP-004-01 set opening with the three meta URNs.
func TestScSupportedConstraintsSeededOrDefault(t *testing.T) {
	ts, _, _ := scCovServer(t)
	get := func(id string) []string {
		var sc is11.SupportedConstraints
		if code := scGet(t, ts, scBase+"/senders/"+id+"/constraints/supported/", &sc); code != 200 {
			t.Fatalf("supported %s = %d", id, code)
		}
		return sc.ParameterConstraints
	}
	if got := get(scCovSenderNarrow); !scCovEqual(got, []string{"urn:x-nmos:cap:format:frame_width"}) {
		t.Errorf("seeded supported = %v, want the seed list only", got)
	}
	def := get(scSender)
	if len(def) < 4 || def[0] != "urn:x-nmos:cap:meta:label" ||
		def[1] != "urn:x-nmos:cap:meta:preference" || def[2] != "urn:x-nmos:cap:meta:enabled" {
		t.Errorf("default supported = %v, want the three meta URNs first", def)
	}
	seen := map[string]bool{}
	for _, u := range def {
		seen[u] = true
	}
	for _, u := range []string{"urn:x-nmos:cap:format:frame_width", "urn:x-nmos:cap:format:sample_rate",
		"urn:x-nmos:cap:transport:packet_time"} {
		if !seen[u] {
			t.Errorf("default supported lacks %s", u)
		}
	}
}

// TestScPutActiveHonoursSeededSupportedList: on a Sender with a
// narrow seeded list, a URN outside it is 400 (RAML "doesn't support
// a Parameter Constraint URN"), a URN inside it is 200, and meta
// URNs are never checked against the list.
func TestScPutActiveHonoursSeededSupportedList(t *testing.T) {
	ts, _, _ := scCovServer(t)
	p := scBase + "/senders/" + scCovSenderNarrow + "/constraints/active/"
	cases := []struct {
		name string
		body string
		want int
	}{
		{"URN outside the seeded list is refused",
			`{"constraint_sets":[{"urn:x-nmos:cap:format:grain_rate":{"enum":[{"numerator":25,"denominator":1}]}}]}`, 400},
		{"URN inside the seeded list is applied",
			`{"constraint_sets":[{"urn:x-nmos:cap:format:frame_width":{"enum":[1920]}}]}`, 200},
		{"meta URNs pass without being in the list",
			`{"constraint_sets":[{"urn:x-nmos:cap:meta:label":"hd","urn:x-nmos:cap:meta:enabled":true,` +
				`"urn:x-nmos:cap:format:frame_width":{"enum":[1280]}}]}`, 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body := scCovReadBody(t, scDo(t, ts, stdhttp.MethodPut, p, []byte(tc.body), "application/json"))
			if code != tc.want {
				t.Errorf("PUT = %d %s, want %d", code, body, tc.want)
			}
		})
	}
	var st is11.Status
	scGet(t, ts, scBase+"/senders/"+scCovSenderNarrow+"/status/", &st)
	if st.State != is11.SenderConstrained {
		t.Errorf("state after accepted PUTs = %s, want constrained", st.State)
	}
}

// scCovFailingBody is a request body whose first read fails —
// the client dropped mid-upload.
type scCovFailingBody struct{}

func (scCovFailingBody) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

// TestScPutActiveUnreadableBodyIs400: a body that cannot be read is
// a client error, reported as 400 with the read failure in debug.
func TestScPutActiveUnreadableBodyIs400(t *testing.T) {
	s := NewIS11StreamCompatServer(slog.New(slog.NewTextHandler(io.Discard, nil)), scCovBundle(),
		IS11StreamCompatConfig{APIVer: "v1.0"})
	req := httptest.NewRequest(stdhttp.MethodPut, scBase+"/senders/"+scSender+"/constraints/active/",
		scCovFailingBody{})
	code, body, err := s.putActive(scSender, req)
	if err != nil {
		t.Fatalf("putActive returned transport error %v", err)
	}
	eb, ok := body.(httpsession.ErrorBody)
	if code != 400 || !ok || eb.Error != "Unreadable body" || !strings.Contains(eb.Debug, "connection reset") {
		t.Errorf("putActive = %d %+v, want 400 Unreadable body carrying the read error", code, body)
	}
	if _, has := s.active[scSender]; has {
		t.Error("an unreadable PUT must not change Active Constraints")
	}
}

// TestScUnassociatedEndpointsListEmptyArray: the IS-11 data model
// allows 0..n Inputs per Sender and Outputs per Receiver — an
// endpoint with none answers a JSON array, never null.
func TestScUnassociatedEndpointsListEmptyArray(t *testing.T) {
	ts, _, _ := scCovServer(t)
	cases := []struct {
		name, path string
	}{
		{"sender feeding no input", "/senders/" + scCovSenderNarrow + "/inputs/"},
		{"receiver driving no output", "/receivers/" + scCovReceiverBare + "/outputs/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body := scCovReadBody(t, scDo(t, ts, stdhttp.MethodGet, scBase+tc.path, nil, ""))
			if code != 200 || strings.TrimSpace(string(body)) != "[]" {
				t.Errorf("GET %s = %d %q, want 200 []", tc.path, code, body)
			}
		})
	}
}

// TestScInputWithoutEDIDSupport: an Input with edid_support=false has
// no Effective or Base EDID (204) and refuses Base EDID writes with
// 405 (RAML "Input does not support EDID").
func TestScInputWithoutEDIDSupport(t *testing.T) {
	ts, _, _ := scCovServer(t)
	in := scBase + "/inputs/" + scCovInputNoEDID + "/edid/"
	cases := []struct {
		name, method, path string
		body               []byte
		want               int
	}{
		{"effective EDID is absent", stdhttp.MethodGet, in + "effective/", nil, 204},
		{"base EDID is absent", stdhttp.MethodGet, in + "base/", nil, 204},
		{"PUT base EDID is not allowed", stdhttp.MethodPut, in + "base/", make([]byte, 128), 405},
		{"DELETE base EDID is not allowed", stdhttp.MethodDelete, in + "base/", nil, 405},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body := scCovReadBody(t, scDo(t, ts, tc.method, tc.path, tc.body, "application/octet-stream"))
			if code != tc.want {
				t.Errorf("%s %s = %d %s, want %d", tc.method, tc.path, code, body, tc.want)
			}
		})
	}
}

// TestScOutputEDIDAndProperties: an EDID-capable Output serves its
// downstream EDID verbatim as octet-stream and its properties as the
// IS-11 output resource.
func TestScOutputEDIDAndProperties(t *testing.T) {
	ts, _, _ := scCovServer(t)
	resp := scDo(t, ts, stdhttp.MethodGet, scBase+"/outputs/"+scCovOutputEDID+"/edid/", nil, "")
	ct := resp.Header.Get("Content-Type")
	code, blob := scCovReadBody(t, resp)
	if code != 200 || ct != "application/octet-stream" || !bytes.Equal(blob, scCovOutputEDIDBlob()) {
		t.Errorf("output EDID = %d %s (%d bytes), want 200 octet-stream with the seeded blob", code, ct, len(blob))
	}
	var out is11.Output
	if c := scGet(t, ts, scBase+"/outputs/"+scCovOutputEDID+"/properties/", &out); c != 200 ||
		out.ID != scCovOutputEDID || !out.EDIDSupport || out.DeviceID != scDevice {
		t.Errorf("output properties = %d %+v", c, out)
	}
}

// TestScSeedEDIDs: a bundle EDID decodes into the Input's initial Base
// EDID (served verbatim); an undecodable one is logged and leaves the
// resource without an EDID rather than serving garbage.
func TestScSeedEDIDs(t *testing.T) {
	ts, _, logBuf := scCovServer(t)

	resp := scDo(t, ts, stdhttp.MethodGet, scBase+"/inputs/"+scInput+"/edid/base/", nil, "")
	code, blob := scCovReadBody(t, resp)
	if code != 200 || !bytes.Equal(blob, scCovInputEDID()) {
		t.Errorf("seeded base EDID = %d (%d bytes), want 200 with the bundle blob", code, len(blob))
	}
	eff := scReadEffective(t, ts, scInput)
	if !bytes.Equal(eff, scCovInputEDID()) {
		t.Error("effective EDID must be the seeded base, served verbatim")
	}

	cases := []struct {
		name, path string
	}{
		{"input with undecodable seed has no base EDID", "/inputs/" + scCovInputBadSeed + "/edid/base/"},
		{"output with undecodable seed has no EDID", "/outputs/" + scCovOutputBadSeed + "/edid/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if c, _ := scCovReadBody(t, scDo(t, ts, stdhttp.MethodGet, scBase+tc.path, nil, "")); c != 204 {
				t.Errorf("GET %s = %d, want 204", tc.path, c)
			}
		})
	}
	log := logBuf.String()
	for _, id := range []string{"input=" + scCovInputBadSeed, "output=" + scCovOutputBadSeed} {
		if !strings.Contains(log, "bad base64 EDID") || !strings.Contains(log, id) {
			t.Errorf("seed diagnostics lack a warning naming %s:\n%s", id, log)
		}
	}
}

// scCovRecorder captures hook invocations from handler goroutines.
type scCovRecorder struct {
	mu      sync.Mutex
	devices []string
	senders []string
}

func (r *scCovRecorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.devices, r.senders = nil, nil
}

func (r *scCovRecorder) snapshot() (devs, snds []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.devices...), append([]string(nil), r.senders...)
}

// TestScVersionBumpHooks: Interoperability.md ties versions across
// APIs — a Base EDID change bumps the owning Device and every Sender
// fed by the Input; an Active Constraints change bumps the Sender and
// the Device of every Input it feeds. A dangling association and an
// Input feeding no Sender bump nothing they cannot name.
func TestScVersionBumpHooks(t *testing.T) {
	ts, s, _ := scCovServer(t)
	rec := &scCovRecorder{}
	s.onDeviceChanged = func(dev string) {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		rec.devices = append(rec.devices, dev)
	}
	s.onSenderConstraintsChanged = func(id string) {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		rec.senders = append(rec.senders, id)
	}

	cases := []struct {
		name, method, path string
		body               []byte
		ct                 string
		want               int
		wantDevs, wantSnds []string
	}{
		{"PUT base EDID bumps the device and the fed sender",
			stdhttp.MethodPut, "/inputs/" + scInput + "/edid/base/", make([]byte, 128), "application/octet-stream",
			204, []string{scDevice}, []string{scSender}},
		{"DELETE base EDID bumps the device and the fed sender",
			stdhttp.MethodDelete, "/inputs/" + scInput + "/edid/base/", nil, "",
			204, []string{scDevice}, []string{scSender}},
		{"PUT base EDID on an input feeding no sender bumps no sender",
			stdhttp.MethodPut, "/inputs/" + scCovInputOrphan + "/edid/base/", make([]byte, 128), "application/octet-stream",
			204, []string{scCovDeviceOrphan}, nil},
		{"PUT active constraints bumps the sender and its inputs' device, skipping the dangling input",
			stdhttp.MethodPut, "/senders/" + scSender + "/constraints/active/",
			[]byte(`{"constraint_sets":[{"urn:x-nmos:cap:format:frame_width":{"enum":[1920]}}]}`), "application/json",
			200, []string{scDevice}, []string{scSender}},
		{"DELETE active constraints bumps the same",
			stdhttp.MethodDelete, "/senders/" + scSender + "/constraints/active/", nil, "",
			200, []string{scDevice}, []string{scSender}},
		{"PUT active constraints on a sender feeding no input bumps only the sender",
			stdhttp.MethodPut, "/senders/" + scCovSenderNarrow + "/constraints/active/",
			[]byte(`{"constraint_sets":[]}`), "application/json",
			200, nil, []string{scCovSenderNarrow}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec.reset()
			code, body := scCovReadBody(t, scDo(t, ts, tc.method, scBase+tc.path, tc.body, tc.ct))
			if code != tc.want {
				t.Fatalf("%s %s = %d %s, want %d", tc.method, tc.path, code, body, tc.want)
			}
			devs, snds := rec.snapshot()
			if !scCovEqual(devs, tc.wantDevs) {
				t.Errorf("device bumps = %v, want %v", devs, tc.wantDevs)
			}
			if !scCovEqual(snds, tc.wantSnds) {
				t.Errorf("sender bumps = %v, want %v", snds, tc.wantSnds)
			}
		})
	}
	// The dangling association survives in the Sender's inputs list —
	// the API reports what the bundle states — but never gained a
	// resource or an EDID epoch.
	s.mu.RLock()
	_, ghostInput := s.inputs[scCovGhostInput]
	_, ghostEpoch := s.edidEpoch[scCovGhostInput]
	s.mu.RUnlock()
	if ghostInput {
		t.Error("dangling association must not materialise an Input")
	}
	if ghostEpoch {
		t.Error("dangling association must not gain an EDID epoch")
	}
}
