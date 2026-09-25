package consumer

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"dhs/internal/ccm/codec"
	dhsc "dhs/internal/consumer"
)

// Putting the routing links INTO the model.
//
// A crosspoint object carries two strings: its name and the name of
// whatever feeds it. That is what the device says and it is not enough
// to act on — "CH04 = IP01" does not tell a UI where to go when
// somebody clicks it, and every consumer of the model would otherwise
// re-derive the same answer from the same three endpoints.
//
// So the answer is derived once, here, and attached to the object:
// which resource this crosspoint IS, and which resource feeds it. A
// dashboard, `dhs-srv` and an operator reading the DM all get the same
// link, and it stays right when the device renumbers, because it was
// resolved from that device's own info endpoint on the walk that
// produced the model.
//
// It is generic on purpose. The two products here describe their
// matrices differently — labels and positions on the video bridge,
// UUIDs on the audio shuffler — and codec.MatrixInfo absorbs both, so
// nothing in this file knows which product it is talking to.

// Meta keys attached to a crosspoint object. Short, because they end
// up in every exported row of a 4352-crosspoint matrix.
const (
	// MetaTarget is the resource this crosspoint addresses — the thing
	// being fed.
	MetaTarget = "target"
	// MetaTargetSub is the channel within it, when the matrix routes a
	// resource's channels separately (an audio delay bank has 16).
	MetaTargetSub = "target_channel"
	// MetaTargetType is the device's own word for what it is.
	MetaTargetType = "target_type"
	// MetaSource is the resource feeding it, and its channel and kind.
	MetaSource     = "source"
	MetaSourceSub  = "source_channel"
	MetaSourceType = "source_type"
)

// linkMatrices resolves every matrix the spec declares and annotates
// the crosspoint objects in place.
//
// Failures are not fatal and not silent: a matrix whose info endpoint
// this device does not serve leaves its crosspoints unannotated, which
// is what the model looked like before, and says so in the debug log.
func (p *Plugin) linkMatrices(ctx context.Context, client *Client, spec *codec.Spec, plan *walkPlan, objs []dhsc.Object) {
	byPath := make(map[string]int, len(objs))
	for i, o := range objs {
		byPath[strings.Join(o.Path, "/")] = i
	}
	channels := channelsByMember(objs)

	for _, infoPath := range matrixInfoPaths(spec) {
		body, err := client.get(ctx, infoPath)
		if err != nil {
			p.deps.Logger.Debug("ccm: matrix info not served", "path", infoPath, "err", err.Error())
			continue
		}
		info, err := codec.ParseMatrixInfo(body)
		if err != nil {
			p.deps.Logger.Debug("ccm: matrix info not understood", "path", infoPath, "err", err.Error())
			continue
		}
		info.SetIndex(
			p.indexAxis(ctx, client, plan, channels, info.Sources),
			p.indexAxis(ctx, client, plan, channels, info.Destinations),
		)

		for _, statePath := range matrixStatePaths(spec, infoPath) {
			state, err := client.get(ctx, statePath)
			if err != nil {
				continue
			}
			points, err := info.ResolveState(state)
			if err != nil {
				p.deps.Logger.Debug("ccm: matrix state not understood",
					"path", statePath, "err", err.Error())
				continue
			}
			linked := 0
			for _, xp := range points {
				key := strings.TrimPrefix(statePath, "/") + "/" + xp.Destination.Key
				i, ok := byPath[key]
				if !ok {
					continue
				}
				annotate(&objs[i], xp)
				linked++
			}
			p.deps.Logger.Debug("ccm: matrix linked",
				"state", statePath, "crosspoints", len(points), "linked", linked)
		}
	}
}

// annotate writes one crosspoint's resolved sides onto its object.
func annotate(o *dhsc.Object, xp codec.Crosspoint) {
	if o.Meta == nil {
		o.Meta = map[string]any{}
	}
	if xp.Destination.Resolved {
		o.Meta[MetaTarget] = xp.Destination.Path
		if xp.Destination.Sub >= 0 {
			o.Meta[MetaTargetSub] = xp.Destination.Sub
		}
		if xp.Destination.Type != "" {
			o.Meta[MetaTargetType] = xp.Destination.Type
		}
	}
	if xp.Source.Resolved {
		o.Meta[MetaSource] = xp.Source.Path
		if xp.Source.Sub >= 0 {
			o.Meta[MetaSourceSub] = xp.Source.Sub
		}
		if xp.Source.Type != "" {
			o.Meta[MetaSourceType] = xp.Source.Type
		}
	}
}

// indexAxis asks the device what the info body leaves unsaid: which of
// an axis's providers owns each crosspoint key, and — when a provider
// routes the channels INSIDE its members — which member each channel
// belongs to.
//
// The audio shuffler needs both. Its matrix has five source providers
// and four destination providers, every key a UUID, and each key names
// a channel of a stream rather than the stream. Without this, not one
// of its 17 728 crosspoints resolves to anything; with it, a crosspoint
// carries the exact resource an operator opens to change that audio.
//
// It costs no extra round trips on a device the walk has already read:
// the collections come from the walk's own listings, and the members
// from the bodies it already fetched.
func (p *Plugin) indexAxis(ctx context.Context, client *Client, plan *walkPlan, channels map[string][]string, providers []codec.MatrixProvider) codec.Index {
	idx := codec.Index{}
	for _, pr := range providers {
		if pr.Template != "" {
			// Positional: the info body lists the children in order,
			// and the label says which. Nothing to look up.
			continue
		}
		coll := relativeTo(client, pr.Path)
		if coll == "" {
			continue
		}
		l := p.listing(ctx, client, plan, coll)
		for _, id := range l.ids {
			if !pr.RoutesChannels() {
				idx[id] = codec.Endpoint{Key: id, ID: id, Sub: -1,
					Path: joinDeclared(pr.Path, id), Type: pr.Type, Resolved: true}
				continue
			}
			member := coll + "/" + id
			// The WALKED MODEL is what names the channels, because it
			// is the one form that has already been normalised: a
			// device may publish its channel list as a JSON array or as
			// an object keyed by position, and flatten has resolved
			// that difference before this code sees it. Re-parsing the
			// raw body here would mean handling both again, and getting
			// it wrong is silent — the matrix simply does not resolve.
			//
			// The device is only asked when the walk did not read this
			// member at all, which is the case a provider names a
			// collection outside the walked model.
			chans := channels[strings.Trim(member, "/")]
			if len(chans) == 0 {
				chans = channelsOf(plan.bodies[member])
			}
			if len(chans) == 0 {
				chans = channelsOf(l.members[id])
			}
			if len(chans) == 0 {
				body, err := client.get(ctx, member)
				if err != nil {
					p.deps.Logger.Debug("ccm: matrix member not served",
						"path", member, "err", err.Error())
					continue
				}
				chans = channelsOf(body)
			}
			for n, ch := range chans {
				idx[ch] = codec.Endpoint{Key: ch, ID: id, Sub: n,
					Path: joinDeclared(pr.Path, id) + "/channels/" + ch,
					Type: pr.Type, Resolved: true}
			}
		}
	}
	if len(idx) == 0 {
		return nil
	}
	return idx
}

// channelsByMember reads every member's channel list out of the walked
// model: "io/ip/senders/audio/<uuid>" -> the channel uuids, by position.
//
// The model is the reliable place to read them. Whatever shape the
// device published — an array of uuids, or an object keyed by position
// — flatten has already turned it into one object per channel whose
// last path segment is the position and whose value is the uuid. The
// position is taken as a NUMBER, so a device that keys its channels
// "0".."15" as an object does not end up with channel 10 sitting
// between 1 and 2.
func channelsByMember(objs []dhsc.Object) map[string][]string {
	type slot struct {
		n  int
		id string
	}
	raw := map[string][]slot{}
	for _, o := range objs {
		if len(o.Path) < 3 || o.Path[len(o.Path)-2] != "channels" {
			continue
		}
		if o.Value.Kind != dhsc.KindString || o.Value.Str == "" {
			continue
		}
		n, err := strconv.Atoi(o.Path[len(o.Path)-1])
		if err != nil {
			continue
		}
		member := strings.Join(o.Path[:len(o.Path)-2], "/")
		raw[member] = append(raw[member], slot{n: n, id: o.Value.Str})
	}
	out := make(map[string][]string, len(raw))
	for member, slots := range raw {
		sort.Slice(slots, func(i, j int) bool { return slots[i].n < slots[j].n })
		ids := make([]string, 0, len(slots))
		for _, s := range slots {
			ids = append(ids, s.id)
		}
		out[member] = ids
	}
	return out
}

// channelsOf reads a member's channel list straight from its body, for
// a member the walk did not read. Both published shapes are accepted,
// for the same reason the model is preferred: a device is free to use
// either and neither is wrong.
func channelsOf(body []byte) []string {
	var fields struct {
		Channels json.RawMessage `json:"channels"`
	}
	if json.Unmarshal(body, &fields) != nil || len(fields.Channels) == 0 {
		return nil
	}
	var list []json.RawMessage
	if json.Unmarshal(fields.Channels, &list) != nil {
		// Not an array: an object keyed by position, which is the same
		// list written differently. Read it in numeric key order.
		var keyed map[string]json.RawMessage
		if json.Unmarshal(fields.Channels, &keyed) != nil {
			return nil
		}
		keys := make([]int, 0, len(keyed))
		byN := map[int]json.RawMessage{}
		for k, v := range keyed {
			n, err := strconv.Atoi(k)
			if err != nil {
				return nil
			}
			keys = append(keys, n)
			byN[n] = v
		}
		sort.Ints(keys)
		for _, n := range keys {
			list = append(list, byN[n])
		}
	}
	var out []string
	for _, e := range list {
		var id string
		if json.Unmarshal(e, &id) == nil {
			out = append(out, id)
			continue
		}
		var obj struct {
			UUID string `json:"uuid"`
		}
		if json.Unmarshal(e, &obj) == nil && obj.UUID != "" {
			out = append(out, obj.UUID)
			continue
		}
		// Keep the position: a channel this connector cannot name is
		// still channel n, and shifting the rest up would mislabel them.
		out = append(out, "")
	}
	return out
}

// relativeTo turns a path the device wrote in its own document into one
// this client can GET.
//
// The two products write it differently: the BRIDGE says
// "/api/v1/processing/video/channels" where the client is already based
// at /api/v1, and the shuffler says "/io/ip/senders/audio" where the
// client is based at /api. Taking either literally would miss.
func relativeTo(client *Client, declared string) string {
	if declared == "" {
		return ""
	}
	u, err := url.Parse(client.base)
	if err != nil {
		return declared
	}
	prefix := strings.TrimSuffix(u.Path, "/")
	if prefix != "" && strings.HasPrefix(declared, prefix+"/") {
		return strings.TrimPrefix(declared, prefix)
	}
	return declared
}

// joinDeclared puts a member under its collection, in the device's own
// spelling — that is what an Endpoint.Path is for, and what a UI hands
// back to the device.
func joinDeclared(collection, id string) string {
	return strings.TrimSuffix(collection, "/") + "/" + id
}

// matrixInfoPaths are the declared resources that describe a matrix's
// axes. Both products name them the same way — a resource called
// "info" — which is the only convention this relies on.
func matrixInfoPaths(spec *codec.Spec) []string {
	var out []string
	for _, p := range spec.With(codec.GET) {
		if strings.HasSuffix(p, "/info") && !strings.Contains(p, "{") {
			out = append(out, p)
		}
	}
	return out
}

// matrixStatePaths are the crosspoint maps an info endpoint describes:
// every other declared resource under its parent.
//
// That covers both layouts without knowing either — the bridge puts
// them beside the info (`/matrix/video/path/{info,main,backup}`) and
// the shuffler puts them a level down
// (`/matrices/audio/info` with `/matrices/audio/state/main`).
func matrixStatePaths(spec *codec.Spec, infoPath string) []string {
	parent := strings.TrimSuffix(infoPath, "/info")
	if parent == "" {
		return nil
	}
	var out []string
	for _, p := range spec.With(codec.GET) {
		switch {
		case p == infoPath, strings.Contains(p, "{"):
			// Itself, or a parameterised member — not a crosspoint map.
		case strings.HasSuffix(p, "/info"):
			// Another matrix's description.
		case strings.HasPrefix(p, parent+"/"):
			out = append(out, p)
		}
	}
	return out
}
