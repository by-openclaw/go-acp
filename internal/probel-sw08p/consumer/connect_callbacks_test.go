package probelsw08p

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"dhs/internal/probel-sw08p/codec"
)

// dialPlugin connects a Plugin to a TCP listener and returns it with a
// cleanup. logger output is discarded.
func dialPlugin(t *testing.T, port int) *Plugin {
	t.Helper()
	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := p.Connect(ctx, "127.0.0.1", port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	return p
}

// TestConnectPortZeroDefault covers the `if port == 0 { port = DefaultPort }`
// branch by binding a listener on DefaultPort and connecting with port 0.
func TestConnectPortZeroDefault(t *testing.T) {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", itoa(codec.DefaultPort)))
	if err != nil {
		t.Skipf("DefaultPort %d not bindable on this host: %v", codec.DefaultPort, err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		go func(c net.Conn) { _, _ = io.Copy(io.Discard, c) }(c)
	}()

	p := dialPlugin(t, 0) // port 0 → DefaultPort
	defer func() { _ = p.Disconnect() }()
	if p.port != codec.DefaultPort {
		t.Errorf("resolved port = %d; want DefaultPort %d", p.port, codec.DefaultPort)
	}
}

// TestConnectOnTimeout covers the OnTimeout compliance callback: a peer
// that accepts but never ACKs forces the Send retry loop to time out.
func TestConnectOnTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		// Drain but never ACK.
		go func(c net.Conn) { _, _ = io.Copy(io.Discard, c) }(c)
	}()
	port := portOf(t, ln)

	p := dialPlugin(t, port)
	defer func() { _ = p.Disconnect() }()

	// Bound the call so we don't wait the full 5×1s retry budget; the
	// first attempt's 1s ACK timer fires OnTimeout before ctx expires.
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	_ = p.Maintenance(ctx, codec.MaintSoftReset, 0, 0)

	prof := p.ComplianceProfile()
	if prof == nil {
		t.Fatal("nil compliance profile")
	}
	if prof.Snapshot()[ACKTimeoutElapsed] == 0 {
		t.Error("ACKTimeoutElapsed not recorded; OnTimeout callback never fired")
	}
}

// TestConnectOnNoACK covers the OnNoACK callback: a peer that sends the
// application reply BEFORE the §2 DLE ACK triggers the reply-before-ack
// deviation path.
func TestConnectOnNoACK(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		defer func() { _ = c.Close() }()
		buf := make([]byte, 256)
		acc := make([]byte, 0, 256)
		for {
			n, rerr := c.Read(buf)
			if rerr != nil {
				return
			}
			acc = append(acc, buf[:n]...)
			for len(acc) >= 2 {
				if codec.IsACK(acc) || codec.IsNAK(acc) {
					acc = acc[2:]
					continue
				}
				if acc[0] != codec.DLE {
					acc = acc[1:]
					continue
				}
				f, consumed, perr := codec.Unpack(acc)
				if errors.Is(perr, io.ErrUnexpectedEOF) {
					break
				}
				if perr != nil {
					acc = acc[2:]
					continue
				}
				acc = acc[consumed:]
				if f.ID == codec.RxCrosspointInterrogate {
					// Reply FIRST, then ACK — the reply-before-ack deviation.
					reply := codec.EncodeCrosspointTally(codec.CrosspointTallyParams{
						DestinationID: 1, SourceID: 2,
					})
					_, _ = c.Write(codec.Pack(reply))
					_, _ = c.Write(codec.PackACK())
				}
			}
		}
	}()
	port := portOf(t, ln)

	p := dialPlugin(t, port)
	defer func() { _ = p.Disconnect() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := p.CrosspointInterrogate(ctx, 0, 0, 1); err != nil {
		t.Fatalf("CrosspointInterrogate: %v", err)
	}
	if p.ComplianceProfile().Snapshot()[ReplyWithoutACK] == 0 {
		t.Error("ReplyWithoutACK not recorded; OnNoACK callback never fired")
	}
}

// TestConnectOnCapSoft covers the OnCapSoft callback: an outbound frame
// whose DATA field exceeds the 128-byte soft cap fires the oversize
// compliance event.
func TestConnectOnCapSoft(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		defer func() { _ = c.Close() }()
		buf := make([]byte, 1024)
		for {
			n, rerr := c.Read(buf)
			if rerr != nil {
				return
			}
			// ACK anything frame-shaped so the Send completes.
			if n >= 2 && buf[0] == codec.DLE {
				_, _ = c.Write(codec.PackACK())
			}
		}
	}()
	port := portOf(t, ln)

	p := dialPlugin(t, port)
	defer func() { _ = p.Disconnect() }()

	cli, err := p.ExposeClient()
	if err != nil {
		t.Fatalf("ExposeClient: %v", err)
	}
	// 200-byte payload → DATA field (ID + payload) = 201 > DataCapSoft(128).
	big := codec.Frame{ID: codec.RxMaintenance, Payload: make([]byte, 200)}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := cli.Send(ctx, big, nil); err != nil {
		t.Fatalf("Send big frame: %v", err)
	}
	if p.ComplianceProfile().Snapshot()[DataFieldOversize] == 0 {
		t.Error("DataFieldOversize not recorded; OnCapSoft callback never fired")
	}
}

// itoa is a tiny int→string without importing strconv at call sites.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
