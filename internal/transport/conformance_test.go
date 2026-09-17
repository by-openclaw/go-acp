package transport

// The conformance battery, run against this package's two oldest transports.
//
// Before this, tcp.go sat at 23% and udp.go at 11% of their own statements —
// the least specified code in the lib, and the most used. They were exercised
// through acp1 and acp2, which is not the same thing: those tests say what
// those connectors need, not what the transport promises.

import (
	"context"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"dhs/internal/transport/conformance"
)

// splitAddr turns "host:port" back into the (host, port) pair our Dial
// helpers take. They accept a port as an int rather than an address string,
// which is the one place the two transports differ from the harness.
func splitAddr(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split %q: %v", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("port %q: %v", portStr, err)
	}
	return host, port
}

// --- TCP ------------------------------------------------------------------

// tcpEcho serves MLEN-framed echo using the same framing TCPConn writes, so
// the test drives our own wire format rather than a hand-rolled one.
func tcpEcho(t *testing.T) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	var wg sync.WaitGroup
	stopped := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func(c net.Conn) {
				defer wg.Done()
				defer func() { _ = c.Close() }()
				srv := &TCPConn{conn: c.(*net.TCPConn)}
				for {
					select {
					case <-stopped:
						return
					default:
					}
					b, err := srv.Receive(context.Background(), 65536)
					if err != nil {
						return
					}
					if err := srv.Send(context.Background(), b); err != nil {
						return
					}
				}
			}(c)
		}
	}()

	stop := func() {
		close(stopped)
		_ = ln.Close()
		wg.Wait()
	}
	return ln.Addr().String(), stop
}

func TestTCPConformance(t *testing.T) {
	conformance.Run(t, conformance.Transport{
		Caps: conformance.Caps{
			Name:          "tcp",
			Client:        true,
			Server:        true,
			ServerReplies: true,
			// 8, not 1: TCPConn.Send prepends an ACP1 MLEN header and
			// Receive rejects MLEN below 8 (spec §"ACP Header" p.10), so a
			// shorter payload cannot round-trip through our own framing.
			MinPayload: 8,
			Ordered:    true,
			TLS:        true,
		},
		StartEcho: tcpEcho,
		Dial: func(ctx context.Context, addr string) (conformance.Conn, error) {
			host, port := splitAddr(t, addr)
			return DialTCP(ctx, host, port)
		},
	})
}

// --- UDP ------------------------------------------------------------------

// udpEcho replies with stdlib WriteToUDP, because transport.UDPListener
// cannot: it exposes Receive, Close and LocalAddr and no way to answer. That
// is deliberate — it exists for ACP1 broadcast announcements, where nothing
// is ever sent back — but it does mean our own API cannot build a UDP echo
// server, which is what ServerReplies:false records.
func udpEcho(t *testing.T) (string, func()) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen packet: %v", err)
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 65536)
		for {
			_ = pc.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
			n, from, rerr := pc.ReadFrom(buf)
			select {
			case <-done:
				return
			default:
			}
			if rerr != nil {
				if ne, ok := rerr.(net.Error); ok && ne.Timeout() {
					continue
				}
				return
			}
			_, _ = pc.WriteTo(buf[:n], from)
		}
	}()

	stop := func() {
		close(done)
		_ = pc.Close()
		wg.Wait()
	}
	return pc.LocalAddr().String(), stop
}

func TestUDPConformance(t *testing.T) {
	conformance.Run(t, conformance.Transport{
		Caps: conformance.Caps{
			Name:   "udp",
			Client: true,
			Server: true,
			// See udpEcho: our listener receives, it does not answer.
			ServerReplies: false,
			MinPayload:    1,
			// Best-effort datagrams: the suite must not assert that every
			// message arrives or that order is kept.
			Ordered: false,
			// No DTLS in this lib, so there is no UDP transport security to
			// test. Declared false rather than left to look untested.
			TLS: false,
		},
		StartEcho: udpEcho,
		Dial: func(ctx context.Context, addr string) (conformance.Conn, error) {
			host, port := splitAddr(t, addr)
			return DialUDP(ctx, host, port)
		},
	})
}
