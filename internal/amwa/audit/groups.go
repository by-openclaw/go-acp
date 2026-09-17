package audit

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// GroupRow is one BCP-002-01 group as it appears across the plant.
//
// NMOS has no level concept. What a router calls a level, NMOS models as
// a separate Sender/Receiver pair per essence — 2110-20 video, 2110-30
// audio per channel group, 2110-40 ANC — so breakaway is the default and
// routing one level is native. The hard part is the inverse: knowing
// which essences are one signal. A group hint is the only thing in NMOS
// that says so, and this row is what it adds up to.
type GroupRow struct {
	// Name is the group-hint group name, the part before the colon.
	Name string `json:"name"`
	// Roles are the levels present in the group, in the order a router
	// would list them where they are recognisable.
	Roles []string `json:"roles"`
	// Senders and Receivers count the resources carrying this group.
	Senders   int `json:"senders"`
	Receivers int `json:"receivers"`
	// Devices are the targets contributing to the group. More than one
	// means no controller can bind the group from NMOS alone.
	Devices []string `json:"devices"`
}

// groupAccumulator collects hints across every captured device.
type groupAccumulator struct {
	roles     map[string]map[string]bool
	senders   map[string]int
	receivers map[string]int
	devices   map[string]map[string]bool
}

func newGroupAccumulator() *groupAccumulator {
	return &groupAccumulator{
		roles:     map[string]map[string]bool{},
		senders:   map[string]int{},
		receivers: map[string]int{},
		devices:   map[string]map[string]bool{},
	}
}

// add records one hint. BCP-002-01 §3 spells a hint `<group>:<role>`;
// a role may itself contain spaces ("audio 1"), and a group name may
// contain colons, so the split is on the LAST colon — splitting on the
// first would make "RACK:1:video" a group called "RACK".
func (g *groupAccumulator) add(hint, target, kind string) {
	name, role, ok := cutLast(hint, ":")
	if !ok {
		return
	}
	name, role = strings.TrimSpace(name), strings.TrimSpace(role)
	if name == "" {
		return
	}
	if g.roles[name] == nil {
		g.roles[name] = map[string]bool{}
		g.devices[name] = map[string]bool{}
	}
	if role != "" {
		g.roles[name][role] = true
	}
	g.devices[name][target] = true
	if kind == "senders" {
		g.senders[name]++
	} else {
		g.receivers[name]++
	}
}

// cutLast splits on the last occurrence of sep.
func cutLast(s, sep string) (before, after string, found bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+len(sep):], true
}

// rows renders the accumulator, group name order.
func (g *groupAccumulator) rows() []GroupRow {
	names := make([]string, 0, len(g.roles))
	for n := range g.roles {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]GroupRow, 0, len(names))
	for _, n := range names {
		out = append(out, GroupRow{
			Name:      n,
			Roles:     sortedRoles(g.roles[n]),
			Senders:   g.senders[n],
			Receivers: g.receivers[n],
			Devices:   sortedSet(g.devices[n]),
		})
	}
	return out
}

// roleOrder puts the levels an operator thinks in first, so a group
// reads video → audio → ANC rather than alphabetically. Anything
// unrecognised keeps its own order after them.
var roleOrder = []string{"video", "audio", "anc", "data", "mux"}

func roleRank(role string) int {
	low := strings.ToLower(role)
	for i, known := range roleOrder {
		if strings.HasPrefix(low, known) {
			return i
		}
	}
	return len(roleOrder)
}

func sortedRoles(set map[string]bool) []string {
	out := sortedSet(set)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := roleRank(out[i]), roleRank(out[j])
		if ri != rj {
			return ri < rj
		}
		return out[i] < out[j]
	})
	return out
}

func sortedSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// hintProbe decodes only the tags a grouping pass needs.
type hintProbe struct {
	ID   string              `json:"id"`
	Tags map[string][]string `json:"tags"`
}

// hintCounts reports how many senders and receivers on one device carry
// a group hint, and how many distinct groups they name. This is the
// "does this node publish BCP-002 at all" measurement.
func hintCounts(h *Harvest) (senders, receivers, groups int) {
	api := "node"
	if _, ok := h.APIs["query"]; ok {
		api = "query"
	}
	names := map[string]bool{}
	for _, kind := range []string{"senders", "receivers"} {
		items, _ := h.collect(api, kind)
		for _, blob := range items {
			var p hintProbe
			if json.Unmarshal(blob, &p) != nil {
				continue
			}
			hints := p.Tags[groupHintTag]
			if len(hints) == 0 {
				continue
			}
			if kind == "senders" {
				senders++
			} else {
				receivers++
			}
			for _, hint := range hints {
				if name, _, ok := cutLast(hint, ":"); ok && strings.TrimSpace(name) != "" {
					names[strings.TrimSpace(name)] = true
				}
			}
		}
	}
	return senders, receivers, len(names)
}

// checkPlantGroups pivots every captured device by group hint and
// reports the two shapes that leave a group unusable.
//
// This runs plant-wide because the second shape only exists there: a
// group whose essences live on different devices cannot be seen as one
// from inside either of them.
func checkPlantGroups(all []*Harvest) ([]GroupRow, []Finding) {
	acc := newGroupAccumulator()
	// firstSeen keeps a device per group so a finding can name where to
	// go and look.
	firstSeen := map[string]*Harvest{}

	// A registry's catalogue is a VIEW of the nodes, not a device of its
	// own: the same sender appears in the registry's query API and in
	// the node's own API. Counting both makes every grouped resource
	// look like it spans two devices and doubles its count — on one real
	// plant that turned 4-receiver services into 7-receiver ones and
	// invented 170 cross-device groups. Nodes are walked first and their
	// resource ids remembered, so a registry only ever contributes
	// resources no node capture covered.
	ordered := make([]*Harvest, 0, len(all))
	for _, h := range all {
		if h.Role != "registry" {
			ordered = append(ordered, h)
		}
	}
	for _, h := range all {
		if h.Role == "registry" {
			ordered = append(ordered, h)
		}
	}

	seenResource := map[string]bool{}
	for _, h := range ordered {
		api := "node"
		if _, ok := h.APIs["query"]; ok {
			api = "query"
		}
		for _, kind := range []string{"senders", "receivers"} {
			items, _ := h.collect(api, kind)
			for _, blob := range items {
				var p hintProbe
				if json.Unmarshal(blob, &p) != nil {
					continue
				}
				if p.ID != "" {
					if seenResource[p.ID] {
						continue
					}
					seenResource[p.ID] = true
				}
				for _, hint := range p.Tags[groupHintTag] {
					acc.add(hint, h.Name(), kind)
					if name, _, ok := cutLast(hint, ":"); ok {
						name = strings.TrimSpace(name)
						if _, seen := firstSeen[name]; !seen && name != "" {
							firstSeen[name] = h
						}
					}
				}
			}
		}
	}

	rows := acc.rows()
	var out []Finding

	// Collapse the single-role case into one finding per device. A
	// Neuron publishes one group per essence, so per-group findings
	// would be 176 lines saying the same thing.
	singleByDevice := map[string][]string{}

	for _, r := range rows {
		if len(r.Roles) <= 1 && len(r.Devices) == 1 {
			singleByDevice[r.Devices[0]] = append(singleByDevice[r.Devices[0]], r.Name)
		}
		if len(r.Devices) > 1 {
			h := firstSeen[r.Name]
			if h == nil {
				continue
			}
			// Two very different situations produce this, and a capture
			// cannot tell them apart: one signal genuinely spanning
			// devices, or a group name that is simply not unique across
			// the plant. Both need saying, because the second is worse
			// — a controller merging by name fuses unrelated devices
			// into one bogus signal.
			out = append(out, h.find(
				"NMOS-BCP002-GROUP-CROSS-DEVICE", SevWarn, "group/"+r.Name,
				fmt.Sprintf("group %q appears on %d devices: %s", r.Name, len(r.Devices), strings.Join(r.Devices, ", ")),
				"BCP-002-01 v1.0 §3 grouphint",
				"either one signal spans them — which IS-04 cannot express, so a controller needs its own signal database — or the name is not unique across the plant, and a controller grouping by name would fuse unrelated devices"))
		}
	}

	for _, dev := range sortedSet(toSet(keysOfStrings(singleByDevice))) {
		names := singleByDevice[dev]
		sort.Strings(names)
		h := firstSeen[names[0]]
		if h == nil {
			continue
		}
		out = append(out, h.find(
			"NMOS-BCP002-GROUP-SINGLE-ROLE", SevWarn, "groups",
			fmt.Sprintf("%d group(s) on %s hold exactly one role, so they express no association between essences: %s",
				len(names), dev, examplesOf(names)),
			"BCP-002-01 v1.0 §3 grouphint",
			"a controller cannot offer \"route all levels\" or a breakaway when each essence is its own group"))
	}

	return rows, out
}

func keysOfStrings(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func toSet(ss []string) map[string]bool {
	out := make(map[string]bool, len(ss))
	for _, s := range ss {
		out[s] = true
	}
	return out
}

// examplesOf names up to three groups, so a finding is actionable
// without printing 176 UUID-shaped names.
func examplesOf(names []string) string {
	if len(names) <= 3 {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:3], ", ") + fmt.Sprintf(" (+%d more)", len(names)-3)
}
