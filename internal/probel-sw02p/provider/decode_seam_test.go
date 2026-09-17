package probelsw02p

import (
	"context"
	"dhs/internal/plugin"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"dhs/internal/export/canonical"
	"dhs/internal/probel-sw02p/codec"
)

// TestDecodedPassthrough pins the production (hook == nil) behaviour of
// the decoded seam: it returns the (value, err) pair verbatim so the
// guard it feeds is identical to calling the codec directly.
func TestDecodedPassthrough(t *testing.T) {
	if decodeErrHook != nil {
		t.Fatal("decodeErrHook must be nil by default (production passthrough)")
	}
	wantErr := errors.New("real decode error")
	got, err := decoded(GoParamsZero(), wantErr)
	if !errors.Is(err, wantErr) {
		t.Errorf("decoded err = %v; want %v (passthrough)", err, wantErr)
	}
	_ = got

	v, err := decoded(7, nil)
	if v != 7 || err != nil {
		t.Errorf("decoded(7,nil) = (%d,%v); want (7,nil)", v, err)
	}
}

// GoParamsZero is a tiny typed-zero helper so the generic decoded call
// above has a concrete type to instantiate.
func GoParamsZero() codec.GoParams { return codec.GoParams{} }

// TestSessionRunHandlerDecodeErrorSeam covers session.run's
// handler-decode-error arm (the "herr != nil" branch). That arm is
// unreachable from the wire because codec.Unpack pre-validates MESSAGE
// length, so handleInterrogate's codec.DecodeInterrogate cannot fail on
// a framed rx 01. The decodeErrHook seam forces that decode to fail for
// the duration of the test; the session must absorb it
// (HandlerDecodeFailed + ObserveDecodeError) and keep reading.
func TestSessionRunHandlerDecodeErrorSeam(t *testing.T) {
	forced := errors.New("forced handler decode failure")
	decodeErrHook = func() error { return forced }
	defer func() { decodeErrHook = nil }()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := newServer(plugin.Deps{Logger: logger}, nil)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.serveListener(ctx, ln) }()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// A well-formed rx 01 INTERROGATE: Unpack succeeds, but the forced
	// hook makes handleInterrogate's decode return an error, driving the
	// session's herr != nil arm.
	in := codec.Pack(codec.EncodeInterrogate(codec.InterrogateParams{Destination: 1}))
	if _, err := conn.Write(in); err != nil {
		t.Fatalf("write rx 01: %v", err)
	}

	// HandlerDecodeFailed must fire; the session must NOT reply.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if srv.profile.Snapshot()[HandlerDecodeFailed] >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := srv.profile.Snapshot()[HandlerDecodeFailed]; got < 1 {
		t.Errorf("HandlerDecodeFailed = %d; want >= 1", got)
	}
	if got := srv.metrics.Snapshot().DecodeErrors; got < 1 {
		t.Errorf("metrics DecodeErrors = %d; want >= 1", got)
	}

	// Session still alive: clear the hook and prove a follow-up rx 01
	// now gets a tx 03 TALLY reply.
	decodeErrHook = nil
	good := codec.Pack(codec.EncodeInterrogate(codec.InterrogateParams{Destination: 1}))
	if _, err := conn.Write(good); err != nil {
		t.Fatalf("write rx 01 #2: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 0, 32)
	tmp := make([]byte, 32)
	var gotReply bool
	for !gotReply {
		n, rerr := conn.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			if f, _, derr := codec.Unpack(buf); derr == nil && f.ID == codec.TxTally {
				gotReply = true
			}
		}
		if rerr != nil {
			break
		}
	}
	if !gotReply {
		t.Error("session did not survive the forced decode error (no tx 03 after recovery)")
	}

	cancel()
	select {
	case <-serveDone:
	case <-time.After(2 * time.Second):
		t.Error("serveListener did not return")
	}
}

// TestNewServerTreeBuildFailureSeam covers newServer's defensive
// "tree build failed" fallback. newTree never returns an error for any
// canonical.Export, so the fallback is unreachable in production. The
// treeBuildErrHook seam forces the failure; newServer must log it and
// substitute an empty (non-nil) tree rather than leaving tree nil.
func TestNewServerTreeBuildFailureSeam(t *testing.T) {
	treeBuildErrHook = func() error { return errors.New("forced tree build failure") }
	defer func() { treeBuildErrHook = nil }()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := newServer(plugin.Deps{Logger: logger}, &canonical.Export{Root: &canonical.Node{
		Header: canonical.Header{Number: 1, Identifier: "root", OID: "1"},
	}})
	if srv.tree == nil {
		t.Fatal("tree = nil after forced build failure; want empty fallback tree")
	}
	if srv.tree.Size() != 0 {
		t.Errorf("fallback tree size = %d; want 0 (empty substitute)", srv.tree.Size())
	}
}
