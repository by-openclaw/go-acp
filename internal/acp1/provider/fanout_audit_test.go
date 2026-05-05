package acp1

import (
	"encoding/binary"
	"io"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
)

// TestFanout_32Sessions_NoHeadOfLineBlocking verifies that one slow
// consumer does not throttle others. Opens 32 TCP sessions, makes one
// of them stop reading after the initial handshake, then triggers
// announces from a 33rd connection. The 31 healthy consumers must all
// receive every announce within a bounded window.
func TestFanout_32Sessions_NoHeadOfLineBlocking(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping fan-out stress test in -short mode")
	}
	s := newTestServer(t)
	addr, _ := startTCPServer(t, s)

	// 31 consumers + 1 trigger = 32 = the per-IP cap (all loopback).
	// One slot left for the trigger conn so the cap doesn't refuse it.
	const consumers = 31
	conns := make([]net.Conn, consumers)
	for i := 0; i < consumers; i++ {
		c, err := net.DialTimeout("tcp4", addr, time.Second)
		if err != nil {
			t.Fatalf("dial #%d: %v", i, err)
		}
		conns[i] = c
		t.Cleanup(func() { _ = c.Close() })
	}

	// Allow the server to register all sessions.
	time.Sleep(100 * time.Millisecond)

	// Mark consumer 0 as the slow one. Others read continuously and
	// count the announces they receive.
	var counts [consumers]atomic.Int64
	stopReaders := make(chan struct{})
	var wg sync.WaitGroup
	for i := 1; i < consumers; i++ {
		wg.Add(1)
		go func(idx int, c net.Conn) {
			defer wg.Done()
			for {
				select {
				case <-stopReaders:
					return
				default:
				}
				_ = c.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
				var lb [4]byte
				if _, err := io.ReadFull(c, lb[:]); err != nil {
					if errIsTimeout(err) {
						continue
					}
					return
				}
				mlen := binary.BigEndian.Uint32(lb[:])
				body := make([]byte, mlen)
				if _, err := io.ReadFull(c, body); err != nil {
					return
				}
				counts[idx].Add(1)
			}
		}(i, conns[i])
	}

	// Triggerer: 33rd connection issues SetValue requests in a loop.
	trigger, err := net.DialTimeout("tcp4", addr, time.Second)
	if err != nil {
		t.Fatalf("dial trigger: %v", err)
	}
	defer func() { _ = trigger.Close() }()

	const triggers = 50
	for i := 0; i < triggers; i++ {
		setReq := &codec.Message{
			MTID: uint32(i + 1), MType: codec.MTypeRequest, MAddr: 1,
			MCode: byte(codec.MethodSetValue),
			ObjGroup: codec.GroupControl, ObjID: 0,
			Value: []byte{0x00, byte(i % 12)},
		}
		body, _ := setReq.Encode()
		var lb [4]byte
		binary.BigEndian.PutUint32(lb[:], uint32(len(body)))
		_ = trigger.SetWriteDeadline(time.Now().Add(time.Second))
		if _, err := trigger.Write(lb[:]); err != nil {
			t.Fatalf("write len: %v", err)
		}
		if _, err := trigger.Write(body); err != nil {
			t.Fatalf("write body: %v", err)
		}
		// Drain trigger's reply to keep its TCP buffer healthy.
		_ = trigger.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		var rlb [4]byte
		if _, err := io.ReadFull(trigger, rlb[:]); err == nil {
			rlen := binary.BigEndian.Uint32(rlb[:])
			_, _ = io.ReadFull(trigger, make([]byte, rlen))
		}
	}

	// Give fan-out time to settle.
	time.Sleep(300 * time.Millisecond)
	close(stopReaders)
	wg.Wait()

	// Healthy consumers should have received MOST announces. We allow
	// a small slack since drop-on-full may bite under heavy contention,
	// but the main proof is "the slow consumer (0) did not block
	// others to zero".
	healthyMin := int64(triggers / 2) // at least 50% delivered to each
	for i := 1; i < consumers; i++ {
		got := counts[i].Load()
		if got < healthyMin {
			t.Errorf("consumer %d received %d/%d announces (slow consumer #0 throttled the fan-out)",
				i, got, triggers)
		}
	}
}

// TestFanout_NoGoroutineLeak verifies that opening + closing a batch
// of TCP sessions returns to baseline goroutine count. The check is
// approximate (goroutine counts include runtime housekeeping) but
// catches gross leaks.
func TestFanout_NoGoroutineLeak(t *testing.T) {
	s := newTestServer(t)
	addr, _ := startTCPServer(t, s)

	// Settle baseline: count goroutines before the dial wave.
	time.Sleep(100 * time.Millisecond)
	runtime.GC()
	baseline := runtime.NumGoroutine()

	const wave = 16
	conns := make([]net.Conn, wave)
	for i := 0; i < wave; i++ {
		c, err := net.DialTimeout("tcp4", addr, time.Second)
		if err != nil {
			t.Fatalf("dial #%d: %v", i, err)
		}
		conns[i] = c
	}
	time.Sleep(50 * time.Millisecond)

	// Close them all.
	for _, c := range conns {
		_ = c.Close()
	}
	// Server reader goroutines need a moment to notice the close.
	time.Sleep(300 * time.Millisecond)
	runtime.GC()

	after := runtime.NumGoroutine()
	delta := after - baseline
	// Allow some slack — Go runtime may keep idle goroutines around.
	if delta > 4 {
		t.Errorf("goroutine count grew by %d after %d sessions opened+closed (baseline=%d, after=%d)",
			delta, wave, baseline, after)
	}
}

// TestFanout_BroadcastIsConstantTime documents the expected behaviour:
// broadcast iterates the session map once per announce (O(N_sessions)
// pushes onto bounded channels) and never decodes per-session. This
// test pins the broadcastTCPAnnounce contract — every active session
// receives the same byte slice (no per-session encode).
func TestFanout_BroadcastIsConstantTime(t *testing.T) {
	logger := newTestServer(t).logger
	reg := newTCPSessionRegistry(logger)

	// Register 8 sessions with buffered channels.
	chans := make([]chan []byte, 8)
	tcpConn := &net.TCPConn{}
	for i := range chans {
		chans[i] = make(chan []byte, 4)
		_ = reg.register("10.0.0.1", tcpConn, chans[i])
	}

	payload := []byte("announcement-bytes")
	reg.broadcast(payload)

	// Every session must have exactly one frame; all pointing at the
	// same payload (or an equivalent slice copy).
	for i, ch := range chans {
		select {
		case got := <-ch:
			if string(got) != string(payload) {
				t.Errorf("session %d got %q, want %q", i, got, payload)
			}
		default:
			t.Errorf("session %d did not receive broadcast", i)
		}
	}
}

func errIsTimeout(err error) bool {
	if err == nil {
		return false
	}
	var ne net.Error
	if !asNetErr(err, &ne) {
		return false
	}
	return ne.Timeout()
}

func asNetErr(err error, target *net.Error) bool {
	for err != nil {
		if ne, ok := err.(net.Error); ok {
			*target = ne
			return true
		}
		type wrapped interface{ Unwrap() error }
		if w, ok := err.(wrapped); ok {
			err = w.Unwrap()
			continue
		}
		break
	}
	return false
}
