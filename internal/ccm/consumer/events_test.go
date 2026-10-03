package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/ccm/codec"
	dhsc "dhs/internal/consumer"
	"dhs/internal/transport/ws"
)

// A device that serves the §13 event channel: a collection of things,
// each with a status and parts of its own, and a matrix state.
const eventSpec = `openapi: 3.1.1
paths:
  /self:
    get:
      operationId: GetSelf
  /things:
    get:
      operationId: ListThings
  /things/{uuid}:
    get:
      operationId: GetThing
    patch:
      operationId: PatchThing
  /things/{uuid}/status:
    get:
      operationId: GetThingStatus
  /things/{uuid}/parts:
    get:
      operationId: ListParts
  /things/{uuid}/parts/{partUuid}:
    get:
      operationId: GetPart
  /things/{uuid}/parts/{partUuid}/pins/{pinUuid}:
    get:
      operationId: GetPin
  /orphans/{uuid}/bits/{bitUuid}:
    get:
      operationId: GetBit
  /matrix/state:
    get:
      operationId: GetState
`

// asked is one request the device received over the channel.
type asked struct {
	Type    string `json:"type"`
	ID      int64  `json:"id"`
	Payload struct {
		RelativeURL    string `json:"relativeUrl"`
		SubscriptionID string `json:"subscriptionId"`
	} `json:"payload"`
}

type eventDevice struct {
	t   *testing.T
	srv *httptest.Server

	mu    sync.Mutex
	conn  *ws.Conn
	seen  []asked
	conns int

	// answer decides what a CreateSubscription gets. nil accepts.
	answer func(d *eventDevice, a asked)
}

func newEventDevice(t *testing.T) *eventDevice {
	t.Helper()
	d := &eventDevice{t: t}
	d.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/self":
			_, _ = w.Write([]byte(`{"app":{"productName":"X","productVersion":"1"}}`))
		case "/docs/api.yml":
			_, _ = w.Write([]byte(eventSpec))
		case codec.WebSocketPath:
			conn, err := ws.Accept(w, r, nil)
			if err != nil {
				t.Errorf("accept: %v", err)
				return
			}
			d.mu.Lock()
			d.conn = conn
			d.conns++
			d.mu.Unlock()
			d.serve(conn)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(d.srv.Close)
	restore := dialClient
	dialClient = func(string) *Client { return testClient(d.srv) }
	t.Cleanup(func() { dialClient = restore })
	return d
}

func (d *eventDevice) serve(conn *ws.Conn) {
	for {
		_, data, err := conn.ReadMessage(context.Background())
		if err != nil {
			return
		}
		var a asked
		_ = json.Unmarshal(data, &a)
		d.mu.Lock()
		d.seen = append(d.seen, a)
		answer := d.answer
		d.mu.Unlock()
		if a.Type != codec.MsgCreateSubscription {
			continue
		}
		if answer == nil {
			answer = accept
		}
		answer(d, a)
	}
}

func accept(d *eventDevice, a asked) {
	d.send(fmt.Sprintf(`{"type":"CreateSubscriptionResponse","id":%d,"payload":{"status":200,"message":"Subscription OK","subscriptionId":"sub-%d"}}`, a.ID, a.ID))
}

func refuse(d *eventDevice, a asked) {
	d.send(fmt.Sprintf(`{"type":"CreateSubscriptionResponse","id":%d,"payload":{"status":404,"message":"does not match any GET route"}}`, a.ID))
}

func (d *eventDevice) send(frame string) {
	d.mu.Lock()
	conn := d.conn
	d.mu.Unlock()
	_ = conn.WriteText(context.Background(), []byte(frame))
}

// events sends one §13.4 notification.
func (d *eventDevice) events(kind, root, patch string) {
	d.send(fmt.Sprintf(`{"type":%q,"id":7,"payload":[{"documentRoot":%q,"patch":%s}]}`, kind, root, patch))
}

func (d *eventDevice) requests(kind string) []asked {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []asked
	for _, a := range d.seen {
		if a.Type == kind {
			out = append(out, a)
		}
	}
	return out
}

// collector gathers what a subscription was handed.
type collector struct {
	mu  sync.Mutex
	evs []dhsc.Event
}

func (c *collector) fn(ev dhsc.Event) {
	c.mu.Lock()
	c.evs = append(c.evs, ev)
	c.mu.Unlock()
}

func (c *collector) paths() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.evs))
	for _, ev := range c.evs {
		out = append(out, ev.Path)
	}
	return out
}

func (c *collector) last() dhsc.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.evs) == 0 {
		return dhsc.Event{}
	}
	return c.evs[len(c.evs)-1]
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("never happened: %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func closed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func TestADeviceWithoutTheEventChannelIsPolled(t *testing.T) {
	d := newTypedDevice(t)
	p := testPluginConnected(t, d.srv)
	if p.SessionDone() != nil {
		t.Error("a polled session has no channel to lose")
	}
	if got := p.ComplianceProfile().Snapshot()[NoWebSocket]; got != 1 {
		t.Errorf("%s noted %d time(s), want 1 (§13.1)", NoWebSocket, got)
	}
	req := dhsc.ValueRequest{Path: "thing"}
	if err := p.Subscribe(req, func(dhsc.Event) {}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if p.poller.Active() != 1 {
		t.Error("the watch is not polling")
	}
	if err := p.Unsubscribe(req); err != nil || p.poller.Active() != 0 {
		t.Errorf("unsubscribe: %v, %d still polling", err, p.poller.Active())
	}
	_ = p.Disconnect()
}

func TestAWatchIsFedByTheEventChannel(t *testing.T) {
	d := newEventDevice(t)
	// §13.3.8 has the initial state ahead of the answer.
	d.answer = func(d *eventDevice, a asked) {
		if a.Payload.RelativeURL == "/things/u1" {
			d.events(codec.MsgEvents, "/things/u1", `[{"op":"replace","path":"","value":{"name":"one","level":3,"legs":[{"uuid":"leg-a","ip":"1.1.1.1"},{"ip":"2.2.2.2"}]}}]`)
		}
		accept(d, a)
	}
	p := testPluginConnected(t, d.srv)
	done := p.SessionDone()
	if done == nil {
		t.Fatal("the device serves the channel and the session does not know it")
	}

	var all, leaf collector
	req := dhsc.ValueRequest{Path: "things.u1"}
	if err := p.Subscribe(req, all.fn); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	var urls []string
	for _, a := range d.requests(codec.MsgCreateSubscription) {
		urls = append(urls, a.Payload.RelativeURL)
	}
	want := []string{"/things/u1", "/things/u1/parts/*", "/things/u1/status"}
	if !sameSet(urls, want) {
		t.Fatalf("subscribed to %q, want %q", urls, want)
	}
	// The whole resource, once: every leaf, an array element named by
	// its uuid where it has one, writable because the spec has a PATCH.
	eventually(t, "the initial state", func() bool { return len(all.paths()) == 5 })
	if got := all.paths(); !reflect.DeepEqual(got, []string{
		"things.u1.legs.leg-a.ip", "things.u1.legs.leg-a.uuid", "things.u1.legs.1.ip", "things.u1.level", "things.u1.name",
	}) {
		t.Errorf("initial state = %q", got)
	}
	if ev := all.last(); ev.Value.Str != "one" || ev.Access&accessWrite == 0 || ev.Freshness != "live" || ev.Label != "name" {
		t.Errorf("leaf = %+v", ev)
	}

	// A second watch, on one leaf of the same resource.
	leafReq := dhsc.ValueRequest{Path: "things/u1/level"}
	if err := p.Subscribe(leafReq, leaf.fn); err != nil {
		t.Fatalf("subscribe to a leaf: %v", err)
	}

	// What changes arrives as a patch — here typed the way SHUFFLE
	// 6.0.0 types it.
	d.events(codec.MsgEvent, "/things/u1", `[{"op":"replace","path":"/level","value":4}]`)
	eventually(t, "the level change", func() bool { return all.last().Path == "things.u1.level" && len(leaf.paths()) > 0 })
	if ev := leaf.last(); ev.Value.Int != 4 {
		t.Errorf("level = %+v", ev.Value)
	}
	if got := p.ComplianceProfile().Snapshot()[EventTypeSingular]; got != 1 {
		t.Errorf("%s noted %d time(s), want 1 (§13.4)", EventTypeSingular, got)
	}

	// Inside an array the pointer says an index; the path says a uuid.
	d.events(codec.MsgEvents, "/things/u1", `[{"op":"replace","path":"/legs/0/ip","value":"9.9.9.9"}]`)
	eventually(t, "the leg change", func() bool { return all.last().Path == "things.u1.legs.leg-a.ip" })
	d.events(codec.MsgEvents, "/things/u1", `[{"op":"add","path":"/legs/-","value":{"uuid":"leg-c","ip":"3.3.3.3"}}]`)
	eventually(t, "the added leg", func() bool { return all.last().Path == "things.u1.legs.leg-c.uuid" })
	// A removal changes the state and publishes no value.
	n := len(all.paths())
	d.events(codec.MsgEvents, "/things/u1", `[{"op":"remove","path":"/legs/2"},{"op":"replace","path":"/name","value":"uno"}]`)
	eventually(t, "the rename", func() bool { return all.last().Value.Str == "uno" })
	if got := len(all.paths()); got != n+1 {
		t.Errorf("a removal published %d event(s)", got-n-1)
	}
	// Another resource is nobody's scope here.
	d.events(codec.MsgEvents, "/matrix/state", `[{"op":"replace","path":"","value":{"d1":"s1"}}]`)
	d.events(codec.MsgEvents, "/things/u1", `[{"op":"replace","path":"/name","value":"een"}]`)
	eventually(t, "the second rename", func() bool { return all.last().Value.Str == "een" })
	for _, path := range all.paths() {
		if strings.HasPrefix(path, "matrix") {
			t.Errorf("out of scope and delivered: %s", path)
		}
	}
	// The leaf watch: its own initial state, then the one change — and
	// none of the resource's other leaves.
	if got := leaf.paths(); !reflect.DeepEqual(got, []string{"things.u1.level", "things.u1.level"}) {
		t.Errorf("the leaf watch saw %q", got)
	}

	// Frames that are not §13 are counted and survived.
	d.send(`not json`)
	d.send(`{"type":"Hello","id":1,"payload":{}}`)
	d.send(`{"type":"CreateSubscriptionResponse","id":999,"payload":{"status":200}}`)
	eventually(t, "the two unreadable frames", func() bool {
		return p.ComplianceProfile().Snapshot()[FrameUnreadable] == 2
	})

	// Unsubscribe gives back what the device handed out.
	if err := p.Unsubscribe(req); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the three deletes", func() bool { return len(d.requests(codec.MsgDeleteSubscription)) == 3 })
	d.send(`{"type":"DeleteSubscriptionResponse","id":1,"payload":{"status":200,"message":"DeleteSubscription OK"}}`)
	if err := p.Unsubscribe(req); err != nil {
		t.Errorf("unsubscribing twice: %v", err)
	}

	if closed(done) {
		t.Fatal("the session ended while it was healthy")
	}
	if err := p.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if !closed(done) || p.SessionDone() != nil {
		t.Error("disconnect left the channel open")
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	in := map[string]bool{}
	for _, s := range a {
		in[s] = true
	}
	for _, s := range b {
		if !in[s] {
			return false
		}
	}
	return true
}

func TestTheScopeDecidesWhatIsSubscribedTo(t *testing.T) {
	spec, err := codec.ParseSpec([]byte(eventSpec))
	if err != nil {
		t.Fatal(err)
	}
	parts := []string{"things", "{uuid}", "parts", "{partUuid}"}
	pins := []string{"things", "{uuid}", "parts", "{partUuid}", "pins", "{pinUuid}"}
	orphans := []string{"/orphans/{uuid}/bits/{bitUuid}"}
	cases := []struct {
		scope   string
		urls    []string
		nested  [][]string
		skipped []string
	}{
		// Everything: one `*` at most, so what lies two parameters deep
		// is left to be subscribed parent by parent — unless its parent
		// is not a GET, and then it is named as left out. A collection
		// is never subscribed to.
		{"", []string{"/matrix/state", "/self", "/things/*", "/things/*/status"}, [][]string{parts, pins}, orphans},
		{"things", []string{"/things/*", "/things/*/status"}, [][]string{parts, pins}, nil},
		{"things/u1", []string{"/things/u1", "/things/u1/parts/*", "/things/u1/status"},
			[][]string{{"things", "u1", "parts", "{partUuid}", "pins", "{pinUuid}"}}, nil},
		{"things/u1/parts/p1", []string{"/things/u1/parts/p1", "/things/u1/parts/p1/pins/*"}, nil, nil},
		{"orphans", nil, nil, orphans},
		{"orphans/o1", []string{"/orphans/o1/bits/*"}, nil, nil},
		// A field: the resource that carries it.
		{"things/u1/name", []string{"/things/u1"}, nil, nil},
		{"matrix/state/d1", []string{"/matrix/state"}, nil, nil},
		{"nothing/here", nil, nil, nil},
	}
	for _, c := range cases {
		urls, nested, skipped := subscriptionURLs(spec, c.scope)
		if !reflect.DeepEqual(urls, c.urls) || !reflect.DeepEqual(nested, c.nested) || !reflect.DeepEqual(skipped, c.skipped) {
			t.Errorf("scope %q:\n got %q nested %q skipped %q\nwant %q nested %q skipped %q",
				c.scope, urls, nested, skipped, c.urls, c.nested, c.skipped)
		}
	}
}

// wideDevice answers a watch of everything: three things, pushed as the
// state of `/things/*`. The first member subscription is answered only
// when hold is closed; the parts of u2 are refused.
func wideDevice(t *testing.T, hold chan struct{}) *eventDevice {
	d := newEventDevice(t)
	var once sync.Once
	d.answer = func(d *eventDevice, a asked) {
		url := a.Payload.RelativeURL
		switch {
		case url == "/things/*":
			d.send(`{"type":"Events","id":1,"payload":[
				{"documentRoot":"/things/u1","patch":[{"op":"replace","path":"","value":{"name":"one"}}]},
				{"documentRoot":"/things/u2","patch":[{"op":"replace","path":"","value":{"name":"two"}}]},
				{"documentRoot":"/things/u3","patch":[{"op":"replace","path":"","value":{"name":"three"}}]}]}`)
			accept(d, a)
		case strings.Contains(url, "/parts/"):
			held := false
			once.Do(func() { held = true })
			switch {
			case held:
				// Off the device's read loop: it keeps serving.
				go func() { <-hold; accept(d, a) }()
			case url == "/things/u2/parts/*":
				refuse(d, a)
			default:
				accept(d, a)
			}
		default:
			accept(d, a)
		}
	}
	return d
}

func memberRequests(d *eventDevice) []string {
	var out []string
	for _, a := range d.requests(codec.MsgCreateSubscription) {
		if strings.Contains(a.Payload.RelativeURL, "/parts/") {
			out = append(out, a.Payload.RelativeURL)
		}
	}
	return out
}

func TestAWideWatchReachesTheMembersParentByParent(t *testing.T) {
	hold := make(chan struct{})
	d := wideDevice(t, hold)
	p := testPluginConnected(t, d.srv)
	var got collector
	req := dhsc.ValueRequest{}
	if err := p.Subscribe(req, got.fn); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	// One request at a time: the first parent's members are asked for
	// and nothing else is until the device has answered.
	eventually(t, "the first member subscription", func() bool { return len(memberRequests(d)) == 1 })
	time.Sleep(20 * time.Millisecond)
	if n := len(memberRequests(d)); n != 1 {
		t.Fatalf("%d member subscription(s) sent ahead of the first answer", n)
	}
	close(hold)
	eventually(t, "the three member subscriptions", func() bool { return len(memberRequests(d)) == 3 })
	if got := memberRequests(d); !sameSet(got, []string{"/things/u1/parts/*", "/things/u2/parts/*", "/things/u3/parts/*"}) {
		t.Errorf("member subscriptions = %q", got)
	}
	// Never two `*` in one request.
	for _, a := range d.requests(codec.MsgCreateSubscription) {
		if strings.Count(a.Payload.RelativeURL, "*") > 1 {
			t.Errorf("subscribed with two wildcards: %s", a.Payload.RelativeURL)
		}
	}
	eventually(t, "the refusal of u2's parts", func() bool {
		return p.ComplianceProfile().Snapshot()[SubscriptionRefused] == 1
	})

	// A member's state arrives, and what lies below it is asked for in
	// turn — three parameters deep, still one `*`.
	d.events(codec.MsgEvents, "/things/u1/parts/p1", `[{"op":"replace","path":"","value":{"gain":-3}}]`)
	eventually(t, "the part", func() bool { return got.last().Path == "things.u1.parts.p1.gain" })
	eventually(t, "the pins of the part", func() bool {
		for _, a := range d.requests(codec.MsgCreateSubscription) {
			if a.Payload.RelativeURL == "/things/u1/parts/p1/pins/*" {
				return true
			}
		}
		return false
	})
	// What is not a member calls for nothing, and a parent seen again
	// is not asked for twice.
	d.events(codec.MsgEvents, "/things/u1/status", `[{"op":"replace","path":"","value":{"ok":true}}]`)
	d.events(codec.MsgEvents, "/matrix/state", `[{"op":"replace","path":"","value":{"d1":"s1"}}]`)
	d.events(codec.MsgEvents, "/things/u1", `[{"op":"replace","path":"","value":{"name":"uno"}}]`)
	eventually(t, "the rename", func() bool { return got.last().Value.Str == "uno" })
	if n := len(d.requests(codec.MsgCreateSubscription)); n != 4+3+1 {
		t.Errorf("%d subscription(s) in all, want 8", n)
	}
	if ev := got.last(); ev.Access&accessWrite == 0 {
		t.Errorf("a thing has a PATCH: %+v", ev)
	}

	// Everything the device granted is handed back: four at the scope
	// (one of them the wildcard), two parents' members, one part's pins.
	if err := p.Unsubscribe(req); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the seven deletes", func() bool { return len(d.requests(codec.MsgDeleteSubscription)) == 7 })
	_ = p.Disconnect()
}

func TestAMemberGrantedAfterTheWatchEndedIsHandedBack(t *testing.T) {
	hold := make(chan struct{})
	d := wideDevice(t, hold)
	p := testPluginConnected(t, d.srv)
	req := dhsc.ValueRequest{}
	if err := p.Subscribe(req, func(dhsc.Event) {}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	eventually(t, "the first member subscription", func() bool { return len(memberRequests(d)) == 1 })
	if err := p.Unsubscribe(req); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the four deletes", func() bool { return len(d.requests(codec.MsgDeleteSubscription)) == 4 })
	// The device grants the member it was holding: nobody watches it
	// any more, so it goes straight back, and the parents still queued
	// are not asked for.
	close(hold)
	eventually(t, "the late grant handed back", func() bool { return len(d.requests(codec.MsgDeleteSubscription)) == 5 })
	time.Sleep(20 * time.Millisecond)
	if n := len(memberRequests(d)); n != 1 {
		t.Errorf("%d member subscription(s) for a watch that ended", n)
	}
	_ = p.Disconnect()
}

func TestAMemberNeverAnsweredEndsTheSession(t *testing.T) {
	restore := subscribeTimeout
	subscribeTimeout = 100 * time.Millisecond
	t.Cleanup(func() { subscribeTimeout = restore })

	d := wideDevice(t, make(chan struct{})) // never released
	p := testPluginConnected(t, d.srv)
	done := p.SessionDone()
	if err := p.Subscribe(dhsc.ValueRequest{}, func(dhsc.Event) {}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	// A channel that stops answering is a lost one: the watch starts
	// again from a new session rather than staying half subscribed.
	eventually(t, "the session ending", func() bool { return closed(done) })
	_ = p.Disconnect()
}

func TestASubscriptionThatCannotBeMadeIsRefused(t *testing.T) {
	d := newEventDevice(t)
	p := testPluginConnected(t, d.srv)
	t.Cleanup(func() { _ = p.Disconnect() })
	nop := func(dhsc.Event) {}

	if err := p.Subscribe(dhsc.ValueRequest{Path: "things.u1"}, nil); err == nil {
		t.Error("a watch with nowhere to send its events was accepted")
	}
	if err := p.Subscribe(dhsc.ValueRequest{Path: "nothing.here"}, nop); err == nil || !strings.Contains(err.Error(), "nothing to watch") {
		t.Errorf("an unknown scope: %v", err)
	}
	if err := p.Subscribe(dhsc.ValueRequest{Path: "self"}, nop); err != nil {
		t.Fatal(err)
	}
	if err := p.Subscribe(dhsc.ValueRequest{Path: "self"}, nop); err == nil || !strings.Contains(err.Error(), "already watching") {
		t.Errorf("the same scope twice: %v", err)
	}

	// One path refused: the watch stands on the others and the refusal
	// is counted (§13.3: every GET is subscribable).
	d.mu.Lock()
	d.answer = func(d *eventDevice, a asked) {
		if strings.HasSuffix(a.Payload.RelativeURL, "/status") {
			refuse(d, a)
			return
		}
		accept(d, a)
	}
	d.mu.Unlock()
	if err := p.Subscribe(dhsc.ValueRequest{Path: "things.u2"}, nop); err != nil {
		t.Errorf("one refusal out of three ended the watch: %v", err)
	}
	if got := p.ComplianceProfile().Snapshot()[SubscriptionRefused]; got != 1 {
		t.Errorf("%s noted %d time(s), want 1", SubscriptionRefused, got)
	}

	// Every path refused: there is nothing to watch.
	d.mu.Lock()
	d.answer = refuse
	d.mu.Unlock()
	err := p.Subscribe(dhsc.ValueRequest{Path: "matrix.state"}, nop)
	if err == nil || !strings.Contains(err.Error(), "refused every subscription") {
		t.Errorf("all refused: %v", err)
	}
	// And it left nothing behind: the scope can be asked for again.
	d.mu.Lock()
	d.answer = nil
	d.mu.Unlock()
	if err := p.Subscribe(dhsc.ValueRequest{Path: "matrix.state"}, nop); err != nil {
		t.Errorf("after a refusal: %v", err)
	}
}

func TestASilentDeviceDoesNotHoldASubscribeForever(t *testing.T) {
	d := newEventDevice(t)
	d.answer = func(*eventDevice, asked) {}
	restore := subscribeTimeout
	subscribeTimeout = 30 * time.Millisecond
	t.Cleanup(func() { subscribeTimeout = restore })

	p := testPluginConnected(t, d.srv)
	t.Cleanup(func() { _ = p.Disconnect() })
	err := p.Subscribe(dhsc.ValueRequest{Path: "self"}, func(dhsc.Event) {})
	if err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Errorf("err = %v", err)
	}
}

func TestAChannelLostMidSubscribeIsReported(t *testing.T) {
	d := newEventDevice(t)
	d.answer = func(d *eventDevice, _ asked) { _ = d.conn.Close(1001, "going away") }
	p := testPluginConnected(t, d.srv)
	done := p.SessionDone()
	err := p.Subscribe(dhsc.ValueRequest{Path: "self"}, func(dhsc.Event) {})
	if err == nil || !strings.Contains(err.Error(), "closed while subscribing") {
		t.Errorf("err = %v", err)
	}
	eventually(t, "the session ending", func() bool { return closed(done) })

	// Nothing can be sent on what is left of it.
	err = p.Subscribe(dhsc.ValueRequest{Path: "self"}, func(dhsc.Event) {})
	if err == nil || !strings.Contains(err.Error(), "subscribe /self") {
		t.Errorf("on a dead channel: %v", err)
	}
	_ = p.Disconnect()
}

func TestAPatchThatDoesNotFitEndsTheSession(t *testing.T) {
	d := newEventDevice(t)
	p := testPluginConnected(t, d.srv)
	done := p.SessionDone()
	if err := p.Subscribe(dhsc.ValueRequest{Path: "self"}, func(dhsc.Event) {}); err != nil {
		t.Fatal(err)
	}
	// A change to a state the device never sent (§13.4.1: discard and
	// repopulate — which a new session does).
	d.events(codec.MsgEvents, "/self", `[{"op":"replace","path":"/app/productName","value":"Y"}]`)
	eventually(t, "the session ending", func() bool { return closed(done) })
	if got := p.ComplianceProfile().Snapshot()[PatchUnapplied]; got != 1 {
		t.Errorf("%s noted %d time(s), want 1", PatchUnapplied, got)
	}
	_ = p.Disconnect()
}

func TestReconnectingReplacesTheChannel(t *testing.T) {
	d := newEventDevice(t)
	p := testPluginConnected(t, d.srv)
	first := p.SessionDone()
	if err := p.Connect(context.Background(), "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if !closed(first) {
		t.Error("the first channel outlived the reconnect")
	}
	if second := p.SessionDone(); second == nil || closed(second) {
		t.Error("the reconnect has no channel")
	}
	d.mu.Lock()
	conns := d.conns
	d.mu.Unlock()
	if conns != 2 {
		t.Errorf("%d channel(s) opened, want 2", conns)
	}
	_ = p.Disconnect()
}

func TestAnIdleChannelIsKeptAliveByPings(t *testing.T) {
	restore := pingEvery
	pingEvery = 50 * time.Millisecond
	t.Cleanup(func() { pingEvery = restore })

	d := newEventDevice(t)
	p := testPluginConnected(t, d.srv)
	done := p.SessionDone()
	// Nothing changes on the device for well over the idle limit of
	// three pings; only the pongs keep the channel from looking dead.
	time.Sleep(400 * time.Millisecond)
	if closed(done) {
		t.Error("an idle channel was taken for a dead one")
	}
	_ = p.Disconnect()
}

// Everything the channel does shows on the connector's metrics — what
// --metrics-addr serves — and so does every REST request.
func TestTheChannelAndTheRESTClientAreCounted(t *testing.T) {
	d := newEventDevice(t)
	p := testPluginConnected(t, d.srv)
	rest := p.Metrics().Snapshot()
	if rest.TxFrames == 0 || rest.RxBytes == 0 {
		t.Fatalf("connect made REST requests and counted none: %+v", rest)
	}

	var got collector
	if err := p.Subscribe(dhsc.ValueRequest{Path: "self"}, got.fn); err != nil {
		t.Fatal(err)
	}
	d.events(codec.MsgEvents, "/self", `[{"op":"replace","path":"","value":{"a":1}}]`)
	eventually(t, "the value", func() bool { return len(got.paths()) == 1 })
	d.send(`not json`)
	eventually(t, "the unreadable frame", func() bool { return p.Metrics().Snapshot().DecodeErrors == 1 })
	snap := p.Metrics().Snapshot()
	if snap.TxFrames != rest.TxFrames+1 || snap.TxBytes <= rest.TxBytes {
		t.Errorf("one CreateSubscription sent, counted %d frame(s) / %d bytes more", snap.TxFrames-rest.TxFrames, snap.TxBytes-rest.TxBytes)
	}
	// The answer, the event and the unreadable frame.
	if snap.RxFrames != rest.RxFrames+3 || snap.RxBytes <= rest.RxBytes {
		t.Errorf("three frames read, counted %d", snap.RxFrames-rest.RxFrames)
	}

	// A refusal is a NAK.
	d.mu.Lock()
	d.answer = refuse
	d.mu.Unlock()
	_ = p.Subscribe(dhsc.ValueRequest{Path: "matrix.state"}, got.fn)
	if n := p.Metrics().Snapshot().NAKs; n != 1 {
		t.Errorf("NAKs = %d, want 1", n)
	}
	// The unsubscribe's DeleteSubscription is sent and counted too.
	before := p.Metrics().Snapshot().TxFrames
	if err := p.Unsubscribe(dhsc.ValueRequest{Path: "self"}); err != nil {
		t.Fatal(err)
	}
	if after := p.Metrics().Snapshot().TxFrames; after != before+1 {
		t.Errorf("DeleteSubscription: %d frame(s) counted", after-before)
	}

	// A channel the device drops is a reconnect to come.
	d.mu.Lock()
	conn := d.conn
	d.mu.Unlock()
	done := p.SessionDone()
	_ = conn.Close(1001, "going away")
	eventually(t, "the loss", func() bool { return closed(done) })
	if n := p.Metrics().Snapshot().Reconnects; n != 1 {
		t.Errorf("Reconnects = %d, want 1", n)
	}
	_ = p.Disconnect()
}

func TestASilentDeviceIsCountedAsATimeout(t *testing.T) {
	d := newEventDevice(t)
	d.answer = func(*eventDevice, asked) {}
	restore := subscribeTimeout
	subscribeTimeout = 30 * time.Millisecond
	t.Cleanup(func() { subscribeTimeout = restore })
	p := testPluginConnected(t, d.srv)
	t.Cleanup(func() { _ = p.Disconnect() })
	_ = p.Subscribe(dhsc.ValueRequest{Path: "self"}, func(dhsc.Event) {})
	if n := p.Metrics().Snapshot().Timeouts; n != 1 {
		t.Errorf("Timeouts = %d, want 1", n)
	}
}
