//go:build linux

package dnssd

// A scripted avahi-daemon.
//
// The Avahi backend's whole job is translation: DBus signals into
// [dnssd.Instance], an Instance into an EntryGroup, a Close into RFC 6762
// goodbye packets on the wire. None of that is reachable without a running
// daemon on a system bus, which no CI runner has and no developer laptop
// reliably has either — so the rules were pinned by nothing. These tests
// hand the backend a bus that answers from a table.

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/amwa/codec/dnssd"

	"github.com/godbus/dbus/v5"
)

// ---------------------------------------------------------------------
// the scripted bus
// ---------------------------------------------------------------------

// busCall is one method invocation, recorded so a test can assert what
// the backend asked the daemon to do and with which arguments.
type busCall struct {
	path   dbus.ObjectPath
	method string
	args   []any
}

// scriptedBus answers DBus calls from a table keyed by the method's
// last segment ("AddService", "Commit", …). A method with no entry
// succeeds with an empty body, which is what Avahi's void methods
// return.
type scriptedBus struct {
	mu       sync.Mutex
	replies  map[string]*dbus.Call
	calls    []busCall
	matchErr error
	signals  chan<- *dbus.Signal
}

func newBus() *scriptedBus {
	return &scriptedBus{replies: map[string]*dbus.Call{
		"GetVersionString":  {Body: []any{"0.8"}},
		"ServiceBrowserNew": {Body: []any{dbus.ObjectPath("/Client1/ServiceBrowser1")}},
		"EntryGroupNew":     {Body: []any{dbus.ObjectPath("/Client1/EntryGroup1")}},
		"ResolveService":    resolveReply("registry-a", "reg.local", "10.0.0.7", 8080, "api_proto=http"),
	}}
}

// resolveReply is the 11-value tuple Avahi's Server.ResolveService
// answers with.
func resolveReply(name, host, addr string, port uint16, txt ...string) *dbus.Call {
	raw := make([][]byte, 0, len(txt))
	for _, s := range txt {
		raw = append(raw, []byte(s))
	}
	return &dbus.Call{Body: []any{
		int32(2), int32(0), name, "_nmos-register._tcp", "local",
		host, int32(0), addr, port, raw, uint32(0),
	}}
}

func (b *scriptedBus) reply(method string, c *dbus.Call) *scriptedBus {
	b.replies[method] = c
	return b
}

func (b *scriptedBus) fail(method string, err error) *scriptedBus {
	return b.reply(method, &dbus.Call{Err: err})
}

func (b *scriptedBus) Object(_ string, path dbus.ObjectPath) dbus.BusObject {
	return &scriptedObject{bus: b, path: path}
}

func (b *scriptedBus) AddMatchSignal(...dbus.MatchOption) error { return b.matchErr }

func (b *scriptedBus) Signal(ch chan<- *dbus.Signal) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.signals = ch
}

// emit delivers one signal to whatever channel the backend registered,
// waiting for it to appear — Browse registers it before it returns, but
// a test that races the constructor would otherwise drop the signal.
func (b *scriptedBus) emit(t *testing.T, sig *dbus.Signal) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		b.mu.Lock()
		ch := b.signals
		b.mu.Unlock()
		if ch != nil {
			ch <- sig
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the backend never registered a signal channel")
		}
		time.Sleep(time.Millisecond)
	}
}

// closeSignals is the bus going away: dbus.Conn closes every registered
// signal channel when the connection drops.
func (b *scriptedBus) closeSignals() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.signals != nil {
		close(b.signals)
		b.signals = nil
	}
}

func (b *scriptedBus) seen(method string) []busCall {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []busCall
	for _, c := range b.calls {
		if strings.HasSuffix(c.method, "."+method) {
			out = append(out, c)
		}
	}
	return out
}

type scriptedObject struct {
	bus  *scriptedBus
	path dbus.ObjectPath
}

func (o *scriptedObject) Call(method string, _ dbus.Flags, args ...any) *dbus.Call {
	o.bus.mu.Lock()
	o.bus.calls = append(o.bus.calls, busCall{path: o.path, method: method, args: args})
	reply := o.bus.replies[method[strings.LastIndexByte(method, '.')+1:]]
	o.bus.mu.Unlock()
	if reply == nil {
		return &dbus.Call{}
	}
	return reply
}

func (o *scriptedObject) CallWithContext(_ context.Context, m string, f dbus.Flags, a ...any) *dbus.Call {
	return o.Call(m, f, a...)
}

// The rest of dbus.BusObject. The backend calls none of them; they are
// here because the interface is the daemon's, not ours.
func (o *scriptedObject) Go(string, dbus.Flags, chan *dbus.Call, ...any) *dbus.Call { return nil }
func (o *scriptedObject) GoWithContext(context.Context, string, dbus.Flags, chan *dbus.Call, ...any) *dbus.Call {
	return nil
}
func (o *scriptedObject) AddMatchSignal(string, string, ...dbus.MatchOption) *dbus.Call { return nil }
func (o *scriptedObject) RemoveMatchSignal(string, string, ...dbus.MatchOption) *dbus.Call {
	return nil
}
func (o *scriptedObject) GetProperty(string) (dbus.Variant, error) { return dbus.Variant{}, nil }
func (o *scriptedObject) StoreProperty(string, any) error          { return nil }
func (o *scriptedObject) SetProperty(string, any) error            { return nil }
func (o *scriptedObject) Destination() string                      { return avahiBusName }
func (o *scriptedObject) Path() dbus.ObjectPath                    { return o.path }

// itemNew is the signal Avahi raises when a browser sees an instance.
func itemNew(path dbus.ObjectPath, name string) *dbus.Signal {
	return &dbus.Signal{
		Path: path,
		Name: avahiBrowserIf + ".ItemNew",
		Body: []any{int32(2), int32(0), name, "_nmos-register._tcp", "local", uint32(0)},
	}
}

const browserPath = dbus.ObjectPath("/Client1/ServiceBrowser1")

// ---------------------------------------------------------------------
// daemon detection
// ---------------------------------------------------------------------

// The daemon is used when it answers and skipped when it does not — a
// backend that picked Avahi on a host without it would take every
// discovery down with it rather than falling back to stdlib.
func TestDaemonProbe(t *testing.T) {
	for _, tc := range []struct {
		name string
		bus  func() (avahiBus, error)
		want bool
	}{
		{"no system bus", func() (avahiBus, error) { return nil, errors.New("no DBUS_SYSTEM_BUS_ADDRESS") }, false},
		{"bus but no daemon", func() (avahiBus, error) {
			return newBus().fail("GetVersionString", errors.New("name not owned")), nil
		}, false},
		{"a daemon that answers with nothing", func() (avahiBus, error) {
			return newBus().reply("GetVersionString", &dbus.Call{Body: []any{""}}), nil
		}, false},
		{"a daemon that answers with no value at all", func() (avahiBus, error) {
			return newBus().reply("GetVersionString", &dbus.Call{Body: []any{}}), nil
		}, false},
		{"a daemon", func() (avahiBus, error) { return newBus(), nil }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prev := systemBus
			systemBus = tc.bus
			t.Cleanup(func() { systemBus = prev })

			br, okB := tryDaemonBrowser(discardLogger())
			rs, okR := tryDaemonResponder(discardLogger())
			if okB != tc.want || okR != tc.want {
				t.Fatalf("browser=%v responder=%v, want %v", okB, okR, tc.want)
			}
			if tc.want {
				if br == nil || rs == nil {
					t.Fatal("a reachable daemon must produce both backends")
				}
				_ = br.Close()
				_ = rs.Close()
			}
		})
	}
}

// ---------------------------------------------------------------------
// browse
// ---------------------------------------------------------------------

// An ItemNew signal becomes a resolved Instance: the daemon reports the
// browse hit, we resolve it to host+port+TXT, and that is what the
// caller receives.
func TestBrowseResolvesAnItemNew(t *testing.T) {
	bus := newBus()
	b := newAvahiBrowser(discardLogger(), bus)
	t.Cleanup(func() { _ = b.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out, err := b.Browse(ctx, "_nmos-register._tcp")
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	bus.emit(t, itemNew(browserPath, "registry-a"))

	select {
	case ins := <-out:
		if ins.Name != "registry-a" || ins.Host != "reg.local" || ins.Port != 8080 {
			t.Errorf("instance = %+v", ins)
		}
		if ins.TXT["api_proto"] != "http" {
			t.Errorf("TXT = %v, want the resolved records", ins.TXT)
		}
		if len(ins.IPv4) != 1 || !ins.IPv4[0].Equal(net.IPv4(10, 0, 0, 7)) {
			t.Errorf("IPv4 = %v, want the resolved address", ins.IPv4)
		}
		// ItemNew means alive: Avahi never surfaces record TTLs, and a
		// zero TTL has to keep meaning "goodbye" across every backend.
		if ins.TTL != dnssd.DefaultAnnounceTTL {
			t.Errorf("TTL = %d, want the default", ins.TTL)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no instance arrived")
	}
}

// A resolved address that is not IPv4 leaves IPv4 empty rather than
// filing a v6 address in a v4 field.
func TestBrowseAddressHandling(t *testing.T) {
	for _, tc := range []struct{ name, addr string }{
		{"an address that does not parse", "not-an-address"},
		{"an IPv6 address", "fe80::1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bus := newBus().reply("ResolveService",
				resolveReply("registry-a", "reg.local", tc.addr, 8080))
			b := newAvahiBrowser(discardLogger(), bus)
			t.Cleanup(func() { _ = b.Close() })

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			out, err := b.Browse(ctx, "_nmos-register._tcp")
			if err != nil {
				t.Fatalf("Browse: %v", err)
			}
			bus.emit(t, itemNew(browserPath, "registry-a"))

			select {
			case ins := <-out:
				if len(ins.IPv4) != 0 {
					t.Errorf("IPv4 = %v, want none", ins.IPv4)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("no instance arrived")
			}
		})
	}
}

// Signals that are not this browser's ItemNew are ignored: one DBus
// connection carries every subscription on the host, so the channel sees
// traffic that belongs to somebody else.
func TestBrowseIgnoresForeignSignals(t *testing.T) {
	bus := newBus()
	b := newAvahiBrowser(discardLogger(), bus)
	t.Cleanup(func() { _ = b.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out, err := b.Browse(ctx, "_nmos-register._tcp")
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}

	bus.emit(t, &dbus.Signal{Path: "/Client9/ServiceBrowser9", Name: avahiBrowserIf + ".ItemNew"})
	bus.emit(t, &dbus.Signal{Path: browserPath, Name: avahiBrowserIf + ".ItemRemove"})
	bus.emit(t, itemNew(browserPath, "registry-a"))

	select {
	case ins := <-out:
		if ins.Name != "registry-a" {
			t.Errorf("a foreign signal was dispatched: %+v", ins)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the browser's own signal never arrived")
	}
}

// A resolve that fails is logged and skipped, not fatal: one instance the
// daemon cannot resolve must not stop the browse that finds the others.
// The same for a signal whose body is not the documented six-tuple.
func TestBrowseSkipsWhatItCannotResolve(t *testing.T) {
	for _, tc := range []struct {
		name   string
		bus    *scriptedBus
		signal *dbus.Signal
		logger bool
	}{
		{"a resolver that refuses", newBus().fail("ResolveService", errors.New("timeout")),
			itemNew(browserPath, "registry-a"), true},
		{"a short signal body", newBus(),
			&dbus.Signal{Path: browserPath, Name: avahiBrowserIf + ".ItemNew", Body: []any{int32(2)}}, true},
		{"and with no logger at all", newBus().fail("ResolveService", errors.New("timeout")),
			itemNew(browserPath, "registry-a"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b *avahiBrowser
			if tc.logger {
				b = newAvahiBrowser(discardLogger(), tc.bus)
			} else {
				b = newAvahiBrowser(nil, tc.bus)
			}
			t.Cleanup(func() { _ = b.Close() })

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			out, err := b.Browse(ctx, "_nmos-register._tcp")
			if err != nil {
				t.Fatalf("Browse: %v", err)
			}
			tc.bus.emit(t, tc.signal)

			select {
			case ins, ok := <-out:
				if ok {
					t.Fatalf("an unresolvable item was emitted: %+v", ins)
				}
			case <-time.After(150 * time.Millisecond):
				// Nothing emitted, which is the point.
			}
		})
	}
}

// Browse refuses the shapes it cannot act on, and reports the daemon's
// own refusals rather than returning a channel that never yields.
func TestBrowseRefusals(t *testing.T) {
	t.Run("an empty service", func(t *testing.T) {
		b := newAvahiBrowser(discardLogger(), newBus())
		if _, err := b.Browse(context.Background(), ""); err == nil {
			t.Error("an empty service type must be refused")
		}
	})

	t.Run("a daemon that will not open a browser", func(t *testing.T) {
		bus := newBus().fail("ServiceBrowserNew", errors.New("invalid service type"))
		b := newAvahiBrowser(discardLogger(), bus)
		_, err := b.Browse(context.Background(), "_nmos-register._tcp")
		if err == nil || !strings.Contains(err.Error(), "ServiceBrowserNew") {
			t.Errorf("= %v, want the daemon's refusal", err)
		}
	})

	t.Run("a bus that will not route the signal", func(t *testing.T) {
		bus := newBus()
		bus.matchErr = errors.New("match rule rejected")
		b := newAvahiBrowser(discardLogger(), bus)
		_, err := b.Browse(context.Background(), "_nmos-register._tcp")
		if err == nil || !strings.Contains(err.Error(), "AddMatch ItemNew") {
			t.Errorf("= %v, want the match failure", err)
		}
	})
}

// Cancelling the browse closes the caller's channel and frees the
// browser object on the daemon — a leaked ServiceBrowser keeps the
// daemon resolving for a caller that has gone.
func TestBrowseTeardownFreesTheDaemonObject(t *testing.T) {
	bus := newBus()
	b := newAvahiBrowser(discardLogger(), bus)
	t.Cleanup(func() { _ = b.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	out, err := b.Browse(ctx, "_nmos-register._tcp")
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	cancel()

	select {
	case _, ok := <-out:
		if ok {
			t.Error("the channel must close, not yield")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the channel was never closed")
	}
	waitFor(t, func() bool { return len(bus.seen("Free")) == 1 })
}

// Close cancels every live subscription, and is safe to call twice — a
// Node tearing down calls it from more than one place.
func TestBrowserCloseIsIdempotent(t *testing.T) {
	bus := newBus()
	b := newAvahiBrowser(discardLogger(), bus)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out, err := b.Browse(ctx, "_nmos-register._tcp")
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	select {
	case _, ok := <-out:
		if ok {
			t.Error("Close must end the subscription")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not end the subscription")
	}
}

// removeSub tolerates a sub that is already gone: Close and the
// per-subscription teardown goroutine both call it, and either can win.
func TestRemoveSubOfSomethingAlreadyGone(t *testing.T) {
	b := newAvahiBrowser(discardLogger(), newBus())
	live := &avahiBrowseSub{browserPath: "/a"}
	b.subs = []*avahiBrowseSub{live}

	b.removeSub(&avahiBrowseSub{browserPath: "/b"})
	if len(b.subs) != 1 {
		t.Errorf("subs = %d, want the live one untouched", len(b.subs))
	}
	b.removeSub(live)
	if len(b.subs) != 0 {
		t.Errorf("subs = %d, want it dropped", len(b.subs))
	}
}

// A caller that stops reading while the daemon is still emitting does
// not wedge the dispatch goroutine: cancelling the browse releases it
// even mid-send.
func TestDispatchReleasesAStalledConsumer(t *testing.T) {
	bus := newBus()
	b := newAvahiBrowser(discardLogger(), bus)
	t.Cleanup(func() { _ = b.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	out, err := b.Browse(ctx, "_nmos-register._tcp")
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	// The channel is buffered at 16; overfill it so the send blocks,
	// then cancel underneath.
	go func() {
		for i := 0; i < 64; i++ {
			bus.emit(t, itemNew(browserPath, "registry-a"))
		}
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()

	deadline := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-out:
			if !ok {
				return // closed: the goroutines let go
			}
		case <-deadline:
			t.Fatal("the dispatch goroutine did not release")
		}
	}
}

// ---------------------------------------------------------------------
// announce
// ---------------------------------------------------------------------

// fixedIfaces pins the interface table for one test.
func fixedIfaces(t *testing.T, ifs []net.Interface, err error) {
	t.Helper()
	prev := netInterfaces
	netInterfaces = func() ([]net.Interface, error) { return ifs, err }
	t.Cleanup(func() { netInterfaces = prev })
}

func upMulticast(idx int, name string) net.Interface {
	return net.Interface{Index: idx, Name: name, Flags: net.FlagUp | net.FlagMulticast}
}

func sampleInstance() dnssd.Instance {
	return dnssd.Instance{
		Name: "dhs-node", Service: "_nmos-node._tcp", Domain: "local",
		Host: "dhs-node.local", Port: 3212,
		TXT: map[string]string{"api_ver": "v1.3", "flag": ""},
	}
}

// An announce lands on the qualifying interfaces one at a time, never on
// AVAHI_IF_UNSPEC: Unspec includes `lo` on a stock daemon, which
// publishes a 127.0.0.1 A record that resolves peers to themselves.
func TestAnnouncePublishesPerInterface(t *testing.T) {
	fixedIfaces(t, []net.Interface{
		upMulticast(2, "eth0"),
		upMulticast(3, "eth1"),
		{Index: 1, Name: "lo", Flags: net.FlagUp | net.FlagMulticast | net.FlagLoopback},
	}, nil)

	bus := newBus()
	r := newAvahiResponder(discardLogger(), bus)

	if err := r.Announce(context.Background(), sampleInstance()); err != nil {
		t.Fatalf("Announce: %v", err)
	}

	adds := bus.seen("AddService")
	if len(adds) != 2 {
		t.Fatalf("AddService called %d times, want one per qualifying interface", len(adds))
	}
	for _, c := range adds {
		if idx := c.args[0].(int32); idx != 2 && idx != 3 {
			t.Errorf("announced on ifindex %d, want eth0 or eth1", idx)
		}
	}
	if len(bus.seen("Commit")) != 1 {
		t.Error("the group must be committed exactly once")
	}
}

// With nothing to announce on, Unspec is the documented fallback — a
// container mid-setup should still announce rather than go dark — and it
// is warned about, because it is the shape that poisons peer resolution.
func TestAnnounceFallsBackToUnspec(t *testing.T) {
	for _, tc := range []struct {
		name   string
		ifs    []net.Interface
		err    error
		logger bool
	}{
		{"no qualifying interface", []net.Interface{{Index: 1, Name: "lo", Flags: net.FlagLoopback}}, nil, true},
		{"an interface table that cannot be read", nil, errors.New("procfs"), true},
		{"and with no logger", nil, errors.New("procfs"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixedIfaces(t, tc.ifs, tc.err)
			bus := newBus()
			var r *avahiResponder
			if tc.logger {
				r = newAvahiResponder(discardLogger(), bus)
			} else {
				r = newAvahiResponder(nil, bus)
			}

			if err := r.Announce(context.Background(), sampleInstance()); err != nil {
				t.Fatalf("Announce: %v", err)
			}
			adds := bus.seen("AddService")
			if len(adds) != 1 || adds[0].args[0].(int32) != avahiIfaceUnspec {
				t.Fatalf("AddService args = %v, want a single AVAHI_IF_UNSPEC", adds)
			}
		})
	}
}

// Avahi rejects a bare hostname as the SRV target, so a host that is not
// already `.local` is normalised to empty and the daemon supplies its own
// canonical name.
func TestAnnounceHostNormalisation(t *testing.T) {
	fixedIfaces(t, []net.Interface{upMulticast(2, "eth0")}, nil)

	for _, tc := range []struct{ name, host, want string }{
		{"a .local name travels as-is", "dhs-node.local", "dhs-node.local"},
		{"a trailing dot is trimmed", "dhs-node.local.", "dhs-node.local"},
		{"a bare label is left to the daemon", "dhs-node", ""},
		{"and so is nothing at all", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bus := newBus()
			r := newAvahiResponder(discardLogger(), bus)
			ins := sampleInstance()
			ins.Host = tc.host
			if err := r.Announce(context.Background(), ins); err != nil {
				t.Fatalf("Announce: %v", err)
			}
			if got := bus.seen("AddService")[0].args[6].(string); got != tc.want {
				t.Errorf("host = %q, want %q", got, tc.want)
			}
		})
	}
}

// A domain the caller left empty is the link-local default, not an empty
// DBus argument the daemon would reject.
func TestAnnounceDefaultsTheDomain(t *testing.T) {
	fixedIfaces(t, []net.Interface{upMulticast(2, "eth0")}, nil)
	bus := newBus()
	r := newAvahiResponder(discardLogger(), bus)

	ins := sampleInstance()
	ins.Domain = ""
	if err := r.Announce(context.Background(), ins); err != nil {
		t.Fatalf("Announce: %v", err)
	}
	if got := bus.seen("AddService")[0].args[5].(string); got != dnssd.DefaultDomain {
		t.Errorf("domain = %q, want %q", got, dnssd.DefaultDomain)
	}
}

// A half-built group is freed rather than left committed-pending on the
// daemon, whichever call failed.
func TestAnnounceFailuresFreeTheGroup(t *testing.T) {
	fixedIfaces(t, []net.Interface{upMulticast(2, "eth0")}, nil)

	for _, tc := range []struct {
		name, method, want string
	}{
		{"AddService refused", "AddService", "AddService"},
		{"Commit refused", "Commit", "Commit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bus := newBus().fail(tc.method, errors.New("collision"))
			r := newAvahiResponder(discardLogger(), bus)

			err := r.Announce(context.Background(), sampleInstance())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("= %v, want the %s failure", err, tc.method)
			}
			if len(bus.seen("Free")) != 1 {
				t.Error("the half-built group must be freed")
			}
		})
	}
}

// The shapes Announce cannot act on, and the daemon refusal it reports.
func TestAnnounceRefusals(t *testing.T) {
	fixedIfaces(t, []net.Interface{upMulticast(2, "eth0")}, nil)

	t.Run("an instance with no identity", func(t *testing.T) {
		r := newAvahiResponder(discardLogger(), newBus())
		if err := r.Announce(context.Background(), dnssd.Instance{Service: "_x._tcp"}); err == nil {
			t.Error("a nameless instance must be refused")
		}
		if err := r.Announce(context.Background(), dnssd.Instance{Name: "x"}); err == nil {
			t.Error("a serviceless instance must be refused")
		}
	})

	t.Run("a daemon that will not open a group", func(t *testing.T) {
		bus := newBus().fail("EntryGroupNew", errors.New("too many groups"))
		r := newAvahiResponder(discardLogger(), bus)
		err := r.Announce(context.Background(), sampleInstance())
		if err == nil || !strings.Contains(err.Error(), "EntryGroupNew") {
			t.Errorf("= %v, want the daemon's refusal", err)
		}
	})
}

// ---------------------------------------------------------------------
// update
// ---------------------------------------------------------------------

// Update swaps the TXT in place on the same (interface, protocol, name,
// type, domain) tuples the group was announced on — Avahi matches on the
// whole tuple, so an update addressed differently is silently ignored and
// the IS-04 ver_ counter never moves.
func TestUpdateAddressesTheAnnouncedTuples(t *testing.T) {
	fixedIfaces(t, []net.Interface{upMulticast(2, "eth0"), upMulticast(3, "eth1")}, nil)
	bus := newBus()
	r := newAvahiResponder(discardLogger(), bus)

	ins := sampleInstance()
	if err := r.Announce(context.Background(), ins); err != nil {
		t.Fatalf("Announce: %v", err)
	}
	ins.TXT = map[string]string{"ver_slf": "3"}
	if err := r.Update(context.Background(), ins); err != nil {
		t.Fatalf("Update: %v", err)
	}

	ups := bus.seen("UpdateServiceTxt")
	if len(ups) != 2 {
		t.Fatalf("UpdateServiceTxt called %d times, want one per announced interface", len(ups))
	}
	for _, c := range ups {
		if c.args[3].(string) != ins.Name || c.args[5].(string) != "local" {
			t.Errorf("update addressed %v, want the announced tuple", c.args[:6])
		}
	}
}

// A group recorded with no interfaces still updates, on Unspec — the
// mirror of the announce fallback.
func TestUpdateFallsBackToUnspec(t *testing.T) {
	bus := newBus()
	r := newAvahiResponder(discardLogger(), bus)
	ins := sampleInstance()
	r.groups[ins.FullName()] = avahiGroup{
		path: "/Client1/EntryGroup1", name: ins.Name,
		service: ins.Service, domain: "local", instance: ins,
	}

	if err := r.Update(context.Background(), ins); err != nil {
		t.Fatalf("Update: %v", err)
	}
	ups := bus.seen("UpdateServiceTxt")
	if len(ups) != 1 || ups[0].args[0].(int32) != avahiIfaceUnspec {
		t.Fatalf("UpdateServiceTxt args = %v, want a single AVAHI_IF_UNSPEC", ups)
	}
}

// Update refuses what it cannot address and reports what the daemon
// refuses — an update that quietly did nothing would leave peers on a
// stale TXT with no sign anything was wrong.
func TestUpdateRefusals(t *testing.T) {
	fixedIfaces(t, []net.Interface{upMulticast(2, "eth0")}, nil)

	t.Run("an instance with no identity", func(t *testing.T) {
		r := newAvahiResponder(discardLogger(), newBus())
		if err := r.Update(context.Background(), dnssd.Instance{Service: "_x._tcp"}); err == nil {
			t.Error("a nameless instance must be refused")
		}
		if err := r.Update(context.Background(), dnssd.Instance{Name: "x"}); err == nil {
			t.Error("a serviceless instance must be refused")
		}
	})

	t.Run("something never announced", func(t *testing.T) {
		r := newAvahiResponder(discardLogger(), newBus())
		err := r.Update(context.Background(), sampleInstance())
		if err == nil || !strings.Contains(err.Error(), "not announced") {
			t.Errorf("= %v, want the missing-group refusal", err)
		}
	})

	t.Run("a responder already closed", func(t *testing.T) {
		r := newAvahiResponder(discardLogger(), newBus())
		if err := r.Announce(context.Background(), sampleInstance()); err != nil {
			t.Fatalf("Announce: %v", err)
		}
		_ = r.Close()
		if err := r.Update(context.Background(), sampleInstance()); err == nil {
			t.Error("a closed responder must refuse to update")
		}
	})

	t.Run("a daemon that refuses the update", func(t *testing.T) {
		bus := newBus().fail("UpdateServiceTxt", errors.New("no such service"))
		r := newAvahiResponder(discardLogger(), bus)
		if err := r.Announce(context.Background(), sampleInstance()); err != nil {
			t.Fatalf("Announce: %v", err)
		}
		err := r.Update(context.Background(), sampleInstance())
		if err == nil || !strings.Contains(err.Error(), "UpdateServiceTxt") {
			t.Errorf("= %v, want the daemon's refusal", err)
		}
	})
}

// ---------------------------------------------------------------------
// close and the goodbye packets
// ---------------------------------------------------------------------

// captureGoodbye replaces the goodbye socket with one that keeps the
// packets, or refuses to open, or refuses to write.
type captureGoodbye struct {
	mu     sync.Mutex
	pkts   [][]byte
	dialer error
	writer error
}

func (g *captureGoodbye) Write(p []byte) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.pkts = append(g.pkts, append([]byte(nil), p...))
	return len(p), g.writer
}

func (g *captureGoodbye) Close() error { return nil }

func (g *captureGoodbye) install(t *testing.T) *captureGoodbye {
	t.Helper()
	prev := dialGoodbye
	dialGoodbye = func() (io.WriteCloser, error) {
		if g.dialer != nil {
			return nil, g.dialer
		}
		return g, nil
	}
	t.Cleanup(func() { dialGoodbye = prev })
	return g
}

func (g *captureGoodbye) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.pkts)
}

// Close frees the daemon's groups FIRST and only then emits the RFC 6762
// §10.1 goodbyes: the other order lets the daemon defend records it still
// owns, and AMWA IS-04-01 test_12 reads the peer-side cache.
func TestCloseFreesThenSaysGoodbye(t *testing.T) {
	fixedIfaces(t, []net.Interface{upMulticast(2, "eth0")}, nil)
	gb := (&captureGoodbye{}).install(t)
	bus := newBus()
	r := newAvahiResponder(discardLogger(), bus)

	if err := r.Announce(context.Background(), sampleInstance()); err != nil {
		t.Fatalf("Announce: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if len(bus.seen("Free")) != 1 {
		t.Error("the EntryGroup must be freed")
	}
	// Three sends per group: multicast is best-effort and a single
	// dropped goodbye leaves peer caches stale for the whole TTL.
	if got := gb.count(); got != 3 {
		t.Errorf("%d goodbye packets, want 3", got)
	}
	if err := r.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if got := gb.count(); got != 3 {
		t.Errorf("a second Close sent %d more packets", got-3)
	}
}

// A daemon that will not free a group is reported, and does not stop the
// goodbye from going out — the records have to be withdrawn either way.
func TestCloseReportsAFreeFailureAndStillSaysGoodbye(t *testing.T) {
	fixedIfaces(t, []net.Interface{upMulticast(2, "eth0")}, nil)
	gb := (&captureGoodbye{}).install(t)
	bus := newBus()
	r := newAvahiResponder(discardLogger(), bus)

	if err := r.Announce(context.Background(), sampleInstance()); err != nil {
		t.Fatalf("Announce: %v", err)
	}
	bus.fail("Free", errors.New("no such group"))

	if err := r.Close(); err == nil {
		t.Error("a Free failure must be reported")
	}
	if gb.count() != 3 {
		t.Errorf("%d goodbye packets, want the goodbye sent anyway", gb.count())
	}
}

// The goodbye is best-effort: a socket that will not open, or will not
// write, is logged and does not turn Close into a failure the caller has
// to handle. With no logger at all it is simply silent.
func TestGoodbyeIsBestEffort(t *testing.T) {
	fixedIfaces(t, []net.Interface{upMulticast(2, "eth0")}, nil)

	for _, tc := range []struct {
		name   string
		gb     *captureGoodbye
		logger bool
	}{
		{"a socket that will not open", &captureGoodbye{dialer: errors.New("network unreachable")}, true},
		{"and with no logger", &captureGoodbye{dialer: errors.New("network unreachable")}, false},
		{"a socket that will not write", &captureGoodbye{writer: errors.New("EPERM")}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.gb.install(t)
			var r *avahiResponder
			if tc.logger {
				r = newAvahiResponder(discardLogger(), newBus())
			} else {
				r = newAvahiResponder(nil, newBus())
			}
			if err := r.Announce(context.Background(), sampleInstance()); err != nil {
				t.Fatalf("Announce: %v", err)
			}
			if err := r.Close(); err != nil {
				t.Errorf("Close = %v, want the goodbye failure absorbed", err)
			}
		})
	}
}

// An instance that cannot be encoded as a goodbye is logged and skipped;
// the groups that CAN be withdrawn still are.
func TestGoodbyeSkipsWhatItCannotEncode(t *testing.T) {
	fixedIfaces(t, []net.Interface{upMulticast(2, "eth0")}, nil)

	for _, withLogger := range []bool{true, false} {
		gb := (&captureGoodbye{}).install(t)
		var r *avahiResponder
		if withLogger {
			r = newAvahiResponder(discardLogger(), newBus())
		} else {
			r = newAvahiResponder(nil, newBus())
		}

		// Port 0 is not encodable as an SRV record, so EncodeGoodbye
		// refuses it.
		broken := sampleInstance()
		broken.Port = 0
		if err := r.Announce(context.Background(), broken); err != nil {
			t.Fatalf("Announce: %v", err)
		}
		if err := r.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if gb.count() != 0 {
			t.Errorf("%d packets, want the unencodable instance skipped", gb.count())
		}
	}
}

// The saved SRV target is what the daemon actually put on the wire, or
// the goodbye names a record no peer has cached. Avahi appends `.local`
// to a bare label and invents one from the instance name when we passed
// nothing at all.
func TestGoodbyeNamesWhatAvahiAnnounced(t *testing.T) {
	fixedIfaces(t, []net.Interface{upMulticast(2, "eth0")}, nil)

	for _, tc := range []struct{ name, host, want string }{
		{"a .local host is kept", "dhs-node.local", "dhs-node.local"},
		{"a bare label gains the suffix", "dhs-node", "dhs-node.local"},
		{"an empty host becomes the instance name", "", "dhs-node.local"},
		{"a trailing dot is trimmed first", "dhs-node.", "dhs-node.local"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newAvahiResponder(discardLogger(), newBus())
			ins := sampleInstance()
			ins.Host = tc.host
			if err := r.Announce(context.Background(), ins); err != nil {
				t.Fatalf("Announce: %v", err)
			}
			if got := r.groups[ins.FullName()].instance.Host; got != tc.want {
				t.Errorf("saved SRV target = %q, want %q", got, tc.want)
			}
		})
	}
}

// A responder announced to after Close rebuilds its group table rather
// than panicking on the nil map Close left behind.
func TestAnnounceAfterCloseRebuildsTheTable(t *testing.T) {
	fixedIfaces(t, []net.Interface{upMulticast(2, "eth0")}, nil)
	(&captureGoodbye{}).install(t)
	r := newAvahiResponder(discardLogger(), newBus())

	if err := r.Announce(context.Background(), sampleInstance()); err != nil {
		t.Fatalf("Announce: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := r.Announce(context.Background(), sampleInstance()); err != nil {
		t.Fatalf("Announce after Close: %v", err)
	}
	if len(r.groups) != 1 {
		t.Errorf("groups = %d, want the table rebuilt", len(r.groups))
	}
}

// ---------------------------------------------------------------------
// TXT
// ---------------------------------------------------------------------

// Avahi carries TXT as RFC 6763 "key" / "key=value" byte strings, and
// the round trip has to preserve the difference: a valueless key is not
// the same record as a key with an empty value.
func TestAvahiTXTRoundTrip(t *testing.T) {
	in := map[string]string{"api_ver": "v1.3", "flag": "", "eq": "a=b"}
	out := decodeAvahiTXT(encodeAvahiTXT(in))

	if len(out) != len(in) {
		t.Fatalf("round trip = %v, want %v", out, in)
	}
	for k, v := range in {
		if out[k] != v {
			t.Errorf("%s = %q, want %q", k, out[k], v)
		}
	}
	// An empty segment is padding, not a record with an empty key.
	if got := decodeAvahiTXT([][]byte{nil, []byte("")}); len(got) != 0 {
		t.Errorf("empty segments decoded to %v, want nothing", got)
	}
}

// A bus that goes away closes the signal channels it was routing into.
// The dispatch goroutine has to treat that as the end of the
// subscription rather than spinning on a closed channel forever, which
// is a busy loop on a Node that has just lost its system bus.
func TestDispatchStopsWhenTheBusGoesAway(t *testing.T) {
	bus := newBus()
	b := newAvahiBrowser(discardLogger(), bus)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out, err := b.Browse(ctx, "_nmos-register._tcp")
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}

	// Take the subscription BEFORE the bus goes away, so the exit can be
	// waited for rather than raced. The context is still live at this
	// point, so the only way dispatch can return is the closed-channel
	// arm — waiting here is what makes that deterministic instead of a
	// coin flip against Close cancelling the context first.
	b.mu.Lock()
	sub := b.subs[0]
	b.mu.Unlock()

	bus.closeSignals()

	select {
	case <-sub.done:
	case <-time.After(3 * time.Second):
		t.Fatal("dispatch did not notice the bus going away")
	}

	// And Close still tears down cleanly rather than blocking on a
	// goroutine that has already gone.
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case _, ok := <-out:
		if ok {
			t.Error("nothing can be dispatched after the bus went away")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the subscription never ended")
	}
}

// waitFor polls until cond holds, so a test can assert on work a
// teardown goroutine does after the call that triggered it returned.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition never held")
		}
		time.Sleep(time.Millisecond)
	}
}
