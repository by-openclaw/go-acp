package dnssd

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"dhs/internal/amwa/codec/dnssd"
)

// DefaultUnicastTimeout caps a single unicast DNS-SD query.
const DefaultUnicastTimeout = 3 * time.Second

// ResolveUnicast performs an RFC 6763 §10 unicast DNS-SD lookup
// against the given resolver and returns one Instance per discovered
// service. Strategy:
//
//  1. Send a PTR query for the service-type name. If the resolver
//     packs SRV/TXT/A records into the answer or additional sections
//     (the bandwidth-rich style — Apple Bonjour, Avahi default), we
//     decode every Instance in one shot via DecodeInstances.
//  2. Otherwise (the bandwidth-minimising style — Unbound default,
//     and most resolver implementations), the answer is PTR-only.
//     We then *chase the PTR*: for every PTR target, fire follow-up
//     SRV + TXT (+ A) queries on the same connection and stitch the
//     records back together into an Instance.
//
// resolver is host:port; when port is omitted ":53" is appended.
// service is "_nmos-register._tcp" etc; domain is "local" for mDNS-
// equivalent unicast or a public/internal domain for site-wide DNS-SD.
// timeout zero means DefaultUnicastTimeout — and applies to the whole
// chase, not a single query.
func ResolveUnicast(ctx context.Context, resolver, service, domain string, timeout time.Duration) ([]dnssd.Instance, error) {
	if timeout <= 0 {
		timeout = DefaultUnicastTimeout
	}
	if !strings.Contains(resolver, ":") {
		resolver += ":53"
	}
	dctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	qname := service
	// Only append the default domain when the caller passed a bare
	// service-type name (last label starts with "_"). If the service
	// already includes its own domain (e.g. `_nmos-register._tcp.by-
	// systems.arpa`), use it verbatim.
	if domain != "" && isBareServiceType(service) {
		qname += "." + domain
	}

	d := net.Dialer{}
	conn, err := d.DialContext(dctx, "udp", resolver)
	if err != nil {
		return nil, fmt.Errorf("dnssd: dial resolver %s: %w", resolver, err)
	}
	defer func() { _ = conn.Close() }()
	if dl, ok := dctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}

	// Step 1 — PTR query.
	ptrMsg, err := dnsRoundtrip(conn, qname, dnssd.TypePTR)
	if err != nil {
		return nil, err
	}

	// Step 2 — bandwidth-rich path: server packed SRV/TXT in the same
	// response. Try first; this is the cheapest happy path.
	if instances := dnssd.DecodeInstances(ptrMsg, service); len(instances) > 0 {
		return instances, nil
	}

	// Step 3 — chase-the-PTR. Walk every PTR answer and resolve each
	// instance with explicit SRV + TXT (and one A) queries.
	var targets []string
	for _, rr := range ptrMsg.Answers {
		if rr.Type == dnssd.TypePTR && rr.PTR != "" {
			targets = append(targets, strings.TrimSuffix(rr.PTR, "."))
		}
	}
	if len(targets) == 0 {
		return nil, nil
	}

	out := make([]dnssd.Instance, 0, len(targets))
	for _, full := range targets {
		ins, ok := chaseInstance(conn, full, service, domain)
		if ok {
			out = append(out, ins)
		}
	}
	return out, nil
}

// dnsRoundtrip sends a single DNS query and reads one response on the
// already-connected UDP socket. The caller is responsible for setting
// a connection deadline before calling.
func dnsRoundtrip(conn net.Conn, qname string, qtype uint16) (*dnssd.Message, error) {
	qbytes, err := dnssd.EncodeQuery(qname, qtype, false)
	if err != nil {
		return nil, fmt.Errorf("dnssd: encode query %s: %w", qname, err)
	}
	if _, err := conn.Write(qbytes); err != nil {
		return nil, fmt.Errorf("dnssd: write query %s: %w", qname, err)
	}
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("dnssd: read response %s: %w", qname, err)
	}
	msg, err := dnssd.Decode(buf[:n])
	if err != nil {
		return nil, fmt.Errorf("dnssd: decode response %s: %w", qname, err)
	}
	if !msg.Header.IsResponse() {
		return nil, fmt.Errorf("dnssd: resolver returned non-response message for %s", qname)
	}
	return msg, nil
}

// chaseInstance queries SRV + TXT (and one A) for a fully-qualified
// instance name and stitches the records back into an Instance.
// Returns ok=false if no usable SRV record came back.
func chaseInstance(conn net.Conn, fullName, service, domain string) (dnssd.Instance, bool) {
	var ins dnssd.Instance
	ins.Service = service
	ins.Domain = domain
	// Instance label = leftmost dot-delimited segment of the full name.
	// Adequate for the standard pattern; instance labels with embedded
	// dots use DNS escaping, which the codec handles in EncodeQuery.
	if i := strings.IndexByte(fullName, '.'); i > 0 {
		ins.Name = fullName[:i]
	} else {
		ins.Name = fullName
	}

	// SRV.
	srvMsg, err := dnsRoundtrip(conn, fullName, dnssd.TypeSRV)
	if err != nil {
		return ins, false
	}
	for _, rr := range srvMsg.Answers {
		if rr.Type == dnssd.TypeSRV && rr.SRV != nil {
			ins.Host = strings.TrimSuffix(rr.SRV.Target, ".")
			ins.Port = rr.SRV.Port
			break
		}
	}
	if ins.Host == "" || ins.Port == 0 {
		return ins, false
	}

	// TXT.
	if txtMsg, err := dnsRoundtrip(conn, fullName, dnssd.TypeTXT); err == nil {
		ins.TXT = map[string]string{}
		for _, rr := range txtMsg.Answers {
			if rr.Type != dnssd.TypeTXT {
				continue
			}
			for _, kv := range rr.TXT {
				if eq := strings.IndexByte(kv, '='); eq >= 0 {
					ins.TXT[kv[:eq]] = kv[eq+1:]
				} else {
					ins.TXT[kv] = ""
				}
			}
		}
	}

	// A (optional). Failure here is non-fatal — the SRV target name is
	// still usable for HTTP clients via system resolver.
	if aMsg, err := dnsRoundtrip(conn, ins.Host, dnssd.TypeA); err == nil {
		for _, rr := range aMsg.Answers {
			if rr.Type == dnssd.TypeA && rr.A != nil {
				ins.IPv4 = append(ins.IPv4, rr.A)
			}
		}
	}

	return ins, true
}

// isBareServiceType reports whether s is a DNS-SD service type without
// an attached domain — e.g. `_nmos-register._tcp` returns true,
// `_nmos-register._tcp.by-systems.arpa` returns false. The heuristic
// is RFC 6763 §4.1.2: every label of a bare service type starts with
// "_"; once a non-underscore label appears, the rest is the domain.
func isBareServiceType(s string) bool {
	if s == "" {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if label == "" || !strings.HasPrefix(label, "_") {
			return false
		}
	}
	return true
}
