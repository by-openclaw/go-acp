package rollcall

import (
	"fmt"

	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
)

// The provider can present as a RollCall IP Proxy — the vendor's way of putting
// a RollCall network onto Ethernet — fronting one frame at a network address.
//
// A client of the real proxy sees a chain: the proxy unit advertises the map
// service and lists a virtual node; that node's net service lists the next
// virtual node; the last one's net service lists the frame's gateway; and the
// gateway's port service lists its cards. Each frame is added to the proxy with
// a subnet (a network address such as 0x1100), and that subnet is the RollCall
// route stamped onto everything behind the frame — its nibbles are the bridge
// hops a client crosses to reach it (spec 5.3).
//
// This reproduces that chain in one process. The virtual-node hierarchy is
// derived from the subnet rather than configured: a subnet of 0x1100 is two
// hops, so two virtual nodes, and the frame's gateway sits at 1100-<frame unit>.
// The frame itself is the model this provider already serves — port 0 stays its
// own gateway — so nothing about serving a frame changes; the proxy is a routing
// layer in front of it that resolves a routed address back to a model port.
//
// Like the vendor proxy, the list entries it hands back carry no route (net
// zero): a client composes the route as it descends, which is exactly the path
// our own consumer takes against the vendor box. codec.Address.Compose is the
// shared transform both sides use, so the addresses a client dials are the ones
// this resolver knows.

// Proxy unit type ids, from the codec catalogue.
const (
	proxyTypeService uint16 = 305 // ID_ROLLPROXYSERVICE — the proxy unit itself
	proxyTypeNode    uint16 = 306 // ID_ROLLPROXY_NODE — a virtual routing node
)

// proxyName is what the proxy unit calls itself.
const proxyName = "dhs rollproxy"

// ProxyConfig fronts one frame behind a proxy at a network address.
//
// A frame is added the way the vendor RollProxy adds one: an entry carrying the
// frame and the subnet (network address) to reach it by. Unit is the proxy's own
// unit — the vendor RollProxy Service answers as 0xFF — Subnet is the frame's
// route, and Frame is the fronted frame's own unit.
type ProxyConfig struct {
	Unit   uint8
	Subnet uint16
	Frame  uint8
}

// proxyRole says what a routed address resolves to.
type proxyRole int

const (
	roleNone proxyRole = iota
	roleProxy
	roleVirtual
	roleFrame
)

// virtualNode is one hop of the derived chain.
type virtualNode struct {
	// dialed is the address a client reaches this node by, after composing the
	// route as it descends. local is how the node lists itself — net zero, the
	// address its own segment knows it by. far is what this node's net service
	// hands back: the next node's local address, or the frame gateway's.
	dialed codec.Address
	local  codec.Address
	far    codec.Address
	name   string
}

// proxyTopology is the precomputed chain a proxy serves.
type proxyTopology struct {
	unit      uint8
	subnet    uint16
	frameUnit uint8
	nodes     []virtualNode
}

// buildProxyTopology derives the virtual-node chain from the subnet.
//
// The subnet's non-zero nibbles, top down, are the bridge hops to the frame
// (spec 5.3): each becomes a virtual node whose own unit is that nibble. A
// client reaches node i by composing the route hop by hop, which is what its
// dialed address records; the far side each node hands back is the next node's
// local address, and the last node's is the frame gateway's.
func buildProxyTopology(cfg ProxyConfig) (*proxyTopology, error) {
	nibbles := routeNibbles(cfg.Subnet)
	if len(nibbles) == 0 {
		return nil, fmt.Errorf("rollcall: proxy subnet %04X names no route", cfg.Subnet)
	}
	if !(codec.Address{Net: cfg.Subnet}).ValidRoute() {
		return nil, fmt.Errorf("rollcall: proxy subnet %04X is not a valid route", cfg.Subnet)
	}
	if cfg.Unit == 0 {
		return nil, fmt.Errorf("rollcall: proxy unit may not be zero, the broadcast address")
	}
	if cfg.Frame == 0 || cfg.Frame == cfg.Unit {
		return nil, fmt.Errorf("rollcall: frame unit %02X collides with the proxy or broadcast", cfg.Frame)
	}
	for _, n := range nibbles {
		if n == cfg.Unit {
			return nil, fmt.Errorf("rollcall: proxy unit %02X collides with a route hop", cfg.Unit)
		}
	}

	t := &proxyTopology{unit: cfg.Unit, subnet: cfg.Subnet, frameUnit: cfg.Frame}
	var prev codec.Address
	for i, nib := range nibbles {
		local := codec.Address{Unit: nib, Index: codec.IndexUnknown}
		dialed := local.Device()
		if i > 0 {
			dialed = prev.Compose(local)
		}
		far := codec.Address{Unit: cfg.Frame, Index: codec.IndexUnknown}
		name := "Frame Network"
		if i+1 < len(nibbles) {
			far = codec.Address{Unit: nibbles[i+1], Index: codec.IndexUnknown}
			name = fmt.Sprintf("Network(%04X)", uint16(nib)<<uint(12-4*i))
		}
		t.nodes = append(t.nodes, virtualNode{dialed: dialed, local: local, far: far, name: name})
		prev = dialed
	}

	// By construction the last hop composed with the frame unit reaches
	// cfg.Subnet-cfg.Frame — each node inserts its own nibble at its own position,
	// which is exactly the subnet the nibbles were taken from — so resolve can key
	// the frame on the subnet and frame unit directly.
	return t, nil
}

// routeNibbles returns a route's hops, top nibble first, stopping at the first
// zero (a route fills from the top down, spec 5.3).
func routeNibbles(net uint16) []uint8 {
	var out []uint8
	for shift := 12; shift >= 0; shift -= 4 {
		nib := uint8((net >> uint(shift)) & 0xF)
		if nib == 0 {
			break
		}
		out = append(out, nib)
	}
	return out
}

// resolve maps a dialed address to what it names.
//
// The proxy unit is on the local segment (net zero). A virtual node is matched
// by the address a client composed to reach it. The frame and its cards all
// share the subnet route and the frame unit, differing only by port, so the
// port is handed back for the model to serve.
func (t *proxyTopology) resolve(dst codec.Address) (role proxyRole, hop int, port uint8, ok bool) {
	if dst.Net == 0 && dst.Unit == t.unit {
		return roleProxy, 0, 0, true
	}
	for i := range t.nodes {
		if dst.SameDevice(t.nodes[i].dialed) {
			return roleVirtual, i, 0, true
		}
	}
	if dst.Net == t.subnet && dst.Unit == t.frameUnit {
		return roleFrame, 0, dst.Port, true
	}
	return roleNone, 0, 0, false
}

// proxyID is what the proxy unit says it is: a RollProxy Service offering the
// map service, which is all a proxy serves.
func (t *proxyTopology) proxyID() codec.ID {
	return codec.ID{
		Services: codec.SvcMap,
		TypeID:   proxyTypeService,
		Version:  codec.Version{Major: 1, Minor: 0, Alpha: ' ', CmdSet: 1},
		Name:     codec.TruncateFixed(proxyName, codec.MaxTextSize),
	}
}

// virtualID is what a virtual node says it is: a Proxy Virtual Node offering the
// net service, which is how a client reads past it to what it fronts.
func (t *proxyTopology) virtualID(hop int) codec.ID {
	return codec.ID{
		Services: codec.SvcNet,
		TypeID:   proxyTypeNode,
		Version:  codec.Version{Major: 1, Minor: 0, Alpha: ' ', CmdSet: 1},
		Name:     codec.TruncateFixed(t.nodes[hop].name, codec.MaxTextSize),
	}
}

// present is the status every node in the chain reports.
func proxyStatus() codec.UnitStatus {
	return codec.UnitStatus{Status: codec.StatusPresent | codec.StatusOnline}
}

// mapItems is what the proxy's map service lists: the first virtual node, at its
// own net-zero address, the way the vendor proxy lists it.
func (t *proxyTopology) mapItems() [][]byte {
	info := codec.DeviceInfo{
		ProtocolVersion: codec.ProtocolVersion,
		Address:         t.nodes[0].local,
		ID:              t.virtualID(0),
		Status:          proxyStatus(),
	}
	payload, _ := info.AppendTo(nil)
	return [][]byte{payload}
}

// answerProxyNode answers a request addressed to the proxy unit itself: it
// identifies as a RollProxy Service and lists its virtual nodes on the map.
func (p *Provider) answerProxyNode(s *session.Session, req codec.Frame) error {
	switch req.Type {
	case codec.MsgKeepAlive:
		return s.Answer(codec.MsgAck, nil)
	case codec.MsgGetID:
		payload, _ := p.proxy.proxyID().AppendTo(nil)
		return s.Answer(codec.MsgRetID, payload)
	case codec.MsgGetStat:
		return s.Answer(codec.MsgRetStat, proxyStatus().AppendTo(nil))
	case codec.MsgGetDevInfo:
		addr := codec.Address{Unit: p.proxy.unit, Port: req.Dst.Port, Index: codec.IndexUnknown}
		return s.Answer(codec.MsgRetDevInfo, proxyInfoPayload(addr, p.proxy.proxyID()))
	case codec.MsgGetDevList, codec.MsgGetLocDevMap:
		return p.beginTransfer(s, req.Type, codec.MsgRetDevInfo, p.proxy.mapItems())
	default:
		return session.RefuseInvalidCommand()
	}
}

// answerVirtualNode answers a request addressed to one of the proxy's virtual
// routing nodes: it identifies as a Proxy Virtual Node and lists its far side on
// the net service.
func (p *Provider) answerVirtualNode(s *session.Session, req codec.Frame, hop int) error {
	switch req.Type {
	case codec.MsgKeepAlive:
		return s.Answer(codec.MsgAck, nil)
	case codec.MsgGetID:
		payload, _ := p.proxy.virtualID(hop).AppendTo(nil)
		return s.Answer(codec.MsgRetID, payload)
	case codec.MsgGetStat:
		return s.Answer(codec.MsgRetStat, proxyStatus().AppendTo(nil))
	case codec.MsgGetDevInfo:
		return s.Answer(codec.MsgRetDevInfo, proxyInfoPayload(p.proxy.nodes[hop].dialed, p.proxy.virtualID(hop)))
	case codec.MsgGetDevList, codec.MsgGetLocDevMap:
		// The far side carries the frame gateway's own identity on the last hop,
		// which is what the frame this provider serves reports for port zero.
		gw, _ := p.identityOf(0)
		return p.beginTransfer(s, req.Type, codec.MsgRetDevInfo, p.proxy.farItems(hop, gw))
	default:
		return session.RefuseInvalidCommand()
	}
}

// proxyInfoPayload renders a DeviceInfo for a proxy-chain node at an address.
func proxyInfoPayload(addr codec.Address, id codec.ID) []byte {
	info := codec.DeviceInfo{
		ProtocolVersion: codec.ProtocolVersion,
		Address:         addr,
		ID:              id,
		Status:          proxyStatus(),
	}
	payload, _ := info.AppendTo(nil)
	return payload
}

// farItems is what one virtual node's net service lists: the next node's local
// address, or — for the last node — the frame gateway's, carrying the gateway's
// own identity. Both are net zero, so a client composes the route itself.
func (t *proxyTopology) farItems(hop int, gateway codec.ID) [][]byte {
	var info codec.DeviceInfo
	if hop+1 < len(t.nodes) {
		info = codec.DeviceInfo{
			ProtocolVersion: codec.ProtocolVersion,
			Address:         t.nodes[hop+1].local,
			ID:              t.virtualID(hop + 1),
			Status:          proxyStatus(),
		}
	} else {
		info = codec.DeviceInfo{
			ProtocolVersion: codec.ProtocolVersion,
			Address:         codec.Address{Unit: t.frameUnit, Index: codec.IndexUnknown},
			ID:              gateway,
			Status:          proxyStatus(),
		}
	}
	payload, _ := info.AppendTo(nil)
	return [][]byte{payload}
}
