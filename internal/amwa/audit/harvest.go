package audit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Harvest is one captured device — a Node, a Registry, or something
// that answered on /x-nmos but identified as neither.
//
// A Registry harvest carries Children: the exporter follows every node
// the Registry lists and captures it in a nested folder. That nesting
// is what makes a plant-wide audit possible from a single export.
type Harvest struct {
	// Dir is the folder this harvest was read from.
	Dir string `json:"dir"`
	// Target is `host:port` as the exporter reached it.
	Target string `json:"target"`
	// Role is `node`, `registry`, or `unknown` — as the exporter
	// classified it from which APIs answered.
	Role string `json:"role"`
	// Label and ID come from the device's own self resource.
	Label string `json:"label,omitempty"`
	ID    string `json:"id,omitempty"`

	// APIs is the captured `/x-nmos` surface, keyed by API name
	// (`node`, `connection`, `query`, `channelmapping`, `system`, …).
	APIs map[string]API `json:"apis"`
	// Report is report.txt split into lines — the per-request record of
	// what answered and what did not. Status codes live only here, so
	// checks that need them read the report, not the tree.
	Report []string `json:"-"`
	// SDP maps a captured filename to its bytes.
	SDP map[string][]byte `json:"-"`

	// Partial records that the capture folder holds an identity but no
	// tree.json — the exporter started on this device and did not
	// finish. Everything below is then a floor, not a description: the
	// device may serve APIs and publish resources that were never
	// fetched, and no absence in this harvest is evidence of anything.
	Partial bool `json:"partial,omitempty"`

	Children []*Harvest `json:"children,omitempty"`
}

// API is one captured `/x-nmos/<name>` surface: the versions the device
// advertised, and the resources actually fetched, keyed by version then
// by resource name.
type API struct {
	Versions []string                              `json:"versions"`
	Data     map[string]map[string]json.RawMessage `json:"data"`
}

// treeFile mirrors tree.json. `apis` holds a mix of shapes — the
// `_root` key is the raw `/x-nmos` array, every other key is an API
// object — so it is decoded lazily, one entry at a time.
type treeFile struct {
	Target string                     `json:"target"`
	APIs   map[string]json.RawMessage `json:"apis"`
}

// deviceFile mirrors device.json, the exporter's identity stamp.
type deviceFile struct {
	Target string `json:"target"`
	Role   string `json:"role"`
	Label  string `json:"label"`
	ID     string `json:"id"`
}

// ErrNoHarvest reports that a directory holds no capture. It is
// returned rather than silently producing an empty audit, so a
// mistyped path is not mistaken for a clean plant.
var ErrNoHarvest = errors.New("audit: no harvest found (no tree.json under the given directory)")

// Load reads every harvest under dir.
//
// dir may be a single harvest folder or a parent holding many. Nested
// `nodes/` folders become Children of the harvest that followed them,
// so a Registry export loads as one tree rather than as N unrelated
// devices. A folder whose tree.json is unreadable is reported as an
// error rather than skipped — a truncated capture would otherwise audit
// as a compliant one.
func Load(dir string) ([]*Harvest, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("audit: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("audit: %s is not a directory", dir)
	}

	roots, err := findHarvestDirs(dir)
	if err != nil {
		return nil, err
	}
	if len(roots) == 0 {
		return nil, ErrNoHarvest
	}

	out := make([]*Harvest, 0, len(roots))
	for _, d := range roots {
		h, err := loadOne(d)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, nil
}

// findHarvestDirs returns the top-most folders under dir that hold a
// capture. Descent stops at each hit: anything below is that harvest's
// own followed nodes, loaded as Children.
//
// A folder counts as a capture if it holds EITHER a tree.json or a
// device.json. The exporter writes device.json first and tree.json
// last, so a run that was interrupted leaves the identity without the
// tree — and requiring tree.json would make that folder invisible.
// On a registry capture that is the worst possible failure: the
// registry level vanishes, its followed nodes load as unrelated
// devices, and the report describes a plant that was never audited as
// one. Better to load it and say it is incomplete.
func isHarvestDir(p string) bool {
	for _, f := range []string{"tree.json", "device.json"} {
		if _, err := os.Stat(filepath.Join(p, f)); err == nil {
			return true
		}
	}
	return false
}

func findHarvestDirs(dir string) ([]string, error) {
	var hits []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if isHarvestDir(p) {
			hits = append(hits, p)
			return fs.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("audit: scanning %s: %w", dir, err)
	}
	sort.Strings(hits)
	return hits, nil
}

// loadOne reads a single harvest folder plus its followed nodes.
func loadOne(dir string) (*Harvest, error) {
	h := &Harvest{
		Dir:  dir,
		Role: "unknown",
		APIs: map[string]API{},
		SDP:  map[string][]byte{},
	}

	var tf treeFile
	raw, err := os.ReadFile(filepath.Join(dir, "tree.json"))
	switch {
	case os.IsNotExist(err):
		// Interrupted capture. The identity below still names the
		// device, and the checks report the gap rather than auditing
		// the absence as if it were a finding about the device.
		h.Partial = true
	case err != nil:
		return nil, fmt.Errorf("audit: %w", err)
	default:
		if err := json.Unmarshal(raw, &tf); err != nil {
			return nil, fmt.Errorf("audit: %s/tree.json: %w", dir, err)
		}
		h.Target = tf.Target
	}

	for name, blob := range tf.APIs {
		// `_root` is the bare /x-nmos array, not an API object.
		if strings.HasPrefix(name, "_") {
			continue
		}
		var a API
		if err := json.Unmarshal(blob, &a); err != nil {
			// A device that answered /x-nmos/<name>/ with something
			// other than the expected object is itself a finding, but
			// it is the checks' job to say so — loading must not fail.
			continue
		}
		if a.Data == nil {
			a.Data = map[string]map[string]json.RawMessage{}
		}
		h.APIs[name] = a
	}

	// device.json carries the identity the exporter settled on. It is
	// written twice — once at start, once at the end with the label
	// filled in — so a truncated capture still has a target.
	if b, err := os.ReadFile(filepath.Join(dir, "device.json")); err == nil {
		var df deviceFile
		if json.Unmarshal(b, &df) == nil {
			if df.Target != "" {
				h.Target = df.Target
			}
			if df.Role != "" && df.Role != "in-progress" {
				h.Role = df.Role
			}
			h.Label, h.ID = df.Label, df.ID
		}
	}
	if h.Role == "unknown" {
		h.Role = inferRole(h)
	}

	if b, err := os.ReadFile(filepath.Join(dir, "report.txt")); err == nil {
		h.Report = splitLines(string(b))
	}

	// SDPs are exported under sdp/{is04,is05,receivers}/<id>.sdp — a
	// flat ReadDir misses them all (it skips directories). Walk the
	// subtree; key each file by its relative path so is04 and is05
	// captures of the same sender id stay distinct.
	sdpDir := filepath.Join(dir, "sdp")
	_ = filepath.WalkDir(sdpDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(d.Name()), ".sdp") {
			return nil //nolint:nilerr // a missing sdp dir is not an error
		}
		if b, rerr := os.ReadFile(p); rerr == nil {
			rel, relErr := filepath.Rel(sdpDir, p)
			if relErr != nil {
				rel = d.Name()
			}
			h.SDP[filepath.ToSlash(rel)] = b
		}
		return nil
	})

	// Followed nodes. Each is a full harvest in its own right.
	kidsDir := filepath.Join(dir, "nodes")
	if entries, err := os.ReadDir(kidsDir); err == nil {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, n := range names {
			kid := filepath.Join(kidsDir, n)
			if !isHarvestDir(kid) {
				continue
			}
			c, err := loadOne(kid)
			if err != nil {
				return nil, err
			}
			h.Children = append(h.Children, c)
		}
	}

	return h, nil
}

// inferRole classifies a device that did not record its own role, from
// which APIs answered. A query API means it serves a catalogue to
// controllers; a node API means it publishes its own resources.
func inferRole(h *Harvest) string {
	if _, ok := h.APIs["query"]; ok {
		return "registry"
	}
	if _, ok := h.APIs["registration"]; ok {
		return "registry"
	}
	if _, ok := h.APIs["node"]; ok {
		return "node"
	}
	return "unknown"
}

// splitLines splits on either line ending and drops the trailing empty
// element, so a CRLF report.txt written on Windows reads the same as an
// LF one written on Linux.
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	out := strings.Split(s, "\n")
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// All flattens a harvest and everything it followed into one slice,
// parents before children.
func (h *Harvest) All() []*Harvest {
	out := []*Harvest{h}
	for _, c := range h.Children {
		out = append(out, c.All()...)
	}
	return out
}

// Name is the most identifying string available: the label if the
// device published one, else the target it was reached at.
func (h *Harvest) Name() string {
	if h.Label != "" {
		return h.Label
	}
	return h.Target
}

// resources returns the raw JSON array captured for an API/resource at
// the highest captured version, plus the version it came from.
//
// The exporter walks a node's own APIs at the highest minor only — the
// same resources repeat at every minor — but walks a Registry's query
// API at every minor, because IS-04 version isolation means a lower
// minor can list resources a higher one hides. Taking the highest here
// is therefore right for a node and wrong for a registry, which is why
// resourcesEveryVersion exists alongside it.
func (h *Harvest) resources(api, name string) ([]json.RawMessage, string) {
	a, ok := h.APIs[api]
	if !ok {
		return nil, ""
	}
	for _, v := range sortedVersionsDesc(keysOf(a.Data)) {
		blob, ok := a.Data[v][name]
		if !ok {
			continue
		}
		items := decodeArray(blob)
		if items != nil {
			return items, v
		}
	}
	return nil, ""
}

// resourcesEveryVersion returns the union of a resource across every
// captured minor, keyed by resource id, plus the per-version counts.
// The counts are the evidence for a version-isolation finding.
func (h *Harvest) resourcesEveryVersion(api, name string) (map[string]json.RawMessage, map[string]int) {
	a, ok := h.APIs[api]
	if !ok {
		return nil, nil
	}
	union := map[string]json.RawMessage{}
	counts := map[string]int{}
	for _, v := range sortedVersionsDesc(keysOf(a.Data)) {
		blob, ok := a.Data[v][name]
		if !ok {
			continue
		}
		items := decodeArray(blob)
		counts[v] = len(items)
		for _, it := range items {
			if id := idOf(it); id != "" {
				if _, seen := union[id]; !seen {
					union[id] = it
				}
			}
		}
	}
	return union, counts
}

// captured reports whether a resource was actually fetched, at any
// minor, as distinct from fetched-and-empty.
//
// The difference decides whether a dangling reference is a finding. A
// node that publishes `"flows": []` has told us it has no flows, so a
// sender pointing at one is broken. A capture that never reached
// /flows has told us nothing, and reporting every sender on it would
// bury the real findings under noise from a partial export.
func (h *Harvest) captured(api, name string) bool {
	a, ok := h.APIs[api]
	if !ok {
		return false
	}
	for _, bucket := range a.Data {
		if blob, ok := bucket[name]; ok && len(blob) > 0 && !isJSONNull(blob) {
			return true
		}
	}
	return false
}

// object returns a single captured object (not an array) for an
// API/resource at the highest captured version.
func (h *Harvest) object(api, name string) (json.RawMessage, string) {
	a, ok := h.APIs[api]
	if !ok {
		return nil, ""
	}
	for _, v := range sortedVersionsDesc(keysOf(a.Data)) {
		if blob, ok := a.Data[v][name]; ok && len(blob) > 0 && !isJSONNull(blob) {
			return blob, v
		}
	}
	return nil, ""
}

// decodeArray unmarshals a captured blob as a JSON array. A capture
// that recorded `null` — the request failed — yields nil, so callers
// treat "absent" and "empty" alike.
//
// A lone object is read as a one-element array. This is not leniency
// about the wire: it is leniency about the capture. PowerShell's
// ConvertTo-Json serialises a one-element array as a bare object, so
// every export taken with the original exporter records a node's single
// Device as `{...}` rather than `[{...}]`. Read strictly, that node has
// no devices, and every Sender, Flow and Receiver on it dangles — 18104
// findings on one real 7-node capture, none of them true.
//
// The device-side version of this mistake would be a genuine
// violation, but a capture cannot tell the two apart after the fact,
// and inventing 18104 findings is far worse than missing one. The Go
// exporter cannot produce the collapsed shape, so captures taken with
// it are unambiguous.
func decodeArray(blob json.RawMessage) []json.RawMessage {
	if len(blob) == 0 || isJSONNull(blob) {
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(blob, &items); err == nil {
		return items
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(blob, &obj); err == nil && len(obj) > 0 {
		return []json.RawMessage{blob}
	}
	return nil
}

func isJSONNull(b json.RawMessage) bool {
	return strings.TrimSpace(string(b)) == "null"
}

// idOf pulls the `id` field out of a resource blob without decoding
// the whole resource — used for union/dedup work where the rest of the
// resource is irrelevant.
func idOf(b json.RawMessage) string {
	var probe struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(b, &probe) != nil {
		return ""
	}
	return probe.ID
}

func keysOf(m map[string]map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// sortedVersionsDesc orders `v1.10` after `v1.9`, which a plain string
// sort gets wrong. Anything unparseable sorts last.
func sortedVersionsDesc(vs []string) []string {
	out := append([]string(nil), vs...)
	sort.SliceStable(out, func(i, j int) bool {
		return versionRank(out[i]) > versionRank(out[j])
	})
	return out
}

// versionRank maps `vMAJOR.MINOR` to a sortable integer.
func versionRank(v string) int {
	s := strings.TrimPrefix(strings.TrimSpace(v), "v")
	maj, min, ok := strings.Cut(s, ".")
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
