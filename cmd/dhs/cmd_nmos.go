package main

// AMWA NMOS CLI verbs. Phase 1 #1 shipped discovery only; Phase 1 #2
// adds IS-09 System API on top:
//
//   dhs consumer nmos discover [--mdns | --unicast --resolver IP] [--service ...]
//   dhs consumer nmos system   [--mdns | --unicast --resolver IP | --peer-list F | --direct H:P]
//                              [--api-ver v1.0] [--api-proto http] [--service ...] [--timeout 5s]
//   dhs producer nmos serve    [--role node|system] [--config FILE] [--mdns | --no-mdns] [--bind :PORT]
//   dhs registry nmos serve    [--mdns | --no-mdns] [--advertise-host host:port] [--bind :PORT] [--priority N]
//
// Higher-level NMOS verbs (walk, watch, connect, ncp) land alongside the
// IS-04/05/07/12 plugin layers in later phases.

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	codec "dhs/internal/amwa/codec/dnssd"
	"dhs/internal/amwa/codec/is09"
	"dhs/internal/amwa/codec/spec"
	"dhs/internal/amwa/consumer"
	"dhs/internal/amwa/provider"
	session "dhs/internal/amwa/session/dnssd"
	registryslot "dhs/internal/registry"
)

// runNMOSConsumer dispatches `dhs consumer nmos <verb> [args]`.
func runNMOSConsumer(ctx context.Context, args []string) error {
	// Help IN PLACE of a verb = catalogue; after the verb it belongs to
	// the verb's own FlagSet (#462).
	if len(args) == 0 || isHelpToken(args[0]) {
		printNMOSConsumerHelp()
		return nil
	}
	verb := args[0]
	rest := args[1:]
	switch verb {
	case "discover":
		return runNMOSDiscover(ctx, rest)
	case "system":
		return runNMOSSystem(ctx, rest)
	case "walk":
		return runNMOSWalk(ctx, rest)
	case "events":
		return runNMOSEventsConsumer(ctx, rest)
	}
	return fmt.Errorf("consumer nmos: unknown verb %q (expected: discover, system, walk, events)", verb)
}

// runNMOSProducer dispatches `dhs producer nmos <verb> [args]`.
func runNMOSProducer(ctx context.Context, args []string) error {
	if len(args) == 0 || isHelpToken(args[0]) {
		printNMOSProducerHelp()
		return nil
	}
	verb := args[0]
	rest := args[1:]
	switch verb {
	case "serve":
		return runNMOSNodeServe(ctx, rest)
	case "events":
		return runNMOSEventsProducer(ctx, rest)
	}
	return fmt.Errorf("producer nmos: unknown verb %q (expected: serve, events)", verb)
}

// runNMOSRegistry dispatches `dhs registry nmos <verb> [args]`.
func runNMOSRegistry(ctx context.Context, args []string) error {
	if len(args) == 0 || isHelpToken(args[0]) {
		printNMOSRegistryHelp()
		return nil
	}
	verb := args[0]
	rest := args[1:]
	switch verb {
	case "serve":
		return runNMOSRegistryServe(ctx, rest)
	}
	return fmt.Errorf("registry nmos: unknown verb %q (expected: serve)", verb)
}

// ---- discover ---------------------------------------------------------------

func runNMOSDiscover(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("discover", flag.ContinueOnError)
	mdns := fs.Bool("mdns", true, "use mDNS multicast discovery (Mode A)")
	noMDNS := fs.Bool("no-mdns", false, "disable mDNS (forces unicast or peer-list mode)")
	unicast := fs.Bool("unicast", false, "use unicast DNS-SD (Mode B); requires --resolver")
	resolver := fs.String("resolver", "", "unicast DNS-SD resolver host[:port]; default port 53")
	peerList := fs.String("peer-list", "", "static CSV peer list (Mode C: host,port[,api_ver])")
	service := fs.String("service", codec.ServiceRegister, "DNS-SD service type to discover")
	timeout := fs.Duration("timeout", 5*time.Second, "discovery deadline")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *peerList != "" {
		return runNMOSDiscoverPeerList(*peerList)
	}
	if *unicast || *resolver != "" {
		return runNMOSDiscoverUnicast(ctx, *resolver, *service, *timeout)
	}
	if !*mdns || *noMDNS {
		return fmt.Errorf("nmos discover: pick exactly one of --mdns / --unicast / --peer-list")
	}
	return runNMOSDiscoverMDNS(ctx, *service, *timeout)
}

func runNMOSDiscoverPeerList(path string) error {
	entries, err := session.ReadPeerList(path)
	if err != nil {
		return err
	}
	fmt.Printf("Peer list (%d entries) from %s:\n", len(entries), path)
	for _, e := range entries {
		ver := e.APIVer
		if ver == "" {
			ver = "(default)"
		}
		fmt.Printf("  - %s:%d  api_ver=%s\n", e.Host, e.Port, ver)
	}
	return nil
}

func runNMOSDiscoverUnicast(ctx context.Context, resolver, service string, timeout time.Duration) error {
	if resolver == "" {
		return fmt.Errorf("nmos discover --unicast: --resolver is required")
	}
	// If the user passed a fully-qualified service (e.g.
	// `_nmos-register._tcp.by-systems.arpa`), peel the domain off so
	// instance Domain is set correctly and FullName doesn't suffix
	// `.local`. A bare service-type uses DefaultDomain ("local").
	bareSvc, dom := splitServiceDomain(service)
	if dom == "" {
		dom = codec.DefaultDomain
	}
	insts, err := session.ResolveUnicast(ctx, resolver, bareSvc, dom, timeout)
	if err != nil {
		return err
	}
	if len(insts) == 0 {
		fmt.Printf("Unicast DNS-SD: no instances of %s found via %s\n", service, resolver)
		return nil
	}
	printInstances(service, insts)
	return nil
}

func runNMOSDiscoverMDNS(ctx context.Context, service string, timeout time.Duration) error {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	br, err := session.NewBrowser(logger)
	if err != nil {
		return err
	}
	defer func() { _ = br.Close() }()

	dctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ch, err := br.Browse(dctx, service)
	if err != nil {
		return err
	}
	seen := make(map[string]codec.Instance)
	for ins := range ch {
		seen[ins.FullName()] = ins
	}
	if len(seen) == 0 {
		fmt.Printf("mDNS: no instances of %s found within %s\n", service, timeout)
		return nil
	}
	insts := make([]codec.Instance, 0, len(seen))
	for _, v := range seen {
		insts = append(insts, v)
	}
	sort.Slice(insts, func(i, j int) bool { return insts[i].Name < insts[j].Name })
	printInstances(service, insts)
	return nil
}

// splitServiceDomain peels the domain off a possibly-FQDN service
// name. `_nmos-register._tcp` → ("_nmos-register._tcp", "");
// `_nmos-register._tcp.by-systems.arpa` → ("_nmos-register._tcp",
// "by-systems.arpa"). Heuristic per RFC 6763 §4.1.2: leading
// underscore-prefixed labels form the bare service-type, the rest
// is the discovery domain.
func splitServiceDomain(s string) (bare, domain string) {
	parts := strings.Split(s, ".")
	cut := len(parts)
	for i, p := range parts {
		if !strings.HasPrefix(p, "_") {
			cut = i
			break
		}
	}
	if cut == len(parts) {
		return s, ""
	}
	return strings.Join(parts[:cut], "."), strings.Join(parts[cut:], ".")
}

func printInstances(service string, insts []codec.Instance) {
	fmt.Printf("Discovered %d instance(s) of %s:\n", len(insts), service)
	for _, ins := range insts {
		fmt.Printf("  %s\n", ins.FullName())
		fmt.Printf("    host = %s:%d\n", ins.Host, ins.Port)
		if pri, ok := codec.PriorityFromTXT(ins.TXT); ok {
			fmt.Printf("    pri  = %d\n", pri)
		}
		if v, ok := ins.TXT[codec.TXTKeyAPIProto]; ok {
			fmt.Printf("    proto= %s\n", v)
		}
		if v, ok := ins.TXT[codec.TXTKeyAPIVer]; ok {
			fmt.Printf("    ver  = %s\n", v)
		}
		if v, ok := ins.TXT[codec.TXTKeyAPIAuth]; ok {
			fmt.Printf("    auth = %s\n", v)
		}
		for _, ip := range ins.IPv4 {
			fmt.Printf("    ipv4 = %s\n", ip)
		}
	}
}

// ---- producer serve ---------------------------------------------------------
// Phase 1 #1 ships --role node (mDNS announce only, no HTTP).
// Phase 1 #2 adds  --role system (IS-09 System API).

func runNMOSNodeServe(ctx context.Context, args []string) error {
	// Two-pass parse: peek at --role first so we can dispatch to the
	// IS-09 path without rejecting unknown flags.
	role := "node"
	for i, a := range args {
		if a == "--role" || a == "-role" {
			if i+1 < len(args) {
				role = strings.ToLower(args[i+1])
			}
		} else if strings.HasPrefix(a, "--role=") {
			role = strings.ToLower(strings.TrimPrefix(a, "--role="))
		} else if strings.HasPrefix(a, "-role=") {
			role = strings.ToLower(strings.TrimPrefix(a, "-role="))
		}
	}
	switch role {
	case "system":
		return runNMOSSystemServe(ctx, args)
	case "node", "":
		return runNMOSNodeServeLegacy(ctx, args)
	}
	return fmt.Errorf("producer nmos serve: unknown --role %q (expected: node, system)", role)
}

func runNMOSNodeServeLegacy(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	_ = fs.String("role", "node", "producer role to start (node | system)")
	configPath := fs.String("config", "", "path to IS-04 Node bundle JSON (node + devices + sources + flows + senders + receivers)")
	bind := fs.String("bind", ":8080", "Node API HTTP listen address")
	advertise := fs.String("advertise-host", "", "host[:port] placed in DNS-SD A/SRV records (default: hostname + bind port)")
	mdns := fs.Bool("mdns", true, "advertise via mDNS")
	noMDNS := fs.Bool("no-mdns", false, "disable mDNS announce (Mode B / static)")
	apiVer := fs.String("api-ver", "v1.3", "IS-04 wire version exposed under /x-nmos/node/<v>")
	priority := fs.Int("priority", 0, "DNS-SD `pri` TXT (0-99 production, 100+ dev)")
	registry := fs.String("registry", "", "Registration API base URL — when set, the Node POSTs to /resource + heartbeats")
	heartbeat := fs.Duration("heartbeat", 0, "POST /health cadence (default 5s, IS-04 §6.1). Set it longer than the registry's --heartbeat-timeout to have this node garbage-collected on purpose")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" {
		return fmt.Errorf("producer nmos serve --role node: --config FILE required (use Phase 0 #1 mDNS-only placeholder via --discover-only flag if you really mean to)")
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	bundle, err := provider.LoadNodeConfigFromFile(*configPath)
	if err != nil {
		return err
	}

	mode := "mdns"
	if !*mdns || *noMDNS {
		mode = "static"
	}
	cfg := provider.IS04NodeConfig{
		Bind:              *bind,
		AdvertiseHost:     *advertise,
		DiscoveryMode:     mode,
		Priority:          *priority,
		APIVer:            *apiVer,
		RegistryURL:       *registry,
		HeartbeatInterval: *heartbeat,
	}
	srv, err := provider.NewIS04NodeServer(logger, bundle, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = srv.Stop() }()

	fmt.Printf("Node API: bind=%s, mode=%s, api_ver=%s, priority=%d\n", *bind, mode, *apiVer, *priority)
	fmt.Printf("  GET http://<host>%s/x-nmos/node/%s/{,self,devices,sources,flows,senders,receivers}\n", *bind, *apiVer)
	if mode == "mdns" {
		fmt.Println("Announcing _nmos-node._tcp via mDNS.")
	}
	if *registry != "" {
		beat := *heartbeat
		if beat <= 0 {
			beat = provider.HeartbeatInterval
		}
		fmt.Printf("Registering against %s, heartbeat every %s.\n", *registry, beat)
	}
	return srv.Serve(ctx)
}

// ---- consumer system (IS-09) ------------------------------------------------

func runNMOSSystem(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("system", flag.ContinueOnError)
	mdns := fs.Bool("mdns", true, "discover via mDNS (Mode A)")
	noMDNS := fs.Bool("no-mdns", false, "disable mDNS")
	unicast := fs.Bool("unicast", false, "discover via unicast DNS-SD (Mode B); requires --resolver")
	resolver := fs.String("resolver", "", "unicast DNS-SD resolver host[:port]")
	peerList := fs.String("peer-list", "", "static CSV peer list (Mode C: host,port[,api_ver])")
	direct := fs.String("direct", "", "skip discovery and GET /global from this host:port")
	service := fs.String("service", codec.ServiceSystem, "DNS-SD service type to discover")
	apiVer := fs.String("api-ver", is09.APIVersion, "IS-09 wire version (TXT api_ver filter + URL prefix)")
	apiProto := fs.String("api-proto", "http", "TXT api_proto filter (http | https)")
	timeout := fs.Duration("timeout", 5*time.Second, "discovery deadline")
	if err := fs.Parse(args); err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Direct override — skip discovery entirely.
	if *direct != "" {
		res, err := consumer.Fetch(ctx, consumer.IS09FetchOptions{
			Logger:   logger,
			APIVer:   *apiVer,
			APIProto: *apiProto,
			Direct:   *direct,
		})
		if err != nil {
			return err
		}
		printIS09Result(res)
		return nil
	}

	// Discovery phase — pick exactly one source.
	bareSvc, dom := splitServiceDomain(*service)
	if dom == "" {
		dom = codec.DefaultDomain
	}
	var insts []codec.Instance
	switch {
	case *peerList != "":
		entries, err := session.ReadPeerList(*peerList)
		if err != nil {
			return err
		}
		for _, e := range entries {
			insts = append(insts, codec.Instance{
				Name:    e.Host,
				Service: codec.ServiceSystem,
				Domain:  dom,
				Host:    e.Host,
				Port:    uint16(e.Port),
				TXT: map[string]string{
					codec.TXTKeyAPIProto: *apiProto,
					codec.TXTKeyAPIVer:   *apiVer,
				},
			})
		}
	case *unicast || *resolver != "":
		if *resolver == "" {
			return fmt.Errorf("nmos system --unicast: --resolver is required")
		}
		got, err := session.ResolveUnicast(ctx, *resolver, bareSvc, dom, *timeout)
		if err != nil {
			return err
		}
		insts = got
	case *mdns && !*noMDNS:
		got, err := consumer.DiscoverMDNS(ctx, *timeout, logger)
		if err != nil {
			return err
		}
		insts = got
	default:
		return fmt.Errorf("nmos system: pick exactly one of --mdns / --unicast / --peer-list / --direct")
	}

	if len(insts) == 0 {
		return fmt.Errorf("nmos system: no %s instances found", *service)
	}

	res, err := consumer.Fetch(ctx, consumer.IS09FetchOptions{
		Logger:     logger,
		APIVer:     *apiVer,
		APIProto:   *apiProto,
		Discovered: insts,
	})
	if err != nil {
		return err
	}
	printIS09Result(res)
	return nil
}

func printIS09Result(res *consumer.FetchResult) {
	fmt.Println("System API selected:")
	if res.Selected.Name != "" && res.Selected.Service != "" {
		fmt.Printf("  instance = %s\n", res.Selected.FullName())
	}
	fmt.Printf("  url      = %s\n", res.URL)
	if pri, ok := codec.PriorityFromTXT(res.Selected.TXT); ok {
		fmt.Printf("  pri      = %d\n", pri)
	}
	if v, ok := res.Selected.TXT[codec.TXTKeyAPIVer]; ok {
		fmt.Printf("  api_ver  = %s\n", v)
	}
	if v, ok := res.Selected.TXT[codec.TXTKeyAPIProto]; ok {
		fmt.Printf("  api_proto= %s\n", v)
	}

	g := res.Global
	fmt.Println("\n/global:")
	fmt.Printf("  id                          = %s\n", g.ID)
	fmt.Printf("  version                     = %s\n", g.Version)
	fmt.Printf("  label                       = %q\n", g.Label)
	fmt.Printf("  description                 = %q\n", g.Description)
	fmt.Printf("  is04.heartbeat_interval     = %d\n", g.IS04.HeartbeatInterval)
	fmt.Printf("  ptp.announce_receipt_timeout = %d\n", g.PTP.AnnounceReceiptTimeout)
	fmt.Printf("  ptp.domain_number            = %d\n", g.PTP.DomainNumber)
	if g.SyslogV2 != nil {
		fmt.Printf("  syslogv2                    = %s:%d\n", g.SyslogV2.Hostname, g.SyslogV2.Port)
	} else {
		fmt.Println("  syslogv2                    = (none)")
	}
	if g.Syslog != nil {
		fmt.Printf("  syslog                      = %s:%d\n", g.Syslog.Hostname, g.Syslog.Port)
	} else {
		fmt.Println("  syslog                      = (none)")
	}
}

// ---- producer serve --role system (IS-09) -----------------------------------

func runNMOSSystemServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	_ = fs.String("role", "system", "producer role")
	configPath := fs.String("config", "", "path to IS-09 /global resource JSON file (required)")
	bind := fs.String("bind", ":10641", "HTTP listen address")
	advertise := fs.String("advertise-host", "", "host[:port] placed in DNS-SD A/SRV records")
	mdns := fs.Bool("mdns", true, "advertise via mDNS")
	noMDNS := fs.Bool("no-mdns", false, "disable mDNS announce (Mode B / static)")
	apiVer := fs.String("api-ver", is09.APIVersion, "IS-09 wire version exposed under /x-nmos/system/<v>")
	priority := fs.Int("priority", 0, "DNS-SD `pri` TXT (0-99 production, 100+ dev)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" {
		return fmt.Errorf("producer nmos serve --role system: --config FILE required")
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	g, err := provider.LoadIS09GlobalFromFile(*configPath)
	if err != nil {
		return err
	}

	mode := "mdns"
	if !*mdns || *noMDNS {
		mode = "static"
	}
	cfg := provider.IS09Config{
		Bind:          *bind,
		AdvertiseHost: *advertise,
		DiscoveryMode: mode,
		Priority:      *priority,
		APIVer:        *apiVer,
	}
	srv, err := provider.NewIS09Server(logger, g, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = srv.Stop() }()

	fmt.Printf("System API: bind=%s, mode=%s, api_ver=%s, priority=%d\n", *bind, mode, *apiVer, *priority)
	fmt.Printf("  GET http://<host>%s/x-nmos/system/%s/        (index)\n", *bind, *apiVer)
	fmt.Printf("  GET http://<host>%s/x-nmos/system/%s/global  (global resource)\n", *bind, *apiVer)
	if mode == "mdns" {
		fmt.Println("Announcing _nmos-system._tcp via mDNS.")
	} else {
		fmt.Println("DNS-SD announce disabled — caller publishes records (e.g. pfSense Unbound, see docs/dns-sd-unbound.md).")
	}
	return srv.Serve(ctx)
}

// ---- registry serve ---------------------------------------------------------

func runNMOSRegistryServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	mdns := fs.Bool("mdns", true, "advertise via mDNS (Mode A)")
	noMDNS := fs.Bool("no-mdns", false, "disable mDNS announce (Modes B / C)")
	advertise := fs.String("advertise-host", "", "host:port placed in DNS-SD A/SRV records (default: hostname:port)")
	bind := fs.String("bind", ":8235", "Registration/Query API listen address")
	priority := fs.Int("priority", 0, "DNS-SD `pri` TXT (0-99 production, 100+ dev)")
	apiVer := fs.String("api-ver", "", "IS-04 wire version exposed at /x-nmos/{registration,query}/<v>. Empty (default) mounts every codec registered (v1.0/v1.1/v1.2/v1.3 in parallel) — pin to one minor for per-version integration testing.")
	gcInterval := fs.Duration("gc-interval", time.Second, "heartbeat watchdog tick rate")
	heartbeatTimeout := fs.Duration("heartbeat-timeout", 12*time.Second, "evict Nodes after this long without heartbeats (IS-04 §6.1 default 12s)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	mode := "mdns"
	if !*mdns || *noMDNS {
		mode = "static"
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	f, ok := registryslot.Lookup("nmos")
	if !ok {
		return fmt.Errorf("registry plugin %q not registered", "nmos")
	}
	r := f.New(logger)

	opts := registryslot.ServeOptions{
		BindAddrs:        []string{*bind},
		AdvertiseHost:    *advertise,
		Priority:         *priority,
		DiscoveryMode:    mode,
		APIVer:           *apiVer,
		GCInterval:       *gcInterval,
		HeartbeatTimeout: *heartbeatTimeout,
	}
	verLabel := *apiVer
	if verLabel == "" {
		verLabel = "<all>"
	}
	fmt.Printf("Registry: bind=%s, mode=%s, priority=%d, api_ver=%s\n", *bind, mode, *priority, verLabel)
	fmt.Printf("  Registration: POST/GET/DELETE under http://<host>%s/x-nmos/registration/%s/...\n", *bind, verLabel)
	fmt.Printf("  Query:        GET + POST /subscriptions under http://<host>%s/x-nmos/query/%s/...\n", *bind, verLabel)
	fmt.Printf("  WS subs:      ws://<host>%s/x-nmos/query/%s/subscriptions/<id>/ws\n", *bind, verLabel)
	fmt.Printf("  GC: tick=%s, heartbeat-timeout=%s\n", *gcInterval, *heartbeatTimeout)
	if mode == "mdns" {
		fmt.Println("Announcing _nmos-register._tcp + _nmos-query._tcp via mDNS.")
	}

	return r.Serve(ctx, opts)
}

// ---- help text --------------------------------------------------------------

func printNMOSConsumerHelp() {
	fmt.Println(`Usage:
  dhs consumer nmos discover [flags]
  dhs consumer nmos system   [flags]

discover — print every NMOS instance the configured discovery mode reveals.
  --mdns                Use mDNS multicast (Mode A; default)
  --no-mdns             Disable mDNS
  --unicast             Use unicast DNS-SD (Mode B); requires --resolver
  --resolver host[:53]  Authoritative DNS server for unicast queries
  --peer-list FILE      Static CSV (Mode C; "host,port[,api_ver]")
  --service NAME        DNS-SD service type (default _nmos-register._tcp)
  --timeout DURATION    Discovery deadline (default 5s)

system — IS-09 System API client (Node bootstrap). Discovers _nmos-system._tcp,
applies the spec-strict selection rule (filter by api_proto + api_ver, sort by
pri, randomised tie-break), GETs /global, validates against the IS-09 v1.0
JSON Schema, prints the parsed fields.
  (any of the discovery flags above)
  --direct host:port    Skip discovery; fetch /global directly from this peer
  --api-ver V           Requested IS-09 version (default v1.0)
  --api-proto P         Requested protocol filter (default http)`)
}

func printNMOSProducerHelp() {
	fmt.Println(`Usage:
  dhs producer nmos serve [flags]

  --role node|system    Producer role (default: node)

Role: node (Phase 1 #3 — IS-04 v1.3 Node API)
  Loads a Node bundle JSON (node + devices + sources + flows + senders +
  receivers), validates each resource against the IS-04 v1.3 JSON Schema,
  serves the Node API on --bind, optionally announces _nmos-node._tcp via
  mDNS, and (when --registry is set) POSTs every resource + heartbeats
  every 5 s to a Registry's Registration API.

  --config FILE         Node bundle JSON (required)
  --bind ADDR           HTTP listen address (default :8080)
  --advertise-host H    host[:port] placed in DNS-SD A/SRV records
  --mdns / --no-mdns    Toggle mDNS announce (default on)
  --api-ver V           IS-04 version exposed under /x-nmos/node/<v> (default v1.3)
  --priority N          DNS-SD pri TXT (0-99 prod, 100+ dev)
  --registry URL        Registration API base URL (e.g. http://reg.local:8235);
                        omit to run mDNS-only / direct-Node mode
  --heartbeat DURATION  POST /health cadence (default 5s, IS-04 §6.1). Pair it
                        with the registry's --heartbeat-timeout to drive the
                        garbage collector on purpose: a cadence LONGER than the
                        timeout must get this node evicted, a shorter one must
                        not. The threshold is NOT discoverable — IS-04 gives no
                        way for a registry to advertise it, so both sides just
                        assume the 5s/12s defaults.

Role: system (Phase 1 #2 — IS-09 System API)
  Loads + validates a /global resource JSON, serves the two IS-09 endpoints
  on --bind, optionally announces _nmos-system._tcp via mDNS.

  --config FILE         IS-09 /global resource JSON (required)
  --bind ADDR           HTTP listen address (default :10641)
  --advertise-host H    host[:port] placed in DNS-SD A/SRV records
  --mdns / --no-mdns    Toggle mDNS announce (default on)
  --api-ver V           IS-09 version exposed under /x-nmos/system/<v> (default v1.0)
  --priority N          DNS-SD pri TXT (0-99 prod, 100+ dev)`)
}

func printNMOSRegistryHelp() {
	fmt.Println(`Usage:
  dhs registry nmos serve [flags]

Boots the IS-04 v1.3 Registry — Registration API + Query API + WS
subscriptions on the configured --bind, plus the optional mDNS
announce of _nmos-register._tcp + _nmos-query._tcp.

  --bind ADDR              HTTP listen address (default :8235)
  --advertise-host H:P     host:port placed in SRV / ws_href records
                           (default: derived from --bind + hostname)
  --mdns / --no-mdns       Toggle mDNS announce (default on)
  --api-ver V              IS-04 version exposed (default v1.3)
  --priority N             DNS-SD pri TXT (0-99 prod, 100+ dev)
  --gc-interval D          Heartbeat watchdog tick rate (default 1s)
  --heartbeat-timeout D    Evict Nodes that miss heartbeats this long
                           (default 12s, IS-04 §6.1)`)
}

// ---- consumer walk (IS-04 Controller) ---------------------------------------

func runNMOSWalk(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("walk", flag.ContinueOnError)
	registry := fs.String("registry", "", "Registry origin (Mode B unicast — http://host:port). When empty, --mdns or --unicast triggers DNS-SD discovery.")
	mdns := fs.Bool("mdns", true, "discover the Registry via mDNS (Mode A); ignored if --registry is set")
	unicast := fs.Bool("unicast", false, "discover via unicast DNS-SD (Mode B); requires --resolver")
	resolver := fs.String("resolver", "", "unicast DNS resolver IP")
	domain := fs.String("domain", "by-systems.arpa", "unicast DNS-SD discovery domain")
	apiVer := fs.String("api-ver", "", "force a specific IS-04 wire minor (v1.1 / v1.2 / v1.3); empty = highest mutual")
	timeout := fs.Duration("timeout", 5*time.Second, "DNS-SD discovery timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *registry == "" && !*mdns && !*unicast {
		return fmt.Errorf("nmos walk: pick exactly one of --registry / --mdns / --unicast")
	}

	mode := ""
	switch {
	case *unicast:
		if *resolver == "" {
			return fmt.Errorf("nmos walk --unicast: --resolver is required")
		}
		mode = "unicast"
	case *mdns:
		mode = "mdns"
	}

	rep := &spec.SliceReporter{}
	logger := slog.Default()
	c, err := consumer.NewController(ctx, consumer.ControllerOptions{
		Logger:           logger,
		Reporter:         rep,
		RegistryURL:      *registry,
		DiscoveryMode:    mode,
		DiscoveryTimeout: *timeout,
		UnicastResolver:  *resolver,
		UnicastDomain:    *domain,
		APIVer:           *apiVer,
	})
	if err != nil {
		return fmt.Errorf("nmos walk: %w", err)
	}

	fmt.Printf("Registry %s (api_ver=%s, spec=%s)\n", c.BaseURL(), c.Codec().APIVer(), c.Codec().SpecPatch())

	snap, errs := c.Walk(ctx)
	fmt.Printf("Catalogue:\n")
	fmt.Printf("  nodes:     %d\n", len(snap.Nodes))
	fmt.Printf("  devices:   %d\n", len(snap.Devices))
	fmt.Printf("  sources:   %d\n", len(snap.Sources))
	fmt.Printf("  flows:     %d\n", len(snap.Flows))
	fmt.Printf("  senders:   %d\n", len(snap.Senders))
	fmt.Printf("  receivers: %d\n", len(snap.Receivers))
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "warn: %v\n", e)
	}
	if events := rep.Snapshot(); len(events) > 0 {
		fmt.Fprintf(os.Stderr, "%d compliance event(s) fired:\n", len(events))
		for _, ev := range events {
			fmt.Fprintf(os.Stderr, "  [%s] %s/%s %s: %s\n",
				ev.Severity, ev.SpecID, ev.APIVer, ev.Code, ev.Detail)
		}
	}
	return nil
}
