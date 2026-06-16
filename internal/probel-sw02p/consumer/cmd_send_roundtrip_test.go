package probelsw02p

import (
	"context"
	"net"
	"testing"
	"time"

	"dhs/internal/probel-sw02p/codec"
)

// newPipePlugin returns a Plugin wired to one end of a net.Pipe plus
// the peer (matrix) end. Cleanup closes both.
func newPipePlugin(t *testing.T) (*Plugin, net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	logger := quietLogger()
	disable := false
	client := codec.NewClientFromConn(a, logger, codec.ClientConfig{WireHexLog: &disable})
	p := &Plugin{logger: logger}
	p.client = client
	t.Cleanup(func() {
		_ = client.Close()
		_ = b.Close()
	})
	return p, b
}

// readFrame accumulates bytes from peer until one whole SW-P-02 frame
// Unpacks, returning it. Fails the test on read error.
func readFrame(t *testing.T, peer net.Conn) codec.Frame {
	t.Helper()
	buf := make([]byte, 0, 64)
	tmp := make([]byte, 64)
	for {
		_ = peer.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, err := peer.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			if f, _, perr := codec.Unpack(buf); perr == nil {
				return f
			}
		}
		if err != nil {
			t.Fatalf("readFrame: %v", err)
		}
	}
}

// writeFrame packs and writes a reply frame onto the peer.
func writeFrame(t *testing.T, peer net.Conn, f codec.Frame) {
	t.Helper()
	if _, err := peer.Write(codec.Pack(f)); err != nil {
		t.Errorf("writeFrame: %v", err)
	}
}

// TestSendGoRoundTrip drives SendGo: the matrix reads rx 06 and replies
// tx 13 echoing the operation.
func TestSendGoRoundTrip(t *testing.T) {
	p, peer := newPipePlugin(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		req := readFrame(t, peer)
		if req.ID != codec.RxGo {
			t.Errorf("matrix got cmd %#x; want RxGo", req.ID)
			return
		}
		writeFrame(t, peer, codec.EncodeGoDoneAck(codec.GoDoneAckParams{Operation: codec.GoOpSet}))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ack, err := p.SendGo(ctx, codec.GoOpSet)
	if err != nil {
		t.Fatalf("SendGo: %v", err)
	}
	if ack.Operation != codec.GoOpSet {
		t.Errorf("ack.Operation = %v; want GoOpSet", ack.Operation)
	}
	<-done
}

// TestSendConnectOnGoGroupSalvoRoundTrip drives rx 35 → tx 37.
func TestSendConnectOnGoGroupSalvoRoundTrip(t *testing.T) {
	p, peer := newPipePlugin(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		req := readFrame(t, peer)
		if req.ID != codec.RxConnectOnGoGroupSalvo {
			t.Errorf("matrix got cmd %#x; want RxConnectOnGoGroupSalvo", req.ID)
			return
		}
		cp, _ := codec.DecodeConnectOnGoGroupSalvo(req)
		writeFrame(t, peer, codec.EncodeConnectOnGoGroupSalvoAck(codec.ConnectOnGoGroupSalvoAckParams{
			Destination: cp.Destination, Source: cp.Source, SalvoID: cp.SalvoID,
		}))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ack, err := p.SendConnectOnGoGroupSalvo(ctx, 200, 300, 5, false)
	if err != nil {
		t.Fatalf("SendConnectOnGoGroupSalvo: %v", err)
	}
	if ack.Destination != 200 || ack.Source != 300 || ack.SalvoID != 5 {
		t.Errorf("ack = %+v; want dst=200 src=300 salvo=5", ack)
	}
	<-done
}

// TestSendGoGroupSalvoRoundTrip drives rx 36 → tx 38 with a matching
// SalvoID. Also covers the matcher's SalvoID filter accepting the
// right group.
func TestSendGoGroupSalvoRoundTrip(t *testing.T) {
	p, peer := newPipePlugin(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		req := readFrame(t, peer)
		if req.ID != codec.RxGoGroupSalvo {
			t.Errorf("matrix got cmd %#x; want RxGoGroupSalvo", req.ID)
			return
		}
		g, _ := codec.DecodeGoGroupSalvo(req)
		writeFrame(t, peer, codec.EncodeGoDoneGroupSalvoAck(codec.GoDoneGroupSalvoAckParams{
			Result: codec.GoGroupResultSet, SalvoID: g.SalvoID,
		}))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ack, err := p.SendGoGroupSalvo(ctx, codec.GoOpSet, 9)
	if err != nil {
		t.Fatalf("SendGoGroupSalvo: %v", err)
	}
	if ack.Result != codec.GoGroupResultSet || ack.SalvoID != 9 {
		t.Errorf("ack = %+v; want result=set salvo=9", ack)
	}
	<-done
}

// TestSendExtendedConnectOnGoGroupSalvoRoundTrip drives rx 71 → tx 72.
func TestSendExtendedConnectOnGoGroupSalvoRoundTrip(t *testing.T) {
	p, peer := newPipePlugin(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		req := readFrame(t, peer)
		if req.ID != codec.RxExtendedConnectOnGoGroupSalvo {
			t.Errorf("matrix got cmd %#x; want RxExtendedConnectOnGoGroupSalvo", req.ID)
			return
		}
		cp, _ := codec.DecodeExtendedConnectOnGoGroupSalvo(req)
		writeFrame(t, peer, codec.EncodeExtendedConnectOnGoGroupSalvoAck(codec.ExtendedConnectOnGoGroupSalvoAckParams(cp)))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ack, err := p.SendExtendedConnectOnGoGroupSalvo(ctx, 5000, 6000, 11)
	if err != nil {
		t.Fatalf("SendExtendedConnectOnGoGroupSalvo: %v", err)
	}
	if ack.Destination != 5000 || ack.Source != 6000 || ack.SalvoID != 11 {
		t.Errorf("ack = %+v; want dst=5000 src=6000 salvo=11", ack)
	}
	<-done
}

// TestSendExtendedProtectInterrogateRoundTrip drives rx 101 → tx 96.
func TestSendExtendedProtectInterrogateRoundTrip(t *testing.T) {
	p, peer := newPipePlugin(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		req := readFrame(t, peer)
		if req.ID != codec.RxExtendedProtectInterrogate {
			t.Errorf("matrix got cmd %#x; want RxExtendedProtectInterrogate", req.ID)
			return
		}
		q, _ := codec.DecodeExtendedProtectInterrogate(req)
		writeFrame(t, peer, codec.EncodeExtendedProtectTally(codec.ExtendedProtectTallyParams{
			Destination: q.Destination, Device: 77, Protect: codec.ProtectProBel,
		}))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tally, err := p.SendExtendedProtectInterrogate(ctx, 1234)
	if err != nil {
		t.Fatalf("SendExtendedProtectInterrogate: %v", err)
	}
	if tally.Destination != 1234 || tally.Device != 77 || tally.Protect != codec.ProtectProBel {
		t.Errorf("tally = %+v; want dst=1234 dev=77 protect=ProBel", tally)
	}
	<-done
}

// TestSendExtendedProtectConnectRoundTrip drives rx 102 → tx 97.
func TestSendExtendedProtectConnectRoundTrip(t *testing.T) {
	p, peer := newPipePlugin(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		req := readFrame(t, peer)
		if req.ID != codec.RxExtendedProtectConnect {
			t.Errorf("matrix got cmd %#x; want RxExtendedProtectConnect", req.ID)
			return
		}
		c, _ := codec.DecodeExtendedProtectConnect(req)
		writeFrame(t, peer, codec.EncodeExtendedProtectConnected(codec.ExtendedProtectConnectedParams{
			Destination: c.Destination, Device: c.Device, Protect: codec.ProtectProBel,
		}))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := p.SendExtendedProtectConnect(ctx, 800, 12)
	if err != nil {
		t.Fatalf("SendExtendedProtectConnect: %v", err)
	}
	if resp.Destination != 800 || resp.Device != 12 || resp.Protect != codec.ProtectProBel {
		t.Errorf("resp = %+v; want dst=800 dev=12 protect=ProBel", resp)
	}
	<-done
}

// TestSendExtendedProtectDisconnectRoundTrip drives rx 104 → tx 98.
func TestSendExtendedProtectDisconnectRoundTrip(t *testing.T) {
	p, peer := newPipePlugin(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		req := readFrame(t, peer)
		if req.ID != codec.RxExtendedProtectDisconnect {
			t.Errorf("matrix got cmd %#x; want RxExtendedProtectDisconnect", req.ID)
			return
		}
		d, _ := codec.DecodeExtendedProtectDisconnect(req)
		writeFrame(t, peer, codec.EncodeExtendedProtectDisconnected(codec.ExtendedProtectDisconnectedParams{
			Destination: d.Destination, Device: d.Device, Protect: codec.ProtectNone,
		}))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := p.SendExtendedProtectDisconnect(ctx, 800, 12)
	if err != nil {
		t.Fatalf("SendExtendedProtectDisconnect: %v", err)
	}
	if resp.Destination != 800 || resp.Protect != codec.ProtectNone {
		t.Errorf("resp = %+v; want dst=800 protect=None", resp)
	}
	<-done
}
