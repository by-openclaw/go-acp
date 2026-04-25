//go:build integration

// Multi-peer integration tests: at least 3 dhs instances exchanging
// data over the same loopback transport. Validates that OSC's symmetric
// peer model holds end-to-end across UDP, TCP-LP, and TCP-SLIP.
//
// Topologies covered:
//
//	1) Fan-out: 1 producer  → N consumers (UDP shared port; TCP per-conn).
//	2) Merge:   N producers → 1 consumer.
//	3) Mixed:   2 producers + 2 consumers exchanging via the same fabric.
//
// Run with:
//
//	go test -tags integration ./internal/osc/integration/... -run MultiPeer

package osc_integration

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"acp/internal/osc/codec"
	consumer "acp/internal/osc/consumer"
	provider "acp/internal/osc/provider"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// ---- 1) UDP fan-out: 1 producer, 2 consumers on the same shared port ----------
//
// SO_REUSEADDR lets multiple consumers bind the same UDP port; OS-level
// duplication of broadcast traffic isn't relied on here — instead we
// have the producer add BOTH consumer addresses as destinations and
// fan-out at the application layer.

func TestMultiPeer_UDP_OneProducer_TwoConsumers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := quietLogger()

	c1 := consumer.NewPluginV10(logger)
	c2 := consumer.NewPluginV10(logger)
	defer func() { _ = c1.Disconnect() }()
	defer func() { _ = c2.Disconnect() }()
	if err := c1.Connect(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("c1: %v", err)
	}
	if err := c2.Connect(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("c2: %v", err)
	}

	got1 := make(chan codec.Message, 8)
	got2 := make(chan codec.Message, 8)
	_ = c1.SubscribePattern("", func(ev consumer.PacketEvent) { got1 <- ev.Msg })
	_ = c2.SubscribePattern("", func(ev consumer.PacketEvent) { got2 <- ev.Msg })

	srv := provider.NewServerV10(logger)
	defer func() { _ = srv.Stop() }()
	if err := srv.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if err := srv.AddDestination("127.0.0.1", c1.BoundAddr().Port); err != nil {
		t.Fatalf("add dest c1: %v", err)
	}
	if err := srv.AddDestination("127.0.0.1", c2.BoundAddr().Port); err != nil {
		t.Fatalf("add dest c2: %v", err)
	}

	const fanouts = 5
	for i := 0; i < fanouts; i++ {
		msg := codec.Message{Address: fmt.Sprintf("/fan/%d", i), Args: []codec.Arg{codec.Int32(int32(i))}}
		if err := srv.SendMessage(msg); err != nil {
			t.Fatalf("send: %v", err)
		}
	}
	expectN(t, got1, fanouts, "consumer-1")
	expectN(t, got2, fanouts, "consumer-2")
}

// ---- 2) UDP merge: 2 producers, 1 consumer ------------------------------------

func TestMultiPeer_UDP_TwoProducers_OneConsumer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := quietLogger()

	cons := consumer.NewPluginV10(logger)
	defer func() { _ = cons.Disconnect() }()
	if err := cons.Connect(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("consumer: %v", err)
	}
	bus := make(chan codec.Message, 16)
	_ = cons.SubscribePattern("", func(ev consumer.PacketEvent) { bus <- ev.Msg })

	srvA := provider.NewServerV10(logger)
	srvB := provider.NewServerV10(logger)
	defer func() { _ = srvA.Stop() }()
	defer func() { _ = srvB.Stop() }()
	for _, s := range []*provider.Server{srvA, srvB} {
		if err := s.Bind("127.0.0.1:0"); err != nil {
			t.Fatalf("bind: %v", err)
		}
		if err := s.AddDestination("127.0.0.1", cons.BoundAddr().Port); err != nil {
			t.Fatalf("add dest: %v", err)
		}
	}

	const each = 3
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < each; i++ {
			_ = srvA.SendMessage(codec.Message{Address: fmt.Sprintf("/A/%d", i), Args: []codec.Arg{codec.Int32(int32(i))}})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < each; i++ {
			_ = srvB.SendMessage(codec.Message{Address: fmt.Sprintf("/B/%d", i), Args: []codec.Arg{codec.Int32(int32(i))}})
		}
	}()
	wg.Wait()

	addrs := drainAddrs(t, bus, 2*each, 3*time.Second)
	gotA, gotB := 0, 0
	for a := range addrs {
		if a[1] == 'A' {
			gotA++
		} else if a[1] == 'B' {
			gotB++
		}
	}
	if gotA != each || gotB != each {
		t.Errorf("merge: gotA=%d gotB=%d, want %d each", gotA, gotB, each)
	}
}

// ---- 3) Mixed UDP: 2 producers + 2 consumers on shared bus --------------------

func TestMultiPeer_UDP_TwoProducers_TwoConsumers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := quietLogger()

	c1 := consumer.NewPluginV10(logger)
	c2 := consumer.NewPluginV10(logger)
	defer func() { _ = c1.Disconnect() }()
	defer func() { _ = c2.Disconnect() }()
	if err := c1.Connect(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("c1: %v", err)
	}
	if err := c2.Connect(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("c2: %v", err)
	}

	var c1Hits, c2Hits int64
	_ = c1.SubscribePattern("", func(ev consumer.PacketEvent) { atomic.AddInt64(&c1Hits, 1) })
	_ = c2.SubscribePattern("", func(ev consumer.PacketEvent) { atomic.AddInt64(&c2Hits, 1) })

	srvA := provider.NewServerV10(logger)
	srvB := provider.NewServerV10(logger)
	defer func() { _ = srvA.Stop() }()
	defer func() { _ = srvB.Stop() }()
	for _, s := range []*provider.Server{srvA, srvB} {
		if err := s.Bind("127.0.0.1:0"); err != nil {
			t.Fatalf("bind: %v", err)
		}
		_ = s.AddDestination("127.0.0.1", c1.BoundAddr().Port)
		_ = s.AddDestination("127.0.0.1", c2.BoundAddr().Port)
	}

	const each = 4
	for i := 0; i < each; i++ {
		_ = srvA.SendMessage(codec.Message{Address: "/A", Args: []codec.Arg{codec.Int32(int32(i))}})
		_ = srvB.SendMessage(codec.Message{Address: "/B", Args: []codec.Arg{codec.Int32(int32(i))}})
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&c1Hits) >= 2*each && atomic.LoadInt64(&c2Hits) >= 2*each {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := atomic.LoadInt64(&c1Hits); got != 2*each {
		t.Errorf("c1: %d hits, want %d", got, 2*each)
	}
	if got := atomic.LoadInt64(&c2Hits); got != 2*each {
		t.Errorf("c2: %d hits, want %d", got, 2*each)
	}
}

// ---- 4) TCP length-prefix: 1 producer, 2 consumers, separate connections -----

func TestMultiPeer_TCPLenPrefix_OneProducer_TwoConsumers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := quietLogger()

	c1 := consumer.NewPluginV10(logger)
	c2 := consumer.NewPluginV10(logger)
	defer func() { _ = c1.Disconnect() }()
	defer func() { _ = c2.Disconnect() }()
	if err := c1.ConnectTCP(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("c1 tcp: %v", err)
	}
	if err := c2.ConnectTCP(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("c2 tcp: %v", err)
	}
	got1 := make(chan codec.Message, 8)
	got2 := make(chan codec.Message, 8)
	_ = c1.SubscribePattern("", func(ev consumer.PacketEvent) { got1 <- ev.Msg })
	_ = c2.SubscribePattern("", func(ev consumer.PacketEvent) { got2 <- ev.Msg })

	srv := provider.NewServerV10(logger)
	defer func() { _ = srv.Stop() }()

	const frames = 3
	for i := 0; i < frames; i++ {
		m := codec.Message{Address: fmt.Sprintf("/lp/%d", i), Args: []codec.Arg{codec.Int32(int32(i))}}
		if err := srv.SendMessageTCP("127.0.0.1", c1.BoundTCPAddr().Port, m); err != nil {
			t.Fatalf("send c1: %v", err)
		}
		if err := srv.SendMessageTCP("127.0.0.1", c2.BoundTCPAddr().Port, m); err != nil {
			t.Fatalf("send c2: %v", err)
		}
	}
	expectN(t, got1, frames, "tcp-lp c1")
	expectN(t, got2, frames, "tcp-lp c2")
}

// ---- 5) TCP-SLIP: 2 producers (osc-v11 servers) → 1 consumer ----------------

func TestMultiPeer_TCPSLIP_TwoProducers_OneConsumer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := quietLogger()

	cons := consumer.NewPluginV11(logger)
	defer func() { _ = cons.Disconnect() }()
	if err := cons.ConnectTCP(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("consumer: %v", err)
	}
	bus := make(chan codec.Message, 16)
	_ = cons.SubscribePattern("", func(ev consumer.PacketEvent) { bus <- ev.Msg })

	srvA := provider.NewServerV11(logger)
	srvB := provider.NewServerV11(logger)
	defer func() { _ = srvA.Stop() }()
	defer func() { _ = srvB.Stop() }()

	const each = 3
	port := cons.BoundTCPAddr().Port
	for i := 0; i < each; i++ {
		// V11 message that exercises 1.1-only payload-less tags T+F+N+I.
		mA := codec.Message{Address: fmt.Sprintf("/A/%d", i), Args: []codec.Arg{codec.True(), codec.Int32(int32(i))}}
		mB := codec.Message{Address: fmt.Sprintf("/B/%d", i), Args: []codec.Arg{codec.False(), codec.Int32(int32(i))}}
		if err := srvA.SendMessageTCP("127.0.0.1", port, mA); err != nil {
			t.Fatalf("A send: %v", err)
		}
		if err := srvB.SendMessageTCP("127.0.0.1", port, mB); err != nil {
			t.Fatalf("B send: %v", err)
		}
	}
	addrs := drainAddrs(t, bus, 2*each, 3*time.Second)
	gotA, gotB := 0, 0
	for a := range addrs {
		if a[1] == 'A' {
			gotA++
		} else if a[1] == 'B' {
			gotB++
		}
	}
	if gotA != each || gotB != each {
		t.Errorf("slip merge: gotA=%d gotB=%d, want %d each", gotA, gotB, each)
	}
}

// ---- helpers ------------------------------------------------------------------

func expectN(t *testing.T, ch <-chan codec.Message, n int, label string) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	got := 0
	for got < n {
		select {
		case <-ch:
			got++
		case <-deadline:
			t.Fatalf("%s: got %d frames, want %d", label, got, n)
		}
	}
}

// drainAddrs returns a set of all addresses received within the timeout.
func drainAddrs(t *testing.T, ch <-chan codec.Message, expect int, timeout time.Duration) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	deadline := time.After(timeout)
	for len(out) < expect {
		select {
		case m := <-ch:
			out[m.Address] = true
		case <-deadline:
			return out
		}
	}
	return out
}
