package consumer

import (
	"context"
	"fmt"

	"dhs/internal/rrcs/codec"
)

// Crosspoint volume notifications are not part of RegisterForAllEvents.
// They take two steps of their own (§8.15):
//
//  1. RegisterForEventsEx with the member XpVolumeChange set, which names
//     the receiver by IP address and TCP port;
//  2. XpVolumeChangeRegistryAdd for each crosspoint to follow — "the
//     method fails, if RegisterForEventsEx has not been called with the
//     member XpVolumeChange set to true". At most 8192 crosspoints.
//
// RRCS then sends XpVolumeChange (§9.4), starting with the current volume
// of every crosspoint just registered.

// MethodXpVolumeChange is the crosspoint volume notification (§9.4).
const MethodXpVolumeChange = "XpVolumeChange"

// Crosspoint is a source heard at a destination.
type Crosspoint struct {
	SrcNode, SrcPort int
	DstNode, DstPort int
}

func (x Crosspoint) value() codec.Value {
	end := func(isInput bool, node, port int) codec.Value {
		return codec.Struct(
			codec.Member{Name: "IsInput", Value: codec.Bool(isInput)},
			codec.Member{Name: "Node", Value: codec.Int(int32(node))},
			codec.Member{Name: "Port", Value: codec.Int(int32(port))},
		)
	}
	// §8.15 prints Destination before Source, the destination with
	// IsInput 0 and the source with IsInput 1.
	return codec.Struct(
		codec.Member{Name: "Destination", Value: end(false, x.DstNode, x.DstPort)},
		codec.Member{Name: "Source", Value: end(true, x.SrcNode, x.SrcPort)},
	)
}

// RegisterVolumeEvents asks RRCS to send crosspoint volume changes to
// ip:port (§8.15 RegisterForEventsEx). Only the member XpVolumeChange is
// given: "if they are not given, no notifications will be sent", so the
// other kinds stay with RegisterForAllEvents and nothing arrives twice.
func (c *Client) RegisterVolumeEvents(ctx context.Context, ip string, port int) (Reply, error) {
	if ip == "" || port < 1 || port > 65535 {
		return Reply{}, fmt.Errorf("rrcs: volume events: receiver %q port %d: %w", ip, port, codec.ErrRange)
	}
	return c.Call(ctx, "RegisterForEventsEx",
		codec.String(ip),
		codec.Int(int32(port)),
		codec.Struct(codec.Member{Name: "XpVolumeChange", Value: codec.Bool(true)}),
	)
}

// UnregisterVolumeEvents removes what RegisterVolumeEvents made (§8.15
// UnregisterForEventsEx). The receiver is named by its address alone, as
// §8.15 prints it: RRCS 9.0 answers fault 14 "requires 2 input parameters
// (3 received)" when the port is sent too.
func (c *Client) UnregisterVolumeEvents(ctx context.Context, ip string) (Reply, error) {
	return c.Call(ctx, "UnregisterForEventsEx", codec.String(ip))
}

// FollowVolumes adds crosspoints to the volume registry of the receiver
// ip:port (§8.15 XpVolumeChangeRegistryAdd).
func (c *Client) FollowVolumes(ctx context.Context, ip string, port int, xps ...Crosspoint) (Reply, error) {
	if len(xps) == 0 {
		return Reply{}, nil
	}
	list := make([]codec.Value, 0, len(xps))
	for _, x := range xps {
		list = append(list, x.value())
	}
	return c.Call(ctx, "XpVolumeChangeRegistryAdd",
		codec.String(ip),
		codec.Int(int32(port)),
		codec.Array(list...),
	)
}
