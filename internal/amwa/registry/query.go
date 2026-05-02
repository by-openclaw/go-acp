package registry

import (
	"context"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	stdhttp "net/http"

	"acp/internal/amwa/codec/is04"
	httpsession "acp/internal/amwa/session/http"
)

// installQueryRoutes wires the Query API endpoints onto the server.
// base = `/x-nmos/query/<api-ver>`. apiVer mirrors the URL prefix's
// minor — it's how the Query layer enforces the no-downgrade-by-
// default rule (IS-04 §6.1.5: a v1.0-registered resource doesn't show
// up at /query/v1.3 unless `?query.downgrade=v1.0` opts in).
//
// Spec: https://specs.amwa.tv/is-04/releases/v1.3.3/APIs/QueryAPI.html
func installQueryRoutes(srv *httpsession.Server, store *Store, mgr *SubscriptionManager, base, apiVer string) {
	srv.Handle(stdhttp.MethodGet, base+"/", func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		return 0, []string{"nodes/", "devices/", "sources/", "flows/", "senders/", "receivers/", "subscriptions/"}, nil
	})

	for _, t := range is04.AllResourceTypes {
		plural := t.Plural()
		// Capture loop variables.
		listType := t
		// GET /<plural> — IS-04 §6.1.6 paged list. Always emits
		// X-Paging-Limit / X-Paging-Since / X-Paging-Until + Link
		// headers. RQL-lite filters (id/label/description) apply to
		// the page contents; pagination cursors index the time series
		// of registry-side update_ts.
		srv.Handle(stdhttp.MethodGet, base+"/"+plural, func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
			return servePagedList(store, listType, base, plural, apiVer, r)
		})
	}

	// Per-id GETs use a prefix dispatcher (path = .../<plural>/<id>).
	for _, t := range is04.AllResourceTypes {
		plural := t.Plural()
		typeCopy := t
		prefix := base + "/" + plural + "/"
		srv.HandlePrefix(prefix, stdhttp.MethodGet, func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
			id := strings.TrimPrefix(r.URL.Path, prefix)
			if id == "" || strings.Contains(id, "/") {
				return stdhttp.StatusNotFound, httpsession.ErrorBody{Code: 404, Error: "Not Found", Debug: r.URL.Path}, nil
			}
			if !versionAllowed(store.APIVerOf(typeCopy, id), apiVer, r.URL.Query().Get("query.downgrade")) {
				return stdhttp.StatusNotFound, httpsession.ErrorBody{Code: 404, Error: "Not Found", Debug: id}, nil
			}
			body, ok := getResource(store, typeCopy, id)
			if !ok {
				return stdhttp.StatusNotFound, httpsession.ErrorBody{Code: 404, Error: "Not Found", Debug: id}, nil
			}
			// Encode via the URL's wire codec so v1.0 GETs don't
			// leak v1.1+ fields like `device_id` (Flow). Falls back
			// to canonical marshal when no codec is registered.
			if raw, ok := encodeForVersion(typeCopy, body, apiVer); ok {
				return 0, &httpsession.RawBody{ContentType: "application/json", Body: raw}, nil
			}
			return 0, body, nil
		})
	}

	// Subscriptions — POST returns ws_href; GET /subscriptions
	// returns the active list; GET /subscriptions/{id} returns one.
	srv.Handle(stdhttp.MethodPost, base+"/subscriptions", mgr.HandlePost(base))
	srv.Handle(stdhttp.MethodGet, base+"/subscriptions", mgr.HandleList())
	subPrefix := base + "/subscriptions/"
	srv.HandlePrefix(subPrefix, stdhttp.MethodGet, mgr.HandleGetByID(subPrefix))
}

// hasAncestryFilter reports whether the request asked for an
// IS-04 §6.1.5 ancestry query (`query.ancestry_id` /
// `query.ancestry_type`). The Registry plugin doesn't implement
// ancestry yet — it returns HTTP 501 so the AMWA test_25 suite
// records the gap as OPTIONAL rather than failing on stale data.
func hasAncestryFilter(q map[string][]string) bool {
	_, a := q["query.ancestry_id"]
	_, b := q["query.ancestry_type"]
	return a || b
}

// servePagedList drives one IS-04 Query API GET /<plural> response.
// It reads paging.* query params, asks Store.ListPaged for the matching
// page, applies the RQL-lite top-level field filter to the page
// contents, then attaches the four mandatory pagination response
// headers (X-Paging-Limit / X-Paging-Since / X-Paging-Until / Link).
//
// apiVer is the URL prefix's wire minor and gates the visibility of
// resources by their registration version (IS-04 §6.1.5 — no implicit
// downgrade; opt-in via `?query.downgrade=v1.X`).
//
// Spec: IS-04 v1.3.3 §6.1.6 + Query API RAML pagination clause.
func servePagedList(store *Store, t is04.ResourceType, base, plural, apiVer string, r *stdhttp.Request) (int, any, error) {
	q := r.URL.Query()
	if hasAncestryFilter(q) {
		return stdhttp.StatusNotImplemented,
			httpsession.ErrorBody{Code: 501, Error: "Not Implemented", Debug: "ancestry queries (query.ancestry_id/query.ancestry_type) not supported by this Registry"},
			nil
	}
	since := q.Get("paging.since")
	until := q.Get("paging.until")
	if since != "" && until != "" && taiCmp(since, until) > 0 {
		// IS-04 §6.1.6 — paging.since must be <= paging.until. AMWA
		// test_21_6 specifically exercises since > until and expects
		// HTTP 400.
		return stdhttp.StatusBadRequest,
			httpsession.ErrorBody{Code: 400, Error: "Bad Request", Debug: "paging.since must be <= paging.until"},
			nil
	}
	opts := PageOptions{Since: since, Until: until}
	limitProvided := false
	if v := q.Get("paging.limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			opts.Limit = n
			limitProvided = true
		}
	}
	// Push the basic-equality / RQL filter AND the version-isolation
	// gate down into the page so the X-Paging-Since / X-Paging-Until
	// cursors index the post-filter set, not the unfiltered registry
	// order. test_21_2 / test_21_3 fail without this; test_22 fails
	// without the version gate.
	flat, rql := splitFilterParams(q)
	downgrade := q.Get("query.downgrade")
	opts.Predicate = func(res any) bool {
		// Type-assert through reflect to read the resource's id, look
		// up its registration api_ver, and apply the gate. Predicate
		// runs INSIDE Store.ListPaged which already holds the read
		// lock, so we use the lock-free apiVerOfLocked variant.
		rv := reflect.ValueOf(res)
		if id := readJSONField(rv, "id"); id != "" {
			if !versionAllowed(store.apiVerOfLocked(t, id), apiVer, downgrade) {
				return false
			}
		}
		if !matchesAll(rv, flat) {
			return false
		}
		if rql != nil && !rqlMatch(rv, rql) {
			return false
		}
		return true
	}
	page := store.ListPaged(t, opts)
	body := page.Items
	// limit=0 explicitly: server returns no items but still emits
	// paging cursors. AMWA test_21_4 verifies that with limit=0 both
	// X-Paging-Since and X-Paging-Until collapse onto the explicit
	// cursor the client supplied (until-if-set, otherwise since,
	// otherwise the registry's now-clock), AND that X-Paging-Limit
	// echoes 0 verbatim (not replaced by the server's default).
	if limitProvided && opts.Limit == 0 {
		body = collectTyped(t, nil)
		anchor := until
		if anchor == "" {
			anchor = since
		}
		if anchor == "" {
			anchor = nowTAI()
		}
		page.Since = anchor
		page.Until = anchor
		page.Limit = 0
	}

	limitStr := strconv.Itoa(page.Limit)
	headers := map[string]string{
		"X-Paging-Limit": limitStr,
		"X-Paging-Since": page.Since,
		"X-Paging-Until": page.Until,
		"Link":           buildLinkHeader(r, base+"/"+plural, page, limitStr),
	}
	return 0, &httpsession.WithHeaders{Body: body, Headers: headers}, nil
}

// buildLinkHeader assembles the rfc5988 Link header advertising prev /
// next / first / last cursors. The test harness asserts that:
//   - prev contains `paging.until=<X-Paging-Since>` and no paging.since
//   - next contains `paging.since=<X-Paging-Until>` and no paging.until
//   - first contains `paging.since=0:0` and no paging.until
//   - last is bare (no paging.since / paging.until)
//   - every cursor preserves the original non-paging query parameters
//     (id / label / description / etc.) and carries `paging.limit`.
//
// We DO NOT use url.Values.Encode() — it percent-encodes the colon in
// TAI timestamps (`0:0` → `0%3A0`), which makes the test harness's
// substring check (`"paging.until=" + since not in prev`) fail. The
// `:` is a sub-delim in RFC 3986 query syntax so leaving it raw is
// spec-legal; nmos-cpp does the same. We do percent-encode user-
// supplied filter values (label/description) to keep the URL
// well-formed.
func buildLinkHeader(r *stdhttp.Request, path string, page PageResult, limitStr string) string {
	scheme := "http"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	} else if r.TLS != nil {
		scheme = "https"
	}
	absBase := scheme + "://" + r.Host + path

	preserved := make([][2]string, 0)
	for k, vs := range r.URL.Query() {
		if strings.HasPrefix(k, "paging.") {
			continue
		}
		for _, v := range vs {
			preserved = append(preserved, [2]string{k, v})
		}
	}

	build := func(extra ...[2]string) string {
		var b strings.Builder
		b.WriteString(absBase)
		first := true
		write := func(k, v string, encodeVal bool) {
			if first {
				b.WriteByte('?')
				first = false
			} else {
				b.WriteByte('&')
			}
			b.WriteString(k)
			b.WriteByte('=')
			if encodeVal {
				b.WriteString(url.QueryEscape(v))
			} else {
				b.WriteString(v)
			}
		}
		for _, kv := range preserved {
			// Filter values may contain reserved chars — encode them.
			write(kv[0], kv[1], true)
		}
		// Paging values are TAI strings ("<sec>:<nsec>") + the literal
		// limit integer — both safe and required-raw.
		write("paging.limit", limitStr, false)
		for _, kv := range extra {
			write(kv[0], kv[1], false)
		}
		return b.String()
	}

	prev := build([2]string{"paging.until", page.Since})
	next := build([2]string{"paging.since", page.Until})
	first := build([2]string{"paging.since", taiBeforeAll})
	last := build()

	return "<" + prev + ">; rel=\"prev\", <" + next + ">; rel=\"next\", <" + first + ">; rel=\"first\", <" + last + ">; rel=\"last\""
}

// encodeForVersion serialises a typed resource via the IS-04 codec
// for the URL's wire minor. The codec strips fields that don't exist
// on that minor's schema (e.g. v1.0 Flow has no `device_id`, v1.1/v1.2
// drop `authorization`). Returns the serialised bytes + true on
// success; (nil, false) when no codec is registered for apiVer or
// the encode fails — caller falls back to canonical marshal.
func encodeForVersion(t is04.ResourceType, body any, apiVer string) ([]byte, bool) {
	codec, ok := is04.Get(apiVer)
	if !ok {
		return nil, false
	}
	switch t {
	case is04.ResourceNode:
		if v, ok := body.(is04.Node); ok {
			if b, err := codec.EncodeNode(v); err == nil {
				return b, true
			}
		}
	case is04.ResourceDevice:
		if v, ok := body.(is04.Device); ok {
			if b, err := codec.EncodeDevice(v); err == nil {
				return b, true
			}
		}
	case is04.ResourceSource:
		if v, ok := body.(is04.Source); ok {
			if b, err := codec.EncodeSource(v); err == nil {
				return b, true
			}
		}
	case is04.ResourceFlow:
		if v, ok := body.(is04.Flow); ok {
			if b, err := codec.EncodeFlow(v); err == nil {
				return b, true
			}
		}
	case is04.ResourceSender:
		if v, ok := body.(is04.Sender); ok {
			if b, err := codec.EncodeSender(v); err == nil {
				return b, true
			}
		}
	case is04.ResourceReceiver:
		if v, ok := body.(is04.Receiver); ok {
			if b, err := codec.EncodeReceiver(v); err == nil {
				return b, true
			}
		}
	}
	return nil, false
}

// versionAllowed reports whether a resource registered at
// `resourceVer` should be visible at the URL's `urlVer`. IS-04 §6.1.5:
//
//   - exact match always visible
//   - if the client opts in via `query.downgrade=vX`, resources
//     registered at versions in [vX, urlVer] become visible
//   - resourceVer == "" (unstamped) is treated as "any version" so
//     pre-existing fixtures and unit-test plants don't disappear.
func versionAllowed(resourceVer, urlVer, downgrade string) bool {
	if resourceVer == "" || urlVer == "" {
		return true
	}
	if resourceVer == urlVer {
		return true
	}
	if downgrade == "" {
		return false
	}
	// resourceVer must satisfy: downgrade <= resourceVer <= urlVer.
	return apiVerLE(downgrade, resourceVer) && apiVerLE(resourceVer, urlVer)
}

// apiVerLE compares two `vMAJOR.MINOR` strings; returns true when a
// is at or below b. Returns true on parse error so callers fail
// permissively rather than 404'ing on a transient string.
func apiVerLE(a, b string) bool {
	aMaj, aMin, aOK := parseAPIVer(a)
	bMaj, bMin, bOK := parseAPIVer(b)
	if !aOK || !bOK {
		return true
	}
	if aMaj != bMaj {
		return aMaj < bMaj
	}
	return aMin <= bMin
}

func parseAPIVer(v string) (maj, min int, ok bool) {
	if len(v) < 4 || v[0] != 'v' {
		return 0, 0, false
	}
	dot := strings.IndexByte(v, '.')
	if dot < 1 {
		return 0, 0, false
	}
	mj, err := strconv.Atoi(v[1:dot])
	if err != nil {
		return 0, 0, false
	}
	mn, err := strconv.Atoi(v[dot+1:])
	if err != nil {
		return 0, 0, false
	}
	return mj, mn, true
}

// rqlEq is one parsed RQL `eq(field,value)` predicate.
type rqlEq struct {
	Field string
	Value string
}

// splitFilterParams splits the query map into the basic-equality
// pairs and (at most) one RQL predicate. Pagination + control
// parameters are dropped — they're handled higher up the stack.
func splitFilterParams(q map[string][]string) (flat map[string][]string, rql *rqlEq) {
	flat = make(map[string][]string)
	for k, vs := range q {
		switch {
		case strings.HasPrefix(k, "paging."):
			continue
		case k == "query.rql":
			if len(vs) > 0 {
				rql = parseRQLEq(vs[0])
			}
		case strings.HasPrefix(k, "query."):
			// query.downgrade and friends — not filters.
			continue
		default:
			flat[k] = vs
		}
	}
	if len(flat) == 0 {
		flat = nil
	}
	return flat, rql
}

// parseRQLEq accepts the `eq(field,value)` shape and returns the
// parsed predicate, or nil for anything else. Value strings that
// arrive percent-encoded have already been decoded by net/url.
func parseRQLEq(expr string) *rqlEq {
	expr = strings.TrimSpace(expr)
	if !strings.HasPrefix(expr, "eq(") || !strings.HasSuffix(expr, ")") {
		return nil
	}
	body := expr[len("eq(") : len(expr)-1]
	comma := strings.Index(body, ",")
	if comma < 0 {
		return nil
	}
	field := strings.TrimSpace(body[:comma])
	value := strings.TrimSpace(body[comma+1:])
	if field == "" {
		return nil
	}
	return &rqlEq{Field: field, Value: value}
}

// rqlMatch returns true when item's `field` equals predicate.Value.
func rqlMatch(item reflect.Value, p *rqlEq) bool {
	return matchesField(item, p.Field, []string{p.Value})
}

func matchesAll(item reflect.Value, q map[string][]string) bool {
	if q == nil {
		return true
	}
	for k, vs := range q {
		if !matchesField(item, k, vs) {
			return false
		}
	}
	return true
}

// matchesField reads the named JSON field from item (via reflect) and
// compares against any of the requested values.
func matchesField(item reflect.Value, name string, vs []string) bool {
	if item.Kind() == reflect.Pointer {
		item = item.Elem()
	}
	if item.Kind() != reflect.Struct {
		return false
	}
	got := readJSONField(item, name)
	for _, v := range vs {
		if got == v {
			return true
		}
	}
	return false
}

// readJSONField walks the struct's fields looking for one whose
// `json:` tag matches name. Returns "" on miss.
func readJSONField(s reflect.Value, name string) string {
	t := s.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		if tag == "" {
			tag = f.Name
		}
		if tag != name {
			// Walk embedded structs (resource_core).
			if f.Anonymous && f.Type.Kind() == reflect.Struct {
				if got := readJSONField(s.Field(i), name); got != "" {
					return got
				}
			}
			continue
		}
		fv := s.Field(i)
		if fv.Kind() == reflect.String {
			return fv.String()
		}
	}
	return ""
}
