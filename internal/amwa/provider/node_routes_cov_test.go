package provider

// The IS-04 Node API surface as a controller walks it: the API-tree
// listing, every collection, every resource by id, the transport file,
// and what happens when a resource the routing table knows about is no
// longer in the bundle.

import (
	"encoding/json"
	"io"
	"net"
	stdhttp "net/http"
	"strings"
	"testing"

	dnssdcodec "dhs/internal/amwa/codec/dnssd"
	"dhs/internal/amwa/codec/is04"
	authsession "dhs/internal/amwa/session/auth"
	"dhs/internal/amwa/session/certmgr"
)

// get fetches a path from a served Node and returns its status and
// body.
func get(t *testing.T, addr, path string) (int, []byte) {
	t.Helper()
	resp, err := stdhttp.Get("http://" + addr + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return resp.StatusCode, body
}

// fullBundle is routableBundle with a Source and a Flow linked to the
// Sender, so every IS-04 collection has something in it.
func fullBundle(t *testing.T) *NodeConfig {
	t.Helper()
	b := routableBundle(t)
	dev := b.Devices[0].ID
	clock := "clk0"
	src := is04.Source{
		ResourceCore: is04.ResourceCore{
			ID: "cccccccc-3333-4333-8333-333333333333", Version: "0:0",
			Label: "src-1", Description: "test source", Tags: map[string][]string{},
		},
		DeviceID:  dev,
		Parents:   []string{},
		Format:    is04.FormatVideo,
		Caps:      map[string]any{},
		ClockName: &clock,
	}
	flow := is04.Flow{
		ResourceCore: is04.ResourceCore{
			ID: "dddddddd-4444-4444-8444-444444444444", Version: "0:0",
			Label: "flow-1", Description: "test flow", Tags: map[string][]string{},
		},
		SourceID:    src.ID,
		DeviceID:    dev,
		Parents:     []string{},
		Format:      is04.FormatVideo,
		MediaType:   "video/raw",
		FrameWidth:  1920,
		FrameHeight: 1080,
		Interlace:   "progressive",
		ColorSpace:  "BT709",
		Components: []is04.FlowVideoComponent{
			{Name: "Y", Width: 1920, Height: 1080, BitDepth: 10},
			{Name: "Cb", Width: 960, Height: 1080, BitDepth: 10},
			{Name: "Cr", Width: 960, Height: 1080, BitDepth: 10},
		},
	}
	b.Sources = append(b.Sources, src)
	b.Flows = append(b.Flows, flow)
	id := flow.ID
	b.Senders[0].FlowID = &id
	b.Node.Clocks = []is04.NodeClock{{Name: clock, RefType: "internal"}}
	return b
}

// Every collection lists, and every resource in it answers by id. A
// resource that is served but not listed — or listed but not served —
// is the trap that AMWA's auto_node tests exist to catch.
func TestNodeServesEveryCollectionAndResource(t *testing.T) {
	b := fullBundle(t)
	n := startNodeWith(t, b, nil)
	base := "/x-nmos/node/v1.3"

	collections := map[string][]string{
		"devices":   idsFromDevices(b.Devices),
		"sources":   idsFromSources(b.Sources),
		"flows":     idsFromFlows(b.Flows),
		"senders":   idsFromSenders(b.Senders),
		"receivers": idsFromReceivers(b.Receivers),
	}
	for plural, ids := range collections {
		code, body := get(t, n.addr, base+"/"+plural)
		if code != stdhttp.StatusOK {
			t.Errorf("GET %s = %d", plural, code)
			continue
		}
		var listed []json.RawMessage
		if err := json.Unmarshal(body, &listed); err != nil {
			t.Errorf("%s is not a JSON array: %v", plural, err)
			continue
		}
		if len(listed) != len(ids) {
			t.Errorf("%s listed %d, want %d", plural, len(listed), len(ids))
		}
		for _, id := range ids {
			if code, _ := get(t, n.addr, base+"/"+plural+"/"+id); code != stdhttp.StatusOK {
				t.Errorf("GET %s/%s = %d, want the resource the list advertised", plural, id, code)
			}
		}
	}
}

// The API-tree listing at the root names every tree this host serves.
// A Connection API that is served but unlisted is absent as far as
// auto_connection_1 is concerned, which is the same trap one level up
// from device.controls.
func TestNodeListsTheAPITreesItServes(t *testing.T) {
	n := startNode(t, nil)

	for _, path := range []string{"/x-nmos", "/x-nmos/", "/x-nmos/node", "/x-nmos/node/"} {
		code, body := get(t, n.addr, path)
		if code != stdhttp.StatusOK {
			t.Errorf("GET %s = %d", path, code)
			continue
		}
		var trees []string
		if err := json.Unmarshal(body, &trees); err != nil {
			t.Errorf("%s is not a JSON array: %v", path, err)
			continue
		}
		if len(trees) == 0 {
			t.Errorf("%s listed nothing", path)
		}
	}
}

// A resource the routing table knows about but the bundle no longer
// holds is a 404, not a panic and not an empty 200: the route outlives
// the resource, and a controller asking for it must be told it is
// gone.
func TestNodeAnswers404ForAResourceNoLongerInTheBundle(t *testing.T) {
	b := routableBundle(t)
	n := startNodeWith(t, b, nil)
	id := b.Senders[0].ID

	if code, _ := get(t, n.addr, "/x-nmos/node/v1.3/senders/"+id); code != stdhttp.StatusOK {
		t.Fatalf("the sender must be there to begin with: %d", code)
	}

	n.s.mu.Lock()
	n.s.bundle.Senders = nil
	n.s.mu.Unlock()

	code, body := get(t, n.addr, "/x-nmos/node/v1.3/senders/"+id)
	if code != stdhttp.StatusNotFound {
		t.Fatalf("= %d %s, want 404", code, body)
	}
	if !strings.Contains(string(body), id) {
		t.Errorf("the refusal must name what was asked for: %s", body)
	}
}

// The transport file is how a controller learns to receive a Sender's
// flow, and manifest_href points at it, so it answers even with no
// Connection API mounted.
func TestNodeServesASenderTransportFile(t *testing.T) {
	b := routableBundle(t)
	n := startNodeWith(t, b, nil)

	code, body := get(t, n.addr, "/x-nmos/node/v1.3/senders/"+b.Senders[0].ID+"/transportfile")
	if code != stdhttp.StatusOK {
		t.Fatalf("= %d %s, want the SDP", code, body)
	}
	if !strings.HasPrefix(string(body), "v=0") {
		t.Errorf("the transport file must be an SDP: %s", body)
	}
}

// A body that carries no encodable resource is a 404 whether it is a
// plain nil or a typed nil hiding inside an interface — which a
// `body == nil` test misses, and which is exactly what encodeOne
// returns for a resource this minor cannot express.
func TestIsNilBody(t *testing.T) {
	if !isNilBody(nil) {
		t.Error("a nil body is no resource")
	}
	if !isNilBody(json.RawMessage(nil)) {
		t.Error("a typed nil is no resource either")
	}
	if isNilBody(json.RawMessage(`{}`)) {
		t.Error("an encoded resource is a resource")
	}
}

// A resource type this Node has no counter for is not advertised, and
// asking to bump it changes nothing rather than panicking on a nil
// counter.
func TestBumpingAnUnknownResourceTypeIsANoOp(t *testing.T) {
	s, err := NewIS04NodeServer(newLogTap().logger(), validBundle(), IS04NodeConfig{
		Bind: "127.0.0.1:0", DiscoveryMode: "static",
	})
	if err != nil {
		t.Fatal(err)
	}
	if c, key := s.counterForResource(is04.ResourceType("subscription")); c != nil || key != "" {
		t.Fatalf("counterForResource(subscription) = %v, %q", c, key)
	}
	s.BumpResourceVersion(is04.ResourceType("subscription"))
}

// In static discovery there is no announcement to republish, so a bump
// stages nothing and returns.
func TestBumpingWithNothingAnnouncedIsANoOp(t *testing.T) {
	s, err := NewIS04NodeServer(newLogTap().logger(), validBundle(), IS04NodeConfig{
		Bind: "127.0.0.1:0", DiscoveryMode: "static",
	})
	if err != nil {
		t.Fatal(err)
	}
	s.BumpResourceVersion(is04.ResourceSender)
	if s.verSender.Load() != 1 {
		t.Errorf("the counter still advances: %d", s.verSender.Load())
	}
}

// A responder that will not take the updated TXT is reported, not
// retried into silence: Mode-D peers are reading those counters to
// decide whether to re-fetch, and one that never changes reads as a
// Node whose resources never change.
func TestBumpReportsATXTRepublishItCannotMake(t *testing.T) {
	tap := newLogTap()
	s, err := NewIS04NodeServer(tap.logger(), validBundle(), IS04NodeConfig{
		Bind: "127.0.0.1:0", DiscoveryMode: "static",
	})
	if err != nil {
		t.Fatal(err)
	}
	rs := &scriptedResponder{updateErr: errNoUpdate}
	s.mu.Lock()
	s.announceInstance = dnssdcodec.Instance{Service: dnssdcodec.ServiceNode}
	s.responder = rs
	s.mu.Unlock()

	s.BumpResourceVersion(is04.ResourceSender)
	if !tap.has("republish ver_* TXT failed") {
		t.Errorf("the failed republish must be reported; saw %v", tap.snapshot())
	}

	// With the TXT map now seeded, a second bump takes the other arm
	// and must still carry the new value.
	rs.updateErr = nil
	s.BumpResourceVersion(is04.ResourceSender)
	if got := s.announceInstance.TXT[dnssdcodec.TXTKeyVerSnd]; got != "2" {
		t.Errorf("ver_snd = %q, want the second bump staged", got)
	}
}

// A staged counter with no responder — the registered state, where
// IS-04 §4.2.1 requires the P2P announce to be suspended — is kept for
// the next re-announce rather than dropped.
func TestBumpStagesTheCounterWhileRegistered(t *testing.T) {
	s, err := NewIS04NodeServer(newLogTap().logger(), validBundle(), IS04NodeConfig{
		Bind: "127.0.0.1:0", DiscoveryMode: "static",
	})
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.announceInstance = dnssdcodec.Instance{Service: dnssdcodec.ServiceNode}
	s.mu.Unlock()

	s.BumpResourceVersion(is04.ResourceReceiver)
	if got := s.announceInstance.TXT[dnssdcodec.TXTKeyVerRcv]; got != "1" {
		t.Errorf("ver_rcv = %q, want it staged for the next announce", got)
	}
}

// errNoUpdate is the responder failure the bump test scripts.
var errNoUpdate = errTest("responder will not update")

// put sends a body to a path on a served Node.
func put(t *testing.T, addr, path, body string) (int, []byte) {
	t.Helper()
	req, err := stdhttp.NewRequest(stdhttp.MethodPut, "http://"+addr+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

// PUT /receivers/{id}/target is IS-04 §4.3.1 legacy connection
// control: a Sender object connects, an empty body or {} disconnects,
// and the answer is 202 either way. v1.2+ points at IS-05 instead, but
// the path stays and controllers still reach for it.
func TestReceiverTargetConnectsAndDisconnects(t *testing.T) {
	b := fullBundle(t)
	n := startNodeWith(t, b, nil)
	rid := b.Receivers[0].ID
	path := "/x-nmos/node/v1.3/receivers/" + rid + "/target"

	sender, err := json.Marshal(b.Senders[0])
	if err != nil {
		t.Fatal(err)
	}
	if code, body := put(t, n.addr, path, string(sender)); code != stdhttp.StatusAccepted {
		t.Fatalf("connect = %d %s, want 202", code, body)
	}
	n.s.mu.Lock()
	rcv := findReceiverByID(n.s.bundle.Receivers, rid)
	connected := rcv.Subscription.Active && rcv.Subscription.SenderID != nil &&
		*rcv.Subscription.SenderID == b.Senders[0].ID
	n.s.mu.Unlock()
	if !connected {
		t.Error("a connect must record the sender on the receiver's subscription")
	}

	for _, body := range []string{"", "{}"} {
		if code, out := put(t, n.addr, path, body); code != stdhttp.StatusAccepted {
			t.Fatalf("disconnect with %q = %d %s, want 202", body, code, out)
		}
	}
	n.s.mu.Lock()
	rcv = findReceiverByID(n.s.bundle.Receivers, rid)
	stillOn := rcv.Subscription.Active || rcv.Subscription.SenderID != nil
	n.s.mu.Unlock()
	if stillOn {
		t.Error("a disconnect must clear the subscription")
	}
}

// A target body that is not JSON at all cannot name a sender, and is
// refused before anything is recorded: a receiver whose subscription
// names something that is not a sender is worse than one that is idle.
func TestReceiverTargetRefusesABodyItCannotDecode(t *testing.T) {
	b := fullBundle(t)
	n := startNodeWith(t, b, nil)
	path := "/x-nmos/node/v1.3/receivers/" + b.Receivers[0].ID + "/target"

	if code, out := put(t, n.addr, path, `{"id":`); code != stdhttp.StatusBadRequest {
		t.Fatalf("= %d %s, want 400", code, out)
	}
}

// The Sender is decoded tolerantly — a v1.0-shaped body is spec-correct
// for a v1.0 URL and must not be refused for missing v1.3 vocabulary —
// but the answer carries the Sender back on the wire, and one the codec
// cannot render is a 500 rather than a 202 with nothing in it.
func TestReceiverTargetReportsAnAnswerItCannotEncode(t *testing.T) {
	b := fullBundle(t)
	n := startNodeWith(t, b, nil)
	path := "/x-nmos/node/v1.3/receivers/" + b.Receivers[0].ID + "/target"

	code, out := put(t, n.addr, path, `{"id":"not-a-uuid"}`)
	if code != stdhttp.StatusInternalServerError {
		t.Fatalf("= %d %s, want 500", code, out)
	}
	if !strings.Contains(string(out), "Encode failed") {
		t.Errorf("the refusal must say what happened: %s", out)
	}
}

// The route outlives the receiver here too: a target PUT for a
// receiver no longer in the bundle is a 404 naming it.
func TestReceiverTargetAnswers404WhenTheReceiverIsGone(t *testing.T) {
	b := fullBundle(t)
	n := startNodeWith(t, b, nil)
	rid := b.Receivers[0].ID

	n.s.mu.Lock()
	n.s.bundle.Receivers = nil
	n.s.mu.Unlock()

	code, out := put(t, n.addr, "/x-nmos/node/v1.3/receivers/"+rid+"/target", "{}")
	if code != stdhttp.StatusNotFound {
		t.Fatalf("= %d %s, want 404", code, out)
	}
}

// /self answers from the codec, and a Node the codec will not encode
// is a 500 that says so — not an empty 200 a controller would cache as
// the Node's identity.
func TestSelfReportsAnEncodeItCannotDo(t *testing.T) {
	n := startNode(t, nil)

	n.s.mu.Lock()
	n.s.bundle.Node.ID = "not-a-uuid" // the schema requires one
	n.s.mu.Unlock()

	code, body := get(t, n.addr, "/x-nmos/node/v1.3/self")
	if code != stdhttp.StatusInternalServerError {
		t.Fatalf("= %d %s, want 500", code, body)
	}
	if !strings.Contains(string(body), "Encode failed") {
		t.Errorf("the refusal must say what happened: %s", body)
	}
}

// The registration client is handed the Node's own credentials and
// trust roots when it has them: a Registry behind BCP-003-02 refuses
// an unauthenticated POST, and one behind BCP-003-01 refuses a client
// that does not trust its chain.
func TestRegistrationClientInheritsCredentialsAndTrust(t *testing.T) {
	s, err := NewIS04NodeServer(newLogTap().logger(), validBundle(), IS04NodeConfig{
		Bind: "127.0.0.1:0", DiscoveryMode: "static",
	})
	if err != nil {
		t.Fatal(err)
	}

	// With neither armed, both attachments are no-ops rather than nil
	// dereferences.
	rc := NewRegistrationClient(newLogTap().logger(), "http://127.0.0.1:1", "v1.3", validBundle())
	s.attachAuthToken(rc)
	s.attachTLSTrust(rc)

	s.authTokens = authsession.NewTokenClient(authsession.TokenClientOptions{
		MetadataURL: "http://127.0.0.1:1/.well-known/oauth-authorization-server",
		ClientID:    "node", ClientSecret: "s", Scope: "registration",
	})
	mgr, err := certmgr.New(certmgr.Options{
		Hostnames: []string{"node.local"}, Serial: "1",
		DataDir: t.TempDir(), Logger: newLogTap().logger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	s.certs = mgr

	s.attachAuthToken(rc)
	s.attachTLSTrust(rc)
}

// With IS-05 opted out there is no connection state to render an SDP
// from, but manifest_href still points here — so the standalone
// renderer answers rather than leaving every Sender advertising a URL
// that 404s.
func TestTransportFileFallsBackWithoutTheConnectionAPI(t *testing.T) {
	b := fullBundle(t)
	n := startNodeWith(t, b, func(c *IS04NodeConfig) { c.NoConnectionAPI = true })

	code, body := get(t, n.addr, "/x-nmos/node/v1.3/senders/"+b.Senders[0].ID+"/transportfile")
	if code != stdhttp.StatusOK {
		t.Fatalf("= %d %s, want the standalone SDP", code, body)
	}
	if !strings.HasPrefix(string(body), "v=0") {
		t.Errorf("the fallback must still be an SDP: %s", body)
	}
}

// Every collection's per-id lookup answers 404 once the resource is
// out of the bundle. The route is built at Serve and outlives the
// resource, so each one has to say so for itself.
func TestEveryCollectionAnswers404ForAResourceThatIsGone(t *testing.T) {
	b := fullBundle(t)
	n := startNodeWith(t, b, nil)
	base := "/x-nmos/node/v1.3"

	cases := []struct {
		plural string
		id     string
		clear  func(*NodeConfig)
	}{
		{"devices", b.Devices[0].ID, func(c *NodeConfig) { c.Devices = nil }},
		{"sources", b.Sources[0].ID, func(c *NodeConfig) { c.Sources = nil }},
		{"flows", b.Flows[0].ID, func(c *NodeConfig) { c.Flows = nil }},
		{"receivers", b.Receivers[0].ID, func(c *NodeConfig) { c.Receivers = nil }},
	}
	for _, tc := range cases {
		if code, _ := get(t, n.addr, base+"/"+tc.plural+"/"+tc.id); code != stdhttp.StatusOK {
			t.Fatalf("%s/%s must be there to begin with: %d", tc.plural, tc.id, code)
		}
		n.s.mu.Lock()
		tc.clear(n.s.bundle)
		n.s.mu.Unlock()

		if code, body := get(t, n.addr, base+"/"+tc.plural+"/"+tc.id); code != stdhttp.StatusNotFound {
			t.Errorf("%s/%s = %d %s, want 404", tc.plural, tc.id, code, body)
		}
	}
}

// A request whose body stops short of the length it declared is a
// bad request, not a half-applied target: the Node must not act on
// what it managed to read.
func TestReceiverTargetRefusesATruncatedBody(t *testing.T) {
	b := fullBundle(t)
	n := startNodeWith(t, b, nil)
	path := "/x-nmos/node/v1.3/receivers/" + b.Receivers[0].ID + "/target"

	conn, err := net.Dial("tcp", n.addr)
	if err != nil {
		t.Fatal(err)
	}
	req := "PUT " + path + " HTTP/1.1\r\nHost: " + n.addr +
		"\r\nContent-Type: application/json\r\nContent-Length: 200\r\n\r\n{\"id\":\"x\"}"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	// Half-close: the server reads what arrived and then EOF, well
	// short of the 200 bytes the request promised.
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.CloseWrite()
	}
	resp := make([]byte, 256)
	nRead, _ := conn.Read(resp)
	_ = conn.Close()
	if !strings.Contains(string(resp[:nRead]), "400") {
		t.Errorf("a truncated body = %q, want 400", resp[:nRead])
	}
}

// A bundle whose resources do not refer to each other is refused at
// construction: a Device naming a Node that is not there, or a Sender
// on a Device that is not there, describes a plant that cannot exist,
// and serving it would publish that fiction.
func TestNewNodeServerRefusesABundleThatDoesNotHangTogether(t *testing.T) {
	b := validBundle()
	b.Devices[0].NodeID = "aaaaaaaa-1234-4abc-9def-1234567890ab"

	_, err := NewIS04NodeServer(newLogTap().logger(), b, IS04NodeConfig{
		Bind: "127.0.0.1:0", DiscoveryMode: "static",
	})
	if err == nil || !strings.Contains(err.Error(), "node_id") {
		t.Fatalf("= %v, want the dangling reference reported", err)
	}
}

// An activation re-registers the changed resource and republishes the
// ver_* counter. Skipping the re-POST left the Query API insisting a
// live receiver was idle; skipping the bump left P2P peers reading a
// stale resource list. Both hang off the same hook.
func TestResourceChangeRepublishesAndReRegisters(t *testing.T) {
	s, err := NewIS04NodeServer(newLogTap().logger(), fullBundle(t), IS04NodeConfig{
		Bind: "127.0.0.1:0", DiscoveryMode: "static",
	})
	if err != nil {
		t.Fatal(err)
	}
	rc := NewRegistrationClient(newLogTap().logger(), "http://127.0.0.1:1", "v1.3", validBundle())
	s.wireResourceChanged(rc)

	if s.connection.onResourceChanged == nil {
		t.Fatal("the hook must be installed")
	}
	s.connection.onResourceChanged(is04.ResourceSender, s.bundle.Senders[0])

	// The counter bump runs detached, so what this asserts is that the
	// hook ran to completion with a registration client attached — the
	// arm that used to be skipped.
	if s.verSender.Load() > 1 {
		t.Errorf("one change bumped the counter %d times", s.verSender.Load())
	}
}
