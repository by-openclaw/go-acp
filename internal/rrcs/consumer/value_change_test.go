package consumer

import (
	"context"
	"errors"
	"strings"
	"testing"

	dhsc "dhs/internal/consumer"
	"dhs/internal/rrcs/codec"
)

// A property the lists report is written through a configuration change
// in the proven forms: the channel count of an NMOS main through manual
// mode and back, typed and bounded by the tables, an address on an NMOS
// stream refused, a link's Mode sent as is on an input and refused alone
// on an output.
func TestPluginSetValueChange(t *testing.T) {
	channels := int32(1)
	var sent []codec.Value
	f := newFakeRRCS(t, func(call codec.Call) (codec.Value, *codec.Fault) {
		key := call.Params[0]
		switch call.Method {
		case "GetVersion":
			return codec.Array(key, codec.String("9.0.1")), nil
		case "GetPort":
			return codec.Array(key, codec.Struct(
				codec.Member{Name: "Label", Value: codec.String("O.-7.41")},
				codec.Member{Name: "PortAes67Output", Value: codec.Struct(
					codec.Member{Name: "Protocol", Value: codec.Int(5)},
					codec.Member{Name: "Channels", Value: codec.Int(channels)},
					codec.Member{Name: "SSRC", Value: codec.Int(0)},
					codec.Member{Name: "Multicast", Value: codec.String("0.0.0.0")},
				)},
			)), nil
		case "ConfigurationChange":
			change := call.Params[1].Items[0]
			sent = append(sent, change)
			sp, _ := change.Field("SpecificParams")
			if out, ok := sp.Field("PortAes67Output"); ok {
				if ch, ok := out.Field("Channels"); ok {
					channels = ch.Int
				}
			}
			return codec.Array(key, codec.Int(0)), nil
		}
		return codec.Value{}, &codec.Fault{Code: 1, String: "unexpected " + call.Method}
	})
	p := pluginConnect(t, f)
	ctx := context.Background()
	out := "net.1.node.63.port.1064.out.PortAes67Output."

	v, err := p.SetValue(ctx, dhsc.ValueRequest{Path: out + "Channels"}, dhsc.Value{Kind: dhsc.KindInt, Int: 8})
	if err != nil || v.Int != 8 {
		t.Fatalf("Channels = %+v, %v", v, err)
	}
	if len(sent) != 2 {
		t.Fatalf("%d requests, want the round trip of two", len(sent))
	}
	first, _ := sent[0].Field("SpecificParams")
	block, _ := first.Field("PortAes67Output")
	proto, _ := block.Field("Protocol")
	ch, _ := block.Field("Channels")
	second, _ := sent[1].Field("SpecificParams")
	block2, _ := second.Field("PortAes67Output")
	proto2, _ := block2.Field("Protocol")
	if proto.Int != 2 || ch.Int != 8 || proto2.Int != 5 || len(block2.Members) != 1 {
		t.Errorf("round trip: %s then %s", Compact(sent[0]), Compact(sent[1]))
	}

	var val *dhsc.ValidationError
	refusals := map[string]struct {
		path string
		v    dhsc.Value
		want string
	}{
		"above the range":   {out + "Channels", dhsc.Value{Kind: dhsc.KindInt, Int: 99}, "above the specification's maximum 16"},
		"address on NMOS":   {out + "Multicast", dhsc.Value{Kind: dhsc.KindString, Str: "239.1.1.1"}, "NMOS"},
		"read only":         {out + "SSRC", dhsc.Value{Kind: dhsc.KindInt, Int: 1}, "read only"},
		"output link alone": {out + "Mode", dhsc.Value{Kind: dhsc.KindInt, Int: 1064}, "in one request"},
	}
	for name, tc := range refusals {
		before := len(sent)
		_, err := p.SetValue(ctx, dhsc.ValueRequest{Path: tc.path}, tc.v)
		if !errors.As(err, &val) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want a validation error saying %q", name, err, tc.want)
		}
		if len(sent) != before {
			t.Errorf("%s: a request was sent", name)
		}
	}

	// An input's Mode alone is the first request of a link; RRCS never
	// reports it, the value given is answered.
	before := len(sent)
	v, err = p.SetValue(ctx, dhsc.ValueRequest{Path: "net.1.node.63.port.1065.in.PortAes67Input.Mode"}, dhsc.Value{Kind: dhsc.KindInt, Int: 1064})
	if err != nil || v.Int != 1064 || len(sent) != before+1 {
		t.Fatalf("input Mode: %+v, %v, %d requests", v, err, len(sent)-before)
	}
	link, _ := sent[before].Field("SpecificParams")
	if in, ok := link.Field("PortAes67Input"); !ok || len(in.Members) != 1 || in.Members[0].Name != "Mode" {
		t.Errorf("link request: %s", Compact(sent[before]))
	}
}
