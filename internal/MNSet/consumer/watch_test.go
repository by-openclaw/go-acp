package mnset

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/plugin"
)

// A watch on a Riedel module is a poll: the dictionary's plan says what
// and how often, the neutral monitor runs it, changes become events.
// Tests swap in a plan with millisecond intervals.

const fastPlan = `{"model":"FusioN6","entries":[],"sibling_thresholds":[],
 "poll":{"defaults":{"interval":"40ms"},"oids":[
   {"match":"**.network.pkt_cnt","interval":"40ms"},
   {"match":"refclk.**","interval":"40ms"}]}}`

func withPlan(t *testing.T, plan string) {
	t.Helper()
	saved := fusion6Dictionary
	fusion6Dictionary = []byte(plan)
	t.Cleanup(func() { fusion6Dictionary = saved })
}

func collect() (consumer.EventFunc, func() []consumer.Event) {
	var mu sync.Mutex
	var got []consumer.Event
	return func(ev consumer.Event) { mu.Lock(); got = append(got, ev); mu.Unlock() },
		func() []consumer.Event { mu.Lock(); defer mu.Unlock(); return append([]consumer.Event(nil), got...) }
}

func waitFor(t *testing.T, d time.Duration, ok func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if ok() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return ok()
}

func TestWatchPollsAndReportsChanges(t *testing.T) {
	withPlan(t, fastPlan)
	m := newModule(t)
	m.docs["flows/fee338d3"] = `{"id":"fee338d3","name":"rx","network":{"dst_ip_addr":"239.0.1.2","pkt_cnt":1}}`
	p := connected(t, m)
	fn, got := collect()
	req := consumer.ValueRequest{Slot: 0, Path: "flows.fee338d3"}
	if err := p.Subscribe(req, fn); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if !waitFor(t, 3*time.Second, func() bool { return len(got()) > 0 }) {
		t.Fatal("no first sample")
	}
	// The counter moves; the document cache expires within docTTL, then
	// the next poll sees it.
	m.setDoc("flows/fee338d3", `{"id":"fee338d3","name":"rx","network":{"dst_ip_addr":"239.0.1.2","pkt_cnt":2}}`)
	if !waitFor(t, docTTL+3*time.Second, func() bool {
		for _, ev := range got() {
			if ev.Path == "flows.fee338d3.network.pkt_cnt" && ev.Value.Int == 2 {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("change not reported: %+v", got())
	}
	for _, ev := range got() {
		if ev.Path == "flows.fee338d3.network.dst_ip_addr" {
			t.Errorf("a leaf outside the plan must not be polled: %+v", ev)
		}
	}
	if err := p.Unsubscribe(req); err != nil {
		t.Errorf("Unsubscribe: %v", err)
	}
}

func TestWatchErrors(t *testing.T) {
	withPlan(t, fastPlan)
	p := (&Factory{}).New(plugin.Deps{}).(*Plugin)
	if err := p.Subscribe(consumer.ValueRequest{}, func(consumer.Event) {}); !errors.Is(err, consumer.ErrNotConnected) {
		t.Errorf("not connected err = %v", err)
	}
	m := newModule(t)
	p = connected(t, m)
	// nothing in the plan under that path
	if err := p.Subscribe(consumer.ValueRequest{Slot: 0, Path: "self.ipconfig"}, func(consumer.Event) {}); err == nil || !strings.Contains(err.Error(), "nothing to poll under") {
		t.Errorf("empty scope err = %v", err)
	}
	// slot that does not exist
	if err := p.Subscribe(consumer.ValueRequest{Slot: 7}, func(consumer.Event) {}); !errors.Is(err, consumer.ErrObjectNotFound) {
		t.Errorf("bad slot err = %v", err)
	}
	// broken dictionary
	withPlan(t, "{")
	if err := p.Subscribe(consumer.ValueRequest{Slot: 0}, func(consumer.Event) {}); err == nil || !strings.Contains(err.Error(), "embedded dictionary") {
		t.Errorf("broken dictionary err = %v", err)
	}
	withPlan(t, `{"model":"F","entries":[],"sibling_thresholds":[],"poll":{"defaults":{"interval":"soon"}}}`)
	if err := p.Subscribe(consumer.ValueRequest{Slot: 0}, func(consumer.Event) {}); err == nil || !strings.Contains(err.Error(), "poll default interval") {
		t.Errorf("bad default err = %v", err)
	}
	withPlan(t, `{"model":"F","entries":[],"sibling_thresholds":[],"poll":{"defaults":{"interval":"1s"},"oids":[{"match":"refclk.**","interval":"x"}]}}`)
	if err := p.Subscribe(consumer.ValueRequest{Slot: 0}, func(consumer.Event) {}); err == nil || !strings.Contains(err.Error(), "poll interval for refclk") {
		t.Errorf("bad entry interval err = %v", err)
	}
}

func TestWatchAllSlotsOfAFrameAndFrameRefresh(t *testing.T) {
	withPlan(t, fastPlan)
	frameRefresh = 30 * time.Millisecond
	t.Cleanup(func() { frameRefresh = 10 * time.Second }) // after Disconnect joined the loop
	mod := newModule(t)
	modHost, _ := mod.hostPort(t)
	mn := newMNSet(t)
	// OFFLINE 00:1b…, ONLINE 40:a3:6b:a2…, ONLINE 40:a3:6b:ff…; the offline
	// one sits on 127.0.0.2, where nothing listens on the module port, so
	// when it comes ONLINE its probe is refused at once — no timeout to
	// tune, no stall under -race.
	silent := func(list string) string { return strings.Replace(list, "10.6.40.99/24", "127.0.0.2/24", 1) }
	mn.devices = silent(deviceList(modHost))
	p := frameConnected(t, mod, mn, 0)
	// The silent module's probe must fail INSIDE this test's budget on
	// every platform. Linux routes the whole 127.0.0.0/8, so a connect
	// to 127.0.0.2 is refused at once; macOS has only 127.0.0.1 unless
	// an alias is added, so the same connect hangs until the client
	// timeout — 8s by default, against the waits below. That is a
	// property of the host's loopback, not of this connector, so the
	// test bounds the probe instead of assuming the network refuses it.
	//
	// One second, not less: this timeout applies to EVERY request the
	// plugin makes, the MN SET device list and the healthy modules
	// included. At 300ms a loaded macOS runner missed those too and
	// the watch had no present slot to build a profile from — the
	// bound has to be short against the waits and long against a
	// local HTTP round trip, and 1s against 6s is both.
	p.SetTimeout(time.Second)
	fn, got := collect()
	req := consumer.ValueRequest{Slot: -1}
	if err := p.Subscribe(req, fn); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := p.Subscribe(req, fn); err == nil {
		t.Error("double subscribe must be refused")
	}
	// A second frame-wide scope shares the one refresh loop.
	if err := p.Subscribe(consumer.ValueRequest{Slot: 1}, fn); err != nil {
		t.Fatalf("second scope: %v", err)
	}
	t.Cleanup(func() { _ = p.Unsubscribe(consumer.ValueRequest{Slot: 1}) })
	// values from both present slots
	if !waitFor(t, 6*time.Second, func() bool {
		s := map[int]bool{}
		for _, ev := range got() {
			if ev.Path != "slot" {
				s[ev.Slot] = true
			}
		}
		return s[1] && s[2]
	}) {
		t.Fatalf("expected samples from slots 1 and 2: %+v", got())
	}
	// MN SET's list changes: the offline module comes ONLINE (but is silent
	// → error), a module vanishes, a new one appears.
	next := strings.Replace(silent(deviceList(modHost)), `"id":"00:1b:c5:00:00:01","status":"OFFLINE"`, `"id":"00:1b:c5:00:00:01","status":"ONLINE"`, 1)
	next = strings.Replace(next, `"id":"40:a3:6b:ff:ff:ff"`, `"id":"40:a3:6b:ff:ff:fe"`, 1)
	mn.set(next, 0)
	if !waitFor(t, 6*time.Second, func() bool {
		states := map[string]string{}
		for _, ev := range got() {
			if ev.Path == "slot" {
				states[ev.Label] = ev.Value.Str
			}
		}
		return states["00:1b:c5:00:00:01"] == "error" && states["40:a3:6b:ff:ff:ff"] == "removed" && states["40:a3:6b:ff:ff:fe"] == "present"
	}) {
		t.Fatalf("frame refresh events missing: %+v", got())
	}
	// MN SET unreachable: the refresh warns and keeps the old table.
	mn.set(next, 500)
	host, port := mn.hostPort(t)
	p.refreshFrame(context.Background(), host, port, fn)
	if n, _ := p.session(); n != 3 {
		t.Errorf("slot table must survive a failed refresh, got %d", n)
	}
	if err := p.Unsubscribe(req); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	still := p.frameStop != nil
	p.mu.Unlock()
	if !still {
		t.Error("frame refresh must survive while another scope is subscribed")
	}
	if err := p.Unsubscribe(consumer.ValueRequest{Slot: 1}); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	stopped := p.frameStop == nil
	p.mu.Unlock()
	if !stopped {
		t.Error("frame refresh must stop with the last subscription")
	}
	// Disconnect with an active frame refresh stops it too.
	//
	// Subscribing needs a present slot to build a poll profile from,
	// and at this point in the test a refresh may still be in flight —
	// the silent module at an unroutable address holds one up for the
	// client timeout on a host that does not refuse the connect. That
	// is a race with the test, not a property of Disconnect, which is
	// what this part actually checks. So wait for the subscription to
	// become possible rather than requiring it to be possible already.
	var serr error
	if !waitFor(t, 6*time.Second, func() bool {
		serr = p.Subscribe(req, fn)
		return serr == nil
	}) {
		t.Fatalf("no slot became watchable: %v", serr)
	}
	_ = p.Disconnect()
	p.mu.Lock()
	stopped = p.frameStop == nil
	p.mu.Unlock()
	if !stopped || p.poller.Active() != 0 {
		t.Error("Disconnect must stop the frame refresh and every poll")
	}
}

func TestWatchAFrameWithNoPresentSlot(t *testing.T) {
	withPlan(t, fastPlan)
	mod := newModule(t)
	mn := newMNSet(t)
	mn.devices = `[{"id":"a","status":"OFFLINE"},{"id":"b","status":"OFFLINE"}]`
	p := frameConnected(t, mod, mn, 0)
	if err := p.Subscribe(consumer.ValueRequest{Slot: -1}, func(consumer.Event) {}); err == nil || !strings.Contains(err.Error(), "no present slot") {
		t.Errorf("err = %v", err)
	}
}

func TestWatchWalksAnUnwalkedSlotOnce(t *testing.T) {
	withPlan(t, fastPlan)
	m := newModule(t)
	p := connected(t, m)
	before := m.count("GET ")
	if err := p.Subscribe(consumer.ValueRequest{Slot: 0}, func(consumer.Event) {}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Unsubscribe(consumer.ValueRequest{Slot: 0}) })
	if got := m.count("GET "); got != before+1 {
		t.Errorf("one walk expected at Subscribe (root GET %d → %d)", before, got)
	}
	p.mu.Lock()
	_, walked := p.lastWalk[0]
	p.mu.Unlock()
	if !walked {
		t.Error("the walk must be kept for the next Subscribe")
	}
}

func TestCachedGetServesWithinTTL(t *testing.T) {
	m := newModule(t)
	p := connected(t, m)
	ctx := context.Background()
	if _, err := p.GetValue(ctx, consumer.ValueRequest{Path: "refclk.mode"}); err != nil {
		t.Fatal(err)
	}
	n := m.calls["GET refclk"]
	if _, err := p.GetValue(ctx, consumer.ValueRequest{Path: "refclk.status"}); err != nil {
		t.Fatal(err)
	}
	if m.calls["GET refclk"] != n {
		t.Errorf("a second leaf of the same document within docTTL must not refetch (%d → %d)", n, m.calls["GET refclk"])
	}
	// A set reads fresh and its read-back refreshes what a later get sees.
	if _, err := p.SetValue(ctx, consumer.ValueRequest{Path: "refclk.delay_req"}, consumer.Value{Str: "-2"}); err != nil {
		t.Fatal(err)
	}
	if m.calls["GET refclk"] < n+2 {
		t.Errorf("SetValue must read fresh before and after the PUT (%d → %d)", n, m.calls["GET refclk"])
	}
	// A cached-path error still surfaces.
	delete(m.docs, "telemetry")
	if _, err := p.GetValue(ctx, consumer.ValueRequest{Path: "telemetry.node.health"}); err == nil {
		t.Error("a missing listing must error through the cache")
	}
}

func TestPollProfileFilterAndPatterns(t *testing.T) {
	d, _, err := parseDictionary([]byte(fastPlan), videoFormatsJSON)
	if err != nil {
		t.Fatal(err)
	}
	objs := []consumer.Object{
		{Path: []string{"flows", "a", "network", "pkt_cnt"}},
		{Path: []string{"flows", "a", "network", "dst_ip_addr"}},
		{Path: []string{"refclk", "mode"}},
		{Path: []string{"refclk", "uuid", "0"}},
	}
	prof, err := pollProfile(d, objs, 3, "")
	if err != nil || len(prof.Entries) != 3 || prof.Entries[0].Slot != 3 {
		t.Fatalf("profile = %+v, %v", prof, err)
	}
	prof, err = pollProfile(d, objs, 0, "refclk")
	if err != nil || len(prof.Entries) != 2 {
		t.Errorf("filtered profile = %+v, %v", prof, err)
	}
	if _, err := pollProfile(d, objs, 0, "flows.a.network.dst_ip_addr"); err == nil {
		t.Error("a leaf the plan does not name yields nothing to poll")
	}
	for pat, want := range map[[2]string]bool{
		{"telemetry.node.**", "telemetry.node.health.core_temp"}: true,
		{"telemetry.node.**", "telemetry.node"}:                  true,
		{"telemetry.node.**", "telemetry"}:                       false,
		{"refclk.**", "self.refclk"}:                             false,
	} {
		if got := matchPath(pat[0], strings.Split(pat[1], ".")); got != want {
			t.Errorf("matchPath(%q, %q) = %v", pat[0], pat[1], got)
		}
	}
}

func TestPollProfileCarriesOnChangeFromThePlan(t *testing.T) {
	// A measurement is judged per sample, so its plan row turns
	// on_change off; a setting is only news when it moves, so its row
	// says nothing and inherits the default.
	const plan = `{"model":"FusioN6","entries":[],"sibling_thresholds":[],
 "poll":{"defaults":{"interval":"40ms"},"oids":[
   {"match":"**.network.pkt_cnt","interval":"40ms","on_change":false},
   {"match":"refclk.mode","interval":"40ms"}]}}`
	d, _, err := parseDictionary([]byte(plan), videoFormatsJSON)
	if err != nil {
		t.Fatal(err)
	}
	objs := []consumer.Object{
		{Path: []string{"flows", "a", "network", "pkt_cnt"}},
		{Path: []string{"refclk", "mode"}},
	}
	prof, err := pollProfile(d, objs, 0, "")
	if err != nil || len(prof.Entries) != 2 {
		t.Fatalf("profile = %+v, %v", prof, err)
	}
	if prof.Entries[0].OnChange == nil || *prof.Entries[0].OnChange {
		t.Errorf("pkt_cnt must publish every sample: %+v", prof.Entries[0].OnChange)
	}
	if prof.Entries[1].OnChange != nil {
		t.Errorf("a row that says nothing must inherit the default: %+v", prof.Entries[1].OnChange)
	}
	if !prof.Defaults.OnChange {
		t.Error("the default stays on_change: most leaves are settings")
	}
}

func TestShippedPlanPublishesEverySampleOfAHealthLeaf(t *testing.T) {
	// The FusioN6 plan we ship must keep the health leaves on every
	// sample: a stopped counter and a down link are conditions that
	// PERSIST, and an alarm rule cannot see them from changes alone.
	d, _, err := parseDictionary(fusion6Dictionary, videoFormatsJSON)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, e := range d.Poll.Entries {
		if e.OnChange != nil && !*e.OnChange {
			seen[e.Match] = true
		}
	}
	for _, pat := range []string{"**.network.pkt_cnt", "refclk.status", "telemetry.node.**", "port.*.link"} {
		if !seen[pat] {
			t.Errorf("shipped plan: %q must carry on_change:false", pat)
		}
	}
}
