package consumer

import (
	"context"
	"testing"

	dhsc "dhs/internal/consumer"
	"dhs/internal/plugin"
)

// ADR-0037 P4: the schema of a point comes from the tables, without a
// request, with the manifest address of the thing it belongs to.
func TestDescribeOffline(t *testing.T) {
	p, ok := (&Factory{}).New(plugin.Deps{}).(*Plugin)
	if !ok {
		t.Fatal("Factory.New: not a *Plugin")
	}
	var _ dhsc.Describer = p
	ctx := context.Background()

	o, ref, err := p.Describe(ctx, dhsc.ValueRequest{Path: "net.1.node.61.port.1025.in.PortAes67Input.Channels"})
	if err != nil {
		t.Fatal(err)
	}
	if o.Kind != dhsc.KindInt || o.Min != int64(1) || o.Max != int64(16) || !o.HasWrite() || o.Label != "Channels" {
		t.Errorf("Channels: %+v", o)
	}
	if ref.Addr["node"] != 61 || ref.Addr["port"] != 1025 || ref.Path != "in.PortAes67Input.Channels" {
		t.Errorf("Channels ref: %+v", ref)
	}

	o, _, err = p.Describe(ctx, dhsc.ValueRequest{Path: "net.1.node.61.port.1025.out.PortAes67Output.Protocol"})
	if err != nil || len(o.EnumItems) != 3 || o.EnumItems[2] != "5=NMOS" {
		t.Errorf("Protocol: %v %+v", err, o)
	}

	o, _, err = p.Describe(ctx, dhsc.ValueRequest{Path: "net.1.node.61.port.1025.in.Label"})
	if err != nil || o.Kind != dhsc.KindString || o.MaxLen != 8 || !o.HasWrite() {
		t.Errorf("Label: %v %+v", err, o)
	}

	o, _, err = p.Describe(ctx, dhsc.ValueRequest{Path: "net.1.node.61.port.1025.in.PortAes67Input.SSRC"})
	if err != nil || o.HasWrite() {
		t.Errorf("SSRC: %v %+v", err, o)
	}

	o, ref, err = p.Describe(ctx, dhsc.ValueRequest{Path: "net.1.node.60.card.1.Ptp.PtpPriority"})
	if err != nil || o.Max != int64(255) || ref.Addr["bay"] != 1 || ref.Addr["card_node"] != 60 || ref.Path != "Ptp.PtpPriority" {
		t.Errorf("card: %v %+v %+v", err, o, ref)
	}

	_, ref, err = p.Describe(ctx, dhsc.ValueRequest{Path: "conference.300.Label"})
	if err != nil || ref.Template != "conference" || ref.Path != "Label" || ref.Addr["bay"] != 0 {
		t.Errorf("conference: %v %+v", err, ref)
	}

	if _, _, err := p.Describe(ctx, dhsc.ValueRequest{Path: "net.1.node.61.port.1025"}); err == nil {
		t.Error("an object path without a property was described")
	}
}
