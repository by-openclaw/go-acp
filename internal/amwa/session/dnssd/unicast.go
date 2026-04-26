package dnssd

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"acp/internal/amwa/codec/dnssd"
)

// DefaultUnicastTimeout caps a single unicast DNS-SD query.
const DefaultUnicastTimeout = 3 * time.Second

// ResolveUnicast performs an RFC 6763 §10 unicast DNS-SD lookup
// against the given resolver and returns every instance the response
// fully describes (PTR + SRV + TXT in one shot — most DNS servers
// answer DNS-SD PTR queries with the SRV/TXT/A/AAAA records in the
// Additional section).
//
// resolver is host:port; when port is omitted ":53" is appended.
// service is "_nmos-register._tcp" etc; domain is "local" or a
// public domain. timeout zero means DefaultUnicastTimeout.
//
// If a peer returns only PTR records (RFC-compliant but bandwidth-
// minimising) the returned slice will be empty. Phase 1.5 will add
// chase-the-PTR retries; the issue tracker should record that case
// when we hit it in the wild.
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
	if domain != "" {
		qname += "." + domain
	}
	qbytes, err := dnssd.EncodeQuery(qname, dnssd.TypePTR, false)
	if err != nil {
		return nil, fmt.Errorf("dnssd: encode unicast query: %w", err)
	}

	d := net.Dialer{}
	conn, err := d.DialContext(dctx, "udp", resolver)
	if err != nil {
		return nil, fmt.Errorf("dnssd: dial resolver %s: %w", resolver, err)
	}
	defer func() { _ = conn.Close() }()

	dl, _ := dctx.Deadline()
	if !dl.IsZero() {
		_ = conn.SetDeadline(dl)
	}

	if _, err := conn.Write(qbytes); err != nil {
		return nil, fmt.Errorf("dnssd: write unicast query: %w", err)
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("dnssd: read unicast response: %w", err)
	}
	msg, err := dnssd.Decode(buf[:n])
	if err != nil {
		return nil, fmt.Errorf("dnssd: decode unicast response: %w", err)
	}
	if !msg.Header.IsResponse() {
		return nil, fmt.Errorf("dnssd: resolver returned non-response message")
	}
	return dnssd.DecodeInstances(msg, service), nil
}
