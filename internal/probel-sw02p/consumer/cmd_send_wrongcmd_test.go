package probelsw02p

import (
	"context"
	"testing"
	"time"

	"dhs/internal/probel-sw02p/codec"
)

// decoyThenReply runs the standard fake-matrix pattern: read the
// request, write a decoy frame the matcher must reject (wrong COMMAND),
// then write the real reply.
func decoyThenReply(t *testing.T, peer interface {
	Write([]byte) (int, error)
}, decoy, reply codec.Frame) {
	t.Helper()
	writeRaw(t, peer, decoy)
	writeRaw(t, peer, reply)
}

func writeRaw(t *testing.T, w interface{ Write([]byte) (int, error) }, f codec.Frame) {
	t.Helper()
	if _, err := w.Write(codec.Pack(f)); err != nil {
		t.Errorf("write frame %#x: %v", f.ID, err)
	}
}

// A tx 04 CONNECTED makes a convenient decoy for any matcher whose
// target reply is NOT tx 04.
func connectedDecoy() codec.Frame {
	return codec.EncodeConnected(codec.ConnectedParams{Destination: 0, Source: 0})
}

// A tx 03 TALLY is the decoy for matchers whose target IS tx 04.
func tallyDecoy() codec.Frame {
	return codec.EncodeTally(codec.TallyParams{Destination: 0, Source: 0})
}

func TestSendConnectIgnoresWrongCommand(t *testing.T) {
	p, peer := newPipePlugin(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = readFrame(t, peer)
		decoyThenReply(t, peer, tallyDecoy(),
			codec.EncodeConnected(codec.ConnectedParams{Destination: 7, Source: 8}))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cp, err := p.SendConnect(ctx, 7, 8, false)
	if err != nil {
		t.Fatalf("SendConnect: %v", err)
	}
	if cp.Destination != 7 {
		t.Errorf("dst = %d; want 7", cp.Destination)
	}
	<-done
}

func TestSendExtendedInterrogateIgnoresWrongCommand(t *testing.T) {
	p, peer := newPipePlugin(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = readFrame(t, peer)
		decoyThenReply(t, peer, connectedDecoy(),
			codec.EncodeExtendedTally(codec.ExtendedTallyParams{Destination: 5000, Source: 6}))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tl, err := p.SendExtendedInterrogate(ctx, 5000)
	if err != nil {
		t.Fatalf("SendExtendedInterrogate: %v", err)
	}
	if tl.Destination != 5000 {
		t.Errorf("dst = %d; want 5000", tl.Destination)
	}
	<-done
}

func TestSendExtendedConnectIgnoresWrongCommand(t *testing.T) {
	p, peer := newPipePlugin(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = readFrame(t, peer)
		decoyThenReply(t, peer, connectedDecoy(),
			codec.EncodeExtendedConnected(codec.ExtendedConnectedParams{Destination: 3000, Source: 4000}))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cp, err := p.SendExtendedConnect(ctx, 3000, 4000)
	if err != nil {
		t.Fatalf("SendExtendedConnect: %v", err)
	}
	if cp.Destination != 3000 {
		t.Errorf("dst = %d; want 3000", cp.Destination)
	}
	<-done
}

func TestSendExtendedProtectInterrogateIgnoresWrongCommand(t *testing.T) {
	p, peer := newPipePlugin(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = readFrame(t, peer)
		decoyThenReply(t, peer, connectedDecoy(),
			codec.EncodeExtendedProtectTally(codec.ExtendedProtectTallyParams{Destination: 600, Device: 5, Protect: codec.ProtectOEM}))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tl, err := p.SendExtendedProtectInterrogate(ctx, 600)
	if err != nil {
		t.Fatalf("SendExtendedProtectInterrogate: %v", err)
	}
	if tl.Destination != 600 {
		t.Errorf("dst = %d; want 600", tl.Destination)
	}
	<-done
}

func TestSendExtendedProtectConnectIgnoresWrongCommand(t *testing.T) {
	p, peer := newPipePlugin(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = readFrame(t, peer)
		decoyThenReply(t, peer, connectedDecoy(),
			codec.EncodeExtendedProtectConnected(codec.ExtendedProtectConnectedParams{Destination: 700, Device: 9}))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := p.SendExtendedProtectConnect(ctx, 700, 9)
	if err != nil {
		t.Fatalf("SendExtendedProtectConnect: %v", err)
	}
	if resp.Destination != 700 {
		t.Errorf("dst = %d; want 700", resp.Destination)
	}
	<-done
}

func TestSendExtendedProtectDisconnectIgnoresWrongCommand(t *testing.T) {
	p, peer := newPipePlugin(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = readFrame(t, peer)
		decoyThenReply(t, peer, connectedDecoy(),
			codec.EncodeExtendedProtectDisconnected(codec.ExtendedProtectDisconnectedParams{Destination: 700}))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := p.SendExtendedProtectDisconnect(ctx, 700, 9)
	if err != nil {
		t.Fatalf("SendExtendedProtectDisconnect: %v", err)
	}
	if resp.Destination != 700 {
		t.Errorf("dst = %d; want 700", resp.Destination)
	}
	<-done
}

func TestSendProtectDeviceNameRequestIgnoresWrongCommand(t *testing.T) {
	p, peer := newPipePlugin(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = readFrame(t, peer)
		decoyThenReply(t, peer, connectedDecoy(),
			codec.EncodeProtectDeviceNameResponse(codec.ProtectDeviceNameResponseParams{Device: 42, Name: "REAL"}))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := p.SendProtectDeviceNameRequest(ctx, 42)
	if err != nil {
		t.Fatalf("SendProtectDeviceNameRequest: %v", err)
	}
	if resp.Device != 42 {
		t.Errorf("device = %d; want 42", resp.Device)
	}
	<-done
}

// TestSendGoGroupSalvoIgnoresWrongCommand drives the rx 36 matcher's
// wrong-COMMAND early return (distinct from the wrong-SalvoID filter):
// a tx 04 decoy is rejected on the ID check before the SalvoID compare.
func TestSendGoGroupSalvoIgnoresWrongCommand(t *testing.T) {
	p, peer := newPipePlugin(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = readFrame(t, peer)
		decoyThenReply(t, peer, connectedDecoy(),
			codec.EncodeGoDoneGroupSalvoAck(codec.GoDoneGroupSalvoAckParams{Result: codec.GoGroupResultSet, SalvoID: 4}))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ack, err := p.SendGoGroupSalvo(ctx, codec.GoOpSet, 4)
	if err != nil {
		t.Fatalf("SendGoGroupSalvo: %v", err)
	}
	if ack.SalvoID != 4 {
		t.Errorf("ack.SalvoID = %d; want 4", ack.SalvoID)
	}
	<-done
}

// TestSendExtendedProtectTallyDumpRequestWriteError drives rx 105's
// write-error branch: the request uses a nil matcher (fire-and-forget),
// so the only error path is a failed underlying write. Closing the
// client first makes cli.Send → conn.Write fail.
func TestSendExtendedProtectTallyDumpRequestWriteError(t *testing.T) {
	p, peer := newPipePlugin(t)
	_ = peer.Close()
	if cli, err := p.ExposeClient(); err == nil {
		_ = cli.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := p.SendExtendedProtectTallyDumpRequest(ctx, 0, 4); err == nil {
		t.Error("SendExtendedProtectTallyDumpRequest on closed client = nil; want write error")
	}
}
