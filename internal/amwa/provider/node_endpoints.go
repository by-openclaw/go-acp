package provider

import (
	"net"
	"strconv"
	"strings"

	"acp/internal/amwa/codec/is04"
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
	want := []is04.NodeEndpoint{}
	if host != "" && port != 0 && !isWildcard(host) {
		want = append(want, is04.NodeEndpoint{Host: host, Port: port, Protocol: "http"})
	}
	for _, ip := range localIPv4() {
		want = append(want, is04.NodeEndpoint{Host: ip, Port: port, Protocol: "http"})
	}
	for _, candidate := range want {
		if endpointAlreadyListed(n.API.Endpoints, candidate) {
			continue
		}
		n.API.Endpoints = append(n.API.Endpoints, candidate)
	}
}

// clearUnservedManifestHrefs sets each Sender's manifest_href to nil.
// IS-04 v1.3.3 sender.json declares manifest_href as required-nullable;
// dhs does not yet ship a /transportfile route, so a non-nil URL would
// be a lie that fails AMWA test_20_01.
func clearUnservedManifestHrefs(senders []is04.Sender) {
	for i := range senders {
		senders[i].ManifestHref = nil
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
