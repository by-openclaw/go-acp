package mnset

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	stdhttp "net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Discovered is one module found by a sweep.
type Discovered struct {
	IP       string
	Port     int
	Type     string // e.g. "22 - ST2110 UHD Transceiver"
	BaseType string // e.g. "FusioN6"
	Serial   string
	Firmware string // current_version, e.g. 0x68cd783f
	App      string // active firmware slot description, e.g. MN-FusioN-6-B-APP-25-2110-SDI-2R6T-N
}

// DiscoverConfig drives a sweep.
type DiscoverConfig struct {
	// Ranges lists what to probe: "10.6.40.53", "10.6.40.50-99" (last
	// octet range) or "10.6.40.0/24".
	Ranges []string
	// Port is the node API port (0 → 80).
	Port int
	// Timeout bounds one probe (0 → 2s). A module answers in milliseconds;
	// an empty address is a full timeout, so this sets the sweep's cost.
	Timeout time.Duration
	// Concurrency bounds probes in flight (0 → 32).
	Concurrency int
}

// Discover sweeps the ranges for emSFP nodes: every address that answers
// self/information is a module. Results are sorted by address so two
// sweeps of the same plant diff cleanly.
func Discover(ctx context.Context, cfg DiscoverConfig) ([]Discovered, error) {
	ips, err := expandRanges(cfg.Ranges)
	if err != nil {
		return nil, err
	}
	if cfg.Port <= 0 {
		cfg.Port = DefaultPort
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 2 * time.Second
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 32
	}
	if cfg.Concurrency > len(ips) {
		cfg.Concurrency = len(ips)
	}

	var (
		mu    sync.Mutex
		found []Discovered
		wg    sync.WaitGroup
		sem   = make(chan struct{}, cfg.Concurrency)
	)
	for _, ip := range ips {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			d, ok := probe(ctx, ip, cfg.Port, cfg.Timeout)
			if ok {
				mu.Lock()
				found = append(found, d)
				mu.Unlock()
			}
		}(ip)
	}
	wg.Wait()
	sort.Slice(found, func(i, j int) bool { return ipLess(found[i].IP, found[j].IP) })
	return found, nil
}

// probe asks one address for its identity and, when it is a module,
// its active program.
func probe(ctx context.Context, ip string, port int, timeout time.Duration) (Discovered, bool) {
	c := newClient(ip, port, timeout, nil, nil)
	v, err := c.get(ctx, "self/information")
	if err != nil {
		return Discovered{}, false
	}
	info, ok := v.(map[string]any)
	if !ok {
		return Discovered{}, false
	}
	d := Discovered{
		IP: ip, Port: port,
		Type: str(info["type"]), BaseType: str(info["base_type"]),
		Serial: str(info["serial_number"]), Firmware: str(info["current_version"]),
	}
	// The app is in self/firmware; a module that serves identity but
	// not firmware is still a module.
	if fw, err := c.get(ctx, "self/firmware"); err == nil {
		d.App = activeProgram(fw)
	}
	return d, true
}

// activeProgram finds the active slot in self/firmware.info[].
func activeProgram(fw any) string {
	m, ok := fw.(map[string]any)
	if !ok {
		return ""
	}
	slots, ok := m["info"].([]any)
	if !ok {
		return ""
	}
	for _, s := range slots {
		sm, ok := s.(map[string]any)
		if ok && str(sm["active"]) == "yes" {
			return str(sm["desc"])
		}
	}
	return ""
}

// expandRanges turns the range spellings into a de-duplicated address
// list. Refuses a spelling it does not understand rather than probing
// the wrong subnet.
func expandRanges(ranges []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	add := func(ip string) {
		if !seen[ip] {
			seen[ip] = true
			out = append(out, ip)
		}
	}
	for _, r := range ranges {
		r = strings.TrimSpace(r)
		switch {
		case r == "":
			continue
		case strings.Contains(r, "/"):
			_, n, err := net.ParseCIDR(r)
			if err != nil {
				return nil, fmt.Errorf("mnset discover: %q: %w", r, err)
			}
			ones, bits := n.Mask.Size()
			if bits-ones > 16 {
				return nil, fmt.Errorf("mnset discover: %q: wider than /16, refusing", r)
			}
			for ip := n.IP.Mask(n.Mask); n.Contains(ip); ip = nextIP(ip) {
				add(ip.String())
			}
		case strings.Contains(r, "-"):
			dash := strings.LastIndex(r, "-")
			head, tail := r[:dash], r[dash+1:]
			dot := strings.LastIndex(head, ".")
			if dot < 0 || net.ParseIP(head) == nil {
				return nil, fmt.Errorf("mnset discover: %q: want a.b.c.x-y", r)
			}
			lo, err1 := strconv.Atoi(head[dot+1:])
			hi, err2 := strconv.Atoi(tail)
			if err1 != nil || err2 != nil || lo < 0 || hi > 255 || lo > hi {
				return nil, fmt.Errorf("mnset discover: %q: bad last-octet range", r)
			}
			for o := lo; o <= hi; o++ {
				add(head[:dot+1] + strconv.Itoa(o))
			}
		default:
			if net.ParseIP(r) == nil {
				return nil, fmt.Errorf("mnset discover: %q is not an address", r)
			}
			add(r)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("mnset discover: nothing to probe")
	}
	return out, nil
}

// nextIP returns ip+1 (IPv4).
func nextIP(ip net.IP) net.IP {
	n := make(net.IP, len(ip))
	copy(n, ip)
	for i := len(n) - 1; i >= 0; i-- {
		n[i]++
		if n[i] != 0 {
			break
		}
	}
	return n
}

// ipLess orders dotted addresses numerically, not lexically.
func ipLess(a, b string) bool {
	pa, pb := net.ParseIP(a).To4(), net.ParseIP(b).To4()
	if pa == nil || pb == nil {
		return a < b
	}
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return false
}

// InventoryDevice is one module as MN SET lists it.
type InventoryDevice struct {
	ID       string // the module MAC, MN SET's device id
	Status   string // ONLINE / OFFLINE
	IP       string // media address (interfaces.e1.current_ip)
	Type     string
	Serial   string
	Location string // LLDP: switch chassis + port
}

// Inventory lists the modules MN SET manages, for a plant where the
// sweep range is not known. Login is MN SET's own: the username in the
// path, the password as a raw text body (not JSON), the token returned
// as JSON and presented back in X-AUTH-TOKEN. Password and token are
// never logged.
func Inventory(ctx context.Context, host string, port int, user, pass string, timeout time.Duration) ([]InventoryDevice, error) {
	if port <= 0 {
		port = 8080
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	base := "http://" + net.JoinHostPort(host, strconv.Itoa(port))
	hc := &stdhttp.Client{Timeout: timeout}

	raw, status, err := mnsetCall(ctx, hc, stdhttp.MethodPost, base+"/api/authentication/login/"+user, pass, "")
	if err != nil {
		return nil, fmt.Errorf("mnset inventory: login: %w", err)
	}
	var login struct {
		Token   string `json:"token"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &login); err != nil || login.Token == "" {
		if login.Message != "" {
			return nil, fmt.Errorf("mnset inventory: login refused: %s", login.Message)
		}
		return nil, fmt.Errorf("mnset inventory: login: no token in answer (http %d)", status)
	}

	raw, _, err = mnsetCall(ctx, hc, stdhttp.MethodGet, base+"/api/device", "", login.Token)
	if err != nil {
		return nil, fmt.Errorf("mnset inventory: device list: %w", err)
	}
	var devices []map[string]any
	if err := json.Unmarshal(raw, &devices); err != nil {
		return nil, fmt.Errorf("mnset inventory: device list is not a JSON array: %w", err)
	}

	out := make([]InventoryDevice, 0, len(devices))
	for _, d := range devices {
		info, _ := d["info"].(map[string]any)
		ip := ""
		if ifs, ok := d["interfaces"].(map[string]any); ok {
			if e1, ok := ifs["e1"].(map[string]any); ok {
				ip = strings.SplitN(str(e1["current_ip"]), "/", 2)[0]
			}
		}
		out = append(out, InventoryDevice{
			ID: str(d["id"]), Status: str(d["status"]), IP: ip,
			Type: str(info["type"]), Serial: str(info["serial_number"]),
			Location: strings.ReplaceAll(str(d["lldpLocation"]), "\n", " "),
		})
	}
	return out, nil
}

// mnsetCall is one MN SET app-API request. body is sent as text/plain
// (the login shape); token, when set, goes in X-AUTH-TOKEN.
func mnsetCall(ctx context.Context, hc *stdhttp.Client, method, url, body, token string) ([]byte, int, error) {
	req, err := stdhttp.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "text/plain")
	}
	if token != "" {
		req.Header.Set("X-AUTH-TOKEN", token)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read: %w", err)
	}
	return raw, resp.StatusCode, nil
}

// str renders a JSON scalar; a missing key is "".
func str(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	}
	return fmt.Sprintf("%v", v)
}
