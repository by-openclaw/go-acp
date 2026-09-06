package transport

// The oversized-datagram contract, which the platforms disagree about.
//
// A Unix recv truncates and reports the short length, so Receive catches it
// with its maxSize+1 buffer. Windows does not truncate -- wsarecv fails with
// WSAEMSGSIZE. Without the translation in Receive this test passes on
// rocky9 and fails on winsrv, which is how the difference stayed invisible.

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A datagram larger than the caller's max is a malformed packet, not a
// truncated one: the transport allocates max+1 precisely so it can tell.
func TestUDPReceiveRejectsOversizedDatagram(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = pc.Close() }()
	host, portStr, _ := net.SplitHostPort(pc.LocalAddr().String())
	port, _ := strconv.Atoi(portStr)

	c, err := DialUDP(context.Background(), host, port)
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer func() { _ = c.Close() }()

	// Make the far end send more than the client will accept.
	if err := c.Send(context.Background(), []byte("ping")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	buf := make([]byte, 16)
	_ = pc.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, from, err := pc.ReadFrom(buf)
	if err != nil {
		t.Fatalf("server read: %v", err)
	}
	if _, err := pc.WriteTo([]byte(strings.Repeat("A", 64)), from); err != nil {
		t.Fatalf("server write: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = c.Receive(ctx, 8)
	if !errors.Is(err, ErrOversizedDatagram) {
		t.Errorf("Receive = %v, want ErrOversizedDatagram", err)
	}
}
