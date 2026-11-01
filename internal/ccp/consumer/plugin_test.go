package ccp

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/ccp/codec"
	dhsc "dhs/internal/consumer"
	"dhs/internal/plugin"
)

// fakePanel answers like the lab CCP-1601 (bytes captured 2026-10-25) and
// records what it was sent.
type fakePanel struct {
	ln   net.Listener
	mu   sync.Mutex
	got  [][]byte
	conn net.Conn
}

func newFakePanel(t *testing.T) *fakePanel {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakePanel{ln: ln}
	t.Cleanup(func() { _ = ln.Close() })
	go f.serve()
	return f
}

func (f *fakePanel) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

func (f *fakePanel) serve() {
	c, err := f.ln.Accept()
	if err != nil {
		return
	}
	f.mu.Lock()
	f.conn = c
	f.mu.Unlock()
	buf := make([]byte, 4096)
	for {
		n, err := c.Read(buf)
		if err != nil {
			return
		}
		in := append([]byte(nil), buf[:n]...)
		for len(in) > 0 {
			cmd := in[0]
			size := 1
			var reply []byte
			switch cmd {
			case codec.CmdPoll:
				reply = []byte{0x81, 0x01, 0x0d, 0x01, 0x00, 0x01}
			case codec.CmdReportFirmware:
				reply = append([]byte{0xF1}, "DEL869-12"...)
			case codec.CmdReadMAC:
				reply = []byte{0x83, 0x02, 0x76, 0x00}
			case codec.CmdReadDHCP:
				reply = []byte{0x89, 0x01}
			case codec.CmdReadSubnet:
				reply = []byte{0x87, 0xff, 0xff, 0xff, 0x00}
			case codec.CmdReadGateway:
				reply = []byte{0x88, 0xc0, 0xa8, 0x00, 0x01}
			case codec.CmdSetColour:
				size, reply = 3, []byte{0x84}
			case codec.CmdWriteBitmap:
				size, reply = 258, []byte{0x84}
			}
			if size > len(in) {
				size = len(in)
			}
			f.mu.Lock()
			f.got = append(f.got, append([]byte(nil), in[:size]...))
			f.mu.Unlock()
			in = in[size:]
			// A button event glued in front of the reply, as the panel does.
			_, _ = c.Write(append([]byte{0x82, 0x03, 0x11, 0x02}, reply...))
		}
	}
}

func (f *fakePanel) push(b []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, _ = f.conn.Write(b)
}

func connect(t *testing.T) (*Plugin, *fakePanel) {
	f := newFakePanel(t)
	p := (&Factory{}).New(plugin.Deps{}).(*Plugin)
	if err := p.Connect(context.Background(), "127.0.0.1", f.port()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Disconnect() })
	return p, f
}

func TestConnectStatusAndNetwork(t *testing.T) {
	p, _ := connect(t)
	pr, fw, n := p.Status()
	if pr.Model != "CCP-1601" || pr.ProtocolVersion != 13 || fw != "DEL869-12" || n != 16 {
		t.Fatalf("status %+v %q %d", pr, fw, n)
	}
	ctx := context.Background()
	for path, want := range map[string]string{
		"status.model": "CCP-1601", "status.psu_a": "on", "status.psu_b": "off", "status.refresh_required": "on",
		"network.dhcp": "on", "network.subnet": "255.255.255.0", "network.gateway": "192.168.0.1",
	} {
		v, err := p.GetValue(ctx, dhsc.ValueRequest{Path: path})
		if err != nil || v.Str != want {
			t.Errorf("%s = %q %v, want %q", path, v.Str, err, want)
		}
	}
	_, err := p.Send(ctx, codec.ReadMAC())
	var pe codec.PanelError
	if !errors.As(err, &pe) || pe.Code != codec.ErrInvalidCommand {
		t.Fatalf("read MAC must surface the panel's error: %v", err)
	}
}

func TestLabelColourAndWalk(t *testing.T) {
	p, f := connect(t)
	ctx := context.Background()
	if _, err := p.SetValue(ctx, dhsc.ValueRequest{Path: "buttons.1.label"}, dhsc.Value{Kind: dhsc.KindString, Str: "CAM 1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.SetValue(ctx, dhsc.ValueRequest{Path: "buttons.1.colour"}, dhsc.Value{Kind: dhsc.KindString, Str: "green"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.SetValue(ctx, dhsc.ValueRequest{Path: "buttons.1.colour"}, dhsc.Value{Kind: dhsc.KindString, Str: "pink"}); err == nil {
		t.Fatal("unknown colour must be refused")
	}
	f.mu.Lock()
	var bitmap, colour []byte
	for _, g := range f.got {
		switch g[0] {
		case codec.CmdWriteBitmap:
			bitmap = g
		case codec.CmdSetColour:
			colour = g
		}
	}
	f.mu.Unlock()
	want := codec.Text("CAM 1")
	if len(bitmap) != 258 || bitmap[1] != 1 || codec.Decode(bitmap[2:]) != want {
		t.Fatalf("bitmap command wrong (len %d)", len(bitmap))
	}
	if string(colour) != string([]byte{0x05, 0x01, 0x0C}) {
		t.Fatalf("colour command % x", colour)
	}
	objs, err := p.Walk(ctx, 0)
	if err != nil || len(objs) != 9+2*16 {
		t.Fatalf("walk %d objects %v", len(objs), err)
	}
	if v, _ := p.GetValue(ctx, dhsc.ValueRequest{Path: "buttons.1.label"}); v.Str != "CAM 1" {
		t.Fatalf("label read-back %q", v.Str)
	}
}

func TestEvents(t *testing.T) {
	p, f := connect(t)
	var mu sync.Mutex
	var got []string
	if err := p.Subscribe(dhsc.ValueRequest{}, func(e dhsc.Event) {
		mu.Lock()
		got = append(got, e.Path+"="+e.Value.Str)
		mu.Unlock()
	}); err != nil {
		t.Fatal(err)
	}
	f.push([]byte{0x82, 0x01, 0x05, 0x00, 0x82, 0x02, 0x05, 0x00, 0x82, 0x04, 0x11, 0x07, 0x82, 0x05, 0x01, 0x27, 0x10})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 4 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(got, " ")
	for _, w := range []string{"buttons.5=down", "buttons.5=up", "buttons.17=-7", "buttons.1=t-bar 10000"} {
		if !strings.Contains(joined, w) {
			t.Errorf("missing %s in %s", w, joined)
		}
	}
}

func TestClosedByPanel(t *testing.T) {
	p, f := connect(t)
	f.mu.Lock()
	_ = f.conn.Close()
	f.mu.Unlock()
	select {
	case <-p.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done not closed after the panel hung up")
	}
	if _, err := p.Poll(context.Background()); err == nil {
		t.Fatal("poll on a closed session must fail")
	}
	_ = io.EOF
	_ = strconv.Itoa
}
