// Package testnet hands tests a loopback port that nothing else on the
// machine will be given before the test binds it.
//
// The usual trick — listen on :0, read the port, close, return it — hands
// back a port the kernel is free to give straight to another process: :0
// draws from the ephemeral range, which every other process on a CI runner
// is being served from at the same moment. The test then binds it later,
// often after building a server, and fails with "address already in use"
// (#1118).
//
// So ports come from a band BELOW every platform's ephemeral range — Linux
// auto-assigns from 32768, macOS and Windows from 49152 — where the kernel
// hands out nothing by itself. The only way to collide is another test
// drawing the same number at random from the same band, and each port is
// checked bindable before it is returned.
//
// Stdlib only, no package state.
package testnet

import (
	"math/rand/v2"
	"net"
	"strconv"
	"testing"
)

// Low and High bracket the band ports are drawn from.
const (
	Low  = 20000
	High = 32700
)

const attempts = 200

// FreeAddr returns "127.0.0.1:<port>" for a TCP port that is bindable now
// and outside the range the kernel assigns on its own.
func FreeAddr(t testing.TB) string {
	t.Helper()
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(FreePort(t)))
}

// FreePort is FreeAddr's port alone.
func FreePort(t testing.TB) int {
	t.Helper()
	return pick(t, "tcp", bindTCP)
}

// FreeUDPPort returns a UDP port on 127.0.0.1, drawn the same way.
func FreeUDPPort(t testing.TB) int {
	t.Helper()
	return pick(t, "udp", bindUDP)
}

// bindTCP reports whether addr can be listened on right now.
func bindTCP(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return ln.Close()
}

// bindUDP is bindTCP for UDP.
func bindUDP(addr string) error {
	c, err := net.ListenPacket("udp", addr)
	if err != nil {
		return err
	}
	return c.Close()
}

func pick(t testing.TB, network string, bindable func(addr string) error) int {
	t.Helper()
	for range attempts {
		port := Low + rand.IntN(High-Low)
		if bindable(net.JoinHostPort("127.0.0.1", strconv.Itoa(port))) == nil {
			return port
		}
	}
	t.Fatalf("testnet: no free %s port in %d-%d after %d attempts", network, Low, High, attempts)
	return 0
}
