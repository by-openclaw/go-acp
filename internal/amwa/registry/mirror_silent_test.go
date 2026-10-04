package registry

// A target that answers slowly, or not at all (#1311): a heartbeat round
// is not a queue behind its slowest answer, a pass stops after a few
// requests in a row go unanswered instead of queueing a plant behind a
// timeout each, and what went unanswered is tried again after a wait
// that doubles while the silence lasts.

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is04"
)

// One node's heartbeat hangs in the target. The other nodes of the
// round are heartbeated meanwhile, and the evicted ones are reported
// once the round ends.
func TestHeartbeatRoundDoesNotQueueBehindASlowAnswer(t *testing.T) {
	var (
		mu      sync.Mutex
		seen    = map[string]bool{}
		release = make(chan struct{})
		once    sync.Once
	)
	letGo := func() { once.Do(func() { close(release) }) }
	tsrv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		id := r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:]
		mu.Lock()
		seen[id] = true
		mu.Unlock()
		switch {
		case id == "a-slow":
			<-release
			w.WriteHeader(stdhttp.StatusOK)
		case strings.HasSuffix(id, "-gone"):
			w.WriteHeader(stdhttp.StatusNotFound)
		default:
			w.WriteHeader(stdhttp.StatusOK)
		}
	}))
	defer tsrv.Close()
	defer letGo() // a failed run must not leave the held request blocking Close

	m := mirrorTo(t, tsrv.URL)
	m.logger = newRegistryLogTap().logger()
	refs := []nodeRef{{"a-slow", "v1.3"}, {"b", "v1.3"}, {"c", "v1.3"}, {"x-gone", "v1.3"}, {"y-gone", "v1.3"}}

	result := make(chan []string, 1)
	go func() { result <- m.heartbeatRound(context.Background(), refs) }()

	waitFor(t, 5*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return seen["b"] && seen["c"] && seen["x-gone"] && seen["y-gone"]
	}, "the other nodes to be heartbeated while the first answer hangs")

	letGo()
	select {
	case evicted := <-result:
		if exp := []string{"x-gone", "y-gone"}; !reflect.DeepEqual(evicted, exp) {
			t.Errorf("evicted = %v, want %v", evicted, exp)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the round must end once every answer is in")
	}
}

// silentUntil answers 503 to the first n POSTs, then accepts.
func silentUntil(n int) func(string, int) int {
	count := 0
	return func(string, int) int { // called under the target's lock
		count++
		if count <= n {
			return stdhttp.StatusServiceUnavailable
		}
		return stdhttp.StatusCreated
	}
}

// running marks the mirror as inside Run, with test-sized waits.
func running(m *Mirror, ctx context.Context, lo, hi time.Duration) {
	m.mu.Lock()
	m.runCtx = ctx
	m.retryMin, m.retryMax = lo, hi
	m.mu.Unlock()
}

// The target stops answering. The pass stops at the third request in a
// row that goes unanswered — it does not walk the catalogue into the
// silence — and the same work is tried again until the target answers.
func TestMirrorStopsAPassTheTargetDoesNotAnswerAndTriesAgain(t *testing.T) {
	target := &orderTarget{answer: silentUntil(6)}
	m, tap := repairMirror(t, target)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	running(m, ctx, 20*time.Millisecond, 40*time.Millisecond)

	m.resync(ctx)
	waitFor(t, 5*time.Second, func() bool { return m.Stats().Forwarded == 12 }, "the catalogue to land once the target answers")

	sent := target.sent()
	if len(sent) != 18 {
		t.Fatalf("%d requests, want 3 + 3 into the silence and then the 12: %v", len(sent), sent)
	}
	if exp := []string{"node:n1", "node:n2", "device:d1", "node:n1", "node:n2", "device:d1"}; !reflect.DeepEqual(sent[:6], exp) {
		t.Errorf("the two stopped passes sent %v, want %v", sent[:6], exp)
	}
	if !tap.has("target is not answering") {
		t.Errorf("a stopped pass must say so; saw %v", tap.snapshot())
	}
	waitFor(t, 5*time.Second, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.retryDelay == 0 && m.retryTimer == nil
	}, "the wait to go back to its shortest once the target has answered")
	if st := m.Stats(); st.Resyncs != 3 {
		t.Errorf("resyncs = %d, want the repair and its two retries", st.Resyncs)
	}
}

// While the target stays silent the wait doubles, up to its cap.
func TestMirrorWaitsLongerEachTimeUpToItsCap(t *testing.T) {
	target := &orderTarget{answer: func(string, int) int { return stdhttp.StatusServiceUnavailable }}
	m, _ := repairMirror(t, target)
	ctx, cancel := context.WithCancel(context.Background())
	running(m, ctx, 10*time.Millisecond, 25*time.Millisecond)

	m.resync(ctx)
	waitFor(t, 5*time.Second, func() bool { return len(target.sent()) >= 12 }, "four passes into the silence")
	m.mu.Lock()
	delay := m.retryDelay
	m.mu.Unlock()
	if delay != 25*time.Millisecond {
		t.Errorf("the next wait is %v, want the cap", delay)
	}

	// A mirror that stops does not try again.
	cancel()
	time.Sleep(60 * time.Millisecond)
	before := len(target.sent())
	time.Sleep(60 * time.Millisecond)
	if after := len(target.sent()); after != before {
		t.Errorf("%d more requests after the stop", after-before)
	}
}

// One request of a pass goes unanswered. The pass finishes, and that
// resource is sent again behind the ancestors it names.
func TestMirrorTriesAgainWhatWentUnanswered(t *testing.T) {
	target := &orderTarget{answer: func(key string, nth int) int {
		if key == "sender:x1" && nth == 1 {
			return stdhttp.StatusInternalServerError
		}
		return stdhttp.StatusCreated
	}}
	m, _ := repairMirror(t, target)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	running(m, ctx, 20*time.Millisecond, 40*time.Millisecond)

	m.resync(ctx)
	waitFor(t, 5*time.Second, func() bool { return len(target.sent()) >= 17 }, "the unanswered sender to be sent again")
	time.Sleep(100 * time.Millisecond)

	sent := target.sent()
	if exp := []string{"node:n1", "device:d1", "source:s1", "flow:f1", "sender:x1"}; len(sent) != 17 || !reflect.DeepEqual(sent[12:], exp) {
		t.Errorf("after the pass the target saw %v, want exactly the sender's chain %v", sent[12:], exp)
	}
}

// A live forward the target did not answer is owed again too.
func TestMirrorTriesAgainALiveForwardTheTargetDidNotAnswer(t *testing.T) {
	target := &orderTarget{answer: func(key string, nth int) int {
		if key == "flow:f1" && nth == 1 {
			return stdhttp.StatusBadGateway
		}
		return stdhttp.StatusCreated
	}}
	m, _ := repairMirror(t, target)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	running(m, ctx, 20*time.Millisecond, 40*time.Millisecond)
	m.mu.Lock()
	doc := m.cache["flows"]["f1"]
	delete(m.cache["flows"], "f1") // it arrives now, on the live path
	m.mu.Unlock()

	m.forwardRow(ctx, "flows", "v1.3", is04.GrainDataRow{Path: "f1", Post: doc})
	waitFor(t, 5*time.Second, func() bool { return len(target.sent()) >= 5 }, "the unanswered flow to be sent again")
	time.Sleep(100 * time.Millisecond)

	if got, exp := target.sent(), []string{"flow:f1", "node:n1", "device:d1", "source:s1", "flow:f1"}; !reflect.DeepEqual(got, exp) {
		t.Errorf("target saw %v, want %v", got, exp)
	}
}

// A mirror that stops while a retry is waiting takes the timer with it.
func TestMirrorRunStopsAWaitingRetry(t *testing.T) {
	target := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusServiceUnavailable)
	}))
	defer target.Close()

	plant := &fakePlant{}
	frames := map[string][][]byte{
		"nodes":   {grainFrame("nodes", "n1", "", `{"id":"n1"}`)},
		"devices": {grainFrame("devices", "d1", "", `{"id":"d1","node_id":"n1"}`)},
	}
	src := httptest.NewServer(stdhttp.NotFoundHandler())
	src.Config.Handler = plant.sourceHandler(t, func() string { return src.URL }, frames)
	defer src.Close()

	m, err := NewMirror(MirrorOptions{Source: src.URL, Target: target.URL, APIVer: "v1.3"})
	if err != nil {
		t.Fatal(err)
	}
	m.logger = newRegistryLogTap().logger()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = m.Run(ctx)
		close(done)
	}()
	// Both forwards go unanswered: the second rides the first's timer.
	waitFor(t, 5*time.Second, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.retryTimer != nil && m.retry.refused.size() == 2
	}, "both unanswered forwards to be waiting for the retry")

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run must end with its context")
	}
	m.mu.Lock()
	armed := m.retryTimer != nil
	m.mu.Unlock()
	if armed {
		t.Error("the retry timer outlived the mirror")
	}
}
