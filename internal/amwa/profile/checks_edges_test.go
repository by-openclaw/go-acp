package profile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// reply is one scripted answer. The scripted handler exists for the
// response shapes the device mock cannot produce without a knob per
// shape: a fault on the root, an API whose index is not a list, a
// downgrade that returns fewer rows, a body the client cannot finish
// reading.
type reply struct {
	status int
	body   string
	ct     string // "" means application/json
	// truncate declares more Content-Length than is written, so the
	// client's body read fails after the status arrived. This is the
	// only way to get a request error that is the same on every
	// platform: a closed socket is retried by the transport, a short
	// body is not.
	truncate bool
}

// scripted maps a path — or a path plus its exact query string — to a
// reply. A path-plus-query key wins over the bare path, so one path can
// answer differently to ?paging.limit and ?query.downgrade. Anything
// unlisted is a 404.
type scripted map[string]reply

func (s scripted) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	rep, ok := s[r.URL.Path+"?"+r.URL.RawQuery]
	if !ok {
		rep, ok = s[r.URL.Path]
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	ct := rep.ct
	if ct == "" {
		ct = "application/json"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if rep.truncate {
		w.Header().Set("Content-Length", strconv.Itoa(len(rep.body)+64))
	}
	status := rep.status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write([]byte(rep.body))
}

// registryScript is a spec-correct registry serving one query minor
// and a registration API. Tests overlay the one answer they break.
func registryScript(over scripted) scripted {
	s := scripted{
		"/x-nmos":                  {body: `["query/","registration/"]`},
		"/x-nmos/query/":           {body: `["v1.3/"]`},
		"/x-nmos/registration/":    {body: `["v1.3/"]`},
		"/x-nmos/query/v1.3/":      {body: `["nodes/"]`},
		"/x-nmos/query/v1.3":       {body: `["nodes/"]`},
		"/x-nmos/query/v1.3/nodes": {body: `[{"id":"11111111-1111-4111-8111-111111111111"}]`},
		"/x-nmos/query/v1.3/nodes/00000000-0000-4000-8000-000000000000": {
			status: http.StatusNotFound, body: `{"code":404,"error":"not found","debug":null}`},
		"/x-nmos/registration/v1.3/health/nodes/00000000-0000-4000-8000-000000000000": {
			status: http.StatusNotFound, body: `{"code":404,"error":"not found","debug":null}`},
	}
	for k, v := range over {
		s[k] = v
	}
	return s
}

func runScripted(t *testing.T, s scripted) *Report {
	t.Helper()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	rep, err := Run(context.Background(), Options{
		Target:  strings.TrimPrefix(srv.URL, "http://"),
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return rep
}

// detailOf returns the detail of the first result with the given id.
func detailOf(t *testing.T, r *Report, id string) string {
	t.Helper()
	for _, res := range r.Results {
		if res.ID == id {
			return res.Detail
		}
	}
	t.Fatalf("no result with id %s; got %v", id, idsOf(r))
	return ""
}

func wantDetail(t *testing.T, r *Report, id string, st Status, word string) {
	t.Helper()
	want(t, r, id, st)
	if d := detailOf(t, r, id); !strings.Contains(d, word) {
		t.Errorf("%s detail %q should say %q", id, d, word)
	}
}

// TestRootFault: a root that answers but not with 200 is a failure
// naming the status, distinct from a root that does not answer.
func TestRootFault(t *testing.T) {
	rep := runScripted(t, scripted{"/x-nmos": {status: http.StatusInternalServerError, body: "boom", ct: "text/plain"}})
	wantDetail(t, rep, "PROFILE-ROOT-001", StatusFail, "returned 500, want 200")
}

// TestRootListsUnusableAPIs: an API named at /x-nmos whose own index
// does not answer, or answers with something that is not a version
// list, is discovered with no usable versions and reported as such.
// An empty name is not an API and is ignored.
func TestRootListsUnusableAPIs(t *testing.T) {
	rep := runScripted(t, registryScript(scripted{
		"/x-nmos":       {body: `["/","ghost/","blob/","query/"]`},
		"/x-nmos/blob/": {body: `{"not":"a list"}`},
	}))
	var got []Result
	for _, res := range rep.Results {
		if res.ID == "PROFILE-VER-001" {
			got = append(got, res)
		}
	}
	if len(got) != 3 {
		t.Fatalf("want one version result per named API (blob, ghost, query), got %v", idsOf(rep))
	}
	for _, res := range got[:2] {
		if res.Status != StatusFail || !strings.Contains(res.Detail, "no usable version list") {
			t.Errorf("%s: %+v, want FAIL for an unusable version list", res.ID, res)
		}
	}
	if got[2].Status != StatusPass {
		t.Errorf("query: %+v, want PASS", got[2])
	}
}

// TestUnknownVersionAnswers: the spec's answer is 404. A fault is a
// failure; another 4xx is tolerated with a warning; a request that
// does not complete is a failure that says so.
func TestUnknownVersionAnswers(t *testing.T) {
	const path = "/x-nmos/query/v9.9/"
	for _, tc := range []struct {
		name string
		rep  reply
		want Status
		word string
	}{
		{"fault", reply{status: http.StatusInternalServerError}, StatusFail, "must 404, not fault"},
		{"other client error", reply{status: http.StatusBadRequest}, StatusWarn, "404 is the spec's answer"},
		{"incomplete", reply{body: "[]", truncate: true}, StatusFail, "did not complete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rep := runScripted(t, registryScript(scripted{path: tc.rep}))
			wantDetail(t, rep, "PROFILE-VER-002", tc.want, tc.word)
		})
	}
}

// TestUnknownResourceOnNodeAPIIsSkipped: the Node API has no per-id
// path the probe can ask for without guessing, so it stands down.
func TestUnknownResourceOnNodeAPIIsSkipped(t *testing.T) {
	rep := runScripted(t, scripted{
		"/x-nmos":       {body: `["node/"]`},
		"/x-nmos/node/": {body: `["v1.3/"]`},
	})
	wantDetail(t, rep, "PROFILE-404-001", StatusSkip, "node API")
}

// TestUnknownResourceAnswers covers the answers other than 404 and 500,
// and a 404 whose body is JSON but not the IS-04 error object.
func TestUnknownResourceAnswers(t *testing.T) {
	const path = "/x-nmos/query/v1.3/nodes/00000000-0000-4000-8000-000000000000"
	t.Run("200 for a resource that does not exist", func(t *testing.T) {
		rep := runScripted(t, registryScript(scripted{path: {body: `{}`}}))
		wantDetail(t, rep, "PROFILE-404-001", StatusFail, "does not exist")
	})
	t.Run("incomplete", func(t *testing.T) {
		rep := runScripted(t, registryScript(scripted{path: {body: `{}`, truncate: true}}))
		wantDetail(t, rep, "PROFILE-404-001", StatusFail, "did not complete")
	})
	t.Run("404 body without code and error", func(t *testing.T) {
		rep := runScripted(t, registryScript(scripted{path: {status: http.StatusNotFound, body: `{"debug":null}`}}))
		want(t, rep, "PROFILE-404-001", StatusPass)
		wantDetail(t, rep, "PROFILE-404-002", StatusWarn, "omits code and/or error")
	})
}

// TestTrailingSlashRequestError: if either form cannot be fetched the
// comparison cannot be made, and that is a failure of the device.
func TestTrailingSlashRequestError(t *testing.T) {
	rep := runScripted(t, registryScript(scripted{"/x-nmos/query/v1.3": {body: `["nodes/"]`, truncate: true}}))
	wantDetail(t, rep, "PROFILE-PATH-001", StatusFail, "did not complete")
}

// TestPagingNotAnArray: a limited page that is not a JSON array cannot
// be counted, and a collection that cannot be counted cannot be paged.
func TestPagingNotAnArray(t *testing.T) {
	rep := runScripted(t, registryScript(scripted{"/x-nmos/query/v1.3/nodes?paging.limit=1": {body: `{}`}}))
	wantDetail(t, rep, "PROFILE-PAGE-001", StatusFail, "did not return a JSON array")
}

// twoMinorRegistry serves v1.1 and v1.3 so the downgrade check runs.
func twoMinorRegistry(over scripted) scripted {
	s := registryScript(scripted{"/x-nmos/query/": {body: `["v1.1/","v1.3/"]`}})
	for k, v := range over {
		s[k] = v
	}
	return s
}

// TestDowngradeAnswers: the two lists must both arrive and both be
// lists before they can be compared; a downgrade that returns FEWER
// rows than the plain query is a registry that filters the wrong way.
func TestDowngradeAnswers(t *testing.T) {
	const plain, down = "/x-nmos/query/v1.3/nodes", "/x-nmos/query/v1.3/nodes?query.downgrade=v1.1"
	t.Run("plain query faults but downgrade answers", func(t *testing.T) {
		rep := runScripted(t, twoMinorRegistry(scripted{
			plain: {status: http.StatusInternalServerError, body: "boom"},
			down:  {body: `[]`},
		}))
		wantDetail(t, rep, "PROFILE-VER-003", StatusSkip, "could not be compared")
	})
	t.Run("downgrade answer is not a list", func(t *testing.T) {
		rep := runScripted(t, twoMinorRegistry(scripted{down: {body: `{}`}}))
		wantDetail(t, rep, "PROFILE-VER-003", StatusFail, "not a JSON array")
	})
	t.Run("downgrade returns fewer nodes", func(t *testing.T) {
		rep := runScripted(t, twoMinorRegistry(scripted{
			plain: {body: `[{"id":"a"},{"id":"b"}]`},
			down:  {body: `[{"id":"a"}]`},
		}))
		wantDetail(t, rep, "PROFILE-VER-003", StatusFail, "FEWER")
	})
}

// TestHeartbeatRequestError: a health probe that does not complete is a
// failure, not a skip — a node in that state cannot learn anything.
func TestHeartbeatRequestError(t *testing.T) {
	rep := runScripted(t, registryScript(scripted{
		"/x-nmos/registration/v1.3/health/nodes/00000000-0000-4000-8000-000000000000": {body: `{}`, truncate: true},
	}))
	wantDetail(t, rep, "PROFILE-REG-001", StatusFail, "did not complete")
}

// connectionScript is a node serving only IS-05 with the given sender
// list and transport file answer.
func connectionScript(senders string, tf reply) scripted {
	return scripted{
		"/x-nmos":                                {body: `["connection/"]`},
		"/x-nmos/connection/":                    {body: `["v1.1/"]`},
		"/x-nmos/connection/v1.1/single/senders": {body: senders},
		"/x-nmos/connection/v1.1/single/senders/44444444-4444-4444-8444-444444444444/transportfile": tf,
	}
}

// TestNoIS05SendersSkipsTransportChecks: with no senders there is no
// transport file to fetch, and both IS-05 checks stand down.
func TestNoIS05SendersSkipsTransportChecks(t *testing.T) {
	rep := runScripted(t, connectionScript(`[]`, reply{}))
	wantDetail(t, rep, "PROFILE-IS05-001", StatusSkip, "publishes no IS-05 senders")
	wantDetail(t, rep, "PROFILE-SDP-001", StatusSkip, "publishes no IS-05 senders")
}

// TestTransportFileRequestError: a transport file the client cannot
// finish reading is a failure of the endpoint, and the SDP check then
// defers to that result rather than judging a partial body.
func TestTransportFileRequestError(t *testing.T) {
	rep := runScripted(t, connectionScript(
		`["44444444-4444-4444-8444-444444444444/"]`,
		reply{body: "v=0\r\n", ct: "application/sdp", truncate: true},
	))
	wantDetail(t, rep, "PROFILE-IS05-001", StatusFail, "did not complete")
	wantDetail(t, rep, "PROFILE-SDP-001", StatusSkip, "not retrievable")
}

// A well-formed SDP whose a=group:DUP names a second leg that has no
// media section: ST 2022-7 redundancy declared and not carried.
const dupOneLegSDP = "v=0\r\no=- 1 1 IN IP4 198.51.100.5\r\ns=x\r\nt=0 0\r\n" +
	"a=group:DUP primary secondary\r\n" +
	"m=video 5004 RTP/AVP 96\r\nc=IN IP4 233.252.0.10/64\r\na=rtpmap:96 raw/90000\r\na=mid:primary\r\n"

// TestSenderSDPDupUnderfilled is the live counterpart of the offline
// audit's NMOS-SDP-DUP-INCOMPLETE: parseable, no line-level deviation,
// and still a sender that will come up single-path.
func TestSenderSDPDupUnderfilled(t *testing.T) {
	rep := runScripted(t, connectionScript(
		`["44444444-4444-4444-8444-444444444444/"]`,
		reply{body: dupOneLegSDP, ct: "application/sdp"},
	))
	wantDetail(t, rep, "PROFILE-SDP-001", StatusWarn, "only one leg resolves")
}

// TestUnbuildableTargetIsReported: a target that cannot be turned into
// a URL fails every request at the request-building step, and that
// failure is reported like any other rather than panicking.
func TestUnbuildableTargetIsReported(t *testing.T) {
	rep, err := Run(context.Background(), Options{Target: "bad host:80", Timeout: time.Second})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantDetail(t, rep, "PROFILE-ROOT-001", StatusFail, "did not complete")
}

// TestPercentileClamps: a rank below the first sample reads the first,
// a rank past the last reads the last — nearest-rank never indexes
// outside the data.
func TestPercentileClamps(t *testing.T) {
	xs := []float64{1, 2, 3}
	if got := percentile(xs, 0); got != 1 {
		t.Errorf("percentile(0) = %v, want the first sample", got)
	}
	if got := percentile(xs, 150); got != 3 {
		t.Errorf("percentile(150) = %v, want the last sample", got)
	}
}

// TestTruncLeftKeepsTail: the distinguishing part of a path is its end.
func TestTruncLeftKeepsTail(t *testing.T) {
	if got := truncLeft("/x-nmos/query/v1.3/nodes", 8); got != "…3/nodes" {
		t.Errorf("truncLeft = %q, want the tail marked", got)
	}
	if got := truncLeft("short", 8); got != "short" {
		t.Errorf("truncLeft should not touch a value that fits: %q", got)
	}
}
