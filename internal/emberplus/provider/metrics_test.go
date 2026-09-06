package emberplus

import (
	"net"
	"testing"

	"dhs/internal/emberplus/codec/s101"
	"dhs/internal/plugin"
)

// The provider exposed no metrics at all, so `producer emberplus serve
// --metrics-addr` mounted an endpoint with no emberplus series in it — the
// CLI warns "provider does not expose Metrics() — skipping".
func TestServerExposesMetrics(t *testing.T) {
	if newServer(plugin.Deps{}, nil).Metrics() == nil {
		t.Fatal("Metrics must be non-nil — WithDefaults always fills it")
	}
}

// Frames are attributed by S101 command byte, which separates EmBER
// payloads from keep-alives — the distinction that matters in a scrape,
// since a link can be busy with keep-alives and carrying no data.
func TestNewServerRegistersTheS101Commands(t *testing.T) {
	names := newServer(plugin.Deps{}, nil).Metrics().Snapshot().CmdNames
	for cmd, want := range map[byte]string{
		s101.CmdEmBER:         "ember",
		s101.CmdKeepAliveReq:  "keepalive-req",
		s101.CmdKeepAliveResp: "keepalive-resp",
	} {
		if got := names[cmd]; got != want {
			t.Errorf("S101 command 0x%02x registered as %q, want %q", cmd, got, want)
		}
	}
}

// A keep-alive request is answered, and the answer is counted — a link
// carrying nothing but keep-alives is a real state worth being able to see.
func TestKeepAliveResponseIsCounted(t *testing.T) {
	srv := newServer(plugin.Deps{}, buildRichExport())
	clientConn, srvConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close() })

	sess := newSession(srv, srvConn)
	t.Cleanup(sess.close)

	// net.Pipe is synchronous: drain what the provider writes, or the
	// keep-alive response blocks forever.
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 256)
		_, _ = clientConn.Read(buf)
	}()

	if err := sess.handleFrame(&s101.Frame{Command: s101.CmdKeepAliveReq}); err != nil {
		t.Fatalf("handleFrame keepalive-req: %v", err)
	}
	<-done

	if got := srv.Metrics().Snapshot().TxHitsByCmd[s101.CmdKeepAliveResp]; got != 1 {
		t.Errorf("keepalive responses counted = %d, want 1", got)
	}
}

// An EmBER message is counted once, not once per S101 chunk: the chunking
// is a transport detail and a consumer sees one message.
func TestEmBERMessageIsCountedOncePerMessage(t *testing.T) {
	srv := newServer(plugin.Deps{}, buildRichExport())
	clientConn, srvConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close() })

	sess := newSession(srv, srvConn)
	t.Cleanup(sess.close)

	// Big enough to be split across several S101 frames.
	payload := make([]byte, maxS101Payload*3)

	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := clientConn.Read(buf); err != nil {
				return
			}
		}
	}()

	if err := sess.writeEmBERChunks(payload); err != nil {
		t.Fatalf("writeEmBERChunks: %v", err)
	}

	snap := srv.Metrics().Snapshot()
	if got := snap.TxHitsByCmd[s101.CmdEmBER]; got != 1 {
		t.Errorf("a multi-chunk message counted %d times, want 1", got)
	}
	if got := snap.TxBytesByCmd[s101.CmdEmBER]; got != uint64(len(payload)) {
		t.Errorf("counted %d bytes, want the whole payload %d", got, len(payload))
	}
}

// A keep-alive that cannot be written surfaces the error and counts
// nothing: a response that never left is not a response.
func TestKeepAliveWriteFailureIsNotCounted(t *testing.T) {
	srv := newServer(plugin.Deps{}, buildRichExport())
	clientConn, srvConn := net.Pipe()
	sess := newSession(srv, srvConn)

	// Both ends closed, so the write fails immediately.
	_ = clientConn.Close()
	_ = srvConn.Close()

	if err := sess.handleFrame(&s101.Frame{Command: s101.CmdKeepAliveReq}); err == nil {
		t.Fatal("handleFrame must surface the write error")
	}
	if got := srv.Metrics().Snapshot().TxHitsByCmd[s101.CmdKeepAliveResp]; got != 0 {
		t.Errorf("counted %d responses for a write that failed", got)
	}
}
