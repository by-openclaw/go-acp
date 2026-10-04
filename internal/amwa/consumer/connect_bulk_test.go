package consumer

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is05"
	"dhs/internal/amwa/codec/spec"
)

// bulkPlant is two Devices behind the harness server, each with its
// own Connection API endpoint: A owns receivers 2 and 3 and the
// sender, B owns receiver 4.
type bulkPlant struct {
	h      *harness
	hrefA  string
	hrefB  string
	mu     sync.Mutex
	posts  map[string][]is05.BulkItem // endpoint ("A"/"B") -> entries received
	single []string                   // receiver ids PATCHed one by one
}

const bulkSDP = "v=0\r\no=- 0 0 IN IP4 10.6.0.9\r\n"

// answer decides what an endpoint says to its bulk POST. Returning a
// status other than 200 makes it the whole request's answer.
type bulkAnswer func(endpoint string, items []is05.BulkItem) (status int, body string)

func allOK(_ string, items []is05.BulkItem) (int, string) {
	out := make([]is05.BulkResult, len(items))
	for i, it := range items {
		out[i] = is05.BulkResult{ID: it.ID, Code: 200}
	}
	b, _ := json.Marshal(out)
	return 200, string(b)
}

func newBulkPlant(t *testing.T, answer bulkAnswer) *bulkPlant {
	t.Helper()
	h := newHarness(t)
	p := &bulkPlant{h: h, posts: map[string][]is05.BulkItem{}}
	p.hrefA = h.controlHref
	p.hrefB = strings.Replace(h.controlHref, "/x-nmos/", "/b/x-nmos/", 1)
	devB := uuidN(50)
	h.cat.devices = []is04.Device{deviceWith(testUUID, p.hrefA), deviceWith(devB, p.hrefB)}
	h.cat.senders = []is04.Sender{senderOn(uuidN(1), "CAM 1", testUUID, is04.TransportRTPMcast)}
	h.cat.receivers = []is04.Receiver{
		receiverOn(uuidN(2), testUUID, is04.TransportRTPMcast),
		receiverOn(uuidN(3), testUUID, is04.TransportRTPMcast),
		receiverOn(uuidN(4), devB, is04.TransportRTPMcast),
	}
	h.is05 = func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		endpoint := "A"
		if strings.HasPrefix(path, "/b/") {
			endpoint = "B"
		}
		switch {
		case strings.HasSuffix(path, "/transportfile"):
			_, _ = io.WriteString(w, bulkSDP)
		case strings.HasSuffix(path, "/bulk/receivers") && r.Method == http.MethodPost:
			var items []is05.BulkItem
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &items); err != nil {
				t.Errorf("bulk body is not an array of entries: %v (%s)", err, raw)
			}
			p.mu.Lock()
			p.posts[endpoint] = append(p.posts[endpoint], items...)
			p.mu.Unlock()
			status, body := answer(endpoint, items)
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
		case strings.HasSuffix(path, "/staged") && r.Method == http.MethodPatch:
			segs := strings.Split(path, "/")
			id := segs[len(segs)-2]
			p.mu.Lock()
			p.single = append(p.single, id)
			p.mu.Unlock()
			if id == uuidN(3) {
				http.Error(w, `{"code":400,"error":"bad transport file"}`, http.StatusBadRequest)
				return
			}
			_, _ = w.Write(stagedReceiverBody(t, nil, true, nil))
		case strings.HasSuffix(path, "/active"):
			sender := uuidN(1)
			_, _ = w.Write(stagedReceiverBody(t, &sender, true, nil))
		default:
			http.Error(w, "unexpected is05 "+path, http.StatusNotFound)
		}
	}
	return p
}

// threeRoutes: connect 2 and 4 to the sender, disconnect 3.
func threeRoutes() []ConnectRequest {
	return []ConnectRequest{
		{ReceiverID: uuidN(2), SenderID: uuidN(1)},
		{ReceiverID: uuidN(3)},
		{ReceiverID: uuidN(4), SenderID: uuidN(1)},
	}
}

func codesFired(rep *spec.SliceReporter) map[string]int {
	out := map[string]int{}
	for _, e := range rep.Snapshot() {
		out[e.Code]++
	}
	return out
}

// A salvo goes out as one POST per Connection API endpoint, each entry
// the body the single PATCH would have carried, and every route gets
// the Device's verdict.
func TestConnectBulkOneRequestPerEndpoint(t *testing.T) {
	p := newBulkPlant(t, allOK)
	res, err := p.h.ctrl.ConnectBulk(context.Background(), threeRoutes(), false)
	if err != nil {
		t.Fatalf("ConnectBulk: %v", err)
	}
	if res.Requests != 2 || res.Failed() != 0 || len(res.Routes) != 3 {
		t.Fatalf("result = %+v, want 2 requests and three applied routes", res)
	}
	for i, rt := range res.Routes {
		if !rt.OK() || rt.Code != 200 || rt.ReceiverID != threeRoutes()[i].ReceiverID {
			t.Errorf("route %d = %+v", i, rt)
		}
	}
	if res.Routes[0].Endpoint != p.hrefA || res.Routes[2].Endpoint != p.hrefB {
		t.Errorf("endpoints = %q, %q", res.Routes[0].Endpoint, res.Routes[2].Endpoint)
	}
	if res.Routes[0].SDPBytes != len(bulkSDP) || res.Routes[1].SDPBytes != 0 {
		t.Errorf("SDP bytes = %d, %d", res.Routes[0].SDPBytes, res.Routes[1].SDPBytes)
	}

	a, b := p.posts["A"], p.posts["B"]
	if len(a) != 2 || len(b) != 1 || a[0].ID != uuidN(2) || a[1].ID != uuidN(3) || b[0].ID != uuidN(4) {
		t.Fatalf("entries: A=%+v B=%+v", a, b)
	}
	// The connect entry: sender, master_enable, the SDP, the activation.
	tf, _ := a[0].Params["transport_file"].(map[string]any)
	act, _ := a[0].Params["activation"].(map[string]any)
	if a[0].Params["sender_id"] != uuidN(1) || a[0].Params["master_enable"] != true ||
		tf["data"] != bulkSDP || act["mode"] != "activate_immediate" {
		t.Errorf("connect entry = %+v", a[0].Params)
	}
	// The disconnect entry: sender_id present AND null, master_enable false.
	if v, present := a[1].Params["sender_id"]; !present || v != nil || a[1].Params["master_enable"] != false {
		t.Errorf("disconnect entry = %+v", a[1].Params)
	}
	if len(p.single) != 0 {
		t.Errorf("a Device serving bulk was also PATCHed singly: %v", p.single)
	}
}

// A partial success stays partial: the refused entry carries the
// Device's code and message, an entry the Device never answered is
// reported as unknown, and a response outside AMWA's schema is on the
// record — while the applied entry is still reported applied.
func TestConnectBulkPartialFailure(t *testing.T) {
	p := newBulkPlant(t, func(endpoint string, items []is05.BulkItem) (int, string) {
		if endpoint == "B" {
			return 200, `[]` // owes a verdict for receiver 4, gives none
		}
		return 200, `[{"id":"` + uuidN(2) + `","code":200},{"id":"` + uuidN(3) + `","code":423,"error":"Resource is locked","extra":1},{"id":"x","code":302}]`
	})
	res, err := p.h.ctrl.ConnectBulk(context.Background(), threeRoutes(), false)
	if err != nil {
		t.Fatalf("ConnectBulk: %v", err)
	}
	if !res.Routes[0].OK() {
		t.Errorf("the applied route = %+v", res.Routes[0])
	}
	if r := res.Routes[1]; r.OK() || r.Code != 423 || r.Error != "Resource is locked" {
		t.Errorf("the refused route = %+v", r)
	}
	if r := res.Routes[2]; r.OK() || r.Code != 0 || !strings.Contains(r.Error, "no verdict") {
		t.Errorf("the unanswered route = %+v", r)
	}
	if res.Failed() != 2 {
		t.Errorf("Failed = %d, want 2", res.Failed())
	}
	fired := codesFired(p.h.rep)
	for _, want := range []string{"nmos_is05_bulk_entry_refused", "nmos_is05_bulk_no_verdict", "nmos_is05_bulk_response_deviation"} {
		if fired[want] != 1 {
			t.Errorf("event %s fired %d times, want once (%v)", want, fired[want], fired)
		}
	}
}

// One Device failing its whole request does not stop the others: its
// routes carry the failure, the other endpoint's routes their verdict.
func TestConnectBulkOneEndpointFails(t *testing.T) {
	p := newBulkPlant(t, func(endpoint string, items []is05.BulkItem) (int, string) {
		if endpoint == "A" {
			return 500, `{"code":500,"error":"scheduler unavailable"}`
		}
		return allOK(endpoint, items)
	})
	res, err := p.h.ctrl.ConnectBulk(context.Background(), threeRoutes(), false)
	if err != nil {
		t.Fatalf("ConnectBulk: %v", err)
	}
	for _, i := range []int{0, 1} {
		if r := res.Routes[i]; r.OK() || r.Code != 0 || !strings.Contains(r.Error, "scheduler unavailable") {
			t.Errorf("route %d on the failed endpoint = %+v", i, r)
		}
	}
	if !res.Routes[2].OK() || res.Failed() != 2 || res.Requests != 2 {
		t.Errorf("result = %+v", res)
	}
	if fired := codesFired(p.h.rep); fired["nmos_is05_bulk_failed"] != 1 {
		t.Errorf("events = %v, want one nmos_is05_bulk_failed", fired)
	}
}

// IS-05 requires /bulk. A Device without it deviates — that is
// recorded — and the salvo still lands, entry by entry on the single
// endpoint, each with the status its PATCH answered.
func TestConnectBulkFallsBackWhenTheDeviceServesNoBulk(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented} {
		p := newBulkPlant(t, func(string, []is05.BulkItem) (int, string) {
			return status, `{"code":404,"error":"no such resource"}`
		})
		res, err := p.h.ctrl.ConnectBulk(context.Background(), threeRoutes(), false)
		if err != nil {
			t.Fatalf("HTTP %d: ConnectBulk: %v", status, err)
		}
		if len(p.single) != 3 {
			t.Fatalf("HTTP %d: single PATCHes = %v, want all three receivers", status, p.single)
		}
		if r := res.Routes[0]; !r.OK() || !r.MasterEnable {
			t.Errorf("HTTP %d: the route PATCHed singly = %+v", status, r)
		}
		// Receiver 3's single PATCH is refused by the stub with a 400.
		if r := res.Routes[1]; r.OK() || r.Code != 400 || !strings.Contains(r.Error, "bad transport file") {
			t.Errorf("HTTP %d: the refused single = %+v", status, r)
		}
		if fired := codesFired(p.h.rep); fired["nmos_is05_bulk_unsupported"] != 2 {
			t.Errorf("HTTP %d: events = %v, want the deviation once per endpoint", status, fired)
		}
	}
}

// A single PATCH that fails below HTTP — the Device is gone — leaves
// the route with the error and no code.
func TestConnectBulkFallbackTransportFailure(t *testing.T) {
	p := newBulkPlant(t, func(string, []is05.BulkItem) (int, string) { return 404, `{}` })
	// Receiver 4's Device answers the bulk POST 404, then its single
	// endpoint is unreachable: hijack the connection on PATCH.
	inner := p.h.is05
	p.h.is05 = func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/b/") {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		inner(w, r)
	}
	res, err := p.h.ctrl.ConnectBulk(context.Background(), threeRoutes(), false)
	if err != nil {
		t.Fatalf("ConnectBulk: %v", err)
	}
	if r := res.Routes[2]; r.OK() || r.Code != 0 || r.Error == "" {
		t.Errorf("the route whose Device went away = %+v", r)
	}
}

// A dry run sends nothing: every route reports the body it would have
// carried and what its receiver is doing now.
func TestConnectBulkDryRun(t *testing.T) {
	p := newBulkPlant(t, allOK)
	res, err := p.h.ctrl.ConnectBulk(context.Background(), threeRoutes(), true)
	if err != nil {
		t.Fatalf("ConnectBulk: %v", err)
	}
	if len(p.posts) != 0 || len(p.single) != 0 || res.Requests != 0 {
		t.Fatalf("a dry run reached the Devices: posts=%v single=%v requests=%d", p.posts, p.single, res.Requests)
	}
	for i, rt := range res.Routes {
		if !rt.DryRun || rt.OK() || rt.Patch == nil || rt.CurrentSenderID == nil || *rt.CurrentSenderID != uuidN(1) || !rt.CurrentMasterEnable {
			t.Errorf("route %d = %+v", i, rt)
		}
	}
	if res.Failed() != 0 {
		t.Errorf("Failed = %d on a dry run, want 0 — nothing was refused", res.Failed())
	}
}

// A salvo that is malformed anywhere is refused whole, before any
// request goes out.
func TestConnectBulkRefusedWholeBeforeAnythingIsSent(t *testing.T) {
	cases := []struct {
		name string
		reqs []ConnectRequest
		want string
	}{
		{"no routes", nil, "at least one route"},
		{"a route without a receiver", []ConnectRequest{{ReceiverID: uuidN(2), SenderID: uuidN(1)}, {SenderID: uuidN(1)}}, "route 2: nmos connect: receiver id is required"},
		{"a route with a bad mode", []ConnectRequest{{ReceiverID: uuidN(2), Mode: "activate_whenever"}}, "route 1:"},
		{"a receiver routed twice", []ConnectRequest{{ReceiverID: uuidN(2), SenderID: uuidN(1)}, {ReceiverID: uuidN(2)}}, "routed twice"},
		{"a receiver the catalogue does not have", []ConnectRequest{{ReceiverID: uuidN(2), SenderID: uuidN(1)}, {ReceiverID: uuidN(77)}}, "route 2: nmos connect: no receiver"},
	}
	for _, tc := range cases {
		p := newBulkPlant(t, allOK)
		_, err := p.h.ctrl.ConnectBulk(context.Background(), tc.reqs, false)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want one mentioning %q", tc.name, err, tc.want)
		}
		if len(p.posts) != 0 || len(p.single) != 0 {
			t.Errorf("%s: something was sent: posts=%v single=%v", tc.name, p.posts, p.single)
		}
	}
}

// Routes whose Sender lives on a Node the catalogue never saw walk
// that Node once for the whole salvo, not once per route.
func TestConnectBulkWalksAForeignSenderNodeOnce(t *testing.T) {
	const foreignSDP = "v=0\r\no=- 1 1 IN IP4 10.6.40.50\r\ns=VTX-01\r\n"
	p := newBulkPlant(t, allOK)
	p.h.cat.senders = nil // the plant's own catalogue knows no sender
	sn := senderNode(t, uuidN(1), foreignSDP, true)
	walks := 0
	inner := sn.Config.Handler
	sn.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/senders") {
			walks++
		}
		inner.ServeHTTP(w, r)
	})

	res, err := p.h.ctrl.ConnectBulk(context.Background(), []ConnectRequest{
		{ReceiverID: uuidN(2), SenderID: uuidN(1), SenderNode: sn.URL},
		{ReceiverID: uuidN(3), SenderID: uuidN(1), SenderNode: sn.URL},
	}, true)
	if err != nil {
		t.Fatalf("ConnectBulk: %v", err)
	}
	if walks != 1 {
		t.Errorf("the foreign Node's senders were listed %d times, want once", walks)
	}
	for i, rt := range res.Routes {
		tf, _ := rt.Patch["transport_file"].(map[string]any)
		if tf == nil || tf["data"] != foreignSDP {
			t.Errorf("route %d transport_file = %v, want the foreign Node's SDP", i, rt.Patch["transport_file"])
		}
	}
}
