package registry

// A repair sends what the target lost and nothing else (#1311): the
// subtree of an evicted node, a refused child behind the ancestors it
// names, the whole catalogue only when the work cannot be bounded. And
// a pass does not send the children of a parent the target has just
// refused.

import (
	"context"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is04"
)

// plantDocs is a two-node catalogue with every kind of parent link:
// f2 is a v1.0 flow (it names its source only) and x2 a sender with no
// flow.
func plantDocs() map[string]map[string]string {
	return map[string]map[string]string{
		"nodes":     {"n1": `{"id":"n1"}`, "n2": `{"id":"n2"}`},
		"devices":   {"d1": `{"id":"d1","node_id":"n1"}`, "d2": `{"id":"d2","node_id":"n2"}`},
		"sources":   {"s1": `{"id":"s1","device_id":"d1"}`, "s2": `{"id":"s2","device_id":"d2"}`},
		"flows":     {"f1": `{"id":"f1","source_id":"s1","device_id":"d1"}`, "f2": `{"id":"f2","source_id":"s2"}`},
		"senders":   {"x1": `{"id":"x1","device_id":"d1","flow_id":"f1"}`, "x2": `{"id":"x2","device_id":"d2","flow_id":null}`},
		"receivers": {"r1": `{"id":"r1","device_id":"d1"}`, "r2": `{"id":"r2","device_id":"d2"}`},
	}
}

func asCatalogue(docs map[string]map[string]string) catalogue {
	c := catalogue{}
	for topic, byID := range docs {
		c[topic] = map[string]json.RawMessage{}
		for id, doc := range byID {
			c[topic][id] = json.RawMessage(doc)
		}
	}
	return c
}

// load puts docs in the mirror's cache, all registered at v1.3.
func load(m *Mirror, docs map[string]map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for topic, byID := range docs {
		for id, doc := range byID {
			m.cache[topic][id] = json.RawMessage(doc)
			m.cacheVer[topic][id] = "v1.3"
		}
	}
}

// flat lists a scope as sorted "topic/id" strings.
func flat(s scopeSet) []string {
	var out []string
	for _, topic := range mirrorTopics {
		for id := range s[topic] {
			out = append(out, topic+"/"+id)
		}
	}
	sortStrings(out)
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func TestParentsOf(t *testing.T) {
	cases := []struct {
		topic, doc string
		want       []parentRef
	}{
		{"nodes", `{"id":"n1"}`, nil},
		{"devices", `{"id":"d1","node_id":"n1"}`, []parentRef{{"nodes", "n1"}}},
		{"sources", `{"id":"s1","device_id":"d1"}`, []parentRef{{"devices", "d1"}}},
		{"receivers", `{"id":"r1","device_id":"d1"}`, []parentRef{{"devices", "d1"}}},
		{"flows", `{"id":"f1","source_id":"s1","device_id":"d1"}`, []parentRef{{"sources", "s1"}, {"devices", "d1"}}},
		{"flows", `{"id":"f2","source_id":"s2"}`, []parentRef{{"sources", "s2"}}},
		{"senders", `{"id":"x1","device_id":"d1","flow_id":"f1"}`, []parentRef{{"devices", "d1"}, {"flows", "f1"}}},
		{"senders", `{"id":"x2","device_id":"d2","flow_id":null}`, []parentRef{{"devices", "d2"}}},
		{"devices", `not json`, nil},
	}
	for _, tc := range cases {
		if got := parentsOf(tc.topic, json.RawMessage(tc.doc)); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("parentsOf(%s, %s) = %v, want %v", tc.topic, tc.doc, got, tc.want)
		}
	}
}

func TestCatalogueSubtree(t *testing.T) {
	c := asCatalogue(plantDocs())

	want := scopeSet{}
	c.subtree(map[string]bool{"n1": true}, want)
	if got, exp := flat(want), []string{"devices/d1", "flows/f1", "nodes/n1", "receivers/r1", "senders/x1", "sources/s1"}; !reflect.DeepEqual(got, exp) {
		t.Errorf("subtree(n1) = %v, want %v", got, exp)
	}

	// The v1.0 flow hangs under its source, the flowless sender under
	// its device.
	want = scopeSet{}
	c.subtree(map[string]bool{"n2": true}, want)
	if got, exp := flat(want), []string{"devices/d2", "flows/f2", "nodes/n2", "receivers/r2", "senders/x2", "sources/s2"}; !reflect.DeepEqual(got, exp) {
		t.Errorf("subtree(n2) = %v, want %v", got, exp)
	}

	// A node that left the source has nothing to repair.
	want = scopeSet{}
	c.subtree(map[string]bool{"gone": true}, want)
	if want.size() != 0 {
		t.Errorf("subtree of an unknown node = %v, want nothing", flat(want))
	}
}

func TestCatalogueChain(t *testing.T) {
	c := asCatalogue(plantDocs())

	want := scopeSet{}
	if !c.chain("senders", "x1", want) {
		t.Fatal("a sender whose ancestors are all cached must be bounded")
	}
	if got, exp := flat(want), []string{"devices/d1", "flows/f1", "nodes/n1", "senders/x1", "sources/s1"}; !reflect.DeepEqual(got, exp) {
		t.Errorf("chain(x1) = %v, want %v", got, exp)
	}

	if c.chain("senders", "nope", scopeSet{}) {
		t.Error("a resource that is not cached has no chain")
	}
	delete(c["devices"], "d1")
	if c.chain("senders", "x1", scopeSet{}) {
		t.Error("a chain with a missing ancestor cannot be bounded")
	}
}

func TestCatalogueNodeOf(t *testing.T) {
	c := asCatalogue(plantDocs())
	c["devices"]["stray"] = json.RawMessage(`{"id":"stray"}`) // names no node

	for _, tc := range []struct {
		topic, id, node string
		ok              bool
	}{
		{"nodes", "n1", "n1", true},
		{"senders", "x1", "n1", true},
		{"flows", "f2", "n2", true}, // through its source
		{"senders", "nope", "", false},
		{"devices", "stray", "", false},
	} {
		node, ok := c.nodeOf(tc.topic, tc.id)
		if node != tc.node || ok != tc.ok {
			t.Errorf("nodeOf(%s/%s) = %q, %v — want %q, %v", tc.topic, tc.id, node, ok, tc.node, tc.ok)
		}
	}
}

func TestCatalogueScope(t *testing.T) {
	c := asCatalogue(plantDocs())

	// An evicted node and a refused child of the other node, together.
	work := subtreeOf("n1")
	work.add(refusedResource("flows", "f2"))
	work.add(refusedResource("flows", "left-the-source"))
	want, bounded := c.scope(work)
	exp := []string{"devices/d1", "devices/d2", "flows/f1", "flows/f2", "nodes/n1", "nodes/n2", "receivers/r1", "senders/x1", "sources/s1", "sources/s2"}
	if got := flat(want); !bounded || !reflect.DeepEqual(got, exp) {
		t.Errorf("scope = %v (bounded %v), want %v", got, bounded, exp)
	}

	// A refused child whose parent the mirror has never seen.
	delete(c["sources"], "s2")
	if _, bounded := c.scope(refusedResource("flows", "f2")); bounded {
		t.Error("a refused child with an unknown ancestor must not be bounded")
	}
}

func TestOwedWorkFoldsTogether(t *testing.T) {
	var o owedWork
	if !o.empty() {
		t.Fatal("no work is empty work")
	}
	o.add(subtreeOf("n1"))
	o.add(subtreeOf("n2"))
	o.add(refusedResource("flows", "f1"))
	o.add(wholeCatalogue("initial_fill"))
	o.add(wholeCatalogue("resync"))
	if o.empty() || !o.all || o.why != "initial_fill" || len(o.nodes) != 2 || o.refused.size() != 1 {
		t.Errorf("folded work = %+v", o)
	}
}

// orderTarget records every POSTed resource as "type:id", in arrival
// order, and lets a test choose the answer.
type orderTarget struct {
	mu     sync.Mutex
	posts  []string
	answer func(key string, nth int) int // nth = how many times this key has been POSTed, from 1
	seen   map[string]int
	health func(node string, nth int) int
	beats  map[string]int
}

func (o *orderTarget) handler() stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		o.mu.Lock()
		defer o.mu.Unlock()
		if strings.Contains(r.URL.Path, "/health/nodes/") {
			id := r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:]
			if o.beats == nil {
				o.beats = map[string]int{}
			}
			o.beats[id]++
			status := stdhttp.StatusOK
			if o.health != nil {
				status = o.health(id, o.beats[id])
			}
			w.WriteHeader(status)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var env struct {
			Type string `json:"type"`
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		_ = json.Unmarshal(body, &env)
		key := env.Type + ":" + env.Data.ID
		if o.seen == nil {
			o.seen = map[string]int{}
		}
		o.seen[key]++
		o.posts = append(o.posts, key)
		status := stdhttp.StatusCreated
		if o.answer != nil {
			status = o.answer(key, o.seen[key])
		}
		w.WriteHeader(status)
	})
}

func (o *orderTarget) sent() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.posts...)
}

// repairMirror is a mirror holding plantDocs, pointed at an orderTarget.
func repairMirror(t *testing.T, target *orderTarget) (*Mirror, *registryLogTap) {
	t.Helper()
	tsrv := httptest.NewServer(target.handler())
	t.Cleanup(tsrv.Close)
	m := mirrorTo(t, tsrv.URL)
	tap := newRegistryLogTap()
	m.logger = tap.logger()
	load(m, plantDocs())
	return m, tap
}

// The target evicts one node. Only that node and what hangs under it
// are sent again — not the other node, which the target still holds.
func TestMirrorRepairsOnlyTheEvictedNodesSubtree(t *testing.T) {
	target := &orderTarget{health: func(node string, nth int) int {
		if node == "n1" && nth == 1 {
			return stdhttp.StatusNotFound
		}
		return stdhttp.StatusOK
	}}
	m, _ := repairMirror(t, target)
	m.heartbeatEvery = 20 * time.Millisecond
	m.mu.Lock()
	m.targetNodes["n1"], m.targetNodes["n2"] = true, true
	m.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.heartbeatLoop(ctx)
		close(done)
	}()
	waitFor(t, 5*time.Second, func() bool { return len(target.sent()) >= 6 }, "the evicted node's subtree to be sent again")
	time.Sleep(100 * time.Millisecond) // anything more would show up now
	st := m.Stats()                    // read before the stop: a heartbeat cut short by it counts as a failure
	cancel()
	<-done

	exp := []string{"node:n1", "device:d1", "source:s1", "flow:f1", "sender:x1", "receiver:r1"}
	if got := target.sent(); !reflect.DeepEqual(got, exp) {
		t.Errorf("repair sent %v, want exactly %v", got, exp)
	}
	if st.Resyncs != 1 || st.Failures != 0 {
		t.Errorf("stats = %+v, want one repair and no failure", st)
	}
}

// A flow whose parents the target accepted earlier, and has lost since,
// is refused on the live path. The repair sends it again behind the
// ancestors it names — four POSTs, not the catalogue.
func TestMirrorResendsARefusedChildBehindItsAncestors(t *testing.T) {
	target := &orderTarget{answer: func(key string, nth int) int {
		if key == "flow:f1" && nth == 1 {
			return stdhttp.StatusBadRequest
		}
		return stdhttp.StatusCreated
	}}
	m, _ := repairMirror(t, target)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.mu.Lock()
	m.runCtx = ctx
	m.landed["devices"]["d1"], m.landed["sources"]["s1"] = true, true
	doc := m.cache["flows"]["f1"]
	delete(m.cache["flows"], "f1") // it arrives now, on the live path
	m.mu.Unlock()

	m.forwardRow(ctx, "flows", "v1.3", is04.GrainDataRow{Path: "f1", Post: doc})
	waitFor(t, 5*time.Second, func() bool { return len(target.sent()) >= 5 }, "the refused flow to be sent again")
	time.Sleep(100 * time.Millisecond)

	exp := []string{"flow:f1", "node:n1", "device:d1", "source:s1", "flow:f1"}
	if got := target.sent(); !reflect.DeepEqual(got, exp) {
		t.Errorf("target saw %v, want the refusal then exactly the flow's chain: %v", got, exp)
	}
	if st := m.Stats(); st.Resyncs != 1 || st.Failures != 1 {
		t.Errorf("stats = %+v, want one repair for the one refusal", st)
	}
}

// A flow that arrives ahead of its parents on the live path — the six
// topics stream concurrently — is not sent ahead of them to be refused:
// it goes with the ordered pass, behind the ancestors it names. Once the
// target holds its parents, a change to it goes at once.
func TestMirrorHoldsALiveChildUntilTheTargetHoldsItsParents(t *testing.T) {
	target := &orderTarget{}
	m, _ := repairMirror(t, target)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.mu.Lock()
	m.runCtx = ctx
	doc := m.cache["flows"]["f1"]
	delete(m.cache["flows"], "f1") // it arrives now, on the live path
	m.mu.Unlock()

	m.forwardRow(ctx, "flows", "v1.3", is04.GrainDataRow{Path: "f1", Post: doc})
	if got := target.sent(); len(got) != 0 {
		t.Fatalf("the flow went ahead of its parents: %v", got)
	}
	waitFor(t, 5*time.Second, func() bool { return len(target.sent()) >= 4 }, "the held flow to be sent behind its ancestors")
	time.Sleep(100 * time.Millisecond)
	exp := []string{"node:n1", "device:d1", "source:s1", "flow:f1"}
	if got := target.sent(); !reflect.DeepEqual(got, exp) {
		t.Errorf("target saw %v, want exactly the flow's chain, in order: %v", got, exp)
	}
	if st := m.Stats(); st.Resyncs != 1 || st.Failures != 0 {
		t.Errorf("stats = %+v, want one ordered pass and nothing refused", st)
	}

	// Its parents have landed now: a change to it is forwarded at once.
	changed := json.RawMessage(strings.Replace(string(doc), "{", `{"label":"renamed",`, 1))
	m.forwardRow(ctx, "flows", "v1.3", is04.GrainDataRow{Path: "f1", Pre: doc, Post: changed})
	if got := target.sent(); len(got) != 5 || got[4] != "flow:f1" {
		t.Errorf("a change behind landed parents: target saw %v", got)
	}

	// Removed, it is no longer something the target holds.
	m.forwardRow(ctx, "flows", "v1.3", is04.GrainDataRow{Path: "f1", Pre: changed})
	m.mu.Lock()
	still := m.landed["flows"]["f1"]
	m.mu.Unlock()
	if still {
		t.Error("a removed flow is still marked as held by the target")
	}
}

// A refused child whose parent the mirror has never seen cannot be
// bounded: the whole catalogue goes, as it always did.
func TestMirrorRepairsTheWholeCatalogueWhenAnAncestorIsUnknown(t *testing.T) {
	target := &orderTarget{}
	tsrv := httptest.NewServer(target.handler())
	defer tsrv.Close()
	m := mirrorTo(t, tsrv.URL)
	m.logger = newRegistryLogTap().logger()
	load(m, map[string]map[string]string{
		"nodes": {"n2": `{"id":"n2"}`},
		"flows": {"f1": `{"id":"f1","source_id":"never-seen"}`},
	})

	m.owe(refusedResource("flows", "f1"))
	m.runOwed(context.Background())

	if got, exp := target.sent(), []string{"node:n2", "flow:f1"}; !reflect.DeepEqual(got, exp) {
		t.Errorf("target saw %v, want the whole cache %v", got, exp)
	}
}

// The target refuses a device. Nothing that hangs under it is sent —
// the answer is known — and the rest of the catalogue goes through.
func TestMirrorSkipsTheChildrenOfARefusedParent(t *testing.T) {
	target := &orderTarget{answer: func(key string, _ int) int {
		if key == "device:d1" {
			return stdhttp.StatusBadRequest
		}
		return stdhttp.StatusCreated
	}}
	m, tap := repairMirror(t, target)

	m.resync(context.Background())

	exp := []string{"node:n1", "node:n2", "device:d1", "device:d2", "source:s2", "flow:f2", "sender:x2", "receiver:r2"}
	if got := target.sent(); !reflect.DeepEqual(got, exp) {
		t.Errorf("target saw %v, want %v", got, exp)
	}
	if st := m.Stats(); st.Skipped != 4 || st.Failures != 1 {
		t.Errorf("stats = %+v, want 4 skipped (source, flow, sender, receiver of d1) and the one refusal", st)
	}
	if !tap.has("children not sent") {
		t.Errorf("the skip must be said once; saw %v", tap.snapshot())
	}
}

// A server error says nothing about what the target holds: the
// children are still sent.
func TestMirrorSendsTheChildrenWhenTheParentsFateIsUnknown(t *testing.T) {
	target := &orderTarget{answer: func(key string, _ int) int {
		if key == "device:d1" {
			return stdhttp.StatusInternalServerError
		}
		return stdhttp.StatusCreated
	}}
	m, _ := repairMirror(t, target)

	m.resync(context.Background())

	if got := len(target.sent()); got != 12 {
		t.Errorf("%d POSTs, want all 12", got)
	}
	if st := m.Stats(); st.Skipped != 0 {
		t.Errorf("skipped = %d, want none", st.Skipped)
	}
}

// An evicted node that has left the source since leaves nothing to
// repair.
func TestMirrorScopedPassWithNothingLeftSendsNothing(t *testing.T) {
	target := &orderTarget{}
	m, _ := repairMirror(t, target)

	m.owe(subtreeOf("left-the-source"))
	m.runOwed(context.Background())

	if got := target.sent(); len(got) != 0 {
		t.Errorf("target saw %v, want nothing", got)
	}
}

// A pass stops between two resources when the mirror does.
func TestMirrorPassStopsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	target := &orderTarget{answer: func(string, int) int {
		cancel()
		return stdhttp.StatusCreated
	}}
	m, _ := repairMirror(t, target)

	m.resync(ctx)

	if got := target.sent(); len(got) != 1 {
		t.Errorf("target saw %v after the stop, want the one request in flight", got)
	}
}

// Reconciling a version conflict deletes the target's copy, and a
// delete cascades: the node the resource belongs to is owed its
// subtree. A resource whose node cannot be named owes the catalogue.
func TestMirrorOwesTheSweptSubtreeAfterAConflict(t *testing.T) {
	m, _ := repairMirror(t, &orderTarget{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.mu.Lock()
	m.runCtx = ctx
	m.mu.Unlock()
	pending := func() owedWork {
		m.mu.Lock()
		defer m.mu.Unlock()
		p := m.pending
		if m.resyncTimer != nil {
			m.resyncTimer.Stop() // the test reads what is owed; it does not run it
			m.resyncTimer = nil
		}
		m.pending = owedWork{}
		return p
	}

	m.scheduleSweptRepair("senders", "x1")
	if p := pending(); p.all || !p.nodes["n1"] || len(p.nodes) != 1 {
		t.Errorf("after a conflict on x1 the mirror owes %+v, want the subtree of n1", p)
	}

	m.scheduleSweptRepair("senders", "never-seen")
	if p := pending(); !p.all {
		t.Errorf("after a conflict on an unknown resource the mirror owes %+v, want the whole catalogue", p)
	}
}
