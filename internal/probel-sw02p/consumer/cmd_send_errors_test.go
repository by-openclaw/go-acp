package probelsw02p

import (
	"context"
	"net"
	"testing"
	"time"

	"dhs/internal/probel-sw02p/codec"
)

// drainPeer reads and discards everything the plugin writes so a Send
// that expects no reply doesn't block on the pipe write. Stops when the
// peer is closed (test cleanup).
func drainPeer(p net.Conn) {
	go func() {
		buf := make([]byte, 256)
		for {
			if _, err := p.Read(buf); err != nil {
				return
			}
		}
	}()
}

// shortCtx returns a context that expires almost immediately, used to
// drive every Send*'s reply-wait timeout (the cli.Send error branch).
func shortCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}

// TestSendTimeouts drives every reply-waiting Send* against a peer that
// never answers, exercising each function's cli.Send error branch.
func TestSendTimeouts(t *testing.T) {
	cases := []struct {
		name string
		call func(ctx context.Context, p *Plugin) error
	}{
		{"Interrogate", func(c context.Context, p *Plugin) error { _, e := p.SendInterrogate(c, 1); return e }},
		{"Connect", func(c context.Context, p *Plugin) error { _, e := p.SendConnect(c, 1, 2, false); return e }},
		{"ConnectOnGo", func(c context.Context, p *Plugin) error { _, e := p.SendConnectOnGo(c, 1, 2, false); return e }},
		{"Go", func(c context.Context, p *Plugin) error { _, e := p.SendGo(c, codec.GoOpSet); return e }},
		{"StatusRequest", func(c context.Context, p *Plugin) error {
			_, e := p.SendStatusRequest(c, codec.ControllerLH)
			return e
		}},
		{"SourceLockStatusRequest", func(c context.Context, p *Plugin) error {
			_, e := p.SendSourceLockStatusRequest(c, codec.ControllerLH)
			return e
		}},
		{"DualControllerStatusRequest", func(c context.Context, p *Plugin) error {
			_, e := p.SendDualControllerStatusRequest(c)
			return e
		}},
		{"ConnectOnGoGroupSalvo", func(c context.Context, p *Plugin) error {
			_, e := p.SendConnectOnGoGroupSalvo(c, 1, 2, 3, false)
			return e
		}},
		{"GoGroupSalvo", func(c context.Context, p *Plugin) error {
			_, e := p.SendGoGroupSalvo(c, codec.GoOpSet, 3)
			return e
		}},
		{"ExtendedInterrogate", func(c context.Context, p *Plugin) error {
			_, e := p.SendExtendedInterrogate(c, 1)
			return e
		}},
		{"ExtendedConnect", func(c context.Context, p *Plugin) error {
			_, e := p.SendExtendedConnect(c, 1, 2)
			return e
		}},
		{"ExtendedConnectOnGo", func(c context.Context, p *Plugin) error {
			_, e := p.SendExtendedConnectOnGo(c, 1, 2)
			return e
		}},
		{"ExtendedConnectOnGoGroupSalvo", func(c context.Context, p *Plugin) error {
			_, e := p.SendExtendedConnectOnGoGroupSalvo(c, 1, 2, 3)
			return e
		}},
		{"RouterConfigRequest", func(c context.Context, p *Plugin) error {
			_, e := p.SendRouterConfigRequest(c)
			return e
		}},
		{"ExtendedProtectInterrogate", func(c context.Context, p *Plugin) error {
			_, e := p.SendExtendedProtectInterrogate(c, 1)
			return e
		}},
		{"ExtendedProtectConnect", func(c context.Context, p *Plugin) error {
			_, e := p.SendExtendedProtectConnect(c, 1, 2)
			return e
		}},
		{"ExtendedProtectDisconnect", func(c context.Context, p *Plugin) error {
			_, e := p.SendExtendedProtectDisconnect(c, 1, 2)
			return e
		}},
		{"ProtectDeviceNameRequest", func(c context.Context, p *Plugin) error {
			_, e := p.SendProtectDeviceNameRequest(c, 1)
			return e
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, peer := newPipePlugin(t)
			drainPeer(peer)
			if err := tc.call(shortCtx(t), p); err == nil {
				t.Errorf("%s with no reply returned nil; want timeout error", tc.name)
			}
		})
	}
}

// TestSendNotConnectedAll drives every Send*'s getClient error branch on
// an unconnected plugin. Complements the per-command round-trip files
// (some of which already pin a subset of these).
func TestSendNotConnectedAll(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		call func() error
	}{
		{"Go", func() error { _, e := (&Plugin{logger: quietLogger()}).SendGo(ctx, codec.GoOpSet); return e }},
		{"ConnectOnGoGroupSalvo", func() error {
			_, e := (&Plugin{logger: quietLogger()}).SendConnectOnGoGroupSalvo(ctx, 1, 2, 3, false)
			return e
		}},
		{"GoGroupSalvo", func() error {
			_, e := (&Plugin{logger: quietLogger()}).SendGoGroupSalvo(ctx, codec.GoOpSet, 3)
			return e
		}},
		{"ExtendedConnectOnGoGroupSalvo", func() error {
			_, e := (&Plugin{logger: quietLogger()}).SendExtendedConnectOnGoGroupSalvo(ctx, 1, 2, 3)
			return e
		}},
		{"ExtendedProtectInterrogate", func() error {
			_, e := (&Plugin{logger: quietLogger()}).SendExtendedProtectInterrogate(ctx, 1)
			return e
		}},
		{"ExtendedProtectConnect", func() error {
			_, e := (&Plugin{logger: quietLogger()}).SendExtendedProtectConnect(ctx, 1, 2)
			return e
		}},
		{"ExtendedProtectDisconnect", func() error {
			_, e := (&Plugin{logger: quietLogger()}).SendExtendedProtectDisconnect(ctx, 1, 2)
			return e
		}},
		{"ExtendedConnect", func() error {
			_, e := (&Plugin{logger: quietLogger()}).SendExtendedConnect(ctx, 1, 2)
			return e
		}},
		{"ExtendedInterrogate", func() error {
			_, e := (&Plugin{logger: quietLogger()}).SendExtendedInterrogate(ctx, 1)
			return e
		}},
		{"ExtendedConnectOnGo", func() error {
			_, e := (&Plugin{logger: quietLogger()}).SendExtendedConnectOnGo(ctx, 1, 2)
			return e
		}},
		{"Interrogate", func() error {
			_, e := (&Plugin{logger: quietLogger()}).SendInterrogate(ctx, 1)
			return e
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Errorf("%s on unconnected plugin returned nil; want ErrNotConnected", tc.name)
			}
		})
	}
}
