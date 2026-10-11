package consumer

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	dhsc "dhs/internal/consumer"
	"dhs/internal/rrcs/codec"
)

// §8.1 Crosspoints. A crosspoint is not an object of RRCS: it has no
// ObjectID. It is named by its two ends, each the path of a port.

// XpAddress is a crosspoint as §8.1 addresses it: net, node and port of
// the source, then of the destination.
type XpAddress struct {
	SrcNet, SrcNode, SrcPort int
	DstNet, DstNode, DstPort int
}

// PortOfPath reads net, node and port out of the path of a port
// (net.N.node.N.port.P, with or without .in or .out).
func PortOfPath(path string) (net, node, port int, isInput, ok bool) {
	parts := strings.Split(path, ".")
	if len(parts) < 6 || parts[0] != "net" || parts[2] != "node" || parts[4] != "port" {
		return 0, 0, 0, false, false
	}
	// A net that is not a number reads as net 1, as the verbs always did.
	net, err0 := strconv.Atoi(parts[1])
	if err0 != nil {
		net = 1
	}
	node, err1 := strconv.Atoi(parts[3])
	port, err2 := strconv.Atoi(parts[5])
	if err1 != nil || err2 != nil {
		return 0, 0, 0, false, false
	}
	return net, node, port, len(parts) > 6 && parts[6] == "in", true
}

// XpOfPaths is the crosspoint from one port to another, both given as
// paths.
func XpOfPaths(src, dst string) (XpAddress, error) {
	sNet, sNode, sPort, _, ok1 := PortOfPath(src)
	dNet, dNode, dPort, _, ok2 := PortOfPath(dst)
	if !ok1 || !ok2 {
		return XpAddress{}, fmt.Errorf("rrcs: a crosspoint wants two port paths (net.N.node.N.port.P), got %q and %q", src, dst)
	}
	return XpAddress{SrcNet: sNet, SrcNode: sNode, SrcPort: sPort, DstNet: dNet, DstNode: dNode, DstPort: dPort}, nil
}

// Crosspoint is the same crosspoint as the volume registration names it.
func (a XpAddress) Crosspoint() Crosspoint {
	return Crosspoint{SrcNode: a.SrcNode, SrcPort: a.SrcPort, DstNode: a.DstNode, DstPort: a.DstPort}
}

func (a XpAddress) params() []codec.Value {
	return []codec.Value{
		codec.Int(int32(a.SrcNet)), codec.Int(int32(a.SrcNode)), codec.Int(int32(a.SrcPort)),
		codec.Int(int32(a.DstNet)), codec.Int(int32(a.DstNode)), codec.Int(int32(a.DstPort)),
	}
}

// SetXp makes a crosspoint (§8.1).
func (c *Client) SetXp(ctx context.Context, a XpAddress) (Reply, error) {
	return c.Call(ctx, "SetXp", a.params()...)
}

// KillXp removes a crosspoint (§8.1).
func (c *Client) KillXp(ctx context.Context, a XpAddress) (Reply, error) {
	return c.Call(ctx, "KillXp", a.params()...)
}

// SetXpPrio makes a crosspoint with a priority (§8.1).
func (c *Client) SetXpPrio(ctx context.Context, a XpAddress, priority int) (Reply, error) {
	return c.Call(ctx, "SetXpPrio", append(a.params(), codec.Int(int32(priority)))...)
}

// SetXpDestructive makes a crosspoint and removes the routes it
// displaces (§8.1).
func (c *Client) SetXpDestructive(ctx context.Context, a XpAddress, priority int) (Reply, error) {
	return c.Call(ctx, "SetXpDestructive", append(a.params(), codec.Int(int32(priority)))...)
}

// XpStatus reads whether a crosspoint is made (§8.1 GetXpStatus).
func (c *Client) XpStatus(ctx context.Context, a XpAddress) (bool, error) {
	reply, err := c.Call(ctx, "GetXpStatus", a.params()...)
	if err != nil {
		return false, err
	}
	p := reply.Payload()
	return len(p.Items) > 0 && p.Items[0].Kind == codec.KindBool && p.Items[0].Bool, nil
}

// ActiveXps reads the crosspoints that are made now (§8.1
// GetAllActiveXps): one request. The answer is a struct with one member
// "XP#N" per crosspoint, each a list of the six numbers of XpAddress.
func (c *Client) ActiveXps(ctx context.Context) ([]XpAddress, error) {
	reply, err := c.Call(ctx, "GetAllActiveXps")
	if err != nil {
		return nil, err
	}
	var out []XpAddress
	for _, m := range reply.Value.Members {
		if !strings.HasPrefix(m.Name, "XP#") || m.Value.Kind != codec.KindArray || len(m.Value.Items) < 6 {
			continue
		}
		n := func(i int) int {
			if m.Value.Items[i].Kind != codec.KindInt {
				return 0
			}
			return int(m.Value.Items[i].Int)
		}
		out = append(out, XpAddress{SrcNet: n(0), SrcNode: n(1), SrcPort: n(2), DstNet: n(3), DstNode: n(4), DstPort: n(5)})
	}
	return out, nil
}

// Crosspoints reads the crosspoints that are made now, each end named by
// the path and the label of its port: two requests, the active
// crosspoints and the ports.
func (p *Plugin) Crosspoints(ctx context.Context) ([]Xp, error) {
	p.mu.Lock()
	client := p.client
	p.mu.Unlock()
	if client == nil {
		return nil, dhsc.ErrNotConnected
	}
	snap, _, err := Collect(ctx, client, CollectOpts{Only: []string{"GetAllActiveXps", "GetAllPorts"}})
	if err != nil {
		return nil, err
	}
	m, err := modelOf(snap)
	if err != nil {
		return nil, err
	}
	return m.Xps, nil
}
