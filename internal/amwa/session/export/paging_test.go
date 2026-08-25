package export

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// pagingMode selects which of the three cursor signals a fake registry
// offers. Real registries differ here, and relying on only the first
// one ended a field capture after ten nodes.
type pagingMode int

const (
	// modeLink emits the full prev/next Link set, as a real registry
	// does.
	modeLink pagingMode = iota
	// modeHeaders emits X-Paging-* and NO Link header.
	modeHeaders
	// modeBare emits neither: a full page and nothing else to go on.
	modeBare
	// modeIgnoreSince serves the same first page forever, whatever
	// cursor it is handed.
	modeIgnoreSince
	// modeBrokenPrev is the real EVS registry's /flows defect: the
	// forward cursor is sound, but `rel="prev"` carries an empty
	// `paging.until` and X-Paging-Since is blank, so the descending
	// walk re-serves page one forever while the ascending walk
	// completes normally.
	modeBrokenPrev
	// modeEmptyAscending is the SAME registry's /flows endpoint one
	// export later: asked with paging.since it answers with an empty
	// window — Since and Until both 0:0, zero rows — while the
	// descending walk still returns resources. A collection that is
	// genuinely empty is indistinguishable from this on the first
	// page, which is exactly why one direction cannot settle it.
	modeEmptyAscending
)

// pagingRegistry serves `total` nodes in pages of `limit`, honouring
// paging.since over the resource version.
type pagingRegistry struct {
	total int
	limit int
	mode  pagingMode
	// serverLimit caps what the registry will serve regardless of what
	// the client asks for, the way a real one clamps paging.limit.
	serverLimit int
	requests    int
}

// nodeAt builds node N with version N+1000 seconds, so versions sort in
// the order the registry serves them.
func nodeAt(i int) map[string]any {
	return map[string]any{
		"id":      fmt.Sprintf("%08d-1111-4111-8111-111111111111", i),
		"version": fmt.Sprintf("%d:0", 1000+i),
		"label":   fmt.Sprintf("node-%03d", i),
		"tags":    map[string]any{},
	}
}

func (p *pagingRegistry) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/x-nmos":
		writeJSON(w, []string{"query/"})
		return
	case "/x-nmos/query/":
		writeJSON(w, []string{"v1.3/"})
		return
	}
	if r.URL.Path != "/x-nmos/query/v1.3/nodes" {
		if strings.HasPrefix(r.URL.Path, "/x-nmos/query/v1.3/") {
			writeJSON(w, []any{})
			return
		}
		http.NotFound(w, r)
		return
	}

	p.requests++

	limit := p.limit
	if askedRaw := r.URL.Query().Get("paging.limit"); askedRaw != "" {
		if asked, err := strconv.Atoi(askedRaw); err == nil && asked > 0 {
			limit = asked
		}
	}
	if p.serverLimit > 0 && limit > p.serverLimit {
		limit = p.serverLimit
	}

	// IS-04 orders a collection by `version`, NEWEST FIRST, and the
	// window is described by paging.since / paging.until. Both bounds
	// are exclusive here, and both directions are servable:
	//
	//	paging.until=V  the `limit` resources STRICTLY OLDER than V,
	//	                which is how a descending walk moves back.
	//	paging.since=V  the `limit` resources STRICTLY NEWER than V,
	//	                taken from the OLDEST end, which is how an
	//	                ascending walk moves forward.
	//
	// Resource i has version 1000+i, so the newest is total-1.
	lo, hi := 0, p.total-1
	ascending := false
	if p.mode == modeEmptyAscending && r.URL.Query().Get("paging.since") != "" {
		// The defect: an empty window, whatever was asked for.
		w.Header().Set("X-Paging-Limit", strconv.Itoa(limit))
		w.Header().Set("X-Paging-Since", "0:0")
		w.Header().Set("X-Paging-Until", "0:0")
		w.Header().Set("Link", fmt.Sprintf(
			`<%s?paging.since=0:0>; rel="next", <%s?paging.until=0:0>; rel="prev"`,
			r.URL.Path, r.URL.Path))
		writeJSON(w, []any{})
		return
	}
	if p.mode != modeIgnoreSince {
		if until := r.URL.Query().Get("paging.until"); until != "" {
			if sec, _, ok := splitTAI(until); ok {
				hi = int(sec) - 1000 - 1
			}
		}
		if since := r.URL.Query().Get("paging.since"); since != "" {
			ascending = true
			if sec, _, ok := splitTAI(since); ok {
				if n := int(sec) - 1000 + 1; n > lo {
					lo = n
				}
			}
		}
	}

	// Which end of the window the page comes from depends on the
	// direction asked for; the BODY is newest-first either way.
	first, last := hi, hi-limit+1
	if ascending {
		first, last = lo+limit-1, lo
		if first > hi {
			first = hi
		}
	}
	if last < lo {
		last = lo
	}
	out := []any{}
	for i := first; i >= last && i >= 0 && i <= p.total-1; i-- {
		out = append(out, nodeAt(i))
	}

	newest, oldest := first, last
	switch p.mode {
	case modeLink, modeBrokenPrev:
		// A real registry emits BOTH members on every response,
		// including the last — so the presence of `next` says nothing
		// about whether more resources exist.
		next := fmt.Sprintf(`<%s?paging.since=%d:0>; rel="next"`, r.URL.Path, 1000+newest)
		prev := fmt.Sprintf(`<%s?paging.until=%d:0>; rel="prev"`, r.URL.Path, 1000+oldest)
		if p.mode == modeBrokenPrev {
			// The defect, verbatim from the field capture: a prev
			// cursor with nothing in it.
			prev = fmt.Sprintf(`<%s?paging.until=>; rel="prev"`, r.URL.Path)
		}
		if len(out) > 0 {
			w.Header().Set("Link", next+", "+prev)
		} else {
			w.Header().Set("Link", next)
		}
		if p.mode == modeBrokenPrev {
			w.Header().Set("X-Paging-Limit", strconv.Itoa(limit))
			w.Header().Set("X-Paging-Since", "")
			w.Header().Set("X-Paging-Until", "0:0")
		}
	case modeHeaders, modeIgnoreSince:
		w.Header().Set("X-Paging-Limit", strconv.Itoa(limit))
		if len(out) > 0 {
			w.Header().Set("X-Paging-Since", fmt.Sprintf("%d:0", 1000+oldest))
			w.Header().Set("X-Paging-Until", fmt.Sprintf("%d:0", 1000+newest))
		}
	case modeBare:
		// nothing at all
	}
	writeJSON(w, out)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func exportNodes(t *testing.T, p *pagingRegistry) (*Result, string) {
	t.Helper()
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)

	opts := baseOpts(t, strings.TrimPrefix(srv.URL, "http://"))
	got, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return got, readReport(t, got.Dir)
}

// TestPagingWithoutLinkHeader is the field failure that prompted this:
// a registry serving ten per page with no `Link: rel="next"` ended the
// walk after one page and reported a ten-node plant.
func TestPagingWithoutLinkHeader(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode pagingMode
	}{
		{"Link header (AMWA reference)", modeLink},
		{"X-Paging-* only, no Link", modeHeaders},
		{"no paging headers at all", modeBare},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// serverLimit 10 makes the registry clamp to ten per page
			// no matter what we ask for — the observed field shape.
			p := &pagingRegistry{total: 44, limit: 10, serverLimit: 10, mode: tc.mode}
			got, rep := exportNodes(t, p)
			if got.NodesSeen != 44 {
				t.Errorf("captured %d of 44 nodes\n%s", got.NodesSeen, rep)
			}
		})
	}
}

// TestPagingAsksForABigPage: left to itself a registry applies its own
// default, and the walk costs a round trip per ten resources.
func TestPagingAsksForABigPage(t *testing.T) {
	p := &pagingRegistry{total: 44, limit: 10, mode: modeLink}
	got, rep := exportNodes(t, p)
	if got.NodesSeen != 44 {
		t.Fatalf("captured %d of 44\n%s", got.NodesSeen, rep)
	}
	// 44 nodes at the requested 100 per page is one page, plus the
	// empty page that ends the walk.
	if p.requests > 3 {
		t.Errorf("took %d requests for 44 nodes; paging.limit was not honoured", p.requests)
	}
	if !strings.Contains(rep, "paging.limit") && !strings.Contains(rep, "PAGING") {
		t.Errorf("the paging decision was not recorded:\n%s", rep)
	}
}

// TestPagingEvidenceIsRecorded: a short capture in the field has to be
// diagnosable from report.txt alone. Not recording this is why the
// original failure needed a guess.
func TestPagingEvidenceIsRecorded(t *testing.T) {
	p := &pagingRegistry{total: 5, limit: 10, mode: modeHeaders}
	_, rep := exportNodes(t, p)
	for _, want := range []string{"PAGING", "X-Paging-Limit=", "Link="} {
		if !strings.Contains(rep, want) {
			t.Errorf("report.txt is missing %q:\n%s", want, rep)
		}
	}
}

// TestPagingStopsOnIgnoredCursor: a registry that serves the same page
// whatever cursor it is given must stop the walk and be recorded, not
// loop and not silently truncate.
func TestPagingStopsOnIgnoredCursor(t *testing.T) {
	p := &pagingRegistry{total: 44, limit: 10, serverLimit: 10, mode: modeIgnoreSince}
	got, rep := exportNodes(t, p)
	if !strings.Contains(rep, "paging cursor did not advance") {
		t.Errorf("a stuck cursor was not recorded:\n%s", rep)
	}
	// It captured what it could rather than nothing.
	if got.NodesSeen == 0 {
		t.Error("the walk should keep the page it did get")
	}
	if p.requests > 10 {
		t.Errorf("%d requests — the walk did not stop", p.requests)
	}
}

// TestBrokenPrevCursorWalksForwardInstead is the field defect, on a
// registry modelled from the capture that exposed it.
//
// One real registry answers /flows — and only /flows — with a blank
// X-Paging-Since and a rel="prev" whose paging.until is empty. The
// descending walk follows that cursor, gets page one back, and stops:
// 100 flows of 5,168, and every sender in the plant then appeared to
// reference a flow that did not exist. The forward cursor on the same
// endpoint is sound, so walking the collection oldest-first completes.
//
// IS-04 §7 admits both enumerations. Preferring the ascending one, and
// only falling back to descending, is what makes this registry
// capturable at all.
func TestBrokenPrevCursorWalksForwardInstead(t *testing.T) {
	p := &pagingRegistry{total: 250, limit: 100, mode: modeBrokenPrev}
	got, rep := exportNodes(t, p)
	if got.NodesSeen != 250 {
		t.Errorf("captured %d of 250 — the broken prev cursor was not routed around\n%s", got.NodesSeen, rep)
	}
	if strings.Contains(rep, "cursor did not advance") {
		t.Errorf("the ascending walk should never have touched the broken cursor:\n%s", rep)
	}
	if !strings.Contains(rep, "walk=ascending") {
		t.Errorf("report should record which direction was walked:\n%s", rep)
	}
}

// TestEmptyAscendingPageFallsBackToDescending is the regression that
// cost a real capture its entire flows collection.
//
// The registry answers /flows ascending with an empty window — Since
// and Until both 0:0, zero rows — while descending still returns
// resources. Treating an empty FIRST page as "the collection is empty"
// reported 0 flows and never asked the other direction, which is worse
// than the 100 the descending-only walk had been getting.
//
// An empty page after a full one still ends the walk normally; it is
// only the first one that has to be checked against the other
// direction.
func TestEmptyAscendingPageFallsBackToDescending(t *testing.T) {
	p := &pagingRegistry{total: 250, limit: 100, mode: modeEmptyAscending}
	got, rep := exportNodes(t, p)
	if got.NodesSeen != 250 {
		t.Errorf("captured %d of 250 — an empty ascending page was taken as the truth\n%s", got.NodesSeen, rep)
	}
	if !strings.Contains(rep, "[descending]") {
		t.Errorf("the fallback should have run:\n%s", rep)
	}
}

// TestGenuinelyEmptyCollectionCostsOneExtraRequest: the price of not
// trusting an empty first page. A collection that really is empty is
// walked in both directions — two requests, not one — and still
// reports empty. Cheap insurance, but it must not be more than that.
func TestGenuinelyEmptyCollectionCostsOneExtraRequest(t *testing.T) {
	p := &pagingRegistry{total: 0, limit: 100, mode: modeLink}
	got, rep := exportNodes(t, p)
	if got.NodesSeen != 0 {
		t.Errorf("an empty registry should list no nodes, got %d\n%s", got.NodesSeen, rep)
	}
	if p.requests != 2 {
		t.Errorf("an empty collection should cost 2 requests (one per direction), took %d\n%s", p.requests, rep)
	}
}

// TestAscendingIsPreferred: on a healthy registry the ascending walk
// completes on its own, and the descending fallback never runs. The
// fallback is insurance, not a second pass over every collection.
func TestAscendingIsPreferred(t *testing.T) {
	p := &pagingRegistry{total: 250, limit: 100, mode: modeLink}
	got, rep := exportNodes(t, p)
	if got.NodesSeen != 250 {
		t.Errorf("captured %d of 250\n%s", got.NodesSeen, rep)
	}
	// Scoped to the populated collection: the fake's other collections
	// are genuinely empty, and an empty collection is checked in both
	// directions by design.
	for _, line := range strings.Split(rep, "\n") {
		if strings.Contains(line, "/nodes") && strings.Contains(line, "[descending]") {
			t.Errorf("a healthy registry must not need the fallback:\n%s", rep)
			break
		}
	}
}

// TestPagingCursorRewrittenToTarget: a registry behind Docker or NAT
// advertises its own internal address in the Link header. The cursor's
// query string is the paging state; the authority is ours to supply.
func TestPagingCursorRewrittenToTarget(t *testing.T) {
	h := &harvester{target: "10.44.55.56:8080", scheme: "http"}
	got := h.pagingURL(
		"http://10.44.55.56:8080/x-nmos/query/v1.3/nodes?paging.limit=100",
		"http://172.17.0.3:8080/x-nmos/query/v1.3/nodes?paging.since=1787607060:080832800")
	want := "http://10.44.55.56:8080/x-nmos/query/v1.3/nodes?paging.since=1787607060:080832800"
	if got != want {
		t.Errorf("pagingURL =\n  %s\nwant\n  %s", got, want)
	}
	if len(h.report) == 0 || !strings.Contains(h.report[0], "172.17.0.3:8080") {
		t.Errorf("the rewrite must be recorded, got %v", h.report)
	}
	// A cursor already on our host is passed through untouched, and
	// silently — the note would otherwise fire on every page.
	h2 := &harvester{target: "10.44.55.56:8080", scheme: "http"}
	same := "http://10.44.55.56:8080/x-nmos/query/v1.3/nodes?paging.since=1:0"
	if got := h2.pagingURL(same, same); got != same {
		t.Errorf("pagingURL rewrote a same-host cursor: %s", got)
	}
	if len(h2.report) != 0 {
		t.Errorf("no note expected for a same-host cursor, got %v", h2.report)
	}
}

// TestMaxVersionIsTheAscendingMirror guards the direction of the
// data-derived cursor. minVersion sends the walk back, maxVersion
// sends it forward; swapping them re-serves one page forever.
func TestMaxVersionIsTheAscendingMirror(t *testing.T) {
	page := []json.RawMessage{
		json.RawMessage(`{"version":"1005:0"}`),
		json.RawMessage(`{"version":"1001:0"}`),
		json.RawMessage(`{"version":"not-a-version"}`),
		json.RawMessage(`{"version":"1009:0"}`),
	}
	if got := maxVersion(page); got != "1009:0" {
		t.Errorf("maxVersion = %q, want 1009:0", got)
	}
	if got := minVersion(page); got != "1001:0" {
		t.Errorf("minVersion = %q, want 1001:0", got)
	}
	if got := maxVersion(nil); got != "" {
		t.Errorf("maxVersion(nil) = %q, want empty", got)
	}
}

// TestShortPageEndsTheWalk: without a Link header, a page shorter than
// the requested limit is the only safe end-of-collection signal.
func TestShortPageEndsTheWalk(t *testing.T) {
	p := &pagingRegistry{total: 7, limit: 100, mode: modeBare}
	got, rep := exportNodes(t, p)
	if got.NodesSeen != 7 {
		t.Errorf("captured %d of 7\n%s", got.NodesSeen, rep)
	}
	// With no Link and no X-Paging-Limit there is no evidence of what
	// the registry applied, so the walk probes one more page rather
	// than guessing the collection ended. One wasted request is the
	// price of never truncating a plant silently.
	if p.requests > 2 {
		t.Errorf("a short first page should end the walk in at most 2 requests; took %d", p.requests)
	}
}

func TestMinVersionAndTAIOrdering(t *testing.T) {
	items := []json.RawMessage{
		json.RawMessage(`{"version":"1000:5"}`),
		json.RawMessage(`{"version":"1000:900"}`),
		json.RawMessage(`{"version":"999:999999999"}`),
		json.RawMessage(`{"version":"not-a-version"}`),
		json.RawMessage(`{"no":"version"}`),
	}
	if got := minVersion(items); got != "999:999999999" {
		t.Errorf("minVersion = %q, want 999:999999999", got)
	}
	if minVersion(nil) != "" {
		t.Error("minVersion of nothing should be empty")
	}
	// A malformed version must never win, or the cursor jumps.
	if taiLess("1000:0", "garbage") {
		t.Error("a malformed version must not outrank a valid one")
	}
	if !taiLess("garbage", "1000:0") {
		t.Error("a valid version must outrank a malformed one")
	}
	if !taiLess("999:0", "1000:0") || taiLess("1000:0", "999:0") {
		t.Error("seconds must dominate the comparison")
	}
}

// sdpNode serves a sender whose SDP is published at both its IS-04
// manifest_href and its IS-05 transportfile.
type sdpNode struct {
	is04SDP string
	is05SDP string
}

const sdpSenderID = "44444444-4444-4444-8444-444444444444"

func (n *sdpNode) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/x-nmos":
		writeJSON(w, []string{"node/", "connection/"})
	case "/x-nmos/node/":
		writeJSON(w, []string{"v1.3/"})
	case "/x-nmos/node/v1.3/self":
		writeJSON(w, map[string]any{"id": "11111111-1111-4111-8111-111111111111", "version": "1:0", "label": "n"})
	case "/x-nmos/node/v1.3/senders":
		writeJSON(w, []any{map[string]any{
			"id": sdpSenderID, "version": "1:0", "label": "s", "tags": map[string]any{},
			"transport": "urn:x-nmos:transport:rtp.mcast", "device_id": "33333333-3333-4333-8333-333333333333",
			"manifest_href": "/x-nmos/node/v1.3/sdp/" + sdpSenderID,
		}})
	case "/x-nmos/node/v1.3/sdp/" + sdpSenderID:
		w.Header().Set("Content-Type", "application/sdp")
		_, _ = w.Write([]byte(n.is04SDP))
	case "/x-nmos/connection/":
		writeJSON(w, []string{"v1.1/"})
	case "/x-nmos/connection/v1.1/single/senders":
		writeJSON(w, []string{sdpSenderID + "/"})
	case "/x-nmos/connection/v1.1/single/senders/" + sdpSenderID + "/transportfile":
		w.Header().Set("Content-Type", "application/sdp")
		_, _ = w.Write([]byte(n.is05SDP))
	case "/x-nmos/connection/v1.1/single/receivers":
		writeJSON(w, []any{})
	default:
		if strings.HasPrefix(r.URL.Path, "/x-nmos/node/v1.3/") || strings.HasPrefix(r.URL.Path, "/x-nmos/connection/") {
			writeJSON(w, []any{})
			return
		}
		http.NotFound(w, r)
	}
}

// TestSenderSDPPublishedTwice: a sender publishes its SDP at the IS-04
// manifest_href AND the IS-05 transportfile. Writing both when they
// agree doubled the SDP folder for nothing — 366 files where 190 would
// do on one real device. Fetching both is still right, because a node
// publishing two different descriptions of one stream is a real fault
// and only comparing them finds it.
func TestSenderSDPPublishedTwice(t *testing.T) {
	const body = "v=0\r\no=- 1 1 IN IP4 198.51.100.5\r\ns=one\r\n"

	t.Run("identical - one file per source, agreement recorded", func(t *testing.T) {
		srv := httptest.NewServer(&sdpNode{is04SDP: body, is05SDP: body})
		defer srv.Close()
		got, err := Run(context.Background(), baseOpts(t, strings.TrimPrefix(srv.URL, "http://")))
		if err != nil {
			t.Fatal(err)
		}
		// One file per publication point, in its own folder, named by
		// the resource id alone. `diff -r sdp/is04 sdp/is05` is then the
		// disagreement check.
		if got.SDPFiles != 2 {
			t.Errorf("wrote %d SDP files, want one per source", got.SDPFiles)
		}
		for _, rel := range []string{
			filepath.Join("is04", sdpSenderID+".sdp"),
			filepath.Join("is05", sdpSenderID+".sdp"),
		} {
			if _, err := os.Stat(filepath.Join(got.Dir, "sdp", rel)); err != nil {
				t.Errorf("%s missing: %v", rel, err)
			}
		}
		rep := readReport(t, got.Dir)
		if !strings.Contains(rep, "matches") {
			t.Errorf("the agreement was not recorded:\n%s", rep)
		}
		if strings.Contains(rep, "DIFFERENT SDP") {
			t.Error("identical SDPs were reported as differing")
		}
	})

	t.Run("different - both kept, disagreement warned", func(t *testing.T) {
		srv := httptest.NewServer(&sdpNode{
			is04SDP: body,
			is05SDP: "v=0\r\no=- 2 2 IN IP4 198.51.100.5\r\ns=OTHER\r\n",
		})
		defer srv.Close()
		got, err := Run(context.Background(), baseOpts(t, strings.TrimPrefix(srv.URL, "http://")))
		if err != nil {
			t.Fatal(err)
		}
		if got.SDPFiles != 2 {
			t.Errorf("wrote %d SDP files, want 2 when the sources disagree", got.SDPFiles)
		}
		rep := readReport(t, got.Dir)
		if !strings.Contains(rep, "DIFFERENT SDP") {
			t.Errorf("the disagreement was not reported:\n%s", rep)
		}
		// Both copies survive, one per source folder, so the evidence
		// for the finding is still on disk.
		for _, rel := range []string{
			filepath.Join("is04", sdpSenderID+".sdp"),
			filepath.Join("is05", sdpSenderID+".sdp"),
		} {
			if _, err := os.Stat(filepath.Join(got.Dir, "sdp", rel)); err != nil {
				t.Errorf("%s was not kept: %v", rel, err)
			}
		}
	})
}

// identityNode serves a node with a chosen hostname and label, so the
// folder-naming rules can be exercised.
type identityNode struct{ hostname, label string }

func (n *identityNode) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/x-nmos":
		writeJSON(w, []string{"node/"})
	case "/x-nmos/node/":
		writeJSON(w, []string{"v1.3/"})
	case "/x-nmos/node/v1.3/self":
		writeJSON(w, map[string]any{
			"id": "11111111-1111-4111-8111-111111111111", "version": "1:0",
			"label": n.label, "hostname": n.hostname,
		})
	default:
		writeJSON(w, []any{})
	}
}

// TestFolderCarriesHostnameAndLabel: `10_41_40_80_3000` tells an
// operator nothing six months later. Both hostname and label are
// appended because they are frequently different and both are how
// someone searches — DNS knows one, the operator typed the other.
func TestFolderCarriesHostnameAndLabel(t *testing.T) {
	for _, tc := range []struct {
		name     string
		hostname string
		label    string
		wantHas  []string
		wantNot  []string
	}{
		// The label wins: it is what an operator typed and searches for.
		// The full identity — hostname, id, APIs — is in manifest.json,
		// which is what keeps the folder short enough to copy.
		{"both - label wins", "bm-n-nnbrg-t01", "Nevion Virtuoso 1",
			[]string{"Nevion_Virtuoso_1"}, []string{"bm_n_nnbrg_t01"}},
		{"identical - not repeated", "cam-01", "cam-01",
			[]string{"cam_01"}, []string{"cam_01__cam_01"}},
		{"hostname only - used as fallback", "cam-02", "",
			[]string{"cam_02"}, nil},
		{"label only", "", "cam-03",
			[]string{"cam_03"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(&identityNode{hostname: tc.hostname, label: tc.label})
			defer srv.Close()
			got, err := Run(context.Background(), baseOpts(t, strings.TrimPrefix(srv.URL, "http://")))
			if err != nil {
				t.Fatal(err)
			}
			base := filepath.Base(got.Dir)
			for _, want := range tc.wantHas {
				if !strings.Contains(base, want) {
					t.Errorf("folder %q does not carry %q", base, want)
				}
			}
			for _, not := range tc.wantNot {
				if strings.Contains(base, not) {
					t.Errorf("folder %q repeats itself: %q", base, not)
				}
			}
			// The address stays, because it is the only unambiguous part.
			if !strings.Contains(base, "127_0_0_1") {
				t.Errorf("folder %q lost the address", base)
			}
			// And the folder the Result names must be the one on disk.
			if _, err := os.Stat(got.Dir); err != nil {
				t.Errorf("Result.Dir does not exist after the rename: %v", err)
			}
		})
	}
}

// TestAnonymousDeviceKeepsAddressOnlyFolder: a device that names itself
// nothing keeps the address-only folder rather than gaining a trailing
// separator.
func TestAnonymousDeviceKeepsAddressOnlyFolder(t *testing.T) {
	srv := httptest.NewServer(&identityNode{})
	defer srv.Close()
	got, err := Run(context.Background(), baseOpts(t, strings.TrimPrefix(srv.URL, "http://")))
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(filepath.Base(got.Dir), "__") {
		t.Errorf("folder %q gained an empty identity suffix", filepath.Base(got.Dir))
	}
}

// TestManifestIndexesThePlant: the manifest is the index someone opens
// first, and it is what lets folder names stay short enough to copy.
func TestManifestIndexesThePlant(t *testing.T) {
	nodeSrv := httptest.NewServer(&identityNode{hostname: "neuron-0003", label: "bm-n-nnbrg-t01"})
	defer nodeSrv.Close()

	reg := &pagingRegistry{total: 0, limit: 100, mode: modeLink}
	regSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/x-nmos/query/v1.3/nodes" {
			writeJSON(w, []any{map[string]any{
				"id": "11111111-1111-4111-8111-111111111111", "version": "1000:0",
				"label": "bm-n-nnbrg-t01", "href": nodeSrv.URL + "/",
			}})
			return
		}
		reg.ServeHTTP(w, r)
	}))
	defer regSrv.Close()

	out := t.TempDir()
	opts := baseOpts(t, strings.TrimPrefix(regSrv.URL, "http://"))
	opts.Out = out
	if _, err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(filepath.Join(out, "manifest.json"))
	if err != nil {
		t.Fatalf("no manifest at the capture root: %v", err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}
	if len(m.Devices) != 2 {
		t.Fatalf("manifest lists %d devices, want registry + node", len(m.Devices))
	}
	// Registry first — the order someone reads it in.
	if m.Devices[0].Role != "registry" {
		t.Errorf("first entry is %q, want the registry", m.Devices[0].Role)
	}

	node := m.Devices[1]
	for name, got := range map[string]string{
		"role": node.Role, "hostname": node.Hostname, "label": node.Label,
		"host": node.Host, "port": node.Port, "folder": node.Folder, "id": node.ID,
	} {
		if got == "" {
			t.Errorf("manifest entry has no %s", name)
		}
	}
	if node.Hostname != "neuron-0003" || node.Label != "bm-n-nnbrg-t01" {
		t.Errorf("identity not carried: hostname=%q label=%q", node.Hostname, node.Label)
	}
	if len(node.APIs) == 0 {
		t.Error("manifest does not record which APIs the node serves")
	}
	// The folder must be RELATIVE, so the capture survives being moved.
	if filepath.IsAbs(node.Folder) {
		t.Errorf("folder %q is absolute; the manifest would break on a copy", node.Folder)
	}
	if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(node.Folder))); err != nil {
		t.Errorf("manifest points at a folder that is not there: %v", err)
	}
}

// TestRawIsOptIn: raw/ duplicates tree.json and was 14,719 of the
// 27,299 files on a real plant — and held every path over the Windows
// 260-character limit.
func TestRawIsOptIn(t *testing.T) {
	newSrv := func() *httptest.Server {
		return httptest.NewServer(&identityNode{hostname: "h", label: "l"})
	}

	srv := newSrv()
	defer srv.Close()
	got, err := Run(context.Background(), baseOpts(t, strings.TrimPrefix(srv.URL, "http://")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(got.Dir, "raw")); err == nil {
		t.Error("raw/ was written without --raw")
	}
	// tree.json still carries every body, so nothing is lost.
	if _, err := os.Stat(filepath.Join(got.Dir, "tree.json")); err != nil {
		t.Errorf("tree.json missing: %v", err)
	}

	srv2 := newSrv()
	defer srv2.Close()
	opts := baseOpts(t, strings.TrimPrefix(srv2.URL, "http://"))
	opts.Raw = true
	got2, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(got2.Dir, "raw")); err != nil {
		t.Errorf("--raw did not write raw/: %v", err)
	}
}

// TestFollowedNodesAreNotStampedTwice: a followed node already sits
// inside a stamped registry folder, so stamping it again adds 16
// characters to every path below it and says nothing new.
func TestFollowedNodesAreNotStampedTwice(t *testing.T) {
	nodeSrv := httptest.NewServer(&identityNode{hostname: "h", label: "cam-01"})
	defer nodeSrv.Close()

	reg := &pagingRegistry{total: 0, limit: 100, mode: modeLink}
	regSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/x-nmos/query/v1.3/nodes" {
			writeJSON(w, []any{map[string]any{
				"id": "11111111-1111-4111-8111-111111111111", "version": "1000:0",
				"label": "cam-01", "href": nodeSrv.URL + "/",
			}})
			return
		}
		reg.ServeHTTP(w, r)
	}))
	defer regSrv.Close()

	opts := baseOpts(t, strings.TrimPrefix(regSrv.URL, "http://"))
	opts.NoStamp = false // the registry IS stamped
	got, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(filepath.Base(got.Dir), "_20260824-") {
		t.Errorf("the top-level capture should be stamped: %q", filepath.Base(got.Dir))
	}
	if len(got.Followed) != 1 {
		t.Fatalf("followed %d nodes", len(got.Followed))
	}
	child := filepath.Base(got.Followed[0].Dir)
	if strings.Contains(child, "_20260824-") {
		t.Errorf("followed node was stamped again: %q", child)
	}
	if !strings.Contains(child, "cam_01") {
		t.Errorf("followed node lost its identity: %q", child)
	}
}

// TestNodesAreCapturedConcurrently: each node is ~600 requests against
// a different device, so the walk is latency-bound and parallelises
// almost perfectly. In series, 44 nodes took four minutes.
//
// Run this with -race in CI; it is the only place the worker pool,
// the shared visit tracker and the per-node harvesters meet.
func TestNodesAreCapturedConcurrently(t *testing.T) {
	const (
		nodes   = 12
		perReq  = 60 * time.Millisecond
		workers = 6
	)

	// One server per node, each slow, so the walk is latency-bound the
	// way a real plant is.
	list := make([]any, 0, nodes)
	for i := 0; i < nodes; i++ {
		n := &identityNode{hostname: fmt.Sprintf("h%02d", i), label: fmt.Sprintf("node-%02d", i)}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(perReq)
			n.ServeHTTP(w, r)
		}))
		t.Cleanup(srv.Close)
		list = append(list, map[string]any{
			"id":      fmt.Sprintf("%08d-1111-4111-8111-111111111111", i),
			"version": fmt.Sprintf("%d:0", 1000+i),
			"label":   n.label,
			"href":    srv.URL + "/",
		})
	}

	reg := &pagingRegistry{total: 0, limit: 100, mode: modeLink}
	regSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/x-nmos/query/v1.3/nodes" {
			writeJSON(w, list)
			return
		}
		reg.ServeHTTP(w, r)
	}))
	defer regSrv.Close()

	opts := baseOpts(t, strings.TrimPrefix(regSrv.URL, "http://"))
	opts.Workers = workers

	start := time.Now()
	got, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)

	if len(got.Followed) != nodes {
		t.Fatalf("followed %d of %d nodes", len(got.Followed), nodes)
	}
	// Results keep the registry's order regardless of completion order,
	// so a capture is reproducible.
	for i, f := range got.Followed {
		want := fmt.Sprintf("node-%02d", i)
		if f.Label != want {
			t.Errorf("result %d is %q, want %q — the pool reordered them", i, f.Label, want)
		}
	}

	// Each node costs at least a handful of sequential requests. Serial
	// would be nodes x that; with `workers` running at once it should be
	// close to nodes/workers of it. The bound is loose on purpose — this
	// asserts "parallel", not a stopwatch.
	serialFloor := time.Duration(nodes) * 5 * perReq
	if elapsed > serialFloor {
		t.Errorf("capture took %s, serial would be about %s — the pool is not running nodes in parallel",
			elapsed.Round(time.Millisecond), serialFloor)
	}
}

func TestWorkersDefault(t *testing.T) {
	if got := (Options{}).workers(); got != defaultWorkers {
		t.Errorf("default workers = %d, want %d", got, defaultWorkers)
	}
	if got := (Options{Workers: 3}).workers(); got != 3 {
		t.Errorf("explicit workers = %d, want 3", got)
	}
}

func TestVisitTrackerClaimsOnce(t *testing.T) {
	v := newVisitTracker()
	if !v.claim("a:1") {
		t.Error("first claim should succeed")
	}
	if v.claim("a:1") {
		t.Error("second claim of the same target must fail")
	}
	if !v.claim("b:1") {
		t.Error("a different target should claim")
	}
}

func TestIdentitySuffix(t *testing.T) {
	// One part, not two: the folder recognises a device, the manifest
	// identifies it. Two parts produced 76-character folders and 329
	// paths past the Windows limit on one real plant.
	if got := identitySuffix("host", "label"); got != "label" {
		t.Errorf("identitySuffix = %q, want the label", got)
	}
	if got := identitySuffix("host", ""); got != "host" {
		t.Errorf("the hostname is the fallback, got %q", got)
	}
	if got := identitySuffix("", ""); got != "" {
		t.Errorf("identitySuffix of nothing = %q", got)
	}
	// Windows MAX_PATH is 260 and these folders nest one level under a
	// registry capture; a label that is a sentence has produced
	// unopenable paths before.
	long := strings.Repeat("x", 200)
	if got := identitySuffix("", long); len(got) > 24 {
		t.Errorf("a long label was not capped: %d chars", len(got))
	}
}

func TestWithQuery(t *testing.T) {
	got := withQuery("http://h/x-nmos/query/v1.3/nodes", "paging.limit", "100")
	if !strings.Contains(got, "paging.limit=100") {
		t.Errorf("withQuery = %q", got)
	}
	// Replacing, not appending — two limits would be ambiguous.
	got = withQuery(got, "paging.limit", "50")
	if strings.Count(got, "paging.limit") != 1 || !strings.Contains(got, "paging.limit=50") {
		t.Errorf("withQuery did not replace: %q", got)
	}
	if got := withQuery(":://bad", "a", "b"); got != ":://bad" {
		t.Errorf("an unparseable URL should pass through, got %q", got)
	}
}
