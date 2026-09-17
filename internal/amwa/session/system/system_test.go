package system

import (
	"context"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	dnssdcodec "dhs/internal/amwa/codec/dnssd"
	"dhs/internal/amwa/codec/is09"
	"dhs/internal/amwa/codec/spec"
	httpsession "dhs/internal/amwa/session/http"
)

func TestSelectInstanceFiltersByProto(t *testing.T) {
	insts := []dnssdcodec.Instance{
		{Name: "https-only", Service: dnssdcodec.ServiceSystem,
			TXT: map[string]string{
				dnssdcodec.TXTKeyAPIProto: "https",
				dnssdcodec.TXTKeyAPIVer:   "v1.0",
				dnssdcodec.TXTKeyPriority: "0",
			}},
		{Name: "http-ok", Service: dnssdcodec.ServiceSystem,
			TXT: map[string]string{
				dnssdcodec.TXTKeyAPIProto: "http",
				dnssdcodec.TXTKeyAPIVer:   "v1.0",
				dnssdcodec.TXTKeyPriority: "5",
			}},
	}
	got, err := SelectInstance(insts, "http", "v1.0", nil)
	if err != nil {
		t.Fatalf("SelectInstance: %v", err)
	}
	if got.Name != "http-ok" {
		t.Fatalf("selected %q, want http-ok", got.Name)
	}
}

func TestSelectInstanceFiltersByVersion(t *testing.T) {
	insts := []dnssdcodec.Instance{
		{Name: "old-only", Service: dnssdcodec.ServiceSystem,
			TXT: map[string]string{
				dnssdcodec.TXTKeyAPIProto: "http",
				dnssdcodec.TXTKeyAPIVer:   "v0.9",
				dnssdcodec.TXTKeyPriority: "0",
			}},
		{Name: "modern", Service: dnssdcodec.ServiceSystem,
			TXT: map[string]string{
				dnssdcodec.TXTKeyAPIProto: "http",
				dnssdcodec.TXTKeyAPIVer:   "v1.0,v1.1",
				dnssdcodec.TXTKeyPriority: "0",
			}},
	}
	got, err := SelectInstance(insts, "http", "v1.0", nil)
	if err != nil {
		t.Fatalf("SelectInstance: %v", err)
	}
	if got.Name != "modern" {
		t.Fatalf("selected %q, want modern", got.Name)
	}
}

func TestSelectInstancePicksLowestPri(t *testing.T) {
	insts := []dnssdcodec.Instance{
		{Name: "dev", Service: dnssdcodec.ServiceSystem,
			TXT: map[string]string{
				dnssdcodec.TXTKeyAPIProto: "http",
				dnssdcodec.TXTKeyAPIVer:   "v1.0",
				dnssdcodec.TXTKeyPriority: "100",
			}},
		{Name: "prod", Service: dnssdcodec.ServiceSystem,
			TXT: map[string]string{
				dnssdcodec.TXTKeyAPIProto: "http",
				dnssdcodec.TXTKeyAPIVer:   "v1.0",
				dnssdcodec.TXTKeyPriority: "0",
			}},
		{Name: "mid", Service: dnssdcodec.ServiceSystem,
			TXT: map[string]string{
				dnssdcodec.TXTKeyAPIProto: "http",
				dnssdcodec.TXTKeyAPIVer:   "v1.0",
				dnssdcodec.TXTKeyPriority: "50",
			}},
	}
	got, err := SelectInstance(insts, "http", "v1.0", nil)
	if err != nil {
		t.Fatalf("SelectInstance: %v", err)
	}
	if got.Name != "prod" {
		t.Fatalf("selected %q, want prod (pri 0)", got.Name)
	}
}

func TestSelectInstanceTieBreakStaysWithinTied(t *testing.T) {
	insts := []dnssdcodec.Instance{
		{Name: "a", Service: dnssdcodec.ServiceSystem,
			TXT: map[string]string{
				dnssdcodec.TXTKeyAPIProto: "http",
				dnssdcodec.TXTKeyAPIVer:   "v1.0",
				dnssdcodec.TXTKeyPriority: "0",
			}},
		{Name: "b", Service: dnssdcodec.ServiceSystem,
			TXT: map[string]string{
				dnssdcodec.TXTKeyAPIProto: "http",
				dnssdcodec.TXTKeyAPIVer:   "v1.0",
				dnssdcodec.TXTKeyPriority: "0",
			}},
		{Name: "c", Service: dnssdcodec.ServiceSystem,
			TXT: map[string]string{
				dnssdcodec.TXTKeyAPIProto: "http",
				dnssdcodec.TXTKeyAPIVer:   "v1.0",
				dnssdcodec.TXTKeyPriority: "5",
			}},
	}
	picked := map[string]int{}
	for i := 0; i < 200; i++ {
		got, err := SelectInstance(insts, "http", "v1.0", nil)
		if err != nil {
			t.Fatalf("SelectInstance: %v", err)
		}
		if got.Name == "c" {
			t.Fatal("tie-break must never reach pri=5 entry")
		}
		picked[got.Name]++
	}
	// 200 trials, two equally-likely outcomes — both must appear.
	if picked["a"] == 0 || picked["b"] == 0 {
		t.Fatalf("tie-break covers both winners across 200 trials: %v", picked)
	}
}

func TestSelectInstanceMissingPriDeprioritised(t *testing.T) {
	insts := []dnssdcodec.Instance{
		{Name: "no-pri", Service: dnssdcodec.ServiceSystem,
			TXT: map[string]string{
				dnssdcodec.TXTKeyAPIProto: "http",
				dnssdcodec.TXTKeyAPIVer:   "v1.0",
			}},
		{Name: "with-pri", Service: dnssdcodec.ServiceSystem,
			TXT: map[string]string{
				dnssdcodec.TXTKeyAPIProto: "http",
				dnssdcodec.TXTKeyAPIVer:   "v1.0",
				dnssdcodec.TXTKeyPriority: "10",
			}},
	}
	got, err := SelectInstance(insts, "http", "v1.0", nil)
	if err != nil {
		t.Fatalf("SelectInstance: %v", err)
	}
	if got.Name != "with-pri" {
		t.Fatalf("missing pri should fall to the back, got %q", got.Name)
	}
}

func TestSelectInstanceNoneMatch(t *testing.T) {
	insts := []dnssdcodec.Instance{
		{Name: "wrong-proto", Service: dnssdcodec.ServiceSystem,
			TXT: map[string]string{
				dnssdcodec.TXTKeyAPIProto: "https",
				dnssdcodec.TXTKeyAPIVer:   "v1.0",
			}},
	}
	if _, err := SelectInstance(insts, "http", "v1.0", nil); err != ErrNoInstances {
		t.Fatalf("expected ErrNoInstances, got %v", err)
	}
}

func TestVersionListContains(t *testing.T) {
	cases := map[string]bool{
		"v1.0":        true,
		"v1.0,v1.1":   true,
		" v1.0 ,v1.1": true,
		"v0.9,v1.1":   false,
		"":            false,
		"v1.0.0":      false,
		"V1.0":        false, // case-sensitive on the leading V
	}
	for csv, want := range cases {
		if got := versionListContains(csv, "v1.0"); got != want {
			t.Fatalf("versionListContains(%q) = %v, want %v", csv, got, want)
		}
	}
}

func TestFetchAgainstFakeServer(t *testing.T) {
	// Build a spec-valid Global, serve it on a httptest server, fetch it.
	globalRaw := []byte(`{
  "id": "3b8be755-08ff-452b-b217-c9151eb21193",
  "version": "1441700172:318426300",
  "label": "ZBQ System",
  "description": "System Global Information for ZBQ",
  "tags": {},
  "is04": {
    "heartbeat_interval": 8
  },
  "ptp": {
    "announce_receipt_timeout": 2,
    "domain_number": 57
  }
}`)
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		switch r.URL.Path {
		case "/x-nmos/system/v1.0/":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(is09.IndexBody())
		case "/x-nmos/system/v1.0/global":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.Copy(w, strings.NewReader(string(globalRaw)))
		default:
			w.WriteHeader(stdhttp.StatusNotFound)
		}
	}))
	defer srv.Close()

	host, port := splitHostPortForTest(srv.URL)
	res, err := Fetch(context.Background(), IS09FetchOptions{
		APIVer:     "v1.0",
		APIProto:   "http",
		Direct:     host + ":" + strconv.Itoa(port),
		HTTPClient: httpsession.NewClient(),
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if res.Global.IS04.HeartbeatInterval != 8 {
		t.Fatalf("heartbeat = %d", res.Global.IS04.HeartbeatInterval)
	}
	if res.Global.PTP.DomainNumber != 57 {
		t.Fatalf("ptp.domain_number = %d", res.Global.PTP.DomainNumber)
	}
}

func TestFetchAbsorbsOutOfSpecPeer(t *testing.T) {
	// Out-of-range heartbeat (1001 > max 1000).
	bad := `{
  "id": "3b8be755-08ff-452b-b217-c9151eb21193",
  "version": "0:0",
  "label": "X",
  "description": "Y",
  "tags": {},
  "is04": {"heartbeat_interval": 1001},
  "ptp": {"announce_receipt_timeout": 2, "domain_number": 0}
}`
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, bad)
	}))
	defer srv.Close()
	host, port := splitHostPortForTest(srv.URL)
	var rep spec.SliceReporter
	res, err := Fetch(context.Background(), IS09FetchOptions{
		APIVer:   "v1.0",
		APIProto: "http",
		Direct:   host + ":" + strconv.Itoa(port),
		Reporter: &rep,
	})
	// Inverted deliberately. Refusing a /global sends this Node back to
	// picking a Registry from mDNS priority alone — the precise outcome
	// IS-09 exists to stop. AMWA's own IS-09-02 mock serves a /global
	// missing `label` and `description`, and rejecting it scored "did
	// not attempt to contact the advertised System API" on all four
	// minors while the Node had in fact contacted it.
	if err != nil {
		t.Fatalf("an out-of-spec /global must be absorbed, not refused: %v", err)
	}
	if res == nil || res.Global == nil {
		t.Fatal("the config must still reach the caller")
	}
	if res.Global.IS04.HeartbeatInterval != 1001 {
		t.Fatalf("the peer's own value must survive, got %d", res.Global.IS04.HeartbeatInterval)
	}
	events := rep.Snapshot()
	if len(events) != 1 {
		t.Fatalf("the deviation must be reported exactly once, got %d", len(events))
	}
	if events[0].Code != "nmos_is09_global_deviation" {
		t.Fatalf("code = %q", events[0].Code)
	}
	if events[0].Severity != spec.SeverityWarn {
		t.Fatalf("severity = %v, want Warn", events[0].Severity)
	}
}

// TestFetchWithoutReporterStillAbsorbs: tolerance must not depend on a
// Reporter being wired. A caller who never passes one still gets the
// config; the deviation simply goes unrecorded.
func TestFetchWithoutReporterStillAbsorbs(t *testing.T) {
	bad := `{"id":"3b8be755-08ff-452b-b217-c9151eb21193","version":"0:0",
	  "is04":{"heartbeat_interval":5},
	  "ptp":{"announce_receipt_timeout":2,"domain_number":0}}`
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, bad)
	}))
	defer srv.Close()
	host, port := splitHostPortForTest(srv.URL)
	res, err := Fetch(context.Background(), IS09FetchOptions{
		APIVer: "v1.0", APIProto: "http",
		Direct: host + ":" + strconv.Itoa(port),
	})
	if err != nil {
		t.Fatalf("no Reporter must not make this fatal: %v", err)
	}
	if res.Global.IS04.HeartbeatInterval != 5 {
		t.Fatalf("heartbeat_interval = %d", res.Global.IS04.HeartbeatInterval)
	}
}

// splitHostPortForTest turns httptest.NewServer().URL ("http://127.0.0.1:NNNN")
// into ("127.0.0.1", NNNN) for the IS09FetchOptions.Direct flag.
func splitHostPortForTest(url string) (string, int) {
	u := strings.TrimPrefix(url, "http://")
	i := strings.LastIndex(u, ":")
	if i < 0 {
		return u, 0
	}
	port, _ := strconv.Atoi(u[i+1:])
	return u[:i], port
}
