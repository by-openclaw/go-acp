package audit

import (
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"
)

// activeEndpoint is an IS-05 `/active` payload, decoded leniently.
// Every transport parameter is a pointer: IS-05 staged and active
// objects are partial by design, and "absent" carries different meaning
// from "present and zero" on every one of these fields.
type activeEndpoint struct {
	MasterEnable *bool `json:"master_enable"`
	Activation   struct {
		Mode *string `json:"mode"`
	} `json:"activation"`
	TransportParams []transportParam `json:"transport_params"`
	ReceiverID      *string          `json:"receiver_id"`
	SenderID        *string          `json:"sender_id"`
}

type transportParam struct {
	DestinationIP   *string `json:"destination_ip"`
	DestinationPort *any    `json:"destination_port"`
	SourceIP        *string `json:"source_ip"`
	SourcePort      *any    `json:"source_port"`
	RTPEnabled      *bool   `json:"rtp_enabled"`
}

// port renders destination_port, which IS-05 allows to be either an
// integer or the string "auto".
func (p transportParam) port() string {
	if p.DestinationPort == nil {
		return ""
	}
	switch v := (*p.DestinationPort).(type) {
	case float64:
		return fmt.Sprintf("%d", int(v))
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}

// endpoints walks every captured IS-05 `/active` (or `/staged`) payload
// for one side, in id order.
func (h *Harvest) endpoints(side, sub string) []idEndpoint {
	a, ok := h.APIs["connection"]
	if !ok {
		return nil
	}
	var out []idEndpoint
	seen := map[string]bool{}
	for _, v := range sortedVersionsDesc(keysOf(a.Data)) {
		keys := make([]string, 0, len(a.Data[v]))
		for k := range a.Data[v] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			// keys look like `senders/<uuid>/active`
			parts := strings.Split(k, "/")
			if len(parts) != 3 || parts[0] != side || parts[2] != sub {
				continue
			}
			if seen[parts[1]] {
				continue
			}
			blob := a.Data[v][k]
			if len(blob) == 0 || isJSONNull(blob) {
				continue
			}
			var e activeEndpoint
			if json.Unmarshal(blob, &e) != nil {
				continue
			}
			seen[parts[1]] = true
			out = append(out, idEndpoint{ID: parts[1], Ver: v, E: e})
		}
	}
	return out
}

type idEndpoint struct {
	ID  string
	Ver string
	E   activeEndpoint
}

// checkIS05Active audits what each sender and receiver is actually
// doing, as opposed to what it is configured to be able to do.
func checkIS05Active(h *Harvest) []Finding {
	var out []Finding

	// Per-leg faults are collected rather than emitted directly. On a
	// 176-sender device the same condition on every sender is one fact,
	// not 176 findings — and whether it holds for ALL of them or only a
	// few is the difference between "this device is idle" and "these
	// three senders are broken".
	noDest := newGroup()
	legOff := newGroup()
	allOff := newGroup()
	portZero := newGroup()
	senders := 0

	for _, ep := range h.endpoints("senders", "active") {
		senders++
		res := "sender/" + ep.ID
		enabled := ep.E.MasterEnable != nil && *ep.E.MasterEnable

		if len(ep.E.TransportParams) == 0 {
			out = append(out, h.find(
				"NMOS-IS05-NO-PARAMS", SevError, res,
				"active endpoint carries an empty transport_params array",
				"IS-05 "+ep.Ver+" §4.2 sender-response-schema",
				"transport_params is required and must hold one entry per leg"))
			continue
		}

		live := 0
		for i, p := range ep.E.TransportParams {
			legRes := fmt.Sprintf("%s#leg%d", res, i)
			rtpOn := p.RTPEnabled == nil || *p.RTPEnabled
			if rtpOn {
				live++
			}
			if !enabled {
				continue
			}
			dst := ""
			if p.DestinationIP != nil {
				dst = *p.DestinationIP
			}
			// Three distinct states, and they mean different things:
			// the leg is switched off; the destination is deliberately
			// unset; the destination is set to something that is not an
			// address at all. Only the last is a malformed resource.
			unset := dst == "" || dst == "auto" || (net.ParseIP(dst) != nil && !isRoutableDest(dst))
			switch {
			case !rtpOn:
				legOff.add(legRes, ep.Ver)
			case unset:
				noDest.add(legRes, ep.Ver)
			case net.ParseIP(dst) == nil:
				out = append(out, h.find(
					"NMOS-IS05-BAD-DESTINATION", SevError, legRes,
					fmt.Sprintf("destination_ip %q is not an IP address", dst),
					"IS-05 "+ep.Ver+" §4.2 transport_params[].destination_ip", ""))
			}
			if enabled && p.port() == "" {
				out = append(out, h.find(
					"NMOS-IS05-NO-PORT", SevError, legRes,
					"sender is master-enabled with no destination_port",
					"IS-05 "+ep.Ver+" §4.2 transport_params[].destination_port", ""))
			}
			if p.port() == "0" {
				portZero.add(legRes, ep.Ver)
			}
		}

		if enabled && live == 0 {
			allOff.add(res, ep.Ver)
		}
	}

	if n := allOff.senders(); n > 0 {
		out = append(out, h.find(
			"NMOS-IS05-ALL-LEGS-DISABLED", SevCritical, "senders",
			fmt.Sprintf("%d of %d sender(s) are master-enabled with every leg at rtp_enabled=false: %s",
				n, senders, allOff.examples()),
			"IS-05 "+allOff.ver+" §4.2 transport_params[].rtp_enabled",
			"these senders report themselves as on while emitting nothing"))
	}
	if n := portZero.senders(); n > 0 {
		out = append(out, h.find(
			"NMOS-IS05-PORT-ZERO", SevError, "senders",
			fmt.Sprintf("%d of %d sender(s) have a leg with destination_port 0: %s",
				n, senders, portZero.examples()),
			"IS-05 "+portZero.ver+" §4.2 transport_params[].destination_port",
			"port 0 is not a routable RTP destination"))
	}

	// A device where EVERY sender is master-enabled with no destination
	// is a device sitting idle — many implementations leave the flag on
	// and simply have nothing staged. A device where a handful are is a
	// device with a handful of senders that believe they are on air and
	// are emitting nowhere. Same observation, opposite meaning, and the
	// count is what separates them.
	if n := noDest.senders(); n > 0 {
		sev, why := SevCritical, "these senders report themselves on air and emit nowhere"
		if n >= senders {
			sev = SevInfo
			why = "every sender on this device is in this state, so the device is idle rather than misconfigured"
		}
		out = append(out, h.find(
			"NMOS-IS05-NO-DESTINATION", sev, "senders",
			fmt.Sprintf("%d of %d sender(s) are master-enabled with an unset destination_ip: %s",
				n, senders, noDest.examples()),
			"IS-05 "+noDest.ver+" §4.2 transport_params[].destination_ip", why))
	}
	if n := legOff.senders(); n > 0 {
		out = append(out, h.find(
			"NMOS-IS05-LEG-DISABLED", SevWarn, "senders",
			fmt.Sprintf("%d of %d sender(s) are master-enabled with a leg at rtp_enabled=false: %s",
				n, senders, legOff.examples()),
			"IS-05 "+legOff.ver+" §4.2 transport_params[].rtp_enabled",
			"on a 2022-7 pair this leaves the stream unprotected"))
	}

	rawSDP := newGroup()
	receivers := 0
	for _, ep := range h.endpoints("receivers", "active") {
		receivers++
		enabled := ep.E.MasterEnable != nil && *ep.E.MasterEnable
		if enabled && (ep.E.SenderID == nil || *ep.E.SenderID == "") {
			// Legal — a receiver may be enabled against a raw SDP with
			// no NMOS sender behind it — but it is the shape that hides
			// a half-completed route, so it is worth surfacing.
			rawSDP.add("receiver/"+ep.ID, ep.Ver)
		}
	}
	if n := rawSDP.senders(); n > 0 {
		out = append(out, h.find(
			"NMOS-IS05-RX-NO-SENDER", SevInfo, "receivers",
			fmt.Sprintf("%d of %d receiver(s) are master-enabled with a null sender_id (raw-SDP subscription): %s",
				n, receivers, rawSDP.examples()),
			"IS-05 "+rawSDP.ver+" §4.2 receiver-response-schema sender_id",
			"legal, but the registry cannot show these routes in the senders' subscriptions"))
	}
	return out
}

// checkIS05TransportParams audits leg structure — the SMPTE 2022-7
// redundancy the transport parameters either do or do not describe.
func checkIS05TransportParams(h *Harvest) []Finding {
	var out []Finding

	// Which senders did IS-04 say are bound to two interfaces? A sender
	// bound to two NICs and staged with one leg is not redundant, no
	// matter what the label claims.
	bindings := map[string]int{}
	api := "node"
	if _, ok := h.APIs["query"]; ok {
		api = "query"
	}
	if items, _ := h.collect(api, "senders"); len(items) > 0 {
		for _, blob := range items {
			var s struct {
				ID                string   `json:"id"`
				InterfaceBindings []string `json:"interface_bindings"`
			}
			if json.Unmarshal(blob, &s) == nil && s.ID != "" {
				bindings[s.ID] = len(s.InterfaceBindings)
			}
		}
	}

	for _, ep := range h.endpoints("senders", "active") {
		res := "sender/" + ep.ID
		legs := len(ep.E.TransportParams)

		if n, ok := bindings[ep.ID]; ok && n >= 2 && legs == 1 {
			out = append(out, h.find(
				"NMOS-2022-7-SINGLE-LEG", SevWarn, res,
				fmt.Sprintf("sender is bound to %d interfaces but stages only 1 transport_params leg", n),
				"IS-05 v1.1 §4.2 / SMPTE ST 2022-7",
				"the second NIC carries nothing — this stream has no seamless protection"))
		}

		if legs < 2 {
			continue
		}
		var nets []string
		for _, p := range ep.E.TransportParams {
			if p.DestinationIP == nil {
				continue
			}
			// An unset destination is not a subnet. Both legs of an
			// idle sender read as 0.0.0.0, and calling that "the same
			// /24" turns every idle sender in a plant into a redundancy
			// finding — 3048 of them on one real 44-node capture.
			if !isRoutableDest(*p.DestinationIP) {
				continue
			}
			ip := net.ParseIP(*p.DestinationIP)
			if ip == nil || ip.To4() == nil {
				continue
			}
			nets = append(nets, ip.Mask(net.CIDRMask(24, 32)).String())
		}
		if len(nets) >= 2 && allSame(nets) {
			out = append(out, h.find(
				"NMOS-2022-7-SAME-SUBNET", SevWarn, res,
				fmt.Sprintf("both 2022-7 legs target the same /24 (%s)", nets[0]),
				"SMPTE ST 2022-7 §5 / IS-05 v1.1 §4.2",
				"redundancy assumes disjoint paths; one switch failure takes both legs"))
		}
	}
	return out
}

// group accumulates one condition across many legs so it can be
// reported once, with a count and enough examples to go and look.
type group struct {
	legs []string
	ids  map[string]bool
	ver  string
}

func newGroup() *group { return &group{ids: map[string]bool{}} }

func (g *group) add(legResource, ver string) {
	g.legs = append(g.legs, legResource)
	// One sender with two bad legs is one bad sender, not two.
	g.ids[strings.SplitN(legResource, "#", 2)[0]] = true
	if g.ver == "" {
		g.ver = ver
	}
}

func (g *group) senders() int { return len(g.ids) }

// examples names up to three affected senders, so the finding is
// actionable without dumping hundreds of UUIDs into the report.
func (g *group) examples() string {
	ids := make([]string, 0, len(g.ids))
	for id := range g.ids {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) <= 3 {
		return strings.Join(ids, ", ")
	}
	return strings.Join(ids[:3], ", ") + fmt.Sprintf(" (+%d more)", len(ids)-3)
}

// isRoutableDest reports whether a destination_ip names somewhere a
// packet can actually go.
//
// IS-05 uses the unspecified address and the literal "auto" for a
// destination that has not been set. Neither is a place; treating them
// as one turns every idle sender into a finding.
func isRoutableDest(s string) bool {
	switch s {
	case "", "auto", "0.0.0.0", "::":
		return false
	}
	ip := net.ParseIP(s)
	return ip != nil && !ip.IsUnspecified()
}

func allSame(ss []string) bool {
	for _, s := range ss[1:] {
		if s != ss[0] {
			return false
		}
	}
	return true
}
