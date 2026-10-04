package registry

// The mirror behind a source that translates (IS-04 "Upgrade Path"): a
// resource registered at v1.3 is shown on every lower minor too, as a
// different document. The resource is what its own minor shows; the
// rest are views, and the target is told about the resource once, at
// its minor.

import (
	"context"
	"encoding/json"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/session/query"
)

var fourMinors = []string{"v1.0", "v1.1", "v1.2", "v1.3"}

// translatingSource is a Query API that shows a resource on the minor it
// is registered at and on every minor below it, and — asked to
// downgrade — on the minors above it down to the one named.
type translatingSource struct {
	mu         sync.Mutex
	registered map[string]string // id -> minor
	revision   map[string]int    // id -> bumps the document
	down       bool              // every read answers 500
	ghost      bool              // "registered somewhere below" is yes, and no minor lists it
	asked      []string          // "<minor> <topic> <query>"
	onRead     func()            // runs inside the next read, once
}

// view is the document of id as the minor at shows it.
func (s *translatingSource) view(id, at string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"id":%q,"registered":%q,"shown_at":%q,"rev":%d}`, id, s.registered[id], at, s.revision[id]))
}

// handler is the source's Query API as one minor serves it. (The test
// binary carries one codec, so each minor is a server of its own rather
// than a path.)
func (s *translatingSource) handler(at string) stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		topic := parts[len(parts)-1]
		s.mu.Lock()
		hook := s.onRead
		s.onRead = nil
		s.asked = append(s.asked, at+" "+topic+" "+r.URL.RawQuery)
		down, ghost := s.down, s.ghost
		id, downgrade := r.URL.Query().Get("id"), r.URL.Query().Get("query.downgrade")
		out := []json.RawMessage{}
		for known, reg := range s.registered {
			if id != "" && known != id {
				continue
			}
			above := reg == at || minorLess(at, reg)
			below := downgrade != "" && minorLess(reg, at) && !minorLess(reg, downgrade)
			if (above || below) && !ghost {
				out = append(out, s.view(known, at))
			}
		}
		if ghost && downgrade != "" {
			out = append(out, json.RawMessage(`{"id":"`+id+`"}`))
		}
		s.mu.Unlock()
		if hook != nil {
			hook()
		}
		if down {
			stdhttp.Error(w, "source fault", stdhttp.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	})
}

func (s *translatingSource) questions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]string(nil), s.asked...)
	s.asked = nil
	return out
}

// minorTarget records what the mirror sends its target, with the minor
// each request was addressed at.
type minorTarget struct {
	mu   sync.Mutex
	sent []string // "POST v1.3 <id> rev=<n> shown_at=<minor>" / "DELETE v1.3 <id>"
}

func (g *minorTarget) handler() stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/") // x-nmos registration <minor> resource [<topic> <id>]
		g.mu.Lock()
		defer g.mu.Unlock()
		if r.Method == stdhttp.MethodDelete {
			g.sent = append(g.sent, "DELETE "+parts[2]+" "+parts[len(parts)-1])
			w.WriteHeader(stdhttp.StatusNoContent)
			return
		}
		var body struct {
			Data struct {
				ID      string `json:"id"`
				ShownAt string `json:"shown_at"`
				Rev     int    `json:"rev"`
			} `json:"data"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		g.sent = append(g.sent, fmt.Sprintf("POST %s %s rev=%d shown_at=%s", parts[2], body.Data.ID, body.Data.Rev, body.Data.ShownAt))
		w.WriteHeader(stdhttp.StatusCreated)
	})
}

func (g *minorTarget) requests() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := strings.Join(g.sent, "; ")
	g.sent = nil
	return out
}

// minorMirror is a Mirror between a translating source and a recording
// target, with a client for each of the four minors.
func minorMirror(t *testing.T, minors ...string) (*Mirror, *translatingSource, *minorTarget) {
	t.Helper()
	prev := supportedVersions
	supportedVersions = func() []string { return fourMinors }
	t.Cleanup(func() { supportedVersions = prev })

	src := &translatingSource{registered: map[string]string{}, revision: map[string]int{}}
	tgt := &minorTarget{}
	tgtSrv := httptest.NewServer(tgt.handler())
	t.Cleanup(tgtSrv.Close)

	m, err := NewMirror(MirrorOptions{Source: "http://source.invalid:1", Target: tgtSrv.URL, APIVer: "v1.3"})
	if err != nil {
		t.Fatal(err)
	}
	m.logger = newRegistryLogTap().logger()
	m.audit, _ = newAuditor("", 0)
	codec, _ := is04.Get("v1.3")
	if len(minors) == 0 {
		minors = fourMinors
	}
	m.sourceClients = map[string]*query.Client{}
	for _, ver := range minors {
		srv := httptest.NewServer(src.handler(ver))
		t.Cleanup(srv.Close)
		qc, err := query.NewClient(srv.URL, codec)
		if err != nil {
			t.Fatal(err)
		}
		m.sourceClients[ver] = qc
	}
	return m, src, tgt
}

const minorNode = "5d3f0e1a-7c42-4b8e-9a61-0f2b6c8d4e17"

func (m *Mirror) tracked(topic, id string) (ver string, viewed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cacheVer[topic][id], m.lowerSeen[topic][id]
}

// A resource registered at v1.3 reaches the target once, at v1.3, as
// the document v1.3 shows — whichever subscription shows it first, and
// whatever the lower ones show of it afterwards.
func TestALowerMinorsViewIsNotTheResource(t *testing.T) {
	m, src, tgt := minorMirror(t)
	ctx := context.Background()
	src.registered[minorNode] = "v1.3"

	// The v1.1 subscription is the first to show it.
	m.forwardRow(ctx, "nodes", "v1.1", is04.GrainDataRow{Path: minorNode, Post: src.view(minorNode, "v1.1")})
	if got, want := tgt.requests(), "POST v1.3 "+minorNode+" rev=0 shown_at=v1.3"; got != want {
		t.Fatalf("the target received %q, want %q", got, want)
	}
	if asked := src.questions(); len(asked) != 1 || !strings.HasPrefix(asked[0], "v1.3 nodes id="+minorNode) {
		t.Errorf("the source was asked %v, want the one question: is it at v1.3", asked)
	}
	if ver, viewed := m.tracked("nodes", minorNode); ver != "v1.3" || !viewed {
		t.Errorf("tracked at %q (viewed below: %t), want v1.3 and viewed", ver, viewed)
	}

	// Its own subscription then shows the same document; the lower ones
	// show their views, and take theirs away. Nothing of that is news.
	m.forwardRow(ctx, "nodes", "v1.3", is04.GrainDataRow{Path: minorNode, Post: src.view(minorNode, "v1.3")})
	m.forwardRow(ctx, "nodes", "v1.0", is04.GrainDataRow{Path: minorNode, Pre: src.view(minorNode, "v1.0"), Post: src.view(minorNode, "v1.0")})
	m.forwardRow(ctx, "nodes", "v1.2", is04.GrainDataRow{Path: minorNode, Pre: src.view(minorNode, "v1.2")})
	if got := tgt.requests(); got != "" {
		t.Errorf("views and a repeated document reached the target: %q", got)
	}
	if asked := src.questions(); len(asked) != 0 {
		t.Errorf("a tracked resource costs the source nothing, it was asked %v", asked)
	}

	// A change arrives on its own subscription, and is forwarded there.
	src.revision[minorNode] = 1
	m.forwardRow(ctx, "nodes", "v1.3", is04.GrainDataRow{Path: minorNode, Pre: src.view(minorNode, "v1.3"), Post: src.view(minorNode, "v1.3")})
	if got, want := tgt.requests(), "POST v1.3 "+minorNode+" rev=1 shown_at=v1.3"; got != want {
		t.Errorf("the change reached the target as %q, want %q", got, want)
	}

	// It leaves: one DELETE at its minor, and — a lower minor showed it
	// too — one question to the source: is it registered anywhere now?
	delete(src.registered, minorNode)
	m.forwardRow(ctx, "nodes", "v1.3", is04.GrainDataRow{Path: minorNode, Pre: src.view(minorNode, "v1.3")})
	if got, want := tgt.requests(), "DELETE v1.3 "+minorNode; got != want {
		t.Errorf("the removal reached the target as %q, want %q", got, want)
	}
	if asked := src.questions(); len(asked) != 1 || !strings.Contains(asked[0], "query.downgrade=v1.0") || !strings.HasPrefix(asked[0], "v1.3 ") {
		t.Errorf("the source was asked %v, want the one question at v1.3 with query.downgrade=v1.0", asked)
	}
	if ver, viewed := m.tracked("nodes", minorNode); ver != "" || viewed {
		t.Errorf("after the removal it is still tracked at %q (viewed: %t)", ver, viewed)
	}

	// A removal for something the cache does not hold is nobody's news.
	m.forwardRow(ctx, "nodes", "v1.3", is04.GrainDataRow{Path: minorNode, Pre: src.view(minorNode, "v1.3")})
	if got := tgt.requests(); got != "" {
		t.Errorf("a second removal reached the target: %q", got)
	}
}

// A resource shown on one minor only — a source that does not
// translate, or a resource registered at the lowest minor — is asked
// about at the minors above, and stays where it was seen.
func TestAResourceOnlyItsOwnMinorShows(t *testing.T) {
	m, src, tgt := minorMirror(t)
	ctx := context.Background()
	src.registered[minorNode] = "v1.2"

	m.forwardRow(ctx, "nodes", "v1.2", is04.GrainDataRow{Path: minorNode, Post: src.view(minorNode, "v1.2")})
	if got, want := tgt.requests(), "POST v1.2 "+minorNode+" rev=0 shown_at=v1.2"; got != want {
		t.Fatalf("the target received %q, want %q", got, want)
	}
	if asked := src.questions(); len(asked) != 1 || !strings.HasPrefix(asked[0], "v1.3 ") {
		t.Errorf("the source was asked %v, want the one question at v1.3", asked)
	}
	// Removed with no lower view ever seen: nothing to ask.
	delete(src.registered, minorNode)
	m.forwardRow(ctx, "nodes", "v1.2", is04.GrainDataRow{Path: minorNode, Pre: src.view(minorNode, "v1.2")})
	if got, want := tgt.requests(), "DELETE v1.2 "+minorNode; got != want {
		t.Errorf("the removal reached the target as %q, want %q", got, want)
	}
	if asked := src.questions(); len(asked) != 0 {
		t.Errorf("the source was asked %v, want nothing", asked)
	}
}

// A Node that changes minor deletes and registers again, and the two
// subscriptions race: the lower one's row comes first and is taken for
// a view. The removal on the old minor finds the resource below.
func TestAResourceRegisteredAgainLowerDownIsFollowed(t *testing.T) {
	m, src, tgt := minorMirror(t)
	ctx := context.Background()
	src.registered[minorNode] = "v1.3"
	m.forwardRow(ctx, "nodes", "v1.3", is04.GrainDataRow{Path: minorNode, Post: src.view(minorNode, "v1.3")})
	_ = tgt.requests()

	src.registered[minorNode] = "v1.1"
	m.forwardRow(ctx, "nodes", "v1.1", is04.GrainDataRow{Path: minorNode, Pre: src.view(minorNode, "v1.1"), Post: src.view(minorNode, "v1.1")})
	if got := tgt.requests(); got != "" {
		t.Fatalf("the lower subscription's row reached the target: %q", got)
	}
	_ = src.questions()
	m.forwardRow(ctx, "nodes", "v1.3", is04.GrainDataRow{Path: minorNode, Pre: src.view(minorNode, "v1.3")})

	if got, want := tgt.requests(), "DELETE v1.3 "+minorNode+"; POST v1.1 "+minorNode+" rev=0 shown_at=v1.1"; got != want {
		t.Errorf("the target received %q, want %q", got, want)
	}
	// Is it anywhere (at v1.3, including every earlier minor)? Then, from
	// the top: at v1.3? at v1.2? at v1.1?
	if asked := src.questions(); len(asked) != 4 || !strings.HasPrefix(asked[1], "v1.3 nodes id=") || !strings.HasPrefix(asked[3], "v1.1 nodes id=") {
		t.Errorf("the source was asked %v", asked)
	}
	if ver, _ := m.tracked("nodes", minorNode); ver != "v1.1" {
		t.Errorf("tracked at %q, want v1.1", ver)
	}
}

// A source that says the resource is registered below and lists it at
// no minor leaves it removed.
func TestAResourceTheSourceCannotPlaceStaysRemoved(t *testing.T) {
	m, src, tgt := minorMirror(t)
	ctx := context.Background()
	src.registered[minorNode] = "v1.3"
	m.forwardRow(ctx, "nodes", "v1.1", is04.GrainDataRow{Path: minorNode, Post: src.view(minorNode, "v1.1")})
	_ = tgt.requests()

	src.mu.Lock()
	src.ghost = true
	src.mu.Unlock()
	m.forwardRow(ctx, "nodes", "v1.3", is04.GrainDataRow{Path: minorNode, Pre: src.view(minorNode, "v1.3")})
	if got, want := tgt.requests(), "DELETE v1.3 "+minorNode; got != want {
		t.Errorf("the target received %q, want %q", got, want)
	}
	if ver, _ := m.tracked("nodes", minorNode); ver != "" {
		t.Errorf("tracked at %q, want it gone", ver)
	}
}

// A source that does not answer cannot place a resource a lower minor's
// subscription shows first: sent as that row has it, a v1.3 resource
// would reach the target as its v1.1 view, registered at v1.1. The row
// is not sent; the resource's own subscription lands it.
func TestAResourceTheSourceCannotPlaceWaitsForItsOwnSubscription(t *testing.T) {
	m, src, tgt := minorMirror(t)
	ctx := context.Background()
	src.registered[minorNode] = "v1.3"
	src.mu.Lock()
	src.down = true
	src.mu.Unlock()

	m.forwardRow(ctx, "nodes", "v1.1", is04.GrainDataRow{Path: minorNode, Post: src.view(minorNode, "v1.1")})
	if got := tgt.requests(); got != "" {
		t.Fatalf("a resource the source could not place was sent: %q", got)
	}
	if ver, _ := m.tracked("nodes", minorNode); ver != "" {
		t.Errorf("tracked at %q before the source said where it is", ver)
	}
	var looked bool
	for _, ev := range m.audit.recent() {
		looked = looked || ev.Kind == "source_lookup_failed"
	}
	if !looked {
		t.Error("the failed lookup is not in the audit trail")
	}

	m.forwardRow(ctx, "nodes", "v1.3", is04.GrainDataRow{Path: minorNode, Post: src.view(minorNode, "v1.3")})
	if got, want := tgt.requests(), "POST v1.3 "+minorNode+" rev=0 shown_at=v1.3"; got != want {
		t.Errorf("the target received %q, want %q", got, want)
	}
}

// A resource registered again at a later minor — its Node was upgraded —
// is moved up when that minor's subscription shows it.
func TestAResourceRegisteredAgainHigherUpIsMoved(t *testing.T) {
	m, src, tgt := minorMirror(t)
	ctx := context.Background()
	src.registered[minorNode] = "v1.1"
	m.forwardRow(ctx, "nodes", "v1.1", is04.GrainDataRow{Path: minorNode, Post: src.view(minorNode, "v1.1")})
	if got, want := tgt.requests(), "POST v1.1 "+minorNode+" rev=0 shown_at=v1.1"; got != want {
		t.Fatalf("the target received %q, want %q", got, want)
	}

	src.registered[minorNode] = "v1.3"
	m.forwardRow(ctx, "nodes", "v1.3", is04.GrainDataRow{Path: minorNode, Post: src.view(minorNode, "v1.3")})
	if got, want := tgt.requests(), "POST v1.3 "+minorNode+" rev=0 shown_at=v1.3"; got != want {
		t.Errorf("the target received %q, want %q", got, want)
	}
	if ver, _ := m.tracked("nodes", minorNode); ver != "v1.3" {
		t.Errorf("tracked at %q, want v1.3", ver)
	}
}

// A Node restarts: its resources leave and come back at the minor they
// had. The lower minors go on showing their views of them throughout —
// which says nothing about where they are registered. They stay at
// their minor. (On the plant, v0.36.1, 81 sources went to the target at
// v1.1 and v1.2 this way.)
func TestAResourceThatLeavesAndComesBackStaysAtItsMinor(t *testing.T) {
	m, src, tgt := minorMirror(t)
	ctx := context.Background()
	src.registered[minorNode] = "v1.3"
	m.forwardRow(ctx, "nodes", "v1.3", is04.GrainDataRow{Path: minorNode, Post: src.view(minorNode, "v1.3")})
	m.forwardRow(ctx, "nodes", "v1.1", is04.GrainDataRow{Path: minorNode, Post: src.view(minorNode, "v1.1")}) // a view: noted
	_ = tgt.requests()

	// The removal reaches the mirror when the Node has registered again.
	src.revision[minorNode] = 1
	m.forwardRow(ctx, "nodes", "v1.3", is04.GrainDataRow{Path: minorNode, Pre: src.view(minorNode, "v1.3")})
	if got, want := tgt.requests(), "DELETE v1.3 "+minorNode+"; POST v1.3 "+minorNode+" rev=1 shown_at=v1.3"; got != want {
		t.Fatalf("the target received %q, want %q", got, want)
	}
	if ver, _ := m.tracked("nodes", minorNode); ver != "v1.3" {
		t.Errorf("tracked at %q, want v1.3", ver)
	}
	// Its own subscription then shows the new registration: nothing new.
	m.forwardRow(ctx, "nodes", "v1.3", is04.GrainDataRow{Path: minorNode, Post: src.view(minorNode, "v1.3")})
	m.forwardRow(ctx, "nodes", "v1.0", is04.GrainDataRow{Path: minorNode, Post: src.view(minorNode, "v1.0")})
	if got := tgt.requests(); got != "" {
		t.Errorf("the registration shown again reached the target: %q", got)
	}
}

// land does not put a resource below where another subscription has
// placed it meanwhile.
func TestLandDoesNotMoveAResourceDown(t *testing.T) {
	m, src, tgt := minorMirror(t)
	ctx := context.Background()
	src.registered[minorNode] = "v1.3"
	m.forwardRow(ctx, "nodes", "v1.3", is04.GrainDataRow{Path: minorNode, Post: src.view(minorNode, "v1.3")})
	_ = tgt.requests()

	m.land(ctx, "nodes", "v1.1", "v1.1", is04.GrainDataRow{Path: minorNode, Post: src.view(minorNode, "v1.1")})
	if got := tgt.requests(); got != "" {
		t.Errorf("a lower placement reached the target: %q", got)
	}
	if ver, viewed := m.tracked("nodes", minorNode); ver != "v1.3" || !viewed {
		t.Errorf("tracked at %q (viewed below: %t), want v1.3 and viewed", ver, viewed)
	}
}

// Two subscriptions show a new resource at once: while the lower one
// asks the source where it is registered, its own lands it. The lower
// one then has nothing left to send — whether the source answered its
// question (its row is seen to be a view) or not (its row is dropped).
func TestTwoSubscriptionsShowANewResourceAtOnce(t *testing.T) {
	for name, down := range map[string]bool{"the source answers": false, "the source does not": true} {
		t.Run(name, func(t *testing.T) {
			m, src, tgt := minorMirror(t)
			ctx := context.Background()
			src.registered[minorNode] = "v1.3"
			src.down = down
			src.onRead = func() {
				m.forwardRow(ctx, "nodes", "v1.3", is04.GrainDataRow{Path: minorNode, Post: src.view(minorNode, "v1.3")})
			}
			m.forwardRow(ctx, "nodes", "v1.0", is04.GrainDataRow{Path: minorNode, Post: src.view(minorNode, "v1.0")})

			if got, want := tgt.requests(), "POST v1.3 "+minorNode+" rev=0 shown_at=v1.3"; got != want {
				t.Errorf("the target received %q, want the one %q", got, want)
			}
			if ver, viewed := m.tracked("nodes", minorNode); ver != "v1.3" || viewed == down {
				t.Errorf("tracked at %q (viewed below: %t), want v1.3, and the view noted when the source answered", ver, viewed)
			}
		})
	}
}

// A minor the mirror has no client for is not asked, and the minors are
// read highest first.
func TestSourceMinorsHighestFirst(t *testing.T) {
	m, src, _ := minorMirror(t, "v1.1", "v1.2")
	if got := fmt.Sprint(m.sourceMinors()); got != "[v1.3 v1.2 v1.1 v1.0]" {
		t.Errorf("sourceMinors = %s", got)
	}
	src.registered[minorNode] = "v1.1"
	reg, _, known := m.registeredAt(context.Background(), "nodes", "v1.1", minorNode, src.view(minorNode, "v1.1"))
	if reg != "v1.1" || !known {
		t.Errorf("registered at %q, want v1.1", reg)
	}
	// v1.3 has no client; v1.2 is asked and does not list it.
	if asked := src.questions(); len(asked) != 1 || !strings.HasPrefix(asked[0], "v1.2 ") {
		t.Errorf("the source was asked %v, want the one question at v1.2", asked)
	}
}

// The catalogue is read minor by minor, and the reads are not one
// instant. A resource registered at v1.3 while they are under way must
// not be taken at the first minor that happens to list it: read highest
// first, it missed the v1.3 listing and was claimed by a lower minor's
// view of it — on the plant, v0.36.1, 81 sources went to the target at
// v1.1 and v1.2 while a Node was registering during the fill. Read
// lowest first with the higher minor overriding, it ends at its minor.
func TestARefreshPlacesAResourceRegisteredMidReadAtItsMinor(t *testing.T) {
	m, src, _ := minorMirror(t)
	const early, late = minorNode, "7e5a1c9d-3b6f-4d2a-8c41-9f0e2b7a6d53"
	src.registered[early] = "v1.3"
	// Registered just after the first listing has been read.
	src.onRead = func() {
		src.mu.Lock()
		src.registered[late] = "v1.3"
		src.mu.Unlock()
	}

	m.refreshCacheFromSource(context.Background())

	for _, id := range []string{early, late} {
		if ver, _ := m.tracked("nodes", id); ver != "v1.3" {
			t.Errorf("node %s is tracked at %q, want v1.3", id, ver)
		}
		m.mu.Lock()
		doc := string(m.cache["nodes"][id])
		m.mu.Unlock()
		if !strings.Contains(doc, `"shown_at":"v1.3"`) {
			t.Errorf("node %s is cached as %s, want the document v1.3 shows", id, doc)
		}
	}
}
