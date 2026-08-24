package profile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// device is a scriptable NMOS surface. Its defaults are spec-correct,
// and each test breaks exactly one thing — so a failing assertion names
// the rule that was broken rather than a pile of unrelated noise.
type device struct {
	apis map[string][]string
	// nodes is what the query API lists.
	nodes []any

	// the knobs each test turns
	noCORS          bool
	refusePreflight bool
	contentType     string
	unknownVerOK    bool
	faultOnMissing  bool
	plainErrorBody  bool
	noTrailingSlash bool
	ignorePaging    bool
	noPagingHeaders bool
	noLinkNext      bool
	downgradeFails  bool
	healthOnUnknown int
	transportFile   string
	transportCT     string
	transportCode   int
}

func newDevice() *device {
	return &device{
		apis:            map[string][]string{"query": {"v1.1", "v1.3"}, "registration": {"v1.3"}},
		contentType:     "application/json",
		healthOnUnknown: http.StatusNotFound,
		transportCT:     "application/sdp",
		transportCode:   http.StatusOK,
		transportFile:   "v=0\r\no=- 1 1 IN IP4 198.51.100.5\r\ns=probe\r\n",
	}
}

func (d *device) writeJSON(w http.ResponseWriter, v any) {
	if !d.noCORS {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}
	if d.contentType != "" {
		w.Header().Set("Content-Type", d.contentType)
	}
	_ = json.NewEncoder(w).Encode(v)
}

func (d *device) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path

	if r.Method == http.MethodOptions {
		if d.refusePreflight {
			http.Error(w, "no", http.StatusForbidden)
			return
		}
		if !d.noCORS {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	switch {
	case p == "/x-nmos":
		names := make([]string, 0, len(d.apis))
		for k := range d.apis {
			names = append(names, k+"/")
		}
		d.writeJSON(w, names)
		return

	case strings.HasPrefix(p, "/x-nmos/") && strings.Count(strings.Trim(p, "/"), "/") == 1:
		api := strings.TrimSuffix(strings.TrimPrefix(p, "/x-nmos/"), "/")
		vs, ok := d.apis[api]
		if !ok {
			http.NotFound(w, r)
			return
		}
		out := make([]string, 0, len(vs))
		for _, v := range vs {
			out = append(out, v+"/")
		}
		d.writeJSON(w, out)
		return
	}

	// Anything at an unadvertised version.
	if strings.Contains(p, "/v9.9/") {
		if d.unknownVerOK {
			d.writeJSON(w, []any{})
			return
		}
		http.NotFound(w, r)
		return
	}

	// The version root, with and without a trailing slash.
	if p == "/x-nmos/query/v1.3" || p == "/x-nmos/query/v1.3/" {
		if d.noTrailingSlash && p == "/x-nmos/query/v1.3" {
			http.NotFound(w, r)
			return
		}
		d.writeJSON(w, []string{"nodes/", "devices/"})
		return
	}

	// A single node by id — the check only ever asks for one that is
	// not there.
	if strings.HasPrefix(p, "/x-nmos/query/v1.3/nodes/") {
		if d.faultOnMissing {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		if d.plainErrorBody {
			_, _ = w.Write([]byte("not found"))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 404, "error": "resource not found", "debug": nil,
		})
		return
	}

	if p == "/x-nmos/query/v1.1/nodes" || p == "/x-nmos/query/v1.3/nodes" {
		items := d.nodes
		limit := r.URL.Query().Get("paging.limit")

		// Version isolation: v1.3 hides the LAST node — whatever was
		// registered at the lower minor — unless the caller asks to
		// downgrade. Hiding all but one instead would leave the page
		// always at length 1, and the paging assertions below would
		// have nothing to bite on.
		if p == "/x-nmos/query/v1.3/nodes" {
			down := r.URL.Query().Get("query.downgrade")
			if down != "" && d.downgradeFails {
				http.Error(w, "unsupported", http.StatusBadRequest)
				return
			}
			if down == "" && len(items) > 1 {
				items = items[:len(items)-1]
			}
		}

		if limit != "" && !d.ignorePaging && len(items) > 1 {
			items = items[:1]
		}
		if !d.noPagingHeaders {
			w.Header().Set("X-Paging-Limit", "10")
			w.Header().Set("X-Paging-Since", "0:0")
			w.Header().Set("X-Paging-Until", "0:0")
		}
		if limit != "" && !d.noLinkNext && len(d.nodes) > len(items) {
			w.Header().Set("Link", fmt.Sprintf(`<%s?paging.since=1>; rel="next"`, p))
		}
		d.writeJSON(w, items)
		return
	}

	if strings.HasPrefix(p, "/x-nmos/registration/v1.3/health/nodes/") {
		w.WriteHeader(d.healthOnUnknown)
		return
	}

	if p == "/x-nmos/connection/v1.1/single/senders" {
		d.writeJSON(w, []string{"44444444-4444-4444-8444-444444444444/"})
		return
	}
	if strings.HasSuffix(p, "/transportfile") {
		if d.transportCode != http.StatusOK {
			w.WriteHeader(d.transportCode)
			return
		}
		w.Header().Set("Content-Type", d.transportCT)
		_, _ = w.Write([]byte(d.transportFile))
		return
	}

	http.NotFound(w, r)
}

func node(id, label string) map[string]any {
	return map[string]any{"id": id, "version": "1755000000:0", "label": label, "tags": map[string]any{}}
}

func profileIt(t *testing.T, d *device) *Report {
	t.Helper()
	srv := httptest.NewServer(d)
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

// statusOf returns the status of the first result with the given id.
func statusOf(t *testing.T, r *Report, id string) Status {
	t.Helper()
	for _, res := range r.Results {
		if res.ID == id {
			return res.Status
		}
	}
	t.Fatalf("no result with id %s; got %v", id, idsOf(r))
	return ""
}

func idsOf(r *Report) []string {
	out := make([]string, 0, len(r.Results))
	for _, res := range r.Results {
		out = append(out, res.ID+"="+string(res.Status))
	}
	return out
}

func want(t *testing.T, r *Report, id string, st Status) {
	t.Helper()
	if got := statusOf(t, r, id); got != st {
		for _, res := range r.Results {
			if res.ID == id {
				t.Errorf("%s = %s, want %s\n  detail: %s", id, got, st, res.Detail)
				return
			}
		}
	}
}

// TestCompliantDeviceHasNoFailures is the baseline. Everything after it
// breaks exactly one rule, so a failure elsewhere is unambiguous.
func TestCompliantDeviceHasNoFailures(t *testing.T) {
	d := newDevice()
	d.nodes = []any{
		node("11111111-1111-4111-8111-111111111111", "a"),
		node("22222222-2222-4222-8222-222222222222", "b"),
	}
	rep := profileIt(t, d)

	for _, res := range rep.Results {
		if res.Status == StatusFail {
			t.Errorf("a compliant device produced %s: %s\n  %s", res.ID, res.Status, res.Detail)
		}
	}
	if rep.Counts[string(StatusPass)] == 0 {
		t.Error("no assertion passed against a compliant device")
	}
	if worst, any := rep.Worst(); !any || worst == StatusFail {
		t.Errorf("Worst() = %s,%v on a compliant device", worst, any)
	}
}

func TestUnknownVersionServed(t *testing.T) {
	d := newDevice()
	d.unknownVerOK = true
	want(t, profileIt(t, d), "PROFILE-VER-002", StatusFail)
}

func TestMissingCORS(t *testing.T) {
	d := newDevice()
	d.noCORS = true
	rep := profileIt(t, d)
	want(t, rep, "PROFILE-CORS-001", StatusFail)
	// The preflight still answers; it just names no methods.
	want(t, rep, "PROFILE-CORS-002", StatusWarn)
}

func TestRefusedPreflight(t *testing.T) {
	d := newDevice()
	d.refusePreflight = true
	want(t, profileIt(t, d), "PROFILE-CORS-002", StatusFail)
}

func TestWrongContentType(t *testing.T) {
	d := newDevice()
	d.contentType = "text/html"
	want(t, profileIt(t, d), "PROFILE-CT-001", StatusFail)

	d2 := newDevice()
	d2.contentType = ""
	want(t, profileIt(t, d2), "PROFILE-CT-001", StatusFail)
}

// TestFaultOnMissingResource is the distinction that turns a stale
// reference into a retry storm.
func TestFaultOnMissingResource(t *testing.T) {
	d := newDevice()
	d.faultOnMissing = true
	want(t, profileIt(t, d), "PROFILE-404-001", StatusFail)
}

func TestErrorBodyShape(t *testing.T) {
	d := newDevice()
	d.plainErrorBody = true
	rep := profileIt(t, d)
	want(t, rep, "PROFILE-404-001", StatusPass) // the status is still right
	want(t, rep, "PROFILE-404-002", StatusWarn) // the body is not
}

func TestTrailingSlashInconsistency(t *testing.T) {
	d := newDevice()
	d.noTrailingSlash = true
	want(t, profileIt(t, d), "PROFILE-PATH-001", StatusWarn)
}

// TestPagingLimitIgnored is the defect that captured 11 nodes of a
// 68-node registry.
func TestPagingLimitIgnored(t *testing.T) {
	d := newDevice()
	d.ignorePaging = true
	// Three nodes so that v1.3 still lists two after version isolation
	// hides the one registered lower — a limit of 1 then has something
	// to fail to honour.
	d.nodes = []any{
		node("11111111-1111-4111-8111-111111111111", "a"),
		node("22222222-2222-4222-8222-222222222222", "b"),
		node("33333333-3333-4333-8333-333333333333", "c"),
	}
	want(t, profileIt(t, d), "PROFILE-PAGE-001", StatusFail)
}

func TestPagingHeadersAndLink(t *testing.T) {
	d := newDevice()
	d.noPagingHeaders = true
	d.noLinkNext = true
	d.nodes = []any{
		node("11111111-1111-4111-8111-111111111111", "a"),
		node("22222222-2222-4222-8222-222222222222", "b"),
	}
	rep := profileIt(t, d)
	want(t, rep, "PROFILE-PAGE-002", StatusWarn)
	want(t, rep, "PROFILE-PAGE-003", StatusWarn)
}

// TestPagingSkippedOnEmptyRegistry: an empty catalogue cannot prove or
// disprove paging, and must not be reported as either.
func TestPagingSkippedOnEmptyRegistry(t *testing.T) {
	d := newDevice()
	d.nodes = nil
	rep := profileIt(t, d)
	want(t, rep, "PROFILE-PAGE-003", StatusSkip)
}

// TestDowngradeEscapesVersionIsolation: the fake registry hides its
// second node at v1.3, exactly as a real one does.
func TestDowngradeEscapesVersionIsolation(t *testing.T) {
	d := newDevice()
	d.nodes = []any{
		node("11111111-1111-4111-8111-111111111111", "a"),
		node("22222222-2222-4222-8222-222222222222", "b"),
	}
	rep := profileIt(t, d)
	want(t, rep, "PROFILE-VER-003", StatusPass)
	for _, res := range rep.Results {
		if res.ID == "PROFILE-VER-003" && !strings.Contains(res.Detail, "isolation is escapable") {
			t.Errorf("the downgrade result should say what it proved: %q", res.Detail)
		}
	}
}

func TestDowngradeRejected(t *testing.T) {
	d := newDevice()
	d.downgradeFails = true
	d.nodes = []any{
		node("11111111-1111-4111-8111-111111111111", "a"),
		node("22222222-2222-4222-8222-222222222222", "b"),
	}
	want(t, profileIt(t, d), "PROFILE-VER-003", StatusFail)
}

func TestDowngradeSkippedOnSingleMinor(t *testing.T) {
	d := newDevice()
	d.apis["query"] = []string{"v1.3"}
	want(t, profileIt(t, d), "PROFILE-VER-003", StatusSkip)
}

func TestHeartbeatForUnknownNode(t *testing.T) {
	for _, tc := range []struct {
		code int
		want Status
	}{
		{http.StatusNotFound, StatusPass},
		{http.StatusOK, StatusFail},
		{http.StatusInternalServerError, StatusFail},
		{http.StatusMethodNotAllowed, StatusWarn},
	} {
		t.Run(fmt.Sprint(tc.code), func(t *testing.T) {
			d := newDevice()
			d.healthOnUnknown = tc.code
			want(t, profileIt(t, d), "PROFILE-REG-001", tc.want)
		})
	}
}

func TestNoRegistrationAPISkipsHeartbeat(t *testing.T) {
	d := newDevice()
	delete(d.apis, "registration")
	want(t, profileIt(t, d), "PROFILE-REG-001", StatusSkip)
}

// TestTransportFile covers each way a transport file can be wrong. The
// 502 case is a real device behaviour, observed live.
func TestTransportFile(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*device)
		want Status
	}{
		{"served correctly", func(d *device) {}, StatusPass},
		{"502", func(d *device) { d.transportCode = http.StatusBadGateway }, StatusFail},
		{"404 while inactive", func(d *device) { d.transportCode = http.StatusNotFound }, StatusWarn},
		{"403", func(d *device) { d.transportCode = http.StatusForbidden }, StatusFail},
		{"wrong content type", func(d *device) { d.transportCT = "text/plain" }, StatusFail},
		{"not an SDP", func(d *device) { d.transportFile = "<html>nope</html>" }, StatusFail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDevice()
			d.apis["connection"] = []string{"v1.1"}
			tc.set(d)
			want(t, profileIt(t, d), "PROFILE-IS05-001", tc.want)
		})
	}
}

func TestNoConnectionAPISkipsTransportFile(t *testing.T) {
	want(t, profileIt(t, newDevice()), "PROFILE-IS05-001", StatusSkip)
}

// TestDeepProbesEverySender proves --deep is what catches a device that
// is broken on some endpoints and not others.
func TestDeepProbesEverySender(t *testing.T) {
	ids := []string{
		"44444444-4444-4444-8444-444444444444/",
		"55555555-5555-4555-8555-555555555555/",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/x-nmos":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			_ = json.NewEncoder(w).Encode([]string{"connection/"})
		case r.URL.Path == "/x-nmos/connection/":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]string{"v1.1/"})
		case r.URL.Path == "/x-nmos/connection/v1.1/single/senders":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(ids)
		case strings.HasSuffix(r.URL.Path, "/transportfile"):
			// The second sender is broken; the first is fine.
			if strings.Contains(r.URL.Path, "5555") {
				http.Error(w, "bad gateway", http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "application/sdp")
			_, _ = w.Write([]byte("v=0\r\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	target := strings.TrimPrefix(srv.URL, "http://")

	shallow, err := Run(context.Background(), Options{Target: target, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	deep, err := Run(context.Background(), Options{Target: target, Timeout: 2 * time.Second, Deep: true})
	if err != nil {
		t.Fatal(err)
	}

	countIS05 := func(r *Report, st Status) int {
		n := 0
		for _, res := range r.Results {
			if res.ID == "PROFILE-IS05-001" && res.Status == st {
				n++
			}
		}
		return n
	}
	if countIS05(shallow, StatusFail) != 0 {
		t.Error("the shallow probe should have checked only the first, working sender")
	}
	if countIS05(deep, StatusFail) != 1 {
		t.Errorf("--deep found %d broken senders, want 1", countIS05(deep, StatusFail))
	}
	if countIS05(deep, StatusPass) != 1 {
		t.Errorf("--deep found %d working senders, want 1", countIS05(deep, StatusPass))
	}
}

// TestUnreachableTargetFailsCleanly: pointing the probe at nothing must
// report, not panic.
func TestUnreachableTargetFailsCleanly(t *testing.T) {
	srv := httptest.NewServer(newDevice())
	target := strings.TrimPrefix(srv.URL, "http://")
	srv.Close()

	rep, err := Run(context.Background(), Options{Target: target, Timeout: 500 * time.Millisecond})
	if err != nil {
		t.Fatalf("an unreachable target should report, not error: %v", err)
	}
	want(t, rep, "PROFILE-ROOT-001", StatusFail)
	if worst, any := rep.Worst(); !any || worst != StatusFail {
		t.Errorf("Worst() = %s,%v", worst, any)
	}
}

func TestRootNotAnArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"not":"an array"}`))
	}))
	defer srv.Close()
	rep, err := Run(context.Background(), Options{Target: strings.TrimPrefix(srv.URL, "http://"), Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	want(t, rep, "PROFILE-ROOT-001", StatusFail)
	// With no APIs discovered, the version check has nothing to assert
	// and must skip rather than pass.
	want(t, rep, "PROFILE-VER-001", StatusSkip)
}

func TestMalformedVersionList(t *testing.T) {
	d := newDevice()
	d.apis["query"] = []string{"1.3", "latest"}
	want(t, profileIt(t, d), "PROFILE-VER-001", StatusFail)
}

func TestTargetRequired(t *testing.T) {
	if _, err := Run(context.Background(), Options{}); err == nil {
		t.Error("an empty target should be refused")
	}
}

// TestLatencyIsRecorded: the timing is half the point of the verb.
func TestLatencyIsRecorded(t *testing.T) {
	rep := profileIt(t, newDevice())
	if len(rep.Latency) == 0 {
		t.Fatal("no latency was recorded")
	}
	for _, s := range rep.Latency {
		if s.Requests == 0 {
			t.Errorf("%s recorded 0 requests", s.Endpoint)
		}
		if s.P50MS > s.P99MS || s.P99MS > s.MaxMS {
			t.Errorf("%s percentiles are not ordered: p50=%.2f p99=%.2f max=%.2f",
				s.Endpoint, s.P50MS, s.P99MS, s.MaxMS)
		}
	}
	// Per-resource URLs must collapse into one endpoint class, or a
	// 176-sender device produces 176 unreadable rows.
	for _, s := range rep.Latency {
		if strings.Contains(s.Endpoint, "0000-4000-8000") {
			t.Errorf("a UUID survived into an endpoint class: %s", s.Endpoint)
		}
	}
}

func TestPercentileNearestRank(t *testing.T) {
	xs := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	for _, tc := range []struct {
		p    int
		want float64
	}{
		{50, 5}, {95, 10}, {99, 10}, {1, 1},
	} {
		if got := percentile(xs, tc.p); got != tc.want {
			t.Errorf("percentile(%d) = %v, want %v", tc.p, got, tc.want)
		}
	}
	if got := percentile(nil, 50); got != 0 {
		t.Errorf("percentile of nothing = %v", got)
	}
	if got := percentile([]float64{42}, 99); got != 42 {
		t.Errorf("percentile of one sample = %v", got)
	}
}

func TestHighestVersionOrdersNumerically(t *testing.T) {
	if got := highestVersion([]string{"v1.9", "v1.10"}); got != "v1.10" {
		t.Errorf("highestVersion = %q, want v1.10", got)
	}
	if got := highestVersion([]string{"junk"}); got != "junk" {
		t.Errorf("highestVersion of one unparseable = %q", got)
	}
}

func TestEndpointClass(t *testing.T) {
	got := endpointClass("http://h:80/x-nmos/connection/v1.1/single/senders/44444444-4444-4444-8444-444444444444/active?x=1")
	if got != "/x-nmos/connection/v1.1/single/senders/{id}/active" {
		t.Errorf("endpointClass = %q", got)
	}
	if got := endpointClass("/other/path"); got != "/other/path" {
		t.Errorf("endpointClass of a non-nmos path = %q", got)
	}
}

// TestRenderers pins each output shape.
func TestRenderers(t *testing.T) {
	d := newDevice()
	d.noCORS = true
	d.nodes = []any{node("11111111-1111-4111-8111-111111111111", "a")}
	rep := profileIt(t, d)

	var text bytes.Buffer
	if err := RenderText(&text, rep); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"NMOS LIVE PROBE", "FAIL", "LATENCY (ms)", "SUMMARY", "PROFILE-CORS-001"} {
		if !strings.Contains(text.String(), s) {
			t.Errorf("text report missing %q", s)
		}
	}
	// Failures first: an operator runs a probe because something is
	// suspected.
	if strings.Index(text.String(), "FAIL") > strings.Index(text.String(), "PASS —") {
		t.Error("failures should be reported before passes")
	}

	var jb bytes.Buffer
	if err := RenderJSON(&jb, rep); err != nil {
		t.Fatal(err)
	}
	var back Report
	if err := json.Unmarshal(jb.Bytes(), &back); err != nil {
		t.Fatalf("the report does not round-trip: %v", err)
	}
	if len(back.Results) != len(rep.Results) {
		t.Error("the round trip lost results")
	}

	var lb bytes.Buffer
	if err := RenderJSONL(&lb, rep); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(lb.String(), "\n"), "\n")
	if len(lines) != len(rep.Results)+len(rep.Latency) {
		t.Errorf("JSONL wrote %d lines for %d results + %d latency rows",
			len(lines), len(rep.Results), len(rep.Latency))
	}
	// Latency rows must be distinguishable from results, or a consumer
	// reading the file cannot tell them apart.
	latencyRows := 0
	for _, ln := range lines {
		var row struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
		}
		if err := json.Unmarshal([]byte(ln), &row); err != nil {
			t.Fatalf("a JSONL line is not an object: %v", err)
		}
		if row.Kind == "latency" {
			latencyRows++
		} else if row.ID == "" {
			t.Error("a non-latency line carries no id")
		}
	}
	if latencyRows != len(rep.Latency) {
		t.Errorf("%d latency rows tagged, want %d", latencyRows, len(rep.Latency))
	}
}

type failWriter struct{ n int }

func (f *failWriter) Write(p []byte) (int, error) {
	if f.n <= 0 {
		return 0, fmt.Errorf("closed")
	}
	f.n--
	return len(p), nil
}

func TestRenderersPropagateWriteErrors(t *testing.T) {
	rep := profileIt(t, newDevice())
	if err := RenderText(&failWriter{n: 1}, rep); err == nil {
		t.Error("RenderText should surface the write error")
	}
	if err := RenderJSONL(&failWriter{n: 0}, rep); err == nil {
		t.Error("RenderJSONL should surface the write error")
	}
	// The latency rows come after the results, so a writer that dies
	// partway must still report.
	if err := RenderJSONL(&failWriter{n: len(rep.Results)}, rep); err == nil {
		t.Error("RenderJSONL should surface a write error on the latency rows")
	}
}

func TestWorstOnEmptyReport(t *testing.T) {
	if _, any := (&Report{}).Worst(); any {
		t.Error("an empty report should report nothing")
	}
}

func TestHTTPSOption(t *testing.T) {
	d := newDevice()
	srv := httptest.NewTLSServer(d)
	defer srv.Close()
	rep, err := Run(context.Background(), Options{
		Target: strings.TrimPrefix(srv.URL, "https://"), HTTPS: true,
		Client: srv.Client(), Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	want(t, rep, "PROFILE-ROOT-001", StatusPass)
}
