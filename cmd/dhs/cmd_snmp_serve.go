package main

// The serving half: `dhs producer snmp serve` is an agent somebody
// else's manager polls, and `dhs producer snmp trap` sends one
// notification to one or more managers in whichever version each speaks.

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"dhs/internal/consumer/compliance"
	"dhs/internal/plugin"
	"dhs/internal/snmp/codec"
	snmpcons "dhs/internal/snmp/consumer"
	"dhs/internal/snmp/mib"
	snmpprov "dhs/internal/snmp/provider"
	"dhs/internal/snmp/usm"
)

// runSNMPServe binds an agent.
func runSNMPServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	bind := fs.String("bind", fmt.Sprintf("0.0.0.0:%d", snmpprov.DefaultPort),
		"address to serve on. Port 161 needs privilege on Linux; use a high port for a dev rig, as Cerebrum's own agent does on 1161.")
	read := fs.String("read-community", "public", "community that admits GET, GETNEXT and GETBULK")
	write := fs.String("write-community", "",
		"community that admits SET. EMPTY REFUSES EVERY SET, including one carrying the read community — a plant where one password does both is one typo from a re-route.")
	descr := fs.String("descr", "", "sysDescr.0 (default: this build's version line)")
	name := fs.String("name", "", "sysName.0 (default: this host's name)")
	contact := fs.String("contact", "", "sysContact.0")
	location := fs.String("location", "", "sysLocation.0")
	pidfile := fs.String("pidfile", "", "write this process's PID to PATH so `dhs producer snmp stop|ensure --pidfile PATH` can manage it")
	metricsAddr := fs.String("metrics-addr", "", "serve /snmp.json and /snapshot.json on this address")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	if *pidfile != "" {
		if err := writePIDFile(*pidfile); err != nil {
			return fmt.Errorf("write pidfile: %w", err)
		}
		defer func() { _ = os.Remove(*pidfile) }()
	}

	logger, logClean := producerLogger(ctx)
	defer logClean()
	deps := pluginDeps(logger)

	if *descr == "" {
		*descr = "dhs SNMP agent " + version
	}
	if *name == "" {
		if h, err := os.Hostname(); err == nil {
			*name = h
		}
	}

	tree := snmpprov.NewMIB()
	if err := tree.Register(snmpprov.SystemGroup(snmpprov.SystemInfo{
		Descr:    *descr,
		ObjectID: mib.DHS,
		Contact:  *contact,
		Name:     *name,
		Location: *location,
	}, deps.Clock)...); err != nil {
		return err
	}

	srv := snmpprov.NewServer(tree, snmpprov.Communities{
		Read: *read, Write: *write,
	}, deps)

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if *metricsAddr != "" {
		serveMetricsEndpoint(ctx, logger, *metricsAddr, srv.Metrics(), map[string]string{
			"proto": "snmp", "role": "producer", "addr": *bind,
		})
	}

	if *write == "" {
		logger.Info("snmp agent: SET is refused (no --write-community)")
	}

	return srv.Serve(ctx, *bind)
}

// runSNMPTrapSend emits one notification.
//
// It exists so a trap destination can be PROVEN before anything depends
// on it: every device in docs/testbed.md currently points its traps at
// an address that no longer exists, and the only way to tell a working
// receiver from a working sender is to send one on purpose.
func runSNMPTrapSend(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("trap", flag.ContinueOnError)
	to := fs.String("to", "", "comma-separated receivers as ADDR[:PORT][/VERSION[/COMMUNITY-OR-USER]] — e.g. 10.6.250.5,10.6.255.9:162/1/public,10.6.250.7/3/operator")
	enterprise := fs.String("enterprise", mib.DHS.String(), "the sending device's sysObjectID, used as the v1 enterprise and the stem of the v2c identity")
	generic := fs.Int("generic", int(codec.EnterpriseSpecific), "RFC 1157 generic trap 0..6; 6 means look at --specific")
	specific := fs.Int("specific", 1, "enterprise-specific trap number, meaningful when --generic is 6")
	agentAddr := fs.String("agent-addr", "", "the v1 agent-address field (default: this host's outbound address). v2c and v3 have no such field.")
	uptime := fs.Uint("uptime", 0, "sysUpTime in CENTISECONDS at the event; 0 means now-since-start (which for a one-shot is 0)")

	engineName := fs.String("engine-id", "dhs-agent", "name in this sender's RFC 3411 engine ID; v3 keys every localised key on it")
	boots := fs.Int("engine-boots", 1, "this engine's restart count. It MUST be persisted and incremented across restarts, or a device accepts messages recorded before its last reboot.")
	user := fs.String("user", "", "USM user name for v3 destinations")
	authProto := fs.String("auth", "", "v3 authentication: md5, sha, sha224, sha256, sha384 or sha512")
	authPass := fs.String("auth-pass", "", "v3 authentication password")
	privProto := fs.String("priv", "", "v3 privacy: des or aes")
	privPass := fs.String("priv-pass", "", "v3 privacy password")

	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	if *to == "" {
		return fmt.Errorf("snmp trap: --to names at least one receiver")
	}
	ent, err := mib.Resolve(*enterprise)
	if err != nil {
		return err
	}
	dests, needV3, err := parseTrapDestinations(*to)
	if err != nil {
		return err
	}

	logger, logClean := producerLogger(ctx)
	defer logClean()
	deps := pluginDeps(logger)

	var engine *usm.Engine
	if needV3 {
		engine, err = buildTrapEngine(*engineName, *boots, *user,
			*authProto, *authPass, *privProto, *privPass, deps)
		if err != nil {
			return err
		}
	}

	sender := snmpprov.NewTrapSenderV3(dests, engine, deps)
	defer func() { _ = sender.Close() }()

	n := snmpprov.Notification{
		Enterprise: ent,
		AgentAddr:  parseIPArg(*agentAddr),
		Generic:    codec.GenericTrap(*generic),
		Specific:   *specific,
		Uptime:     uint32(*uptime),
	}
	if err := sender.Send(ctx, n); err != nil {
		return err
	}
	for _, d := range sender.Destinations() {
		fmt.Printf("sent %s to %s\n", d.Version, d.Addr)
	}
	return nil
}

// parseTrapDestinations reads the --to list.
//
// One string per receiver rather than a flag per field, because a plant
// mid-migration has receivers that disagree about the version and the
// password, and repeating four flags per destination is how a
// destination gets configured with another's community.
func parseTrapDestinations(list string) ([]snmpprov.TrapDestination, bool, error) {
	var out []snmpprov.TrapDestination
	needV3 := false

	for _, spec := range strings.Split(list, ",") {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		parts := strings.Split(spec, "/")
		addr := parts[0]
		if _, _, err := net.SplitHostPort(addr); err != nil {
			addr = net.JoinHostPort(addr, strconv.Itoa(snmpprov.TrapPort))
		}

		d := snmpprov.TrapDestination{Addr: addr, Version: codec.Version2c, Community: "public"}
		if len(parts) > 1 {
			switch strings.ToLower(parts[1]) {
			case "1", "v1":
				d.Version = codec.Version1
			case "2c", "v2c", "2":
				d.Version = codec.Version2c
			case "3", "v3":
				d.Version = codec.Version3
				needV3 = true
			default:
				return nil, false, fmt.Errorf(
					"snmp trap: %q names version %q (expected 1, 2c or 3)", spec, parts[1])
			}
		}
		if len(parts) > 2 {
			if d.Version == codec.Version3 {
				d.User = parts[2]
			} else {
				d.Community = parts[2]
			}
		}
		if d.Version == codec.Version3 && d.User == "" {
			return nil, false, fmt.Errorf(
				"snmp trap: %q is a v3 destination and names no user", spec)
		}
		out = append(out, d)
	}
	if len(out) == 0 {
		return nil, false, fmt.Errorf("snmp trap: --to named no receiver")
	}
	return out, needV3, nil
}

// buildTrapEngine assembles the local USM engine for v3 destinations.
func buildTrapEngine(engineName string, boots int, user,
	authProto, authPass, privProto, privPass string, deps plugin.Deps) (*usm.Engine, error) {
	if user == "" {
		return nil, fmt.Errorf("snmp trap: a v3 destination needs --user")
	}
	id, err := usm.NewEngineID(usm.Enterprise, engineName)
	if err != nil {
		return nil, err
	}
	e, err := usm.NewEngine(id, int32(boots), deps.Clock)
	if err != nil {
		return nil, err
	}
	auth, err := parseAuthProtocol(authProto)
	if err != nil {
		return nil, err
	}
	priv, err := parsePrivProtocol(privProto)
	if err != nil {
		return nil, err
	}
	if err := e.AddUser(usm.User{
		Name: user, Auth: auth, AuthPass: authPass, Priv: priv, PrivPass: privPass,
	}); err != nil {
		return nil, err
	}
	return e, nil
}

func parseAuthProtocol(s string) (usm.AuthProtocol, error) {
	switch strings.ToLower(s) {
	case "", "none":
		return usm.NoAuth, nil
	case "md5":
		return usm.HMACMD5, nil
	case "sha", "sha1":
		return usm.HMACSHA, nil
	case "sha224":
		return usm.HMACSHA224, nil
	case "sha256":
		return usm.HMACSHA256, nil
	case "sha384":
		return usm.HMACSHA384, nil
	case "sha512":
		return usm.HMACSHA512, nil
	}
	return usm.NoAuth, fmt.Errorf("snmp: unknown authentication protocol %q", s)
}

func parsePrivProtocol(s string) (usm.PrivProtocol, error) {
	switch strings.ToLower(s) {
	case "", "none":
		return usm.NoPriv, nil
	case "des":
		return usm.DESCBC, nil
	case "aes", "aes128":
		return usm.AES128CFB, nil
	}
	return usm.NoPriv, fmt.Errorf("snmp: unknown privacy protocol %q", s)
}

// parseIPArg reads an IPv4 address, or nothing.
func parseIPArg(s string) net.IP {
	if s == "" {
		return nil
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return nil
	}
	return ip.To4()
}

// runSNMPTrapListen receives notifications until interrupted.
func runSNMPTrapListen(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("trap-listen", flag.ContinueOnError)
	bind := fs.String("bind", fmt.Sprintf(":%d", snmpcons.TrapPort),
		"address to listen on. Port 162 needs privilege on Linux.")
	communities := fs.String("community", "",
		"comma-separated v1/v2c communities to accept. EMPTY ACCEPTS ANY, which is what a diagnostic listener wants and a production one must not have.")
	user := fs.String("user", "", "USM user name to accept v3 notifications as")
	authProto := fs.String("auth", "", "v3 authentication: md5, sha, sha224, sha256, sha384 or sha512")
	authPass := fs.String("auth-pass", "", "v3 authentication password")
	privProto := fs.String("priv", "", "v3 privacy: des or aes")
	privPass := fs.String("priv-pass", "", "v3 privacy password")
	engineName := fs.String("engine-id", "dhs-agent",
		"name in the SENDER's engine ID; a v3 notification is authenticated as the sender's engine, so this must match what it uses")
	boots := fs.Int("engine-boots", 1, "the sender's engine boot count")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}

	logger, _, logClean, _ := consumerLogger(ctx, "snmp", *bind, "trap-listen")
	defer logClean()
	deps := pluginDeps(logger)

	opts := snmpcons.ListenerOptions{
		Addr:       *bind,
		Compliance: &compliance.Profile{},
	}
	for _, c := range strings.Split(*communities, ",") {
		if c = strings.TrimSpace(c); c != "" {
			opts.Communities = append(opts.Communities, c)
		}
	}
	if *user != "" {
		engine, err := buildTrapEngine(*engineName, *boots, *user,
			*authProto, *authPass, *privProto, *privPass, deps)
		if err != nil {
			return err
		}
		opts.Engine = engine
	}
	if len(opts.Communities) == 0 {
		fmt.Fprintln(os.Stderr,
			"snmp trap-listen: accepting ANY community (--community narrows it)")
	}

	l := snmpcons.NewListener(opts, deps)
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	err := l.Listen(ctx, func(t snmpcons.Trap) {
		fmt.Printf("%s %s\n", time.Now().UTC().Format(time.RFC3339), t)
		for _, vb := range t.VarBinds {
			fmt.Printf("    %s = %s\n", mib.Name(vb.Name), vb.Value)
		}
	})
	if prof, ok := opts.Compliance.(*compliance.Profile); ok {
		printSNMPCompliance(prof)
	}
	return err
}
