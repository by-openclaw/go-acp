package main

import (
	"bytes"
	"context"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/datastore"
	"dhs/internal/manifest"
	"dhs/internal/rrcs/codec"
)

// rrcsChangingFake is the tree gateway that renames its conference on
// the second reading, answers one port's GetPort with a new multicast,
// and announces one configuration change once watch is registered.
func rrcsChangingFake(t *testing.T, cancel context.CancelFunc) (*rrcsFake, func(string) int) {
	t.Helper()
	var mu sync.Mutex
	reads := 0
	counts := map[string]int{}
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		mu.Lock()
		counts[call.Method]++
		mu.Unlock()
		k := call.Params[0]
		switch call.Method {
		case "GetAllConferences":
			mu.Lock()
			reads++
			label := "Conf 003"
			if reads > 1 {
				label = "RENAMED"
			}
			mu.Unlock()
			return codec.Array(k, codec.Array(codec.Struct(rrcsMember("ObjectID", codec.Int(300)),
				rrcsMember("Label", codec.String(label)), rrcsMember("LongName", codec.String("Conference 003"))))), true
		case "GetPort":
			return codec.Array(k, codec.Int(0), codec.Struct(rrcsMember("Label", codec.String("O.-7")), rrcsMember("ObjectID", codec.Int(101)),
				rrcsMember("PortAes67Output", codec.Struct(rrcsMember("Protocol", codec.Int(2)), rrcsMember("Multicast", codec.String("239.9.9.9")))))), true
		case "RegisterForAllEvents":
			base := "http://127.0.0.1:" + strconv.Itoa(int(call.Params[1].Int))
			go func() {
				doc := must(codec.EncodeCall("ConfigurationChange", codec.String("R0000000001")))
				if resp, err := http.Post(base+"/RPC2", "text/xml", bytes.NewReader(doc)); err == nil {
					_ = resp.Body.Close()
				}
				time.Sleep(1500 * time.Millisecond)
				cancel()
			}()
			return k, true
		case "UnregisterForAllEvents":
			return codec.Array(k, codec.Int(0)), true
		}
		return rrcsTreeAnswer(call)
	})
	return f, func(method string) int {
		mu.Lock()
		defer mu.Unlock()
		return counts[method]
	}
}

func rrcsWatchRun(t *testing.T, ctx context.Context, f *rrcsFake, extra ...string) string {
	t.Helper()
	args := append([]string{"watch", f.addr(), "--volume", "no", "--listen", "127.0.0.1:0", "--check", "0"}, extra...)
	out, err := rrcsStdout(t, func() error { return runRRCS(ctx, args) })
	if err != nil {
		t.Fatalf("watch %v: %v", extra, err)
	}
	return out
}

// A configuration change reads the lists again, not the keys of every
// panel; --changes keys reads those too. The change rewrites the
// inventory.
func TestRRCSWatchChangesReadsLists(t *testing.T) {
	prev := treeStore
	treeStore = datastore.NewTreeStore(filepath.Join(t.TempDir(), ".cache"))
	t.Cleanup(func() { treeStore = prev })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f, count := rrcsChangingFake(t, cancel)
	out := rrcsWatchRun(t, ctx, f)
	rrcsWant(t, out, "gateway Configuration = changed", "oid=300 conference.300 Label = RENAMED")
	if count("GetPortsCommandLists") != 0 || count("GetAllPorts") != 2 || count("GetVersion") != 1 {
		t.Errorf("requests: commands %d, port lists %d, version %d", count("GetPortsCommandLists"), count("GetAllPorts"), count("GetVersion"))
	}
	if _, err := manifest.Load(filepath.Join(treeStore.BaseDir(), "manifest", "rrcs", "127.0.0.1.json")); err != nil {
		t.Errorf("inventory after the change: %v", err)
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	f2, count2 := rrcsChangingFake(t, cancel2)
	out = rrcsWatchRun(t, ctx2, f2, "--changes", "keys")
	rrcsWant(t, out, "conference.300 Label = RENAMED")
	if count2("GetPortsCommandLists") == 0 {
		t.Errorf("--changes keys read no keys")
	}
}

// --match naming one port: the change re-reads that port alone, and only
// its lines are printed.
func TestRRCSWatchMatchRereadsOneObject(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f, count := rrcsChangingFake(t, cancel)
	out := rrcsWatchRun(t, ctx, f, "--match", "net.1.node.61.port.7.out")
	rrcsWant(t, out, "gateway Configuration = changed", "net.1.node.61.port.7.out PortAes67Output.Multicast = 239.9.9.9", "was 239.1.2.3")
	if strings.Contains(out, "RENAMED") {
		t.Errorf("a line outside --match was printed:\n%s", out)
	}
	if count("GetPort") != 1 || count("GetAllPorts") != 1 {
		t.Errorf("requests: GetPort %d, GetAllPorts %d (want 1 and 1)", count("GetPort"), count("GetAllPorts"))
	}
}

// --oid and --kind keep the lines of one object or one kind.
func TestRRCSWatchFilters(t *testing.T) {
	for _, tc := range []struct {
		flags []string
		want  bool
	}{
		{[]string{"--oid", "300"}, true},
		{[]string{"--oid", "301"}, false},
		{[]string{"--kind", "conference"}, true},
		{[]string{"--kind", "ifb"}, false},
	} {
		ctx, cancel := context.WithCancel(context.Background())
		f, _ := rrcsChangingFake(t, cancel)
		out := rrcsWatchRun(t, ctx, f, tc.flags...)
		cancel()
		if got := strings.Contains(out, "conference.300 Label = RENAMED"); got != tc.want {
			t.Errorf("%v: line printed %v, want %v:\n%s", tc.flags, got, tc.want, out)
		}
		rrcsWant(t, out, "gateway Registration = registered")
	}
	if err := runRRCS(context.Background(), []string{"watch", "h", "--kind", "panels"}); err == nil || !strings.Contains(err.Error(), "--kind takes") {
		t.Errorf("bad --kind: %v", err)
	}
}
