package probelsw02p

import (
	"context"
	"net"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"dhs/internal/probel-sw02p/codec"
	"dhs/internal/transport"
)

// TestConnectPortZeroAndObservers drives Connect's full happy path with
// observable traffic: port 0 resolves to DefaultPort against a loopback
// listener bound to that port, a recorder is attached so the rx/tx
// recorder wrappers run, a real SendInterrogate round-trip fires the
// per-command tx/rx metrics observers, and a raw sub-frame Write
// exercises the OnTx "not a frame" fallback branch.
func TestConnectPortZeroAndObservers(t *testing.T) {
	// Bind exactly DefaultPort on loopback so a port==0 Connect lands
	// here. If the port is busy on this host, skip rather than fail.
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(DefaultPort)))
	if err != nil {
		t.Skipf("DefaultPort %d unavailable on this host: %v", DefaultPort, err)
	}
	defer func() { _ = ln.Close() }()

	matrixReady := make(chan struct{})
	go func() {
		close(matrixReady)
		conn, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		// Read the rx 01 INTERROGATE, reply with tx 03 TALLY.
		buf := make([]byte, 0, 32)
		tmp := make([]byte, 32)
		for {
			n, rerr := conn.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
				if f, _, perr := codec.Unpack(buf); perr == nil && f.ID == codec.RxInterrogate {
					q, _ := codec.DecodeInterrogate(f)
					_, _ = conn.Write(codec.Pack(codec.EncodeTally(codec.TallyParams{
						Destination: q.Destination, Source: 11,
					})))
					break
				}
			}
			if rerr != nil {
				return
			}
		}
		// Hold open for the rest of the test.
		hold := make([]byte, 32)
		for {
			if _, herr := conn.Read(hold); herr != nil {
				return
			}
		}
	}()
	<-matrixReady

	recPath := filepath.Join(t.TempDir(), "frames.jsonl")
	rec, err := transport.NewRecorder(recPath)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	t.Cleanup(func() { _ = rec.Close() })

	p := &Plugin{logger: quietLogger()}
	p.SetRecorder(rec)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := p.Connect(ctx, "127.0.0.1", 0); err != nil { // port 0 → DefaultPort
		t.Fatalf("Connect(port 0): %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	if p.port != DefaultPort {
		t.Errorf("p.port = %d after port-0 Connect; want DefaultPort %d", p.port, DefaultPort)
	}

	// Real round-trip → fires ObserveCmdTx (rx 01) + ObserveCmdRx (tx 03)
	// and the recorder tx/rx wrappers.
	tally, err := p.SendInterrogate(ctx, 4)
	if err != nil {
		t.Fatalf("SendInterrogate: %v", err)
	}
	if tally.Destination != 4 || tally.Source != 11 {
		t.Errorf("tally = %+v; want dst=4 src=11", tally)
	}

	// Raw sub-frame Write → OnTx "not a frame" fallback (ObserveTx).
	cli, err := p.ExposeClient()
	if err != nil {
		t.Fatalf("ExposeClient: %v", err)
	}
	if err := cli.Write([]byte{0x01}); err != nil {
		t.Fatalf("raw Write: %v", err)
	}

	// Metrics must reflect both tx frames + the rx tally.
	snap := p.Metrics().Snapshot()
	if snap.TxFrames == 0 || snap.RxFrames == 0 {
		t.Errorf("metrics tx/rx frames = (%d, %d); want both > 0", snap.TxFrames, snap.RxFrames)
	}
}

// TestIsOnlineWithinNoRxYet drives the IsOnlineWithin branch where the
// metrics connector exists but no rx frame has been seen — LastRxAt is
// zero so the plugin reports offline despite being connected.
func TestIsOnlineWithinNoRxYet(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	accepted := make(chan struct{})
	go func() {
		conn, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		close(accepted)
		// Never write anything — keep LastRxAt zero.
		buf := make([]byte, 32)
		for {
			if _, rerr := conn.Read(buf); rerr != nil {
				_ = conn.Close()
				return
			}
		}
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	p := &Plugin{logger: quietLogger()}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := p.Connect(ctx, "127.0.0.1", port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()
	<-accepted

	if p.Metrics() == nil {
		t.Fatal("Metrics() nil after Connect")
	}
	// Connected, metrics present, but no rx → offline.
	if p.IsOnlineWithin(time.Hour) {
		t.Error("IsOnlineWithin(1h) = true with no rx yet; want false")
	}
}
