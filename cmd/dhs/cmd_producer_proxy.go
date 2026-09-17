package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"

	rcprovider "dhs/internal/snell-rollcall/provider"
)

// proxyOptions are the serve flags that make a RollCall provider present as a
// RollCall IP Proxy: the vendor RollProxy, fronting the tree it serves and
// real frames on the network, one subnet each.
type proxyOptions struct {
	subnet   string // four hex digits: the route the served tree is reached by
	upstream string // NNNN=host[:port] entries, comma-separated; or one host[:port] with --proxy-subnet
	frame    string // hex unit of the served tree's frame; a real frame's is learned
	unit     int    // --unit, or -1 for the proxy's default
}

// enabled says whether any proxy flag was given.
func (o proxyOptions) enabled() bool {
	return o.subnet != "" || o.upstream != "" || o.frame != ""
}

// proxyDefaultUnit is the unit a proxy answers as unless --unit says otherwise:
// the vendor RollProxy Service answers as 0xFF.
const proxyDefaultUnit = 0xFF

// config turns the flags into a proxy configuration, refusing what the
// provider would refuse rather than starting a proxy nobody can reach.
//
// Two spellings. One frame: --proxy-subnet 2100 with --proxy-upstream host or
// --proxy-frame 0C. Several: --proxy-upstream 2100=10.6.255.113,3000=host:2057
// names a real frame per subnet, and --proxy-subnet with --proxy-frame adds the
// served tree beside them.
func (o proxyOptions) config() (rcprovider.ProxyConfig, error) {
	var cfg rcprovider.ProxyConfig

	cfg.Unit = proxyDefaultUnit
	if o.unit != -1 {
		if o.unit < 1 || o.unit > 0xFF {
			return cfg, fmt.Errorf("--unit %d: a unit address is 1 to 255", o.unit)
		}
		cfg.Unit = uint8(o.unit)
	}

	var frameUnit uint8
	if o.frame != "" {
		u, err := strconv.ParseUint(o.frame, 16, 8)
		if err != nil || u == 0 {
			return cfg, fmt.Errorf("--proxy-frame %q: a unit is one hex byte from 01, e.g. 0C", o.frame)
		}
		frameUnit = uint8(u)
	}

	// Several real frames, each behind its own subnet.
	if strings.Contains(o.upstream, "=") {
		for _, entry := range strings.Split(o.upstream, ",") {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			sub, host, ok := strings.Cut(entry, "=")
			if !ok {
				return cfg, fmt.Errorf("--proxy-upstream %q: want NNNN=host[:port] per frame", entry)
			}
			subnet, err := parseSubnet(sub)
			if err != nil {
				return cfg, fmt.Errorf("--proxy-upstream %q: %w", entry, err)
			}
			addr, err := upstreamAddr(host)
			if err != nil {
				return cfg, fmt.Errorf("--proxy-upstream %q: %w", entry, err)
			}
			cfg.Frames = append(cfg.Frames, rcprovider.ProxyFrame{Subnet: subnet, Upstream: addr})
		}
		// The served tree beside them, when asked for.
		if o.subnet != "" {
			subnet, err := parseSubnet(o.subnet)
			if err != nil {
				return cfg, fmt.Errorf("--proxy-subnet: %w", err)
			}
			if frameUnit == 0 {
				return cfg, fmt.Errorf("--proxy-frame is required to front the served tree at %s", o.subnet)
			}
			cfg.Frames = append(cfg.Frames, rcprovider.ProxyFrame{Subnet: subnet, Unit: frameUnit})
		}
		return cfg, nil
	}

	// One frame.
	if o.subnet == "" {
		return cfg, fmt.Errorf("--proxy-subnet is required with --proxy-upstream or --proxy-frame (or name a subnet per frame: --proxy-upstream NNNN=host,...)")
	}
	subnet, err := parseSubnet(o.subnet)
	if err != nil {
		return cfg, fmt.Errorf("--proxy-subnet: %w", err)
	}
	f := rcprovider.ProxyFrame{Subnet: subnet, Unit: frameUnit}
	if o.upstream != "" {
		addr, err := upstreamAddr(o.upstream)
		if err != nil {
			return cfg, fmt.Errorf("--proxy-upstream: %w", err)
		}
		f.Upstream = addr
	} else if frameUnit == 0 {
		return cfg, fmt.Errorf("--proxy-frame is required to front the served tree (it is learned from the frame with --proxy-upstream)")
	}
	cfg.Frames = []rcprovider.ProxyFrame{f}
	return cfg, nil
}

// parseSubnet reads a four-hex-digit RollCall net.
func parseSubnet(s string) (uint16, error) {
	if len(s) != 4 {
		return 0, fmt.Errorf("%q: a RollCall net is four hex digits, e.g. 2100", s)
	}
	v, err := strconv.ParseUint(s, 16, 16)
	if err != nil {
		return 0, fmt.Errorf("%q: a RollCall net is four hex digits, e.g. 2100", s)
	}
	return uint16(v), nil
}

// upstreamAddr reads host[:port]; a bare host takes the plugin's default port,
// as a consumer's target does.
func upstreamAddr(s string) (string, error) {
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		host, port = s, strconv.Itoa(rcprovider.DefaultPort)
	}
	if host == "" {
		return "", fmt.Errorf("%q: want host[:port]", s)
	}
	return net.JoinHostPort(host, port), nil
}

// applyProxy configures the proxy on a provider that can be one.
func applyProxy(ctx context.Context, srv any, o proxyOptions, logger *slog.Logger) error {
	if !o.enabled() {
		return nil
	}
	p, ok := srv.(interface {
		SetProxy(context.Context, rcprovider.ProxyConfig) error
	})
	if !ok {
		return fmt.Errorf("--proxy-subnet: this protocol has no proxy")
	}
	cfg, err := o.config()
	if err != nil {
		return err
	}
	if err := p.SetProxy(ctx, cfg); err != nil {
		return err
	}
	for _, f := range cfg.Frames {
		fronting := "the served tree"
		if f.Upstream != "" {
			fronting = f.Upstream
		}
		logger.Info("serving as a RollCall IP Proxy",
			slog.String("subnet", fmt.Sprintf("%04X", f.Subnet)),
			slog.String("unit", fmt.Sprintf("%02X", cfg.Unit)),
			slog.String("fronting", fronting))
	}
	return nil
}
