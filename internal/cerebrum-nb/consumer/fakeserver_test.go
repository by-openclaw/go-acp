package cerebrumnb

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

// fakeCerebrum is an in-process RFC 6455 server that speaks just enough of
// the Cerebrum NB wire protocol to drive the consumer end to end. There is
// NO live peer and NO vendor emulator available (NB licence missing), so
// every consumer test runs against this fake: it completes the WebSocket
// handshake, then a per-test handler reads client XML frames and writes
// canned 0v13 reply / event frames (ACK / NACK / BUSY / LOGIN_REPLY /
// POLL_REPLY / *_CHANGE) back.
//
// The handler runs on the post-handshake net.Conn. Helpers below read one
// client text frame (unmasking it) and write a server text frame.
type fakeCerebrum struct {
	ln      net.Listener
	handler func(fc *fakeConn)

	mu    sync.Mutex
	conns []net.Conn
}

// fakeConn wraps the server side of one accepted connection with framed
// read/write helpers.
type fakeConn struct {
	c  net.Conn
	br *bufio.Reader
}

func newFakeCerebrum(t *testing.T, handler func(*fakeConn)) *fakeCerebrum {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	fs := &fakeCerebrum{ln: ln, handler: handler}
	go fs.serve()
	t.Cleanup(func() {
		_ = ln.Close()
		fs.mu.Lock()
		for _, c := range fs.conns {
			_ = c.Close()
		}
		fs.mu.Unlock()
	})
	return fs
}

func (fs *fakeCerebrum) host() string {
	return fs.ln.Addr().(*net.TCPAddr).IP.String()
}
func (fs *fakeCerebrum) port() int {
	return fs.ln.Addr().(*net.TCPAddr).Port
}

func (fs *fakeCerebrum) serve() {
	for {
		c, err := fs.ln.Accept()
		if err != nil {
			return
		}
		fs.mu.Lock()
		fs.conns = append(fs.conns, c)
		fs.mu.Unlock()
		go fs.handle(c)
	}
}

func (fs *fakeCerebrum) handle(c net.Conn) {
	br := bufio.NewReader(c)
	req, err := http.ReadRequest(br)
	if err != nil {
		_ = c.Close()
		return
	}
	key := req.Header.Get("Sec-WebSocket-Key")
	h := sha1.New()
	h.Write([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	accept := base64.StdEncoding.EncodeToString(h.Sum(nil))
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	if _, err := io.WriteString(c, resp); err != nil {
		_ = c.Close()
		return
	}
	if fs.handler != nil {
		fs.handler(&fakeConn{c: c, br: br})
	}
}

// readClientFrame reads one masked client text frame and returns the
// unmasked payload. Control frames are skipped (Close ends the read).
func (fc *fakeConn) readClientFrame() ([]byte, error) {
	for {
		var hdr [2]byte
		if _, err := io.ReadFull(fc.br, hdr[:]); err != nil {
			return nil, err
		}
		opcode := hdr[0] & 0x0f
		masked := hdr[1]&0x80 != 0
		plen := int64(hdr[1] & 0x7f)
		switch plen {
		case 126:
			var ext [2]byte
			if _, err := io.ReadFull(fc.br, ext[:]); err != nil {
				return nil, err
			}
			plen = int64(binary.BigEndian.Uint16(ext[:]))
		case 127:
			var ext [8]byte
			if _, err := io.ReadFull(fc.br, ext[:]); err != nil {
				return nil, err
			}
			plen = int64(binary.BigEndian.Uint64(ext[:]))
		}
		var mask [4]byte
		if masked {
			if _, err := io.ReadFull(fc.br, mask[:]); err != nil {
				return nil, err
			}
		}
		body := make([]byte, plen)
		if _, err := io.ReadFull(fc.br, body); err != nil {
			return nil, err
		}
		if masked {
			for i := range body {
				body[i] ^= mask[i&3]
			}
		}
		switch opcode {
		case 0x8: // close
			return nil, io.EOF
		case 0x9: // ping — answer with a pong, as a real server does.
			// The client's keep-alive relies on this: the pong is the
			// inbound frame that re-arms its idle read deadline and proves
			// the peer is alive. A fake that stayed silent here would make
			// every healthy session look dead.
			if err := fc.writePong(body); err != nil {
				return nil, err
			}
			continue
		case 0xA: // pong — ignore
			continue
		default:
			return body, nil
		}
	}
}

// writeText writes an unmasked server->client text frame.
func (fc *fakeConn) writeText(payload []byte) error {
	hdr := []byte{0x81}
	n := len(payload)
	switch {
	case n <= 125:
		hdr = append(hdr, byte(n))
	case n <= 0xffff:
		hdr = append(hdr, 126, 0, 0)
		binary.BigEndian.PutUint16(hdr[len(hdr)-2:], uint16(n))
	default:
		hdr = append(hdr, 127)
		ext := make([]byte, 8)
		binary.BigEndian.PutUint64(ext, uint64(n))
		hdr = append(hdr, ext...)
	}
	if _, err := fc.c.Write(hdr); err != nil {
		return err
	}
	_, err := fc.c.Write(payload)
	return err
}

// writePing writes a server->client ping (control frame, handled inline by
// the client's ws.Conn and never surfaced to the consumer dispatcher).
func (fc *fakeConn) writePing() error {
	_, err := fc.c.Write([]byte{0x89, 0})
	return err
}

// writePong answers a client ping. Body must be echoed verbatim per
// RFC 6455 §5.5.3 and is always ≤125 bytes for a control frame.
func (fc *fakeConn) writePong(body []byte) error {
	if len(body) > 125 {
		body = body[:125]
	}
	if _, err := fc.c.Write([]byte{0x8A, byte(len(body))}); err != nil {
		return err
	}
	if len(body) == 0 {
		return nil
	}
	_, err := fc.c.Write(body)
	return err
}

// writeBinary writes a server->client BINARY frame. Cerebrum never speaks
// binary; the consumer readLoop must drop it (and log) rather than decode.
func (fc *fakeConn) writeBinary(payload []byte) error {
	hdr := []byte{0x82, byte(len(payload))} // FIN + opcode 0x2, len <=125
	if _, err := fc.c.Write(hdr); err != nil {
		return err
	}
	_, err := fc.c.Write(payload)
	return err
}

// mtidOf extracts the MTID="..." attribute value from a raw client frame
// so canned replies can echo the right mtid.
func mtidOf(frame []byte) string {
	return attrOf(frame, "MTID")
}

func attrOf(frame []byte, key string) string {
	s := string(frame)
	needle := key + `="`
	i := indexFold(s, needle)
	if i < 0 {
		return ""
	}
	i += len(needle)
	j := i
	for j < len(s) && s[j] != '"' {
		j++
	}
	if j >= len(s) {
		return ""
	}
	return s[i:j]
}

// indexFold is a tiny ASCII case-insensitive substring search (frames use
// UPPERCASE keys; tests sometimes assert on them generically).
func indexFold(s, sub string) int {
	if sub == "" {
		return 0
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if eqFold(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}

func eqFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'a' <= ca && ca <= 'z' {
			ca -= 32
		}
		if 'a' <= cb && cb <= 'z' {
			cb -= 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// serveLoginThen runs the standard login handshake (read one LOGIN, reply
// LOGIN_REPLY) then hands off to next with the same fakeConn. Used by tests
// that need a logged-in session before exercising another verb.
func serveLoginThen(fc *fakeConn, apiVer string, next func(*fakeConn)) {
	frame, err := fc.readClientFrame()
	if err != nil {
		return
	}
	mtid := mtidOf(frame)
	_ = fc.writeText([]byte(`<LOGIN_REPLY MTID="` + mtid + `" API_VER="` + apiVer + `"/>`))
	if next != nil {
		next(fc)
	}
}

// drainClient keeps reading (and discarding) client frames until the
// connection closes — lets a handler stay alive while the test issues
// several requests it answers inline via a callback.
func drainClient(fc *fakeConn, onFrame func(frame []byte) (reply []byte, ok bool)) {
	for {
		frame, err := fc.readClientFrame()
		if err != nil {
			return
		}
		reply, ok := onFrame(frame)
		if !ok {
			return
		}
		if reply != nil {
			if err := fc.writeText(reply); err != nil {
				return
			}
		}
	}
}

// ackFor builds an <ACK MTID="…"/> for the given client frame.
func ackFor(frame []byte) []byte {
	return []byte(`<ACK MTID="` + mtidOf(frame) + `"/>`)
}

// nackFor builds an <NACK> reply with the given error / error_code.
func nackFor(frame []byte, errName string, code int) []byte {
	return []byte(`<NACK MTID="` + mtidOf(frame) + `" ERROR="` + errName + `" ERROR_CODE="` + itoa(code) + `"/>`)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// waitFor polls cond until true or the deadline; fails the test otherwise.
func waitFor(t *testing.T, d time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("waitFor timed out: %s", msg)
}
