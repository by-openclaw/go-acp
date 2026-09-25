package mnset

import (
	"context"
	"fmt"
	"log/slog"
	stdhttp "net/http"
	"strings"
	"sync"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/consumer/monitor"
	"dhs/internal/consumer/pollwatch"
	"dhs/internal/plugin"
)

// Name is the registry name: dhs consumer mnset <verb>.
const Name = "mnset"

// DefaultPort is the emSFP node API port on every module.
const DefaultPort = 80

// staleAfter is how long a read value stays "current" before watch
// marks it cache: the module is polled, so one poll interval plus slack.
const staleAfter = 30 * time.Second

func init() {
	consumer.Register(&Factory{})
}

// Factory builds Plugin instances.
type Factory struct{}

// Meta describes the plugin to the registry.
func (f *Factory) Meta() consumer.ProtocolMeta {
	return consumer.ProtocolMeta{
		Name:        Name,
		DefaultPort: DefaultPort,
		Description: "Riedel MuoN eMSFP / FusioN — direct REST control (emsfp/node/v1); host = one module (slot 0) or MN SET :8080 (frame, one slot per module)",
	}
}

// New builds a plugin from the injected dependency set.
func (f *Factory) New(deps plugin.Deps) consumer.Protocol {
	deps = deps.WithDefaults()
	p := &Plugin{logger: deps.Logger, timeout: defaultTimeout, modulePort: DefaultPort, framePort: FramePort,
		lastWalk: map[int][]consumer.Object{}, docs: map[string]cachedDoc{}}
	p.Init(deps, staleAfter)
	p.poller = pollwatch.New(p, p.pollProfileFor, deps.Logger, deps.Clock)
	return p
}

// Plugin is one module (host = module: one slot, 0) or one frame (host
// = MN SET on :8080: one slot per managed module, ordered by module
// MAC). Channels (Device CH1..CH8) are entries inside devices /
// receivers / senders / flows, addressed by path — which keeps the
// generic export / import / get / set verbs unchanged.
type Plugin struct {
	consumer.Base

	// Transport is the injected RoundTripper (nil = net/http default).
	// Tests substitute one; production leaves it nil.
	Transport stdhttp.RoundTripper

	logger     *slog.Logger
	timeout    time.Duration
	modulePort int    // node API port of the modules behind a frame
	framePort  int    // the port that means "this host is MN SET" (8080)
	user, pass string // MN SET login, only used when /api/device is gated

	mu   sync.Mutex
	host string
	port int
	// slots is the frame: one entry per module. Module mode is a
	// one-entry frame whose slot is the host itself.
	slots []slotModule
	// deviations are what the last Walk could not read.
	deviations []string
	// lastWalk keeps each slot's last walked leaves: the poll plan is
	// expanded over them, so a watch after a walk costs no second walk.
	lastWalk map[int][]consumer.Object
	// docs is a short-lived cache of fetched documents, so the leaves of
	// one record polled in the same second cost the module one GET.
	docs map[string]cachedDoc
	// poller answers Subscribe by polling (the module has no push).
	poller *pollwatch.Poller
	// frameStop ends the frame-refresh loop of a frame-wide Subscribe;
	// frameDone closes when that loop has exited.
	frameStop context.CancelFunc
	frameDone chan struct{}
}

// cachedDoc is one fetched document with its fetch time.
type cachedDoc struct {
	doc any
	at  time.Time
}

// docTTL bounds how long a fetched document answers resolves. Two
// seconds: a poll of 36 receivers' pkt_cnt reads 36 documents once,
// not 36 x 3 URLs; an operator get after a set still sees the module
// (SetValue always reads fresh).
const docTTL = 2 * time.Second

// frameRefresh is how often a frame-wide watch re-reads MN SET's device
// list to notice a module appearing, vanishing or falling silent.
var frameRefresh = 10 * time.Second

// SetTimeout bounds each request; applied at the next Connect.
func (p *Plugin) SetTimeout(d time.Duration) {
	p.mu.Lock()
	p.timeout = d
	p.mu.Unlock()
}

// SetModulePort sets the node API port used for the modules behind a
// frame (default 80). Tests point it at a fake.
func (p *Plugin) SetModulePort(port int) {
	p.mu.Lock()
	p.modulePort = port
	p.mu.Unlock()
}

// SetFramePort sets the port on which a host is taken for MN SET
// (default 8080). Tests point it at a fake.
func (p *Plugin) SetFramePort(port int) {
	p.mu.Lock()
	p.framePort = port
	p.mu.Unlock()
}

// SetCredentials gives the MN SET login used only when the frame's
// /api/device is token-gated. Never logged, never printed.
func (p *Plugin) SetCredentials(user, pass string) {
	p.mu.Lock()
	p.user, p.pass = user, pass
	p.mu.Unlock()
}

// Connect opens the host. Port 8080 (MN SET) is a frame; any other
// port is a module, proven by self/information. Port 0 tries the module
// on 80 first and MN SET on 8080 second, so `dhs consumer mnset info
// <ip>` works for both without a flag. Callable again to reconnect.
func (p *Plugin) Connect(ctx context.Context, ip string, port int) error {
	var slots []slotModule
	var err error
	switch {
	case port == p.framePort:
		slots, err = p.connectFrame(ctx, ip, port)
	case port <= 0:
		port = DefaultPort
		if slots, err = p.connectModule(ctx, ip, port); err != nil {
			if fslots, ferr := p.connectFrame(ctx, ip, p.framePort); ferr == nil {
				slots, err, port = fslots, nil, p.framePort
			}
		}
	default:
		slots, err = p.connectModule(ctx, ip, port)
	}
	if err != nil {
		return err
	}

	p.mu.Lock()
	p.host, p.port, p.slots = ip, port, slots
	p.mu.Unlock()
	p.Opened("tcp", ip, port, consumer.MetricsTimes{C: p.Metrics()})
	for i, s := range slots {
		if s.c != nil {
			p.logger.Info("mnset: connected", slog.Int("slot", i), slog.String("host", s.ip), slog.String("identity", identityOf(s.info)))
		}
	}
	return nil
}

// connectModule opens one module as a one-slot frame.
func (p *Plugin) connectModule(ctx context.Context, ip string, port int) ([]slotModule, error) {
	c := newClient(ip, port, p.timeout, p.Transport, p.Metrics(), p.Recorder())
	info, err := c.get(ctx, "self/information")
	if err != nil {
		return nil, fmt.Errorf("mnset connect %s:%d: %w", ip, port, err)
	}
	m, _ := info.(map[string]any)
	return []slotModule{{ip: ip, status: "ONLINE", typ: str(m["type"]), serial: str(m["serial_number"]), c: c, info: info, cages: readCages(ctx, c)}}, nil
}

// Disconnect forgets the host and stops every watch. Safe when not
// connected.
func (p *Plugin) Disconnect() error {
	p.poller.Close()
	p.mu.Lock()
	was := p.slots != nil
	p.slots = nil
	stop, done := p.frameStop, p.frameDone
	p.frameStop, p.frameDone = nil, nil
	p.docs = map[string]cachedDoc{}
	p.mu.Unlock()
	if stop != nil {
		stop()
		<-done
	}
	if was {
		p.Closed()
	}
	return nil
}

// slot returns the module behind a slot number, or why not.
func (p *Plugin) slot(n int) (*slotModule, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.slots == nil {
		return nil, consumer.ErrNotConnected
	}
	if n < 0 || n >= len(p.slots) {
		return nil, fmt.Errorf("mnset: slot %d (frame has %d): %w", n, len(p.slots), consumer.ErrObjectNotFound)
	}
	return &p.slots[n], nil
}

// clientFor returns the live client of a slot, or why it cannot answer.
func (p *Plugin) clientFor(n int) (*client, error) {
	s, err := p.slot(n)
	if err != nil {
		return nil, err
	}
	if s.c == nil {
		return nil, fmt.Errorf("mnset: slot %d (%s, MN SET says %s) is not answering: %w", n, s.ip, s.status, consumer.ErrNotConnected)
	}
	return s.c, nil
}

// GetDeviceInfo reports the frame: one slot per module.
func (p *Plugin) GetDeviceInfo(ctx context.Context) (consumer.DeviceInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.slots == nil {
		return consumer.DeviceInfo{}, consumer.ErrNotConnected
	}
	return consumer.DeviceInfo{IP: p.host, Port: p.port, NumSlots: len(p.slots), ProtocolVersion: 1}, nil
}

// GetSlotInfo reports one module: present and online when it answers,
// error when MN SET lists it ONLINE but it does not, no card when MN
// SET lists it OFFLINE. Identity carries ip / serial / type / lldp.
func (p *Plugin) GetSlotInfo(ctx context.Context, slot int) (consumer.SlotInfo, error) {
	s, err := p.slot(slot)
	if err != nil {
		return consumer.SlotInfo{}, err
	}
	return slotInfoOf(slot, *s), nil
}

// IdentityProbe returns the ADR-0022 DM key of a slot's module,
// "<base_type>@<current_version>" — FusioN6@0x68cd783f — so an export
// lands under .cache/dm/mnset/ per firmware.
func (p *Plugin) IdentityProbe(ctx context.Context, slot int) (string, error) {
	s, err := p.slot(slot)
	if err != nil {
		return "", err
	}
	if s.c == nil {
		return "", fmt.Errorf("mnset: slot %d is not answering: %w", slot, consumer.ErrNotConnected)
	}
	return identityOf(s.info), nil
}

// identityOf renders the DM key from self/information; a module that
// hides either field files under emsfp@unknown rather than failing.
func identityOf(info any) string {
	model, ver := "emsfp", "unknown"
	if m, ok := info.(map[string]any); ok {
		if s, ok := m["base_type"].(string); ok && s != "" {
			model = s
		}
		if s, ok := m["current_version"].(string); ok && s != "" {
			ver = s
		}
	}
	return model + "@" + ver
}

// Walk descends the module's own listings from the root and flattens
// every document it reaches. A resource the module lists but does not
// serve is a deviation, kept for Deviations() and logged, and the walk
// continues — a partial module is still worth what it did say. A
// cancelled context stops it.
func (p *Plugin) Walk(ctx context.Context, slot int) ([]consumer.Object, error) {
	c, err := p.clientFor(slot)
	if err != nil {
		return nil, err
	}
	var objs []consumer.Object
	var dev []string
	if err := p.walkResource(ctx, c, "", 0, &objs, &dev); err != nil {
		return nil, err
	}
	labelObjects(objs)
	annotate(objs)
	for i := range objs {
		objs[i].Slot = slot
	}
	p.mu.Lock()
	p.deviations = dev
	p.lastWalk[slot] = objs
	p.mu.Unlock()
	// One line per walk, not one per resource: a FusioN6 lists six
	// route/bulk/receiver items that answer 400 to GET (they are
	// POST-only actions), and that must not drown a walk's log.
	if len(dev) > 0 {
		p.logger.Warn("mnset: walk: listed resources not readable",
			slog.Int("slot", slot), slog.Int("count", len(dev)), slog.String("first", dev[0]), slog.Int("objects", len(objs)))
		for _, d := range dev {
			p.logger.Debug("mnset: walk: deviation", slog.String("deviation", d))
		}
	}
	return objs, nil
}

// walkResource fetches one URL: a listing descends, anything else
// flattens. The only error returned is the context's; the module's
// refusals go to dev.
func (p *Plugin) walkResource(ctx context.Context, c *client, url string, depth int, objs *[]consumer.Object, dev *[]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	doc, err := c.get(ctx, url)
	if err != nil {
		*dev = append(*dev, fmt.Sprintf("%s: %v", url, err))
		return nil
	}
	if names, ok := listing(doc); ok {
		if depth >= maxListingDepth {
			*dev = append(*dev, fmt.Sprintf("%s: listing nested deeper than %d, not descended", url, maxListingDepth))
			return nil
		}
		for _, n := range names {
			child := n
			if url != "" {
				child = url + "/" + n
			}
			if err := p.walkResource(ctx, c, child, depth+1, objs, dev); err != nil {
				return err
			}
		}
		return nil
	}
	access := uint8(accessRead)
	if isWritable(url) {
		access |= accessWrite
	}
	flatten(0, pathElems(url), doc, access, objs)
	return nil
}

// Deviations returns what the last Walk could not read.
func (p *Plugin) Deviations() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.deviations...)
}

// resolved is an operator path mapped onto the module: the URL of the
// document that owns it, that document, and the path left inside it.
type resolved struct {
	url  string
	doc  any
	leaf []string
}

// resolve follows the module's listings token by token — the same way
// Walk does — until a token lands on a document; the remaining tokens
// address a leaf inside it. Every step is one GET, so the answer is
// always the module's current shape, never a guessed catalogue.
func (p *Plugin) resolve(ctx context.Context, c *client, path string, cached bool) (resolved, error) {
	toks := splitPath(path)
	if len(toks) == 0 {
		return resolved{}, fmt.Errorf("mnset: %q: %w", path, consumer.ErrObjectNotFound)
	}
	get := c.get
	if cached {
		get = func(ctx context.Context, url string) (any, error) { return p.cachedGet(ctx, c, url) }
	}
	url := ""
	doc, err := get(ctx, url)
	if err != nil {
		return resolved{}, err
	}
	i := 0
	for {
		names, ok := listing(doc)
		if !ok {
			break
		}
		if i == len(toks) {
			return resolved{}, fmt.Errorf("mnset: %q is a list, not a value", path)
		}
		if !contains(names, toks[i]) {
			return resolved{}, fmt.Errorf("mnset: %q: %s not listed under /%s: %w", path, toks[i], url, consumer.ErrObjectNotFound)
		}
		if url == "" {
			url = toks[i]
		} else {
			url += "/" + toks[i]
		}
		i++
		if doc, err = get(ctx, url); err != nil {
			return resolved{}, err
		}
	}
	return resolved{url: url, doc: doc, leaf: toks[i:]}, nil
}

// cachedGet answers from the document cache inside docTTL, else fetches
// and remembers. Listings and documents alike.
func (p *Plugin) cachedGet(ctx context.Context, c *client, url string) (any, error) {
	key := c.base + url
	p.mu.Lock()
	if d, ok := p.docs[key]; ok && time.Since(d.at) < docTTL {
		p.mu.Unlock()
		return d.doc, nil
	}
	p.mu.Unlock()
	doc, err := c.get(ctx, url)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.docs[key] = cachedDoc{doc: doc, at: time.Now()}
	p.mu.Unlock()
	return doc, nil
}

func contains(names []string, s string) bool {
	for _, n := range names {
		if n == s {
			return true
		}
	}
	return false
}

// GetValue reads one leaf of req.Slot's module. A path that lands on a
// node or a list is refused — a value is a scalar; a text resource (an
// SDP) is its own value.
func (p *Plugin) GetValue(ctx context.Context, req consumer.ValueRequest) (consumer.Value, error) {
	c, err := p.clientFor(req.Slot)
	if err != nil {
		return consumer.Value{}, err
	}
	r, err := p.resolve(ctx, c, req.Path, true)
	if err != nil {
		return consumer.Value{}, err
	}
	cur, ok := lookup(r.doc, r.leaf)
	if !ok {
		return consumer.Value{}, fmt.Errorf("mnset: %q: %w", req.Path, consumer.ErrObjectNotFound)
	}
	switch cur.(type) {
	case map[string]any:
		return consumer.Value{}, fmt.Errorf("mnset: %q is a node, not a value", req.Path)
	case []any:
		return consumer.Value{}, fmt.Errorf("mnset: %q is a list, not a value", req.Path)
	}
	return leafValue(cur), nil
}

// SetValue is read-modify-write on req.Slot's module: resolve the
// owning document, replace the one field, PUT the whole document back
// to the same URL (the module refuses partial bodies), then read it
// again and return what the module holds now — the confirmed value,
// never the requested one.
func (p *Plugin) SetValue(ctx context.Context, req consumer.ValueRequest, val consumer.Value) (consumer.Value, error) {
	c, err := p.clientFor(req.Slot)
	if err != nil {
		return consumer.Value{}, err
	}
	r, err := p.resolve(ctx, c, req.Path, false)
	if err != nil {
		return consumer.Value{}, err
	}
	if !isWritable(r.url) {
		return consumer.Value{}, fmt.Errorf("mnset: %s is read-only", r.url)
	}
	if _, isText := r.doc.(string); isText {
		return consumer.Value{}, fmt.Errorf("mnset: %s is a text document, not a settable field", r.url)
	}
	if _, err := assign(r.doc, r.leaf, val); err != nil {
		return consumer.Value{}, fmt.Errorf("mnset: %w", err)
	}
	if err := c.put(ctx, r.url, r.doc); err != nil {
		return consumer.Value{}, err
	}
	after, err := c.get(ctx, r.url)
	if err != nil {
		return consumer.Value{}, fmt.Errorf("mnset: %q written, but read-back failed: %w", req.Path, err)
	}
	got, ok := lookup(after, r.leaf)
	if !ok {
		return consumer.Value{}, fmt.Errorf("mnset: %q written, but gone on read-back: %w", req.Path, consumer.ErrObjectNotFound)
	}
	return leafValue(got), nil
}

// PathNative declares that GetValue / SetValue resolve a path against
// the module's own listings (see resolve): the CLI must not walk the
// module (~330 GETs, 33 s on a FusioN6) just to answer one --path.
func (p *Plugin) PathNative() bool { return true }

// Subscribe: the module has no push channel (no WebSocket, no IS-07),
// so a watch is a poll: the dictionary's poll plan (which leaves, how
// often) expanded over the slot's tree and run by the neutral monitor;
// every change reaches fn. Slot -1 covers every present slot. A
// frame-wide watch (no path, MN SET as host) also re-reads MN SET's
// device list every frameRefresh and reports slots that appear, vanish
// or fall silent as events on path "slot". Events the module itself
// raises (no signal, PTP, temperature) travel by its syslog.
func (p *Plugin) Subscribe(req consumer.ValueRequest, fn consumer.EventFunc) error {
	if _, err := p.session(); err != nil {
		return err
	}
	if err := p.poller.Subscribe(req, fn); err != nil {
		return err
	}
	if req.Path == "" && p.isFrame() {
		p.startFrameRefresh(fn)
	}
	return nil
}

// Unsubscribe stops that watch.
func (p *Plugin) Unsubscribe(req consumer.ValueRequest) error {
	err := p.poller.Unsubscribe(req)
	if p.poller.Active() == 0 {
		p.mu.Lock()
		stop, done := p.frameStop, p.frameDone
		p.frameStop, p.frameDone = nil, nil
		p.mu.Unlock()
		if stop != nil {
			stop()
			<-done
		}
	}
	return err
}

// session reports whether the plugin is connected at all, and how many
// slots it holds.
func (p *Plugin) session() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.slots == nil {
		return 0, consumer.ErrNotConnected
	}
	return len(p.slots), nil
}

// isFrame reports whether the host is MN SET.
func (p *Plugin) isFrame() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.port == p.framePort
}

// pollProfileFor builds the monitor profile for one Subscribe: the
// dictionary's poll plan over the walked leaves of the requested slot
// (or of every present slot for -1), under the request's path filter.
// A slot never walked is walked now, once.
func (p *Plugin) pollProfileFor(ctx context.Context, req consumer.ValueRequest) (*monitor.Profile, error) {
	n, _ := p.session() // Subscribe checked the session; 0 slots just yields "nothing to watch"
	slots := []int{req.Slot}
	if req.Slot < 0 {
		slots = slots[:0]
		for i := 0; i < n; i++ {
			if s, err := p.slot(i); err == nil && s.c != nil {
				slots = append(slots, i)
			}
		}
	}
	d, _, err := parseDictionary(fusion6Dictionary, videoFormatsJSON)
	if err != nil {
		return nil, err
	}
	merged := &monitor.Profile{Model: d.Model}
	for _, slot := range slots {
		p.mu.Lock()
		objs, walked := p.lastWalk[slot]
		p.mu.Unlock()
		if !walked {
			if objs, err = p.Walk(ctx, slot); err != nil {
				return nil, err
			}
		}
		prof, err := pollProfile(d, objs, slot, req.Path)
		if err != nil {
			return nil, err
		}
		merged.Defaults = prof.Defaults
		merged.Entries = append(merged.Entries, prof.Entries...)
	}
	if len(merged.Entries) == 0 {
		return nil, fmt.Errorf("mnset: no present slot to watch")
	}
	return merged, nil
}

// startFrameRefresh re-reads MN SET's device list on a timer and reports
// slot changes as events: Path "slot", Label the slot's module id, Value
// the new state (present / error / no_card / removed), Description the
// module address.
func (p *Plugin) startFrameRefresh(fn consumer.EventFunc) {
	p.mu.Lock()
	if p.frameStop != nil {
		p.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	p.frameStop, p.frameDone = cancel, done
	host, port := p.host, p.port
	every := frameRefresh
	p.mu.Unlock()
	go func() {
		defer close(done)
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				p.refreshFrame(ctx, host, port, fn)
			}
		}
	}()
}

// refreshFrame is one frame-refresh pass: connectFrame again, diff the
// slot table against the held one by module id, emit one event per
// change, swap in the new table.
func (p *Plugin) refreshFrame(ctx context.Context, host string, port int, fn consumer.EventFunc) {
	fresh, err := p.connectFrame(ctx, host, port)
	if err != nil {
		p.logger.Warn("mnset: frame refresh failed", slog.String("err", err.Error()))
		return
	}
	p.mu.Lock()
	old := p.slots
	p.slots = fresh
	p.mu.Unlock()
	state := func(s slotModule) string { return slotInfoOf(0, s).Status.String() }
	seen := map[string]bool{}
	for i, s := range fresh {
		seen[s.id] = true
		prev, had := findSlot(old, s.id)
		if !had || state(prev) != state(s) {
			fn(consumer.Event{Slot: i, Path: "slot", Label: s.id, Description: s.ip,
				Value: consumer.Value{Kind: consumer.KindString, Str: state(s)}, Timestamp: time.Now()})
		}
	}
	for i, s := range old {
		if !seen[s.id] {
			fn(consumer.Event{Slot: i, Path: "slot", Label: s.id, Description: s.ip,
				Value: consumer.Value{Kind: consumer.KindString, Str: "removed"}, Timestamp: time.Now()})
		}
	}
}

func findSlot(slots []slotModule, id string) (slotModule, bool) {
	for _, s := range slots {
		if s.id == id {
			return s, true
		}
	}
	return slotModule{}, false
}

// String renders the plugin for logs: host and slot count, never a secret.
func (p *Plugin) String() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	ids := make([]string, 0, len(p.slots))
	for _, s := range p.slots {
		ids = append(ids, identityOf(s.info))
	}
	return strings.TrimSpace(fmt.Sprintf("mnset %s:%d %s", p.host, p.port, strings.Join(ids, ",")))
}
