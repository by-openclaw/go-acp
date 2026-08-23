package audit

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"dhs/internal/amwa/codec/is04"
)

// The IS-05 control URN, as it appears in a Device's `controls` array.
// The version suffix moves with the IS-05 minor, so matching is done on
// the stem.
const controlIS05Stem = "urn:x-nmos:control:sr-ctrl/"

// controlStems maps a `controls` URN stem to the `/x-nmos` API name it
// promises. A device that advertises the control but does not serve the
// API — or serves the API but advertises no control — is unroutable by
// a spec-following controller, which walks `controls` and nothing else.
//
// Only controls that correspond to a REST API under `/x-nmos` belong
// here. `urn:x-nmos:control:ncp` (IS-12) is a WebSocket at the href and
// has no `/x-nmos/ncp` index to fetch, so cross-checking it against the
// API list reports every conformant IS-12 device as broken — six of
// them on one real capture. `manifest-b` is likewise a direct href, not
// an API.
var controlStems = map[string]string{
	controlIS05Stem:               "connection",
	"urn:x-nmos:control:events/":  "events",
	"urn:x-nmos:control:cm-ctrl/": "channelmapping",
}

// checkCaptureCompleteness reports a harvest the exporter never
// finished.
//
// This has to come before every other check and it has to be loud. An
// interrupted capture has an identity and no resources, which is
// indistinguishable — to every check below — from a device that serves
// nothing. Auditing it silently would turn "we did not look" into "we
// looked and found nothing", which is the one lie a compliance report
// must not tell.
func checkCaptureCompleteness(h *Harvest) []Finding {
	if !h.Partial {
		return nil
	}
	return []Finding{h.find(
		"NMOS-AUDIT-CAPTURE-INCOMPLETE", SevWarn, "",
		fmt.Sprintf("%s was captured without a tree.json — the export did not finish for this device", h.Name()),
		"",
		"nothing below is evidence about this device; re-export it before drawing conclusions")}
}

// checkAPISurface records what the device exposes, and flags the two
// surfaces whose absence changes what an operator can do with it.
func checkAPISurface(h *Harvest) []Finding {
	// A capture that did not finish proves nothing about what the
	// device serves.
	if h.Partial {
		return nil
	}
	var out []Finding

	names := make([]string, 0, len(h.APIs))
	for n := range h.APIs {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, n := range names {
		a := h.APIs[n]
		out = append(out, h.find(
			"NMOS-API-INVENTORY", SevInfo, "",
			fmt.Sprintf("exposes /x-nmos/%s at %s", n, strings.Join(a.Versions, ",")),
			"", ""))
	}

	if h.Role != "node" {
		return out
	}

	// A Node with senders but no Connection API can be discovered and
	// listed, and can never be routed: IS-05 is the only standard write
	// path to a sender's destination.
	senders, _ := h.resources("node", "senders")
	if _, ok := h.APIs["connection"]; !ok && len(senders) > 0 {
		out = append(out, h.find(
			"NMOS-IS05-ABSENT", SevCritical, "",
			fmt.Sprintf("node publishes %d sender(s) but serves no /x-nmos/connection API", len(senders)),
			"IS-05 v1.1 §4 Connection API",
			"the node is read-only to any controller; routing it needs a vendor-specific path"))
	}

	return out
}

// checkControlAdvertisement cross-checks each Device's `controls` array
// against the APIs that actually answered.
func checkControlAdvertisement(h *Harvest) []Finding {
	devices, ver := h.resources("node", "devices")
	if len(devices) == 0 {
		return nil
	}

	var out []Finding
	advertised := map[string]bool{}

	for _, blob := range devices {
		var d is04.Device
		if json.Unmarshal(blob, &d) != nil {
			continue
		}
		for _, c := range d.Controls {
			for stem, api := range controlStems {
				if strings.HasPrefix(c.Type, stem) {
					advertised[api] = true
				}
			}
			if c.Href == "" {
				out = append(out, h.find(
					"NMOS-IS04-CONTROL-HREF", SevError, "device/"+d.ID,
					fmt.Sprintf("controls entry %q has an empty href", c.Type),
					"IS-04 "+ver+" device.json controls[].href",
					"a controller cannot reach the control without an href"))
			}
		}
	}

	for stem, api := range controlStems {
		_, served := h.APIs[api]
		switch {
		case advertised[api] && !served:
			out = append(out, h.find(
				"NMOS-IS04-CONTROL-DANGLING", SevError, "",
				fmt.Sprintf("a device advertises %s but /x-nmos/%s did not answer", stem, api),
				"IS-04 "+ver+" device.json controls[]",
				"controllers follow controls[]; a dangling entry fails at connect time, not at discovery"))
		case !advertised[api] && served && api == "connection":
			out = append(out, h.find(
				"NMOS-IS04-CONTROL-MISSING", SevError, "",
				"serves /x-nmos/connection but no device advertises a sr-ctrl control",
				"IS-04 "+ver+" device.json controls[]",
				"a spec-following controller walks controls[] only — it will never find this IS-05 API"))
		}
	}
	return out
}

// resourceKinds is the IS-04 resource set an audit walks, in graph
// order. Nodes are included because a Registry's catalogue holds them.
var resourceKinds = []string{"nodes", "devices", "sources", "flows", "senders", "receivers"}

// coreProbe is the subset of every IS-04 resource an audit needs to
// check the shared `resource_core` rules, decoded leniently so a vendor
// extension field does not abort the check.
type coreProbe struct {
	ID      string              `json:"id"`
	Version string              `json:"version"`
	Label   string              `json:"label"`
	Tags    map[string][]string `json:"tags"`
}

// checkIS04Core validates the fields every IS-04 resource shares: a
// v1-5 UUID id, and a `<sec>:<nsec>` TAI version. Both are load-bearing
// — the id is the only stable handle a controller has, and the version
// is how a Registry decides which of two copies is newer.
func checkIS04Core(h *Harvest) []Finding {
	var out []Finding
	for _, api := range []string{"node", "query"} {
		if _, ok := h.APIs[api]; !ok {
			continue
		}
		for _, kind := range resourceKinds {
			items, ver := h.collect(api, kind)
			seen := map[string]bool{}
			for _, blob := range items {
				var c coreProbe
				if json.Unmarshal(blob, &c) != nil {
					out = append(out, h.find(
						"NMOS-IS04-UNDECODABLE", SevError, kind,
						"a resource in "+kind+" is not a JSON object",
						"IS-04 "+ver+" resource_core.json", ""))
					continue
				}
				res := strings.TrimSuffix(kind, "s") + "/" + c.ID
				if !is04.IsValidUUID(c.ID) {
					out = append(out, h.find(
						"NMOS-IS04-BAD-ID", SevError, res,
						fmt.Sprintf("id %q does not match the RFC 4122 v1-5 UUID pattern", c.ID),
						"IS-04 "+ver+" resource_core.json id",
						"controllers key every cross-reference on this value"))
				}
				if !is04.IsValidVersion(c.Version) {
					out = append(out, h.find(
						"NMOS-IS04-BAD-VERSION", SevError, res,
						fmt.Sprintf("version %q does not match the `<sec>:<nsec>` TAI form", c.Version),
						"IS-04 "+ver+" resource_core.json version",
						"a registry cannot order updates without a parseable version"))
				}
				if c.ID != "" && seen[c.ID] {
					out = append(out, h.find(
						"NMOS-IS04-DUPLICATE-ID", SevCritical, res,
						"id appears more than once in "+kind,
						"IS-04 "+ver+" resource_core.json id",
						"one of the two resources is unreachable — the id is the only handle"))
				}
				seen[c.ID] = true
			}
		}
	}
	return out
}

// refProbe carries the cross-resource references IS-04 defines. Each is
// a UUID pointing at another resource in the same graph.
type refProbe struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"`
	NodeID   string  `json:"node_id"`
	DeviceID string  `json:"device_id"`
	SourceID *string `json:"source_id"`
	FlowID   *string `json:"flow_id"`
	ParentID *string `json:"parent_id"`
}

// checkIS04Graph walks the reference graph and reports every edge that
// points at a resource the capture does not contain.
//
// A dangling edge is not cosmetic: a controller building the tree
// Sender → Flow → Source → Device → Node drops the sender entirely when
// any link is missing, so the device shows up in the registry and is
// absent from the routing UI.
func checkIS04Graph(h *Harvest) []Finding {
	api := "node"
	if _, ok := h.APIs["query"]; ok {
		api = "query"
	}
	if _, ok := h.APIs[api]; !ok {
		return nil
	}

	// A collection whose walk may have stopped short cannot be used to
	// prove a reference dangles: the target may simply be on a page
	// nobody fetched.
	maybeShort := truncatedKinds(h.Report)

	ids := map[string]map[string]bool{}
	have := map[string]bool{}
	for _, kind := range resourceKinds {
		items, _ := h.collect(api, kind)
		set := map[string]bool{}
		for _, b := range items {
			if id := idOf(b); id != "" {
				set[id] = true
			}
		}
		ids[kind] = set
		have[kind] = h.captured(api, kind)
	}
	// A Node's own API has no /nodes collection — it publishes /self.
	if api == "node" {
		if self, _ := h.object("node", "self"); self != nil {
			if id := idOf(self); id != "" {
				ids["nodes"] = map[string]bool{id: true}
				have["nodes"] = true
			}
		}
	}

	var out []Finding
	edge := func(kind, from, field, to, toKind string) {
		if to == "" || ids[toKind][to] {
			return
		}
		// A capture that never reached a collection cannot prove an
		// edge dangles — absence of the collection is not absence of
		// the target. A collection that WAS captured and came back
		// empty is different: the device has said it has none.
		if !have[toKind] || maybeShort[toKind] {
			return
		}
		out = append(out, h.find(
			"NMOS-IS04-DANGLING-REF", SevError,
			strings.TrimSuffix(kind, "s")+"/"+from,
			fmt.Sprintf("%s %s points at %s %s, which is not in the capture", field, to, strings.TrimSuffix(toKind, "s"), to),
			"IS-04 "+strings.TrimSuffix(kind, "s")+".json "+field,
			"a controller drops the whole branch when a link cannot be resolved"))
	}

	for _, kind := range []string{"devices", "sources", "flows", "senders", "receivers"} {
		items, _ := h.collect(api, kind)
		for _, blob := range items {
			var r refProbe
			if json.Unmarshal(blob, &r) != nil {
				continue
			}
			switch kind {
			case "devices":
				edge(kind, r.ID, "node_id", r.NodeID, "nodes")
			case "sources":
				edge(kind, r.ID, "device_id", r.DeviceID, "devices")
			case "flows":
				edge(kind, r.ID, "device_id", r.DeviceID, "devices")
				if r.SourceID != nil {
					edge(kind, r.ID, "source_id", *r.SourceID, "sources")
				}
			case "senders":
				edge(kind, r.ID, "device_id", r.DeviceID, "devices")
				if r.FlowID != nil {
					edge(kind, r.ID, "flow_id", *r.FlowID, "flows")
				}
			case "receivers":
				edge(kind, r.ID, "device_id", r.DeviceID, "devices")
			}
		}
	}
	return out
}

// senderProbe decodes only what a manifest / transport check needs.
type senderProbe struct {
	ID           string              `json:"id"`
	Label        string              `json:"label"`
	Transport    string              `json:"transport"`
	ManifestHref *string             `json:"manifest_href"`
	Tags         map[string][]string `json:"tags"`
	Subscription struct {
		Active bool `json:"active"`
	} `json:"subscription"`
}

// checkIS04Manifest audits `manifest_href` on RTP senders.
//
// IS-04 requires the field on every RTP sender. It became nullable in
// v1.3 for a sender that is not currently transmitting — so a null on
// v1.3 is legal, a null on v1.0–v1.2 is not, and a null on an ACTIVE
// v1.3 sender is a real gap: the receiver has nowhere to fetch the SDP.
func checkIS04Manifest(h *Harvest) []Finding {
	api := "node"
	if _, ok := h.APIs["query"]; ok {
		api = "query"
	}
	items, ver := h.collect(api, "senders")
	if len(items) == 0 {
		return nil
	}

	nullableFrom13 := versionRank(ver) >= versionRank("v1.3")

	var out []Finding
	for _, blob := range items {
		var s senderProbe
		if json.Unmarshal(blob, &s) != nil {
			continue
		}
		if !strings.HasPrefix(s.Transport, "urn:x-nmos:transport:rtp") {
			continue
		}
		res := "sender/" + s.ID
		switch {
		case s.ManifestHref == nil && !nullableFrom13:
			out = append(out, h.find(
				"NMOS-IS04-MANIFEST-NULL", SevError, res,
				fmt.Sprintf("RTP sender %q has a null manifest_href on %s, where the field is required", s.Label, ver),
				"IS-04 "+ver+" sender.json manifest_href",
				"null became legal only at v1.3, and only for an inactive sender"))
		case s.ManifestHref == nil && s.Subscription.Active:
			out = append(out, h.find(
				"NMOS-IS04-MANIFEST-ACTIVE-NULL", SevError, res,
				fmt.Sprintf("sender %q is subscribed but publishes no manifest_href", s.Label),
				"IS-04 v1.3 sender.json manifest_href",
				"a receiver has no way to fetch the SDP for a stream it is already consuming"))
		case s.ManifestHref != nil && *s.ManifestHref == "":
			out = append(out, h.find(
				"NMOS-IS04-MANIFEST-EMPTY", SevError, res,
				"manifest_href is an empty string",
				"IS-04 "+ver+" sender.json manifest_href",
				"use null, not \"\", when there is no manifest"))
		}
	}
	return out
}

// groupHintTag is the BCP-002-01 tag that tells a controller which
// senders and receivers belong to the same logical device, and which
// role each plays inside it.
const groupHintTag = "urn:x-nmos:tag:grouphint/v1.0"

// checkBCP002GroupHint reports senders and receivers carrying no group
// hint, and hints that do not parse.
//
// Without it a controller sees a flat list of essences and cannot
// present, or break away, a video+audio group — the operator has to
// know by heart which of 176 senders belong together.
func checkBCP002GroupHint(h *Harvest) []Finding {
	api := "node"
	if _, ok := h.APIs["query"]; ok {
		api = "query"
	}

	var out []Finding
	for _, kind := range []string{"senders", "receivers"} {
		items, ver := h.collect(api, kind)
		if len(items) == 0 {
			continue
		}
		missing := 0
		for _, blob := range items {
			var s senderProbe
			if json.Unmarshal(blob, &s) != nil {
				continue
			}
			hints := s.Tags[groupHintTag]
			if len(hints) == 0 {
				missing++
				continue
			}
			for _, hint := range hints {
				// BCP-002-01 §3: `<group-name>:<role-in-group>`.
				if _, _, ok := strings.Cut(hint, ":"); !ok {
					out = append(out, h.find(
						"NMOS-BCP002-HINT-MALFORMED", SevWarn,
						strings.TrimSuffix(kind, "s")+"/"+s.ID,
						fmt.Sprintf("group hint %q is not `<group>:<role>`", hint),
						"BCP-002-01 v1.0 §3 grouphint",
						"controllers split on the first colon; a hint without one groups nothing"))
				}
			}
		}
		if missing > 0 {
			sev := SevWarn
			if missing == len(items) {
				sev = SevError
			}
			out = append(out, h.find(
				"NMOS-BCP002-HINT-ABSENT", sev, kind,
				fmt.Sprintf("%d of %d %s carry no %s tag", missing, len(items), kind, groupHintTag),
				"BCP-002-01 v1.0 §3 grouphint ("+ver+" resources)",
				"a controller cannot group or break away essences without it"))
		}
	}
	return out
}

// receiverProbe decodes the caps a controller filters on.
type receiverProbe struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Caps  struct {
		MediaTypes     []string         `json:"media_types"`
		ConstraintSets []map[string]any `json:"constraint_sets"`
		Version        string           `json:"version"`
	} `json:"caps"`
	Format string `json:"format"`
}

// checkBCP004ReceiverCaps audits what a receiver says it can accept.
//
// A controller uses caps to decide which senders it may offer for a
// given receiver. An empty caps object means "anything", so the
// controller either offers every sender in the plant — including ones
// that will not decode — or refuses to offer any.
func checkBCP004ReceiverCaps(h *Harvest) []Finding {
	api := "node"
	if _, ok := h.APIs["query"]; ok {
		api = "query"
	}
	items, ver := h.collect(api, "receivers")
	if len(items) == 0 {
		return nil
	}

	var out []Finding
	noMedia := 0
	for _, blob := range items {
		var r receiverProbe
		if json.Unmarshal(blob, &r) != nil {
			continue
		}
		if len(r.Caps.MediaTypes) == 0 {
			noMedia++
		}
		// BCP-004-01 §1: caps.version is mandatory once constraint_sets
		// is published — it is how a controller knows its cached
		// constraints are stale.
		if len(r.Caps.ConstraintSets) > 0 && r.Caps.Version == "" {
			out = append(out, h.find(
				"NMOS-BCP004-CAPS-VERSION", SevError, "receiver/"+r.ID,
				fmt.Sprintf("receiver %q publishes constraint_sets with no caps.version", r.Label),
				"BCP-004-01 v1.0 §1 caps.version",
				"controllers cache constraints; without a version they never re-read them"))
		}
		if r.Caps.Version != "" && !is04.IsValidVersion(r.Caps.Version) {
			out = append(out, h.find(
				"NMOS-BCP004-CAPS-VERSION-FORM", SevError, "receiver/"+r.ID,
				fmt.Sprintf("caps.version %q is not the `<sec>:<nsec>` TAI form", r.Caps.Version),
				"BCP-004-01 v1.0 §1 caps.version", ""))
		}
	}
	if noMedia > 0 {
		out = append(out, h.find(
			"NMOS-BCP004-CAPS-EMPTY", SevWarn, "receivers",
			fmt.Sprintf("%d of %d receivers declare no caps.media_types", noMedia, len(items)),
			"BCP-004-01 v1.0 §1 ("+ver+" resources)",
			"a controller cannot filter which senders are connectable"))
	}
	return out
}

// collect returns the resources for a kind, using the union across
// every captured minor for a Registry's query API and the highest
// captured minor for a Node's own API.
//
// The asymmetry is deliberate and load-bearing: IS-04 version isolation
// means a Registry's v1.3 query hides resources registered at v1.1,
// while a Node repeats the same resources at every minor it serves.
func (h *Harvest) collect(api, kind string) ([]json.RawMessage, string) {
	if api == "query" {
		union, counts := h.resourcesEveryVersion(api, kind)
		if len(union) == 0 {
			return nil, highestOf(counts)
		}
		keys := make([]string, 0, len(union))
		for k := range union {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make([]json.RawMessage, 0, len(keys))
		for _, k := range keys {
			out = append(out, union[k])
		}
		return out, highestOf(counts)
	}
	return h.resources(api, kind)
}

// highestOf names the highest version present in a per-version tally.
func highestOf(counts map[string]int) string {
	best, rank := "", -2
	for v := range counts {
		if r := versionRank(v); r > rank {
			best, rank = v, r
		}
	}
	return best
}
