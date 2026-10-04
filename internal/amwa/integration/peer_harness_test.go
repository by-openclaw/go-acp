//go:build integration

package amwa_integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	nmoshttp "dhs/internal/amwa/session/http"
)

// What the registry and mirror tests share: the processes they start,
// and the reading of two NMOS APIs side by side.

// transcript collects a process's output while the test reads it.
type transcript struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *transcript) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *transcript) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// proc is one `dhs …` process a test started.
type proc struct {
	cmd  *exec.Cmd
	logs *transcript
	once sync.Once
}

// spawn starts the CLI under test with args and stops it with the test.
func spawn(t *testing.T, args ...string) *proc {
	t.Helper()
	p := &proc{cmd: exec.Command(binary(t), args...), logs: &transcript{}}
	p.cmd.Stdout, p.cmd.Stderr = p.logs, p.logs
	if err := p.cmd.Start(); err != nil {
		t.Fatalf("FAIL-real: start dhs %s: %v", strings.Join(args, " "), err)
	}
	t.Cleanup(p.stop)
	return p
}

// stop interrupts the process and waits for it; a second call is a no-op.
func (p *proc) stop() {
	p.once.Do(func() {
		_ = p.cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _ = p.cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			_ = p.cmd.Process.Kill()
			<-done
		}
	})
}

// freeAddr is a loopback host:port nothing listens on right now.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("FAIL-real: no free port: %v", err)
	}
	defer l.Close()
	return l.Addr().String()
}

// startRegistry runs `dhs registry nmos serve` on addr and waits until
// its Query API answers.
func startRegistry(t *testing.T, addr string) *proc {
	t.Helper()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("FAIL-real: %q is not host:port: %v", addr, err)
	}
	p := spawn(t, "registry", "nmos", "serve", "--bind", ":"+port,
		"--advertise-host", addr, "--no-mdns", "--heartbeat-timeout", "12s")
	if !eventually(func() bool { return status("http://"+addr+"/x-nmos/query/v1.3/nodes") == http.StatusOK }, 15*time.Second) {
		t.Fatalf("FAIL-real: our registry does not answer on %s\n%s", addr, tail(p.logs.String()))
	}
	return p
}

// status is the HTTP status of a GET, 0 when nothing answered.
func status(url string) int {
	resp, err := peer.Get(url)
	if err != nil {
		return 0
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode
}

// docs is one collection, by resource id.
type docs map[string]map[string]any

// catalogue is the six IS-04 collections of one API.
type catalogue map[string]docs

// kinds are the IS-04 collections in the order a registry needs them.
var kinds = []string{"nodes", "devices", "sources", "flows", "senders", "receivers"}

// page reads one collection, or one page of it, with the response
// header that carries the paging links; ok is false when the API did not
// answer 200 with a JSON list.
func page(url string) (docs, http.Header, bool) {
	resp, err := peer.Get(url)
	if err != nil {
		return nil, nil, false
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	var rows []map[string]any
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &rows) != nil {
		return nil, nil, false
	}
	out := docs{}
	for _, r := range rows {
		id, _ := r["id"].(string)
		out[id] = r
	}
	return out, resp.Header, true
}

// list is page() for a caller that reads the whole collection at once.
func list(url string) (docs, bool) {
	d, _, ok := page(url)
	return d, ok
}

// own reads JSON from a process this test started: no answer is ours to
// explain, not a peer's.
func own(t *testing.T, url string, v any) {
	t.Helper()
	resp, err := peer.Get(url)
	if err != nil {
		t.Fatalf("FAIL-real: GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("FAIL-real: GET %s answered %s", url, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("FAIL-real: GET %s is not the JSON this test expects: %v", url, err)
	}
}

// nodeAPI reads everything a Node says of itself from its own Node API:
// the oracle of what it registers.
func nodeAPI(t *testing.T, node string) catalogue {
	t.Helper()
	var self map[string]any
	oracle(t, node+"/x-nmos/node/v1.3/self", &self)
	id, _ := self["id"].(string)
	c := catalogue{"nodes": docs{id: self}}
	for _, k := range kinds[1:] {
		d, ok := list(node + "/x-nmos/node/v1.3/" + k)
		if !ok {
			t.Fatalf("TIMEOUT: the peer's Node API did not list its %s", k)
		}
		c[k] = d
	}
	return c
}

// queryAPI reads the six collections from a Registry's Query API.
func queryAPI(reg string) (catalogue, bool) {
	c := catalogue{}
	for _, k := range kinds {
		d, ok := list(reg + "/x-nmos/query/v1.3/" + k + "?paging.limit=1000")
		if !ok {
			return nil, false
		}
		c[k] = d
	}
	return c, true
}

// differences says how got departs from want: a resource that is not
// there, one whose document is not the JSON that was registered, and —
// when exact — one that should not be there.
func differences(want, got catalogue, exact bool) []string {
	var out []string
	for _, k := range kinds {
		for id, w := range want[k] {
			g, ok := got[k][id]
			switch {
			case !ok:
				out = append(out, fmt.Sprintf("%s %s is missing", k, id))
			case !reflect.DeepEqual(w, g):
				out = append(out, fmt.Sprintf("%s %s differs in %v", k, id, differingKeys(w, g)))
			}
		}
		if !exact {
			continue
		}
		for id := range got[k] {
			if _, ok := want[k][id]; !ok {
				out = append(out, fmt.Sprintf("%s %s is not the peer's", k, id))
			}
		}
	}
	sort.Strings(out)
	return out
}

// differingKeys names the top-level attributes two documents disagree on.
func differingKeys(a, b map[string]any) []string {
	seen := map[string]bool{}
	var keys []string
	for _, m := range []map[string]any{a, b} {
		for k := range m {
			if !seen[k] && !reflect.DeepEqual(a[k], b[k]) {
				keys = append(keys, k)
			}
			seen[k] = true
		}
	}
	sort.Strings(keys)
	return keys
}

// converges polls until read() shows no difference from want(), and
// returns the last differences seen. Both sides are read again on every
// round: a Node may update a resource, and then registers the update.
func converges(want func() catalogue, read func() (catalogue, bool), exact bool, within time.Duration) []string {
	var last []string
	eventually(func() bool {
		got, ok := read()
		if !ok {
			last = []string{"the Query API did not answer"}
			return false
		}
		last = differences(want(), got, exact)
		return len(last) == 0
	}, within)
	return last
}

// count is the number of resources in a catalogue.
func (c catalogue) count() int {
	n := 0
	for _, d := range c {
		n += len(d)
	}
	return n
}

// grain is a Query API WebSocket message.
type grain struct {
	Grain struct {
		Topic string `json:"topic"`
		Data  []struct {
			Path string         `json:"path"`
			Pre  map[string]any `json:"pre"`
			Post map[string]any `json:"post"`
		} `json:"data"`
	} `json:"grain"`
}

// subscribe opens a Query API subscription on a Registry and dials its
// WebSocket.
func subscribe(t *testing.T, reg, resourcePath string) *nmoshttp.WebSocket {
	t.Helper()
	body := fmt.Sprintf(`{"max_update_rate_ms":100,"resource_path":%q,"params":{},"persist":false,"secure":false}`, resourcePath)
	resp, err := peer.Post(reg+"/x-nmos/query/v1.3/subscriptions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("TIMEOUT: %s did not answer the subscription request: %v", reg, err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	var sub struct {
		WSHref string `json:"ws_href"`
	}
	if resp.StatusCode/100 != 2 || json.Unmarshal(raw, &sub) != nil || sub.WSHref == "" {
		t.Fatalf("FAIL-real: %s answered the subscription request with %s: %s", reg, resp.Status, tail(string(raw)))
	}
	ws, err := nmoshttp.DialWebSocket(context.Background(), sub.WSHref, nil)
	if err != nil {
		t.Fatalf("FAIL-real: dial %s: %v", sub.WSHref, err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return ws
}

// nextGrain reads grains until one satisfies want, or the time is up.
func nextGrain(ws *nmoshttp.WebSocket, within time.Duration, want func(grain) bool) (grain, bool) {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		_ = ws.SetReadDeadline(deadline)
		raw, err := ws.ReadText()
		if err != nil {
			return grain{}, false
		}
		var g grain
		if json.Unmarshal(raw, &g) == nil && want(g) {
			return g, true
		}
	}
	return grain{}, false
}

// mirrorStats reads the counters of a running `dhs registry nmos mirror`.
type mirrorStats struct {
	Forwarded uint64 `json:"forwarded"`
	Deleted   uint64 `json:"deleted"`
	Resyncs   uint64 `json:"resyncs"`
	Failures  uint64 `json:"failures"`
	Skipped   uint64 `json:"skipped"`
}

func readMirror(t *testing.T, addr string) mirrorStats {
	t.Helper()
	var s struct {
		Stats mirrorStats `json:"stats"`
	}
	own(t, "http://"+addr+"/status.json", &s)
	return s.Stats
}
