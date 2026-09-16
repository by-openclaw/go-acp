package rollcall

import (
	"context"
	"fmt"
	"sync"

	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
)

// The provider can present as a RollCall IP Proxy — the vendor's way of putting
// a RollCall network onto Ethernet — fronting frames at network addresses.
//
// A client of the real proxy sees a chain per frame: the proxy unit advertises
// the map service and lists a virtual node; that node's net service lists the
// next virtual node; the last one's net service lists the frame's gateway; and
// the gateway's port service lists its cards. Each frame is added to the proxy
// with a subnet (a network address such as 0x1100), and that subnet is the
// RollCall route stamped onto everything behind the frame — its nibbles are the
// bridge hops a client crosses to reach it (spec 5.3). The vendor box fronts
// many chassis this way, one subnet each, and the manual's whole reason for it
// is "to enable connection to more than one Ethernet enabled IQ chassis".
//
// This reproduces those chains in one process. The virtual-node hierarchy is
// derived from each subnet rather than configured: a subnet of 0x1100 is two
// hops, so two virtual nodes, and the frame's gateway sits at 1100-<unit>. A
// frame is either the model this provider already serves — port 0 stays its
// own gateway, and the proxy is a routing layer that resolves a routed address
// back to a model port — or a real frame on the network, reached over a
// connection per client (proxy_relay.go).
//
// The entries it hands back are the vendor box's, byte for byte, measured on
// 2026-09-16 (docs/captures/vendor-proxy-walk-2026-09-16.txt): a virtual node
// is listed at its net-zero address with session index 0, and a client composes
// the route as it descends; the frame at the last hop is listed already routed
// (1100-0C-00), with the frame's own identity and status. codec.Address.Compose
// is the shared transform both sides use — it returns a routed entry as given —
// so the addresses a client dials are the ones this resolver knows.

// Proxy unit type ids, from the codec catalogue.
const (
	proxyTypeService uint16 = 305 // ID_ROLLPROXYSERVICE — the proxy unit itself
	proxyTypeNode    uint16 = 306 // ID_ROLLPROXY_NODE — a virtual routing node
)

// proxyName is what the proxy unit calls itself.
const proxyName = "dhs rollproxy"

// proxyPort is the port the proxy unit answers at. The vendor RollProxy Service
// answers the handshake from 0000-FF-01 and takes its map session there, not
// at port zero (measured 2026-09-16).
const proxyPort uint8 = 0x01

// proxyVersion is what the proxy unit and its virtual nodes report: the vendor
// RollProxy's 4.6, command set 0. A Control Panel keys what it knows about a
// unit on type and command set, so the chain reports the vendor's.
var proxyVersion = codec.Version{Major: 4, Minor: 6, Alpha: ' ', CmdSet: 0}

// ProxyFrame is one frame behind the proxy, the way the vendor RollProxy adds
// one: the subnet (substitution address) it is reached by, and what is there.
//
// Upstream names a real frame, as host:port; empty fronts the tree this
// provider serves, of which there can be one. Unit is the frame's own unit,
// which its addresses carry; a real frame's is learned from the frame itself
// when left zero.
type ProxyFrame struct {
	Subnet   uint16
	Unit     uint8
	Upstream string
}

// ProxyConfig makes the provider a proxy. Unit is the proxy's own unit — the
// vendor RollProxy Service answers as 0xFF — and Frames is what it fronts.
//
// Subnet, Frame and Upstream are the one-frame form of the same thing, kept
// for callers that front a single frame: when any is set they are appended to
// Frames as one entry.
type ProxyConfig struct {
	Unit   uint8
	Frames []ProxyFrame

	Subnet   uint16
	Frame    uint8
	Upstream string
}

// frames is the full list, with the one-frame form folded in.
func (cfg ProxyConfig) frames() []ProxyFrame {
	out := append([]ProxyFrame(nil), cfg.Frames...)
	if cfg.Subnet != 0 || cfg.Frame != 0 || cfg.Upstream != "" {
		out = append(out, ProxyFrame{Subnet: cfg.Subnet, Unit: cfg.Frame, Upstream: cfg.Upstream})
	}
	return out
}

// proxyRole says what a routed address resolves to.
type proxyRole int

const (
	roleNone proxyRole = iota
	roleProxy
	roleVirtual
	roleFrame
)

// virtualNode is one hop of a derived chain.
type virtualNode struct {
	// dialed is the address a client reaches this node by, after composing the
	// route as it descends. local is how the node lists itself — net zero, the
	// address its own segment knows it by.
	dialed codec.Address
	local  codec.Address
	name   string
}

// frameChain is one frame behind the proxy and the virtual nodes that lead to
// it.
type frameChain struct {
	subnet   uint16
	nodes    []virtualNode
	upstream string // "" fronts the served tree

	// What the frame is. For the served tree it is read from the model when
	// asked. For a real frame it is what the probe answered, guarded by mu
	// because a frame that was not there at start is probed again when a
	// client asks for it — the vendor box's "Calling" column, which turns to
	// "Connected" when the chassis appears.
	mu      sync.Mutex
	unit    uint8
	info    codec.DeviceInfo
	known   bool
	probing bool
}

// proxyTopology is the precomputed chains a proxy serves.
type proxyTopology struct {
	unit   uint8
	frames []*frameChain
}

// buildProxyTopology derives the virtual-node chains from the subnets.
//
// A subnet's non-zero nibbles, top down, are the bridge hops to its frame (spec
// 5.3): each becomes a virtual node whose own unit is that nibble. A client
// reaches node i by composing the route hop by hop, which is what its dialed
// address records. Two frames may not share a first hop: the proxy's map lists
// one virtual node per frame, and a node that led to two frames would need a
// merged chain nobody has measured. A frame served from the tree needs its unit
// given; a real frame's may be learned later.
func buildProxyTopology(cfg ProxyConfig) (*proxyTopology, error) {
	frames := cfg.frames()
	if len(frames) == 0 {
		return nil, fmt.Errorf("rollcall: a proxy fronts at least one frame")
	}
	if cfg.Unit == 0 {
		return nil, fmt.Errorf("rollcall: proxy unit may not be zero, the broadcast address")
	}

	t := &proxyTopology{unit: cfg.Unit}
	seenTop := make(map[uint8]bool)
	served := 0
	for _, f := range frames {
		nibbles := routeNibbles(f.Subnet)
		if len(nibbles) == 0 {
			return nil, fmt.Errorf("rollcall: proxy subnet %04X names no route", f.Subnet)
		}
		if !(codec.Address{Net: f.Subnet}).ValidRoute() {
			return nil, fmt.Errorf("rollcall: proxy subnet %04X is not a valid route", f.Subnet)
		}
		if seenTop[nibbles[0]] {
			return nil, fmt.Errorf("rollcall: two frames behind the proxy share the first hop %X", nibbles[0])
		}
		seenTop[nibbles[0]] = true
		if f.Unit == cfg.Unit {
			return nil, fmt.Errorf("rollcall: frame unit %02X collides with the proxy", f.Unit)
		}
		for _, n := range nibbles {
			if n == cfg.Unit {
				return nil, fmt.Errorf("rollcall: proxy unit %02X collides with a route hop", cfg.Unit)
			}
		}
		if f.Upstream == "" {
			served++
			if served > 1 {
				return nil, fmt.Errorf("rollcall: the proxy serves one tree; the other frames need an upstream")
			}
			if f.Unit == 0 {
				return nil, fmt.Errorf("rollcall: the served frame behind subnet %04X needs its unit", f.Subnet)
			}
		}

		c := &frameChain{subnet: f.Subnet, upstream: f.Upstream, unit: f.Unit}
		var prev codec.Address
		for i, nib := range nibbles {
			local := codec.Address{Unit: nib, Index: codec.IndexUnknown}
			dialed := local.Device()
			if i > 0 {
				dialed = prev.Compose(local)
			}
			// The vendor names every node "Network" plus the substitution address
			// as far as that hop (manual 3.4); the operator renames them.
			name := fmt.Sprintf("Network(%04X)", f.Subnet&(0xFFFF<<uint(12-4*i)))
			c.nodes = append(c.nodes, virtualNode{dialed: dialed, local: local, name: name})
			prev = dialed
		}
		t.frames = append(t.frames, c)
	}
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

// hasRelay says whether any frame is a real one reached over the network.
func (t *proxyTopology) hasRelay() bool {
	for _, c := range t.frames {
		if c.upstream != "" {
			return true
		}
	}
	return false
}

// resolve maps a dialed address to what it names.
//
// The proxy unit is on the local segment (net zero). A virtual node is matched
// by the address a client composed to reach it. Everything on a frame's subnet
// is that frame's: a real frame's whole segment is behind the route, whatever
// unit it is — a Centra puts every node on a unit of its own — while the served
// tree is one unit whose ports the model serves, so only its unit resolves.
func (t *proxyTopology) resolve(dst codec.Address) (role proxyRole, frame, hop int, port uint8, ok bool) {
	if dst.Net == 0 && dst.Unit == t.unit {
		return roleProxy, 0, 0, 0, true
	}
	for f, c := range t.frames {
		for i := range c.nodes {
			if dst.SameDevice(c.nodes[i].dialed) {
				return roleVirtual, f, i, 0, true
			}
		}
	}
	for f, c := range t.frames {
		if dst.Net != c.subnet {
			continue
		}
		if c.upstream != "" || dst.Unit == c.unit {
			return roleFrame, f, 0, dst.Port, true
		}
	}
	return roleNone, 0, 0, 0, false
}

// proxyID is what the proxy unit says it is: a RollProxy Service offering the
// map service, which is all a proxy serves.
func (t *proxyTopology) proxyID() codec.ID {
	return codec.ID{
		Services: codec.SvcMap,
		TypeID:   proxyTypeService,
		Version:  proxyVersion,
		Name:     codec.TruncateFixed(proxyName, codec.MaxTextSize),
	}
}

// virtualID is what a virtual node says it is: a Proxy Virtual Node offering the
// net service, which is how a client reads past it to what it fronts.
func (t *proxyTopology) virtualID(frame, hop int) codec.ID {
	return codec.ID{
		Services: codec.SvcNet,
		TypeID:   proxyTypeNode,
		Version:  proxyVersion,
		Name:     codec.TruncateFixed(t.frames[frame].nodes[hop].name, codec.MaxTextSize),
	}
}

// proxyStatus is the status the proxy unit and its virtual nodes report:
// present, and only present, as the vendor's do. Online is a unit's word for
// its own running state, and a routing node has none.
func proxyStatus() codec.UnitStatus {
	return codec.UnitStatus{Status: codec.StatusPresent}
}

// proxyAddr is the address the proxy unit answers as.
func (t *proxyTopology) proxyAddr() codec.Address {
	return codec.Address{Unit: t.unit, Port: proxyPort, Index: codec.IndexUnknown}
}

// listed is how a virtual node appears in a list: its net-zero address with
// session index zero, as the vendor proxy lists its own.
func listed(a codec.Address) codec.Address {
	a.Index = 0
	return a
}

// nodeEntry is one virtual node as a list carries it.
func (t *proxyTopology) nodeEntry(frame, hop int) []byte {
	info := codec.DeviceInfo{
		ProtocolVersion: codec.ProtocolVersion,
		Address:         listed(t.frames[frame].nodes[hop].local),
		ID:              t.virtualID(frame, hop),
		Status:          proxyStatus(),
	}
	payload, _ := info.AppendTo(nil)
	return payload
}

// mapItems is what the proxy's map service lists: the first virtual node of
// every frame, each at its own net-zero address, the way the vendor proxy
// lists them.
func (t *proxyTopology) mapItems() [][]byte {
	items := make([][]byte, 0, len(t.frames))
	for f := range t.frames {
		items = append(items, t.nodeEntry(f, 0))
	}
	return items
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
		return s.Answer(codec.MsgRetDevInfo, proxyInfoPayload(p.proxy.proxyAddr(), p.proxy.proxyID()))
	case codec.MsgGetDevList, codec.MsgGetLocDevMap:
		return p.beginTransfer(s, req.Type, codec.MsgRetDevInfo, p.proxy.mapItems())
	case codec.MsgBkChnReady, codec.MsgRepFChg, codec.MsgStopRepFChg:
		return chainBackChannel(s, req)
	default:
		return session.RefuseInvalidCommand()
	}
}

// chainBackChannel answers a back-channel request on a proxy or virtual node.
//
// A connected map or net session carries updates: an entry that changes state
// is pushed as SP_RETDEVINFO on the back channel (spec 7.5, 7.7; the vendor's
// MapServer.c queues exactly the entries that changed). The chain served here
// never changes, so enabling the channel queues nothing — but it must be
// acknowledged. Measured 2026-09-16: the vendor Control Panel reads the map,
// sends SP_BKCHNREADY on that session, and on InvCmd terminates the session and
// gives up on the proxy without ever asking a virtual node for its far side.
func chainBackChannel(s *session.Session, req codec.Frame) error {
	if req.Type == codec.MsgBkChnReady && len(req.Payload) != 1 {
		return session.RefuseNack("back channel state is one byte")
	}
	return s.Answer(codec.MsgAck, nil)
}

// answerVirtualNode answers a request addressed to one of the proxy's virtual
// routing nodes: it identifies as a Proxy Virtual Node and lists its far side on
// the net service.
func (p *Provider) answerVirtualNode(s *session.Session, req codec.Frame, frame, hop int) error {
	switch req.Type {
	case codec.MsgKeepAlive:
		return s.Answer(codec.MsgAck, nil)
	case codec.MsgGetID:
		payload, _ := p.proxy.virtualID(frame, hop).AppendTo(nil)
		return s.Answer(codec.MsgRetID, payload)
	case codec.MsgGetStat:
		return s.Answer(codec.MsgRetStat, proxyStatus().AppendTo(nil))
	case codec.MsgGetDevInfo:
		return s.Answer(codec.MsgRetDevInfo,
			proxyInfoPayload(p.proxy.frames[frame].nodes[hop].dialed, p.proxy.virtualID(frame, hop)))
	case codec.MsgGetDevList, codec.MsgGetLocDevMap:
		return p.beginTransfer(s, req.Type, codec.MsgRetDevInfo, p.farItems(frame, hop))
	case codec.MsgBkChnReady, codec.MsgRepFChg, codec.MsgStopRepFChg:
		return chainBackChannel(s, req)
	default:
		return session.RefuseInvalidCommand()
	}
}

// farItems is what one virtual node's net service lists: the next node at its
// net-zero address, for a client to compose the route to — or, for the last
// node, the frame itself, already routed, exactly as the vendor proxy lists it.
// A real frame that has not answered yet is an empty far side, which is what
// the vendor box lists for a chassis it is still calling; asking is what
// prompts another call.
func (p *Provider) farItems(frame, hop int) [][]byte {
	c := p.proxy.frames[frame]
	if hop+1 < len(c.nodes) {
		return [][]byte{p.proxy.nodeEntry(frame, hop+1)}
	}
	entry, ok := p.frameEntry(frame)
	if !ok {
		p.probeLater(c)
		return nil
	}
	payload, _ := entry.AppendTo(nil)
	return [][]byte{payload}
}

// frameEntry is the frame as the last virtual node lists it: at its routed
// address, with its own identity and status — a real frame's as it reported
// them to the probe, the served frame's as it reports them now. A real frame
// that has never answered has no entry yet.
func (p *Provider) frameEntry(frame int) (codec.DeviceInfo, bool) {
	c := p.proxy.frames[frame]
	c.mu.Lock()
	unit, info, known := c.unit, c.info, c.known
	c.mu.Unlock()

	if c.upstream == "" {
		id, _ := p.identityOf(0)
		info = codec.DeviceInfo{ID: id, Status: p.statusOf(0)}
		known = true
	}
	if !known {
		return codec.DeviceInfo{}, false
	}
	return codec.DeviceInfo{
		ProtocolVersion: codec.ProtocolVersion,
		Address:         codec.Address{Net: c.subnet, Unit: unit, Index: codec.IndexUnknown},
		ID:              info.ID,
		Status:          info.Status,
	}, true
}

// probe reaches a real frame to learn what it is, and records it.
func (p *Provider) probe(ctx context.Context, c *frameChain) error {
	info, at, err := p.probeFrame(ctx, c.upstream)
	if err != nil {
		return err
	}
	c.mu.Lock()
	if c.unit == 0 {
		c.unit = at.Unit
	}
	c.info = info
	c.known = true
	unit := c.unit
	c.mu.Unlock()

	p.log.Info("rollcall: proxy reached a frame",
		"frame", c.upstream,
		"unit", fmt.Sprintf("%02X", unit),
		"name", info.ID.Name,
		"type", codec.UnitTypeName(info.ID.TypeID),
		"services", info.ID.Services.String(),
		"subnet", fmt.Sprintf("%04X", c.subnet))
	return nil
}

// probeLater calls a frame that has not answered yet, off the read loop, once
// at a time.
func (p *Provider) probeLater(c *frameChain) {
	c.mu.Lock()
	if c.probing {
		c.mu.Unlock()
		return
	}
	c.probing = true
	c.mu.Unlock()

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		defer func() {
			c.mu.Lock()
			c.probing = false
			c.mu.Unlock()
		}()
		if err := p.probe(context.Background(), c); err != nil {
			p.log.Warn("rollcall: the frame behind the proxy still does not answer",
				"frame", c.upstream, "err", err)
		}
	}()
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
