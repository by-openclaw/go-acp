package main

// `dhs consumer snmp` and `dhs producer snmp` — the operator's side of
// internal/snmp.
//
// The verbs are named the way net-snmp names them, deliberately: an
// engineer who has typed snmpget and snmpwalk for twenty years should
// not have to learn a second vocabulary to use this one.

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"dhs/internal/consumer/compliance"
	"dhs/internal/snmp/codec"
	snmpcons "dhs/internal/snmp/consumer"
	"dhs/internal/snmp/mib"
)

// runSNMPConsumer dispatches `dhs consumer snmp <verb> [args]`.
// runSNMPConsumer answers the verbs that are SNMP's own shape and says
// so; handled=false hands the rest to the neutral dispatcher, where the
// registered plugin answers tree / export / watch / alarm like any
// other connector.
func runSNMPConsumer(ctx context.Context, args []string) (bool, error) {
	var lf *logFlags
	lf, args = stripLogFlags(args)
	ctx = withLogFlags(ctx, lf)

	if len(args) == 0 || isHelpToken(args[0]) {
		printSNMPConsumerHelp()
		return true, nil
	}
	verb := args[0]
	rest := args[1:]
	switch verb {
	case "get":
		// get / set exist in both shapes. SNMP's own speaks OIDs and
		// MIB names (--oid sysDescr.0); the neutral one speaks the
		// paths a walk produced (--path ateme.dr5000.…). Whichever the
		// operator named is the one they meant.
		if namesAPath(rest) {
			return false, nil
		}
		return true, runSNMPGet(ctx, rest)
	case "walk":
		// Same rule as get/set. SNMP's own walk speaks subtrees
		// (--oid 1.3.6.1.4.1.27338, --limit); the neutral one walks a
		// SLOT and writes the device model to the DM cache, which is
		// what every other connector's walk does and what an alarm
		// template and a fixture are keyed by.
		if namesASlot(rest) {
			return false, nil
		}
		return true, runSNMPWalk(ctx, rest)
	case "set":
		if namesAPath(rest) {
			return false, nil
		}
		return true, runSNMPSet(ctx, rest)
	case "trap-listen", "listen":
		return true, runSNMPTrapListen(ctx, rest)
	case "validate":
		return true, runValidate(ctx, append([]string{"--protocol", "snmp"}, rest...))
	}
	return false, nil
}

// runSNMPProducer dispatches `dhs producer snmp <verb> [args]`.
func runSNMPProducer(ctx context.Context, args []string) error {
	var lf *logFlags
	lf, args = stripLogFlags(args)
	ctx = withLogFlags(ctx, lf)

	if len(args) == 0 || isHelpToken(args[0]) {
		printSNMPProducerHelp()
		return nil
	}
	verb := args[0]
	rest := args[1:]
	switch verb {
	case "serve":
		return runSNMPServe(ctx, rest)
	case "trap":
		return runSNMPTrapSend(ctx, rest)
	case "inform":
		return runSNMPInform(ctx, rest)
	case "mib":
		return runSNMPMIB(ctx, rest)
	}
	return fmt.Errorf("producer snmp: unknown verb %q (expected: serve | trap | inform | mib)", verb)
}

// snmpFlags are what every consumer verb needs to reach an agent.
type snmpFlags struct {
	version   string
	community string
	timeout   time.Duration
	retries   int
	bulk      int
	prefer    string

	// v3 credentials. A user with no passwords is noAuthNoPriv, which
	// identifies the manager without protecting the exchange; adding
	// --auth makes it authNoPriv and --priv authPriv.
	user      string
	authProto string
	authPass  string
	privProto string
	privPass  string
	context   string
}

func (f *snmpFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&f.version, "version", "2c", "SNMP version: 1 or 2c. The Tandberg IRDs in this lab answer v1 ONLY; v2c gets no reply at all from them. The ATEME DR5000 answers both — prefer 2c there, it has GETBULK.")
	fs.StringVar(&f.community, "community", "public", "read community (write community for `set`)")
	fs.DurationVar(&f.timeout, "timeout", snmpcons.DefaultTimeout, "per-request timeout")
	fs.IntVar(&f.retries, "retries", snmpcons.DefaultRetries, "how many times to repeat an unanswered request; UDP loses datagrams")
	fs.IntVar(&f.bulk, "max-repetitions", snmpcons.DefaultMaxRepetitions, "GETBULK window for `walk` (v2c only)")
	fs.StringVar(&f.prefer, "mib", "", "comma-separated MIB modules to name objects from first, where two devices name one OID differently — the TT1260 and RX1290 report the same sysObjectID (e.g. ETV-TT1260-MIB)")
	fs.StringVar(&f.user, "user", "", "v3 USM user name (or SNMP_V3_USER). v3 has no community: it authenticates as a user")
	fs.StringVar(&f.authProto, "auth", "", "v3 authentication: md5, sha, sha224, sha256, sha384 or sha512")
	fs.StringVar(&f.authPass, "auth-pass", "", "v3 authentication password (prefer SNMP_V3_AUTH_PASS — a password on a command line is in the shell history and in ps)")
	fs.StringVar(&f.privProto, "priv", "", "v3 privacy: des or aes")
	fs.StringVar(&f.privPass, "priv-pass", "", "v3 privacy password (prefer SNMP_V3_PRIV_PASS)")
	fs.StringVar(&f.context, "context", "", "v3 context name; empty is the agent's default context")
}

// v3Env fills anything the flags left empty from the environment, the
// same way the communities are taken. A password on a command line is
// in the shell history and visible in ps to every user on the host.
func (f *snmpFlags) v3Env() {
	for _, p := range []struct {
		field *string
		env   string
	}{
		{&f.user, "SNMP_V3_USER"},
		{&f.authProto, "SNMP_V3_AUTH"},
		{&f.authPass, "SNMP_V3_AUTH_PASS"},
		{&f.privProto, "SNMP_V3_PRIV"},
		{&f.privPass, "SNMP_V3_PRIV_PASS"},
		{&f.context, "SNMP_V3_CONTEXT"},
	} {
		if *p.field == "" {
			*p.field = strings.TrimSpace(os.Getenv(p.env))
		}
	}
}

// credentials builds the v3 user from the flags and the environment.
func (f *snmpFlags) credentials() (*snmpcons.V3, error) {
	f.v3Env()
	if f.user == "" {
		return nil, fmt.Errorf("snmp: v3 authenticates as a USER — pass --user or set SNMP_V3_USER")
	}
	auth, err := parseAuthProtocol(f.authProto)
	if err != nil {
		return nil, err
	}
	priv, err := parsePrivProtocol(f.privProto)
	if err != nil {
		return nil, err
	}
	return &snmpcons.V3{
		User: f.user, Auth: auth, AuthPass: f.authPass,
		Priv: priv, PrivPass: f.privPass, Context: f.context,
	}, nil
}

// modules is --mib as a list.
func (f *snmpFlags) modules() []string {
	var out []string
	for _, m := range strings.Split(f.prefer, ",") {
		if m = strings.TrimSpace(m); m != "" {
			out = append(out, m)
		}
	}
	return out
}

// options builds the session options, refusing a version this manager
// does not speak rather than defaulting quietly to one it does.
func (f *snmpFlags) options(addr string, prof compliance.Recorder) (snmpcons.Options, error) {
	var v codec.Version
	switch strings.ToLower(f.version) {
	case "1", "v1":
		v = codec.Version1
	case "2c", "v2c", "2":
		v = codec.Version2c
	case "3", "v3":
		v = codec.Version3
	default:
		return snmpcons.Options{}, fmt.Errorf("snmp: unknown version %q (expected 1, 2c or 3)", f.version)
	}
	opts := snmpcons.Options{
		Addr: addr, Version: v, Community: f.community,
		Timeout: f.timeout, Retries: f.retries, MaxRepetitions: f.bulk,
		Compliance: prof,
	}
	if v == codec.Version3 {
		cred, err := f.credentials()
		if err != nil {
			return snmpcons.Options{}, err
		}
		opts.V3 = cred
	}
	return opts, nil
}

// hostArg takes the one positional argument every consumer verb needs.
func hostArg(fs *flag.FlagSet, verb string) (string, error) {
	if fs.NArg() < 1 {
		return "", fmt.Errorf("snmp %s: needs an agent address (host or host:port)", verb)
	}
	return fs.Arg(0), nil
}

// runSNMPGet reads named objects.
func runSNMPGet(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	var f snmpFlags
	f.register(fs)
	oids := fs.String("oid", "", "comma-separated objects, by standard name or dotted number (e.g. sysDescr.0,1.3.6.1.4.1.7995.1)")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	host, err := hostArg(fs, "get")
	if err != nil {
		return err
	}
	names, err := resolveOIDs(*oids)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		// The system group is what a manager reads first, so it is what
		// `get` with no --oid means.
		names = []codec.OID{mib.SysDescr, mib.SysObjectID, mib.SysUpTime,
			mib.SysContact, mib.SysName, mib.SysLocation}
	}

	prof := &compliance.Profile{}
	s, err := dialSNMP(ctx, &f, host, prof)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	binds, err := s.Get(ctx, names...)
	if err != nil {
		return err
	}
	printBinds(binds, f.modules())
	printSNMPCompliance(prof)
	return nil
}

// runSNMPWalk discovers a subtree.
func runSNMPWalk(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("walk", flag.ContinueOnError)
	var f snmpFlags
	f.register(fs)
	root := fs.String("oid", "1.3.6.1.2.1", "subtree root, by standard name or dotted number")
	limit := fs.Int("limit", snmpcons.DefaultWalkLimit, "stop after this many objects; a device whose table grows while it is walked would otherwise never end")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	host, err := hostArg(fs, "walk")
	if err != nil {
		return err
	}
	start, err := mib.Resolve(*root)
	if err != nil {
		return err
	}

	prof := &compliance.Profile{}
	s, err := dialSNMP(ctx, &f, host, prof)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	n := 0
	prefer := f.modules()
	walkErr := s.Walk(ctx, start, func(vb codec.VarBind) error {
		n++
		writeBind(w, vb, prefer)
		if n >= *limit {
			return fmt.Errorf("snmp: stopped at the --limit of %d objects", *limit)
		}
		return nil
	})
	_ = w.Flush()
	fmt.Fprintf(os.Stderr, "%d object(s)\n", n)
	printSNMPCompliance(prof)
	return walkErr
}

// runSNMPSet writes.
//
// A refused SET is usually the DEVICE rather than the request. Most
// agents gate control separately from reads — the Tandberg IRDs and the
// Snell frames both do — so a device that answers every GET can still
// refuse every write until somebody enables it on the front panel or the
// management page. The help below says so, because that is the first
// thing anyone hitting this needs to check and the last thing the error
// text can tell them.
func runSNMPSet(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("set", flag.ContinueOnError)
	var f snmpFlags
	f.register(fs)
	oid := fs.String("oid", "", "the object to write, by standard name or dotted number")
	typ := fs.String("type", "s", "value type: i(nteger) s(tring) o(id) a(ddress) u(nsigned) t(imeticks) — the net-snmp letters")
	value := fs.String("value", "", "the value to write")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(fs.Output(), `dhs consumer snmp set — write one object

A SET needs the WRITE community in --community, which is NOT the read
one. Get that wrong on a Tandberg IRD and you get no answer at all — the
agent drops a request it will not serve (RFC 1157 §4.1) rather than
explaining, so it looks exactly like a device that is switched off.
Theirs is "private".

Most agents also gate CONTROL separately from reads, so a device that
answers every GET can still refuse every write. On the Tandberg IRDs
that gate is an object you can write:

  controlMode  1.3.6.1.4.1.1773.1.3.200.1.11.0
               fp(1) serial(2) ncp(3) snmp(4) web(5)

and its MIB says it "may always be written to using SNMP" — so a
receiver left on the front panel can be taken back over the network,
without a trip to the rack:

  dhs consumer snmp set --version 1 --community private       --oid 1.3.6.1.4.1.1773.1.3.200.1.11.0 --type i --value 4 <host>

The Snell frames have per-slot "SNMP Control" checkboxes on the RollCall
page instead. Either way a readOnly, notWritable or noSuchName from a
device that reads fine is the DEVICE, not this tool.

FLAGS`)
		fs.PrintDefaults()
	}
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	host, err := hostArg(fs, "set")
	if err != nil {
		return err
	}
	if *oid == "" {
		return fmt.Errorf("snmp set: --oid names what to write")
	}
	name, err := mib.Resolve(*oid)
	if err != nil {
		return err
	}
	v, err := parseSNMPValue(*typ, *value)
	if err != nil {
		return err
	}

	prof := &compliance.Profile{}
	s, err := dialSNMP(ctx, &f, host, prof)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	binds, err := s.Set(ctx, codec.VarBind{Name: name, Value: v})
	if err != nil {
		return err
	}
	// The echo is what the device actually took, which is not always
	// what was asked for.
	printBinds(binds, f.modules())
	printSNMPCompliance(prof)
	return nil
}

// dialSNMP opens the session, with the uniform logging every connector
// has.
func dialSNMP(ctx context.Context, f *snmpFlags, host string,
	prof compliance.Recorder) (*snmpcons.Session, error) {
	opts, err := f.options(host, prof)
	if err != nil {
		return nil, err
	}
	logger, _, _, _ := consumerLogger(ctx, "snmp", host, "get")
	return snmpcons.Dial(ctx, opts, pluginDeps(logger))
}

// resolveOIDs turns a comma-separated list into OIDs.
func resolveOIDs(list string) ([]codec.OID, error) {
	if strings.TrimSpace(list) == "" {
		return nil, nil
	}
	var out []codec.OID
	for _, s := range strings.Split(list, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		o, err := mib.Resolve(s)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, nil
}

// parseSNMPValue takes net-snmp's type letters, because that is what an
// operator's fingers already know.
func parseSNMPValue(typ, raw string) (codec.Value, error) {
	switch strings.ToLower(typ) {
	case "s", "string", "octet":
		return codec.String(raw), nil
	case "i", "int", "integer":
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return codec.Value{}, fmt.Errorf("snmp: %q is not an integer", raw)
		}
		return codec.Int(n), nil
	case "u", "unsigned", "gauge":
		n, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			return codec.Value{}, fmt.Errorf("snmp: %q is not an unsigned number", raw)
		}
		return codec.Gauge32(uint32(n)), nil
	case "t", "timeticks":
		n, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			return codec.Value{}, fmt.Errorf("snmp: %q is not a tick count", raw)
		}
		return codec.TimeTicks(uint32(n)), nil
	case "o", "oid":
		o, err := mib.Resolve(raw)
		if err != nil {
			return codec.Value{}, err
		}
		return codec.ObjectID(o), nil
	case "a", "address", "ipaddress":
		ip := parseIPArg(raw)
		if ip == nil {
			return codec.Value{}, fmt.Errorf("snmp: %q is not an IPv4 address", raw)
		}
		return codec.IPAddress(ip), nil
	}
	return codec.Value{}, fmt.Errorf(
		"snmp: unknown type %q (expected i, s, o, a, u or t)", typ)
}

// printBinds renders a result the way snmpget does: one object a line,
// name, type, value.
func printBinds(binds []codec.VarBind, prefer []string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, vb := range binds {
		writeBind(w, vb, prefer)
	}
	_ = w.Flush()
}

// writeBind is one line of printBinds or walk.
func writeBind(w io.Writer, vb codec.VarBind, prefer []string) {
	name, obj := mib.Describe(vb.Name, prefer...)
	_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", name, vb.Value.Type, displayValue(vb.Value, obj, prefer))
}

// displayValue renders a value the way net-snmp does: an enumerated
// INTEGER as its MIB label with the number, snmp(4), and an OBJECT
// IDENTIFIER by name, sysObjectID.0 = dhsAgent. The bare number is what an
// operator would otherwise have to look up.
func displayValue(v codec.Value, obj *mib.Object, prefer []string) string {
	switch v.Type {
	case codec.TypeInteger:
		if obj != nil {
			if label, ok := obj.EnumName(v.Int); ok {
				return fmt.Sprintf("%s(%d)", label, v.Int)
			}
		}
	case codec.TypeOID:
		name, _ := mib.Describe(v.OID, prefer...)
		return name
	}
	return v.String()
}

// printSNMPCompliance reports what the peer did that the RFCs do not
// describe. Silent when it did nothing — a clean run should look clean.
func printSNMPCompliance(p *compliance.Profile) {
	if line := p.SummaryLine(); line != "" {
		_, _ = fmt.Fprintf(os.Stderr, "compliance: %s (%s)\n", line, p.Classification())
	}
}

func printSNMPConsumerHelp() {
	fmt.Println(`dhs consumer snmp — poll an agent, and listen for what it sends unasked

VERBS
  get           read named objects (default: the RFC 1213 system group)
  walk          discover a subtree; GETBULK under v2c, GETNEXT under v1
  set           write one object
  trap-listen   receive notifications, in any version
  validate      decode a captured frames.jsonl offline

VERSIONS
  --version 1 | 2c | 3. The Tandberg IRDs in this lab answer v1 ONLY —
  v2c gets no reply at all from them, which looks exactly like a device
  that is down. The ATEME DR5000 answers v2c as well, and v2c has
  GETBULK.

  v3 authenticates as a USER rather than with a community, so it needs
  --user (or SNMP_V3_USER) and, for anything above noAuthNoPriv,
  --auth/--auth-pass and --priv/--priv-pass. The manager discovers the
  agent's engine first (RFC 3414 §4) and re-discovers by itself if the
  agent reboots mid-session.

  The neutral verbs — info, tree, walk, watch, alarm — take the same
  credential from SNMP_V3_* and then prefer v3 over v2c and v1, because
  v1 and v2c put a password in clear on every datagram.

EXAMPLES
  # what a device says it is
  dhs consumer snmp get 10.6.255.113

  # the Snell frame's own tree (enterprise 7995)
  dhs consumer snmp walk --oid 1.3.6.1.4.1.7995 10.6.255.113

  # a Tandberg IRD, which is v1-only
  dhs consumer snmp get --version 1 --oid sysDescr.0 10.6.255.110

  # Cerebrum's agent, which answers on 1161 rather than 161
  dhs consumer snmp walk 10.6.250.5:1161

  # name the device from the NMS
  dhs consumer snmp set --community private --oid sysLocation.0       --type s --value "TEC RACK 23" 10.6.255.113

  # listen for alarms from anything, then narrow it
  dhs consumer snmp trap-listen --bind :1162
  dhs consumer snmp trap-listen --bind :1162 --community public

  # and for v3 notifications, as the sender's engine
  dhs consumer snmp trap-listen --bind :1162 --user operator       --auth sha256 --auth-pass '...' --priv aes --priv-pass '...'

  # poll over v3, authenticated and encrypted. The passwords belong in
  # the environment: a password on a command line is in the shell
  # history and visible in ps to everyone on the host.
  export SNMP_V3_USER=operator SNMP_V3_AUTH=sha256 SNMP_V3_PRIV=aes
  export SNMP_V3_AUTH_PASS=... SNMP_V3_PRIV_PASS=...
  dhs consumer snmp get --version 3 --oid sysDescr.0 10.6.255.114

  # the neutral verbs take the same credential and prefer v3 with it
  dhs consumer snmp info 10.6.255.114`)
}

func printSNMPProducerHelp() {
	fmt.Println(`dhs producer snmp — BE an agent, and emit notifications

VERBS
  serve   answer polls against a served MIB
  trap    send one notification to one or more receivers
  inform  the same notification, ACKNOWLEDGED: retried until each
          receiver answers, and it says which one did not
  mib     write DHS-MIB, the module defining what the agent serves and sends
          under BY-SYSTEMS' IANA enterprise number 54981, for a manager to load
  status  runtime snapshot of a serving instance (--url)
  stop    stop one keyed on --pidfile
  ensure  converge to --state present|absent

EXAMPLES
  # an agent on a high port, which needs no privilege
  dhs producer snmp serve --bind 0.0.0.0:1161 --location "TEC RACK 23"

  # writable, deliberately: an empty --write-community refuses every SET
  dhs producer snmp serve --bind 0.0.0.0:1161 --write-community private

  # prove a receiver is listening, in whichever version it speaks
  dhs producer snmp trap --to 10.6.250.5/2c/public
  dhs producer snmp trap --to 10.6.255.9:162/1/public,10.6.250.5/2c/public
  dhs producer snmp trap --to 10.6.250.7/3/operator       --user operator --auth sha256 --auth-pass '...' --priv aes --priv-pass '...'

  # an alarm you need to KNOW arrived: retried until acknowledged
  dhs producer snmp inform --to 10.6.250.5/2c/public
  dhs producer snmp inform --to 10.6.250.7/3/operator       --user operator --auth sha256 --auth-pass '...' --priv aes --priv-pass '...'

  # the module a receiver loads to name what it gets from us
  dhs producer snmp mib --out DHS-MIB.mib

NOTE
  Every trap destination on the devices in docs/testbed.md currently
  points at an address that no longer exists, so they emit to nobody.
  ` + "`trap`" + ` is how a receiver is proven before anything depends on it.`)
}

// namesASlot reports whether the operator asked for the neutral walk:
// a device model for one slot, rather than an OID subtree.
func namesASlot(args []string) bool {
	for _, a := range args {
		if a == "--slot" || a == "-slot" || strings.HasPrefix(a, "--slot=") || strings.HasPrefix(a, "-slot=") {
			return true
		}
	}
	return false
}

// namesAPath reports whether the operator addressed the object the
// neutral way — by the path or the label a walk produced.
func namesAPath(args []string) bool {
	for _, a := range args {
		if a == "--path" || a == "-path" || a == "--label" || a == "-label" ||
			strings.HasPrefix(a, "--path=") || strings.HasPrefix(a, "--label=") {
			return true
		}
	}
	return false
}

// snmpV3FromEnv builds the neutral connector's v3 credential from the
// environment. It is how `info`, `tree`, `walk`, `watch` and `alarm`
// get one: those verbs are protocol-neutral and have no SNMP flags, so
// the credential arrives the same way the communities do.
//
// Nil means "no user configured", which leaves the connector on v2c/v1
// — there is no anonymous v3 to fall back to.
func snmpV3FromEnv() *snmpcons.V3 {
	f := &snmpFlags{}
	f.v3Env()
	if f.user == "" {
		return nil
	}
	cred, err := f.credentials()
	if err != nil {
		// A half-configured credential is worth saying out loud: the
		// alternative is a connector that quietly polls v2c while an
		// operator believes it is authenticating.
		fmt.Fprintf(os.Stderr, "warning: SNMP_V3_* ignored: %v\n", err)
		return nil
	}
	return cred
}
