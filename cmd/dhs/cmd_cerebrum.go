package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"acp/internal/cerebrum-nb/codec"
	cerebrum "acp/internal/cerebrum-nb/consumer"
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
	case "list-routers":
		return cerebrumListRouters(ctx, rest)
	case "walk":
		return cerebrumWalk(ctx, rest)
	}
	return fmt.Errorf("cerebrum-nb: unknown verb %q (expected: connect | listen | list-devices | list-routers | walk)", verb)
}

func printCerebrumHelp() {
	fmt.Println(`dhs consumer cerebrum-nb — EVS Cerebrum Northbound API (XML over WebSocket)

USAGE
  dhs consumer cerebrum-nb <verb> <host>[:port] [flags]

VERBS
  connect       login + ping loop only (sanity check + redundancy probe)
  listen        subscribe to all routing/category/salvo/device events
  list-devices  one-shot device list (obtain device_change LIST)
  list-routers  one-shot router list (filter device_change LIST by Router)
  walk          full obtain across all §5 types (devices + categories + salvos)

FLAGS (order doesn't matter — flags can come before OR after the host)
  --port N                  WebSocket port (default 40007)
  --user U                  NB username (or $DHS_CEREBRUM_USER)
  --pass P                  NB password (or $DHS_CEREBRUM_PASS)
  --tls                     use wss:// instead of ws://
  --insecure-skip-verify    with --tls, skip cert validation
  --debug                   verbose RX/TX XML logging
  --timeout DUR             per-request timeout (default 5s — fail fast)

EXAMPLES
  dhs consumer cerebrum-nb connect      10.6.239.50
  dhs consumer cerebrum-nb connect      127.0.0.1 --port 4008
  dhs consumer cerebrum-nb listen       10.6.239.50 --user admin --pass s3cr3t
  dhs consumer cerebrum-nb list-devices 10.6.239.50:40007
  dhs consumer cerebrum-nb walk         cerebrum.local --tls`)
}

// connectAndLogin: parse flags, build a Plugin, Connect.
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
	fmt.Fprintf(os.Stderr, "connected; logged in.\n")
	return p, p.Session(), cf, rest[1:], nil
}

// ----------------------------------------------------------------------
// Verbs
// ----------------------------------------------------------------------

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

	subCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	items := []codec.SubItem{
		&codec.RoutingChange{Type: "ROUTE", DeviceName: "*", DeviceType: codec.DeviceType("*")},
		&codec.RoutingChange{Type: "SRCE_LOCK", DeviceName: "*", DeviceType: codec.DeviceType("*")},
		&codec.RoutingChange{Type: "DEST_LOCK", DeviceName: "*", DeviceType: codec.DeviceType("*")},
		&codec.CategoryChange{Type: "CATEGORY_LIST"},
		&codec.SalvoChange{Type: "GROUP_LIST"},
		&codec.DeviceChange{Type: "LIST"},
	}
	if err := sess.Subscribe(subCtx, items); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "listening for routing/category/salvo/device events; Ctrl+C to stop")
	<-ctx.Done()
	return nil
}

func cerebrumListDevices(_ context.Context, args []string) error {
	p, sess, _, _, err := connectAndLogin(args, "list-devices")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()
	return obtainAndPrintDeviceList(sess, "")
}

func cerebrumListRouters(_ context.Context, args []string) error {
	p, sess, _, _, err := connectAndLogin(args, "list-routers")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()
	return obtainAndPrintDeviceList(sess, "Router")
}

func cerebrumWalk(_ context.Context, args []string) error {
	p, sess, cf, _, err := connectAndLogin(args, "walk")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()

	devices := []*codec.DeviceChange{}
	categories := []*codec.CategoryChange{}
	salvoGroups := []*codec.SalvoChange{}
	done := make(chan struct{})
	timer := time.AfterFunc(cf.timeout, func() { close(done) })

	sess.OnEvent(codec.KindDeviceChange, func(f *codec.Frame) {
		if f.Device != nil && f.Device.Type == "LIST" {
			devices = append(devices, f.Device)
		}
	})
	sess.OnEvent(codec.KindCategoryChange, func(f *codec.Frame) {
		if f.Category != nil {
			categories = append(categories, f.Category)
		}
	})
	sess.OnEvent(codec.KindSalvoChange, func(f *codec.Frame) {
		if f.Salvo != nil {
			salvoGroups = append(salvoGroups, f.Salvo)
		}
	})

	obCtx, cancel := context.WithTimeout(context.Background(), cf.timeout)
	defer cancel()
	items := []codec.SubItem{
		&codec.DeviceChange{Type: "LIST"},
		&codec.CategoryChange{Type: "CATEGORY_LIST"},
		&codec.SalvoChange{Type: "GROUP_LIST"},
	}
	if err := sess.Obtain(obCtx, items); err != nil {
		return err
	}
	<-done
	timer.Stop()

	fmt.Printf("devices     %d\n", len(devices))
	for _, d := range devices {
		fmt.Printf("  %-12s %-20s %s\n", d.DeviceType, d.DeviceName, d.IPAddress)
	}
	fmt.Printf("categories  %d\n", len(categories))
	for _, c := range categories {
		fmt.Printf("  %s\n", c.Category)
	}
	fmt.Printf("salvos      %d\n", len(salvoGroups))
	for _, s := range salvoGroups {
		fmt.Printf("  %s/%s\n", s.Group, s.Instance)
	}
	return nil
}

// ----------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------

func obtainAndPrintDeviceList(sess *cerebrum.Session, deviceTypeFilter string) error {
	devices := []*codec.DeviceChange{}
	done := make(chan struct{})
	timer := time.AfterFunc(15*time.Second, func() { close(done) })

	sess.OnEvent(codec.KindDeviceChange, func(f *codec.Frame) {
		if f.Device != nil && f.Device.Type == "LIST" {
			if deviceTypeFilter != "" && string(f.Device.DeviceType) != deviceTypeFilter {
				return
			}
			devices = append(devices, f.Device)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	items := []codec.SubItem{&codec.DeviceChange{Type: "LIST"}}
	if err := sess.Obtain(ctx, items); err != nil {
		return err
	}
	<-done
	timer.Stop()

	fmt.Printf("%-10s  %-30s  %s\n", "DEVICE_TYPE", "DEVICE_NAME", "IP_ADDRESS")
	for _, d := range devices {
		fmt.Printf("%-10s  %-30s  %s\n", d.DeviceType, d.DeviceName, d.IPAddress)
	}
	if len(devices) == 0 {
		fmt.Fprintln(os.Stderr, "(no devices reported within 15s)")
	}
	return nil
}

func printEvent(f *codec.Frame) {
	switch f.Kind {
	case codec.KindRoutingChange:
		rc := f.Routing
		fmt.Printf("[routing] %-8s dev=%s/%s srce=%s(%s) dest=%s(%s) lvl=%s(%s)\n",
			rc.Type, rc.DeviceType, rc.DeviceName,
			rc.SrceID, rc.SrceName, rc.DestID, rc.DestName,
			rc.LevelID, rc.LevelName)
	case codec.KindCategoryChange:
		fmt.Printf("[category] %s %s\n", f.Category.Type, f.Category.Category)
	case codec.KindSalvoChange:
		fmt.Printf("[salvo] %s group=%s inst=%s\n", f.Salvo.Type, f.Salvo.Group, f.Salvo.Instance)
	case codec.KindDeviceChange:
		fmt.Printf("[device] %-8s type=%s name=%s ip=%s sub=%s obj=%s\n",
			f.Device.Type, f.Device.DeviceType, f.Device.DeviceName,
			f.Device.IPAddress, f.Device.SubDevice, f.Device.Object)
	case codec.KindDatastoreChange:
		fmt.Printf("[datastore] %s type=%s\n", f.Datastore.Name, f.Datastore.Type)
	default:
		fmt.Printf("[%s] %s\n", f.Kind, f.Root.String())
	}
}

func boolFlag(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func currentAPIVer(sess *cerebrum.Session) string {
	host, _ := sess.RemoteHostPort()
	major := sess.APIVersionMajor()
	if major == 0 {
		return "(unknown)"
	}
	_ = host
	return strconv.Itoa(major)
}
