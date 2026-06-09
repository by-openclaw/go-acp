package acp1

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
	"dhs/internal/transport"
)

// echoTCPServer accepts one or more MLEN-framed ACP1 requests and answers each
// with a reply (same MTID) carrying replyValue, then pushes an unsolicited
// announcement (MTID=0) so the client's listener fan-out path runs too.
func echoTCPServer(t *testing.T, replyValue []byte, announce bool) (host string, port int, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				for {
					var lb [4]byte
					if _, err := io.ReadFull(c, lb[:]); err != nil {
						return
					}
					n := binary.BigEndian.Uint32(lb[:])
					buf := make([]byte, n)
					if _, err := io.ReadFull(c, buf); err != nil {
						return
					}
					req, err := codec.Decode(buf)
					if err != nil {
						return
					}
					rep := &codec.Message{
						MTID: req.MTID, MType: codec.MTypeReply, MAddr: req.MAddr,
						MCode: req.MCode, ObjGroup: req.ObjGroup, ObjID: req.ObjID, Value: replyValue,
					}
					writeFrame(c, rep)
					if announce {
						writeFrame(c, &codec.Message{
							MTID: 0, MType: codec.MTypeAnnounce, MAddr: req.MAddr,
							MCode: 0, ObjGroup: codec.GroupFrame, ObjID: 0, Value: []byte{2, 2},
						})
					}
				}
			}(conn)
		}
	}()
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	pn, _ := strconv.Atoi(p)
	return h, pn, func() { _ = ln.Close(); <-done }
}

func writeFrame(c net.Conn, m *codec.Message) {
	out, err := m.Encode()
	if err != nil {
		return
	}
	var lb [4]byte
	binary.BigEndian.PutUint32(lb[:], uint32(len(out)))
	_, _ = c.Write(lb[:])
	_, _ = c.Write(out)
}

func TestTCPClient_Loopback_DoAndAnnounce(t *testing.T) {
	host, port, stop := echoTCPServer(t, []byte{0x00, 0x05}, true)
	defer stop()

	ctx := context.Background()
	conn, err := transport.DialTCP(ctx, host, port)
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	c := NewTCPClient(conn, nil, ClientConfig{ReceiveTimeout: 2 * time.Second})
	defer func() { _ = c.Close() }()

	// Listener fan-out: capture the unsolicited announcement.
	annCh := make(chan *codec.Message, 4)
	h := c.AddListener(func(m *codec.Message) { annCh <- m })
	if h < 0 {
		t.Fatal("AddListener returned bad handle")
	}

	rep, err := c.Do(ctx, &codec.Message{
		MType: codec.MTypeRequest, MCode: byte(codec.MethodGetValue),
		ObjGroup: codec.GroupControl, ObjID: 5,
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if rep.MType != codec.MTypeReply || len(rep.Value) != 2 || rep.Value[1] != 5 {
		t.Fatalf("reply = %+v", rep)
	}
	// The MTID the client allocated must round-trip on the reply.
	if rep.MTID == 0 {
		t.Error("reply MTID is zero, want the client-allocated id")
	}

	select {
	case <-annCh:
	case <-time.After(2 * time.Second):
		t.Error("announcement not delivered to listener")
	}

	// A second Do must use a fresh, monotonically-greater MTID.
	rep2, err := c.Do(ctx, &codec.Message{
		MType: codec.MTypeRequest, MCode: byte(codec.MethodGetValue),
		ObjGroup: codec.GroupControl, ObjID: 5,
	})
	if err != nil {
		t.Fatalf("Do2: %v", err)
	}
	if rep2.MTID == rep.MTID {
		t.Errorf("MTID not advanced: %d == %d", rep2.MTID, rep.MTID)
	}

	// RemoveListener (valid + out-of-range no-ops).
	c.RemoveListener(h)
	c.RemoveListener(99)
}

func TestTCPClient_DoErrors(t *testing.T) {
	host, port, stop := echoTCPServer(t, []byte{0x00, 0x01}, false)
	defer stop()
	conn, err := transport.DialTCP(context.Background(), host, port)
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	c := NewTCPClient(conn, nil, ClientConfig{ReceiveTimeout: time.Second})

	// nil request.
	if _, err := c.Do(context.Background(), nil); err == nil {
		t.Error("Do(nil): want error")
	}

	// Close, then Do must fail; Close is idempotent.
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if _, err := c.Do(context.Background(), &codec.Message{MType: codec.MTypeRequest}); err == nil {
		t.Error("Do on closed client: want error")
	}
}

func TestTCPClient_DoTimeout(t *testing.T) {
	// Silent server: reads the request but never replies → Do times out.
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		// Drain and discard; never reply.
		buf := make([]byte, 256)
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
	c := NewTCPClient(conn, nil, ClientConfig{ReceiveTimeout: 100 * time.Millisecond})
	defer func() { _ = c.Close() }()

	if _, err := c.Do(context.Background(), &codec.Message{
		MType: codec.MTypeRequest, MCode: byte(codec.MethodGetValue), ObjGroup: codec.GroupControl, ObjID: 5,
	}); err == nil {
		t.Error("Do against silent server: want timeout error")
	}
}

func TestTCPClient_allocMTID_WrapSkipsZero(t *testing.T) {
	c := &TCPClient{nextMTID: 0xFFFFFFFF}
	if got := c.allocMTID(); got != 1 {
		t.Errorf("wrap = %d, want 1", got)
	}
}
