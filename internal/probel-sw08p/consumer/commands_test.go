package probelsw08p

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/probel-sw08p/codec"
)

// freshPlugin is an unconnected Plugin for the not-connected error paths.
func freshPlugin() *Plugin {
	return &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// assertTransportDecodeErr checks err is a *consumer.TransportError with
// Op=="decode" — the contract every per-command decode guard fulfils.
func assertTransportDecodeErr(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("want decode TransportError, got nil")
	}
	var te *consumer.TransportError
	if !errors.As(err, &te) {
		t.Fatalf("want *consumer.TransportError, got %T (%v)", err, err)
	}
	if te.Op != "decode" {
		t.Errorf("TransportError.Op = %q; want decode", te.Op)
	}
}

const testTimeout = 2 * time.Second

func ctxT(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), testTimeout)
}

// ---- rx 001 Crosspoint Interrogate ----------------------------------------

func TestCrosspointInterrogate(t *testing.T) {
	p, peer, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeCrosspointTally(codec.CrosspointTallyParams{
			MatrixID: 1, LevelID: 2, DestinationID: 42, SourceID: 7,
		}),
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()

	got, err := p.CrosspointInterrogate(ctx, 1, 2, 42)
	if err != nil {
		t.Fatalf("CrosspointInterrogate: %v", err)
	}
	if got.DestinationID != 42 || got.SourceID != 7 {
		t.Errorf("tally = dst=%d src=%d; want 42,7", got.DestinationID, got.SourceID)
	}
	req, ok := peer.lastRequest()
	if !ok || req.ID != codec.RxCrosspointInterrogate {
		t.Errorf("peer saw req ID %#x; want RxCrosspointInterrogate", req.ID)
	}
}

func TestCrosspointInterrogateNotConnected(t *testing.T) {
	_, err := freshPlugin().CrosspointInterrogate(context.Background(), 0, 0, 0)
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("err = %v; want ErrNotConnected", err)
	}
}

func TestCrosspointInterrogateDecodeErr(t *testing.T) {
	// Reply has the matched ID but a too-short payload → DecodeCrosspointTally
	// returns ErrShortPayload, exercising the decode guard.
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.Frame{ID: codec.TxCrosspointTally, Payload: []byte{0x00}},
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	_, err := p.CrosspointInterrogate(ctx, 0, 0, 0)
	assertTransportDecodeErr(t, err)
}

func TestCrosspointInterrogateTimeout(t *testing.T) {
	// Peer ACKs but never replies → ctx deadline fires in the reply phase.
	p, _, cleanup := newPeerHarness(t, ackOnly)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err := p.CrosspointInterrogate(ctx, 0, 0, 0)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v; want DeadlineExceeded", err)
	}
}

// ---- rx 002 Crosspoint Connect --------------------------------------------

func TestCrosspointConnect(t *testing.T) {
	p, peer, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeCrosspointConnected(codec.CrosspointConnectedParams{
			MatrixID: 0, LevelID: 0, DestinationID: 5, SourceID: 9,
		}),
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	got, err := p.CrosspointConnect(ctx, 0, 0, 5, 9)
	if err != nil {
		t.Fatalf("CrosspointConnect: %v", err)
	}
	if got.DestinationID != 5 || got.SourceID != 9 {
		t.Errorf("connected = dst=%d src=%d; want 5,9", got.DestinationID, got.SourceID)
	}
	req, _ := peer.lastRequest()
	if req.ID != codec.RxCrosspointConnect {
		t.Errorf("req ID %#x; want RxCrosspointConnect", req.ID)
	}
}

func TestCrosspointConnectNotConnected(t *testing.T) {
	_, err := freshPlugin().CrosspointConnect(context.Background(), 0, 0, 0, 0)
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("err = %v; want ErrNotConnected", err)
	}
}

func TestCrosspointConnectDecodeErr(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.Frame{ID: codec.TxCrosspointConnected, Payload: []byte{0x00}},
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	_, err := p.CrosspointConnect(ctx, 0, 0, 0, 0)
	assertTransportDecodeErr(t, err)
}

// ---- rx 007 Maintenance (no reply) ----------------------------------------

func TestMaintenance(t *testing.T) {
	p, peer, cleanup := newPeerHarness(t, ackOnly)
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	if err := p.Maintenance(ctx, codec.MaintClearProtects, 0xFF, 0xFF); err != nil {
		t.Fatalf("Maintenance: %v", err)
	}
	req, _ := peer.lastRequest()
	if req.ID != codec.RxMaintenance {
		t.Errorf("req ID %#x; want RxMaintenance", req.ID)
	}
}

func TestMaintenanceNotConnected(t *testing.T) {
	err := freshPlugin().Maintenance(context.Background(), codec.MaintSoftReset, 0, 0)
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("err = %v; want ErrNotConnected", err)
	}
}

func TestMaintenanceSendErr(t *testing.T) {
	// Close the client so Send fails on the write path → wrapped error.
	p, _, cleanup := newPeerHarness(t, ackOnly)
	cleanup() // closes the client + pipe before we Send
	err := p.Maintenance(context.Background(), codec.MaintSoftReset, 0, 0)
	if err == nil {
		t.Fatal("Maintenance after close returned nil; want send error")
	}
}

// ---- rx 008 Dual Controller Status ----------------------------------------

func TestDualControllerStatus(t *testing.T) {
	p, peer, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeDualControllerStatusResponse(codec.DualControllerStatusParams{
			SlaveActive: true, Active: true, IdleControllerFaulty: false,
		}),
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	got, err := p.DualControllerStatus(ctx)
	if err != nil {
		t.Fatalf("DualControllerStatus: %v", err)
	}
	if !got.SlaveActive || !got.Active || got.IdleControllerFaulty {
		t.Errorf("status = %+v; want slave+active, idle ok", got)
	}
	req, _ := peer.lastRequest()
	if req.ID != codec.RxDualControllerStatusRequest {
		t.Errorf("req ID %#x; want RxDualControllerStatusRequest", req.ID)
	}
}

func TestDualControllerStatusNotConnected(t *testing.T) {
	_, err := freshPlugin().DualControllerStatus(context.Background())
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("err = %v; want ErrNotConnected", err)
	}
}

func TestDualControllerStatusDecodeErr(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.Frame{ID: codec.TxDualControllerStatusResponse, Payload: []byte{0x00}},
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	_, err := p.DualControllerStatus(ctx)
	assertTransportDecodeErr(t, err)
}

// ---- rx 010 Protect Interrogate -------------------------------------------

func TestProtectInterrogate(t *testing.T) {
	p, peer, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeProtectTally(codec.ProtectTallyParams{
			MatrixID: 0, LevelID: 0, DestinationID: 3, DeviceID: 11, State: codec.ProtectProbel,
		}),
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	got, err := p.ProtectInterrogate(ctx, 0, 0, 3, 11)
	if err != nil {
		t.Fatalf("ProtectInterrogate: %v", err)
	}
	if got.State != codec.ProtectProbel || got.DeviceID != 11 {
		t.Errorf("got %+v; want state=Probel device=11", got)
	}
	req, _ := peer.lastRequest()
	if req.ID != codec.RxProtectInterrogate {
		t.Errorf("req ID %#x; want RxProtectInterrogate", req.ID)
	}
}

func TestProtectInterrogateNotConnected(t *testing.T) {
	_, err := freshPlugin().ProtectInterrogate(context.Background(), 0, 0, 0, 0)
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("err = %v; want ErrNotConnected", err)
	}
}

func TestProtectInterrogateDecodeErr(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.Frame{ID: codec.TxProtectTally, Payload: []byte{0x00}},
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	_, err := p.ProtectInterrogate(ctx, 0, 0, 0, 0)
	assertTransportDecodeErr(t, err)
}

// ---- rx 012 Protect Connect -----------------------------------------------

func TestProtectConnect(t *testing.T) {
	p, peer, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeProtectConnected(codec.ProtectConnectedParams{
			DestinationID: 4, DeviceID: 8, State: codec.ProtectProbel,
		}),
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	got, err := p.ProtectConnect(ctx, 0, 0, 4, 8)
	if err != nil {
		t.Fatalf("ProtectConnect: %v", err)
	}
	if got.DestinationID != 4 || got.DeviceID != 8 {
		t.Errorf("got %+v; want dst=4 device=8", got)
	}
	req, _ := peer.lastRequest()
	if req.ID != codec.RxProtectConnect {
		t.Errorf("req ID %#x; want RxProtectConnect", req.ID)
	}
}

func TestProtectConnectNotConnected(t *testing.T) {
	_, err := freshPlugin().ProtectConnect(context.Background(), 0, 0, 0, 0)
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("err = %v; want ErrNotConnected", err)
	}
}

func TestProtectConnectDecodeErr(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.Frame{ID: codec.TxProtectConnected, Payload: []byte{0x00}},
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	_, err := p.ProtectConnect(ctx, 0, 0, 0, 0)
	assertTransportDecodeErr(t, err)
}

// ---- rx 014 Protect Disconnect --------------------------------------------

func TestProtectDisconnect(t *testing.T) {
	p, peer, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeProtectDisconnected(codec.ProtectDisconnectedParams{
			DestinationID: 6, DeviceID: 2, State: codec.ProtectNone,
		}),
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	got, err := p.ProtectDisconnect(ctx, 0, 0, 6, 2)
	if err != nil {
		t.Fatalf("ProtectDisconnect: %v", err)
	}
	if got.DestinationID != 6 || got.State != codec.ProtectNone {
		t.Errorf("got %+v; want dst=6 state=None", got)
	}
	req, _ := peer.lastRequest()
	if req.ID != codec.RxProtectDisconnect {
		t.Errorf("req ID %#x; want RxProtectDisconnect", req.ID)
	}
}

func TestProtectDisconnectNotConnected(t *testing.T) {
	_, err := freshPlugin().ProtectDisconnect(context.Background(), 0, 0, 0, 0)
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("err = %v; want ErrNotConnected", err)
	}
}

func TestProtectDisconnectDecodeErr(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.Frame{ID: codec.TxProtectDisconnected, Payload: []byte{0x00}},
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	_, err := p.ProtectDisconnect(ctx, 0, 0, 0, 0)
	assertTransportDecodeErr(t, err)
}

// ---- rx 017 Protect Device Name -------------------------------------------

func TestProtectDeviceName(t *testing.T) {
	p, peer, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeProtectDeviceNameResponse(codec.ProtectDeviceNameResponseParams{
			DeviceID: 12, DeviceName: "CAM1",
		}),
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	name, err := p.ProtectDeviceName(ctx, 12)
	if err != nil {
		t.Fatalf("ProtectDeviceName: %v", err)
	}
	if name != "CAM1" {
		t.Errorf("name = %q; want CAM1", name)
	}
	req, _ := peer.lastRequest()
	if req.ID != codec.RxProtectDeviceNameRequest {
		t.Errorf("req ID %#x; want RxProtectDeviceNameRequest", req.ID)
	}
}

func TestProtectDeviceNameNotConnected(t *testing.T) {
	_, err := freshPlugin().ProtectDeviceName(context.Background(), 0)
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("err = %v; want ErrNotConnected", err)
	}
}

func TestProtectDeviceNameDecodeErr(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.Frame{ID: codec.TxProtectDeviceNameResponse, Payload: []byte{0x00}},
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	_, err := p.ProtectDeviceName(ctx, 0)
	assertTransportDecodeErr(t, err)
}

// ---- rx 019 Protect Tally Dump --------------------------------------------

func TestProtectTallyDump(t *testing.T) {
	p, peer, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeProtectTallyDump(codec.ProtectTallyDumpParams{
			MatrixID: 0, LevelID: 0, FirstDestinationID: 0,
			Items: []codec.ProtectTallyItem{
				{State: codec.ProtectProbel, DeviceID: 1},
				{State: codec.ProtectNone, DeviceID: 0},
			},
		}),
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	got, err := p.ProtectTallyDump(ctx, 0, 0, 0)
	if err != nil {
		t.Fatalf("ProtectTallyDump: %v", err)
	}
	if len(got.Items) != 2 || got.Items[0].DeviceID != 1 {
		t.Errorf("items = %+v; want 2 items, first device 1", got.Items)
	}
	req, _ := peer.lastRequest()
	if req.ID != codec.RxProtectTallyDumpRequest {
		t.Errorf("req ID %#x; want RxProtectTallyDumpRequest", req.ID)
	}
}

func TestProtectTallyDumpNotConnected(t *testing.T) {
	_, err := freshPlugin().ProtectTallyDump(context.Background(), 0, 0, 0)
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("err = %v; want ErrNotConnected", err)
	}
}

func TestProtectTallyDumpDecodeErr(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.Frame{ID: codec.TxProtectTallyDump, Payload: []byte{0x00}},
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	_, err := p.ProtectTallyDump(ctx, 0, 0, 0)
	assertTransportDecodeErr(t, err)
}

// ---- rx 021 Crosspoint Tally Dump (byte + word) ---------------------------

func TestCrosspointTallyDumpByte(t *testing.T) {
	p, peer, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeCrosspointTallyDumpByte(codec.CrosspointTallyDumpByteParams{
			MatrixID: 0, LevelID: 0, FirstDestinationID: 0,
			SourceIDs: []uint8{1, 2, 3},
		}),
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	got, err := p.CrosspointTallyDump(ctx, 0, 0)
	if err != nil {
		t.Fatalf("CrosspointTallyDump: %v", err)
	}
	if got.IsWord {
		t.Fatal("expected byte form")
	}
	if len(got.Byte.SourceIDs) != 3 || got.Byte.SourceIDs[2] != 3 {
		t.Errorf("byte srcs = %v; want [1 2 3]", got.Byte.SourceIDs)
	}
	req, _ := peer.lastRequest()
	if req.ID != codec.RxCrosspointTallyDumpRequest {
		t.Errorf("req ID %#x; want RxCrosspointTallyDumpRequest", req.ID)
	}
}

func TestCrosspointTallyDumpWord(t *testing.T) {
	// FirstDestinationID > 191 forces word promotion on the byte encoder;
	// build the word form directly so the reply is tx 023.
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeCrosspointTallyDumpWord(codec.CrosspointTallyDumpWordParams{
			MatrixID: 0, LevelID: 0, FirstDestinationID: 0,
			SourceIDs: []uint16{300, 1000},
		}),
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	got, err := p.CrosspointTallyDump(ctx, 0, 0)
	if err != nil {
		t.Fatalf("CrosspointTallyDump word: %v", err)
	}
	if !got.IsWord {
		t.Fatal("expected word form")
	}
	if len(got.Word.SourceIDs) != 2 || got.Word.SourceIDs[1] != 1000 {
		t.Errorf("word srcs = %v; want [300 1000]", got.Word.SourceIDs)
	}
}

func TestCrosspointTallyDumpNotConnected(t *testing.T) {
	_, err := freshPlugin().CrosspointTallyDump(context.Background(), 0, 0)
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("err = %v; want ErrNotConnected", err)
	}
}

func TestCrosspointTallyDumpByteDecodeErr(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.Frame{ID: codec.TxCrosspointTallyDumpByte, Payload: []byte{0x00}},
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	_, err := p.CrosspointTallyDump(ctx, 0, 0)
	assertTransportDecodeErr(t, err)
}

func TestCrosspointTallyDumpWordDecodeErr(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.Frame{ID: codec.TxCrosspointTallyDumpWord, Payload: []byte{0x00}},
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	_, err := p.CrosspointTallyDump(ctx, 0, 0)
	assertTransportDecodeErr(t, err)
}

// ---- rx 029 Master Protect Connect ----------------------------------------

func TestMasterProtectConnect(t *testing.T) {
	p, peer, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeProtectConnected(codec.ProtectConnectedParams{
			DestinationID: 7, DeviceID: 3, State: codec.ProtectProbelOver,
		}),
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	got, err := p.MasterProtectConnect(ctx, 0, 0, 7, 3)
	if err != nil {
		t.Fatalf("MasterProtectConnect: %v", err)
	}
	if got.State != codec.ProtectProbelOver {
		t.Errorf("state = %v; want ProbelOver", got.State)
	}
	req, _ := peer.lastRequest()
	if req.ID != codec.RxMasterProtectConnect {
		t.Errorf("req ID %#x; want RxMasterProtectConnect", req.ID)
	}
}

func TestMasterProtectConnectNotConnected(t *testing.T) {
	_, err := freshPlugin().MasterProtectConnect(context.Background(), 0, 0, 0, 0)
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("err = %v; want ErrNotConnected", err)
	}
}

func TestMasterProtectConnectDecodeErr(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.Frame{ID: codec.TxProtectConnected, Payload: []byte{0x00}},
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	_, err := p.MasterProtectConnect(ctx, 0, 0, 0, 0)
	assertTransportDecodeErr(t, err)
}
