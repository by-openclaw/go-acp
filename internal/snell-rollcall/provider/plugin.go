package rollcall

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"dhs/internal/clock"
	"dhs/internal/export/canonical"
	"dhs/internal/metrics"
	"dhs/internal/plugin"
	"dhs/internal/provider"
	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
	"dhs/internal/transport"
)

// DefaultPort is the IPShare port a gateway listens on.
const DefaultPort = 2050

// Register this provider on import.
func init() {
	provider.Register(&Factory{})
}

// Factory builds Provider instances.
type Factory struct{}

// Meta describes the provider to the registry.
func (f *Factory) Meta() provider.Meta {
	return provider.Meta{
		Name:        "rollcall",
		DefaultPort: DefaultPort,
		Description: "Snell RollCall gateway over IPShare, 16-bit and 32-bit generations",
	}
}

// New builds a provider serving a tree.
func (f *Factory) New(deps plugin.Deps, tree *canonical.Export) provider.Provider {
	return New(deps, tree)
}

// New builds a provider. It binds nothing: Serve does that, through the
// injected transport.
func New(deps plugin.Deps, tree *canonical.Export) *Provider {
	deps = deps.WithDefaults()

	p := &Provider{
		log:   deps.Logger,
		net:   deps.Net,
		clk:   deps.Clock,
		met:   deps.Metrics,
		deps:  deps,
		model: buildModel(tree, "dhs rollcall"),
		files: make(map[string][]byte),
		links: make(map[*session.Link]*linkState),
		done:  make(chan struct{}),
		unit:  defaultUnit,

		// A frame speaks the newer generation unless it is told to be an older
		// one, because that is what the tree it serves can express.
		longStrings:   true,
		longStringsAt: make(map[uint8]bool),
	}

	// The Control Panel will not render a device without this. It reads the
	// archive over the file service before it draws anything, which the menu
	// service alone cannot supply.
	// One template per card, because that is how a real frame serves it: each
	// node's file service is rooted at its own directory, and two cards can
	// carry different menus.
	p.templates = make(map[uint8][]byte)
	p.refreshGateway()
	for _, n := range append([]uint8{0}, p.model.portNumbers()...) {
		p.templates[n] = buildTemplate(p.model.port(n))
	}
	return p
}

// Provider serves a tree as a RollCall gateway.
type Provider struct {
	log  *slog.Logger
	net  transport.Net
	clk  clock.Clock
	met  *metrics.Connector
	deps plugin.Deps

	model *model

	// longStrings says whether this frame advertises SV_LONGSTR by default,
	// and longStringsAt overrides it for one card.
	//
	// A rack is not one generation. Services are advertised per unit and a
	// session is negotiated with the node it is opened on, so an older card
	// that speaks only the 16-bit forms can sit behind a gateway that speaks
	// both, and a client talking to two cards in one frame can be in two
	// generations at once.
	longStrings   bool
	longStringsAt map[uint8]bool

	mu       sync.RWMutex
	files    map[string][]byte

	// templates are the per-card archives a Control Panel reads, keyed by
	// port. They are not in files because a name means a different file
	// depending on which card was asked.
	templates map[uint8][]byte
	listener net.Listener
	links    map[*session.Link]*linkState
	addr     string

	// unit is the address this gateway presents as. A provider knows its own,
	// unlike a client, because it is the one doing the stamping.
	unit uint8

	done     chan struct{}
	doneOnce sync.Once
	wg       sync.WaitGroup

	comp compliance
}

// linkState is what a provider tracks per connection.
type linkState struct {
	mu       sync.Mutex
	sessions map[int16]*session.Session

	// nextPort is the port number stamped into the next client's address.
	// A gateway hands out ports from the top of its Ethernet range downwards,
	// which is where the vendor puts its own connection slots.
	assigned uint8

	// handles are the files clients have open on this link, each remembering
	// which session opened it so a session that ends takes its own with it.
	handles map[int16]openFile
	nextH   int16

	// open is the multi-packet transfer each session has in progress. It
	// lives here rather than in a package variable so two links, or two
	// providers in one process, cannot overwrite each other's whenever their
	// session indices happen to collide.
	open map[int16]*transfer

	// subscribed holds the sessions that asked for pushes, each with the
	// queue and the goroutine that sends them.
	subscribed map[int16]*subscriber
}

// setTransfer records the transfer a session has open.
func (st *linkState) setTransfer(index int16, t *transfer) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.open[index] = t
}

// transfer returns what a session has open, or nil.
func (st *linkState) transfer(index int16) *transfer {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.open[index]
}

// forget removes everything a session left behind: its transfer, its
// subscription and the pump that served it, and any file it left open.
func (st *linkState) forget(index int16) {
	st.closeHandlesOf(index)

	st.mu.Lock()
	defer st.mu.Unlock()

	delete(st.sessions, index)
	delete(st.open, index)
	if sub, ok := st.subscribed[index]; ok {
		delete(st.subscribed, index)
		close(sub.done)
	}
}

var _ provider.Provider = (*Provider)(nil)

// Serve binds and answers until the context ends.
func (p *Provider) Serve(ctx context.Context, addr string) error {
	if addr == "" {
		addr = fmt.Sprintf(":%d", DefaultPort)
	}

	ln, err := p.net.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("rollcall: listen %s: %w", addr, err)
	}

	p.mu.Lock()
	p.listener = ln
	p.addr = ln.Addr().String()
	p.mu.Unlock()

	p.refreshGateway()
	p.log.Info("rollcall: serving", "addr", p.addr, "ports", len(p.model.portNumbers()))

	// Closing the listener is what unblocks Accept, so the context has to
	// reach it rather than only the accept loop. The third case is this call
	// returning on its own — a listener that failed — because otherwise the
	// wait below would be waiting for a goroutine waiting for it.
	stopped := make(chan struct{})
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		select {
		case <-ctx.Done():
		case <-p.done:
		case <-stopped:
		}
		_ = ln.Close()
	}()

	err = p.accept(ctx, ln)
	close(stopped)
	p.wg.Wait()
	return err
}

// accept answers connections until the listener stops.
func (p *Provider) accept(ctx context.Context, ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-p.done:
				return nil
			default:
			}
			return fmt.Errorf("rollcall: accept: %w", err)
		}
		p.serveConn(conn)
	}
}

// serveConn attaches a link to a new connection.
func (p *Provider) serveConn(conn net.Conn) {
	cfg := session.Config{
		Handler: p,
		Local:   session.Address{Unit: p.unit},
		// A server does not probe its clients. A client that stops talking
		// stops being a client, and its socket closing is what says so; a
		// server probing every panel that connected to it multiplies traffic
		// by the number of panels for no information.
		KeepaliveInterval: -1,
	}

	l := session.NewLink(conn, cfg, p.deps)

	p.mu.Lock()
	p.links[l] = &linkState{
		sessions:   make(map[int16]*session.Session),
		handles:    make(map[int16]openFile),
		open:       make(map[int16]*transfer),
		subscribed: make(map[int16]*subscriber),
		assigned:   firstClientPort,
	}
	p.mu.Unlock()

	// Say we are here, and keep saying it.
	//
	// A client builds its picture of the network by listening for Iam (spec
	// 7.5): "Normally when a unit joins the network it obtains its own map of
	// the network by monitoring SP_IAM messages." A gateway that answers the
	// device enquiry and then never announces leaves the client with an empty
	// network, and the vendor Control Panel gives up on it after five seconds
	// - measured, by capturing the bytes: enquiry, our reply, the panel's own
	// Iam naming itself "ControlPanel", silence, disconnect.
	//
	// Only the gateway announces. A frame's cards are ports of it and are
	// found through the port service, not by announcing themselves (spec 7.6).
	session.NewAnnouncer(context.Background(), l, session.Identity{
		Info: p.gatewayInfo(),
	})

	p.log.Debug("rollcall: client connected", "remote", conn.RemoteAddr().String())

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		<-l.Done()

		p.mu.Lock()
		delete(p.links, l)
		p.mu.Unlock()

		p.log.Debug("rollcall: client gone", "remote", conn.RemoteAddr().String())
	}()
}

// gatewayInfo is what this provider announces about itself: port zero of its
// own unit, which is the gateway rather than any card in it.
func (p *Provider) gatewayInfo() codec.DeviceInfo {
	// Through identityOf so the announcement says the same about this frame
	// as an enquiry does, including which generation it offers.
	id, _ := p.identityOf(0)
	return codec.DeviceInfo{
		ProtocolVersion: codec.ProtocolVersion,
		Address:         codec.Address{Unit: p.unit, Port: 0, Index: codec.IndexUnknown},
		ID:              id,
		Status:          p.statusOf(0),
	}
}

// defaultUnit is the unit number a gateway presents as.
//
// It may not be zero. Zero is the unit a client addresses before it knows
// anything — the broadcast address is 0000-00-00 — so a gateway that answers
// as unit zero tells a client that its address was never assigned, and the
// client keeps using the broadcast one.
const defaultUnit uint8 = 1

// SetUnit changes the unit number this gateway presents as, for a plant whose
// numbering is decided elsewhere. It must be called before Serve.
func (p *Provider) SetUnit(u uint8) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.unit = u
}

// SetLongStrings chooses which generation this frame offers.
//
// A frame that does not advertise SV_LONGSTR cannot be asked for the newer
// generation at all, so every client on it speaks 16-bit. It is how a 16-bit
// device is emulated without a second implementation: the same tree, the same
// menus, the same values, projected into the older forms by the code that
// already does that for a 16-bit client of a 32-bit frame.
//
// It takes effect on the next connection, because a session's generation is
// fixed when it is called.
func (p *Provider) SetLongStrings(on bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.longStrings = on
}

// SetLongStringsAt chooses the generation of one card, whatever the frame does.
//
// A rack holds cards of different ages. The service mask is per unit, so an
// old card offering no long strings is reached in the 16-bit forms while the
// card beside it is reached in the newer ones, on the same connection.
func (p *Provider) SetLongStringsAt(port uint8, on bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.longStringsAt[port] = on
}

// advertiseAt masks off what one node does not offer.
func (p *Provider) advertiseAt(port uint8, s codec.Service) codec.Service {
	p.mu.RLock()
	long, ok := p.longStringsAt[port]
	if !ok {
		long = p.longStrings
	}
	p.mu.RUnlock()

	if !long {
		s &^= codec.SvcLongStr
	}
	return s
}

// advertise masks off what the frame as a whole does not offer, which is what
// the gateway itself says.
func (p *Provider) advertise(s codec.Service) codec.Service {
	return p.advertiseAt(0, s)
}

// firstClientPort is the port number the first client on a link is given.
//
// The vendor's gateway hands out ports from 0xE0 upwards for Ethernet clients,
// which keeps them clear of the card slots below and of the RS-422 port at
// 0xFF.
const firstClientPort uint8 = 0xE0

// Stop closes the listener and every link.
func (p *Provider) Stop() error {
	p.doneOnce.Do(func() { close(p.done) })

	p.mu.Lock()
	ln := p.listener
	p.listener = nil
	links := make([]*session.Link, 0, len(p.links))
	for l := range p.links {
		links = append(links, l)
	}
	p.mu.Unlock()

	if ln != nil {
		_ = ln.Close()
	}
	for _, l := range links {
		_ = l.Close()
	}
	p.wg.Wait()
	return nil
}

// Addr returns the address actually bound, which a caller needs when it asked
// for port zero.
func (p *Provider) Addr() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.addr
}

// SetValue changes a served value and tells every subscriber.
//
// The path is the canonical tree's own dotted identifier path, and the value
// that comes back is what was stored: a numeric write is clamped to the line's
// range, exactly as a device does, so a caller learns what it got.
func (p *Provider) SetValue(ctx context.Context, path string, val any) (any, error) {
	slot, command, port, ok := p.resolve(path)
	if !ok {
		return nil, fmt.Errorf("rollcall: no object at %q", path)
	}

	mode, num, text := encodeAny(port, command, val)
	stored, err := port.setValue(command, mode, num, text)
	if err != nil {
		return nil, fmt.Errorf("rollcall: set %q: %w", path, err)
	}

	p.publish(ctx, slot, stored)
	return decodeStored(stored), nil
}

// resolve finds the port and command a path names.
func (p *Provider) resolve(path string) (slot uint8, command uint32, prt *port, ok bool) {
	for _, n := range p.model.portNumbers() {
		pt := p.model.port(n)
		if cmd, found := pt.commandForPath(path); found {
			return n, cmd, pt, true
		}
	}
	return 0, 0, nil, false
}

// encodeAny turns a caller's value into the wire form the line expects.
func encodeAny(prt *port, command uint32, val any) (codec.Mode, int32, string) {
	switch v := val.(type) {
	case string:
		return codec.ModeString, 0, v
	case bool:
		if v {
			return codec.ModeValue, 1, ""
		}
		return codec.ModeValue, 0, ""
	default:
		scale := float64(1)
		if l, ok := prt.lineFor(command); ok {
			scale = float64(l.Scale())
		}
		return codec.ModeValue, int32(numberOf(val) * scale), ""
	}
}

// decodeStored renders a stored value for a caller that speaks no RollCall.
func decodeStored(v codec.Value) any {
	if v.Mode.Has(codec.ModeString) {
		return v.Text
	}
	return int64(v.Val)
}

// lineFor returns the menu line behind a command.
func (p *port) lineFor(command uint32) (line, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	i, ok := p.byCmd[command]
	if !ok {
		return line{}, false
	}
	return p.lines[i], true
}

// SetDisplay sets one of a slot's status lines and pushes it to subscribers.
//
// Lines zero to three are the front panel; minus one and minus two are an
// error and a warning, which are priorities rather than positions.
func (p *Provider) SetDisplay(ctx context.Context, slot int, n int16, text string) error {
	prt := p.model.port(uint8(slot))
	if prt == nil {
		return fmt.Errorf("rollcall: no slot %d", slot)
	}
	prt.setDisplay(n, text)

	// Cut to the field first, so the encode cannot refuse it.
	payload, _ := codec.Disp{Line: n, Text: codec.TruncateFixed(text, codec.MaxTextSize)}.AppendTo(nil)
	p.pushToSlot(ctx, uint8(slot), codec.MsgDispData, payload)
	return nil
}

// AddFile makes a file available over the file service.
//
// The name is reduced to the form a request is matched against: a client may
// ask for the same file with a device root in front of it, with either
// separator, in any case, and all of those must find it.
func (p *Provider) AddFile(name string, body []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.files[cleanPath(name)] = append([]byte(nil), body...)
}

// Files returns the names on offer, which is what a directory listing walks.
func (p *Provider) Files() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	out := make([]string, 0, len(p.files)+1)
	for name := range p.files {
		out = append(out, name)
	}
	// Every card serves a template, under one name and with different bytes,
	// so the name belongs in the list once.
	if len(p.templates) > 0 {
		out = append(out, cleanPath(TemplateFileName))
	}
	return out
}

// filesAt is what one card offers: everything the provider serves, plus its
// own template when it has one.
func (p *Provider) filesAt(port uint8) []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	out := make([]string, 0, len(p.files)+1)
	for name := range p.files {
		out = append(out, name)
	}
	if _, ok := p.templates[port]; ok {
		out = append(out, cleanPath(TemplateFileName))
	}
	return out
}

// fileAt resolves a name against one card.
//
// Every card serves its own template under the same name, so the name alone
// does not identify the bytes. Everything else a provider serves is the same
// whichever card asked.
func (p *Provider) fileAt(port uint8, name string) ([]byte, bool) {
	if name == cleanPath(TemplateFileName) {
		p.mu.RLock()
		b, ok := p.templates[port]
		p.mu.RUnlock()
		if ok {
			return b, true
		}
	}
	return p.file(name)
}

func (p *Provider) file(name string) ([]byte, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	b, ok := p.files[name]
	return b, ok
}
