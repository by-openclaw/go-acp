package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"dhs/internal/ccm/codec"
	dhsc "dhs/internal/consumer"
)

// Turning a REST device into a device model.
//
// The shape is already a tree — a path is a path — so the work is not
// discovering structure, it is deciding WHAT TO READ and what each
// leaf means. Both come from the device's own api.yml:
//
//   - every GET the spec declares is read, not only the ones a parent
//     node happens to list. That is the difference between 41
//     resources and the whole model;
//   - a parameterised path (`/io/sdi/{uuid}`) is expanded with the ids
//     the device itself returns, so the per-stream resources — where
//     the live state is — are in the model rather than one level
//     below it;
//   - a leaf is writable iff the spec declares PUT on the resource
//     that owns it. Nothing in a GET response says so, and an operator
//     who cannot tell a setting from a reading will eventually try to
//     write a reading.

// access bits, matching the ACP1 access byte the neutral Object uses.
const (
	accessRead  uint8 = 1
	accessWrite uint8 = 2
)

// Walk reads the device model: every resource the spec declares,
// flattened to one Object per leaf value.
func (p *Plugin) Walk(ctx context.Context, slot int) ([]dhsc.Object, error) {
	if slot != 0 {
		return nil, nil
	}
	client, spec, err := p.session()
	if err != nil {
		return nil, err
	}

	paths, collections, err := p.resourcePaths(ctx, client, spec)
	if err != nil {
		return nil, err
	}

	var (
		objs []dhsc.Object
		read int
	)
	for _, path := range paths {
		// A collection whose members are resources of their own is
		// read for its ids, not for its contents: this device answers
		// `/processing/video/channels` with the full body of every
		// channel, and `/processing/video/channels/{uuid}` with one of
		// them again. Flattening both puts every field in the model
		// twice — once with the collection's access bits (no PUT) and
		// once with the member's (PUT) — which is worse than either,
		// because it makes a writable object look read-only depending
		// on which copy you read.
		if collections[path] {
			continue
		}
		body, gerr := client.get(ctx, path)
		if gerr != nil {
			// A declared resource this device does not serve — an
			// unlicensed option, a board that is not fitted. The spec
			// describes the product; the box is one build of it.
			p.deps.Logger.Debug("ccm: declared resource not served",
				"path", path, "err", gerr.Error())
			continue
		}
		read++
		writable := false
		if tpl, ok := spec.TemplateFor(path); ok {
			writable = spec.Writable(tpl)
		}
		objs = append(objs, flatten(path, body, writable)...)
	}
	p.RecordRx()

	sort.Slice(objs, func(i, j int) bool {
		return strings.Join(objs[i].Path, "/") < strings.Join(objs[j].Path, "/")
	})

	p.mu.Lock()
	p.tree = objs
	p.byPath = make(map[string]dhsc.Object, len(objs))
	for _, o := range objs {
		p.byPath[strings.Join(o.Path, ".")] = o
	}
	p.mu.Unlock()

	p.deps.Logger.Debug("ccm: model read",
		"resources", read, "declared", len(paths), "objects", len(objs))
	return objs, nil
}

// resourcePaths is every path to read: the spec's GETs, with each
// parameterised one expanded by the ids the device returns.
func (p *Plugin) resourcePaths(ctx context.Context, client *Client, spec *codec.Spec) ([]string, map[string]bool, error) {
	var (
		static []string
		params []string
	)
	for _, path := range spec.With(codec.GET) {
		// The API root is a listing of the top-level node names, every
		// one of which the spec declares in its own right. Reading it
		// as a resource would put `.0`, `.1`, `.2` in the model —
		// index-addressed copies of names that are already paths.
		if path == "" || path == "/" {
			continue
		}
		if strings.Contains(path, "{") {
			params = append(params, path)
			continue
		}
		static = append(static, path)
	}
	if len(static) == 0 {
		return nil, nil, fmt.Errorf("ccm: the spec declares no readable path")
	}

	// The ids come from the collection each parameterised path hangs
	// off: /io/sdi/{uuid} is expanded with the uuids /io/sdi returns.
	// Those collections are recorded, because their own bodies must
	// not become objects — see Walk.
	ids := map[string][]string{}
	collections := map[string]bool{}
	for _, path := range params {
		coll := path[:strings.Index(path, "{")]
		coll = strings.TrimSuffix(coll, "/")
		collections[coll] = true
		if _, done := ids[coll]; done {
			continue
		}
		body, err := client.get(ctx, coll)
		if err != nil {
			p.deps.Logger.Debug("ccm: collection not served", "path", coll, "err", err.Error())
			ids[coll] = nil
			continue
		}
		ids[coll] = identifiers(body)
	}

	out := static
	for _, path := range params {
		coll := strings.TrimSuffix(path[:strings.Index(path, "{")], "/")
		rest := path[strings.Index(path, "{"):]
		tail := ""
		if i := strings.Index(rest, "/"); i >= 0 {
			tail = rest[i:] // "/status"
		}
		for _, id := range ids[coll] {
			out = append(out, coll+"/"+id+tail)
		}
	}

	// Deduplicate. The spec declares some resources under two parameter
	// NAMES for the same thing — `/processing/video/channels/{id}` and
	// `/processing/video/channels/{uuid}` are one resource — and both
	// expand with the same ids. Reading each twice would double the
	// round trips and put every object in the model twice.
	sort.Strings(out)
	return dedupe(out), collections, nil
}

// dedupe removes repeats from a sorted slice, in place.
func dedupe(sorted []string) []string {
	if len(sorted) < 2 {
		return sorted
	}
	n := 1
	for _, s := range sorted[1:] {
		if s != sorted[n-1] {
			sorted[n] = s
			n++
		}
	}
	return sorted[:n]
}

// identifiers pulls the ids out of a collection body: the "uuid" of
// every element, or the elements themselves when the collection is a
// list of names.
func identifiers(body []byte) []string {
	var asObjects []struct {
		UUID string `json:"uuid"`
		ID   *int   `json:"id"`
	}
	if err := json.Unmarshal(body, &asObjects); err == nil {
		var out []string
		for _, e := range asObjects {
			switch {
			case e.UUID != "":
				out = append(out, e.UUID)
			case e.ID != nil:
				out = append(out, strconv.Itoa(*e.ID))
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	var asNames []string
	if err := json.Unmarshal(body, &asNames); err == nil {
		return asNames
	}
	return nil
}

// flatten turns one resource body into one Object per leaf value.
//
// The Object's path is the resource path plus the field path inside
// it, so an operator addresses exactly what they see: an array element
// carrying a uuid is addressed by that uuid rather than by its index,
// because an index moves when the device reorders and a uuid does not.
func flatten(resource string, body []byte, writable bool) []dhsc.Object {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		// Not JSON — the resource is still real, so it is recorded as
		// one opaque string rather than dropped.
		return []dhsc.Object{leaf(segments(resource), string(body), writable)}
	}
	base := segments(resource)
	var out []dhsc.Object
	walkJSON(base, v, writable, &out)
	return out
}

func walkJSON(path []string, v any, writable bool, out *[]dhsc.Object) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			walkJSON(append(path[:len(path):len(path)], k), t[k], writable, out)
		}
	case []any:
		for i, e := range t {
			seg := strconv.Itoa(i)
			if m, ok := e.(map[string]any); ok {
				if id, ok := m["uuid"].(string); ok && id != "" {
					seg = id
				}
			}
			walkJSON(append(path[:len(path):len(path)], seg), e, writable, out)
		}
	default:
		*out = append(*out, leaf(path, v, writable))
	}
}

// leaf builds one Object from one scalar.
func leaf(path []string, v any, writable bool) dhsc.Object {
	o := dhsc.Object{
		Slot:   0,
		Path:   append([]string(nil), path...),
		Label:  path[len(path)-1],
		Access: accessRead,
	}
	if writable {
		o.Access |= accessWrite
	}
	switch t := v.(type) {
	case bool:
		o.Kind = dhsc.KindBool
		o.Value = dhsc.Value{Kind: dhsc.KindBool, Bool: t}
	case float64:
		// JSON has one number type. An integral value is presented as
		// an integer, because that is what the device's own document
		// says it is and what an operator types.
		if t == float64(int64(t)) {
			o.Kind = dhsc.KindInt
			o.Value = dhsc.Value{Kind: dhsc.KindInt, Int: int64(t)}
			break
		}
		o.Kind = dhsc.KindFloat
		o.Value = dhsc.Value{Kind: dhsc.KindFloat, Float: t}
	case string:
		o.Kind = dhsc.KindString
		o.Value = dhsc.Value{Kind: dhsc.KindString, Str: summarise(t)}
		o.MaxLen = len(t)
	case nil:
		// A field the device declares and has no value for. Recorded,
		// so its absence is visible rather than inferred from a gap.
		o.Kind = dhsc.KindString
		o.Value = dhsc.Value{Kind: dhsc.KindString, Str: ""}
	default:
		o.Kind = dhsc.KindString
		o.Value = dhsc.Value{Kind: dhsc.KindString, Str: fmt.Sprint(t)}
	}
	return o
}

// maxValueLen is the longest string value kept verbatim in the model.
//
// A device model is what an operator reads, an alarm rule matches and
// a firmware diff compares. This device answers
// `/processing/video/channels/{uuid}` with a `thumbnail` field
// carrying a whole JPEG — tens of kilobytes of binary per channel,
// which is a picture of the video, not a property of the device. Keep
// the fact that it is there and how big it is; drop the bytes.
const maxValueLen = 256

// summarise keeps a value readable: short text verbatim, anything long
// or not printable replaced by a description of itself.
func summarise(s string) string {
	if len(s) <= maxValueLen && utf8.ValidString(s) && isPrintable(s) {
		return s
	}
	if !utf8.ValidString(s) || !isPrintable(s) {
		return fmt.Sprintf("<binary, %d bytes>", len(s))
	}
	return s[:maxValueLen] + fmt.Sprintf("… <%d bytes total>", len(s))
}

// isPrintable reports whether every rune is something a terminal can
// show — tab and newline included, control bytes and NULs not.
func isPrintable(s string) bool {
	for _, r := range s {
		if r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// segments splits an API path into path elements.
func segments(path string) []string {
	return strings.Split(strings.Trim(path, "/"), "/")
}

// session returns the connected client and spec.
func (p *Plugin) session() (*Client, *codec.Spec, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.client == nil {
		return nil, nil, errNotConnected
	}
	return p.client, p.spec, nil
}
