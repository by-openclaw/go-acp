package acp1

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
)

func TestToProtocolObject_AllTypes(t *testing.T) {
	cases := []struct {
		d    *codec.DecodedObject
		want consumer.ValueKind
	}{
		{&codec.DecodedObject{Type: codec.TypeInteger, IntVal: -6, MinInt: -60, MaxInt: 12}, consumer.KindInt},
		{&codec.DecodedObject{Type: codec.TypeLong, IntVal: 100000}, consumer.KindInt},
		{&codec.DecodedObject{Type: codec.TypeByte, ByteVal: 7, MaxByte: 255}, consumer.KindUint},
		{&codec.DecodedObject{Type: codec.TypeFloat, FloatVal: 1.5}, consumer.KindFloat},
		{&codec.DecodedObject{Type: codec.TypeIPAddr, UintVal: 0x0A000001}, consumer.KindIPAddr},
		{&codec.DecodedObject{Type: codec.TypeEnum, ByteVal: 1, EnumItems: []string{"Off", "On"}}, consumer.KindEnum},
		{&codec.DecodedObject{Type: codec.TypeString, StrValue: "x", MaxLen: 8}, consumer.KindString},
		{&codec.DecodedObject{Type: codec.TypeAlarm, Priority: 1, EventOnMsg: "F", EventOffMsg: "OK"}, consumer.KindAlarm},
		{&codec.DecodedObject{Type: codec.TypeFrame, SlotStatus: []uint8{2, 2}}, consumer.KindFrame},
		{&codec.DecodedObject{Type: codec.TypeFile}, consumer.KindUnknown},     // default arm
		{&codec.DecodedObject{Type: codec.TypeReserved}, consumer.KindUnknown}, // default arm
	}
	for _, c := range cases {
		o := toProtocolObject(c.d, 1, codec.GroupControl, 0)
		if o.Kind != c.want {
			t.Errorf("type %d → kind %v, want %v", c.d.Type, o.Kind, c.want)
		}
	}
	// Enum value-name resolution + IPAddr octet expansion.
	enum := toProtocolObject(&codec.DecodedObject{Type: codec.TypeEnum, ByteVal: 1, EnumItems: []string{"Off", "On"}}, 0, codec.GroupControl, 0)
	if enum.Value.Str != "On" {
		t.Errorf("enum value str = %q, want On", enum.Value.Str)
	}
	ip := toProtocolObject(&codec.DecodedObject{Type: codec.TypeIPAddr, UintVal: 0x0A000001}, 0, codec.GroupStatus, 0)
	if ip.Value.IPAddr != [4]byte{10, 0, 0, 1} {
		t.Errorf("ipaddr octets = %v, want 10.0.0.1", ip.Value.IPAddr)
	}
}

func TestKeepAlive_ProberAndWatchdog(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	// Fast intervals so the prober ticks and the watchdog fires within the
	// test window. The prober probes 127.0.0.1:2071 (no server) → times out;
	// the watchdog observes the silence.
	p.SetKeepAlive(consumer.KeepAliveConfig{Interval: 10 * time.Millisecond, Timeout: 25 * time.Millisecond})
	if err := p.Connect(context.Background(), "127.0.0.1", 0); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = p.Disconnect() })
	// Let the prober tick a few times and the watchdog evaluate liveness.
	time.Sleep(120 * time.Millisecond)
	_ = p.SessionLive() // exercise the accessor (live or dead both fine)
	if err := p.Disconnect(); err != nil {
		t.Errorf("Disconnect: %v", err)
	}
}

func TestKeepAlive_DisabledSessionLive(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	// Disabled watchdog → SessionLive is always true once a sink exists.
	p.SetKeepAlive(consumer.KeepAliveConfig{Timeout: consumer.DisableTimeout})
	if err := p.Connect(context.Background(), "127.0.0.1", 0); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = p.Disconnect() })
	if !p.SessionLive() {
		t.Error("with watchdog disabled SessionLive should be true")
	}
}

func TestConnect_TCPDirect(t *testing.T) {
	host, port, stop := echoTCPServer(t, []byte{0x00, 0x05}, false)
	defer stop()

	p := &Plugin{logger: slog.Default()}
	p.SetTransport(TransportTCPDirect)
	if err := p.Connect(context.Background(), host, port); err != nil {
		t.Fatalf("Connect TCP: %v", err)
	}
	t.Cleanup(func() { _ = p.Disconnect() })
	if p.Transport() != TransportTCPDirect {
		t.Errorf("Transport = %v, want tcp", p.Transport())
	}
	// A GetDeviceInfo round-trips over the TCP client against the echo server.
	if _, err := p.GetDeviceInfo(context.Background()); err != nil {
		t.Fatalf("GetDeviceInfo over TCP: %v", err)
	}
	if err := p.Disconnect(); err != nil {
		t.Errorf("Disconnect: %v", err)
	}
}
