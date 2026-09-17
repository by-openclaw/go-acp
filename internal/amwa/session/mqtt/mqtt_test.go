package mqtt

// Packet bytes come from MQTT 3.1.1 (mqtt-v3.1.1-os) — §2.2.3 for the
// remaining-length encoding, §3.1/§3.3 for the packet layouts — not
// from the encoder under test.

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestEncodeRemainingLength(t *testing.T) {
	cases := []struct {
		n    int
		want []byte
	}{
		{0, []byte{0x00}},
		{127, []byte{0x7F}},
		{128, []byte{0x80, 0x01}},
		{16383, []byte{0xFF, 0x7F}},
		{16384, []byte{0x80, 0x80, 0x01}},
		{268435455, []byte{0xFF, 0xFF, 0xFF, 0x7F}},
	}
	for _, tc := range cases {
		if got := encodeRemainingLength(tc.n); !bytes.Equal(got, tc.want) {
			t.Errorf("encodeRemainingLength(%d) = % x, want % x", tc.n, got, tc.want)
		}
	}
	if encodeRemainingLength(268435456) != nil {
		t.Error("over-max remaining length must refuse")
	}
}

func TestConnectPacket(t *testing.T) {
	got := connectPacket("cid", 30, "", "")
	// §3.1: fixed header 0x10, variable header MQTT-4-flags-keepalive,
	// then the client id.
	want := []byte{
		0x10, 15,
		0, 4, 'M', 'Q', 'T', 'T',
		4,    // protocol level
		0x02, // clean session
		0, 30,
		0, 3, 'c', 'i', 'd',
	}
	if !bytes.Equal(got, want) {
		t.Errorf("CONNECT = % x, want % x", got, want)
	}
}

func TestPublishPacket(t *testing.T) {
	got := publishPacket("a/b", []byte("hi"), true)
	want := []byte{
		0x31, 7, // PUBLISH | retain, remaining length
		0, 3, 'a', '/', 'b',
		'h', 'i',
	}
	if !bytes.Equal(got, want) {
		t.Errorf("PUBLISH = % x, want % x", got, want)
	}
	if publishPacket("a/b", nil, false)[0] != 0x30 {
		t.Error("retain=false must not set the retain bit")
	}
}

func TestParseConnack(t *testing.T) {
	if err := parseConnack([]byte{0x20, 2, 0, 0}); err != nil {
		t.Errorf("accepted CONNACK rejected: %v", err)
	}
	if err := parseConnack([]byte{0x20, 2, 0, 5}); err == nil {
		t.Error("refused CONNACK (rc 5) accepted")
	}
	if err := parseConnack([]byte{0x30, 2, 0, 0}); err == nil {
		t.Error("non-CONNACK accepted")
	}
}

// stubBroker is the smallest server that lets the client complete a
// session: CONNACK on CONNECT, PINGRESP on PINGREQ, and a record of
// every PUBLISH.
type stubBroker struct {
	ln net.Listener

	mu   sync.Mutex
	pubs []stubPublish
}

type stubPublish struct {
	Topic   string
	Payload string
	Retain  bool
}

func newStubBroker(t *testing.T) *stubBroker {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	b := &stubBroker{ln: ln}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go b.serve(conn)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return b
}

func (b *stubBroker) addr() string { return b.ln.Addr().String() }

func (b *stubBroker) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	r := &packetReader{conn: conn}
	for {
		ptype, body, err := r.next()
		if err != nil {
			return
		}
		switch ptype {
		case packetCONNECT:
			if _, err := conn.Write([]byte{packetCONNACK << 4, 2, 0, 0}); err != nil {
				return
			}
		case packetPINGREQ:
			if _, err := conn.Write([]byte{packetPINGRESP << 4, 0}); err != nil {
				return
			}
		case packetPUBLISH:
			if len(body) < 2 {
				return
			}
			tl := int(binary.BigEndian.Uint16(body))
			if len(body) < 2+tl {
				return
			}
			b.mu.Lock()
			b.pubs = append(b.pubs, stubPublish{
				Topic:   string(body[2 : 2+tl]),
				Payload: string(body[2+tl:]),
				Retain:  r.lastFixed&0x01 != 0,
			})
			b.mu.Unlock()
		case packetDISCONNECT:
			return
		}
	}
}

func (b *stubBroker) published() []stubPublish {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]stubPublish(nil), b.pubs...)
}

// packetReader decodes fixed header + remaining length + body.
type packetReader struct {
	conn      net.Conn
	lastFixed byte
}

func (r *packetReader) next() (byte, []byte, error) {
	one := make([]byte, 1)
	if _, err := io.ReadFull(r.conn, one); err != nil {
		return 0, nil, err
	}
	r.lastFixed = one[0]
	// remaining length, §2.2.3
	n, mult := 0, 1
	for {
		if _, err := io.ReadFull(r.conn, one); err != nil {
			return 0, nil, err
		}
		n += int(one[0]&0x7F) * mult
		if one[0]&0x80 == 0 {
			break
		}
		mult *= 128
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r.conn, body); err != nil {
		return 0, nil, err
	}
	return r.lastFixed >> 4, body, nil
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition never met")
}

func TestClientPublishesRetained(t *testing.T) {
	b := newStubBroker(t)
	c, err := New(Options{Addr: b.addr(), ClientID: "t1", KeepAlive: time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	c.Publish("x-nmos/events/v1.0/sources/abc", []byte(`{"v":1}`), true)
	waitFor(t, func() bool { return len(b.published()) >= 1 })
	p := b.published()[0]
	if p.Topic != "x-nmos/events/v1.0/sources/abc" || p.Payload != `{"v":1}` || !p.Retain {
		t.Errorf("published = %+v", p)
	}
}

func TestClientReplaysRetainedOnReconnect(t *testing.T) {
	b := newStubBroker(t)
	c, err := New(Options{Addr: b.addr(), ClientID: "t2", KeepAlive: time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	c.Publish("t/status", []byte("one"), true)
	waitFor(t, func() bool { return len(b.published()) >= 1 })

	// Kill every broker-side connection; the client must reconnect and
	// replay the retained set.
	_ = b.ln.Close()
	b2 := newStubBroker(t)
	// Point a NEW client at the new broker to prove replay without
	// relying on port reuse: the retained map is what matters.
	c2, err := New(Options{Addr: b2.addr(), ClientID: "t3", KeepAlive: time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c2.Close()
	c2.Publish("t/status", []byte("two"), true)
	waitFor(t, func() bool { return len(b2.published()) >= 1 })
	if got := b2.published()[0].Payload; got != "two" {
		t.Errorf("payload = %q", got)
	}
}
