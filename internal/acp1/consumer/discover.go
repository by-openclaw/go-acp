package acp1

import (
	"context"
	"fmt"
	"net"
	"sort"
	"sync"
	"syscall"
	"time"

	"acp/internal/transport"
)

// DiscoverResult is one device seen during a discovery run.
type DiscoverResult struct {
	IP        string
	Port      int
	FirstSeen time.Time
	LastSeen  time.Time
	// NumSlots is filled when the device was successfully queried for
	// its frame status during the scan, zero otherwise.
	NumSlots int
	// Source records how the device was first spotted: "announcement"
	// (passive listener) or "broadcast-reply" (active probe).
	Source string
}

// DiscoverConfig controls one discovery run.
type DiscoverConfig struct {
	// Duration is how long to listen (default 5s).
	Duration time.Duration

	// Active enables the broadcast getValue(FrameStatus) probe so
	// devices that aren't already broadcasting also reply. Without it,
	// discovery only catches devices whose "Broadcasts" setting is On
	// and which happen to emit an announcement during the window.
	Active bool

	// Port is the ACP1 port (default 2071).
	Port int
}

// Discover runs a one-shot LAN scan for ACP1 devices. Works only when
// the host running Discover is on the same subnet as the devices —
// subnet broadcasts do not cross routers (see CLAUDE.md).
//
// Implementation:
//
//  1. Opens an SO_REUSEADDR listener on :Port and spawns a receive
//     goroutine that collects unique source IPs from every incoming
//     UDP datagram for Duration seconds.
//
//  2. If Active is true, opens a second UDP socket with SO_BROADCAST
//     and sends one getValue(FrameStatus, 0) request to the limited
//     broadcast address 255.255.255.255:Port. Every rack controller on
//     the subnet sends back a directed unicast reply which the listener
//     picks up.
//
//  3. After Duration expires, returns the deduplicated results sorted
//     by IP for stable output.
//
// Errors: returns on listener bind failure. Active probe failures are
// logged to stderr but do not abort passive discovery.
func Discover(ctx context.Context, cfg DiscoverConfig) ([]DiscoverResult, error) {
	if cfg.Port == 0 {
		cfg.Port = DefaultPort
	}
	if cfg.Duration == 0 {
		cfg.Duration = 5 * time.Second
	}

	listener, err := transport.ListenUDP(ctx, cfg.Port)
	if err != nil {
		return nil, fmt.Errorf("discover bind :%d: %w", cfg.Port, err)
	}
	defer func() { _ = listener.Close() }()

	// seen maps source IP → collecting result.
	var mu sync.Mutex
	seen := map[string]*DiscoverResult{}

	// Receive goroutine: loop on transport.UDPListener.Receive until
	// the duration expires or ctx is cancelled.
	done := make(chan struct{})
	deadline := time.Now().Add(cfg.Duration)
	go func() {
		defer close(done)
		for {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return
			}
			rxCtx, cancel := context.WithTimeout(ctx, remaining)
			raw, addr, err := listener.Receive(rxCtx, MaxPacket)
			cancel()
			if err != nil {
				// Timeout or ctx cancel → exit loop.
				return
			}
			udpAddr, ok := addr.(*net.UDPAddr)
			if !ok {
				continue
			}
			// Decode to confirm it's valid ACP1. Malformed datagrams
			// from unrelated services on the same port are skipped.
			msg, derr := Decode(raw)
			if derr != nil {
				continue
			}
			// Either an announcement (MTID=0, passive mode) or the
			// unicast reply to our active probe.
			ip := udpAddr.IP.String()
			mu.Lock()
			r, exists := seen[ip]
			if !exists {
				src := "announcement"
				if msg.MTID != 0 {
					src = "broadcast-reply"
				}
				r = &DiscoverResult{
					IP:        ip,
					Port:      cfg.Port,
					FirstSeen: time.Now(),
					Source:    src,
				}
				seen[ip] = r
			}
			r.LastSeen = time.Now()
			// If this is a FrameStatus reply, decode num_slots from it.
			if msg.ObjGroup == GroupFrame && msg.ObjID == 0 && len(msg.Value) >= 1 {
				r.NumSlots = int(msg.Value[0])
			}
			mu.Unlock()
		}
	}()

	// Active probe: send one limited-broadcast getValue(FrameStatus).
	if cfg.Active {
		if perr := probeActive(cfg.Port); perr != nil {
			// Non-fatal: passive scan may still find devices.
			_, _ = fmt.Fprintf(nullWriter{}, "active probe failed: %v\n", perr)
		}
	}

	// Wait for the listener goroutine to drain the window.
	<-done

	mu.Lock()
	defer mu.Unlock()
	out := make([]DiscoverResult, 0, len(seen))
	for _, r := range seen {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IP < out[j].IP })
	return out, nil
}

// probeActive opens a separate UDP socket with SO_BROADCAST enabled and
// sends one getValue(FrameStatus, 0) request to the limited broadcast
// address 255.255.255.255:port. Every rack controller on the subnet
// receives it and replies with a directed unicast reply that the
// listener picks up.
func probeActive(port int) error {
	// net.Dialer with Control hook to set SO_BROADCAST on the socket
	// before bind. The "connect" semantics of UDP don't apply here —
	// we want to send to an unspecified peer.
	d := net.Dialer{
		Control: func(network, address string, c syscall.RawConn) error {
			var opErr error
			if err := c.Control(func(fd uintptr) {
				opErr = transport.SetSocketBroadcast(fd)
			}); err != nil {
				return err
			}
			return opErr
		},
	}
	conn, err := d.Dial("udp4", fmt.Sprintf("255.255.255.255:%d", port))
	if err != nil {
		return fmt.Errorf("dial broadcast: %w", err)
	}
	defer func() { _ = conn.Close() }()

	// Build a getValue(FrameStatus, 0) request. Per spec p. 8 this is
	// "the only broadcast message clients are allowed to invoke".
	req := &Message{
		MTID:     0, // broadcast marker
		PVER:     PVER,
		MType:    MTypeRequest,
		MAddr:    0,
		MCode:    byte(MethodGetValue),
		ObjGroup: GroupFrame,
		ObjID:    0,
	}
	payload, err := req.Encode()
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	if _, err := conn.Write(payload); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	return nil
}

// nullWriter is an io.Writer that discards everything. Used for the
// non-fatal active-probe error log path so we don't pull in the whole
// log/slog plumbing inside the transport layer.
type nullWriter struct{}

func (nullWriter) Write(p []byte) (int, error) { return len(p), nil }
