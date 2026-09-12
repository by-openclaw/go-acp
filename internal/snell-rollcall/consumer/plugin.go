// Package rollcall is the outbound Snell RollCall connector.
//
// It implements consumer.Protocol on top of the session layer, which supplies
// the link, the sessions and the back channel, and the codec, which supplies
// the wire format for both generations.
//
// # How RollCall maps onto the neutral model
//
// A RollCall network is units, and a unit has ports. A gateway is a unit whose
// ports are the cards in its frame, so the mapping in ADR-0022 falls out
// naturally: the unit is the device, a port is a slot, and the type id and
// version a port reports are the card's identity.
//
// An object is a menu line. Its command number is its id, its style says what
// kind of value it holds and whether it may be written, and its position in the
// menu gives its path.
//
// # Which generation
//
// A device may serve both, and which one a session speaks is decided when the
// session opens, not by the device. The peer advertises long strings in its
// service mask; a client that wants the 32-bit generation asks for the bit in
// its Call. Services are all-or-nothing, so a peer that cannot supply it
// refuses the whole call and the client retries without it.
//
// This connector asks for the 32-bit generation whenever the peer advertises
// it, because it is the only one that can express a router's command space,
// and falls back automatically otherwise.
package rollcall

import (
	"log/slog"
	"sync"
	"sync/atomic"

	"dhs/internal/clock"
	"dhs/internal/consumer"
	"dhs/internal/metrics"
	"dhs/internal/plugin"
	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
	"dhs/internal/transport"
)

// DefaultPort is the IPShare port. The vendor dissector decodes 2050 to 2060,
// and a gateway listens on the first of those.
const DefaultPort = 2050

// Register this plugin on import.
func init() {
	consumer.Register(&Factory{})
}

// Factory builds Plugin instances.
type Factory struct{}

// Meta describes the connector to the registry.
func (f *Factory) Meta() consumer.ProtocolMeta {
	return consumer.ProtocolMeta{
		Name:        "rollcall",
		DefaultPort: DefaultPort,
		Description: "Snell RollCall over IPShare, 16-bit and 32-bit generations",
	}
}

// New builds a connector from the dependency set.
func (f *Factory) New(deps plugin.Deps) consumer.Protocol {
	return New(deps)
}

// New builds a connector. It opens nothing: Connect does that, through the
// injected transport.
func New(deps plugin.Deps) *Plugin {
	deps = deps.WithDefaults()
	return &Plugin{
		log:    deps.Logger,
		net:    deps.Net,
		clk:    deps.Clock,
		met:    deps.Metrics,
		deps:   deps,
		trees:  make(map[int]*slotTree),
		subs:   make(map[subKey]consumer.EventFunc),
		events: make(chan struct{}),
	}
}

// Plugin is one connection to one RollCall gateway.
type Plugin struct {
	log  *slog.Logger
	net  transport.Net
	clk  clock.Clock
	met  *metrics.Connector
	deps plugin.Deps

	// recorder captures every frame for a replay fixture, when one was asked
	// for. Nil is the ordinary case.
	recorder session.Recorder

	mu sync.RWMutex

	// name labels this client in the network map, for the operator reading the
	// list of who is attached. Empty means the default.
	name string

	// link and its sessions are replaced wholesale on reconnect, so a caller
	// holding a stale one cannot use it by accident.
	link *link

	// trees holds one walked menu per slot, which is what makes a label or a
	// path resolvable without walking again.
	trees map[int]*slotTree

	// names caches the router name files by the checksum the controller
	// published. A large level's names are tens of thousands of strings and
	// they change only when somebody renames something, which is exactly what
	// the checksum detects.
	names map[namesKey]Names

	// nodeCache is the device's enumerated nodes, walked once per connection.
	// It is what turns a slot number into an address, and a controller's nodes
	// are unreachable without it.
	nodeCache *nodeTable

	subs map[subKey]consumer.EventFunc

	// comp collects spec deviations. The connector absorbs each one and keeps
	// running; the catalogue is what stops that being silent.
	comp compliance

	// events is closed to stop the back-channel pump.
	events chan struct{}

	// fileCeiling bounds one file read. Zero takes DefaultMaxFileBytes.
	fileCeiling int

	// addr is what we were asked to connect to, kept for DeviceInfo.
	addr string
	port int

	// announcements counts the units that have announced themselves on this
	// link. A device that has gone quiet is the usual explanation for a node
	// missing from a gateway's map, so the count is worth having.
	announcements atomic.Uint64
}

// Announcements returns how many unit announcements this connector has
// absorbed since it connected.
func (p *Plugin) Announcements() uint64 { return p.announcements.Load() }

// subKey identifies a subscription. A zero command means every command on the
// slot, which is what an empty label or id in the request asks for.
type subKey struct {
	slot    int
	command uint32
}

// compile-time proof that the plugin satisfies the neutral contract.
var _ consumer.Protocol = (*Plugin)(nil)

// styleKind maps a menu line's style to the neutral value kind.
//
// The mapping is what makes a RollCall menu legible to a caller that has never
// heard of RollCall, so it is worth being exact about the two that are not
// obvious. A checkbox is a boolean rather than a small number, because that is
// what a caller wants to set. A button carries no value at all: writing to it
// is the action, and the value it writes is in its own range field.
func styleKind(style codec.Style) consumer.ValueKind {
	switch style.Kind() {
	case codec.StyleCheckbox:
		return consumer.KindBool

	case codec.StyleNumber, codec.StyleVGraph, codec.StyleHGraph,
		codec.StyleVLevel, codec.StyleHLevel, codec.StyleButton:
		return consumer.KindInt

	case codec.StyleEditString, codec.StyleDisplay:
		return consumer.KindString

	case codec.StyleList, codec.StyleTiled, codec.StylePartial:
		// A container holds no value of its own; it is a node.
		return consumer.KindUnknown

	case codec.StyleData:
		return consumer.KindRaw

	case codec.StyleLink:
		// A link names another unit rather than holding a value.
		return consumer.KindString

	default:
		return consumer.KindUnknown
	}
}

// Access bits, matching the neutral model's byte.
const (
	accessRead   uint8 = 0x01
	accessWrite  uint8 = 0x02
	accessSetDef uint8 = 0x04
)

// styleAccess maps a menu line's style to the neutral access bits.
//
// Everything readable is read; a disabled line is not writable; a container
// and a display line are never writable. The preset bit is carried separately
// on the value, so it is added by the walker when a line reports it.
func styleAccess(style codec.Style) uint8 {
	access := accessRead
	if style.Disabled() {
		return access
	}
	switch style.Kind() {
	case codec.StyleTiled, codec.StyleList, codec.StylePartial, codec.StyleDisplay:
		return access
	default:
		return access | accessWrite
	}
}

// wantedServices is what a control-and-menu session would like.
//
// Display is included because a unit's status lines are pushed on the same
// session, and asking for it later would mean a second call. File is not: it is
// opened on demand, so a unit without a file service is still usable.
const wantedServices = codec.SvcMenus | codec.SvcControl | codec.SvcDisplay

// sessionServices is what to ask a particular peer for.
//
// It is the intersection of what we want with what the peer advertised, and
// the intersection is not optional. Services are all-or-nothing: a call that
// names one service the peer does not have is refused entirely, so asking a
// controller with no display service for a display makes the whole device
// unreachable. Measured against the vendor Centra, which refuses every call
// naming SV_DISPLAY and accepts the identical call without it.
func sessionServices(advertised codec.Service, longStrings bool) codec.Service {
	s := wantedServices & advertised
	if s == 0 {
		// A peer that advertised none of them is either wrong about itself or
		// serving something we do not know about. Ask for the pair every unit
		// has and let it answer for itself, rather than sending an empty mask
		// that asks for nothing at all.
		s = codec.SvcMenus | codec.SvcControl
	}
	if longStrings && advertised.LongStrings() {
		s |= codec.SvcLongStr
	}
	return s
}

// identity is what we present to a peer. The name is what appears in the
// vendor's own session list, so it says what we are.
func (p *Plugin) identity() codec.DeviceInfo {
	return session.ClientIdentity(p.clientName(), wantedServices|codec.SvcLongStr)
}

// announceIdentity is what we broadcast, at the address the gateway gave us.
//
// It differs from identity only in carrying that address. An announcement is
// how the network learns a unit exists, so one sent from the empty address a
// client holds before its handshake would name nothing.
func (p *Plugin) announceIdentity(addr codec.Address) codec.DeviceInfo {
	info := p.identity()
	info.Address = addr
	return info
}

// clientName is what an operator sees in the list of connected clients.
//
// It is settable because the question that list answers is "who is attached",
// and two dhs instances that both call themselves dhs do not answer it. The
// name is what an operator reads before disconnecting clients for a firmware
// upgrade.
func (p *Plugin) clientName() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.name != "" {
		return p.name
	}
	return defaultClientName
}

// SetName labels this client in the network map. It takes effect on the next
// connection, because the name travels in the handshake.
func (p *Plugin) SetName(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.name = name
}

// defaultClientName fits the twenty bytes a name field holds.
const defaultClientName = "dhs rollcall"

// SetRecorder attaches a capture to every link this plugin opens.
//
// It is the hook the CLI looks for when a verb was given somewhere to write a
// capture, and it must be set before Connect: a link records from the frame it
// opens with, and a capture that began halfway through a session is one whose
// replay starts in the middle of a conversation.
func (p *Plugin) SetRecorder(rec *transport.Recorder) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.recorder = rec
}
