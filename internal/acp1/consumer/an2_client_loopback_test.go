package acp1

import (
	"context"
	"log/slog"
	"net"
	"strconv"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
	an2 "dhs/internal/acp2/codec"
)

// echoAN2Server accepts AN2-framed connections, answers each ACP1 data frame
// with a reply (same ACP1 MTID) carrying replyValue, optionally pushes an
// unsolicited ACP1 announcement, and silently consumes the AN2 internal
// EnableProtocolEvents control frame the client sends at startup.
func echoAN2Server(t *testing.T, replyValue []byte, announce bool) (host string, port int, stop func()) {
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
					f, err := an2.ReadAN2Frame(c)
					if err != nil {
						return
					}
					if f.Proto != an2.AN2ProtoACP1 || f.Type != an2.AN2TypeData {
						continue // internal control frames (EnableProtocolEvents): ignore
					}
					req, err := codec.Decode(f.Payload)
					if err != nil {
						return
					}
					rep := &codec.Message{
						MTID: req.MTID, MType: codec.MTypeReply, MAddr: req.MAddr,
						MCode: req.MCode, ObjGroup: req.ObjGroup, ObjID: req.ObjID, Value: replyValue,
					}
					writeAN2(c, rep, 0)
					if announce {
						ann := &codec.Message{
							MTID: 0, MType: codec.MTypeAnnounce, MAddr: req.MAddr,
							MCode: 0, ObjGroup: codec.GroupFrame, ObjID: 0, Value: []byte{2, 2},
						}
						writeAN2(c, ann, 0)
					}
				}
			}(conn)
		}
	}()
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	pn, _ := strconv.Atoi(p)
	return h, pn, func() { _ = ln.Close(); <-done }
}

func writeAN2(c net.Conn, m *codec.Message, slot byte) {
	body, err := m.Encode()
	if err != nil {
		return
	}
	b, err := an2.EncodeAN2Frame(&an2.AN2Frame{
		Proto: an2.AN2ProtoACP1, Slot: slot, MTID: 0, Type: an2.AN2TypeData, Payload: body,
	})
	if err != nil {
		return
	}
	_, _ = c.Write(b)
}

func dialAN2(t *testing.T, host string, port int) *net.TCPConn {
	t.Helper()
	conn, err := net.DialTimeout("tcp4", net.JoinHostPort(host, strconv.Itoa(port)), 2*time.Second)
	if err != nil {
		t.Fatalf("dial an2: %v", err)
	}
	return conn.(*net.TCPConn)
}

func TestAN2Client_Loopback_DoAndAnnounce(t *testing.T) {
	host, port, stop := echoAN2Server(t, []byte{0x00, 0x05}, true)
	defer stop()

	c := NewAN2Client(dialAN2(t, host, port), nil, ClientConfig{ReceiveTimeout: 2 * time.Second})
	defer func() { _ = c.Close() }()

	annCh := make(chan *codec.Message, 4)
	c.AddListener(func(m *codec.Message) { annCh <- m })

	rep, err := c.Do(context.Background(), &codec.Message{
		MType: codec.MTypeRequest, MCode: byte(codec.MethodGetValue), ObjGroup: codec.GroupControl, ObjID: 5,
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if rep.MType != codec.MTypeReply || len(rep.Value) != 2 || rep.Value[1] != 5 {
		t.Fatalf("reply = %+v", rep)
	}
	if rep.MTID == 0 {
		t.Error("reply MTID is zero")
	}
	select {
	case <-annCh:
	case <-time.After(2 * time.Second):
		t.Error("announcement not delivered to listener")
	}

	rep2, err := c.Do(context.Background(), &codec.Message{
		MType: codec.MTypeRequest, MCode: byte(codec.MethodGetValue), ObjGroup: codec.GroupControl, ObjID: 5,
	})
	if err != nil {
		t.Fatalf("Do2: %v", err)
	}
	if rep2.MTID == rep.MTID {
		t.Errorf("MTID not advanced: %d == %d", rep2.MTID, rep.MTID)
	}
}

func TestAN2Client_DoErrors(t *testing.T) {
	host, port, stop := echoAN2Server(t, []byte{0x00, 0x01}, false)
	defer stop()
	c := NewAN2Client(dialAN2(t, host, port), nil, ClientConfig{ReceiveTimeout: time.Second})

	if _, err := c.Do(context.Background(), nil); err == nil {
		t.Error("Do(nil): want error")
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if _, err := c.Do(context.Background(), &codec.Message{MType: codec.MTypeRequest}); err == nil {
		t.Error("Do on closed client: want error")
	}
	c.RemoveListener(0)  // no-op on empty
	c.RemoveListener(-1) // out-of-range no-op
}

func TestAN2Client_allocMTID_WrapSkipsZero(t *testing.T) {
	c := &AN2Client{nextMTID: 0xFFFFFFFF}
	if got := c.allocMTID(); got != 1 {
		t.Errorf("wrap = %d, want 1", got)
	}
}

// TestConnect_AN2 drives the plugin's Mode C connect path end-to-end.
func TestConnect_AN2(t *testing.T) {
	host, port, stop := echoAN2Server(t, []byte{0x03, 0x02, 0x02, 0x00}, false)
	defer stop()

	p := &Plugin{logger: slog.Default()}
	p.SetTransport(TransportAN2)
	if err := p.Connect(context.Background(), host, port); err != nil {
		t.Fatalf("Connect AN2: %v", err)
	}
	t.Cleanup(func() { _ = p.Disconnect() })
	if p.Transport() != TransportAN2 {
		t.Errorf("Transport = %v, want an2", p.Transport())
	}
	di, err := p.GetDeviceInfo(context.Background())
	if err != nil {
		t.Fatalf("GetDeviceInfo over AN2: %v", err)
	}
	if di.NumSlots != 3 {
		t.Errorf("NumSlots = %d, want 3", di.NumSlots)
	}
}
