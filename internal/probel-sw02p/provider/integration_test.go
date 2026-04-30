package probelsw02p_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	consumer "acp/internal/probel-sw02p/consumer"
	"acp/internal/probel-sw02p/codec"
	provider "acp/internal/probel-sw02p/provider"
	"acp/internal/export/canonical"
)

var _ = os.Getenv

// TestIntegrationSalvoBroadcastsConnectedAcrossSessions is the
// end-to-end contract test for the SW-P-02 salvo family. Two
// consumers (A and B) connect to a real TCP listener backing a fresh
// provider; A stages three crosspoints via rx 35 + fires the group
// via rx 36. The test then verifies that B — which never sent a
// frame — still receives the three tx 04 CONNECTED broadcasts and
// the tx 38 GO DONE GROUP SALVO ACK, exercising the full fan-out
// path from pairs 3/4. Also smoke-tests the length-aware Unpack
// scanner against a multi-frame stream.
func TestIntegrationSalvoBroadcastsConnectedAcrossSessions(t *testing.T) {
	// Set DHS_TEST_VERBOSE=1 to stream provider + consumer logs to
	// stderr while diagnosing fan-out races; default is silent.
	var logger *slog.Logger
	if os.Getenv("DHS_TEST_VERBOSE") == "1" {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	} else {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	// Minimal canonical tree so the provider's tree is non-nil.
	exp := &canonical.Export{
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
						TargetCount: 4, SourceCount: 4,
						Labels: []canonical.MatrixLabel{{BasePath: "router.matrix-0.video"}},
					},
				},
			},
		},
	}

	srv := &provider.Factory{}
	prov := srv.New(logger, exp)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close probe listener: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- prov.Serve(ctx, addr)
	}()

	// Wait up to 1 s for the listener to come online.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		c, derr := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if derr == nil {
			_ = c.Close()
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	// Two consumer plugins connect to the same provider.
	fA := &consumer.Factory{}
	fB := &consumer.Factory{}
	plA, _ := fA.New(logger).(*consumer.Plugin)
	plB, _ := fB.New(logger).(*consumer.Plugin)

	host, port := splitHostPort(t, addr)
	if err := plA.Connect(ctx, host, port); err != nil {
		t.Fatalf("A connect: %v", err)
	}
	defer func() { _ = plA.Disconnect() }()
	if err := plB.Connect(ctx, host, port); err != nil {
		t.Fatalf("B connect: %v", err)
	}
	defer func() { _ = plB.Disconnect() }()

	// Subscribe B to every incoming frame. We need to observe the
	// fan-out from A's rx 36 GO command. Also subscribe A for
	// diagnostic visibility — it does not affect the Send validator
	// path (the validator gets first look via client.dispatch).
	clientB, err := plB.ExposeClient()
	if err != nil {
		t.Fatalf("B expose client: %v", err)
	}
	clientA, err := plA.ExposeClient()
	if err != nil {
		t.Fatalf("A expose client: %v", err)
	}
	var (
		bMu        sync.Mutex
		bConnected []codec.ConnectedParams
		bGoDone    *codec.GoDoneGroupSalvoAckParams

		aMu        sync.Mutex
		aSawGoDone bool
	)
	clientB.Subscribe(func(f codec.Frame) {
		switch f.ID {
		case codec.TxCrosspointConnected:
			if p, derr := codec.DecodeConnected(f); derr == nil {
				bMu.Lock()
				bConnected = append(bConnected, p)
				bMu.Unlock()
			}
		case codec.TxGoDoneGroupSalvoAck:
			if p, derr := codec.DecodeGoDoneGroupSalvoAck(f); derr == nil {
				bMu.Lock()
				bGoDone = &p
				bMu.Unlock()
			}
		}
	})
	clientA.Subscribe(func(f codec.Frame) {
		if f.ID == codec.TxGoDoneGroupSalvoAck {
			aMu.Lock()
			aSawGoDone = true
			aMu.Unlock()
		}
	})
	_ = aSawGoDone

	// A stages three crosspoints under salvo 5.
	sendCtx, sendCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer sendCancel()
	for _, s := range []struct{ dst, src uint16 }{{0, 1}, {1, 2}, {2, 3}} {
		if _, err := plA.SendConnectOnGoGroupSalvo(sendCtx, s.dst, s.src, 5, false); err != nil {
			t.Fatalf("A stage (%d,%d): %v", s.dst, s.src, err)
		}
	}

	// A fires salvo 5 via rx 36. The tx 38 ack comes back on A's
	// validator, but the tx 04 + tx 38 broadcasts also hit B.
	if _, err := plA.SendGoGroupSalvo(sendCtx, codec.GoOpSet, 5); err != nil {
		aMu.Lock()
		saw := aSawGoDone
		aMu.Unlock()
		t.Fatalf("A fire: %v (subscribe saw tx 38 on A? %v)", err, saw)
	}

	// Wait up to 2 s for B to observe 3 × tx 04 + 1 × tx 38.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		bMu.Lock()
		ready := len(bConnected) >= 3 && bGoDone != nil
		bMu.Unlock()
		if ready {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	bMu.Lock()
	defer bMu.Unlock()
	if len(bConnected) != 3 {
		t.Fatalf("B received %d tx 04 frames; want 3", len(bConnected))
	}
	for i, want := range []struct{ dst, src uint16 }{{0, 1}, {1, 2}, {2, 3}} {
		if bConnected[i].Destination != want.dst || bConnected[i].Source != want.src {
			t.Errorf("B tx 04 [%d] = (%d, %d); want (%d, %d)",
				i, bConnected[i].Destination, bConnected[i].Source,
				want.dst, want.src)
		}
	}
	if bGoDone == nil {
		t.Fatal("B never received tx 38")
	}
	if bGoDone.Result != codec.GoGroupResultSet || bGoDone.SalvoID != 5 {
		t.Errorf("B tx 38 = %+v; want Result=Set SalvoID=5", *bGoDone)
	}

	cancel()
	select {
	case <-serveDone:
	case <-time.After(2 * time.Second):
		t.Error("provider Serve did not return after ctx cancel")
	}
}

// splitHostPort turns "127.0.0.1:54321" into ("127.0.0.1", 54321).
func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", addr, err)
	}
	var port int
	if _, err := scanInt(portStr, &port); err != nil {
		t.Fatalf("parse port %q: %v", portStr, err)
	}
	return host, port
}

// scanInt is a tiny fmt.Sscanf wrapper used by the helper above.
func scanInt(s string, out *int) (int, error) {
	var n int
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, &scanErr{s: s}
		}
		n = n*10 + int(c-'0')
	}
	*out = n
	return 1, nil
}

type scanErr struct{ s string }

func (e *scanErr) Error() string { return "invalid int: " + e.s }
