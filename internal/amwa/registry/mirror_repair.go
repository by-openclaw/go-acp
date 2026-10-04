package registry

// Repairing the target: what the mirror re-sends, and how much.
//
// Something leaves the target short of the source — it evicted a node,
// it refused a child whose parent had not landed, it held a resource
// under another minor. The mirror then owes the target a pass, and a
// pass sends only what the target can have lost:
//
//   - an evicted node: that node and everything under it;
//   - a refused resource: that resource and the ancestors it names;
//   - what cannot be bounded (an ancestor the mirror has never seen, a
//     target holding none of its nodes, the first fill): the whole
//     catalogue, re-read from the source first.
//
// It used to be the whole catalogue every time: one refused flow
// re-POSTed seven thousand resources into a registry that dies on a
// burst (#1311).
//
// Passes never overlap and nothing waits for one — work asked for while
// a pass runs is folded into the next. Within a pass a child is not
// sent once the target has refused its parent: the answer is known.

import (
	"context"
	"encoding/json"
	"sort"
	"time"
)

// owedWork is what a pass must do.
type owedWork struct {
	// all is the whole catalogue, re-read from the source first; why
	// names that pass in the audit trail.
	all bool
	why string
	// nodes are nodes the target evicted: each is owed its subtree.
	nodes map[string]bool
	// refused are resources the target refused on the live path: each
	// is owed together with the ancestors it names.
	refused scopeSet
}

func wholeCatalogue(why string) owedWork { return owedWork{all: true, why: why} }

func subtreeOf(node string) owedWork { return owedWork{nodes: map[string]bool{node: true}} }

func refusedResource(topic, id string) owedWork {
	return owedWork{refused: scopeSet{topic: {id: true}}}
}

func (o *owedWork) empty() bool {
	return !o.all && len(o.nodes) == 0 && len(o.refused) == 0
}

// add folds more work in.
func (o *owedWork) add(more owedWork) {
	if more.all {
		o.all = true
		if o.why == "" {
			o.why = more.why
		}
	}
	for id := range more.nodes {
		if o.nodes == nil {
			o.nodes = map[string]bool{}
		}
		o.nodes[id] = true
	}
	for topic, ids := range more.refused {
		for id := range ids {
			if o.refused == nil {
				o.refused = scopeSet{}
			}
			o.refused.add(topic, id)
		}
	}
}

// scopeSet is a set of resources, by collection.
type scopeSet map[string]map[string]bool

func (s scopeSet) has(topic, id string) bool { return s[topic][id] }

func (s scopeSet) add(topic, id string) {
	if s[topic] == nil {
		s[topic] = map[string]bool{}
	}
	s[topic][id] = true
}

func (s scopeSet) size() int {
	n := 0
	for _, ids := range s {
		n += len(ids)
	}
	return n
}

// parentRef names one parent of a resource: the collection it lives in
// and its id.
type parentRef struct {
	topic string
	id    string
}

// parentsOf reads the parents a resource names (IS-04 referential
// integrity). A sender's flow_id is nullable and a v1.0 flow carries no
// device_id: an absent or null reference names nothing.
func parentsOf(topic string, doc json.RawMessage) []parentRef {
	var v struct {
		NodeID   string `json:"node_id"`
		DeviceID string `json:"device_id"`
		SourceID string `json:"source_id"`
		FlowID   string `json:"flow_id"`
	}
	_ = json.Unmarshal(doc, &v) // a field of the wrong type names nothing
	var out []parentRef
	add := func(parentTopic, id string) {
		if id != "" {
			out = append(out, parentRef{topic: parentTopic, id: id})
		}
	}
	switch topic {
	case "devices":
		add("nodes", v.NodeID)
	case "sources", "receivers":
		add("devices", v.DeviceID)
	case "flows":
		add("sources", v.SourceID)
		add("devices", v.DeviceID)
	case "senders":
		add("devices", v.DeviceID)
		add("flows", v.FlowID)
	}
	return out
}

// catalogue is a view of the cache a pass is planned on: the documents
// by collection and id. Documents are replaced, never edited, so the
// view shares them.
type catalogue map[string]map[string]json.RawMessage

// chain adds a resource and every ancestor it names to want. False when
// one of them is not in the catalogue — only the source can say what
// it is, so the pass cannot be bounded.
func (c catalogue) chain(topic, id string, want scopeSet) bool {
	doc, cached := c[topic][id]
	if !cached {
		return false
	}
	if want.has(topic, id) {
		return true
	}
	want.add(topic, id)
	for _, p := range parentsOf(topic, doc) {
		if !c.chain(p.topic, p.id, want) {
			return false
		}
	}
	return true
}

// subtree adds the given nodes and everything under them to want.
// Collections are walked in dependency order, so a parent's membership
// is settled before its children are looked at.
func (c catalogue) subtree(nodes map[string]bool, want scopeSet) {
	for id := range nodes {
		if _, cached := c["nodes"][id]; cached {
			want.add("nodes", id)
		}
	}
	if len(want["nodes"]) == 0 {
		return // the nodes left the source since: nothing to repair
	}
	for _, topic := range mirrorTopics[1:] {
		for id, doc := range c[topic] {
			for _, p := range parentsOf(topic, doc) {
				if want.has(p.topic, p.id) {
					want.add(topic, id)
					break
				}
			}
		}
	}
}

// nodeOf names the node a resource belongs to, following the parents it
// names. False when the chain breaks before a node.
func (c catalogue) nodeOf(topic, id string) (string, bool) {
	if topic == "nodes" {
		return id, true
	}
	doc, cached := c[topic][id]
	if !cached {
		return "", false
	}
	for _, p := range parentsOf(topic, doc) {
		if node, ok := c.nodeOf(p.topic, p.id); ok {
			return node, true
		}
	}
	return "", false
}

// scope turns owed work into the set of resources to send. bounded is
// false when the work cannot be scoped from the catalogue.
func (c catalogue) scope(work owedWork) (want scopeSet, bounded bool) {
	want = scopeSet{}
	c.subtree(work.nodes, want)
	for topic, ids := range work.refused {
		for id := range ids {
			if _, cached := c[topic][id]; !cached {
				continue // gone from the source since: nothing to repair
			}
			if !c.chain(topic, id, want) {
				return nil, false
			}
		}
	}
	return want, true
}

// snapshot copies the cache's index — not the documents — so a pass is
// planned without holding the lock while a plant's worth of JSON is
// parsed.
func (m *Mirror) snapshot() (catalogue, map[string]map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	docs := make(catalogue, len(mirrorTopics))
	vers := make(map[string]map[string]string, len(mirrorTopics))
	for _, topic := range mirrorTopics {
		docs[topic] = make(map[string]json.RawMessage, len(m.cache[topic]))
		vers[topic] = make(map[string]string, len(m.cache[topic]))
		for id, doc := range m.cache[topic] {
			docs[topic][id] = doc
			vers[topic][id] = m.cacheVer[topic][id]
		}
	}
	return docs, vers
}

// owe records work for the next pass. It does not start one.
func (m *Mirror) owe(work owedWork) {
	m.mu.Lock()
	m.owed.add(work)
	m.mu.Unlock()
}

// runOwed runs passes until nothing is owed. One runner at a time: a
// second caller returns at once, its work already recorded for the
// runner to find. Two passes interleaved would send a child from one
// before its parent from the other, and a caller made to wait would be
// the heartbeat loop.
func (m *Mirror) runOwed(ctx context.Context) {
	m.mu.Lock()
	if m.filling {
		m.mu.Unlock()
		return
	}
	m.filling = true
	m.mu.Unlock()
	for {
		m.mu.Lock()
		work := m.owed
		m.owed = owedWork{}
		if work.empty() || ctx.Err() != nil {
			m.filling = false
			m.mu.Unlock()
			return
		}
		m.mu.Unlock()
		m.pass(ctx, work)
	}
}

// fill brings the whole target level with the source: Run uses it for
// the first fill ("initial_fill"), resync for a repair that cannot be
// bounded; why names the pass in the audit trail.
func (m *Mirror) fill(ctx context.Context, why string) {
	m.owe(wholeCatalogue(why))
	m.runOwed(ctx)
}

// resync is the repair of the whole catalogue. It is counted — a
// mirror that keeps repairing is telling the operator something.
func (m *Mirror) resync(ctx context.Context) {
	m.noteResync()
	m.fill(ctx, "resync")
}

// noteResync counts one repair asked for, whatever its extent.
func (m *Mirror) noteResync() {
	m.mu.Lock()
	m.stats.Resyncs++
	m.mu.Unlock()
}

// scheduleRepair holds work back until a burst of refusals has gone
// quiet for mirrorResyncDebounce, then runs it as one pass. The six
// collections stream concurrently: the wait is what lets a late parent
// reach the cache, so the refused child can be sent with it.
func (m *Mirror) scheduleRepair(work owedWork) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runCtx == nil {
		return
	}
	m.pending.add(work)
	if m.resyncTimer != nil {
		m.resyncTimer.Reset(mirrorResyncDebounce)
		return
	}
	m.resyncTimer = time.AfterFunc(mirrorResyncDebounce, func() {
		m.mu.Lock()
		m.resyncTimer = nil
		rctx := m.runCtx
		m.owed.add(m.pending)
		m.pending = owedWork{}
		m.mu.Unlock()
		if rctx != nil && rctx.Err() == nil {
			m.noteResync()
			m.runOwed(rctx)
		}
	})
}

// scheduleResync is the debounced repair of the whole catalogue.
func (m *Mirror) scheduleResync() { m.scheduleRepair(wholeCatalogue("resync")) }

// scheduleSweptRepair follows a version-conflict reconciliation: the
// target's copy of a resource was deleted at the minor it held, and a
// delete cascades — whatever hung under it on the target is gone. The
// node the resource belongs to is owed its subtree; a resource whose
// node cannot be named from the cache owes the whole catalogue.
func (m *Mirror) scheduleSweptRepair(topic, id string) {
	docs, _ := m.snapshot()
	node, ok := docs.nodeOf(topic, id)
	if !ok {
		m.scheduleResync()
		return
	}
	m.scheduleRepair(subtreeOf(node))
}

// pass sends what one piece of owed work calls for, in dependency
// order, each resource at its registered minor.
func (m *Mirror) pass(ctx context.Context, work owedWork) {
	docs, vers := m.snapshot()
	var want scopeSet // nil = everything
	if !work.all {
		var bounded bool
		if want, bounded = docs.scope(work); !bounded {
			work = wholeCatalogue("resync")
		}
	}
	switch {
	case work.all:
		want = nil
		m.audit.event(work.why, nil)
		m.refreshCacheFromSource(ctx)
		docs, vers = m.snapshot()
	case want.size() == 0:
		return
	default:
		m.audit.event("resync_scoped", map[string]any{
			"resources": want.size(), "evicted_nodes": len(work.nodes), "refused": work.refused.size(),
		})
	}

	refused := map[string]bool{} // ids the target said no to in this pass
	skipped := 0
	for _, topic := range mirrorTopics {
		ids := make([]string, 0, len(docs[topic]))
		for id := range docs[topic] {
			if want == nil || want.has(topic, id) {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids) // deterministic order for tests + logs
		for _, id := range ids {
			if ctx.Err() != nil {
				return
			}
			doc := docs[topic][id]
			if underRefused(parentsOf(topic, doc), refused) {
				refused[id] = true // and so are its own children
				skipped++
				continue
			}
			// allowResync=false: this pass is already in dependency
			// order, so a refusal here is a genuine one, not a race.
			if m.postResource(ctx, topic, vers[topic][id], id, doc, false) == postRefused {
				refused[id] = true
			}
		}
	}
	if skipped > 0 {
		m.logger.Warn("registry/mirror: children not sent — the target refused their parent", "skipped", skipped)
		m.audit.event("children_skipped", map[string]any{"skipped": skipped})
		m.mu.Lock()
		m.stats.Skipped += uint64(skipped)
		m.mu.Unlock()
	}
	if work.all {
		// The served face is fed from the same authoritative cache, so
		// a whole pass repopulates it too.
		m.serveReplay()
	}
}

// underRefused reports whether any parent was refused in this pass.
func underRefused(parents []parentRef, refused map[string]bool) bool {
	for _, p := range parents {
		if refused[p.id] {
			return true
		}
	}
	return false
}
