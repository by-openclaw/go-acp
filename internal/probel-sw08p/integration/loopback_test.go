//go:build integration

// Loopback integration test: our own SW-P-08 provider (the matrix
// emulator) backs a real TCP listener; our own SW-P-08 consumer dials it
// and drives the full codec → session → handler → fan-out stack over the
// wire. Exercises encode → TCP (DLE STX framing + DLE-stuffing + §2
// ACK/NAK) → decode → handler → broadcast → await-matcher end-to-end. No
// external device required; the provider IS the matrix emulator, so the
// loopback body is CI-safe.
//
// SW-P-08 is level-scoped: every crosspoint / protect / name command
// carries <matrix, level, dst, src>. These tests fix matrix=0 / level=0
// (the single (matrix, level) the loopback emulator serves) and assert
// real round-trips against it.
//
// External mode: set PROBEL_SW08P_TEST_HOST (optionally
// PROBEL_SW08P_TEST_PORT, default 2008) to point the same consumer
// round-trips at a real SW-P-08 matrix / vendor emulator on the lab
// network instead of the in-process provider. Mirrors the env-var gating
// used by the osc / tsl / acp1 / probel-sw02p integration bodies.
//
// Run with:
//
//	go test -tags integration ./internal/probel-sw08p/integration/...
//
// Externally:
//
//	PROBEL_SW08P_TEST_HOST=10.100.0.42 \
//	  go test -tags integration ./internal/probel-sw08p/integration/...
package probelsw08p_integration

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
	"dhs/internal/probel-sw08p/codec"
	consumer "dhs/internal/probel-sw08p/consumer"
	provider "dhs/internal/probel-sw08p/provider"
)

// matrixDsts / matrixSrcs bound the served (matrix, level). The provider
// rejects connects / salvo slots whose dst >= targetCount or
// src >= sourceCount (tree.applyConnect), so every test dst/src must stay
// inside these bounds. Keep the addresses below well inside them.
const (
	matrixDsts = 16
	matrixSrcs = 16
)

// matrixID / matrixLevel are the single (matrix, level) the loopback
// emulator serves. SW-P-08 commands are level-scoped; these pin the
// scope every test drives.
const (
	matrixID    = uint8(0)
	matrixLevel = uint8(0)
)

const externalHostEnv = "PROBEL_SW08P_TEST_HOST"
const externalPortEnv = "PROBEL_SW08P_TEST_PORT"

// testEnv carries the per-test consumer endpoint. In loopback mode the
// provider is started in-process; in external mode it points at the lab
// host.
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
// — a single level-scoped 1-to-N matrix of matrixDsts × matrixSrcs. This
// is the exact shape committed under testdata/exports/matrix_tree.json
// (see fixture_test.go), so the loopback emulator and the fixture never
// drift.
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
						Labels:      []canonical.MatrixLabel{{BasePath: "router.matrix-0.level-0"}},
					},
				},
			},
		},
	}
}

// startLoopbackProvider binds a fresh provider to an ephemeral localhost
// port and returns the bound endpoint. The provider is torn down when the
// test finishes via t.Cleanup.
func startLoopbackProvider(t *testing.T, logger *slog.Logger) testEnv {
	t.Helper()

	f := &provider.Factory{}
	prov := f.New(logger, servedExport())

	// Probe an ephemeral port, then hand the address to Serve. Mirrors
	// the close-then-rebind pattern used by the sw02p integration test;
	// the 1 s dial-wait below tolerates the rebind window.
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

// dialEnv resolves the consumer endpoint. When PROBEL_SW08P_TEST_HOST is
// set, it targets that lab host (no in-process provider); otherwise it
// spins up the loopback emulator. The consumer issues exactly the frames
// each test drives — SW-P-08 has no bootstrap sweep, so there is no
// background rx noise to suppress (the only async traffic is the matrix's
// keepalive ping, auto-answered by the plugin's keepalive responder).
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
		t.Logf("external mode: SW-P-08 matrix at %s:%d", host, port)
	} else {
		env = startLoopbackProvider(t, logger)
	}

	f := &consumer.Factory{}
	pl, ok := f.New(logger).(*consumer.Plugin)
	if !ok {
		t.Fatal("consumer Factory.New did not return *Plugin")
	}
	// Pin the matrix shape so the consumer's connect-source validation
	// matches the served (matrix, level).
	pl.SetMatrixConfig(consumer.MatrixConfig{
		MatrixID: matrixID,
		Level:    matrixLevel,
		Dsts:     matrixDsts,
		Srcs:     matrixSrcs,
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

// TestConnectInterrogateRoundTrip drives rx 002 Crosspoint Connect then
// rx 001 Crosspoint Interrogate on the same destination and asserts the
// interrogate reads back the source the connect just set — the basic "set
// a crosspoint and read it back" contract over a real TCP round-trip.
func TestConnectInterrogateRoundTrip(t *testing.T) {
	pl, closer := dialEnv(t)
	defer closer()

	const dst, src = uint16(2), uint16(5)

	ctx, cancel := sendCtx(t)
	defer cancel()

	cp, err := pl.CrosspointConnect(ctx, matrixID, matrixLevel, dst, src)
	if err != nil {
		t.Fatalf("CrosspointConnect(%d,%d): %v", dst, src, err)
	}
	if cp.DestinationID != dst || cp.SourceID != src {
		t.Fatalf("tx 04 = (%d,%d); want (%d,%d)", cp.DestinationID, cp.SourceID, dst, src)
	}

	tally, err := pl.CrosspointInterrogate(ctx, matrixID, matrixLevel, dst)
	if err != nil {
		t.Fatalf("CrosspointInterrogate(%d): %v", dst, err)
	}
	if tally.DestinationID != dst {
		t.Errorf("tx 03 dst = %d; want %d", tally.DestinationID, dst)
	}
	if tally.SourceID != src {
		t.Errorf("tx 03 src = %d; want %d (route did not stick)", tally.SourceID, src)
	}
}

// TestConnectFanOutToSecondSession drives a single rx 002 Crosspoint
// Connect from session A and asserts the matrix fans out the tx 003
// Crosspoint Tally to a SECOND consumer session that never sent a connect
// — exercising the cross-session broadcast path (§3.2.3 "issued on all
// ports"). Note the originator gets a tx 004 Connected confirm, while
// other sessions get the tx 003 Tally fan-out (see provider
// handleCrosspointConnect), so B subscribes for the tally.
func TestConnectFanOutToSecondSession(t *testing.T) {
	logger := testLogger()

	// This cross-session assertion is loopback-only — it needs two
	// sessions against the SAME emulator instance and observes the
	// fan-out. Against an external matrix we cannot guarantee a second
	// idle controller, so target only the loopback provider here.
	if os.Getenv(externalHostEnv) != "" {
		t.Skip("cross-session fan-out assertion is loopback-only; skipping in external mode")
	}
	env := startLoopbackProvider(t, logger)

	cfg := consumer.MatrixConfig{MatrixID: matrixID, Level: matrixLevel, Dsts: matrixDsts, Srcs: matrixSrcs}

	// Controller A: the active driver.
	fa := &consumer.Factory{}
	plA := fa.New(logger).(*consumer.Plugin)
	plA.SetMatrixConfig(cfg)

	// Controller B: a passive observer subscribed to every frame.
	fb := &consumer.Factory{}
	plB := fb.New(logger).(*consumer.Plugin)
	plB.SetMatrixConfig(cfg)

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
	got := make(chan codec.CrosspointTallyParams, 4)
	clientB.Subscribe(func(f codec.Frame) {
		if f.ID != codec.TxCrosspointTally && f.ID != codec.TxCrosspointTallyExt {
			return
		}
		if p, derr := codec.DecodeCrosspointTally(f); derr == nil {
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
	if _, err := plB.CrosspointInterrogate(sctx, matrixID, matrixLevel, 0); err != nil {
		t.Fatalf("B warm-up interrogate: %v", err)
	}

	if _, err := plA.CrosspointConnect(sctx, matrixID, matrixLevel, dst, src); err != nil {
		t.Fatalf("A CrosspointConnect: %v", err)
	}

	select {
	case p := <-got:
		if p.DestinationID != dst || p.SourceID != src {
			t.Errorf("B observed tx 04 (%d,%d); want (%d,%d)", p.DestinationID, p.SourceID, dst, src)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("B never observed the tx 04 tally fan-out")
	}
}

// TestTallyDump drives rx 021 Crosspoint Tally Dump Request after seeding
// two routes, and asserts the matrix returns a single tx 022 (byte form,
// since matrixDsts < 256) dump whose decoded body reflects the seeded
// crosspoints. Exercises the dump-request → dump-reply round-trip end to
// end (the streaming multi-frame path is covered by the provider's own
// TestIntegrationStreamingTallyDump).
func TestTallyDump(t *testing.T) {
	pl, closer := dialEnv(t)
	defer closer()

	ctx, cancel := sendCtx(t)
	defer cancel()

	seed := []struct{ dst, src uint16 }{{1, 4}, {7, 2}}
	for _, s := range seed {
		if _, err := pl.CrosspointConnect(ctx, matrixID, matrixLevel, s.dst, s.src); err != nil {
			t.Fatalf("seed connect (%d,%d): %v", s.dst, s.src, err)
		}
	}

	res, err := pl.CrosspointTallyDump(ctx, matrixID, matrixLevel)
	if err != nil {
		t.Fatalf("CrosspointTallyDump: %v", err)
	}
	if res.IsWord {
		t.Fatalf("dump returned word form; want byte form for %d dsts", matrixDsts)
	}
	// matrixDsts (16) < the 128-tally byte-form chunk, so the whole level
	// arrives in a single frame starting at destination 0.
	if res.Byte.FirstDestinationID != 0 {
		t.Errorf("dump FirstDestinationID = %d; want 0", res.Byte.FirstDestinationID)
	}
	if len(res.Byte.SourceIDs) != matrixDsts {
		t.Fatalf("dump carried %d source slots; want %d", len(res.Byte.SourceIDs), matrixDsts)
	}
	for _, s := range seed {
		if got := res.Byte.SourceIDs[s.dst]; got != uint8(s.src) {
			t.Errorf("dump dst %d = src %d; want %d", s.dst, got, s.src)
		}
	}
}

// TestProtectCycle drives the full protect lifecycle over the wire:
// interrogate (unprotected) → rx 012 Protect Connect → interrogate
// (Pro-Bel protected, owner echoed) → rx 014 Protect Disconnect →
// interrogate (unprotected again). Pins the owner-only authority model's
// happy path end-to-end.
func TestProtectCycle(t *testing.T) {
	pl, closer := dialEnv(t)
	defer closer()

	const dst, device = uint16(4), uint16(9)

	ctx, cancel := sendCtx(t)
	defer cancel()

	// 1. Start unprotected.
	pre, err := pl.ProtectInterrogate(ctx, matrixID, matrixLevel, dst, device)
	if err != nil {
		t.Fatalf("pre interrogate: %v", err)
	}
	if pre.State != codec.ProtectNone {
		t.Fatalf("pre-protect state = %d; want ProtectNone — test dst dirty", pre.State)
	}

	// 2. Protect for our device.
	on, err := pl.ProtectConnect(ctx, matrixID, matrixLevel, dst, device)
	if err != nil {
		t.Fatalf("protect-connect: %v", err)
	}
	if on.State != codec.ProtectProbel {
		t.Errorf("after protect-connect state = %d; want ProtectProbel", on.State)
	}
	if on.DestinationID != dst || on.DeviceID != device {
		t.Errorf("tx 013 = (dst=%d, dev=%d); want (dst=%d, dev=%d)",
			on.DestinationID, on.DeviceID, dst, device)
	}

	// 3. Interrogate confirms the protect stuck with the right owner.
	mid, err := pl.ProtectInterrogate(ctx, matrixID, matrixLevel, dst, device)
	if err != nil {
		t.Fatalf("mid interrogate: %v", err)
	}
	if mid.State != codec.ProtectProbel || mid.DeviceID != device {
		t.Errorf("mid interrogate = (state=%d, dev=%d); want (ProtectProbel, %d)",
			mid.State, mid.DeviceID, device)
	}

	// 4. Disconnect by the owning device.
	off, err := pl.ProtectDisconnect(ctx, matrixID, matrixLevel, dst, device)
	if err != nil {
		t.Fatalf("protect-disconnect: %v", err)
	}
	if off.State != codec.ProtectNone {
		t.Errorf("after protect-disconnect state = %d; want ProtectNone", off.State)
	}

	// 5. Interrogate confirms cleared.
	post, err := pl.ProtectInterrogate(ctx, matrixID, matrixLevel, dst, device)
	if err != nil {
		t.Fatalf("post interrogate: %v", err)
	}
	if post.State != codec.ProtectNone {
		t.Errorf("post interrogate state = %d; want ProtectNone", post.State)
	}
}

// TestDualControllerStatus drives rx 008 Dual Controller Status Request
// and asserts the single-controller emulator answers Master / active /
// idle OK per its documented single-controller-by-construction policy.
func TestDualControllerStatus(t *testing.T) {
	pl, closer := dialEnv(t)
	defer closer()

	ctx, cancel := sendCtx(t)
	defer cancel()

	resp, err := pl.DualControllerStatus(ctx)
	if err != nil {
		t.Fatalf("DualControllerStatus: %v", err)
	}
	// External matrices with a real redundant pair may legitimately
	// report Slave-active / idle-faulty — only assert the exact shape
	// against our own loopback emulator.
	if os.Getenv(externalHostEnv) == "" {
		if resp.SlaveActive {
			t.Errorf("SlaveActive = true; want false (Master active)")
		}
		if !resp.Active {
			t.Errorf("Active = false; want true (we are the active controller)")
		}
		if resp.IdleControllerFaulty {
			t.Errorf("IdleControllerFaulty = true; want false (no idle peer flagged)")
		}
	}
}

// TestSalvoConnectOnGoThenGo stages three crosspoints under a salvo group
// via rx 120 Crosspoint Connect-On-Go Salvo, fires the group via rx 121
// Crosspoint Go Salvo (op=set), and asserts the matrix returns tx 123
// Go-Done Salvo Ack (status=Set) and that every staged crosspoint reads
// back via a follow-up rx 001 Interrogate — the salvo commit path end to
// end.
func TestSalvoConnectOnGoThenGo(t *testing.T) {
	pl, closer := dialEnv(t)
	defer closer()

	ctx, cancel := sendCtx(t)
	defer cancel()

	const salvoID = uint8(5)
	slots := []struct{ dst, src uint16 }{{0, 1}, {1, 2}, {2, 3}}

	// Clear first so the salvo group starts empty regardless of prior
	// state on an external matrix.
	if _, err := pl.SalvoGo(ctx, codec.SalvoGoParams{Op: codec.SalvoOpClear, SalvoID: salvoID}); err != nil {
		t.Fatalf("pre-clear salvo %d: %v", salvoID, err)
	}

	for _, s := range slots {
		ack, err := pl.SalvoConnectOnGo(ctx, codec.SalvoConnectOnGoParams{
			MatrixID: matrixID, LevelID: matrixLevel,
			DestinationID: s.dst, SourceID: s.src, SalvoID: salvoID,
		})
		if err != nil {
			t.Fatalf("stage (%d,%d) salvo %d: %v", s.dst, s.src, salvoID, err)
		}
		if ack.DestinationID != s.dst || ack.SourceID != s.src || ack.SalvoID != salvoID {
			t.Errorf("tx 122 ack = (%d,%d,salvo=%d); want (%d,%d,salvo=%d)",
				ack.DestinationID, ack.SourceID, ack.SalvoID, s.dst, s.src, salvoID)
		}
	}

	done, err := pl.SalvoGo(ctx, codec.SalvoGoParams{Op: codec.SalvoOpSet, SalvoID: salvoID})
	if err != nil {
		t.Fatalf("fire salvo %d: %v", salvoID, err)
	}
	if done.Status != codec.SalvoDoneSet || done.SalvoID != salvoID {
		t.Errorf("tx 123 = %+v; want Status=SalvoDoneSet SalvoID=%d", done, salvoID)
	}

	// Every staged crosspoint must now be live.
	for _, s := range slots {
		tally, err := pl.CrosspointInterrogate(ctx, matrixID, matrixLevel, s.dst)
		if err != nil {
			t.Fatalf("post-go interrogate dst %d: %v", s.dst, err)
		}
		if tally.SourceID != s.src {
			t.Errorf("dst %d reads src %d after GO; want %d", s.dst, tally.SourceID, s.src)
		}
	}
}

// TestSourceNameRoundTrip drives rx 100 All Source Names then rx 101
// Single Source Name and asserts both reply with the emulator's
// positional default names ("SRC 0001" …). The served export declares no
// explicit source labels, so the provider synthesises stable positional
// strings — this exercises the full name-request → name-response codec +
// handler path over a real TCP round-trip.
func TestSourceNameRoundTrip(t *testing.T) {
	pl, closer := dialEnv(t)
	defer closer()

	ctx, cancel := sendCtx(t)
	defer cancel()

	// All source names — first frame, 8-char width (16 names fit one frame).
	all, err := pl.AllSourceNames(ctx, matrixID, matrixLevel, codec.NameLen8)
	if err != nil {
		t.Fatalf("AllSourceNames: %v", err)
	}
	if len(all.Names) == 0 {
		t.Fatal("all-source-names returned an empty name table")
	}
	// External matrices carry real source labels; only assert the
	// positional-default contract against our own loopback emulator.
	if os.Getenv(externalHostEnv) == "" {
		if all.Names[0] != "SRC 0001" {
			t.Errorf("all-source-names[0] = %q; want %q", all.Names[0], "SRC 0001")
		}
	}

	// Single source name — source index 4 → "SRC 0005".
	const src = uint16(4)
	name, err := pl.SingleSourceName(ctx, matrixID, matrixLevel, codec.NameLen8, src)
	if err != nil {
		t.Fatalf("SingleSourceName(%d): %v", src, err)
	}
	if name == "" {
		t.Fatalf("single-source-name(%d) returned empty", src)
	}
	if os.Getenv(externalHostEnv) == "" {
		if name != "SRC 0005" {
			t.Errorf("single-source-name(%d) = %q; want %q", src, name, "SRC 0005")
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
