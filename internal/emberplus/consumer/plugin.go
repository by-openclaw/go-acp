// Package emberplus is the Ember+ consumer plugin. See the package doc in
// types.go for the layered architecture (ber / s101 / glow / plugin /
// session). This file wires the consumer to the generic protocol.Protocol
// interface and maintains the in-RAM tree model.
//
// Tree model (spec-neutral, project-specific):
//
//	Each decoded Glow element becomes a treeEntry keyed primarily by its
//	numeric RelOID (e.g. "1.1.2"). A secondary index maps the human-readable
//	identifier path (e.g. "router.oneToN.matrix") back to the same entry;
//	the label index is a many-to-one map because labels can collide inside
//	the same provider (see Ember+ Documentation v2.50 p.74 "The identifier
//	property"). Every entry also records a Freshness state per
//	internal/emberplus/docs/consumer.md A2.
package emberplus

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"acp/internal/protocol"
	"acp/internal/protocol/compliance"
	"acp/internal/emberplus/codec/glow"
	"acp/internal/emberplus/codec/matrix"
	"acp/internal/transport"
)

func init() {
	protocol.Register(&Factory{})
}

// Factory registers the Ember+ plugin with the compile-time registry.
type Factory struct{}

// Meta publishes the static descriptor used by the plugin registry, CLI
// help, and API discovery endpoints.
func (f *Factory) Meta() protocol.ProtocolMeta {
	return protocol.ProtocolMeta{
		Name:        "emberplus",
		DefaultPort: DefaultPort,
		Description: "Ember+ (Glow/S101/TCP) consumer",
	}
}

// New constructs a fresh Plugin instance. Each device connection uses a
// separate Plugin so cached tree state cannot cross devices.
func (f *Factory) New(logger *slog.Logger) protocol.Protocol {
	return &Plugin{logger: logger}
}

// Freshness marks how current a treeEntry's value is believed to be.
// Documented in CLAUDE.md (Value freshness states) and
// internal/emberplus/docs/consumer.md A2.
type Freshness uint8

const (
	// FreshnessStale means the entry was loaded from disk cache and has
	// not yet been confirmed against the live provider. The UI shows
	// these values grayed / italicised.
	FreshnessStale Freshness = iota
	// FreshnessLive means the entry was observed from a walk, get, or
	// announce during this session.
	FreshnessLive
	// FreshnessUpdated is like FreshnessLive but set immediately after a
	// value-change announcement so the UI can flash the field.
	FreshnessUpdated
)

// Plugin implements protocol.Protocol for Ember+ providers.
type Plugin struct {
	logger  *slog.Logger
	session *Session
	mu      sync.Mutex

	// treeMu guards every *Index* map below. Writers hold the write
	// lock for the duration of a processElement call; readers use RLock.
	treeMu     sync.RWMutex
	numIndex   map[string]*treeEntry   // numeric RelOID → entry (primary)
	pathIndex  map[string]*treeEntry   // identifier path → entry (secondary)
	labelIndex map[string][]*treeEntry // bare identifier → entries (many-to-one)
	numPath    map[string]string       // numeric prefix → identifier (for string-path resolution)

	// subs is keyed by numeric path; "*" is the wildcard watch-all key.
	// streamSubs tracks the subset of subs that were established with an
	// explicit Command 30 (spec p.30–31) — we must send a matching
	// Command 31 to release them on Disconnect or Unsubscribe.
	// streamIndex maps a streamIdentifier (spec p.93 StreamEntry) to the
	// set of parameter paths that share it. A single stream identifier
	// may fan out across several parameters via StreamDescription offset.
	subs        map[string]protocol.EventFunc
	streamSubs  map[string][]int32
	streamIndex map[int64][]string
	subsMu      sync.RWMutex

	// templates is keyed by the canonical numeric RelOID of the
	// template; used by ResolveTemplate and TemplateFor callers.
	// Spec p.54–58 (Ember+ 1.4 Templates).
	templates   map[string]*glow.Template
	templatesMu sync.RWMutex

	// profile tracks tolerance events (spec deviations absorbed
	// during this session). Exposed via ComplianceProfile(); a
	// summary line is logged on Disconnect. See compliance/profile.go
	// and internal/emberplus/docs/consumer.md §A9.
	profile *compliance.Profile

	// recorder captures raw S101 frames (tx + rx) to a JSONL file
	// when the CLI passed --capture. Shared with the Session so the
	// reader/writer taps fire on every frame.
	recorder *transport.Recorder

	// connIP / connPort are captured at Connect time for log context.
	connIP   string
	connPort int

	// sessionConnected mirrors the underlying session's liveness.
	// Updated by onSessionStateChange; read by SetValue to gate
	// writes and by the watch event synthesiser. Guarded by p.mu.
	sessionConnected bool

	// pendingSets tracks in-flight SetValue calls awaiting the
	// provider's confirming announce. Keyed by the parameter's
	// numeric OID string.
	pendingSets *pendingSetRegistry

	// reconnect drives the auto-redial goroutine that fires on
	// unsolicited disconnect and exits when a fresh session is
	// established or the user calls Disconnect.
	reconnect reconnectCtrl
}

// ComplianceProfile returns the live compliance profile for this
// connection. Callers use it to classify the peer provider (strict
// vs partial) and to drive a compatibility matrix.
func (p *Plugin) ComplianceProfile() *compliance.Profile {
	return p.profile
}

// SetRecorder attaches a raw-traffic recorder to this plugin. When set,
// every S101 frame (both TX and RX) is written to the recorder with
// proto="emberplus" and the raw bytes include BOF/EOF/CRC, so the
// capture file is sufficient input for replay-based unit tests.
// Call before Connect.
func (p *Plugin) SetRecorder(r *transport.Recorder) {
	p.mu.Lock()
	p.recorder = r
	p.mu.Unlock()
}

// treeEntry is the in-RAM record per decoded element. It keeps both the
// protocol-agnostic Object (consumed by CLI/format code) and the raw Glow
// struct (consumed by matrix / invoke operations that need the numeric
// path and full metadata).
type treeEntry struct {
	obj protocol.Object

	// Exactly one Glow pointer is non-nil — mirrors glow.Element's union.
	glowNode   *glow.Node
	glowParam  *glow.Parameter
	glowMatrix *glow.Matrix
	glowFunc   *glow.Function

	// numericPath is the canonical numeric RelOID for this entry; also
	// the primary numIndex map key. Stored explicitly so callers don't
	// have to re-read the Glow pointer to obtain it.
	numericPath []int32

	// matrixState is the derived RAM-only state attached to matrix
	// entries (A4). Nil for non-matrix entries. See matrix.State.
	matrixState *matrix.State

	// freshness is updated whenever the entry is refreshed from the
	// provider. Disk-loaded entries start at FreshnessStale and flip to
	// FreshnessLive on the first live confirmation.
	freshness Freshness
	updatedAt time.Time

	// pendingChanges is populated by processParameter just before
	// the subscriber notify; carries the field-diff between the
	// prior and current glowParam. Copied onto the outgoing Event
	// by notifySubscribers, then cleared (diff is per-event).
	pendingChanges []protocol.FieldChange
}

// Connect opens the TCP session, installs the element handler, and prepares
// the in-RAM tree. GetDirectory is deferred to Walk so the provider's
// keep-alive reply is received first.
func (p *Plugin) Connect(ctx context.Context, ip string, port int) error {
	s := NewSession(p.logger)
	p.mu.Lock()
	p.session = s
	p.treeMu.Lock()
	p.numIndex = make(map[string]*treeEntry)
	p.pathIndex = make(map[string]*treeEntry)
	p.labelIndex = make(map[string][]*treeEntry)
	p.numPath = make(map[string]string)
	p.treeMu.Unlock()
	p.subs = make(map[string]protocol.EventFunc)
	p.streamSubs = make(map[string][]int32)
	p.streamIndex = make(map[int64][]string)
	p.templates = make(map[string]*glow.Template)
	p.pendingSets = newPendingSetRegistry()
	p.profile = &compliance.Profile{}
	p.connIP = ip
	p.connPort = port
	p.mu.Unlock()

	s.SetOnElement(p.handleElements)
	s.SetProfile(p.profile)
	s.SetOnStateChange(p.onSessionStateChange)
	if p.recorder != nil {
		s.SetRecorder(p.recorder)
	}
	return s.Connect(ctx, ip, port)
}

// onSessionStateChange reacts to session liveness transitions.
// Called once with connected=true after successful Connect, and
// once with connected=false on any unsolicited disconnect
// (keep-alive timeout, TCP EOF, decode failure).
//
// On disconnect: mark every walked entry freshness=Stale, keep
// their last known values, and fire a synthetic root event so
// wildcard watch subscribers see the transition as a single
// "root isOnline y→n" announcement — they can derive the cascade
// themselves (every child is effectively offline while root is).
//
// On reconnect: mirror event n→y. Re-walk is the caller's
// responsibility (today: restart watch). Auto-reconnect is out of
// scope for the consumer — parked per scope_sequencing.
func (p *Plugin) onSessionStateChange(connected bool, reason string) {
	p.mu.Lock()
	prev := p.sessionConnected
	p.sessionConnected = connected
	p.mu.Unlock()
	if prev == connected {
		return
	}

	if !connected {
		p.markTreeStale()

		// Clear stream-subscription tracking. The old session is
		// dead, so Command 31 (Unsubscribe) is pointless and in
		// any case impossible. The fresh session created by
		// reconnect needs to re-issue Command 30 for every
		// stream-backed parameter, which autoSubscribeStreams
		// will do — but ONLY if streamSubs is empty, since its
		// idempotency check skips entries it already thinks are
		// subscribed. Without this clear, no streams resume.
		//
		// streamIndex (streamId → paths) stays intact because
		// the mapping is still correct on the wire for paths
		// that haven't changed.
		p.subsMu.Lock()
		p.streamSubs = make(map[string][]int32)
		p.subsMu.Unlock()

		p.logger.Info("emberplus: session disconnected",
			"host", p.connIP, "port", p.connPort, "reason", reason)
		// Unsolicited disconnect kicks off auto-reconnect.
		// Deliberate Disconnect() from the caller cancels it.
		p.reconnect.start(p)
	} else {
		p.logger.Info("emberplus: session connected",
			"host", p.connIP, "port", p.connPort)
	}
	p.emitRootSessionEvent(connected, reason)
}

// markTreeStale walks numIndex under the write lock and flips every
// entry's freshness to Stale. Values are left intact — the last
// known value is still useful to display (with "stale" badge).
func (p *Plugin) markTreeStale() {
	p.treeMu.Lock()
	for _, e := range p.numIndex {
		e.freshness = FreshnessStale
	}
	p.treeMu.Unlock()
}

// emitRootSessionEvent synthesises one Event signalling the session
// transition to any wildcard subscriber. The event targets the
// tree's root node (shortest numericPath) when known; when the
// plugin hasn't walked anything yet (disconnect during initial
// connect), the event carries empty OID/Path and just the change.
func (p *Plugin) emitRootSessionEvent(connected bool, reason string) {
	p.subsMu.RLock()
	fn := p.subs["*"]
	p.subsMu.RUnlock()
	if fn == nil {
		return
	}

	var rootEntry *treeEntry
	p.treeMu.RLock()
	for _, e := range p.numIndex {
		if rootEntry == nil || len(e.numericPath) < len(rootEntry.numericPath) {
			rootEntry = e
		}
	}
	p.treeMu.RUnlock()

	oldLbl, newLbl := "y", "n"
	if connected {
		oldLbl, newLbl = "n", "y"
	}

	changes := []protocol.FieldChange{
		{Name: "isOnline", Old: oldLbl, New: newLbl},
	}
	if reason != "" {
		changes = append(changes, protocol.FieldChange{
			Name: "reason", Old: "", New: reason,
		})
	}

	ev := protocol.Event{
		Slot:      0,
		Timestamp: time.Now(),
		Changes:   changes,
		Freshness: "stale",
	}
	if connected {
		ev.Freshness = "live"
	}
	if rootEntry != nil {
		ev.ID = rootEntry.obj.ID
		ev.OID = rootEntry.obj.OID
		ev.Path = strings.Join(rootEntry.obj.Path, ".")
		ev.Label = rootEntry.obj.Label
	} else {
		ev.Label = "session"
	}
	fn(ev)
}

// Disconnect releases every explicit stream subscription (Command 31),
// logs the compliance profile summary (so each session leaves a
// per-provider tolerance footprint in the log), then tears the session
// down. Safe to call on an already-disconnected Plugin.
func (p *Plugin) Disconnect() error {
	// Stop the auto-reconnect loop first so a race can't re-dial
	// right as we're tearing down.
	p.reconnect.stop()

	p.unsubscribeAll()

	if p.profile != nil {
		summary := p.profile.SummaryLine()
		class := p.profile.Classification()
		if summary == "" {
			p.logger.Info("emberplus: compliance profile",
				"host", p.connIP, "port", p.connPort,
				"classification", class)
		} else {
			p.logger.Info("emberplus: compliance profile",
				"host", p.connIP, "port", p.connPort,
				"classification", class,
				"deviations", summary)
		}
	}

	p.mu.Lock()
	s := p.session
	p.session = nil
	p.mu.Unlock()
	if s != nil {
		return s.Disconnect()
	}
	return nil
}

// GetDeviceInfo reports a single virtual slot (Ember+ has no slot
// concept; the whole tree lives under slot 0) and the connected
// host/port. The single-slot report lets generic code — `acp export`,
// `acp import`, `--all` — iterate once instead of skipping the
// provider entirely.
func (p *Plugin) GetDeviceInfo(ctx context.Context) (protocol.DeviceInfo, error) {
	return protocol.DeviceInfo{
		IP:              p.connIP,
		Port:            p.connPort,
		ProtocolVersion: 1,
		NumSlots:        1,
	}, nil
}

// GetSlotInfo pretends the whole Ember+ tree is slot 0. Any other slot is
// a usage error.
func (p *Plugin) GetSlotInfo(ctx context.Context, slot int) (protocol.SlotInfo, error) {
	if slot != 0 {
		return protocol.SlotInfo{}, fmt.Errorf("emberplus: only slot 0 supported")
	}
	return protocol.SlotInfo{Slot: 0, Status: protocol.SlotPresent}, nil
}

// Walk triggers a full GetDirectory and blocks until the tree stops
// growing. Uses a settle timer: after receiving new entries it waits 2s of
// silence before returning (typical TinyEmber+ walk = 2–3s, real devices
// can take longer). Total bound is 15s of silence.
func (p *Plugin) Walk(ctx context.Context, slot int) ([]protocol.Object, error) {
	if slot != 0 {
		return nil, fmt.Errorf("emberplus: only slot 0")
	}

	s := p.currentSession()
	if s == nil {
		return nil, protocol.ErrNotConnected
	}

	if err := s.SendGetDirectory(); err != nil {
		return nil, err
	}

	time.Sleep(500 * time.Millisecond)

	settle := time.NewTimer(15 * time.Second)
	defer settle.Stop()
	lastCount := 0
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-settle.C:
			goto done
		default:
			p.treeMu.RLock()
			count := len(p.numIndex)
			p.treeMu.RUnlock()
			if count > lastCount {
				lastCount = count
				settle.Reset(2 * time.Second)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
done:
	return p.snapshot(), nil
}

// snapshot returns every live Object under RLock.
func (p *Plugin) snapshot() []protocol.Object {
	p.treeMu.RLock()
	defer p.treeMu.RUnlock()
	out := make([]protocol.Object, 0, len(p.numIndex))
	for _, entry := range p.numIndex {
		out = append(out, entry.obj)
	}
	return out
}

// findEntry resolves a ValueRequest to a treeEntry. Resolution order:
//
//  1. Path — try as numeric RelOID ("1.1.2") against numIndex; if that
//     misses, try as identifier path ("router.oneToN.matrix") against
//     pathIndex. Both are O(1).
//  2. Label — first match in labelIndex (may be ambiguous; logs a warning
//     if multiple entries share the label).
//  3. Group+ID — linear scan over numIndex. Ember+ has no Group concept,
//     so this only matches on ID.
//
// Returns ("", nil) when nothing matches.
func (p *Plugin) findEntry(req protocol.ValueRequest) (string, *treeEntry) {
	p.treeMu.RLock()
	defer p.treeMu.RUnlock()

	if req.Path != "" {
		if entry, ok := p.numIndex[req.Path]; ok {
			return req.Path, entry
		}
		if entry, ok := p.pathIndex[req.Path]; ok {
			return numericKey(entry.numericPath), entry
		}
	}

	if req.Label != "" {
		if entries, ok := p.labelIndex[req.Label]; ok && len(entries) > 0 {
			if len(entries) > 1 {
				p.logger.Warn("emberplus: ambiguous label",
					"label", req.Label, "matches", len(entries))
			}
			return numericKey(entries[0].numericPath), entries[0]
		}
	}

	if req.ID >= 0 {
		for key, entry := range p.numIndex {
			if entry.obj.ID == req.ID {
				return key, entry
			}
		}
	}
	return "", nil
}

// ensureWalked runs Walk if the tree is empty. Every path-addressed
// operation goes through this first — a consumer can't address a path it
// hasn't seen.
func (p *Plugin) ensureWalked(ctx context.Context, op string) error {
	p.treeMu.RLock()
	empty := len(p.numIndex) == 0
	p.treeMu.RUnlock()
	if !empty {
		return nil
	}
	if _, err := p.Walk(ctx, 0); err != nil {
		return fmt.Errorf("emberplus: walk for %s: %w", op, err)
	}
	return nil
}

// GetValue reads a cached value from the tree. Ember+ providers
// spontaneously announce value changes, so the walker-populated cache is
// generally the current value. If freshness is Stale the caller can check
// entry.freshness and issue a targeted Subscribe to wait for a live value
// (not done here — callers opt in).
func (p *Plugin) GetValue(ctx context.Context, req protocol.ValueRequest) (protocol.Value, error) {
	if err := p.ensureWalked(ctx, "get"); err != nil {
		return protocol.Value{}, err
	}

	key, entry := p.findEntry(req)
	if entry == nil {
		p.treeMu.RLock()
		size := len(p.numIndex)
		p.treeMu.RUnlock()
		return protocol.Value{}, WrapProto(fmt.Sprintf("object not found (tree has %d entries)", size), nil)
	}
	p.logger.Debug("emberplus: GetValue",
		"key", key, "label", entry.obj.Label, "freshness", entry.freshness)
	return entry.obj.Value, nil
}

// SetValue writes a value to a parameter. The provider echoes the new
// value as an announcement; the returned Value is the *requested* value,
// not the confirmed one. Callers that need the confirmed value should
// subscribe first.
// SetValue sends a write to the provider and waits for the
// confirming announce. Returns the confirmed value (may differ from
// val when the provider coerces) and an error describing anomalies.
//
// Error semantics (see internal/protocol/errors.go):
//
//   - protocol.ErrNotConnected → session is dead; no wire traffic sent.
//     Returns the last known value from the tree.
//   - protocol.ErrWriteTimeout → send succeeded but no confirming
//     announce arrived within defaultWriteTimeout. Tree value
//     unchanged.
//   - protocol.ErrWriteCoerced → provider announced a different value
//     (clamp, round). Returned Value reflects what the provider
//     applied. Caller opts-in to accept via errors.Is.
//
// The method is blocking; callers wanting fire-and-forget writes
// should cancel ctx early.
func (p *Plugin) SetValue(ctx context.Context, req protocol.ValueRequest, val protocol.Value) (protocol.Value, error) {
	// Session-liveness gate: never send on a dead session.
	p.mu.Lock()
	connected := p.sessionConnected
	p.mu.Unlock()
	if !connected {
		return protocol.Value{}, protocol.ErrNotConnected
	}

	s := p.currentSession()
	if s == nil {
		return protocol.Value{}, protocol.ErrNotConnected
	}

	if err := p.ensureWalked(ctx, "set"); err != nil {
		return protocol.Value{}, err
	}

	key, entry := p.findEntry(req)
	if entry == nil || entry.glowParam == nil {
		return protocol.Value{}, WrapProto("parameter not found", nil)
	}
	path := entry.glowParam.Path
	if len(path) == 0 {
		// Fallback for non-qualified providers that registered the
		// parameter without a wire Path: use our canonical numeric.
		path = cloneInt32Slice(entry.numericPath)
	}
	if len(path) == 0 {
		return protocol.Value{}, WrapProto("parameter has no path", nil)
	}

	if val.Kind == protocol.KindUnknown {
		val.Kind = entry.obj.Kind
	}
	coerceStringToTyped(&val)

	glowVal := valueToGlow(val)

	// Register the pending-set watcher BEFORE sending so we don't
	// miss a fast provider echo.
	ps := &pendingSet{
		expected: glowVal,
		kind:     entry.obj.Kind,
		done:     make(chan pendingResult, 1),
	}
	p.pendingSets.register(key, ps)
	defer p.pendingSets.unregister(key)

	if err := s.SendSetValue(path, glowVal); err != nil {
		return protocol.Value{}, err
	}

	// Await confirmation or timeout.
	timer := time.NewTimer(defaultWriteTimeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return entry.obj.Value, ctx.Err()
	case <-timer.C:
		return entry.obj.Value, protocol.ErrWriteTimeout
	case r := <-ps.done:
		return r.value, r.err
	}
}

// Subscribe registers a callback for one parameter path. Behaviour per
// spec v2.50 pp. 30–31:
//
//   - Regular parameter (no streamIdentifier set): the provider reports
//     value changes automatically once GetDirectory has been issued on
//     the containing node. We register the callback locally; no Command
//     30 is sent.
//   - Streamed parameter (streamIdentifier != 0): the provider only
//     transmits updates after an explicit Subscribe. We register the
//     callback and send Command 30 (Subscribe). A6 wires the matching
//     StreamCollection dispatch.
//   - Empty Path/Label/ID: wildcard "*" watch-all. Every future value
//     notification invokes this callback in addition to any specific one.
//
// Returns an error only when the session is dead or the path is not a
// Parameter.
func (p *Plugin) Subscribe(req protocol.ValueRequest, fn protocol.EventFunc) error {
	s := p.currentSession()
	if s == nil {
		return protocol.ErrNotConnected
	}

	if req.Path == "" && req.Label == "" && req.ID < 0 {
		p.subsMu.Lock()
		p.subs["*"] = fn
		p.subsMu.Unlock()

		// Stream-backed Parameters require an explicit Command 30
		// (spec p.30–31) — GetDirectory alone won't start the flow.
		// Enumerate already-walked stream parameters now; new ones
		// discovered later during walk/announce get auto-subscribed
		// from processParameter via maybeWildcardStreamSubscribe.
		p.autoSubscribeStreams()
		return nil
	}

	if err := p.ensureWalked(context.Background(), "subscribe"); err != nil {
		return err
	}

	key, entry := p.findEntry(req)
	if entry == nil || entry.glowParam == nil || len(entry.glowParam.Path) == 0 {
		return WrapProto("parameter not found for subscribe", nil)
	}

	p.subsMu.Lock()
	p.subs[key] = fn
	if entry.glowParam.StreamIdentifier != 0 {
		p.streamSubs[key] = entry.glowParam.Path
	}
	p.subsMu.Unlock()

	if entry.glowParam.StreamIdentifier != 0 {
		p.logger.Debug("emberplus: explicit stream subscribe",
			"path", key, "stream_identifier", entry.glowParam.StreamIdentifier)
		return s.SendSubscribe(entry.glowParam.Path)
	}
	p.logger.Debug("emberplus: implicit subscribe via GetDirectory", "path", key)
	return nil
}

// Unsubscribe removes a callback. Sends Command 31 only if the original
// subscription was explicit (streamed parameter per spec p.31).
func (p *Plugin) Unsubscribe(req protocol.ValueRequest) error {
	s := p.currentSession()
	if s == nil {
		return nil
	}

	key, entry := p.findEntry(req)
	if entry == nil {
		// Wildcard or unknown — just clear any matching callback entry.
		if req.Path == "" && req.Label == "" && req.ID < 0 {
			p.subsMu.Lock()
			delete(p.subs, "*")
			p.subsMu.Unlock()
		}
		return nil
	}

	p.subsMu.Lock()
	delete(p.subs, key)
	wasStream := false
	if _, ok := p.streamSubs[key]; ok {
		delete(p.streamSubs, key)
		wasStream = true
	}
	p.subsMu.Unlock()

	if wasStream && entry.glowParam != nil && len(entry.glowParam.Path) > 0 {
		return s.SendUnsubscribe(entry.glowParam.Path)
	}
	return nil
}

// unsubscribeAll sends Command 31 for every remaining streamed subscription.
// Called from Disconnect to keep the provider's internal subscriber list
// clean — polite but not required by spec.
func (p *Plugin) unsubscribeAll() {
	p.subsMu.Lock()
	paths := make([][]int32, 0, len(p.streamSubs))
	for _, path := range p.streamSubs {
		paths = append(paths, path)
	}
	p.streamSubs = make(map[string][]int32)
	p.subs = make(map[string]protocol.EventFunc)
	p.subsMu.Unlock()

	s := p.currentSession()
	if s == nil {
		return
	}
	for _, path := range paths {
		_ = s.SendUnsubscribe(path)
	}
}

// MatrixConnect sends a Connection command to the provider. Path may be
// numeric ("1.1.2") or identifier-based ("router.oneToN.matrix"); the
// tree resolves it via pathIndex.
//
// operation: 0=absolute, 1=connect (nToN add), 2=disconnect (nToN remove).
// See Glow DTD p.89 ConnectionOperation.
//
// Before sending, validates via matrix.State.CanConnect so bad requests
// (oneToN overload, oneToOne source collision, nToN cap breach, locked
// target) fail locally with a spec-cited error. On success, the provider
// will announce the new tally which processMatrix merges into state.
func (p *Plugin) MatrixConnect(ctx context.Context, matrixPath string, target int32, sources []int32, operation int64) error {
	s := p.currentSession()
	if s == nil {
		return protocol.ErrNotConnected
	}
	if err := p.ensureWalked(ctx, "matrix"); err != nil {
		return err
	}

	_, entry := p.findEntry(protocol.ValueRequest{Path: matrixPath, ID: -1})
	if entry == nil || entry.glowMatrix == nil {
		return WrapProto(fmt.Sprintf("matrix not found at path %q", matrixPath), nil)
	}

	if entry.matrixState != nil {
		if err := entry.matrixState.CanConnect(target, sources, operation); err != nil {
			return WrapProto("matrix validation", err)
		}
	}

	p.logger.Debug("emberplus: MatrixConnect",
		"matrix_path", matrixPath,
		"numeric_path", entry.glowMatrix.Path,
		"target", target, "sources", sources, "operation", operation)

	if err := s.SendMatrixConnect(entry.glowMatrix.Path, target, sources, operation); err != nil {
		return err
	}

	if entry.matrixState != nil {
		entry.matrixState.ApplyConnection(glow.Connection{
			Target:      target,
			Sources:     sources,
			Operation:   operation,
			Disposition: glow.ConnDispPending,
		}, matrix.ChangeUser)
	}
	return nil
}

// StreamParameterPaths returns the numeric paths of every parameter
// that carries a streamIdentifier. If filter >= 0 the result is limited
// to that one streamIdentifier. Used by cmd stream (A8).
func (p *Plugin) StreamParameterPaths(filter int64) []string {
	p.subsMu.RLock()
	defer p.subsMu.RUnlock()
	var out []string
	for id, paths := range p.streamIndex {
		if filter >= 0 && id != filter {
			continue
		}
		out = append(out, paths...)
	}
	return out
}

// MatrixSnapshot returns the derived matrix state for the matrix at
// matrixPath, or nil if not a matrix. Useful for UIs rendering a tally.
func (p *Plugin) MatrixSnapshot(matrixPath string) []matrix.TargetState {
	_, entry := p.findEntry(protocol.ValueRequest{Path: matrixPath, ID: -1})
	if entry == nil || entry.matrixState == nil {
		return nil
	}
	return entry.matrixState.Snapshot()
}

// InvokeFunction calls a function by path and waits for InvocationResult.
// Blocks until the provider replies or ctx expires.
func (p *Plugin) InvokeFunction(ctx context.Context, funcPath string, args []any) (*glow.InvocationResult, error) {
	s := p.currentSession()
	if s == nil {
		return nil, protocol.ErrNotConnected
	}
	if err := p.ensureWalked(ctx, "invoke"); err != nil {
		return nil, err
	}

	_, entry := p.findEntry(protocol.ValueRequest{Path: funcPath, ID: -1})
	if entry == nil || entry.glowFunc == nil {
		return nil, WrapProto(fmt.Sprintf("function not found at path %q", funcPath), nil)
	}

	invID := s.NextInvocationID()
	resultCh := make(chan *glow.InvocationResult, 1)
	s.RegisterInvocation(invID, resultCh)
	defer s.UnregisterInvocation(invID)

	p.logger.Debug("emberplus: InvokeFunction",
		"path", funcPath, "numeric_path", entry.glowFunc.Path,
		"invocation_id", invID, "args", args)

	if err := s.SendInvoke(entry.glowFunc.Path, invID, args); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resultCh:
		return result, nil
	}
}

// currentSession returns the active session under lock; nil if disconnected.
func (p *Plugin) currentSession() *Session {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.session
}

// handleElements is the session callback for every decoded Glow message.
func (p *Plugin) handleElements(elements []glow.Element) {
	for _, el := range elements {
		p.processElement(el, nil, nil)
	}
}

// processElement dispatches one Glow element to the right handler.
// Inherited parentPath / parentNumPath are used when the provider sends
// non-qualified elements (their numeric path is implicit from the
// parent node, per spec p.87: Node ::= SEQUENCE { number [0] ... }).
func (p *Plugin) processElement(el glow.Element, parentPath []string, parentNumPath []int32) {
	switch {
	case el.Node != nil:
		p.processNode(el.Node, parentPath, parentNumPath)
	case el.Parameter != nil:
		p.processParameter(el.Parameter, parentPath, parentNumPath)
	case el.Matrix != nil:
		p.processMatrix(el.Matrix, parentPath, parentNumPath)
	case el.Function != nil:
		p.processFunction(el.Function, parentPath, parentNumPath)
	case el.InvocationResult != nil:
		if s := p.currentSession(); s != nil {
			s.deliverInvocationResult(el.InvocationResult)
		}
	case el.Template != nil:
		p.processTemplate(el.Template)
	case len(el.Streams) > 0:
		p.dispatchStreams(el.Streams)
	}
}

// resolveNumPath returns the canonical numeric RelOID for an element.
// Qualified elements carry their full path explicitly; non-qualified
// elements compose it from the parent path plus their own number.
// Spec p.87/p.85: non-qualified Node/Parameter hold only [0] number.
//
// When we take the derive branch we note the tolerance event on the
// plugin's profile so the compatibility matrix can attribute it.
func (p *Plugin) resolveNumPath(explicit []int32, parent []int32, number int32) []int32 {
	if len(explicit) > 0 {
		return cloneInt32Slice(explicit)
	}
	p.profile.Note(NonQualifiedElement)
	out := make([]int32, 0, len(parent)+1)
	out = append(out, parent...)
	out = append(out, number)
	return out
}

// processTemplate stores the Template keyed by its numeric RelOID
// (qualified) or its number as a synthetic single-element path
// (non-qualified). Consumers may then resolve templateReference fields
// on Parameter/Node/Matrix/Function via ResolveTemplate.
//
// Spec p.54-58 (Ember+ 1.4).
func (p *Plugin) processTemplate(t *glow.Template) {
	var key string
	if t.Qualified {
		key = numericKey(t.Path)
	} else {
		key = strconv.FormatInt(int64(t.Number), 10)
	}
	if key == "" {
		return
	}
	p.templatesMu.Lock()
	p.templates[key] = t
	p.templatesMu.Unlock()
	p.logger.Debug("emberplus: template stored", "key", key, "qualified", t.Qualified)
}

// ResolveTemplate returns the Template stored at the given numeric path
// (dot-separated, e.g. "0.5.2"), or nil if none known. Safe for
// concurrent reads. Use it to resolve a Parameter.TemplateReference /
// Node.TemplateReference / Matrix.TemplateReference / Function.TemplateReference.
func (p *Plugin) ResolveTemplate(path []int32) *glow.Template {
	if len(path) == 0 {
		return nil
	}
	p.templatesMu.RLock()
	defer p.templatesMu.RUnlock()
	return p.templates[numericKey(path)]
}

// dispatchStreams fires callbacks for every StreamEntry that maps to a
// subscribed parameter path (spec p.29-30, p.93). When the parameter has
// a StreamDescription (format + offset) the raw stream value is decoded
// accordingly; otherwise the value is passed through as-is.
func (p *Plugin) dispatchStreams(entries []glow.StreamEntry) {
	for _, e := range entries {
		p.subsMu.RLock()
		paths := p.streamIndex[e.StreamIdentifier]
		p.subsMu.RUnlock()
		if len(paths) == 0 {
			continue
		}
		for _, key := range paths {
			p.treeMu.RLock()
			entry := p.numIndex[key]
			p.treeMu.RUnlock()
			if entry == nil || entry.glowParam == nil {
				continue
			}
			val := resolveStreamValue(entry.glowParam, e.Value)
			p.deliverStreamValue(entry, val)
		}
	}
}

// deliverStreamValue updates the entry's cached value to FreshnessUpdated
// and invokes any registered callback (specific or wildcard). Stream
// frames carry only the new value — we synthesise a one-item field
// diff so watch/UI see "changed: value X→Y" just like announces.
func (p *Plugin) deliverStreamValue(entry *treeEntry, value any) {
	if entry.glowParam == nil {
		return
	}

	// Snapshot the old rendered-value before we overwrite so the
	// diff shows the transition. formatAny handles nil/typed.
	oldRendered := formatAny(entry.glowParam.Value)

	entry.freshness = FreshnessUpdated
	entry.updatedAt = time.Now()
	assignToValue(&entry.obj.Value, entry.obj.Kind, value)

	// Keep glowParam.Value in sync so the next diff against this
	// entry uses the correct "prior" baseline. Without this, the
	// next announce's diff would compare against the pre-stream
	// value, producing noisy deltas.
	entry.glowParam.Value = value

	newRendered := formatAny(value)
	if oldRendered != newRendered {
		entry.pendingChanges = []protocol.FieldChange{{
			Name: "value",
			Old:  oldRendered,
			New:  newRendered,
		}}
	}
	p.notifySubscribers(entry)
}

// resolveStreamValue decodes a StreamEntry.Value according to the
// parameter's StreamDescription (spec p.86). If no description is set
// or the value is already native-typed, returns it unchanged.
//
// StreamDescription format bits (DTD p.86):
//
//	bit 4-5 unused, bit 3..2 byte width, bit 1 endianness, bit 0 sign/float
//
// We decode from an octet string payload when present; native Go values
// from decodeAnyValue are forwarded as-is.
func resolveStreamValue(param *glow.Parameter, raw any) any {
	if param.StreamDescriptor == nil {
		return raw
	}
	payload, ok := raw.([]byte)
	if !ok {
		return raw
	}
	off := int(param.StreamDescriptor.Offset)
	if off < 0 || off >= len(payload) {
		return raw
	}
	return decodeStreamBytes(param.StreamDescriptor.Format, payload[off:])
}

// decodeStreamBytes interprets a StreamFormat-encoded slice. Returns nil
// when there is not enough data for the requested format.
func decodeStreamBytes(format int64, buf []byte) any {
	need := streamFormatSize(format)
	if need == 0 || len(buf) < need {
		return nil
	}
	switch format {
	case glow.StreamFmtUnsignedInt8:
		return int64(buf[0])
	case glow.StreamFmtSignedInt8:
		return int64(int8(buf[0]))
	case glow.StreamFmtUnsignedInt16BigEndian:
		return int64(uint16(buf[0])<<8 | uint16(buf[1]))
	case glow.StreamFmtUnsignedInt16LittleEndian:
		return int64(uint16(buf[1])<<8 | uint16(buf[0]))
	case glow.StreamFmtSignedInt16BigEndian:
		return int64(int16(uint16(buf[0])<<8 | uint16(buf[1])))
	case glow.StreamFmtSignedInt16LittleEndian:
		return int64(int16(uint16(buf[1])<<8 | uint16(buf[0])))
	case glow.StreamFmtUnsignedInt32BigEndian:
		return int64(uint32(buf[0])<<24 | uint32(buf[1])<<16 | uint32(buf[2])<<8 | uint32(buf[3]))
	case glow.StreamFmtUnsignedInt32LittleEndian:
		return int64(uint32(buf[3])<<24 | uint32(buf[2])<<16 | uint32(buf[1])<<8 | uint32(buf[0]))
	case glow.StreamFmtSignedInt32BigEndian:
		return int64(int32(uint32(buf[0])<<24 | uint32(buf[1])<<16 | uint32(buf[2])<<8 | uint32(buf[3])))
	case glow.StreamFmtSignedInt32LittleEndian:
		return int64(int32(uint32(buf[3])<<24 | uint32(buf[2])<<16 | uint32(buf[1])<<8 | uint32(buf[0])))
	case glow.StreamFmtFloat32BigEndian:
		bits := uint32(buf[0])<<24 | uint32(buf[1])<<16 | uint32(buf[2])<<8 | uint32(buf[3])
		return float64(math.Float32frombits(bits))
	case glow.StreamFmtFloat32LittleEndian:
		bits := uint32(buf[3])<<24 | uint32(buf[2])<<16 | uint32(buf[1])<<8 | uint32(buf[0])
		return float64(math.Float32frombits(bits))
	case glow.StreamFmtFloat64BigEndian:
		var bits uint64
		for i := 0; i < 8; i++ {
			bits = bits<<8 | uint64(buf[i])
		}
		return math.Float64frombits(bits)
	case glow.StreamFmtFloat64LittleEndian:
		var bits uint64
		for i := 7; i >= 0; i-- {
			bits = bits<<8 | uint64(buf[i])
		}
		return math.Float64frombits(bits)
	}
	return nil
}

// streamFormatSize returns the byte width implied by a StreamFormat.
func streamFormatSize(format int64) int {
	switch format {
	case glow.StreamFmtUnsignedInt8, glow.StreamFmtSignedInt8:
		return 1
	case glow.StreamFmtUnsignedInt16BigEndian, glow.StreamFmtUnsignedInt16LittleEndian,
		glow.StreamFmtSignedInt16BigEndian, glow.StreamFmtSignedInt16LittleEndian:
		return 2
	case glow.StreamFmtUnsignedInt32BigEndian, glow.StreamFmtUnsignedInt32LittleEndian,
		glow.StreamFmtSignedInt32BigEndian, glow.StreamFmtSignedInt32LittleEndian,
		glow.StreamFmtFloat32BigEndian, glow.StreamFmtFloat32LittleEndian:
		return 4
	case glow.StreamFmtUnsignedInt64BigEndian, glow.StreamFmtUnsignedInt64LittleEndian,
		glow.StreamFmtSignedInt64BigEndian, glow.StreamFmtSignedInt64LittleEndian,
		glow.StreamFmtFloat64BigEndian, glow.StreamFmtFloat64LittleEndian:
		return 8
	}
	return 0
}

// assignToValue writes value into v using the kind classification.
func assignToValue(v *protocol.Value, kind protocol.ValueKind, value any) {
	v.Kind = kind
	switch t := value.(type) {
	case int64:
		v.Int = t
		v.Float = float64(t)
	case float64:
		v.Float = t
		v.Int = int64(t)
	case string:
		v.Str = t
	case bool:
		v.Bool = t
	case []byte:
		v.Raw = append(v.Raw[:0], t...)
	}
}

// processNode stores a Node in the tree and recurses into its children.
// If the node arrives with no children on FIRST SIGHTING, issues a lazy
// GetDirectory so we can fetch the subtree on demand.
//
// The "first sighting" guard matters: Ember+ announces are deltas (spec
// p.85). When a child Parameter changes (e.g. the user toggles a stream
// on Fader), the provider routinely re-delivers the parent Node with
// an empty `children[]` because it is only carrying the change —
// the child list is implicit from the earlier walk. Without this
// guard, every stream toggle on Fader would cause us to fire
// GetDirectory on Channel 1, which the provider responds to by
// re-announcing every child under Channel 1 — the symptom the user
// reported as "we have a walk of the entire tree" on each streamId
// toggle.
//
// Re-fetching after reconnect is handled by refreshAfterReconnect +
// clearTree(); that path wipes numIndex before walking, so the
// "first sighting" check trips naturally.
func (p *Plugin) processNode(n *glow.Node, parentPath []string, parentNumPath []int32) {
	numPath := p.resolveNumPath(n.Path, parentNumPath, n.Number)
	if len(numPath) > 0 {
		p.registerNumericPath(numPath, n.Identifier)
	}
	stringPath := p.pathForElement(numPath, n.Identifier, n.Number, parentPath)

	// "Have we seen this Node before?" — determines whether an empty
	// children[] means "unwalked subtree" (fetch now) or "announce
	// delta with no structural change" (ignore).
	firstSighting := true
	if len(numPath) > 0 {
		p.treeMu.RLock()
		_, alreadyStored := p.numIndex[numericKey(numPath)]
		p.treeMu.RUnlock()
		firstSighting = !alreadyStored
	}

	entry := &treeEntry{
		glowNode:    n,
		numericPath: numPath,
		freshness:   FreshnessLive,
		updatedAt:   time.Now(),
		obj: protocol.Object{
			Slot:   0,
			ID:     int(n.Number),
			OID:    numericKey(numPath),
			Label:  n.Identifier,
			Kind:   protocol.KindRaw,
			Path:   stringPath,
			Access: 1,
			Meta:   nodeMeta(n),
		},
	}
	if len(stringPath) > 1 {
		entry.obj.Group = stringPath[0]
	}
	p.storeEntry(entry, stringPath)

	for _, child := range n.Children {
		p.processElement(child, stringPath, numPath)
	}

	if firstSighting && len(n.Children) == 0 && len(numPath) > 0 {
		if s := p.currentSession(); s != nil {
			numCopy := cloneInt32Slice(numPath)
			go func() {
				p.logger.Debug("emberplus: requesting children",
					"path", numCopy, "identifier", n.Identifier)
				_ = s.SendGetDirectoryFor(numCopy)
			}()
		}
	}
}

// nodeMeta exposes the Ember+ Node properties (spec p.87 NodeContents)
// the generic tree view should render: description, isOnline flag,
// schemaIdentifiers, templateReference. Returns nil when nothing
// worth surfacing was set.
func nodeMeta(n *glow.Node) map[string]any {
	m := map[string]any{"element": "node"}
	if n.Description != "" {
		m["description"] = n.Description
	}
	if n.IsRoot {
		m["isRoot"] = true
	}
	// isOnline defaults to true when absent (spec p.87). Only emit it
	// when the provider explicitly set false — absence in the JSON
	// then means the spec default.
	if !n.IsOnline {
		// Field was never set or set false; we cannot disambiguate,
		// so omit it. The consumer must apply the spec default true.
	} else {
		m["isOnline"] = true
	}
	if n.SchemaIdentifiers != "" {
		m["schemaIdentifiers"] = n.SchemaIdentifiers
	}
	if len(n.TemplateReference) > 0 {
		m["templateReference"] = numericKey(n.TemplateReference)
	}
	return m
}

// parameterMeta surfaces every ParameterContents field (spec p.85) that
// the standard Object constraint columns don't already cover. The
// "type" string uses the spec vocabulary (integer/real/string/boolean/
// trigger/enum/octets) so downstream tools can round-trip it.
func parameterMeta(p *glow.Parameter) map[string]any {
	m := map[string]any{"element": "parameter"}
	// Type comes from CTX 13 when the provider sets it (spec p.85).
	// When absent we infer from the decoded value (spec allows this —
	// the consumer sees the Value CHOICE branch that was filled in).
	if typeName := paramTypeName(p.Type); typeName != "" && typeName != "null" {
		m["type"] = typeName
	} else if inferred := inferParamType(p.Value); inferred != "" {
		m["type"] = inferred
	}
	if p.Description != "" {
		m["description"] = p.Description
	}
	if p.Format != "" {
		m["format"] = p.Format
	}
	if p.Formula != "" {
		m["formula"] = p.Formula
	}
	if p.Factor != 0 {
		m["factor"] = p.Factor
	}
	if p.Enumeration != "" {
		m["enumeration"] = p.Enumeration
	}
	if len(p.EnumMap) > 0 {
		entries := make([]map[string]any, 0, len(p.EnumMap))
		for k, v := range p.EnumMap {
			entries = append(entries, map[string]any{"key": k, "value": v})
		}
		m["enumMap"] = entries
	}
	if p.StreamIdentifier != 0 {
		m["streamIdentifier"] = p.StreamIdentifier
	}
	if p.StreamDescriptor != nil {
		m["streamDescriptor"] = map[string]any{
			"format": streamFormatName(p.StreamDescriptor.Format),
			"offset": p.StreamDescriptor.Offset,
		}
	}
	if p.SchemaIdentifiers != "" {
		m["schemaIdentifiers"] = p.SchemaIdentifiers
	}
	if len(p.TemplateReference) > 0 {
		m["templateReference"] = numericKey(p.TemplateReference)
	}
	return m
}

// matrixMeta surfaces every MatrixContents field (spec p.88) plus the
// observed connections tally — this is what providers consume when we
// round-trip the JSON tree back into a live matrix.
func matrixMeta(m *glow.Matrix) map[string]any {
	out := map[string]any{
		"element":     "matrix",
		"type":        matrixTypeName(m.MatrixType),
		"mode":        matrixAddrName(m.AddressingMode),
		"targetCount": m.TargetCount,
		"sourceCount": m.SourceCount,
	}
	if m.Description != "" {
		out["description"] = m.Description
	}
	if m.MatrixType == glow.MatrixTypeNToN {
		if m.MaxTotalConnects != 0 {
			out["maximumTotalConnects"] = m.MaxTotalConnects
		}
		if m.MaxConnectsPerTarget != 0 {
			out["maximumConnectsPerTarget"] = m.MaxConnectsPerTarget
		}
	}
	if m.ParametersLocation != nil {
		switch v := m.ParametersLocation.(type) {
		case []int32:
			out["parametersLocation"] = numericKey(v)
		case int32:
			out["parametersLocation"] = v
		}
	}
	if m.GainParameterNumber != 0 {
		out["gainParameterNumber"] = m.GainParameterNumber
	}
	if len(m.Labels) > 0 {
		labels := make([]map[string]any, 0, len(m.Labels))
		for _, l := range m.Labels {
			labels = append(labels, map[string]any{
				"basePath":    numericKey(l.BasePath),
				"description": l.Description,
			})
		}
		out["labels"] = labels
	}
	if m.SchemaIdentifiers != "" {
		out["schemaIdentifiers"] = m.SchemaIdentifiers
	}
	if len(m.TemplateReference) > 0 {
		out["templateReference"] = numericKey(m.TemplateReference)
	}
	if len(m.Targets) > 0 {
		out["targets"] = m.Targets
	}
	if len(m.Sources) > 0 {
		out["sources"] = m.Sources
	}
	if len(m.Connections) > 0 {
		conns := make(map[string]any, len(m.Connections))
		for _, c := range m.Connections {
			conns[strconv.Itoa(int(c.Target))] = map[string]any{
				"target":      c.Target,
				"sources":     c.Sources,
				"operation":   connOpName(c.Operation),
				"disposition": connDispName(c.Disposition),
			}
		}
		out["connections"] = conns
	}
	return out
}

// functionMeta surfaces the Function signature (spec p.91) — the
// arguments and result tuples are the only reason anyone exports a
// function definition in the first place.
func functionMeta(f *glow.Function) map[string]any {
	out := map[string]any{"element": "function"}
	if f.Description != "" {
		out["description"] = f.Description
	}
	if len(f.Arguments) > 0 {
		out["arguments"] = tupleItemsToJSON(f.Arguments)
	}
	if len(f.Result) > 0 {
		out["result"] = tupleItemsToJSON(f.Result)
	}
	if len(f.TemplateReference) > 0 {
		out["templateReference"] = numericKey(f.TemplateReference)
	}
	return out
}

func tupleItemsToJSON(items []glow.TupleItem) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		entry := map[string]any{"type": paramTypeName(it.Type)}
		if it.Name != "" {
			entry["name"] = it.Name
		}
		out = append(out, entry)
	}
	return out
}

// --- enum → string helpers (spec vocabulary) ---

// inferParamType returns the spec type name for a decoded Value when
// CTX 13 (ParameterContents.type) was not sent. The decoded Go type
// identifies which Value CHOICE branch the provider used.
func inferParamType(v any) string {
	switch v.(type) {
	case int64:
		return "integer"
	case float64:
		return "real"
	case string:
		return "string"
	case bool:
		return "boolean"
	case []byte:
		return "octets"
	}
	return ""
}

func paramTypeName(t int64) string {
	switch t {
	case glow.ParamTypeNull:
		return "null"
	case glow.ParamTypeInteger:
		return "integer"
	case glow.ParamTypeReal:
		return "real"
	case glow.ParamTypeString:
		return "string"
	case glow.ParamTypeBoolean:
		return "boolean"
	case glow.ParamTypeTrigger:
		return "trigger"
	case glow.ParamTypeEnum:
		return "enum"
	case glow.ParamTypeOctets:
		return "octets"
	}
	return ""
}

func matrixTypeName(t int64) string {
	switch t {
	case glow.MatrixTypeOneToN:
		return "oneToN"
	case glow.MatrixTypeOneToOne:
		return "oneToOne"
	case glow.MatrixTypeNToN:
		return "nToN"
	}
	return "oneToN"
}

func matrixAddrName(a int64) string {
	if a == glow.MatrixAddrNonLinear {
		return "nonLinear"
	}
	return "linear"
}

func connOpName(op int64) string {
	switch op {
	case glow.ConnOpConnect:
		return "connect"
	case glow.ConnOpDisconnect:
		return "disconnect"
	}
	return "absolute"
}

func connDispName(d int64) string {
	switch d {
	case glow.ConnDispModified:
		return "modified"
	case glow.ConnDispPending:
		return "pending"
	case glow.ConnDispLocked:
		return "locked"
	}
	return "tally"
}

func streamFormatName(f int64) string {
	switch f {
	case glow.StreamFmtUnsignedInt8:
		return "unsignedInt8"
	case glow.StreamFmtUnsignedInt16BigEndian:
		return "unsignedInt16BigEndian"
	case glow.StreamFmtUnsignedInt16LittleEndian:
		return "unsignedInt16LittleEndian"
	case glow.StreamFmtUnsignedInt32BigEndian:
		return "unsignedInt32BigEndian"
	case glow.StreamFmtUnsignedInt32LittleEndian:
		return "unsignedInt32LittleEndian"
	case glow.StreamFmtUnsignedInt64BigEndian:
		return "unsignedInt64BigEndian"
	case glow.StreamFmtUnsignedInt64LittleEndian:
		return "unsignedInt64LittleEndian"
	case glow.StreamFmtSignedInt8:
		return "signedInt8"
	case glow.StreamFmtSignedInt16BigEndian:
		return "signedInt16BigEndian"
	case glow.StreamFmtSignedInt16LittleEndian:
		return "signedInt16LittleEndian"
	case glow.StreamFmtSignedInt32BigEndian:
		return "signedInt32BigEndian"
	case glow.StreamFmtSignedInt32LittleEndian:
		return "signedInt32LittleEndian"
	case glow.StreamFmtSignedInt64BigEndian:
		return "signedInt64BigEndian"
	case glow.StreamFmtSignedInt64LittleEndian:
		return "signedInt64LittleEndian"
	case glow.StreamFmtFloat32BigEndian:
		return "float32BigEndian"
	case glow.StreamFmtFloat32LittleEndian:
		return "float32LittleEndian"
	case glow.StreamFmtFloat64BigEndian:
		return "float64BigEndian"
	case glow.StreamFmtFloat64LittleEndian:
		return "float64LittleEndian"
	}
	return ""
}

// processParameter stores a Parameter with full ParameterContents metadata
// carried in obj constraints + glowParam pointer for lossless access.
func (p *Plugin) processParameter(param *glow.Parameter, parentPath []string, parentNumPath []int32) {
	numPath := p.resolveNumPath(param.Path, parentNumPath, param.Number)
	if len(numPath) > 0 {
		p.registerNumericPath(numPath, param.Identifier)
	}

	// Announce-vs-walk merge (Ember+ spec p.85): announces typically
	// carry ONLY the changed field(s) inside ParameterContents — the
	// other 17 fields are absent on the wire and arrive as zero-values
	// in the decoded struct. If we already walked this parameter, the
	// prior glowParam has the full metadata (Type, Identifier, Access,
	// ranges, enumMap, streamDescriptor, ...). Overwriting wholesale
	// would strip that metadata on every announce, which is the root
	// cause of the "decoded value mismatch" symptom during watch.
	//
	// Merge: start from the prior param, overlay only the fields the
	// announce actually carried (non-zero / non-nil). The merged
	// *glow.Parameter is what we store and what builds the obj.
	p.treeMu.RLock()
	existing, seen := p.numIndex[numericKey(numPath)]
	p.treeMu.RUnlock()

	// Capture whether THIS announce carried a new Value on the wire
	// BEFORE merge — the merge fills incoming.Value from the prior
	// glowParam when the announce omitted it, which would make the
	// "did the announce change the value?" question impossible to
	// answer later.
	announceCarriedValue := param.Value != nil

	// Snapshot the prior glowParam for field-diff. nil on first
	// sighting → diffParameters returns empty, which is what we want.
	var priorParam *glow.Parameter
	if seen && existing.glowParam != nil {
		priorParam = existing.glowParam
	}

	if seen && existing.glowParam != nil {
		param = mergeAnnouncedParameter(existing.glowParam, param)
	}

	stringPath := p.pathForElement(numPath, param.Identifier, param.Number, parentPath)

	obj := protocol.Object{
		Slot:   0,
		ID:     int(param.Number),
		OID:    numericKey(numPath),
		Label:  param.Identifier,
		Path:   stringPath,
		Min:    param.Minimum,
		Max:    param.Maximum,
		Step:   param.Step,
		Def:    param.Default,
		Meta:   parameterMeta(param),
	}
	if len(stringPath) > 1 {
		obj.Group = stringPath[0]
	}
	if param.Format != "" {
		obj.Unit = param.Format
	}

	switch param.Access {
	case glow.AccessRead:
		obj.Access = 1
	case glow.AccessWrite:
		obj.Access = 2
	case glow.AccessReadWrite:
		obj.Access = 3
	}

	obj.Kind = paramKindFrom(param)
	obj.Value.Kind = obj.Kind
	populateValue(&obj, param)

	// Preserve the last live value across description-only announces.
	// Neither the walk nor this announce carried a typed Value, but the
	// existing entry's obj.Value may already hold the current live state
	// (written directly by stream dispatch via deliverStreamValue). Without
	// this restore, a description-change announce would reset the watch
	// output to "?" (Kind=Unknown).
	if !announceCarriedValue && seen {
		if existing.obj.Value.Kind != protocol.KindUnknown {
			obj.Value = existing.obj.Value
			if obj.Kind == protocol.KindUnknown {
				obj.Kind = existing.obj.Value.Kind
			}
		}
	}

	if param.Type == glow.ParamTypeEnum || obj.Kind == protocol.KindEnum {
		if param.Enumeration != "" {
			obj.EnumItems = strings.Split(param.Enumeration, "\n")
		}
		if param.EnumMap != nil {
			obj.EnumItems = appendEnumMap(obj.EnumItems, param.EnumMap)
		}
	}

	entry := &treeEntry{
		glowParam:   param,
		numericPath: numPath,
		freshness:   FreshnessLive,
		updatedAt:   time.Now(),
		obj:         obj,
	}
	// Stash the field-diff on the entry so notifySubscribers can
	// attach it to the outgoing Event without re-walking state.
	entry.pendingChanges = diffParameters(priorParam, param)
	p.storeEntry(entry, stringPath)
	if param.StreamIdentifier != 0 {
		key := numericKey(entry.numericPath)
		p.subsMu.Lock()
		existing := p.streamIndex[param.StreamIdentifier]
		// Spec §7: a shared streamIdentifier is legal only when every
		// participating Parameter carries a streamDescriptor (format +
		// offset) — that is the CollectionAggregate pattern. Sharing
		// without a descriptor is a provider bug; values dispatched to
		// that streamId would overwrite one another. Flag it on the
		// new registrant's side; duplicate-detection-on-existing would
		// require re-reading numIndex entries here, which we avoid on
		// the hot path.
		if len(existing) > 0 && param.StreamDescriptor == nil && p.profile != nil {
			isNewPath := true
			for _, k := range existing {
				if k == key {
					isNewPath = false
					break
				}
			}
			if isNewPath {
				p.profile.Note(StreamIDCollisionNoDescriptor)
			}
		}
		p.streamIndex[param.StreamIdentifier] = appendUnique(existing, key)
		p.subsMu.Unlock()

		// If wildcard watch is active, send Command 30 for this
		// stream on first discovery. Without this, stream-backed
		// Parameters stay silent under `acp watch` (the provider
		// requires explicit subscription, spec p.30–31).
		p.maybeWildcardStreamSubscribe(entry)
	}
	p.notifySubscribers(entry)
	// Resolve any SetValue waiting on this OID. Done last so the
	// caller's Value reflects the post-merge state.
	p.signalPendingSet(entry)
}

// processMatrix stores a Matrix with all raw contents + targets/sources/
// connections and attaches derived state (matrix.State) for canConnect
// pre-flight validation and announce merging (A4).
//
// A subsequent processMatrix for the same path merges announced
// connections into the existing State so lastChanged and ChangedBy stay
// accurate — providers emit delta announcements, not full rewrites.
func (p *Plugin) processMatrix(m *glow.Matrix, parentPath []string, parentNumPath []int32) {
	numPath := p.resolveNumPath(m.Path, parentNumPath, m.Number)
	if len(numPath) > 0 {
		p.registerNumericPath(numPath, m.Identifier)
	}
	stringPath := p.pathForElement(numPath, m.Identifier, m.Number, parentPath)
	numKey := numericKey(numPath)

	p.treeMu.RLock()
	existing := p.numIndex[numKey]
	p.treeMu.RUnlock()

	var state *matrix.State
	isInitial := existing == nil || existing.matrixState == nil
	if !isInitial {
		state = existing.matrixState
		for _, c := range m.Connections {
			state.ApplyConnection(c, matrix.ChangeAnnounce)
		}
	} else {
		state = matrix.NewStateFromGlow(m)
	}

	entry := &treeEntry{
		glowMatrix:  m,
		numericPath: numPath,
		matrixState: state,
		freshness:   FreshnessLive,
		updatedAt:   time.Now(),
		obj: protocol.Object{
			Slot:   0,
			ID:     int(m.Number),
			OID:    numericKey(numPath),
			Label:  m.Identifier,
			Kind:   protocol.KindRaw,
			Path:   stringPath,
			Access: 3,
			Meta:   matrixMeta(m),
		},
	}
	if len(stringPath) > 1 {
		entry.obj.Group = stringPath[0]
	}
	p.storeEntry(entry, stringPath)

	// Notify subscribers of matrix crosspoint changes. On the first
	// sight of a matrix (isInitial=true) we do NOT fire per-connection
	// events — that would flood the watch with initial-state noise.
	// On subsequent updates each announced Connection is a genuine
	// crosspoint delta and fires one event. The Event carries the
	// matrix OID/Path plus a MatrixChange payload identifying the
	// specific crosspoint within it.
	if !isInitial {
		for _, c := range m.Connections {
			p.notifyMatrixSubscribers(entry, c)
		}
	}

	for _, child := range m.Children {
		p.processElement(child, stringPath, numPath)
	}

	// Spec p.42: issuing GetDirectory on a matrix node implicitly
	// subscribes the consumer to connection changes AND asks the
	// provider to emit the current tally. Many providers (incl.
	// TinyEmberPlus) only send `connections` on demand, so without
	// this call the matrix._meta.connections field stays empty.
	if len(m.Connections) == 0 && len(numPath) > 0 {
		if s := p.currentSession(); s != nil {
			numCopy := cloneInt32Slice(numPath)
			go func() {
				p.logger.Debug("emberplus: matrix GetDirectory",
					"path", numCopy, "identifier", m.Identifier)
				_ = s.SendMatrixGetDirectory(numCopy)
			}()
		}
	}
}

// notifyMatrixSubscribers fires one event per crosspoint change
// observed on an announce. Targets the per-matrix-OID callback if
// one is registered, falling back to the wildcard "*".
func (p *Plugin) notifyMatrixSubscribers(entry *treeEntry, c glow.Connection) {
	if entry == nil || entry.glowMatrix == nil {
		return
	}
	numKey := numericKey(entry.numericPath)
	p.subsMu.RLock()
	fn := p.subs[numKey]
	wildcard := p.subs["*"]
	p.subsMu.RUnlock()
	if fn == nil {
		fn = wildcard
	}
	if fn == nil {
		return
	}

	sources := make([]int64, 0, len(c.Sources))
	for _, s := range c.Sources {
		sources = append(sources, int64(s))
	}

	desc := ""
	if entry.glowMatrix != nil {
		desc = entry.glowMatrix.Description
	}
	fn(protocol.Event{
		Slot:        0,
		ID:          entry.obj.ID,
		OID:         entry.obj.OID,
		Path:        strings.Join(entry.obj.Path, "."),
		Label:       entry.obj.Label,
		Description: desc,
		Access:      entry.obj.Access,
		Group:       entry.obj.Group,
		Freshness:   freshnessLabel(entry.freshness),
		Timestamp:   time.Now(),
		MatrixChange: &protocol.MatrixChange{
			Target:      int64(c.Target),
			Sources:     sources,
			Operation:   connOpName(c.Operation),
			Disposition: connDispName(c.Disposition),
			Locked:      connDispName(c.Disposition) == "locked",
		},
	})
}

// processFunction stores a Function record; invocation plumbing lives in
// session.go.
func (p *Plugin) processFunction(f *glow.Function, parentPath []string, parentNumPath []int32) {
	numPath := p.resolveNumPath(f.Path, parentNumPath, f.Number)
	if len(numPath) > 0 {
		p.registerNumericPath(numPath, f.Identifier)
	}
	stringPath := p.pathForElement(numPath, f.Identifier, f.Number, parentPath)

	entry := &treeEntry{
		glowFunc:    f,
		numericPath: numPath,
		freshness:   FreshnessLive,
		updatedAt:   time.Now(),
		obj: protocol.Object{
			Slot:   0,
			ID:     int(f.Number),
			OID:    numericKey(numPath),
			Label:  f.Identifier,
			Kind:   protocol.KindRaw,
			Path:   stringPath,
			Access: 2,
			Meta:   functionMeta(f),
		},
	}
	if len(stringPath) > 1 {
		entry.obj.Group = stringPath[0]
	}
	p.storeEntry(entry, stringPath)

	for _, child := range f.Children {
		p.processElement(child, stringPath, numPath)
	}
}

// storeEntry writes the entry into all three indices atomically.
func (p *Plugin) storeEntry(entry *treeEntry, stringPath []string) {
	numKey := numericKey(entry.numericPath)
	strKey := strings.Join(stringPath, ".")

	p.treeMu.Lock()
	if numKey != "" {
		p.numIndex[numKey] = entry
	}
	if strKey != "" && strKey != numKey {
		p.pathIndex[strKey] = entry
	}
	if entry.obj.Label != "" {
		p.labelIndex[entry.obj.Label] = append(p.labelIndex[entry.obj.Label], entry)
	}
	p.treeMu.Unlock()
}

// notifySubscribers fires callbacks for a parameter that was just updated.
// The numeric-path callback wins over the wildcard "*".
func (p *Plugin) notifySubscribers(entry *treeEntry) {
	if entry.glowParam == nil {
		return
	}
	numKey := numericKey(entry.numericPath)
	p.subsMu.RLock()
	fn := p.subs[numKey]
	wildcard := p.subs["*"]
	p.subsMu.RUnlock()
	if fn == nil {
		fn = wildcard
	}
	if fn != nil {
		desc := ""
		if entry.glowParam != nil {
			desc = entry.glowParam.Description
		}
		changes := entry.pendingChanges
		entry.pendingChanges = nil
		fn(protocol.Event{
			Slot:        0,
			ID:          entry.obj.ID,
			OID:         entry.obj.OID,
			Path:        strings.Join(entry.obj.Path, "."),
			Label:       entry.obj.Label,
			Description: desc,
			Access:      entry.obj.Access,
			Group:       entry.obj.Group,
			Value:       entry.obj.Value,
			Freshness:   freshnessLabel(entry.freshness),
			Changes:     changes,
			Timestamp:   time.Now(),
		})
	}
}

// freshnessLabel maps the internal Freshness enum to the canonical
// string used on protocol.Event.Freshness. Keeps the enum internal
// while the public surface stays string-based.
func freshnessLabel(f Freshness) string {
	switch f {
	case FreshnessLive:
		return "live"
	case FreshnessUpdated:
		return "updated"
	case FreshnessStale:
		return "stale"
	}
	return ""
}

// pathForElement builds the human-readable identifier path. Prefers the
// numeric-path resolution when available (full hierarchy reconstructed
// from ancestor identifiers), else falls back to parent+identifier.
func (p *Plugin) pathForElement(nums []int32, identifier string, number int32, parentPath []string) []string {
	if len(nums) > 0 {
		return p.resolveStringPath(nums, identifier)
	}
	name := identifier
	if name == "" {
		name = strconv.FormatInt(int64(number), 10)
	}
	out := make([]string, len(parentPath)+1)
	copy(out, parentPath)
	out[len(parentPath)] = name
	return out
}

// resolveStringPath reconstructs the full string path by looking up each
// ancestor's identifier in numPath. Missing ancestors fall back to the
// numeric subidentifier so we still get a valid path.
func (p *Plugin) resolveStringPath(nums []int32, identifier string) []string {
	out := make([]string, len(nums))
	p.treeMu.RLock()
	for i := range nums {
		prefix := numericKey(nums[:i+1])
		if name, ok := p.numPath[prefix]; ok {
			out[i] = name
		} else {
			out[i] = strconv.FormatInt(int64(nums[i]), 10)
		}
	}
	p.treeMu.RUnlock()
	if identifier != "" && len(out) > 0 {
		out[len(out)-1] = identifier
	}
	return out
}

// registerNumericPath records a numeric prefix → identifier mapping.
// Called on every Node/Parameter/Matrix/Function we see so later children
// can reconstruct their full identifier path.
func (p *Plugin) registerNumericPath(nums []int32, identifier string) {
	if len(nums) == 0 {
		return
	}
	key := numericKey(nums)
	name := identifier
	if name == "" {
		name = strconv.FormatInt(int64(nums[len(nums)-1]), 10)
	}
	p.treeMu.Lock()
	p.numPath[key] = name
	p.treeMu.Unlock()
}

// --- small pure helpers ---

func numericKey(nums []int32) string {
	if len(nums) == 0 {
		return ""
	}
	parts := make([]string, len(nums))
	for i, n := range nums {
		parts[i] = strconv.FormatInt(int64(n), 10)
	}
	return strings.Join(parts, ".")
}

func cloneInt32Slice(in []int32) []int32 {
	if len(in) == 0 {
		return nil
	}
	out := make([]int32, len(in))
	copy(out, in)
	return out
}

// paramKindFrom maps a Glow ParameterType to the generic ValueKind.
func paramKindFrom(p *glow.Parameter) protocol.ValueKind {
	switch p.Type {
	case glow.ParamTypeInteger:
		return protocol.KindInt
	case glow.ParamTypeReal:
		return protocol.KindFloat
	case glow.ParamTypeString:
		return protocol.KindString
	case glow.ParamTypeBoolean:
		return protocol.KindBool
	case glow.ParamTypeEnum:
		return protocol.KindEnum
	case glow.ParamTypeOctets:
		return protocol.KindRaw
	}
	return protocol.KindUnknown
}

// populateValue unions-in the decoded Parameter.Value into obj.Value using
// the kind classification decided above. A nil Value (spec null) leaves
// Value at its zero.
func populateValue(obj *protocol.Object, param *glow.Parameter) {
	if param.Value == nil {
		return
	}
	switch v := param.Value.(type) {
	case int64:
		if obj.Kind == protocol.KindUnknown {
			obj.Kind = protocol.KindInt
			obj.Value.Kind = protocol.KindInt
		}
		switch obj.Kind {
		case protocol.KindInt:
			obj.Value.Int = v
		case protocol.KindUint:
			obj.Value.Uint = uint64(v)
		case protocol.KindEnum:
			obj.Value.Enum = uint8(v)
			obj.Value.Uint = uint64(v)
		case protocol.KindFloat:
			obj.Value.Float = float64(v)
		default:
			obj.Value.Int = v
		}
	case float64:
		if obj.Kind == protocol.KindUnknown {
			obj.Kind = protocol.KindFloat
			obj.Value.Kind = protocol.KindFloat
		}
		obj.Value.Float = v
	case string:
		if obj.Kind == protocol.KindUnknown {
			obj.Kind = protocol.KindString
			obj.Value.Kind = protocol.KindString
		}
		obj.Value.Str = v
	case bool:
		if obj.Kind == protocol.KindUnknown {
			obj.Kind = protocol.KindBool
			obj.Value.Kind = protocol.KindBool
		}
		obj.Value.Bool = v
	case []byte:
		obj.Value.Raw = append([]byte(nil), v...)
	}
}

// appendUnique adds s to slice if not already present. Used for the
// streamIndex where the same parameter path can be encountered twice
// when the provider re-announces a node (walk refresh, reconnect).
func appendUnique(slice []string, s string) []string {
	for _, existing := range slice {
		if existing == s {
			return slice
		}
	}
	return append(slice, s)
}

// appendEnumMap flattens an EnumMap into the obj.EnumItems slice. EnumMap
// is map[key]label but for display we only need the labels; the key is
// the value a SetValue would send.
func appendEnumMap(existing []string, em map[int64]string) []string {
	for _, label := range em {
		existing = append(existing, label)
	}
	return existing
}

// coerceStringToTyped parses val.Str into the typed field that matches
// val.Kind. Called when the CLI passes --value as a string but the
// parameter type is numeric/boolean/enum.
func coerceStringToTyped(val *protocol.Value) {
	if val.Str == "" || val.Kind == protocol.KindString {
		return
	}
	switch val.Kind {
	case protocol.KindInt:
		if n, err := strconv.ParseInt(val.Str, 10, 64); err == nil {
			val.Int = n
			val.Str = ""
		}
	case protocol.KindUint:
		if n, err := strconv.ParseUint(val.Str, 10, 64); err == nil {
			val.Uint = n
			val.Str = ""
		}
	case protocol.KindFloat:
		if f, err := strconv.ParseFloat(val.Str, 64); err == nil {
			val.Float = f
			val.Str = ""
		}
	case protocol.KindBool:
		val.Bool = val.Str == "true" || val.Str == "1" || val.Str == "yes"
		val.Str = ""
	case protocol.KindEnum:
		if n, err := strconv.ParseUint(val.Str, 10, 8); err == nil {
			val.Enum = uint8(n)
			val.Uint = n
			val.Str = ""
		}
	}
}

// valueToGlow renders a typed protocol.Value into a Glow-encoder input.
// Every encoded wire field uses the returned Go type's default mapping
// (see glow/encoder.go encodeAnyValue).
func valueToGlow(val protocol.Value) any {
	switch val.Kind {
	case protocol.KindInt:
		return val.Int
	case protocol.KindUint:
		return int64(val.Uint)
	case protocol.KindFloat:
		return val.Float
	case protocol.KindString:
		return val.Str
	case protocol.KindBool:
		return val.Bool
	case protocol.KindEnum:
		return int64(val.Enum)
	}
	if val.Str != "" {
		return val.Str
	}
	return val.Int
}
