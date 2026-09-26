package testnet

import (
	"net"
	"strconv"
	"strings"
	"testing"
)

func inBand(t *testing.T, port int) {
	t.Helper()
	if port < Low || port >= High {
		t.Errorf("port %d outside the band %d-%d", port, Low, High)
	}
}

func TestFreeAddrIsLoopbackInTheBandAndBindable(t *testing.T) {
	addr := FreeAddr(t)
	host, p, err := net.SplitHostPort(addr)
	if err != nil || host != "127.0.0.1" {
		t.Fatalf("FreeAddr = %q", addr)
	}
	port, _ := strconv.Atoi(p)
	inBand(t, port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("the returned address is not bindable: %v", err)
	}
	_ = ln.Close()
}

func TestFreeUDPPortIsInTheBandAndBindable(t *testing.T) {
	port := FreeUDPPort(t)
	inBand(t, port)
	c, err := net.ListenPacket("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatalf("the returned port is not bindable: %v", err)
	}
	_ = c.Close()
}

// fatalTB records Fatalf instead of ending the test, so the give-up path
// can be observed.
type fatalTB struct {
	testing.TB
	msg string
}

func (f *fatalTB) Helper() {}
func (f *fatalTB) Fatalf(format string, args ...any) {
	f.msg = format
}

func TestPickGivesUpWhenNothingIsBindable(t *testing.T) {
	ft := &fatalTB{TB: t}
	calls := 0
	got := pick(ft, "tcp", func(string) error { calls++; return net.ErrClosed })
	if got != 0 || calls != attempts || !strings.Contains(ft.msg, "no free") {
		t.Errorf("pick = %d after %d calls, fatal %q", got, calls, ft.msg)
	}
}

func TestBindChecksRefuseAnOccupiedPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	if bindTCP(ln.Addr().String()) == nil {
		t.Error("bindTCP accepted a port that is held")
	}

	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if bindUDP(c.LocalAddr().String()) == nil {
		t.Error("bindUDP accepted a port that is held")
	}
}
