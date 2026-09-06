package acp2

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/acp2/codec"
	"dhs/internal/consumer"
)

// session_waiters_test.go pins the deterministic waiter-death protocol
// (issue #694): the readLoop exit sweeps every registered waiter with a
// nil sentinel under waitMu, preserving an already-delivered reply, and
// later registrations fail fast. No test here depends on scheduling.

// TestFailWaiters_SentinelAndPreservedReply drives failWaiters directly:
// an empty waiter gets the nil sentinel, a waiter whose reply was already
// delivered keeps the real reply (the sentinel send is skipped), and any
// registration after the sweep is refused.
func TestFailWaiters_SentinelAndPreservedReply(t *testing.T) {
	s := NewSession(nil, testLogger())

	chEmpty, err := s.addWaiter(1)
	if err != nil {
		t.Fatalf("addWaiter(1): %v", err)
	}
	chFull, err := s.addWaiter(2)
	if err != nil {
		t.Fatalf("addWaiter(2): %v", err)
	}
	// Simulate routeReply having delivered before the connection died.
	chFull <- &codec.ACP2Message{Type: codec.ACP2TypeReply, MTID: 2}

	s.failWaiters()

	if msg := <-chEmpty; msg != nil {
		t.Errorf("empty waiter got %+v, want the nil sentinel", msg)
	}
	if msg := <-chFull; msg == nil || msg.MTID != 2 {
		t.Errorf("delivered reply not preserved across the sweep: %+v", msg)
	}
	if _, err := s.addWaiter(3); err == nil {
		t.Error("addWaiter after failWaiters must refuse")
	}
}

// TestDoACP2_FailFastAfterDeath covers DoACP2's addWaiter error return:
// once the read loop has exited, the request is refused before any frame
// is sent (no connection needed).
func TestDoACP2_FailFastAfterDeath(t *testing.T) {
	s := NewSession(nil, testLogger())
	s.failWaiters()

	_, err := s.DoACP2(context.Background(), 0, &codec.ACP2Message{
		Type: codec.ACP2TypeRequest,
		Func: codec.ACP2FuncGetVersion,
	})
	if err == nil || !strings.Contains(err.Error(), "connection closed") {
		t.Fatalf("DoACP2 on dead session: got %v, want connection-closed error", err)
	}
}

// TestAN2Request_FailFastAfterDeath covers an2Request's addWaiter error
// return on a session whose read loop has exited.
func TestAN2Request_FailFastAfterDeath(t *testing.T) {
	s := NewSession(nil, testLogger())
	s.failWaiters()

	_, err := s.an2Request(context.Background(), codec.AN2FuncGetVersion, 0, nil)
	if err == nil || !strings.Contains(err.Error(), "connection closed") {
		t.Fatalf("an2Request on dead session: got %v, want connection-closed error", err)
	}
}

// msgWaiter is a slog.Handler that signals a channel the first time a
// record with the wanted message is handled. It lets a test observe
// "this code path executed" without polling or sleeping.
type msgWaiter struct {
	want string
	ch   chan struct{}
	once sync.Once
}

func (h *msgWaiter) Enabled(context.Context, slog.Level) bool { return true }
func (h *msgWaiter) Handle(_ context.Context, r slog.Record) error {
	if r.Message == h.want {
		h.once.Do(func() { close(h.ch) })
	}
	return nil
}
func (h *msgWaiter) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *msgWaiter) WithGroup(string) slog.Handler      { return h }

// TestRunReconnectBackoff_FailureArm deterministically covers the
// backoff loop's attempt-failed arm (warn + delay doubling). The loop
// dials a dead port, so the first attempt always fails; the test waits
// for the logged failure (msgWaiter — no timing guess) before closing
// rc.done to stop the loop. Previously this arm was only covered when
// TestReconnect_BackoffRetries' relisten goroutine happened to lose an
// 850 ms scheduling race (#694 coverage class, named by the CI floor).
func TestRunReconnectBackoff_FailureArm(t *testing.T) {
	// A dead port: bind, close. Even if another process grabs it, the
	// reconnect attempt still fails (an AN2 handshake against a stranger
	// errors), which is all this test needs.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	_ = ln.Close()

	failed := &msgWaiter{want: "acp2 reconnect: attempt failed", ch: make(chan struct{})}
	p := &Plugin{logger: slog.New(failed)}
	p.rc = &reconnectState{done: make(chan struct{})}

	loopDone := make(chan struct{})
	go func() { p.runReconnectBackoff(host, port); close(loopDone) }()

	select {
	case <-failed.ch: // the failure arm ran
	case <-time.After(15 * time.Second):
		t.Fatal("backoff loop never logged a failed attempt")
	}
	close(p.rc.done)
	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Fatal("backoff loop did not stop on rc.done")
	}
}

// TestRequests_SendErrorNotConnected covers the sendFrame error returns of
// DoACP2 and an2Request (and sendFrame's conn==nil guard): the waiter table
// is alive, but there is no connection to write to.
func TestRequests_SendErrorNotConnected(t *testing.T) {
	s := NewSession(nil, testLogger())

	_, err := s.DoACP2(context.Background(), 0, &codec.ACP2Message{
		Type: codec.ACP2TypeRequest,
		Func: codec.ACP2FuncGetVersion,
	})
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("DoACP2 without a conn: got %v, want ErrNotConnected", err)
	}

	_, err = s.an2Request(context.Background(), codec.AN2FuncGetVersion, 0, nil)
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("an2Request without a conn: got %v, want ErrNotConnected", err)
	}
}

// TestSendFrame_WriteError covers sendFrame's conn.Write error arm with a
// deterministically dead pipe (no TCP race involved).
func TestSendFrame_WriteError(t *testing.T) {
	s := NewSession(nil, testLogger())
	c1, c2 := net.Pipe()
	_ = c1.Close()
	_ = c2.Close()
	s.conn = c1

	err := s.sendFrame(context.Background(), &codec.AN2Frame{
		Proto: codec.AN2ProtoACP2, Type: codec.AN2TypeData, Payload: []byte{0, 0, 0, 0},
	})
	var te *consumer.TransportError
	if !errors.As(err, &te) || te.Op != "send" {
		t.Fatalf("sendFrame on closed pipe: got %v, want TransportError{Op: send}", err)
	}
}

// TestDiagProbe_SendError covers diagProbe's send-error status arm: the
// waiter table is alive but the session has no connection, so the probe
// reports the send failure verbatim.
func TestDiagProbe_SendError(t *testing.T) {
	s := NewSession(nil, testLogger())
	r := diagProbe(context.Background(), s, "probe", codec.AN2ProtoACP2, 1,
		codec.AN2TypeData, []byte{0x00, 0x00, 0x01, 0x00}, 100*time.Millisecond)
	if !strings.HasPrefix(r.Status, "error: send: ") {
		t.Fatalf("probe status = %q, want 'error: send: ...'", r.Status)
	}
}
