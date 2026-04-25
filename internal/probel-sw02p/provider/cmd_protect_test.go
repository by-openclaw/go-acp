package probelsw02p

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"acp/internal/probel-sw02p/codec"
)

// TestExtendedProtectEmitFanout confirms the three Emit* helpers fan
// the Extended PROTECT tally / connected / disconnected broadcasts
// out to every connected session.
func TestExtendedProtectEmitFanout(t *testing.T) {
	srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	defer func() { _ = srv.Stop() }()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if serr := srv.Serve(ctx, ln.Addr().String()); serr != nil {
			t.Logf("server: %v", serr)
		}
	}()
	_ = ln.Close() // hand port to Serve

	// Wait for listener to come up.
	deadline := time.Now().Add(2 * time.Second)
	var conn net.Conn
	for time.Now().Before(deadline) {
		c, derr := net.Dial("tcp", ln.Addr().String())
		if derr == nil {
			conn = c
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if conn == nil {
		t.Fatal("could not dial provider")
	}
	defer func() { _ = conn.Close() }()

	// Wait for server-side to see the session.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		srv.mu.Lock()
		n := len(srv.sessions)
		srv.mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	cases := []struct {
		name string
		emit func()
		wantCmd codec.CommandID
	}{
		{
			"tx 96",
			func() {
				srv.EmitExtendedProtectTally(codec.ExtendedProtectTallyParams{
					Protect: codec.ProtectProBel, Destination: 1, Device: 2,
				})
			},
			codec.TxExtendedProtectTally,
		},
		{
			"tx 97",
			func() {
				srv.EmitExtendedProtectConnected(codec.ExtendedProtectConnectedParams{
					Protect: codec.ProtectOEM, Destination: 3, Device: 4,
				})
			},
			codec.TxExtendedProtectConnected,
		},
		{
			"tx 98",
			func() {
				srv.EmitExtendedProtectDisconnected(codec.ExtendedProtectDisconnectedParams{
					Protect: codec.ProtectNone, Destination: 5, Device: 6,
				})
			},
			codec.TxExtendedProtectDisconnected,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.emit()
			// Each frame is 1 + 1 + 5 + 1 = 8 bytes.
			buf := make([]byte, 8)
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			if _, err := io.ReadFull(conn, buf); err != nil {
				t.Fatalf("read %s: %v", tc.name, err)
			}
			f, _, err := codec.Unpack(buf)
			if err != nil {
				t.Fatalf("unpack %s: %v", tc.name, err)
			}
			if f.ID != tc.wantCmd {
				t.Errorf("ID = %#x; want %#x", f.ID, tc.wantCmd)
			}
		})
	}
}
