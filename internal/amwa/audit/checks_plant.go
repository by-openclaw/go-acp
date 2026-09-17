package audit

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

// emitter is one sender leg observed on the wire, with enough context
// to name it in a collision report.
type emitter struct {
	target   string
	device   string
	senderID string
	leg      int
	group    string
	port     string
}

func (e emitter) String() string {
	name := e.device
	if name == "" {
		name = e.target
	}
	return fmt.Sprintf("%s sender/%s leg%d", name, e.senderID, e.leg)
}

// checkPlantMulticast finds two senders emitting to the same multicast
// group and port.
//
// This is invisible from inside either device — each one is correctly
// configured and reports itself healthy. What the operator sees is a
// receiver rendering two interleaved streams, or a stream that breaks
// up whenever the other source is enabled. It only shows up when the
// whole plant is looked at at once, which is exactly what a registry
// export gives you.
func checkPlantMulticast(all []*Harvest) []Finding {
	byGroup := map[string][]emitter{}

	for _, h := range all {
		for _, ep := range h.endpoints("senders", "active") {
			if ep.E.MasterEnable != nil && !*ep.E.MasterEnable {
				continue
			}
			for i, p := range ep.E.TransportParams {
				if p.RTPEnabled != nil && !*p.RTPEnabled {
					continue
				}
				if p.DestinationIP == nil {
					continue
				}
				ip := net.ParseIP(*p.DestinationIP)
				if ip == nil || !ip.IsMulticast() {
					continue
				}
				port := p.port()
				if port == "" || port == "auto" {
					continue
				}
				key := ip.String() + ":" + port
				byGroup[key] = append(byGroup[key], emitter{
					target: h.Target, device: h.Label, senderID: ep.ID,
					leg: i, group: ip.String(), port: port,
				})
			}
		}
	}

	keys := make([]string, 0, len(byGroup))
	for k := range byGroup {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var out []Finding
	for _, k := range keys {
		es := byGroup[k]
		if len(es) < 2 {
			continue
		}
		// The same sender captured twice — once from the registry's
		// catalogue and once from following the node — is one emitter,
		// not a collision.
		uniq := map[string]bool{}
		for _, e := range es {
			uniq[e.senderID+"#"+fmt.Sprint(e.leg)] = true
		}
		if len(uniq) < 2 {
			continue
		}
		names := make([]string, 0, len(es))
		for _, e := range es {
			names = append(names, e.String())
		}
		sort.Strings(names)
		names = dedupe(names)

		out = append(out, Finding{
			Code:     "NMOS-PLANT-MCAST-COLLISION",
			Severity: SevCritical,
			Target:   es[0].target,
			Device:   es[0].device,
			Resource: k,
			Detail:   fmt.Sprintf("%d senders emit to %s: %s", len(names), k, strings.Join(names, ", ")),
			Spec:     "SMPTE ST 2110-10 / IS-05 v1.1 §4.2 destination_ip",
			Hint:     "receivers joining this group get interleaved essence from both sources",
		})
	}
	return out
}

// checkPlantRegistration compares what a Registry lists against the
// Nodes that were actually captured.
//
// The two directions fail differently. A node in the catalogue that
// nobody could reach is a stale registration — the registry is telling
// controllers about a device that is gone. A node captured directly but
// absent from the catalogue never registered, so no controller will
// ever discover it.
func checkPlantRegistration(roots []*Harvest) []Finding {
	var registries, nodes []*Harvest
	for _, r := range roots {
		for _, h := range r.All() {
			// A registry whose capture did not finish has an empty
			// catalogue for the same reason it has empty everything:
			// nobody asked it. Comparing nodes against that catalogue
			// would report every device in the plant as unregistered.
			if h.Partial {
				continue
			}
			switch h.Role {
			case "registry":
				registries = append(registries, h)
			case "node":
				nodes = append(nodes, h)
			}
		}
	}
	if len(registries) == 0 {
		return nil
	}

	listed := map[string]bool{}
	for _, reg := range registries {
		union, _ := reg.resourcesEveryVersion("query", "nodes")
		for id := range union {
			listed[id] = true
		}
	}

	var out []Finding
	for _, n := range nodes {
		id := n.ID
		if id == "" {
			if self, _ := n.object("node", "self"); self != nil {
				id = idOf(self)
			}
		}
		if id == "" || listed[id] {
			continue
		}
		out = append(out, n.find(
			"NMOS-NODE-UNREGISTERED", SevError, "node/"+id,
			fmt.Sprintf("node %q answers its own API but no captured registry lists it", n.Name()),
			"IS-04 v1.3 §4.1 Registration API",
			"controllers discover through the registry; an unregistered node is invisible to all of them"))
	}

	// A registry that lists nothing is either empty or not serving the
	// catalogue it thinks it is.
	for _, reg := range registries {
		union, _ := reg.resourcesEveryVersion("query", "nodes")
		if len(union) == 0 {
			out = append(out, reg.find(
				"NMOS-REGISTRY-EMPTY", SevWarn, "query/nodes",
				"registry serves the Query API but lists zero nodes",
				"IS-04 v1.3 §7 Query API",
				"either nothing has registered, or heartbeats are timing out and the garbage collector is emptying it"))
		}
	}
	return out
}

// dedupe removes adjacent duplicates from a sorted slice.
func dedupe(ss []string) []string {
	out := ss[:0]
	var prev string
	for i, s := range ss {
		if i > 0 && s == prev {
			continue
		}
		out = append(out, s)
		prev = s
	}
	return out
}
