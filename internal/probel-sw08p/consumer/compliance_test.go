package probelsw08p

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"acp/internal/probel-sw08p/codec"
)

// TestComplianceProfileNilBeforeConnect: accessing ComplianceProfile on a
// fresh plugin returns nil (the session profile is installed at Connect
// time).
func TestComplianceProfileNilBeforeConnect(t *testing.T) {
	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if p.ComplianceProfile() != nil {
		t.Error("ComplianceProfile() = non-nil; want nil before Connect")
	}
}

// TestComplianceProfileCountsNAK: a NAK observed during Send bumps
// NAKReceived. Uses a loopback listener so we can script the peer's
// replies without a real matrix.
func TestComplianceProfileCountsNAK(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	addr := ln.Addr().String()
	_, portStr, _ := net.SplitHostPort(addr)
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		t.Fatalf("port: %v", err)
	}

	// Peer: NAK every frame twice, ACK the third attempt.
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		accepted <- c
		buf := make([]byte, 512)
		count := 0
		tmp := make([]byte, 0, 512)
		for {
			n, err := c.Read(buf)
			if err != nil {
				return
			}
			tmp = append(tmp, buf[:n]...)
			for len(tmp) > 0 && tmp[0] == codec.DLE && len(tmp) >= 2 && tmp[1] == codec.STX {
				_, consumed, perr := codec.Unpack(tmp)
				if perr != nil {
					break
				}
				count++
				if count < 3 {
					_, _ = c.Write(codec.PackNAK())
				} else {
					_, _ = c.Write(codec.PackACK())
				}
				tmp = tmp[consumed:]
			}
		}
	}()

	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := p.Connect(ctx, "127.0.0.1", port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	select {
	case <-accepted:
	case <-time.After(2 * time.Second):
		t.Fatal("peer never accepted")
	}

	if err := p.Maintenance(ctx, codec.MaintSoftReset, 0, 0); err != nil {
		t.Fatalf("Maintenance: %v", err)
	}

	prof := p.ComplianceProfile()
	if prof == nil {
		t.Fatal("ComplianceProfile() nil after Connect")
	}
	snap := prof.Snapshot()
	if snap[NAKReceived] != 2 {
		t.Errorf("NAKReceived = %d; want 2", snap[NAKReceived])
	}
	if snap[RetryAttempted] != 2 {
		t.Errorf("RetryAttempted = %d; want 2", snap[RetryAttempted])
	}
	if prof.Classification() != "partial" {
		t.Errorf("Classification = %q; want partial", prof.Classification())
	}
}
