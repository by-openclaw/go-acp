package audit

import (
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"

	"dhs/internal/amwa/codec/sdp"
)

// Network-plane validation (#852): every finding here is derivable
// from an export we already take — no device, no network — and every
// one is a failure that reads as "the route came up and there is no
// picture". Spec-derivable checks always run; site-policy checks
// (multicast class, expected GM, private/public) SKIP without a
// --policy file rather than guess.

// Reserved multicast blocks a media stream must avoid.
var (
	mcastLinkLocal = mustCIDR("224.0.0.0/24")   // routers do not forward — dies at hop 1
	mcastDocRange  = mustCIDR("233.252.0.0/24") // the documentation range (copy-paste config)
	mcastSSMRange  = mustCIDR("232.0.0.0/8")    // source-specific: needs a source-filter
	mcastAdminScp  = mustCIDR("239.0.0.0/8")    // admin-scoped: fine, but bound
)

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic("audit: bad reserved CIDR " + s)
	}
	return n
}

// checkNetworkPlane audits the addressing, MAC, and PTP facts of one
// device's senders. pol may be nil (spec checks still run).
func checkNetworkPlane(h *Harvest, pol *Policy) []Finding {
	var out []Finding
	out = append(out, h.checkMulticastRanges(pol)...)
	out = append(out, h.checkUnicastSourceIPs(pol)...)
	out = append(out, h.checkInterfaceMACs()...)
	out = append(out, h.checkSenderPTP(pol)...)
	return out
}

// checkMulticastRanges classifies each destination against the
// reserved blocks and, with a policy, the site bandwidth-class map.
func (h *Harvest) checkMulticastRanges(pol *Policy) []Finding {
	var out []Finding
	policyChecked := false
	for _, ep := range h.endpoints("senders", "active") {
		res := "sender/" + ep.ID
		for _, p := range ep.E.TransportParams {
			if p.DestinationIP == nil || !isRoutableDest(*p.DestinationIP) {
				continue
			}
			ip := net.ParseIP(*p.DestinationIP)
			if ip == nil || !ip.IsMulticast() {
				continue
			}
			switch {
			case mcastLinkLocal.Contains(ip):
				out = append(out, h.find("NMOS-NET-MCAST-LINKLOCAL", SevError, res,
					fmt.Sprintf("destination %s is in the link-local control block 224.0.0.0/24 — routers do not forward it", ip),
					"RFC 5771 §4", "the stream dies at the first hop; pick an admin-scoped 239/8 group"))
			case mcastDocRange.Contains(ip):
				out = append(out, h.find("NMOS-NET-MCAST-DOCRANGE", SevWarn, res,
					fmt.Sprintf("destination %s is in the documentation range 233.252.0.0/24 — a shipped copy-paste config", ip),
					"RFC 5771 §4", "assign a real group from the plant's allocation"))
			case mcastSSMRange.Contains(ip):
				if !hasSourceFilter(h, ep.ID) {
					out = append(out, h.find("NMOS-NET-SSM-NO-FILTER", SevWarn, res,
						fmt.Sprintf("destination %s is SSM (232/8) but the SDP carries no a=source-filter — the receiver cannot join", ip),
						"RFC 4607 / RFC 4570", "publish a=source-filter, or use an ASM group"))
				}
			}
			if mc := pol.classify(ip); mc != nil {
				policyChecked = true
				out = append(out, h.checkFlowFitsClass(ep.ID, res, mc)...)
			} else if pol != nil && len(pol.MulticastClasses) > 0 && mcastAdminScp.Contains(ip) {
				out = append(out, h.find("NMOS-NET-MCAST-UNCLASSED", SevWarn, res,
					fmt.Sprintf("destination %s is admin-scoped but falls in no policy multicast_classes range", ip),
					"site policy", "add the range to the policy or move the sender into a classed range"))
				policyChecked = true
			}
		}
	}
	if pol == nil && len(h.endpoints("senders", "active")) > 0 && !policyChecked {
		out = append(out, h.skipFinding("NMOS-NET-MCAST-CLASS",
			"multicast bandwidth-class check skipped: no --policy"))
	}
	return out
}

// checkFlowFitsClass compares a sender's Flow bitrate hint (when the
// IS-04 flow exposes one) to the policy ceiling for its address class.
func (h *Harvest) checkFlowFitsClass(_ /*senderID*/, res string, mc *MulticastClass) []Finding {
	// The Flow-to-bitrate mapping needs the parameter registers (#851);
	// until then the class membership itself is the check — a sender in
	// a named class range is reported at INFO so the pivot is visible.
	return []Finding{h.find("NMOS-NET-MCAST-CLASS", SevInfo, res,
		fmt.Sprintf("destination is in policy class %q (ceiling %.1f Gbps)", mc.Class, mc.MaxBitrateGbps),
		"site policy", "")}
}

// checkUnicastSourceIPs validates each leg's source_ip and, across the
// two 2022-7 legs, that they sit on different subnets.
func (h *Harvest) checkUnicastSourceIPs(pol *Policy) []Finding {
	var out []Finding
	var privateSeen, publicSeen bool
	for _, ep := range h.endpoints("senders", "active") {
		res := "sender/" + ep.ID
		var nets []string
		for _, p := range ep.E.TransportParams {
			if p.SourceIP == nil || *p.SourceIP == "" {
				continue
			}
			ip := net.ParseIP(*p.SourceIP)
			switch {
			case ip == nil:
				out = append(out, h.find("NMOS-NET-SRC-INVALID", SevError, res,
					fmt.Sprintf("source_ip %q is not an IP address", *p.SourceIP),
					"IS-05 transport_params[].source_ip", ""))
				continue
			case ip.IsMulticast() || ip.IsLoopback() || ip.IsUnspecified():
				out = append(out, h.find("NMOS-NET-SRC-INVALID", SevError, res,
					fmt.Sprintf("source_ip %s is not a unicast address", ip),
					"IS-05 transport_params[].source_ip", "a sender's source must be a real unicast interface address"))
				continue
			case ip.IsLinkLocalUnicast():
				out = append(out, h.find("NMOS-NET-SRC-LINKLOCAL", SevError, res,
					fmt.Sprintf("source_ip %s is link-local (169.254/16) — the interface never got an address", ip),
					"RFC 3927", "the NIC has no DHCP/static lease; the stream cannot be sourced"))
				continue
			}
			if ip.To4() != nil {
				nets = append(nets, ip.Mask(net.CIDRMask(24, 32)).String())
			}
			if isRFC1918(ip) {
				privateSeen = true
			} else if ip.To4() != nil {
				publicSeen = true
			}
		}
		if len(nets) >= 2 && allSame(nets) {
			out = append(out, h.find("NMOS-NET-SRC-SAME-SUBNET", SevWarn, res,
				fmt.Sprintf("both 2022-7 legs source from the same /24 (%s)", nets[0]),
				"SMPTE ST 2022-7 §5", "redundant legs should originate on disjoint networks"))
		}
	}
	// Private/public is a policy question when a policy states an
	// expectation; the mix itself is always worth a flag.
	if privateSeen && publicSeen {
		out = append(out, h.find("NMOS-NET-SRC-MIXED-SCOPE", SevWarn, "",
			"the plant mixes RFC 1918 and globally-routable source addresses on the media plane",
			"site addressing", "a media network should be uniformly private or uniformly routable"))
	}
	if pol == nil || pol.PrivateMediaPlane == nil {
		out = append(out, h.skipFinding("NMOS-NET-SRC-SCOPE",
			"private/public source-IP expectation skipped: no --policy"))
	} else {
		want := *pol.PrivateMediaPlane
		if want && publicSeen {
			out = append(out, h.find("NMOS-NET-SRC-SCOPE", SevError, "",
				"policy expects a private media plane but a globally-routable source_ip is present",
				"site policy", ""))
		}
		if !want && privateSeen {
			out = append(out, h.find("NMOS-NET-SRC-SCOPE", SevWarn, "",
				"policy expects a routable media plane but an RFC 1918 source_ip is present",
				"site policy", ""))
		}
	}
	return out
}

var eui48Colon = regexp.MustCompile(`^[0-9a-fA-F]{2}(:[0-9a-fA-F]{2}){5}$`)

// checkInterfaceMACs validates node.interfaces chassis_id / port_id
// form and that sender interface_bindings name declared interfaces.
func (h *Harvest) checkInterfaceMACs() []Finding {
	var out []Finding
	// A node publishes its interfaces on /self; a registry capture
	// carries the same object in the /nodes collection, but MAC form is
	// a per-node fact so we read the node's own self here.
	nodeBlob, _ := h.object("node", "self")
	declared := map[string]bool{}
	if nodeBlob != nil {
		var n struct {
			Interfaces []struct {
				Name      string `json:"name"`
				ChassisID string `json:"chassis_id"`
				PortID    string `json:"port_id"`
			} `json:"interfaces"`
		}
		if json.Unmarshal(nodeBlob, &n) == nil {
			for _, iface := range n.Interfaces {
				if iface.Name != "" {
					declared[iface.Name] = true
				}
				for label, mac := range map[string]string{"chassis_id": iface.ChassisID, "port_id": iface.PortID} {
					if mac == "" {
						continue
					}
					if eui48Colon.MatchString(mac) || mac != strings.ToLower(mac) {
						out = append(out, h.find("NMOS-NET-MAC-FORM", SevWarn, "node/"+iface.Name,
							fmt.Sprintf("interface %s %s = %q — IS-04 wants lowercase hyphen-separated xx-xx-xx-xx-xx-xx", iface.Name, label, mac),
							"IS-04 node.json interface", "a strict controller rejects colon-separated or uppercase MACs"))
					}
				}
			}
		}
	}
	// interface_bindings must name declared interfaces.
	if len(declared) > 0 {
		if senders, _ := h.collect(nodeAPIFor(h), "senders"); len(senders) > 0 {
			for _, blob := range senders {
				var s struct {
					ID                string   `json:"id"`
					InterfaceBindings []string `json:"interface_bindings"`
				}
				if json.Unmarshal(blob, &s) != nil {
					continue
				}
				for _, b := range s.InterfaceBindings {
					if !declared[b] {
						out = append(out, h.find("NMOS-NET-BINDING-UNKNOWN", SevError, "sender/"+s.ID,
							fmt.Sprintf("interface_binding %q names an interface the node does not declare", b),
							"IS-04 sender.interface_bindings", "the sender is unroutable — bind to a declared interface"))
					}
				}
			}
		}
	}
	return out
}

// checkSenderPTP reads the PTP grandmaster from each sender's SDP and
// validates gmid form + domain range; with a policy, that they match
// the expected GM/domain.
func (h *Harvest) checkSenderPTP(pol *Policy) []Finding {
	var out []Finding
	for key, body := range h.SDP {
		sess, _, err := sdp.Parse(string(body))
		if err != nil {
			continue
		}
		res := "sender/" + sdpResourceID(key)
		for _, m := range sess.Media {
			if m.TSRefClk == nil {
				continue
			}
			gm := m.TSRefClk.GMID
			if gm == "traceable" {
				continue
			}
			if gm != "" && !isEUI64(gm) {
				out = append(out, h.find("NMOS-NET-PTP-GMID", SevWarn, res,
					fmt.Sprintf("%s: ts-refclk grandmaster %q is not EUI-64 form", key, gm),
					"SMPTE ST 2110-10 / IEEE 1588", ""))
			}
			if m.TSRefClk.Domain >= 0 && m.TSRefClk.Domain > 127 {
				out = append(out, h.find("NMOS-NET-PTP-DOMAIN", SevWarn, res,
					fmt.Sprintf("%s: PTP domain %d out of range 0-127", key, m.TSRefClk.Domain),
					"IEEE 1588 §7.1", ""))
			}
			if pol != nil && pol.ExpectedGrandmaster != "" && gm != "" && !strings.EqualFold(gm, pol.ExpectedGrandmaster) {
				out = append(out, h.find("NMOS-NET-PTP-WRONG-GM", SevError, res,
					fmt.Sprintf("%s: grandmaster %s is not the policy-expected %s", key, gm, pol.ExpectedGrandmaster),
					"site policy", "a sender locked to a different GM is not synchronised with the plant"))
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Resource < out[j].Resource })
	return out
}

// checkPlantGrandmaster reports every distinct PTP grandmaster seen
// across the plant — two GMs in one plant is a fault invisible from
// any single device.
func checkPlantGrandmaster(all []*Harvest, pol *Policy) []Finding {
	gms := map[string]bool{} // distinct grandmaster ids seen plant-wide
	for _, h := range all {
		for _, body := range h.SDP {
			sess, _, err := sdp.Parse(string(body))
			if err != nil {
				continue
			}
			for _, m := range sess.Media {
				if m.TSRefClk == nil || m.TSRefClk.GMID == "" || m.TSRefClk.GMID == "traceable" {
					continue
				}
				gms[m.TSRefClk.GMID] = true
			}
		}
	}
	if len(gms) <= 1 {
		return nil
	}
	ids := make([]string, 0, len(gms))
	for id := range gms {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	detail := "the plant references " + fmt.Sprintf("%d", len(gms)) + " distinct PTP grandmasters: " + strings.Join(ids, ", ")
	f := Finding{
		Code: "NMOS-NET-PTP-MULTIPLE-GM", Severity: SevError,
		Detail: detail, Spec: "SMPTE ST 2059 / IEEE 1588 — one time domain per plant",
		Hint: "senders locked to different grandmasters are not mutually synchronised",
	}
	if pol != nil && pol.ExpectedGrandmaster != "" {
		f.Hint += "; policy expects " + pol.ExpectedGrandmaster
	}
	return []Finding{f}
}

// --- small helpers ---

func isRFC1918(ip net.IP) bool {
	for _, c := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} {
		if _, n, _ := net.ParseCIDR(c); n.Contains(ip) {
			return true
		}
	}
	return false
}

var eui64 = regexp.MustCompile(`^[0-9a-fA-F]{2}([-:][0-9a-fA-F]{2}){7}$`)

func isEUI64(s string) bool { return eui64.MatchString(s) }

// hasSourceFilter reports whether the sender's captured SDP carries an
// a=source-filter — needed for an SSM (232/8) group to be joinable.
func hasSourceFilter(h *Harvest, senderID string) bool {
	for key, body := range h.SDP {
		if sdpResourceID(key) != senderID {
			continue
		}
		sess, _, err := sdp.Parse(string(body))
		if err != nil {
			continue
		}
		for _, m := range sess.Media {
			if m.SourceFilt != nil {
				return true
			}
		}
	}
	return false
}

// nodeAPIFor picks query when present (registry capture) else node.
func nodeAPIFor(h *Harvest) string {
	if _, ok := h.APIs["query"]; ok {
		return "query"
	}
	return "node"
}

// skipFinding renders a SKIP-shaped finding (SevInfo, code suffixed so
// the render shows it stood down rather than passed).
func (h *Harvest) skipFinding(code, detail string) Finding {
	return Finding{Code: code, Severity: SevInfo, Target: h.Target, Device: h.Label,
		Detail: "SKIP: " + detail}
}
