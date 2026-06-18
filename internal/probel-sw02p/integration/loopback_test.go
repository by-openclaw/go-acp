//go:build integration

// Loopback integration test: our own SW-P-02 provider (the router
// emulator) backs a real TCP listener; our own SW-P-02 consumer dials
// it and drives the full codec → session → handler → fan-out stack
// over the wire. Exercises encode → TCP (SOM 0xFF + 7-bit checksum, no
// DLE) → decode → handler → broadcast → await-matcher end-to-end. No
// external device required; the provider IS the matrix emulator, so the
// loopback body is CI-safe.
//
// External mode: set PROBEL_SW02P_TEST_HOST (optionally
// PROBEL_SW02P_TEST_PORT, default 2002) to point the same consumer
// round-trips at a real SW-P-02 matrix / vendor emulator on the lab
// network instead of the in-process provider. Mirrors the env-var
// gating used by the osc / tsl / acp1 integration bodies.
//
// Run with:
//
//	go test -tags integration ./internal/probel-sw02p/integration/...
//
// Externally:
//
//	PROBEL_SW02P_TEST_HOST=10.100.0.42 \
//	  go test -tags integration ./internal/probel-sw02p/integration/...
package probelsw02p_integration

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"dhs/internal/export/canonical"
	"dhs/internal/probel-sw02p/codec"
	consumer "dhs/internal/probel-sw02p/consumer"
	provider "dhs/internal/probel-sw02p/provider"
)

// matrixDsts / matrixSrcs bound the served matrix. The provider applies
// rx 02 CONNECT leniently against these declared counts, so every test
// dst/src must stay inside the matrix or the route is silently dropped
// (no tx 04). Keep the addresses below well inside the bounds.
const (
	matrixDsts = 8
	matrixSrcs = 8
)

const externalHostEnv = "PROBEL_SW02P_TEST_HOST"
const externalPortEnv = "PROBEL_SW02P_TEST_PORT"

// testEnv carries the per-test consumer endpoint plus a teardown hook.
// In loopback mode the provider is started in-process; in external mode
// it points at the lab host and the provider hook is a no-op.
type testEnv struct {
	host string
	port int
}

// testLogger returns a debug logger when DHS_TEST_VERBOSE=1, otherwise a
// silent one — mirrors the provider integration_test convention.
func testLogger() *slog.Logger {
	if os.Getenv("DHS_TEST_VERBOSE") == "1" {
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// servedExport is the canonical matrix tree the loopback provider serves
// — a single 1-to-N matrix of matrixDsts × matrixSrcs. This is the exact
// shape committed under testdata/exports/matrix_tree.json (see
// fixture_test.go), so the loopback emulator and the fixture never drift.
func servedExport() *canonical.Export {
	return &canonical.Export{
		Root: &canonical.Node{
			Header: canonical.Header{
				Number: 1, Identifier: "router", OID: "1",
				Children: []canonical.Element{
					&canonical.Matrix{
						Header: canonical.Header{
							Number: 1, Identifier: "matrix-0", OID: "1.1",
						},
						Type:        canonical.MatrixOneToN,
						Mode:        canonical.ModeLinear,
						TargetCount: matrixDsts,
						SourceCount: matrixSrcs,
						Labels:      []canonical.MatrixLabel{{BasePath: "router.matrix-0.video"}},
					},
				},
			},
		},
	}
}

// startLoopbackProvider binds a fresh provider to an ephemeral localhost
// port and returns the bound endpoint. The provider is torn down when
// the test finishes via t.Cleanup.
func startLoopbackProvider(t *testing.T, logger *slog.Logger) testEnv {
	t.Helper()

	f := &provider.Factory{}
	prov := f.New(logger, servedExport())

	// Probe an ephemeral port, then hand the address to Serve. Mirrors
	// the close-then-rebind pattern used by the provider integration
	// test; the 1 s dial-wait below tolerates the rebind window.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close probe listener: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- prov.Serve(ctx, addr) }()

	t.Cleanup(func() {
		cancel()
		select {
		case <-serveDone:
		case <-time.After(2 * time.Second):
			t.Error("provider Serve did not return after ctx cancel")
		}
	})

	// Wait up to 1 s for the listener to accept.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		c, derr := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if derr == nil {
			_ = c.Close()
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	host, port := splitHostPort(t, addr)
	return testEnv{host: host, port: port}
}

// dialEnv resolves the consumer endpoint. When PROBEL_SW02P_TEST_HOST is
// set, it targets that lab host (no in-process provider); otherwise it
// spins up the loopback emulator. The matrix-config bootstrap sweep +
// keep-alive poll is left disabled so tests drive exactly the frames
// they assert — no background rx 01 noise.
func dialEnv(t *testing.T) (*consumer.Plugin, func()) {
	t.Helper()
	logger := testLogger()

	var env testEnv
	if host := os.Getenv(externalHostEnv); host != "" {
		port := codec.DefaultPort
		if ps := os.Getenv(externalPortEnv); ps != "" {
			p, err := strconv.Atoi(ps)
			if err != nil {
				t.Fatalf("%s=%q: %v", externalPortEnv, ps, err)
			}
			port = p
		}
		env = testEnv{host: host, port: port}
		t.Logf("external mode: SW-P-02 matrix at %s:%d", host, port)
	} else {
		env = startLoopbackProvider(t, logger)
	}

	f := &consumer.Factory{}
	pl, ok := f.New(logger).(*consumer.Plugin)
	if !ok {
		t.Fatal("consumer Factory.New did not return *Plugin")
	}
	// Disable the bootstrap rx 01 sweep + keep-alive so each test owns
	// every frame on the wire.
	pl.SetMatrixConfig(consumer.MatrixConfig{
		InitialPoll:         false,
		AppKeepaliveSpacing: -1,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pl.Connect(ctx, env.host, env.port); err != nil {
		t.Fatalf("consumer Connect %s:%d: %v", env.host, env.port, err)
	}
	return pl, func() { _ = pl.Disconnect() }
}

// sendCtx returns a 3 s send context — long enough to absorb the rebind
// window and lab RTT, short enough to fail fast on a wedged matrix.
func sendCtx(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 3*time.Second)
}

// TestConnectInterrogateRoundTrip drives rx 02 CONNECT then rx 01
// INTERROGATE on the same destination and asserts the interrogate reads
// back the source the connect just set — the basic "set a crosspoint and
// read it back" contract over a real TCP round-trip.
func TestConnectInterrogateRoundTrip(t *testing.T) {
	pl, closer := dialEnv(t)
	defer closer()

	const dst, src = uint16(2), uint16(5)

	ctx, cancel := sendCtx(t)
	defer cancel()

	cp, err := pl.SendConnect(ctx, dst, src, false)
	if err != nil {
		t.Fatalf("SendConnect(%d,%d): %v", dst, src, err)
	}
	if cp.Destination != dst || cp.Source != src {
		t.Fatalf("tx 04 = (%d,%d); want (%d,%d)", cp.Destination, cp.Source, dst, src)
	}

	tally, err := pl.SendInterrogate(ctx, dst)
	if err != nil {
		t.Fatalf("SendInterrogate(%d): %v", dst, err)
	}
	if tally.Destination != dst {
		t.Errorf("tx 03 dst = %d; want %d", tally.Destination, dst)
	}
	if tally.Source != src {
		t.Errorf("tx 03 src = %d; want %d (route did not stick)", tally.Source, src)
	}
}

// TestConnectTally drives a single rx 02 CONNECT and asserts the matrix
// fans out the tx 04 CROSSPOINT CONNECTED tally to a SECOND consumer
// session that never sent a frame — exercising the cross-session
// broadcast path (§3.2.6 "issued on all ports").
func TestConnectTally(t *testing.T) {
	logger := testLogger()

	// This cross-session assertion is loopback-only — it needs two
	// sessions against the SAME emulator instance and observes the
	// fan-out. Against an external matrix we cannot guarantee a second
	// idle controller, so target only the loopback provider here.
	if os.Getenv(externalHostEnv) != "" {
		t.Skip("cross-session fan-out assertion is loopback-only; skipping in external mode")
	}
	env := startLoopbackProvider(t, logger)

	// Controller A: the active driver.
	fa := &consumer.Factory{}
	plA := fa.New(logger).(*consumer.Plugin)
	plA.SetMatrixConfig(consumer.MatrixConfig{InitialPoll: false, AppKeepaliveSpacing: -1})

	// Controller B: a passive observer subscribed to every frame.
	fb := &consumer.Factory{}
	plB := fb.New(logger).(*consumer.Plugin)
	plB.SetMatrixConfig(consumer.MatrixConfig{InitialPoll: false, AppKeepaliveSpacing: -1})

	connCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := plA.Connect(connCtx, env.host, env.port); err != nil {
		t.Fatalf("A connect: %v", err)
	}
	defer func() { _ = plA.Disconnect() }()
	if err := plB.Connect(connCtx, env.host, env.port); err != nil {
		t.Fatalf("B connect: %v", err)
	}
	defer func() { _ = plB.Disconnect() }()

	clientB, err := plB.ExposeClient()
	if err != nil {
		t.Fatalf("B expose client: %v", err)
	}
	got := make(chan codec.ConnectedParams, 4)
	clientB.Subscribe(func(f codec.Frame) {
		if f.ID != codec.TxCrosspointConnected {
			return
		}
		if p, derr := codec.DecodeConnected(f); derr == nil {
			got <- p
		}
	})

	const dst, src = uint16(3), uint16(6)
	sctx, scancel := sendCtx(t)
	defer scancel()

	// Prove B's session is fully registered in the provider's session
	// map BEFORE A drives the connect — otherwise A's fan-out can race
	// ahead of B's accept and B misses the broadcast. A completed B
	// round-trip guarantees the server has B in its session set.
	if _, err := plB.SendInterrogate(sctx, 0); err != nil {
		t.Fatalf("B warm-up interrogate: %v", err)
	}

	if _, err := plA.SendConnect(sctx, dst, src, false); err != nil {
		t.Fatalf("A SendConnect: %v", err)
	}

	select {
	case p := <-got:
		if p.Destination != dst || p.Source != src {
			t.Errorf("B observed tx 04 (%d,%d); want (%d,%d)", p.Destination, p.Source, dst, src)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("B never observed the tx 04 tally fan-out")
	}
}

// TestProtectCycle drives the full protect lifecycle over the wire:
// interrogate (unprotected) → rx 102 PROTECT CONNECT → interrogate
// (Pro-Bel protected, owner echoed) → rx 104 PROTECT DIS-CONNECT →
// interrogate (unprotected again). Pins the owner-only authority model's
// happy path end-to-end.
func TestProtectCycle(t *testing.T) {
	pl, closer := dialEnv(t)
	defer closer()

	const dst, device = uint16(4), uint16(9)

	ctx, cancel := sendCtx(t)
	defer cancel()

	// 1. Start unprotected.
	pre, err := pl.SendExtendedProtectInterrogate(ctx, dst)
	if err != nil {
		t.Fatalf("pre interrogate: %v", err)
	}
	if pre.Protect != codec.ProtectNone {
		t.Fatalf("pre-protect state = %d; want ProtectNone — test dst dirty", pre.Protect)
	}

	// 2. Protect for our device.
	on, err := pl.SendExtendedProtectConnect(ctx, dst, device)
	if err != nil {
		t.Fatalf("protect-connect: %v", err)
	}
	if on.Protect != codec.ProtectProBel {
		t.Errorf("after protect-connect state = %d; want ProtectProBel", on.Protect)
	}
	if on.Destination != dst || on.Device != device {
		t.Errorf("tx 097 = (dst=%d, dev=%d); want (dst=%d, dev=%d)",
			on.Destination, on.Device, dst, device)
	}

	// 3. Interrogate confirms the protect stuck with the right owner.
	mid, err := pl.SendExtendedProtectInterrogate(ctx, dst)
	if err != nil {
		t.Fatalf("mid interrogate: %v", err)
	}
	if mid.Protect != codec.ProtectProBel || mid.Device != device {
		t.Errorf("mid interrogate = (state=%d, dev=%d); want (ProtectProBel, %d)",
			mid.Protect, mid.Device, device)
	}

	// 4. Disconnect by the owning device.
	off, err := pl.SendExtendedProtectDisconnect(ctx, dst, device)
	if err != nil {
		t.Fatalf("protect-disconnect: %v", err)
	}
	if off.Protect != codec.ProtectNone {
		t.Errorf("after protect-disconnect state = %d; want ProtectNone", off.Protect)
	}

	// 5. Interrogate confirms cleared.
	post, err := pl.SendExtendedProtectInterrogate(ctx, dst)
	if err != nil {
		t.Fatalf("post interrogate: %v", err)
	}
	if post.Protect != codec.ProtectNone {
		t.Errorf("post interrogate state = %d; want ProtectNone", post.Protect)
	}
}

// TestDualControllerStatus drives rx 050 DUAL CONTROLLER STATUS REQUEST
// and asserts the single-controller emulator answers Master active /
// idle OK per its documented single-controller-by-construction policy.
func TestDualControllerStatus(t *testing.T) {
	pl, closer := dialEnv(t)
	defer closer()

	ctx, cancel := sendCtx(t)
	defer cancel()

	resp, err := pl.SendDualControllerStatusRequest(ctx)
	if err != nil {
		t.Fatalf("SendDualControllerStatusRequest: %v", err)
	}
	// External matrices with a real redundant pair may legitimately
	// report Slave-active / idle-faulty — only assert the exact shape
	// against our own loopback emulator.
	if os.Getenv(externalHostEnv) == "" {
		if resp.Active != codec.ActiveControllerMaster {
			t.Errorf("active = %d; want ActiveControllerMaster", resp.Active)
		}
		if resp.IdleStatus != codec.IdleControllerOK {
			t.Errorf("idle = %d; want IdleControllerOK", resp.IdleStatus)
		}
	}
}

// TestSourceLockStatus drives rx 014 SOURCE LOCK STATUS REQUEST and
// asserts the emulator returns a lock bitmap. The software emulator has
// no physical input cards, so it reports every declared source as
// locked=true (§3.2.17 all-present interpretation).
func TestSourceLockStatus(t *testing.T) {
	pl, closer := dialEnv(t)
	defer closer()

	ctx, cancel := sendCtx(t)
	defer cancel()

	resp, err := pl.SendSourceLockStatusRequest(ctx, codec.ControllerLH)
	if err != nil {
		t.Fatalf("SendSourceLockStatusRequest: %v", err)
	}
	if len(resp.Locked) == 0 {
		t.Fatal("source-lock response carried an empty bitmap")
	}
	if os.Getenv(externalHostEnv) == "" {
		// Loopback emulator reports every source locked.
		for i, locked := range resp.Locked {
			if !locked {
				t.Errorf("source %d reported unlocked; loopback emulator should report all locked", i)
			}
		}
	}
}

// TestSalvoConnectOnGoThenGo stages three crosspoints under a named
// salvo via rx 35 CONNECT ON GO GROUP SALVO, fires the group via rx 36
// GO GROUP SALVO, and asserts the matrix returns tx 38 GO DONE GROUP
// SALVO ACK (Set) and that every staged crosspoint reads back via a
// follow-up rx 01 INTERROGATE — the salvo commit path end-to-end.
func TestSalvoConnectOnGoThenGo(t *testing.T) {
	pl, closer := dialEnv(t)
	defer closer()

	ctx, cancel := sendCtx(t)
	defer cancel()

	const salvoID = uint8(5)
	slots := []struct{ dst, src uint16 }{{0, 1}, {1, 2}, {2, 3}}

	for _, s := range slots {
		ack, err := pl.SendConnectOnGoGroupSalvo(ctx, s.dst, s.src, salvoID, false)
		if err != nil {
			t.Fatalf("stage (%d,%d) salvo %d: %v", s.dst, s.src, salvoID, err)
		}
		if ack.Destination != s.dst || ack.Source != s.src || ack.SalvoID != salvoID {
			t.Errorf("tx 37 ack = (%d,%d,salvo=%d); want (%d,%d,salvo=%d)",
				ack.Destination, ack.Source, ack.SalvoID, s.dst, s.src, salvoID)
		}
	}

	done, err := pl.SendGoGroupSalvo(ctx, codec.GoOpSet, salvoID)
	if err != nil {
		t.Fatalf("fire salvo %d: %v", salvoID, err)
	}
	if done.Result != codec.GoGroupResultSet || done.SalvoID != salvoID {
		t.Errorf("tx 38 = %+v; want Result=Set SalvoID=%d", done, salvoID)
	}

	// Every staged crosspoint must now be live.
	for _, s := range slots {
		tally, err := pl.SendInterrogate(ctx, s.dst)
		if err != nil {
			t.Fatalf("post-go interrogate dst %d: %v", s.dst, err)
		}
		if tally.Source != s.src {
			t.Errorf("dst %d reads src %d after GO; want %d", s.dst, tally.Source, s.src)
		}
	}
}

// splitHostPort turns "127.0.0.1:54321" into ("127.0.0.1", 54321).
func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port %q: %v", portStr, err)
	}
	return host, port
}
