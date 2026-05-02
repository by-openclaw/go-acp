package dnssd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"acp/internal/amwa/codec/dnssd"
)

// mDNS link-local addresses (RFC 6762 §3). IPv6 is staged for a
// follow-up; many production switches drop ff02::fb so IPv4 ships
// first.
var mdnsIPv4 = net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

// MaxMDNSPacketSize is the upper bound for an mDNS UDP payload per
// RFC 6762 §17 (MUST NOT exceed the path MTU; on Ethernet that is
// 1500 minus IP+UDP headers, so 1500 is a safe ceiling for receive).
const MaxMDNSPacketSize = 1500

// QueryInterval is the default browser query cadence (RFC 6762 §5.2 —
// the recommendation is roughly every minute, increasing for quiet
// services).
const QueryInterval = 30 * time.Second

// openMulticastConns returns one mDNS socket per up + multicast + IPv4
// interface so sends and joins use IP_MULTICAST_IF explicitly. Without
// this, Windows often picks a virtual adapter (Tailscale / Hyper-V /
// WSL) and our packets vanish. Falls back to a single nil-interface
// socket if no usable interface is found, so loopback-only test
// environments still work.
func openMulticastConns(logger *slog.Logger) ([]*net.UDPConn, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("dnssd: enumerate interfaces: %w", err)
	}
	var conns []*net.UDPConn
	for i := range ifaces {
		ifi := &ifaces[i]
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		if !hasIPv4(ifi) {
			continue
		}
		c, err := net.ListenMulticastUDP("udp4", ifi, &mdnsIPv4)
		if err != nil {
			if logger != nil {
				logger.Debug("dnssd: bind iface failed", "iface", ifi.Name, "err", err)
			}
			continue
		}
		// RFC 6762 §11 expects link-local loopback delivery; Go's
		// stdlib disables IP_MULTICAST_LOOP on ListenMulticastUDP, so
		// re-enable per platform — required for same-host
		// Node/Controller discovery.
		if err := setMulticastLoopback(c, true); err != nil && logger != nil {
			logger.Debug("dnssd: enable multicast loopback failed", "iface", ifi.Name, "err", err)
		}
		conns = append(conns, c)
		if logger != nil {
			logger.Info("dnssd: mDNS bound", "iface", ifi.Name)
		}
	}
	if len(conns) == 0 {
		c, err := net.ListenMulticastUDP("udp4", nil, &mdnsIPv4)
		if err != nil {
			return nil, fmt.Errorf("dnssd: listen mDNS multicast: %w", err)
		}
		_ = setMulticastLoopback(c, true)
		conns = []*net.UDPConn{c}
		if logger != nil {
			logger.Warn("dnssd: no IPv4 multicast iface — fell back to OS default")
		}
	}
	return conns, nil
}

func hasIPv4(ifi *net.Interface) bool {
	addrs, err := ifi.Addrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
			return true
		}
	}
	return false
}

func closeConns(conns []*net.UDPConn) error {
	var firstErr error
	for _, c := range conns {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Browser scans the link for instances of a given service. Browse()
// returns a channel that yields one Instance per response observed
// until ctx is cancelled. The same instance may be reported multiple
// times — callers should de-duplicate by FullName().
type Browser struct {
	logger  *slog.Logger
	conns   []*net.UDPConn
	mu      sync.Mutex
	closed  bool
	subs    []*browseSub // active Browse subscriptions
	reading bool         // true once readLoop goroutines have started
}

// NewBrowser opens an mDNS receive socket on every up + multicast IPv4
// interface (see openMulticastConns).
func NewBrowser(logger *slog.Logger) (*Browser, error) {
	conns, err := openMulticastConns(logger)
	if err != nil {
		return nil, err
	}
	return &Browser{logger: logger, conns: conns}, nil
}

// Close shuts the receive sockets. Safe to call multiple times.
func (b *Browser) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	return closeConns(b.conns)
}

// Browse runs a query/listen loop until ctx is cancelled. Discovered
// instances are sent to the returned channel. The channel closes when
// every per-interface goroutine exits.
//
// Browse can be called multiple times concurrently on the same Browser
// — each call gets its own filtered channel. Internally a single read
// loop per UDP conn fans every received Instance out to ALL active
// subscriptions, with each subscription filtering by its own service
// name. Sharing one read loop is critical: spinning a separate
// ReadFromUDP goroutine per Browse call would race on the same socket
// (each packet lands in only one reader, the wrong one would filter
// it out and lose it). See `feedback_amwa_strict_all_versions`.
func (b *Browser) Browse(ctx context.Context, service string) (<-chan dnssd.Instance, error) {
	if service == "" {
		return nil, errors.New("dnssd: empty service in Browse")
	}
	out := make(chan dnssd.Instance, 16)
	sub := &browseSub{ctx: ctx, service: service, out: out}

	b.mu.Lock()
	b.subs = append(b.subs, sub)
	first := !b.reading
	if first {
		b.reading = true
	}
	b.mu.Unlock()

	if first {
		for _, c := range b.conns {
			go b.readLoop(c)
		}
	}

	go b.sendQueries(ctx, service)

	go func() {
		<-ctx.Done()
		b.mu.Lock()
		for i, s := range b.subs {
			if s == sub {
				b.subs = append(b.subs[:i], b.subs[i+1:]...)
				break
			}
		}
		b.mu.Unlock()
		close(out)
	}()

	return out, nil
}

// browseSub is one active Browse subscription on a shared Browser.
type browseSub struct {
	ctx     context.Context
	service string
	out     chan dnssd.Instance
}

// readLoop reads mDNS responses from one socket and fans every
// Instance out to every active subscription (each subscription filters
// by its own service name). Runs until the conn is closed.
func (b *Browser) readLoop(c *net.UDPConn) {
	buf := make([]byte, MaxMDNSPacketSize)
	for {
		if err := c.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
			return
		}
		n, _, err := c.ReadFromUDP(buf)
		b.mu.Lock()
		closed := b.closed
		b.mu.Unlock()
		if closed {
			return
		}
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			if b.logger != nil {
				b.logger.Debug("dnssd: read error", "err", err)
			}
			return
		}
		msg, err := dnssd.Decode(buf[:n])
		if err != nil {
			if b.logger != nil {
				b.logger.Debug("dnssd: decode error", "err", err, "len", n)
			}
			continue
		}
		if !msg.Header.IsResponse() {
			continue
		}
		// Snapshot the subs slice under lock — sub list may grow / shrink
		// concurrently as Browse calls come and go.
		b.mu.Lock()
		subs := make([]*browseSub, len(b.subs))
		copy(subs, b.subs)
		b.mu.Unlock()
		for _, sub := range subs {
			for _, ins := range dnssd.DecodeInstances(msg, sub.service) {
				select {
				case sub.out <- ins:
				case <-sub.ctx.Done():
				}
			}
		}
	}
}

func (b *Browser) sendQueries(ctx context.Context, service string) {
	send := func() {
		qbytes, err := dnssd.EncodeQuery(service+"."+dnssd.DefaultDomain, dnssd.TypePTR, false)
		if err != nil {
			if b.logger != nil {
				b.logger.Debug("dnssd: encode query", "err", err)
			}
			return
		}
		for _, c := range b.conns {
			if _, err := c.WriteToUDP(qbytes, &mdnsIPv4); err != nil {
				if b.logger != nil && ctx.Err() == nil {
					b.logger.Debug("dnssd: write query", "err", err)
				}
			}
		}
	}
	send()
	t := time.NewTicker(QueryInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			send()
		}
	}
}

// Responder advertises one or more Instances on the link, replying to
// queries and emitting unsolicited announcements per RFC 6762 §8.3.
type Responder struct {
	logger    *slog.Logger
	conns     []*net.UDPConn
	mu        sync.Mutex
	instances []dnssd.Instance
	closed    bool
}

// NewResponder opens an mDNS socket on every up + multicast IPv4
// interface (see openMulticastConns).
func NewResponder(logger *slog.Logger) (*Responder, error) {
	conns, err := openMulticastConns(logger)
	if err != nil {
		return nil, err
	}
	return &Responder{logger: logger, conns: conns}, nil
}

// Close emits goodbye packets (TTL=0) on every interface per RFC 6762
// §10.1 best-effort, then shuts the sockets.
func (r *Responder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	for _, ins := range r.instances {
		bye := ins
		bye.TTL = 0
		if pkt, err := dnssd.EncodeAnnounce(bye, true); err == nil {
			for _, c := range r.conns {
				_, _ = c.WriteToUDP(pkt, &mdnsIPv4)
			}
		}
	}
	return closeConns(r.conns)
}

// Announce starts emitting an Instance on the link. The first three
// packets are sent ~1 s apart per RFC 6762 §8.3 on every bound
// interface; thereafter the instance is re-emitted in response to
// matching queries.
func (r *Responder) Announce(ctx context.Context, ins dnssd.Instance) error {
	if ins.Name == "" || ins.Service == "" {
		return errors.New("dnssd: Announce requires Name and Service")
	}
	r.mu.Lock()
	r.instances = append(r.instances, ins)
	r.mu.Unlock()

	pkt, err := dnssd.EncodeAnnounce(ins, true)
	if err != nil {
		return err
	}
	go func() {
		for i := 0; i < 3; i++ {
			for _, c := range r.conns {
				if _, err := c.WriteToUDP(pkt, &mdnsIPv4); err != nil {
					if r.logger != nil && ctx.Err() == nil {
						r.logger.Debug("dnssd: announce write", "err", err)
					}
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}()

	go r.serveQueries(ctx)
	return nil
}

func (r *Responder) serveQueries(ctx context.Context) {
	var wg sync.WaitGroup
	for _, c := range r.conns {
		wg.Add(1)
		go func(c *net.UDPConn) {
			defer wg.Done()
			buf := make([]byte, MaxMDNSPacketSize)
			for {
				if ctx.Err() != nil {
					return
				}
				if err := c.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
					return
				}
				n, src, err := c.ReadFromUDP(buf)
				if ctx.Err() != nil {
					return
				}
				if err != nil {
					if ne, ok := err.(net.Error); ok && ne.Timeout() {
						continue
					}
					return
				}
				msg, err := dnssd.Decode(buf[:n])
				if err != nil {
					continue
				}
				if msg.Header.IsResponse() {
					continue
				}
				for _, q := range msg.Questions {
					r.mu.Lock()
					matches := make([]dnssd.Instance, 0, len(r.instances))
					for _, ins := range r.instances {
						if q.Name == ins.PTRName() && (q.Type == dnssd.TypePTR || q.Type == dnssd.TypeANY) {
							matches = append(matches, ins)
						}
					}
					r.mu.Unlock()
					for _, ins := range matches {
						pkt, err := dnssd.EncodeAnnounce(ins, true)
						if err != nil {
							continue
						}
						if q.Class&dnssd.ClassUnicastBit != 0 {
							_, _ = c.WriteToUDP(pkt, src)
						} else {
							_, _ = c.WriteToUDP(pkt, &mdnsIPv4)
						}
					}
				}
			}
		}(c)
	}
	wg.Wait()
}
