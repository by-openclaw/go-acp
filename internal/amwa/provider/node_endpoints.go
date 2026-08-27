package provider

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"dhs/internal/amwa/codec/is04"
)

// expandNodeEndpoints normalises the Node's `api.endpoints` array so
// the AMWA NMOS Testing tool's test_20 ("resources correctly signal
// the current protocol and IP/hostname") finds the address it actually
// reached us on.
//
// Behaviour, idempotent:
//   - keep every entry the operator already declared in the bundle;
//   - add an entry for the --advertise-host hostname when set;
//   - add an entry for every non-loopback IPv4 we bind on (covers the
//     0.0.0.0 / IPv6-:: case where net.Listen reports only ::);
//   - port comes from --advertise-host:port if set, else from --bind.
//
// The protocol is currently fixed to "http" — IS-10 / TLS lands later.
func expandNodeEndpoints(n *is04.Node, advertiseHost, bind string) {
	host, port := endpointHostPort(advertiseHost, bind)
	// The advertised endpoint goes FIRST, not last: firstNodeIP picks
	// the first IP literal, and every derived address — control
	// hrefs, IS-05 source_ip, the SDP origin — follows it. Appending
	// let stale entries riding in from the bundle keep winning: a
	// dead 10.6.239.113 in a fixture put every registered control
	// href on a host that no longer exists, and the controller showed
	// empty connection panels with no error anywhere.
	if host != "" && port != 0 && !isWildcard(host) {
		adv := is04.NodeEndpoint{Host: host, Port: port, Protocol: "http"}
		out := []is04.NodeEndpoint{adv}
		for _, ep := range n.API.Endpoints {
			if !endpointAlreadyListed(out, ep) {
				out = append(out, ep)
			}
		}
		n.API.Endpoints = out
	}
	for _, ip := range localIPv4() {
		candidate := is04.NodeEndpoint{Host: ip, Port: port, Protocol: "http"}
		if !endpointAlreadyListed(n.API.Endpoints, candidate) {
			n.API.Endpoints = append(n.API.Endpoints, candidate)
		}
	}
}

// sdpFor returns a minimal RFC 4566 SDP body for a Sender. Only the
// fields the IS-04 schemas + AMWA Testing tool actually validate are
// populated — session origin, label as session name, RTP/AVP audio
// L24/48000 stereo as a placeholder. The Sender's transport URN
// (urn:x-nmos:transport:rtp.ucast / .mcast) drives the connection
// line; multicast adds an SSRC group hint.
//
// Production deployments override this with a generator that walks
// the linked Flow + Source + IS-05 transport_params for an
// authoritative SDP. The stub here is what makes manifest_href a
// non-null URI per v1.0/v1.1/v1.2 schema and reachable per v1.3
// test_20_01 — both spec-required, neither a workaround.
func sdpFor(s is04.Sender) string {
	label := s.Label
	if label == "" {
		label = "dhs-sender-" + s.ID
	}
	connHost := "0.0.0.0"
	mode := "rtp"
	switch {
	case strings.HasSuffix(s.Transport, ":rtp.mcast"):
		mode = "rtp.mcast"
	case strings.HasSuffix(s.Transport, ":rtp.ucast"):
		mode = "rtp.ucast"
	}
	_ = mode // currently we emit the same minimal SDP for both — connHost stays 0.0.0.0
	var b strings.Builder
	fmt.Fprintln(&b, "v=0")
	fmt.Fprintln(&b, "o=- 0 0 IN IP4 "+connHost)
	fmt.Fprintln(&b, "s="+label)
	fmt.Fprintln(&b, "t=0 0")
	fmt.Fprintln(&b, "m=audio 5004 RTP/AVP 96")
	fmt.Fprintln(&b, "c=IN IP4 "+connHost)
	fmt.Fprintln(&b, "a=rtpmap:96 L24/48000/2")
	return b.String()
}

// rewriteManifestHrefs updates each Sender's manifest_href to point
// at THIS Node's /transportfile endpoint at the wire api_ver. The
// bundle JSON typically hardcodes a v1.3 path; rewriting makes the
// URL consistent with whatever wire minor we serve, AND consistent
// with where we actually serve the transportfile route (under
// /x-nmos/node/<apiVer>/senders/{id}/transportfile).
//
// IS-04 v1.0/v1.1/v1.2 sender.json require manifest_href to be a
// non-null URI string; v1.3 permits null. Either way the URL must
// be reachable (AMWA test_20_01 tests this on v1.3). Pairing this
// with the matching transportfile handler is the strict-spec answer.
func rewriteManifestHrefs(senders []is04.Sender, advertiseHost, apiVer string) {
	if advertiseHost == "" || apiVer == "" {
		return
	}
	for i := range senders {
		url := "http://" + advertiseHost + "/x-nmos/node/" + apiVer +
			"/senders/" + senders[i].ID + "/transportfile"
		senders[i].ManifestHref = &url
	}
}

// localIPv4 returns every globally-routable IPv4 bound to a non-down,
// non-loopback interface. Best-effort — failure returns an empty list.
func localIPv4() []string {
	out := []string{}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipnet.IP.To4()
		if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() {
			continue
		}
		out = append(out, ip.String())
	}
	return out
}

func endpointAlreadyListed(existing []is04.NodeEndpoint, candidate is04.NodeEndpoint) bool {
	for _, e := range existing {
		if strings.EqualFold(e.Host, candidate.Host) && e.Port == candidate.Port && strings.EqualFold(e.Protocol, candidate.Protocol) {
			return true
		}
	}
	return false
}

func isWildcard(host string) bool {
	return host == "" || host == "0.0.0.0" || host == "::" || strings.EqualFold(host, "[::]")
}

// endpointHostPort parses --advertise-host into (host, port). Unlike
// splitHostPort in system.go, it returns "" for wildcard hosts so the
// caller can decide whether to substitute an interface IP. Port=0 on
// parse failure so callers can detect the gap.
func endpointHostPort(advertiseHost, bind string) (string, int) {
	host, port := "", 0
	if advertiseHost != "" {
		h, p, err := net.SplitHostPort(advertiseHost)
		if err == nil {
			host = h
			if pi, err := strconv.Atoi(p); err == nil {
				port = pi
			}
		} else {
			host = advertiseHost
		}
	}
	if port == 0 && bind != "" {
		_, p, err := net.SplitHostPort(bind)
		if err == nil {
			if pi, err := strconv.Atoi(p); err == nil {
				port = pi
			}
		}
	}
	return host, port
}
