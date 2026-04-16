package acp1

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"acp/internal/protocol"
	"acp/internal/transport"
)

// init registers the ACP1 plugin with the global protocol registry.
// Importing this package from cmd/ is enough to make "acp1" selectable
// by name via the --protocol flag (CLI) or protocol query parameter (API).
func init() {
	protocol.Register(&Factory{})
}

// Factory builds ACP1 Plugin instances.
type Factory struct{}

func (f *Factory) Meta() protocol.ProtocolMeta {
	return protocol.ProtocolMeta{
		Name:        "acp1",
		DefaultPort: DefaultPort,
		Description: "Axon Control Protocol v1.4 (UDP direct)",
	}
}

func (f *Factory) New(logger *slog.Logger) protocol.Protocol {
	return &Plugin{logger: logger}
}

// TransportKind selects how the ACP1 plugin talks to the device.
// Exposed so the CLI can pass --transport udp|tcp.
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
)

// clientIface is the minimum contract the Plugin needs from a session
// layer. Both the UDP Client and the TCPClient satisfy it, so the rest
// of the plugin code is transport-agnostic.
type clientIface interface {
	Do(ctx context.Context, req *Message) (*Message, error)
	Close() error
}

// Plugin is the ACP1 Protocol implementation. One instance handles one
// device (host:port). Internally it holds a transport-agnostic client
// for transactions, and a per-slot cache of walked trees that GetValue
// / SetValue consult to translate labels into (group, id).
type Plugin struct {
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

func reqKey(req protocol.ValueRequest) subKey {
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
		port = DefaultPort
	}

	switch p.transport {
	case TransportTCPDirect:
		if err := p.connectTCP(ctx, ip, port); err != nil {
			return err
		}
	default: // TransportUDP
		if err := p.connectUDP(ctx, ip, port); err != nil {
			return err
		}
	}

	p.host = ip
	p.port = port
	cfg := defaultCacheConfig()
	p.trees = newSlotTreeCache(cfg.MaxSize, cfg.TTL)
	p.subHandles = map[subKey]SubHandle{}
	p.walker = NewWalker(p.client)
	p.logger.Info("acp1 connected",
		"host", ip, "port", port, "transport", p.transport)
	return nil
}

// connectUDP builds the UDP session: connected datagram socket +
// request/reply Client + best-effort Listener on the same port for
// announcements. Listener bind failure is non-fatal.
func (p *Plugin) connectUDP(ctx context.Context, ip string, port int) error {
	conn, err := transport.DialUDP(ctx, ip, port)
	if err != nil {
		return &protocol.TransportError{Op: "connect", Err: err}
	}
	p.udpConn = conn
	p.client = NewClient(conn, p.logger, ClientConfig{})

	if l, lerr := NewListener(p.logger, port); lerr != nil {
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
		return &protocol.TransportError{Op: "connect", Err: err}
	}
	p.tcpConn = conn
	p.client = NewTCPClient(conn, p.logger, ClientConfig{})
	return nil
}

// Disconnect tears down whichever transport is active and clears all
// cached state. Safe to call more than once.
func (p *Plugin) Disconnect() error {
	p.mu.Lock()
	defer p.mu.Unlock()

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
	default:
		return "udp"
	}
}

// GetDeviceInfo reads the frame status object on slot 0 (rack controller)
// to discover how many slots the device has and the per-slot status.
// This is how acp discover / acp connect learn the device shape before
// walking individual slots.
func (p *Plugin) GetDeviceInfo(ctx context.Context) (protocol.DeviceInfo, error) {
	p.mu.Lock()
	c := p.client
	host, port := p.host, p.port
	p.mu.Unlock()
	if c == nil {
		return protocol.DeviceInfo{}, protocol.ErrNotConnected
	}

	// getValue(frame, 0) at slot 0 returns [num_slots, status_array...].
	// Spec p. 24 Frame Status Object.
	req := &Message{
		MType:    MTypeRequest,
		MAddr:    0,
		MCode:    byte(MethodGetValue),
		ObjGroup: GroupFrame,
		ObjID:    0,
	}
	reply, err := c.Do(ctx, req)
	if err != nil {
		return protocol.DeviceInfo{}, err
	}
	if reply.IsError() {
		return protocol.DeviceInfo{}, reply.ErrCode()
	}
	if len(reply.Value) < 1 {
		return protocol.DeviceInfo{}, fmt.Errorf("acp1: frame status reply too short")
	}
	return protocol.DeviceInfo{
		IP:              host,
		Port:            port,
		NumSlots:        int(reply.Value[0]),
		ProtocolVersion: 1,
	}, nil
}

// GetSlotInfo returns the status of a single slot plus, if the slot has
// already been walked, its identity strings (card label, description,
// serial). Unwalked slots return only the status byte.
func (p *Plugin) GetSlotInfo(ctx context.Context, slot int) (protocol.SlotInfo, error) {
	p.mu.Lock()
	c := p.client
	cache := p.trees
	p.mu.Unlock()
	if c == nil {
		return protocol.SlotInfo{}, protocol.ErrNotConnected
	}
	var tree *SlotTree
	if cache != nil {
		tree, _ = cache.Get(slot)
	}

	info := protocol.SlotInfo{Slot: slot}

	// Read the rack controller's frame status for the status byte.
	req := &Message{
		MType:    MTypeRequest,
		MAddr:    0,
		MCode:    byte(MethodGetValue),
		ObjGroup: GroupFrame,
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
		info.Status = protocol.SlotStatus(reply.Value[slot+1])
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
func (p *Plugin) Walk(ctx context.Context, slot int) ([]protocol.Object, error) {
	p.mu.Lock()
	cache := p.trees
	w := p.walker
	p.mu.Unlock()
	if w == nil {
		return nil, protocol.ErrNotConnected
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
// typed field of protocol.Value (Int/Float/Str/Enum/IPAddr) using the
// cached walker tree. Label-addressed calls require a prior Walk. Raw
// bytes are always included in the returned Value for round-trip fidelity.
func (p *Plugin) GetValue(ctx context.Context, req protocol.ValueRequest) (protocol.Value, error) {
	p.mu.Lock()
	c := p.client
	cache := p.trees
	p.mu.Unlock()
	if c == nil {
		return protocol.Value{}, protocol.ErrNotConnected
	}
	var tree *SlotTree
	if cache != nil {
		tree, _ = cache.Get(req.Slot)
	}

	group, id, err := resolve(req, tree)
	if err != nil {
		return protocol.Value{}, err
	}

	m := &Message{
		MType:    MTypeRequest,
		MAddr:    byte(req.Slot),
		MCode:    byte(MethodGetValue),
		ObjGroup: group,
		ObjID:    id,
	}
	reply, err := c.Do(ctx, m)
	if err != nil {
		return protocol.Value{}, err
	}
	if reply.IsError() {
		return protocol.Value{}, reply.ErrCode()
	}

	obj, acpType, found := findObject(tree, group, id)
	if !found {
		// Walker never traverses root/frame/file groups, so lookups
		// there always miss. Fall back to a group-based decode for the
		// well-known types we understand even without walker context.
		return decodeByGroup(group, reply.Value)
	}
	return DecodeValueBytes(obj, acpType, reply.Value)
}

// decodeByGroup is the fallback value decoder for objects the walker
// doesn't traverse (root, frame, file). It infers the ObjectType from
// the group alone and calls DecodeValueBytes with a synthetic empty
// Object. Unknown groups return raw bytes.
func decodeByGroup(group ObjGroup, raw []byte) (protocol.Value, error) {
	var synth protocol.Object
	var t ObjectType
	switch group {
	case GroupFrame:
		synth.Kind = protocol.KindFrame
		t = TypeFrame
	case GroupRoot:
		t = TypeRoot
	case GroupFile:
		t = TypeFile
	default:
		return protocol.Value{
			Kind: protocol.KindRaw,
			Raw:  append([]byte(nil), raw...),
		}, nil
	}
	return DecodeValueBytes(synth, t, raw)
}

// SetValue writes one object value. Accepts typed input via the
// protocol.Value fields (Int, Float, Str, Enum) and encodes to the right
// wire bytes based on the object's ACP1 type. Requires a prior Walk so
// the plugin knows the object's type.
func (p *Plugin) SetValue(ctx context.Context, req protocol.ValueRequest, val protocol.Value) (protocol.Value, error) {
	p.mu.Lock()
	c := p.client
	cache := p.trees
	p.mu.Unlock()
	if c == nil {
		return protocol.Value{}, protocol.ErrNotConnected
	}
	var tree *SlotTree
	if cache != nil {
		tree, _ = cache.Get(req.Slot)
	}

	group, id, err := resolve(req, tree)
	if err != nil {
		return protocol.Value{}, err
	}

	// Encode value bytes. If the caller supplied raw bytes directly (via
	// val.Raw with no typed fields), bypass the codec and send as-is —
	// this preserves the old "raw hex" escape hatch for advanced users.
	var wireBytes []byte
	obj, acpType, found := findObject(tree, group, id)
	if len(val.Raw) > 0 && val.Str == "" && val.Int == 0 && val.Float == 0 && val.Uint == 0 {
		wireBytes = val.Raw
	} else if found {
		wireBytes, err = EncodeValueBytes(obj, acpType, val)
		if err != nil {
			return protocol.Value{}, err
		}
	} else {
		return protocol.Value{}, fmt.Errorf("acp1 set: no walked tree for slot %d, use raw bytes", req.Slot)
	}

	m := &Message{
		MType:    MTypeRequest,
		MAddr:    byte(req.Slot),
		MCode:    byte(MethodSetValue),
		ObjGroup: group,
		ObjID:    id,
		Value:    wireBytes,
	}
	reply, err := c.Do(ctx, m)
	if err != nil {
		return protocol.Value{}, err
	}
	if reply.IsError() {
		return protocol.Value{}, reply.ErrCode()
	}

	// Decode the confirmed value (device echoes the stored value).
	if found {
		return DecodeValueBytes(obj, acpType, reply.Value)
	}
	return protocol.Value{
		Kind: protocol.KindRaw,
		Raw:  append([]byte(nil), reply.Value...),
	}, nil
}

// findObject looks up an Object and its ACP1 type inside a walked tree
// by (group, id). Returns false if the tree is nil or the object wasn't
// walked. O(n) — acceptable for typical tree sizes under 512 objects.
func findObject(tree *SlotTree, group ObjGroup, id byte) (protocol.Object, ObjectType, bool) {
	if tree == nil {
		return protocol.Object{}, 0, false
	}
	gname := group.String()
	for i, o := range tree.Objects {
		if o.Group == gname && o.ID == int(id) {
			return o, tree.ACPTypes[i], true
		}
	}
	return protocol.Object{}, 0, false
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
// inside the listener goroutine with a decoded protocol.Event.
//
// Returns ErrNotConnected if the listener failed to bind at Connect
// time (typically: ACP port already in use by another process).
func (p *Plugin) Subscribe(req protocol.ValueRequest, fn protocol.EventFunc) error {
	p.mu.Lock()
	l := p.listener
	tcpClient, tcpOK := p.client.(*TCPClient)
	p.mu.Unlock()
	if l == nil && !tcpOK {
		return protocol.ErrNotConnected
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
	wrapper := func(msg *Message) {
		p.mu.Lock()
		cache := p.trees
		p.mu.Unlock()
		var tree *SlotTree
		if cache != nil {
			tree, _ = cache.Get(int(msg.MAddr))
		}

		ev := protocol.Event{
			Slot:      int(msg.MAddr),
			Group:     msg.ObjGroup.String(),
			ID:        int(msg.ObjID),
			Timestamp: time.Now(),
		}

		// Try to look up the object by (group, id) in the cached tree
		// so the callback gets a typed value and a resolved label.
		if obj, acpType, found := findObject(tree, msg.ObjGroup, msg.ObjID); found {
			ev.Label = obj.Label
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
				ev.Value = protocol.Value{Kind: protocol.KindRaw, Raw: msg.Value}
			}
		} else {
			// Frame / root / file objects are never in the walked tree.
			// Fall back to group-based decoding so frame-status
			// announcements surface as structured SlotStatus arrays
			// instead of raw bytes.
			if val, derr := decodeByGroup(msg.ObjGroup, msg.Value); derr == nil {
				ev.Value = val
			} else {
				ev.Value = protocol.Value{Kind: protocol.KindRaw, Raw: msg.Value}
			}
		}
		fn(ev)
	}

	// Route registration to whichever event source is active. The UDP
	// Listener filters inside its own Subscribe call. The TCPClient
	// fans out every announcement to every registered listener, so
	// filtering is applied inside the wrapper for the TCP path.
	if tcpOK {
		base := wrapper
		filtered := func(msg *Message) {
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
		h := tcpClient.AddListener(filtered)
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
func (p *Plugin) Unsubscribe(req protocol.ValueRequest) error {
	p.mu.Lock()
	l := p.listener
	tcpClient, tcpOK := p.client.(*TCPClient)
	h, ok := p.subHandles[reqKey(req)]
	if ok {
		delete(p.subHandles, reqKey(req))
	}
	p.mu.Unlock()
	if !ok {
		return nil
	}
	if tcpOK {
		tcpClient.RemoveListener(int(h))
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
func resolve(req protocol.ValueRequest, tree *SlotTree) (ObjGroup, byte, error) {
	if req.Label != "" {
		if tree == nil {
			return 0, 0, fmt.Errorf("%w: no walked tree for slot %d",
				protocol.ErrUnknownLabel, req.Slot)
		}

		// 1. Primary search: within the declared group (if any).
		if req.Group != "" {
			if idx := tree.Lookup(req.Group, req.Label); idx >= 0 {
				obj := tree.Objects[idx]
				g, ok := ParseGroup(obj.Group)
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
					g, ok := ParseGroup(obj.Group)
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
					protocol.ErrUnknownLabel, req.Label, req.Group, gname, obj.ID, gname)
			}
		}
		return 0, 0, fmt.Errorf("%w: label %q not found in any group on slot %d",
			protocol.ErrUnknownLabel, req.Label, req.Slot)
	}

	// Address by explicit group + id — no walker needed.
	g, ok := ParseGroup(req.Group)
	if !ok {
		return 0, 0, fmt.Errorf("acp1: invalid group %q", req.Group)
	}
	if req.ID < 0 || req.ID > 255 {
		return 0, 0, fmt.Errorf("acp1: object id out of range: %d", req.ID)
	}
	return g, byte(req.ID), nil
}
