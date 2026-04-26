package main

// AMWA NMOS CLI verbs. Phase 1 step #1 ships discovery only:
//
//   dhs consumer nmos discover [--mdns | --unicast --resolver IP] [--service _nmos-register._tcp]
//   dhs producer nmos serve    [--mdns | --no-mdns] [--advertise-host host:port] [--port N]
//   dhs registry nmos serve    [--mdns | --no-mdns] [--advertise-host host:port] [--port N] [--priority N]
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
	"strconv"
	"strings"
	"time"

	codec "acp/internal/amwa/codec/dnssd"
	session "acp/internal/amwa/session/dnssd"
	registryslot "acp/internal/registry"
)

// runNMOSConsumer dispatches `dhs consumer nmos <verb> [args]`.
func runNMOSConsumer(ctx context.Context, args []string) error {
	if len(args) == 0 || hasHelpFlag(args) {
		printNMOSConsumerHelp()
		return nil
	}
	verb := args[0]
	rest := args[1:]
	switch verb {
	case "discover":
		return runNMOSDiscover(ctx, rest)
	}
	return fmt.Errorf("consumer nmos: unknown verb %q (expected: discover)", verb)
}

// runNMOSProducer dispatches `dhs producer nmos <verb> [args]`.
func runNMOSProducer(ctx context.Context, args []string) error {
	if len(args) == 0 || hasHelpFlag(args) {
		printNMOSProducerHelp()
		return nil
	}
	verb := args[0]
	rest := args[1:]
	switch verb {
	case "serve":
		return runNMOSNodeServe(ctx, rest)
	}
	return fmt.Errorf("producer nmos: unknown verb %q (expected: serve)", verb)
}

// runNMOSRegistry dispatches `dhs registry nmos <verb> [args]`.
func runNMOSRegistry(ctx context.Context, args []string) error {
	if len(args) == 0 || hasHelpFlag(args) {
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
	insts, err := session.ResolveUnicast(ctx, resolver, service, codec.DefaultDomain, timeout)
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

// ---- producer serve (Phase 1 #1: mDNS announce only, placeholder Node) ------

func runNMOSNodeServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	mdns := fs.Bool("mdns", true, "advertise via mDNS")
	noMDNS := fs.Bool("no-mdns", false, "disable mDNS announce")
	advertise := fs.String("advertise-host", "", "host:port placed in DNS-SD A/SRV records (default: hostname:port)")
	port := fs.Int("port", 8080, "Node API port advertised in SRV record")
	apiVer := fs.String("api-ver", "v1.3", "IS-04 Node API version advertised in TXT")
	if err := fs.Parse(args); err != nil {
		return err
	}
	mdnsActive := *mdns && !*noMDNS
	if !mdnsActive {
		fmt.Println("dhs producer nmos serve: mDNS announce disabled; nothing else implemented yet (Phase 1 #1 scope).")
		<-ctx.Done()
		return nil
	}

	host, err := resolveAdvertiseHost(*advertise, *port)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	resp, err := session.NewResponder(logger)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Close() }()

	ins := codec.Instance{
		Name:    "dhs-nmos-node",
		Service: codec.ServiceNode,
		Domain:  codec.DefaultDomain,
		Host:    host,
		Port:    uint16(*port),
		TXT: map[string]string{
			codec.TXTKeyAPIProto: "http",
			codec.TXTKeyAPIVer:   *apiVer,
			codec.TXTKeyAPIAuth:  "false",
		},
	}
	if err := resp.Announce(ctx, ins); err != nil {
		return err
	}
	fmt.Printf("Announcing %s on %s:%d (mDNS).\n", codec.ServiceNode, host, *port)
	fmt.Println("Note: Phase 1 #1 ships mDNS announce only; Node API REST surface lands in Phase 1 #3.")

	<-ctx.Done()
	return nil
}

// ---- registry serve ---------------------------------------------------------

func runNMOSRegistryServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	mdns := fs.Bool("mdns", true, "advertise via mDNS (Mode A)")
	noMDNS := fs.Bool("no-mdns", false, "disable mDNS announce (Modes B / C)")
	advertise := fs.String("advertise-host", "", "host:port placed in DNS-SD A/SRV records (default: hostname:port)")
	bind := fs.String("bind", ":8235", "Registration/Query API listen address (HTTP surface lands in Phase 1 #4)")
	priority := fs.Int("priority", 0, "DNS-SD `pri` TXT (0-99 production, 100+ dev)")
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

	binds := []string{*bind}
	// AdvertiseHost (when non-empty) wins over BindAddrs in
	// pickAdvertiseHostPort, so we just pass both through.
	opts := registryslot.ServeOptions{
		BindAddrs:     binds,
		AdvertiseHost: *advertise,
		Priority:      *priority,
		DiscoveryMode: mode,
	}
	fmt.Printf("Registry: bind=%s, mode=%s, priority=%d\n", *bind, mode, *priority)
	if mode == "mdns" {
		fmt.Println("Announcing _nmos-register._tcp + _nmos-query._tcp via mDNS.")
	}
	fmt.Println("Note: Phase 1 #1 ships mDNS announce only; Registration/Query API REST lands in Phase 1 #4.")

	return r.Serve(ctx, opts)
}

// resolveAdvertiseHost honours an explicit --advertise-host, otherwise
// derives one from os.Hostname + the requested port.
func resolveAdvertiseHost(adv string, port int) (string, error) {
	if adv != "" {
		host, _, err := splitNMOSHostPort(adv)
		if err != nil {
			return "", err
		}
		return host, nil
	}
	h, err := os.Hostname()
	if err != nil || h == "" {
		h = "localhost"
	}
	if !strings.Contains(h, ".") {
		h = h + "." + codec.DefaultDomain
	}
	_ = port
	return h, nil
}

// splitNMOSHostPort wraps net.SplitHostPort so callers can pass "host:port"
// or "host" — the latter yields an empty port.
func splitNMOSHostPort(s string) (string, string, error) {
	if !strings.Contains(s, ":") {
		return s, "", nil
	}
	idx := strings.LastIndex(s, ":")
	host := s[:idx]
	port := s[idx+1:]
	if _, err := strconv.Atoi(port); err != nil {
		return "", "", fmt.Errorf("nmos: bad port %q", port)
	}
	return host, port, nil
}

// ---- help text --------------------------------------------------------------

func printNMOSConsumerHelp() {
	fmt.Println(`Usage:
  dhs consumer nmos discover [flags]

Discovery flags:
  --mdns                Use mDNS multicast (Mode A; default)
  --no-mdns             Disable mDNS
  --unicast             Use unicast DNS-SD (Mode B); requires --resolver
  --resolver host[:53]  Authoritative DNS server for unicast queries
  --peer-list FILE      Static CSV (Mode C; "host,port[,api_ver]")
  --service NAME        DNS-SD service type (default _nmos-register._tcp)
  --timeout DURATION    Discovery deadline (default 5s)`)
}

func printNMOSProducerHelp() {
	fmt.Println(`Usage:
  dhs producer nmos serve [flags]

Phase 1 #1 scope: announces a placeholder Node via mDNS only — IS-04
Node API REST surface lands in Phase 1 #3.

  --mdns                Advertise via mDNS (default)
  --no-mdns             Disable mDNS announce
  --advertise-host H    host placed in SRV record (default: os.Hostname)
  --port N              Port advertised in SRV (default 8080)
  --api-ver V           IS-04 version in TXT (default v1.3)`)
}

func printNMOSRegistryHelp() {
	fmt.Println(`Usage:
  dhs registry nmos serve [flags]

Phase 1 #1 scope: announces _nmos-register._tcp + _nmos-query._tcp
via mDNS only — Registration/Query API REST lands in Phase 1 #4.

  --mdns                Advertise via mDNS (default)
  --no-mdns             Disable mDNS announce
  --advertise-host H    host:port placed in SRV record
  --bind ADDR           Future HTTP listen address (default :8235)
  --priority N          DNS-SD pri TXT (0-99 prod, 100+ dev)`)
}
