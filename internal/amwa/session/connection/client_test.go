package connection_test

import (
	"context"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is05"
	"dhs/internal/amwa/session/connection"
)

// THE question this package answers: can IS-05 change a sender's multicast
// address and port on a node?
//
// Yes — and this test pins the exact request a node receives. Values are the
// real ones observed on an EVS Neuron bridge (video sender VTX-01, two
// SMPTE 2022-7 legs on the red and blue fabrics).
func TestSetDestinationSendsMulticastAndPort(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
		gotCT     string
		gotBody   map[string]any
	)

	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		gotMethod, gotPath, gotCT = r.Method, r.URL.Path, r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &gotBody); err != nil {
			t.Errorf("body is not JSON: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
		  "master_enable": true,
		  "activation": {"mode": "activate_immediate", "requested_time": null, "activation_time": "1787500000:0"},
		  "transport_params": [
		    {"destination_ip": "239.131.4.27", "destination_port": 20000, "rtp_enabled": true},
		    {"destination_ip": "239.132.4.27", "destination_port": 20000, "rtp_enabled": true}
		  ]
		}`))
	}))
	defer srv.Close()

	c, err := connection.NewClient(srv.URL + "/x-nmos/connection/v1.1")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	legs := []connection.Leg{
		{DestinationIP: connection.StrPtr("239.131.4.27"), DestinationPort: connection.IntPtr(20000)},
		{DestinationIP: connection.StrPtr("239.132.4.27"), DestinationPort: connection.IntPtr(20000)},
	}
	staged, err := c.PatchStaged(context.Background(), connection.Senders,
		"2c47bf5e-1b2c-4abc-9def-deadbeef0005",
		connection.SetDestination(legs, is05.ActivationModeImmediate))
	if err != nil {
		t.Fatalf("PatchStaged: %v", err)
	}

	// --- the request the node actually receives ---
	if gotMethod != stdhttp.MethodPatch {
		t.Errorf("method = %s, want PATCH (IS-05 staged is a partial update)", gotMethod)
	}
	want := "/x-nmos/connection/v1.1/single/senders/2c47bf5e-1b2c-4abc-9def-deadbeef0005/staged"
	if gotPath != want {
		t.Errorf("path = %s\nwant   %s", gotPath, want)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q, want application/json", gotCT)
	}

	if got, ok := gotBody["master_enable"].(bool); !ok || !got {
		t.Errorf("master_enable = %v, want true — a staged target does nothing until it is set", gotBody["master_enable"])
	}

	act, ok := gotBody["activation"].(map[string]any)
	if !ok {
		t.Fatalf("activation missing: %v", gotBody)
	}
	if act["mode"] != string(is05.ActivationModeImmediate) {
		t.Errorf("activation.mode = %v, want activate_immediate — staging alone never activates", act["mode"])
	}

	tp, ok := gotBody["transport_params"].([]any)
	if !ok {
		t.Fatalf("transport_params missing or not an array: %v", gotBody)
	}
	if len(tp) != 2 {
		t.Fatalf("transport_params has %d entries, want 2 (one per 2022-7 leg, in SDP m= order)", len(tp))
	}
	for i, wantIP := range []string{"239.131.4.27", "239.132.4.27"} {
		leg, ok := tp[i].(map[string]any)
		if !ok {
			t.Fatalf("leg %d is not an object: %v", i, tp[i])
		}
		if leg["destination_ip"] != wantIP {
			t.Errorf("leg %d destination_ip = %v, want %s", i, leg["destination_ip"], wantIP)
		}
		if got, _ := leg["destination_port"].(float64); int(got) != 20000 {
			t.Errorf("leg %d destination_port = %v, want 20000", i, leg["destination_port"])
		}
		// Fields the caller did not set must NOT appear: IS-05 staged is a
		// partial update, and sending source_port:0 would ask for port 0.
		for _, absent := range []string{"source_ip", "source_port", "rtp_enabled"} {
			if _, present := leg[absent]; present {
				t.Errorf("leg %d carries %q although the caller never set it: %v", i, absent, leg)
			}
		}
	}

	// --- and the node's answer decodes ---
	if len(staged.TransportParams) != 2 {
		t.Fatalf("response transport_params = %d, want 2", len(staged.TransportParams))
	}
	if staged.TransportParams[0]["destination_ip"] != "239.131.4.27" {
		t.Errorf("staged leg 1 = %v", staged.TransportParams[0])
	}
	if staged.Activation.Mode != is05.ActivationModeImmediate {
		t.Errorf("staged activation = %q", staged.Activation.Mode)
	}
}

func TestPatchOnlyCarriesWhatWasSet(t *testing.T) {
	// Changing ONLY the port must not silently re-assert an address.
	var body map[string]any
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		_, _ = w.Write([]byte(`{"transport_params":[{}],"activation":{"mode":"activate_immediate"}}`))
	}))
	defer srv.Close()

	c, _ := connection.NewClient(srv.URL + "/x-nmos/connection/v1.1")
	_, err := c.PatchStaged(context.Background(), connection.Senders, "id-1", connection.Patch{
		TransportParams: []connection.Leg{{DestinationPort: connection.IntPtr(30000)}},
	})
	if err != nil {
		t.Fatalf("PatchStaged: %v", err)
	}
	leg := body["transport_params"].([]any)[0].(map[string]any)
	if _, present := leg["destination_ip"]; present {
		t.Errorf("destination_ip appeared in a port-only patch: %v", leg)
	}
	if got, _ := leg["destination_port"].(float64); int(got) != 30000 {
		t.Errorf("destination_port = %v, want 30000", leg["destination_port"])
	}
	if _, present := body["master_enable"]; present {
		t.Errorf("master_enable appeared although it was not set: %v", body)
	}
}

func TestReceiverPathAndTransportFile(t *testing.T) {
	// A receiver is where a controller DOES supply SDP — it is telling the
	// receiver which sender to subscribe to, not editing an SDP.
	var gotPath string
	var body map[string]any
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		_, _ = w.Write([]byte(`{"transport_params":[{}],"activation":{"mode":"activate_immediate"}}`))
	}))
	defer srv.Close()

	c, _ := connection.NewClient(srv.URL + "/x-nmos/connection/v1.1")
	_, err := c.PatchStaged(context.Background(), connection.Receivers, "rx-1", connection.Patch{
		SenderID:      connection.StrPtr("tx-1"),
		TransportFile: &is05.TransportFile{Type: "application/sdp", Data: "v=0\r\n"},
		Activation:    &is05.Activation{Mode: is05.ActivationModeImmediate},
	})
	if err != nil {
		t.Fatalf("PatchStaged: %v", err)
	}
	if !strings.Contains(gotPath, "/single/receivers/rx-1/staged") {
		t.Errorf("path = %s, want the receivers staged endpoint", gotPath)
	}
	tf, ok := body["transport_file"].(map[string]any)
	if !ok || tf["type"] != "application/sdp" {
		t.Errorf("transport_file = %v", body["transport_file"])
	}
	if body["sender_id"] != "tx-1" {
		t.Errorf("sender_id = %v, want tx-1", body["sender_id"])
	}
}

func TestGetEndpoints(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/active"):
			_, _ = w.Write([]byte(`{"master_enable":true,"activation":{"mode":""},"transport_params":[{"destination_ip":"239.131.4.27"}]}`))
		case strings.HasSuffix(r.URL.Path, "/constraints"):
			_, _ = w.Write([]byte(`[{"destination_port":{"minimum":1024,"maximum":65535}},{"destination_port":{"minimum":1024,"maximum":65535}}]`))
		case strings.HasSuffix(r.URL.Path, "/transportfile"):
			w.Header().Set("Content-Type", "application/sdp")
			_, _ = w.Write([]byte("v=0\r\ns=VTX-01\r\n"))
		default:
			w.WriteHeader(stdhttp.StatusNotFound)
		}
	}))
	defer srv.Close()
	c, _ := connection.NewClient(srv.URL + "/x-nmos/connection/v1.1")
	ctx := context.Background()

	act, err := c.Active(ctx, connection.Senders, "id-1")
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	if act.TransportParams[0]["destination_ip"] != "239.131.4.27" {
		t.Errorf("active leg 1 = %v", act.TransportParams[0])
	}

	cons, err := c.Constraints(ctx, connection.Senders, "id-1")
	if err != nil {
		t.Fatalf("Constraints: %v", err)
	}
	if len(cons) != 2 {
		t.Errorf("constraints = %d entries, want one per leg", len(cons))
	}

	sdp, err := c.TransportFile(ctx, "id-1")
	if err != nil {
		t.Fatalf("TransportFile: %v", err)
	}
	if !strings.Contains(string(sdp), "s=VTX-01") {
		t.Errorf("transport file = %q", sdp)
	}
}

func TestRejects(t *testing.T) {
	if _, err := connection.NewClient(""); err == nil {
		t.Error("empty base URL must be rejected")
	}
	if _, err := connection.NewClient("10.44.72.18:3000"); err == nil {
		t.Error("base URL without a scheme must be rejected")
	}

	c, _ := connection.NewClient("http://example.invalid/x-nmos/connection/v1.1")
	ctx := context.Background()
	if _, err := c.PatchStaged(ctx, connection.Senders, "", connection.Patch{}); err == nil {
		t.Error("empty resource id must be rejected")
	}
	bad := connection.Patch{Activation: &is05.Activation{Mode: is05.ActivationMode("activate_whenever")}}
	if _, err := c.PatchStaged(ctx, connection.Senders, "id-1", bad); err == nil {
		t.Error("an activation mode outside the spec set must be rejected before it reaches the node")
	}

	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusBadRequest)
	}))
	defer srv.Close()
	c2, _ := connection.NewClient(srv.URL + "/x-nmos/connection/v1.1")
	if _, err := c2.PatchStaged(ctx, connection.Senders, "id-1", connection.Patch{}); err == nil {
		t.Error("a 400 from the node must be an error")
	}
}

func TestStagedRead(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if !strings.HasSuffix(r.URL.Path, "/staged") {
			w.WriteHeader(stdhttp.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"master_enable":false,"activation":{"mode":""},"transport_params":[{"destination_port":20000}]}`))
	}))
	defer srv.Close()

	c, _ := connection.NewClient(srv.URL + "/x-nmos/connection/v1.1")
	st, err := c.Staged(context.Background(), connection.Senders, "id-1")
	if err != nil {
		t.Fatalf("Staged: %v", err)
	}
	// A staged target that is not master-enabled emits nothing — reading it is
	// how a controller checks before activating.
	if st.MasterEnable {
		t.Errorf("master_enable = true, want false")
	}
	if len(st.TransportParams) != 1 {
		t.Fatalf("transport_params = %d, want 1", len(st.TransportParams))
	}
}

func TestBoolPtr(t *testing.T) {
	if p := connection.BoolPtr(true); p == nil || !*p {
		t.Errorf("BoolPtr(true) = %v", p)
	}
	if p := connection.BoolPtr(false); p == nil || *p {
		t.Errorf("BoolPtr(false) = %v", p)
	}
	// Disabling a sender is master_enable:false, which must survive omitempty
	// as an explicit false rather than vanishing.
	var body map[string]any
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		_, _ = w.Write([]byte(`{"transport_params":[],"activation":{"mode":""}}`))
	}))
	defer srv.Close()
	c, _ := connection.NewClient(srv.URL + "/x-nmos/connection/v1.1")
	if _, err := c.PatchStaged(context.Background(), connection.Senders, "id-1",
		connection.Patch{MasterEnable: connection.BoolPtr(false)}); err != nil {
		t.Fatalf("PatchStaged: %v", err)
	}
	v, present := body["master_enable"]
	if !present {
		t.Fatal("master_enable:false was dropped — a pointer field must survive omitempty")
	}
	if v != false {
		t.Errorf("master_enable = %v, want false", v)
	}
}

func TestTransportErrors(t *testing.T) {
	// Unreachable host: every accessor must report the failure, never a
	// zero-valued struct that looks like a healthy answer.
	c, _ := connection.NewClient("http://127.0.0.1:1/x-nmos/connection/v1.1")
	ctx := context.Background()
	if _, err := c.Staged(ctx, connection.Senders, "id-1"); err == nil {
		t.Error("Staged must fail on an unreachable host")
	}
	if _, err := c.Constraints(ctx, connection.Senders, "id-1"); err == nil {
		t.Error("Constraints must fail on an unreachable host")
	}
	if _, err := c.TransportFile(ctx, "id-1"); err == nil {
		t.Error("TransportFile must fail on an unreachable host")
	}
	if _, err := c.PatchStaged(ctx, connection.Senders, "id-1", connection.Patch{}); err == nil {
		t.Error("PatchStaged must fail on an unreachable host")
	}

	// Malformed bodies must not decode into a plausible-looking struct.
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		_, _ = w.Write([]byte(`{`))
	}))
	defer srv.Close()
	c2, _ := connection.NewClient(srv.URL + "/x-nmos/connection/v1.1")
	if _, err := c2.Staged(ctx, connection.Senders, "id-1"); err == nil {
		t.Error("undecodable staged body must be an error")
	}
	if _, err := c2.Constraints(ctx, connection.Senders, "id-1"); err == nil {
		t.Error("undecodable constraints body must be an error")
	}
	if _, err := c2.PatchStaged(ctx, connection.Senders, "id-1", connection.Patch{}); err == nil {
		t.Error("undecodable patch response must be an error")
	}

	// A 404 on any GET is an error, not an empty result.
	srv2 := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusNotFound)
	}))
	defer srv2.Close()
	c3, _ := connection.NewClient(srv2.URL + "/x-nmos/connection/v1.1")
	if _, err := c3.TransportFile(ctx, "id-1"); err == nil {
		t.Error("404 on transportfile must be an error")
	}
}
