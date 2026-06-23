// Package emberplus is the Ember+ consumer plugin. See the package doc in
// types.go for the layered architecture (ber / s101 / glow / plugin /
// session). This file wires the consumer to the generic consumer.Protocol
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

	"dhs/internal/consumer"
	"dhs/internal/consumer/compliance"
	"dhs/internal/emberplus/codec/glow"
	"dhs/internal/emberplus/codec/matrix"
	"dhs/internal/transport"
)

func init() {
	consumer.Register(&Factory{})
}

// Factory registers the Ember+ plugin with the compile-time registry.
type Factory struct{}

// Meta publishes the static descriptor used by the plugin registry, CLI
// help, and API discovery endpoints.
func (f *Factory) Meta() consumer.ProtocolMeta {
	return consumer.ProtocolMeta{
		Name:        "emberplus",
		DefaultPort: DefaultPort,
		Description: "Ember+ (Glow/S101/TCP) consumer",
	}
}

// New constructs a fresh Plugin instance. Each device connection uses a
// separate Plugin so cached tree state cannot cross devices.
func (f *Factory) New(logger *slog.Logger) consumer.Protocol {
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

// Plugin implements consumer.Protocol for Ember+ providers.
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
	subs        map[string]consumer.EventFunc
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

	// reconnectPolicyOverride, when non-nil, replaces
	// defaultReconnectPolicy() inside reconnectLoop. Production code
	// never sets it; it exists purely as a testability seam so a test
	// can drive the reconnect loop with sub-millisecond backoff and a
	// bounded attempt count instead of the 2s/unlimited production
	// defaults. Set via setReconnectPolicy before the loop starts.
	reconnectPolicyOverride *reconnectPolicy

	// wildcardFilter controls which Parameters the wildcard "*"
	// Subscribe registers Command 30 for. Set BEFORE Subscribe via
	// SetWildcardSubscribeFilter (no-op for non-wildcard subs).
	// Empty defaults = subscribe everything.
	wildcardFilterMu    sync.RWMutex
	wildcardPathFilter  []string // dotted prefixes: OID ("1.2.3") or label-path ("mixers.primary")
	wildcardSkipStreams bool     // true → drop streamIdentifier != 0
	wildcardStreamsOnly bool     // true → keep streamIdentifier != 0 only

	// unknownCTX accumulates CTX tags the decoder skipped in
	// NodeContents / ParameterContents / MatrixContents /
	// FunctionContents SETs (spec p.93 — decoders MUST tolerate
	// unknown CTX silently). The audit Markdown report is rendered
	// from this collection at end of walk / session for sharing
	// with the device vendor.
	unknownCTX *unknownCTXAudit
}

// SetWildcardSubscribeFilter narrows the set of Parameters the
// wildcard "*" Subscribe will register Command 30 for. Must be called
// BEFORE Subscribe() with the wildcard request. All filters compose:
//
//   - paths empty AND noStreams=false AND streamsOnly=false → subscribe every Parameter
//   - paths non-empty                                       → subscribe only Parameters whose
//     numericKey ("1.2.3") OR label-path ("mixers.primary")
//     matches one of the prefixes (exact or strict prefix
//     followed by ".")
//   - noStreams=true                                        → drop Parameters with streamIdentifier != 0
//   - streamsOnly=true                                      → drop Parameters with streamIdentifier == 0
//
// noStreams and streamsOnly are mutually exclusive — caller MUST set
// at most one true. Plugin does not enforce; result is "nothing
// subscribed" if both set (intersection is empty).
//
// Filter takes effect on the NEXT Subscribe() wildcard call; existing
// subscriptions are not retroactively cancelled.
func (p *Plugin) SetWildcardSubscribeFilter(paths []string, noStreams, streamsOnly bool) {
	p.wildcardFilterMu.Lock()
	p.wildcardPathFilter = append([]string(nil), paths...)
	p.wildcardSkipStreams = noStreams
	p.wildcardStreamsOnly = streamsOnly
	p.wildcardFilterMu.Unlock()
}

// wildcardMatches returns true when a tree entry passes the active
// filter (or there is no filter). Used by both the Parameter
// notification path (notifySubscribers / subscribeAllParameters /
// subscribeOnDiscovery) and the Matrix notification path
// (notifyMatrixSubscribers). Non-Parameter entries (Matrix, Node,
// Function) have no streamIdentifier and are treated as non-stream
// for the --streams-only / --no-streams flags.
func (p *Plugin) wildcardMatches(entry *treeEntry) bool {
	if entry == nil {
		return false
	}
	p.wildcardFilterMu.RLock()
	paths := p.wildcardPathFilter
	skipStreams := p.wildcardSkipStreams
	streamsOnly := p.wildcardStreamsOnly
	p.wildcardFilterMu.RUnlock()

	isStream := false
	if entry.glowParam != nil {
		isStream = entry.glowParam.StreamIdentifier != 0
	}
	if skipStreams && isStream {
		return false
	}
	if streamsOnly && !isStream {
		// --streams-only drops non-stream entries — Matrix events,
		// plain Parameters, Node events all classify as non-stream.
		return false
	}
	if len(paths) == 0 {
		return true
	}
	labelPath := strings.Join(entry.obj.Path, ".")
	numPath := numericKey(entry.numericPath)
	for _, prefix := range paths {
		if labelPath == prefix || strings.HasPrefix(labelPath, prefix+".") {
			return true
		}
		if numPath == prefix || strings.HasPrefix(numPath, prefix+".") {
			return true
		}
	}
	return false
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
	obj consumer.Object

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
	pendingChanges []consumer.FieldChange

	// lastEmittedRendered is the stringified value rendered at the
	// last successful notifySubscribers fire. Used to suppress the
	// dual-fire case where a Parameter announce AND a StreamEntry
	// both deliver the same final value (Lawo metering emits both
	// per step). When pendingChanges is empty AND the new render
	// matches this string, the second emission is dropped.
	lastEmittedRendered string
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
	p.subs = make(map[string]consumer.EventFunc)
	p.streamSubs = make(map[string][]int32)
	p.streamIndex = make(map[int64][]string)
	p.templates = make(map[string]*glow.Template)
	p.pendingSets = newPendingSetRegistry()
	p.profile = &compliance.Profile{}
	p.connIP = ip
	p.connPort = port
	p.unknownCTX = newUnknownCTXAudit()
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

		// Clear subscription tracking. The old session is dead, so
		// Command 31 (Unsubscribe) is pointless and in any case
		// impossible. The fresh session created by reconnect needs
		// to re-issue Command 30 for every Parameter, which
		// subscribeAllParameters will do — but ONLY if streamSubs
		// is empty, since its idempotency check skips entries it
		// already thinks are subscribed. Without this clear, no
		// announces resume.
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

	changes := []consumer.FieldChange{
		{Name: "isOnline", Old: oldLbl, New: newLbl},
	}
	if reason != "" {
		changes = append(changes, consumer.FieldChange{
			Name: "reason", Old: "", New: reason,
		})
	}

	ev := consumer.Event{
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
//
// DtdVersion (refs #470): primary source is S101 app-bytes (offsets 7+8
// = DTD minor/major), latched by the session on the first EmBER frame.
// When `info` runs before any payload exchange the session has only
// seen the S101 handshake, so GetDeviceInfo probes the provider with a
// root GetDirectory and waits briefly for its reply — every shipping
// Ember+ provider answers GetDirectory(root) with a 9-byte header
// carrying the DTD app-bytes. The fallback is reading
// `identity.dtdVersion` from the walked tree (rare — only providers
// that emit 5-byte S101 headers ever need this path). Both routes
// best-effort; failure leaves DtdVersion="" and cmd_info prints
// "dtd_version unknown".
func (p *Plugin) GetDeviceInfo(ctx context.Context) (consumer.DeviceInfo, error) {
	info := consumer.DeviceInfo{
		IP:              p.connIP,
		Port:            p.connPort,
		ProtocolVersion: 1,
		NumSlots:        1,
	}
	info.DtdVersion = p.resolveDtdVersion(ctx)
	return info, nil
}

// resolveDtdVersion returns the Ember+ DTD revision the connected
// provider speaks. See GetDeviceInfo for the resolution order.
func (p *Plugin) resolveDtdVersion(ctx context.Context) string {
	s := p.currentSession()
	if s == nil {
		return ""
	}
	if v := s.DtdVersion(); v != "" {
		return v
	}
	// Probe: provoke an EmBER reply so the session can latch app-bytes.
	// Bounded wait — never hold up `info` for more than a short window.
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.SendGetDirectory(); err == nil {
		if v := s.WaitForDtdVersion(probeCtx); v != "" {
			return v
		}
	}
	// Fallback: identity.dtdVersion in the walked tree. Skip the
	// auto-walk — operators that want the slow path can run `walk`
	// explicitly. If the tree is empty here the fallback simply
	// returns "".
	p.treeMu.RLock()
	defer p.treeMu.RUnlock()
	if entry, ok := p.pathIndex["identity.dtdVersion"]; ok {
		if entry.obj.Value.Kind == consumer.KindString {
			return entry.obj.Value.Str
		}
	}
	return ""
}

// GetSlotInfo pretends the whole Ember+ tree is slot 0. Any other slot is
// a usage error.
//
// IsOnline tracks the underlying session liveness — true when the consumer
// is past Ember+ S101 handshake (sessionConnected). Ember+ has no per-slot
// hot-plug semantics, so the slot follows the session bool 1:1. Refs #458.
func (p *Plugin) GetSlotInfo(ctx context.Context, slot int) (consumer.SlotInfo, error) {
	if slot != 0 {
		return consumer.SlotInfo{}, fmt.Errorf("emberplus: only slot 0 supported")
	}
	p.mu.Lock()
	live := p.sessionConnected
	p.mu.Unlock()
	return consumer.SlotInfo{
		Slot:     0,
		Status:   consumer.SlotPresent,
		State:    consumer.SlotStatePresent,
		IsOnline: live,
	}, nil
}

// Walk triggers a full GetDirectory and blocks until the tree stops
// growing. Uses a settle timer: after receiving new entries it waits 2s of
// silence before returning (typical TinyEmber+ walk = 2–3s, real devices
// can take longer). Total bound is 15s of silence.
func (p *Plugin) Walk(ctx context.Context, slot int) ([]consumer.Object, error) {
	if slot != 0 {
		return nil, fmt.Errorf("emberplus: only slot 0")
	}

	s := p.currentSession()
	if s == nil {
		return nil, consumer.ErrNotConnected
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

// snapshot returns every live Object under RLock, enriched for cache
// serialisation (refs #438). The walker writes per-element Meta during
// processElement (matrixMeta / parameterMeta / etc.), but matrix
// targetLabels / sourceLabels can only be resolved post-walk once all
// label parameters under each basePath have been decoded.
// enrichMatrixLabels does that inline join so the DM file is
// self-contained — provider-side canonical.Matrix shape.
func (p *Plugin) snapshot() []consumer.Object {
	p.treeMu.RLock()
	out := make([]consumer.Object, 0, len(p.numIndex))
	for _, entry := range p.numIndex {
		out = append(out, entry.obj)
	}
	p.treeMu.RUnlock()
	out = appendTemplateObjects(out, p)
	return enrichMatrixLabels(out)
}

// appendTemplateObjects serializes templates (which live in
// p.templates separate from the tree) as synthetic consumer.Objects
// with Meta["element"] = "template", so the DM cache round-trip
// preserves them. SeedTreeFromCachedObjects registers them back in
// p.templates via seedTemplate (no entry in numIndex — templates are
// looked up by ResolveTemplate, not iterated).
func appendTemplateObjects(out []consumer.Object, p *Plugin) []consumer.Object {
	p.templatesMu.RLock()
	defer p.templatesMu.RUnlock()
	for key, t := range p.templates {
		if t == nil {
			continue
		}
		out = append(out, consumer.Object{
			OID:   key,
			Label: t.Description,
			Meta: map[string]any{
				"element":     "template",
				"qualified":   t.Qualified,
				"number":      int64(t.Number),
				"description": t.Description,
			},
		})
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
func (p *Plugin) findEntry(req consumer.ValueRequest) (string, *treeEntry) {
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
func (p *Plugin) GetValue(ctx context.Context, req consumer.ValueRequest) (consumer.Value, error) {
	if err := p.ensureWalked(ctx, "get"); err != nil {
		return consumer.Value{}, err
	}

	key, entry := p.findEntry(req)
	if entry == nil {
		p.treeMu.RLock()
		size := len(p.numIndex)
		p.treeMu.RUnlock()
		return consumer.Value{}, WrapProto(fmt.Sprintf("object not found (tree has %d entries)", size), nil)
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
// Error semantics (see internal/consumer/errors.go):
//
//   - consumer.ErrNotConnected → session is dead; no wire traffic sent.
//     Returns the last known value from the tree.
//   - consumer.ErrWriteTimeout → send succeeded but no confirming
//     announce arrived within defaultWriteTimeout. Tree value
//     unchanged.
//   - consumer.ErrWriteCoerced → provider announced a different value
//     (clamp, round). Returned Value reflects what the provider
//     applied. Caller opts-in to accept via errors.Is.
//
// The method is blocking; callers wanting fire-and-forget writes
// should cancel ctx early.
func (p *Plugin) SetValue(ctx context.Context, req consumer.ValueRequest, val consumer.Value) (consumer.Value, error) {
	// Session-liveness gate: never send on a dead session.
	p.mu.Lock()
	connected := p.sessionConnected
	p.mu.Unlock()
	if !connected {
		return consumer.Value{}, consumer.ErrNotConnected
	}

	s := p.currentSession()
	if s == nil {
		return consumer.Value{}, consumer.ErrNotConnected
	}

	if err := p.ensureWalked(ctx, "set"); err != nil {
		return consumer.Value{}, err
	}

	key, entry := p.findEntry(req)
	if entry == nil || entry.glowParam == nil {
		return consumer.Value{}, WrapProto("parameter not found", nil)
	}
	path := entry.glowParam.Path
	if len(path) == 0 {
		// Fallback for non-qualified providers that registered the
		// parameter without a wire Path: use our canonical numeric.
		path = cloneInt32Slice(entry.numericPath)
	}
	if len(path) == 0 {
		return consumer.Value{}, WrapProto("parameter has no path", nil)
	}

	if val.Kind == consumer.KindUnknown {
		val.Kind = entry.obj.Kind
	}
	// R16 #483: resolve enum label → integer index BEFORE the type
	// coerce so `--value "Low"` on `{Off=0, Low=1, ...}` works
	// transparently. Numeric input passes through unchanged.
	if err := resolveEnumLabelToIndex(&val, entry.glowParam); err != nil {
		return consumer.Value{}, err
	}
	if err := coerceStringToTyped(&val); err != nil {
		return consumer.Value{}, err
	}
	// R16 #483: range / step gate. Strict by default; --round snaps
	// off-step numerics to the nearest legal grid point.
	if err := applyParameterConstraints(&val, entry.glowParam, req); err != nil {
		return consumer.Value{}, err
	}

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
		return consumer.Value{}, err
	}

	// Await confirmation or timeout.
	timer := time.NewTimer(defaultWriteTimeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return entry.obj.Value, ctx.Err()
	case <-timer.C:
		return entry.obj.Value, consumer.ErrWriteTimeout
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
func (p *Plugin) Subscribe(req consumer.ValueRequest, fn consumer.EventFunc) error {
	s := p.currentSession()
	if s == nil {
		return consumer.ErrNotConnected
	}

	if req.Path == "" && req.Label == "" && req.ID < 0 {
		p.subsMu.Lock()
		p.subs["*"] = fn
		p.subsMu.Unlock()

		// Strict providers require explicit Command 30 per
		// Parameter (spec p.30–31) — GetDirectory alone won't
		// start announces. Walk + subscribe happen in the
		// background so Subscribe() returns immediately to the
		// caller (same pattern as ACP1/ACP2 watch: caller enters
		// the event loop and sees announces stream in as the walk
		// discovers each Parameter via subscribeOnDiscovery).
		// ensureWalked is a no-op when numIndex is already
		// populated (cached DM seeded via SeedTreeFromCachedObjects
		// or a prior Walk in the same session).
		go func() {
			if err := p.ensureWalked(context.Background(), "wildcard subscribe"); err != nil {
				p.logger.Debug("emberplus: wildcard subscribe — initial walk failed",
					"err", err)
				// Continue anyway: subscribe whatever IS in
				// numIndex (partial walk), and
				// subscribeOnDiscovery catches any further
				// Parameters that arrive later.
			}
			// Final batch — covers Parameters that landed before
			// the wildcard was registered (DM-seeded entries).
			// subscribeOnDiscovery already handled anything
			// discovered during the walk above; the idempotency
			// check in subscribeAllParameters skips duplicates.
			p.subscribeAllParameters()
		}()
		return nil
	}

	if err := p.ensureWalked(context.Background(), "subscribe"); err != nil {
		return err
	}

	key, entry := p.findEntry(req)
	if entry == nil || entry.glowParam == nil {
		return WrapProto("parameter not found for subscribe", nil)
	}
	// Non-qualified Glow Parameters (DTD <2.10) carry only [0] Number;
	// the absolute path is composed by the walker into entry.numericPath.
	// Fall back to that when glowParam.Path is empty so the SendSubscribe
	// encoder still has a target — strict spec: both forms are valid.
	subscribePath := entry.glowParam.Path
	if len(subscribePath) == 0 {
		subscribePath = entry.numericPath
	}
	if len(subscribePath) == 0 {
		return WrapProto("parameter not found for subscribe", nil)
	}

	p.subsMu.Lock()
	p.subs[key] = fn
	if entry.glowParam.HasStreamIdentifier {
		p.streamSubs[key] = subscribePath
	}
	p.subsMu.Unlock()

	if entry.glowParam.HasStreamIdentifier {
		p.logger.Debug("emberplus: explicit stream subscribe",
			"path", key, "stream_identifier", entry.glowParam.StreamIdentifier)
		return s.SendSubscribe(subscribePath)
	}
	p.logger.Debug("emberplus: implicit subscribe via GetDirectory", "path", key)
	return nil
}

// Unsubscribe removes a callback. Sends Command 31 only if the original
// subscription was explicit (streamed parameter per spec p.31).
func (p *Plugin) Unsubscribe(req consumer.ValueRequest) error {
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
	p.subs = make(map[string]consumer.EventFunc)
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
		return consumer.ErrNotConnected
	}
	if err := p.ensureWalked(ctx, "matrix"); err != nil {
		return err
	}

	_, entry := p.findEntry(consumer.ValueRequest{Path: matrixPath, ID: -1})
	if entry == nil || entry.glowMatrix == nil {
		return WrapProto(fmt.Sprintf("matrix not found at path %q", matrixPath), nil)
	}

	if entry.matrixState != nil {
		if err := entry.matrixState.CanConnect(target, sources, operation); err != nil {
			return WrapProto("matrix validation", err)
		}
		// Fire one compliance event per implicit source-steal pair on
		// oneToOne SETs (spec p.33 says source-exclusive, every
		// shipping provider source-steals). Pre-flight detection lets
		// the operator see the deviation in `dhs consumer emberplus
		// profile`. Refs #465.
		if stolen := entry.matrixState.DetectOneToOneSourceSteal(target, sources); len(stolen) > 0 {
			for _, pair := range stolen {
				p.profile.Note(OneToOneSourceStealAccepted)
				p.logger.Debug("emberplus: oneToOne source-steal accepted",
					"matrix_path", matrixPath,
					"target", target,
					"source", pair.Source,
					"from_target", pair.FromTarget)
			}
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
// that carries a streamIdentifier. If filter is empty, every stream
// parameter is returned. Otherwise the result is limited to parameters
// whose streamIdentifier appears in filter (R10 multi-subscribe).
// Used by cmd stream (A8).
func (p *Plugin) StreamParameterPaths(filter []int64) []string {
	p.subsMu.RLock()
	defer p.subsMu.RUnlock()
	var allow map[int64]struct{}
	if len(filter) > 0 {
		allow = make(map[int64]struct{}, len(filter))
		for _, id := range filter {
			allow[id] = struct{}{}
		}
	}
	var out []string
	for id, paths := range p.streamIndex {
		if allow != nil {
			if _, ok := allow[id]; !ok {
				continue
			}
		}
		out = append(out, paths...)
	}
	return out
}

// MatrixSnapshot returns the derived matrix state for the matrix at
// matrixPath, or nil if not a matrix. Useful for UIs rendering a tally.
func (p *Plugin) MatrixSnapshot(matrixPath string) []matrix.TargetState {
	_, entry := p.findEntry(consumer.ValueRequest{Path: matrixPath, ID: -1})
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
		return nil, consumer.ErrNotConnected
	}
	if err := p.ensureWalked(ctx, "invoke"); err != nil {
		return nil, err
	}

	_, entry := p.findEntry(consumer.ValueRequest{Path: funcPath, ID: -1})
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
		// Wire-level Success=false → typed error so the CLI exit
		// dispatcher returns 1. The result pointer is still returned so
		// callers can print the (possibly-empty) Result tuple alongside
		// the error. Per docs/protocols/error-codes.md: the
		// error string carries the diagnostic, exit code carries the
		// pass/fail signal.
		if result != nil && !result.Success {
			return result, fmt.Errorf("%w: invocation %d on %q",
				ErrInvocationFailed, result.InvocationID, funcPath)
		}
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
	// Provider speaks legacy non-qualified Glow (DTD <2.10). Pin the
	// session so subscribe / unsubscribe / setvalue use the nested
	// Node/Parameter chain wire form the provider's path index
	// recognises. Both forms are spec — strict-spec compliance
	// requires emitting whichever the provider speaks. See Ember+
	// Documentation.pdf §Glow Compatibility.
	p.mu.Lock()
	if p.session != nil {
		p.session.SetUseLegacyGlow(true)
	}
	p.mu.Unlock()
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
		entry.pendingChanges = []consumer.FieldChange{{
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
func assignToValue(v *consumer.Value, kind consumer.ValueKind, value any) {
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
	if p.unknownCTX != nil && len(n.UnknownContents) > 0 {
		p.unknownCTX.record("node", numPath, n.UnknownContents)
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
		obj: consumer.Object{
			Slot:   0,
			ID:     int(n.Number),
			OID:    numericKey(numPath),
			Label:  n.Identifier,
			Kind:   consumer.KindRaw,
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
	if p.HasStreamIdentifier {
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
// matrixStaticMetaKeys are the MatrixContents fields that describe the
// matrix shape (not per-frame connection deltas). A connection-only
// announce omits them; they must persist from the frame that carried them
// rather than be clobbered to zero/empty. "element" and "connections" are
// excluded — those are kept fresh from the current frame.
var matrixStaticMetaKeys = []string{
	"type", "mode", "targetCount", "sourceCount",
	"maximumTotalConnects", "maximumConnectsPerTarget",
	"parametersLocation", "gainParameterNumber",
	"labels", "targets", "sources",
	"schemaIdentifiers", "templateReference", "description",
}

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
	if p.unknownCTX != nil && len(param.UnknownContents) > 0 {
		p.unknownCTX.record("parameter", numPath, param.UnknownContents)
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

	obj := consumer.Object{
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
	// Format is printf-style with optional unit after a '°' marker
	// (spec p.85 Contents [6]). Store just the unit suffix on
	// obj.Unit so watch output renders "value <unit>" cleanly; the
	// raw format string would clutter the display.
	if u := extractFormatUnit(param.Format); u != "" {
		obj.Unit = u
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
		if existing.obj.Value.Kind != consumer.KindUnknown {
			obj.Value = existing.obj.Value
			if obj.Kind == consumer.KindUnknown {
				obj.Kind = existing.obj.Value.Kind
			}
		}
	}

	if param.Type == glow.ParamTypeEnum || obj.Kind == consumer.KindEnum {
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
	if param.HasStreamIdentifier {
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
		// Parameter on first discovery. Without this, strict
		// providers stay silent under `dhs consumer emberplus
		// watch` (spec p.30–31).
		p.subscribeOnDiscovery(entry)
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
	if p.unknownCTX != nil && len(m.UnknownContents) > 0 {
		p.unknownCTX.record("matrix", numPath, m.UnknownContents)
	}
	stringPath := p.pathForElement(numPath, m.Identifier, m.Number, parentPath)
	numKey := numericKey(numPath)

	p.treeMu.RLock()
	existing := p.numIndex[numKey]
	p.treeMu.RUnlock()

	var state *matrix.State
	// changedConns holds only the connections that genuinely rerouted a
	// target we already knew — the subset worth a watch event. The initial
	// tally (every target new) and provider re-broadcasts (same sources)
	// are excluded so a big console matrix like DHD Device.Routing.2 doesn't
	// flood the watch with hundreds of false "modified" lines (it streams
	// the tally in multiple waves, which the old per-message first-sighting
	// flag could not suppress past wave 1).
	var changedConns []glow.Connection
	isInitial := existing == nil || existing.matrixState == nil
	if !isInitial {
		state = existing.matrixState
		for _, c := range m.Connections {
			existed, changed := state.ApplyConnectionReport(c, matrix.ChangeAnnounce)
			if existed && changed {
				changedConns = append(changedConns, c)
			}
		}
	} else {
		state = matrix.NewStateFromGlow(m)
	}

	// Build Meta, but DON'T let a connection-only delta wipe the matrix's
	// static MatrixContents. Providers send the full MatrixContents
	// (targetCount/sourceCount/labels/targets/sources) once, then stream
	// connection-only deltas (everything else zero/empty). matrixMeta(m)
	// rebuilt from such a delta would reset those to 0/[] — which on the
	// DHD console zeroed targetCount (real: 147) and dropped the "Primary"
	// labels descriptor (basePath 0.5.1), so enrichMatrixLabels resolved
	// nothing. When this frame carries no contents, keep the static fields
	// from the contents-bearing frame and take only the fresh connections.
	meta := matrixMeta(m)
	matrixHasContents := m.TargetCount != 0 || m.SourceCount != 0 ||
		len(m.Labels) > 0 || len(m.Targets) > 0 || len(m.Sources) > 0 ||
		m.ParametersLocation != nil || len(m.TemplateReference) > 0
	if !isInitial && !matrixHasContents && existing != nil && existing.obj.Meta != nil {
		for _, k := range matrixStaticMetaKeys {
			if v, ok := existing.obj.Meta[k]; ok {
				meta[k] = v
			} else {
				delete(meta, k)
			}
		}
	}

	entry := &treeEntry{
		glowMatrix:  m,
		numericPath: numPath,
		matrixState: state,
		freshness:   FreshnessLive,
		updatedAt:   time.Now(),
		obj: consumer.Object{
			Slot:   0,
			ID:     int(m.Number),
			OID:    numericKey(numPath),
			Label:  m.Identifier,
			Kind:   consumer.KindRaw,
			Path:   stringPath,
			Access: 3,
			Meta:   meta,
		},
	}
	if len(stringPath) > 1 {
		entry.obj.Group = stringPath[0]
	}
	p.storeEntry(entry, stringPath)

	// Notify subscribers of matrix crosspoint changes. First sight of a
	// matrix (isInitial=true) populates state silently — no initial-state
	// noise. On later messages we fire ONLY for changedConns: connections
	// that rerouted a target we already knew. That excludes both the
	// initial tally streamed across multiple waves (targets still new) and
	// provider re-broadcasts of the same tally (same sources). The Event
	// carries the matrix OID/Path plus a MatrixChange payload identifying
	// the specific crosspoint.
	if !isInitial {
		for _, c := range changedConns {
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
	//
	// Gate on isInitial — same "first sighting" guard processNode uses
	// for its lazy GetDirectory. A matrix with NO current routes is a
	// legal steady state: the provider answers our GetDirectory with the
	// matrix again, still connections-empty. Without the guard we'd
	// re-request on every such reply, and since each reply keeps the walk
	// settle-timer alive this spins forever (observed live on DHD
	// Device.routing.2, a matrix with zero connections). One request on
	// first sight is enough; empty-after-that means "no routes".
	if isInitial && len(m.Connections) == 0 && len(numPath) > 0 {
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
	// Same wildcard-filter gate as notifySubscribers: when only the
	// wildcard would fire (no per-matrix subscriber), respect the
	// --path / --no-streams / --streams-only filter. Per spec p.88
	// matrix change events are implicitly subscribed via GetDirectory
	// on the matrix, so the provider keeps emitting; the filter is
	// the consumer's mechanism to scope what the wildcard watcher
	// sees.
	if fn == nil {
		if wildcard == nil || !p.wildcardMatches(entry) {
			return
		}
		fn = wildcard
	}
	// unreachable: after the block above fn is either the original
	// non-nil per-matrix subscriber (we only entered the block when fn was
	// nil) or `wildcard`, which is proven non-nil by the `wildcard == nil`
	// guard at line ~2066. A second nil-check here can therefore never be
	// true; removed to keep the function fully exercised. fn is always
	// safe to call below.

	sources := make([]int64, 0, len(c.Sources))
	for _, s := range c.Sources {
		sources = append(sources, int64(s))
	}

	desc := ""
	if entry.glowMatrix != nil {
		desc = entry.glowMatrix.Description
	}
	fn(consumer.Event{
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
		MatrixChange: &consumer.MatrixChange{
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
	if p.unknownCTX != nil && len(f.UnknownContents) > 0 {
		p.unknownCTX.record("function", numPath, f.UnknownContents)
	}
	stringPath := p.pathForElement(numPath, f.Identifier, f.Number, parentPath)

	entry := &treeEntry{
		glowFunc:    f,
		numericPath: numPath,
		freshness:   FreshnessLive,
		updatedAt:   time.Now(),
		obj: consumer.Object{
			Slot:   0,
			ID:     int(f.Number),
			OID:    numericKey(numPath),
			Label:  f.Identifier,
			Kind:   consumer.KindRaw,
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
	// When falling back to the wildcard (no per-OID subscriber for
	// this Parameter), respect the wildcard filter — value-change
	// announces AND StreamEntry dispatch both end up here, so this
	// is the single chokepoint that gates --no-streams /
	// --streams-only / --path at the callback layer even when the
	// provider broadcasts beyond our Subscribe(30) intent.
	if fn == nil {
		if wildcard == nil || !p.wildcardMatches(entry) {
			return
		}
		fn = wildcard
	}
	if fn != nil {
		// Apply Parameter factor (spec p.85 Contents [8]) and unit
		// suffix (spec p.85 Contents [6]) so watch / dhs-srv get
		// engineering values, not raw wire integers. ACP1/ACP2 ship
		// engineering values directly; Ember+ owes the same.
		val, unit := displayValueAndUnit(entry)

		// Dual-fire suppression: Lawo metering (and any stream-
		// tagged Parameter) emits both a Parameter announce AND a
		// StreamEntry per value step; both converge here with the
		// same final value. Skip the redundant fire when no diff
		// is pending AND the rendered value matches the last fire.
		rendered := formatAny(entry.glowParam.Value)
		if len(entry.pendingChanges) == 0 && rendered == entry.lastEmittedRendered {
			return
		}

		desc := ""
		if entry.glowParam != nil {
			desc = entry.glowParam.Description
		}
		changes := entry.pendingChanges
		entry.pendingChanges = nil
		entry.lastEmittedRendered = rendered
		fn(consumer.Event{
			Slot:        0,
			ID:          entry.obj.ID,
			OID:         entry.obj.OID,
			Path:        strings.Join(entry.obj.Path, "."),
			Label:       entry.obj.Label,
			Description: desc,
			Unit:        unit,
			Access:      entry.obj.Access,
			Group:       entry.obj.Group,
			Value:       val,
			Freshness:   freshnessLabel(entry.freshness),
			Changes:     changes,
			Timestamp:   time.Now(),
		})
	}
}

// freshnessLabel maps the internal Freshness enum to the canonical
// string used on consumer.Event.Freshness. Keeps the enum internal
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
func paramKindFrom(p *glow.Parameter) consumer.ValueKind {
	switch p.Type {
	case glow.ParamTypeInteger:
		return consumer.KindInt
	case glow.ParamTypeReal:
		return consumer.KindFloat
	case glow.ParamTypeString:
		return consumer.KindString
	case glow.ParamTypeBoolean:
		return consumer.KindBool
	case glow.ParamTypeEnum:
		return consumer.KindEnum
	case glow.ParamTypeOctets:
		return consumer.KindRaw
	}
	return consumer.KindUnknown
}

// populateValue unions-in the decoded Parameter.Value into obj.Value using
// the kind classification decided above. A nil Value (spec null) leaves
// Value at its zero.
func populateValue(obj *consumer.Object, param *glow.Parameter) {
	if param.Value == nil {
		return
	}
	switch v := param.Value.(type) {
	case int64:
		if obj.Kind == consumer.KindUnknown {
			obj.Kind = consumer.KindInt
			obj.Value.Kind = consumer.KindInt
		}
		switch obj.Kind {
		case consumer.KindInt:
			obj.Value.Int = v
		case consumer.KindUint:
			obj.Value.Uint = uint64(v)
		case consumer.KindEnum:
			obj.Value.Enum = uint8(v)
			obj.Value.Uint = uint64(v)
		case consumer.KindFloat:
			obj.Value.Float = float64(v)
		default:
			obj.Value.Int = v
		}
	case float64:
		if obj.Kind == consumer.KindUnknown {
			obj.Kind = consumer.KindFloat
			obj.Value.Kind = consumer.KindFloat
		}
		obj.Value.Float = v
	case string:
		if obj.Kind == consumer.KindUnknown {
			obj.Kind = consumer.KindString
			obj.Value.Kind = consumer.KindString
		}
		obj.Value.Str = v
	case bool:
		if obj.Kind == consumer.KindUnknown {
			obj.Kind = consumer.KindBool
			obj.Value.Kind = consumer.KindBool
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
//
// On parse failure the function returns a *consumer.ValidationError so
// the CLI exits with status 2 (usage error) and the typed field is NOT
// silently coerced to the Go zero value. Refs #445 — the previous
// behaviour silently rewrote "-25.5" → 0 for an integer Parameter,
// which violates the strict-spec posture in internal/emberplus/CLAUDE.md.
//
// KindBool stays permissive ("true"/"1"/"yes" → true; anything else →
// false) to preserve existing CLI semantics — booleans have no natural
// invalid form and operators routinely pass "off"/"no"/"0" expecting
// the obvious mapping.
func coerceStringToTyped(val *consumer.Value) error {
	if val.Str == "" || val.Kind == consumer.KindString {
		return nil
	}
	switch val.Kind {
	case consumer.KindInt:
		n, err := strconv.ParseInt(val.Str, 10, 64)
		if err != nil {
			return &consumer.ValidationError{
				Field:  "value",
				Reason: fmt.Sprintf("invalid integer %q", val.Str),
			}
		}
		val.Int = n
		val.Str = ""
	case consumer.KindUint:
		n, err := strconv.ParseUint(val.Str, 10, 64)
		if err != nil {
			return &consumer.ValidationError{
				Field:  "value",
				Reason: fmt.Sprintf("invalid unsigned integer %q", val.Str),
			}
		}
		val.Uint = n
		val.Str = ""
	case consumer.KindFloat:
		f, err := strconv.ParseFloat(val.Str, 64)
		if err != nil {
			return &consumer.ValidationError{
				Field:  "value",
				Reason: fmt.Sprintf("invalid real %q", val.Str),
			}
		}
		val.Float = f
		val.Str = ""
	case consumer.KindBool:
		val.Bool = val.Str == "true" || val.Str == "1" || val.Str == "yes"
		val.Str = ""
	case consumer.KindEnum:
		n, err := strconv.ParseUint(val.Str, 10, 8)
		if err != nil {
			return &consumer.ValidationError{
				Field:  "value",
				Reason: fmt.Sprintf("invalid enum index %q", val.Str),
			}
		}
		val.Enum = uint8(n)
		val.Uint = n
		val.Str = ""
	}
	return nil
}

// valueToGlow renders a typed consumer.Value into a Glow-encoder input.
// Every encoded wire field uses the returned Go type's default mapping
// (see glow/encoder.go encodeAnyValue).
func valueToGlow(val consumer.Value) any {
	switch val.Kind {
	case consumer.KindInt:
		return val.Int
	case consumer.KindUint:
		return int64(val.Uint)
	case consumer.KindFloat:
		return val.Float
	case consumer.KindString:
		return val.Str
	case consumer.KindBool:
		return val.Bool
	case consumer.KindEnum:
		return int64(val.Enum)
	}
	if val.Str != "" {
		return val.Str
	}
	return val.Int
}
