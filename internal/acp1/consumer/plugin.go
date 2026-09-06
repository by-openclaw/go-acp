package acp1

import (
	"context"
	"dhs/internal/plugin"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
	"dhs/internal/consumer/compliance"
	"dhs/internal/transport"
)

// init registers the ACP1 plugin with the global protocol registry.
// Importing this package from cmd/ is enough to make "acp1" selectable
// by name via the --protocol flag (CLI) or protocol query parameter (API).
func init() {
	consumer.Register(&Factory{})
}

// Factory builds ACP1 Plugin instances.
type Factory struct{}

func (f *Factory) Meta() consumer.ProtocolMeta {
	return consumer.ProtocolMeta{
		Name:        "acp1",
		DefaultPort: codec.DefaultPort,
		Description: "Axon Control Protocol v1.4 (UDP direct)",
	}
}

func (f *Factory) New(deps plugin.Deps) consumer.Protocol {
	deps = deps.WithDefaults()
	p := &Plugin{logger: deps.Logger}
	p.Configure(deps.Net, acp1StaleAfter)
	return p
}

// TransportKind selects how the ACP1 plugin talks to the device.
// Exposed so the CLI can pass --transport udp|tcp|auto.
type TransportKind int

const (
	// TransportUDP is the spec v1.0 mode: UDP direct on port 2071.
	// Subnet broadcast announcements arrive via a separate listener
	// socket. Does not cross VLAN boundaries for announcements.
	TransportUDP TransportKind = iota

	// TransportTCPDirect is the spec v1.4 addition: one long-lived
	// TCP connection on port 2071 carries requests, replies, and
	// announcements multiplexed together. Routes cleanly across
	// VLANs since everything is unicast.
	TransportTCPDirect

	// TransportAuto tries TCP direct first with a short dial timeout,
	// falls back to UDP on connection refused / RST / timeout. Mirrors
	// the probe-then-pick behaviour of real ACP1 controllers (VSM
	// Studio, Cerebrum). The actual selected mode is logged after
	// Connect returns. After fallback the Plugin reports its effective
	// transport via Transport() — call sites that care (e.g. session
	// health) can read it.
	TransportAuto

	// TransportAN2 is the spec Mode C: ACP1 PDUs wrapped in AN2 frames over
	// a long-lived TCP connection on port 2072. Like TCP direct, requests /
	// replies / announcements multiplex on one unicast socket (routes across
	// VLANs); the client sends AN2 EnableProtocolEvents([ACP1]) at startup so
	// the device pushes announces back.
	TransportAN2
)

// autoTCPProbeTimeout is the per-attempt dial budget when TransportAuto
// races TCP first. Short enough that a refused/RST cross-VLAN attempt
// falls back to UDP within ~half a second; long enough to absorb
// typical LAN dial jitter.
const autoTCPProbeTimeout = 500 * time.Millisecond

// clientIface is the minimum contract the Plugin needs from a session
// layer. Both the UDP Client and the TCPClient satisfy it, so the rest
// of the plugin code is transport-agnostic.
type clientIface interface {
	Do(ctx context.Context, req *codec.Message) (*codec.Message, error)
	Close() error
}

// announceFanout is implemented by the multiplexing clients (TCPClient,
// AN2Client) that carry announcements on the same socket as replies and
// therefore expose per-listener registration. The UDP path uses the
// separate Listener instead. Subscribe/Unsubscribe route through whichever
// is active.
type announceFanout interface {
	AddListener(fn RawEventFunc) int
	RemoveListener(h int)
}

// Plugin is the ACP1 Protocol implementation. One instance handles one
// device (host:port). Internally it holds a transport-agnostic client
// for transactions, and a per-slot cache of walked trees that GetValue
// / SetValue consult to translate labels into (group, id).
type Plugin struct {
	// Health supplies SessionHealth. Inherited, not reimplemented:
	// ACP1 contributes only the stale window, the time source and —
	// the part that used to be wrong — which network it is actually
	// talking over.
	consumer.Health

	logger *slog.Logger

	mu        sync.Mutex
	transport TransportKind
	host      string
	port      int

	// UDP path: udpConn holds the connected datagram socket.
	// TCP path: tcpConn holds the long-lived framed socket.
	// Exactly one is non-nil after Connect returns.
	udpConn *transport.UDPConn
	tcpConn *transport.TCPConn

	client clientIface
	walker *Walker

	// trees caches the walked object tree per slot. GetValue / SetValue
	// read it to resolve Label → (group, id). A ValueRequest that passes
	// an explicit Group+ID bypasses the cache and does not require Walk.
	//
	// Backed by an LRU + TTL so long-running processes don't hold every
	// slot ever walked for the lifetime of the connection.
	trees *slotTreeCache

	// UDP announcement path: dedicated listener on its own UDP socket.
	listener *Listener

	// TCP announcement path: handles returned by TCPClient.AddListener.
	tcpListenerHandles []int

	subHandles map[subKey]SubHandle

	// Optional traffic capture for unit test data generation.
	recorder *transport.Recorder

	// dialer opens the AN2 (Mode C) socket. Injected rather than built
	// inline so the pipe is substitutable — see dial() for the default.
	dialer transport.Dialer

	// profile aggregates wire-tolerance events observed during this
	// session. See compliance_events.go for the catalog. Nil until
	// Connect fires; callers read via ComplianceProfile().
	profile *compliance.Profile

	// tsSink tracks the most-recent rx/tx wire timestamps so
	// SessionHealth() can compute Live without blocking. Nil until
	// Connect fires.
	tsSink *timestampSink

	// Keep-alive (cross-protocol contract). kaCfg is the operator's
	// CLI choice (set via SetKeepAlive); ka holds the running prober +
	// watchdog goroutines for the current Connect lifetime.
	kaCfg consumer.KeepAliveConfig
	ka    *keepAliveState

	// newListener builds the UDP announcement listener. Nil ⇒ the real
	// NewListener. Injection seam so tests can drive connectUDP's
	// listener-unavailable warn branch deterministically (a real bind
	// failure on the connected port can't be forced cross-platform with
	// SO_REUSEADDR enabled).
	newListener func(logger *slog.Logger, port int) (*Listener, error)
}

// ComplianceProfile returns the session-scoped compliance profile.
// Returns nil if Connect hasn't been called yet. Safe to call from
// any goroutine.
func (p *Plugin) ComplianceProfile() *compliance.Profile {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.profile
}

// SetRecorder attaches a traffic recorder to this plugin.
// Call before Connect. All sent and received frames are recorded.
func (p *Plugin) SetRecorder(rec *transport.Recorder) {
	p.mu.Lock()
	p.recorder = rec
	p.mu.Unlock()
}

// SetTransport selects UDP or TCP for subsequent Connect calls. Must be
// called before Connect. Defaults to UDP when not set.
func (p *Plugin) SetTransport(k TransportKind) {
	p.mu.Lock()
	p.transport = k
	p.mu.Unlock()
}

// subKey canonicalises a ValueRequest for map lookup. Unsubscribe needs
// to find the SubHandle that Subscribe produced, keyed by the same
// request tuple.
type subKey struct {
	slot  int
	group string
	label string
	id    int
}

func reqKey(req consumer.ValueRequest) subKey {
	return subKey{req.Slot, req.Group, req.Label, req.ID}
}

// Connect opens the selected transport (UDP by default, TCP direct if
// SetTransport(TransportTCPDirect) was called beforehand). Port 0 means
// "use the plugin's default" (2071 for ACP1, same for both transports).
func (p *Plugin) Connect(ctx context.Context, ip string, port int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.client != nil {
		if p.host == ip && (port == 0 || port == p.port) {
			return nil
		}
		return fmt.Errorf("acp1: already connected to %s:%d", p.host, p.port)
	}
	if port == 0 {
		port = codec.DefaultPort
	}

	switch p.transport {
	case TransportTCPDirect:
		if err := p.connectTCP(ctx, ip, port); err != nil {
			return err
		}
	case TransportAN2:
		// Mode C uses its own default port (2072), distinct from UDP/TCP
		// direct (2071). A caller that passed port 0 above already had it
		// defaulted to 2071; override to the AN2 port when it wasn't set
		// explicitly to the TCP-direct port.
		an2Port := port
		if port == codec.DefaultPort {
			an2Port = AN2DefaultPort
		}
		if err := p.connectAN2(ctx, ip, an2Port); err != nil {
			return err
		}
		port = an2Port
	case TransportAuto:
		// Try TCP first with a short budget. Only fall back on
		// transport-level errors (refused/RST/timeout) — a TCP
		// session that connects but then fails an ACP1 round-trip
		// is the wrong moment to switch transport. Caller's outer
		// timeout still bounds total time.
		probeCtx, cancel := context.WithTimeout(ctx, autoTCPProbeTimeout)
		err := p.connectTCP(probeCtx, ip, port)
		cancel()
		if err == nil {
			p.transport = TransportTCPDirect
			break
		}
		p.logger.Info("acp1 auto: TCP unreachable, falling back to UDP",
			"host", ip, "port", port, "err", err.Error())
		if err := p.connectUDP(ctx, ip, port); err != nil {
			return err
		}
		p.transport = TransportUDP
	default: // TransportUDP
		if err := p.connectUDP(ctx, ip, port); err != nil {
			return err
		}
	}

	p.host = ip
	p.port = port
	p.Opened(p.transport.network(), ip, port, p.tsSink)
	cfg := defaultCacheConfig()
	p.trees = newSlotTreeCache(cfg.MaxSize, cfg.TTL)
	p.subHandles = map[subKey]SubHandle{}
	p.profile = &compliance.Profile{}
	// p.tsSink is guaranteed non-nil here: every transport path
	// (connectUDP/connectTCP/connectAN2 above) allocates it before
	// returning, so the former `if p.tsSink == nil` guard was unreachable.
	p.walker = NewWalker(p.client)
	p.walker.SetProfile(p.profile)
	p.logger.Info("acp1 connected",
		"host", ip, "port", port, "transport", p.transport)

	// Prime lastRx so SessionLive() returns true immediately after a
	// successful connect, before any actual rx has happened. Mirrors
	// the Ember+ pattern (session.go:230). Without this, the
	// watchdog's first tick fires a spurious "session went silent"
	// warning during the connect-to-first-rx grace period — and the
	// TCP path doesn't go through timestampingTransport, so its
	// initial lastRx stays zero indefinitely.
	if p.tsSink != nil {
		p.tsSink.recordRx()
	}

	// Spawn the keep-alive prober + dead-man watchdog. Honours the
	// operator's SetKeepAlive choice; defaults to 5s probe / 15s
	// timeout when nothing was set.
	p.startKeepAlive(context.Background())
	return nil
}

// connectUDP builds the UDP session: connected datagram socket +
// request/reply Client + best-effort Listener on the same port for
// announcements. Listener bind failure is non-fatal.
func (p *Plugin) connectUDP(ctx context.Context, ip string, port int) error {
	conn, err := transport.DialUDP(ctx, ip, port)
	if err != nil {
		return &consumer.TransportError{Op: "connect", Err: err}
	}
	p.udpConn = conn
	var tr Transport = conn
	if p.recorder != nil {
		tr = p.recorder.WrapTransport(conn, "acp1")
	}
	// Wrap with timestamp tap so SessionHealth (#266) sees rx/tx
	// activity without each call needing to probe the wire.
	if p.tsSink == nil {
		p.tsSink = &timestampSink{}
	}
	tr = &timestampingTransport{inner: tr, sink: p.tsSink}
	p.client = NewClient(tr, p.logger, ClientConfig{})

	mkListener := p.newListener
	if mkListener == nil {
		mkListener = NewListener
	}
	if l, lerr := mkListener(p.logger, port); lerr != nil {
		p.logger.Warn("acp1 listener unavailable — Subscribe will fail",
			"port", port, "err", lerr)
	} else {
		p.listener = l
		p.listener.Start(context.Background())
	}
	return nil
}

// connectTCP opens the long-lived framed TCP connection and wraps it
// in a TCPClient. The multiplexing reader inside TCPClient handles
// both replies and announcements on the same socket — no separate
// listener needed.
func (p *Plugin) connectTCP(ctx context.Context, ip string, port int) error {
	conn, err := transport.DialTCP(ctx, ip, port)
	if err != nil {
		return &consumer.TransportError{Op: "connect", Err: err}
	}
	p.tcpConn = conn
	// Note: TCP recording not yet supported — TCPClient takes *TCPConn directly.
	// Recorder wraps the UDP path where it is used and tested.
	if p.tsSink == nil {
		p.tsSink = &timestampSink{}
	}
	cfg := ClientConfig{
		// Keep-alive RX tap — TCP doesn't go through
		// timestampingTransport (the UDP-only wrapper), so the
		// TCPClient surfaces every received frame through OnRx
		// instead. Without this the watchdog never sees rx and
		// flips the session to dead after the first timeout window.
		OnRx: p.tsSink.recordRx,
	}
	p.client = NewTCPClient(conn, p.logger, cfg)
	return nil
}

// connectAN2 opens the ACP1 Mode C connection: a raw TCP socket on port
// 2072 wrapped in an AN2Client that frames every ACP1 PDU in an AN2 data
// frame. Like the TCP-direct path, the multiplexing reader inside AN2Client
// handles replies and announcements on the one socket — no separate
// listener needed.
func (p *Plugin) connectAN2(ctx context.Context, ip string, port int) error {
	conn, err := p.dial().DialContext(ctx, "tcp4", net.JoinHostPort(ip, strconv.Itoa(port)))
	if err != nil {
		return &consumer.TransportError{Op: "connect", Err: err}
	}
	if p.tsSink == nil {
		p.tsSink = &timestampSink{}
	}
	p.client = NewAN2Client(conn, p.logger, ClientConfig{OnRx: p.tsSink.recordRx})
	return nil
}

// dial returns the injected dialer, or the shared default.
//
// AN2 was the one acp1 transport that opened its own socket — UDP and TCP
// direct already go through transport.DialUDP / transport.DialTCP — so it
// also missed the socket policy those apply. The default here keeps Nagle
// off (ACP1 messages are ≤141 bytes and latency-sensitive) and adds the
// SO_KEEPALIVE this path never had.
//
// Plugin is built as a bare struct literal throughout the package, so the
// nil case is the normal one; a caller that wants a different pipe sets the
// field.
func (p *Plugin) dial() transport.Dialer {
	if p.dialer != nil {
		return p.dialer
	}
	return transport.TCPDialer{Options: transport.SocketOptions{NoDelay: true}}
}

// Disconnect tears down whichever transport is active and clears all
// cached state. Safe to call more than once.
func (p *Plugin) Disconnect() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Stop the keep-alive prober + watchdog before tearing the
	// transport down — otherwise the prober may try one last write
	// against a closing socket and surface a spurious error.
	p.stopKeepAlive()

	// Stop the announcement listener first so no stray callback fires
	// during teardown.
	if p.listener != nil {
		p.listener.Stop()
		p.listener = nil
	}
	if p.client != nil {
		_ = p.client.Close()
	}

	if p.trees != nil {
		p.trees.Clear()
	}
	p.udpConn = nil
	p.tcpConn = nil
	p.client = nil
	p.Closed()
	p.walker = nil
	p.trees = nil
	p.subHandles = nil
	p.tcpListenerHandles = nil
	p.host = ""
	p.port = 0
	return nil
}

// String returns a human-readable transport name for log output.
func (k TransportKind) String() string {
	switch k {
	case TransportTCPDirect:
		return "tcp"
	case TransportAN2:
		return "an2"
	case TransportAuto:
		return "auto"
	default:
		return "udp"
	}
}

// network is the net package name for this transport, which is what
// decides whether a connect attempt carries any information. TCP direct
// and AN2 are both TCP streams; UDP is not.
func (k TransportKind) network() string {
	switch k {
	case TransportTCPDirect, TransportAN2:
		return "tcp"
	default:
		return "udp"
	}
}

// Transport reports the currently selected transport. After a Connect()
// with TransportAuto, this returns the resolved choice (TransportUDP or
// TransportTCPDirect), so callers can see which path was actually used.
func (p *Plugin) Transport() TransportKind {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.transport
}

// GetDeviceInfo reads the frame status object on slot 0 (rack controller)
// to discover how many slots the device has and the per-slot status.
// This is how acp discover / acp connect learn the device shape before
// walking individual slots.
func (p *Plugin) GetDeviceInfo(ctx context.Context) (consumer.DeviceInfo, error) {
	p.mu.Lock()
	c := p.client
	host, port := p.host, p.port
	p.mu.Unlock()
	if c == nil {
		return consumer.DeviceInfo{}, consumer.ErrNotConnected
	}

	// getValue(frame, 0) at slot 0 returns [num_slots, status_array...].
	// Spec p. 24 Frame Status Object.
	req := &codec.Message{
		MType:    codec.MTypeRequest,
		MAddr:    0,
		MCode:    byte(codec.MethodGetValue),
		ObjGroup: codec.GroupFrame,
		ObjID:    0,
	}
	reply, err := c.Do(ctx, req)
	if err != nil {
		return consumer.DeviceInfo{}, err
	}
	if reply.IsError() {
		return consumer.DeviceInfo{}, reply.ErrCode()
	}
	if len(reply.Value) < 1 {
		return consumer.DeviceInfo{}, fmt.Errorf("acp1: frame status reply too short")
	}
	return consumer.DeviceInfo{
		IP:              host,
		Port:            port,
		NumSlots:        int(reply.Value[0]),
		ProtocolVersion: 1,
	}, nil
}

// GetSlotInfo returns the status of a single slot plus, if the slot has
// already been walked, its identity strings (card label, description,
// serial). Unwalked slots return only the status byte.
func (p *Plugin) GetSlotInfo(ctx context.Context, slot int) (consumer.SlotInfo, error) {
	p.mu.Lock()
	c := p.client
	cache := p.trees
	p.mu.Unlock()
	if c == nil {
		return consumer.SlotInfo{}, consumer.ErrNotConnected
	}
	var tree *SlotTree
	if cache != nil {
		tree, _ = cache.Get(slot)
	}

	info := consumer.SlotInfo{Slot: slot}

	// Read the rack controller's frame status for the status byte.
	req := &codec.Message{
		MType:    codec.MTypeRequest,
		MAddr:    0,
		MCode:    byte(codec.MethodGetValue),
		ObjGroup: codec.GroupFrame,
		ObjID:    0,
	}
	reply, err := c.Do(ctx, req)
	if err != nil {
		return info, err
	}
	if reply.IsError() {
		return info, reply.ErrCode()
	}
	// reply.Value = [num_slots, status_0, status_1, ...]
	if len(reply.Value) >= slot+2 {
		info.Status = consumer.SlotStatus(reply.Value[slot+1])
	}

	// If we have a walked tree, surface identity strings from the
	// identity group. Spec p. 17 mandates object IDs 0..7 for the
	// standard identity set.
	if tree != nil {
		info.Identity = map[string]string{}
	}
	return info, nil
}

// Walk enumerates every object on the given slot, caches the result in
// the per-slot tree map, and returns the flat object list. Label-based
// GetValue / SetValue require a prior Walk on the same slot.
//
// Subsequent calls for the same slot return the cached tree without
// hitting the wire — the walker is idempotent and re-walking 200+
// objects on every get call would be prohibitively slow. To force a
// fresh walk, call Disconnect and re-Connect first. A dedicated
// --refresh flag on the CLI (and /api/.../objects?refresh=true on the
// server) is the Phase-2 story.
func (p *Plugin) Walk(ctx context.Context, slot int) ([]consumer.Object, error) {
	p.mu.Lock()
	cache := p.trees
	w := p.walker
	p.mu.Unlock()
	if w == nil {
		return nil, consumer.ErrNotConnected
	}
	if cache != nil {
		if cached, ok := cache.Get(slot); ok {
			return cached.Objects, nil
		}
	}

	tree, err := w.Walk(ctx, slot)
	if err != nil {
		return nil, err
	}

	if cache != nil {
		cache.Put(slot, tree)
	}
	return tree.Objects, nil
}

// GetValue reads one object value and decodes it into the appropriate
// typed field of consumer.Value (Int/Float/Str/Enum/IPAddr) using the
// cached walker tree. Label-addressed calls require a prior Walk. Raw
// bytes are always included in the returned Value for round-trip fidelity.
func (p *Plugin) GetValue(ctx context.Context, req consumer.ValueRequest) (consumer.Value, error) {
	p.mu.Lock()
	c := p.client
	cache := p.trees
	p.mu.Unlock()
	if c == nil {
		return consumer.Value{}, consumer.ErrNotConnected
	}
	var tree *SlotTree
	if cache != nil {
		tree, _ = cache.Get(req.Slot)
	}

	group, id, err := resolve(req, tree)
	if err != nil {
		return consumer.Value{}, err
	}

	m := &codec.Message{
		MType:    codec.MTypeRequest,
		MAddr:    byte(req.Slot),
		MCode:    byte(codec.MethodGetValue),
		ObjGroup: group,
		ObjID:    id,
	}
	reply, err := c.Do(ctx, m)
	if err != nil {
		return consumer.Value{}, err
	}
	if reply.IsError() {
		return consumer.Value{}, reply.ErrCode()
	}

	obj, acpType, found := findObject(tree, group, id)
	if !found {
		// No cached tree → try a single getObject to discover the type
		// (refs #421). Mirrors SetValue's cold-cache path so get/set
		// stay symmetric. Falls back to decodeByGroup if the meta
		// fetch itself fails (covers root/frame/file groups the
		// walker never traverses).
		mObj, mType, mErr := p.fetchObjectMeta(ctx, req.Slot, group, id)
		if mErr != nil {
			return decodeByGroup(group, reply.Value)
		}
		obj, acpType = mObj, mType
	}
	return DecodeValueBytes(obj, acpType, reply.Value)
}

// decodeByGroup is the fallback value decoder for objects the walker
// doesn't traverse (root, frame, file). It infers the ObjectType from
// the group alone and calls DecodeValueBytes with a synthetic empty
// Object. Unknown groups return raw bytes.
func decodeByGroup(group codec.ObjGroup, raw []byte) (consumer.Value, error) {
	var synth consumer.Object
	var t codec.ObjectType
	switch group {
	case codec.GroupFrame:
		synth.Kind = consumer.KindFrame
		t = codec.TypeFrame
	case codec.GroupRoot:
		t = codec.TypeRoot
	case codec.GroupFile:
		t = codec.TypeFile
	default:
		return consumer.Value{
			Kind: consumer.KindRaw,
			Raw:  append([]byte(nil), raw...),
		}, nil
	}
	return DecodeValueBytes(synth, t, raw)
}

// SetValue writes one object value. Accepts typed input via the
// consumer.Value fields (Int, Float, Str, Enum) and encodes to the right
// wire bytes based on the object's ACP1 type. Requires a prior Walk so
// the plugin knows the object's type.
func (p *Plugin) SetValue(ctx context.Context, req consumer.ValueRequest, val consumer.Value) (consumer.Value, error) {
	p.mu.Lock()
	c := p.client
	cache := p.trees
	p.mu.Unlock()
	if c == nil {
		return consumer.Value{}, consumer.ErrNotConnected
	}
	var tree *SlotTree
	if cache != nil {
		tree, _ = cache.Get(req.Slot)
	}

	group, id, err := resolve(req, tree)
	if err != nil {
		return consumer.Value{}, err
	}

	// Encode value bytes. If the caller supplied raw bytes directly (via
	// val.Raw with no typed fields), bypass the codec and send as-is —
	// this preserves the old "raw hex" escape hatch for advanced users.
	//
	// Otherwise the codec needs the object's ACPType to pick the right
	// wire encoder. Try the cached tree first; on miss, do a single
	// getObject via fetchObjectMeta (refs #421) so import doesn't have
	// to walk the whole slot to set one value.
	var wireBytes []byte
	obj, acpType, found := findObject(tree, group, id)
	if len(val.Raw) > 0 && val.Str == "" && val.Int == 0 && val.Float == 0 && val.Uint == 0 {
		wireBytes = val.Raw
	} else {
		if !found {
			obj, acpType, err = p.fetchObjectMeta(ctx, req.Slot, group, id)
			if err != nil {
				return consumer.Value{}, fmt.Errorf("acp1 set: fetch meta slot=%d group=%s id=%d: %w",
					req.Slot, group, id, err)
			}
			found = true
		}
		wireBytes, err = EncodeValueBytes(obj, acpType, val)
		if err != nil {
			return consumer.Value{}, err
		}
	}

	m := &codec.Message{
		MType:    codec.MTypeRequest,
		MAddr:    byte(req.Slot),
		MCode:    byte(codec.MethodSetValue),
		ObjGroup: group,
		ObjID:    id,
		Value:    wireBytes,
	}
	reply, err := c.Do(ctx, m)
	if err != nil {
		return consumer.Value{}, err
	}
	if reply.IsError() {
		return consumer.Value{}, reply.ErrCode()
	}

	// Decode the confirmed value (device echoes the stored value).
	if found {
		return DecodeValueBytes(obj, acpType, reply.Value)
	}
	return consumer.Value{
		Kind: consumer.KindRaw,
		Raw:  append([]byte(nil), reply.Value...),
	}, nil
}

// findObject looks up an Object and its ACP1 type inside a walked tree
// by (group, id). Returns false if the tree is nil or the object wasn't
// walked. O(n) — acceptable for typical tree sizes under 512 objects.
func findObject(tree *SlotTree, group codec.ObjGroup, id byte) (consumer.Object, codec.ObjectType, bool) {
	if tree == nil {
		return consumer.Object{}, 0, false
	}
	gname := group.String()
	for i, o := range tree.Objects {
		if o.Group == gname && o.ID == int(id) {
			// Defensive bound check: a seeder that forgot to populate
			// ACPTypes parallel to Objects would otherwise panic on
			// every announce. Treat a missing entry as "type unknown"
			// and fall back to the wrapper's decodeByGroup branch.
			if i >= len(tree.ACPTypes) {
				return o, 0, false
			}
			return o, tree.ACPTypes[i], true
		}
	}
	return consumer.Object{}, 0, false
}

// Subscribe registers a live-announcement callback. The request's Slot,
// Group, and Label/ID fields act as filters with wildcard semantics:
//
//	Slot  < 0  → any slot
//	Group == "" → any group
//	Label == "" && ID < 0 → any object in the matched group
//
// Label resolution requires a prior Walk on the target slot. When a
// matching announcement arrives the callback is invoked synchronously
// inside the listener goroutine with a decoded consumer.Event.
//
// Returns ErrNotConnected if the listener failed to bind at Connect
// time (typically: ACP port already in use by another process).
func (p *Plugin) Subscribe(req consumer.ValueRequest, fn consumer.EventFunc) error {
	p.mu.Lock()
	l := p.listener
	fanout, fanOK := p.client.(announceFanout)
	p.mu.Unlock()
	if l == nil && !fanOK {
		return consumer.ErrNotConnected
	}

	// Resolve label to (group, id) if a label was supplied. Bare group
	// or wildcard filters skip resolution.
	slot := req.Slot
	groupName := req.Group
	objID := req.ID
	if req.Label != "" {
		p.mu.Lock()
		cache := p.trees
		p.mu.Unlock()
		var tree *SlotTree
		if cache != nil {
			tree, _ = cache.Get(req.Slot)
		}
		g, id, err := resolve(req, tree)
		if err != nil {
			return err
		}
		groupName = g.String()
		objID = int(id)
	}

	// Wrap the user's high-level callback with a low-level RawEventFunc
	// that decodes the value bytes using the cached tree.
	wrapper := func(msg *codec.Message) {
		p.mu.Lock()
		cache := p.trees
		p.mu.Unlock()
		var tree *SlotTree
		if cache != nil {
			tree, _ = cache.Get(int(msg.MAddr))
		}

		ev := consumer.Event{
			Slot:      int(msg.MAddr),
			Group:     msg.ObjGroup.String(),
			ID:        int(msg.ObjID),
			Timestamp: time.Now(),
		}

		// Try to look up the object by (group, id) in the cached tree
		// so the callback gets a typed value, a resolved label, and
		// the engineering unit (#359).
		if obj, acpType, found := findObject(tree, msg.ObjGroup, msg.ObjID); found {
			ev.Label = obj.Label
			ev.Unit = obj.Unit
			ev.Access = obj.Access
			// browser.go builds obj.Path as [group, (sub-group,) label], so
			// eventPath joins it as-is and only appends the label for an older
			// flat Path=[group] shape — never duplicating the trailing element
			// (avoids the "control.Out-Mode.Out-Mode" watcher bug).
			ev.Path = eventPath(obj.Path, obj.Label)
			if val, derr := DecodeValueBytes(obj, acpType, msg.Value); derr == nil {
				ev.Value = val
				// Keep the cached tree in sync with live events so
				// subsequent walk / cache readers see the fresh value.
				cache.UpdateObjectValue(
					int(msg.MAddr),
					msg.ObjGroup.String(),
					int(msg.ObjID),
					val,
				)
			} else {
				ev.Value = consumer.Value{Kind: consumer.KindRaw, Raw: msg.Value}
			}
		} else {
			// Frame / root / file objects are never in the walked tree.
			// Fall back to group-based decoding so frame-status
			// announcements surface as structured SlotStatus arrays
			// instead of raw bytes.
			if val, derr := decodeByGroup(msg.ObjGroup, msg.Value); derr == nil {
				ev.Value = val
			} else {
				ev.Value = consumer.Value{Kind: consumer.KindRaw, Raw: msg.Value}
			}
		}
		fn(ev)
	}

	// Route registration to whichever event source is active. The UDP
	// Listener filters inside its own Subscribe call. The TCP / AN2
	// clients fan out every announcement to every registered listener,
	// so filtering is applied inside the wrapper for those paths.
	if fanOK {
		base := wrapper
		filtered := func(msg *codec.Message) {
			if slot >= 0 && int(msg.MAddr) != slot {
				return
			}
			if groupName != "" && msg.ObjGroup.String() != groupName {
				return
			}
			if objID >= 0 && int(msg.ObjID) != objID {
				return
			}
			base(msg)
		}
		h := fanout.AddListener(filtered)
		p.mu.Lock()
		p.tcpListenerHandles = append(p.tcpListenerHandles, h)
		p.subHandles[reqKey(req)] = SubHandle(h)
		p.mu.Unlock()
		return nil
	}

	// UDP path — the Listener understands (slot, group, id) filters.
	h := l.Subscribe(slot, groupName, objID, wrapper)
	p.mu.Lock()
	p.subHandles[reqKey(req)] = h
	p.mu.Unlock()
	return nil
}

// Unsubscribe removes a previously-registered Subscribe by matching the
// same request tuple. A request that was never registered is a no-op.
func (p *Plugin) Unsubscribe(req consumer.ValueRequest) error {
	p.mu.Lock()
	l := p.listener
	fanout, fanOK := p.client.(announceFanout)
	h, ok := p.subHandles[reqKey(req)]
	if ok {
		delete(p.subHandles, reqKey(req))
	}
	p.mu.Unlock()
	if !ok {
		return nil
	}
	if fanOK {
		fanout.RemoveListener(int(h))
		return nil
	}
	if l != nil {
		l.Unsubscribe(h)
	}
	return nil
}

// resolve translates a ValueRequest into a concrete (ObjGroup, ObjID)
// pair. Priority: Label lookup via the walked tree → explicit Group+ID
// fallback. If a label lookup in the declared group fails but the label
// exists in a different group, the error message suggests where it was
// found — a common source of confusion for users who forget which group
// a label belongs to.
//
// Importer-friendly cold-cache fall-through (#421): when Label is set
// AND tree is nil AND a valid (Group, ID) is also provided, use the
// explicit pair instead of erroring. CSV import rows carry id + label
// for round-trip fidelity; label resolution needs a walked tree but
// id doesn't, so this lets walk-free import work for ACP1.
func resolve(req consumer.ValueRequest, tree *SlotTree) (codec.ObjGroup, byte, error) {
	if req.Label != "" {
		if tree == nil {
			// Cold-cache fall-through: prefer the explicit (Group, ID)
			// when supplied (CSV importer carries both for round-trip).
			if req.Group != "" && req.ID >= 0 && req.ID <= 255 {
				if g, ok := codec.ParseGroup(req.Group); ok {
					return g, byte(req.ID), nil
				}
			}
			return 0, 0, fmt.Errorf("%w: no walked tree for slot %d",
				consumer.ErrUnknownLabel, req.Slot)
		}

		// 1. Primary search: within the declared group (if any).
		if req.Group != "" {
			if idx := tree.Lookup(req.Group, req.Label); idx >= 0 {
				obj := tree.Objects[idx]
				g, ok := codec.ParseGroup(obj.Group)
				if !ok {
					return 0, 0, fmt.Errorf("acp1: bad group %q in tree", obj.Group)
				}
				return g, byte(obj.ID), nil
			}
		} else {
			// 2. No group given — search all four in canonical order.
			for _, gname := range []string{"identity", "control", "status", "alarm"} {
				if idx := tree.Lookup(gname, req.Label); idx >= 0 {
					obj := tree.Objects[idx]
					g, ok := codec.ParseGroup(obj.Group)
					if !ok {
						return 0, 0, fmt.Errorf("acp1: bad group %q in tree", obj.Group)
					}
					return g, byte(obj.ID), nil
				}
			}
		}

		// 3. Miss in the declared group: look everywhere else and
		//    surface a helpful "did you mean" error.
		for _, gname := range []string{"identity", "control", "status", "alarm"} {
			if gname == req.Group {
				continue
			}
			if idx := tree.Lookup(gname, req.Label); idx >= 0 {
				obj := tree.Objects[idx]
				return 0, 0, fmt.Errorf("%w: label %q not in group %q — found in %q (id %d). Try --group %s",
					consumer.ErrUnknownLabel, req.Label, req.Group, gname, obj.ID, gname)
			}
		}
		return 0, 0, fmt.Errorf("%w: label %q not found in any group on slot %d",
			consumer.ErrUnknownLabel, req.Label, req.Slot)
	}

	// Address by explicit group + id — no walker needed.
	g, ok := codec.ParseGroup(req.Group)
	if !ok {
		return 0, 0, fmt.Errorf("acp1: invalid group %q", req.Group)
	}
	if req.ID < 0 || req.ID > 255 {
		return 0, 0, fmt.Errorf("acp1: object id out of range: %d", req.ID)
	}
	return g, byte(req.ID), nil
}
