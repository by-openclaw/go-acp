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

// TestSendProtectDeviceNameRequestRoundTrip drives SendProtectDevice
// NameRequest through a net.Pipe with a fake matrix that replies
// tx 099 carrying a device name.
func TestSendProtectDeviceNameRequestRoundTrip(t *testing.T) {
	a, b := net.Pipe()
	defer func() {
		_ = a.Close()
		_ = b.Close()
	}()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	disable := false
	client := codec.NewClientFromConn(a, logger, codec.ClientConfig{WireHexLog: &disable})

	// rx 103 = 5 bytes.
	matrixDone := make(chan struct{})
	go func() {
		defer close(matrixDone)
		buf := make([]byte, 5)
		if _, err := io.ReadFull(b, buf); err != nil {
			t.Errorf("fake matrix read: %v", err)
			return
		}
		req, _, err := codec.Unpack(buf)
		if err != nil || req.ID != codec.RxProtectDeviceNameRequest {
			t.Errorf("fake matrix unpack got=%+v err=%v", req, err)
			return
		}
		p, _ := codec.DecodeProtectDeviceNameRequest(req)
		reply := codec.EncodeProtectDeviceNameResponse(codec.ProtectDeviceNameResponseParams{
			Device: p.Device, Name: "DEV-7A",
		})
		if _, werr := b.Write(codec.Pack(reply)); werr != nil {
			t.Errorf("fake matrix write: %v", werr)
		}
	}()

	p := &Plugin{logger: logger}
	p.client = client

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := p.SendProtectDeviceNameRequest(ctx, 7)
	if err != nil {
		t.Fatalf("SendProtectDeviceNameRequest: %v", err)
	}
	if resp.Device != 7 || resp.Name != "DEV-7A" {
		t.Errorf("resp = %+v; want {Device:7 Name:DEV-7A}", resp)
	}

	<-matrixDone
	_ = client.Close()
}

// TestSubscribeProtectDeviceNameRequest confirms a matrix-initiated
// rx 103 (asking the controller "who are you?") reaches the
// registered listener.
func TestSubscribeProtectDeviceNameRequest(t *testing.T) {
	a, b := net.Pipe()
	defer func() {
		_ = a.Close()
		_ = b.Close()
	}()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	disable := false
	client := codec.NewClientFromConn(a, logger, codec.ClientConfig{WireHexLog: &disable})

	p := &Plugin{logger: logger}
	p.client = client

	got := make(chan codec.ProtectDeviceNameRequestParams, 1)
	_ = p.SubscribeProtectDeviceNameRequest(func(params codec.ProtectDeviceNameRequestParams) {
		got <- params
	})

	// Matrix writes rx 103 onto b.
	f := codec.EncodeProtectDeviceNameRequest(codec.ProtectDeviceNameRequestParams{Device: 500})
	if _, err := b.Write(codec.Pack(f)); err != nil {
		t.Fatalf("matrix write: %v", err)
	}
	select {
	case params := <-got:
		if params.Device != 500 {
			t.Errorf("got Device=%d; want 500", params.Device)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listener did not fire")
	}
	_ = client.Close()
}

// TestSendProtectDeviceNameRequestNotConnected verifies error path.
func TestSendProtectDeviceNameRequestNotConnected(t *testing.T) {
	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if _, err := p.SendProtectDeviceNameRequest(context.Background(), 1); err == nil {
		t.Error("SendProtectDeviceNameRequest on unconnected plugin returned nil")
	}
}
