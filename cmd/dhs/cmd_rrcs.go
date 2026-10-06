package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/rrcs/codec"
	rrcs "dhs/internal/rrcs/consumer"
	"dhs/internal/transport"
	"dhs/internal/wiretrace"
)

const rrcsProto = "rrcs"

// rrcsInfoMethods are the status requests of `info` (spec 9.0.1 §8.8).
var rrcsInfoMethods = []string{
	"GetVersion", "GetState", "IsConnectedToArtist", "GetConfigurationID",
}

// rrcsDiscoverMethods are the read-only list requests of `discover`
// (§8.1, §8.6, §8.7, §8.8). Each takes the transaction key alone.
var rrcsDiscoverMethods = []string{
	"GetAllNodes", "GetAllClientCards", "GetAllDevices", "GetAllPorts",
	"GetAllConferences", "GetAllGroups", "GetAllIFBs", "GetAllLogicSources_v2",
	"GetAllGpIns", "GetAllGpOuts", "GetAllActiveXps",
}

func rrcsValErr(verb, reason string) error {
	return &consumer.ValidationError{Field: "rrcs " + verb, Reason: reason}
}

// runRRCS is the dispatcher for `dhs consumer rrcs <verb>`.
func runRRCS(ctx context.Context, args []string) error {
	if len(args) == 0 || isHelpToken(args[0]) {
		printRRCSHelp()
		return nil
	}
	switch args[0] {
	case "info":
		return rrcsQuery(ctx, "info", rrcsInfoMethods, args[1:])
	case "discover":
		return rrcsQuery(ctx, "discover", rrcsDiscoverMethods, args[1:])
	case "watch":
		return rrcsWatch(ctx, args[1:])
	case "walk":
		return rrcsWalk(ctx, args[1:])
	case "get":
		return rrcsGet(ctx, args[1:])
	case "list":
		return rrcsList(ctx, args[1:])
	case "tree":
		return rrcsTree(ctx, args[1:])
	}
	return rrcsValErr(args[0], "unknown verb (run 'dhs consumer rrcs --help')")
}

func printRRCSHelp() {
	fmt.Println(`dhs consumer rrcs — Riedel RRCS gateway (XML-RPC over HTTP, default port 8193)

USAGE
  dhs consumer rrcs <verb> <host>[:port] [flags]

VERBS

  Verb      Wire
  --------  ---------------------------------------------------------------
  info      GetVersion, GetState, IsConnectedToArtist, GetConfigurationID
  discover  GetAllNodes, GetAllClientCards, GetAllDevices, GetAllPorts,
            GetAllConferences, GetAllGroups, GetAllIFBs, GetAllLogicSources_v2,
            GetAllGpIns, GetAllGpOuts, GetAllActiveXps — read only
  walk      everything readable, into one JSON snapshot: info + discover +
            type lists, GetLicenseInfo per node, GetObjectList per object
            type, GetObjectProperty of every object, GetPortsCommandLists
            of every port (what is on its keys) — read only
  tree      the system as a tree: node, client cards, ports, what is on
            each key; conferences, groups, IFBs with members, logic sources
  list      one table: nodes | cards | ports | panels | keys | conferences |
            groups | ifbs | logic; --node, --type, --match select rows
  get       the properties of one thing: --path (as tree and list print it),
            or --id (GetObjectProperty; --names yes = GetObjectPropertyNames)

  tree, list and get --path also read a snapshot written by walk: --from FILE
  watch     RegisterForAllEvents, then every event RRCS sends; answers
            GetAlive; UnregisterForAllEvents on Ctrl+C

Run 'dhs consumer rrcs <verb> --help' for the flags of a verb.`)
}

// rrcsFlags is the flag set common to the rrcs verbs.
type rrcsFlags struct {
	timeout time.Duration
	output  string
	capture string

	// note writes one line of the verb's own account of the run into
	// the capture, so the file explains itself without the console.
	note func(text string)
}

// say prints a diagnostic on stderr and keeps it in the capture.
func (c *rrcsFlags) say(format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	fmt.Fprintln(os.Stderr, text)
	if c.note != nil {
		c.note(text)
	}
}

func newRRCSFlags(fs *flag.FlagSet) *rrcsFlags {
	c := &rrcsFlags{}
	fs.DurationVar(&c.timeout, "timeout", rrcs.DefaultTimeout, "per-request timeout")
	fs.StringVar(&c.output, "output", "text", "output: text | json")
	fs.StringVar(&c.capture, "capture", "", "record every XML document sent and received to this JSONL wire-trace (it holds the configuration names in clear). Literal \"auto\" = captures/rrcs/<host>/<verb>-<utcstamp>.jsonl (ADR-0028)")
	return c
}

// open builds the client and, with --capture, the recorder behind its tap.
func (c *rrcsFlags) open(verb, addr string) (*rrcs.Client, rrcs.Tap, func(), error) {
	if c.output != "text" && c.output != "json" {
		return nil, nil, nil, rrcsValErr(verb, "--output must be text or json")
	}
	var tap rrcs.Tap
	closeFn := func() {}
	if c.capture == "auto" {
		c.capture = defaultCapturePath(rrcsProto, hostOnly(addr), verb, "", time.Now())
		fmt.Fprintf(os.Stderr, "rrcs %s: --capture auto → %s (ADR-0028)\n", verb, c.capture)
	}
	if c.capture != "" {
		if err := os.MkdirAll(filepath.Dir(c.capture), 0o755); err != nil {
			return nil, nil, nil, fmt.Errorf("capture dir: %w", err)
		}
		rec, err := transport.NewRecorder(c.capture)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("capture: %w", err)
		}
		rec.WriteMeta(captureMeta(rrcsProto, addr, verb))
		tap = func(dir wiretrace.Direction, _ string, doc []byte) { rec.Record(rrcsProto, string(dir), doc) }
		c.note = func(text string) {
			rec.WriteMeta(struct {
				Note string `json:"note"`
			}{text})
		}
		closeFn = func() { _ = rec.Close() }
	}
	client, err := rrcs.NewClient(rrcs.Config{Addr: addr, Timeout: c.timeout, Tap: tap})
	if err != nil {
		closeFn()
		return nil, nil, nil, rrcsValErr(verb, err.Error())
	}
	return client, tap, closeFn, nil
}

// rrcsJSON turns an XML-RPC value into what encoding/json prints.
func rrcsJSON(v codec.Value) any {
	switch v.Kind {
	case codec.KindInt:
		return v.Int
	case codec.KindBool:
		return v.Bool
	case codec.KindDouble:
		return v.Double
	case codec.KindBase64:
		return base64.StdEncoding.EncodeToString(v.Bytes)
	case codec.KindStruct:
		m := make(map[string]any, len(v.Members))
		for _, mem := range v.Members {
			m[mem.Name] = rrcsJSON(mem.Value)
		}
		return m
	case codec.KindArray:
		a := make([]any, len(v.Items))
		for i, it := range v.Items {
			a[i] = rrcsJSON(it)
		}
		return a
	}
	return v.Str
}

// rrcsCompact is the one-line rendering of a value.
func rrcsCompact(v codec.Value) string {
	b, err := json.Marshal(rrcsJSON(v))
	if err != nil {
		return err.Error()
	}
	return string(b)
}

// rrcsSize says how much an answer holds: the entries of the list it
// wraps, or its own elements.
func rrcsSize(payload codec.Value) string {
	switch payload.Kind {
	case codec.KindArray:
		if len(payload.Items) == 1 && payload.Items[0].Kind == codec.KindArray {
			return strconv.Itoa(len(payload.Items[0].Items)) + " entries"
		}
		return strconv.Itoa(len(payload.Items)) + " elements"
	case codec.KindStruct:
		return strconv.Itoa(len(payload.Members)) + " members"
	}
	return "1 value"
}

// rrcsQueryResult is one request of info / discover.
type rrcsQueryResult struct {
	Method  string `json:"method"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Millis  int64  `json:"ms"`
	Payload any    `json:"payload,omitempty"`
}

// rrcsQuery runs a fixed list of read-only requests, each alone: one that
// fails does not stop the next.
func rrcsQuery(ctx context.Context, verb string, methods []string, args []string) error {
	fs := flag.NewFlagSet("rrcs "+verb, flag.ContinueOnError)
	cf := newRRCSFlags(fs)
	if err := parseVerbFlags(fs, reorderFlagsFirst(args)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return rrcsValErr(verb, "want exactly one host[:port] argument")
	}
	client, _, closeFn, err := cf.open(verb, fs.Arg(0))
	if err != nil {
		return err
	}
	defer closeFn()

	results := make([]rrcsQueryResult, 0, len(methods))
	failed := 0
	for _, method := range methods {
		start := time.Now()
		reply, err := client.Call(ctx, method)
		res := rrcsQueryResult{Method: method, OK: err == nil, Millis: time.Since(start).Milliseconds()}
		if err != nil {
			res.Error = err.Error()
			failed++
		} else {
			res.Payload = rrcsJSON(reply.Payload())
		}
		results = append(results, res)
		if cf.output == "text" {
			switch {
			case err != nil:
				fmt.Printf("%-22s FAILED  %5d ms  %v\n", method, res.Millis, err)
			case verb == "info":
				fmt.Printf("%-22s ok      %5d ms  %s\n", method, res.Millis, rrcsCompact(reply.Payload()))
			default:
				fmt.Printf("%-22s ok      %5d ms  %s\n", method, res.Millis, rrcsSize(reply.Payload()))
			}
		}
		if ctx.Err() != nil {
			break
		}
	}
	if cf.output == "json" {
		doc := struct {
			Target  string            `json:"target"`
			Verb    string            `json:"verb"`
			Results []rrcsQueryResult `json:"results"`
		}{client.Peer(), verb, results}
		b, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
	}
	if failed == len(results) {
		return fmt.Errorf("rrcs %s: no request was answered by %s", verb, client.Peer())
	}
	return nil
}

// rrcsWatch registers for every event and prints what RRCS sends until
// the context ends.
func rrcsWatch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("rrcs watch", flag.ContinueOnError)
	cf := newRRCSFlags(fs)
	listen := fs.String("listen", ":8195", "local [ip]:port RRCS sends its events to. RRCS uses the source address of our registration and this port (§8.15.1)")
	path := fs.String("path", rrcs.DefaultPath, "URL path RRCS posts its events to")
	check := fs.Duration("check", 30*time.Second, "ask RRCS this often whether we are still registered, and register again if not; 0 = never")
	alive := fs.String("alive", "count", "GetAlive pings: count (summary only) | show (one line each)")
	if err := parseVerbFlags(fs, reorderFlagsFirst(args)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return rrcsValErr("watch", "want exactly one host[:port] argument")
	}
	if *alive != "count" && *alive != "show" {
		return rrcsValErr("watch", "--alive must be count or show")
	}
	if !rrcs.ValidPath(*path) {
		return rrcsValErr("watch", "--path: ASCII without control characters or spaces only")
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		return fmt.Errorf("rrcs watch: listen %s: %w", *listen, err)
	}
	reg := rrcs.Registration{Port: ln.Addr().(*net.TCPAddr).Port, Path: *path}

	client, tap, closeFn, err := cf.open("watch", fs.Arg(0))
	if err != nil {
		_ = ln.Close()
		return err
	}
	defer closeFn()

	var mu sync.Mutex // one line at a time on stdout
	jsonOut := cf.output == "json"
	listener := &rrcs.Listener{
		Path: *path,
		Tap:  tap,
		OnReject: func(remote string, err error) {
			cf.say("rrcs watch: unreadable request from %s: %v", remote, err)
		},
		OnEvent: func(e rrcs.Event) {
			if e.Method == rrcs.MethodGetAlive && *alive != "show" {
				return
			}
			params := e.Params
			if e.TransKey != "" {
				params = params[1:]
			}
			mu.Lock()
			defer mu.Unlock()
			stamp := e.Time.UTC().Format("2006-01-02T15:04:05.000Z")
			if jsonOut {
				b, _ := json.Marshal(struct {
					Time     string `json:"ts"`
					Remote   string `json:"remote"`
					Method   string `json:"method"`
					TransKey string `json:"trans_key,omitempty"`
					Params   any    `json:"params"`
				}{stamp, e.Remote, e.Method, e.TransKey, rrcsJSON(codec.Array(params...))})
				fmt.Println(string(b))
				return
			}
			fmt.Printf("%s  %-24s %s\n", stamp, e.Method, rrcsCompact(codec.Array(params...)))
		},
	}
	srv := &http.Server{Handler: listener, ReadHeaderTimeout: 10 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	defer func() {
		shut, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()

	if _, err := client.Register(ctx, reg); err != nil {
		return fmt.Errorf("rrcs watch: %w", err)
	}
	cf.say("rrcs watch: registered at %s — events go to port %d path %s; Ctrl+C to stop",
		client.Peer(), reg.Port, rrcs.NormalizePath(reg.Path))

	var tick <-chan time.Time
	if *check > 0 {
		t := time.NewTicker(*check)
		defer t.Stop()
		tick = t.C
	}
	registrations := 1
	var runErr error
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case err := <-serveErr:
			if !errors.Is(err, http.ErrServerClosed) {
				runErr = fmt.Errorf("rrcs watch: listener: %w", err)
			}
			break loop
		case <-tick:
			ok, err := client.IsRegistered(ctx, reg)
			switch {
			case ctx.Err() != nil:
			case err != nil:
				cf.say("rrcs watch: registration check: %v", err)
			case !ok:
				cf.say("rrcs watch: RRCS dropped the registration — registering again")
				if _, err := client.Register(ctx, reg); err != nil {
					cf.say("rrcs watch: %v", err)
				} else {
					registrations++
				}
			}
		}
	}

	// The context is over; the goodbye needs one of its own.
	cf.say("rrcs watch: stopping — unregistering (up to %s)", cf.timeout)
	bye, cancel := context.WithTimeout(context.Background(), cf.timeout)
	defer cancel()
	start := time.Now()
	if _, err := client.Unregister(bye, reg); err != nil {
		cf.say("rrcs watch: unregister failed after %s: %v", time.Since(start).Round(time.Millisecond), err)
	} else {
		cf.say("rrcs watch: unregistered in %s", time.Since(start).Round(time.Millisecond))
	}
	cf.say("rrcs watch: %d events, %d GetAlive answered, %d registration(s)",
		listener.Events(), listener.Alives(), registrations)
	return runErr
}
