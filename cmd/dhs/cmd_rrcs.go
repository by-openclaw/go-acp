package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"dhs/internal/clock"
	"dhs/internal/consumer"
	"dhs/internal/consumer/alarm"
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
	case "set":
		return rrcsSet(ctx, args[1:])
	case "export":
		return rrcsExport(ctx, args[1:])
	case "call":
		return rrcsCall(ctx, args[1:])
	case "ensure":
		return rrcsEnsure(ctx, args[1:])
	case "xp":
		return rrcsXpVerb(ctx, args[1:])
	case "import":
		return rrcsImport(ctx, args[1:])
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
  list      one table: nodes | cards | ports | panels | keys | streams |
            sources | dests | xp | conferences | groups | ifbs | logic |
            users | patches | logicdests (those three from a walk snapshot);
            --node, --type, --match select rows. streams = the AES67
            receivers and senders; sources and dests = the two axes of the
            crosspoint matrix; xp = the crosspoints active now
  get       the properties of one thing: --path (as tree and list print it),
            or --id (GetObjectProperty; --names yes = GetObjectPropertyNames)

  set       WRITES: edit properties of a port or of a client card
            (ConfigurationChangeEx): --path, --prop NAME=VALUE (repeat).
            Shows the change and sends nothing unless --apply yes

  export    the values of the ports and client cards to one file, one row
            per value: --format json|csv, --out FILE, --path TEXT
  import    WRITES: every writable value of an export file that differs
            from the live system (ConfigurationChangeEx): --file, --path,
            --dry-run to compare and send nothing

  ensure    converge to a desired-state file (values, crosspoints): --file,
            --check to report and send nothing; the contract of every dhs
            ensure, for Ansible. Without --check it WRITES
  xp        one crosspoint: read its state (GetXpStatus), or WRITE it with
            --state on|off (SetXp, KillXp): --src PATH --dst PATH
  call      any method of the specification, parameters as JSON (--arg);
            a method that is not Get… or Is… WRITES and needs --write-to

  tree, list, export and get --path also read a snapshot written by walk:
  --from FILE
  watch     RegisterForAllEvents, then every event RRCS sends; answers
            GetAlive; UnregisterForAllEvents on Ctrl+C; --spy adds the keys
            pressed and released on the panels (ChangePanelSpyRegistry)

Run 'dhs consumer rrcs <verb> --help' for the flags of a verb.`)
}

// rrcsFlags is the flag set common to the rrcs verbs.
type rrcsFlags struct {
	timeout time.Duration
	output  string
	capture string

	// The uniform logging contract (docs/logging.md): a local file in
	// syslog format by default, optionally a remote syslog server.
	logPath      string
	logFormat    string
	logLevel     string
	syslogAddr   string
	logRetention int
	// sink receives the operational lines and the event stream; nil
	// when every sink is off.
	sink *slog.Logger

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
	if c.sink != nil {
		c.sink.Info(text, slog.String("proto", rrcsProto))
	}
}

// logValue writes one decoded event to the sinks, with the fields of the
// generic watch (docs/logging.md, msg=value_change).
func (c *rrcsFlags) logValue(l rrcsChangeLine) {
	if c.sink == nil {
		return
	}
	attrs := []any{
		slog.String("proto", rrcsProto), slog.String("event", l.Event),
		slog.String("path", l.Path), slog.String("label", l.Label), slog.String("value", l.Value),
	}
	if l.OID != 0 {
		attrs = append(attrs, slog.Int("oid", l.OID))
	}
	if l.Unit != "" {
		attrs = append(attrs, slog.String("unit", l.Unit))
	}
	if l.Name != "" {
		attrs = append(attrs, slog.String("name", l.Name))
	}
	c.sink.Info("value_change", attrs...)
}

func newRRCSFlags(fs *flag.FlagSet) *rrcsFlags {
	c := &rrcsFlags{}
	fs.DurationVar(&c.timeout, "timeout", rrcs.DefaultTimeout, "per-request timeout")
	fs.StringVar(&c.output, "output", "text", "output: text | json")
	fs.StringVar(&c.logPath, "log", "auto", "local log FILE in --log-format. Default \"auto\" = .cache/logs/rrcs/<host>/<verb>.log, one file per day; a path overrides it; \"off\" disables the local file")
	fs.StringVar(&c.logFormat, "log-format", DefaultLogFormat, "log format: syslog (RFC 5424, default) | json | text — the log stream only, the terminal stays as it is")
	fs.StringVar(&c.logLevel, "log-level", "info", "log level: debug | info | warn | error")
	fs.StringVar(&c.syslogAddr, "syslog-addr", "", "also forward the logs as RFC 5424 UDP datagrams to host:port")
	fs.IntVar(&c.logRetention, "log-retention", 0, "days of daily log files to keep; 0 = keep every day")
	fs.StringVar(&c.capture, "capture", "", "record every XML document sent and received to this JSONL wire-trace (it holds the configuration names in clear). Literal \"auto\" = captures/rrcs/<host>/<verb>-<utcstamp>.jsonl (ADR-0028)")
	return c
}

// open builds the client and, with --capture, the recorder behind its tap.
func (c *rrcsFlags) open(verb, addr string) (*rrcs.Client, rrcs.Tap, func(), error) {
	if c.output != "text" && c.output != "json" {
		return nil, nil, nil, rrcsValErr(verb, "--output must be text or json")
	}
	var tap rrcs.Tap
	_, sink, logClose, _, lerr := buildConsumerLoggers(parseLogLevel(c.logLevel), c.logFormat, c.logPath, c.syslogAddr,
		defaultLogPath(rrcsProto, hostOnly(addr), verb), c.logRetention)
	if lerr != nil {
		return nil, nil, nil, rrcsValErr(verb, lerr.Error())
	}
	c.sink = sink
	closeFn := logClose
	if c.capture == "auto" {
		c.capture = defaultCapturePath(rrcsProto, hostOnly(addr), verb, "", time.Now())
		fmt.Fprintf(os.Stderr, "rrcs %s: --capture auto → %s (ADR-0028)\n", verb, c.capture)
	}
	if c.capture != "" {
		if err := os.MkdirAll(filepath.Dir(c.capture), 0o755); err != nil {
			logClose()
			return nil, nil, nil, fmt.Errorf("capture dir: %w", err)
		}
		rec, err := transport.NewRecorder(c.capture)
		if err != nil {
			logClose()
			return nil, nil, nil, fmt.Errorf("capture: %w", err)
		}
		rec.WriteMeta(captureMeta(rrcsProto, addr, verb))
		tap = func(dir wiretrace.Direction, _ string, doc []byte) { rec.Record(rrcsProto, string(dir), doc) }
		c.note = func(text string) {
			rec.WriteMeta(struct {
				Note string `json:"note"`
			}{text})
		}
		closeFn = func() {
			_ = rec.Close()
			logClose()
		}
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
	spy := fs.String("spy", "none", "panel spy — key pressed and released, function keys, numeric keys, rotary encoders: none | all (every port that has keys) | NODE.PORT[,NODE.PORT...]. Adds one registration per panel on RRCS, removed on exit")
	events := fs.String("events", "values", "values = one line per value an event carries: time, object ID, path, member = value, with unit and range where the protocol has them (the system is read once at start to name things) | raw = the method and its parameters as received")
	alarmFile := fs.String("alarm", "", "judge the values with this alarm template (ADR-0033), e.g. internal/rrcs/alarm/RRCS@9.0.json: a line is printed, and logged with its severity, each time a verdict changes")
	alive := fs.String("alive", "count", "GetAlive pings: count (summary only) | show (one line each)")
	if err := parseVerbFlags(fs, reorderFlagsFirst(args)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return rrcsValErr("watch", "want exactly one host[:port] argument")
	}
	spyAll, spyPanels, err := rrcsParseSpy(*spy)
	if err != nil {
		return rrcsValErr("watch", err.Error())
	}
	if *events != "values" && *events != "raw" {
		return rrcsValErr("watch", "--events must be values or raw")
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

	var judge *alarm.Evaluator
	if *alarmFile != "" {
		tpl, err := alarm.LoadFile(*alarmFile)
		if err != nil {
			return rrcsValErr("watch", "--alarm: "+err.Error())
		}
		judge = alarm.New(tpl, clock.System())
	}
	// raise prints and logs one change of verdict.
	raise := func(tr alarm.Transition) {
		fmt.Printf("%s  ALARM %s\n", tr.At.UTC().Format("15:04:05.000"), tr.String())
		if cf.sink != nil {
			level := slog.LevelWarn
			if tr.Severity == alarm.Normal {
				level = slog.LevelInfo
			}
			cf.sink.Log(ctx, level, "alarm", slog.String("proto", rrcsProto), slog.String("severity", tr.Severity.String()),
				slog.String("prior", tr.Prior.String()), slog.String("path", tr.Path), slog.String("value", tr.Value),
				slog.String("band", tr.Band), slog.String("text", tr.Text))
		}
	}

	// The names behind the numbers of the events: read once, before the
	// registration, so no event meets a half-built tree.
	var model *rrcsModel
	if *events == "values" {
		snap, _, err := rrcsCollect(ctx, client, rrcsCollectOpts{commands: spyAll || len(spyPanels) > 0})
		if err == nil {
			model, err = rrcsModelOf(snap)
		}
		if err != nil {
			cf.say("rrcs watch: the system could not be read, events are printed with numbers only: %v", err)
		}
	}

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
			if *events == "raw" {
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
				return
			}
			for _, l := range rrcsDecode(model, e) {
				cf.logValue(l)
				if judge != nil {
					if tr := judge.Eval(client.Peer(), consumer.Event{Path: l.Path + "." + l.Label, Label: l.Label, Unit: l.Unit,
						Value: consumer.Value{Kind: consumer.KindString, Str: l.Value}}); tr != nil {
						raise(*tr)
					}
				}
				if jsonOut {
					l.Time = stamp
					b, _ := json.Marshal(l)
					fmt.Println(string(b))
					continue
				}
				fmt.Println(l.text())
			}
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

	// Panel spy is a second registration, per panel, on top of the first
	// (§9.9.1). It is made again after every new registration.
	if spyAll {
		reply, err := client.Call(ctx, "GetAllPorts")
		if err != nil {
			cf.say("rrcs watch: panel spy: %v", err)
		}
		for _, p := range rrcsListOf(reply) {
			if n, _ := rrcsFieldInt(p, "KeyCount"); n > 0 {
				node, _ := rrcsFieldInt(p, "Node")
				port, _ := rrcsFieldInt(p, "Port")
				spyPanels = append(spyPanels, [2]int{int(node), int(port)})
			}
		}
	}
	setSpy := func(c context.Context, on bool) {
		failed := 0
		for _, panel := range spyPanels {
			if _, err := client.PanelSpy(c, reg, panel[0], panel[1], on); err != nil {
				failed++
				cf.say("rrcs watch: panel spy node %d port %d: %v", panel[0], panel[1], err)
			}
		}
		if len(spyPanels) > 0 {
			cf.say("rrcs watch: panel spy on=%v for %d panel(s), %d refused", on, len(spyPanels), failed)
		}
	}
	setSpy(ctx, true)

	var tick <-chan time.Time
	if *check > 0 {
		t := time.NewTicker(*check)
		defer t.Stop()
		tick = t.C
	}
	// A verdict that waits out a hold becomes true with time alone.
	var sweep <-chan time.Time
	if judge != nil {
		st := time.NewTicker(time.Second)
		defer st.Stop()
		sweep = st.C
	}
	registrations := 1
	var runErr error
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-sweep:
			mu.Lock()
			for _, tr := range judge.Sweep() {
				raise(tr)
			}
			mu.Unlock()
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
					setSpy(ctx, true)
				}
			}
		}
	}

	// The context is over; the goodbye needs one of its own.
	cf.say("rrcs watch: stopping — unregistering (up to %s)", cf.timeout)
	bye, cancel := context.WithTimeout(context.Background(), cf.timeout)
	defer cancel()
	setSpy(bye, false)
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

// rrcsParseSpy reads the --spy value: none, all, or a list of NODE.PORT.
func rrcsParseSpy(v string) (all bool, panels [][2]int, err error) {
	switch v {
	case "", "none":
		return false, nil, nil
	case "all":
		return true, nil, nil
	}
	for _, item := range strings.Split(v, ",") {
		node, port, ok := strings.Cut(strings.TrimSpace(item), ".")
		n, err1 := strconv.Atoi(node)
		p, err2 := strconv.Atoi(port)
		if !ok || err1 != nil || err2 != nil || n < 0 || p < 0 {
			return false, nil, fmt.Errorf("--spy: %q is not NODE.PORT", item)
		}
		panels = append(panels, [2]int{n, p})
	}
	return false, panels, nil
}
