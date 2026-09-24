package consumer

import (
	"context"
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
func (p *Plugin) linkMatrices(ctx context.Context, client *Client, spec *codec.Spec, objs []dhsc.Object) {
	byPath := make(map[string]int, len(objs))
	for i, o := range objs {
		byPath[strings.Join(o.Path, "/")] = i
	}

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
