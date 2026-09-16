package main

import (
	"context"
	"io"
	"log/slog"
	"testing"

	rcprovider "dhs/internal/snell-rollcall/provider"
)

// fakeProxy stands in for a provider that can present as a RollCall IP Proxy.
type fakeProxy struct {
	got rcprovider.ProxyConfig
	set bool
}

func (f *fakeProxy) SetProxy(_ context.Context, cfg rcprovider.ProxyConfig) error {
	f.got, f.set = cfg, true
	return nil
}

func TestProxyFlagsFrontARealFrame(t *testing.T) {
	// The rack at .113, behind subnet 2100: the unit is learned from the frame,
	// the proxy answers as FF, a bare host takes the default port.
	for _, tc := range []struct {
		name     string
		opts     proxyOptions
		upstream string
		frame    uint8
		unit     uint8
	}{
		{"host and port", proxyOptions{subnet: "2100", upstream: "10.6.255.113:2050", unit: -1}, "10.6.255.113:2050", 0, 0xFF},
		{"bare host", proxyOptions{subnet: "2100", upstream: "10.6.255.113", unit: -1}, "10.6.255.113:2050", 0, 0xFF},
		{"frame given", proxyOptions{subnet: "2100", upstream: "10.6.255.113", frame: "0C", unit: -1}, "10.6.255.113:2050", 0x0C, 0xFF},
		{"unit given", proxyOptions{subnet: "2100", upstream: "10.6.255.113", unit: 0xFE}, "10.6.255.113:2050", 0, 0xFE},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := tc.opts.config()
			if err != nil {
				t.Fatalf("config: %v", err)
			}
			if len(cfg.Frames) != 1 {
				t.Fatalf("%d frames, want 1", len(cfg.Frames))
			}
			f := cfg.Frames[0]
			if f.Subnet != 0x2100 || f.Upstream != tc.upstream || f.Unit != tc.frame || cfg.Unit != tc.unit {
				t.Errorf("config = %+v", cfg)
			}
		})
	}
}

func TestProxyFlagsFrontTheServedTree(t *testing.T) {
	cfg, err := proxyOptions{subnet: "1100", frame: "0c", unit: -1}.config()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if len(cfg.Frames) != 1 || cfg.Frames[0].Subnet != 0x1100 || cfg.Frames[0].Unit != 0x0C ||
		cfg.Frames[0].Upstream != "" || cfg.Unit != 0xFF {
		t.Errorf("config = %+v", cfg)
	}
}

func TestProxyFlagsFrontSeveralFrames(t *testing.T) {
	// The rack, the Centra emulator and our own router, one subnet each, the
	// way the vendor box fronts several chassis; plus the served tree at 1100.
	cfg, err := proxyOptions{
		upstream: "2100=10.6.255.113, 3000=10.6.250.105:2057,4000=10.6.250.104:2052",
		subnet:   "1100", frame: "0C", unit: -1,
	}.config()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	want := []rcprovider.ProxyFrame{
		{Subnet: 0x2100, Upstream: "10.6.255.113:2050"},
		{Subnet: 0x3000, Upstream: "10.6.250.105:2057"},
		{Subnet: 0x4000, Upstream: "10.6.250.104:2052"},
		{Subnet: 0x1100, Unit: 0x0C},
	}
	if len(cfg.Frames) != len(want) {
		t.Fatalf("frames = %+v, want %+v", cfg.Frames, want)
	}
	for i := range want {
		if cfg.Frames[i] != want[i] {
			t.Errorf("frame %d = %+v, want %+v", i, cfg.Frames[i], want[i])
		}
	}

	// Real frames alone need no subnet flag.
	cfg, err = proxyOptions{upstream: "2100=10.6.255.113", unit: -1}.config()
	if err != nil || len(cfg.Frames) != 1 || cfg.Frames[0].Subnet != 0x2100 {
		t.Errorf("real frames alone: %+v, %v", cfg, err)
	}
}

func TestProxyFlagsAreRefusedRatherThanGuessedAt(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts proxyOptions
	}{
		{"no subnet", proxyOptions{upstream: "10.6.255.113", unit: -1}},
		{"short subnet", proxyOptions{subnet: "21", upstream: "10.6.255.113", unit: -1}},
		{"subnet not hex", proxyOptions{subnet: "21G0", upstream: "10.6.255.113", unit: -1}},
		{"tree with no frame unit", proxyOptions{subnet: "2100", unit: -1}},
		{"frame unit zero", proxyOptions{subnet: "2100", frame: "00", unit: -1}},
		{"frame unit too wide", proxyOptions{subnet: "2100", frame: "100", unit: -1}},
		{"empty host", proxyOptions{subnet: "2100", upstream: ":2050", unit: -1}},
		{"unit out of range", proxyOptions{subnet: "2100", upstream: "10.6.255.113", unit: 256}},
		{"list entry with a bad subnet", proxyOptions{upstream: "21=10.6.255.113", unit: -1}},
		{"list entry with no host", proxyOptions{upstream: "2100=", unit: -1}},
		{"list plus a tree with no unit", proxyOptions{upstream: "2100=10.6.255.113", subnet: "1100", unit: -1}},
		{"list plus a bad tree subnet", proxyOptions{upstream: "2100=10.6.255.113", subnet: "11", frame: "0C", unit: -1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.opts.config(); err == nil {
				t.Errorf("%+v was accepted", tc.opts)
			}
		})
	}
}

func TestTheProxyIsAppliedWhenAskedFor(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	f := &fakeProxy{}
	err := applyProxy(context.Background(), f, proxyOptions{subnet: "2100", upstream: "10.6.255.113", unit: -1}, logger)
	if err != nil {
		t.Fatalf("applyProxy: %v", err)
	}
	if !f.set || len(f.got.Frames) != 1 || f.got.Frames[0].Subnet != 0x2100 || f.got.Frames[0].Upstream != "10.6.255.113:2050" {
		t.Errorf("applied %+v (set %v)", f.got, f.set)
	}
}

func TestTheProxyIsLeftAloneWhenNoneIsAskedFor(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	f := &fakeProxy{}
	if err := applyProxy(context.Background(), f, proxyOptions{unit: -1}, logger); err != nil {
		t.Fatalf("applyProxy: %v", err)
	}
	if f.set {
		t.Error("a proxy was configured though none was asked for")
	}
}

func TestAProxyOnAProtocolThatHasNone(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := applyProxy(context.Background(), struct{}{}, proxyOptions{subnet: "2100", upstream: "h", unit: -1}, logger)
	if err == nil {
		t.Error("a proxy was configured on a protocol with none")
	}
}

func TestBadProxyFlagsAreNotApplied(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	f := &fakeProxy{}
	if err := applyProxy(context.Background(), f, proxyOptions{subnet: "21", upstream: "h", unit: -1}, logger); err == nil || f.set {
		t.Error("a malformed proxy configuration was applied")
	}
}
