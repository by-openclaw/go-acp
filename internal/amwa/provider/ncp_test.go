package provider

// IS-12 NCP WebSocket over a live Node: the control endpoint is
// advertised, Commands drive the SAME model IS-14 serves, a
// Subscription turns a Set into a PropertyChanged Notification, and
// protocol errors answer without dropping the socket.

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is12"
	httpsession "dhs/internal/amwa/session/http"
)

func serveNCPNode(t *testing.T) string {
	return serveNCPBundleNode(t, validBundle())
}

func serveNCPBundleNode(t *testing.T, bundle *NodeConfig) string {
	t.Helper()
	addr := freeAddr(t)
	s, err := NewIS04NodeServer(nil, bundle, IS04NodeConfig{
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

func ncpDial(t *testing.T, addr string) *httpsession.WebSocket {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, err := httpsession.DialWebSocket(ctx, "ws://"+addr+"/x-nmos/ncp/v1.0", nil)
	if err != nil {
		t.Fatalf("dial ncp: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return ws
}

func ncpRoundTrip(t *testing.T, ws *httpsession.WebSocket, m is12.Message) is12.Message {
	t.Helper()
	raw, err := is12.Encode(m)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := ws.SendText(raw); err != nil {
		t.Fatalf("send: %v", err)
	}
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	resp, err := ws.ReadText()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	out, err := is12.Decode(resp)
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}

func TestNCPControlAdvertised(t *testing.T) {
	addr := serveNCPNode(t)
	st, body := mxlGet(t, "http://"+addr+"/x-nmos/node/v1.3/devices")
	if st != 200 {
		t.Fatalf("devices GET = %d", st)
	}
	if !strings.Contains(string(body), NCPControlType+ncpWireVersion) {
		t.Errorf("devices do not advertise %s: %s", NCPControlType+ncpWireVersion, body)
	}
	if !strings.Contains(string(body), "ws://") {
		t.Errorf("ncp control href must be a ws:// URL: %s", body)
	}
}

func TestNCPGetAndErrors(t *testing.T) {
	addr := serveNCPNode(t)
	ws := ncpDial(t, addr)

	// Get root role (NcObject 1p5) on oid 1.
	resp := ncpRoundTrip(t, ws, is12.CommandMessage{Commands: []is12.Command{{
		Handle: 7, OID: 1, MethodID: is12.MethodID{Level: 1, Index: 1},
		Arguments: json.RawMessage(`{"id":{"level":1,"index":5}}`),
	}}})
	cr, ok := resp.(is12.CommandResponseMessage)
	if !ok {
		t.Fatalf("response type %T", resp)
	}
	if len(cr.Responses) != 1 || cr.Responses[0].Handle != 7 {
		t.Fatalf("responses = %+v", cr.Responses)
	}
	if cr.Responses[0].Result.Status != 200 || string(cr.Responses[0].Result.Value) != `"root"` {
		t.Errorf("root role result = %+v", cr.Responses[0].Result)
	}

	// Unknown oid answers per-command, not with a socket error.
	resp = ncpRoundTrip(t, ws, is12.CommandMessage{Commands: []is12.Command{{
		Handle: 8, OID: 9999, MethodID: is12.MethodID{Level: 1, Index: 1},
		Arguments: json.RawMessage(`{"id":{"level":1,"index":5}}`),
	}}})
	cr = resp.(is12.CommandResponseMessage)
	if cr.Responses[0].Result.Status != 404 {
		t.Errorf("bad oid status = %d, want 404", cr.Responses[0].Result.Status)
	}

	// Garbage frame answers a protocol Error and keeps the socket.
	if err := ws.SendText([]byte(`{"messageType":`)); err != nil {
		t.Fatalf("send garbage: %v", err)
	}
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	raw, err := ws.ReadText()
	if err != nil {
		t.Fatalf("read after garbage: %v", err)
	}
	em, err := is12.Decode(raw)
	if err != nil {
		t.Fatalf("decode error frame: %v", err)
	}
	if e, ok := em.(is12.ErrorMessage); !ok || e.Status != 400 {
		t.Errorf("garbage answer = %#v, want ErrorMessage 400", em)
	}

	// Root block members list the model.
	resp = ncpRoundTrip(t, ws, is12.CommandMessage{Commands: []is12.Command{{
		Handle: 9, OID: 1, MethodID: is12.MethodID{Level: 2, Index: 1},
		Arguments: json.RawMessage(`{}`),
	}}})
	cr = resp.(is12.CommandResponseMessage)
	if cr.Responses[0].Result.Status != 200 || !strings.Contains(string(cr.Responses[0].Result.Value), "DeviceManager") {
		t.Errorf("members = %+v", cr.Responses[0].Result)
	}

	// ClassManager GetControlClass for NcObject [1].
	resp = ncpRoundTrip(t, ws, is12.CommandMessage{Commands: []is12.Command{{
		Handle: 10, OID: 1, MethodID: is12.MethodID{Level: 3, Index: 1},
		Arguments: json.RawMessage(`{"classId":[1],"includeInherited":true}`),
	}}})
	cr = resp.(is12.CommandResponseMessage)
	if cr.Responses[0].Result.Status != 200 || !strings.Contains(string(cr.Responses[0].Result.Value), "NcObject") {
		t.Errorf("GetControlClass = %+v", cr.Responses[0].Result)
	}
}

// TestBCP008MonitorFollowsActivation: every sender/receiver carries a
// status monitor whose statuses boot Inactive and flip to Healthy when
// IS-05 activates the endpoint (BCP-008 test_02 behaviour), and the
// monitor methods answer over NCP.
func TestBCP008MonitorFollowsActivation(t *testing.T) {
	addr := serveNCPBundleNode(t, audioBundle())

	// The monitor exists and boots Inactive.
	st, body := mxlGet(t, "http://"+addr+"/x-nmos/configuration/v1.0/rolePaths/root.ReceiverMonitor-00/properties/3p1/value")
	if st != 200 || !strings.Contains(string(body), `"value": 0`) && !strings.Contains(string(body), `"value":0`) {
		t.Fatalf("monitor overallStatus before activation = %d %s", st, body)
	}

	// Activate the receiver over IS-05.
	rid := ""
	for _, r := range audioBundle().Receivers {
		rid = r.ID
		break
	}
	patch := `{"master_enable":true,"activation":{"mode":"activate_immediate","requested_time":null,"activation_time":null}}`
	req, _ := stdhttp.NewRequest(stdhttp.MethodPatch,
		"http://"+addr+"/x-nmos/connection/v1.2/single/receivers/"+rid+"/staged",
		strings.NewReader(patch))
	req.Header.Set("Content-Type", "application/json")
	resp, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("activate receiver: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("activate receiver: status=%d", resp.StatusCode)
	}

	// The flip happens via a goroutine — poll briefly.
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, body = mxlGet(t, "http://"+addr+"/x-nmos/configuration/v1.0/rolePaths/root.ReceiverMonitor-00/properties/3p1/value")
		if strings.Contains(string(body), `"value": 1`) || strings.Contains(string(body), `"value":1`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("monitor never went Healthy: %s", body)
		}
		time.Sleep(25 * time.Millisecond)
	}

	// Monitor methods over NCP: counters getter + reset answer OK.
	ws := ncpDial(t, addr)
	resp2 := ncpRoundTrip(t, ws, is12.CommandMessage{Commands: []is12.Command{
		{Handle: 20, OID: 5, MethodID: is12.MethodID{Level: 4, Index: 1}, Arguments: json.RawMessage(`{}`)},
		{Handle: 21, OID: 5, MethodID: is12.MethodID{Level: 4, Index: 3}, Arguments: json.RawMessage(`{}`)},
	}})
	cr := resp2.(is12.CommandResponseMessage)
	for _, r := range cr.Responses {
		if r.Result.Status != 200 {
			t.Errorf("monitor method handle %d = %+v", r.Handle, r.Result)
		}
	}
}

func TestNCPSubscriptionNotifiesOnSet(t *testing.T) {
	addr := serveNCPNode(t)
	ws := ncpDial(t, addr)

	// Subscribe to root (oid 1) — unknown oids are dropped from the
	// accepted list.
	resp := ncpRoundTrip(t, ws, is12.SubscriptionMessage{Subscriptions: []int{1, 4242}})
	sr, ok := resp.(is12.SubscriptionResponseMessage)
	if !ok || len(sr.Subscriptions) != 1 || sr.Subscriptions[0] != 1 {
		t.Fatalf("subscription response = %#v", resp)
	}

	// Set root userLabel (1p6) → CommandResponse + Notification.
	raw, _ := is12.Encode(is12.CommandMessage{Commands: []is12.Command{{
		Handle: 11, OID: 1, MethodID: is12.MethodID{Level: 1, Index: 2},
		Arguments: json.RawMessage(`{"id":{"level":1,"index":6},"value":"ncp-label"}`),
	}}})
	if err := ws.SendText(raw); err != nil {
		t.Fatalf("send set: %v", err)
	}
	gotResponse, gotNotification := false, false
	for i := 0; i < 2; i++ {
		_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
		frame, err := ws.ReadText()
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		m, err := is12.Decode(frame)
		if err != nil {
			t.Fatalf("decode %d: %v", i, err)
		}
		switch v := m.(type) {
		case is12.CommandResponseMessage:
			gotResponse = true
			if v.Responses[0].Result.Status != 200 {
				t.Errorf("set result = %+v", v.Responses[0].Result)
			}
		case is12.NotificationMessage:
			gotNotification = true
			n := v.Notifications[0]
			if n.OID != 1 || n.EventData.PropertyID != (is12.PropertyID{Level: 1, Index: 6}) {
				t.Errorf("notification = %+v", n)
			}
			if string(n.EventData.Value) != `"ncp-label"` {
				t.Errorf("notification value = %s", n.EventData.Value)
			}
		}
	}
	if !gotResponse || !gotNotification {
		t.Errorf("gotResponse=%v gotNotification=%v", gotResponse, gotNotification)
	}

	// The write is visible over IS-14 — one model, two protocols.
	st, body := mxlGet(t, "http://"+addr+"/x-nmos/configuration/v1.0/rolePaths/root/properties/1p6/value")
	if st != 200 || !strings.Contains(string(body), "ncp-label") {
		t.Errorf("IS-14 read-back = %d %s", st, body)
	}
}
