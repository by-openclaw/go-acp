package events_test

// Fault paths of the IS-07 transport, driven from the outside: a peer
// that sends junk, a codec that cannot encode, a connection that dies
// between two frames. Each test names the contract it pins; none of
// them sleeps to synchronise — every wait is a channel, a deadline on
// the socket, or a poll bounded by a deadline (ADR-0029).

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is07"
	_ "dhs/internal/amwa/codec/is07/v10"
	"dhs/internal/amwa/session/events"
	httpsession "dhs/internal/amwa/session/http"
)

const (
	srcC        = "3f3e3d3c-3b3a-4039-8837-363534333231"
	testTimeout = 5 * time.Second
)

// logSink captures slog output so a test can assert on the operator
// sentence a fault produced, not just on the side effect.
type logSink struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (l *logSink) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logSink) contains(s string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Contains(l.buf.String(), s)
}

func (l *logSink) logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(l, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// waitUntil polls cond until it holds or the deadline passes. The
// poll interval is not a synchronisation primitive — the deadline is
// the bound, the interval just keeps the loop from spinning.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// fakeCodec wraps the real IS-07 codec and lets a test replace one
// method at a time. The Codec option on Publisher and Subscriber is
// the seam the package already offers for exactly this.
type fakeCodec struct {
	is07.Codec
	encodeMessage func(is07.Message) ([]byte, error)
	encodeCommand func(is07.Command) ([]byte, error)
	decodeMessage func([]byte) (is07.Message, error)
}

func (f *fakeCodec) EncodeMessage(m is07.Message) ([]byte, error) {
	if f.encodeMessage != nil {
		return f.encodeMessage(m)
	}
	return f.Codec.EncodeMessage(m)
}

func (f *fakeCodec) EncodeCommand(c is07.Command) ([]byte, error) {
	if f.encodeCommand != nil {
		return f.encodeCommand(c)
	}
	return f.Codec.EncodeCommand(c)
}

func (f *fakeCodec) DecodeMessage(b []byte) (is07.Message, error) {
	if f.decodeMessage != nil {
		return f.decodeMessage(b)
	}
	return f.Codec.DecodeMessage(b)
}

func boolEvent(src string) is07.EventBoolean {
	return is07.EventBoolean{
		EventCommon: is07.EventCommon{
			Identity:  is07.Identity{SourceID: src},
			Timing:    is07.Timing{CreationTimestamp: "1:0"},
			EventType: "boolean",
		},
		Payload: is07.PayloadBoolean{Value: true},
	}
}

// servePublisher mounts pub behind an httptest server and returns
// the ws:// URL. The server is closed after the publisher, so no
// connection outlives the thing serving it.
func servePublisher(t *testing.T, pub *events.Publisher) string {
	t.Helper()
	srv := httptest.NewServer(pub.Handler())
	t.Cleanup(func() {
		_ = pub.Close()
		srv.Close()
	})
	return wsURL(srv.URL)
}

// connectAndRun dials, starts Run in the background and returns the
// message stream plus a wait for Run's result.
func connectAndRun(t *testing.T, ctx context.Context, sub *events.Subscriber, url string) (<-chan is07.Message, func() error) {
	t.Helper()
	if err := sub.Connect(ctx, url); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = sub.Close() })
	got := make(chan is07.Message, 16)
	done := make(chan error, 1)
	go func() { done <- sub.Run(ctx, func(m is07.Message) { got <- m }) }()
	return got, func() error {
		select {
		case err := <-done:
			return err
		case <-time.After(testTimeout):
			t.Fatal("Run did not return")
			return nil
		}
	}
}

func recvOne(t *testing.T, got <-chan is07.Message) is07.Message {
	t.Helper()
	select {
	case m := <-got:
		return m
	case <-time.After(testTimeout):
		t.Fatal("no frame arrived")
		return nil
	}
}

// --- Publisher ---

// TestHandlerRejectsNonUpgradeRequest: the events path is a WebSocket
// endpoint; a plain GET gets a 400 that says so, not a hung socket.
func TestHandlerRejectsNonUpgradeRequest(t *testing.T) {
	pub := events.NewPublisher(events.PublisherOptions{})
	url := servePublisher(t, pub)
	resp, err := http.Get(strings.Replace(url, "ws://", "http://", 1))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a request that is not a WebSocket upgrade", resp.StatusCode)
	}
}

// TestPublishRejectsWhatCannotBeFannedOut: the source filter needs a
// source_id, so a nil message and a message the codec refuses are
// errors to the caller rather than silent no-ops.
func TestPublishRejectsWhatCannotBeFannedOut(t *testing.T) {
	pub := events.NewPublisher(events.PublisherOptions{})
	defer func() { _ = pub.Close() }()
	cases := []struct {
		name string
		msg  is07.Message
	}{
		{"nil message", nil},
		{"health is not a state event", is07.MessageHealth{}},
		{"state event without a source_id fails encoding", boolEvent("")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := pub.Publish(tc.msg); err == nil {
				t.Fatal("Publish accepted a message it cannot deliver")
			}
		})
	}
}

// TestPublishFansOutEveryStateVariant: number, string and object
// events carry their source_id in the same place as boolean and must
// reach a subscriber of that source.
func TestPublishFansOutEveryStateVariant(t *testing.T) {
	pub := events.NewPublisher(events.PublisherOptions{})
	url := servePublisher(t, pub)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	sub := events.NewSubscriber(events.SubscriberOptions{HeartbeatInterval: 0})
	got, _ := connectAndRun(t, ctx, sub, url)
	if err := sub.Subscribe([]string{srcA}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	waitUntil(t, "subscription applied", func() bool { return pub.SubscribedTo(srcA) })

	common := is07.EventCommon{
		Identity: is07.Identity{SourceID: srcA},
		Timing:   is07.Timing{CreationTimestamp: "1:0"},
	}
	number := common
	number.EventType = "number"
	str := common
	str.EventType = "string"
	obj := common
	obj.EventType = "object"
	for _, m := range []is07.Message{
		is07.EventNumber{EventCommon: number, Payload: is07.Number{Value: 3, Scale: 1}},
		is07.EventString{EventCommon: str, Payload: is07.PayloadString{Value: "on air"}},
		is07.EventObject{EventCommon: obj, Payload: is07.PayloadObject{"k": "v"}},
	} {
		if err := pub.Publish(m); err != nil {
			t.Fatalf("Publish(%T): %v", m, err)
		}
	}
	wantTypes := []string{"is07.EventNumber", "is07.EventString", "is07.EventObject"}
	for _, want := range wantTypes {
		if got := fmt.Sprintf("%T", recvOne(t, got)); got != want {
			t.Fatalf("received %s, want %s", got, want)
		}
	}
}

// TestCloseIsIdempotent: Node shutdown may Close the publisher from
// more than one path; the second call is a no-op, not a double-close
// panic.
func TestCloseIsIdempotent(t *testing.T) {
	pub := events.NewPublisher(events.PublisherOptions{HeartbeatInterval: time.Millisecond})
	if err := pub.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := pub.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestConnectionAfterCloseIsRefused: a socket that completes the
// upgrade against a closed publisher is closed straight away rather
// than registered on a publisher that will never publish again.
func TestConnectionAfterCloseIsRefused(t *testing.T) {
	pub := events.NewPublisher(events.PublisherOptions{})
	url := servePublisher(t, pub)
	if err := pub.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	sub := events.NewSubscriber(events.SubscriberOptions{HeartbeatInterval: 0})
	_, wait := connectAndRun(t, ctx, sub, url)
	if err := wait(); err != nil {
		t.Fatalf("Run after the publisher closed = %v, want a clean close", err)
	}
	if n := pub.SubscriberCount(); n != 0 {
		t.Fatalf("closed publisher holds %d clients", n)
	}
}

// TestBadCommandIsLoggedAndSkipped: IS-07 §5 gives the receiver two
// commands. Anything else is a defect on the peer, recorded and
// ignored — the connection stays up and a later valid command still
// lands.
func TestBadCommandIsLoggedAndSkipped(t *testing.T) {
	var sink logSink
	pub := events.NewPublisher(events.PublisherOptions{Logger: sink.logger()})
	url := servePublisher(t, pub)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	ws, err := httpsession.DialWebSocket(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = ws.Close() }()
	if err := ws.SendText([]byte(`{"command":"bogus"}`)); err != nil {
		t.Fatalf("send junk: %v", err)
	}
	valid, err := is07.Default().EncodeCommand(is07.CommandSubscription{Sources: []string{srcA}})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := ws.SendText(valid); err != nil {
		t.Fatalf("send subscription: %v", err)
	}
	waitUntil(t, "subscription after a bad command", func() bool { return pub.SubscribedTo(srcA) })
	if !sink.contains("bad command") {
		t.Fatal("the unknown command was not recorded")
	}
}

// TestSubscriptionReplayHonoursStateOf: the initial state is sent per
// source and only for sources the Node can encode a state for — an
// unknown source and an unencodable state are skipped, and neither
// stops the sources after them nor the subscription itself.
func TestSubscriptionReplayHonoursStateOf(t *testing.T) {
	pub := events.NewPublisher(events.PublisherOptions{
		StateOf: func(id string) (is07.Message, bool) {
			switch id {
			case srcA:
				return boolEvent(""), true // fails encoding: no source_id
			case srcB:
				return nil, false // unknown to this Node
			default:
				return boolEvent(srcC), true
			}
		},
	})
	url := servePublisher(t, pub)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	sub := events.NewSubscriber(events.SubscriberOptions{HeartbeatInterval: 0})
	got, _ := connectAndRun(t, ctx, sub, url)
	if err := sub.Subscribe([]string{srcA, srcB, srcC}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	m, ok := recvOne(t, got).(is07.EventBoolean)
	if !ok || m.Identity.SourceID != srcC {
		t.Fatalf("first frame = %+v, want the replayed state of %s", m, srcC)
	}
	// The connection is still live: a later publish arrives.
	if err := pub.Publish(boolEvent(srcA)); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if m, ok := recvOne(t, got).(is07.EventBoolean); !ok || m.Identity.SourceID != srcA {
		t.Fatalf("published event did not arrive after the replay: %+v", m)
	}
}

// TestHealthEncodeFailureKeepsTheConnection: a codec that cannot
// build the health reply logs it and keeps serving; the subscription
// that follows still applies.
func TestHealthEncodeFailureKeepsTheConnection(t *testing.T) {
	var sink logSink
	pub := events.NewPublisher(events.PublisherOptions{
		Logger: sink.logger(),
		Codec: &fakeCodec{Codec: is07.Default(), encodeMessage: func(m is07.Message) ([]byte, error) {
			if _, isHealth := m.(is07.MessageHealth); isHealth {
				return nil, errors.New("health cannot be encoded")
			}
			return is07.Default().EncodeMessage(m)
		}},
	})
	url := servePublisher(t, pub)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	sub := events.NewSubscriber(events.SubscriberOptions{HeartbeatInterval: 5 * time.Millisecond})
	connectAndRun(t, ctx, sub, url)
	waitUntil(t, "encode failure logged", func() bool { return sink.contains("encode health") })
	if err := sub.Subscribe([]string{srcA}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	waitUntil(t, "subscription after the encode failure", func() bool { return pub.SubscribedTo(srcA) })
}

// TestHeartbeatLoopSurvivesEncodeFailure: the unsolicited heartbeat
// skips a tick it cannot encode and keeps ticking, so a transient
// codec fault does not silently end the publisher's heartbeat.
func TestHeartbeatLoopSurvivesEncodeFailure(t *testing.T) {
	var ticks atomic.Int32
	pub := events.NewPublisher(events.PublisherOptions{
		HeartbeatInterval: time.Millisecond,
		Codec: &fakeCodec{Codec: is07.Default(), encodeMessage: func(is07.Message) ([]byte, error) {
			ticks.Add(1)
			return nil, errors.New("no")
		}},
	})
	waitUntil(t, "heartbeat still ticking after failures", func() bool { return ticks.Load() >= 3 })
	if err := pub.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// --- Subscriber ---

func TestConnectTwiceIsRefused(t *testing.T) {
	pub := events.NewPublisher(events.PublisherOptions{})
	url := servePublisher(t, pub)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	sub := events.NewSubscriber(events.SubscriberOptions{})
	if err := sub.Connect(ctx, url); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = sub.Close() }()
	if err := sub.Connect(ctx, url); err == nil {
		t.Fatal("a second Connect on a live subscriber must be refused")
	}
}

// TestSubscriberRequiresConnection: Subscribe and Run before Connect
// are caller errors with a sentence, not nil dereferences.
func TestSubscriberRequiresConnection(t *testing.T) {
	sub := events.NewSubscriber(events.SubscriberOptions{})
	cases := []struct {
		name string
		call func() error
	}{
		{"subscribe", func() error { return sub.Subscribe([]string{srcA}) }},
		{"run", func() error { return sub.Run(context.Background(), nil) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil || !strings.Contains(err.Error(), "not connected") {
				t.Fatalf("err = %v, want 'not connected'", err)
			}
		})
	}
}

// TestSubscribeRejectsDuplicateSources: command_subscription sources
// MUST be unique (IS-07 command_subscription.json); the codec refuses
// and the subscriber surfaces it rather than sending a frame the Node
// would reject.
func TestSubscribeRejectsDuplicateSources(t *testing.T) {
	pub := events.NewPublisher(events.PublisherOptions{})
	url := servePublisher(t, pub)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	sub := events.NewSubscriber(events.SubscriberOptions{})
	if err := sub.Connect(ctx, url); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = sub.Close() }()
	if err := sub.Subscribe([]string{srcA, srcA}); err == nil {
		t.Fatal("duplicate sources were sent on the wire")
	}
	// A nil set is a legal "subscribe to nothing" and encodes as [].
	if err := sub.Subscribe(nil); err != nil {
		t.Fatalf("Subscribe(nil): %v", err)
	}
}

// TestRunReturnsOnCancelledContext: cancellation is a clean exit,
// indistinguishable from Close to the caller.
func TestRunReturnsOnCancelledContext(t *testing.T) {
	pub := events.NewPublisher(events.PublisherOptions{})
	url := servePublisher(t, pub)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	sub := events.NewSubscriber(events.SubscriberOptions{})
	if err := sub.Connect(ctx, url); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = sub.Close() }()
	done, stop := context.WithCancel(ctx)
	stop()
	if err := sub.Run(done, nil); err != nil {
		t.Fatalf("Run on a cancelled context = %v, want nil", err)
	}
}

// TestRunAcceptsNilHandler: a caller that only wants the heartbeat
// kept alive passes nil; frames are decoded and dropped, not
// dereferenced.
func TestRunAcceptsNilHandler(t *testing.T) {
	pub := events.NewPublisher(events.PublisherOptions{
		StateOf: func(string) (is07.Message, bool) { return boolEvent(srcA), true },
	})
	url := servePublisher(t, pub)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	var decoded atomic.Int32
	sub := events.NewSubscriber(events.SubscriberOptions{
		HeartbeatInterval: 0,
		Codec: &fakeCodec{Codec: is07.Default(), decodeMessage: func(b []byte) (is07.Message, error) {
			decoded.Add(1)
			return is07.Default().DecodeMessage(b)
		}},
	})
	if err := sub.Connect(ctx, url); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- sub.Run(ctx, nil) }()
	if err := sub.Subscribe([]string{srcA}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	waitUntil(t, "a frame handed to the nil handler", func() bool { return decoded.Load() >= 1 })
	if err := sub.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Run = %v", err)
	}
}

// TestHeartbeatEncodeFailureKeepsTicking: the subscriber's heartbeat
// goroutine skips a tick it cannot encode rather than dying — the
// Node would otherwise reap the connection after the idle timeout.
func TestHeartbeatEncodeFailureKeepsTicking(t *testing.T) {
	pub := events.NewPublisher(events.PublisherOptions{})
	url := servePublisher(t, pub)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	var ticks atomic.Int32
	sub := events.NewSubscriber(events.SubscriberOptions{
		HeartbeatInterval: time.Millisecond,
		Codec: &fakeCodec{Codec: is07.Default(), encodeCommand: func(is07.Command) ([]byte, error) {
			ticks.Add(1)
			return nil, errors.New("no")
		}},
	})
	_, wait := connectAndRun(t, ctx, sub, url)
	waitUntil(t, "heartbeat still ticking after failures", func() bool { return ticks.Load() >= 3 })
	if err := sub.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := wait(); err != nil {
		t.Fatalf("Run = %v", err)
	}
}

// TestHeartbeatStopsWhenSendFails: once the socket is gone the
// heartbeat goroutine exits on its first failed send, so Run's
// teardown does not wait on a ticker that can never deliver. The
// codec gate makes the interleaving exact: the tick is inside
// EncodeCommand when Close lands, so the send that follows is the
// one that fails.
func TestHeartbeatStopsWhenSendFails(t *testing.T) {
	pub := events.NewPublisher(events.PublisherOptions{})
	url := servePublisher(t, pub)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	inEncode := make(chan struct{}, 1)
	gate := make(chan struct{})
	var once sync.Once
	sub := events.NewSubscriber(events.SubscriberOptions{
		HeartbeatInterval: time.Millisecond,
		Codec: &fakeCodec{Codec: is07.Default(), encodeCommand: func(c is07.Command) ([]byte, error) {
			once.Do(func() {
				inEncode <- struct{}{}
				<-gate
			})
			return is07.Default().EncodeCommand(c)
		}},
	})
	_, wait := connectAndRun(t, ctx, sub, url)
	select {
	case <-inEncode:
	case <-time.After(testTimeout):
		t.Fatal("heartbeat never ticked")
	}
	if err := sub.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	close(gate)
	if err := wait(); err != nil {
		t.Fatalf("Run = %v, want nil after Close", err)
	}
}

// TestBadFrameIsLoggedAndSkipped: a Node that emits a frame the
// codec rejects is a defect worth a log line, not a reason to drop
// the subscription — the valid frame after it still reaches the
// handler.
func TestBadFrameIsLoggedAndSkipped(t *testing.T) {
	health, err := is07.Default().EncodeMessage(is07.MessageHealth{
		Timing: is07.Timing{CreationTimestamp: "1:0", OriginTimestamp: "1:0"},
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	hold := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := httpsession.AcceptWebSocket(w, r)
		if err != nil {
			return
		}
		defer func() { _ = ws.Close() }()
		_ = ws.SendText([]byte("this is not an IS-07 frame"))
		_ = ws.SendText(health)
		<-hold
	}))
	defer srv.Close()
	defer close(hold)

	var sink logSink
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	sub := events.NewSubscriber(events.SubscriberOptions{HeartbeatInterval: 0, Logger: sink.logger()})
	got, _ := connectAndRun(t, ctx, sub, wsURL(srv.URL))
	if _, ok := recvOne(t, got).(is07.MessageHealth); !ok {
		t.Fatal("the valid frame after the junk one did not arrive")
	}
	if !sink.contains("bad frame") {
		t.Fatal("the junk frame was not recorded")
	}
}

// TestReadErrorIsSurfaced: a peer that breaks RFC 6455 (here: a
// reserved opcode) is not a clean close. Run returns the transport
// error so the Controller can tell "the Node hung up" from "the Node
// is broken".
func TestReadErrorIsSurfaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(rawUpgradeThenJunk))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	sub := events.NewSubscriber(events.SubscriberOptions{HeartbeatInterval: 0})
	_, wait := connectAndRun(t, ctx, sub, wsURL(srv.URL))
	err := wait()
	// Release the scripted peer (it waits for our close) before the
	// server is torn down.
	_ = sub.Close()
	if err == nil || !strings.Contains(err.Error(), "read") {
		t.Fatalf("Run = %v, want the read error surfaced", err)
	}
}

// rawUpgradeThenJunk completes the RFC 6455 §4.2.2 handshake by hand
// and then writes one frame with opcode 0x3 (reserved, §5.2) — a
// scripted peer the transport must reject.
func rawUpgradeThenJunk(w http.ResponseWriter, r *http.Request) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "no hijack", http.StatusInternalServerError)
		return
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	h := sha1.New()
	_, _ = io.WriteString(h, r.Header.Get("Sec-WebSocket-Key")+"258EAFA5-E914-47DA-95CA-C5AB0DC85B11")
	accept := base64.StdEncoding.EncodeToString(h.Sum(nil))
	_, _ = fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept)
	// FIN=1, RSV=0, opcode=0x3; MASK=0, length 0.
	_, _ = rw.Write([]byte{0x83, 0x00})
	_ = rw.Flush()
	// Keep the connection open until the client has read the frame;
	// closing first could race the junk with an EOF.
	_, _ = bufio.NewReader(conn).ReadByte()
}

func TestCloseWithoutConnectIsNil(t *testing.T) {
	sub := events.NewSubscriber(events.SubscriberOptions{})
	if err := sub.Close(); err != nil {
		t.Fatalf("Close before Connect = %v", err)
	}
	// And after a failed dial the subscriber is still closable.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := sub.Connect(ctx, "http://not-a-ws-url/"); err == nil {
		t.Fatal("expected a scheme error")
	}
	if err := sub.Close(); err != nil {
		t.Fatalf("Close after a failed Connect = %v", err)
	}
}
