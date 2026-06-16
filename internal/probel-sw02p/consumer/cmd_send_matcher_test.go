package probelsw02p

import (
	"context"
	"testing"
	"time"

	"dhs/internal/probel-sw02p/codec"
)

// TestSendInterrogateIgnoresWrongCommand: the matrix first emits an
// unrelated frame (tx 04) the matcher must reject, then the real tx 03
// TALLY. Exercises the matcher's wrong-ID return-false branch.
func TestSendInterrogateIgnoresWrongCommand(t *testing.T) {
	p, peer := newPipePlugin(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = readFrame(t, peer)
		// Decoy: wrong command, the matcher must skip it.
		writeFrame(t, peer, codec.EncodeConnected(codec.ConnectedParams{Destination: 9, Source: 9}))
		// Real reply.
		writeFrame(t, peer, codec.EncodeTally(codec.TallyParams{Destination: 5, Source: 6}))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tally, err := p.SendInterrogate(ctx, 5)
	if err != nil {
		t.Fatalf("SendInterrogate: %v", err)
	}
	if tally.Destination != 5 || tally.Source != 6 {
		t.Errorf("tally = %+v; want dst=5 src=6", tally)
	}
	<-done
}

// TestSendInterrogateIgnoresWrongDestination: a tally for a different
// destination must be rejected by the matcher before the matching one
// arrives.
func TestSendInterrogateIgnoresWrongDestination(t *testing.T) {
	p, peer := newPipePlugin(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = readFrame(t, peer)
		writeFrame(t, peer, codec.EncodeTally(codec.TallyParams{Destination: 99, Source: 1})) // wrong dst
		writeFrame(t, peer, codec.EncodeTally(codec.TallyParams{Destination: 5, Source: 6}))  // right dst
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tally, err := p.SendInterrogate(ctx, 5)
	if err != nil {
		t.Fatalf("SendInterrogate: %v", err)
	}
	if tally.Destination != 5 {
		t.Errorf("tally.Destination = %d; want 5", tally.Destination)
	}
	<-done
}

// TestSendConnectIgnoresWrongDestination drives the dst/src mismatch
// branch of SendConnect's matcher.
func TestSendConnectIgnoresWrongDestination(t *testing.T) {
	p, peer := newPipePlugin(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = readFrame(t, peer)
		writeFrame(t, peer, codec.EncodeConnected(codec.ConnectedParams{Destination: 1, Source: 1})) // mismatch
		writeFrame(t, peer, codec.EncodeConnected(codec.ConnectedParams{Destination: 7, Source: 8})) // match
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cp, err := p.SendConnect(ctx, 7, 8, false)
	if err != nil {
		t.Fatalf("SendConnect: %v", err)
	}
	if cp.Destination != 7 || cp.Source != 8 {
		t.Errorf("cp = %+v; want dst=7 src=8", cp)
	}
	<-done
}

// TestSendExtendedInterrogateIgnoresWrongDestination drives the
// matcher's extended-tally dst filter.
func TestSendExtendedInterrogateIgnoresWrongDestination(t *testing.T) {
	p, peer := newPipePlugin(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = readFrame(t, peer)
		writeFrame(t, peer, codec.EncodeExtendedTally(codec.ExtendedTallyParams{Destination: 2000, Source: 1}))
		writeFrame(t, peer, codec.EncodeExtendedTally(codec.ExtendedTallyParams{Destination: 5000, Source: 6}))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tally, err := p.SendExtendedInterrogate(ctx, 5000)
	if err != nil {
		t.Fatalf("SendExtendedInterrogate: %v", err)
	}
	if tally.Destination != 5000 {
		t.Errorf("tally.Destination = %d; want 5000", tally.Destination)
	}
	<-done
}

// TestSendExtendedConnectIgnoresMismatch drives the dst/src mismatch in
// SendExtendedConnect's matcher.
func TestSendExtendedConnectIgnoresMismatch(t *testing.T) {
	p, peer := newPipePlugin(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = readFrame(t, peer)
		writeFrame(t, peer, codec.EncodeExtendedConnected(codec.ExtendedConnectedParams{Destination: 1, Source: 1}))
		writeFrame(t, peer, codec.EncodeExtendedConnected(codec.ExtendedConnectedParams{Destination: 3000, Source: 4000}))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cp, err := p.SendExtendedConnect(ctx, 3000, 4000)
	if err != nil {
		t.Fatalf("SendExtendedConnect: %v", err)
	}
	if cp.Destination != 3000 || cp.Source != 4000 {
		t.Errorf("cp = %+v; want dst=3000 src=4000", cp)
	}
	<-done
}

// TestSendExtendedProtectInterrogateIgnoresWrongDestination drives the
// tx 96 dst filter.
func TestSendExtendedProtectInterrogateIgnoresWrongDestination(t *testing.T) {
	p, peer := newPipePlugin(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = readFrame(t, peer)
		writeFrame(t, peer, codec.EncodeExtendedProtectTally(codec.ExtendedProtectTallyParams{Destination: 1}))
		writeFrame(t, peer, codec.EncodeExtendedProtectTally(codec.ExtendedProtectTallyParams{Destination: 600, Device: 5, Protect: codec.ProtectOEM}))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tl, err := p.SendExtendedProtectInterrogate(ctx, 600)
	if err != nil {
		t.Fatalf("SendExtendedProtectInterrogate: %v", err)
	}
	if tl.Destination != 600 || tl.Protect != codec.ProtectOEM {
		t.Errorf("tally = %+v; want dst=600 protect=OEM", tl)
	}
	<-done
}

// TestSendExtendedProtectConnectIgnoresWrongDestination drives the
// tx 97 dst filter.
func TestSendExtendedProtectConnectIgnoresWrongDestination(t *testing.T) {
	p, peer := newPipePlugin(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = readFrame(t, peer)
		writeFrame(t, peer, codec.EncodeExtendedProtectConnected(codec.ExtendedProtectConnectedParams{Destination: 1}))
		writeFrame(t, peer, codec.EncodeExtendedProtectConnected(codec.ExtendedProtectConnectedParams{Destination: 700, Device: 9, Protect: codec.ProtectProBel}))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := p.SendExtendedProtectConnect(ctx, 700, 9)
	if err != nil {
		t.Fatalf("SendExtendedProtectConnect: %v", err)
	}
	if resp.Destination != 700 {
		t.Errorf("resp.Destination = %d; want 700", resp.Destination)
	}
	<-done
}

// TestSendExtendedProtectDisconnectIgnoresWrongDestination drives the
// tx 98 dst filter.
func TestSendExtendedProtectDisconnectIgnoresWrongDestination(t *testing.T) {
	p, peer := newPipePlugin(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = readFrame(t, peer)
		writeFrame(t, peer, codec.EncodeExtendedProtectDisconnected(codec.ExtendedProtectDisconnectedParams{Destination: 1}))
		writeFrame(t, peer, codec.EncodeExtendedProtectDisconnected(codec.ExtendedProtectDisconnectedParams{Destination: 700}))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := p.SendExtendedProtectDisconnect(ctx, 700, 9)
	if err != nil {
		t.Fatalf("SendExtendedProtectDisconnect: %v", err)
	}
	if resp.Destination != 700 {
		t.Errorf("resp.Destination = %d; want 700", resp.Destination)
	}
	<-done
}

// TestSendProtectDeviceNameRequestIgnoresWrongDevice drives the tx 099
// device filter (matcher rejects a non-matching device number).
func TestSendProtectDeviceNameRequestIgnoresWrongDevice(t *testing.T) {
	p, peer := newPipePlugin(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = readFrame(t, peer)
		writeFrame(t, peer, codec.EncodeProtectDeviceNameResponse(codec.ProtectDeviceNameResponseParams{Device: 1, Name: "OTHER"}))
		writeFrame(t, peer, codec.EncodeProtectDeviceNameResponse(codec.ProtectDeviceNameResponseParams{Device: 42, Name: "REAL"}))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := p.SendProtectDeviceNameRequest(ctx, 42)
	if err != nil {
		t.Fatalf("SendProtectDeviceNameRequest: %v", err)
	}
	if resp.Device != 42 || resp.Name != "REAL" {
		t.Errorf("resp = %+v; want device=42 name=REAL", resp)
	}
	<-done
}

// TestSendGoGroupSalvoIgnoresWrongSalvoID drives the matcher's SalvoID
// filter: a tx 38 ack for a different group is skipped.
func TestSendGoGroupSalvoIgnoresWrongSalvoID(t *testing.T) {
	p, peer := newPipePlugin(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = readFrame(t, peer)
		writeFrame(t, peer, codec.EncodeGoDoneGroupSalvoAck(codec.GoDoneGroupSalvoAckParams{Result: codec.GoGroupResultSet, SalvoID: 1}))
		writeFrame(t, peer, codec.EncodeGoDoneGroupSalvoAck(codec.GoDoneGroupSalvoAckParams{Result: codec.GoGroupResultSet, SalvoID: 7}))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ack, err := p.SendGoGroupSalvo(ctx, codec.GoOpSet, 7)
	if err != nil {
		t.Fatalf("SendGoGroupSalvo: %v", err)
	}
	if ack.SalvoID != 7 {
		t.Errorf("ack.SalvoID = %d; want 7", ack.SalvoID)
	}
	<-done
}
