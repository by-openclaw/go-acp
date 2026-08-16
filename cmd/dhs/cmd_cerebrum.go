package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"dhs/internal/cerebrum-nb/codec"
	"dhs/internal/cerebrum-nb/codec/ws"
	cerebrum "dhs/internal/cerebrum-nb/consumer"
	"dhs/internal/consumer"
)

// cerebrumValErr returns a client-side ValidationError — mapped to exit 2
// (docs/protocols/error-codes.md: 0 outcome-ok / 1 runtime / 2 validation).
func cerebrumValErr(verb, reason string) error {
	return &consumer.ValidationError{Field: "cerebrum-nb " + verb, Reason: reason}
}

// cerebrumFlags is the common flag set for every dhs consumer cerebrum-nb
// verb. host[:port] is positional; everything else is a flag.
type cerebrumFlags struct {
	port     int
	user     string
	pass     string
	tls      bool
	insecure bool
	debug    bool
	logPath  string
	timeout  time.Duration
}

func newCerebrumFlags(fs *flag.FlagSet) *cerebrumFlags {
	c := &cerebrumFlags{}
	fs.IntVar(&c.port, "port", cerebrum.DefaultPort, "Cerebrum NB WebSocket port")
	fs.StringVar(&c.user, "user", os.Getenv("DHS_CEREBRUM_USER"), "NB username (or $DHS_CEREBRUM_USER)")
	fs.StringVar(&c.pass, "pass", os.Getenv("DHS_CEREBRUM_PASS"), "NB password (or $DHS_CEREBRUM_PASS)")
	fs.BoolVar(&c.tls, "tls", false, "use wss:// instead of ws://")
	fs.BoolVar(&c.insecure, "insecure-skip-verify", false, "with --tls, skip TLS cert verification")
	fs.BoolVar(&c.debug, "debug", false, "verbose RX/TX XML logging")
	fs.StringVar(&c.logPath, "log", "", "write the diagnostic log (incl. RX/TX XML at full debug verbosity) to this file — clean UTF-8, no PowerShell 2> stderr wrapping; stderr stays silent")
	fs.DurationVar(&c.timeout, "timeout", 5*time.Second, "per-request timeout")
	return c
}

// newLogger builds the verb logger per flags: --log FILE writes a clean
// debug-verbosity log to the file (stderr untouched — avoids PowerShell 5.1
// wrapping redirected native stderr into error records, the "red flag");
// otherwise stderr at Warn (quiet success) or Debug with --debug. The
// returned closer is a no-op for stderr.
func (c *cerebrumFlags) newLogger() (*slog.Logger, func(), error) {
	if c.logPath != "" {
		if dir := filepath.Dir(c.logPath); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, nil, fmt.Errorf("--log %s: %w", c.logPath, err)
			}
		}
		f, err := os.Create(c.logPath)
		if err != nil {
			return nil, nil, fmt.Errorf("--log %s: %w", c.logPath, err)
		}
		return slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug})), func() { _ = f.Close() }, nil
	}
	level := slog.LevelWarn // quiet stderr on success (PS 2> flags any stderr as error)
	if c.debug {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})), func() {}, nil
}

// cerebrumWriteFile writes an output file, creating missing parent
// directories first (so --out captures\x.csv works on a fresh host).
func cerebrumWriteFile(path string, data []byte) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0o644)
}

// reorderFlagsFirst moves any tokens that look like Go flags (start with
// '-') ahead of positional arguments, so that stdlib flag.Parse — which
// stops at the first non-flag — sees all flags before the host.
//
// This lets users write either:
//
//	cerebrum-nb connect --port 4008 --user u --pass p 10.41.64.95
//	cerebrum-nb connect 10.41.64.95 --port 4008 --user u --pass p
//
// Any '--' literal terminator is preserved at its original position.
func reorderFlagsFirst(args []string) []string {
	flags := make([]string, 0, len(args))
	pos := make([]string, 0, len(args))
	i := 0
	for i < len(args) {
		a := args[i]
		if a == "--" {
			// Everything after '--' is positional verbatim.
			pos = append(pos, args[i:]...)
			break
		}
		if len(a) > 1 && a[0] == '-' {
			flags = append(flags, a)
			// Bool-vs-value distinction: peek the next token; if it's not
			// itself a flag and the current flag form is "-name" (not
			// "-name=value"), treat the next token as the flag value.
			if !strings.Contains(a, "=") && i+1 < len(args) {
				next := args[i+1]
				if (len(next) <= 1 || next[0] != '-') && !isKnownBoolFlag(a) {
					flags = append(flags, next)
					i += 2
					continue
				}
			}
			i++
			continue
		}
		pos = append(pos, a)
		i++
	}
	return append(flags, pos...)
}

// isKnownBoolFlag reports whether the given flag is one of the cerebrum
// boolean flags (no value follows). Conservative — anything not listed
// is assumed to take a value.
func isKnownBoolFlag(a string) bool {
	name := strings.TrimLeft(a, "-")
	if eq := strings.IndexByte(name, '='); eq >= 0 {
		name = name[:eq]
	}
	switch name {
	case "tls", "insecure-skip-verify", "debug", "h", "help":
		return true
	}
	return false
}

// runCerebrum is the dispatcher for `dhs consumer cerebrum-nb <verb>`.
func runCerebrum(ctx context.Context, args []string) error {
	if len(args) == 0 || hasHelpFlag(args) {
		printCerebrumHelp()
		return nil
	}
	verb := args[0]
	rest := args[1:]
	switch verb {
	case "connect":
		return cerebrumConnect(ctx, rest)
	case "listen":
		return cerebrumListen(ctx, rest)
	case "list-devices":
		return cerebrumListDevices(ctx, rest)
	case "device-details":
		return cerebrumDeviceDetails(ctx, rest)
	case "device-value":
		return cerebrumDeviceValue(ctx, rest)
	case "list-categories":
		return cerebrumListCategories(ctx, rest)
	case "category-details":
		return cerebrumCategoryDetails(ctx, rest)
	case "list-salvo-groups":
		return cerebrumListSalvoGroups(ctx, rest)
	case "list-salvo-instances":
		return cerebrumListSalvoInstances(ctx, rest)
	case "salvo-instance-details":
		return cerebrumSalvoInstanceDetails(ctx, rest)
	case "keepalive-probe":
		return cerebrumKeepaliveProbe(ctx, rest)
	case "tree":
		return cerebrumTree(ctx, rest)
	case "watch":
		return cerebrumWatch(ctx, rest)
	case "route":
		return cerebrumRoute(ctx, rest)
	case "export":
		return cerebrumExportXpoint(ctx, rest)
	case "list-sources":
		return cerebrumListMne(ctx, rest, "SRCE_MNE", "srce")
	case "list-dests", "list-destinations":
		return cerebrumListMne(ctx, rest, "DEST_MNE", "dest")
	case "list-levels":
		return cerebrumListMne(ctx, rest, "LEVEL_MNE", "level")
	case "import":
		return cerebrumImportXpoint(ctx, rest)
	case "lock":
		return cerebrumLock(ctx, rest, codec.LockProtect)
	case "unlock":
		// Default = RELEASED, the wire-actual clearing value (live
		// 2026-08-16: the spec's RELEASE / UNLOCKED both NACK 8).
		return cerebrumLock(ctx, rest, codec.LockReleased)
	case "device-config":
		return cerebrumDeviceConfig(ctx, rest)
	case "set-mnemonic":
		return cerebrumSetMnemonic(ctx, rest)
	case "set-tags":
		return cerebrumSetTags(ctx, rest)
	case "salvo":
		return cerebrumSalvo(ctx, rest)
	case "category":
		return cerebrumCategory(ctx, rest)
	case "set-value":
		return cerebrumSetValue(ctx, rest)
	case "obtain-datastore":
		return cerebrumObtainDatastore(ctx, rest)
	}
	return fmt.Errorf("cerebrum-nb: unknown verb %q (run dhs consumer cerebrum-nb -h for the catalogue)", verb)
}

func printCerebrumHelp() {
	fmt.Println(`dhs consumer cerebrum-nb — EVS Cerebrum Northbound API (XML over WebSocket)

USAGE
  dhs consumer cerebrum-nb <verb> <host>[:port] [flags]

VERBS

  Verb                     Wire
  -----------------------  -----------------------------------------------
  connect                  POLL (LOGIN auto when --user/--pass set)
  listen                   SUBSCRIBE — routing / category / salvo / device events; Ctrl+C to stop  [--router IP: point the routing subscriptions at a specific router instead of the route-master 0.0.0.0]
  route                    ACTION <ROUTING TYPE='ROUTE'/> — single (--dest --srce --level), batch (--route dst:src:lvl), or --csv FILE
  list-sources             one-shot OBTAIN SRCE_MNE  → every source: ID + capability levels + label + alts  [--id N] [--out FILE]
  list-dests               one-shot OBTAIN DEST_MNE  → same for destinations (alias: list-destinations)     [--id N] [--out FILE]
  list-levels              one-shot OBTAIN LEVEL_MNE → every level ID + name  [--id N] [--out FILE]
  export                   one-shot OBTAIN wildcards → CSVs. Crosspoints only: [--out FILE] [--level N]. Full snapshot (src+dst+level mnemonics+xpoint+categories as -cat-src.csv/-cat-dst.csv): --out-dir DIR [--prefix P]
  import                   ENSURE (ADR-0007): read live state, diff vs CSVs, converge only differences. --in-dir DIR [--prefix P] reads the set export wrote (missing files = out of scope), or per-file --xpoint (--csv alias) / --src / --dst / --levels / --cat-src / --cat-dst (categories: category,type,value rows — row order = slot order; builds the navigation panel). --check = report would_change, send nothing. --output json = ADR-0007 {changed|would_change, diff[]} on stdout. Empty cell/absent column = untouched; --allow-clear makes an empty MANAGED cell clear the live label. Run-twice = 0.
  list-devices             OBTAIN <device_change type='LIST'/>  [--device-type Router|SNMP|Device]
  device-details           OBTAIN <device_change type='DETAILS'/>  --device IP --device-type DEVICE
  device-value             OBTAIN <device_change type='VALUE'/>    --device NAME --by-name --sub-device X --object Y
  list-categories          OBTAIN <category_change type='CATEGORY_LIST'/>
  category-details         OBTAIN <category_change type='CATEGORY_DETAILS'/>  --category NAME
  tree                     NB catalogue tree — canonical renderer (same as <proto> tree). Categories (§5.2) + Salvos (§5.3)  [--domain salvos|categories|all] [--alt N | --no-mne]; DEVICE OBJECT TREE (acp2-walk analogue, §5.4.3 group obtains): --device NAME --by-name --sub-device N [--path GROUP] [--max-requests N]. Common: [--format ascii|plantuml] [--path P] [--depth N] [--filter S] [--out FILE]
  list-salvo-groups        OBTAIN <salvo_change type='GROUP_LIST'/>
  list-salvo-instances     OBTAIN <salvo_change type='INSTANCE_LIST'/>      --group NAME
  salvo-instance-details   OBTAIN <salvo_change type='INSTANCE_DETAILS'/>   --group NAME --instance NAME
  keepalive-probe          DIAGNOSTIC — hold WS open, observe TCP keep-alives  [--idle DUR] [--send-login]
  watch                    SUBSCRIBE one device (§5.4): --device IP [--device-type T] = DETAILS state watch; --device NAME --by-name --sub-device S --object O = VALUE watch (object path must be known — wildcards refused, live-verified)

  Write verbs (§4 ACTION — auto-LOGIN with --user/--pass; require an authenticated session)
  -----------------------  -----------------------------------------------
  lock                     ACTION <ROUTING LOCK='…'/>         --kind SRCE_LOCK|DEST_LOCK [--srce ID|--dest ID] [--level ID | "1;2;3" | omit = ALL levels] [--duration S] [--mode locked|protected|locked_path|protected_path|released]
  unlock                   ACTION <ROUTING LOCK='RELEASED'/>  (same flags as lock; RELEASED is the wire-actual clearing value — the spec's RELEASE/UNLOCKED NACK on live Cerebrums)
  device-config            <DEVICE_CONFIGURATION TYPE='ADD|MODIFY|REMOVE'/>  add|modify|remove --device-type generic|panel|router|snmp --ip IP [per-type flags]
  set-mnemonic             ACTION <ROUTING TYPE='*_MNE'/>     --kind LEVEL_MNE|SRCE_MNE|DEST_MNE [--srce|--dest ID] --level ID --mnemonic TXT [--alt SLOT]
  set-tags                 ACTION <ROUTING TYPE='RM_*_TAGS'/> --kind RM_SRCE_TAGS|RM_DEST_TAGS [--srce|--dest ID] --tags a,b,c
  salvo                    ACTION <SALVO TYPE='…'/>           --op run|save|rename|description|delete --group G [--instance I] [--new-name N] [--description D] [--check] [--output json]
                           ENSURE (ADR-0007): description/rename/delete read live state first — already-converged = changed:false, nothing sent; run/save are events (always fire, always changed). --check sends nothing.
  category                 ACTION <CATEGORY TYPE='…'/>        --op create|modify|modify-all|modify-desc|delete|delete-item --category C [--index N] [--item-type T] [--value V] [--name N] [--label L] [--inherits P] [--description D]
  set-value                ACTION <DEVICE TYPE='SET_VALUE'/>  --device NAME --sub-device X --object Y --value V
  obtain-datastore         OBTAIN <datastore_change name='…'/>  --name PATH

FLAGS (order doesn't matter — flags can come before OR after the host)
  --port N                  WebSocket port (default 40007)
  --user U                  NB username (or $DHS_CEREBRUM_USER)
  --pass P                  NB password (or $DHS_CEREBRUM_PASS)
  --tls                     use wss:// instead of ws://
  --insecure-skip-verify    with --tls, skip cert validation
  --debug                   verbose RX/TX XML logging (stderr)
  --log FILE                full debug log incl. RX/TX XML to FILE (clean UTF-8; stderr stays quiet)
  --timeout DUR             per-request timeout (default 5s — fail fast)

EXAMPLES
  dhs consumer cerebrum-nb connect       10.6.239.50
  dhs consumer cerebrum-nb listen        10.6.239.50 --user admin --pass s3cr3t
  dhs consumer cerebrum-nb list-devices  10.6.239.50:40007 --device-type Router
  dhs consumer cerebrum-nb route         10.6.239.50 --dest 60 --srce 60 --level 1
  dhs consumer cerebrum-nb route         10.6.239.50 --route 60:60:1 --route 61:61:1
  dhs consumer cerebrum-nb device-details 10.6.239.50 --port 40008 --device 10.107.30.100`)
}

// connectAndLogin: parse flags, build a Plugin, Connect. LOGIN is NOT
// performed — per EVS support 2026-04-30 it is not required to open a
// Cerebrum NB session and appears to arm a server-side 30 s timeout.
// If a future verb needs an authenticated session, call Plugin.Login()
// explicitly. The historical name is kept so the call sites read the
// same.
func connectAndLogin(args []string, verb string) (*cerebrum.Plugin, *cerebrum.Session, *cerebrumFlags, []string, error) {
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb "+verb, flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	if err := fs.Parse(args); err != nil {
		return nil, nil, nil, nil, err
	}
	p, sess, rest, err := dialCerebrum(cf, fs.Args(), verb)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return p, sess, cf, rest, nil
}

// dialCerebrum connects using ALREADY-PARSED connection flags. Verbs that
// parse their own FlagSet (which registers the shared connection flags via
// newCerebrumFlags) MUST call this with their cf + fs.Args() — handing
// fs.Args() back to connectAndLogin re-parses only the leftover positionals,
// silently dropping --port/--user/--pass/--log/--timeout (the bug that made
// every §4 write verb dial the default port).
func dialCerebrum(cf *cerebrumFlags, positionals []string, verb string) (*cerebrum.Plugin, *cerebrum.Session, []string, error) {
	rest := positionals
	if len(rest) < 1 {
		return nil, nil, nil, fmt.Errorf("cerebrum-nb %s: missing host[:port] argument", verb)
	}
	host, portArg, err := splitHostPort(rest[0], cf.port)
	if err != nil {
		return nil, nil, nil, err
	}
	cf.port = portArg

	logger, _, lerr := cf.newLogger()
	if lerr != nil {
		return nil, nil, nil, lerr
	}

	p := cerebrum.NewPlugin(logger)
	p.Username = cf.user
	p.Password = cf.pass
	p.UseTLS = cf.tls
	p.InsecureSkipVerify = cf.insecure

	scheme := "ws"
	if cf.tls {
		scheme = "wss"
	}
	fmt.Fprintf(os.Stderr, "dialing %s://%s:%d/ (timeout %s) ...\n", scheme, host, cf.port, cf.timeout)

	ctx, cancel := context.WithTimeout(context.Background(), cf.timeout)
	defer cancel()
	if err := p.Connect(ctx, host, cf.port); err != nil {
		return nil, nil, nil, err
	}
	if p.Session().LoggedIn() {
		fmt.Fprintf(os.Stderr, "connected; logged in.\n")
	} else {
		fmt.Fprintf(os.Stderr, "connected (no LOGIN — set --user/--pass for verbs that act on data).\n")
	}
	return p, p.Session(), rest[1:], nil
}

// dialCerebrumAuth is dialCerebrum + mandatory authenticated session (the
// §4 write-verb requirement — mirrors connectAndAuth for pre-parsed flags).
func dialCerebrumAuth(cf *cerebrumFlags, positionals []string, verb string) (*cerebrum.Plugin, *cerebrum.Session, []string, error) {
	p, sess, rest, err := dialCerebrum(cf, positionals, verb)
	if err != nil {
		return nil, nil, nil, err
	}
	if !sess.LoggedIn() {
		loginCtx, cancel := context.WithTimeout(context.Background(), cf.timeout)
		defer cancel()
		if err := p.Login(loginCtx); err != nil {
			_ = p.Disconnect()
			return nil, nil, nil, fmt.Errorf("cerebrum-nb %s: LOGIN required (set --user/--pass): %w", verb, err)
		}
	}
	return p, sess, rest, nil
}

// ----------------------------------------------------------------------
// Verbs
// ----------------------------------------------------------------------

// cerebrumKeepaliveProbe dials a bare WebSocket against Cerebrum (no
// LOGIN, no POLL, no app-level WS PING) and holds the connection idle
// for --idle. Per EVS support 2026-04-30: the server emits TCP-layer
// keep-alive segments during idle and the kernel auto-ACKs them; this
// verb proves both halves of that via tshark on the same wire.
//
// Sending any application traffic (LOGIN / POLL / WS PING) suppresses
// the server-side TCP keep-alive timer, so this verb deliberately stays
// silent.
func cerebrumKeepaliveProbe(_ context.Context, args []string) error {
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb keepalive-probe", flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	idle := fs.Duration("idle", 120*time.Second, "hold the connection idle for this long, then exit")
	sendLogin := fs.Bool("send-login", false, "send <LOGIN .../> after handshake then idle (isolates whether LOGIN alone arms a server-side timer)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return fmt.Errorf("cerebrum-nb keepalive-probe: missing host[:port] argument")
	}
	host, portArg, err := splitHostPort(rest[0], cf.port)
	if err != nil {
		return err
	}
	cf.port = portArg

	scheme := "ws"
	if cf.tls {
		scheme = "wss"
	}
	urlStr := fmt.Sprintf("%s://%s:%d/", scheme, host, cf.port)

	dialCtx, dialCancel := context.WithTimeout(context.Background(), cf.timeout)
	defer dialCancel()

	t0 := time.Now()
	fmt.Fprintf(os.Stderr, "[%s] dialing %s (idle %s) ...\n",
		t0.Format(time.RFC3339), urlStr, *idle)

	conn, err := ws.Dial(dialCtx, urlStr, nil)
	if err != nil {
		return fmt.Errorf("cerebrum-nb keepalive-probe: dial: %w", err)
	}
	defer func() { _ = conn.Close(1000, "client closing") }()

	tConn := time.Now()
	fmt.Fprintf(os.Stderr, "[%s] connected (laddr %s -> raddr %s, %.0fms)\n",
		tConn.Format(time.RFC3339), conn.LocalAddr(), conn.RemoteAddr(),
		float64(tConn.Sub(t0).Milliseconds()))

	// If --send-login was passed, emit a single LOGIN frame and read its
	// reply synchronously, before the RX goroutine takes over. This
	// isolates whether LOGIN alone arms a server-side session timer.
	// Use whatever the caller supplied as --user / --pass — the test
	// is to characterise server behaviour after a LOGIN attempt
	// (success or NACK), not to authenticate.
	if *sendLogin {
		loginPayload := codec.EncodeLogin(1, cf.user, cf.pass)
		fmt.Fprintf(os.Stderr, "[%s] tx %s\n",
			time.Now().Format(time.RFC3339), string(loginPayload))
		txCtx, txCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := conn.WriteText(txCtx, loginPayload); err != nil {
			txCancel()
			return fmt.Errorf("cerebrum-nb keepalive-probe: write LOGIN: %w", err)
		}
		txCancel()
		rxCtx, rxCancel := context.WithTimeout(context.Background(), 5*time.Second)
		op, reply, err := conn.ReadMessage(rxCtx)
		rxCancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%s] login reply read error: %v\n",
				time.Now().Format(time.RFC3339), err)
		} else {
			fmt.Fprintf(os.Stderr, "[%s] rx opcode=0x%x %s\n",
				time.Now().Format(time.RFC3339), op, string(reply))
		}
	}

	// Drain RX in the background so any control frames (PING/PONG) are
	// processed by ws.Conn's inline handler. Cerebrum is not expected
	// to send unsolicited app frames; whatever arrives is logged.
	rxDone := make(chan struct{})
	go func() {
		defer close(rxDone)
		for {
			op, payload, err := conn.ReadMessage(context.Background())
			if err != nil {
				if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
					fmt.Fprintf(os.Stderr, "[%s] rx error: %v\n",
						time.Now().Format(time.RFC3339), err)
				} else {
					fmt.Fprintf(os.Stderr, "[%s] rx closed: %v\n",
						time.Now().Format(time.RFC3339), err)
				}
				return
			}
			fmt.Fprintf(os.Stderr, "[%s] rx app frame opcode=0x%x len=%d\n",
				time.Now().Format(time.RFC3339), op, len(payload))
		}
	}()

	// Idle hold; periodic heartbeat to stderr so the operator sees the
	// probe is still alive without sending anything on the wire.
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	deadline := time.After(*idle)
	for {
		select {
		case <-deadline:
			fmt.Fprintf(os.Stderr, "[%s] idle deadline reached; closing\n",
				time.Now().Format(time.RFC3339))
			_ = conn.Close(1000, "client closing")
			<-rxDone
			return nil
		case t := <-tick.C:
			fmt.Fprintf(os.Stderr, "[%s] still idle (T+%s)\n",
				t.Format(time.RFC3339), t.Sub(t0).Truncate(time.Second))
		case <-rxDone:
			fmt.Fprintf(os.Stderr, "[%s] connection closed by peer or error\n",
				time.Now().Format(time.RFC3339))
			return nil
		}
	}
}

func cerebrumConnect(_ context.Context, args []string) error {
	p, sess, cf, _, err := connectAndLogin(args, "connect")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()

	pollCtx, cancel := context.WithTimeout(context.Background(), cf.timeout)
	defer cancel()
	pr, err := sess.Poll(pollCtx)
	if err != nil {
		return fmt.Errorf("cerebrum-nb: poll: %w", err)
	}
	host, port := sess.RemoteHostPort()
	fmt.Printf("connected            %s:%d\n", host, port)
	fmt.Printf("api_ver              %s\n", currentAPIVer(sess))
	fmt.Printf("connected_active     %s\n", boolFlag(pr.ConnectedServerActive))
	fmt.Printf("primary_state        %s\n", boolFlag(pr.PrimaryServerState))
	fmt.Printf("secondary_state      %s\n", boolFlag(pr.SecondaryServerState))
	return nil
}

func cerebrumListen(ctx context.Context, args []string) error {
	// --router points the ROUTING_CHANGE subscriptions at a specific
	// router device instead of the route-master sentinel — the live
	// experiment "does a crosspoint change surface per-device or only on
	// the Routemaster?".
	router, args, err := extractStringFlag(args, "--router")
	if err != nil {
		return err
	}
	if router == "" {
		router = "0.0.0.0"
	}
	p, sess, _, _, err := connectAndLogin(args, "listen")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()

	// Source-label join: ROUTE rows never carry a source name (§5.1.1), so
	// pre-fetch the source catalogue once and fill labels client-side.
	// Lenient — a NACK (e.g. per-router MNE not granted) just means events
	// print IDs only.
	srcNames := map[string]string{}
	if sess.LoggedIn() {
		if st, serr := cerebrumObtainState(ctx, sess, router, "ROUTER", 10*time.Second, cerebrumStateWant{
			SrcMne: true, Verb: "listen",
		}); serr == nil {
			for _, r := range st.Src {
				srcNames[r.ID] = r.Mnemonic
			}
			fmt.Fprintf(os.Stderr, "source labels: %d loaded for %s\n", len(srcNames), router)
		} else {
			fmt.Fprintf(os.Stderr, "source labels unavailable for %s (%v) — events print IDs only\n", router, serr)
		}
	}

	// Print every dispatched event. Wildcard-everything subscription. The
	// spurious MTID-less WILDCARD_COMPLETE the server emits after every
	// event (§1.6 deviation, live-verified) is suppressed from the display;
	// MTID-carrying completes (real end-of-snapshot markers) still print.
	sess.OnEvent(codec.KindUnknown, func(f *codec.Frame) {
		if f.Kind == codec.KindWildcardComplete && f.Root != nil && f.Root.Attr("mtid") == "" {
			return
		}
		printEventLabeled(f, srcNames)
	})

	// Submit one SUBSCRIBE per item, each with its own mtid, so a NACK
	// on one entry does not invalidate the whole transaction. Live
	// Cerebrum verified 2026-04-27: at least one ROUTING_CHANGE
	// subscribe NACKs ONE_OR_MORE_EVENTS_INVALID on this server, while
	// the snapshot subscribes (CATEGORY_LIST / GROUP_LIST / LIST)
	// succeed.
	type sub struct {
		name string
		item codec.SubItem
	}
	plan := []sub{
		// Route-master sentinel per CLAUDE.md "Routing model" + per-type
		// wildcards verified live 2026-04-29 (audit-FR §wire). Server
		// requires IP_ADDRESS="0.0.0.0" DEVICE_TYPE="ROUTER" plus the
		// row-specific ID wildcards from spec §5.1; missing them yields
		// NACK ONE_OR_MORE_EVENTS_INVALID.
		{"ROUTING_CHANGE TYPE=ROUTE", &codec.RoutingChange{
			Type: "ROUTE", IPAddress: router, DeviceType: codec.DeviceType("ROUTER"),
			DestID: "*", LevelID: "*",
		}},
		// SRCE_LOCK deliberately NOT subscribed: every live Cerebrum tested
		// (RT + per-router, 2026-04..2026-08) NACKs it, and source locks
		// are unreal in this production. Skipped with a notice below so a
		// clean run shows 0 failed — re-add the row if a server ever
		// grants it.
		{"ROUTING_CHANGE TYPE=DEST_LOCK", &codec.RoutingChange{
			Type: "DEST_LOCK", IPAddress: router, DeviceType: codec.DeviceType("ROUTER"),
			DestID: "*", LevelID: "*",
		}},
	}
	// Category / salvo / device rows exist only at Routemaster scope
	// (owner rule: no cat/salvo on a physical ROUTER — 0.0.0.0 RT only).
	// A per-router listen subscribes just the routing rows.
	if router == "0.0.0.0" {
		plan = append(plan,
			sub{"CATEGORY_CHANGE TYPE=CATEGORY_LIST", &codec.CategoryChange{Type: "CATEGORY_LIST"}},
			sub{"SALVO_CHANGE TYPE=GROUP_LIST", &codec.SalvoChange{Type: "GROUP_LIST"}},
			sub{"DEVICE_CHANGE TYPE=LIST", &codec.DeviceChange{Type: "LIST"}},
		)
	}
	var ok, fail int
	for _, p := range plan {
		// No timeout: SUBSCRIBE on a wildcard ROUTING_CHANGE can stream
		// 100k+ snapshot rows before the ACK lands. Bind to the verb's
		// own ctx so the call exits cleanly on Ctrl+C, and if the WS
		// dies the underlying read returns immediately.
		err := sess.Subscribe(ctx, []codec.SubItem{p.item})
		if err != nil {
			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				return nil // Ctrl+C mid-subscribe — clean exit
			}
			slog.Warn("subscribe failed", "plugin", "cerebrum-nb", "item", p.name, "err", err)
			fail++
			continue
		}
		ok++
	}
	fmt.Fprintf(os.Stderr, "subscribed: %d ok, %d failed (SRCE_LOCK skipped — every live Cerebrum NACKs it; re-enable if a server ever grants it); listening for events; Ctrl+C to stop\n", ok, fail)
	<-ctx.Done()
	fmt.Fprintln(os.Stderr, "listen stopped.")
	return nil
}

func cerebrumListDevices(_ context.Context, args []string) error {
	classFilter := ""
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--device-type" && i+1 < len(args) {
			classFilter = args[i+1]
			i++
			continue
		}
		filtered = append(filtered, args[i])
	}
	p, sess, _, _, err := connectAndLogin(filtered, "list-devices")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()
	return obtainAndPrintDeviceList(sess, classFilter)
}

// cerebrumDeviceDetails issues an OBTAIN of DEVICE_CHANGE TYPE=DETAILS for a
// single device and prints the response. Address by IP (default) or name
// per spec §1.7. The wire shape of the DETAILS response is unconfirmed;
// first run captures the raw XML so we can refine the structured printer.
func cerebrumDeviceDetails(_ context.Context, args []string) error {
	device, deviceType, byName, rest, err := extractDeviceDetailsFlags(args)
	if err != nil {
		return err
	}
	if device == "" {
		return fmt.Errorf("cerebrum-nb device-details: --device IP|NAME is required")
	}

	p, sess, cf, _, err := connectAndLogin(rest, "device-details")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()

	var got *codec.Frame
	done := make(chan struct{})
	var once sync.Once
	signalDone := func() { once.Do(func() { close(done) }) }
	timer := time.AfterFunc(cf.timeout, signalDone)

	sess.OnEvent(codec.KindDeviceChange, func(f *codec.Frame) {
		if f.Device != nil && f.Device.Type == "DETAILS" {
			got = f
			signalDone()
		}
	})

	dc := &codec.DeviceChange{Type: "DETAILS"}
	if byName {
		dc.DeviceName = device
	} else {
		dc.IPAddress = device
	}
	if deviceType != "" {
		// Wire-actual: the server is case-sensitive on DEVICE_TYPE values
		// and accepts the UPPERCASE forms only (live 2026-08-16: "Router"
		// NACKs 10, "ROUTER" answers) — normalize so operators can type
		// either.
		dc.DeviceType = codec.DeviceType(strings.ToUpper(deviceType))
	}
	obCtx, cancel := context.WithTimeout(context.Background(), cf.timeout)
	defer cancel()
	if err := sess.Obtain(obCtx, []codec.SubItem{dc}); err != nil {
		return err
	}
	<-done
	timer.Stop()

	if got == nil {
		fmt.Fprintln(os.Stderr, "(no DEVICE_CHANGE TYPE=DETAILS response within", cf.timeout, ")")
		return nil
	}

	d := got.Device
	fmt.Printf("device       %s\n", d.IPAddress)
	fmt.Printf("device_type  %s\n", d.DeviceType)
	if d.Details != nil {
		if d.Details.Name != "" {
			fmt.Printf("name         %s\n", d.Details.Name)
		}
		if d.Details.VendorType != "" {
			fmt.Printf("vendor       %s\n", d.Details.VendorType)
		}
		if d.Details.IP1 != "" || d.Details.IP2 != "" {
			fmt.Printf("control      ip1=%s ip2=%s\n", d.Details.IP1, displayDash(d.Details.IP2))
		}
	}
	if d.Service != nil && (d.Service.IP1 != "" || d.Service.IP2 != "") {
		fmt.Printf("service      ip1=%s ip2=%s\n", d.Service.IP1, displayDash(d.Service.IP2))
	}
	if d.Connection != nil {
		fmt.Printf("connection   primary=%q secondary=%q\n", d.Connection.PrimaryState, d.Connection.SecondaryState)
	}
	if d.SubDevice != "" {
		fmt.Printf("sub_device   %s\n", d.SubDevice)
	}
	if d.Object != "" {
		fmt.Printf("object       %s\n", d.Object)
	}
	if len(d.SubDevices) > 0 {
		fmt.Printf("sub_devices  %d\n", len(d.SubDevices))
		for _, e := range d.SubDevices {
			if e.PrimaryState != "" || e.SecondaryState != "" {
				// Positional DEVICE_N shape (live NOC): index + model + states.
				fmt.Printf("  %02d  %-20s primary=%q secondary=%q\n", e.Index, displayName(e.DeviceName), e.PrimaryState, e.SecondaryState)
				continue
			}
			fmt.Printf("  %-12s %-20s %s\n", e.DeviceType, displayName(e.DeviceName), e.IPAddress)
		}
	}
	return nil
}

func displayDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// cerebrumDeviceValue issues OBTAIN DEVICE_CHANGE TYPE=VALUE for one
// (device, sub_device, object) tuple. Per spec §5.4 VALUE addresses by
// device_name + sub_device + object. Wire shape unconfirmed; raw XML
// is always dumped so we can refine the decoder on first capture.
func cerebrumDeviceValue(_ context.Context, args []string) error {
	device, _, byName, rest, err := extractDeviceDetailsFlags(args)
	if err != nil {
		return err
	}
	subDev, rest, err := extractStringFlag(rest, "--sub-device")
	if err != nil {
		return err
	}
	object, rest, err := extractStringFlag(rest, "--object")
	if err != nil {
		return err
	}
	if device == "" {
		return fmt.Errorf("cerebrum-nb device-value: --device NAME is required (use --by-name)")
	}
	if subDev == "" || object == "" {
		return fmt.Errorf("cerebrum-nb device-value: --sub-device and --object are both required (use --object \".\" for the ROOT group listing)")
	}
	// Group paths return their CHILDREN (live 2026-08-16: a §5.4.3 obtain
	// on "PROCESSING AUDIO" listed sub-groups as available=0 rows and leaf
	// values inline) — "." is the CLI sentinel for the root listing.
	if object == "." {
		object = ""
	}

	p, sess, cf, _, err := connectAndLogin(rest, "device-value")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()

	dc := &codec.DeviceChange{Type: "VALUE", SubDevice: subDev, Object: object}
	if byName {
		dc.DeviceName = device
	} else {
		dc.IPAddress = device
	}
	got, err := obtainSingleDeviceChange(sess, cf.timeout, dc, "VALUE")
	if err != nil {
		return err
	}
	if got == nil || got.Device == nil {
		fmt.Fprintln(os.Stderr, "(no DEVICE_CHANGE TYPE=VALUE response within timeout)")
		return nil
	}
	d := got.Device
	fmt.Printf("device      %s (%s)\n", d.IPAddress, displayName(d.DeviceName))
	fmt.Printf("sub_device  %s\n", d.SubDevice)
	fmt.Printf("object      %s\n", d.Object)
	// Print EVERY OBJECT_VALUE child with the full 0v16 descriptor — a
	// wildcard OBJECT/SUB_DEVICE obtain may return many.
	if n := len(d.ObjectValues); n > 1 {
		fmt.Printf("values      %d\n", n)
	}
	for _, ov := range d.ObjectValues {
		line := fmt.Sprintf("  %-40s available=%s", ov.Object, boolFlag(ov.Available))
		if ov.Value != "" {
			line += fmt.Sprintf(" value=%q", ov.Value)
		}
		if ov.DataType != "" {
			line += " type=" + ov.DataType
		}
		if ov.Readable || ov.Writable {
			line += fmt.Sprintf(" rw=%s%s", boolFlag(ov.Readable), boolFlag(ov.Writable))
		}
		if ov.Units != "" {
			line += " units=" + ov.Units
		}
		if ov.Label != "" {
			line += fmt.Sprintf(" label=%q", ov.Label)
		}
		if (ov.Min != "" || ov.Max != "") && ov.Min != ov.Max {
			line += fmt.Sprintf(" range=%s..%s", ov.Min, ov.Max)
		}
		if ov.Step != "" && ov.Step != "0.000000" {
			line += " step=" + ov.Step
		}
		if ov.Default != "" {
			line += " default=" + ov.Default
		}
		if len(ov.EnumList) > 0 {
			line += fmt.Sprintf(" enum=%s", strings.Join(ov.EnumList, "|"))
		}
		fmt.Println(line)
	}
	return nil
}

// cerebrumListCategories issues OBTAIN CATEGORY_CHANGE TYPE=CATEGORY_LIST.
// Wire shape known (CATEGORY child with comma-separated LIST attr).
func cerebrumListCategories(_ context.Context, args []string) error {
	p, sess, cf, _, err := connectAndLogin(args, "list-categories")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()
	got, err := obtainSingleCategoryChange(sess, cf.timeout, &codec.CategoryChange{Type: "CATEGORY_LIST"}, "CATEGORY_LIST")
	if err != nil {
		return err
	}
	if got == nil || got.Category == nil {
		return nil
	}
	fmt.Printf("count %d\n", len(got.Category.Categories))
	for _, c := range got.Category.Categories {
		fmt.Println(c)
	}
	return nil
}

// cerebrumCategoryDetails issues OBTAIN CATEGORY_CHANGE TYPE=CATEGORY_DETAILS
// for one category. Wire shape unconfirmed; raw XML dumped.
func cerebrumCategoryDetails(_ context.Context, args []string) error {
	cat, rest, err := extractStringFlag(args, "--category")
	if err != nil {
		return err
	}
	if cat == "" {
		return fmt.Errorf("cerebrum-nb category-details: --category NAME is required")
	}
	p, sess, cf, _, err := connectAndLogin(rest, "category-details")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()
	got, err := obtainSingleCategoryChange(sess, cf.timeout,
		&codec.CategoryChange{Type: "CATEGORY_DETAILS", Category: cat},
		"CATEGORY_DETAILS")
	if err != nil {
		return err
	}
	if got == nil || got.Category == nil {
		fmt.Fprintln(os.Stderr, "(no CATEGORY_CHANGE TYPE=CATEGORY_DETAILS response within timeout)")
		return nil
	}
	c := got.Category
	fmt.Printf("category     %s\n", c.Category)
	if c.Details != nil {
		fmt.Printf("label        %s\n", displayDash(c.Details.Label))
		fmt.Printf("available    %s\n", boolFlag(c.Details.Available))
		if c.Details.Description != "" {
			fmt.Printf("description  %s\n", c.Details.Description)
		}
		if len(c.Details.Items) > 0 {
			fmt.Printf("items        %d (BLANK slots dropped)\n", len(c.Details.Items))
			for _, it := range c.Details.Items {
				fmt.Printf("  %3d  %-12s %s\n", it.Index, it.Type, it.Value)
			}
		}
	}
	return nil
}

// cerebrumListSalvoGroups issues OBTAIN SALVO_CHANGE TYPE=GROUP_LIST.
// Wire shape known (GROUPS child with comma-separated LIST attr).
func cerebrumListSalvoGroups(_ context.Context, args []string) error {
	p, sess, cf, _, err := connectAndLogin(args, "list-salvo-groups")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()
	got, err := obtainSingleSalvoChange(sess, cf.timeout, &codec.SalvoChange{Type: "GROUP_LIST"}, "GROUP_LIST")
	if err != nil {
		return err
	}
	if got == nil || got.Salvo == nil {
		return nil
	}
	fmt.Printf("count %d\n", len(got.Salvo.Groups))
	for _, g := range got.Salvo.Groups {
		fmt.Println(g)
	}
	return nil
}

// cerebrumListSalvoInstances issues OBTAIN SALVO_CHANGE TYPE=INSTANCE_LIST.
// Wire shape unconfirmed; raw XML dumped.
func cerebrumListSalvoInstances(_ context.Context, args []string) error {
	group, rest, err := extractStringFlag(args, "--group")
	if err != nil {
		return err
	}
	if group == "" {
		return fmt.Errorf("cerebrum-nb list-salvo-instances: --group NAME is required")
	}
	p, sess, cf, _, err := connectAndLogin(rest, "list-salvo-instances")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()
	got, err := obtainSingleSalvoChange(sess, cf.timeout,
		&codec.SalvoChange{Type: "INSTANCE_LIST", Group: group},
		"INSTANCE_LIST")
	if err != nil {
		return err
	}
	if got == nil || got.Salvo == nil {
		fmt.Fprintln(os.Stderr, "(no SALVO_CHANGE TYPE=INSTANCE_LIST response within timeout)")
		return nil
	}
	fmt.Printf("group       %s\n", got.Salvo.Group)
	fmt.Printf("count       %d\n", len(got.Salvo.Instances))
	for _, ins := range got.Salvo.Instances {
		fmt.Println(ins)
	}
	return nil
}

// cerebrumSalvoInstanceDetails issues OBTAIN SALVO_CHANGE TYPE=INSTANCE_DETAILS.
// Wire shape unconfirmed; raw XML dumped.
func cerebrumSalvoInstanceDetails(_ context.Context, args []string) error {
	group, rest, err := extractStringFlag(args, "--group")
	if err != nil {
		return err
	}
	instance, rest, err := extractStringFlag(rest, "--instance")
	if err != nil {
		return err
	}
	if group == "" || instance == "" {
		return fmt.Errorf("cerebrum-nb salvo-instance-details: --group and --instance are both required")
	}
	p, sess, cf, _, err := connectAndLogin(rest, "salvo-instance-details")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()
	got, err := obtainSingleSalvoChange(sess, cf.timeout,
		&codec.SalvoChange{Type: "INSTANCE_DETAILS", Group: group, Instance: instance},
		"INSTANCE_DETAILS")
	if err != nil {
		return err
	}
	if got == nil || got.Salvo == nil {
		fmt.Fprintln(os.Stderr, "(no SALVO_CHANGE TYPE=INSTANCE_DETAILS response within timeout)")
		return nil
	}
	s := got.Salvo
	fmt.Printf("group       %s\n", s.Group)
	fmt.Printf("instance    %s\n", s.Instance)
	if d := s.InstanceDetails; d != nil {
		fmt.Printf("available   %s\n", boolFlag(d.Available))
		fmt.Printf("active      %s\n", boolFlag(d.Active))
		if d.Description != "" {
			fmt.Printf("description %s\n", d.Description)
		}
		if d.Date != "" || d.Time != "" {
			fmt.Printf("saved       %s %s\n", d.Date, d.Time)
		}
	}
	return nil
}

// ----------------------------------------------------------------------
// helpers shared by every single-OBTAIN verb
// ----------------------------------------------------------------------

func obtainSingleDeviceChange(sess *cerebrum.Session, timeout time.Duration, item *codec.DeviceChange, wantType string) (*codec.Frame, error) {
	return obtainOneEvent(sess, timeout, codec.KindDeviceChange, item, func(f *codec.Frame) bool {
		return f.Device != nil && f.Device.Type == wantType
	})
}

func obtainSingleCategoryChange(sess *cerebrum.Session, timeout time.Duration, item *codec.CategoryChange, wantType string) (*codec.Frame, error) {
	return obtainOneEvent(sess, timeout, codec.KindCategoryChange, item, func(f *codec.Frame) bool {
		return f.Category != nil && f.Category.Type == wantType
	})
}

func obtainSingleSalvoChange(sess *cerebrum.Session, timeout time.Duration, item *codec.SalvoChange, wantType string) (*codec.Frame, error) {
	return obtainOneEvent(sess, timeout, codec.KindSalvoChange, item, func(f *codec.Frame) bool {
		return f.Salvo != nil && f.Salvo.Type == wantType
	})
}

// obtainOneEvent issues a one-shot OBTAIN with the given SubItem and
// blocks until either the matching event arrives or timeout fires.
func obtainOneEvent(sess *cerebrum.Session, timeout time.Duration, kind codec.FrameKind, item codec.SubItem, match func(*codec.Frame) bool) (*codec.Frame, error) {
	var got *codec.Frame
	done := make(chan struct{})
	var once sync.Once
	signal := func() { once.Do(func() { close(done) }) }
	timer := time.AfterFunc(timeout, signal)
	defer timer.Stop()

	sess.OnEvent(kind, func(f *codec.Frame) {
		if match(f) {
			got = f
			signal()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := sess.Obtain(ctx, []codec.SubItem{item}); err != nil {
		return nil, err
	}
	<-done
	return got, nil
}

// extractStringFlag pulls --name VALUE out of args. Returns the value
// and the args minus that flag pair. Unknown flags pass through
// untouched so connectAndLogin can consume them.
func extractStringFlag(args []string, name string) (string, []string, error) {
	rest := make([]string, 0, len(args))
	var val string
	for i := 0; i < len(args); i++ {
		if args[i] == name {
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("%s needs a value", name)
			}
			val = args[i+1]
			i++
			continue
		}
		rest = append(rest, args[i])
	}
	return val, rest, nil
}

// extractDeviceDetailsFlags splits the device-details argv into the
// verb-specific flags (--device, --by-name) and the remainder consumed
// by connectAndLogin. We don't extend cerebrumFlags because these
// flags only apply to this one verb.
func extractDeviceDetailsFlags(args []string) (device, deviceType string, byName bool, rest []string, err error) {
	rest = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--device":
			if i+1 >= len(args) {
				return "", "", false, nil, fmt.Errorf("--device needs a value")
			}
			device = args[i+1]
			i++
		case "--device-type":
			if i+1 >= len(args) {
				return "", "", false, nil, fmt.Errorf("--device-type needs a value")
			}
			deviceType = args[i+1]
			i++
		case "--by-name":
			byName = true
		default:
			rest = append(rest, args[i])
		}
	}
	return device, deviceType, byName, rest, nil
}

// ----------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------

func obtainAndPrintDeviceList(sess *cerebrum.Session, deviceTypeFilter string) error {
	var entries []codec.DeviceEntry
	var snapshotEntries int
	var snapshotTypes []string
	done := make(chan struct{})
	var once sync.Once
	signalDone := func() { once.Do(func() { close(done) }) }
	timer := time.AfterFunc(15*time.Second, signalDone)

	sess.OnEvent(codec.KindDeviceChange, func(f *codec.Frame) {
		if f.Device == nil || f.Device.Type != "LIST" {
			return
		}
		// Each <DEVICE> may carry multiple <INSTANCE DEVICE_TYPE=…/>
		// children (one per class — Device / Router / SNMP). Flatten
		// to one row per (IP × class).
		seen := map[string]bool{}
		for _, e := range f.Device.Devices {
			classes := e.DeviceTypes
			if len(classes) == 0 {
				classes = []codec.DeviceType{e.DeviceType}
			}
			for _, t := range classes {
				snapshotEntries++
				ts := string(t)
				if !seen[ts] {
					seen[ts] = true
					snapshotTypes = append(snapshotTypes, ts)
				}
				if deviceTypeFilter != "" && !strings.EqualFold(ts, deviceTypeFilter) {
					continue
				}
				entries = append(entries, codec.DeviceEntry{
					IPAddress:  e.IPAddress,
					DeviceType: t,
					DeviceName: e.DeviceName,
				})
			}
		}
		// Server returns the whole list in one frame — the snapshot
		// arrives in a single event, so we can finish as soon as we
		// have it.
		signalDone()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	items := []codec.SubItem{&codec.DeviceChange{Type: "LIST", DeviceType: codec.DeviceType(deviceTypeFilter)}}
	if err := sess.Obtain(ctx, items); err != nil {
		return err
	}
	<-done
	timer.Stop()

	fmt.Printf("%-10s  %s\n", "DEVICE_TYPE", "IP_ADDRESS")
	// Prepend the route-master sentinel (`0.0.0.0/ROUTER`) — central
	// addressing target per spec §4.1, present on every Cerebrum,
	// never returned in the wire LIST. Always show it in the unfiltered
	// output and when filtering by ROUTER class; suppress for Device /
	// SNMP filters.
	if deviceTypeFilter == "" || strings.EqualFold(deviceTypeFilter, "Router") {
		fmt.Printf("%-10s  %s\n", "ROUTER", "0.0.0.0")
	}
	for _, d := range entries {
		fmt.Printf("%-10s  %s\n", d.DeviceType, d.IPAddress)
	}
	if len(entries) == 0 {
		switch {
		case snapshotEntries == 0:
			fmt.Fprintln(os.Stderr, "(server returned no DEVICE entries within 15s — check connectivity / licence)")
		case deviceTypeFilter != "":
			fmt.Fprintf(os.Stderr,
				"(snapshot has %d entries but none of DEVICE_TYPE=%q; types seen: %s)\n",
				snapshotEntries, deviceTypeFilter, strings.Join(snapshotTypes, ", "))
		default:
			fmt.Fprintf(os.Stderr, "(snapshot has %d entries — none matched the filter)\n", snapshotEntries)
		}
	}
	return nil
}

// cerebrumWatch subscribes to one device's changes — the DEVICE-class
// analogue of `listen --router` (§2.4: SUBSCRIBE takes the same §5.4 rows):
//
//	watch --device IP [--device-type DEVICE|ROUTER|SNMP]
//	    DETAILS subscribe — connection / sub-device state changes
//	watch --device NAME --by-name --sub-device S --object O
//	    VALUE subscribe — one object's value changes (object paths must be
//	    known a priori; wildcards are refused on this row, live-verified)
func cerebrumWatch(ctx context.Context, args []string) error {
	device, deviceType, byName, rest, err := extractDeviceDetailsFlags(args)
	if err != nil {
		return err
	}
	subDev, rest, err := extractStringFlag(rest, "--sub-device")
	if err != nil {
		return err
	}
	object, rest, err := extractStringFlag(rest, "--object")
	if err != nil {
		return err
	}
	if device == "" {
		return fmt.Errorf("cerebrum-nb watch: --device IP|NAME is required")
	}
	if (subDev == "") != (object == "") {
		return fmt.Errorf("cerebrum-nb watch: --sub-device and --object go together (both = VALUE watch, neither = DETAILS watch; --object \".\" = root group)")
	}
	if object == "." {
		object = "" // root-group sentinel, same as device-value
	}

	args = reorderFlagsFirst(rest)
	fs := flag.NewFlagSet("cerebrum-nb watch", flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	p, sess, _, err := dialCerebrumAuth(cf, fs.Args(), "watch")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()

	dc := &codec.DeviceChange{}
	if subDev != "" {
		dc.Type = "VALUE"
		dc.SubDevice = subDev
		dc.Object = object
		if byName {
			dc.DeviceName = device
		} else {
			dc.IPAddress = device
		}
	} else {
		// DETAILS addressing is IP-only (by-name NACKs — spec-conform).
		dc.Type = "DETAILS"
		dc.IPAddress = device
		if deviceType == "" {
			deviceType = "DEVICE"
		}
	}
	if deviceType != "" {
		dc.DeviceType = codec.DeviceType(strings.ToUpper(deviceType))
	}

	sess.OnEvent(codec.KindUnknown, func(f *codec.Frame) {
		switch {
		case f.Kind == codec.KindWildcardComplete && f.Root != nil && f.Root.Attr("mtid") == "":
			return // spurious §1.6 deviation
		case f.Kind == codec.KindAck:
			return // transaction plumbing, not an event
		}
		printEventLabeled(f, nil)
	})
	if err := sess.Subscribe(ctx, []codec.SubItem{dc}); err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return nil // Ctrl+C during subscribe — clean exit, not an error
		}
		return fmt.Errorf("cerebrum-nb watch: subscribe %s: %w", dc.Type, err)
	}
	fmt.Fprintf(os.Stderr, "watching DEVICE_CHANGE TYPE=%s on %s — Ctrl+C to stop\n", dc.Type, device)
	<-ctx.Done()
	fmt.Fprintln(os.Stderr, "watch stopped.")
	return nil
}

// printEventLabeled renders one dispatched frame; srcNames (optional) joins
// source labels by ID — the wire never carries a SOURCE_NAME on ROUTE rows
// (§5.1.1), so listen pre-fetches the catalogue and fills them client-side.
func printEventLabeled(f *codec.Frame, srcNames map[string]string) {
	switch f.Kind {
	case codec.KindRoutingChange:
		rc := f.Routing
		// ROUTE rows carry the source in the <route source_id source_level_id/>
		// child element (RouteSourceID / RouteSourceLevelID), not the
		// top-level srce_id attribute. SRCE_LOCK / DEST_LOCK / *_MNE rows
		// keep the source on the top-level attrs as before.
		srceID, srceName := rc.SrceID, rc.SrceName
		rawID := rc.SrceID
		if rc.Type == "ROUTE" && rc.RouteSourceID != "" {
			rawID = rc.RouteSourceID
			srceID = rc.RouteSourceID
			if rc.RouteSourceLevelID != "" && rc.RouteSourceLevelID != rc.LevelID {
				srceID = fmt.Sprintf("%s@lvl%s", rc.RouteSourceID, rc.RouteSourceLevelID)
			}
		}
		if srceName == "" && srcNames != nil {
			srceName = srcNames[rawID]
		}
		line := fmt.Sprintf("[routing] %-8s dev=%s/%s srce=%s(%s) dest=%s(%s) lvl=%s(%s)",
			rc.Type, rc.DeviceType, rc.DeviceName,
			srceID, srceName, rc.DestID, rc.DestName,
			rc.LevelID, rc.LevelName)
		if rc.Lock != nil {
			line += fmt.Sprintf(" state=%s by=%q", rc.Lock.LockState, rc.Lock.LockedBy)
		}
		fmt.Println(line)
	case codec.KindCategoryChange:
		if f.Category.Type == "CATEGORY_LIST" {
			fmt.Printf("[category] CATEGORY_LIST count=%d %s\n", len(f.Category.Categories), summarise(f.Category.Categories))
		} else {
			fmt.Printf("[category] %s %s\n", f.Category.Type, f.Category.Category)
		}
	case codec.KindSalvoChange:
		if f.Salvo.Type == "GROUP_LIST" {
			fmt.Printf("[salvo] GROUP_LIST count=%d %s\n", len(f.Salvo.Groups), summarise(f.Salvo.Groups))
		} else {
			fmt.Printf("[salvo] %s group=%s inst=%s\n", f.Salvo.Type, f.Salvo.Group, f.Salvo.Instance)
		}
	case codec.KindDeviceChange:
		if f.Device.Type == "LIST" {
			fmt.Printf("[device] LIST count=%d\n", len(f.Device.Devices))
			for _, d := range f.Device.Devices {
				fmt.Printf("           %-10s %-20s %s\n", d.DeviceType, displayName(d.DeviceName), d.IPAddress)
			}
		} else {
			// Only print the attrs the row kind actually carries: SUB_DEVICE/
			// OBJECT belong to VALUE rows; on DETAILS the name lives in the
			// <DETAILS> child, not the outer attribute.
			name := f.Device.DeviceName
			if name == "" && f.Device.Details != nil {
				name = f.Device.Details.Name
			}
			line := fmt.Sprintf("[device] %-8s type=%s", f.Device.Type, f.Device.DeviceType)
			if name != "" {
				line += " name=" + name
			}
			if f.Device.IPAddress != "" {
				line += " ip=" + f.Device.IPAddress
			}
			if f.Device.SubDevice != "" {
				line += " sub=" + f.Device.SubDevice
			}
			if f.Device.Object != "" {
				line += " obj=" + f.Device.Object
			}
			fmt.Println(line)
			for _, ov := range f.Device.ObjectValues {
				fmt.Printf("           %-40s available=%s value=%q\n", ov.Object, boolFlag(ov.Available), ov.Value)
			}
			if c := f.Device.Connection; c != nil {
				fmt.Printf("           connection primary=%q secondary=%q\n", c.PrimaryState, c.SecondaryState)
			}
			for _, sd := range f.Device.SubDevices {
				if sd.PrimaryState != "" || sd.SecondaryState != "" {
					fmt.Printf("           sub %02d %-20s primary=%q secondary=%q\n", sd.Index, displayName(sd.DeviceName), sd.PrimaryState, sd.SecondaryState)
				}
			}
		}
	case codec.KindDatastoreChange:
		fmt.Printf("[datastore] %s type=%s\n", f.Datastore.Name, f.Datastore.Type)
	default:
		fmt.Printf("[%s] %s\n", f.Kind, f.Root.String())
	}
}

// summarise renders the first 3 entries of a list inline; longer lists
// get an ellipsis. Keeps the listen Info column readable when a server
// returns 80+ categories.
func summarise(items []string) string {
	if len(items) == 0 {
		return ""
	}
	if len(items) <= 3 {
		return "[" + strings.Join(items, ", ") + "]"
	}
	return "[" + strings.Join(items[:3], ", ") + ", ...]"
}

func displayName(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func boolFlag(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func currentAPIVer(sess *cerebrum.Session) string {
	v := sess.APIVersion()
	if v == "" {
		return "(unknown)"
	}
	return v
}

// ----------------------------------------------------------------------
// route — issue one or more <action><routing TYPE='ROUTE'/></action>
// per Ember+ Cerebrum Northbound API spec §4.1.1.
// ----------------------------------------------------------------------

// routeSpec is one (dest, srce, level) triple parsed from --route or --csv.
type routeSpec struct{ Dest, Srce, Level string }

// parseRouteFlag parses "dst:src:lvl" into a routeSpec.
func parseRouteFlag(s string) (routeSpec, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return routeSpec{}, fmt.Errorf("--route %q: expected dst:src:lvl", s)
	}
	return routeSpec{Dest: parts[0], Srce: parts[1], Level: parts[2]}, nil
}

// readRoutesCSV parses a CSV file with header dest,srce,level (any order; case-insensitive).
func readRoutesCSV(path string) ([]routeSpec, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		return nil, fmt.Errorf("--csv %s: empty", path)
	}
	header := strings.Split(strings.ToLower(strings.TrimSpace(lines[0])), ",")
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.TrimSpace(h)] = i
	}
	for _, k := range []string{"dest", "srce", "level"} {
		if _, ok := idx[k]; !ok {
			return nil, fmt.Errorf("--csv %s: missing column %q (need dest,srce,level)", path, k)
		}
	}
	out := []routeSpec{}
	for n, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, ",")
		if len(f) < len(header) {
			return nil, fmt.Errorf("--csv %s line %d: %d cols < %d", path, n+2, len(f), len(header))
		}
		out = append(out, routeSpec{
			Dest:  strings.TrimSpace(f[idx["dest"]]),
			Srce:  strings.TrimSpace(f[idx["srce"]]),
			Level: strings.TrimSpace(f[idx["level"]]),
		})
	}
	return out, nil
}

// stringSliceFlag captures repeatable string flags like --route a --route b.
type stringSliceFlag []string

func (s *stringSliceFlag) String() string     { return strings.Join(*s, ",") }
func (s *stringSliceFlag) Set(v string) error { *s = append(*s, v); return nil }

func cerebrumRoute(_ context.Context, args []string) error {
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb route", flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	router := fs.String("router", "0.0.0.0", "router target — IP_ADDRESS for the route-master sentinel (0.0.0.0) or a physical Router IP")
	deviceName := fs.String("device-name", "", "alternative to --router: address by DEVICE_NAME")
	dest := fs.String("dest", "", "destination ID (single-route mode)")
	srce := fs.String("srce", "", "source ID (single-route mode)")
	level := fs.String("level", "", "level ID (single-route mode)")
	var routesFlag stringSliceFlag
	fs.Var(&routesFlag, "route", "repeatable: dst:src:lvl (use multiple --route to batch)")
	csv := fs.String("csv", "", "CSV file with columns dest,srce,level")
	check := fs.Bool("check", false, "dry-run (ADR-0007): read live routes, report would_change, send nothing")
	output := fs.String("output", "text", "output format: text | json (ADR-0002; json = {changed|would_change, diff[]})")
	if err := fs.Parse(args); err != nil {
		return err
	}
	jsonOut, oerr := resolveEnsureOutput(*output, false)
	if oerr != nil {
		return oerr
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return fmt.Errorf("cerebrum-nb route: missing host[:port] argument")
	}

	// Build the route list from flags.
	var routes []routeSpec
	if *dest != "" || *srce != "" || *level != "" {
		if *dest == "" || *srce == "" || *level == "" {
			return fmt.Errorf("--dest/--srce/--level: all three required for single-route mode")
		}
		routes = append(routes, routeSpec{Dest: *dest, Srce: *srce, Level: *level})
	}
	for _, r := range routesFlag {
		rs, err := parseRouteFlag(r)
		if err != nil {
			return err
		}
		routes = append(routes, rs)
	}
	if *csv != "" {
		rs, err := readRoutesCSV(*csv)
		if err != nil {
			return err
		}
		routes = append(routes, rs...)
	}
	if len(routes) == 0 {
		return fmt.Errorf("cerebrum-nb route: no routes specified (use --dest/--srce/--level, --route, or --csv)")
	}

	host, portArg, err := splitHostPort(rest[0], cf.port)
	if err != nil {
		return err
	}
	cf.port = portArg

	logger, _, lerr := cf.newLogger()
	if lerr != nil {
		return lerr
	}

	p := cerebrum.NewPlugin(logger)
	p.Username = cf.user
	p.Password = cf.pass
	p.UseTLS = cf.tls
	p.InsecureSkipVerify = cf.insecure

	dialCtx, dialCancel := context.WithTimeout(context.Background(), cf.timeout)
	defer dialCancel()
	if err := p.Connect(dialCtx, host, cf.port); err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()
	sess := p.Session()
	logw := os.Stdout
	if jsonOut {
		logw = os.Stderr
	}

	// ADR-0007 (amendment): matrix connections MUST converge — read the live
	// crosspoint state, diff, send only the differences (probel tally-first
	// template). Never disconnects; identical cells cost no wire write.
	st, serr := cerebrumObtainState(context.Background(), sess, *router, "ROUTER", 15*time.Second, cerebrumStateWant{
		Routes: true, Verb: "route",
	})
	if serr != nil {
		return serr
	}
	changes := diffCerebrumRoutes(st.Routes, routes)
	diffs := make([]ensureDiff, 0, len(changes))
	for _, c := range changes {
		diffs = append(diffs, ensureDiff{Field: fmt.Sprintf("route.%s.%s", c.Dest, c.Level), From: c.From, To: c.To})
	}
	changed := len(changes) > 0

	if *check {
		for _, c := range changes {
			_, _ = fmt.Fprintf(logw, "[would-route] dst=%s lvl=%s: %q -> src %s\n", c.Dest, c.Level, c.From, c.To)
		}
		_, _ = fmt.Fprintf(logw, "cerebrum-nb route --check: would_change=%d of %d desired — nothing sent\n", len(changes), len(routes))
		if jsonOut {
			return emitEnsure(true, ensureResult{WouldChange: &changed, Diff: diffs})
		}
		return nil
	}

	fails := 0
	for _, c := range changes {
		body := &codec.RoutingAction{
			Type:       "ROUTE",
			IPAddress:  *router,
			DeviceName: *deviceName,
			DeviceType: codec.DeviceType("ROUTER"),
			DestID:     c.Dest,
			SrceID:     c.To,
			LevelID:    c.Level,
		}
		if err := sess.Action(context.Background(), body); err != nil {
			_, _ = fmt.Fprintf(logw, "[route] NACK dst=%s src=%s lvl=%s reason=%s\n", c.Dest, c.To, c.Level, err)
			fails++
			continue
		}
		_, _ = fmt.Fprintf(logw, "[route] OK   dst=%s lvl=%s: %q -> src %s\n", c.Dest, c.Level, c.From, c.To)
	}
	if fails > 0 {
		return fmt.Errorf("%d/%d route change(s) failed", fails, len(changes))
	}
	_, _ = fmt.Fprintf(logw, "cerebrum-nb route: changed=%d of %d desired (already-converged cells untouched); run again to verify 0\n", len(changes), len(routes))
	if jsonOut {
		return emitEnsure(true, ensureResult{Changed: &changed, Diff: diffs})
	}
	return nil
}

// ----------------------------------------------------------------------
// §4 write verbs — ACTION round-trips (ACK/NACK). These wrap the Session
// action methods; each maps to a codec action encoder that exists today:
//   lock / unlock / set-mnemonic / set-tags → codec.RoutingAction (§4.1)
//   salvo                                    → codec.SalvoAction    (§4.3)
//   category                                 → codec.CategoryAction (§4.2)
//   set-value                                → codec.DeviceAction   (§4.4)
//   obtain-datastore                         → codec.DatastoreChange (§5.5)
//
// All require an authenticated session (the server NACKs NOT_LOGGED_IN
// otherwise), so they LOGIN after connect using --user/--pass (or
// $DHS_CEREBRUM_USER / $DHS_CEREBRUM_PASS) before sending the action.
// ----------------------------------------------------------------------

// routeTargetFromFlags builds the router-addressing tuple. Defaults to the
// route-master sentinel (0.0.0.0 / ROUTER) unless --router / --device-name
// override it.
func routeTargetFromFlags(router, deviceName string) cerebrum.RouteTarget {
	t := cerebrum.DefaultRouteTarget()
	if router != "" {
		t.IPAddress = router
	}
	if deviceName != "" {
		t.IPAddress = ""
		t.DeviceName = deviceName
	}
	return t
}

func cerebrumLock(_ context.Context, args []string, mode codec.LockKind) error {
	verb := "lock"
	if mode == codec.LockRelease || mode == codec.LockReleased {
		verb = "unlock"
	}
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb "+verb, flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	kind := fs.String("kind", "DEST_LOCK", "lock kind: SRCE_LOCK | DEST_LOCK")
	router := fs.String("router", "0.0.0.0", "router IP target (route-master sentinel 0.0.0.0)")
	deviceName := fs.String("device-name", "", "address by DEVICE_NAME instead of --router")
	srce := fs.String("srce", "", "source ID (SRCE_LOCK)")
	dest := fs.String("dest", "", "destination ID (DEST_LOCK)")
	level := fs.String("level", "", "level ID")
	duration := fs.String("duration", "", "optional timed-lock duration")
	// --mode (0v16 §3.2): override the default PROTECT/RELEASE verb with one
	// of the five canonical LOCK_STATE values. Empty keeps the verb default.
	modeFlag := fs.String("mode", "", "0v16 lock mode: locked | protected | locked_path | protected_path | released (overrides the default "+verb+" verb)")
	check := fs.Bool("check", false, "dry-run (ADR-0007): read the live lock state, report would_change, send nothing")
	output := fs.String("output", "text", "output format: text | json (ADR-0002; json = {changed|would_change, diff[]})")
	if err := fs.Parse(args); err != nil {
		return err
	}
	jsonOut, oerr := resolveEnsureOutput(*output, false)
	if oerr != nil {
		return oerr
	}
	if err := requireKind(*kind, verb, "SRCE_LOCK", "DEST_LOCK"); err != nil {
		return err
	}
	if *modeFlag != "" {
		m, err := lockModeValue(*modeFlag)
		if err != nil {
			return err
		}
		mode = m
	}
	if *srce == "" && *dest == "" {
		return cerebrumValErr(verb, "--srce or --dest is required")
	}
	p, sess, _, err := dialCerebrumAuth(cf, fs.Args(), verb)
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()
	logw := os.Stdout
	if jsonOut {
		logw = os.Stderr
	}

	// --level takes one level, a ';'-separated list (client-side expansion,
	// one action per level — the CSV grammar), or empty = the wire's
	// all-level form (live-verified: locks every existing level in one
	// action, nonexistent levels no-op).
	levels := splitLevelCell(*level)

	// Ensure read phase (ADR-0007, probel protect-connect template).
	// DEST_LOCK state is readable (LOCK_STATE + LOCKED_BY per cell); the
	// desired state maps: LOCKED/PROTECTED/... set values compare directly,
	// the clearing values (RELEASED wire-actual, legacy RELEASE) converge to
	// RELEASED. SRCE_LOCK reads are refused by every live Cerebrum tested —
	// falls through to an imperative send reported as changed.
	desired := strings.ToUpper(string(mode))
	if desired == "RELEASE" {
		desired = "RELEASED"
	}
	type lockChange struct{ Level, From string }
	var changes []lockChange
	readOK := false
	if *kind == "DEST_LOCK" && *dest != "" {
		st, serr := cerebrumObtainState(context.Background(), sess, *router, "ROUTER", 10*time.Second, cerebrumStateWant{
			DestLock: true, Verb: verb,
		})
		if serr == nil {
			readOK = true
			live := map[string]cerebrumLockSpec{}
			var destLevels []string
			for _, l := range st.Locks {
				if l.Dest == *dest {
					live[l.Level] = l
					destLevels = append(destLevels, l.Level)
				}
			}
			want := levels
			if len(want) == 0 {
				want = destLevels // all-level: every level the snapshot shows
			}
			for _, lvl := range want {
				cur, ok := live[lvl]
				state := "RELEASED" // absent cell = no lock
				if ok && cur.State != "" {
					state = strings.ToUpper(cur.State)
				}
				if state != desired {
					changes = append(changes, lockChange{Level: lvl, From: state})
				}
			}
		} else {
			fmt.Fprintf(os.Stderr, "cerebrum-nb %s: lock-state read unavailable (%v) — imperative send, reported changed\n", verb, serr)
		}
	} else {
		fmt.Fprintf(os.Stderr, "cerebrum-nb %s: SRCE_LOCK state is not readable on live Cerebrums — imperative send, reported changed\n", verb)
	}
	if !readOK {
		lv := levels
		if len(lv) == 0 {
			lv = []string{""}
		}
		for _, lvl := range lv {
			changes = append(changes, lockChange{Level: lvl, From: "?"})
		}
	}

	target := *dest
	if target == "" {
		target = *srce
	}
	diffs := make([]ensureDiff, 0, len(changes))
	for _, c := range changes {
		diffs = append(diffs, ensureDiff{Field: fmt.Sprintf("%s.%s.%s", strings.ToLower(*kind), target, displayDash(c.Level)), From: c.From, To: desired})
	}
	changed := len(changes) > 0

	if *check {
		for _, c := range changes {
			_, _ = fmt.Fprintf(logw, "[would-%s] %s dest=%s lvl=%s: %s -> %s\n", verb, *kind, displayDash(*dest), displayDash(c.Level), c.From, desired)
		}
		_, _ = fmt.Fprintf(logw, "cerebrum-nb %s --check: would_change=%d — nothing sent\n", verb, len(changes))
		if jsonOut {
			return emitEnsure(true, ensureResult{WouldChange: &changed, Diff: diffs})
		}
		return nil
	}
	for _, c := range changes {
		ctx, cancel := context.WithTimeout(context.Background(), cf.timeout)
		err := sess.Lock(ctx, *kind, mode, routeTargetFromFlags(*router, *deviceName), *srce, *dest, c.Level, *duration)
		cancel()
		if err != nil {
			return fmt.Errorf("cerebrum-nb %s lvl=%s: %w", verb, displayDash(c.Level), err)
		}
		_, _ = fmt.Fprintf(logw, "[%s] OK %s mode=%s srce=%s dest=%s lvl=%s (was %s)\n", verb, *kind, mode, displayDash(*srce), displayDash(*dest), displayDash(c.Level), c.From)
	}
	if len(changes) == 0 {
		_, _ = fmt.Fprintf(logw, "[%s] already converged — nothing sent (state=%s)\n", verb, desired)
	}
	if jsonOut {
		return emitEnsure(true, ensureResult{Changed: &changed, Diff: diffs})
	}
	return nil
}

// lockModeValue maps a --mode string to one of the 0v16 §3.2 five-value
// LOCK enum. Case-insensitive; rejects anything outside the canonical set so
// the CLI never fabricates a wire value the codec doesn't define.
func lockModeValue(mode string) (codec.LockKind, error) {
	switch strings.ToLower(mode) {
	case "unlocked":
		return codec.LockUnlocked, nil
	case "locked":
		return codec.LockLocked, nil
	case "protected":
		return codec.LockProtected, nil
	case "locked_path", "locked-path":
		return codec.LockLockedPath, nil
	case "protected_path", "protected-path":
		return codec.LockProtectedPath, nil
	case "released":
		// Wire-actual, NOT spec (live 2026-08-16): a UI release reports
		// LOCK_STATE="RELEASED" — a sixth value absent from the §3.2 table
		// and the §4.1.2/4.1.3 worked examples (whose RELEASE the server
		// NACKs, as it does UNLOCKED). RELEASED is the state machine's own
		// cleared value and the candidate clearing action.
		return codec.LockKind("RELEASED"), nil
	default:
		return "", fmt.Errorf("cerebrum-nb lock: unknown --mode %q (want unlocked|locked|protected|locked_path|protected_path|released)", mode)
	}
}

func cerebrumSetMnemonic(_ context.Context, args []string) error {
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb set-mnemonic", flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	kind := fs.String("kind", "DEST_MNE", "mnemonic kind: LEVEL_MNE | SRCE_MNE | DEST_MNE")
	router := fs.String("router", "0.0.0.0", "router IP target")
	deviceName := fs.String("device-name", "", "address by DEVICE_NAME")
	srce := fs.String("srce", "", "source ID")
	dest := fs.String("dest", "", "destination ID")
	level := fs.String("level", "", "level ID")
	mnemonic := fs.String("mnemonic", "", "mnemonic text")
	alt := fs.String("alt", "", "alternate mnemonic slot (ALT_MNE)")
	check := fs.Bool("check", false, "dry-run (ADR-0007): read the live label, report would_change, send nothing")
	output := fs.String("output", "text", "output format: text | json (ADR-0002; json = {changed|would_change, previous, current, diff[]})")
	if err := fs.Parse(args); err != nil {
		return err
	}
	jsonOut, oerr := resolveEnsureOutput(*output, false)
	if oerr != nil {
		return oerr
	}
	if err := requireKind(*kind, "set-mnemonic", "LEVEL_MNE", "SRCE_MNE", "DEST_MNE"); err != nil {
		return err
	}
	if *mnemonic == "" {
		return cerebrumValErr("set-mnemonic", "--mnemonic is required")
	}
	p, sess, _, err := dialCerebrumAuth(cf, fs.Args(), "set-mnemonic")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()
	logw := os.Stdout
	if jsonOut {
		logw = os.Stderr
	}

	// Ensure read phase (ADR-0007): read the kind's catalogue, find the
	// target row, compare the addressed slot (primary or --alt N).
	kindUp := strings.ToUpper(*kind)
	want := cerebrumStateWant{Verb: "set-mnemonic", StrictMne: true}
	id := *level
	switch kindUp {
	case "SRCE_MNE":
		want.SrcMne = true
		id = *srce
	case "DEST_MNE":
		want.DstMne = true
		id = *dest
	case "LEVEL_MNE":
		want.LvlMne = true
	}
	if id == "" {
		return cerebrumValErr("set-mnemonic", "the target ID is required (--srce / --dest / --level per --kind)")
	}
	st, serr := cerebrumObtainState(context.Background(), sess, *router, "ROUTER", 15*time.Second, want)
	if serr != nil {
		return serr
	}
	rows := st.Src
	switch kindUp {
	case "DEST_MNE":
		rows = st.Dst
	case "LEVEL_MNE":
		rows = st.Lvl
	}
	slot := 0
	if *alt != "" {
		n, aerr := strconv.Atoi(*alt)
		if aerr != nil || n < 1 {
			return cerebrumValErr("set-mnemonic", "--alt must be a positive slot index")
		}
		slot = n
	}
	previous := ""
	for _, r := range dedupeCerebrumMnes(rows) {
		if r.ID != id {
			continue
		}
		if slot == 0 {
			previous = r.Mnemonic
		} else {
			previous = r.Alts[slot]
		}
		break
	}
	changed := previous != *mnemonic
	diffs := []ensureDiff{}
	if changed {
		diffs = append(diffs, ensureDiff{Field: fmt.Sprintf("%s.%s.%d", strings.ToLower(kindUp), id, slot), From: previous, To: *mnemonic})
	}

	if *check {
		_, _ = fmt.Fprintf(logw, "cerebrum-nb set-mnemonic --check: would_change=%t (%q -> %q) — nothing sent\n", changed, previous, *mnemonic)
		if jsonOut {
			return emitEnsure(true, ensureResult{WouldChange: &changed, Current: previous, Target: *mnemonic, Diff: diffs})
		}
		return nil
	}
	if !changed {
		_, _ = fmt.Fprintf(logw, "[set-mnemonic] already converged — %s id=%s slot=%d = %q (nothing sent)\n", kindUp, id, slot, previous)
		if jsonOut {
			return emitEnsure(true, ensureResult{Changed: &changed, Previous: previous, Current: previous, Diff: diffs})
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), cf.timeout)
	defer cancel()
	if err := sess.SetMnemonic(ctx, kindUp, routeTargetFromFlags(*router, *deviceName), *srce, *dest, *level, *mnemonic, *alt); err != nil {
		return fmt.Errorf("cerebrum-nb set-mnemonic: %w", err)
	}
	_, _ = fmt.Fprintf(logw, "[set-mnemonic] OK %s id=%s slot=%d: %q -> %q\n", kindUp, id, slot, previous, *mnemonic)
	if jsonOut {
		return emitEnsure(true, ensureResult{Changed: &changed, Previous: previous, Current: *mnemonic, Diff: diffs})
	}
	return nil
}

func cerebrumSetTags(_ context.Context, args []string) error {
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb set-tags", flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	kind := fs.String("kind", "RM_DEST_TAGS", "tags kind: RM_SRCE_TAGS | RM_DEST_TAGS")
	router := fs.String("router", "0.0.0.0", "router IP target")
	deviceName := fs.String("device-name", "", "address by DEVICE_NAME")
	srce := fs.String("srce", "", "source ID")
	dest := fs.String("dest", "", "destination ID")
	tags := fs.String("tags", "", "comma-separated tag list")
	check := fs.Bool("check", false, "dry-run (ADR-0007): read the live tags where the server allows, report would_change, send nothing")
	output := fs.String("output", "text", "output format: text | json (ADR-0002)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	jsonOut, oerr := resolveEnsureOutput(*output, false)
	if oerr != nil {
		return oerr
	}
	if err := requireKind(*kind, "set-tags", "RM_SRCE_TAGS", "RM_DEST_TAGS"); err != nil {
		return err
	}
	if *tags == "" {
		return cerebrumValErr("set-tags", "--tags is required")
	}
	p, sess, _, err := dialCerebrumAuth(cf, fs.Args(), "set-tags")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()
	logw := os.Stdout
	if jsonOut {
		logw = os.Stderr
	}

	// Ensure read phase — best effort: a single-row RM tags obtain has no
	// live-proven grant (SRCE_LOCK-style refusals are likely). A granted
	// read enables a real diff; a refusal degrades to an imperative send
	// reported as changed.
	kindUp := strings.ToUpper(*kind)
	id := *dest
	item := &codec.RoutingChange{Type: kindUp, IPAddress: *router, DeviceType: codec.DeviceType("ROUTER"), DestID: *dest, SrceID: *srce, LevelID: "*"}
	if kindUp == "RM_SRCE_TAGS" {
		id = *srce
	}
	previous, readOK := "", false
	if got, gerr := obtainOneEvent(sess, cf.timeout, codec.KindRoutingChange, item, func(f *codec.Frame) bool {
		return f.Routing != nil && f.Routing.Type == kindUp
	}); gerr == nil && got != nil && got.Routing != nil {
		readOK = true
		previous = strings.Join(got.Routing.TagList, ",")
	} else {
		fmt.Fprintf(os.Stderr, "cerebrum-nb set-tags: live tags not readable — imperative send, reported changed\n")
	}
	changed := !readOK || previous != *tags
	diffs := []ensureDiff{}
	if changed {
		from := previous
		if !readOK {
			from = "?"
		}
		diffs = append(diffs, ensureDiff{Field: fmt.Sprintf("%s.%s", strings.ToLower(kindUp), id), From: from, To: *tags})
	}

	if *check {
		_, _ = fmt.Fprintf(logw, "cerebrum-nb set-tags --check: would_change=%t — nothing sent\n", changed)
		if jsonOut {
			return emitEnsure(true, ensureResult{WouldChange: &changed, Current: previous, Target: *tags, Diff: diffs})
		}
		return nil
	}
	if !changed {
		_, _ = fmt.Fprintf(logw, "[set-tags] already converged — %s id=%s tags=%q (nothing sent)\n", kindUp, id, previous)
		if jsonOut {
			return emitEnsure(true, ensureResult{Changed: &changed, Previous: previous, Current: previous, Diff: diffs})
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), cf.timeout)
	defer cancel()
	if err := sess.SetTags(ctx, kindUp, routeTargetFromFlags(*router, *deviceName), *srce, *dest, *tags); err != nil {
		return fmt.Errorf("cerebrum-nb set-tags: %w", err)
	}
	_, _ = fmt.Fprintf(logw, "[set-tags] OK %s id=%s tags=%q\n", kindUp, id, *tags)
	if jsonOut {
		return emitEnsure(true, ensureResult{Changed: &changed, Previous: previous, Current: *tags, Diff: diffs})
	}
	return nil
}

func cerebrumSalvo(_ context.Context, args []string) error {
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb salvo", flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	op := fs.String("op", "", "operation: run | save | rename | description | delete")
	group := fs.String("group", "", "salvo group")
	instance := fs.String("instance", "", "salvo instance")
	newName := fs.String("new-name", "", "new name (rename)")
	desc := fs.String("description", "", "description text (op=description; §4.3 DESCRIPTION)")
	check := fs.Bool("check", false, "dry-run (ADR-0007): read live state, report would_change, send nothing")
	output := fs.String("output", "text", "output format: text | json (ADR-0002; json = {changed|would_change, diff[]})")
	if err := fs.Parse(args); err != nil {
		return err
	}
	salvoType, err := salvoOpType(*op)
	if err != nil {
		return err
	}
	jsonOut, oerr := resolveEnsureOutput(*output, false)
	if oerr != nil {
		return oerr
	}
	if *group == "" {
		return fmt.Errorf("cerebrum-nb salvo: --group is required")
	}
	if salvoType == "RENAME" && *newName == "" {
		return fmt.Errorf("cerebrum-nb salvo: --new-name is required for rename")
	}
	if salvoType == "DESCRIPTION" && *desc == "" {
		return fmt.Errorf("cerebrum-nb salvo: --description is required for description")
	}
	p, sess, _, err := dialCerebrumAuth(cf, fs.Args(), "salvo")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()

	// Ensure read phase (ADR-0007, probel-style: the verb itself converges).
	// Stateful ops read live state and diff; RUN/SAVE are events — they
	// always fire and always report changed (Ansible command-module rule).
	changed := true
	diffs := []ensureDiff{}
	switch salvoType {
	case "DESCRIPTION":
		got, oe := obtainSingleSalvoChange(sess, cf.timeout,
			&codec.SalvoChange{Type: "INSTANCE_DETAILS", Group: *group, Instance: *instance}, "INSTANCE_DETAILS")
		if oe != nil {
			return oe
		}
		if got == nil || got.Salvo == nil || got.Salvo.InstanceDetails == nil {
			return fmt.Errorf("cerebrum-nb salvo: no INSTANCE_DETAILS reply for %s/%s (unknown instance?)", *group, *instance)
		}
		cur := got.Salvo.InstanceDetails.Description
		if cur == *desc {
			changed = false
		} else {
			diffs = append(diffs, ensureDiff{Field: "description", From: cur, To: *desc})
		}
	case "DELETE", "RENAME":
		got, oe := obtainSingleSalvoChange(sess, cf.timeout,
			&codec.SalvoChange{Type: "INSTANCE_LIST", Group: *group}, "INSTANCE_LIST")
		if oe != nil {
			return oe
		}
		if got == nil || got.Salvo == nil {
			return fmt.Errorf("cerebrum-nb salvo: no INSTANCE_LIST reply for group %s", *group)
		}
		has := slices.Contains(got.Salvo.Instances, *instance)
		if salvoType == "DELETE" {
			if !has {
				changed = false // already absent — idempotent no-op
			} else {
				diffs = append(diffs, ensureDiff{Field: "instance." + *instance, From: "present", To: "absent"})
			}
		} else {
			switch {
			case has:
				diffs = append(diffs, ensureDiff{Field: "instance." + *instance, From: *instance, To: *newName})
			case slices.Contains(got.Salvo.Instances, *newName):
				changed = false // already renamed — idempotent no-op
			default:
				return fmt.Errorf("cerebrum-nb salvo: neither %q nor %q exists in group %q", *instance, *newName, *group)
			}
		}
	default: // RUN / SAVE
		diffs = append(diffs, ensureDiff{Field: "salvo." + displayDash(*instance), From: "", To: salvoType})
	}

	if *check {
		if jsonOut {
			return emitEnsure(true, ensureResult{WouldChange: &changed, Diff: diffs})
		}
		fmt.Printf("[salvo] --check %s group=%s instance=%s: would_change=%t — nothing sent\n",
			salvoType, *group, displayDash(*instance), changed)
		for _, d := range diffs {
			fmt.Printf("  %s: %q -> %q\n", d.Field, d.From, d.To)
		}
		return nil
	}
	if !changed {
		if jsonOut {
			return emitEnsure(true, ensureResult{Changed: &changed, Diff: diffs})
		}
		fmt.Printf("[salvo] OK %s group=%s instance=%s (already converged — nothing sent)\n",
			salvoType, *group, displayDash(*instance))
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), cf.timeout)
	defer cancel()
	if err := sess.Salvo(ctx, salvoType, *group, *instance, *newName, *desc); err != nil {
		return fmt.Errorf("cerebrum-nb salvo: %w", err)
	}
	if jsonOut {
		return emitEnsure(true, ensureResult{Changed: &changed, Diff: diffs})
	}
	fmt.Printf("[salvo] OK %s group=%s instance=%s\n", salvoType, *group, displayDash(*instance))
	return nil
}

func cerebrumCategory(_ context.Context, args []string) error {
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb category", flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	op := fs.String("op", "", "operation: create | modify | modify-all | modify-desc | delete | delete-item")
	category := fs.String("category", "", "category name")
	index := fs.String("index", "", "item index (modify / delete-item)")
	itemType := fs.String("item-type", "", "§3.3 ITEM_TYPE (modify / modify-all): BLANK|SRCE|SOURCE|DEST|CATEGORY|SALVO|INHERIT|TEXT|FILE|CUSTOM")
	value := fs.String("value", "", "item value (modify); comma-separated list (modify-all)")
	name := fs.String("name", "", "name (create)")
	label := fs.String("label", "", "label (create)")
	inherits := fs.String("inherits", "", "parent category (create)")
	desc := fs.String("description", "", "description (create / modify-desc)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	catType, err := categoryOpType(*op)
	if err != nil {
		return err
	}
	it, err := categoryItemType(*itemType)
	if err != nil {
		return err
	}
	if *category == "" {
		return fmt.Errorf("cerebrum-nb category: --category is required")
	}
	// Required attrs per the §4.2 table.
	switch catType {
	case "MODIFY_ITEM":
		if *index == "" || it == "" || *value == "" {
			return fmt.Errorf("cerebrum-nb category: --index, --item-type and --value are required for modify (§4.2 MODIFY_ITEM)")
		}
	case "MODIFY_ALL":
		if it == "" || *value == "" {
			return fmt.Errorf("cerebrum-nb category: --item-type and --value are required for modify-all (§4.2 MODIFY_ALL)")
		}
	case "MODIFY_DESC":
		if *desc == "" {
			return fmt.Errorf("cerebrum-nb category: --description is required for modify-desc (§4.2 MODIFY_DESC)")
		}
	case "DELETE_ITEM":
		if *index == "" {
			return fmt.Errorf("cerebrum-nb category: --index is required for delete-item (§4.2 DELETE_ITEM)")
		}
	case "CREATE":
		if *name == "" {
			return fmt.Errorf("cerebrum-nb category: --name is required for create (§4.2 CREATE)")
		}
	}
	p, sess, _, err := dialCerebrumAuth(cf, fs.Args(), "category")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()
	ctx, cancel := context.WithTimeout(context.Background(), cf.timeout)
	defer cancel()
	act := &codec.CategoryAction{
		Type:        catType,
		Category:    *category,
		Index:       *index,
		ItemType:    it,
		Value:       *value,
		Name:        *name,
		Label:       *label,
		Inherits:    *inherits,
		Description: *desc,
	}
	if err := sess.Category(ctx, act); err != nil {
		return fmt.Errorf("cerebrum-nb category: %w", err)
	}
	fmt.Printf("[category] OK %s category=%s\n", catType, *category)
	return nil
}

func cerebrumSetValue(_ context.Context, args []string) error {
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb set-value", flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	device := fs.String("device", "", "device name (or use --ip)")
	ip := fs.String("ip", "", "device IP address (alternative to --device, §4.4.1)")
	subDevice := fs.String("sub-device", "", "sub-device")
	object := fs.String("object", "", "object")
	value := fs.String("value", "", "value to set")
	isEnum := fs.Bool("is-enum", false, "VALUE is a string representation of an enumeration (§4.4.1 IS_ENUM)")
	mode := fs.String("mode", "", "CSV write mode: SET|ADD_TAIL|INSERT_AT_HEAD|REMOVE (default SET)")
	check := fs.Bool("check", false, "dry-run (ADR-0007): read the live value, report would_change, send nothing")
	output := fs.String("output", "text", "output format: text | json (ADR-0002; json = {changed|would_change, previous, current, diff[]})")
	if err := fs.Parse(args); err != nil {
		return err
	}
	jsonOut, oerr := resolveEnsureOutput(*output, false)
	if oerr != nil {
		return oerr
	}
	if (*device == "" && *ip == "") || *subDevice == "" || *object == "" {
		return cerebrumValErr("set-value", "--device (or --ip), --sub-device and --object are required")
	}
	normMode, err := setValueModeValue(*mode)
	if err != nil {
		return err
	}
	p, sess, _, err := dialCerebrumAuth(cf, fs.Args(), "set-value")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()
	logw := os.Stdout
	if jsonOut {
		logw = os.Stderr
	}
	target := *device
	if target == "" {
		target = *ip
	}

	// Ensure read phase (ADR-0007): read the object first (§5.4.3 — the
	// live-proven happy path). Numeric-aware comparison so "5" converges
	// with a canonical "5.000000" reply instead of churning. MODE-flagged
	// CSV writes (ADD_TAIL/INSERT_AT_HEAD/REMOVE) are list edits, not a
	// converge — they skip the diff and always send.
	previous, readOK := "", false
	if normMode == "" || normMode == "SET" {
		dc := &codec.DeviceChange{Type: "VALUE", SubDevice: *subDevice, Object: *object, DeviceName: *device, IPAddress: *ip}
		if got, gerr := obtainSingleDeviceChange(sess, cf.timeout, dc, "VALUE"); gerr == nil && got != nil && got.Device != nil && got.Device.ObjectValue != nil && got.Device.ObjectValue.Available {
			readOK = true
			previous = got.Device.ObjectValue.Value
		} else {
			fmt.Fprintf(os.Stderr, "cerebrum-nb set-value: live value not readable — imperative send, reported changed\n")
		}
	}
	equalValue := func(a, b string) bool {
		if a == b {
			return true
		}
		fa, ea := strconv.ParseFloat(strings.TrimSpace(a), 64)
		fb, eb := strconv.ParseFloat(strings.TrimSpace(b), 64)
		return ea == nil && eb == nil && fa == fb
	}
	changed := !readOK || !equalValue(previous, *value)
	diffs := []ensureDiff{}
	if changed {
		from := previous
		if !readOK {
			from = "?"
		}
		diffs = append(diffs, ensureDiff{Field: fmt.Sprintf("value.%s.%s.%s", strings.TrimSpace(target), *subDevice, *object), From: from, To: *value})
	}

	if *check {
		_, _ = fmt.Fprintf(logw, "cerebrum-nb set-value --check: would_change=%t (%q -> %q) — nothing sent\n", changed, previous, *value)
		if jsonOut {
			return emitEnsure(true, ensureResult{WouldChange: &changed, Current: previous, Target: *value, Diff: diffs})
		}
		return nil
	}
	if !changed {
		_, _ = fmt.Fprintf(logw, "[set-value] already converged — %s.%s.%s = %q (nothing sent)\n", target, *subDevice, *object, previous)
		if jsonOut {
			return emitEnsure(true, ensureResult{Changed: &changed, Previous: previous, Current: previous, Diff: diffs})
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), cf.timeout)
	defer cancel()
	// Build the full §4.4 DEVICE SET_VALUE (incl. 0v16 IP_ADDRESS / IS_ENUM /
	// MODE) and send it via the generic Action path.
	body := &codec.DeviceAction{
		Type:       "SET_VALUE",
		DeviceName: *device,
		IPAddress:  *ip,
		SubDevice:  *subDevice,
		Object:     *object,
		Value:      *value,
		IsEnum:     *isEnum,
		Mode:       normMode,
	}
	if err := sess.Action(ctx, body); err != nil {
		return fmt.Errorf("cerebrum-nb set-value: %w", err)
	}
	_, _ = fmt.Fprintf(logw, "[set-value] OK %s.%s.%s: %q -> %q\n", target, *subDevice, *object, previous, *value)
	if jsonOut {
		return emitEnsure(true, ensureResult{Changed: &changed, Previous: previous, Current: *value, Diff: diffs})
	}
	return nil
}

// setValueModeValue validates/normalises the set-value --mode flag to the
// §4.4.1 CSV MODE enum. Empty = default (SET) and is omitted on the wire.
func setValueModeValue(mode string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(mode)) {
	case "":
		return "", nil
	case "SET":
		return "SET", nil
	case "ADD_TAIL":
		return "ADD_TAIL", nil
	case "INSERT_AT_HEAD":
		return "INSERT_AT_HEAD", nil
	case "REMOVE":
		return "REMOVE", nil
	default:
		return "", fmt.Errorf("cerebrum-nb set-value: unknown --mode %q (want SET|ADD_TAIL|INSERT_AT_HEAD|REMOVE)", mode)
	}
}

func cerebrumObtainDatastore(_ context.Context, args []string) error {
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb obtain-datastore", flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	name := fs.String("name", "", "datastore path/name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" {
		return fmt.Errorf("cerebrum-nb obtain-datastore: --name is required")
	}
	p, sess, _, err := dialCerebrum(cf, fs.Args(), "obtain-datastore")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()

	var got *codec.Frame
	done := make(chan struct{})
	var once sync.Once
	signal := func() { once.Do(func() { close(done) }) }
	timer := time.AfterFunc(cf.timeout, signal)
	defer timer.Stop()
	sess.OnEvent(codec.KindDatastoreChange, func(f *codec.Frame) {
		got = f
		signal()
	})
	ctx, cancel := context.WithTimeout(context.Background(), cf.timeout)
	defer cancel()
	if err := sess.ObtainDatastore(ctx, *name); err != nil {
		return fmt.Errorf("cerebrum-nb obtain-datastore: %w", err)
	}
	<-done
	if got == nil || got.Datastore == nil {
		fmt.Fprintln(os.Stderr, "(no DATASTORE_CHANGE response within timeout)")
		return nil
	}
	ds := got.Datastore
	fmt.Printf("name       %s\n", ds.Name)
	fmt.Printf("type       %s\n", displayDash(ds.Type))
	fmt.Printf("available  %s\n", boolFlag(ds.Available))
	// 0v16 §5.5.1: the obtain reply carries the store body in <DATA>…</DATA>.
	// Surface the raw inner XML so operators see the store contents directly.
	if ds.Data != "" {
		fmt.Printf("data       %d bytes\n", len(ds.Data))
		fmt.Println(ds.Data)
	} else {
		fmt.Println("data       (none)")
	}
	return nil
}

// cerebrumDeviceConfig issues a 0v16 §4.5 DEVICE_CONFIGURATION command
// (ADD / MODIFY / REMOVE) for one of the four device types (GENERIC / PANEL
// / ROUTER / SNMP), mapping per-type flags to the codec config structs, and
// prints the <RESULT VALUE="ACCEPTED|FAILED"/> verdict. Requires an
// authenticated session.
func cerebrumDeviceConfig(_ context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("cerebrum-nb device-config: missing operation (add|modify|remove)")
	}
	op := args[0]
	cfgType, err := deviceConfigOpType(op)
	if err != nil {
		return err
	}
	rest := reorderFlagsFirst(args[1:])
	fs := flag.NewFlagSet("cerebrum-nb device-config "+op, flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	deviceType := fs.String("device-type", "", "device type: generic | panel | router | snmp")
	ip := fs.String("ip", "", "device IP_ADDRESS (required; the addressing key)")
	dname := fs.String("device-name", "", "DEVICE_NAME (optional on add; with --ip identifies the device on modify)")

	// shared GENERIC / PANEL body
	device := fs.String("device", "", "DEVICE (driver/model identifier)")
	version := fs.String("version", "", "VERSION")
	name := fs.String("name", "", "NAME (display name; also the SNMP NAME)")
	connType := fs.String("conn-type", "", "PROTOCOL connection type: ASYNC_HTTP | UDP | TCP | WEBSOCKET_SERVER")
	port := fs.String("port-number", "", "PROTOCOL PORT_NUMBER (generic/panel/snmp body port)")
	timeout := fs.String("timeout-ms", "", "PROTOCOL TIMEOUT (ms)")
	poll := fs.String("poll", "", "PROTOCOL POLL_PERIOD (ms)")
	// PANEL extras
	cpf := fs.String("cpf", "", "PANEL CPF")
	panelID := fs.String("panel-id", "", "PANEL PANEL_ID")
	panelType := fs.String("panel-type", "", "PANEL PANEL_TYPE")
	// ROUTER body
	rtype := fs.String("router-type", "", "ROUTER numeric routing-device TYPE id")
	baud := fs.String("baud", "", "ROUTER SERIAL BAUD")
	parity := fs.String("parity", "", "ROUTER SERIAL PARITY")
	maxLevel := fs.String("max-level", "", "ROUTER MAX_LEVEL")
	maxSource := fs.String("max-source", "", "ROUTER MAX_SOURCE")
	maxDest := fs.String("max-dest", "", "ROUTER MAX_DEST")
	// SNMP body
	snmpPort := fs.String("snmp-port", "", "SNMP PORT")
	check := fs.Bool("check", false, "dry-run (ADR-0007): consult the live device LIST, report would_change, send nothing")
	output := fs.String("output", "text", "output format: text | json (ADR-0002)")

	if err := fs.Parse(rest); err != nil {
		return err
	}
	jsonOut, oerr := resolveEnsureOutput(*output, false)
	if oerr != nil {
		return oerr
	}
	if *ip == "" {
		return cerebrumValErr("device-config", "--ip is required")
	}
	dc := &codec.DeviceConfiguration{
		Type:       cfgType,
		IPAddress:  *ip,
		DeviceName: *dname,
	}
	if cfgType != codec.DeviceConfigRemove {
		dt, buildErr := buildDeviceConfigBody(dc, *deviceType, deviceConfigBodyFlags{
			device: *device, version: *version, name: *name,
			connType: *connType, port: *port, timeout: *timeout, poll: *poll,
			cpf: *cpf, panelID: *panelID, panelType: *panelType,
			routerType: *rtype, baud: *baud, parity: *parity,
			maxLevel: *maxLevel, maxSource: *maxSource, maxDest: *maxDest,
			snmpPort: *snmpPort, snmpName: *name,
		})
		if buildErr != nil {
			return buildErr
		}
		dc.DeviceType = dt
	}

	p, sess, _, err := dialCerebrumAuth(cf, fs.Args(), "device-config")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()
	logw := os.Stdout
	if jsonOut {
		logw = os.Stderr
	}

	// Ensure read phase (ADR-0007): ADD and REMOVE are idempotent against
	// the live device LIST (add-existing / remove-absent = changed:false,
	// nothing sent). MODIFY has no per-field read-back over NB, so it
	// always sends and always reports changed.
	changed := true
	from := "?"
	if cfgType != "MODIFY" {
		if got, gerr := obtainSingleDeviceChange(sess, cf.timeout, &codec.DeviceChange{Type: "LIST"}, "LIST"); gerr == nil && got != nil && got.Device != nil {
			present := false
			for _, d := range got.Device.Devices {
				if d.IPAddress == *ip {
					present = true
					break
				}
			}
			if present {
				from = "present"
			} else {
				from = "absent"
			}
			if cfgType == codec.DeviceConfigRemove {
				changed = present
			} else { // ADD
				changed = !present
			}
		} else {
			fmt.Fprintf(os.Stderr, "cerebrum-nb device-config: device LIST not readable — imperative send, reported changed\n")
		}
	}
	diffs := []ensureDiff{}
	if changed {
		diffs = append(diffs, ensureDiff{Field: "device." + *ip, From: from, To: string(cfgType)})
	}

	if *check {
		_, _ = fmt.Fprintf(logw, "cerebrum-nb device-config --check: would_change=%t (%s ip=%s, live=%s) — nothing sent\n", changed, cfgType, *ip, from)
		if jsonOut {
			return emitEnsure(true, ensureResult{WouldChange: &changed, Current: from, Target: string(cfgType), Diff: diffs})
		}
		return nil
	}
	if !changed {
		_, _ = fmt.Fprintf(logw, "[device-config] already converged — %s ip=%s (live=%s, nothing sent)\n", cfgType, *ip, from)
		if jsonOut {
			return emitEnsure(true, ensureResult{Changed: &changed, Previous: from, Current: from, Diff: diffs})
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), cf.timeout)
	defer cancel()
	res, err := sess.DeviceConfig(ctx, dc)
	if err != nil {
		return fmt.Errorf("cerebrum-nb device-config: %w", err)
	}
	_, _ = fmt.Fprintf(logw, "[device-config] %s %s ip=%s result=%s\n", cfgType, dc.DeviceType, dc.IPAddress, displayDash(res.Value))
	if !res.Accepted {
		return fmt.Errorf("cerebrum-nb device-config: server returned %s", displayDash(res.Value))
	}
	if jsonOut {
		return emitEnsure(true, ensureResult{Changed: &changed, Previous: from, Diff: diffs})
	}
	return nil
}

// deviceConfigBodyFlags carries the flattened per-type body flags so
// buildDeviceConfigBody can map them onto the right codec struct.
type deviceConfigBodyFlags struct {
	device, version, name             string
	connType, port, timeout, poll     string
	cpf, panelID, panelType           string
	routerType, baud, parity          string
	maxLevel, maxSource, maxDest      string
	snmpPort, snmpName                string
}

// buildDeviceConfigBody attaches the body struct chosen by deviceType to dc
// and returns the §4.5 ConfigDeviceType selector. Only the flags relevant to
// the chosen type are consumed.
func buildDeviceConfigBody(dc *codec.DeviceConfiguration, deviceType string, f deviceConfigBodyFlags) (codec.ConfigDeviceType, error) {
	protocol := func() *codec.ProtocolConfiguration {
		if f.connType == "" && f.port == "" && f.timeout == "" && f.poll == "" {
			return nil
		}
		return &codec.ProtocolConfiguration{
			ConnectionType: codec.ConnectionType(f.connType),
			PortNumber:     f.port,
			Timeout:        f.timeout,
			PollPeriod:     f.poll,
		}
	}
	switch strings.ToLower(deviceType) {
	case "generic":
		dc.Generic = &codec.GenericConfig{
			Device: f.device, Version: f.version, Name: f.name,
			Protocol: protocol(),
		}
		return codec.ConfigDeviceGeneric, nil
	case "panel":
		dc.Panel = &codec.PanelConfig{
			Device: f.device, Version: f.version, Name: f.name,
			Protocol:  protocol(),
			CPF:       f.cpf,
			PanelID:   f.panelID,
			PanelType: f.panelType,
		}
		return codec.ConfigDevicePanel, nil
	case "router":
		rc := &codec.RouterConfig{
			Device: f.device, Name: f.name, Type: f.routerType,
		}
		if f.baud != "" || f.parity != "" {
			rc.Serial = &codec.SerialConfig{Baud: f.baud, Parity: f.parity}
		}
		if f.maxLevel != "" || f.maxSource != "" || f.maxDest != "" {
			rc.Router = &codec.RouterParams{
				MaxLevel: f.maxLevel, MaxSource: f.maxSource, MaxDest: f.maxDest,
			}
		}
		dc.Router = rc
		return codec.ConfigDeviceRouter, nil
	case "snmp":
		dc.SNMP = &codec.SNMPConfig{Name: f.snmpName, Port: f.snmpPort}
		return codec.ConfigDeviceSNMP, nil
	case "":
		return "", fmt.Errorf("cerebrum-nb device-config: --device-type is required (generic|panel|router|snmp)")
	default:
		return "", fmt.Errorf("cerebrum-nb device-config: unknown --device-type %q (want generic|panel|router|snmp)", deviceType)
	}
}

// deviceConfigOpType maps add|modify|remove to the §4.5 DEVICE_CONFIGURATION
// TYPE verb.
func deviceConfigOpType(op string) (codec.DeviceConfigType, error) {
	switch strings.ToLower(op) {
	case "add":
		return codec.DeviceConfigAdd, nil
	case "modify":
		return codec.DeviceConfigModify, nil
	case "remove":
		return codec.DeviceConfigRemove, nil
	default:
		return "", fmt.Errorf("cerebrum-nb device-config: unknown operation %q (want add|modify|remove)", op)
	}
}

// requireKind validates that kind is one of the allowed values.
func requireKind(kind, verb string, allowed ...string) error {
	for _, a := range allowed {
		if strings.EqualFold(kind, a) {
			return nil
		}
	}
	return fmt.Errorf("cerebrum-nb %s: unknown --kind %q (want one of %s)", verb, kind, strings.Join(allowed, " | "))
}

// salvoOpType maps the --op string to a §4.3 SALVO TYPE.
func salvoOpType(op string) (string, error) {
	switch strings.ToLower(op) {
	case "run":
		return "RUN", nil
	case "save":
		return "SAVE", nil
	case "rename":
		return "RENAME", nil
	case "description":
		return "DESCRIPTION", nil
	case "delete":
		return "DELETE", nil
	case "":
		return "", fmt.Errorf("cerebrum-nb salvo: --op is required (run|save|rename|description|delete)")
	default:
		return "", fmt.Errorf("cerebrum-nb salvo: unknown --op %q (want run|save|rename|description|delete)", op)
	}
}

// categoryOpType maps the --op string to a §4.2 CATEGORY TYPE (all six).
func categoryOpType(op string) (string, error) {
	switch strings.ToLower(op) {
	case "create":
		return "CREATE", nil
	case "modify":
		return "MODIFY_ITEM", nil
	case "modify-all":
		return "MODIFY_ALL", nil
	case "modify-desc":
		return "MODIFY_DESC", nil
	case "delete":
		return "DELETE", nil
	case "delete-item":
		return "DELETE_ITEM", nil
	case "":
		return "", fmt.Errorf("cerebrum-nb category: --op is required (create|modify|modify-all|modify-desc|delete|delete-item)")
	default:
		return "", fmt.Errorf("cerebrum-nb category: unknown --op %q (want create|modify|modify-all|modify-desc|delete|delete-item)", op)
	}
}

// categoryItemType validates a --item-type against the §3.3 ITEM_TYPE enum
// (empty is allowed — the attribute is simply omitted).
func categoryItemType(s string) (codec.ItemType, error) {
	if s == "" {
		return "", nil
	}
	it := codec.ItemType(strings.ToUpper(s))
	switch it {
	case codec.ItemBlank, codec.ItemSrce, codec.ItemSource, codec.ItemDest,
		codec.ItemCategory, codec.ItemSalvo, codec.ItemInherit,
		codec.ItemText, codec.ItemFile, codec.ItemCustom:
		return it, nil
	}
	return "", fmt.Errorf("cerebrum-nb category: unknown --item-type %q (§3.3: BLANK|SRCE|SOURCE|DEST|CATEGORY|SALVO|INHERIT|TEXT|FILE|CUSTOM)", s)
}
