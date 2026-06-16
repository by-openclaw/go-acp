package probelsw08p

import (
	"context"
	"testing"

	"dhs/internal/probel-sw08p/codec"
)

// closedClientPlugin returns a Plugin whose codec.Client has been closed,
// so any Send fails on the write path — exercising every command method's
// "cli.Send returned err" wrapping branch.
func closedClientPlugin(t *testing.T) *Plugin {
	t.Helper()
	p, _, cleanup := newPeerHarness(t, ackOnly)
	cleanup() // closes client + pipe
	return p
}

// TestCommandsSendError walks every reply-expecting command against a
// closed client and asserts each returns a non-nil (wrapped) error from
// the Send failure path.
func TestCommandsSendError(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name string
		call func(p *Plugin) error
	}{
		{"CrosspointInterrogate", func(p *Plugin) error {
			_, err := p.CrosspointInterrogate(ctx, 0, 0, 0)
			return err
		}},
		{"CrosspointConnect", func(p *Plugin) error {
			_, err := p.CrosspointConnect(ctx, 0, 0, 0, 0)
			return err
		}},
		{"DualControllerStatus", func(p *Plugin) error {
			_, err := p.DualControllerStatus(ctx)
			return err
		}},
		{"ProtectInterrogate", func(p *Plugin) error {
			_, err := p.ProtectInterrogate(ctx, 0, 0, 0, 0)
			return err
		}},
		{"ProtectConnect", func(p *Plugin) error {
			_, err := p.ProtectConnect(ctx, 0, 0, 0, 0)
			return err
		}},
		{"ProtectDisconnect", func(p *Plugin) error {
			_, err := p.ProtectDisconnect(ctx, 0, 0, 0, 0)
			return err
		}},
		{"ProtectDeviceName", func(p *Plugin) error {
			_, err := p.ProtectDeviceName(ctx, 0)
			return err
		}},
		{"ProtectTallyDump", func(p *Plugin) error {
			_, err := p.ProtectTallyDump(ctx, 0, 0, 0)
			return err
		}},
		{"CrosspointTallyDump", func(p *Plugin) error {
			_, err := p.CrosspointTallyDump(ctx, 0, 0)
			return err
		}},
		{"MasterProtectConnect", func(p *Plugin) error {
			_, err := p.MasterProtectConnect(ctx, 0, 0, 0, 0)
			return err
		}},
		{"AllSourceNames", func(p *Plugin) error {
			_, err := p.AllSourceNames(ctx, 0, 0, codec.NameLen8)
			return err
		}},
		{"SingleSourceName", func(p *Plugin) error {
			_, err := p.SingleSourceName(ctx, 0, 0, codec.NameLen8, 0)
			return err
		}},
		{"AllDestAssocNames", func(p *Plugin) error {
			_, err := p.AllDestAssocNames(ctx, 0, codec.NameLen8)
			return err
		}},
		{"SingleDestAssocName", func(p *Plugin) error {
			_, err := p.SingleDestAssocName(ctx, 0, codec.NameLen8, 0)
			return err
		}},
		{"TieLineInterrogate", func(p *Plugin) error {
			_, err := p.TieLineInterrogate(ctx, 0, 0)
			return err
		}},
		{"AllSourceAssocNames", func(p *Plugin) error {
			_, err := p.AllSourceAssocNames(ctx, 0, codec.NameLen8)
			return err
		}},
		{"SingleSourceAssocName", func(p *Plugin) error {
			_, err := p.SingleSourceAssocName(ctx, 0, codec.NameLen8, 0)
			return err
		}},
		{"SalvoConnectOnGo", func(p *Plugin) error {
			_, err := p.SalvoConnectOnGo(ctx, codec.SalvoConnectOnGoParams{})
			return err
		}},
		{"SalvoGo", func(p *Plugin) error {
			_, err := p.SalvoGo(ctx, codec.SalvoGoParams{})
			return err
		}},
		{"SalvoGroupInterrogate", func(p *Plugin) error {
			_, err := p.SalvoGroupInterrogate(ctx, codec.SalvoGroupInterrogateParams{})
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := closedClientPlugin(t)
			if err := tc.call(p); err == nil {
				t.Errorf("%s on closed client returned nil; want send error", tc.name)
			}
		})
	}
}
