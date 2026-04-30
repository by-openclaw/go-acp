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

// TestSendRouterConfigRequestRoundTripResponse1 drives SendRouter
// ConfigRequest through a net.Pipe pair with a matrix that replies
// with a tx 076 RESPONSE-1.
func TestSendRouterConfigRequestRoundTripResponse1(t *testing.T) {
	a, b := net.Pipe()
	defer func() {
		_ = a.Close()
		_ = b.Close()
	}()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	disable := false
	client := codec.NewClientFromConn(a, logger, codec.ClientConfig{WireHexLog: &disable})

	// rx 075 = 3 bytes (zero-length MESSAGE).
	matrixDone := make(chan struct{})
	go func() {
		defer close(matrixDone)
		buf := make([]byte, 3)
		if _, err := io.ReadFull(b, buf); err != nil {
			t.Errorf("fake matrix read: %v", err)
			return
		}
		req, _, err := codec.Unpack(buf)
		if err != nil || req.ID != codec.RxRouterConfigRequest {
			t.Errorf("fake matrix unpack got=%+v err=%v", req, err)
			return
		}
		reply := codec.EncodeRouterConfigResponse1(codec.RouterConfigResponse1Params{
			LevelMap: (1 << 0) | (1 << 1),
			Levels: []codec.RouterConfigResponse1LevelEntry{
				{NumDestinations: 128, NumSources: 128},
				{NumDestinations: 64, NumSources: 32},
			},
		})
		if _, werr := b.Write(codec.Pack(reply)); werr != nil {
			t.Errorf("fake matrix write: %v", werr)
		}
	}()

	p := &Plugin{logger: logger}
	p.client = client

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := p.SendRouterConfigRequest(ctx)
	if err != nil {
		t.Fatalf("SendRouterConfigRequest: %v", err)
	}
	if resp.Response1 == nil {
		t.Fatal("Response1 is nil; matrix chose RESPONSE-1")
	}
	if resp.Response2 != nil {
		t.Error("Response2 set for a RESPONSE-1 reply")
	}
	if resp.Response1.LevelMap != 0x03 || len(resp.Response1.Levels) != 2 {
		t.Errorf("Response1 = %+v; want 2 levels, map=0x03", resp.Response1)
	}

	<-matrixDone
	_ = client.Close()
}

// TestSendRouterConfigRequestRoundTripResponse2 drives the same
// helper against a matrix that chose RESPONSE-2.
func TestSendRouterConfigRequestRoundTripResponse2(t *testing.T) {
	a, b := net.Pipe()
	defer func() {
		_ = a.Close()
		_ = b.Close()
	}()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	disable := false
	client := codec.NewClientFromConn(a, logger, codec.ClientConfig{WireHexLog: &disable})

	matrixDone := make(chan struct{})
	go func() {
		defer close(matrixDone)
		buf := make([]byte, 3)
		if _, err := io.ReadFull(b, buf); err != nil {
			t.Errorf("fake matrix read: %v", err)
			return
		}
		if _, _, err := codec.Unpack(buf); err != nil {
			t.Errorf("fake matrix unpack: %v", err)
			return
		}
		reply := codec.EncodeRouterConfigResponse2(codec.RouterConfigResponse2Params{
			LevelMap: 1 << 0,
			Levels: []codec.RouterConfigResponse2LevelEntry{
				{NumDestinations: 32, NumSources: 32, StartDestination: 0, StartSource: 0},
			},
		})
		if _, werr := b.Write(codec.Pack(reply)); werr != nil {
			t.Errorf("fake matrix write: %v", werr)
		}
	}()

	p := &Plugin{logger: logger}
	p.client = client

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := p.SendRouterConfigRequest(ctx)
	if err != nil {
		t.Fatalf("SendRouterConfigRequest: %v", err)
	}
	if resp.Response2 == nil {
		t.Fatal("Response2 is nil; matrix chose RESPONSE-2")
	}
	if resp.Response1 != nil {
		t.Error("Response1 set for a RESPONSE-2 reply")
	}

	<-matrixDone
	_ = client.Close()
}

// TestSendRouterConfigRequestNotConnected verifies the error path.
func TestSendRouterConfigRequestNotConnected(t *testing.T) {
	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if _, err := p.SendRouterConfigRequest(context.Background()); err == nil {
		t.Error("SendRouterConfigRequest on unconnected plugin returned nil")
	}
}
