package probelsw08p

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"dhs/internal/probel-sw08p/codec"
)

// matrixPeer plays the matrix side of a SW-P-08 session over one half of
// a net.Pipe. It runs a reader goroutine that, per SW-P-08 §2, emits a
// DLE ACK for every well-framed inbound frame and then invokes the
// caller-supplied handler with the decoded request. The handler returns
// the reply frames (already codec.Encode*'d) the peer should write back;
// each reply is Pack'd and is itself ACKed by the plugin's reader.
//
// A nil/empty reply slice means "ACK only, no application reply" — the
// shape Maintenance / UpdateNameRequest expect.
type matrixPeer struct {
	t       *testing.T
	conn    net.Conn
	handler func(req codec.Frame) []codec.Frame

	mu       sync.Mutex
	requests []codec.Frame
	done     chan struct{}
}

// newPeerHarness wires a Plugin to a fresh in-memory matrix peer. The
// returned Plugin has a live codec.Client on the A side; the peer plays
// the B side using handler to script replies. cleanup tears both down.
func newPeerHarness(t *testing.T, handler func(req codec.Frame) []codec.Frame) (*Plugin, *matrixPeer, func()) {
	t.Helper()
	a, b := net.Pipe()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	disable := false
	client := codec.NewClientFromConn(a, logger, codec.ClientConfig{WireHexLog: &disable})

	peer := &matrixPeer{
		t:       t,
		conn:    b,
		handler: handler,
		done:    make(chan struct{}),
	}
	go peer.run()

	p := &Plugin{logger: logger}
	p.client = client
	p.host = "test"
	p.port = codec.DefaultPort

	cleanup := func() {
		_ = client.Close()
		_ = b.Close()
		select {
		case <-peer.done:
		case <-time.After(2 * time.Second):
			t.Error("matrix peer did not exit")
		}
	}
	return p, peer, cleanup
}

// run is the peer's read loop: accumulate bytes, ACK each frame, then
// write the handler's replies.
func (m *matrixPeer) run() {
	defer close(m.done)
	buf := make([]byte, 0, 512)
	tmp := make([]byte, 512)
	for {
		n, err := m.conn.Read(tmp)
		if err != nil {
			return
		}
		buf = append(buf, tmp[:n]...)
		for len(buf) >= 2 {
			if codec.IsACK(buf) || codec.IsNAK(buf) {
				buf = buf[2:]
				continue
			}
			if buf[0] != codec.DLE {
				buf = buf[1:]
				continue
			}
			f, consumed, perr := codec.Unpack(buf)
			if errors.Is(perr, io.ErrUnexpectedEOF) {
				break
			}
			if perr != nil {
				if _, werr := m.conn.Write(codec.PackNAK()); werr != nil {
					return
				}
				buf = buf[2:]
				continue
			}
			buf = buf[consumed:]

			// Record the request before ACKing so a match==nil Send that
			// returns on ACK alone always sees it via lastRequest().
			m.mu.Lock()
			m.requests = append(m.requests, f)
			handler := m.handler
			m.mu.Unlock()

			// §2: ACK the request.
			if _, werr := m.conn.Write(codec.PackACK()); werr != nil {
				return
			}

			if handler == nil {
				continue
			}
			// Stream the replies from a separate goroutine so this read loop
			// keeps draining the plugin's per-frame ACKs. On a synchronous
			// net.Pipe a multi-frame reply would otherwise deadlock: the peer
			// blocks writing reply N+1 while the plugin blocks writing the ACK
			// for reply N with nobody reading it. Real matrices likewise stream
			// table replies asynchronously while the consumer ACKs each frame.
			replies := handler(f)
			go func() {
				for _, reply := range replies {
					if _, werr := m.conn.Write(codec.Pack(reply)); werr != nil {
						return
					}
				}
			}()
		}
	}
}

// lastRequest returns the most recent decoded request the peer received.
func (m *matrixPeer) lastRequest() (codec.Frame, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.requests) == 0 {
		return codec.Frame{}, false
	}
	return m.requests[len(m.requests)-1], true
}

// replyOnce returns a handler that emits the given reply frames for the
// first request and ACK-only thereafter.
func replyWith(frames ...codec.Frame) func(codec.Frame) []codec.Frame {
	return func(codec.Frame) []codec.Frame { return frames }
}

// ackOnly is a handler that never writes an application reply.
func ackOnly(codec.Frame) []codec.Frame { return nil }
