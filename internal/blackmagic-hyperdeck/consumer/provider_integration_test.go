package hyperdeck

import (
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	providerpkg "acp/internal/blackmagic-hyperdeck/provider"
	"acp/internal/protocol"
)

func TestConsumerAgainstProvider(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := providerpkg.NewServer(slog.Default(), nil)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	_ = ln.Close()
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx, addr.String()) }()
	defer func() { _ = srv.Stop() }()
	time.Sleep(50 * time.Millisecond)

	p := (&Factory{}).New(slog.Default())
	if err := p.Connect(ctx, "127.0.0.1", addr.Port); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Disconnect() }()

	info, err := p.GetDeviceInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.NumSlots != 1 || info.ProtocolVersion != 1 {
		t.Fatalf("info = %+v", info)
	}

	objs, err := p.Walk(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) == 0 {
		t.Fatal("walk returned no objects")
	}

	confirmed, err := p.SetValue(ctx, protocol.ValueRequest{Path: "transport.status"}, protocol.Value{Str: "play"})
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Str != "play" {
		t.Fatalf("confirmed = %+v", confirmed)
	}
	got, err := p.GetValue(ctx, protocol.ValueRequest{Path: "transport.status"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Str != "play" {
		t.Fatalf("transport.status = %+v", got)
	}

	cancel()
	select {
	case <-errCh:
	case <-time.After(time.Second):
		t.Fatal("provider did not stop")
	}
}
