package mnset

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/plugin"
)

// A fake MN SET: /api/device lists modules whose media address is the
// fake module of plugin_test.go (127.0.0.1, port via SetModulePort).
type mnsetServer struct {
	ts      *httptest.Server
	mu      sync.Mutex // the handler and a test that changes MN SET's answer while a refresh runs
	devices string
	gated   bool // /api/device wants X-AUTH-TOKEN
	status  int  // non-zero: answer /api/device with this status and no body
	hangup  bool // cut the authenticated /api/device answer short
}

func newMNSet(t *testing.T) *mnsetServer {
	t.Helper()
	m := &mnsetServer{}
	m.ts = httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/authentication/login/"):
			b, _ := io.ReadAll(r.Body)
			if string(b) != "s3cret" {
				w.WriteHeader(401)
				_, _ = w.Write([]byte(`{"code":18,"message":"bad credentials"}`))
				return
			}
			_, _ = w.Write([]byte(`{"username":"admin","token":"tok-1"}`))
		case r.URL.Path == "/api/device":
			if m.status != 0 {
				w.WriteHeader(m.status)
				return
			}
			if m.gated && r.Header.Get("X-AUTH-TOKEN") != "tok-1" {
				w.WriteHeader(403)
				return
			}
			if m.hangup {
				conn, _, _ := w.(stdhttp.Hijacker).Hijack()
				_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 99\r\n\r\n["))
				_ = conn.Close()
				return
			}
			_, _ = w.Write([]byte(m.devices))
		default:
			stdhttp.NotFound(w, r)
		}
	}))
	t.Cleanup(m.ts.Close)
	return m
}

// set changes MN SET's answer while a refresh may be reading it.
func (m *mnsetServer) set(devices string, status int) {
	m.mu.Lock()
	m.devices, m.status = devices, status
	m.mu.Unlock()
}

func (m *mnsetServer) hostPort(t *testing.T) (string, int) {
	t.Helper()
	u := strings.TrimPrefix(m.ts.URL, "http://")
	host, port, _ := strings.Cut(u, ":")
	var p int
	_, _ = fmt.Sscan(port, &p)
	return host, p
}

func deviceList(moduleIP string) string {
	return fmt.Sprintf(`[
	 {"id":"40:a3:6b:a2:10:0c","status":"ONLINE","lldpLocation":"FABRIC-1\nEthernet20/1",
	  "info":{"type":"22 - ST2110 UHD Transceiver","serial_number":"125061600012"},
	  "interfaces":{"e1":{"current_ip":"%s/24"}}},
	 {"id":"00:1b:c5:00:00:01","status":"OFFLINE","info":{"type":"MuoN"},
	  "interfaces":{"e1":{"current_ip":"10.6.40.99/24"}}},
	 {"id":"40:a3:6b:ff:ff:ff","status":"ONLINE","info":{"type":"22 - ST2110 UHD Transceiver","serial_number":"dead"},
	  "interfaces":{"e1":{"current_ip":"127.0.0.1/24"}}}
	]`, moduleIP)
}

func frameConnected(t *testing.T, mod *module, mn *mnsetServer, port int) *Plugin {
	t.Helper()
	p := (&Factory{}).New(plugin.Deps{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).(*Plugin)
	_, modPort := mod.hostPort(t)
	p.SetModulePort(modPort)
	host, mnPort := mn.hostPort(t)
	p.SetFramePort(mnPort)
	if err := p.Connect(context.Background(), host, port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = p.Disconnect() })
	return p
}

func TestFrameListsModulesAsSlots(t *testing.T) {
	mod := newModule(t)
	modHost, modPort := mod.hostPort(t)
	mn := newMNSet(t)
	// The third device points at 127.0.0.1 on the module port too, but
	// its MAC sorts last; make it unreachable by giving it a dead port
	// through a second listing entry — simplest: same IP, and we make
	// the fake module 404 self/information for nobody. Instead, point
	// it at an address nothing answers on.
	mn.devices = strings.Replace(deviceList(modHost), `"current_ip":"127.0.0.1/24"`, `"current_ip":"127.0.0.1/24","note":"reachable"`, 1)
	_ = modPort
	p := frameConnected(t, mod, mn, 0)

	info, err := p.GetDeviceInfo(context.Background())
	if err != nil || info.NumSlots != 3 || info.Port == 0 {
		t.Fatalf("device info = %+v, %v", info, err)
	}
	// Sorted by MAC: 00:1b… (OFFLINE) first, then 40:a3:6b:a2… (our fake), then 40:a3:6b:ff… (also our fake).
	s0, _ := p.GetSlotInfo(context.Background(), 0)
	s1, _ := p.GetSlotInfo(context.Background(), 1)
	if s0.Status != consumer.SlotNoCard || s0.Identity["mnset_status"] != "OFFLINE" || s0.IsOnline {
		t.Errorf("slot 0 = %+v", s0)
	}
	if s1.Status != consumer.SlotPresent || !s1.IsOnline || s1.Identity["identity"] != "FusioN6@0x68cd783f" || s1.Identity["lldp"] != "FABRIC-1 Ethernet20/1" || s1.Identity["serial"] != "125061600012" {
		t.Errorf("slot 1 = %+v", s1)
	}
	if _, err := p.GetSlotInfo(context.Background(), 3); !errors.Is(err, consumer.ErrObjectNotFound) {
		t.Errorf("slot 3 err = %v", err)
	}
	// Per-slot identity, walk, get, set go to that slot's module.
	if id, err := p.IdentityProbe(context.Background(), 1); err != nil || id != "FusioN6@0x68cd783f" {
		t.Errorf("identity slot 1 = %q, %v", id, err)
	}
	if _, err := p.IdentityProbe(context.Background(), 0); !errors.Is(err, consumer.ErrNotConnected) {
		t.Errorf("identity of an offline slot = %v", err)
	}
	objs, err := p.Walk(context.Background(), 1)
	if err != nil || len(objs) == 0 || objs[0].Slot != 1 {
		t.Fatalf("walk slot 1: %d objs, %v (slot of first = %d)", len(objs), err, objs[0].Slot)
	}
	if _, err := p.Walk(context.Background(), 0); !errors.Is(err, consumer.ErrNotConnected) {
		t.Errorf("walk of an offline slot = %v", err)
	}
	v, err := p.GetValue(context.Background(), consumer.ValueRequest{Slot: 1, Path: "self.ipconfig.hostname"})
	if err != nil || v.Str != "emsfp-a2-10-0c" {
		t.Errorf("get slot 1 = %+v, %v", v, err)
	}
	if _, err := p.GetValue(context.Background(), consumer.ValueRequest{Slot: 0, Path: "self.ipconfig.hostname"}); !errors.Is(err, consumer.ErrNotConnected) {
		t.Errorf("get on an offline slot = %v", err)
	}
	if got, err := p.SetValue(context.Background(), consumer.ValueRequest{Slot: 2, Path: "refclk.delay_req"}, consumer.Value{Str: "4"}); err != nil || got.Int != 4 {
		t.Errorf("set slot 2 = %+v, %v", got, err)
	}
	if s := p.String(); !strings.Contains(s, "FusioN6@0x68cd783f,FusioN6@0x68cd783f") {
		t.Errorf("String() = %q", s)
	}
}

func TestFrameOnlineButSilentModuleIsAnErrorSlot(t *testing.T) {
	mod := newModule(t)
	mn := newMNSet(t)
	// One ONLINE device at an address nothing answers on (port 1 via SetModulePort would
	// break the reachable one too), so give it an IP that is not routable quickly: the
	// fake module's host with the listing pointing at a closed port is not expressible
	// per device — use 127.0.0.1 and a module port that is closed for everybody.
	mn.devices = `[{"id":"aa","status":"ONLINE","interfaces":{"e1":{"current_ip":"127.0.0.1/24"}}}]`
	p := (&Factory{}).New(plugin.Deps{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).(*Plugin)
	p.SetModulePort(1) // nothing listens on :1
	host, port := mn.hostPort(t)
	p.SetFramePort(port)
	if err := p.Connect(context.Background(), host, port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	si, err := p.GetSlotInfo(context.Background(), 0)
	if err != nil || si.Status != consumer.SlotError || si.IsOnline {
		t.Errorf("slot = %+v, %v (want error: MN SET says ONLINE, module silent)", si, err)
	}
	_ = mod
}

func TestFrameConnectErrors(t *testing.T) {
	mn := newMNSet(t)
	host, port := mn.hostPort(t)
	p := (&Factory{}).New(plugin.Deps{}).(*Plugin)
	p.SetFramePort(port)
	ctx := context.Background()

	mn.devices = `[]`
	if err := p.Connect(ctx, host, port); err == nil || !strings.Contains(err.Error(), "manages no module") {
		t.Errorf("empty list err = %v", err)
	}
	mn.devices = `{"not":"a list"}`
	if err := p.Connect(ctx, host, port); err == nil || !strings.Contains(err.Error(), "not a JSON array") {
		t.Errorf("non-array err = %v", err)
	}
	mn.status = 500
	if err := p.Connect(ctx, host, port); err == nil || !strings.Contains(err.Error(), "http 500") {
		t.Errorf("500 err = %v", err)
	}
	mn.status = 0
	mn.gated = true
	mn.devices = `[{"id":"a","status":"OFFLINE"}]`
	if err := p.Connect(ctx, host, port); err == nil || !strings.Contains(err.Error(), "no credentials set") {
		t.Errorf("gated without creds err = %v", err)
	}
	p.SetCredentials("admin", "wrong")
	if err := p.Connect(ctx, host, port); err == nil || !strings.Contains(err.Error(), "login refused: bad credentials") {
		t.Errorf("bad creds err = %v", err)
	}
	p.SetCredentials("admin", "s3cret")
	if err := p.Connect(ctx, host, port); err != nil {
		t.Errorf("gated with creds: %v", err)
	}
	mn.hangup = true
	if err := p.Connect(ctx, host, port); err == nil || !strings.Contains(err.Error(), "device list:") {
		t.Errorf("authenticated list cut short err = %v", err)
	}
	mn.hangup = false
	// Nothing listening at all.
	p.SetFramePort(FramePort)
	if err := p.Connect(ctx, "127.0.0.1", FramePort); err == nil || !strings.Contains(err.Error(), "device list") {
		t.Errorf("silent MN SET err = %v", err)
	}
}

func TestConnectPortZeroFallsBackToFrame(t *testing.T) {
	// Port 0 tries the module on 80 (nothing there), then MN SET on
	// 8080 (nothing there either): the module error is what comes back,
	// so the operator sees the address that was tried first.
	p := (&Factory{}).New(plugin.Deps{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).(*Plugin)
	err := p.Connect(context.Background(), "127.0.0.1", 0)
	if err == nil || !strings.Contains(err.Error(), "mnset connect 127.0.0.1:80") {
		t.Errorf("err = %v", err)
	}
	// … and when MN SET does answer, port 0 lands on the frame.
	mod := newModule(t)
	mn := newMNSet(t)
	modHost, modPort := mod.hostPort(t)
	mn.devices = deviceList(modHost)
	host, mnPort := mn.hostPort(t)
	p.SetModulePort(modPort)
	p.SetFramePort(mnPort)
	if err := p.Connect(context.Background(), host, 0); err != nil {
		t.Fatalf("port 0 → frame: %v", err)
	}
	if info, _ := p.GetDeviceInfo(context.Background()); info.NumSlots != 3 || info.Port != mnPort {
		t.Errorf("device info = %+v", info)
	}
}

func TestLoginShapes(t *testing.T) {
	mn := newMNSet(t)
	hc := &stdhttp.Client{}
	if tok, err := login(context.Background(), hc, mn.ts.URL, "admin", "s3cret"); err != nil || tok != "tok-1" {
		t.Errorf("login = %q, %v", tok, err)
	}
	if _, err := login(context.Background(), hc, mn.ts.URL, "ad\x7fmin", "s3cret"); err == nil || !strings.Contains(err.Error(), "build request") {
		t.Errorf("bad user err = %v", err)
	}
	// Answer without token and without message.
	ts := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("oops"))
	}))
	defer ts.Close()
	if _, err := login(context.Background(), hc, ts.URL, "admin", "x"); err == nil || !strings.Contains(err.Error(), "no token in answer (http 500)") {
		t.Errorf("no token err = %v", err)
	}
}
