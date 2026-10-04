package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	stdhttp "net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dhs/internal/amwa/consumer"
	"dhs/internal/amwa/provider"
	"dhs/internal/transport/testnet"
)

const (
	rtA = "2c47bf5e-1b2c-4abc-9def-deadbeef0006"
	rtB = "2c47bf5e-1b2c-4abc-9def-deadbeef0024"
	rtS = "2c47bf5e-1b2c-4abc-9def-deadbeef0005"
)

func TestParseRoute(t *testing.T) {
	cases := []struct {
		arg, receiver, sender string
		wantErr               bool
	}{
		{rtA + "=" + rtS, rtA, rtS, false},
		{" " + rtA + " = " + rtS + " ", rtA, rtS, false},
		{rtA + "=", rtA, "", false}, // disconnect
		{rtA, "", "", true},         // no '='
		{"=" + rtS, "", "", true},   // no receiver
	}
	for _, tc := range cases {
		receiver, sender, err := parseRoute(tc.arg)
		if (err != nil) != tc.wantErr || receiver != tc.receiver || sender != tc.sender {
			t.Errorf("parseRoute(%q) = %q, %q, %v", tc.arg, receiver, sender, err)
		}
	}
}

func TestReadRoutes(t *testing.T) {
	got, err := readRoutes(strings.NewReader(
		"# studio A salvo\nreceiver,sender\n\n" + rtA + "," + rtS + "\n  " + rtB + " ,\n" + rtS + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := [][2]string{{rtA, rtS}, {rtB, ""}, {rtS, ""}}
	if len(got) != len(want) {
		t.Fatalf("routes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("route %d = %v, want %v", i, got[i], want[i])
		}
	}
	if _, err := readRoutes(strings.NewReader(rtA + "," + rtS + "\n," + rtS + "\n")); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("a line with no receiver: err = %v, want it named by line", err)
	}
	if _, err := readRoutes(failingReader{}); err == nil {
		t.Error("a read failure must surface")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, fmt.Errorf("disk gone") }

func TestBulkRequests(t *testing.T) {
	file := filepath.Join(t.TempDir(), "routes.csv")
	if err := os.WriteFile(file, []byte(rtB+",\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reqs, err := bulkRequests([]string{rtA + "=" + rtS}, file, "http://10.0.0.9:3000", "activate_scheduled_relative", "2:0", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 2 || reqs[0].ReceiverID != rtA || reqs[0].SenderID != rtS || reqs[1].ReceiverID != rtB || reqs[1].SenderID != "" {
		t.Fatalf("requests = %+v, want the flag's route then the file's", reqs)
	}
	for _, r := range reqs {
		if r.SenderNode != "http://10.0.0.9:3000" || r.Mode != "activate_scheduled_relative" || r.When != "2:0" || !r.Force {
			t.Errorf("the shared flags did not reach %+v", r)
		}
	}
	if _, err := bulkRequests([]string{"nonsense"}, "", "", "", "", false); err == nil {
		t.Error("a malformed --route must be refused")
	}
	if _, err := bulkRequests(nil, filepath.Join(t.TempDir(), "absent.csv"), "", "", "", false); err == nil {
		t.Error("a missing --routes file must be refused")
	}
	bad := filepath.Join(t.TempDir(), "bad.csv")
	_ = os.WriteFile(bad, []byte(","+rtS+"\n"), 0o600)
	if _, err := bulkRequests(nil, bad, "", "", "", false); err == nil || !strings.Contains(err.Error(), "bad.csv") {
		t.Errorf("a malformed --routes file: err = %v, want the file named", err)
	}
}

// The report: one line per route with the Device's code, a count line,
// and a non-nil error when anything was not applied — a salvo that
// half landed must not exit 0.
func TestBulkReport(t *testing.T) {
	reqs := []consumer.ConnectRequest{{ReceiverID: rtA, SenderID: rtS}, {ReceiverID: rtB}}
	cur := rtS
	res := &consumer.BulkConnectResult{Requests: 1, Routes: []consumer.BulkRouteResult{
		{ConnectResult: consumer.ConnectResult{ReceiverID: rtA, Endpoint: "http://d/x-nmos/connection/v1.1"}, Code: 200},
		{ConnectResult: consumer.ConnectResult{ReceiverID: rtB, Endpoint: "http://d/x-nmos/connection/v1.1"}, Code: 423, Error: "Resource is locked"},
	}}
	text, err := bulkReport(reqs, res, false)
	if err == nil || !strings.Contains(err.Error(), "1 of 2 routes") {
		t.Errorf("err = %v, want the failed count", err)
	}
	for _, want := range []string{
		"OK   200  " + rtA + " <- " + rtS,
		"FAIL 423  " + rtB + " <- (disconnect)  (Resource is locked)",
		"2 routes in 1 bulk requests: 1 applied, 1 not applied",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("report lacks %q:\n%s", want, text)
		}
	}

	res.Routes[1].Code, res.Routes[1].Error = 200, ""
	if _, err := bulkReport(reqs, res, false); err != nil {
		t.Errorf("an applied salvo: err = %v", err)
	}

	dry := &consumer.BulkConnectResult{Routes: []consumer.BulkRouteResult{
		{ConnectResult: consumer.ConnectResult{ReceiverID: rtA, Endpoint: "http://d/x-nmos/connection/v1.1", DryRun: true,
			Patch: map[string]any{"master_enable": true}, CurrentSenderID: &cur, CurrentMasterEnable: true}},
		{ConnectResult: consumer.ConnectResult{ReceiverID: rtB, Endpoint: "http://d/x-nmos/connection/v1.1", DryRun: true,
			Patch: map[string]any{"master_enable": false}}},
	}}
	text, err = bulkReport(reqs, dry, true)
	if err != nil {
		t.Errorf("a dry run: err = %v", err)
	}
	for _, want := range []string{"DRY RUN", "via        http://d/x-nmos/connection/v1.1/bulk/receivers",
		"currently  sender=" + rtS + " master_enable=true", "currently  sender=(none) master_enable=false", `"master_enable": true`} {
		if !strings.Contains(text, want) {
			t.Errorf("dry-run report lacks %q:\n%s", want, text)
		}
	}
}

// The verb end to end against a real dhs Node: a salvo of one connect
// and one disconnect goes out, and the Node's own IS-05 /active shows
// both — then the flag conflicts and a refused salvo.
func TestConnectVerbRoutesASalvo(t *testing.T) {
	bundle, err := provider.LoadNodeConfigFromFile("../../tests/integration/nmos/amwa/amwa-test-node.json")
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	addr := testnet.FreeAddr(t)
	node, err := provider.NewIS04NodeServer(nil, bundle, provider.IS04NodeConfig{
		Bind: addr, DiscoveryMode: "static", APIVer: "v1.3", AdvertiseHost: addr,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); _ = node.Stop() })
	go func() { _ = node.Serve(ctx) }()
	base := "http://" + addr
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := stdhttp.Get(base + "/x-nmos/node/v1.3/self")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("node never came up")
		}
		time.Sleep(20 * time.Millisecond)
	}

	active := func(receiver string) (master bool, sender any) {
		t.Helper()
		resp, err := stdhttp.Get(base + "/x-nmos/connection/v1.1/single/receivers/" + receiver + "/active")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		var doc struct {
			MasterEnable bool `json:"master_enable"`
			SenderID     any  `json:"sender_id"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("active %s: %v (%s)", receiver, err, raw)
		}
		return doc.MasterEnable, doc.SenderID
	}

	args := []string{"--node", base, "--route", rtA + "=" + rtS, "--route", rtB + "="}
	if err := runNMOSConnect(ctx, args); err != nil {
		t.Fatalf("connect --route: %v", err)
	}
	if master, sender := active(rtA); !master || sender != rtS {
		t.Errorf("receiver A active = master %v sender %v, want it routed to %s", master, sender, rtS)
	}
	if master, sender := active(rtB); master || sender != nil {
		t.Errorf("receiver B active = master %v sender %v, want it disconnected", master, sender)
	}

	// A dry run sends nothing: A stays routed.
	if err := runNMOSConnect(ctx, []string{"--node", base, "--route", rtA + "=", "--dry-run"}); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if master, _ := active(rtA); !master {
		t.Error("a dry run disconnected the receiver")
	}

	if err := runNMOSConnect(ctx, []string{"--node", base, "--route", rtA + "=" + rtS, "--receiver", rtB}); err == nil ||
		!strings.Contains(err.Error(), "do not combine") {
		t.Errorf("--route with --receiver: err = %v", err)
	}
	if err := runNMOSConnect(ctx, []string{"--node", base, "--route", "nonsense"}); err == nil {
		t.Error("a malformed --route must be refused")
	}
	if err := runNMOSConnect(ctx, []string{"--node", base, "--route", rtA + "=" + rtS, "--route", rtA + "="}); err == nil ||
		!strings.Contains(err.Error(), "routed twice") {
		t.Errorf("a receiver routed twice: err = %v", err)
	}

	// The capability check, through the verb: a websocket tally sender
	// on an RTP audio receiver is refused with the reason and nothing
	// is sent; a sender id that exists nowhere likewise; --force sends
	// the route anyway.
	const tally = "2c47bf5e-1b2c-4abc-9def-deadbeef0009"
	err = runNMOSConnect(ctx, []string{"--node", base, "--receiver", rtB, "--sender", tally})
	if err == nil || !strings.Contains(err.Error(), "the receiver takes rtp, the sender emits websocket") ||
		!strings.Contains(err.Error(), "the receiver is audio, the flow is data") {
		t.Errorf("an incompatible route: err = %v, want the refusal with its reasons", err)
	}
	if master, _ := active(rtB); master {
		t.Error("a refused route reached the receiver")
	}
	err = runNMOSConnect(ctx, []string{"--node", base, "--route", rtB + "=00000000-0000-4000-8000-00000000dead"})
	if err == nil || !strings.Contains(err.Error(), "is not in the catalogue") {
		t.Errorf("an unknown sender in a salvo: err = %v", err)
	}
	if err := runNMOSConnect(ctx, []string{"--node", base, "--receiver", rtB, "--sender", tally, "--force"}); err != nil {
		t.Errorf("--force: %v", err)
	}
	if master, sender := active(rtB); !master || sender != tally {
		t.Errorf("after --force, receiver B active = master %v sender %v", master, sender)
	}
}
