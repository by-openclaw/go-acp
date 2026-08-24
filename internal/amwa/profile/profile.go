// Package profile runs live protocol-conformance assertions against an
// NMOS device and times every request.
//
// It answers what an offline audit cannot. An export records answers;
// a profile records the conversation. Whether an unknown API version is
// rejected, whether CORS is present, whether a paging limit is honoured
// and reported back, whether a heartbeat for an unregistered node
// 404s — none of that survives into a capture, and every one of them
// decides whether a real controller can drive the device.
//
// The profile is strictly read-only. It never PATCHes, never activates,
// never registers. A conformance tool that stages a connection is a
// tool that can take a live source off air, and this one is meant to be
// safe to point at a plant that is on.
package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Status is the outcome of one assertion.
type Status string

const (
	// StatusPass means the device behaved as the spec requires.
	StatusPass Status = "PASS"
	// StatusWarn means the behaviour is permitted but will cause
	// trouble with a stricter peer.
	StatusWarn Status = "WARN"
	// StatusFail means the device contradicts a spec requirement.
	StatusFail Status = "FAIL"
	// StatusSkip means the check did not apply — the device does not
	// serve the API the check is about. A skip is never a pass, and is
	// reported so a green run cannot hide an untested surface.
	StatusSkip Status = "SKIP"
)

// Result is one assertion's outcome. The shape is deliberately flat:
// one JSON object per line, appendable across runs, diffable between
// two firmware versions without a reading exercise.
type Result struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Spec   string `json:"spec,omitempty"`
	Target string `json:"target"`
	Status Status `json:"status"`
	// Detail states what was observed, always including the request
	// that produced it, so a FAIL can be reproduced by hand.
	Detail string `json:"detail"`
	// DurationMS is the wall time of the request this result rests on.
	DurationMS float64 `json:"duration_ms,omitempty"`
}

// Options configures a profile run.
type Options struct {
	Target  string
	HTTPS   bool
	Timeout time.Duration
	Client  *http.Client
	// Deep runs the checks that make many requests — per-endpoint
	// IS-05 assertions across every sender a device publishes.
	Deep bool
}

func (o *Options) defaults() {
	if o.Timeout <= 0 {
		o.Timeout = 10 * time.Second
	}
	if o.Client == nil {
		o.Client = &http.Client{Timeout: o.Timeout}
	}
}

// Report is a complete profile run: the assertions, and how fast the
// device answered.
type Report struct {
	Target  string         `json:"target"`
	Results []Result       `json:"results"`
	Counts  map[string]int `json:"counts"`
	Latency []EndpointStat `json:"latency"`
}

// EndpointStat is the timing profile of one endpoint class.
//
// Percentiles rather than a mean: a registry that answers in 8 ms and
// occasionally in 4 seconds has a fine mean and is unusable, and the
// mean is exactly what hides that.
type EndpointStat struct {
	Endpoint string  `json:"endpoint"`
	Requests int     `json:"requests"`
	P50MS    float64 `json:"p50_ms"`
	P95MS    float64 `json:"p95_ms"`
	P99MS    float64 `json:"p99_ms"`
	MaxMS    float64 `json:"max_ms"`
	Errors   int     `json:"errors"`
}

// Session carries what the checks share: the transport, what the device
// said it serves, and the latency record.
type Session struct {
	opts   Options
	scheme string

	// APIs maps an API name to the versions the device advertised.
	APIs map[string][]string

	samples map[string][]float64
	errors  map[string]int
}

// response is one observed exchange.
type response struct {
	Status  int
	Header  http.Header
	Body    []byte
	Elapsed time.Duration
	Err     error
}

// ms renders the elapsed time for a Result.
func (r response) ms() float64 { return float64(r.Elapsed.Microseconds()) / 1000 }

// checkFn is one assertion group. Each returns every Result it
// produced, so a check that iterates endpoints reports per endpoint.
type checkFn func(ctx context.Context, s *Session) []Result

// checks is the ordered assertion set. Adding one is adding a function
// here — that is the whole extension mechanism.
var checks = []checkFn{
	checkRoot,
	checkVersionLists,
	checkUnknownVersionRejected,
	checkCORS,
	checkContentType,
	checkUnknownResource,
	checkTrailingSlash,
	checkQueryPaging,
	checkQueryDowngrade,
	checkHeartbeatUnknownNode,
	checkTransportFileContentType,
}

// Run probes the target and returns the report.
func Run(ctx context.Context, opts Options) (*Report, error) {
	opts.defaults()
	if opts.Target == "" {
		return nil, fmt.Errorf("profile: --target is required")
	}

	s := &Session{
		opts:    opts,
		scheme:  "http",
		APIs:    map[string][]string{},
		samples: map[string][]float64{},
		errors:  map[string]int{},
	}
	if opts.HTTPS {
		s.scheme = "https"
	}

	rep := &Report{Target: opts.Target, Counts: map[string]int{}}
	for _, c := range checks {
		rep.Results = append(rep.Results, c(ctx, s)...)
	}
	for _, r := range rep.Results {
		rep.Counts[string(r.Status)]++
	}
	rep.Latency = s.latency()
	return rep, nil
}

// Worst reports the most severe status present, ranked FAIL > WARN >
// SKIP > PASS, and whether anything was reported at all.
func (r *Report) Worst() (Status, bool) {
	rank := map[Status]int{StatusPass: 0, StatusSkip: 1, StatusWarn: 2, StatusFail: 3}
	worst, any := StatusPass, false
	for _, res := range r.Results {
		if !any || rank[res.Status] > rank[worst] {
			worst, any = res.Status, true
		}
	}
	return worst, any
}

// --- transport ---

// get performs one request and records its timing.
func (s *Session) get(ctx context.Context, path string) response {
	return s.request(ctx, http.MethodGet, path, nil)
}

func (s *Session) request(ctx context.Context, method, path string, hdr http.Header) response {
	rctx, cancel := context.WithTimeout(ctx, s.opts.Timeout)
	defer cancel()

	url := path
	if strings.HasPrefix(path, "/") {
		url = s.scheme + "://" + s.opts.Target + path
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(rctx, method, url, nil)
	if err != nil {
		return response{Err: err, Elapsed: time.Since(start)}
	}
	for k, vs := range hdr {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	resp, err := s.opts.Client.Do(req)
	if err != nil {
		el := time.Since(start)
		s.record(path, el, true)
		return response{Err: err, Elapsed: el}
	}
	defer func() { _ = resp.Body.Close() }()

	body, readErr := io.ReadAll(resp.Body)
	el := time.Since(start)
	s.record(path, el, readErr != nil || resp.StatusCode >= 500)
	return response{Status: resp.StatusCode, Header: resp.Header, Body: body, Elapsed: el, Err: readErr}
}

// uuidInPath collapses per-resource URLs into one endpoint class, so a
// 176-sender device produces one latency row rather than 176.
var uuidInPath = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

func endpointClass(path string) string {
	if i := strings.Index(path, "/x-nmos/"); i >= 0 {
		path = path[i:]
	}
	if i := strings.Index(path, "?"); i >= 0 {
		path = path[:i]
	}
	return uuidInPath.ReplaceAllString(path, "{id}")
}

func (s *Session) record(path string, d time.Duration, failed bool) {
	class := endpointClass(path)
	s.samples[class] = append(s.samples[class], float64(d.Microseconds())/1000)
	if failed {
		s.errors[class]++
	}
}

// latency renders the timing record, slowest p99 first — the row an
// operator needs to see is the one at the top.
func (s *Session) latency() []EndpointStat {
	out := make([]EndpointStat, 0, len(s.samples))
	for ep, xs := range s.samples {
		sorted := append([]float64(nil), xs...)
		sort.Float64s(sorted)
		out = append(out, EndpointStat{
			Endpoint: ep,
			Requests: len(sorted),
			P50MS:    percentile(sorted, 50),
			P95MS:    percentile(sorted, 95),
			P99MS:    percentile(sorted, 99),
			MaxMS:    sorted[len(sorted)-1],
			Errors:   s.errors[ep],
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].P99MS != out[j].P99MS {
			return out[i].P99MS > out[j].P99MS
		}
		return out[i].Endpoint < out[j].Endpoint
	})
	return out
}

// percentile uses nearest-rank on a sorted slice. With the handful of
// samples a profile run collects, interpolation would invent precision the
// data does not have.
func percentile(sorted []float64, p int) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := (p*len(sorted) + 99) / 100 // ceil(p/100 * n)
	if idx < 1 {
		idx = 1
	}
	if idx > len(sorted) {
		idx = len(sorted)
	}
	return sorted[idx-1]
}

// --- result helpers ---

func (s *Session) result(id, name, spec string, st Status, r response, detail string) Result {
	return Result{
		ID: id, Name: name, Spec: spec, Target: s.opts.Target,
		Status: st, Detail: detail, DurationMS: r.ms(),
	}
}

func (s *Session) skip(id, name, spec, why string) Result {
	return Result{ID: id, Name: name, Spec: spec, Target: s.opts.Target, Status: StatusSkip, Detail: why}
}

// jsonArray decodes a body as an array of strings, the shape every
// NMOS index endpoint uses.
func jsonArray(b []byte) ([]string, bool) {
	var out []string
	if json.Unmarshal(b, &out) != nil {
		return nil, false
	}
	return out, true
}

// versionPattern is the `vMAJOR.MINOR` form every API version takes.
var versionPattern = regexp.MustCompile(`^v\d+\.\d+$`)

// firstAPIWithResources names an API the device serves that has
// walkable collections, preferring the registry's catalogue.
func (s *Session) queryOrNode() (api, ver string, ok bool) {
	for _, name := range []string{"query", "node"} {
		if vs, has := s.APIs[name]; has && len(vs) > 0 {
			return name, highestVersion(vs), true
		}
	}
	return "", "", false
}

// highestVersion orders v1.10 after v1.9, which a string sort gets
// wrong.
func highestVersion(vs []string) string {
	best, rank := "", -2
	for _, v := range vs {
		r := -1
		if maj, min, cut := strings.Cut(strings.TrimPrefix(v, "v"), "."); cut {
			m, e1 := strconv.Atoi(maj)
			n, e2 := strconv.Atoi(min)
			if e1 == nil && e2 == nil {
				r = m*1000 + n
			}
		}
		if r > rank {
			best, rank = v, r
		}
	}
	return best
}
