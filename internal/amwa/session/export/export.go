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
	"bytes"
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
	"sync"
	"time"
)

// Version identifies the exporter in device.json, so a capture can be
// tied to the code that produced it.
const Version = "dhs-export/1"

// defaultPageLimit is what we ask a Query API for per page. Registries
// left to their own default have been seen serving ten.
const defaultPageLimit = 100

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
	// PageLimit is the `paging.limit` asked for on every Query API
	// collection. Zero uses defaultPageLimit. Registries may clamp it
	// and report the applied value in X-Paging-Limit.
	PageLimit int
	// Raw keeps the verbatim response body of every request under
	// `raw/`. Off by default: tree.json already stores each body as
	// json.RawMessage, so raw/ duplicates it — and on a real 44-node
	// plant it was 14,719 of the 27,299 files and held every single
	// path over the Windows 260-character limit. Turn it on when the
	// exact bytes matter, e.g. a non-JSON response or a byte-level
	// diff.
	Raw bool
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
	// Workers is how many nodes a registry capture follows at once.
	// Zero uses defaultWorkers. Each node is ~600 requests against a
	// different device, so the walk is latency-bound and parallelises
	// almost perfectly; 44 nodes took four minutes in series.
	Workers int
}

// defaultWorkers is deliberately modest. The limit is not our CPU, it
// is how many simultaneous HTTP sessions a plant's devices and switches
// tolerate from one host — and an export must never be the thing that
// disturbs a live plant.
const defaultWorkers = 6

func (o Options) workers() int {
	if o.Workers > 0 {
		return o.Workers
	}
	return defaultWorkers
}

// visitTracker claims each target exactly once across the worker pool,
// so a registry that lists the same device twice — or a node that lists
// itself — is captured once.
type visitTracker struct {
	mu   sync.Mutex
	seen map[string]bool
}

func newVisitTracker() *visitTracker { return &visitTracker{seen: map[string]bool{}} }

// claim returns true when the caller is the first to take this target.
func (v *visitTracker) claim(target string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.seen[target] {
		return false
	}
	v.seen[target] = true
	return true
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
	Dir       string              `json:"dir"`
	Target    string              `json:"target"`
	Role      string              `json:"role"`
	Label     string              `json:"label,omitempty"`
	ID        string              `json:"id,omitempty"`
	Hostname  string              `json:"hostname,omitempty"`
	APIs      map[string][]string `json:"apis,omitempty"`
	Requests  int                 `json:"requests"`
	Failures  int                 `json:"failures"`
	SDPFiles  int                 `json:"sdp_files"`
	NodesSeen int                 `json:"nodes_seen,omitempty"`
	Followed  []Result            `json:"followed,omitempty"`
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
	res, err := capture(ctx, opts, opts.Out, opts.Target, newVisitTracker())
	if err != nil || res == nil {
		return res, err
	}
	// The manifest is the index someone opens first: which device is in
	// which folder, at which address, under which name. It is written
	// last so it carries the final folder names, and it is what lets
	// those names stay short.
	if err := writeManifest(opts.Out, res, opts); err != nil {
		return nil, fmt.Errorf("export: writing manifest: %w", err)
	}
	return res, nil
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
func capture(ctx context.Context, opts Options, root, target string, visited *visitTracker) (*Result, error) {
	return captureAt(ctx, opts, root, target, visited, false)
}

func captureAt(ctx context.Context, opts Options, root, target string, visited *visitTracker, nested bool) (*Result, error) {
	if !visited.claim(target) {
		return nil, nil
	}

	h := &harvester{opts: opts, target: target, scheme: "http", nested: nested}
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

	// Now that the device has named itself, put that name in the folder.
	// `10_41_40_80_3000` tells an operator nothing six months later;
	// `10_41_40_80_3000__bm-n-nnbrg-t01` is the thing they search for.
	// Renaming here, before any node is followed, keeps children nested
	// under the final path.
	if err := h.renameWithIdentity(); err != nil {
		return nil, err
	}

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

	apis := map[string][]string{}
	for name, a := range h.apis {
		apis[name] = a.Versions
	}
	res := &Result{
		Dir: h.dir, Target: target, Role: h.role, Label: h.label, ID: h.id,
		Hostname: h.hostname, APIs: apis,
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
		// Nodes are captured concurrently. Each one is ~600 requests
		// against a different device, so the walk is latency-bound and
		// entirely parallel — 44 nodes took four minutes in series.
		// The cap is per plant, not per device, so a slow node delays
		// only itself.
		todo := h.nodes
		capped := 0
		if opts.MaxNodes > 0 && len(todo) > opts.MaxNodes {
			capped = len(todo) - opts.MaxNodes
			todo = todo[:opts.MaxNodes]
		}

		subs := make([]*Result, len(todo))
		errs := make([]error, len(todo))
		sem := make(chan struct{}, opts.workers())
		var wg sync.WaitGroup

		for i, n := range todo {
			addr := hostPortOf(n.Href)
			if addr == "" {
				h.note(fmt.Sprintf("SKIP  node %s '%s' unreachable at: (no usable href)", n.ID, n.Label))
				continue
			}
			wg.Add(1)
			go func(i int, n nodeRef, addr string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				opts.Log(fmt.Sprintf("[%d/%d] %s '%s'", i+1, len(todo), addr, n.Label))
				subs[i], errs[i] = captureAt(ctx, opts, kids, addr, visited, true)
			}(i, n, addr)
		}
		wg.Wait()

		followed := 0
		for i, sub := range subs {
			if errs[i] != nil {
				return nil, errs[i]
			}
			if sub == nil {
				continue
			}
			n := todo[i]
			addr := hostPortOf(n.Href)
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
	ID       string `json:"id"`
	Label    string `json:"label"`
	Href     string `json:"href"`
	Hostname string `json:"hostname"`
}

// harvester holds the state of one device's capture.
type harvester struct {
	opts   Options
	target string
	scheme string
	dir    string

	role     string
	label    string
	hostname string
	// nested marks a followed node: its folder sits inside a stamped
	// registry folder, so it is not stamped again.
	nested bool
	id     string

	apis  map[string]*apiCapture
	nodes []nodeRef

	report   []string
	requests int
	failures int
	sdpFiles int
	sdpSeen  map[string]bool
	// sdpBySender remembers the first SDP seen for a sender so the
	// second source can be compared against it rather than written
	// beside it.
	sdpBySender map[string]sdpRecord
}

// sdpRecord is one sender's SDP as fetched from one of its two
// publication points.
type sdpRecord struct {
	source string // "is04" (manifest_href) or "is05" (transportfile)
	body   []byte
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
	// Only the top-level capture is stamped. A followed node already
	// sits inside a stamped registry folder, so repeating it there adds
	// 16 characters to every path below it and says nothing new.
	name := safe
	if !h.opts.NoStamp && !h.nested {
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

// renameWithIdentity appends the device's own hostname and label to its
// folder name.
//
// Both are appended because they are frequently different and both are
// how someone searches: the hostname is what DNS and the switch know,
// the label is what the operator typed. Identical values collapse to
// one rather than being repeated.
//
// A rename that fails is not fatal — the capture is complete and
// correct under the address-only name, and losing it over a locked
// directory would be absurd.
func (h *harvester) renameWithIdentity() error {
	suffix := identitySuffix(h.hostname, h.label)
	if suffix == "" {
		return nil
	}
	parent, base := filepath.Split(strings.TrimRight(h.dir, string(filepath.Separator)))
	target := filepath.Join(parent, base+"__"+suffix)
	if target == h.dir {
		return nil
	}
	if _, err := os.Stat(target); err == nil {
		// Something is already there — two devices resolving to the same
		// name. Keep the unambiguous address-only folder.
		h.note(fmt.Sprintf("NOTE  folder not renamed to %q: a folder of that name already exists", base+"__"+suffix))
		return nil
	}
	if err := os.Rename(h.dir, target); err != nil {
		h.note(fmt.Sprintf("NOTE  folder not renamed: %v", err))
		return nil
	}
	h.dir = target
	return nil
}

// identitySuffix builds the filesystem-safe tail appended to a capture
// folder.
//
// The folder carries just enough to recognise a device at a glance; the
// FULL identity — hostname, label, id, address, APIs — lives in
// manifest.json at the capture root. That split is deliberate: encoding
// everything in the name produced 76-character folders, and with a
// nested registry capture and a `raw/` filename on the end, 329 paths
// on one real plant went past the Windows 260-character limit and the
// capture could not be copied.
//
// The label is preferred because it is what an operator typed and what
// they will search for; the hostname is the fallback and is in the
// manifest either way.
func identitySuffix(hostname, label string) string {
	const maxPart = 24
	for _, v := range []string{label, hostname} {
		s := sanitize(v)
		if s == "" {
			continue
		}
		if len(s) > maxPart {
			s = s[:maxPart]
		}
		return s
	}
	return ""
}

// walk performs the capture proper.
func (h *harvester) walk(ctx context.Context) {
	h.apis = map[string]*apiCapture{}
	h.sdpSeen = map[string]bool{}
	h.sdpBySender = map[string]sdpRecord{}
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
			h.opts.Log(fmt.Sprintf("  %s /x-nmos/%s/%s", h.target, api, v))
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
				h.hostname = s.Hostname
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

		// The IS-05 pass is the expensive half of a node: one request per
		// endpoint per sub-resource, and a Neuron publishes 176 of each.
		if len(ids) >= 25 {
			h.opts.Log(fmt.Sprintf("    %s: IS-05 %s, %d endpoints x %d", h.target, side, len(ids), len(subs)))
		}
		for n, id := range ids {
			id = strings.TrimSuffix(id, "/")
			if id == "" {
				continue
			}
			if len(ids) >= 100 && n > 0 && n%50 == 0 {
				h.opts.Log(fmt.Sprintf("      %s: %s %d/%d", h.target, side, n, len(ids)))
			}
			for _, sub := range subs {
				blob := h.get(ctx, base+"/single/"+side+"/"+id+"/"+sub)
				bucket[side+"/"+id+"/"+sub] = blob
				// A receiver's SDP is pushed into it by a controller and
				// lives in transport_file.data. Empty means nothing is
				// connected, which is not an error.
				if data := embeddedSDP(blob); data != "" && !h.opts.NoSDP {
					// A receiver's SDP was pushed INTO it by a controller,
					// so it is neither the IS-04 nor the IS-05 sender
					// publication — its own folder, keyed by receiver id.
					h.writeSDP(filepath.Join("receivers", id+"_"+sub+".sdp"), []byte(data))
					h.note(fmt.Sprintf("SDP   %s %s %s (embedded)", side, id, sub))
				}
			}
			if side == "senders" && !h.opts.NoSDP {
				h.fetchSDP(ctx, h.scheme+"://"+h.target+base+"/single/senders/"+id+"/transportfile",
					id, "is05")
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
		h.fetchSDP(ctx, *s.ManifestHref, s.ID, "is04")
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

// linkPrev matches the `rel="prev"` member of an RFC 5988 Link header
// — the OLDER direction. linkNext matches `rel="next"`, the NEWER one.
// See pageWalk for why a collection is enumerated with either.
var (
	linkPrev = regexp.MustCompile(`<([^>]+)>\s*;\s*rel\s*=\s*"?prev"?`)
	linkNext = regexp.MustCompile(`<([^>]+)>\s*;\s*rel\s*=\s*"?next"?`)
)

// A pageWalk is one direction through a Query API collection.
//
// IS-04 §7 orders results by `version` and describes the window with
// `paging.since` / `paging.until`, navigated by the Link rels. That
// admits two complete enumerations, and neither is more correct than
// the other:
//
//	ascending   pin the window to the oldest resource with
//	            `paging.since=0:0`, then follow rel="next" forward.
//	descending  take the default window — newest first — and follow
//	            rel="prev" backward.
//
// Both are spec-legal, and a registry can be broken in exactly one of
// them. One real registry answers /flows with an empty `X-Paging-Since`
// and a rel="prev" cursor whose `paging.until` is blank; following it
// re-serves page one forever, so the descending walk stops at 100 of
// 5,168 flows while the ascending walk completes. A different registry
// could break the other way round.
//
// So the exporter tries ascending first and falls back to descending
// when the walk does not reach the end, keeping whichever enumerated
// more. The fallback costs a second pass over one collection on a
// registry that is already misbehaving, and nothing at all on one that
// is not.
type pageWalk struct {
	name string
	// start builds the first URL of the walk.
	start func(base string) string
	// step returns the next URL, or "" when the walk is over.
	step func(h *harvester, cur string, hdr http.Header, items []json.RawMessage, limit int) string
}

var walkAscending = pageWalk{
	name: "ascending",
	// `0:0` is the zero TAI timestamp — older than any resource, so the
	// first window starts at the beginning of the catalogue.
	start: func(base string) string { return withQuery(base, "paging.since", "0:0") },
	step: func(h *harvester, cur string, hdr http.Header, items []json.RawMessage, limit int) string {
		// 1. The spec's cursor. Opaque — followed, never reconstructed.
		if m := linkNext.FindStringSubmatch(hdr.Get("Link")); m != nil {
			return h.pagingURL(cur, m[1])
		}
		// 2. The window we just received ends at X-Paging-Until; the
		// next one forward starts there.
		if until := hdr.Get("X-Paging-Until"); until != "" && until != "0:0" {
			return withQuery(cur, "paging.since", until)
		}
		// 3. No paging headers at all. Derive the cursor from the data:
		// the NEWEST version on the page is where the page after it
		// begins. Probing costs one request when the collection was
		// already exhausted; guessing it was exhausted costs the rest
		// of the catalogue.
		if v := maxVersion(items); v != "" {
			return withQuery(cur, "paging.since", v)
		}
		return ""
	},
}

var walkDescending = pageWalk{
	name:  "descending",
	start: func(base string) string { return base },
	step: func(h *harvester, cur string, hdr http.Header, items []json.RawMessage, limit int) string {
		return h.olderPage(cur, hdr, items, limit)
	},
}

// getPaged walks a Query API collection to the end.
//
// Without this a registry serving 68 nodes in pages of 10 captures the
// first 10 and reports a 10-node plant. The IS-04 paging contract is a
// `Link: rel="next"` header; the cursor is opaque and must be followed
// rather than reconstructed.
func (h *harvester) getPaged(ctx context.Context, path string) json.RawMessage {
	// Ask for a big page explicitly. Left to itself a registry applies
	// its own default — seen as low as 10 in the field — and the walk
	// then costs one round trip per ten resources.
	limit := h.opts.PageLimit
	if limit <= 0 {
		limit = defaultPageLimit
	}
	base := withQuery(h.scheme+"://"+h.target+path, "paging.limit", strconv.Itoa(limit))

	all, complete, raw := h.walkPages(ctx, path, base, limit, walkAscending)
	if raw != nil {
		return raw
	}
	// The same resource can legitimately appear on two pages when the
	// catalogue changes mid-walk. Deduplicating by id is right; the
	// per-page counts stay in report.txt so the duplication is still
	// auditable. It also has to happen BEFORE the two directions are
	// compared — a stuck walk re-serving one page of 100 four times has
	// more rows than a complete walk and fewer resources.
	uniq, dupes := dedupeByID(all)

	// The walk did not reach the end. Try the other direction before
	// accepting a short catalogue — a registry broken in one direction
	// is usually intact in the other.
	if !complete {
		alt, altComplete, altRaw := h.walkPages(ctx, path, base, limit, walkDescending)
		if altRaw == nil {
			altUniq, altDupes := dedupeByID(alt)
			if altComplete || len(altUniq) > len(uniq) {
				h.note(fmt.Sprintf("NOTE  %s ascending walk stopped at %d resource(s), descending returned %d - keeping descending",
					path, len(uniq), len(altUniq)))
				uniq, dupes, all = altUniq, altDupes, alt
			}
		}
	}

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

// walkPages drives one pageWalk to the end of a collection.
//
// It returns the rows gathered, whether the walk actually REACHED the
// end, and — when the endpoint turned out not to be a collection at all
// — the raw body, which short-circuits everything above.
func (h *harvester) walkPages(ctx context.Context, path, base string, limit int, strat pageWalk) ([]json.RawMessage, bool, json.RawMessage) {
	var all []json.RawMessage
	complete := false
	next := strat.start(base)
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
			return nil, true, body
		}
		all = append(all, items...)
		h.note(fmt.Sprintf("%-5d %s  (page %d, +%d, total %d) [%s]", code, path, page, len(items), len(all), strat.name))
		// A registry catalogue is tens of pages per collection. Without
		// a line per page the capture looks hung for minutes before the
		// first node is even reached.
		// Only when the walk is actually long. The empty page that ends
		// every collection would otherwise print a line for each of six
		// collections on a plant with one node.
		if len(items) > 0 && (page > 1 || len(items) >= limit) {
			h.opts.Log(fmt.Sprintf("    %s: page %d, %d so far", shortPath(path), page, len(all)))
		}
		// Record what the registry said about paging, once. Without
		// this a short capture in the field cannot be diagnosed without
		// a debugger — which is exactly what happened.
		if page == 1 {
			h.note(fmt.Sprintf("PAGING %s  walk=%s limit=%s Link=%q X-Paging-Limit=%q Since=%q Until=%q",
				path, strat.name, strconv.Itoa(limit), hdr.Get("Link"),
				hdr.Get("X-Paging-Limit"), hdr.Get("X-Paging-Since"), hdr.Get("X-Paging-Until")))
		}

		// An empty page is the end of the collection, and it is the
		// normal termination condition. Registries following the AMWA
		// reference implementation emit prev/next/first/last on EVERY
		// response as navigational affordances, including the last one
		// — so the presence of a `next` link says nothing about whether
		// more resources exist. Only the page contents do.
		if len(items) == 0 {
			complete = true
			break
		}

		cand := strat.step(h, next, hdr, items, limit)
		if cand == "" {
			// No cursor offered. The collection is exhausted only if the
			// page was demonstrably short of the size the registry said
			// it APPLIED. Without X-Paging-Limit "short" is a guess, and
			// guessing wrong truncates a catalogue silently — so call it
			// incomplete and let the other direction check.
			complete = hdr.Get("X-Paging-Limit") != "" && len(items) < appliedLimit(hdr, limit)
			break
		}
		// A cursor that repeats WHILE resources are still arriving is a
		// registry defect: the walk cannot reach the end. Recording it
		// and stopping is the honest behaviour — looping would hang the
		// capture, and stopping quietly would report a smaller plant
		// than exists.
		if seenCursor[cand] {
			h.note(fmt.Sprintf("WARN  paging cursor did not advance for %s - stopping (registry paging defect, %s walk)", path, strat.name))
			break
		}
		seenCursor[cand] = true
		next = cand
	}
	return all, complete, nil
}

// appliedLimit is the page size the registry actually used, which is
// not necessarily the one we asked for — clamping a requested 100 down
// to 10 is common, and comparing a full page of 10 against 100 reads
// the end of page one as the end of the plant.
func appliedLimit(hdr http.Header, asked int) int {
	if v := hdr.Get("X-Paging-Limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return asked
}

// pagingURL resolves a cursor from a Link header against the URL it
// arrived on, then forces it back onto the host we are actually
// talking to.
//
// A registry behind Docker or NAT advertises its own internal address
// in the Link header — 172.17.0.3, a compose service name — and that
// host does not resolve from the operator's laptop. The cursor's query
// string is what carries the paging state; the authority is ours to
// supply. Taken from the first-generation nmos-consumer, which hit
// this against a containerised registry.
func (h *harvester) pagingURL(cur, ref string) string {
	abs := absolutize(cur, ref)
	u, err := url.Parse(abs)
	if err != nil {
		return abs
	}
	if u.Host != "" && u.Host != h.target {
		h.note(fmt.Sprintf("NOTE  paging cursor pointed at %s, rewritten to %s", u.Host, h.target))
		u.Host = h.target
		u.Scheme = h.scheme
	}
	return u.String()
}

// olderPage decides where the walk goes after this page.
//
// **Direction matters, and getting it wrong is nearly invisible.** The
// IS-04 Query API orders results by `version`, newest first, and pages
// over that ordering. So the first response already holds the NEWEST
// slice, `rel="next"` points at resources newer still — normally none —
// and the rest of the collection lies behind `rel="prev"`.
//
// Following `next` therefore captures one page and stops on an empty
// second page, which looks exactly like a complete walk. On a real
// registry it captured 100 of 5,222 senders and reported success. To
// enumerate a collection you walk toward OLDER.
//
// Three signals, in order of authority.
func (h *harvester) olderPage(cur string, hdr http.Header, items []json.RawMessage, limit int) string {
	// 1. The spec's cursor. Opaque — followed, never reconstructed.
	if m := linkPrev.FindStringSubmatch(hdr.Get("Link")); m != nil {
		return h.pagingURL(cur, m[1])
	}

	// A page shorter than the limit ends the collection — but it has to
	// be the limit the registry APPLIED, not the one we asked for. A
	// registry that clamps 100 down to 10 serves a full page of 10, and
	// comparing that against 100 calls the end of page one the end of
	// the plant. That is precisely the field failure.
	if len(items) < appliedLimit(hdr, limit) {
		// With no Link and no X-Paging-Limit we have no evidence of what
		// the registry applied, so "short" is a guess — and guessing
		// wrong truncates the plant silently. Probe one more page
		// instead: an empty page or a repeated cursor ends the walk
		// safely, and the cost of being wrong is one request.
		if hdr.Get("X-Paging-Limit") != "" {
			return ""
		}
	}

	// 2. The response reports the version window it served. The next
	// window older than this one ends where this one started.
	if since := hdr.Get("X-Paging-Since"); since != "" {
		return withQuery(cur, "paging.until", since)
	}

	// 3. No paging headers at all, but a full page. Derive the cursor
	// from the data: the LOWEST `version` we were handed is where the
	// older page ends. A registry that ignores paging.until entirely
	// will re-serve the same page, and the caller's repeat-cursor guard
	// stops the walk and records the defect.
	if v := minVersion(items); v != "" {
		return withQuery(cur, "paging.until", v)
	}
	return ""
}

// maxVersion returns the largest IS-04 `<sec>:<nsec>` version among a
// page of resources — the newest thing on the page, and therefore where
// the next page forward begins. The ascending mirror of minVersion.
func maxVersion(items []json.RawMessage) string {
	best := ""
	for _, it := range items {
		var probe struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(it, &probe) != nil || probe.Version == "" {
			continue
		}
		if _, _, ok := splitTAI(probe.Version); !ok {
			continue
		}
		if best == "" || taiLess(best, probe.Version) {
			best = probe.Version
		}
	}
	return best
}

// minVersion returns the smallest IS-04 `<sec>:<nsec>` version among a
// page of resources — the oldest thing on the page, and therefore where
// the next page back ends.
func minVersion(items []json.RawMessage) string {
	best := ""
	for _, it := range items {
		var probe struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(it, &probe) != nil || probe.Version == "" {
			continue
		}
		// A version that does not parse is skipped outright rather than
		// ordered. Ordering malformed values low is right for a maximum
		// and exactly wrong for a minimum, where it would win every
		// time and send the cursor somewhere meaningless.
		if _, _, ok := splitTAI(probe.Version); !ok {
			continue
		}
		if best == "" || taiLess(probe.Version, best) {
			best = probe.Version
		}
	}
	return best
}

// taiLess compares two `<sec>:<nsec>` timestamps. A value that does not
// parse sorts low, so a malformed version never wins the maximum and
// sends the walk backwards.
func taiLess(a, b string) bool {
	as, an, aok := splitTAI(a)
	bs, bn, bok := splitTAI(b)
	switch {
	case !aok:
		return bok
	case !bok:
		return false
	case as != bs:
		return as < bs
	default:
		return an < bn
	}
}

func splitTAI(s string) (sec, nsec uint64, ok bool) {
	l, r, found := strings.Cut(s, ":")
	if !found {
		return 0, 0, false
	}
	a, err1 := strconv.ParseUint(l, 10, 64)
	b, err2 := strconv.ParseUint(r, 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return a, b, true
}

// shortPath trims the `/x-nmos/<api>/<ver>/` prefix off a collection
// path, so a progress line reads `senders` rather than the full URL and
// stays readable when several devices interleave.
func shortPath(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// withQuery sets a query parameter on a URL, replacing any existing
// value. Used to drive paging.limit and paging.since without
// string-splicing a URL the registry handed us.
func withQuery(rawURL, key, value string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	q.Set(key, value)
	u.RawQuery = q.Encode()
	return u.String()
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

// writeSenderSDP stores one of a sender's two published SDPs.
//
// A sender publishes its SDP twice: at the IS-04 `manifest_href` and at
// the IS-05 `/transportfile`. They are usually byte-identical — 176 of
// 176 on one real device — and writing both then doubles the SDP folder
// for nothing.
//
// They are fetched from both places anyway, because a node publishing
// two DIFFERENT descriptions of one stream is a genuine fault and only
// comparing them can find it. So: keep one copy when they agree and say
// so, keep both and warn when they do not.
func (h *harvester) writeSenderSDP(senderID, source string, body []byte) {
	// One folder per publication point, and the filename is just the
	// resource id. `diff -r sdp/is04 sdp/is05` is then the disagreement
	// check, which beats any note this code could write.
	h.writeSDP(filepath.Join(source, senderID+".sdp"), body)

	prev, seen := h.sdpBySender[senderID]
	if !seen {
		h.sdpBySender[senderID] = sdpRecord{source: source, body: body}
		return
	}
	if bytes.Equal(prev.body, body) {
		h.note(fmt.Sprintf("SDP   sender %s: %s matches %s", senderID, source, prev.source))
		return
	}
	h.note(fmt.Sprintf("WARN  sender %s publishes DIFFERENT SDP at its IS-04 manifest_href and its IS-05 transportfile", senderID))
}

// fetchSDP retrieves one SDP and writes it, skipping URLs already seen
// — a sender's manifest and its IS-05 transport file are frequently the
// same document.
func (h *harvester) fetchSDP(ctx context.Context, rawURL, senderID, source string) {
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
	h.writeSenderSDP(senderID, source, body)
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
	if !h.opts.Raw {
		return
	}
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
	// Deepest first: sdp/ can only be removed once its own subfolders
	// are gone.
	for _, sub := range []string{
		filepath.Join("sdp", "is04"), filepath.Join("sdp", "is05"), filepath.Join("sdp", "receivers"),
		"raw", "sdp",
	} {
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
