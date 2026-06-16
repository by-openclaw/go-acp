package probelsw02p

import (
	"context"
	"net"
	"testing"
	"time"

	"dhs/internal/probel-sw02p/codec"
)

// drainConn discards everything written to a pipe peer so the
// keepalive goroutine's writes never block. Returns when the peer
// closes.
func drainConn(peer net.Conn) {
	go func() {
		buf := make([]byte, 512)
		for {
			if _, err := peer.Read(buf); err != nil {
				return
			}
		}
	}()
}

// TestStartKeepaliveAppliesDefaults drives startKeepalive with a
// non-zero Dsts and zero spacings so both default-spacing assignments
// (BootstrapSpacing and AppKeepaliveSpacing) execute, plus the final
// goroutine launch. Cancelled immediately so the 2 s default ping never
// actually fires.
func TestStartKeepaliveAppliesDefaults(t *testing.T) {
	cli, peer := pipeClient(t)
	drainConn(peer)
	p := &Plugin{logger: quietLogger()}
	p.matrixCfg = MatrixConfig{
		Dsts:        2,
		InitialPoll: true,
		// BootstrapSpacing == 0 → DefaultBootstrapSpacing branch.
		// AppKeepaliveSpacing == 0 → DefaultAppKeepaliveSpacing branch.
	}

	ctx, cancel := context.WithCancel(context.Background())
	p.startKeepalive(ctx, cli)
	// Give the goroutine a brief moment to run the bootstrap sweep, then
	// cancel so it exits cleanly (the 2 s default ping never fires).
	cancel()
	_ = cli.Close()
}

// TestBootstrapSweepCtxCanceledFirstIteration: a context cancelled
// before the sweep starts makes the very first loop iteration return
// via the ctx.Done branch.
func TestBootstrapSweepCtxCanceledFirstIteration(t *testing.T) {
	cli, peer := pipeClient(t)
	drainConn(peer)
	p := &Plugin{logger: quietLogger()}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel up front

	// spacing -1 (no pacing); Dsts 4. First iteration sees ctx.Done.
	p.bootstrapSweep(ctx, cli, MatrixConfig{Dsts: 4}, -1)
}

// TestBootstrapSweepWriteError: closing the client makes sendInterrogate
// fail, exercising the "bootstrap sweep aborted" branch.
func TestBootstrapSweepWriteError(t *testing.T) {
	cli, peer := pipeClient(t)
	_ = peer.Close()
	_ = cli.Close() // closed client → Write returns net.ErrClosed

	p := &Plugin{logger: quietLogger()}
	// ctx live; the abort comes from the write error, not cancellation.
	p.bootstrapSweep(context.Background(), cli, MatrixConfig{Dsts: 4}, -1)
}

// TestBootstrapSweepPacingCompletes exercises the inter-frame pacing
// select's time.After arm: a small positive spacing with Dsts>=2 makes
// the sweep wait between frames and run to completion.
func TestBootstrapSweepPacingCompletes(t *testing.T) {
	cli, peer := pipeClient(t)
	drainConn(peer)
	p := &Plugin{logger: quietLogger()}

	done := make(chan struct{})
	go func() {
		// 5 ms spacing, 3 dsts → two pacing waits, then completion.
		p.bootstrapSweep(context.Background(), cli, MatrixConfig{Dsts: 3}, 5*time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("bootstrapSweep did not complete with pacing")
	}
}

// TestBootstrapSweepCancelDuringPacing exercises the ctx.Done arm of the
// inter-frame pacing select. A long spacing parks the sweep in the wait
// after the first frame; once that frame is observed on the wire we
// cancel, deterministically hitting the ctx.Done branch.
func TestBootstrapSweepCancelDuringPacing(t *testing.T) {
	cli, peer := pipeClient(t)
	p := &Plugin{logger: quietLogger()}

	firstFrame := make(chan struct{}, 1)
	go func() {
		buf := make([]byte, 0, 32)
		tmp := make([]byte, 32)
		signalled := false
		for {
			n, err := peer.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
				if _, consumed, perr := codec.Unpack(buf); perr == nil && !signalled {
					buf = buf[consumed:]
					signalled = true
					firstFrame <- struct{}{}
				}
			}
			if err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		// 1 h spacing → after the first frame the sweep parks in the
		// pacing wait until cancellation.
		p.bootstrapSweep(ctx, cli, MatrixConfig{Dsts: 5}, time.Hour)
		close(done)
	}()

	select {
	case <-firstFrame:
	case <-time.After(2 * time.Second):
		t.Fatal("no first frame observed")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("bootstrapSweep did not return on cancel during pacing")
	}
}

// TestKeepalivePingLoopCancel exercises the ping loop's ctx.Done arm
// (the select's cancellation branch) without relying on a tick firing.
func TestKeepalivePingLoopCancel(t *testing.T) {
	cli, peer := pipeClient(t)
	drainConn(peer)
	p := &Plugin{logger: quietLogger()}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		// Long spacing so the loop is parked in the select on ctx.Done.
		p.keepalivePingLoop(ctx, cli, MatrixConfig{Dsts: 4}, time.Hour)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("keepalivePingLoop did not exit on ctx cancel")
	}
}

// TestKeepalivePingLoopWriteError drives the ping loop's send-error
// exit: a closed client makes the first tick's sendInterrogate fail.
func TestKeepalivePingLoopWriteError(t *testing.T) {
	cli, peer := pipeClient(t)
	_ = peer.Close()
	_ = cli.Close()
	p := &Plugin{logger: quietLogger()}

	done := make(chan struct{})
	go func() {
		p.keepalivePingLoop(context.Background(), cli, MatrixConfig{Dsts: 4}, 5*time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("keepalivePingLoop did not exit after write error")
	}
}

// TestSendInterrogateExtendedEscalation pins the dst>1023 branch of the
// internal sendInterrogate helper (rx 65 instead of rx 01).
func TestSendInterrogateExtendedEscalation(t *testing.T) {
	cli, peer := pipeClient(t)
	frames := make(chan codec.Frame, 1)
	go func() {
		buf := make([]byte, 0, 32)
		tmp := make([]byte, 32)
		for {
			n, err := peer.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
				if f, _, perr := codec.Unpack(buf); perr == nil {
					frames <- f
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	if err := sendInterrogate(context.Background(), cli, 2000); err != nil {
		t.Fatalf("sendInterrogate(2000): %v", err)
	}
	select {
	case f := <-frames:
		if f.ID != codec.RxExtendedInterrogate {
			t.Errorf("frame.ID = %#x; want RxExtendedInterrogate for dst>1023", f.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no frame observed")
	}
}

// TestSendInterrogateCtxErr pins the ctx-error early return inside the
// internal sendInterrogate helper.
func TestSendInterrogateCtxErr(t *testing.T) {
	cli, peer := pipeClient(t)
	drainConn(peer)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sendInterrogate(ctx, cli, 1); err == nil {
		t.Error("sendInterrogate(canceled ctx) = nil; want ctx error")
	}
}
