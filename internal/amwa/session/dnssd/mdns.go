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

// Browser scans the link for instances of a given service. Browse()
// returns a channel that yields one Instance per response observed
// until ctx is cancelled. The same instance may be reported multiple
// times — callers should de-duplicate by FullName().
type Browser struct {
	logger *slog.Logger
	conn4  *net.UDPConn
	mu     sync.Mutex
	closed bool
}

// NewBrowser opens an mDNS receive socket on the IPv4 multicast group.
// IPv6 support is a follow-up; many production switches drop IPv6
// multicast, so we ship IPv4-only first.
func NewBrowser(logger *slog.Logger) (*Browser, error) {
	conn, err := net.ListenMulticastUDP("udp4", nil, &mdnsIPv4)
	if err != nil {
		return nil, fmt.Errorf("dnssd: listen mDNS multicast: %w", err)
	}
	return &Browser{logger: logger, conn4: conn}, nil
}

// Close shuts the receive socket. Safe to call multiple times.
func (b *Browser) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	if b.conn4 != nil {
		return b.conn4.Close()
	}
	return nil
}

// Browse runs a query/listen loop until ctx is cancelled. Discovered
// instances are sent to the returned channel. The channel closes when
// the loop exits.
func (b *Browser) Browse(ctx context.Context, service string) (<-chan dnssd.Instance, error) {
	if service == "" {
		return nil, errors.New("dnssd: empty service in Browse")
	}
	out := make(chan dnssd.Instance, 16)

	// Send an initial query immediately, then on QueryInterval.
	go b.sendQueries(ctx, service)

	go func() {
		defer close(out)
		buf := make([]byte, MaxMDNSPacketSize)
		for {
			if err := b.conn4.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
				return
			}
			n, _, err := b.conn4.ReadFromUDP(buf)
			if ctx.Err() != nil {
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
			for _, ins := range dnssd.DecodeInstances(msg, service) {
				select {
				case out <- ins:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
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
		if _, err := b.conn4.WriteToUDP(qbytes, &mdnsIPv4); err != nil {
			if b.logger != nil && ctx.Err() == nil {
				b.logger.Debug("dnssd: write query", "err", err)
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
	conn4     *net.UDPConn
	mu        sync.Mutex
	instances []dnssd.Instance
	closed    bool
}

// NewResponder opens an mDNS socket and prepares to advertise. Add
// instances with Announce(); they are also re-emitted in answer to
// matching queries.
func NewResponder(logger *slog.Logger) (*Responder, error) {
	conn, err := net.ListenMulticastUDP("udp4", nil, &mdnsIPv4)
	if err != nil {
		return nil, fmt.Errorf("dnssd: listen mDNS multicast: %w", err)
	}
	return &Responder{logger: logger, conn4: conn}, nil
}

// Close shuts the socket; goodbye packets (TTL=0) are emitted for each
// instance per RFC 6762 §10.1 best-effort.
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
			_, _ = r.conn4.WriteToUDP(pkt, &mdnsIPv4)
		}
	}
	return r.conn4.Close()
}

// Announce starts emitting an Instance on the link. The first three
// packets are sent ~1 s apart per RFC 6762 §8.3; thereafter the
// instance is re-emitted in response to PTR queries seen for its
// service.
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
		// Burst three times at ~1 s spacing.
		for i := 0; i < 3; i++ {
			if _, err := r.conn4.WriteToUDP(pkt, &mdnsIPv4); err != nil {
				if r.logger != nil && ctx.Err() == nil {
					r.logger.Debug("dnssd: announce write", "err", err)
				}
				return
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
	buf := make([]byte, MaxMDNSPacketSize)
	for {
		if ctx.Err() != nil {
			return
		}
		if err := r.conn4.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
			return
		}
		n, src, err := r.conn4.ReadFromUDP(buf)
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
					_, _ = r.conn4.WriteToUDP(pkt, src)
				} else {
					_, _ = r.conn4.WriteToUDP(pkt, &mdnsIPv4)
				}
			}
		}
	}
}
