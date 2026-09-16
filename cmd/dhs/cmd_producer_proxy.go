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
// RollCall IP Proxy: the vendor RollProxy, fronting either the tree it serves
// or a real frame on the network.
type proxyOptions struct {
	subnet   string // four hex digits, the route the frame is reached by
	upstream string // host[:port] of a real frame to front; empty fronts the tree
	frame    string // hex unit of the fronted frame; learned from a real one
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
func (o proxyOptions) config() (rcprovider.ProxyConfig, error) {
	var cfg rcprovider.ProxyConfig
	if o.subnet == "" {
		return cfg, fmt.Errorf("--proxy-subnet is required with --proxy-upstream or --proxy-frame")
	}
	if len(o.subnet) != 4 {
		return cfg, fmt.Errorf("--proxy-subnet %q: a RollCall net is four hex digits, e.g. 2100", o.subnet)
	}
	subnet, err := strconv.ParseUint(o.subnet, 16, 16)
	if err != nil {
		return cfg, fmt.Errorf("--proxy-subnet %q: a RollCall net is four hex digits, e.g. 2100", o.subnet)
	}
	cfg.Subnet = uint16(subnet)

	if o.frame != "" {
		frame, err := strconv.ParseUint(o.frame, 16, 8)
		if err != nil || frame == 0 {
			return cfg, fmt.Errorf("--proxy-frame %q: a unit is one hex byte from 01, e.g. 0C", o.frame)
		}
		cfg.Frame = uint8(frame)
	}

	if o.upstream != "" {
		host, port, err := net.SplitHostPort(o.upstream)
		if err != nil {
			// A bare host takes the plugin's default port, as a consumer's
			// target does.
			host, port = o.upstream, strconv.Itoa(rcprovider.DefaultPort)
		}
		if host == "" {
			return cfg, fmt.Errorf("--proxy-upstream %q: want host[:port]", o.upstream)
		}
		cfg.Upstream = net.JoinHostPort(host, port)
	} else if cfg.Frame == 0 {
		return cfg, fmt.Errorf("--proxy-frame is required to front the served tree (it is learned from the frame with --proxy-upstream)")
	}

	cfg.Unit = proxyDefaultUnit
	if o.unit != -1 {
		if o.unit < 1 || o.unit > 0xFF {
			return cfg, fmt.Errorf("--unit %d: a unit address is 1 to 255", o.unit)
		}
		cfg.Unit = uint8(o.unit)
	}
	return cfg, nil
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
	fronting := "the served tree"
	if cfg.Upstream != "" {
		fronting = cfg.Upstream
	}
	logger.Info("serving as a RollCall IP Proxy",
		slog.String("subnet", strings.ToUpper(o.subnet)),
		slog.String("unit", fmt.Sprintf("%02X", cfg.Unit)),
		slog.String("fronting", fronting))
	return nil
}
