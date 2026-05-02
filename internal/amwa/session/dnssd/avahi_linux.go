//go:build linux

// Package dnssd — Avahi-via-DBus backend (Linux).
//
// Wire reference: https://github.com/lathiat/avahi/blob/master/avahi-daemon/org.freedesktop.Avahi.Server.xml
//
// We talk to avahi-daemon over the system DBus on the well-known bus
// name `org.freedesktop.Avahi`. The interesting interfaces:
//
//   - org.freedesktop.Avahi.Server      — the daemon root
//   - org.freedesktop.Avahi.ServiceBrowser  — one per Browse subscription
//   - org.freedesktop.Avahi.ServiceResolver — resolves an instance to host+port+TXT
//   - org.freedesktop.Avahi.EntryGroup   — one per Announce / advertised group
//
// Per RFC 6762/6763 + AMWA test_05/15/16, sub-millisecond cascade
// detection requires the daemon's kernel-callback delivery; our stdlib
// fallback's 500 ms read deadline misses cascade windows under tight
// AMWA cycles. nmos-cpp uses the equivalent path via Bonjour
// (Development/mdns/service_advertiser_impl.cpp); we mirror it here
// using DBus on Linux.

package dnssd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"

	"acp/internal/amwa/codec/dnssd"

	"github.com/godbus/dbus/v5"
)

const (
	avahiBusName     = "org.freedesktop.Avahi"
	avahiServerPath  = "/"
	avahiServerIface = "org.freedesktop.Avahi.Server"
	avahiBrowserIf   = "org.freedesktop.Avahi.ServiceBrowser"
	avahiResolverIf  = "org.freedesktop.Avahi.ServiceResolver"
	avahiEntryIf     = "org.freedesktop.Avahi.EntryGroup"

	// Avahi protocol constants — from avahi-common/defs.h.
	avahiIfaceUnspec    = int32(-1) // AVAHI_IF_UNSPEC — every iface
	avahiProtoUnspec    = int32(-1) // AVAHI_PROTO_UNSPEC — every proto
	avahiProtoIPv4      = int32(0)  // AVAHI_PROTO_INET
	avahiLookupNoFlags  = uint32(0)
	avahiPubNoFlags     = uint32(0)
)

// tryDaemonBrowser — Linux: probe avahi-daemon via DBus; pick it if
// reachable, signal a fallback otherwise.
func tryDaemonBrowser(logger *slog.Logger) (Browser, bool) {
	conn, err := dbus.SystemBus()
	if err != nil {
		logger.Debug("dnssd: no system DBus — skipping Avahi", "err", err)
		return nil, false
	}
	if !pingAvahi(conn) {
		logger.Debug("dnssd: avahi-daemon not reachable on system bus — falling back to stdlib")
		return nil, false
	}
	logger.Info("dnssd: using Avahi via DBus (system daemon)")
	return newAvahiBrowser(logger, conn), true
}

// tryDaemonResponder mirrors tryDaemonBrowser.
func tryDaemonResponder(logger *slog.Logger) (Responder, bool) {
	conn, err := dbus.SystemBus()
	if err != nil {
		logger.Debug("dnssd: no system DBus — skipping Avahi", "err", err)
		return nil, false
	}
	if !pingAvahi(conn) {
		logger.Debug("dnssd: avahi-daemon not reachable on system bus — falling back to stdlib")
		return nil, false
	}
	logger.Info("dnssd: using Avahi via DBus (system daemon)")
	return newAvahiResponder(logger, conn), true
}

// pingAvahi returns true when the Avahi server bus name is owned, which
// means the daemon is up and accepting calls.
func pingAvahi(conn *dbus.Conn) bool {
	var version string
	obj := conn.Object(avahiBusName, dbus.ObjectPath(avahiServerPath))
	call := obj.Call(avahiServerIface+".GetVersionString", 0)
	if call.Err != nil {
		return false
	}
	if err := call.Store(&version); err != nil {
		return false
	}
	return version != ""
}

// avahiBrowser implements [Browser] by spawning one
// `org.freedesktop.Avahi.ServiceBrowser` object per Browse() call and
// translating its `ItemNew` / `ItemRemove` signals into a stream of
// resolved [dnssd.Instance].
type avahiBrowser struct {
	logger *slog.Logger
	conn   *dbus.Conn

	mu     sync.Mutex
	closed bool
	subs   []*avahiBrowseSub
}

type avahiBrowseSub struct {
	browserPath dbus.ObjectPath
	out         chan dnssd.Instance
	cancel      context.CancelFunc
}

func newAvahiBrowser(logger *slog.Logger, conn *dbus.Conn) *avahiBrowser {
	return &avahiBrowser{logger: logger, conn: conn}
}

// Browse spawns an Avahi ServiceBrowser for the requested service type
// (e.g. `_nmos-register._tcp`) and returns a channel that yields one
// [dnssd.Instance] per ItemNew signal, after resolving it to host+port.
func (b *avahiBrowser) Browse(ctx context.Context, service string) (<-chan dnssd.Instance, error) {
	if service == "" {
		return nil, errors.New("dnssd: empty service in Browse")
	}

	subCtx, cancel := context.WithCancel(ctx)
	out := make(chan dnssd.Instance, 16)

	// 1. Ask the Avahi server for a new ServiceBrowser object.
	server := b.conn.Object(avahiBusName, dbus.ObjectPath(avahiServerPath))
	var browserPath dbus.ObjectPath
	domain := dnssd.DefaultDomain // "local"
	if err := server.Call(
		avahiServerIface+".ServiceBrowserNew", 0,
		avahiIfaceUnspec, avahiProtoIPv4,
		service, domain, avahiLookupNoFlags,
	).Store(&browserPath); err != nil {
		cancel()
		return nil, fmt.Errorf("dnssd: Avahi ServiceBrowserNew(%s): %w", service, err)
	}

	// 2. Subscribe to ItemNew signals on this browser's object path.
	matchRule := fmt.Sprintf(
		"type='signal',sender='%s',interface='%s',path='%s',member='ItemNew'",
		avahiBusName, avahiBrowserIf, browserPath,
	)
	if err := b.conn.AddMatchSignal(
		dbus.WithMatchObjectPath(browserPath),
		dbus.WithMatchInterface(avahiBrowserIf),
		dbus.WithMatchMember("ItemNew"),
	); err != nil {
		cancel()
		return nil, fmt.Errorf("dnssd: Avahi AddMatch ItemNew: %w (rule=%s)", err, matchRule)
	}

	signals := make(chan *dbus.Signal, 32)
	b.conn.Signal(signals)

	sub := &avahiBrowseSub{browserPath: browserPath, out: out, cancel: cancel}
	b.mu.Lock()
	b.subs = append(b.subs, sub)
	b.mu.Unlock()

	// 3. Goroutine: dispatch ItemNew signals → resolve → emit Instances.
	go b.dispatch(subCtx, sub, signals, service, domain)

	// 4. Tear down on context cancel.
	go func() {
		<-subCtx.Done()
		b.removeSub(sub)
		close(out)
		// Best-effort free the browser server-side.
		_ = b.conn.Object(avahiBusName, browserPath).Call(avahiBrowserIf+".Free", 0).Store()
	}()

	return out, nil
}

// dispatch translates Avahi ItemNew signals into resolved Instances.
func (b *avahiBrowser) dispatch(
	ctx context.Context, sub *avahiBrowseSub, signals chan *dbus.Signal,
	service, domain string,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case sig, ok := <-signals:
			if !ok {
				return
			}
			if sig.Path != sub.browserPath || sig.Name != avahiBrowserIf+".ItemNew" {
				continue
			}
			ins, err := b.resolveItemNew(ctx, sig, service, domain)
			if err != nil {
				if b.logger != nil {
					b.logger.Debug("dnssd: avahi resolve failed",
						"err", err, "service", service, "path", sub.browserPath)
				}
				continue
			}
			select {
			case sub.out <- ins:
			case <-ctx.Done():
				return
			}
		}
	}
}

// resolveItemNew calls ServiceResolverNew on the daemon, reads the
// resolved tuple (host, port, TXT), and projects it into a
// [dnssd.Instance]. Avahi's ItemNew signal carries:
//
//	(int32 iface, int32 proto, string name, string type, string domain, uint32 flags)
//
// We then call ServiceResolverNew with the same identifiers to fetch
// the host:port and TXT records. The synchronous Resolve method
// (`ResolveService`) is simpler than spinning a transient resolver +
// Found signal pair — we use it for clarity and speed.
func (b *avahiBrowser) resolveItemNew(
	ctx context.Context, sig *dbus.Signal, service, domain string,
) (dnssd.Instance, error) {
	if len(sig.Body) < 6 {
		return dnssd.Instance{}, fmt.Errorf("avahi ItemNew: unexpected body len=%d", len(sig.Body))
	}
	iface, _ := sig.Body[0].(int32)
	proto, _ := sig.Body[1].(int32)
	name, _ := sig.Body[2].(string)

	server := b.conn.Object(avahiBusName, dbus.ObjectPath(avahiServerPath))

	// Synchronous resolver call; returns:
	// (int32, int32, string, string, string, string, int32, string, uint16, [][]byte, uint32)
	// = (interface, protocol, name, type, domain, host, aprotocol, address,
	//    port, txt, flags)
	var (
		rIface  int32
		rProto  int32
		rName   string
		rType   string
		rDomain string
		rHost   string
		rAProto int32
		rAddr   string
		rPort   uint16
		rTXT    [][]byte
		rFlags  uint32
	)
	if err := server.CallWithContext(
		ctx, avahiServerIface+".ResolveService", 0,
		iface, proto, name, service, domain,
		avahiProtoUnspec, avahiLookupNoFlags,
	).Store(&rIface, &rProto, &rName, &rType, &rDomain,
		&rHost, &rAProto, &rAddr, &rPort, &rTXT, &rFlags); err != nil {
		return dnssd.Instance{}, fmt.Errorf("avahi ResolveService(%s.%s.%s): %w",
			rName, service, domain, err)
	}

	// Avahi TXT records are RFC 6763 "key=value" byte arrays.
	txt := decodeAvahiTXT(rTXT)

	ins := dnssd.Instance{
		Name:    rName,
		Service: service,
		Domain:  rDomain,
		Host:    rHost,
		Port:    rPort,
		TXT:     txt,
	}
	if ip := net.ParseIP(rAddr); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			ins.IPv4 = []net.IP{ip4}
		}
	}
	return ins, nil
}

// removeSub drops a sub from the active list.
func (b *avahiBrowser) removeSub(sub *avahiBrowseSub) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, s := range b.subs {
		if s == sub {
			b.subs = append(b.subs[:i], b.subs[i+1:]...)
			break
		}
	}
}

// Close cancels every active subscription and tears down the daemon
// objects. Safe to call multiple times.
func (b *avahiBrowser) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	subs := b.subs
	b.subs = nil
	b.mu.Unlock()

	for _, s := range subs {
		s.cancel()
	}
	return nil
}

// avahiResponder implements [Responder] using one Avahi EntryGroup per
// announced [dnssd.Instance]. Avahi's daemon handles the
// probe-then-claim conflict resolution (RFC 6762 §8) automatically.
type avahiResponder struct {
	logger *slog.Logger
	conn   *dbus.Conn

	mu     sync.Mutex
	closed bool
	// groups maps [dnssd.Instance.FullName] → EntryGroup path so
	// Update can locate the right group to call UpdateServiceTxt on.
	groups map[string]avahiGroup
}

// avahiGroup records the EntryGroup path plus the AddService argument
// triple (name, service, domain) needed by UpdateServiceTxt — Avahi's
// API requires the same identifying triple on the update call.
type avahiGroup struct {
	path    dbus.ObjectPath
	name    string
	service string
	domain  string
}

func newAvahiResponder(logger *slog.Logger, conn *dbus.Conn) *avahiResponder {
	return &avahiResponder{logger: logger, conn: conn, groups: map[string]avahiGroup{}}
}

// Announce creates a fresh EntryGroup for the Instance, AddService's
// the SRV+TXT, and Commit's the group. The daemon then takes over —
// announcing per RFC 6762 §8.3 and replying to queries until Close().
func (r *avahiResponder) Announce(ctx context.Context, ins dnssd.Instance) error {
	if ins.Name == "" || ins.Service == "" {
		return errors.New("dnssd: Announce requires Name and Service")
	}
	server := r.conn.Object(avahiBusName, dbus.ObjectPath(avahiServerPath))
	var groupPath dbus.ObjectPath
	if err := server.Call(avahiServerIface+".EntryGroupNew", 0).Store(&groupPath); err != nil {
		return fmt.Errorf("dnssd: Avahi EntryGroupNew: %w", err)
	}

	group := r.conn.Object(avahiBusName, groupPath)
	domain := ins.Domain
	if domain == "" {
		domain = dnssd.DefaultDomain
	}

	// Avahi convention: empty host string ⇒ daemon uses the system
	// hostname (whatever it advertises via the A record on `<host>.local`).
	// A non-empty value MUST be a fully-qualified `.local` name; passing
	// a bare label like "dhs-node" trips "Invalid host name". For an
	// in-tree Instance.Host that already ends in `.local` we pass it
	// through; everything else is normalised to empty so Avahi picks
	// the canonical name.
	host := strings.TrimSuffix(ins.Host, ".")
	if !strings.HasSuffix(host, ".local") {
		host = ""
	}

	if err := group.Call(
		avahiEntryIf+".AddService", 0,
		avahiIfaceUnspec, avahiProtoIPv4, avahiPubNoFlags,
		ins.Name, ins.Service, domain,
		host, ins.Port,
		encodeAvahiTXT(ins.TXT),
	).Store(); err != nil {
		_ = group.Call(avahiEntryIf+".Free", 0).Store()
		return fmt.Errorf("dnssd: Avahi AddService(%s.%s.%s): %w",
			ins.Name, ins.Service, domain, err)
	}

	if err := group.Call(avahiEntryIf+".Commit", 0).Store(); err != nil {
		_ = group.Call(avahiEntryIf+".Free", 0).Store()
		return fmt.Errorf("dnssd: Avahi EntryGroup.Commit: %w", err)
	}

	r.mu.Lock()
	if r.groups == nil {
		r.groups = map[string]avahiGroup{}
	}
	r.groups[ins.FullName()] = avahiGroup{
		path: groupPath, name: ins.Name, service: ins.Service, domain: domain,
	}
	r.mu.Unlock()
	return nil
}

// Update calls EntryGroup.UpdateServiceTxt on the matching group so the
// daemon swaps the TXT records in-place and re-announces per RFC 6762
// §10.2 (cache-flush). Required for IS-04 §3.1.1 P2P Node `ver_*`
// counter bumps; see internal/amwa/provider/node.go bumpResourceVersion.
func (r *avahiResponder) Update(ctx context.Context, ins dnssd.Instance) error {
	if ins.Name == "" || ins.Service == "" {
		return errors.New("dnssd: Update requires Name and Service")
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return errors.New("dnssd: responder closed")
	}
	g, ok := r.groups[ins.FullName()]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("dnssd: Update: instance %q not announced", ins.FullName())
	}
	group := r.conn.Object(avahiBusName, g.path)
	if err := group.Call(
		avahiEntryIf+".UpdateServiceTxt", 0,
		avahiIfaceUnspec, avahiProtoIPv4, avahiPubNoFlags,
		g.name, g.service, g.domain,
		encodeAvahiTXT(ins.TXT),
	).Store(); err != nil {
		return fmt.Errorf("dnssd: Avahi UpdateServiceTxt(%s.%s.%s): %w",
			g.name, g.service, g.domain, err)
	}
	return nil
}

// Close frees every EntryGroup so the daemon emits goodbye packets
// (TTL=0) per RFC 6762 §10.1, then drops references.
func (r *avahiResponder) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	groups := r.groups
	r.groups = nil
	r.mu.Unlock()

	var firstErr error
	for _, g := range groups {
		if err := r.conn.Object(avahiBusName, g.path).Call(avahiEntryIf+".Free", 0).Store(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// decodeAvahiTXT turns Avahi's [][]byte representation of TXT into the
// in-tree map[string]string form. Each segment is "key" or "key=value";
// the empty segment is skipped.
func decodeAvahiTXT(raw [][]byte) map[string]string {
	out := make(map[string]string, len(raw))
	for _, b := range raw {
		s := string(b)
		if s == "" {
			continue
		}
		if i := strings.IndexByte(s, '='); i >= 0 {
			out[s[:i]] = s[i+1:]
		} else {
			out[s] = ""
		}
	}
	return out
}

// encodeAvahiTXT is the inverse — turn a map into the [][]byte Avahi
// expects on AddService.
func encodeAvahiTXT(m map[string]string) [][]byte {
	out := make([][]byte, 0, len(m))
	for k, v := range m {
		if v == "" {
			out = append(out, []byte(k))
		} else {
			out = append(out, []byte(k+"="+v))
		}
	}
	return out
}
