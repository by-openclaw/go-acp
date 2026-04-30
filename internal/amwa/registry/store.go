package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"acp/internal/amwa/codec/is04"
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

	// health tracks the last heartbeat per Node ID. The GC loop walks
	// this map every tick.
	health map[string]time.Time

	listeners []changeListener
}

// NewStore returns an empty Store. The caller wires changeListeners
// via AddListener — the subscription manager registers itself there.
func NewStore() *Store {
	return &Store{
		nodes:     make(map[string]is04.Node),
		devices:   make(map[string]is04.Device),
		sources:   make(map[string]is04.Source),
		flows:     make(map[string]is04.Flow),
		senders:   make(map[string]is04.Sender),
		receivers: make(map[string]is04.Receiver),
		health:    make(map[string]time.Time),
	}
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
	// On insert, mark health = now so the GC doesn't immediately evict.
	if !hadPrev {
		s.health[n.ID] = time.Now()
	}
	post, _ := json.Marshal(n)
	c := Change{ResourceType: is04.ResourceNode, ID: n.ID, Post: post, Timestamp: time.Now()}
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
	post, _ := json.Marshal(d)
	c := Change{ResourceType: is04.ResourceDevice, ID: d.ID, Post: post, Timestamp: time.Now()}
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
	post, _ := json.Marshal(src)
	c := Change{ResourceType: is04.ResourceSource, ID: src.ID, Post: post, Timestamp: time.Now()}
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

// PutFlow inserts/updates. Requires parent Device.
func (s *Store) PutFlow(f is04.Flow) error {
	if err := f.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.devices[f.DeviceID]; !ok {
		return fmt.Errorf("registry: flow %s.device_id %q not registered", f.ID, f.DeviceID)
	}
	prev, hadPrev := s.flows[f.ID]
	s.flows[f.ID] = f
	post, _ := json.Marshal(f)
	c := Change{ResourceType: is04.ResourceFlow, ID: f.ID, Post: post, Timestamp: time.Now()}
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
	post, _ := json.Marshal(snd)
	c := Change{ResourceType: is04.ResourceSender, ID: snd.ID, Post: post, Timestamp: time.Now()}
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
	post, _ := json.Marshal(r)
	c := Change{ResourceType: is04.ResourceReceiver, ID: r.ID, Post: post, Timestamp: time.Now()}
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
		// Evict children of this device.
		for sid, src := range s.sources {
			if src.DeviceID == did {
				pre, _ := json.Marshal(src)
				delete(s.sources, sid)
				s.fanOut(Change{Kind: ChangeDeleted, ResourceType: is04.ResourceSource, ID: sid, Pre: pre, Timestamp: now})
			}
		}
		for fid, f := range s.flows {
			if f.DeviceID == did {
				pre, _ := json.Marshal(f)
				delete(s.flows, fid)
				s.fanOut(Change{Kind: ChangeDeleted, ResourceType: is04.ResourceFlow, ID: fid, Pre: pre, Timestamp: now})
			}
		}
		for sid, snd := range s.senders {
			if snd.DeviceID == did {
				pre, _ := json.Marshal(snd)
				delete(s.senders, sid)
				s.fanOut(Change{Kind: ChangeDeleted, ResourceType: is04.ResourceSender, ID: sid, Pre: pre, Timestamp: now})
			}
		}
		for rid, r := range s.receivers {
			if r.DeviceID == did {
				pre, _ := json.Marshal(r)
				delete(s.receivers, rid)
				s.fanOut(Change{Kind: ChangeDeleted, ResourceType: is04.ResourceReceiver, ID: rid, Pre: pre, Timestamp: now})
			}
		}
		pre, _ := json.Marshal(d)
		delete(s.devices, did)
		s.fanOut(Change{Kind: ChangeDeleted, ResourceType: is04.ResourceDevice, ID: did, Pre: pre, Timestamp: now})
	}
	pre, _ := json.Marshal(s.nodes[id])
	delete(s.nodes, id)
	delete(s.health, id)
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
		s.fanOut(Change{Kind: ChangeDeleted, ResourceType: t, ID: id, Pre: pre, Timestamp: now})
	case is04.ResourceSource:
		v, ok := s.sources[id]
		if !ok {
			return ErrNotFound
		}
		pre, _ := json.Marshal(v)
		delete(s.sources, id)
		s.fanOut(Change{Kind: ChangeDeleted, ResourceType: t, ID: id, Pre: pre, Timestamp: now})
	case is04.ResourceFlow:
		v, ok := s.flows[id]
		if !ok {
			return ErrNotFound
		}
		pre, _ := json.Marshal(v)
		delete(s.flows, id)
		s.fanOut(Change{Kind: ChangeDeleted, ResourceType: t, ID: id, Pre: pre, Timestamp: now})
	case is04.ResourceSender:
		v, ok := s.senders[id]
		if !ok {
			return ErrNotFound
		}
		pre, _ := json.Marshal(v)
		delete(s.senders, id)
		s.fanOut(Change{Kind: ChangeDeleted, ResourceType: t, ID: id, Pre: pre, Timestamp: now})
	case is04.ResourceReceiver:
		v, ok := s.receivers[id]
		if !ok {
			return ErrNotFound
		}
		pre, _ := json.Marshal(v)
		delete(s.receivers, id)
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
// used to bootstrap a new WS subscriber.
func (s *Store) SnapshotChanges() []Change {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	out := make([]Change, 0, len(s.nodes)+len(s.devices)+len(s.sources)+len(s.flows)+len(s.senders)+len(s.receivers))
	for _, v := range s.nodes {
		raw, _ := json.Marshal(v)
		out = append(out, Change{Kind: ChangeSync, ResourceType: is04.ResourceNode, ID: v.ID, Post: raw, Timestamp: now})
	}
	for _, v := range s.devices {
		raw, _ := json.Marshal(v)
		out = append(out, Change{Kind: ChangeSync, ResourceType: is04.ResourceDevice, ID: v.ID, Post: raw, Timestamp: now})
	}
	for _, v := range s.sources {
		raw, _ := json.Marshal(v)
		out = append(out, Change{Kind: ChangeSync, ResourceType: is04.ResourceSource, ID: v.ID, Post: raw, Timestamp: now})
	}
	for _, v := range s.flows {
		raw, _ := json.Marshal(v)
		out = append(out, Change{Kind: ChangeSync, ResourceType: is04.ResourceFlow, ID: v.ID, Post: raw, Timestamp: now})
	}
	for _, v := range s.senders {
		raw, _ := json.Marshal(v)
		out = append(out, Change{Kind: ChangeSync, ResourceType: is04.ResourceSender, ID: v.ID, Post: raw, Timestamp: now})
	}
	for _, v := range s.receivers {
		raw, _ := json.Marshal(v)
		out = append(out, Change{Kind: ChangeSync, ResourceType: is04.ResourceReceiver, ID: v.ID, Post: raw, Timestamp: now})
	}
	return out
}

// IngestRegistration decodes the envelope, decodes the inner data per
// type, and writes through to the store.
func (s *Store) IngestRegistration(env *is04.RegistrationRequest) error {
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
