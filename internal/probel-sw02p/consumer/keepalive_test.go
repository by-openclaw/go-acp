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

// pipeClient builds a codec.Client wrapping one end of a net.Pipe and
// returns it together with the peer end so a test can read what the
// keepalive goroutine actually wrote on the wire.
func pipeClient(t *testing.T) (*codec.Client, net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cli := codec.NewClientFromConn(a, logger, codec.ClientConfig{})
	t.Cleanup(func() {
		_ = cli.Close()
		_ = b.Close()
	})
	return cli, b
}

// drainFrames reads framed bytes off the pipe peer and returns the
// parsed Frames seen until ctx fires or readErr is non-nil.
func drainFrames(ctx context.Context, peer net.Conn) []codec.Frame {
	out := []codec.Frame{}
	go func() {
		<-ctx.Done()
		_ = peer.SetReadDeadline(time.Now())
	}()
	buf := make([]byte, 0, 1024)
	tmp := make([]byte, 256)
	for {
		n, err := peer.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
		for {
			f, consumed, err := codec.Unpack(buf)
			if err != nil || consumed == 0 {
				break
			}
			out = append(out, f)
			buf = buf[consumed:]
		}
	}
	return out
}

// TestBootstrapSweepFiresNFrames pins the bootstrap-sweep contract:
// with --dsts=N --initial-poll=true and keep-alive disabled, the
// goroutine emits exactly N rx 01 frames.
func TestBootstrapSweepFiresNFrames(t *testing.T) {
	cli, peer := pipeClient(t)
	defer func() { _ = peer.Close() }()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := &Plugin{logger: logger}

	const dsts = 8
	cfg := MatrixConfig{
		Dsts:                dsts,
		InitialPoll:         true,
		BootstrapSpacing:    -1, // skip pacing in test
		AppKeepaliveSpacing: -1, // disable keep-alive
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go p.runKeepalive(ctx, cli, cfg, 0, -1)

	deadline, _ := ctx.Deadline()
	readCtx, readCancel := context.WithDeadline(context.Background(), deadline)
	defer readCancel()
	frames := drainFrames(readCtx, peer)

	if len(frames) != dsts {
		t.Fatalf("got %d frames, want %d", len(frames), dsts)
	}
	for i, f := range frames {
		if f.ID != codec.RxInterrogate {
			t.Errorf("frame[%d].ID = %#x; want RxInterrogate", i, f.ID)
		}
		got, err := codec.DecodeInterrogate(f)
		if err != nil {
			t.Errorf("frame[%d] decode: %v", i, err)
			continue
		}
		if got.Destination != uint16(i) {
			t.Errorf("frame[%d].Destination = %d; want %d", i, got.Destination, i)
		}
	}
}

// TestBootstrapSweepEscalatesToExtended pins auto-escalation: once dst
// crosses 1023, the goroutine switches from rx 01 to rx 65.
func TestBootstrapSweepEscalatesToExtended(t *testing.T) {
	cli, peer := pipeClient(t)
	defer func() { _ = peer.Close() }()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := &Plugin{logger: logger}

	cfg := MatrixConfig{
		Dsts:                1026, // 0..1025 = 1024 narrow + 2 extended
		InitialPoll:         true,
		BootstrapSpacing:    -1,
		AppKeepaliveSpacing: -1,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go p.runKeepalive(ctx, cli, cfg, 0, -1)

	deadline, _ := ctx.Deadline()
	readCtx, readCancel := context.WithDeadline(context.Background(), deadline)
	defer readCancel()
	frames := drainFrames(readCtx, peer)

	if len(frames) != int(cfg.Dsts) {
		t.Fatalf("got %d frames, want %d", len(frames), cfg.Dsts)
	}
	// First 1024 (dst 0..1023) must be RxInterrogate; 1024 + 1025 must
	// be RxExtendedInterrogate.
	for i := 0; i < 1024; i++ {
		if frames[i].ID != codec.RxInterrogate {
			t.Fatalf("frame[%d].ID = %#x; want RxInterrogate (narrow)", i, frames[i].ID)
		}
	}
	for i := 1024; i < int(cfg.Dsts); i++ {
		if frames[i].ID != codec.RxExtendedInterrogate {
			t.Fatalf("frame[%d].ID = %#x; want RxExtendedInterrogate", i, frames[i].ID)
		}
		got, err := codec.DecodeExtendedInterrogate(frames[i])
		if err != nil {
			t.Errorf("frame[%d] decode ext: %v", i, err)
			continue
		}
		if got.Destination != uint16(i) {
			t.Errorf("frame[%d].Destination = %d; want %d", i, got.Destination, i)
		}
	}
}

// TestBootstrapSweepDisabled confirms InitialPoll=false suppresses the
// bootstrap sweep (and with keep-alive also disabled, no frames fire).
func TestBootstrapSweepDisabled(t *testing.T) {
	cli, peer := pipeClient(t)
	defer func() { _ = peer.Close() }()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := &Plugin{logger: logger}

	cfg := MatrixConfig{
		Dsts:                32,
		InitialPoll:         false,
		AppKeepaliveSpacing: -1, // disable keep-alive
	}

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	go p.runKeepalive(ctx, cli, cfg, 0, -1)

	readCtx, readCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer readCancel()
	frames := drainFrames(readCtx, peer)

	if len(frames) != 0 {
		t.Errorf("got %d frames; want 0 (initial-poll disabled)", len(frames))
	}
}

// TestKeepalivePingRotates pins the ping-rotation contract: after the
// bootstrap sweep, the goroutine fires one rx 01 per tick advancing
// the dst cursor modulo Dsts.
func TestKeepalivePingRotates(t *testing.T) {
	cli, peer := pipeClient(t)
	defer func() { _ = peer.Close() }()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := &Plugin{logger: logger}

	const dsts = 4
	cfg := MatrixConfig{
		Dsts:        dsts,
		InitialPoll: false, // skip bootstrap, focus on ping
	}

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()

	// 50 ms spacing → ~10 ticks in 600 ms; we want ≥ 2 full rotations.
	go p.runKeepalive(ctx, cli, cfg, 0, 50*time.Millisecond)

	readCtx, readCancel := context.WithTimeout(context.Background(), 550*time.Millisecond)
	defer readCancel()
	frames := drainFrames(readCtx, peer)

	if len(frames) < dsts {
		t.Fatalf("got %d ping frames; want >= %d (one full rotation)", len(frames), dsts)
	}
	// Each frame's Destination must be (cursor++ % dsts).
	for i, f := range frames {
		got, err := codec.DecodeInterrogate(f)
		if err != nil {
			t.Fatalf("frame[%d] decode: %v", i, err)
		}
		want := uint16(i % dsts)
		if got.Destination != want {
			t.Errorf("frame[%d].Destination = %d; want %d", i, got.Destination, want)
		}
	}
}

// TestKeepaliveCancelOnCtx confirms the goroutine exits promptly when
// its context is cancelled (Disconnect path).
func TestKeepaliveCancelOnCtx(t *testing.T) {
	cli, peer := pipeClient(t)
	defer func() { _ = peer.Close() }()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := &Plugin{logger: logger}

	cfg := MatrixConfig{
		Dsts:        4,
		InitialPoll: false,
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		p.runKeepalive(ctx, cli, cfg, 0, 25*time.Millisecond)
		close(done)
	}()
	// Drain pipe so writes don't block the goroutine.
	go func() {
		buf := make([]byte, 256)
		for {
			if _, err := peer.Read(buf); err != nil {
				return
			}
		}
	}()
	time.Sleep(75 * time.Millisecond) // let it send a few pings
	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("runKeepalive did not exit within 500 ms of ctx cancel")
	}
}

// TestKeepaliveDisabledWhenDstsZero confirms the goroutine never starts
// firing if MatrixConfig.Dsts == 0 (caller didn't set a matrix size).
func TestKeepaliveDisabledWhenDstsZero(t *testing.T) {
	cli, peer := pipeClient(t)
	defer func() { _ = peer.Close() }()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := &Plugin{logger: logger}

	// Empty config → Dsts == 0 → startKeepalive is a no-op.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.startKeepalive(ctx, cli)

	readCtx, readCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer readCancel()
	frames := drainFrames(readCtx, peer)

	if len(frames) != 0 {
		t.Errorf("got %d frames with Dsts=0; want 0", len(frames))
	}
}
