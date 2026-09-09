package registry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is04"
	httpsession "dhs/internal/amwa/session/http"
)

// wsPair returns a live server-side WebSocket and the raw client
// socket feeding it. Closing the client is what makes a send fail on
// demand — the push path's error arms are otherwise unreachable.
func wsPair(t *testing.T) (*httpsession.WebSocket, net.Conn) {
	t.Helper()
	accepted := make(chan *httpsession.WebSocket, 1)
	ts := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		ws, err := httpsession.AcceptWebSocket(w, r)
		if err != nil {
			t.Errorf("AcceptWebSocket: %v", err)
			return
		}
		accepted <- ws
		<-r.Context().Done()
	}))
	t.Cleanup(ts.Close)

	addr := strings.TrimPrefix(ts.URL, "http://")
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	handshake := "GET /ws HTTP/1.1\r\nHost: " + addr + "\r\n" +
		"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := conn.Write([]byte(handshake)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1024)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Read(buf); err != nil {
		t.Fatalf("read upgrade: %v", err)
	}

	select {
	case ws := <-accepted:
		t.Cleanup(func() { _ = conn.Close() })
		return ws, conn
	case <-time.After(10 * time.Second):
		t.Fatal("the server never accepted the socket")
		return nil, nil
	}
}

// pushSub builds a subscription bound to a live socket.
func pushSub(ws *httpsession.WebSocket, rate int) *subscription {
	return &subscription{
		ID: "sub-1", ResourcePath: "/nodes", MaxUpdateRate: rate,
		ws: ws, source: "sub-1", closeCh: make(chan struct{}),
	}
}

// nodeChange is one store change carrying a real Node document.
func nodeChange(t *testing.T, kind ChangeKind, id string) Change {
	t.Helper()
	raw := mustJSONBytes(t, validNode(id))
	c := Change{Kind: kind, ResourceType: is04.ResourceNode, ID: id, Timestamp: time.Now()}
	switch kind {
	case ChangeCreated:
		c.Post = raw
	case ChangeDeleted:
		c.Pre = raw
	default:
		c.Pre, c.Post = raw, raw
	}
	return c
}

// An unrated subscription is written to inline; when its socket has
// gone the failure is reported rather than retried or swallowed.
func TestEnqueueUnratedSendsInlineAndReportsFailure(t *testing.T) {
	ws, conn := wsPair(t)
	m := NewSubscriptionManager(nil, NewStore(), "127.0.0.1:0", "v1.3")
	sub := pushSub(ws, 0)

	m.enqueue(sub, nodeChange(t, ChangeCreated, fxNode))
	peer := &wsPeer{conn: conn}
	if topic, rows := grainRows(t, peer.nextGrain(t)); topic != "/nodes/" || len(rows) != 1 {
		t.Fatalf("inline grain = %s %v", topic, rows)
	}

	// A document that cannot be marshalled produces no frame at all —
	// the grain is dropped, not sent half-built.
	broken := Change{
		Kind: ChangeCreated, ResourceType: is04.ResourceNode, ID: fxNode,
		Post: json.RawMessage(`{`), Timestamp: time.Now(),
	}
	m.enqueue(sub, broken)

	// With the peer gone, the send fails and is reported.
	_ = conn.Close()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		m.enqueue(sub, nodeChange(t, ChangeCreated, fxDevice))
		if err := ws.SendText([]byte("probe")); err != nil {
			return // the socket is gone, which is what the arm above saw
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("the socket never reported the peer's disappearance")
}

// A rated subscription buffers and flushes on its timer; flushing with
// nothing pending, or with no socket, is a no-op rather than an empty
// frame.
func TestFlushBuffersAndRefusals(t *testing.T) {
	ws, conn := wsPair(t)
	m := NewSubscriptionManager(nil, NewStore(), "127.0.0.1:0", "v1.3")
	sub := pushSub(ws, 50)

	m.enqueue(sub, nodeChange(t, ChangeCreated, fxNode))
	m.enqueue(sub, nodeChange(t, ChangeCreated, fxDevice))
	sub.bufMu.Lock()
	pending := len(sub.pending)
	armed := sub.flushTimer != nil
	sub.bufMu.Unlock()
	if pending != 2 || !armed {
		t.Fatalf("pending = %d, armed = %v; want both changes buffered behind a timer", pending, armed)
	}

	m.flush(sub)
	peer := &wsPeer{conn: conn}
	if _, rows := grainRows(t, peer.nextGrain(t)); len(rows) != 2 {
		t.Errorf("the flush must carry both buffered rows: %d", len(rows))
	}

	// Nothing pending: no frame.
	m.flush(sub)
	// A subscription whose socket was never installed: also no frame.
	m.flush(&subscription{ID: "sub-2", source: "sub-2", MaxUpdateRate: 50})

	// A grain that cannot be built is skipped; a send to a dead peer
	// is reported.
	sub.bufMu.Lock()
	sub.pending = []Change{{
		Kind: ChangeCreated, ResourceType: is04.ResourceNode, ID: fxNode,
		Post: json.RawMessage(`{`), Timestamp: time.Now(),
	}}
	sub.bufMu.Unlock()
	m.flush(sub)

	_ = conn.Close()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		sub.bufMu.Lock()
		sub.pending = []Change{nodeChange(t, ChangeCreated, fxNode)}
		sub.bufMu.Unlock()
		m.flush(sub)
		if err := ws.SendText([]byte("probe")); err != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("the flush never saw the peer disappear")
}

// A subscription that was created but never upgraded receives
// nothing: onChange skips it rather than writing to a nil socket.
func TestOnChangeSkipsASubscriptionWithNoSocket(t *testing.T) {
	store := NewStore()
	m := NewSubscriptionManager(nil, store, "127.0.0.1:0", "v1.3")
	m.mu.Lock()
	m.subs["sub-1"] = &subscription{ID: "sub-1", ResourcePath: "/nodes", source: "sub-1"}
	m.mu.Unlock()

	if err := store.PutNode(validNode(fxNode)); err != nil {
		t.Fatal(err)
	}
}

// Releasing a subscription stops its pending flush: a timer left
// running would write to a socket the manager has already dropped.
func TestRemoveSubStopsAPendingFlush(t *testing.T) {
	ws, _ := wsPair(t)
	m := NewSubscriptionManager(nil, NewStore(), "127.0.0.1:0", "v1.3")
	sub := pushSub(ws, 5_000) // long window: the timer is still armed
	m.mu.Lock()
	m.subs[sub.ID] = sub
	m.mu.Unlock()

	m.enqueue(sub, nodeChange(t, ChangeCreated, fxNode))
	sub.bufMu.Lock()
	armed := sub.flushTimer != nil
	sub.bufMu.Unlock()
	if !armed {
		t.Fatal("the flush must be armed before the subscription is released")
	}

	m.removeSub(sub.ID)
	m.mu.Lock()
	_, still := m.subs[sub.ID]
	m.mu.Unlock()
	if still {
		t.Error("the subscription must be gone")
	}
	sub.bufMu.Lock()
	stopped := sub.flushTimer == nil
	sub.bufMu.Unlock()
	if !stopped {
		t.Error("the pending flush must be stopped with it")
	}
}

// The ancestry projection maintains its own index as changes arrive:
// a first change on an empty index, a removal that drops the entry,
// and a resource that stays in the set across an update.
func TestAncestryProjectMaintainsItsIndex(t *testing.T) {
	const (
		root  = "10000000-0000-4000-8000-000000000001"
		child = "10000000-0000-4000-8000-000000000002"
	)
	sub := &subscription{
		ID: "sub-1", ResourcePath: "/sources", source: "sub-1",
		ancestryType: ancestryChildren, ancestryID: root,
	}

	inSet := mustJSONBytes(t, map[string]any{"id": child, "parents": []string{root}})

	// The first change allocates the index and reports the entry.
	got, ok := sub.ancestryProject(Change{
		Kind: ChangeCreated, ResourceType: is04.ResourceSource, ID: child, Post: inSet,
	})
	if !ok || got.Kind != ChangeCreated {
		t.Fatalf("entering the set = %+v, %v", got, ok)
	}

	// An update that keeps it in the set carries both halves.
	got, ok = sub.ancestryProject(Change{
		Kind: ChangeUpdated, ResourceType: is04.ResourceSource, ID: child,
		Pre: inSet, Post: inSet,
	})
	if !ok || got.Kind != ChangeUpdated || len(got.Pre) == 0 || len(got.Post) == 0 {
		t.Errorf("staying in the set = %+v, %v", got, ok)
	}

	// A removal drops the entry from the index and reports it.
	got, ok = sub.ancestryProject(Change{
		Kind: ChangeDeleted, ResourceType: is04.ResourceSource, ID: child, Pre: inSet,
	})
	if !ok || got.Kind != ChangeDeleted {
		t.Errorf("leaving the set = %+v, %v", got, ok)
	}

	// Once gone, a further change about it is outside the set on both
	// sides and is not reported at all.
	outside := mustJSONBytes(t, map[string]any{"id": child, "parents": []string{}})
	if _, ok := sub.ancestryProject(Change{
		Kind: ChangeCreated, ResourceType: is04.ResourceSource, ID: child, Post: outside,
	}); ok {
		t.Error("a resource outside the ancestry set must not be reported")
	}

	// A subscription with no ancestry filter passes everything through.
	plain := &subscription{ID: "sub-2"}
	if _, ok := plain.ancestryProject(Change{Kind: ChangeCreated}); !ok {
		t.Error("no ancestry filter reports everything")
	}
}

// A subscription cannot be minted without an id: entropy the host
// cannot supply is a 500, not a subscription a controller can never
// address.
func TestSubscriptionPostWithoutEntropy(t *testing.T) {
	prev := randRead
	randRead = func([]byte) (int, error) { return 0, errors.New("no entropy") }
	t.Cleanup(func() { randRead = prev })

	if _, err := newUUIDLike(); err == nil {
		t.Error("newUUIDLike must report the failure")
	}

	m := newSubManager(t)
	status, body := callHandler(t, m.HandlePost(subQueryBase), stdhttp.MethodPost,
		subQueryBase+"/subscriptions", `{"resource_path":"/nodes"}`)
	if status != stdhttp.StatusInternalServerError {
		t.Errorf("POST without entropy = %d (%+v)", status, body)
	}
}

// A grain that cannot be rendered is reported and skipped on every
// push path — the socket stays open and the next change still gets
// through, rather than the subscriber receiving a half-formed frame.
func TestGrainThatCannotBeBuiltIsSkipped(t *testing.T) {
	prev := buildGrain
	buildGrain = func(string, []Change, time.Time) ([]byte, error) {
		return nil, errors.New("scripted grain failure")
	}
	t.Cleanup(func() { buildGrain = prev })

	ws, _ := wsPair(t)
	m := NewSubscriptionManager(nil, NewStore(), "127.0.0.1:0", "v1.3")

	// The inline path.
	m.enqueue(pushSub(ws, 0), nodeChange(t, ChangeCreated, fxNode))

	// The buffered path.
	rated := pushSub(ws, 50)
	m.enqueue(rated, nodeChange(t, ChangeCreated, fxNode))
	m.flush(rated)

	// The sync path: a fresh subscriber whose snapshot cannot be
	// rendered still gets a socket, and the server does not fall over.
	addr, store, _ := wsFixture(t)
	if err := store.PutNode(validNode("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")); err != nil {
		t.Fatal(err)
	}
	peer, _ := openSubscription(t, addr, SubscriptionRequest{ResourcePath: "/nodes"})
	if op, _, arrived := peer.tryFrame(t, 500*time.Millisecond); arrived && op == 0x1 {
		t.Error("no grain can be rendered, so no text frame may arrive")
	}
}

// A subscriber that vanishes the instant it is upgraded fails both
// the snapshot send and the keep-alive ping; neither may take the
// registry with it.
func TestSubscriberThatDiesAtTheUpgrade(t *testing.T) {
	store := populated(t)
	// A large snapshot so the socket is closed while the frame is
	// still being written.
	for i := 0; i < 400; i++ {
		id := fmt.Sprintf("50000000-0000-4000-8000-%012d", i)
		if err := store.PutNode(validNode(id)); err != nil {
			t.Fatal(err)
		}
	}
	mgr := NewSubscriptionManager(nil, store, "127.0.0.1:0", "v1.3")
	mgr.SetWSKeepAlive(10*time.Millisecond, time.Minute)
	addr, stop := startRegistryHTTP(t, store, mgr)
	t.Cleanup(stop)

	body, err := json.Marshal(SubscriptionRequest{ResourcePath: "/nodes"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := stdhttp.Post("http://"+addr+"/x-nmos/query/v1.3/subscriptions",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	var sub SubscriptionResource
	if err := json.Unmarshal(raw, &sub); err != nil {
		t.Fatal(err)
	}

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	handshake := "GET /x-nmos/query/v1.3/subscriptions/" + sub.ID + "/ws HTTP/1.1\r\n" +
		"Host: " + addr + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := conn.Write([]byte(handshake)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 256)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Read(buf); err != nil {
		t.Fatalf("read upgrade: %v", err)
	}
	// Gone, mid-snapshot.
	_ = conn.Close()

	// The registry releases the subscription and keeps serving.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		mgr.mu.Lock()
		_, still := mgr.subs[sub.ID]
		mgr.mu.Unlock()
		if !still {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if status, _ := get(t, "http://"+addr+"/x-nmos/query/v1.3/nodes?paging.limit=1"); status != stdhttp.StatusOK {
		t.Errorf("the registry must keep serving after a peer vanished: %d", status)
	}
}

// The keep-alive stops on its own when the socket refuses a ping —
// the reader is unblocking on the same dead socket and owns the
// teardown, so the pinger must not spin on a peer that is gone.
func TestPingUntilStopsOnADeadSocket(t *testing.T) {
	ws, conn := wsPair(t)
	_ = conn.Close()

	done := make(chan struct{})
	go func() {
		pingUntil(ws, time.Millisecond, make(chan struct{}))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the pinger must stop when the socket refuses a ping")
	}

	// And it stops when the caller says so, on a live socket.
	live, _ := wsPair(t)
	stop := make(chan struct{})
	done = make(chan struct{})
	go func() {
		pingUntil(live, time.Millisecond, stop)
		close(done)
	}()
	close(stop)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the pinger must stop when it is told to")
	}
}
