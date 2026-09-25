package mnset

import (
	"context"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A sweep is only as good as the addresses it decides to probe; the
// range grammar is pinned first, then a sweep against fake modules.

func TestExpandRanges(t *testing.T) {
	good := []struct {
		in   []string
		want []string
	}{
		{[]string{"10.6.40.53"}, []string{"10.6.40.53"}},
		{[]string{"10.6.40.53", " 10.6.40.53 ", ""}, []string{"10.6.40.53"}},
		{[]string{"10.6.40.52-54"}, []string{"10.6.40.52", "10.6.40.53", "10.6.40.54"}},
		{[]string{"10.0.0.254/31"}, []string{"10.0.0.254", "10.0.0.255"}},
		{[]string{"10.0.0.0/30", "10.0.0.1"}, []string{"10.0.0.0", "10.0.0.1", "10.0.0.2", "10.0.0.3"}},
	}
	for _, c := range good {
		got, err := expandRanges(c.in)
		if err != nil || strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("expandRanges(%v) = %v, %v", c.in, got, err)
		}
	}
	bad := []struct {
		in   string
		want string
	}{
		{"10.6.40/24", "invalid CIDR"},
		{"10.0.0.0/8", "wider than /16"},
		{"1040-50", "want a.b.c.x-y"},
		{"10.6.40.x-50", "want a.b.c.x-y"},
		{"10.6.40.50-x", "bad last-octet range"},
		{"10.6.40.50-300", "bad last-octet range"},
		{"10.6.40.60-50", "bad last-octet range"},
		{"riedel", "is not an address"},
	}
	for _, c := range bad {
		if _, err := expandRanges([]string{c.in}); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("expandRanges(%q) err = %v, want %q", c.in, err, c.want)
		}
	}
	if _, err := expandRanges([]string{"", " "}); err == nil || !strings.Contains(err.Error(), "nothing to probe") {
		t.Errorf("empty err = %v", err)
	}
	if _, err := expandRanges(nil); err == nil {
		t.Error("nil ranges must refuse")
	}
}

func TestIPLessOrdersNumerically(t *testing.T) {
	if !ipLess("10.6.40.9", "10.6.40.10") || ipLess("10.6.40.10", "10.6.40.9") || ipLess("10.6.40.9", "10.6.40.9") {
		t.Error("numeric order")
	}
	if !ipLess("a", "b") {
		t.Error("non-IP falls back to lexical")
	}
}

func TestActiveProgram(t *testing.T) {
	if activeProgram("x") != "" || activeProgram(map[string]any{}) != "" {
		t.Error("non-document shapes must yield empty")
	}
	fw := map[string]any{"info": []any{"junk", map[string]any{"active": "no", "desc": "old"}, map[string]any{"active": "yes", "desc": "APP-25"}}}
	if got := activeProgram(fw); got != "APP-25" {
		t.Errorf("got %q", got)
	}
	if got := activeProgram(map[string]any{"info": []any{map[string]any{"active": "no"}}}); got != "" {
		t.Errorf("no active slot: got %q", got)
	}
}

// fakeNode is a module that answers on a port; on the same 127.0.0.1
// address, so a sweep with an explicit port hits it.
func fakeNode(t *testing.T, information, firmware string) (host string, port int) {
	t.Helper()
	ts := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		switch strings.TrimPrefix(r.URL.Path, apiPrefix) {
		case "self/information":
			_, _ = w.Write([]byte(information))
		case "self/firmware":
			if firmware == "" {
				stdhttp.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(firmware))
		default:
			stdhttp.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	h, p, _ := net.SplitHostPort(strings.TrimPrefix(ts.URL, "http://"))
	port, _ = strconv.Atoi(p)
	return h, port
}

func TestDiscoverFindsModulesAndSkipsTheRest(t *testing.T) {
	host, port := fakeNode(t,
		`{"type":"22 - ST2110 UHD Transceiver","base_type":"FusioN6","serial_number":"125061600012","current_version":"0x68cd783f"}`,
		`{"info":[{"slot":1,"desc":"MN-FusioN-6-B-APP-25-2110-SDI-2R6T-N","active":"yes"}]}`)
	found, err := Discover(context.Background(), DiscoverConfig{Ranges: []string{host}, Port: port, Timeout: time.Second, Concurrency: 4})
	if err != nil || len(found) != 1 {
		t.Fatalf("found = %v, %v", found, err)
	}
	d := found[0]
	if d.IP != host || d.Port != port || d.BaseType != "FusioN6" || d.Firmware != "0x68cd783f" || d.Serial != "125061600012" || !strings.HasPrefix(d.App, "MN-FusioN-6") {
		t.Errorf("discovered = %+v", d)
	}

	// Identity served, firmware not: still a module, App empty.
	host2, port2 := fakeNode(t, `{"base_type":"MuoN"}`, "")
	found, err = Discover(context.Background(), DiscoverConfig{Ranges: []string{host2}, Port: port2})
	if err != nil || len(found) != 1 || found[0].BaseType != "MuoN" || found[0].App != "" {
		t.Fatalf("no-firmware module = %v, %v", found, err)
	}

	// Identity is not an object: not a module.
	host3, port3 := fakeNode(t, `["not","a","module"]`, "")
	found, err = Discover(context.Background(), DiscoverConfig{Ranges: []string{host3}, Port: port3})
	if err != nil || len(found) != 0 {
		t.Fatalf("non-module = %v, %v", found, err)
	}

	// Nothing listening: silence is not a module either.
	found, err = Discover(context.Background(), DiscoverConfig{Ranges: []string{"127.0.0.1"}, Port: 1, Timeout: 200 * time.Millisecond})
	if err != nil || len(found) != 0 {
		t.Fatalf("silent = %v, %v", found, err)
	}

	// Port 0 is the module default (80): the sweep runs, whatever answers there.
	if _, err := Discover(context.Background(), DiscoverConfig{Ranges: []string{"127.0.0.1"}, Timeout: 200 * time.Millisecond}); err != nil {
		t.Errorf("default port sweep err = %v", err)
	}

	// Bad range is refused before any probe; cancelled context probes nothing.
	if _, err := Discover(context.Background(), DiscoverConfig{Ranges: []string{"nope"}}); err == nil {
		t.Error("bad range must refuse")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	found, err = Discover(ctx, DiscoverConfig{Ranges: []string{host}, Port: port})
	if err != nil || len(found) != 0 {
		t.Errorf("cancelled sweep = %v, %v", found, err)
	}
}

// fakeMNSet is the MN SET app API: raw-text password login, X-AUTH-TOKEN
// gate on the device list.
func fakeMNSet(t *testing.T, devices string, loginStatus int, loginBody string) (host string, port int) {
	t.Helper()
	ts := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/authentication/login/"):
			if loginStatus != 0 {
				w.WriteHeader(loginStatus)
				_, _ = w.Write([]byte(loginBody))
				return
			}
			b := make([]byte, 32)
			n, _ := r.Body.Read(b)
			if r.Header.Get("Content-Type") != "text/plain" || string(b[:n]) != "s3cret" {
				w.WriteHeader(401)
				_, _ = w.Write([]byte(`{"code":18,"message":"bad credentials"}`))
				return
			}
			_, _ = w.Write([]byte(`{"username":"admin","token":"tok-1"}`))
		case r.URL.Path == "/api/device":
			if r.Header.Get("X-AUTH-TOKEN") != "tok-1" {
				w.WriteHeader(403)
				return
			}
			if devices == "hangup" {
				conn, _, _ := w.(stdhttp.Hijacker).Hijack()
				_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 99\r\n\r\n["))
				_ = conn.Close()
				return
			}
			_, _ = w.Write([]byte(devices))
		default:
			stdhttp.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	h, p, _ := net.SplitHostPort(strings.TrimPrefix(ts.URL, "http://"))
	port, _ = strconv.Atoi(p)
	return h, port
}

func TestInventoryListsManagedModules(t *testing.T) {
	devices := `[{"id":"00:1b:c5:a2:10:0c","status":"ONLINE","lldpLocation":"FABRIC-1\nEthernet20/1",
	  "info":{"type":"22 - ST2110 UHD Transceiver","serial_number":"125061600012"},
	  "interfaces":{"e1":{"current_ip":"10.6.40.53/24"}}},
	 {"id":"00:1b:c5:a2:10:0d","status":"OFFLINE"}]`
	host, port := fakeMNSet(t, devices, 0, "")
	got, err := Inventory(context.Background(), host, port, "admin", "s3cret", time.Second)
	if err != nil || len(got) != 2 {
		t.Fatalf("inventory = %v, %v", got, err)
	}
	if d := got[0]; d.ID != "00:1b:c5:a2:10:0c" || d.IP != "10.6.40.53" || d.Serial != "125061600012" || d.Location != "FABRIC-1 Ethernet20/1" || d.Status != "ONLINE" {
		t.Errorf("device 0 = %+v", d)
	}
	if d := got[1]; d.IP != "" || d.Type != "" || d.Status != "OFFLINE" {
		t.Errorf("device 1 = %+v", d)
	}
}

func TestInventoryErrors(t *testing.T) {
	ctx := context.Background()
	host, port := fakeMNSet(t, `[]`, 0, "")

	if _, err := Inventory(ctx, host, port, "admin", "wrong", 0); err == nil || !strings.Contains(err.Error(), "login refused: bad credentials") {
		t.Errorf("wrong password err = %v", err)
	}
	if _, err := Inventory(ctx, host, port, "ad\x7fmin", "s3cret", 0); err == nil || !strings.Contains(err.Error(), "build request") {
		t.Errorf("bad user err = %v", err)
	}
	if _, err := Inventory(ctx, "127.0.0.1", 1, "admin", "s3cret", 200*time.Millisecond); err == nil || !strings.Contains(err.Error(), "login:") {
		t.Errorf("nothing listening err = %v", err)
	}

	// Login answers without a token and without a message.
	h2, p2 := fakeMNSet(t, `[]`, 500, `oops`)
	if _, err := Inventory(ctx, h2, p2, "admin", "s3cret", 0); err == nil || !strings.Contains(err.Error(), "no token in answer (http 500)") {
		t.Errorf("no token err = %v", err)
	}
	// Device list is not an array.
	h3, p3 := fakeMNSet(t, `{"not":"array"}`, 0, "")
	if _, err := Inventory(ctx, h3, p3, "admin", "s3cret", 0); err == nil || !strings.Contains(err.Error(), "not a JSON array") {
		t.Errorf("non-array err = %v", err)
	}
	// Device list body cut short by the server.
	h4, p4 := fakeMNSet(t, "hangup", 0, "")
	if _, err := Inventory(ctx, h4, p4, "admin", "s3cret", 0); err == nil || !strings.Contains(err.Error(), "device list:") {
		t.Errorf("hangup err = %v", err)
	}
	// Port 0 means MN SET's default 8080 — proven by the address the
	// error names, without needing anything on 8080.
	if _, err := Inventory(ctx, "127.0.0.1", 0, "admin", "s3cret", 200*time.Millisecond); err == nil || !strings.Contains(err.Error(), ":8080") {
		t.Errorf("default port err = %v", err)
	}
}

func TestStrRendersScalars(t *testing.T) {
	if str(nil) != "" || str("a") != "a" || str(3.0) != "3" || str(true) != "true" {
		t.Error("str")
	}
}

// twoNodes serves the same fake module on 127.0.0.1 and 127.0.0.2 at one
// port, so a two-address sweep finds two modules and has to order them.
func twoNodes(t *testing.T) int {
	t.Helper()
	l1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l1.Addr().(*net.TCPAddr).Port
	l2, err := net.Listen("tcp", "127.0.0.2:"+strconv.Itoa(port))
	if err != nil {
		_ = l1.Close()
		t.Skipf("127.0.0.2 not bindable here: %v", err)
	}
	h := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if strings.HasSuffix(r.URL.Path, "self/information") {
			_, _ = w.Write([]byte(`{"base_type":"MuoN","current_version":"0x1"}`))
			return
		}
		stdhttp.NotFound(w, r)
	})
	for _, l := range []net.Listener{l1, l2} {
		srv := &stdhttp.Server{Handler: h}
		go func(l net.Listener) { _ = srv.Serve(l) }(l)
		t.Cleanup(func() { _ = srv.Close() })
	}
	return port
}

func TestDiscoverOrdersResultsByAddress(t *testing.T) {
	port := twoNodes(t)
	found, err := Discover(context.Background(), DiscoverConfig{Ranges: []string{"127.0.0.2", "127.0.0.1"}, Port: port, Timeout: time.Second})
	if err != nil || len(found) != 2 {
		t.Fatalf("found = %v, %v", found, err)
	}
	if found[0].IP != "127.0.0.1" || found[1].IP != "127.0.0.2" {
		t.Errorf("order = %s, %s", found[0].IP, found[1].IP)
	}
}
