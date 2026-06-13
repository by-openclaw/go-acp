package acp2

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"dhs/internal/acp2/codec"
)

// dialRaw opens a raw TCP connection for the session-edge tests.
func dialRaw(ctx context.Context, host string, port int) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
}

// coverage_session_edges_test.go drives the directly-callable Session error
// arms: DoACP2 allocMTID-cancel + EncodeACP2Message error, sendFrame encode
// error, handleACP2Frame decode error, and an2Request ctx.Done.

// TestDoACP2_AllocMTIDCanceled covers DoACP2's allocMTID error arm: a
// pre-cancelled context makes allocMTID return ctx.Err before any send.
func TestDoACP2_AllocMTIDCanceled(t *testing.T) {
	s := NewSession(unitLogger())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.DoACP2(ctx, 0, &codec.ACP2Message{
		Type: codec.ACP2TypeRequest, Func: codec.ACP2FuncGetObject, ObjID: 1})
	if err == nil {
		t.Fatal("DoACP2 with cancelled ctx should fail at allocMTID")
	}
}

// TestDoACP2_EncodeError covers DoACP2's EncodeACP2Message error arm: a
// set_property request with no properties makes the codec reject the encode.
func TestDoACP2_EncodeError(t *testing.T) {
	s := NewSession(unitLogger())
	_, err := s.DoACP2(context.Background(), 0, &codec.ACP2Message{
		Type: codec.ACP2TypeRequest, Func: codec.ACP2FuncSetProperty,
		ObjID: 1, Properties: nil}) // set with no properties → encode error
	if err == nil {
		t.Fatal("DoACP2 set_property with no properties should fail at encode")
	}
}

// TestSendFrame_EncodeError covers sendFrame's EncodeAN2Frame error arm via
// an oversize payload the AN2 encoder rejects.
func TestSendFrame_EncodeError(t *testing.T) {
	s := NewSession(unitLogger())
	big := make([]byte, 0x1_0001) // > AN2 MaxPayload
	err := s.sendFrame(context.Background(), &codec.AN2Frame{
		Proto: codec.AN2ProtoACP2, Slot: 1, Type: codec.AN2TypeData, Payload: big})
	if err == nil {
		t.Fatal("sendFrame with oversize payload should surface an encode error")
	}
}

// TestHandleACP2Frame_DecodeError covers handleACP2Frame's
// DecodeACP2Message error arm. A 4-byte payload passes the length guard but
// a crafted body... DecodeACP2Message only fails on <4 bytes, so to reach
// the decode-error arm we feed a payload that is >=4 bytes yet still makes
// the decoder error: a reply/announce body that DecodeProperties rejects.
func TestHandleACP2Frame_DecodeError(t *testing.T) {
	s := NewSession(unitLogger())
	// type=reply, mtid=1, func=get_object, then a truncated obj-id/idx +
	// a malformed property header so the reply-body parse path errors.
	// DecodeACP2Message parses properties for reply funcs 1-3; a property
	// with plen larger than the remaining bytes triggers a decode error.
	payload := []byte{
		byte(codec.ACP2TypeReply), 1, byte(codec.ACP2FuncGetObject), 0,
		0, 0, 0, 1, // obj-id
		0, 0, 0, 0, // idx
		0x01, 0x00, 0xFF, 0xFF, // property header: plen=0xFFFF (overruns) → decode error
	}
	s.handleACP2Frame(&codec.AN2Frame{
		Proto: codec.AN2ProtoACP2, Type: codec.AN2TypeData, Slot: 1, Payload: payload})
	// No assertion: the test asserts only that the decode-error arm runs
	// without panic (the message is dropped).
}

// TestAN2Request_CtxDone covers an2Request's ctx.Done arm: the frame is sent
// on a live (loopback) connection, but the context is cancelled before any
// reply arrives, so the select returns ctx.Err().
func TestAN2Request_CtxDone(t *testing.T) {
	// A raw listener that accepts but never replies to AN2 internal
	// requests, so an2Request blocks until the ctx deadline fires.
	host, port, stop := rawListener(t, func(c net.Conn) {
		defer func() { _ = c.Close() }()
		// Read + drop everything; never reply.
		buf := make([]byte, 256)
		_ = c.SetDeadline(time.Now().Add(time.Second))
		for {
			if _, err := c.Read(buf); err != nil {
				return
			}
		}
	})
	defer stop()

	s := NewSession(unitLogger())
	// Connect would hang in the handshake (no replies); dial the socket
	// directly and start the read loop, then call an2Request with a short
	// ctx so its ctx.Done arm fires deterministically.
	dctx, dcancel := context.WithTimeout(context.Background(), time.Second)
	defer dcancel()
	conn, err := dialRaw(dctx, host, port)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	s.conn = conn
	s.done = make(chan struct{})
	go s.readLoop(conn)
	defer func() { _ = s.Disconnect() }()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := s.an2Request(ctx, codec.AN2FuncGetVersion, 0, nil); err == nil {
		t.Fatal("an2Request with a non-answering peer should hit ctx.Done")
	}
}
