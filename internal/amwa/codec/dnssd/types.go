package dnssd

import (
	"fmt"
	"net"
	"strings"
)

// NMOS service types (RFC 6763). One PTR query per type discovers all
// instances of that role on the link / domain.
const (
	ServiceRegister = "_nmos-register._tcp" // IS-04 Registration API (Registry left face)
	ServiceQuery    = "_nmos-query._tcp"    // IS-04 Query API (Registry right face)
	ServiceSystem   = "_nmos-system._tcp"   // IS-09 System API
	ServiceNode     = "_nmos-node._tcp"     // IS-04 Node API (P2P fallback)
)

// DefaultDomain is the link-local mDNS suffix (RFC 6762 §3).
const DefaultDomain = "local"

// Instance describes a discovered service instance (or one we want to
// announce). Instance.Name is the human label that goes in the leftmost
// position of the PTR target — e.g. "dhs-registry-1".
type Instance struct {
	Name    string            // instance label, e.g. "dhs-registry-1"
	Service string            // service type, e.g. "_nmos-register._tcp"
	Domain  string            // discovery domain, e.g. "local"
	Host    string            // SRV target hostname, e.g. "dhs-registry-1.local"
	Port    uint16            // SRV target port
	IPv4    []net.IP          // A records advertised alongside SRV
	IPv6    []net.IP          // AAAA records advertised alongside SRV
	TXT     map[string]string // RFC 6763 §6 TXT key=value pairs

	// TTL applied to the PTR/SRV/TXT/A/AAAA records when announcing.
	// Zero is treated as DefaultAnnounceTTL.
	TTL uint32
}

// DefaultAnnounceTTL matches RFC 6762 §10 recommended TTL for shared
// records (PTR pointing to a service type) and unique records
// (SRV/TXT/A/AAAA): 75 minutes for shared, 120 seconds for unique.
// We use the lower bound for everything to keep peers fresh.
const DefaultAnnounceTTL = 120

// FullName returns the fully-qualified instance name in the
// "<instance>.<service>.<domain>" form used by SRV/TXT.
func (i Instance) FullName() string {
	dom := i.Domain
	if dom == "" {
		dom = DefaultDomain
	}
	return fmt.Sprintf("%s.%s.%s", escapeInstanceLabel(i.Name), i.Service, dom)
}

// PTRName returns the service-type-only name used for browse PTR
// queries / responses.
func (i Instance) PTRName() string {
	dom := i.Domain
	if dom == "" {
		dom = DefaultDomain
	}
	return fmt.Sprintf("%s.%s", i.Service, dom)
}

// EncodeAnnounce builds an unsolicited DNS-SD response advertising
// the instance. Mirrors RFC 6762 §8.3 announcement format.
//
// The response carries:
//   - one PTR record on the service-type name pointing to the instance
//   - one SRV on the instance name
//   - one TXT on the instance name
//   - one A per IPv4 in the instance
//   - one AAAA per IPv6 in the instance
//
// The cache-flush bit is set on SRV/TXT/A/AAAA per RFC 6762 §10.2 since
// they are unique records; PTR is shared and never gets flush.
func EncodeAnnounce(i Instance, asResponse bool) ([]byte, error) {
	if i.Name == "" || i.Service == "" || i.Host == "" || i.Port == 0 {
		return nil, fmt.Errorf("dnssd: instance missing required fields")
	}
	ttl := i.TTL
	if ttl == 0 {
		ttl = DefaultAnnounceTTL
	}

	full := i.FullName()
	ptrName := i.PTRName()

	msg := &Message{}
	msg.Header.SetResponse(asResponse)
	msg.Header.SetAuthoritative(asResponse)

	msg.Answers = append(msg.Answers, RR{
		Name:  ptrName,
		Type:  TypePTR,
		Class: ClassIN,
		TTL:   ttl,
		PTR:   full,
	})
	msg.Answers = append(msg.Answers, RR{
		Name:  full,
		Type:  TypeSRV,
		Class: ClassIN | ClassFlushBit,
		TTL:   ttl,
		SRV: &SRVData{
			Priority: 0,
			Weight:   0,
			Port:     i.Port,
			Target:   strings.TrimSuffix(i.Host, "."),
		},
	})
	txtSegs, err := EncodeTXT(i.TXT)
	if err != nil {
		return nil, err
	}
	msg.Answers = append(msg.Answers, RR{
		Name:  full,
		Type:  TypeTXT,
		Class: ClassIN | ClassFlushBit,
		TTL:   ttl,
		TXT:   txtSegs,
	})
	for _, ip := range i.IPv4 {
		ip4 := ip.To4()
		if ip4 == nil {
			continue
		}
		msg.Answers = append(msg.Answers, RR{
			Name:  strings.TrimSuffix(i.Host, "."),
			Type:  TypeA,
			Class: ClassIN | ClassFlushBit,
			TTL:   ttl,
			A:     ip4,
		})
	}
	for _, ip := range i.IPv6 {
		ip6 := ip.To16()
		if ip6 == nil || ip.To4() != nil {
			continue
		}
		msg.Answers = append(msg.Answers, RR{
			Name:  strings.TrimSuffix(i.Host, "."),
			Type:  TypeAAAA,
			Class: ClassIN | ClassFlushBit,
			TTL:   ttl,
			AAAA:  ip6,
		})
	}
	return msg.Encode()
}

// EncodeQuery builds a single-question DNS query for a service type.
// queryUnicast sets the QU bit (RFC 6762 §5.4) requesting unicast reply.
func EncodeQuery(qname string, qtype uint16, queryUnicast bool) ([]byte, error) {
	class := ClassIN
	if queryUnicast {
		class |= ClassUnicastBit
	}
	msg := &Message{}
	msg.Questions = []Question{{Name: qname, Type: qtype, Class: class}}
	return msg.Encode()
}

// DecodeInstances scans a response message for SRV+TXT pairs and
// returns one Instance per discovered service instance. Pairs A/AAAA
// records by SRV.Target. Records with mismatched names are skipped.
func DecodeInstances(m *Message, service string) []Instance {
	if m == nil {
		return nil
	}
	type tmp struct {
		full string
		ptr  string
		srv  *SRVData
		txt  map[string]string
	}
	byFull := map[string]*tmp{}
	addrs4 := map[string][]net.IP{}
	addrs6 := map[string][]net.IP{}

	addAll := func(rrs []RR) {
		for _, rr := range rrs {
			switch rr.Type {
			case TypePTR:
				if service != "" && !strings.HasPrefix(rr.Name, service+".") {
					continue
				}
				t := byFull[rr.PTR]
				if t == nil {
					t = &tmp{full: rr.PTR}
					byFull[rr.PTR] = t
				}
				t.ptr = rr.Name
			case TypeSRV:
				t := byFull[rr.Name]
				if t == nil {
					t = &tmp{full: rr.Name}
					byFull[rr.Name] = t
				}
				t.srv = rr.SRV
			case TypeTXT:
				t := byFull[rr.Name]
				if t == nil {
					t = &tmp{full: rr.Name}
					byFull[rr.Name] = t
				}
				t.txt = DecodeTXT(rr.TXT)
			case TypeA:
				addrs4[rr.Name] = append(addrs4[rr.Name], rr.A)
			case TypeAAAA:
				addrs6[rr.Name] = append(addrs6[rr.Name], rr.AAAA)
			}
		}
	}
	addAll(m.Answers)
	addAll(m.Authority)
	addAll(m.Additional)

	out := make([]Instance, 0, len(byFull))
	for full, t := range byFull {
		if t.srv == nil {
			continue
		}
		name, svc, dom := splitFullName(full)
		if svc != "" && service != "" && svc != service {
			continue
		}
		ins := Instance{
			Name:    name,
			Service: svc,
			Domain:  dom,
			Host:    t.srv.Target,
			Port:    t.srv.Port,
			IPv4:    addrs4[t.srv.Target],
			IPv6:    addrs6[t.srv.Target],
			TXT:     t.txt,
		}
		out = append(out, ins)
	}
	return out
}

// splitFullName breaks "instance.<svc>._tcp.<domain>" (or _udp) into
// its three pieces. The protocol label (_tcp / _udp) is the anchor:
// the label before it is the service name, everything before that is
// the instance, everything after is the domain.
func splitFullName(full string) (instance, service, domain string) {
	full = strings.TrimSuffix(full, ".")
	parts := strings.Split(full, ".")
	protoIdx := -1
	for i, p := range parts {
		if p == "_tcp" || p == "_udp" {
			protoIdx = i
		}
	}
	if protoIdx < 1 {
		return "", "", full
	}
	if !strings.HasPrefix(parts[protoIdx-1], "_") {
		return "", "", full
	}
	instance = unescapeInstanceLabel(strings.Join(parts[:protoIdx-1], "."))
	service = parts[protoIdx-1] + "." + parts[protoIdx]
	domain = strings.Join(parts[protoIdx+1:], ".")
	return
}

// escapeInstanceLabel applies RFC 6763 §4.3 dot-escape inside the
// instance label position. Backslash and dot get backslash-escaped.
func escapeInstanceLabel(s string) string {
	var b strings.Builder
	for _, c := range s {
		if c == '.' || c == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// unescapeInstanceLabel reverses escapeInstanceLabel.
func unescapeInstanceLabel(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			b.WriteByte(s[i+1])
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
