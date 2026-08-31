package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"dhs/internal/amwa/codec/is04"
)

// ErrNotFound is returned by every getter when the resource is unknown.
var ErrNotFound = errors.New("registry: resource not found")

// ErrTypeMismatch is returned when a registration envelope's `type`
// disagrees with the URL path's resource type — Registry detects the
// inconsistency and rejects.
var ErrTypeMismatch = errors.New("registry: registration envelope type mismatch")

// ChangeKind enumerates the four IS-04 §5.2 grain change types.
type ChangeKind string

const (
	ChangeCreated ChangeKind = "created"
	ChangeUpdated ChangeKind = "updated"
	ChangeDeleted ChangeKind = "deleted"
	ChangeSync    ChangeKind = "sync"
)

// Change is a single IS-04 §5.2 grain payload — the WS subscription
// stream carries one grain per resource change.
type Change struct {
	Kind         ChangeKind
	ResourceType is04.ResourceType
	ID           string
	APIVer       string          // wire version this resource was registered at
	Pre          json.RawMessage // nil on create
	Post         json.RawMessage // nil on delete
	Timestamp    time.Time
}

// changeListener is the internal callback shape registered by the
// subscription manager. Store invokes every listener under its read
// lock so listeners must NOT call back into the store.
type changeListener func(Change)

// Store is the in-memory IS-04 resource catalogue. Spec-strict per
// IS-04 §3 — six resource types keyed by UUID, with cascade-delete on
// Node removal.
type Store struct {
	mu sync.RWMutex

	nodes     map[string]is04.Node
	devices   map[string]is04.Device
	sources   map[string]is04.Source
	flows     map[string]is04.Flow
	senders   map[string]is04.Sender
	receivers map[string]is04.Receiver

	// defaultPageLimit overrides DefaultPageLimit for requests that
	// carry no paging.limit. 0 keeps the spec-parity default (100).
	// Exists because first-page-only clients are real: Cerebrum's
	// Network Media reader takes page one per collection and stops, so
	// on a plant where one device owns 208 senders, everything
	// registered earlier silently vanishes from such a controller.
	// Raising the DEFAULT is the operator's spec-legal lever — an
	// explicit paging.limit from the client always wins.
	defaultPageLimit int

	// health tracks the last heartbeat per Node ID. The GC loop walks
	// this map every tick.
	health map[string]time.Time

	// owners maps a resource id to the IS-10 client_id that registered
	// it (BCP-003-02: the Registration API rejects updates from a
	// DIFFERENT client with 403 — IS-04-02 test_33/test_33_1). Entries
	// are overwritten on every authenticated create, so a stale entry
	// left by an evicted resource can never block a re-registration.
	// Empty when auth is off.
	owners map[string]string

	// updateTSByType tracks the per-resource last-update TAI timestamp
	// per IS-04 §6.1.6 — Query API pagination indexes resources by
	// this. Keyed type → id → "<secs>:<nanos>". Maintained on every
	// Put and pruned on every Delete.
	updateTSByType map[is04.ResourceType]map[string]string

	// lastUpdateTS is the most recent timestamp handed out by
	// markUpdated, kept strictly monotonic across the whole store.
	// Without it, a burst of registrations inside one clock tick mints
	// resources with IDENTICAL update_ts, and paging's exclusive
	// `since` cursor then skips every tied item past a page boundary —
	// a walk of a 211-sender plant returned 100 with page 2 empty
	// (found live 2026-08-29, reproduced by paging_walk_test.go).
	lastUpdateTS string

	// apiVerByType tracks the IS-04 wire version each resource was
	// registered at. Drives the no-downgrade-by-default Query
	// semantics IS-04 §6.1.5 (and AMWA test_22 / test_32) — a Node
	// posted at /registration/v1.0 doesn't appear at /query/v1.3
	// unless the client opts in via `?query.downgrade=v1.0`.
	apiVerByType map[is04.ResourceType]map[string]string

	listeners []changeListener
}

// NewStore returns an empty Store. The caller wires changeListeners
// via AddListener — the subscription manager registers itself there.
func NewStore() *Store {
	return &Store{
		nodes:          make(map[string]is04.Node),
		devices:        make(map[string]is04.Device),
		sources:        make(map[string]is04.Source),
		flows:          make(map[string]is04.Flow),
		senders:        make(map[string]is04.Sender),
		receivers:      make(map[string]is04.Receiver),
		health:         make(map[string]time.Time),
		owners:         make(map[string]string),
		updateTSByType: make(map[is04.ResourceType]map[string]string, 6),
		apiVerByType:   make(map[is04.ResourceType]map[string]string, 6),
	}
}

// markUpdated stamps id's update_ts to "now (TAI)" under the type
// bucket. Caller MUST hold the write lock.
func (s *Store) markUpdated(t is04.ResourceType, id string) {
	bucket, ok := s.updateTSByType[t]
	if !ok {
		bucket = make(map[string]string)
		s.updateTSByType[t] = bucket
	}
	// Strictly monotonic across the store: a wall-clock reading that
	// ties or precedes the last handed-out timestamp is bumped by one
	// nanosecond past it. Cursor pagination depends on update_ts being
	// a total order — see the lastUpdateTS field comment.
	ts := nowTAI()
	if s.lastUpdateTS != "" && taiCmp(ts, s.lastUpdateTS) <= 0 {
		ts = taiBump(s.lastUpdateTS)
	}
	s.lastUpdateTS = ts
	bucket[id] = ts
}

// dropUpdated drops id from the type bucket on delete. Caller MUST
// hold the write lock.
func (s *Store) dropUpdated(t is04.ResourceType, id string) {
	if bucket, ok := s.updateTSByType[t]; ok {
		delete(bucket, id)
	}
	if bucket, ok := s.apiVerByType[t]; ok {
		delete(bucket, id)
	}
}

// markAPIVer records the wire version a resource was registered at.
// Caller MUST hold the write lock. Empty apiVer is treated as "any"
// (drops the entry so subsequent queries return the resource at any
// version) — used by tests that pre-populate the store directly.
func (s *Store) markAPIVer(t is04.ResourceType, id, apiVer string) {
	if apiVer == "" {
		if bucket, ok := s.apiVerByType[t]; ok {
			delete(bucket, id)
		}
		return
	}
	bucket, ok := s.apiVerByType[t]
	if !ok {
		bucket = make(map[string]string)
		s.apiVerByType[t] = bucket
	}
	bucket[id] = apiVer
}

// apiVerOfLocked is the lock-free reader used by Put* + fanOut paths
// that already hold the store mutex. Returns "" when the resource has
// no api_ver stamp.
func (s *Store) apiVerOfLocked(t is04.ResourceType, id string) string {
	if bucket, ok := s.apiVerByType[t]; ok {
		return bucket[id]
	}
	return ""
}

// APIVerOf returns the registered wire version for (t, id), or "" if
// the resource isn't registered or was registered without a version
// stamp. Read-locked.
func (s *Store) APIVerOf(t is04.ResourceType, id string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.apiVerOfLocked(t, id)
}

// maxUpdateTSLocked returns the largest update_ts across ALL resources
// of type t, irrespective of any filter. Used by the Query API
// pagination layer to anchor X-Paging-Until at the head of the time
// series. Returns "" when the type bucket is empty. Caller MUST hold
// the read or write lock.
func (s *Store) maxUpdateTSLocked(t is04.ResourceType) string {
	bucket := s.updateTSByType[t]
	max := ""
	for _, ts := range bucket {
		if max == "" || taiCmp(ts, max) > 0 {
			max = ts
		}
	}
	return max
}

// AddListener registers a callback for every Change emission. Returns
// an unsubscribe func.
func (s *Store) AddListener(fn changeListener) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, fn)
	idx := len(s.listeners) - 1
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if idx < len(s.listeners) {
			s.listeners[idx] = nil
		}
	}
}

// fanOut emits c to every registered listener. Caller MUST hold the
// write lock.
func (s *Store) fanOut(c Change) {
	for _, fn := range s.listeners {
		if fn != nil {
			fn(c)
		}
	}
}

// PutNode inserts or updates a Node. Triggers `created` or `updated`.
func (s *Store) PutNode(n is04.Node) error {
	if err := n.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, hadPrev := s.nodes[n.ID]
	s.nodes[n.ID] = n
	s.markUpdated(is04.ResourceNode, n.ID)
	// On insert, mark health = now so the GC doesn't immediately evict.
	if !hadPrev {
		s.health[n.ID] = time.Now()
	}
	post, _ := json.Marshal(n)
	c := Change{ResourceType: is04.ResourceNode, ID: n.ID, APIVer: s.apiVerOfLocked(is04.ResourceNode, n.ID), Post: post, Timestamp: time.Now()}
	if hadPrev {
		pre, _ := json.Marshal(prev)
		c.Kind = ChangeUpdated
		c.Pre = pre
	} else {
		c.Kind = ChangeCreated
	}
	s.fanOut(c)
	return nil
}

// PutDevice inserts/updates. Returns ErrNotFound if the parent Node is
// absent — referential integrity per IS-04 §3.
func (s *Store) PutDevice(d is04.Device) error {
	if err := d.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.nodes[d.NodeID]; !ok {
		return fmt.Errorf("registry: device %s.node_id %q not registered", d.ID, d.NodeID)
	}
	prev, hadPrev := s.devices[d.ID]
	s.devices[d.ID] = d
	s.markUpdated(is04.ResourceDevice, d.ID)
	post, _ := json.Marshal(d)
	c := Change{ResourceType: is04.ResourceDevice, ID: d.ID, APIVer: s.apiVerOfLocked(is04.ResourceDevice, d.ID), Post: post, Timestamp: time.Now()}
	if hadPrev {
		pre, _ := json.Marshal(prev)
		c.Kind = ChangeUpdated
		c.Pre = pre
	} else {
		c.Kind = ChangeCreated
	}
	s.fanOut(c)
	return nil
}

// PutSource inserts/updates. Requires parent Device.
func (s *Store) PutSource(src is04.Source) error {
	if err := src.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.devices[src.DeviceID]; !ok {
		return fmt.Errorf("registry: source %s.device_id %q not registered", src.ID, src.DeviceID)
	}
	prev, hadPrev := s.sources[src.ID]
	s.sources[src.ID] = src
	s.markUpdated(is04.ResourceSource, src.ID)
	post, _ := json.Marshal(src)
	c := Change{ResourceType: is04.ResourceSource, ID: src.ID, APIVer: s.apiVerOfLocked(is04.ResourceSource, src.ID), Post: post, Timestamp: time.Now()}
	if hadPrev {
		pre, _ := json.Marshal(prev)
		c.Kind = ChangeUpdated
		c.Pre = pre
	} else {
		c.Kind = ChangeCreated
	}
	s.fanOut(c)
	return nil
}

// PutFlow inserts/updates. Requires parent Device — except on the
// v1.0 wire shape, which removes `device_id` from the Flow object
// entirely (Flow's parent in v1.0 is the Source, not the Device). In
// every IS-04 minor the source_id MUST point at a registered Source.
func (s *Store) PutFlow(f is04.Flow) error {
	if err := f.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if f.DeviceID != "" {
		if _, ok := s.devices[f.DeviceID]; !ok {
			return fmt.Errorf("registry: flow %s.device_id %q not registered", f.ID, f.DeviceID)
		}
	}
	if _, ok := s.sources[f.SourceID]; !ok {
		return fmt.Errorf("registry: flow %s.source_id %q not registered", f.ID, f.SourceID)
	}
	prev, hadPrev := s.flows[f.ID]
	s.flows[f.ID] = f
	s.markUpdated(is04.ResourceFlow, f.ID)
	post, _ := json.Marshal(f)
	c := Change{ResourceType: is04.ResourceFlow, ID: f.ID, APIVer: s.apiVerOfLocked(is04.ResourceFlow, f.ID), Post: post, Timestamp: time.Now()}
	if hadPrev {
		pre, _ := json.Marshal(prev)
		c.Kind = ChangeUpdated
		c.Pre = pre
	} else {
		c.Kind = ChangeCreated
	}
	s.fanOut(c)
	return nil
}

// PutSender inserts/updates. Requires parent Device.
func (s *Store) PutSender(snd is04.Sender) error {
	if err := snd.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.devices[snd.DeviceID]; !ok {
		return fmt.Errorf("registry: sender %s.device_id %q not registered", snd.ID, snd.DeviceID)
	}
	prev, hadPrev := s.senders[snd.ID]
	s.senders[snd.ID] = snd
	s.markUpdated(is04.ResourceSender, snd.ID)
	post, _ := json.Marshal(snd)
	c := Change{ResourceType: is04.ResourceSender, ID: snd.ID, APIVer: s.apiVerOfLocked(is04.ResourceSender, snd.ID), Post: post, Timestamp: time.Now()}
	if hadPrev {
		pre, _ := json.Marshal(prev)
		c.Kind = ChangeUpdated
		c.Pre = pre
	} else {
		c.Kind = ChangeCreated
	}
	s.fanOut(c)
	return nil
}

// PutReceiver inserts/updates. Requires parent Device.
func (s *Store) PutReceiver(r is04.Receiver) error {
	if err := r.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.devices[r.DeviceID]; !ok {
		return fmt.Errorf("registry: receiver %s.device_id %q not registered", r.ID, r.DeviceID)
	}
	prev, hadPrev := s.receivers[r.ID]
	s.receivers[r.ID] = r
	s.markUpdated(is04.ResourceReceiver, r.ID)
	post, _ := json.Marshal(r)
	c := Change{ResourceType: is04.ResourceReceiver, ID: r.ID, APIVer: s.apiVerOfLocked(is04.ResourceReceiver, r.ID), Post: post, Timestamp: time.Now()}
	if hadPrev {
		pre, _ := json.Marshal(prev)
		c.Kind = ChangeUpdated
		c.Pre = pre
	} else {
		c.Kind = ChangeCreated
	}
	s.fanOut(c)
	return nil
}

// DeleteNode evicts the Node and every Device/Source/Flow/Sender/
// Receiver pointing at it (cascade). Fires deleted events for every
// evicted resource.
func (s *Store) DeleteNode(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteNodeLocked(id)
}

func (s *Store) deleteNodeLocked(id string) {
	if _, ok := s.nodes[id]; !ok {
		return
	}
	now := time.Now()
	// Evict dependents first so observers never see orphan resources
	// without their parent.
	for did, d := range s.devices {
		if d.NodeID != id {
			continue
		}
		// Evict children of this device. Walk Sources first, then
		// Flows — IS-04 v1.0 Flows DO NOT carry device_id, only
		// source_id, so we cascade them via the just-deleted source's
		// id rather than via the device id.
		removedSourceIDs := make(map[string]struct{})
		for sid, src := range s.sources {
			if src.DeviceID == did {
				pre, _ := json.Marshal(src)
				delete(s.sources, sid)
				s.dropUpdated(is04.ResourceSource, sid)
				removedSourceIDs[sid] = struct{}{}
				s.fanOut(Change{Kind: ChangeDeleted, ResourceType: is04.ResourceSource, ID: sid, Pre: pre, Timestamp: now})
			}
		}
		for fid, f := range s.flows {
			_, sourceGone := removedSourceIDs[f.SourceID]
			if f.DeviceID == did || sourceGone {
				pre, _ := json.Marshal(f)
				delete(s.flows, fid)
				s.dropUpdated(is04.ResourceFlow, fid)
				s.fanOut(Change{Kind: ChangeDeleted, ResourceType: is04.ResourceFlow, ID: fid, Pre: pre, Timestamp: now})
			}
		}
		for sid, snd := range s.senders {
			if snd.DeviceID == did {
				pre, _ := json.Marshal(snd)
				delete(s.senders, sid)
				s.dropUpdated(is04.ResourceSender, sid)
				s.fanOut(Change{Kind: ChangeDeleted, ResourceType: is04.ResourceSender, ID: sid, Pre: pre, Timestamp: now})
			}
		}
		for rid, r := range s.receivers {
			if r.DeviceID == did {
				pre, _ := json.Marshal(r)
				delete(s.receivers, rid)
				s.dropUpdated(is04.ResourceReceiver, rid)
				s.fanOut(Change{Kind: ChangeDeleted, ResourceType: is04.ResourceReceiver, ID: rid, Pre: pre, Timestamp: now})
			}
		}
		pre, _ := json.Marshal(d)
		delete(s.devices, did)
		s.dropUpdated(is04.ResourceDevice, did)
		s.fanOut(Change{Kind: ChangeDeleted, ResourceType: is04.ResourceDevice, ID: did, Pre: pre, Timestamp: now})
	}
	pre, _ := json.Marshal(s.nodes[id])
	delete(s.nodes, id)
	delete(s.health, id)
	s.dropUpdated(is04.ResourceNode, id)
	s.fanOut(Change{Kind: ChangeDeleted, ResourceType: is04.ResourceNode, ID: id, Pre: pre, Timestamp: now})
}

// DeleteResource evicts a non-Node resource. For Node, use DeleteNode.
func (s *Store) DeleteResource(t is04.ResourceType, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	switch t {
	case is04.ResourceNode:
		s.deleteNodeLocked(id)
		return nil
	case is04.ResourceDevice:
		d, ok := s.devices[id]
		if !ok {
			return ErrNotFound
		}
		pre, _ := json.Marshal(d)
		delete(s.devices, id)
		s.dropUpdated(t, id)
		s.fanOut(Change{Kind: ChangeDeleted, ResourceType: t, ID: id, Pre: pre, Timestamp: now})
	case is04.ResourceSource:
		v, ok := s.sources[id]
		if !ok {
			return ErrNotFound
		}
		pre, _ := json.Marshal(v)
		delete(s.sources, id)
		s.dropUpdated(t, id)
		s.fanOut(Change{Kind: ChangeDeleted, ResourceType: t, ID: id, Pre: pre, Timestamp: now})
	case is04.ResourceFlow:
		v, ok := s.flows[id]
		if !ok {
			return ErrNotFound
		}
		pre, _ := json.Marshal(v)
		delete(s.flows, id)
		s.dropUpdated(t, id)
		s.fanOut(Change{Kind: ChangeDeleted, ResourceType: t, ID: id, Pre: pre, Timestamp: now})
	case is04.ResourceSender:
		v, ok := s.senders[id]
		if !ok {
			return ErrNotFound
		}
		pre, _ := json.Marshal(v)
		delete(s.senders, id)
		s.dropUpdated(t, id)
		s.fanOut(Change{Kind: ChangeDeleted, ResourceType: t, ID: id, Pre: pre, Timestamp: now})
	case is04.ResourceReceiver:
		v, ok := s.receivers[id]
		if !ok {
			return ErrNotFound
		}
		pre, _ := json.Marshal(v)
		delete(s.receivers, id)
		s.dropUpdated(t, id)
		s.fanOut(Change{Kind: ChangeDeleted, ResourceType: t, ID: id, Pre: pre, Timestamp: now})
	default:
		return fmt.Errorf("registry: invalid resource type %q", t)
	}
	return nil
}

// Heartbeat updates the last-seen timestamp for a Node. Returns
// ErrNotFound if the Node isn't registered.
func (s *Store) Heartbeat(nodeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.nodes[nodeID]; !ok {
		return ErrNotFound
	}
	s.health[nodeID] = time.Now()
	return nil
}

// HealthFor reports the last-seen timestamp for a Node. Returns
// ErrNotFound if the Node isn't registered.
func (s *Store) HealthFor(nodeID string) (time.Time, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.health[nodeID]
	if !ok {
		return time.Time{}, ErrNotFound
	}
	return t, nil
}

// Owner returns the client_id that registered a resource, "" when
// unknown (auth off, or registered before auth was armed).
func (s *Store) Owner(id string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.owners[id]
}

// SetOwner records the registering client for a resource.
func (s *Store) SetOwner(id, client string) {
	s.mu.Lock()
	s.owners[id] = client
	s.mu.Unlock()
}

// EvictStale walks the health map and DeleteNodes any whose last
// heartbeat is older than threshold. Returns the count evicted.
func (s *Store) EvictStale(threshold time.Duration) int {
	cutoff := time.Now().Add(-threshold)
	s.mu.Lock()
	stale := make([]string, 0)
	for id, last := range s.health {
		if last.Before(cutoff) {
			stale = append(stale, id)
		}
	}
	for _, id := range stale {
		s.deleteNodeLocked(id)
	}
	s.mu.Unlock()
	return len(stale)
}

// ListNodes returns a snapshot copy of all Nodes.
func (s *Store) ListNodes() []is04.Node {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]is04.Node, 0, len(s.nodes))
	for _, v := range s.nodes {
		out = append(out, v)
	}
	return out
}
func (s *Store) ListDevices() []is04.Device {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]is04.Device, 0, len(s.devices))
	for _, v := range s.devices {
		out = append(out, v)
	}
	return out
}
func (s *Store) ListSources() []is04.Source {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]is04.Source, 0, len(s.sources))
	for _, v := range s.sources {
		out = append(out, v)
	}
	return out
}
func (s *Store) ListFlows() []is04.Flow {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]is04.Flow, 0, len(s.flows))
	for _, v := range s.flows {
		out = append(out, v)
	}
	return out
}
func (s *Store) ListSenders() []is04.Sender {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]is04.Sender, 0, len(s.senders))
	for _, v := range s.senders {
		out = append(out, v)
	}
	return out
}
func (s *Store) ListReceivers() []is04.Receiver {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]is04.Receiver, 0, len(s.receivers))
	for _, v := range s.receivers {
		out = append(out, v)
	}
	return out
}

// GetNode returns a copy of the named Node or ErrNotFound.
func (s *Store) GetNode(id string) (is04.Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.nodes[id]
	if !ok {
		return is04.Node{}, ErrNotFound
	}
	return v, nil
}
func (s *Store) GetDevice(id string) (is04.Device, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.devices[id]
	if !ok {
		return is04.Device{}, ErrNotFound
	}
	return v, nil
}
func (s *Store) GetSource(id string) (is04.Source, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.sources[id]
	if !ok {
		return is04.Source{}, ErrNotFound
	}
	return v, nil
}
func (s *Store) GetFlow(id string) (is04.Flow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.flows[id]
	if !ok {
		return is04.Flow{}, ErrNotFound
	}
	return v, nil
}
func (s *Store) GetSender(id string) (is04.Sender, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.senders[id]
	if !ok {
		return is04.Sender{}, ErrNotFound
	}
	return v, nil
}
func (s *Store) GetReceiver(id string) (is04.Receiver, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.receivers[id]
	if !ok {
		return is04.Receiver{}, ErrNotFound
	}
	return v, nil
}

// SnapshotChanges returns a `sync` change per existing resource —
// used to bootstrap a new WS subscriber. When wireVer is empty the
// resources are marshaled in their canonical shape; when set, each
// body is run through the matching is04.Codec so the SYNC payload
// matches the wire shape AMWA test_31 expects (which compares
// pre/post against the per-version fixture the test posted).
func (s *Store) SnapshotChanges(wireVer string) []Change {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	out := make([]Change, 0, len(s.nodes)+len(s.devices)+len(s.sources)+len(s.flows)+len(s.senders)+len(s.receivers))

	codec, _ := is04.Get(wireVer)
	encNode := func(n is04.Node) []byte {
		if codec != nil {
			if b, err := codec.EncodeNode(n); err == nil {
				return b
			}
		}
		b, _ := json.Marshal(n)
		return b
	}
	encDevice := func(d is04.Device) []byte {
		if codec != nil {
			if b, err := codec.EncodeDevice(d); err == nil {
				return b
			}
		}
		b, _ := json.Marshal(d)
		return b
	}
	encSource := func(v is04.Source) []byte {
		if codec != nil {
			if b, err := codec.EncodeSource(v); err == nil {
				return b
			}
		}
		b, _ := json.Marshal(v)
		return b
	}
	encFlow := func(v is04.Flow) []byte {
		if codec != nil {
			if b, err := codec.EncodeFlow(v); err == nil {
				return b
			}
		}
		b, _ := json.Marshal(v)
		return b
	}
	encSender := func(v is04.Sender) []byte {
		if codec != nil {
			if b, err := codec.EncodeSender(v); err == nil {
				return b
			}
		}
		b, _ := json.Marshal(v)
		return b
	}
	encReceiver := func(v is04.Receiver) []byte {
		if codec != nil {
			if b, err := codec.EncodeReceiver(v); err == nil {
				return b
			}
		}
		b, _ := json.Marshal(v)
		return b
	}

	for _, v := range s.nodes {
		out = append(out, Change{Kind: ChangeSync, ResourceType: is04.ResourceNode, ID: v.ID, APIVer: s.apiVerOfLocked(is04.ResourceNode, v.ID), Post: encNode(v), Timestamp: now})
	}
	for _, v := range s.devices {
		out = append(out, Change{Kind: ChangeSync, ResourceType: is04.ResourceDevice, ID: v.ID, APIVer: s.apiVerOfLocked(is04.ResourceDevice, v.ID), Post: encDevice(v), Timestamp: now})
	}
	for _, v := range s.sources {
		out = append(out, Change{Kind: ChangeSync, ResourceType: is04.ResourceSource, ID: v.ID, APIVer: s.apiVerOfLocked(is04.ResourceSource, v.ID), Post: encSource(v), Timestamp: now})
	}
	for _, v := range s.flows {
		out = append(out, Change{Kind: ChangeSync, ResourceType: is04.ResourceFlow, ID: v.ID, APIVer: s.apiVerOfLocked(is04.ResourceFlow, v.ID), Post: encFlow(v), Timestamp: now})
	}
	for _, v := range s.senders {
		out = append(out, Change{Kind: ChangeSync, ResourceType: is04.ResourceSender, ID: v.ID, APIVer: s.apiVerOfLocked(is04.ResourceSender, v.ID), Post: encSender(v), Timestamp: now})
	}
	for _, v := range s.receivers {
		out = append(out, Change{Kind: ChangeSync, ResourceType: is04.ResourceReceiver, ID: v.ID, APIVer: s.apiVerOfLocked(is04.ResourceReceiver, v.ID), Post: encReceiver(v), Timestamp: now})
	}
	return out
}

// IngestRegistration decodes the envelope, decodes the inner data per
// type, and writes through to the store. Defaults to the v1.3 presence
// rules — registry handlers wired against an older minor should call
// IngestRegistrationVersioned to swap in a per-spec required-key set.
func (s *Store) IngestRegistration(env *is04.RegistrationRequest) error {
	return s.IngestRegistrationVersioned(env, is04.APIVersion)
}

// ErrAPIVerConflict is returned when a registration POST targets an
// existing (type, id) but at a different wire version than the one
// it was originally registered with — IS-04 §6.1.1 mandates HTTP 409
// (Conflict) for this case (AMWA test_32 verifies it).
var ErrAPIVerConflict = errors.New("registry: api_ver conflict — resource already registered at a different wire version")

// IngestRegistrationVersioned runs the same path as IngestRegistration
// but applies the per-version JSON-Schema "required" set on the raw
// envelope before unmarshal. v1.0 of the spec doesn't carry `tags` or
// `description` on most resources; v1.1+ does. AMWA IS-04-02 test_03
// (and friends) post the per-version downgraded fixtures to the
// version-specific URL, so the registry must accept a v1.0 body
// without `tags`/`description` and reject a v1.0 body that's missing
// `id` or `label`.
//
// Returns ErrAPIVerConflict when a record already exists for (type, id)
// but at a different api_ver — the registration handler maps this to
// HTTP 409.
func (s *Store) IngestRegistrationVersioned(env *is04.RegistrationRequest, apiVer string) error {
	if err := validateRegistrationPresenceVersioned(env, apiVer); err != nil {
		return err
	}
	id := idFromEnvelope(env)
	if id != "" {
		if existing := s.APIVerOf(env.Type, id); existing != "" && apiVer != "" && existing != apiVer {
			return ErrAPIVerConflict
		}
	}
	// Stamp the api_ver BEFORE the typed Put so the Change emitted on
	// fanOut carries the correct version (the manager's onChange
	// listener reads it without re-locking).
	if id != "" && apiVer != "" {
		s.mu.Lock()
		s.markAPIVer(env.Type, id, apiVer)
		s.mu.Unlock()
	}
	if err := s.ingestTyped(env); err != nil {
		// Roll back the api_ver stamp on failure so we don't end up
		// with an api_ver entry pointing at a resource that doesn't
		// exist.
		if id != "" && apiVer != "" {
			s.mu.Lock()
			if bucket, ok := s.apiVerByType[env.Type]; ok {
				delete(bucket, id)
			}
			s.mu.Unlock()
		}
		return err
	}
	return nil
}

func (s *Store) ingestTyped(env *is04.RegistrationRequest) error {
	switch env.Type {
	case is04.ResourceNode:
		var n is04.Node
		if err := json.Unmarshal(env.Data, &n); err != nil {
			return fmt.Errorf("registry: decode node: %w", err)
		}
		return s.PutNode(n)
	case is04.ResourceDevice:
		var d is04.Device
		if err := json.Unmarshal(env.Data, &d); err != nil {
			return fmt.Errorf("registry: decode device: %w", err)
		}
		return s.PutDevice(d)
	case is04.ResourceSource:
		var v is04.Source
		if err := json.Unmarshal(env.Data, &v); err != nil {
			return fmt.Errorf("registry: decode source: %w", err)
		}
		return s.PutSource(v)
	case is04.ResourceFlow:
		var v is04.Flow
		if err := json.Unmarshal(env.Data, &v); err != nil {
			return fmt.Errorf("registry: decode flow: %w", err)
		}
		return s.PutFlow(v)
	case is04.ResourceSender:
		var v is04.Sender
		if err := json.Unmarshal(env.Data, &v); err != nil {
			return fmt.Errorf("registry: decode sender: %w", err)
		}
		return s.PutSender(v)
	case is04.ResourceReceiver:
		var v is04.Receiver
		if err := json.Unmarshal(env.Data, &v); err != nil {
			return fmt.Errorf("registry: decode receiver: %w", err)
		}
		return s.PutReceiver(v)
	}
	return fmt.Errorf("registry: invalid resource type %q", env.Type)
}

// validateRegistrationPresenceVersioned enforces the per-API-version
// JSON-Schema "required" set for the resource_core fields. The
// inbound JSON shape differs across minors:
//
//   - v1.0:        required = {id, version, label}
//                  (`tags` and `description` were added in v1.1)
//   - v1.1, v1.2:  required = {id, version, label, description, tags}
//   - v1.3:        required = {id, version, label, description, tags}
//
// AMWA test_04 (and its do_400_check siblings test_06/test_08/...)
// delete the `label` key and expect HTTP 400 — that's the canonical
// schema-validation gate the AMWA tool exercises. Without an explicit
// presence check the typed Validate() can't distinguish "field
// absent" from "field present with zero value" since json.Unmarshal
// collapses both into the Go zero value.
func validateRegistrationPresenceVersioned(env *is04.RegistrationRequest, apiVer string) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(env.Data, &raw); err != nil {
		return fmt.Errorf("registry: data is not a JSON object: %w", err)
	}
	required := []string{"id", "version", "label", "description", "tags"}
	if apiVer == "v1.0" {
		required = []string{"id", "version", "label"}
	}
	for _, key := range required {
		if _, ok := raw[key]; !ok {
			return fmt.Errorf("registry: %s.%s: required (resource_core)", env.Type, key)
		}
	}
	return nil
}
