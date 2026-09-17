//go:build linux

package lldp

// Everything worth pinning in Neighbors sits above a socket that needs
// CAP_NET_RAW, so these tests drive it through the osCalls seam: which
// frames are kept, which are skipped, which failures explain themselves
// and which are returned bare. No privileges, no live network.

import (
	"context"
	"errors"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeOS is a scripted kernel. reads is played back one entry per
// recvfrom; when it runs dry the socket reports EAGAIN forever, which
// is what a quiet link does until the window closes.
type fakeOS struct {
	socketErr  error
	bindErr    error
	timeoutErr error
	ifaceErr   error

	ifaces []net.Interface
	reads  []read

	bound     *syscall.SockaddrLinklayer
	closed    bool
	timeouts  []time.Duration
	socketArg [3]int
}

// read is one recvfrom outcome: either an error, or a frame that
// arrived on an interface index.
type read struct {
	err     error
	ifindex int
	pdu     []byte
	// notLinkLayer makes recvfrom answer with an address that is not a
	// SockaddrLinklayer, which the kernel does not do on AF_PACKET but
	// the type assertion in the loop has to survive anyway.
	notLinkLayer bool
}

func (f *fakeOS) calls() *osCalls {
	return &osCalls{
		socket: func(domain, typ, proto int) (int, error) {
			f.socketArg = [3]int{domain, typ, proto}
			if f.socketErr != nil {
				return -1, f.socketErr
			}
			return 7, nil
		},
		bind: func(_ int, sa syscall.Sockaddr) error {
			if ll, ok := sa.(*syscall.SockaddrLinklayer); ok {
				f.bound = ll
			}
			return f.bindErr
		},
		setTimeout: func(_ int, tv *syscall.Timeval) error {
			f.timeouts = append(f.timeouts, time.Duration(syscall.TimevalToNsec(*tv)))
			return f.timeoutErr
		},
		recvfrom: func(_ int, p []byte) (int, syscall.Sockaddr, error) {
			if len(f.reads) == 0 {
				return 0, nil, syscall.EAGAIN
			}
			r := f.reads[0]
			f.reads = f.reads[1:]
			if r.err != nil {
				return 0, nil, r.err
			}
			if r.notLinkLayer {
				return 0, &syscall.SockaddrInet4{}, nil
			}
			return copy(p, r.pdu), &syscall.SockaddrLinklayer{Ifindex: r.ifindex}, nil
		},
		close: func(int) error {
			f.closed = true
			return nil
		},
		interfaces: func() ([]net.Interface, error) {
			if f.ifaceErr != nil {
				return nil, f.ifaceErr
			}
			return f.ifaces, nil
		},
	}
}

// twoPorts is the interface table these tests capture against.
func twoPorts() []net.Interface {
	return []net.Interface{
		{Index: 1, Name: "eth0"},
		{Index: 2, Name: "eth1"},
	}
}

// capture builds a Capture on the fake kernel with a window short
// enough that a test never waits on a real clock.
func capture(f *fakeOS, iface string) Capture {
	return Capture{Iface: iface, Window: 30 * time.Millisecond, sys: f.calls()}
}

// A host whose interface table cannot be read is reported, not treated
// as a host with no interfaces — the second reads as "no neighbours",
// which is a different fact.
func TestInterfaceTableFailureIsReported(t *testing.T) {
	f := &fakeOS{ifaceErr: errors.New("procfs is gone")}
	_, err := capture(f, "").Neighbors(context.Background())
	if err == nil || !strings.Contains(err.Error(), "list interfaces") {
		t.Fatalf("= %v, want the listing failure reported", err)
	}
	if f.closed {
		t.Error("no socket was opened, so none should have been closed")
	}
}

// The socket is opened for the LLDP Ethertype at the datagram level,
// and a refusal for want of the capability says which capability.
func TestSocketRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"no capability", syscall.EPERM, "cap_net_raw"},
		{"denied", syscall.EACCES, "cap_net_raw"},
		{"anything else", syscall.EAFNOSUPPORT, "AF_PACKET socket"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeOS{ifaces: twoPorts(), socketErr: tc.err}
			_, err := capture(f, "").Neighbors(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("= %v, want %q", err, tc.want)
			}
		})
	}
}

// Naming an interface binds the socket to that ifindex, and the
// Ethertype travels on the bind too — an unbound socket would hear the
// whole host's LLDP and answer for a port the operator did not ask about.
func TestNamingAnInterfaceBindsToIt(t *testing.T) {
	f := &fakeOS{ifaces: twoPorts()}
	if _, err := capture(f, "eth1").Neighbors(context.Background()); err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	if f.bound == nil {
		t.Fatal("a named interface must bind")
	}
	if f.bound.Ifindex != 2 {
		t.Errorf("bound ifindex = %d, want 2 (eth1)", f.bound.Ifindex)
	}
	if f.bound.Protocol != htons(EtherType) {
		t.Errorf("bound protocol = 0x%04X, want the LLDP Ethertype", f.bound.Protocol)
	}
	if !f.closed {
		t.Error("the socket must be closed on the way out")
	}
}

// A bind that fails names the interface it failed on.
func TestBindFailureNamesTheInterface(t *testing.T) {
	f := &fakeOS{ifaces: twoPorts(), bindErr: syscall.ENODEV}
	_, err := capture(f, "eth0").Neighbors(context.Background())
	if err == nil || !strings.Contains(err.Error(), "bind eth0") {
		t.Fatalf("= %v, want the bind failure to name eth0", err)
	}
}

// Capturing on every interface opens no bind at all.
func TestNoInterfaceMeansNoBind(t *testing.T) {
	f := &fakeOS{ifaces: twoPorts()}
	if _, err := capture(f, "").Neighbors(context.Background()); err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	if f.bound != nil {
		t.Error("an unnamed interface must not bind")
	}
}

// A read timeout that cannot be set is returned rather than left to a
// blocking read that ignores the window and the context both.
func TestReadTimeoutFailureIsReturned(t *testing.T) {
	f := &fakeOS{ifaces: twoPorts(), timeoutErr: syscall.EINVAL}
	_, err := capture(f, "").Neighbors(context.Background())
	if err == nil || !strings.Contains(err.Error(), "set read timeout") {
		t.Fatalf("= %v, want the timeout failure reported", err)
	}
}

// Each read is bounded by readTick so cancellation lands within a tick,
// and the last one is trimmed to what is left of the window rather than
// overrunning it.
func TestReadsAreBoundedByTheTick(t *testing.T) {
	f := &fakeOS{ifaces: twoPorts()}
	if _, err := (Capture{Window: readTick / 4, sys: f.calls()}).
		Neighbors(context.Background()); err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	if len(f.timeouts) == 0 {
		t.Fatal("no read timeout was ever set")
	}
	for i, d := range f.timeouts {
		if d > readTick {
			t.Errorf("read %d bounded at %s, want at most %s", i, d, readTick)
		}
	}
}

// A window of zero means the documented default rather than a socket
// that returns instantly.
func TestZeroWindowTakesTheDefault(t *testing.T) {
	f := &fakeOS{ifaces: twoPorts()}
	// A context deadline well inside defaultWindow ends the loop, which
	// also pins that the deadline wins when it is the earlier of the two.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	// Either exit is correct and which one lands is a matter of a
	// microsecond: the loop returns cleanly when the deadline has passed
	// on entry, and returns ctx.Err() when the context expires between
	// the two checks. What is asserted is that it did not sit for the
	// default window.
	if _, err := (Capture{sys: f.calls()}).Neighbors(ctx); err != nil &&
		!errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Neighbors: %v", err)
	}
	if elapsed := time.Since(start); elapsed >= defaultWindow {
		t.Errorf("ran for %s: the context deadline did not shorten the window", elapsed)
	}
}

// A context already cancelled is reported with what has been heard so
// far, not discarded: a caller that gave up still wants the frames that
// did arrive.
func TestCancelledContextReturnsWhatWasHeard(t *testing.T) {
	f := &fakeOS{ifaces: twoPorts()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	out, err := capture(f, "").Neighbors(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("= %v, want context.Canceled", err)
	}
	if out == nil {
		t.Error("the frames heard before cancellation must come back")
	}
}

// EAGAIN is the window ticking on a quiet link and EINTR is a signal,
// so both continue; anything else is a broken socket and returns.
func TestRecvErrors(t *testing.T) {
	t.Run("a quiet link is not a failure", func(t *testing.T) {
		f := &fakeOS{ifaces: twoPorts(), reads: []read{
			{err: syscall.EAGAIN}, {err: syscall.EINTR},
		}}
		out, err := capture(f, "").Neighbors(context.Background())
		if err != nil {
			t.Fatalf("= %v, want the window to close quietly", err)
		}
		if len(out) != 0 {
			t.Errorf("heard %d neighbours from a quiet link", len(out))
		}
	})

	t.Run("a broken socket is", func(t *testing.T) {
		f := &fakeOS{ifaces: twoPorts(), reads: []read{{err: syscall.EBADF}}}
		_, err := capture(f, "").Neighbors(context.Background())
		if err == nil || !strings.Contains(err.Error(), "lldp: recv") {
			t.Fatalf("= %v, want the receive failure reported", err)
		}
	})
}

// Frames the loop cannot attribute to a port are skipped rather than
// filed under an empty interface name: an address that is not a link
// layer one, an ifindex this host does not have, and — when one
// interface was named — a frame that arrived on another.
func TestUnattributableFramesAreSkipped(t *testing.T) {
	good := frame(t, mandatory(t)...)

	for _, tc := range []struct {
		name  string
		iface string
		r     read
	}{
		{"not a link-layer address", "", read{notLinkLayer: true}},
		{"an ifindex this host does not have", "", read{ifindex: 99, pdu: good}},
		{"another port than the one named", "eth0", read{ifindex: 2, pdu: good}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeOS{ifaces: twoPorts(), reads: []read{tc.r}}
			out, err := capture(f, tc.iface).Neighbors(context.Background())
			if err != nil {
				t.Fatalf("Neighbors: %v", err)
			}
			if len(out) != 0 {
				t.Errorf("kept %v, want nothing", out)
			}
		})
	}
}

// A malformed LLDPDU from one switch must not hide the frames that are
// fine, so it is skipped and the next frame is still filed.
func TestAMalformedFrameDoesNotHideAGoodOne(t *testing.T) {
	f := &fakeOS{ifaces: twoPorts(), reads: []read{
		{ifindex: 1, pdu: []byte{0xff}}, // truncated: Decode refuses it
		{ifindex: 2, pdu: frame(t, mandatory(t)...)},
	}}

	out, err := capture(f, "").Neighbors(context.Background())
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("heard %v, want only the good frame", out)
	}
	if _, ok := out["eth1"]; !ok {
		t.Errorf("the good frame was filed under %v, want eth1", out)
	}
}

// A shutdown LLDPDU (TTL 0) withdraws the neighbour: keeping it would
// publish a switch port that is no longer attached.
func TestShutdownWithdrawsTheNeighbour(t *testing.T) {
	live := frame(t, mandatory(t)...)
	gone := frame(t, chassisMAC(1, 2, 3, 4, 5, 6), portMAC(6, 5, 4, 3, 2, 1), ttl(0))

	f := &fakeOS{ifaces: twoPorts(), reads: []read{
		{ifindex: 1, pdu: live},
		{ifindex: 1, pdu: gone},
	}}

	out, err := capture(f, "").Neighbors(context.Background())
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	if _, still := out["eth0"]; still {
		t.Errorf("eth0 is still listed after its neighbour announced shutdown: %v", out)
	}
}

// The ordinary case: a frame on a known port is filed under that port's
// name, decoded.
func TestAFrameIsFiledUnderItsPort(t *testing.T) {
	f := &fakeOS{ifaces: twoPorts(), reads: []read{
		{ifindex: 2, pdu: frame(t, mandatory(t)...)},
	}}

	out, err := capture(f, "").Neighbors(context.Background())
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	nb, ok := out["eth1"]
	if !ok {
		t.Fatalf("heard %v, want a neighbour on eth1", out)
	}
	if nb.ChassisID == "" || nb.PortID == "" {
		t.Errorf("neighbour = %+v, want the mandatory TLVs decoded", nb)
	}
	if f.socketArg != [3]int{syscall.AF_PACKET, syscall.SOCK_DGRAM, int(htons(EtherType))} {
		t.Errorf("socket opened as %v, want AF_PACKET/SOCK_DGRAM on the LLDP Ethertype", f.socketArg)
	}
}

// A Capture with no seam set is the real kernel — the production path,
// and the reason the seam is a field rather than a package variable.
func TestTheDefaultCaptureIsTheRealKernel(t *testing.T) {
	if (Capture{}).os().socket == nil {
		t.Error("an unseamed Capture must resolve to the real syscalls")
	}
}

// The two adapters in realOS are the only code between this package and
// the kernel, and they are the only code the seam above cannot reach.
// They take an fd, so an ordinary UDP socket exercises them — no
// AF_PACKET, no CAP_NET_RAW, which is what CI has.
func TestTheRealAdaptersTalkToTheKernel(t *testing.T) {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, 0)
	if err != nil {
		t.Fatalf("a plain UDP socket: %v", err)
	}
	t.Cleanup(func() { _ = realOS.close(fd) })

	addr := &syscall.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}}
	if err := realOS.bind(fd, addr); err != nil {
		t.Fatalf("bind: %v", err)
	}

	// A read timeout that the kernel accepts, then a read that hits it:
	// nothing was sent, so recvfrom returns EAGAIN rather than blocking
	// this test for as long as the suite is allowed to run.
	tv := syscall.NsecToTimeval(int64(50 * time.Millisecond))
	if err := realOS.setTimeout(fd, &tv); err != nil {
		t.Fatalf("set read timeout: %v", err)
	}
	if _, _, err := realOS.recvfrom(fd, make([]byte, 16)); err == nil {
		t.Error("a read with nothing to read must time out, not succeed")
	} else if !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EWOULDBLOCK) {
		t.Errorf("recvfrom = %v, want the timeout", err)
	}

	if realOS.socket == nil || realOS.interfaces == nil {
		t.Error("realOS must carry the kernel's own calls")
	}
}
