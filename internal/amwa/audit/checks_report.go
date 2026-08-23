package audit

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The exporter's report.txt is the only place HTTP status codes
// survive: tree.json records what a request returned, not how it
// returned it. Anything status-shaped is therefore parsed back out of
// the report rather than re-derived from the tree.
var (
	statusLine = regexp.MustCompile(`^(\d{3}|ERR)\s+(\S+)`)
	pageLine   = regexp.MustCompile(`^\d{3}\s+(\S+)\s+\(page (\d+), \+(\d+), total (\d+)\)`)
	skipLine   = regexp.MustCompile(`^SKIP\s+node\s+(\S+)\s+'([^']*)'\s+unreachable at:\s*(.*)$`)
	pagingDupe = regexp.MustCompile(`^NOTE\s+(\S+)\s+returned\s+(\d+)\s+rows across pages,\s+(\d+)\s+unique`)
	cappedLine = regexp.MustCompile(`^CAPPED\s+(\d+)\s+node`)
)

// checkTransportReport turns the exporter's per-request record into
// findings: server faults, unreachable registered nodes, and the
// registry paging defects that silently truncate a catalogue.
func checkTransportReport(h *Harvest) []Finding {
	if len(h.Report) == 0 {
		return nil
	}

	var out []Finding
	var stuck []string
	// Server faults are grouped by the shape of the path, not the exact
	// URL: a device that 502s on every one of 176 senders should
	// produce one finding naming 176, not 176 findings.
	type bucket struct {
		code    string
		example string
		n       int
	}
	faults := map[string]*bucket{}

	for _, line := range h.Report {
		switch {
		case skipLine.MatchString(line):
			m := skipLine.FindStringSubmatch(line)
			out = append(out, h.find(
				"NMOS-NODE-UNREACHABLE", SevCritical, "node/"+m[1],
				fmt.Sprintf("node %q is registered but did not answer at %s", m[2], m[3]),
				"IS-04 v1.3 §4.2 Registration lifecycle",
				"either the node died without deregistering, or its href is unreachable from the controller network"))

		case pagingDupe.MatchString(line):
			m := pagingDupe.FindStringSubmatch(line)
			rows, _ := strconv.Atoi(m[2])
			uniq, _ := strconv.Atoi(m[3])
			if rows > uniq {
				out = append(out, h.find(
					"NMOS-QUERY-PAGING-DUPES", SevWarn, m[1],
					fmt.Sprintf("paging returned %d rows for %d unique resources", rows, uniq),
					"IS-04 v1.3 §7 Query API paging",
					"a cursor that re-serves rows can also skip them; page boundaries are not stable"))
			}

		case strings.HasPrefix(line, "WARN  paging cursor did not advance"):
			// One defect, not one per collection. A registry serving
			// four minors × six collections produces 24 identical lines,
			// and 24 identical findings bury everything else in the
			// report.
			stuck = append(stuck, pathOfStuckLine(line))

		case cappedLine.MatchString(line):
			m := cappedLine.FindStringSubmatch(line)
			out = append(out, h.find(
				"NMOS-AUDIT-COVERAGE-CAPPED", SevInfo, "",
				fmt.Sprintf("%s node(s) were not followed by the exporter — this audit does not cover them", m[1]),
				"", "re-export with a higher node cap for full plant coverage"))

		case statusLine.MatchString(line):
			m := statusLine.FindStringSubmatch(line)
			code, path := m[1], m[2]
			if code != "ERR" && !strings.HasPrefix(code, "5") && !isNotableClientError(code, path) {
				continue
			}
			key := code + " " + shapeOf(path)
			b, ok := faults[key]
			if !ok {
				b = &bucket{code: code, example: path}
				faults[key] = b
			}
			b.n++
		}
	}

	// Suppressing findings against a possibly-short collection is only
	// honest if the suppression itself is reported. Otherwise a
	// truncated capture reads as a clean plant.
	if short := truncatedKinds(h.Report); len(short) > 0 {
		kinds := make([]string, 0, len(short))
		for k := range short {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		out = append(out, h.find(
			"NMOS-AUDIT-COLLECTION-MAYBE-SHORT", SevWarn, "query",
			fmt.Sprintf("the walk of %s ended on an empty page directly after a full one",
				strings.Join(kinds, ", ")),
			"IS-04 v1.3 §7 Query API paging",
			"either the registry holds exactly one page, or its cursor stopped advancing; checks that reason from a missing resource were suppressed for these"))
	}

	if len(stuck) > 0 {
		sort.Strings(stuck)
		detail := fmt.Sprintf("the paging cursor did not advance for %s", stuck[0])
		if len(stuck) > 1 {
			detail = fmt.Sprintf("the paging cursor did not advance for %d collection(s), e.g. %s",
				len(stuck), stuck[0])
		}
		out = append(out, h.find(
			"NMOS-QUERY-PAGING-STUCK", SevError, "query",
			detail,
			"IS-04 v1.3 §7 Query API paging",
			"resources were still arriving when the cursor repeated — the catalogue cannot be walked to the end"))
	}

	keys := make([]string, 0, len(faults))
	for k := range faults {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b := faults[k]
		shape := strings.TrimPrefix(k, b.code+" ")
		switch {
		case b.code == "ERR":
			out = append(out, h.find(
				"NMOS-HTTP-UNREACHABLE", SevError, shape,
				fmt.Sprintf("%d request(s) to %s did not complete (e.g. %s)", b.n, shape, b.example),
				"", "connection refused, timed out, or TLS failed — the endpoint is not serving"))
		case strings.HasPrefix(b.code, "5"):
			out = append(out, h.find(
				"NMOS-HTTP-SERVER-FAULT", SevError, shape,
				fmt.Sprintf("%d request(s) to %s returned HTTP %s (e.g. %s)", b.n, shape, b.code, b.example),
				"IS-04 / IS-05 — a spec-defined endpoint must not 5xx",
				"the endpoint exists and is broken; a controller sees this as the device failing, not as absent"))
		default:
			out = append(out, h.find(
				"NMOS-HTTP-MISSING-ENDPOINT", SevError, shape,
				fmt.Sprintf("%d request(s) to %s returned HTTP %s (e.g. %s)", b.n, shape, b.code, b.example),
				"IS-05 v1.1 §4.2 transportfile",
				"the sender is RTP and advertises the API, but the transport file it must serve is not there"))
		}
	}
	return out
}

// stuckPath pulls the collection path out of the exporter's
// stuck-cursor line.
var stuckPath = regexp.MustCompile(`did not advance for (\S+)`)

func pathOfStuckLine(line string) string {
	if m := stuckPath.FindStringSubmatch(line); m != nil {
		return m[1]
	}
	return "query"
}

// isNotableClientError keeps the 4xx noise out. Most 4xx in an export
// are the exporter probing an API the device does not have, which is
// not a defect. A 4xx on a transport file is different: the endpoint is
// mandatory for an RTP sender on an API the device chose to serve.
func isNotableClientError(code, path string) bool {
	return strings.HasPrefix(code, "4") && strings.Contains(path, "/transportfile")
}

// uuidInPath matches any UUID inside a URL path segment.
var uuidInPath = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// shapeOf collapses a concrete URL to its endpoint shape so 176
// per-sender faults group into one finding.
func shapeOf(p string) string {
	if i := strings.Index(p, "/x-nmos/"); i >= 0 {
		p = p[i:]
	}
	return uuidInPath.ReplaceAllString(p, "{id}")
}

// checkQueryVersionIsolation compares the resource counts a Registry
// returned at each minor it serves.
//
// IS-04 isolates versions: a resource registered at v1.1 is not visible
// on a v1.3 query unless the client asks for `query.downgrade`. A
// controller that speaks only the highest minor therefore sees a subset
// of the plant, silently — the missing devices do not error, they are
// simply absent. This is read from the captured counts, not from a
// vendor claim.
func checkQueryVersionIsolation(h *Harvest) []Finding {
	if _, ok := h.APIs["query"]; !ok {
		return nil
	}

	var out []Finding
	for _, kind := range resourceKinds {
		_, counts := h.resourcesEveryVersion("query", kind)
		if len(counts) < 2 {
			continue
		}
		vs := sortedVersionsDesc(keysOf2(counts))
		top := vs[0]
		for _, v := range vs[1:] {
			if counts[v] <= counts[top] {
				continue
			}
			out = append(out, h.find(
				"NMOS-QUERY-VERSION-ISOLATION", SevError, "query/"+kind,
				fmt.Sprintf("%s lists %d %s at %s but only %d at %s — %d resource(s) are invisible to a %s controller",
					kind, counts[v], kind, v, counts[top], top, counts[v]-counts[top], top),
				"IS-04 v1.3 §6.1 API versioning / query.downgrade",
				"a controller on the highest minor must request query.downgrade, or it will never see these devices"))
		}
	}
	return out
}

// truncatedKinds reads the exporter's per-page record and names the
// resource kinds whose walk may have stopped short.
//
// The signature is a walk that ended on an empty page immediately after
// a FULL one. A registry holding exactly one page of resources produces
// the same trace as a registry whose paging cursor stops advancing, and
// nothing in the capture distinguishes them. What the two share is that
// neither proves the collection is complete — so anything that reasons
// from the ABSENCE of a resource has to stand down.
//
// Ignoring this cost 97 dangling-reference findings on one real 44-node
// capture: the registry served exactly 100 senders and 100 flows, and
// every sender past the boundary appeared to point at a flow that did
// not exist.
func truncatedKinds(report []string) map[string]bool {
	type walk struct {
		increments []int
	}
	walks := map[string]*walk{}
	for _, line := range report {
		m := pageLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		path, inc := m[1], m[3]
		n, err := strconv.Atoi(inc)
		if err != nil {
			continue
		}
		w, ok := walks[path]
		if !ok {
			w = &walk{}
			walks[path] = w
		}
		w.increments = append(w.increments, n)
	}

	out := map[string]bool{}
	for path, w := range walks {
		if len(w.increments) < 2 {
			continue
		}
		last := w.increments[len(w.increments)-1]
		prev := w.increments[len(w.increments)-2]
		if last == 0 && prev > 0 {
			out[kindOfPath(path)] = true
		}
	}
	return out
}

// kindOfPath takes the resource kind off the end of a query path.
func kindOfPath(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func keysOf2(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
