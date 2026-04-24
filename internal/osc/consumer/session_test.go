package osc

import (
	"context"
	"net"
	"testing"
	"time"

	"acp/internal/osc/codec"
)

func TestUDPSession_ReceiveMessage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := newUDPSession()
	if err := s.listen(ctx, "127.0.0.1:0"); err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = s.close() }()

	got := make(chan PacketEvent, 1)
	s.subscribe("/x/y", func(ev PacketEvent) { got <- ev })

	wire, _ := codec.Message{Address: "/x/y", Args: []codec.Arg{codec.Int32(7)}}.Encode()
	sock, _ := net.DialUDP("udp", nil, s.boundAddr())
	defer func() { _ = sock.Close() }()
	_, _ = sock.Write(wire)

	select {
	case ev := <-got:
		if ev.Msg.Address != "/x/y" || ev.Msg.Args[0].Int32 != 7 {
			t.Errorf("got %+v", ev.Msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event")
	}
}

func TestUDPSession_BundleFlattensToSubscribers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newUDPSession()
	_ = s.listen(ctx, "127.0.0.1:0")
	defer func() { _ = s.close() }()

	got := make(chan string, 4)
	s.subscribe("", func(ev PacketEvent) { got <- ev.Msg.Address })

	bundle := codec.Bundle{
		Timetag: 1,
		Elements: []codec.Packet{
			codec.Message{Address: "/a", Args: []codec.Arg{codec.Int32(1)}},
			codec.Message{Address: "/b", Args: []codec.Arg{codec.Int32(2)}},
			codec.Bundle{
				Timetag: 2,
				Elements: []codec.Packet{
					codec.Message{Address: "/c", Args: []codec.Arg{codec.Int32(3)}},
				},
			},
		},
	}
	wire, _ := bundle.Encode()
	sock, _ := net.DialUDP("udp", nil, s.boundAddr())
	defer func() { _ = sock.Close() }()
	_, _ = sock.Write(wire)

	seen := map[string]bool{}
	deadline := time.After(2 * time.Second)
	for len(seen) < 3 {
		select {
		case a := <-got:
			seen[a] = true
		case <-deadline:
			t.Fatalf("got %v, want /a /b /c", seen)
		}
	}
	for _, want := range []string{"/a", "/b", "/c"} {
		if !seen[want] {
			t.Errorf("missing %s", want)
		}
	}
}

func TestAddressMatches(t *testing.T) {
	cases := []struct {
		pattern, addr string
		want          bool
	}{
		{"", "/anything/here", true},
		{"/exact", "/exact", true},
		{"/exact", "/exact/nope", false},
		{"/foo/*", "/foo/bar", true},
		{"/foo/*", "/foo/bar/baz", false},
		{"/foo/*", "/foo/", false},
		{"/foo/*", "/bar/x", false},
	}
	for _, c := range cases {
		if got := addressMatches(c.pattern, c.addr); got != c.want {
			t.Errorf("match(%q, %q) = %v, want %v", c.pattern, c.addr, got, c.want)
		}
	}
}

func TestUDPSession_GlobPattern(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newUDPSession()
	_ = s.listen(ctx, "127.0.0.1:0")
	defer func() { _ = s.close() }()

	hits := make(chan string, 3)
	s.subscribe("/tally/*", func(ev PacketEvent) { hits <- ev.Matched })

	// Should match
	for _, a := range []string{"/tally/1", "/tally/pgm"} {
		w, _ := codec.Message{Address: a, Args: []codec.Arg{codec.Int32(1)}}.Encode()
		sock, _ := net.DialUDP("udp", nil, s.boundAddr())
		_, _ = sock.Write(w)
		_ = sock.Close()
	}
	// Should NOT match (too deep)
	w2, _ := codec.Message{Address: "/tally/pgm/1", Args: []codec.Arg{codec.Int32(1)}}.Encode()
	sock, _ := net.DialUDP("udp", nil, s.boundAddr())
	_, _ = sock.Write(w2)
	_ = sock.Close()

	seen := map[string]bool{}
	deadline := time.After(1 * time.Second)
loop:
	for {
		select {
		case a := <-hits:
			seen[a] = true
			if len(seen) == 2 {
				// Give the "shouldn't match" packet a moment to NOT fire.
				time.Sleep(50 * time.Millisecond)
				break loop
			}
		case <-deadline:
			break loop
		}
	}
	if len(seen) != 2 || !seen["/tally/1"] || !seen["/tally/pgm"] {
		t.Errorf("seen=%v, want /tally/1 + /tally/pgm only", seen)
	}
}
