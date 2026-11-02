package acp1

import (
	"net"
	"sync"
	"testing"
	"time"

	an2 "dhs/internal/acp2/codec"
)

// A session that ends while announces are fanned out must not be reached
// by the fan-out once its send channel is closed: a send on a closed
// channel is a panic, and it took the whole provider down with every
// other session on it. The session leaves the registry first.
//
// Waves of sessions open and close while a fan-out runs without pause.
// With the channel closed before the session left the registry this
// panics (and the race detector reports the close against the send).
func TestSessionEndingDuringAnnounceFanout(t *testing.T) {
	const waves, perWave = 20, 16
	announce := []byte{0x00, 0x00, 0x00, 0x01, 0x00}

	t.Run("tcp", func(t *testing.T) {
		s := newTestServer(t)
		addr, _ := startTCPServer(t, s)
		stop := fanOutUntilStopped(func() { s.broadcastTCPAnnounce(announce, 0) })
		defer stop()

		for w := 0; w < waves; w++ {
			conns := dialWave(t, addr, perWave)
			waitForSessions(t, s, perWave)
			closeWave(conns)
			waitForNoSessions(t, func() int { return tcpRegistryOf(s).activeSessions() })
		}
	})

	t.Run("an2", func(t *testing.T) {
		s := newTestServer(t)
		addr := startAN2Server(t, s)
		stop := fanOutUntilStopped(func() { s.broadcastAN2Announce(announce) })
		defer stop()

		for w := 0; w < waves; w++ {
			conns := dialWave(t, addr, perWave)
			// Only a session that asked for ACP1 events is fanned out to.
			for i, c := range conns {
				_ = c.SetDeadline(time.Now().Add(5 * time.Second))
				sendAN2(t, c, &an2.AN2Frame{
					Proto: an2.AN2ProtoInternal, Slot: 0, MTID: uint8(i + 1), Type: an2.AN2TypeRequest,
					Payload: []byte{an2.AN2FuncEnableProtocolEvents, byte(an2.AN2ProtoACP1)},
				})
			}
			// Let the fan-out reach them before they go.
			time.Sleep(20 * time.Millisecond)
			closeWave(conns)
			waitForNoSessions(t, func() int { return an2SessionCount(s) })
		}
	})
}

func fanOutUntilStopped(fanOut func()) (stop func()) {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				fanOut()
			}
		}
	}()
	return func() { close(done); wg.Wait() }
}

func dialWave(t *testing.T, addr string, n int) []net.Conn {
	t.Helper()
	conns := make([]net.Conn, 0, n)
	for i := 0; i < n; i++ {
		c, err := net.DialTimeout("tcp4", addr, 2*time.Second)
		if err != nil {
			closeWave(conns)
			t.Fatalf("dial #%d: %v", i, err)
		}
		conns = append(conns, c)
	}
	return conns
}

func closeWave(conns []net.Conn) {
	for _, c := range conns {
		_ = c.Close()
	}
}

func waitForNoSessions(t *testing.T, count func() int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for count() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%d session(s) still registered 10 s after their sockets closed", count())
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func tcpRegistryOf(s *server) *tcpSessionRegistry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tcpRegistry
}

func an2SessionCount(s *server) int {
	s.mu.Lock()
	reg := s.an2Registry
	s.mu.Unlock()
	if reg == nil {
		return 0
	}
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	return len(reg.sessions)
}
