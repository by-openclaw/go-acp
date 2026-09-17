package provider

// BCP-007-03 (NMOS With MXL) provider rules, exercised over a live
// Node with an MXL sender + receiver: transport URN, empty
// interface_bindings, null manifest_href, /transportfile 404, and the
// mxl_domain_id / mxl_flow_id IS-05 transport parameters.

import (
	"context"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"sync"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is04"

	_ "dhs/internal/amwa/codec/is05/v10"
	_ "dhs/internal/amwa/codec/is05/v11"
	_ "dhs/internal/amwa/codec/is05/v12"
)

func mxlBundle(t *testing.T) *NodeConfig {
	t.Helper()
	b := validBundle()
	dev := b.Devices[0].ID
	snd := is04.Sender{
		ResourceCore: is04.ResourceCore{
			ID: "cccccccc-3333-4333-8333-333333333333", Version: "0:0",
			Label: "mxl-snd", Description: "MXL sender", Tags: map[string][]string{},
		},
		Transport:         is04.TransportMXL,
		DeviceID:          dev,
		InterfaceBindings: []string{}, // BCP-007-03: empty
	}
	rcv := is04.Receiver{
		ResourceCore: is04.ResourceCore{
			ID: "dddddddd-4444-4444-8444-444444444444", Version: "0:0",
			Label: "mxl-rcv", Description: "MXL receiver", Tags: map[string][]string{},
		},
		Transport:         is04.TransportMXL,
		DeviceID:          dev,
		Format:            is04.FormatVideo,
		InterfaceBindings: []string{}, // BCP-007-03: empty
	}
	b.Senders = append(b.Senders, snd)
	b.Receivers = append(b.Receivers, rcv)
	b.Devices[0].Senders = []string{snd.ID}
	b.Devices[0].Receivers = []string{rcv.ID}
	return b
}

func serveMXLNode(t *testing.T) string {
	t.Helper()
	addr := freeAddr(t)
	s, err := NewIS04NodeServer(nil, mxlBundle(t), IS04NodeConfig{
		Bind: addr, DiscoveryMode: "static", ConnectionAPIVer: "v1.2", APIVer: "v1.3",
	})
	if err != nil {
		t.Fatalf("NewIS04NodeServer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = s.Serve(ctx) }()
	t.Cleanup(func() { cancel(); _ = s.Stop(); wg.Wait() })
	if !waitReachable(t, "http://"+addr+"/__ready__", 2*time.Second) {
		t.Fatal("server never came up")
	}
	return addr
}

func mxlGet(t *testing.T, url string) (int, []byte) {
	t.Helper()
	resp, err := stdhttp.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// TestMXLFlowIDAutoRules: BCP-007-03 lets a SENDER resolve mxl_flow_id
// via "auto" but a RECEIVER's mxl_flow_id is null-or-UUID only — the
// suite's test_18 rejects an endpoint that accepts "auto" there.
func TestMXLFlowIDAutoRules(t *testing.T) {
	if err := validateParamValue("mxl_flow_id", "auto", true); err != nil {
		t.Errorf("sender auto must be accepted: %v", err)
	}
	if err := validateParamValue("mxl_flow_id", "auto", false); err == nil {
		t.Error("receiver auto must be rejected")
	}
	if err := validateParamValue("mxl_flow_id", nil, false); err != nil {
		t.Errorf("receiver null must be accepted: %v", err)
	}
	if err := validateParamValue("mxl_flow_id", "00000000-0000-4000-8000-00000000000f", false); err != nil {
		t.Errorf("receiver uuid must be accepted: %v", err)
	}
}

func TestMXLSenderIS04Rules(t *testing.T) {
	addr := serveMXLNode(t)
	sid := "cccccccc-3333-4333-8333-333333333333"

	st, body := mxlGet(t, "http://"+addr+"/x-nmos/node/v1.3/senders/"+sid)
	if st != 200 {
		t.Fatalf("sender GET = %d", st)
	}
	var snd struct {
		Transport         string   `json:"transport"`
		ManifestHref      *string  `json:"manifest_href"`
		InterfaceBindings []string `json:"interface_bindings"`
	}
	if err := json.Unmarshal(body, &snd); err != nil {
		t.Fatalf("decode sender: %v", err)
	}
	if snd.Transport != is04.TransportMXL {
		t.Errorf("transport = %q, want mxl", snd.Transport)
	}
	// BCP-007-03: manifest_href MUST be null.
	if snd.ManifestHref != nil {
		t.Errorf("manifest_href = %v, want null", *snd.ManifestHref)
	}
	// BCP-007-03: interface_bindings MUST be empty.
	if len(snd.InterfaceBindings) != 0 {
		t.Errorf("interface_bindings = %v, want empty", snd.InterfaceBindings)
	}

	// BCP-007-03: /transportfile MUST 404 (IS-04 side).
	if st, _ := mxlGet(t, "http://"+addr+"/x-nmos/node/v1.3/senders/"+sid+"/transportfile"); st != 404 {
		t.Errorf("IS-04 transportfile = %d, want 404", st)
	}
}

func TestMXLIS05TransportParams(t *testing.T) {
	addr := serveMXLNode(t)
	sid := "cccccccc-3333-4333-8333-333333333333"
	rid := "dddddddd-4444-4444-8444-444444444444"

	// Sender: staged + constraints carry mxl_domain_id + mxl_flow_id.
	st, body := mxlGet(t, "http://"+addr+"/x-nmos/connection/v1.2/single/senders/"+sid+"/staged")
	if st != 200 {
		t.Fatalf("sender staged = %d %s", st, body)
	}
	var staged struct {
		TransportParams []map[string]json.RawMessage `json:"transport_params"`
	}
	_ = json.Unmarshal(body, &staged)
	if len(staged.TransportParams) != 1 {
		t.Fatalf("MXL uses a single transport-param set, got %d", len(staged.TransportParams))
	}
	for _, k := range []string{"mxl_domain_id", "mxl_flow_id"} {
		if _, ok := staged.TransportParams[0][k]; !ok {
			t.Errorf("sender staged missing %s: %v", k, staged.TransportParams[0])
		}
	}

	st, cbody := mxlGet(t, "http://"+addr+"/x-nmos/connection/v1.2/single/senders/"+sid+"/constraints")
	if st != 200 {
		t.Fatalf("sender constraints = %d", st)
	}
	var cons []map[string]json.RawMessage
	_ = json.Unmarshal(cbody, &cons)
	if len(cons) != 1 {
		t.Fatalf("constraints sets = %d, want 1", len(cons))
	}
	for _, k := range []string{"mxl_domain_id", "mxl_flow_id"} {
		if _, ok := cons[0][k]; !ok {
			t.Errorf("sender constraints missing %s", k)
		}
	}
	// The constraints endpoint MUST NOT offer "auto".
	if string(cons[0]["mxl_domain_id"]) != "{}" {
		t.Errorf("mxl_domain_id constraint = %s, want {} (no auto listed)", cons[0]["mxl_domain_id"])
	}

	// Receiver staged carries both params too.
	st, rbody := mxlGet(t, "http://"+addr+"/x-nmos/connection/v1.2/single/receivers/"+rid+"/staged")
	if st != 200 {
		t.Fatalf("receiver staged = %d", st)
	}
	var rstaged struct {
		TransportParams []map[string]json.RawMessage `json:"transport_params"`
	}
	_ = json.Unmarshal(rbody, &rstaged)
	if len(rstaged.TransportParams) != 1 {
		t.Fatalf("receiver param sets = %d, want 1", len(rstaged.TransportParams))
	}
	for _, k := range []string{"mxl_domain_id", "mxl_flow_id"} {
		if _, ok := rstaged.TransportParams[0][k]; !ok {
			t.Errorf("receiver staged missing %s", k)
		}
	}
	// Receiver's mxl_flow_id MUST be null (never "auto").
	if got := string(rstaged.TransportParams[0]["mxl_flow_id"]); got != "null" {
		t.Errorf("receiver mxl_flow_id = %s, want null", got)
	}

	// IS-05 sender transportfile MUST 404 for MXL.
	if st, _ := mxlGet(t, "http://"+addr+"/x-nmos/connection/v1.2/single/senders/"+sid+"/transportfile"); st != 404 {
		t.Errorf("IS-05 transportfile = %d, want 404", st)
	}
}
