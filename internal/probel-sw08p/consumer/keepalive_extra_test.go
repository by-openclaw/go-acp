package probelsw08p

import (
	"io"
	"log/slog"
	"net"
	"testing"

	"dhs/internal/probel-sw08p/codec"
	sw08session "dhs/internal/probel-sw08p/session"
)

// TestKeepaliveResponderIgnoresNonKeepalive: a frame that is not a
// keepalive request is ignored (early return, no write).
func TestKeepaliveResponderIgnoresNonKeepalive(t *testing.T) {
	p := freshPlugin()
	fn := p.keepaliveAutoResponder()
	// A nil client is safe here because the ID check returns before any
	// client use.
	fn(nil, codec.Frame{ID: codec.TxCrosspointTally, Payload: []byte{1, 2, 3, 4}})
}

// TestKeepaliveResponderRejectsBadPayload: a keepalive-id frame carrying a
// payload fails DecodeKeepaliveRequest and is dropped without a write.
func TestKeepaliveResponderRejectsBadPayload(t *testing.T) {
	p := freshPlugin()
	fn := p.keepaliveAutoResponder()
	fn(nil, codec.Frame{ID: codec.TxAppKeepaliveRequest, Payload: []byte{0xFF}})
}

// TestKeepaliveResponderWriteFailureLogged: a valid keepalive on a closed
// client exercises the cli.Write error → Warn log branch.
func TestKeepaliveResponderWriteFailureLogged(t *testing.T) {
	a, b := net.Pipe()
	_ = b.Close()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	disable := false
	cli := sw08session.NewClientFromConn(a, logger, sw08session.ClientConfig{WireHexLog: &disable})
	_ = cli.Close() // closing makes Write return net.ErrClosed

	p := &Plugin{logger: logger}
	fn := p.keepaliveAutoResponder()
	fn(cli, codec.EncodeKeepaliveRequest()) // ID ok, payload empty → reaches Write, fails
}
