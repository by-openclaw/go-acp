package consumer

import (
	"context"
	"errors"
	"strings"
	"testing"

	dhsc "dhs/internal/consumer"
	"dhs/internal/plugin"
)

// ADR-0037 P6: the plugin refuses offline what the specification's
// tables would not take, and accepts the rest, without a gateway.
func TestValidateValueOffline(t *testing.T) {
	p, ok := (&Factory{}).New(plugin.Deps{}).(*Plugin)
	if !ok {
		t.Fatal("Factory.New: not a *Plugin")
	}
	var _ dhsc.ValueValidator = p
	str := func(s string) dhsc.Value { return dhsc.Value{Kind: dhsc.KindString, Str: s} }
	num := func(n int64) dhsc.Value { return dhsc.Value{Kind: dhsc.KindInt, Int: n} }
	cases := []struct {
		path   string
		val    dhsc.Value
		reason string // empty = accepted
	}{
		{"net.1.node.61.port.1024.in.PortAes67Input.Channels", num(8), ""},
		{"net.1.node.61.port.1024.in.PortAes67Input.Channels", num(32), "above the specification's maximum 16"},
		{"net.1.node.61.port.1024.in.PortAes67Input.Channels", str("eight"), "is not a whole number"},
		{"net.1.node.61.port.1024.in.PortAes67Input.Protocol", num(5), ""},
		{"net.1.node.61.port.1024.in.PortAes67Input.Protocol", num(4), "is not one of"},
		{"net.1.node.61.port.1025.in.PortAes67Input.Mode", num(1024), ""},
		{"net.1.node.61.port.1025.out.PortAes67Output.Mode", num(1024), "Mode and Selection in one request"},
		{"net.1.node.61.port.1024.in.PortAes67Input.Multicast", str("239.1.1.1"), ""},
		{"net.1.node.61.port.1024.in.PortAes67Input.Multicast", str("not-an-ip"), "is not an IP address"},
		{"net.1.node.61.port.1024.in.PortAes67Input.SSRC", str("x"), "read only"},
		{"net.1.node.61.port.1024.in.PortType", num(3), ""},
		{"net.1.node.61.port.1024.in.Label", str("I.-7.1"), ""},
		{"net.1.node.61.port.1024.in.Label", str("longer than eight"), "longer than the specification's 8 characters"},
		{"net.1.node.61.port.1024.in.InputGain", num(-128), ""},
		{"net.1.node.61.port.1024.in.InputGain", num(-40), "outside -36..36"},
		{"net.1.node.60.card.1.Ptp.PtpPriority", num(128), ""},
		{"net.1.node.60.card.1.Ptp.PtpPriority", num(300), "above the specification's maximum 255"},
		{"logic.7.State", str("true"), ""},
		{"logic.7.State", str("maybe"), "is not true or false"},
		{"net.1.node.61.port.1024", num(1), "does not name an object and a property"},
		{"", num(1), "by --path"},
	}
	for _, c := range cases {
		err := p.ValidateValue(context.Background(), dhsc.ValueRequest{Path: c.path}, c.val)
		if c.reason == "" {
			if err != nil {
				t.Errorf("%s = %v: refused: %v", c.path, c.val, err)
			}
			continue
		}
		var v *dhsc.ValidationError
		if !errors.As(err, &v) || !strings.Contains(err.Error(), c.reason) {
			t.Errorf("%s = %v: got %v, want a validation error saying %q", c.path, c.val, err, c.reason)
		}
	}
}
