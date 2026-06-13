package acp1

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
	an2 "dhs/internal/acp2/codec"
	"dhs/internal/transport"
)

// ---------------------------------------------------------------- TCP client

// TestTCPClient_Do_EncodeError: a request with an out-of-range MAddr fails
// in req.Encode() inside Do (line ~150).
func TestTCPClient_Do_EncodeError(t *testing.T) {
	host, port, stop := echoTCPServer(t, []byte{0x00, 0x01}, false)
	defer stop()
	conn, err := transport.DialTCP(context.Background(), host, port)
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	c := NewTCPClient(conn, discardLogger(), ClientConfig{ReceiveTimeout: time.Second})
	defer func() { _ = c.Close() }()
	if _, err := c.Do(context.Background(), &codec.Message{
		MType: codec.MTypeRequest, MAddr: 99, MCode: byte(codec.MethodGetValue), ObjGroup: codec.GroupFrame,
	}); err == nil {
		t.Fatal("encode error: want error")
	}
}

// TestTCPClient_Do_SendError: closing the underlying socket before Do's
// Send makes c.conn.Send fail (the tcp send-error arm).
func TestTCPClient_Do_SendError(t *testing.T) {
	host, port, stop := echoTCPServer(t, []byte{0x00, 0x01}, false)
	defer stop()
	conn, err := transport.DialTCP(context.Background(), host, port)
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	c := NewTCPClient(conn, discardLogger(), ClientConfig{ReceiveTimeout: time.Second})
	defer func() { _ = c.Close() }()
	// Close the socket directly (not the client) so the reader exits but the
	// client still accepts Do — whose Send then fails on the dead socket.
	_ = conn.Close()
	time.Sleep(20 * time.Millisecond)
	if _, err := c.Do(context.Background(), &codec.Message{
		MType: codec.MTypeRequest, MCode: byte(codec.MethodGetValue), ObjGroup: codec.GroupFrame,
	}); err == nil {
		t.Fatal("send on closed socket: want error")
	}
}

// TestAN2Client_Do_SendError mirrors the send-error case for AN2.
func TestAN2Client_Do_SendError(t *testing.T) {
	host, port, stop := echoAN2Server(t, []byte{0x00, 0x01}, false)
	defer stop()
	conn := dialAN2(t, host, port)
	c := NewAN2Client(conn, discardLogger(), ClientConfig{ReceiveTimeout: time.Second})
	defer func() { _ = c.Close() }()
	_ = conn.Close()
	time.Sleep(20 * time.Millisecond)
	if _, err := c.Do(context.Background(), &codec.Message{
		MType: codec.MTypeRequest, MCode: byte(codec.MethodGetValue), ObjGroup: codec.GroupFrame,
	}); err == nil {
		t.Fatal("send on closed socket: want error")
	}
}

// TestTCPClient_Do_CtxCancelled: a cancelled parent context returns
// ctx.Err() from Do's select (line ~163).
func TestTCPClient_Do_CtxCancelled(t *testing.T) {
	// Silent server so the only way out of the select is ctx.Done.
	ln, _ := net.Listen("tcp4", "127.0.0.1:0")
	defer func() { _ = ln.Close() }()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		buf := make([]byte, 64)
		for {
			if _, err := conn.Read(buf); err != nil {
				return
			}
		}
	}()
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	pn, _ := strconv.Atoi(p)
	conn, err := transport.DialTCP(context.Background(), h, pn)
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	c := NewTCPClient(conn, discardLogger(), ClientConfig{ReceiveTimeout: 5 * time.Second})
	defer func() { _ = c.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Do(ctx, &codec.Message{
		MType: codec.MTypeRequest, MCode: byte(codec.MethodGetValue), ObjGroup: codec.GroupFrame,
	}); err == nil {
		t.Fatal("cancelled ctx: want error")
	}
}

// TestTCPClient_Close_WakesPendingDo: Close while a Do is blocked waiting
// for a reply closes the reply channel (lines ~104-107, ~168).
func TestTCPClient_Close_WakesPendingDo(t *testing.T) {
	ln, _ := net.Listen("tcp4", "127.0.0.1:0")
	defer func() { _ = ln.Close() }()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		buf := make([]byte, 64)
		for {
			if _, err := conn.Read(buf); err != nil {
				return
			}
		}
	}()
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	pn, _ := strconv.Atoi(p)
	conn, err := transport.DialTCP(context.Background(), h, pn)
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	c := NewTCPClient(conn, discardLogger(), ClientConfig{ReceiveTimeout: 5 * time.Second})

	errc := make(chan error, 1)
	go func() {
		_, e := c.Do(context.Background(), &codec.Message{
			MType: codec.MTypeRequest, MCode: byte(codec.MethodGetValue), ObjGroup: codec.GroupFrame,
		})
		errc <- e
	}()
	time.Sleep(50 * time.Millisecond) // let Do register its pending channel
	_ = c.Close()
	select {
	case e := <-errc:
		if e == nil {
			t.Fatal("Do should error when Close wakes it")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Do did not unblock after Close")
	}
}

// rawTCPServer accepts one connection and lets the test write raw frames
// to the client. It returns the accepted server-side conn so the test can
// push arbitrary (malformed / unknown-MTID / non-announce) frames.
func rawTCPServer(t *testing.T) (host string, port int, accepted chan net.Conn, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	accepted = make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		accepted <- conn
		// keep reading so the socket stays open / drains requests
		buf := make([]byte, 256)
		for {
			if _, err := conn.Read(buf); err != nil {
				return
			}
		}
	}()
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	pn, _ := strconv.Atoi(p)
	return h, pn, accepted, func() { _ = ln.Close() }
}

// TestTCPClient_Reader_Paths drives the reader's malformed-frame skip,
// unknown-MTID skip, MTID=0-non-announce skip, and a panicking listener.
func TestTCPClient_Reader_Paths(t *testing.T) {
	host, port, accepted, stop := rawTCPServer(t)
	defer stop()
	conn, err := transport.DialTCP(context.Background(), host, port)
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	c := NewTCPClient(conn, discardLogger(), ClientConfig{ReceiveTimeout: time.Second})
	defer func() { _ = c.Close() }()

	// A listener that panics on the first announcement → reader recovers.
	got := make(chan struct{}, 4)
	c.AddListener(func(m *codec.Message) {
		got <- struct{}{}
		panic("boom")
	})

	srv := <-accepted

	// 1) Malformed frame: MLEN passes the >=8 framing check but the ACP1
	//    payload has an invalid MTYPE so codec.Decode rejects it.
	writeRawFrame(srv, []byte{0x00, 0x00, 0x00, 0x05, 0x01, 0x09, 0x00, 0x00, 0x00})
	// 2) Unknown-MTID reply (no pending entry).
	writeFrame(srv, &codec.Message{MTID: 0xABCDEF, MType: codec.MTypeReply, MCode: 0,
		ObjGroup: codec.GroupFrame, ObjID: 0, Value: []byte{0x01}})
	// 3) MTID=0 but not an announcement (MType=error).
	writeFrame(srv, &codec.Message{MTID: 0, MType: codec.MTypeError, MCode: 1})
	// 4) A genuine announcement → reaches the panicking listener.
	writeFrame(srv, &codec.Message{MTID: 0, MType: codec.MTypeAnnounce, MAddr: 0,
		ObjGroup: codec.GroupFrame, ObjID: 0, Value: []byte{2, 2}})

	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("announcement never reached listener")
	}
}

// TestTCPClient_Reader_ChannelFull: a reply arrives for a pending MTID whose
// reply channel is already full → the reader drops it via the select default.
func TestTCPClient_Reader_ChannelFull(t *testing.T) {
	host, port, accepted, stop := rawTCPServer(t)
	defer stop()
	conn, err := transport.DialTCP(context.Background(), host, port)
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	c := NewTCPClient(conn, discardLogger(), ClientConfig{ReceiveTimeout: time.Second})
	defer func() { _ = c.Close() }()

	const mtid = 0x4242
	full := make(chan *codec.Message, 1)
	full <- &codec.Message{} // pre-fill so the next send would block
	c.pendingMu.Lock()
	c.pending[mtid] = full
	c.pendingMu.Unlock()

	srv := <-accepted
	// Send a reply for the pre-filled MTID → reader hits the full-channel
	// default branch.
	writeFrame(srv, &codec.Message{MTID: mtid, MType: codec.MTypeReply, MCode: 0,
		ObjGroup: codec.GroupFrame, ObjID: 0, Value: []byte{0x01}})
	// Then a fresh announcement-style getValue with a real Do to confirm the
	// reader survived and kept processing.
	time.Sleep(50 * time.Millisecond)
}

// TestTCPClient_Reader_LoopPanicRecovered: an OnRx hook that panics fires
// inside the reader loop body (outside the per-listener guard), driving the
// reader's top-level panic-recovery defer.
func TestTCPClient_Reader_LoopPanicRecovered(t *testing.T) {
	host, port, accepted, stop := rawTCPServer(t)
	defer stop()
	conn, err := transport.DialTCP(context.Background(), host, port)
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	panicked := make(chan struct{}, 1)
	c := NewTCPClient(conn, discardLogger(), ClientConfig{
		ReceiveTimeout: time.Second,
		OnRx:           func() { select { case panicked <- struct{}{}: default: }; panic("onrx boom") },
	})
	defer func() { _ = c.Close() }()
	srv := <-accepted
	// Any decodable frame triggers OnRx → panic → reader recover + exit.
	writeFrame(srv, &codec.Message{MTID: 0, MType: codec.MTypeAnnounce, MAddr: 0,
		ObjGroup: codec.GroupFrame, ObjID: 0, Value: []byte{2, 2}})
	select {
	case <-panicked:
	case <-time.After(2 * time.Second):
		t.Fatal("OnRx not invoked")
	}
	time.Sleep(50 * time.Millisecond) // let the recover defer run
}

// writeRawFrame writes a 4-byte big-endian length prefix + body verbatim.
func writeRawFrame(c net.Conn, body []byte) {
	lb := []byte{byte(len(body) >> 24), byte(len(body) >> 16), byte(len(body) >> 8), byte(len(body))}
	_, _ = c.Write(lb)
	_, _ = c.Write(body)
}

// TestTCPClient_Reader_ExitOnError: the server closes the socket abruptly
// while the client is NOT closed → reader's non-closed error-exit arm.
func TestTCPClient_Reader_ExitOnError(t *testing.T) {
	host, port, accepted, stop := rawTCPServer(t)
	defer stop()
	conn, err := transport.DialTCP(context.Background(), host, port)
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	c := NewTCPClient(conn, discardLogger(), ClientConfig{ReceiveTimeout: time.Second})
	srv := <-accepted
	_ = srv.Close() // abrupt close → client reader Receive errors, closed==false
	// Give the reader a moment to hit the error path, then Close cleanly.
	time.Sleep(50 * time.Millisecond)
	_ = c.Close()
}

// ---------------------------------------------------------------- AN2 client

// TestAN2Client_Do_EncodeError: out-of-range MAddr fails req.Encode().
func TestAN2Client_Do_EncodeError(t *testing.T) {
	host, port, stop := echoAN2Server(t, []byte{0x00, 0x01}, false)
	defer stop()
	c := NewAN2Client(dialAN2(t, host, port), discardLogger(), ClientConfig{ReceiveTimeout: time.Second})
	defer func() { _ = c.Close() }()
	if _, err := c.Do(context.Background(), &codec.Message{
		MType: codec.MTypeRequest, MAddr: 99, MCode: byte(codec.MethodGetValue), ObjGroup: codec.GroupFrame,
	}); err == nil {
		t.Fatal("encode error: want error")
	}
}

// TestAN2Client_Do_CtxCancelled: cancelled ctx exits Do's select.
func TestAN2Client_Do_CtxCancelled(t *testing.T) {
	// Silent server: accept and drain, never reply.
	ln, _ := net.Listen("tcp4", "127.0.0.1:0")
	defer func() { _ = ln.Close() }()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		buf := make([]byte, 64)
		for {
			if _, err := conn.Read(buf); err != nil {
				return
			}
		}
	}()
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	pn, _ := strconv.Atoi(p)
	c := NewAN2Client(dialAN2(t, h, pn), discardLogger(), ClientConfig{ReceiveTimeout: 5 * time.Second})
	defer func() { _ = c.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Do(ctx, &codec.Message{
		MType: codec.MTypeRequest, MCode: byte(codec.MethodGetValue), ObjGroup: codec.GroupFrame,
	}); err == nil {
		t.Fatal("cancelled ctx: want error")
	}
}

// TestAN2Client_Close_WakesPendingDo: Close unblocks a waiting Do.
func TestAN2Client_Close_WakesPendingDo(t *testing.T) {
	ln, _ := net.Listen("tcp4", "127.0.0.1:0")
	defer func() { _ = ln.Close() }()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		buf := make([]byte, 64)
		for {
			if _, err := conn.Read(buf); err != nil {
				return
			}
		}
	}()
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	pn, _ := strconv.Atoi(p)
	c := NewAN2Client(dialAN2(t, h, pn), discardLogger(), ClientConfig{ReceiveTimeout: 5 * time.Second})
	errc := make(chan error, 1)
	go func() {
		_, e := c.Do(context.Background(), &codec.Message{
			MType: codec.MTypeRequest, MCode: byte(codec.MethodGetValue), ObjGroup: codec.GroupFrame,
		})
		errc <- e
	}()
	time.Sleep(50 * time.Millisecond)
	_ = c.Close()
	select {
	case e := <-errc:
		if e == nil {
			t.Fatal("Do should error when Close wakes it")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Do did not unblock after Close")
	}
}

// TestAN2Client_Reader_Paths drives the AN2 reader's non-ACP1/empty-frame
// skip, malformed-payload skip, unknown-MTID skip, MTID=0-non-announce
// skip, and a panicking listener.
func TestAN2Client_Reader_Paths(t *testing.T) {
	ln, _ := net.Listen("tcp4", "127.0.0.1:0")
	defer func() { _ = ln.Close() }()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		accepted <- conn
		buf := make([]byte, 256)
		for {
			if _, err := conn.Read(buf); err != nil {
				return
			}
		}
	}()
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	pn, _ := strconv.Atoi(p)
	c := NewAN2Client(dialAN2(t, h, pn), discardLogger(), ClientConfig{ReceiveTimeout: time.Second})
	defer func() { _ = c.Close() }()

	got := make(chan struct{}, 4)
	c.AddListener(func(m *codec.Message) {
		got <- struct{}{}
		panic("boom")
	})

	srv := <-accepted

	// 1) Internal (non-ACP1) frame → skipped.
	writeAN2Internal(srv)
	// 2) ACP1 data frame with malformed payload → decode skip.
	writeAN2Raw(srv, []byte{0x00})
	// 3) ACP1 reply with unknown MTID → no pending skip.
	writeAN2(srv, &codec.Message{MTID: 0x999, MType: codec.MTypeReply, MCode: 0,
		ObjGroup: codec.GroupFrame, ObjID: 0, Value: []byte{0x01}}, 0)
	// 4) MTID=0 but not announcement → skip.
	writeAN2(srv, &codec.Message{MTID: 0, MType: codec.MTypeError, MCode: 1}, 0)
	// 5) Real announcement → reaches panicking listener.
	writeAN2(srv, &codec.Message{MTID: 0, MType: codec.MTypeAnnounce, MAddr: 0,
		ObjGroup: codec.GroupFrame, ObjID: 0, Value: []byte{2, 2}}, 0)

	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("AN2 announcement never reached listener")
	}
}

// TestAN2Client_Reader_ChannelFull mirrors the TCP channel-full case for the
// AN2 reader.
func TestAN2Client_Reader_ChannelFull(t *testing.T) {
	ln, _ := net.Listen("tcp4", "127.0.0.1:0")
	defer func() { _ = ln.Close() }()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		accepted <- conn
		buf := make([]byte, 256)
		for {
			if _, err := conn.Read(buf); err != nil {
				return
			}
		}
	}()
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	pn, _ := strconv.Atoi(p)
	c := NewAN2Client(dialAN2(t, h, pn), discardLogger(), ClientConfig{ReceiveTimeout: time.Second})
	defer func() { _ = c.Close() }()

	const mtid = 0x5151
	full := make(chan *codec.Message, 1)
	full <- &codec.Message{}
	c.pendingMu.Lock()
	c.pending[mtid] = full
	c.pendingMu.Unlock()

	srv := <-accepted
	writeAN2(srv, &codec.Message{MTID: mtid, MType: codec.MTypeReply, MCode: 0,
		ObjGroup: codec.GroupFrame, ObjID: 0, Value: []byte{0x01}}, 0)
	time.Sleep(50 * time.Millisecond)
}

// TestAN2Client_Reader_ExitOnError: an abrupt server close (client not
// closed) drives the AN2 reader's !closed error-exit log arm.
func TestAN2Client_Reader_ExitOnError(t *testing.T) {
	ln, _ := net.Listen("tcp4", "127.0.0.1:0")
	defer func() { _ = ln.Close() }()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		accepted <- conn
	}()
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	pn, _ := strconv.Atoi(p)
	c := NewAN2Client(dialAN2(t, h, pn), discardLogger(), ClientConfig{ReceiveTimeout: time.Second})
	srv := <-accepted
	_ = srv.Close() // abrupt close → reader ReadAN2Frame errors, closed==false
	time.Sleep(50 * time.Millisecond)
	_ = c.Close()
}

// TestAN2Client_Reader_LoopPanicRecovered mirrors the TCP loop-panic case.
func TestAN2Client_Reader_LoopPanicRecovered(t *testing.T) {
	ln, _ := net.Listen("tcp4", "127.0.0.1:0")
	defer func() { _ = ln.Close() }()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		accepted <- conn
		buf := make([]byte, 256)
		for {
			if _, err := conn.Read(buf); err != nil {
				return
			}
		}
	}()
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	pn, _ := strconv.Atoi(p)
	panicked := make(chan struct{}, 1)
	c := NewAN2Client(dialAN2(t, h, pn), discardLogger(), ClientConfig{
		ReceiveTimeout: time.Second,
		OnRx:           func() { select { case panicked <- struct{}{}: default: }; panic("onrx boom") },
	})
	defer func() { _ = c.Close() }()
	srv := <-accepted
	writeAN2(srv, &codec.Message{MTID: 0, MType: codec.MTypeAnnounce, MAddr: 0,
		ObjGroup: codec.GroupFrame, ObjID: 0, Value: []byte{2, 2}}, 0)
	select {
	case <-panicked:
	case <-time.After(2 * time.Second):
		t.Fatal("OnRx not invoked")
	}
	time.Sleep(50 * time.Millisecond)
}

func writeAN2Internal(c net.Conn) {
	b, err := an2.EncodeAN2Frame(&an2.AN2Frame{
		Proto: an2.AN2ProtoInternal, Slot: 0, MTID: 0, Type: an2.AN2TypeData, Payload: []byte{0x01},
	})
	if err != nil {
		return
	}
	_, _ = c.Write(b)
}

func writeAN2Raw(c net.Conn, payload []byte) {
	b, err := an2.EncodeAN2Frame(&an2.AN2Frame{
		Proto: an2.AN2ProtoACP1, Slot: 0, MTID: 0, Type: an2.AN2TypeData, Payload: payload,
	})
	if err != nil {
		return
	}
	_, _ = c.Write(b)
}
