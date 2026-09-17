package acp1

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// adminServerAddr starts ServeAdmin and returns its listening address by
// reading the discovery file (so raw-frame tests can dial it directly).
func adminServerAddr(t *testing.T, s *server, name string) string {
	t.Helper()
	c := startAdminServer(t, s, name)
	return c.addr
}

// fakeListener returns scripted Accept results then blocks (so the serve
// loop's accept call drives the warn arm then the closed-exit arm).
type fakeListener struct {
	results []struct {
		c   net.Conn
		err error
	}
	pos  int
	addr net.Addr
}

func (l *fakeListener) Accept() (net.Conn, error) {
	if l.pos >= len(l.results) {
		return nil, net.ErrClosed
	}
	r := l.results[l.pos]
	l.pos++
	return r.c, r.err
}
func (l *fakeListener) Close() error   { return nil }
func (l *fakeListener) Addr() net.Addr { return l.addr }

type fakeAddr struct{}

func (fakeAddr) Network() string { return "tcp" }
func (fakeAddr) String() string  { return "127.0.0.1:0" }

// TestServeAdmin_ListenError: an injected listen that fails surfaces from
// ServeAdmin.
func TestServeAdmin_ListenError(t *testing.T) {
	s := newTestServer(t)
	s.adminListenHook = func() (net.Listener, error) { return nil, errAdminIO }
	if err := s.ServeAdmin(context.Background(), "listen-err"); err == nil {
		t.Fatal("listen error: want error")
	}
}

// TestServeAdmin_DiscoveryWriteError: an injected discovery write that fails
// surfaces from ServeAdmin (and closes the listener).
func TestServeAdmin_DiscoveryWriteError(t *testing.T) {
	s := newTestServer(t)
	s.adminWriteDiscoveryHook = func(string, *AdminDiscovery) error { return errAdminIO }
	if err := s.ServeAdmin(context.Background(), "disc-err"); err == nil {
		t.Fatal("discovery write error: want error")
	}
}

// TestServeAdmin_AcceptWarn: an injected listener whose Accept returns a
// transient (non-closed) error once, then ErrClosed, drives the accept warn.
func TestServeAdmin_AcceptWarn(t *testing.T) {
	s := newTestServer(t)
	s.adminListenHook = func() (net.Listener, error) {
		return &fakeListener{
			addr: fakeAddr{},
			results: []struct {
				c   net.Conn
				err error
			}{
				{nil, errAdminIO},    // transient → warn + continue
				{nil, net.ErrClosed}, // → return nil
			},
		}, nil
	}
	// Discovery write would try the real temp dir; that's fine.
	if err := s.ServeAdmin(context.Background(), "accept-warn"); err != nil {
		t.Fatalf("ServeAdmin: %v", err)
	}
}

// TestServeAdmin_NameDefault: an empty name defaults to "dhs-acp1".
func TestServeAdmin_NameDefault(t *testing.T) {
	s := newTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.ServeAdmin(ctx, "") }()
	// The discovery file is written under the sanitised default name.
	deadline := time.Now().Add(2 * time.Second)
	var ok bool
	for time.Now().Before(deadline) {
		if _, err := ReadAdminDiscovery(""); err == nil {
			ok = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	if !ok {
		t.Fatal("default-named discovery file not found")
	}
}

// TestWriteAdminDiscovery_BadPath: a path under a non-existent directory
// fails the WriteFile step.
func TestWriteAdminDiscovery_BadPath(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "no-such-dir", "disc.json")
	if err := writeAdminDiscovery(bad, &AdminDiscovery{Name: "x"}); err == nil {
		t.Fatal("bad path: want error")
	}
}

// TestWriteAdminDiscovery_RenameError: when the target path is an existing
// directory, WriteFile(tmp) succeeds but os.Rename(tmp, dir) fails.
func TestWriteAdminDiscovery_RenameError(t *testing.T) {
	dir := t.TempDir() // an existing directory; Rename onto it fails
	if err := writeAdminDiscovery(dir, &AdminDiscovery{Name: "x"}); err == nil {
		t.Fatal("rename onto a directory: want error")
	}
}

// TestReadAdminDiscovery_DecodeError: a garbage discovery file fails decode.
func TestReadAdminDiscovery_DecodeError(t *testing.T) {
	name := "decode-err-test"
	p := AdminDiscoveryPath(name)
	if err := os.WriteFile(p, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	defer func() { _ = os.Remove(p) }()
	if _, err := ReadAdminDiscovery(name); err == nil {
		t.Fatal("garbage discovery: want decode error")
	}
}

// writeFrame writes a 4-byte length prefix + body to conn.
func writeAdminFrame(c net.Conn, body []byte) {
	var lb [4]byte
	binary.BigEndian.PutUint32(lb[:], uint32(len(body)))
	_, _ = c.Write(lb[:])
	_, _ = c.Write(body)
}

// TestHandleAdminConn_FrameErrors drives the server-side frame guards:
// zero-length frame, oversize frame, and a non-JSON body.
func TestHandleAdminConn_FrameErrors(t *testing.T) {
	s := newTestServer(t)
	addr := adminServerAddr(t, s, "frame-errs")

	dial := func() net.Conn {
		conn, err := net.DialTimeout("tcp4", addr, 2*time.Second)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		return conn
	}

	// Zero-length frame → "frame size 0 out of range".
	c1 := dial()
	var zero [4]byte
	_, _ = c1.Write(zero[:])
	_ = c1.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 64)
	_, _ = c1.Read(buf)
	_ = c1.Close()

	// Oversize frame length prefix → "frame size N out of range".
	c2 := dial()
	var big [4]byte
	binary.BigEndian.PutUint32(big[:], adminMaxFrame+1)
	_, _ = c2.Write(big[:])
	_ = c2.SetReadDeadline(time.Now().Add(time.Second))
	_, _ = c2.Read(buf)
	_ = c2.Close()

	// Non-JSON body → decode error.
	c3 := dial()
	writeAdminFrame(c3, []byte("not json at all"))
	_ = c3.SetReadDeadline(time.Now().Add(time.Second))
	_, _ = c3.Read(buf)
	_ = c3.Close()

	// Truncated: send a length prefix then close before the body → ReadFull
	// body error (silent return, no reply).
	c4 := dial()
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], 100)
	_, _ = c4.Write(hdr[:])
	_ = c4.Close()

	// Close immediately after connect → ReadFull lenBuf error (silent).
	c5 := dial()
	_ = c5.Close()

	time.Sleep(50 * time.Millisecond) // let the server-side goroutines run
}

// rawAdminServer accepts one connection and runs handler(conn), letting a
// test script arbitrary server-side behaviour for AdminClient.Call.
func rawAdminServer(t *testing.T, handler func(net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		handler(conn)
		_ = conn.Close()
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String()
}

// TestAdminCall_ReadAndDecodeErrors covers Call's read-len error (server
// closes after reading), reply-too-big guard, and reply-decode error.
func TestAdminCall_ReadAndDecodeErrors(t *testing.T) {
	// Server reads the request then closes → client read-len error.
	addr1 := rawAdminServer(t, func(c net.Conn) {
		buf := make([]byte, 256)
		_, _ = c.Read(buf)
		// close immediately (deferred) → client's ReadFull(len) fails
	})
	c1 := &AdminClient{addr: addr1}
	if _, err := c1.Call(context.Background(), &AdminRequest{Verb: "ping"}); err == nil {
		t.Error("server-close-after-read: want read error")
	}

	// Server replies with a length prefix exceeding adminMaxFrame.
	addr2 := rawAdminServer(t, func(c net.Conn) {
		buf := make([]byte, 256)
		_, _ = c.Read(buf)
		var lb [4]byte
		lb[0] = 0xFF // huge length
		_, _ = c.Write(lb[:])
		time.Sleep(50 * time.Millisecond)
	})
	c2 := &AdminClient{addr: addr2}
	if _, err := c2.Call(context.Background(), &AdminRequest{Verb: "ping"}); err == nil {
		t.Error("oversize reply: want error")
	}

	// Server replies with a valid length + garbage body → decode error.
	addr3 := rawAdminServer(t, func(c net.Conn) {
		buf := make([]byte, 256)
		_, _ = c.Read(buf)
		body := []byte("not json")
		var lb [4]byte
		lb[3] = byte(len(body))
		_, _ = c.Write(lb[:])
		_, _ = c.Write(body)
		time.Sleep(50 * time.Millisecond)
	})
	c3 := &AdminClient{addr: addr3}
	if _, err := c3.Call(context.Background(), &AdminRequest{Verb: "ping"}); err == nil {
		t.Error("garbage reply: want decode error")
	}

	// Server reads only the length prefix then closes → client read-body
	// error (sends a valid len, no body).
	addr4 := rawAdminServer(t, func(c net.Conn) {
		buf := make([]byte, 256)
		_, _ = c.Read(buf)
		var lb [4]byte
		lb[3] = 100 // promise 100 bytes, send none
		_, _ = c.Write(lb[:])
		// close → client ReadFull(body) fails
	})
	c4 := &AdminClient{addr: addr4}
	if _, err := c4.Call(context.Background(), &AdminRequest{Verb: "ping"}); err == nil {
		t.Error("truncated reply body: want error")
	}
}

// TestAdminCall_PeerRSTErrors: a server that accepts and immediately closes
// with SO_LINGER=0 (RST) makes the client's write or read fail. Looped so a
// transient success still eventually surfaces the error path.
func TestAdminCall_PeerRSTErrors(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			if tc, ok := c.(*net.TCPConn); ok {
				_ = tc.SetLinger(0)
			}
			_ = c.Close() // RST
		}
	}()
	c := &AdminClient{addr: ln.Addr().String()}
	sawErr := false
	for i := 0; i < 50 && !sawErr; i++ {
		if _, err := c.Call(context.Background(), &AdminRequest{Verb: "ping"}); err != nil {
			sawErr = true
		}
		time.Sleep(time.Millisecond)
	}
	if !sawErr {
		t.Skip("peer RST did not surface a Call error on this host")
	}
}

// TestAdminCall_MarshalError: a request whose Args hold an unmarshalable
// value (a channel) fails json.Marshal inside Call.
func TestAdminCall_MarshalError(t *testing.T) {
	s := newTestServer(t)
	c := startAdminServer(t, s, "marshal-req-err")
	_, err := c.Call(context.Background(), &AdminRequest{
		Verb: "ping",
		Args: map[string]any{"bad": make(chan int)},
	})
	if err == nil {
		t.Fatal("unmarshalable request Args: want marshal error")
	}
}

// scriptConn is a net.Conn whose Write fails after writeOK successful writes
// and whose Read returns readScript bytes then io.EOF.
type scriptConn struct {
	writeOK    int
	writes     int
	readScript []byte
	readPos    int
}

func (c *scriptConn) Write(p []byte) (int, error) {
	c.writes++
	if c.writes > c.writeOK {
		return 0, errAdminIO
	}
	return len(p), nil
}
func (c *scriptConn) Read(p []byte) (int, error) {
	if c.readPos >= len(c.readScript) {
		return 0, io.EOF
	}
	n := copy(p, c.readScript[c.readPos:])
	c.readPos += n
	return n, nil
}
func (c *scriptConn) Close() error                     { return nil }
func (c *scriptConn) LocalAddr() net.Addr              { return nil }
func (c *scriptConn) RemoteAddr() net.Addr             { return nil }
func (c *scriptConn) SetDeadline(time.Time) error      { return nil }
func (c *scriptConn) SetReadDeadline(time.Time) error  { return nil }
func (c *scriptConn) SetWriteDeadline(time.Time) error { return nil }

var errAdminIO = &adminIOErr{}

type adminIOErr struct{}

func (*adminIOErr) Error() string { return "synthetic admin io error" }

// TestAdminCall_ScriptedIOErrors deterministically drives Call's request
// write-len error, write-body error, and reply read-body error via an
// injected scripted conn.
func TestAdminCall_ScriptedIOErrors(t *testing.T) {
	// Write len fails (writeOK=0 → first Write errors).
	c1 := &AdminClient{dial: func() (net.Conn, error) { return &scriptConn{writeOK: 0}, nil }}
	if _, err := c1.Call(context.Background(), &AdminRequest{Verb: "ping"}); err == nil {
		t.Error("write-len error: want error")
	}
	// Write len OK, write body fails (writeOK=1).
	c2 := &AdminClient{dial: func() (net.Conn, error) { return &scriptConn{writeOK: 1}, nil }}
	if _, err := c2.Call(context.Background(), &AdminRequest{Verb: "ping"}); err == nil {
		t.Error("write-body error: want error")
	}
	// Writes OK; reply has a valid length prefix but a truncated body → read
	// body error.
	c3 := &AdminClient{dial: func() (net.Conn, error) {
		return &scriptConn{writeOK: 2, readScript: []byte{0x00, 0x00, 0x00, 0x10}}, nil // promise 16 bytes, none follow
	}}
	if _, err := c3.Call(context.Background(), &AdminRequest{Verb: "ping"}); err == nil {
		t.Error("read-body error: want error")
	}
}

// TestAdminCall_DialError: a client pointed at a dead address fails to dial.
func TestAdminCall_DialError(t *testing.T) {
	c := &AdminClient{addr: "127.0.0.1:1"} // nothing listening
	if _, err := c.Call(context.Background(), &AdminRequest{Verb: "ping"}); err == nil {
		t.Fatal("dead address: want dial error")
	}
}

// TestAdminCall_WithDeadline exercises the ctx.Deadline() branch in Call.
func TestAdminCall_WithDeadline(t *testing.T) {
	s := newTestServer(t)
	c := startAdminServer(t, s, "deadline-test")
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(5*time.Second))
	defer cancel()
	if _, err := c.Call(ctx, &AdminRequest{Verb: "ping"}); err != nil {
		t.Fatalf("Call with deadline: %v", err)
	}
}

// TestWriteAdminResp_MarshalFallback: a handler whose Result holds a value
// json can't marshal (a channel) drives writeAdminResp's marshal-error
// fallback.
func TestWriteAdminResp_MarshalFallback(t *testing.T) {
	s := newTestServer(t)
	const verb = "test-unmarshalable"
	registerAdminHandler(verb, func(context.Context, *server, map[string]any) (*AdminResponse, error) {
		return &AdminResponse{Result: map[string]any{"bad": make(chan int)}}, nil
	})
	c := startAdminServer(t, s, "marshal-fallback")
	// The reply Result can't be marshalled → server falls back to an error
	// response. The client should still decode a well-formed (error) reply.
	resp, err := c.Call(context.Background(), &AdminRequest{Verb: verb})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Status != "error" {
		t.Errorf("status = %q, want error (marshal fallback)", resp.Status)
	}
}

// TestHandleAdminRequest_NilResponse: a handler returning (nil, nil) yields
// a default ok response.
func TestHandleAdminRequest_NilResponse(t *testing.T) {
	s := newTestServer(t)
	const verb = "test-nilresp-verb"
	registerAdminHandler(verb, func(context.Context, *server, map[string]any) (*AdminResponse, error) {
		return nil, nil
	})
	resp := s.handleAdminRequest(context.Background(), &AdminRequest{Verb: verb})
	if resp == nil || resp.Status != "ok" {
		t.Fatalf("nil handler response → %+v, want ok", resp)
	}
}
