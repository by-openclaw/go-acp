package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"dhs/internal/amwa/codec/sdp"
)

// checkRoot fetches /x-nmos and records which APIs the device serves.
// Every later check depends on this, so it also populates the session.
func checkRoot(ctx context.Context, s *Session) []Result {
	const (
		id   = "PROFILE-ROOT-001"
		name = "/x-nmos lists the APIs served"
		spec = "IS-04 v1.3 §4 API paths"
	)
	r := s.get(ctx, "/x-nmos")
	switch {
	case r.Err != nil:
		return []Result{s.result(id, name, spec, StatusFail, r,
			fmt.Sprintf("GET /x-nmos did not complete: %v", r.Err))}
	case r.Status != http.StatusOK:
		return []Result{s.result(id, name, spec, StatusFail, r,
			fmt.Sprintf("GET /x-nmos returned %d, want 200", r.Status))}
	}

	names, ok := jsonArray(r.Body)
	if !ok {
		return []Result{s.result(id, name, spec, StatusFail, r,
			"GET /x-nmos did not return a JSON array of API names")}
	}

	out := []Result{s.result(id, name, spec, StatusPass, r,
		fmt.Sprintf("GET /x-nmos returned %d API(s): %s", len(names), strings.Join(names, " ")))}

	// Discover the version list for each, so the later checks know what
	// to ask for. Failures here are reported by checkVersionLists.
	for _, api := range names {
		api = strings.TrimSuffix(api, "/")
		if api == "" {
			continue
		}
		vr := s.get(ctx, "/x-nmos/"+api+"/")
		if vr.Status != http.StatusOK {
			s.APIs[api] = nil
			continue
		}
		vs, ok := jsonArray(vr.Body)
		if !ok {
			s.APIs[api] = nil
			continue
		}
		for i := range vs {
			vs[i] = strings.TrimSuffix(vs[i], "/")
		}
		s.APIs[api] = vs
	}
	return out
}

// checkVersionLists asserts every advertised version is well-formed.
//
// A version string a controller cannot parse is a version it cannot
// select, and version selection is the first thing every NMOS peer
// does.
func checkVersionLists(_ context.Context, s *Session) []Result {
	const (
		id   = "PROFILE-VER-001"
		name = "each API advertises parseable versions"
		spec = "IS-04 v1.3 §6.1 API versioning"
	)
	if len(s.APIs) == 0 {
		return []Result{s.skip(id, name, spec, "no APIs were discovered")}
	}

	var out []Result
	for _, api := range sortedKeys(s.APIs) {
		vs := s.APIs[api]
		res := Result{ID: id, Name: name, Spec: spec, Target: s.opts.Target}
		switch {
		case len(vs) == 0:
			res.Status = StatusFail
			res.Detail = fmt.Sprintf("GET /x-nmos/%s/ returned no usable version list", api)
		default:
			bad := []string{}
			for _, v := range vs {
				if !versionPattern.MatchString(v) {
					bad = append(bad, v)
				}
			}
			if len(bad) > 0 {
				res.Status = StatusFail
				res.Detail = fmt.Sprintf("/x-nmos/%s/ advertises %v, which is not vMAJOR.MINOR", api, bad)
			} else {
				res.Status = StatusPass
				res.Detail = fmt.Sprintf("/x-nmos/%s/ advertises %s", api, strings.Join(vs, ","))
			}
		}
		out = append(out, res)
	}
	return out
}

// checkUnknownVersionRejected asks for a version the device does not
// serve.
//
// A device that answers 200 to /x-nmos/query/v9.9/nodes is telling a
// controller it speaks a version it does not, and the controller will
// then send v9.9 requests it cannot handle. The failure surfaces later,
// somewhere else, as malformed data.
func checkUnknownVersionRejected(ctx context.Context, s *Session) []Result {
	const (
		id   = "PROFILE-VER-002"
		name = "an unknown API version is rejected"
		spec = "IS-04 v1.3 §6.1 API versioning"
	)
	api, _, ok := s.queryOrNode()
	if !ok {
		return []Result{s.skip(id, name, spec, "device serves neither a query nor a node API")}
	}

	path := "/x-nmos/" + api + "/v9.9/"
	r := s.get(ctx, path)
	switch {
	case r.Err != nil:
		return []Result{s.result(id, name, spec, StatusFail, r,
			fmt.Sprintf("GET %s did not complete: %v", path, r.Err))}
	case r.Status == http.StatusNotFound:
		return []Result{s.result(id, name, spec, StatusPass, r,
			fmt.Sprintf("GET %s returned 404", path))}
	case r.Status >= 500:
		return []Result{s.result(id, name, spec, StatusFail, r,
			fmt.Sprintf("GET %s returned %d — an unsupported version must 404, not fault", path, r.Status))}
	case r.Status < 400:
		return []Result{s.result(id, name, spec, StatusFail, r,
			fmt.Sprintf("GET %s returned %d — the device claims to serve a version it does not advertise", path, r.Status))}
	default:
		return []Result{s.result(id, name, spec, StatusWarn, r,
			fmt.Sprintf("GET %s returned %d; 404 is the spec's answer", path, r.Status))}
	}
}

// checkCORS asserts the headers a browser-based controller needs.
//
// IS-04 requires CORS on every API. Without it every web controller
// fails at the first request, with an error that names the browser
// rather than the device — which is why this is worth asserting
// explicitly rather than discovering during an integration.
func checkCORS(ctx context.Context, s *Session) []Result {
	const spec = "IS-04 v1.3 §5 Cross-Origin Resource Sharing"

	get := s.get(ctx, "/x-nmos")
	origin := get.Header.Get("Access-Control-Allow-Origin")

	out := []Result{}
	switch {
	case get.Err != nil || get.Status != http.StatusOK:
		out = append(out, s.skip("PROFILE-CORS-001", "GET carries Access-Control-Allow-Origin", spec,
			"/x-nmos did not answer"))
	case origin == "":
		out = append(out, s.result("PROFILE-CORS-001", "GET carries Access-Control-Allow-Origin", spec,
			StatusFail, get, "GET /x-nmos returned no Access-Control-Allow-Origin header — no browser-based controller can read this API"))
	default:
		out = append(out, s.result("PROFILE-CORS-001", "GET carries Access-Control-Allow-Origin", spec,
			StatusPass, get, "Access-Control-Allow-Origin: "+origin))
	}

	// The preflight matters separately: a browser sends OPTIONS before
	// any request carrying a custom header, and a device that answers
	// GET correctly can still refuse the preflight.
	pre := s.request(ctx, http.MethodOptions, "/x-nmos", http.Header{
		"Origin":                        []string{"http://profile.invalid"},
		"Access-Control-Request-Method": []string{"GET"},
	})
	const (
		preID   = "PROFILE-CORS-002"
		preName = "OPTIONS preflight is answered"
	)
	switch {
	case pre.Err != nil:
		out = append(out, s.result(preID, preName, spec, StatusFail, pre,
			fmt.Sprintf("OPTIONS /x-nmos did not complete: %v", pre.Err)))
	case pre.Status >= 400:
		out = append(out, s.result(preID, preName, spec, StatusFail, pre,
			fmt.Sprintf("OPTIONS /x-nmos returned %d — browsers will not proceed past the preflight", pre.Status)))
	case pre.Header.Get("Access-Control-Allow-Methods") == "":
		out = append(out, s.result(preID, preName, spec, StatusWarn, pre,
			fmt.Sprintf("OPTIONS /x-nmos returned %d but named no Access-Control-Allow-Methods", pre.Status)))
	default:
		out = append(out, s.result(preID, preName, spec, StatusPass, pre,
			"Access-Control-Allow-Methods: "+pre.Header.Get("Access-Control-Allow-Methods")))
	}
	return out
}

// checkContentType asserts JSON endpoints say they serve JSON.
func checkContentType(ctx context.Context, s *Session) []Result {
	const (
		id   = "PROFILE-CT-001"
		name = "JSON endpoints declare application/json"
		spec = "IS-04 v1.3 §4 — all APIs use application/json"
	)
	r := s.get(ctx, "/x-nmos")
	if r.Err != nil || r.Status != http.StatusOK {
		return []Result{s.skip(id, name, spec, "/x-nmos did not answer")}
	}
	ct := r.Header.Get("Content-Type")
	switch {
	case ct == "":
		return []Result{s.result(id, name, spec, StatusFail, r, "GET /x-nmos returned no Content-Type")}
	case strings.HasPrefix(strings.ToLower(ct), "application/json"):
		return []Result{s.result(id, name, spec, StatusPass, r, "Content-Type: "+ct)}
	default:
		return []Result{s.result(id, name, spec, StatusFail, r,
			fmt.Sprintf("GET /x-nmos returned Content-Type %q, want application/json", ct))}
	}
}

// checkUnknownResource asks for a resource that does not exist.
//
// The distinction that matters is 404 versus 500. A controller retries
// a 500 — it reads as "the device is unwell" — and gives up on a 404,
// which reads as "that thing is gone". A device that 500s on a missing
// resource turns every stale reference into a retry storm.
func checkUnknownResource(ctx context.Context, s *Session) []Result {
	const (
		id   = "PROFILE-404-001"
		name = "a missing resource returns 404, not a fault"
		spec = "IS-04 v1.3 §4 error response"
	)
	api, ver, ok := s.queryOrNode()
	if !ok {
		return []Result{s.skip(id, name, spec, "device serves neither a query nor a node API")}
	}
	if api == "node" {
		return []Result{s.skip(id, name, spec, "node API has no per-id resource path to check safely")}
	}

	const absent = "00000000-0000-4000-8000-000000000000"
	path := "/x-nmos/" + api + "/" + ver + "/nodes/" + absent
	r := s.get(ctx, path)

	out := []Result{}
	switch {
	case r.Err != nil:
		out = append(out, s.result(id, name, spec, StatusFail, r,
			fmt.Sprintf("GET %s did not complete: %v", path, r.Err)))
		return out
	case r.Status == http.StatusNotFound:
		out = append(out, s.result(id, name, spec, StatusPass, r,
			fmt.Sprintf("GET %s returned 404", path)))
	case r.Status >= 500:
		out = append(out, s.result(id, name, spec, StatusFail, r,
			fmt.Sprintf("GET %s returned %d — controllers retry a fault and abandon a 404", path, r.Status)))
		return out
	default:
		out = append(out, s.result(id, name, spec, StatusFail, r,
			fmt.Sprintf("GET %s returned %d for a resource that does not exist", path, r.Status)))
		return out
	}

	// IS-04 defines the error body shape. A controller that wants to
	// tell the operator WHY has nothing to show without it.
	const (
		bodyID   = "PROFILE-404-002"
		bodyName = "the 404 body carries the IS-04 error object"
	)
	var e struct {
		Code  *int    `json:"code"`
		Error *string `json:"error"`
		Debug any     `json:"debug"`
	}
	switch {
	case json.Unmarshal(r.Body, &e) != nil:
		out = append(out, s.result(bodyID, bodyName, spec, StatusWarn, r,
			"the 404 body is not a JSON object"))
	case e.Code == nil || e.Error == nil:
		out = append(out, s.result(bodyID, bodyName, spec, StatusWarn, r,
			"the 404 body omits code and/or error"))
	default:
		out = append(out, s.result(bodyID, bodyName, spec, StatusPass, r,
			fmt.Sprintf("404 body: code=%d error=%q", *e.Code, *e.Error)))
	}
	return out
}

// checkTrailingSlash asserts a version root answers with or without its
// trailing slash.
//
// Controllers build these URLs by concatenation and disagree about the
// slash. A device that serves one and 404s the other works with half
// the controllers on the market.
func checkTrailingSlash(ctx context.Context, s *Session) []Result {
	const (
		id   = "PROFILE-PATH-001"
		name = "a version root answers with and without a trailing slash"
		spec = "IS-04 v1.3 §4 API paths"
	)
	api, ver, ok := s.queryOrNode()
	if !ok {
		return []Result{s.skip(id, name, spec, "device serves neither a query nor a node API")}
	}

	base := "/x-nmos/" + api + "/" + ver
	with := s.get(ctx, base+"/")
	without := s.get(ctx, base)

	okStatus := func(c int) bool { return c == http.StatusOK || (c >= 300 && c < 400) }
	switch {
	case with.Err != nil || without.Err != nil:
		return []Result{s.result(id, name, spec, StatusFail, with, "one of the two requests did not complete")}
	case okStatus(with.Status) && okStatus(without.Status):
		return []Result{s.result(id, name, spec, StatusPass, with,
			fmt.Sprintf("%s/ → %d, %s → %d", base, with.Status, base, without.Status))}
	default:
		return []Result{s.result(id, name, spec, StatusWarn, with,
			fmt.Sprintf("%s/ → %d but %s → %d; controllers build these URLs both ways",
				base, with.Status, base, without.Status))}
	}
}

// checkQueryPaging asserts the Query API's paging contract.
//
// This is the defect that produced an 11-node capture of a 68-node
// registry, and it is invisible unless you ask for a limit and count
// what comes back.
func checkQueryPaging(ctx context.Context, s *Session) []Result {
	const spec = "IS-04 v1.3 §7 Query API paging"
	vs, has := s.APIs["query"]
	if !has || len(vs) == 0 {
		return []Result{s.skip("PROFILE-PAGE-001", "paging.limit is honoured", spec, "device serves no query API")}
	}
	ver := highestVersion(vs)
	path := "/x-nmos/query/" + ver + "/nodes?paging.limit=1"
	r := s.get(ctx, path)

	if r.Err != nil || r.Status != http.StatusOK {
		return []Result{s.skip("PROFILE-PAGE-001", "paging.limit is honoured", spec,
			fmt.Sprintf("GET %s returned %d", path, r.Status))}
	}

	var items []json.RawMessage
	if json.Unmarshal(r.Body, &items) != nil {
		return []Result{s.result("PROFILE-PAGE-001", "paging.limit is honoured", spec, StatusFail, r,
			fmt.Sprintf("GET %s did not return a JSON array", path))}
	}

	out := []Result{}
	if len(items) > 1 {
		out = append(out, s.result("PROFILE-PAGE-001", "paging.limit is honoured", spec, StatusFail, r,
			fmt.Sprintf("GET %s returned %d resources", path, len(items))))
	} else {
		out = append(out, s.result("PROFILE-PAGE-001", "paging.limit is honoured", spec, StatusPass, r,
			fmt.Sprintf("GET %s returned %d resource(s)", path, len(items))))
	}

	// The paging headers are how a client knows where it is. Without
	// them it cannot resume, and cannot tell a short page from the end.
	const (
		hdrID   = "PROFILE-PAGE-002"
		hdrName = "paging headers are returned"
	)
	missing := []string{}
	for _, h := range []string{"X-Paging-Limit", "X-Paging-Since", "X-Paging-Until"} {
		if r.Header.Get(h) == "" {
			missing = append(missing, h)
		}
	}
	if len(missing) > 0 {
		out = append(out, s.result(hdrID, hdrName, spec, StatusWarn, r,
			fmt.Sprintf("GET %s omitted %s", path, strings.Join(missing, ", "))))
	} else {
		out = append(out, s.result(hdrID, hdrName, spec, StatusPass, r,
			fmt.Sprintf("X-Paging-Limit=%s Since=%s Until=%s",
				r.Header.Get("X-Paging-Limit"), r.Header.Get("X-Paging-Since"), r.Header.Get("X-Paging-Until"))))
	}

	// A Link: rel="next" is what a walker follows. Its absence on a
	// full page means the catalogue cannot be walked to the end.
	const (
		linkID   = "PROFILE-PAGE-003"
		linkName = "a truncated page carries Link rel=next"
	)
	hasNext := strings.Contains(r.Header.Get("Link"), `rel="next"`) || strings.Contains(r.Header.Get("Link"), "rel=next")
	switch {
	case len(items) == 0:
		out = append(out, s.skip(linkID, linkName, spec, "the registry listed no nodes, so there is no next page to offer"))
	case hasNext:
		out = append(out, s.result(linkID, linkName, spec, StatusPass, r, "Link: "+r.Header.Get("Link")))
	default:
		out = append(out, s.result(linkID, linkName, spec, StatusWarn, r,
			"a limited page carried no Link rel=next; either the catalogue holds exactly one node, or it cannot be paged to the end"))
	}
	return out
}

// checkQueryDowngrade asserts the escape hatch from version isolation.
//
// A registry serving v1.1 and v1.3 hides v1.1-registered resources from
// a v1.3 query. `query.downgrade` is the spec's answer, and a registry
// that does not implement it makes the highest minor the wrong one to
// ask on — which is the opposite of what every controller does by
// default.
func checkQueryDowngrade(ctx context.Context, s *Session) []Result {
	const (
		id   = "PROFILE-VER-003"
		name = "query.downgrade is supported"
		spec = "IS-04 v1.3 §6.1 query.downgrade"
	)
	vs, has := s.APIs["query"]
	if !has || len(vs) < 2 {
		return []Result{s.skip(id, name, spec, "registry serves fewer than two query minors, so isolation cannot arise")}
	}

	top := highestVersion(vs)
	lowest := top
	for _, v := range vs {
		if v != top {
			lowest = v
			break
		}
	}

	plain := s.get(ctx, "/x-nmos/query/"+top+"/nodes")
	down := s.get(ctx, "/x-nmos/query/"+top+"/nodes?query.downgrade="+lowest)
	if plain.Status != http.StatusOK || down.Status != http.StatusOK {
		if down.Status >= 400 {
			return []Result{s.result(id, name, spec, StatusFail, down,
				fmt.Sprintf("GET /x-nmos/query/%s/nodes?query.downgrade=%s returned %d", top, lowest, down.Status))}
		}
		return []Result{s.skip(id, name, spec, "the query could not be compared")}
	}

	var a, b []json.RawMessage
	if json.Unmarshal(plain.Body, &a) != nil || json.Unmarshal(down.Body, &b) != nil {
		return []Result{s.result(id, name, spec, StatusFail, down, "one of the two responses was not a JSON array")}
	}

	switch {
	case len(b) > len(a):
		return []Result{s.result(id, name, spec, StatusPass, down,
			fmt.Sprintf("%s lists %d nodes, downgraded to %s lists %d — isolation is escapable", top, len(a), lowest, len(b)))}
	case len(b) == len(a):
		return []Result{s.result(id, name, spec, StatusPass, down,
			fmt.Sprintf("downgrade accepted; both list %d nodes (nothing is registered at a lower minor)", len(a)))}
	default:
		return []Result{s.result(id, name, spec, StatusFail, down,
			fmt.Sprintf("downgrading to %s returned FEWER nodes (%d) than %s (%d)", lowest, len(b), top, len(a)))}
	}
}

// checkHeartbeatUnknownNode probes the Registration API's answer for a
// node it does not know.
//
// This is read-only: a GET, never a POST. The registry must answer 404,
// because that is the signal a node uses to know it has been garbage
// collected and must re-register. A registry that 200s or 500s here
// leaves a dropped node believing it is still registered.
func checkHeartbeatUnknownNode(ctx context.Context, s *Session) []Result {
	const (
		id   = "PROFILE-REG-001"
		name = "health for an unregistered node returns 404"
		spec = "IS-04 v1.3 §4.2 Registration API health"
	)
	vs, has := s.APIs["registration"]
	if !has || len(vs) == 0 {
		return []Result{s.skip(id, name, spec, "device serves no registration API")}
	}

	const absent = "00000000-0000-4000-8000-000000000000"
	path := "/x-nmos/registration/" + highestVersion(vs) + "/health/nodes/" + absent
	r := s.get(ctx, path)

	switch {
	case r.Err != nil:
		return []Result{s.result(id, name, spec, StatusFail, r,
			fmt.Sprintf("GET %s did not complete: %v", path, r.Err))}
	case r.Status == http.StatusNotFound:
		return []Result{s.result(id, name, spec, StatusPass, r,
			fmt.Sprintf("GET %s returned 404", path))}
	case r.Status == http.StatusMethodNotAllowed:
		return []Result{s.result(id, name, spec, StatusWarn, r,
			fmt.Sprintf("GET %s returned 405; the health endpoint is POST-only here, so the read-only profile cannot assert it", path))}
	case r.Status >= 500:
		return []Result{s.result(id, name, spec, StatusFail, r,
			fmt.Sprintf("GET %s returned %d — a node cannot tell it was garbage collected", path, r.Status))}
	default:
		return []Result{s.result(id, name, spec, StatusFail, r,
			fmt.Sprintf("GET %s returned %d for a node that is not registered", path, r.Status))}
	}
}

// checkTransportFileContentType asserts an IS-05 transport file is
// served as SDP.
//
// A receiver handed `text/html` will not parse it, and the failure
// appears as a route that silently does not come up.
func checkTransportFileContentType(ctx context.Context, s *Session) []Result {
	const (
		id   = "PROFILE-IS05-001"
		name = "a transport file is served as application/sdp"
		spec = "IS-05 v1.1 §4.2 transportfile"
	)
	vs, has := s.APIs["connection"]
	if !has || len(vs) == 0 {
		return []Result{s.skip(id, name, spec, "device serves no connection API")}
	}
	ver := highestVersion(vs)

	list := s.get(ctx, "/x-nmos/connection/"+ver+"/single/senders")
	ids, ok := jsonArray(list.Body)
	if list.Status != http.StatusOK || !ok || len(ids) == 0 {
		return []Result{s.skip(id, name, spec, "the device publishes no IS-05 senders")}
	}

	// One sender by default. --deep asserts every one, which is what
	// catches a device that is broken on some of them — the shape a
	// 502-on-every-transportfile device has.
	if !s.opts.Deep {
		ids = ids[:1]
	}

	var out []Result
	for _, sid := range ids {
		sid = strings.TrimSuffix(sid, "/")
		path := "/x-nmos/connection/" + ver + "/single/senders/" + sid + "/transportfile"
		r := s.get(ctx, path)
		ct := r.Header.Get("Content-Type")
		switch {
		case r.Err != nil:
			out = append(out, s.result(id, name, spec, StatusFail, r,
				fmt.Sprintf("GET %s did not complete: %v", path, r.Err)))
		case r.Status >= 500:
			out = append(out, s.result(id, name, spec, StatusFail, r,
				fmt.Sprintf("GET %s returned %d — the endpoint exists and is broken", path, r.Status)))
		case r.Status == http.StatusNotFound:
			// Legal for a sender that is not transmitting.
			out = append(out, s.result(id, name, spec, StatusWarn, r,
				fmt.Sprintf("GET %s returned 404 (legal only while the sender is inactive)", path)))
		case r.Status != http.StatusOK:
			out = append(out, s.result(id, name, spec, StatusFail, r,
				fmt.Sprintf("GET %s returned %d", path, r.Status)))
		case !strings.HasPrefix(strings.ToLower(ct), "application/sdp"):
			out = append(out, s.result(id, name, spec, StatusFail, r,
				fmt.Sprintf("GET %s returned Content-Type %q, want application/sdp", path, ct)))
		case !strings.HasPrefix(strings.TrimSpace(string(r.Body)), "v="):
			out = append(out, s.result(id, name, spec, StatusFail, r,
				fmt.Sprintf("GET %s returned a body that does not start with `v=` — not an SDP", path)))
		default:
			out = append(out, s.result(id, name, spec, StatusPass, r,
				fmt.Sprintf("GET %s returned %d bytes of application/sdp", path, len(r.Body))))
		}
	}
	return out
}

// checkSenderSDPConformance fetches a live sender's transport file and
// parses it with codec/sdp (#850). A non-conformant SDP a controller
// relays verbatim is the invisible cause of a route that "comes up"
// and decodes nothing — this is the live counterpart to the offline
// audit's SDP checks. Deviations are WARN (permitted-but-risky), an
// unparseable body is FAIL. A 404 (inactive sender) is a SKIP: there
// is no SDP to judge.
func checkSenderSDPConformance(ctx context.Context, s *Session) []Result {
	const (
		id   = "PROFILE-SDP-001"
		name = "served SDP transport files are ST 2110 conformant"
		spec = "SMPTE ST 2110 / RFC 4566 SDP"
	)
	vs, has := s.APIs["connection"]
	if !has || len(vs) == 0 {
		return []Result{s.skip(id, name, spec, "device serves no connection API")}
	}
	ver := highestVersion(vs)

	list := s.get(ctx, "/x-nmos/connection/"+ver+"/single/senders")
	ids, ok := jsonArray(list.Body)
	if list.Status != http.StatusOK || !ok || len(ids) == 0 {
		return []Result{s.skip(id, name, spec, "the device publishes no IS-05 senders")}
	}
	if !s.opts.Deep {
		ids = ids[:1]
	}

	var out []Result
	for _, sid := range ids {
		sid = strings.TrimSuffix(sid, "/")
		path := "/x-nmos/connection/" + ver + "/single/senders/" + sid + "/transportfile"
		r := s.get(ctx, path)
		switch {
		case r.Status == http.StatusNotFound:
			out = append(out, s.result(id, name, spec, StatusSkip, r,
				fmt.Sprintf("sender %s has no transport file (inactive) — no SDP to check", sid)))
			continue
		case r.Err != nil || r.Status != http.StatusOK:
			out = append(out, s.result(id, name, spec, StatusSkip, r,
				fmt.Sprintf("sender %s transport file not retrievable (status %d) — covered by PROFILE-IS05-001", sid, r.Status)))
			continue
		}
		sess, devs, err := sdp.Parse(string(r.Body))
		switch {
		case err != nil:
			out = append(out, s.result(id, name, spec, StatusFail, r,
				fmt.Sprintf("sender %s SDP is structurally invalid: %v", sid, err)))
		case len(devs) > 0:
			out = append(out, s.result(id, name, spec, StatusWarn, r,
				fmt.Sprintf("sender %s SDP has %d deviation(s), first: line %d %s", sid, len(devs), devs[0].Line, devs[0].Reason)))
		case dupUnderfilled(sess):
			out = append(out, s.result(id, name, spec, StatusWarn, r,
				fmt.Sprintf("sender %s SDP declares a=group:DUP but only one leg resolves (ST 2022-7 redundancy not carried)", sid)))
		default:
			out = append(out, s.result(id, name, spec, StatusPass, r,
				fmt.Sprintf("sender %s SDP is conformant (%d media section(s))", sid, len(sess.Media))))
		}
	}
	return out
}

// dupUnderfilled reports an a=group:DUP that resolves to fewer than two
// legs — the same rule the offline audit applies.
func dupUnderfilled(sess *sdp.Session) bool {
	hasDUP := false
	for _, g := range sess.Groups {
		if g.Semantics == "DUP" {
			hasDUP = true
		}
	}
	return hasDUP && len(sess.Legs()) < 2
}

func sortedKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// A stable order keeps two runs diffable.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
