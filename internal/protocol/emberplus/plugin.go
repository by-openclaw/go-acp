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
//	docs/protocols/emberplus.md A2.
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
	"acp/internal/protocol/emberplus/glow"
	"acp/internal/protocol/emberplus/matrix"
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
// docs/protocols/emberplus.md A2.
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
	p.mu.Unlock()

	s.SetOnElement(p.handleElements)
	return s.Connect(ctx, ip, port)
}

// Disconnect releases every explicit stream subscription (Command 31)
// then tears the session down. Safe to call on an already-disconnected
// Plugin.
func (p *Plugin) Disconnect() error {
	p.unsubscribeAll()
	p.mu.Lock()
	s := p.session
	p.session = nil
	p.mu.Unlock()
	if s != nil {
		return s.Disconnect()
	}
	return nil
}

// GetDeviceInfo returns a minimal DeviceInfo. Ember+ doesn't model slots,
// so NumSlots stays 0 and the protocol version is hardcoded to v1 of the
// Glow DTD (spec v2.50 covers Glow 2.40 extensions).
func (p *Plugin) GetDeviceInfo(ctx context.Context) (protocol.DeviceInfo, error) {
	return protocol.DeviceInfo{ProtocolVersion: 1}, nil
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
		return protocol.Value{}, fmt.Errorf("emberplus: object not found (tree has %d entries)", size)
	}
	p.logger.Debug("emberplus: GetValue",
		"key", key, "label", entry.obj.Label, "freshness", entry.freshness)
	return entry.obj.Value, nil
}

// SetValue writes a value to a parameter. The provider echoes the new
// value as an announcement; the returned Value is the *requested* value,
// not the confirmed one. Callers that need the confirmed value should
// subscribe first.
func (p *Plugin) SetValue(ctx context.Context, req protocol.ValueRequest, val protocol.Value) (protocol.Value, error) {
	s := p.currentSession()
	if s == nil {
		return protocol.Value{}, protocol.ErrNotConnected
	}
	if err := p.ensureWalked(ctx, "set"); err != nil {
		return protocol.Value{}, err
	}

	_, entry := p.findEntry(req)
	if entry == nil || entry.glowParam == nil {
		return protocol.Value{}, fmt.Errorf("emberplus: parameter not found")
	}
	path := entry.glowParam.Path
	if len(path) == 0 {
		return protocol.Value{}, fmt.Errorf("emberplus: parameter has no path")
	}

	if val.Kind == protocol.KindUnknown {
		val.Kind = entry.obj.Kind
	}
	coerceStringToTyped(&val)

	glowVal := valueToGlow(val)
	if err := s.SendSetValue(path, glowVal); err != nil {
		return protocol.Value{}, err
	}
	return val, nil
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
		return nil
	}

	if err := p.ensureWalked(context.Background(), "subscribe"); err != nil {
		return err
	}

	key, entry := p.findEntry(req)
	if entry == nil || entry.glowParam == nil || len(entry.glowParam.Path) == 0 {
		return fmt.Errorf("emberplus: parameter not found for subscribe")
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
		return fmt.Errorf("emberplus: matrix not found at path %q", matrixPath)
	}

	if entry.matrixState != nil {
		if err := entry.matrixState.CanConnect(target, sources, operation); err != nil {
			return fmt.Errorf("emberplus: matrix validation: %w", err)
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
		return nil, fmt.Errorf("emberplus: function not found at path %q", funcPath)
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
		p.processElement(el, nil)
	}
}

// processElement dispatches one Glow element to the right handler.
// Inherited parentPath is used when the provider sends non-qualified
// elements (their numeric path is implicit from the parent node).
func (p *Plugin) processElement(el glow.Element, parentPath []string) {
	switch {
	case el.Node != nil:
		p.processNode(el.Node, parentPath)
	case el.Parameter != nil:
		p.processParameter(el.Parameter, parentPath)
	case el.Matrix != nil:
		p.processMatrix(el.Matrix, parentPath)
	case el.Function != nil:
		p.processFunction(el.Function, parentPath)
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
// and invokes any registered callback (specific or wildcard).
func (p *Plugin) deliverStreamValue(entry *treeEntry, value any) {
	if entry.glowParam == nil {
		return
	}
	entry.freshness = FreshnessUpdated
	entry.updatedAt = time.Now()
	assignToValue(&entry.obj.Value, entry.obj.Kind, value)
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
// If the node arrives with no children, issues a lazy GetDirectory so we
// can fetch the subtree on demand.
func (p *Plugin) processNode(n *glow.Node, parentPath []string) {
	if len(n.Path) > 0 {
		p.registerNumericPath(n.Path, n.Identifier)
	}
	stringPath := p.pathForElement(n.Path, n.Identifier, n.Number, parentPath)

	entry := &treeEntry{
		glowNode:    n,
		numericPath: cloneInt32Slice(n.Path),
		freshness:   FreshnessLive,
		updatedAt:   time.Now(),
		obj: protocol.Object{
			Slot:   0,
			ID:     int(n.Number),
			Label:  n.Identifier,
			Kind:   protocol.KindRaw,
			Path:   stringPath,
			Access: 1,
		},
	}
	if len(stringPath) > 1 {
		entry.obj.Group = stringPath[0]
	}
	p.storeEntry(entry, stringPath)

	for _, child := range n.Children {
		p.processElement(child, stringPath)
	}

	if len(n.Children) == 0 && len(n.Path) > 0 {
		if s := p.currentSession(); s != nil {
			numPath := cloneInt32Slice(n.Path)
			go func() {
				p.logger.Debug("emberplus: requesting children",
					"path", numPath, "identifier", n.Identifier)
				_ = s.SendGetDirectoryFor(numPath)
			}()
		}
	}
}

// processParameter stores a Parameter with full ParameterContents metadata
// carried in obj constraints + glowParam pointer for lossless access.
func (p *Plugin) processParameter(param *glow.Parameter, parentPath []string) {
	if len(param.Path) > 0 {
		p.registerNumericPath(param.Path, param.Identifier)
	}
	stringPath := p.pathForElement(param.Path, param.Identifier, param.Number, parentPath)

	obj := protocol.Object{
		Slot:   0,
		ID:     int(param.Number),
		Label:  param.Identifier,
		Path:   stringPath,
		Min:    param.Minimum,
		Max:    param.Maximum,
		Step:   param.Step,
		Def:    param.Default,
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
		numericPath: cloneInt32Slice(param.Path),
		freshness:   FreshnessLive,
		updatedAt:   time.Now(),
		obj:         obj,
	}
	p.storeEntry(entry, stringPath)
	if param.StreamIdentifier != 0 {
		key := numericKey(entry.numericPath)
		p.subsMu.Lock()
		p.streamIndex[param.StreamIdentifier] = appendUnique(p.streamIndex[param.StreamIdentifier], key)
		p.subsMu.Unlock()
	}
	p.notifySubscribers(entry)
}

// processMatrix stores a Matrix with all raw contents + targets/sources/
// connections and attaches derived state (matrix.State) for canConnect
// pre-flight validation and announce merging (A4).
//
// A subsequent processMatrix for the same path merges announced
// connections into the existing State so lastChanged and ChangedBy stay
// accurate — providers emit delta announcements, not full rewrites.
func (p *Plugin) processMatrix(m *glow.Matrix, parentPath []string) {
	if len(m.Path) > 0 {
		p.registerNumericPath(m.Path, m.Identifier)
	}
	stringPath := p.pathForElement(m.Path, m.Identifier, m.Number, parentPath)
	numKey := numericKey(m.Path)

	p.treeMu.RLock()
	existing := p.numIndex[numKey]
	p.treeMu.RUnlock()

	var state *matrix.State
	if existing != nil && existing.matrixState != nil {
		state = existing.matrixState
		for _, c := range m.Connections {
			state.ApplyConnection(c, matrix.ChangeAnnounce)
		}
	} else {
		state = matrix.NewStateFromGlow(m)
	}

	entry := &treeEntry{
		glowMatrix:  m,
		numericPath: cloneInt32Slice(m.Path),
		matrixState: state,
		freshness:   FreshnessLive,
		updatedAt:   time.Now(),
		obj: protocol.Object{
			Slot:   0,
			ID:     int(m.Number),
			Label:  m.Identifier,
			Kind:   protocol.KindRaw,
			Path:   stringPath,
			Access: 3,
		},
	}
	if len(stringPath) > 1 {
		entry.obj.Group = stringPath[0]
	}
	p.storeEntry(entry, stringPath)

	for _, child := range m.Children {
		p.processElement(child, stringPath)
	}
}

// processFunction stores a Function record; invocation plumbing lives in
// session.go.
func (p *Plugin) processFunction(f *glow.Function, parentPath []string) {
	if len(f.Path) > 0 {
		p.registerNumericPath(f.Path, f.Identifier)
	}
	stringPath := p.pathForElement(f.Path, f.Identifier, f.Number, parentPath)

	entry := &treeEntry{
		glowFunc:    f,
		numericPath: cloneInt32Slice(f.Path),
		freshness:   FreshnessLive,
		updatedAt:   time.Now(),
		obj: protocol.Object{
			Slot:   0,
			ID:     int(f.Number),
			Label:  f.Identifier,
			Kind:   protocol.KindRaw,
			Path:   stringPath,
			Access: 2,
		},
	}
	if len(stringPath) > 1 {
		entry.obj.Group = stringPath[0]
	}
	p.storeEntry(entry, stringPath)

	for _, child := range f.Children {
		p.processElement(child, stringPath)
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
		fn(protocol.Event{
			Slot:      0,
			ID:        entry.obj.ID,
			Label:     entry.obj.Label,
			Group:     entry.obj.Group,
			Value:     entry.obj.Value,
			Timestamp: time.Now(),
		})
	}
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
