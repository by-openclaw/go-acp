package acp1

import (
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
)

// TestSubscribe_OverAN2 drives the plugin Subscribe fan-out path on the AN2
// client: a GetDeviceInfo round-trip makes the echo server push an
// announcement, which the AN2 reader fans to the registered subscriber.
func TestSubscribe_OverAN2(t *testing.T) {
	host, port, stop := echoAN2Server(t, []byte{0x03, 0x02, 0x02, 0x00}, true)
	defer stop()

	p := &Plugin{logger: slog.Default()}
	p.SetTransport(TransportAN2)
	if err := p.Connect(context.Background(), host, port); err != nil {
		t.Fatalf("Connect AN2: %v", err)
	}
	t.Cleanup(func() { _ = p.Disconnect() })

	events := make(chan consumer.Event, 8)
	if err := p.Subscribe(consumer.ValueRequest{Slot: -1, Group: "", ID: -1},
		func(ev consumer.Event) { events <- ev }); err != nil {
		t.Fatalf("Subscribe over AN2: %v", err)
	}

	// Trigger device traffic so the echo server emits its announce.
	if _, err := p.GetDeviceInfo(context.Background()); err != nil {
		t.Fatalf("GetDeviceInfo: %v", err)
	}
	select {
	case <-events:
	case <-time.After(2 * time.Second):
		t.Fatal("AN2 announcement not delivered to subscriber")
	}

	if err := p.Unsubscribe(consumer.ValueRequest{Slot: -1, Group: "", ID: -1}); err != nil {
		t.Errorf("Unsubscribe: %v", err)
	}
}

func TestConnectAN2_Refused(t *testing.T) {
	// Bind then close to obtain a port with nothing listening → dial refused.
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	_ = ln.Close()

	p := &Plugin{logger: slog.Default()}
	p.SetTransport(TransportAN2)
	if err := p.Connect(context.Background(), "127.0.0.1", addr.Port); err == nil {
		_ = p.Disconnect()
		t.Fatal("Connect AN2 to closed port: want error")
	}
}

func TestAN2Client_DoTimeout(t *testing.T) {
	// Silent server: reads frames but never replies → Do times out.
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		buf := make([]byte, 256)
		for {
			if _, err := conn.Read(buf); err != nil {
				return
			}
		}
	}()
	a := ln.Addr().(*net.TCPAddr)
	c := NewAN2Client(dialAN2(t, "127.0.0.1", a.Port), nil, ClientConfig{ReceiveTimeout: 100 * time.Millisecond})
	defer func() { _ = c.Close() }()
	if _, err := c.Do(context.Background(), &codec.Message{
		MType: codec.MTypeRequest, MCode: byte(codec.MethodGetValue), ObjGroup: codec.GroupControl, ObjID: 5,
	}); err == nil {
		t.Error("AN2 Do against silent server: want timeout")
	}
}
