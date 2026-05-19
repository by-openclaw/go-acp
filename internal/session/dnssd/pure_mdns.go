package dnssd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// mDNS link-local multicast endpoints per RFC 6762.
var (
	mdnsGroupV4 = net.IPv4(224, 0, 0, 251)
	mdnsPort    = 5353
)

// NewPureBrowser returns a Browser implementation that talks the
// mDNS wire protocol directly via stdlib net — no shell-out to
// avahi-browse or dns-sd. Per R18 #477 strict-spec.
func NewPureBrowser() Browser {
	return &pureBrowser{}
}

type pureBrowser struct{}

// browserInstance is the aggregation row used while walking a
// browse session's PTR / SRV / TXT / A records.
type browserInstance struct {
	Name string
	Host string
	Port int
	TXT  map[string]string
}

func (p *pureBrowser) Browse(ctx context.Context, opts BrowseOptions) ([]Service, error) {
	if opts.ServiceType == "" {
		return nil, errors.New("dnssd: ServiceType is required")
	}
	d := opts.Duration
	if d <= 0 {
		d = 5 * time.Second
	}

	// Bind an ephemeral UDP4 socket. Responses to our query arrive
	// here. We DO NOT need to join the multicast group as a listener
	// to receive the unicast replies that most responders send back.
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, fmt.Errorf("dnssd: open browser socket: %w", err)
	}
	defer func() { _ = conn.Close() }()

	// Make sure the service-type ends with a `.local.` zone like every
	// real DNS-SD responder expects. Operators sometimes type the bare
	// `_ember._tcp`; normalise.
	st := opts.ServiceType
	if !strings.HasSuffix(st, ".local.") && !strings.HasSuffix(st, ".local") {
		st = st + ".local"
	}
	if !strings.HasSuffix(st, ".") {
		st = st + "."
	}

	query, err := EncodeQuery(0, st)
	if err != nil {
		return nil, fmt.Errorf("dnssd: encode query: %w", err)
	}
	dst := &net.UDPAddr{IP: mdnsGroupV4, Port: mdnsPort}
	if _, err := conn.WriteToUDP(query, dst); err != nil {
		return nil, fmt.Errorf("dnssd: send query: %w", err)
	}

	deadline := time.Now().Add(d)
	if err := conn.SetReadDeadline(deadline); err != nil {
		return nil, fmt.Errorf("dnssd: set deadline: %w", err)
	}

	// Aggregator: walk PTR → SRV → TXT → A and assemble Service rows.
	byInstance := map[string]*browserInstance{}
	addrByName := map[string]net.IP{}
	buf := make([]byte, 9000) // safe for typical mDNS payloads
	for ctx.Err() == nil {
		n, _, rerr := conn.ReadFromUDP(buf)
		if rerr != nil {
			// Deadline exceeded ends the loop cleanly.
			var nerr net.Error
			if errors.As(rerr, &nerr) && nerr.Timeout() {
				break
			}
			// Non-timeout error: surface but keep what we collected.
			break
		}
		msg, derr := Decode(buf[:n])
		if derr != nil {
			continue // malformed packet — skip
		}
		// Walk both Answer and Additional records — many responders
		// put the SRV/TXT/A in Additional and the PTR in Answer.
		all := append([]Record{}, msg.Answers...)
		all = append(all, msg.Additional...)
		for _, rr := range all {
			switch rr.Type & 0x7FFF {
			case TypePTR:
				if !sameName(rr.Name, st) {
					continue
				}
				ensureInstance(byInstance, rr.PTR)
			case TypeSRV:
				inst := ensureInstance(byInstance, rr.Name)
				inst.Port = int(rr.SRV.Port)
				inst.Host = rr.SRV.Target
			case TypeTXT:
				inst := ensureInstance(byInstance, rr.Name)
				if inst.TXT == nil {
					inst.TXT = map[string]string{}
				}
				for k, v := range rr.TXT {
					inst.TXT[k] = v
				}
			case TypeA:
				addrByName[rr.Name] = net.IP(rr.A[:])
			}
		}
	}

	out := make([]Service, 0, len(byInstance))
	for name, inst := range byInstance {
		svc := Service{
			Name:     instanceLeaf(name, st),
			Port:     inst.Port,
			Hostname: inst.Host,
			TXT:      inst.TXT,
		}
		if ip, ok := addrByName[inst.Host]; ok {
			svc.Host = ip.String()
		}
		out = append(out, svc)
	}
	return out, nil
}

func ensureInstance(m map[string]*browserInstance, name string) *browserInstance {
	if v, ok := m[name]; ok {
		return v
	}
	v := &browserInstance{Name: name}
	m[name] = v
	return v
}

// instanceLeaf strips the service-type suffix off a DNS-SD instance
// name. `instance._ember._tcp.local.` minus `_ember._tcp.local.`
// becomes `instance`.
func instanceLeaf(fullName, serviceType string) string {
	if strings.HasSuffix(fullName, "."+serviceType) {
		return strings.TrimSuffix(fullName, "."+serviceType)
	}
	return fullName
}

// sameName compares two DNS names case-insensitively, ignoring
// trailing-dot differences. DNS is case-insensitive per RFC 4343 so
// `_ember._tcp.local.` and `_Ember._TCP.LOCAL.` are the same name.
func sameName(a, b string) bool {
	return strings.EqualFold(strings.TrimSuffix(a, "."), strings.TrimSuffix(b, "."))
}

// PureAnnouncer is the mDNS announcer implementation. Per R18 #477,
// when `dhs producer emberplus serve --mdns` runs, the announcer
// publishes our PTR + SRV + TXT + A records so the LAN sees us.
//
// The announcer joins the 224.0.0.251:5353 multicast group, listens
// for incoming PTR queries that match our service type, and
// responds with the full record set. On startup it also emits an
// unsolicited announcement to populate cold caches.
type PureAnnouncer struct {
	Service     Service // Name + Host (IP) + Port + TXT
	ServiceType string  // e.g. `_ember._tcp.local.`
	Hostname    string  // e.g. `dhs.local.`
	TTL         uint32  // default 120 when zero
}

// NewPureAnnouncer constructs an Announcer with default TTL.
func NewPureAnnouncer(svc Service, serviceType, hostname string) *PureAnnouncer {
	return &PureAnnouncer{Service: svc, ServiceType: serviceType, Hostname: hostname, TTL: 120}
}

// Announce binds the multicast group, sends one unsolicited
// announcement, then enters the responder loop until ctx cancels.
// The returned stop closure cancels the inner context — the same
// shape as Announcer.Announce.
func (a *PureAnnouncer) Announce(ctx context.Context, _ Service) (func(), error) {
	st := a.ServiceType
	if !strings.HasSuffix(st, ".") {
		st = st + "."
	}
	if !strings.HasSuffix(st, ".local.") {
		st = strings.TrimSuffix(st, ".") + ".local."
	}
	hostname := a.Hostname
	if !strings.HasSuffix(hostname, ".") {
		hostname = hostname + "."
	}
	if !strings.HasSuffix(hostname, ".local.") {
		hostname = strings.TrimSuffix(hostname, ".") + ".local."
	}

	// IP for the A record. Prefer the explicit Service.Host (operator
	// override); otherwise fall back to the first non-loopback IPv4
	// the local host advertises.
	var ip net.IP
	if a.Service.Host != "" {
		ip = net.ParseIP(a.Service.Host).To4()
	}
	if ip == nil {
		ip = firstNonLoopbackV4()
	}
	if ip == nil {
		return nil, errors.New("dnssd: no IPv4 address found for announcement")
	}

	instanceName := a.Service.Name + "." + st

	answers := []Record{
		{Name: st, Type: TypePTR, Class: ClassIN, TTL: a.TTL, PTR: instanceName},
		{Name: instanceName, Type: TypeSRV, Class: ClassIN, TTL: a.TTL,
			SRV: SRVData{Priority: 0, Weight: 0, Port: uint16(a.Service.Port), Target: hostname}},
		{Name: instanceName, Type: TypeTXT, Class: ClassIN, TTL: a.TTL, TXT: a.Service.TXT},
		{Name: hostname, Type: TypeA, Class: ClassIN, TTL: a.TTL, A: [4]byte{ip[0], ip[1], ip[2], ip[3]}},
	}

	// Listening + sending socket: join the multicast group so we
	// receive queries.
	conn, err := net.ListenMulticastUDP("udp4", nil, &net.UDPAddr{IP: mdnsGroupV4, Port: mdnsPort})
	if err != nil {
		return nil, fmt.Errorf("dnssd: listen multicast: %w", err)
	}

	// Unsolicited initial announcement to cold caches.
	if msg, eerr := EncodeResponse(0, answers); eerr == nil {
		_, _ = conn.WriteToUDP(msg, &net.UDPAddr{IP: mdnsGroupV4, Port: mdnsPort})
	}

	innerCtx, cancel := context.WithCancel(ctx)
	go a.responderLoop(innerCtx, conn, st, answers)

	stop := func() {
		cancel()
		_ = conn.Close()
	}
	return stop, nil
}

// responderLoop reads incoming queries and replies whenever the
// question matches our service type. Per RFC 6762 §6 we MAY answer
// via multicast (the spec recommends multicast for unsolicited
// announcements and "shared records" — DNS-SD pointers qualify).
func (a *PureAnnouncer) responderLoop(ctx context.Context, conn *net.UDPConn, serviceType string, answers []Record) {
	buf := make([]byte, 9000)
	for ctx.Err() == nil {
		_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			// Timeout is expected — keeps the loop responsive to ctx.
			continue
		}
		msg, derr := Decode(buf[:n])
		if derr != nil {
			continue
		}
		if !msg.Response {
			// Query — check if any question matches our service type.
			match := false
			for _, q := range msg.Questions {
				if sameName(q.Name, serviceType) && (q.Type == TypePTR || q.Type == TypeANY) {
					match = true
					break
				}
			}
			if !match {
				continue
			}
			if reply, eerr := EncodeResponse(msg.ID, answers); eerr == nil {
				_, _ = conn.WriteToUDP(reply, &net.UDPAddr{IP: mdnsGroupV4, Port: mdnsPort})
			}
		}
	}
}

// firstNonLoopbackV4 picks the first IPv4 address on a UP, non-loopback
// interface. Used as the announcer's A-record default when the operator
// hasn't supplied one explicitly.
func firstNonLoopbackV4() net.IP {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil {
				continue
			}
			return ip4
		}
	}
	return nil
}
