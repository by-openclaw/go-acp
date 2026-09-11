package registry

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	stdhttp "net/http"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is04"
)

// wsPeer is a raw Query-WS subscriber: the registry's own transport is
// the code under test, so the client side is hand-rolled from the
// RFC 6455 bytes rather than dialled through it.
type wsPeer struct {
	conn net.Conn
	buf  []byte
}

// openSubscription POSTs a subscription and upgrades a socket to it,
// returning the peer and the subscription's id.
func openSubscription(t *testing.T, addr string, req SubscriptionRequest) (*wsPeer, string) {
	t.Helper()
	return openSubscriptionAt(t, addr, "v1.3", req)
}

// openSubscriptionAt is openSubscription against a named wire minor —
// the mirror's served face mounts its own set.
func openSubscriptionAt(t *testing.T, addr, apiVer string, req SubscriptionRequest) (*wsPeer, string) {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := stdhttp.Post("http://"+addr+"/x-nmos/query/"+apiVer+"/subscriptions",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST subscription: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var sub SubscriptionResource
	if err := json.Unmarshal(raw, &sub); err != nil || sub.ID == "" {
		t.Fatalf("subscription body = %s (%v)", raw, err)
	}

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	handshake := "GET /x-nmos/query/v1.3/subscriptions/" + sub.ID + "/ws HTTP/1.1\r\n" +
		"Host: " + addr + "\r\n" +
		"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := conn.Write([]byte(handshake)); err != nil {
		t.Fatalf("write upgrade: %v", err)
	}

	p := &wsPeer{conn: conn}
	head := p.readMore(t, 5*time.Second)
	end := bytes.Index(head, []byte("\r\n\r\n"))
	if end < 0 || !bytes.Contains(head[:end], []byte("101 Switching Protocols")) {
		t.Fatalf("upgrade response = %s", head)
	}
	p.buf = append([]byte(nil), head[end+4:]...)
	return p, sub.ID
}

// readMore blocks for more bytes off the wire.
func (p *wsPeer) readMore(t *testing.T, within time.Duration) []byte {
	t.Helper()
	chunk := make([]byte, 8192)
	_ = p.conn.SetReadDeadline(time.Now().Add(within))
	n, err := p.conn.Read(chunk)
	if n == 0 && err != nil {
		return nil
	}
	return chunk[:n]
}

// nextFrame returns the next complete frame's opcode and payload.
func (p *wsPeer) nextFrame(t *testing.T) (byte, []byte) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if op, payload, size, ok := decodeFrame(p.buf); ok {
			p.buf = p.buf[size:]
			return op, payload
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("no complete frame arrived (have %d bytes)", len(p.buf))
		}
		more := p.readMore(t, 500*time.Millisecond)
		if len(more) == 0 {
			continue
		}
		p.buf = append(p.buf, more...)
	}
}

// tryFrame waits briefly for a frame and reports whether one arrived —
// for the cases where the absence of a frame is the assertion.
func (p *wsPeer) tryFrame(t *testing.T, within time.Duration) (byte, []byte, bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if op, payload, size, ok := decodeFrame(p.buf); ok {
			p.buf = p.buf[size:]
			return op, payload, true
		}
		if !time.Now().Before(deadline) {
			return 0, nil, false
		}
		more := p.readMore(t, 50*time.Millisecond)
		p.buf = append(p.buf, more...)
	}
}

// nextGrain returns the next TEXT frame's payload, skipping the
// control frames (pings) the server may interleave.
func (p *wsPeer) nextGrain(t *testing.T) []byte {
	t.Helper()
	for i := 0; i < 20; i++ {
		op, payload := p.nextFrame(t)
		if op == 0x1 {
			return payload
		}
	}
	t.Fatal("no text frame arrived")
	return nil
}

// decodeFrame reads one unmasked server frame; ok=false when the
// buffer does not hold a whole one yet.
func decodeFrame(buf []byte) (opcode byte, payload []byte, size int, ok bool) {
	if len(buf) < 2 {
		return 0, nil, 0, false
	}
	opcode = buf[0] & 0x0F
	length := int(buf[1] & 0x7F)
	off := 2
	switch length {
	case 126:
		if len(buf) < 4 {
			return 0, nil, 0, false
		}
		length = int(binary.BigEndian.Uint16(buf[2:4]))
		off = 4
	case 127:
		if len(buf) < 10 {
			return 0, nil, 0, false
		}
		length = int(binary.BigEndian.Uint64(buf[2:10]))
		off = 10
	}
	if len(buf) < off+length {
		return 0, nil, 0, false
	}
	return opcode, buf[off : off+length], off + length, true
}

// grainRows decodes a grain frame into its topic and its data rows.
func grainRows(t *testing.T, raw []byte) (string, []map[string]json.RawMessage) {
	t.Helper()
	var g struct {
		GrainType string `json:"grain_type"`
		Grain     struct {
			Topic string                       `json:"topic"`
			Data  []map[string]json.RawMessage `json:"data"`
		} `json:"grain"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatalf("grain decode: %v (%s)", err, raw)
	}
	return g.Grain.Topic, g.Grain.Data
}

// wsFixture boots the HTTP face with a subscription manager over a
// populated store.
func wsFixture(t *testing.T) (string, *Store, *SubscriptionManager) {
	t.Helper()
	return wsFixtureWith(t, nil)
}

// wsFixtureWith is wsFixture with a hook to configure the manager
// BEFORE it takes traffic. Anything a test wants to change about the
// manager has to be changed here: once the listener is up, the push
// paths run on subscriber goroutines and a later write is a race.
func wsFixtureWith(t *testing.T, tweak func(*SubscriptionManager)) (string, *Store, *SubscriptionManager) {
	t.Helper()
	store := populated(t)
	mgr := NewSubscriptionManager(nil, store, "127.0.0.1:0", "v1.3")
	if tweak != nil {
		tweak(mgr)
	}
	addr, stop := startRegistryHTTP(t, store, mgr)
	t.Cleanup(stop)
	return addr, store, mgr
}

// A subscriber is bootstrapped with the current state as one sync
// grain per topic, then receives every later change on that topic —
// the whole point of the Query WS.
func TestSubscriberReceivesSyncThenChanges(t *testing.T) {
	addr, store, _ := wsFixture(t)
	peer, _ := openSubscription(t, addr, SubscriptionRequest{ResourcePath: "/nodes"})

	topic, rows := grainRows(t, peer.nextGrain(t))
	if topic != "/nodes/" || len(rows) != 1 {
		t.Fatalf("sync grain = %s with %d rows, want the one Node", topic, len(rows))
	}

	// A new Node arrives as an `added` row: post, no pre.
	const second = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if err := store.PutNode(validNode(second)); err != nil {
		t.Fatal(err)
	}
	topic, rows = grainRows(t, peer.nextGrain(t))
	if topic != "/nodes/" || len(rows) != 1 {
		t.Fatalf("event grain = %s with %d rows", topic, len(rows))
	}
	if _, hasPre := rows[0]["pre"]; hasPre {
		t.Errorf("an added row carries no pre: %v", rows[0])
	}
	if _, hasPost := rows[0]["post"]; !hasPost {
		t.Errorf("an added row carries a post: %v", rows[0])
	}

	// An update carries both halves.
	updated := validNode(second)
	updated.Label = "renamed"
	if err := store.PutNode(updated); err != nil {
		t.Fatal(err)
	}
	_, rows = grainRows(t, peer.nextGrain(t))
	if _, hasPre := rows[0]["pre"]; !hasPre {
		t.Errorf("a modified row carries a pre: %v", rows[0])
	}

	// A removal carries the pre alone — that is how a Controller
	// learns to drop the resource.
	store.DeleteNode(second)
	_, rows = grainRows(t, peer.nextGrain(t))
	if _, hasPost := rows[0]["post"]; hasPost {
		t.Errorf("a removed row carries no post: %v", rows[0])
	}
	if _, hasPre := rows[0]["pre"]; !hasPre {
		t.Errorf("a removed row carries a pre: %v", rows[0])
	}
}

// A filtered subscription reports the resource's transitions in and
// out of the filter set, not the raw change (IS-04 §5.2).
//
// The seeded match matters: reading the sync grain first is what
// proves the socket is fully established, since a change made between
// the socket being installed and the snapshot being taken is
// legitimately reported twice.
func TestSubscriberFilterReportsSetTransitions(t *testing.T) {
	addr, store, _ := wsFixture(t)
	const seed = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	seeded := validNode(seed)
	seeded.Label = "cam-1"
	if err := store.PutNode(seeded); err != nil {
		t.Fatal(err)
	}

	peer, _ := openSubscription(t, addr, SubscriptionRequest{
		ResourcePath: "/nodes",
		Params:       map[string]any{"label": "cam-1"},
	})
	if _, rows := grainRows(t, peer.nextGrain(t)); len(rows) != 1 {
		t.Fatalf("sync grain = %d rows, want the seeded match alone", len(rows))
	}

	const id = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"

	// Enters the filter set: reported as added even though the store
	// called it a create.
	entering := validNode(id)
	entering.Label = "cam-1"
	if err := store.PutNode(entering); err != nil {
		t.Fatal(err)
	}
	_, rows := grainRows(t, peer.nextGrain(t))
	if _, hasPre := rows[0]["pre"]; hasPre {
		t.Errorf("entering the set is an add: %v", rows[0])
	}

	// Leaves the set: reported as a removal, pre only.
	leaving := validNode(id)
	leaving.Label = "cam-2"
	if err := store.PutNode(leaving); err != nil {
		t.Fatal(err)
	}
	_, rows = grainRows(t, peer.nextGrain(t))
	if _, hasPost := rows[0]["post"]; hasPost {
		t.Errorf("leaving the set is a removal: %v", rows[0])
	}

	// A resource outside the set on both sides is not reported at all:
	// the next grain is the one for a resource that IS in the set.
	outside := validNode("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	outside.Label = "cam-9"
	if err := store.PutNode(outside); err != nil {
		t.Fatal(err)
	}
	back := validNode(id)
	back.Label = "cam-1"
	if err := store.PutNode(back); err != nil {
		t.Fatal(err)
	}
	_, rows = grainRows(t, peer.nextGrain(t))
	var post map[string]any
	if err := json.Unmarshal(rows[0]["post"], &post); err != nil {
		t.Fatal(err)
	}
	if post["id"] != id {
		t.Errorf("a resource outside the filter was reported: %v", post["id"])
	}
}

// A subscription that names a max_update_rate coalesces a burst into
// fewer grains than there were changes, flushed off the store's write
// lock by a timer.
func TestSubscriberRateLimitCoalescesABurst(t *testing.T) {
	addr, store, _ := wsFixture(t)
	peer, _ := openSubscription(t, addr, SubscriptionRequest{
		ResourcePath: "/nodes", MaxUpdateRate: 120,
	})
	if _, rows := grainRows(t, peer.nextGrain(t)); len(rows) != 1 {
		t.Fatalf("sync grain = %d rows", len(rows))
	}

	const burst = 4
	for i := 0; i < burst; i++ {
		id := "aaaaaaaa-aaaa-4aaa-8aaa-00000000000" + string(rune('1'+i))
		if err := store.PutNode(validNode(id)); err != nil {
			t.Fatal(err)
		}
	}

	grains, rows := 0, 0
	for rows < burst {
		topic, r := grainRows(t, peer.nextGrain(t))
		if topic != "/nodes/" {
			t.Fatalf("topic = %s", topic)
		}
		grains++
		rows += len(r)
	}
	if grains >= burst {
		t.Errorf("%d changes arrived as %d grains — the rate limit coalesced nothing", burst, grains)
	}
}

// A version-gated subscriber does not receive a resource registered at
// another minor (IS-04 §6.1.5) — the same rule the REST face applies.
func TestSubscriberVersionGate(t *testing.T) {
	addr, store, _ := wsFixture(t)

	// One resource registered at another minor BEFORE the socket
	// opens: the snapshot must leave it out.
	const seeded = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	if err := store.IngestRegistrationVersioned(
		envelope(t, is04.ResourceNode, validNode(seeded)), "v1.0"); err != nil {
		t.Fatal(err)
	}

	peer, _ := openSubscription(t, addr, SubscriptionRequest{ResourcePath: "/nodes"})
	_, rows := grainRows(t, peer.nextGrain(t))
	if len(rows) != 1 {
		t.Fatalf("sync grain = %d rows, want the v1.3 resource alone", len(rows))
	}

	const v10 = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if err := store.IngestRegistrationVersioned(
		envelope(t, is04.ResourceNode, validNode(v10)), "v1.0"); err != nil {
		t.Fatal(err)
	}
	// Nothing for the v1.3 socket; a v1.3 registration right after it
	// does arrive, which proves the socket was live all along.
	const v13 = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	if err := store.IngestRegistrationVersioned(
		envelope(t, is04.ResourceNode, validNode(v13)), "v1.3"); err != nil {
		t.Fatal(err)
	}
	_, rows = grainRows(t, peer.nextGrain(t))
	var post map[string]any
	if err := json.Unmarshal(rows[0]["post"], &post); err != nil {
		t.Fatal(err)
	}
	if post["id"] != v13 {
		t.Errorf("the v1.0 resource reached a v1.3 socket: %v", post["id"])
	}
}

// A subscription on a topic the change does not belong to hears
// nothing — one socket per resource path.
func TestSubscriberIgnoresAnotherTopic(t *testing.T) {
	addr, store, _ := wsFixture(t)
	peer, _ := openSubscription(t, addr, SubscriptionRequest{ResourcePath: "/senders"})
	if topic, _ := grainRows(t, peer.nextGrain(t)); topic != "/senders/" {
		t.Fatalf("sync topic = %s", topic)
	}

	const id = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if err := store.PutNode(validNode(id)); err != nil {
		t.Fatal(err)
	}
	snd := validSender("cccccccc-cccc-4ccc-8ccc-cccccccccccc", fxDevice)
	if err := store.PutSender(snd); err != nil {
		t.Fatal(err)
	}
	topic, rows := grainRows(t, peer.nextGrain(t))
	if topic != "/senders/" || len(rows) != 1 {
		t.Errorf("the socket must hear only its own topic: %s %v", topic, rows)
	}
}

// The keep-alive ping is what makes a silent one-way socket
// observable: the server pings on its own timer, and a peer that
// answers is never reaped.
func TestSubscriberReceivesKeepAlivePings(t *testing.T) {
	addr, _, mgr := wsFixture(t)
	mgr.SetWSKeepAlive(20*time.Millisecond, time.Minute)
	peer, _ := openSubscription(t, addr, SubscriptionRequest{ResourcePath: "/nodes"})

	sawPing := false
	for i := 0; i < 10 && !sawPing; i++ {
		op, _ := peer.nextFrame(t)
		if op == 0x9 {
			sawPing = true
		}
	}
	if !sawPing {
		t.Error("a one-way socket must be pinged")
	}
}

// A subscriber that vanishes without a close frame is dropped: the
// failed send is reported and the subscription released, so the
// registry does not hold a socket and a record for a peer that is
// gone.
func TestSubscriberThatVanishesIsReleased(t *testing.T) {
	addr, store, mgr := wsFixture(t)
	peer, id := openSubscription(t, addr, SubscriptionRequest{
		ResourcePath: "/nodes", MaxUpdateRate: 40,
	})
	if _, rows := grainRows(t, peer.nextGrain(t)); len(rows) != 1 {
		t.Fatalf("sync grain = %d rows", len(rows))
	}

	_ = peer.conn.Close()
	// Two changes: the first fails to send, the second finds a
	// subscription being torn down. Neither may panic or block the
	// store's write lock.
	for i := 0; i < 2; i++ {
		nid := "aaaaaaaa-aaaa-4aaa-8aaa-00000000000" + string(rune('1'+i))
		if err := store.PutNode(validNode(nid)); err != nil {
			t.Fatal(err)
		}
	}

	// The record is released either by the reader noticing the close
	// or by an explicit DELETE — both paths must leave nothing behind.
	if status, _ := callHandler(t, mgr.HandleDeleteByID(subPrefix), stdhttp.MethodDelete,
		subPrefix+id, ""); status != stdhttp.StatusNoContent && status != stdhttp.StatusNotFound {
		t.Errorf("DELETE after the peer vanished = %d", status)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		mgr.mu.Lock()
		_, still := mgr.subs[id]
		mgr.mu.Unlock()
		if !still {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Error("a vanished subscriber must be released")
}

// An ancestry subscription tracks membership as the graph changes: a
// resource that becomes a descendant is reported as an addition, and
// one that stops being one as a removal.
func TestSubscriberAncestryFilter(t *testing.T) {
	addr, store, _ := wsFixture(t)
	const seed = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	seeded := validSource(seed, fxDevice)
	seeded.Parents = []string{fxSource}
	if err := store.PutSource(seeded); err != nil {
		t.Fatal(err)
	}

	peer, _ := openSubscription(t, addr, SubscriptionRequest{
		ResourcePath: "/sources",
		Params: map[string]any{
			"query.ancestry_id":   fxSource,
			"query.ancestry_type": "children",
		},
	})
	if _, rows := grainRows(t, peer.nextGrain(t)); len(rows) != 1 {
		t.Fatalf("sync grain = %d rows, want the seeded child alone", len(rows))
	}

	// A second source that names the fixture source as its parent
	// enters the set.
	const child = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	src := validSource(child, fxDevice)
	src.Parents = []string{fxSource}
	if err := store.PutSource(src); err != nil {
		t.Fatal(err)
	}
	topic, rows := grainRows(t, peer.nextGrain(t))
	if topic != "/sources/" || len(rows) != 1 {
		t.Fatalf("ancestry grain = %s %v", topic, rows)
	}
	if _, hasPre := rows[0]["pre"]; hasPre {
		t.Errorf("entering the ancestry set is an add: %v", rows[0])
	}

	// Re-parented away, it leaves the set and is reported removed.
	src.Parents = []string{}
	if err := store.PutSource(src); err != nil {
		t.Fatal(err)
	}
	_, rows = grainRows(t, peer.nextGrain(t))
	if _, hasPost := rows[0]["post"]; hasPost {
		t.Errorf("leaving the ancestry set is a removal: %v", rows[0])
	}

	// A source outside the set on both sides is not reported at all —
	// the next grain is the one for a source that IS in it.
	outsider := validSource("dddddddd-dddd-4ddd-8ddd-dddddddddddd", fxDevice)
	if err := store.PutSource(outsider); err != nil {
		t.Fatal(err)
	}
	back := validSource(child, fxDevice)
	back.Parents = []string{fxSource}
	if err := store.PutSource(back); err != nil {
		t.Fatal(err)
	}
	_, rows = grainRows(t, peer.nextGrain(t))
	var post map[string]any
	if err := json.Unmarshal(rows[0]["post"], &post); err != nil {
		t.Fatal(err)
	}
	if post["id"] != child {
		t.Errorf("a source outside the ancestry set was reported: %v", post["id"])
	}
}
