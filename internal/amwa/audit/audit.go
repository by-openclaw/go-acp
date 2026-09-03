package audit

import (
	"sort"
)

// Options tunes what an audit reports. The zero value reports
// everything, which is the right default for a compliance export.
type Options struct {
	// MinSeverity drops findings below the given rank. Inventory lines
	// are SevInfo, so `--min-severity warn` gives the deviations alone.
	MinSeverity Severity
}

// Inventory is what one captured device actually exposes — the answer
// to "what does this thing support", read from the capture rather than
// from a datasheet.
type Inventory struct {
	Target string              `json:"target"`
	Label  string              `json:"label,omitempty"`
	ID     string              `json:"id,omitempty"`
	Role   string              `json:"role"`
	APIs   map[string][]string `json:"apis"`

	Nodes     int `json:"nodes,omitempty"`
	Devices   int `json:"devices,omitempty"`
	Sources   int `json:"sources,omitempty"`
	Flows     int `json:"flows,omitempty"`
	Senders   int `json:"senders,omitempty"`
	Receivers int `json:"receivers,omitempty"`
	SDPFiles  int `json:"sdp_files,omitempty"`

	// HintedSenders / HintedReceivers / Groups measure BCP-002-01
	// grouping on this device: how many resources carry a group hint,
	// and how many distinct groups they name. This is the answer to
	// "which nodes actually express which essences belong together".
	HintedSenders   int `json:"hinted_senders,omitempty"`
	HintedReceivers int `json:"hinted_receivers,omitempty"`
	Groups          int `json:"groups,omitempty"`
}

// Result is a complete audit: what was found, and what is wrong with it.
type Result struct {
	Inventory []Inventory `json:"inventory"`
	// Groups is the plant pivoted by BCP-002-01 group hint: what a
	// controller would be able to offer as one signal.
	Groups   []GroupRow `json:"groups,omitempty"`
	Findings []Finding  `json:"findings"`
	// Counts is the per-severity tally of Findings, after filtering.
	Counts map[string]int `json:"counts"`
}

// checkFn is one per-device check. Splitting the checks this way keeps
// each one testable in isolation against a single crafted harvest.
type checkFn func(h *Harvest) []Finding

// deviceChecks is the ordered check set applied to every captured
// device, including followed nodes.
var deviceChecks = []checkFn{
	checkCaptureCompleteness,
	checkAPISurface,
	checkControlAdvertisement,
	checkIS04Core,
	checkIS04Graph,
	checkIS04Manifest,
	checkBCP002GroupHint,
	checkBCP004ReceiverCaps,
	checkIS05Active,
	checkIS05TransportParams,
	checkTransportReport,
	checkQueryVersionIsolation,
	checkSDPConformance,
}

// Run audits every harvest and returns the merged result.
//
// Plant-wide checks — multicast collisions, clock spread — run across
// every device in every harvest at once, because a collision is by
// definition invisible from inside either device.
func Run(harvests []*Harvest, opts Options) Result {
	var all []*Harvest
	for _, h := range harvests {
		all = append(all, h.All()...)
	}

	res := Result{Counts: map[string]int{}}
	for _, h := range all {
		res.Inventory = append(res.Inventory, inventoryOf(h))
		for _, c := range deviceChecks {
			res.Findings = append(res.Findings, c(h)...)
		}
	}
	res.Findings = append(res.Findings, checkPlantMulticast(all)...)
	res.Findings = append(res.Findings, checkPlantRegistration(harvests)...)

	groups, groupFindings := checkPlantGroups(all)
	res.Groups = groups
	res.Findings = append(res.Findings, groupFindings...)

	kept := res.Findings[:0]
	for _, f := range res.Findings {
		if f.Severity >= opts.MinSeverity {
			kept = append(kept, f)
		}
	}
	res.Findings = kept
	sortFindings(res.Findings)

	for _, f := range res.Findings {
		res.Counts[f.Severity.String()]++
	}
	sort.SliceStable(res.Inventory, func(i, j int) bool {
		return res.Inventory[i].Target < res.Inventory[j].Target
	})
	return res
}

// Worst returns the highest severity present, and whether any finding
// was reported at all. A CLI uses it to pick an exit code.
func (r Result) Worst() (Severity, bool) {
	worst, any := SevInfo, false
	for _, f := range r.Findings {
		if !any || f.Severity > worst {
			worst, any = f.Severity, true
		}
	}
	return worst, any
}

// inventoryOf records what the device exposed, counting resources at
// the version they were captured from.
func inventoryOf(h *Harvest) Inventory {
	inv := Inventory{
		Target: h.Target,
		Label:  h.Label,
		ID:     h.ID,
		Role:   h.Role,
		APIs:   map[string][]string{},
	}
	for name, a := range h.APIs {
		vs := append([]string(nil), a.Versions...)
		sort.SliceStable(vs, func(i, j int) bool { return versionRank(vs[i]) < versionRank(vs[j]) })
		inv.APIs[name] = vs
	}
	inv.SDPFiles = len(h.SDP)
	inv.HintedSenders, inv.HintedReceivers, inv.Groups = hintCounts(h)

	// A Registry's catalogue lives under `query`; a Node's own
	// resources under `node`. Counting both keeps one field set
	// meaningful for either role.
	for _, api := range []string{"node", "query"} {
		if _, ok := h.APIs[api]; !ok {
			continue
		}
		if api == "query" {
			union, _ := h.resourcesEveryVersion(api, "nodes")
			inv.Nodes = len(union)
		}
		for res, target := range map[string]*int{
			"devices":   &inv.Devices,
			"sources":   &inv.Sources,
			"flows":     &inv.Flows,
			"senders":   &inv.Senders,
			"receivers": &inv.Receivers,
		} {
			if api == "query" {
				union, _ := h.resourcesEveryVersion(api, res)
				*target = len(union)
				continue
			}
			items, _ := h.resources(api, res)
			*target = len(items)
		}
	}
	return inv
}

// find builds a Finding already stamped with the device it came from,
// so no check has to remember to fill in Target and Device.
func (h *Harvest) find(code string, sev Severity, resource, detail, spec, hint string) Finding {
	return Finding{
		Code:     code,
		Severity: sev,
		Target:   h.Target,
		Device:   h.Label,
		Resource: resource,
		Detail:   detail,
		Spec:     spec,
		Hint:     hint,
	}
}
