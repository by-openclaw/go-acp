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
	"strings"
	"sync"
	"time"

	"dhs/internal/cerebrum-nb/codec"
	"dhs/internal/cerebrum-nb/codec/ws"
	cerebrum "dhs/internal/cerebrum-nb/consumer"
)

// cerebrumFlags is the common flag set for every dhs consumer cerebrum-nb
// verb. host[:port] is positional; everything else is a flag.
type cerebrumFlags struct {
	port     int
	user     string
	pass     string
	tls      bool
	insecure bool
	debug    bool
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
	fs.DurationVar(&c.timeout, "timeout", 5*time.Second, "per-request timeout")
	return c
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
	case "route":
		return cerebrumRoute(ctx, rest)
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
  listen                   SUBSCRIBE — routing / category / salvo / device events; Ctrl+C to stop
  route                    ACTION <ROUTING TYPE='ROUTE'/> — single (--dest --srce --level), batch (--route dst:src:lvl), or --csv FILE
  list-devices             OBTAIN <device_change type='LIST'/>  [--device-type Router|SNMP|Device]
  device-details           OBTAIN <device_change type='DETAILS'/>  --device IP --device-type DEVICE
  device-value             OBTAIN <device_change type='VALUE'/>    --device NAME --by-name --sub-device X --object Y
  list-categories          OBTAIN <category_change type='CATEGORY_LIST'/>
  category-details         OBTAIN <category_change type='CATEGORY_DETAILS'/>  --category NAME
  list-salvo-groups        OBTAIN <salvo_change type='GROUP_LIST'/>
  list-salvo-instances     OBTAIN <salvo_change type='INSTANCE_LIST'/>      --group NAME
  salvo-instance-details   OBTAIN <salvo_change type='INSTANCE_DETAILS'/>   --group NAME --instance NAME
  keepalive-probe          DIAGNOSTIC — hold WS open, observe TCP keep-alives  [--idle DUR] [--send-login]

FLAGS (order doesn't matter — flags can come before OR after the host)
  --port N                  WebSocket port (default 40007)
  --user U                  NB username (or $DHS_CEREBRUM_USER)
  --pass P                  NB password (or $DHS_CEREBRUM_PASS)
  --tls                     use wss:// instead of ws://
  --insecure-skip-verify    with --tls, skip cert validation
  --debug                   verbose RX/TX XML logging
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
	rest := fs.Args()
	if len(rest) < 1 {
		return nil, nil, nil, nil, fmt.Errorf("cerebrum-nb %s: missing host[:port] argument", verb)
	}
	host, portArg, err := splitHostPort(rest[0], cf.port)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	cf.port = portArg

	logLevel := slog.LevelInfo
	if cf.debug {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))

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
		return nil, nil, nil, nil, err
	}
	if p.Session().LoggedIn() {
		fmt.Fprintf(os.Stderr, "connected; logged in.\n")
	} else {
		fmt.Fprintf(os.Stderr, "connected (no LOGIN — set --user/--pass for verbs that act on data).\n")
	}
	return p, p.Session(), cf, rest[1:], nil
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
	p, sess, _, _, err := connectAndLogin(args, "listen")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()

	// Print every dispatched event. Wildcard-everything subscription.
	sess.OnEvent(codec.KindUnknown, func(f *codec.Frame) {
		printEvent(f)
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
			Type: "ROUTE", IPAddress: "0.0.0.0", DeviceType: codec.DeviceType("ROUTER"),
			DestID: "*", LevelID: "*",
		}},
		{"ROUTING_CHANGE TYPE=SRCE_LOCK", &codec.RoutingChange{
			Type: "SRCE_LOCK", IPAddress: "0.0.0.0", DeviceType: codec.DeviceType("ROUTER"),
			SrceID: "*",
		}},
		{"ROUTING_CHANGE TYPE=DEST_LOCK", &codec.RoutingChange{
			Type: "DEST_LOCK", IPAddress: "0.0.0.0", DeviceType: codec.DeviceType("ROUTER"),
			DestID: "*", LevelID: "*",
		}},
		{"CATEGORY_CHANGE TYPE=CATEGORY_LIST", &codec.CategoryChange{Type: "CATEGORY_LIST"}},
		{"SALVO_CHANGE TYPE=GROUP_LIST", &codec.SalvoChange{Type: "GROUP_LIST"}},
		{"DEVICE_CHANGE TYPE=LIST", &codec.DeviceChange{Type: "LIST"}},
	}
	var ok, fail int
	for _, p := range plan {
		// No timeout: SUBSCRIBE on a wildcard ROUTING_CHANGE can stream
		// 100k+ snapshot rows before the ACK lands. Bind to the verb's
		// own ctx so the call exits cleanly on Ctrl+C, and if the WS
		// dies the underlying read returns immediately.
		err := sess.Subscribe(ctx, []codec.SubItem{p.item})
		if err != nil {
			slog.Warn("subscribe failed", "plugin", "cerebrum-nb", "item", p.name, "err", err)
			fail++
			continue
		}
		ok++
	}
	fmt.Fprintf(os.Stderr, "subscribed: %d ok, %d failed; listening for events; Ctrl+C to stop\n", ok, fail)
	<-ctx.Done()
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
		dc.DeviceType = codec.DeviceType(deviceType)
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
		return fmt.Errorf("cerebrum-nb device-value: --sub-device and --object are both required")
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
	if d.ObjectValue != nil {
		fmt.Printf("available   %s\n", boolFlag(d.ObjectValue.Available))
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
	if s.InstanceDetails != nil {
		fmt.Printf("available   %s\n", boolFlag(s.InstanceDetails.Available))
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

func printEvent(f *codec.Frame) {
	switch f.Kind {
	case codec.KindRoutingChange:
		rc := f.Routing
		// ROUTE rows carry the source in the <route source_id source_level_id/>
		// child element (RouteSourceID / RouteSourceLevelID), not the
		// top-level srce_id attribute. SRCE_LOCK / DEST_LOCK / *_MNE rows
		// keep the source on the top-level attrs as before.
		srceID, srceName := rc.SrceID, rc.SrceName
		if rc.Type == "ROUTE" && rc.RouteSourceID != "" {
			srceID = rc.RouteSourceID
			if rc.RouteSourceLevelID != "" && rc.RouteSourceLevelID != rc.LevelID {
				srceID = fmt.Sprintf("%s@lvl%s", rc.RouteSourceID, rc.RouteSourceLevelID)
			}
		}
		fmt.Printf("[routing] %-8s dev=%s/%s srce=%s(%s) dest=%s(%s) lvl=%s(%s)\n",
			rc.Type, rc.DeviceType, rc.DeviceName,
			srceID, srceName, rc.DestID, rc.DestName,
			rc.LevelID, rc.LevelName)
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
			fmt.Printf("[device] %-8s type=%s name=%s ip=%s sub=%s obj=%s\n",
				f.Device.Type, f.Device.DeviceType, f.Device.DeviceName,
				f.Device.IPAddress, f.Device.SubDevice, f.Device.Object)
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
	if err := fs.Parse(args); err != nil {
		return err
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

	logLevel := slog.LevelInfo
	if cf.debug {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))

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

	fails := 0
	for _, r := range routes {
		body := &codec.RoutingAction{
			Type:        "ROUTE",
			IPAddress:   *router,
			DeviceName:  *deviceName,
			DeviceType:  codec.DeviceType("ROUTER"),
			DestID:      r.Dest,
			SrceID:      r.Srce,
			LevelID:     r.Level,
		}
		if cf.debug {
			fmt.Fprintf(os.Stderr, "tx: <action mtid=N><ROUTING TYPE=ROUTE DEST_ID=%s SRCE_ID=%s LEVEL_ID=%s/></action>\n", r.Dest, r.Srce, r.Level)
		}
		if err := sess.Action(context.Background(), body); err != nil {
			fmt.Printf("[route] NACK dst=%s src=%s lvl=%s reason=%s\n", r.Dest, r.Srce, r.Level, err)
			fails++
			continue
		}
		fmt.Printf("[route] OK   dst=%s src=%s lvl=%s\n", r.Dest, r.Srce, r.Level)
	}
	if fails > 0 {
		return fmt.Errorf("%d/%d routes failed", fails, len(routes))
	}
	return nil
}
