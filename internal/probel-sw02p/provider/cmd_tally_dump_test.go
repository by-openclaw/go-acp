package probelsw02p

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"acp/internal/probel-sw02p/codec"
)

// TestEmitExtendedProtectTallyDumpFanout drives EmitExtendedProtect
// TallyDump through a live TCP session and confirms the wire bytes
// arrive on the client side with the expected shape.
func TestEmitExtendedProtectTallyDumpFanout(t *testing.T) {
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
	_ = ln.Close()

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

	params := codec.ExtendedProtectTallyDumpParams{
		Entries: []codec.ExtendedProtectTallyDumpEntry{
			{Destination: 1, Device: 2, Protect: codec.ProtectProBel},
			{Destination: 3, Device: 4, Protect: codec.ProtectOEM},
		},
	}
	if err := srv.EmitExtendedProtectTallyDump(params); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	// Wire: SOM + CMD + 1 count + 2 * 4 entry + checksum = 12 bytes.
	buf := make([]byte, 12)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	f, _, err := codec.Unpack(buf)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if f.ID != codec.TxExtendedProtectTallyDump {
		t.Errorf("ID = %#x; want TxExtendedProtectTallyDump", f.ID)
	}
	got, err := codec.DecodeExtendedProtectTallyDump(f)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(got.Entries) != 2 || got.Entries[0].Destination != 1 || got.Entries[1].Destination != 3 {
		t.Errorf("decoded = %+v; want 2 entries with dst 1, 3", got)
	}
}

// TestEmitExtendedProtectTallyDumpRejectsOverflow verifies the §3.2.64
// 132-byte cap is enforced: callers passing more than the per-message
// maximum get a descriptive error and no broadcast fires.
func TestEmitExtendedProtectTallyDumpRejectsOverflow(t *testing.T) {
	srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	too := make([]codec.ExtendedProtectTallyDumpEntry, codec.ExtendedProtectTallyDumpMaxCount+1)
	err := srv.EmitExtendedProtectTallyDump(codec.ExtendedProtectTallyDumpParams{Entries: too})
	if err == nil {
		t.Fatal("Emit with overflow returned nil; want error")
	}
	if !strings.Contains(err.Error(), "§3.2.64") {
		t.Errorf("error %q does not cite §3.2.64", err)
	}
}
