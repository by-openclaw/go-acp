package codec

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Linking a matrix to the things it routes.
//
// A crosspoint on its own is two strings — "CH04": "IP01" — which tells
// an operator nothing they can act on and a UI nothing it can navigate
// to. What makes it useful is resolving both sides to the RESOURCE
// each names: the processing channel that receives it, the receiver
// that feeds it. Then "click a crosspoint, open its processing" is a
// link and not a lookup table somebody maintains by hand.
//
// The device publishes what is needed, and EVS expresses it two ways
// across the two products here:
//
//	BRIDGE (video)      /matrix/video/path/info
//	  destinations: [{ children:[{id:<uuid>}…],
//	                   path:"/api/v1/processing/video/channels",
//	                   template:"CH{idx}" }]
//	  crosspoints:  { "CH04": "IP01" }        label -> label
//	  A label resolves by POSITION: CH04 is children[4].
//
//	SHUFFLER (audio)    /matrices/audio/info
//	  destinations: [{ path:"…", group:{uuid,name,slots} }]
//	  crosspoints:  { "<dstFlowUuid>": "<srcFlowUuid>" }   uuid -> uuid
//	  The key IS the identifier; the provider only says where it lives.
//
// Both are the same idea — an axis of things, each living under some
// collection — so both are read into one shape here and resolved by
// one function. A connector that understood only one of them would be
// a connector for one product.

// MatrixInfo is a matrix's two axes, as the device describes them.
type MatrixInfo struct {
	Description  string
	Sources      []MatrixProvider
	Destinations []MatrixProvider
}

// MatrixProvider is one group of things on an axis: where they live,
// and how the crosspoint map names them.
type MatrixProvider struct {
	// Path is the collection the members are resources in, as the
	// device writes it (absolute, including any API prefix).
	Path string
	// Type is the device's own word for what these are ("IP",
	// "Video processing channel"), for display.
	Type string
	// Template is how a member is named in the crosspoint map, with
	// {idx} standing for its position — "CH{idx}". Empty when members
	// are named by UUID instead.
	//
	// A member can itself carry channels, and then the template has a
	// second marker: the BRIDGE's audio matrix is
	// "DB{idx}-{subIdsIdx}", where DB000-05 is channel 5 of the first
	// delay bank. One resource, sixteen crosspoints.
	Template string
	// Children are the members in position order, which is what {idx}
	// indexes into. Empty on a UUID-keyed matrix.
	Children []MatrixMember
	// Group names a UUID-keyed provider's group, when it has one.
	GroupUUID string
	GroupName string
}

// MatrixMember is one thing on an axis: the resource, and how many
// channels of it the matrix routes separately.
type MatrixMember struct {
	// ID is the resource's identifier under the provider's path.
	ID string
	// SubIDs is how many channels this member exposes to the matrix,
	// 0 when it is routed whole.
	SubIDs int
}

// ParseMatrixInfo reads a matrix info body of either shape.
func ParseMatrixInfo(body []byte) (*MatrixInfo, error) {
	var raw struct {
		Description  string           `json:"description"`
		Sources      []providerOnWire `json:"sources"`
		Destinations []providerOnWire `json:"destinations"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("ccm: matrix info: %w", err)
	}
	if len(raw.Sources) == 0 && len(raw.Destinations) == 0 {
		return nil, fmt.Errorf("ccm: matrix info names neither sources nor destinations")
	}
	out := &MatrixInfo{Description: raw.Description}
	for _, p := range raw.Sources {
		out.Sources = append(out.Sources, p.provider())
	}
	for _, p := range raw.Destinations {
		out.Destinations = append(out.Destinations, p.provider())
	}
	return out, nil
}

// providerOnWire is the union of both products' provider shapes.
type providerOnWire struct {
	Path     string `json:"path"`
	Type     string `json:"type"`
	Template string `json:"template"`
	Children []struct {
		ID     string `json:"id"`
		SubIDs int    `json:"subIds"`
	} `json:"children"`
	Group struct {
		UUID string `json:"uuid"`
		Name string `json:"name"`
	} `json:"group"`
}

func (p providerOnWire) provider() MatrixProvider {
	out := MatrixProvider{
		Path: p.Path, Type: p.Type, Template: p.Template,
		GroupUUID: p.Group.UUID, GroupName: p.Group.Name,
	}
	for _, c := range p.Children {
		out.Children = append(out.Children, MatrixMember{ID: c.ID, SubIDs: c.SubIDs})
	}
	return out
}

// Endpoint is one resolved side of a crosspoint: the thing the matrix
// named, and where to go to control it.
type Endpoint struct {
	// Key is what the crosspoint map called it ("CH04", or a UUID).
	Key string
	// ID is the resource's own identifier — the UUID under Path.
	ID string
	// Sub is the channel WITHIN that resource the matrix routes, for
	// an axis whose members carry channels (the audio matrix routes 16
	// per delay bank). -1 when the member is routed whole.
	Sub int
	// Path is the resource itself, ready to GET or PUT.
	Path string
	// Type is the device's word for what it is, when it said.
	Type string
	// Resolved is false when the matrix named something no provider
	// on that axis accounts for. The crosspoint is still reported —
	// an unresolvable name is a fact about the device, not a reason
	// to drop the routing.
	Resolved bool
}

// ResolveSource resolves a key from the source axis.
func (m *MatrixInfo) ResolveSource(key string) Endpoint {
	return resolve(m.Sources, key)
}

// ResolveDestination resolves a key from the destination axis.
func (m *MatrixInfo) ResolveDestination(key string) Endpoint {
	return resolve(m.Destinations, key)
}

// resolve turns one crosspoint key into the resource it names, by
// whichever scheme the axis uses.
func resolve(providers []MatrixProvider, key string) Endpoint {
	out := Endpoint{Key: key}
	if key == "" {
		return out
	}
	for _, p := range providers {
		// Label-keyed: "CH04" against "CH{idx}", or "DB000-05" against
		// "DB{idx}-{subIdsIdx}" — the numbers index the children in
		// position order, and the channel within the one found.
		if p.Template != "" {
			idx, sub, ok := indexesFromTemplate(p.Template, key)
			if !ok || idx < 0 || idx >= len(p.Children) {
				continue
			}
			m := p.Children[idx]
			return Endpoint{Key: key, ID: m.ID, Sub: sub,
				Path: joinPath(p.Path, m.ID), Type: p.Type, Resolved: true}
		}
		// UUID-keyed: the key is the identifier, and the provider says
		// which collection it lives in. With one provider on the axis
		// that is unambiguous; with several, the first that lists it
		// wins, and a provider that lists nothing accepts any key.
		for _, m := range p.Children {
			if m.ID == key {
				return Endpoint{Key: key, ID: m.ID, Sub: -1,
					Path: joinPath(p.Path, m.ID), Type: p.Type, Resolved: true}
			}
		}
	}
	// No provider listed it. On a UUID-keyed matrix with exactly one
	// provider per axis — which is what the shuffler serves — the key
	// still addresses a resource under that provider's path, and
	// saying so is more useful than saying nothing.
	if len(providers) == 1 && providers[0].Template == "" && len(providers[0].Children) == 0 {
		p := providers[0]
		return Endpoint{Key: key, ID: key, Path: joinPath(p.Path, key),
			Type: p.Type, Resolved: true}
	}
	return out
}


// joinPath puts a member under its collection.
func joinPath(collection, id string) string {
	return strings.TrimSuffix(collection, "/") + "/" + id
}

// Crosspoint is one routing entry with both sides resolved.
type Crosspoint struct {
	Destination Endpoint
	Source      Endpoint
}

// ResolveState turns a crosspoint map — destination key to source key,
// in either keying scheme — into resolved pairs.
func (m *MatrixInfo) ResolveState(state []byte) ([]Crosspoint, error) {
	var raw map[string]string
	if err := json.Unmarshal(state, &raw); err != nil {
		return nil, fmt.Errorf("ccm: matrix state: %w", err)
	}
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sortStrings(keys)

	out := make([]Crosspoint, 0, len(keys))
	for _, dst := range keys {
		out = append(out, Crosspoint{
			Destination: m.ResolveDestination(dst),
			Source:      m.ResolveSource(raw[dst]),
		})
	}
	return out, nil
}

// sortStrings keeps the order stable without pulling sort into every
// caller's mental model — a matrix dump that reordered between reads
// would make two captures impossible to compare.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// indexesFromTemplate reads the positions out of a label.
//
// "CH{idx}" and "CH04" give (4, -1). "DB{idx}-{subIdsIdx}" and
// "DB000-05" give (0, 5): the first delay bank, its fifth channel.
//
// The template is walked as literals and markers in order, and each
// marker consumes the digits at that point — so the padding is taken
// as written, a device that pads to two or three keeps working, and a
// label that does not fit is simply not this provider's.
func indexesFromTemplate(template, key string) (idx, sub int, ok bool) {
	idx, sub = -1, -1
	seen := 0
	for template != "" {
		open := strings.Index(template, "{")
		if open < 0 {
			// Trailing literal: it has to be what is left of the key.
			return idx, sub, key == template
		}
		lit := template[:open]
		if !strings.HasPrefix(key, lit) {
			return -1, -1, false
		}
		key = key[len(lit):]
		close := strings.Index(template[open:], "}")
		if close < 0 {
			return -1, -1, false
		}
		template = template[open+close+1:]

		digits := 0
		for digits < len(key) && key[digits] >= '0' && key[digits] <= '9' {
			digits++
		}
		if digits == 0 {
			return -1, -1, false
		}
		n, err := strconv.Atoi(key[:digits])
		if err != nil {
			return -1, -1, false
		}
		key = key[digits:]
		if seen == 0 {
			idx = n
		} else {
			sub = n
		}
		seen++
	}
	return idx, sub, key == ""
}
