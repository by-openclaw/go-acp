package consumer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
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

	plan, err := p.resourcePaths(ctx, client, spec)
	if err != nil {
		return nil, err
	}

	// Read the resources concurrently and flatten them in order.
	//
	// An audio shuffler declares roughly 35 000 of them — every channel
	// of every stream is its own resource — and one at a time that is a
	// ten-minute walk of nothing but waiting. The device is an HTTP
	// server; a handful of requests in flight is ordinary for it, and
	// the model that comes out is identical either way, because the
	// order objects are built in is the order of plan.paths and not the
	// order the answers arrive.
	bodies := make([][]byte, len(plan.paths))
	errs := make([]error, len(plan.paths))
	var (
		wg  sync.WaitGroup
		sem = make(chan struct{}, readConcurrency)
	)
	for i, path := range plan.paths {
		// A collection whose members are resources of their own is
		// read for its ids, not for its contents: this device answers
		// `/processing/video/channels` with the full body of every
		// channel, and `/processing/video/channels/{uuid}` with one of
		// them again. Flattening both puts every field in the model
		// twice — once with the collection's access bits (no PUT) and
		// once with the member's (PUT) — which is worse than either,
		// because it makes a writable object look read-only depending
		// on which copy you read.
		if plan.collections[path] {
			continue
		}
		wg.Add(1)
		go func(i int, path string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			bodies[i], errs[i] = p.read(ctx, client, plan, path)
		}(i, path)
	}
	wg.Wait()

	var (
		objs []dhsc.Object
		read int
	)
	for i, path := range plan.paths {
		if plan.collections[path] {
			continue
		}
		if errs[i] != nil {
			// A declared resource this device does not serve — an
			// unlicensed option, a board that is not fitted. The spec
			// describes the product; the box is one build of it.
			p.deps.Logger.Debug("ccm: declared resource not served",
				"path", path, "err", errs[i].Error())
			continue
		}
		read++
		// Kept for the link pass, which has to know which member owns
		// which channel and would otherwise read every member again.
		if j := strings.LastIndex(path, "/"); j > 0 && plan.collections[path[:j]] {
			plan.bodies[path] = bodies[i]
		}
		writable := false
		if tpl, ok := spec.TemplateFor(path); ok {
			writable = spec.Writable(tpl)
		}
		objs = append(objs, flatten(path, bodies[i], writable)...)
	}
	p.RecordRx()

	sort.Slice(objs, func(i, j int) bool {
		return strings.Join(objs[i].Path, "/") < strings.Join(objs[j].Path, "/")
	})

	// Resolve what the matrices route, once, into the model — so a
	// crosspoint carries the resource it addresses rather than a name
	// every reader would have to resolve again. See link.go.
	p.linkMatrices(ctx, client, spec, plan, objs)

	p.mu.Lock()
	p.tree = objs
	p.byPath = make(map[string]dhsc.Object, len(objs))
	for _, o := range objs {
		p.byPath[strings.Join(o.Path, ".")] = o
	}
	p.mu.Unlock()

	p.deps.Logger.Debug("ccm: model read",
		"resources", read, "declared", len(plan.paths), "objects", len(objs))
	return objs, nil
}

// walkPlan is what one walk reads: the concrete resource paths, which
// of those are collections read for their ids rather than their
// contents, and the member bodies a collection listing already carried.
type walkPlan struct {
	paths       []string
	collections map[string]bool
	// listed is a member's body as its OWN collection listed it, by
	// member path. Whether that may stand in for reading the member is
	// not assumed — it is proven once per collection, in read.
	listed map[string][]byte
	// trusted is decided while the resources are being read
	// concurrently, so it is the one part of the plan under a lock.
	trustedMu sync.Mutex
	trusted   map[string]bool
	// listings is every collection read during the walk, so the link
	// pass that follows re-reads none of them.
	listings map[string]listing
	// bodies is what each member answered, kept for the same reason:
	// the link pass needs to know which stream owns which channel, and
	// the member already said.
	bodies map[string][]byte
}

// newWalkPlan is an empty plan, ready to be filled by one walk.
func newWalkPlan() *walkPlan {
	return &walkPlan{
		collections: map[string]bool{},
		listed:      map[string][]byte{},
		trusted:     map[string]bool{},
		listings:    map[string]listing{},
		bodies:      map[string][]byte{},
	}
}

// readConcurrency is how many resource reads are in flight at once.
//
// Eight is chosen to be unremarkable to the device: a broadcast
// appliance serving its own web UI already answers more than that from
// one browser tab. It is a constant rather than a flag because an
// operator should not have to tune a walk to get a model.
const readConcurrency = 8

// maxParamDepth is how many parameters deep a declared path is
// expanded. Two is what these products use — the shuffler routes audio
// per channel, so `/io/ip/receivers/audio/{uuid}/channels/{channelUuid}`
// is a real resource — and the cap is here so a spec that nests further
// cannot turn one walk into a crawl of the whole device.
const maxParamDepth = 3

// resourcePaths is every path to read: the spec's GETs, with every
// parameterised segment expanded by the ids the device returns.
func (p *Plugin) resourcePaths(ctx context.Context, client *Client, spec *codec.Spec) (*walkPlan, error) {
	plan := newWalkPlan()
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
		return nil, fmt.Errorf("ccm: the spec declares no readable path")
	}

	out := static
	for _, tpl := range params {
		out = append(out, p.expand(ctx, client, plan, tpl)...)
	}

	// Deduplicate. The spec declares some resources under two parameter
	// NAMES for the same thing — `/processing/video/channels/{id}` and
	// `/processing/video/channels/{uuid}` are one resource — and both
	// expand with the same ids. Reading each twice would double the
	// round trips and put every object in the model twice.
	sort.Strings(out)
	plan.paths = dedupe(out)
	return plan, nil
}

// expand turns one declared template into the paths the device serves,
// one parameter at a time.
//
// `/io/sdi/{uuid}` becomes the uuids `/io/sdi` returns. Then — because
// the audio shuffler routes each channel of each stream separately —
// `/io/ip/receivers/audio/{uuid}/channels/{channelUuid}` becomes the
// channels each of those receivers returns. Expanding only the first
// parameter would send `{channelUuid}` to the device as a literal, take
// the 404, and leave every per-channel resource out of the model
// without anything in the export saying so.
func (p *Plugin) expand(ctx context.Context, client *Client, plan *walkPlan, tpl string) []string {
	current := []string{tpl}
	for depth := 0; depth < maxParamDepth; depth++ {
		var next []string
		open := false
		for _, cand := range current {
			i := strings.Index(cand, "{")
			if i < 0 {
				next = append(next, cand)
				continue
			}
			open = true
			coll := strings.TrimSuffix(cand[:i], "/")
			tail := ""
			if j := strings.Index(cand[i:], "/"); j >= 0 {
				tail = cand[i:][j:]
			}
			// The collection is read for its ids, so its own body must
			// not also become objects — see Walk.
			plan.collections[coll] = true
			l := p.listing(ctx, client, plan, coll)
			for _, id := range l.ids {
				member := coll + "/" + id
				if tail == "" {
					if raw, ok := l.members[id]; ok {
						plan.listed[member] = raw
					}
				}
				next = append(next, member+tail)
			}
		}
		current = next
		if !open {
			return current
		}
	}
	var done []string
	for _, c := range current {
		if strings.Contains(c, "{") {
			p.deps.Logger.Debug("ccm: path nests deeper than a walk expands", "path", c)
			continue
		}
		done = append(done, c)
	}
	return done
}

// read returns one resource's body, using the collection listing that
// already carried it when that listing has been PROVEN to say the same
// thing as the resource itself.
//
// The proof is one read per collection: this device answers
// `/processing/video/channels` with the full body of every channel, so
// reading each member again is a round trip for an answer already in
// hand — 63 of them on a 64-member collection. It is proven rather than
// assumed because a device is free to list a summary and serve the
// detail, and a model built from summaries would be missing fields no
// reader could know were missing.
func (p *Plugin) read(ctx context.Context, client *Client, plan *walkPlan, path string) ([]byte, error) {
	listed, ok := plan.listed[path]
	if !ok {
		return client.get(ctx, path)
	}
	coll := path[:strings.LastIndex(path, "/")]
	plan.trustedMu.Lock()
	trusted, decided := plan.trusted[coll]
	plan.trustedMu.Unlock()
	if decided {
		if trusted {
			return listed, nil
		}
		return client.get(ctx, path)
	}
	body, err := client.get(ctx, path)
	if err != nil {
		return nil, err
	}
	same := sameJSON(listed, body)
	plan.trustedMu.Lock()
	plan.trusted[coll] = same
	plan.trustedMu.Unlock()
	p.deps.Logger.Debug("ccm: collection listing checked against its member",
		"collection", coll, "listing_is_the_whole_member", same)
	return body, nil
}

// sameJSON compares two bodies as JSON values, so key order and
// whitespace do not decide it.
func sameJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return bytes.Equal(a, b)
	}
	return reflect.DeepEqual(x, y)
}

// listing is a collection body split into its members.
type listing struct {
	ids     []string
	members map[string][]byte
}

// listing reads a collection once, whatever asks for it.
func (p *Plugin) listing(ctx context.Context, client *Client, plan *walkPlan, coll string) listing {
	if l, done := plan.listings[coll]; done {
		return l
	}
	body, err := client.get(ctx, coll)
	if err == nil {
		if l := parseListing(body); len(l.ids) > 0 {
			plan.listings[coll] = l
			return l
		}
	}
	// A collection the device does not serve as a resource of its own
	// may still be published — as a FIELD of its parent. The shuffler
	// declares /io/ip/receivers/audio/{uuid}/channels/{channelUuid} but
	// serves no .../channels listing; the receiver itself carries
	// `"channels": [uuid, …]`. Without this the ids are unobtainable,
	// every per-channel resource stays out of the model, and nothing in
	// the export says why.
	if l, ok := p.listingFromParent(ctx, client, plan, coll); ok {
		plan.listings[coll] = l
		return l
	}
	if err != nil {
		p.deps.Logger.Debug("ccm: collection not served", "path", coll, "err", err.Error())
	}
	plan.listings[coll] = listing{}
	return listing{}
}

// listingFromParent reads a collection out of the field its parent
// names it by.
func (p *Plugin) listingFromParent(ctx context.Context, client *Client, plan *walkPlan, coll string) (listing, bool) {
	i := strings.LastIndex(coll, "/")
	if i <= 0 {
		return listing{}, false
	}
	parent, field := coll[:i], coll[i+1:]
	// Only the parent's OWN body will do. What its collection listed
	// for it may be a summary — this device lists `{"uuid":"rx1"}` and
	// serves the channels only on the member itself — and a summary
	// that happens to omit the field would look exactly like a member
	// with no channels at all.
	body, ok := plan.bodies[parent]
	if !ok {
		var err error
		if body, err = client.get(ctx, parent); err != nil {
			return listing{}, false
		}
		plan.bodies[parent] = body
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil {
		return listing{}, false
	}
	raw, has := fields[field]
	if !has {
		return listing{}, false
	}
	l := parseListing(raw)
	if len(l.ids) == 0 {
		return listing{}, false
	}
	return l, true
}

// parseListing pulls the members out of a collection body: each
// element's "uuid" or "id", and the element itself when it is an object.
// A collection of plain strings is a list of names, which are ids with
// no body of their own.
func parseListing(body []byte) listing {
	var raw []json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return listing{}
	}
	l := listing{members: map[string][]byte{}}
	for _, e := range raw {
		var name string
		if json.Unmarshal(e, &name) == nil {
			if name != "" {
				l.ids = append(l.ids, name)
			}
			continue
		}
		var id struct {
			UUID string `json:"uuid"`
			ID   *int   `json:"id"`
		}
		if json.Unmarshal(e, &id) != nil {
			continue
		}
		switch {
		case id.UUID != "":
			l.ids = append(l.ids, id.UUID)
			l.members[id.UUID] = e
		case id.ID != nil:
			s := strconv.Itoa(*id.ID)
			l.ids = append(l.ids, s)
			l.members[s] = e
		}
	}
	return l
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
