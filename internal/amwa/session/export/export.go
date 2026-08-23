// Package export captures a live NMOS plant to disk.
//
// The output is the input to the offline audit: one folder per device,
// holding the resources it published, the SDP it serves, and a line per
// request recording what answered. A registry is captured together with
// every node it lists, nested, so one export is one plant.
//
// The capture is deliberately dumb about meaning. It follows what the
// device says — `manifest_href` verbatim, the version list verbatim,
// paging cursors verbatim — and records failures rather than working
// around them. Interpretation is the audit's job, and it can only do
// that job if the capture did not quietly repair anything first.
package export

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Version identifies the exporter in device.json, so a capture can be
// tied to the code that produced it.
const Version = "dhs-export/1"

// Options configures one capture.
type Options struct {
	// Target is the `host:port` to capture.
	Target string
	// Out is the directory harvests are written under. Each device gets
	// its own folder inside it; nothing is ever written to Out itself.
	Out string
	// HTTPS selects the scheme.
	HTTPS bool
	// Deep also fetches staged, constraints and transporttype for every
	// IS-05 endpoint. Off by default: on a 176-sender device it is four
	// requests per endpoint instead of one.
	Deep bool
	// AllVersions walks every minor of every API. Off by default,
	// because a node repeats identical resources at each minor — but a
	// registry's query API is walked at every minor regardless, since
	// version isolation means the minors genuinely differ there.
	AllVersions bool
	// NoSDP skips SDP retrieval.
	NoSDP bool
	// MaxNodes caps how many registered nodes a registry capture
	// follows. Zero means no cap.
	MaxNodes int
	// Timeout is the per-request deadline.
	Timeout time.Duration
	// NoStamp names folders without a timestamp, so a capture can be
	// re-run into a stable path.
	NoStamp bool
	// Now supplies the folder timestamp. Tests set it to keep output
	// paths deterministic.
	Now func() time.Time
	// Log receives progress lines. Nil discards them.
	Log func(string)
	// Client overrides the HTTP client.
	Client *http.Client
}

func (o *Options) defaults() {
	if o.Out == "" {
		o.Out = "nmos-export"
	}
	if o.Timeout <= 0 {
		o.Timeout = 10 * time.Second
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = func(string) {}
	}
	if o.Client == nil {
		o.Client = &http.Client{Timeout: o.Timeout}
	}
}

// Result reports where a capture landed and what it found.
type Result struct {
	Dir       string   `json:"dir"`
	Target    string   `json:"target"`
	Role      string   `json:"role"`
	Label     string   `json:"label,omitempty"`
	ID        string   `json:"id,omitempty"`
	Requests  int      `json:"requests"`
	Failures  int      `json:"failures"`
	SDPFiles  int      `json:"sdp_files"`
	NodesSeen int      `json:"nodes_seen,omitempty"`
	Followed  []Result `json:"followed,omitempty"`
}

// Run captures Target and, when it is a registry, every node it lists.
func Run(ctx context.Context, opts Options) (*Result, error) {
	opts.defaults()
	if opts.Target == "" {
		return nil, fmt.Errorf("export: --target is required")
	}
	if err := os.MkdirAll(opts.Out, 0o755); err != nil {
		return nil, fmt.Errorf("export: %w", err)
	}
	return capture(ctx, opts, opts.Out, opts.Target, map[string]bool{})
}

// resource sets, in the order the specs define them.
var (
	nodeResources  = []string{"self", "devices", "sources", "flows", "senders", "receivers"}
	queryResources = []string{"nodes", "devices", "sources", "flows", "senders", "receivers", "subscriptions"}
	// standardAPIs is the fallback probe list for a device that serves
	// no /x-nmos root. Some do not; the APIs are still there.
	standardAPIs = []string{"node", "connection", "channelmapping", "system", "query", "registration", "events"}
)

// capture harvests one device into its own folder under root.
func capture(ctx context.Context, opts Options, root, target string, visited map[string]bool) (*Result, error) {
	if visited[target] {
		return nil, nil
	}
	visited[target] = true

	h := &harvester{opts: opts, target: target, scheme: "http"}
	if opts.HTTPS {
		h.scheme = "https"
	}

	dir, err := h.makeDir(root)
	if err != nil {
		return nil, err
	}
	h.dir = dir
	opts.Log(fmt.Sprintf("capturing %s -> %s", target, dir))

	// Identity first, so an interrupted capture still leaves an
	// attributable folder rather than an anonymous pile of JSON.
	if err := h.writeDevice("in-progress"); err != nil {
		return nil, err
	}

	h.walk(ctx)

	if err := h.writeTree(); err != nil {
		return nil, err
	}
	if err := h.writeDevice(h.role); err != nil {
		return nil, err
	}
	if err := h.writeReport(); err != nil {
		return nil, err
	}
	if err := h.pruneEmptyDirs(); err != nil {
		return nil, err
	}

	res := &Result{
		Dir: h.dir, Target: target, Role: h.role, Label: h.label, ID: h.id,
		Requests: h.requests, Failures: h.failures, SDPFiles: h.sdpFiles,
		NodesSeen: len(h.nodes),
	}

	// A registry capture is only a plant capture if the nodes come with
	// it. Following them is what makes the cross-device checks possible.
	if len(h.nodes) > 0 {
		kids := filepath.Join(h.dir, "nodes")
		if err := os.MkdirAll(kids, 0o755); err != nil {
			return nil, err
		}
		followed, capped := 0, 0
		for _, n := range h.nodes {
			if opts.MaxNodes > 0 && followed >= opts.MaxNodes {
				capped++
				continue
			}
			addr := hostPortOf(n.Href)
			if addr == "" {
				h.note(fmt.Sprintf("SKIP  node %s '%s' unreachable at: (no usable href)", n.ID, n.Label))
				continue
			}
			sub, err := capture(ctx, opts, kids, addr, visited)
			if err != nil {
				return nil, err
			}
			if sub == nil {
				continue
			}
			// A node that answered nothing at all is not a device with
			// an empty tree — it is a registration pointing at
			// something that is gone. Leaving the folder behind would
			// audit as a fourth device with no APIs; recording the skip
			// on the PARENT is what lets the audit see a registry
			// advertising a node nobody can reach.
			if sub.Requests == 0 {
				if err := os.RemoveAll(sub.Dir); err != nil {
					return nil, err
				}
				h.note(fmt.Sprintf("SKIP  node %s '%s' unreachable at: %s", n.ID, n.Label, addr))
				continue
			}
			followed++
			res.Followed = append(res.Followed, *sub)
		}
		if capped > 0 {
			h.note(fmt.Sprintf("CAPPED %d node(s) not followed (--max-nodes %d)", capped, opts.MaxNodes))
		}
		h.note(fmt.Sprintf("SUMMARY %d listed / %d followed / %d capped", len(h.nodes), followed, capped))
		// The report gains lines while nodes are followed, so it is
		// rewritten once they are done.
		if err := h.writeReport(); err != nil {
			return nil, err
		}
	}
	return res, nil
}

// nodeRef is the little of a registry's node entry the exporter needs
// in order to go and capture it.
type nodeRef struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Href  string `json:"href"`
}

// harvester holds the state of one device's capture.
type harvester struct {
	opts   Options
	target string
	scheme string
	dir    string

	role  string
	label string
	id    string

	apis  map[string]*apiCapture
	nodes []nodeRef

	report   []string
	requests int
	failures int
	sdpFiles int
	sdpSeen  map[string]bool
}

type apiCapture struct {
	Versions []string                              `json:"versions"`
	Data     map[string]map[string]json.RawMessage `json:"data"`
}

func (h *harvester) note(s string) { h.report = append(h.report, s) }

// makeDir builds the per-device folder. It refuses to write into the
// output root: a harvest folder that IS the root leaves raw/ and sdp/
// loose at the top with nothing identifying whose they are.
func (h *harvester) makeDir(root string) (string, error) {
	safe := sanitize(h.target)
	if safe == "" {
		return "", fmt.Errorf("export: target %q has no usable name", h.target)
	}
	name := safe
	if !h.opts.NoStamp {
		name = safe + "_" + h.opts.Now().Format("20060102-150405")
	}
	dir := filepath.Join(root, name)
	rootAbs, _ := filepath.Abs(root)
	dirAbs, _ := filepath.Abs(dir)
	if rootAbs == dirAbs {
		return "", fmt.Errorf("export: refusing to capture into the output root (%s)", root)
	}
	// raw/ and sdp/ are created lazily, when there is something to put
	// in them, and pruned at the end if there is not.
	return dir, os.MkdirAll(dir, 0o755)
}

// walk performs the capture proper.
func (h *harvester) walk(ctx context.Context) {
	h.apis = map[string]*apiCapture{}
	h.sdpSeen = map[string]bool{}
	h.role = "unknown"

	names, _ := h.getStrings(ctx, "/x-nmos")
	if len(names) == 0 {
		h.note("WARN  no /x-nmos root - probing the standard API names")
		names = standardAPIs
	}
	sort.Strings(names)

	for _, api := range names {
		api = strings.TrimSuffix(api, "/")
		if api == "" {
			continue
		}
		vers, _ := h.getStrings(ctx, "/x-nmos/"+api+"/")
		if len(vers) == 0 {
			continue
		}
		for i := range vers {
			vers[i] = strings.TrimSuffix(vers[i], "/")
		}
		cap := &apiCapture{Versions: vers, Data: map[string]map[string]json.RawMessage{}}
		h.apis[api] = cap

		// The query API is walked at EVERY minor. IS-04 isolates
		// versions: a resource registered at v1.1 is not visible on a
		// v1.3 query unless downgrade is requested. Collapsing to the
		// highest minor loses whatever registered lower. Every other
		// API repeats identical resources per minor, so there the
		// highest is enough.
		walk := vers
		if !h.opts.AllVersions && len(vers) > 1 && api != "query" {
			walk = []string{highest(vers)}
			h.note(fmt.Sprintf("NOTE  %s : versions %s present, walking %s only (--all-versions for all)",
				api, strings.Join(vers, ","), walk[0]))
		}

		for _, v := range walk {
			cap.Data[v] = h.walkAPI(ctx, api, v)
		}
	}

	if h.role == "unknown" {
		switch {
		case h.apis["query"] != nil, h.apis["registration"] != nil:
			h.role = "registry"
		case h.apis["node"] != nil:
			h.role = "node"
		}
	}
}

// walkAPI captures one API at one minor.
func (h *harvester) walkAPI(ctx context.Context, api, v string) map[string]json.RawMessage {
	base := "/x-nmos/" + api + "/" + v
	bucket := map[string]json.RawMessage{}

	switch api {
	case "node":
		for _, res := range nodeResources {
			bucket[res] = h.get(ctx, base+"/"+res)
		}
		if self := bucket["self"]; len(self) > 0 {
			var s nodeRef
			if json.Unmarshal(self, &s) == nil && s.ID != "" {
				h.role, h.label, h.id = "node", s.Label, s.ID
			}
		}
		if !h.opts.NoSDP {
			h.fetchSenderManifests(ctx, bucket["senders"])
		}

	case "query":
		h.role = "registry"
		for _, res := range queryResources {
			all := h.getPaged(ctx, base+"/"+res)
			bucket[res] = all
			if res == "nodes" {
				h.collectNodes(all, v)
			}
		}

	case "connection":
		h.walkConnection(ctx, base, v, bucket)

	case "channelmapping":
		bucket["io"] = h.get(ctx, base+"/io")
		bucket["map_active"] = h.get(ctx, base+"/map/active")
		bucket["map_staged"] = h.get(ctx, base+"/map/staged")

	case "system":
		bucket["global"] = h.get(ctx, base+"/global")

	default:
		bucket["root"] = h.get(ctx, base+"/")
	}
	return bucket
}

// walkConnection captures the IS-05 surface for both sides.
func (h *harvester) walkConnection(ctx context.Context, base, v string, bucket map[string]json.RawMessage) {
	for _, side := range []string{"senders", "receivers"} {
		ids, raw := h.getStrings(ctx, base+"/single/"+side)
		bucket["single_"+side] = raw

		subs := []string{"active"}
		if h.opts.Deep {
			subs = []string{"staged", "active", "constraints", "transporttype"}
			// transporttype arrived in IS-05 v1.1. Asking a v1.0
			// Connection API for it is a guaranteed 404 per endpoint —
			// one live run produced 5,951 of them.
			if v == "v1.0" {
				subs = subs[:3]
			}
		}

		for _, id := range ids {
			id = strings.TrimSuffix(id, "/")
			if id == "" {
				continue
			}
			for _, sub := range subs {
				blob := h.get(ctx, base+"/single/"+side+"/"+id+"/"+sub)
				bucket[side+"/"+id+"/"+sub] = blob
				// A receiver's SDP is pushed into it by a controller and
				// lives in transport_file.data. Empty means nothing is
				// connected, which is not an error.
				if data := embeddedSDP(blob); data != "" && !h.opts.NoSDP {
					h.writeSDP(fmt.Sprintf("%s_%s_%s.sdp", side, id, sub), []byte(data))
					h.note(fmt.Sprintf("SDP   %s %s %s (embedded)", side, id, sub))
				}
			}
			if side == "senders" && !h.opts.NoSDP {
				h.fetchSDP(ctx, h.scheme+"://"+h.target+base+"/single/senders/"+id+"/transportfile",
					"is05_sender_"+id+".sdp")
			}
		}
	}
	bucket["bulk"] = h.get(ctx, base+"/bulk")
}

// fetchSenderManifests follows each sender's manifest_href.
//
// IS-04 defines the SDP location ON THE SENDER RESOURCE. There is no
// `/senders/{id}/transportfile` in IS-04 — that is IS-05 — and devices
// place the manifest where they like. Following the field verbatim is
// the only correct behaviour; constructing the URL instead produced
// 1088 errors against a real device that serves its SDP at
// `/x-nmos/node/v1.3/sdp/{id}`.
func (h *harvester) fetchSenderManifests(ctx context.Context, blob json.RawMessage) {
	var senders []struct {
		ID           string  `json:"id"`
		ManifestHref *string `json:"manifest_href"`
	}
	if json.Unmarshal(blob, &senders) != nil {
		return
	}
	for _, s := range senders {
		if s.ID == "" {
			continue
		}
		if s.ManifestHref == nil || *s.ManifestHref == "" {
			h.note(fmt.Sprintf("NOSDP sender %s has no manifest_href (legal for an inactive sender)", s.ID))
			continue
		}
		h.fetchSDP(ctx, *s.ManifestHref, "sender_"+s.ID+".sdp")
	}
}

// collectNodes remembers every node a registry lists, deduplicated
// across minors — the union across versions is the plant.
func (h *harvester) collectNodes(blob json.RawMessage, v string) {
	var list []nodeRef
	if json.Unmarshal(blob, &list) != nil {
		return
	}
	seen := map[string]bool{}
	for _, n := range h.nodes {
		seen[n.ID] = true
	}
	added := 0
	for _, n := range list {
		if n.ID == "" || seen[n.ID] {
			continue
		}
		seen[n.ID] = true
		h.nodes = append(h.nodes, n)
		added++
	}
	h.note(fmt.Sprintf("VERSION %s nodes: %d listed, %d new (union so far %d)", v, len(list), added, len(h.nodes)))
}

// linkNext matches the `rel="next"` member of an RFC 5988 Link header.
var linkNext = regexp.MustCompile(`<([^>]+)>\s*;\s*rel\s*=\s*"?next"?`)

// getPaged walks a Query API collection to the end.
//
// Without this a registry serving 68 nodes in pages of 10 captures the
// first 10 and reports a 10-node plant. The IS-04 paging contract is a
// `Link: rel="next"` header; the cursor is opaque and must be followed
// rather than reconstructed.
func (h *harvester) getPaged(ctx context.Context, path string) json.RawMessage {
	var all []json.RawMessage
	next := h.scheme + "://" + h.target + path
	seenCursor := map[string]bool{}

	for page := 1; page <= 500 && next != ""; page++ {
		body, hdr, code, err := h.do(ctx, next)
		if err != nil || code != http.StatusOK {
			h.note(fmt.Sprintf("%-5s %s", statusToken(code), path))
			h.failures++
			break
		}
		h.requests++
		var items []json.RawMessage
		if json.Unmarshal(body, &items) != nil {
			// Not a collection — record it whole and stop paging.
			h.writeRaw(path, body)
			return body
		}
		all = append(all, items...)
		h.note(fmt.Sprintf("%-5d %s  (page %d, +%d, total %d)", code, path, page, len(items), len(all)))

		// An empty page is the end of the collection, and it is the
		// normal termination condition. Registries following the AMWA
		// reference implementation emit prev/next/first/last on EVERY
		// response as navigational affordances, including the last one
		// — so the presence of a `next` link says nothing about whether
		// more resources exist. Only the page contents do.
		if len(items) == 0 {
			break
		}

		m := linkNext.FindStringSubmatch(hdr.Get("Link"))
		if m == nil {
			break
		}
		cand := absolutize(next, m[1])
		// A cursor that repeats WHILE resources are still arriving is a
		// registry defect: the walk cannot reach the end. Recording it
		// and stopping is the honest behaviour — looping would hang the
		// capture, and stopping quietly would report a smaller plant
		// than exists.
		if seenCursor[cand] {
			h.note(fmt.Sprintf("WARN  paging cursor did not advance for %s - stopping (registry paging defect)", path))
			break
		}
		seenCursor[cand] = true
		next = cand
	}

	// The same resource can legitimately appear on two pages when the
	// catalogue changes mid-walk. Deduplicating by id is right; the
	// per-page counts stay in report.txt so the duplication is still
	// auditable.
	uniq, dupes := dedupeByID(all)
	if dupes > 0 {
		h.note(fmt.Sprintf("NOTE  %s returned %d rows across pages, %d unique", path, len(all), len(uniq)))
	}
	out, err := json.Marshal(uniq)
	if err != nil {
		return nil
	}
	h.writeRaw(path, out)
	return out
}

// get fetches one JSON resource and records it.
func (h *harvester) get(ctx context.Context, path string) json.RawMessage {
	body, _, code, err := h.do(ctx, h.scheme+"://"+h.target+path)
	if err != nil || code != http.StatusOK {
		h.note(fmt.Sprintf("%-5s %s", statusToken(code), path))
		h.failures++
		return nil
	}
	h.requests++
	h.note(fmt.Sprintf("%-5d %s", code, path))
	h.writeRaw(path, body)
	if !json.Valid(body) {
		return nil
	}
	return json.RawMessage(body)
}

// getStrings fetches a resource expected to be an array of strings,
// returning both the parsed list and the raw bytes.
func (h *harvester) getStrings(ctx context.Context, path string) ([]string, json.RawMessage) {
	blob := h.get(ctx, path)
	if len(blob) == 0 {
		return nil, blob
	}
	var out []string
	if json.Unmarshal(blob, &out) != nil {
		return nil, blob
	}
	return out, blob
}

// fetchSDP retrieves one SDP and writes it, skipping URLs already seen
// — a sender's manifest and its IS-05 transport file are frequently the
// same document.
func (h *harvester) fetchSDP(ctx context.Context, rawURL, name string) {
	if rawURL == "" {
		return
	}
	// IS-04 types manifest_href as a URI, not an absolute URL, and a
	// device is entitled to publish `/x-nmos/node/v1.3/sdp/{id}`.
	// Resolving against the device's own base is still following the
	// field verbatim — it is what a relative URI means.
	rawURL = absolutize(h.scheme+"://"+h.target+"/", rawURL)
	if h.sdpSeen[rawURL] {
		return
	}
	h.sdpSeen[rawURL] = true

	body, _, code, err := h.do(ctx, rawURL)
	if err != nil || code != http.StatusOK {
		h.note(fmt.Sprintf("%-5s %s", statusToken(code), rawURL))
		h.failures++
		return
	}
	h.requests++
	h.note(fmt.Sprintf("%-5d %s", code, rawURL))
	h.writeSDP(name, body)
}

// do performs one request.
func (h *harvester) do(ctx context.Context, rawURL string) ([]byte, http.Header, int, error) {
	rctx, cancel := context.WithTimeout(ctx, h.opts.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(rctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, 0, err
	}
	resp, err := h.opts.Client.Do(req)
	if err != nil {
		return nil, nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.Header, resp.StatusCode, err
	}
	return body, resp.Header, resp.StatusCode, nil
}

// --- writers ---

func (h *harvester) writeRaw(path string, body []byte) {
	name := sanitize(strings.Trim(path, "/"))
	if name == "" {
		return
	}
	h.writeFile(filepath.Join(h.dir, "raw", name+".json"), body)
}

func (h *harvester) writeSDP(name string, body []byte) {
	h.writeFile(filepath.Join(h.dir, "sdp", name), body)
	h.sdpFiles++
}

func (h *harvester) writeFile(path string, body []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, body, 0o644)
}

func (h *harvester) writeDevice(role string) error {
	d := map[string]any{
		"target":     h.target,
		"role":       role,
		"started_at": h.opts.Now().Format(time.RFC3339Nano),
		"harvester":  Version,
		"label":      h.label,
		"id":         h.id,
	}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(h.dir, "device.json"), b, 0o644)
}

func (h *harvester) writeTree() error {
	tree := map[string]any{
		"harvested_at": h.opts.Now().Format(time.RFC3339Nano),
		"target":       h.target,
		"harvester":    Version,
		"apis":         h.apis,
	}
	b, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(h.dir, "tree.json"), b, 0o644)
}

func (h *harvester) writeReport() error {
	body := strings.Join(h.report, "\n")
	if body != "" {
		body += "\n"
	}
	return os.WriteFile(filepath.Join(h.dir, "report.txt"), []byte(body), 0o644)
}

// pruneEmptyDirs removes raw/ and sdp/ when nothing landed in them, so
// a capture folder never carries empty scaffolding.
func (h *harvester) pruneEmptyDirs() error {
	for _, sub := range []string{"raw", "sdp"} {
		p := filepath.Join(h.dir, sub)
		entries, err := os.ReadDir(p)
		if err != nil || len(entries) > 0 {
			continue
		}
		if err := os.Remove(p); err != nil {
			return err
		}
	}
	return nil
}

// --- helpers ---

var unsafeChars = regexp.MustCompile(`[^0-9A-Za-z]+`)

// sanitize renders a host:port or URL path as a filesystem-safe name.
func sanitize(s string) string {
	return strings.Trim(unsafeChars.ReplaceAllString(s, "_"), "_")
}

// statusToken renders a status for report.txt, using ERR when the
// request never completed at all.
func statusToken(code int) string {
	if code == 0 {
		return "ERR"
	}
	return strconv.Itoa(code)
}

// highest picks the greatest `vMAJOR.MINOR`, ordering v1.10 after v1.9
// rather than before it as a string sort would.
func highest(vs []string) string {
	// The initial rank must sit BELOW the unparseable rank, or a device
	// advertising a single version we cannot parse yields "" and its
	// API is never walked at all.
	best, rank := "", -2
	for _, v := range vs {
		if r := rankOf(v); r > rank {
			best, rank = v, r
		}
	}
	return best
}

func rankOf(v string) int {
	maj, min, ok := strings.Cut(strings.TrimPrefix(v, "v"), ".")
	if !ok {
		return -1
	}
	m, err1 := strconv.Atoi(maj)
	n, err2 := strconv.Atoi(min)
	if err1 != nil || err2 != nil {
		return -1
	}
	return m*1000 + n
}

// absolutize resolves a Link header target against the URL it came
// from, since a registry may return either an absolute or a relative
// cursor.
func absolutize(base, ref string) string {
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

// hostPortOf extracts `host:port` from a node's href, defaulting the
// port from the scheme when the href omits it.
func hostPortOf(href string) string {
	u, err := url.Parse(href)
	if err != nil || u.Host == "" {
		return ""
	}
	if u.Port() != "" {
		return u.Host
	}
	if u.Scheme == "https" {
		return u.Host + ":443"
	}
	return u.Host + ":80"
}

// embeddedSDP pulls transport_file.data out of an IS-05 receiver
// payload. A short or absent value means nothing is connected.
func embeddedSDP(blob json.RawMessage) string {
	if len(blob) == 0 {
		return ""
	}
	var probe struct {
		TransportFile struct {
			Data *string `json:"data"`
		} `json:"transport_file"`
	}
	if json.Unmarshal(blob, &probe) != nil || probe.TransportFile.Data == nil {
		return ""
	}
	if len(strings.TrimSpace(*probe.TransportFile.Data)) <= 10 {
		return ""
	}
	return *probe.TransportFile.Data
}

// dedupeByID keeps the first copy of each resource id, preserving
// order, and reports how many duplicates it dropped.
func dedupeByID(items []json.RawMessage) ([]json.RawMessage, int) {
	out := make([]json.RawMessage, 0, len(items))
	seen := map[string]bool{}
	dupes := 0
	for _, it := range items {
		var probe struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(it, &probe) != nil || probe.ID == "" {
			out = append(out, it)
			continue
		}
		if seen[probe.ID] {
			dupes++
			continue
		}
		seen[probe.ID] = true
		out = append(out, it)
	}
	return out, dupes
}
