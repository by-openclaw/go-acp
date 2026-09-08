package dnssd

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"dhs/internal/amwa/codec/dnssd"
)

// discardLogger returns a non-nil slog.Logger whose output is dropped.
// Several code paths guard their debug logging behind `logger != nil`
// (readLoop decode/read errors, sendQueries write errors); a non-nil
// discard logger exercises those log statements without polluting test
// output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// localUDPConn binds a throwaway UDP socket on 127.0.0.1:0. It is a
// plain unicast socket — NOT a multicast join — so the browser/responder
// read+write logic can be driven over loopback without binding the real
// 224.0.0.251:5353 group (which openMulticastConns does and which unit
// tests must avoid, per the package's transport note).
func localUDPConn(t *testing.T) *net.UDPConn {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	return c
}

// registerResponse builds a valid mDNS response (QR set) advertising a
// single _nmos-register._tcp instance, used as a canned packet fed into
// readLoop. Built through the codec so the fixture cannot drift from
// what Decode accepts (same discipline as connection/client_test.go).
func registerResponse(t *testing.T) []byte {
	t.Helper()
	pkt, err := dnssd.EncodeAnnounce(dnssd.Instance{
		Name:    "reg1",
		Service: dnssd.ServiceRegister,
		Domain:  "local",
		Host:    "reg1.local",
		Port:    8235,
		IPv4:    []net.IP{net.IPv4(127, 0, 0, 1).To4()},
		TXT:     map[string]string{dnssd.TXTKeyAPIVer: "v1.3"},
	}, true)
	if err != nil {
		t.Fatalf("encode announce: %v", err)
	}
	return pkt
}

// sendTo writes payload to dst from a fresh ephemeral socket. Used to
// deliver canned packets into a readLoop/serveQueries socket over
// loopback.
func sendTo(t *testing.T, dst net.Addr, payload []byte) {
	t.Helper()
	s, err := net.DialUDP("udp4", nil, dst.(*net.UDPAddr))
	if err != nil {
		t.Fatalf("dial udp: %v", err)
	}
	defer func() { _ = s.Close() }()
	if _, err := s.Write(payload); err != nil {
		t.Fatalf("write udp: %v", err)
	}
}

// --- readLoop ---

// TestReadLoop_SetReadDeadlineError guards the earliest exit arm: a
// closed socket makes SetReadDeadline fail on the first iteration, and
// readLoop must return rather than spin. Regression guard against a
// busy-loop if the socket dies underneath the reader.
func TestReadLoop_SetReadDeadlineError(t *testing.T) {
	c := localUDPConn(t)
	_ = c.Close() // deadline set will now fail immediately
	b := &stdlibBrowser{logger: discardLogger(), conns: []*net.UDPConn{c}}
	done := make(chan struct{})
	go func() { b.readLoop(c); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop did not return after SetReadDeadline error")
	}
}

// TestReadLoop_FanOutAndClosedReturn covers the happy fan-out path (a
// decoded response Instance is delivered to an active subscription) and
// the post-read `closed` guard: once Close flips b.closed, the next read
// must make readLoop return even though a packet arrived. Delivering the
// Instance to the sub also exercises the `case sub.out <- ins` arm.
func TestReadLoop_FanOutAndClosedReturn(t *testing.T) {
	recv := localUDPConn(t)
	b := &stdlibBrowser{logger: discardLogger(), conns: []*net.UDPConn{recv}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := &browseSub{ctx: ctx, service: dnssd.ServiceRegister, out: make(chan dnssd.Instance, 4)}
	b.subs = []*browseSub{sub}

	done := make(chan struct{})
	go func() { b.readLoop(recv); close(done) }()

	sendTo(t, recv.LocalAddr(), registerResponse(t))

	select {
	case ins := <-sub.out:
		if ins.Name != "reg1" || ins.Port != 8235 {
			t.Fatalf("unexpected instance: %+v", ins)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no instance fanned out to subscription")
	}

	// Flip closed BEFORE delivering the next packet so the post-read
	// guard sees it and returns.
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	sendTo(t, recv.LocalAddr(), registerResponse(t))

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop did not return after closed flag set")
	}
	_ = recv.Close()
}

// TestReadLoop_DecodeAndNonResponseAndReadError drives three arms in one
// loop: (1) garbage bytes -> Decode error -> continue; (2) a query
// packet (QR clear) -> IsResponse false -> continue; (3) closing the
// socket mid-read -> non-timeout read error -> return (with logger set
// so the debug branch executes).
func TestReadLoop_DecodeAndNonResponseAndReadError(t *testing.T) {
	recv := localUDPConn(t)
	b := &stdlibBrowser{logger: discardLogger(), conns: []*net.UDPConn{recv}}
	done := make(chan struct{})
	go func() { b.readLoop(recv); close(done) }()

	// (1) undecodable: fewer than the 12-byte header.
	sendTo(t, recv.LocalAddr(), []byte{0x01, 0x02, 0x03})
	time.Sleep(40 * time.Millisecond)

	// (2) a valid QUERY (QR bit clear) must be skipped, not fanned out.
	q, err := dnssd.EncodeQuery(dnssd.ServiceRegister+".local", dnssd.TypePTR, false)
	if err != nil {
		t.Fatalf("encode query: %v", err)
	}
	sendTo(t, recv.LocalAddr(), q)
	time.Sleep(40 * time.Millisecond)

	// (3) close the socket to force a non-timeout read error -> return.
	_ = recv.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop did not return after socket close")
	}
}

// TestReadLoop_ReadTimeoutContinues proves the 500 ms read deadline is a
// soft poll: a timeout must `continue` the loop (so the closed flag and
// context can be re-checked), never terminate the reader. Without this
// arm a quiet link would drop the browser after the first half-second.
func TestReadLoop_ReadTimeoutContinues(t *testing.T) {
	recv := localUDPConn(t)
	b := &stdlibBrowser{logger: discardLogger(), conns: []*net.UDPConn{recv}}
	done := make(chan struct{})
	go func() { b.readLoop(recv); close(done) }()

	// Let at least one 500 ms deadline lapse with no traffic so the
	// timeout->continue arm runs, then a valid packet proves the loop
	// is still alive afterwards.
	time.Sleep(650 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("readLoop exited on a read timeout instead of continuing")
	default:
	}

	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	sendTo(t, recv.LocalAddr(), registerResponse(t))
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop did not return after closed flag set")
	}
	_ = recv.Close()
}

// TestReadLoop_FanOutContextCancelled covers the `case <-sub.ctx.Done()`
// arm of the fan-out select: an unbuffered out channel with no reader
// plus an already-cancelled subscription context means readLoop must
// drop the Instance via the ctx.Done branch rather than block forever on
// a departing subscriber.
func TestReadLoop_FanOutContextCancelled(t *testing.T) {
	recv := localUDPConn(t)
	b := &stdlibBrowser{logger: discardLogger(), conns: []*net.UDPConn{recv}}
	subCtx, subCancel := context.WithCancel(context.Background())
	subCancel() // already done
	sub := &browseSub{ctx: subCtx, service: dnssd.ServiceRegister, out: make(chan dnssd.Instance)}
	b.subs = []*browseSub{sub}

	done := make(chan struct{})
	go func() { b.readLoop(recv); close(done) }()

	sendTo(t, recv.LocalAddr(), registerResponse(t))
	// Give the loop time to process and take the ctx.Done arm.
	time.Sleep(80 * time.Millisecond)

	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	sendTo(t, recv.LocalAddr(), registerResponse(t))
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop did not return")
	}
	_ = recv.Close()
}

// --- Browse ---

// TestBrowse_EmptyService guards the input check: an empty service name
// is rejected before any socket work.
func TestBrowse_EmptyService(t *testing.T) {
	b := &stdlibBrowser{logger: discardLogger()}
	if _, err := b.Browse(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty service")
	}
}

// TestBrowse_LifecycleAndSecondSubscription drives the full Browse path
// over loopback: the first call starts the shared read loop (first ==
// true), a second concurrent call must NOT start a second read loop
// (first == false) but still register its own subscription, and
// cancelling a browse context must close that call's channel via the
// cleanup goroutine while leaving the browser usable. Delivering a
// packet proves fan-out to a live subscription end-to-end.
func TestBrowse_LifecycleAndSecondSubscription(t *testing.T) {
	recv := localUDPConn(t)
	b := &stdlibBrowser{logger: discardLogger(), conns: []*net.UDPConn{recv}}
	defer func() { _ = b.Close() }()

	ctx1, cancel1 := context.WithCancel(context.Background())
	ch1, err := b.Browse(ctx1, dnssd.ServiceRegister)
	if err != nil {
		t.Fatalf("browse 1: %v", err)
	}
	if !b.reading {
		t.Fatal("first Browse should have started the read loop")
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	ch2, err := b.Browse(ctx2, dnssd.ServiceQuery)
	if err != nil {
		t.Fatalf("browse 2: %v", err)
	}
	b.mu.Lock()
	nsubs := len(b.subs)
	b.mu.Unlock()
	if nsubs != 2 {
		t.Fatalf("want 2 active subscriptions, got %d", nsubs)
	}

	// Deliver a register response; only ch1 (register) should see it.
	sendTo(t, recv.LocalAddr(), registerResponse(t))
	select {
	case ins := <-ch1:
		if ins.Name != "reg1" {
			t.Fatalf("unexpected instance on ch1: %+v", ins)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("register instance never delivered to ch1")
	}

	// Cancelling ctx1 must close ch1 via the per-Browse cleanup goroutine.
	cancel1()
	select {
	case _, ok := <-ch1:
		// Either the buffered nothing-left close, or a drained value then close.
		if ok {
			// drain until closed
			for range ch1 {
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ch1 not closed after context cancel")
	}
	_ = ch2
}

// --- sendQueries ---

// TestSendQueries_EncodeError guards the encode arm: a service label
// longer than 63 bytes makes EncodeQuery fail; sendQueries must log and
// return without writing or panicking.
func TestSendQueries_EncodeError(t *testing.T) {
	b := &stdlibBrowser{logger: discardLogger(), conns: []*net.UDPConn{localUDPConn(t)}}
	defer func() { _ = closeConns(b.conns) }()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		// 64-char first label -> ErrLabelTooLong inside EncodeQuery's
		// send() closure (logged, no write attempted). sendQueries then
		// blocks on its ticker until the context is cancelled.
		b.sendQueries(ctx, strings.Repeat("a", 64))
		close(done)
	}()
	time.Sleep(60 * time.Millisecond) // let the initial send() encode-fail
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("sendQueries did not return after context cancel")
	}
}

// TestSendQueries_WriteErrorThenCancel exercises the write-error branch
// (a closed conn fails WriteToUDP while ctx.Err()==nil, so the debug log
// fires) and the ctx.Done() return arm. It never waits for the 30 s
// QueryInterval tick — that re-arm is covered separately by design note.
func TestSendQueries_WriteErrorThenCancel(t *testing.T) {
	c := localUDPConn(t)
	_ = c.Close() // WriteToUDP will now fail
	b := &stdlibBrowser{logger: discardLogger(), conns: []*net.UDPConn{c}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		b.sendQueries(ctx, dnssd.ServiceRegister)
		close(done)
	}()
	// Let the initial send() run (write fails + logs) before cancelling.
	time.Sleep(60 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("sendQueries did not return after context cancel")
	}
}

// TestSendQueries_SuccessfulWrite runs one send() over an open socket so
// the non-error side of the WriteToUDP branch executes, then cancels.
func TestSendQueries_SuccessfulWrite(t *testing.T) {
	b := &stdlibBrowser{logger: discardLogger(), conns: []*net.UDPConn{localUDPConn(t)}}
	defer func() { _ = closeConns(b.conns) }()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		b.sendQueries(ctx, dnssd.ServiceRegister)
		close(done)
	}()
	time.Sleep(60 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("sendQueries did not return")
	}
}

// --- stdlibBrowser.Close ---

// TestBrowserClose_Idempotent verifies Close shuts the sockets once and
// is a no-op on the second call (the closed guard).
func TestBrowserClose_Idempotent(t *testing.T) {
	b := &stdlibBrowser{logger: discardLogger(), conns: []*net.UDPConn{localUDPConn(t)}}
	if err := b.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("second Close should be a no-op: %v", err)
	}
}

// --- closeConns / hasIPv4 helpers ---

// TestCloseConns covers the happy multi-socket close, the empty slice
// (returns nil), and the first-error capture when a socket is already
// closed.
func TestCloseConns(t *testing.T) {
	if err := closeConns(nil); err != nil {
		t.Fatalf("closeConns(nil) = %v, want nil", err)
	}
	a, b := localUDPConn(t), localUDPConn(t)
	if err := closeConns([]*net.UDPConn{a, b}); err != nil {
		t.Fatalf("closeConns two open: %v", err)
	}
	// Already-closed socket -> Close returns an error -> captured as firstErr.
	c := localUDPConn(t)
	_ = c.Close()
	if err := closeConns([]*net.UDPConn{c}); err == nil {
		t.Fatal("closeConns should report the error of an already-closed socket")
	}
}

// TestHasIPv4 checks the address-family probe: a bogus interface whose
// Addrs() call fails returns false, and among the host's real interfaces
// at least the loopback (127.0.0.1) must report an IPv4 address.
func TestHasIPv4(t *testing.T) {
	// Fabricated interface index that the OS cannot resolve -> Addrs()
	// errors -> hasIPv4 returns false.
	bogus := &net.Interface{Index: 0x7fffffff, Name: "dhs-nonexistent0"}
	if hasIPv4(bogus) {
		t.Error("hasIPv4 on a nonexistent interface should be false")
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("enumerate interfaces: %v", err)
	}
	sawIPv4 := false
	for i := range ifaces {
		if hasIPv4(&ifaces[i]) {
			sawIPv4 = true
		}
	}
	if !sawIPv4 {
		t.Error("expected at least one host interface to report IPv4")
	}
}

// --- stdlibResponder ---

// TestResponder_AnnounceValidation guards the required-field check on
// Announce: an instance without Name or Service is rejected before any
// packet is queued.
func TestResponder_AnnounceValidation(t *testing.T) {
	r := &stdlibResponder{logger: discardLogger(), conns: []*net.UDPConn{localUDPConn(t)}}
	defer func() { _ = closeConns(r.conns) }()
	if err := r.Announce(context.Background(), dnssd.Instance{Service: dnssd.ServiceRegister}); err == nil {
		t.Fatal("Announce with empty Name should error")
	}
	if err := r.Announce(context.Background(), dnssd.Instance{Name: "x"}); err == nil {
		t.Fatal("Announce with empty Service should error")
	}
}

// TestResponder_AnnounceEncodeError covers the arm where the instance
// passes Announce's Name+Service check but EncodeAnnounce still fails
// (missing Host/Port), so Announce returns the codec error.
func TestResponder_AnnounceEncodeError(t *testing.T) {
	r := &stdlibResponder{logger: discardLogger(), conns: []*net.UDPConn{localUDPConn(t)}}
	defer func() { _ = closeConns(r.conns) }()
	// Name+Service set (passes the guard) but no Host/Port -> EncodeAnnounce fails.
	err := r.Announce(context.Background(), dnssd.Instance{Name: "x", Service: dnssd.ServiceRegister})
	if err == nil {
		t.Fatal("Announce should surface EncodeAnnounce error for missing Host/Port")
	}
}

// TestResponder_AnnounceHappy runs a full Announce over loopback and
// then cancels the context so the 3-packet announce goroutine and the
// serveQueries goroutine both exit promptly (via ctx.Done) rather than
// sleeping ~1 s between announcements.
func TestResponder_AnnounceHappy(t *testing.T) {
	r := &stdlibResponder{logger: discardLogger(), conns: []*net.UDPConn{localUDPConn(t)}}
	defer func() { _ = closeConns(r.conns) }()
	ctx, cancel := context.WithCancel(context.Background())
	ins := dnssd.Instance{Name: "reg1", Service: dnssd.ServiceRegister, Domain: "local", Host: "reg1.local", Port: 8235}
	if err := r.Announce(ctx, ins); err != nil {
		t.Fatalf("Announce: %v", err)
	}
	r.mu.Lock()
	n := len(r.instances)
	r.mu.Unlock()
	if n != 1 {
		t.Fatalf("want 1 announced instance, got %d", n)
	}
	cancel()
	time.Sleep(60 * time.Millisecond) // let the goroutines observe ctx.Done
}

// TestResponder_AnnounceWriteError covers the write-error log arm inside
// the announce goroutine: a closed socket fails WriteToUDP while
// ctx.Err()==nil, so the debug branch fires. The context is cancelled
// promptly so the goroutine does not sleep between the 3 announcements.
func TestResponder_AnnounceWriteError(t *testing.T) {
	c := localUDPConn(t)
	_ = c.Close() // WriteToUDP now fails
	r := &stdlibResponder{logger: discardLogger(), conns: []*net.UDPConn{c}}
	ctx, cancel := context.WithCancel(context.Background())
	ins := dnssd.Instance{Name: "reg1", Service: dnssd.ServiceRegister, Domain: "local", Host: "reg1.local", Port: 8235}
	if err := r.Announce(ctx, ins); err != nil {
		t.Fatalf("Announce: %v", err)
	}
	time.Sleep(60 * time.Millisecond) // let the first write fail + log
	cancel()
	time.Sleep(40 * time.Millisecond)
}

// TestResponder_UpdateWriteError covers Update's write-error log arm: a
// stored, well-formed instance is re-emitted over a closed socket, so
// WriteToUDP fails while ctx.Err()==nil.
func TestResponder_UpdateWriteError(t *testing.T) {
	c := localUDPConn(t)
	_ = c.Close() // WriteToUDP now fails
	r := &stdlibResponder{logger: discardLogger(), conns: []*net.UDPConn{c}}
	ins := dnssd.Instance{Name: "reg1", Service: dnssd.ServiceRegister, Domain: "local", Host: "reg1.local", Port: 8235}
	r.mu.Lock()
	r.instances = append(r.instances, ins)
	r.mu.Unlock()
	if err := r.Update(context.Background(), ins); err != nil {
		t.Fatalf("Update should still succeed even if the re-emit write fails: %v", err)
	}
}

// TestResponder_UpdateArms covers Update's four arms: validation error
// (empty Name/Service), not-announced error, the happy TXT replacement +
// re-emit, and the closed-responder error.
func TestResponder_UpdateArms(t *testing.T) {
	r := &stdlibResponder{logger: discardLogger(), conns: []*net.UDPConn{localUDPConn(t)}}
	defer func() { _ = closeConns(r.conns) }()

	if err := r.Update(context.Background(), dnssd.Instance{Service: dnssd.ServiceRegister}); err == nil {
		t.Fatal("Update with empty Name should error")
	}

	ins := dnssd.Instance{Name: "reg1", Service: dnssd.ServiceRegister, Domain: "local", Host: "reg1.local", Port: 8235, TXT: map[string]string{"ver": "1"}}
	if err := r.Update(context.Background(), ins); err == nil {
		t.Fatal("Update of a never-announced instance should error")
	}

	// Announce it directly (bypass the goroutine) then Update the TXT.
	r.mu.Lock()
	r.instances = append(r.instances, ins)
	r.mu.Unlock()
	upd := ins
	upd.TXT = map[string]string{"ver": "2"}
	if err := r.Update(context.Background(), upd); err != nil {
		t.Fatalf("Update happy path: %v", err)
	}
	r.mu.Lock()
	got := r.instances[0].TXT["ver"]
	r.mu.Unlock()
	if got != "2" {
		t.Fatalf("TXT not replaced: got ver=%q", got)
	}

	// Closed responder -> Update refuses.
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	if err := r.Update(context.Background(), upd); err == nil {
		t.Fatal("Update on a closed responder should error")
	}
}

// TestResponder_UpdateEncodeError covers the arm where a stored instance
// matches by FullName but is missing Host/Port, so the re-emit's
// EncodeAnnounce fails and Update returns that error.
func TestResponder_UpdateEncodeError(t *testing.T) {
	r := &stdlibResponder{logger: discardLogger(), conns: []*net.UDPConn{localUDPConn(t)}}
	defer func() { _ = closeConns(r.conns) }()
	// Stored instance has Name+Service (so FullName matches) but no Host/Port.
	bad := dnssd.Instance{Name: "reg1", Service: dnssd.ServiceRegister, Domain: "local"}
	r.mu.Lock()
	r.instances = append(r.instances, bad)
	r.mu.Unlock()
	if err := r.Update(context.Background(), bad); err == nil {
		t.Fatal("Update should surface EncodeAnnounce error for stored instance missing Host/Port")
	}
}

// TestResponder_CloseGoodbyeAndIdempotent covers Close: a well-formed
// stored instance yields a goodbye packet (EncodeGoodbye err==nil arm),
// a malformed stored instance is skipped (err!=nil arm), and a second
// Close is a no-op.
func TestResponder_CloseGoodbyeAndIdempotent(t *testing.T) {
	r := &stdlibResponder{logger: discardLogger(), conns: []*net.UDPConn{localUDPConn(t)}}
	good := dnssd.Instance{Name: "reg1", Service: dnssd.ServiceRegister, Domain: "local", Host: "reg1.local", Port: 8235}
	bad := dnssd.Instance{Name: "reg2", Service: dnssd.ServiceRegister, Domain: "local"} // no Host/Port -> EncodeGoodbye fails
	r.mu.Lock()
	r.instances = append(r.instances, good, bad)
	r.mu.Unlock()

	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("second Close should be a no-op: %v", err)
	}
}

// --- serveQueries ---

// TestServeQueries_UnicastReply drives the responder's query-serving
// loop: a matching PTR query carrying the QU (unicast-response) bit must
// be answered back to the querying source address, not the multicast
// group. Also exercises the decode-error skip (garbage) and the
// non-matching / non-response skips before the real query arrives.
func TestServeQueries_UnicastReply(t *testing.T) {
	srv := localUDPConn(t)
	r := &stdlibResponder{logger: discardLogger(), conns: []*net.UDPConn{srv}}
	ins := dnssd.Instance{Name: "reg1", Service: dnssd.ServiceRegister, Domain: "local", Host: "reg1.local", Port: 8235}
	// A second instance with the SAME PTRName but no Host/Port: it
	// matches the query so it enters the reply loop, but EncodeAnnounce
	// fails for it, exercising the `err != nil -> continue` skip arm.
	bad := dnssd.Instance{Name: "reg2", Service: dnssd.ServiceRegister, Domain: "local"}
	r.mu.Lock()
	r.instances = append(r.instances, ins, bad)
	r.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	go r.serveQueries(ctx)

	// A client socket that will both send the QU query and receive the reply.
	client, err := net.DialUDP("udp4", nil, srv.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	// Garbage first (decode-error skip), then a response packet
	// (IsResponse -> skip), then the real QU query.
	if _, err := client.Write([]byte{0xDE, 0xAD}); err != nil {
		t.Fatalf("write garbage: %v", err)
	}
	if _, err := client.Write(registerResponse(t)); err != nil {
		t.Fatalf("write response: %v", err)
	}
	q, err := dnssd.EncodeQuery(ins.PTRName(), dnssd.TypePTR, true) // QU bit set
	if err != nil {
		t.Fatalf("encode query: %v", err)
	}
	if _, err := client.Write(q); err != nil {
		t.Fatalf("write query: %v", err)
	}

	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, MaxMDNSPacketSize)
	n, err := client.Read(buf)
	if err != nil {
		t.Fatalf("no unicast reply: %v", err)
	}
	msg, err := dnssd.Decode(buf[:n])
	if err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if !msg.Header.IsResponse() {
		t.Fatal("reply is not a response")
	}
	insts := dnssd.DecodeInstances(msg, dnssd.ServiceRegister)
	if len(insts) != 1 || insts[0].Name != "reg1" {
		t.Fatalf("unexpected reply instances: %+v", insts)
	}
	cancel()
	time.Sleep(60 * time.Millisecond)
}

// TestServeQueries_MulticastReply covers the non-QU branch: a matching
// query without the unicast bit is answered to the multicast group
// address (a WriteToUDP to 224.0.0.251:5353 from the loopback socket —
// a send, not a group join). We can't easily observe the multicast
// datagram, so this test asserts the loop runs and exits cleanly.
func TestServeQueries_MulticastReply(t *testing.T) {
	srv := localUDPConn(t)
	r := &stdlibResponder{logger: discardLogger(), conns: []*net.UDPConn{srv}}
	ins := dnssd.Instance{Name: "reg1", Service: dnssd.ServiceRegister, Domain: "local", Host: "reg1.local", Port: 8235}
	r.mu.Lock()
	r.instances = append(r.instances, ins)
	r.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.serveQueries(ctx); close(done) }()

	q, err := dnssd.EncodeQuery(ins.PTRName(), dnssd.TypePTR, false) // no QU bit
	if err != nil {
		t.Fatalf("encode query: %v", err)
	}
	sendTo(t, srv.LocalAddr(), q)
	// Sleep past one 500 ms read deadline (with the context still live)
	// so the serve loop takes the timeout->continue arm before we cancel.
	time.Sleep(650 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("serveQueries did not exit after cancel")
	}
}

// TestServeQueries_SetDeadlineError covers the arm where the per-conn
// goroutine cannot even set a read deadline (socket already closed) and
// must return immediately, letting serveQueries' WaitGroup unblock.
func TestServeQueries_SetDeadlineError(t *testing.T) {
	c := localUDPConn(t)
	_ = c.Close()
	r := &stdlibResponder{logger: discardLogger(), conns: []*net.UDPConn{c}}
	done := make(chan struct{})
	go func() { r.serveQueries(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("serveQueries did not return when the socket is dead")
	}
}

// TestServeQueries_ReadError covers the non-timeout read-error return:
// closing the socket while the goroutine is blocked in ReadFromUDP makes
// the read fail (not a timeout), so the goroutine returns.
func TestServeQueries_ReadError(t *testing.T) {
	c := localUDPConn(t)
	r := &stdlibResponder{logger: discardLogger(), conns: []*net.UDPConn{c}}
	done := make(chan struct{})
	go func() { r.serveQueries(context.Background()); close(done) }()
	time.Sleep(100 * time.Millisecond) // let it set the deadline and block in read
	_ = c.Close()                      // force a non-timeout read error
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("serveQueries did not return after a read error")
	}
}
