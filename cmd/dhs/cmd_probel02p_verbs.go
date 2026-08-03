package main

// SW-P-02 consumer subcommands. CLI plumbing only — every verb here
// dispatches to an existing Send* method on the probel-sw02p consumer
// Plugin (internal/probel-sw02p/consumer). No new wire commands are
// introduced; this file mirrors the SW-P-08 CLI shape
// (cmd_probel.go / cmd_probel_salvo.go): popPositional <host:port>, a
// per-command flag.FlagSet, a single round-trip, and a one-line decoded
// reply on stdout (wire hex is logged on stderr by the codec client).
//
// SW-P-02 is single-matrix / single-level on the wire (§3.1), so unlike
// SW-P-08 there are no --matrix / --level flags on these verbs; the
// global --mtx-id / --level / --dsts / --srcs flags parsed at the
// dispatcher feed the connection's MatrixConfig (bootstrap sweep +
// keep-alive) and are documented in helpProbelSW02.

import (
	"context"
	"flag"
	"fmt"
	"sync"
	"time"

	codec02 "dhs/internal/probel-sw02p/codec"
)

// probelSW02Flags is the common "host:port + dst/src + extended" tuple
// shared by interrogate / connect.
type probelSW02Flags struct {
	addr      string
	dst       int
	src       int
	extended  bool
	badSource bool
	timeout   time.Duration
	check     bool
	output    string
}

func parseProbelSW02Flags(args []string, want struct{ src, badSource bool }) (probelSW02Flags, error) {
	fs := flag.NewFlagSet("probel-sw02p", flag.ContinueOnError)
	pf := probelSW02Flags{}
	fs.IntVar(&pf.dst, "dst", 0, "destination id (0-16383)")
	fs.IntVar(&pf.src, "src", 0, "source id (0-16383)")
	fs.BoolVar(&pf.extended, "extended", false, "use the Extended wire form (rx 65/66, 14-bit dst/src)")
	if want.badSource {
		fs.BoolVar(&pf.badSource, "bad-source", false, "set the narrow Multiplier bad-source bit (rx 02 only; ignored when --extended)")
	}
	fs.DurationVar(&pf.timeout, "timeout", 5*time.Second, "operation timeout")
	fs.BoolVar(&pf.check, "check", false, "dry-run: report would_change, send nothing (ADR-0007)")
	fs.StringVar(&pf.output, "output", "text", "output format: text | json (ADR-0002)")
	addr, flagArgs := popPositional(args)
	if addr == "" {
		return pf, fmt.Errorf("missing <host:port>")
	}
	if err := fs.Parse(flagArgs); err != nil {
		return pf, err
	}
	pf.addr = addr
	if pf.dst < 0 || pf.dst > 0x3FFF {
		return pf, fmt.Errorf("--dst out of range (0-16383)")
	}
	if want.src && (pf.src < 0 || pf.src > 0x3FFF) {
		return pf, fmt.Errorf("--src out of range (0-16383)")
	}
	return pf, nil
}

func runProbelsw02pInterrogate(ctx context.Context, args []string) error {
	pf, err := parseProbelSW02Flags(args, struct{ src, badSource bool }{})
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, pf.timeout)
	defer cancel()
	p, closer, err := dialProbelSW02(cctx, pf.addr)
	if err != nil {
		return err
	}
	defer closer()
	if pf.extended {
		reply, err := p.SendExtendedInterrogate(cctx, uint16(pf.dst))
		if err != nil {
			return err
		}
		fmt.Printf("extended tally  dst=%d → src=%d bad_source=%v update_off=%v\n",
			reply.Destination, reply.Source, reply.BadSource, reply.UpdateOff)
		return nil
	}
	reply, err := p.SendInterrogate(cctx, uint16(pf.dst))
	if err != nil {
		return err
	}
	fmt.Printf("tally  dst=%d → src=%d bad_source=%v\n",
		reply.Destination, reply.Source, reply.BadSource)
	return nil
}

// runProbelsw02pConnect converges dst to carry --src, idempotently (ADR-0007):
// it interrogates the current source and only sends a connect when the dst is
// not already routed from --src (and same bad-source bit). --check dry-runs;
// --output json emits {changed|would_change, diff[]} — the same shape every
// other protocol uses, so Ansible treats sw02 identically.
func runProbelsw02pConnect(ctx context.Context, args []string) error {
	pf, err := parseProbelSW02Flags(args, struct{ src, badSource bool }{src: true, badSource: true})
	if err != nil {
		return err
	}
	jsonOut, oerr := resolveEnsureOutput(pf.output, false)
	if oerr != nil {
		return oerr
	}
	cctx, cancel := context.WithTimeout(ctx, pf.timeout)
	defer cancel()
	p, closer, err := dialProbelSW02(cctx, pf.addr)
	if err != nil {
		return err
	}
	defer closer()

	// Read current source (read-back) for the idempotency decision.
	var curSrc int
	var curBad bool
	if pf.extended {
		cur, ierr := p.SendExtendedInterrogate(cctx, uint16(pf.dst))
		if ierr != nil {
			return ierr
		}
		curSrc, curBad = int(cur.Source), cur.BadSource
	} else {
		cur, ierr := p.SendInterrogate(cctx, uint16(pf.dst))
		if ierr != nil {
			return ierr
		}
		curSrc, curBad = int(cur.Source), cur.BadSource
	}
	field := fmt.Sprintf("xpoint.%d", pf.dst)
	from := fmt.Sprintf("src=%d bad_source=%v", curSrc, curBad)
	to := fmt.Sprintf("src=%d bad_source=%v", pf.src, pf.badSource)
	changed := curSrc != pf.src || curBad != pf.badSource

	if pf.check {
		return emitEnsure(jsonOut, ensureResult{WouldChange: &changed, Current: from, Target: to, Diff: ensureFieldDiff(changed, field, from, to)})
	}
	if !changed {
		return emitEnsure(jsonOut, ensureResult{Changed: &changed, Previous: from, Current: from, Diff: []ensureDiff{}})
	}

	var now string
	if pf.extended {
		reply, cerr := p.SendExtendedConnect(cctx, uint16(pf.dst), uint16(pf.src))
		if cerr != nil {
			return cerr
		}
		now = fmt.Sprintf("src=%d bad_source=%v", reply.Source, reply.BadSource)
	} else {
		reply, cerr := p.SendConnect(cctx, uint16(pf.dst), uint16(pf.src), pf.badSource)
		if cerr != nil {
			return cerr
		}
		now = fmt.Sprintf("src=%d bad_source=%v", reply.Source, reply.BadSource)
	}
	applied := true
	return emitEnsure(jsonOut, ensureResult{Changed: &applied, Previous: from, Current: now, Diff: ensureFieldDiff(true, field, from, now)})
}

func runProbelsw02pConnectOnGo(ctx context.Context, args []string) error {
	pf, err := parseProbelSW02Flags(args, struct{ src, badSource bool }{src: true, badSource: true})
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, pf.timeout)
	defer cancel()
	p, closer, err := dialProbelSW02(cctx, pf.addr)
	if err != nil {
		return err
	}
	defer closer()
	ack, err := p.SendConnectOnGo(cctx, uint16(pf.dst), uint16(pf.src), pf.badSource)
	if err != nil {
		return err
	}
	fmt.Printf("connect-on-go staged  dst=%d src=%d (call `go --op set` to commit)\n",
		ack.Destination, ack.Source)
	return nil
}

func runProbelsw02pGo(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probel-sw02p-go", flag.ContinueOnError)
	op := fs.String("op", "set", "operation: set (commit pending buffer) | clear (discard)")
	timeout := fs.Duration("timeout", 5*time.Second, "operation timeout")
	addr, flagArgs := popPositional(args)
	if addr == "" {
		return fmt.Errorf("missing <host:port>")
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	goOp, err := parseGoOp(*op)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	p, closer, err := dialProbelSW02(cctx, addr)
	if err != nil {
		return err
	}
	defer closer()
	ack, err := p.SendGo(cctx, goOp)
	if err != nil {
		return err
	}
	fmt.Printf("go done  op=%s result=%s\n", *op, goOpName(ack.Operation))
	return nil
}

// runProbelsw02pSalvoConnect mirrors SW-P-08's salvo-connect: stage N
// crosspoints under a named group (rx 35 CONNECT ON GO GROUP SALVO),
// then fire the group atomically (rx 36 GO GROUP SALVO op=set), and
// optionally clear it. Each staged slot routes --src → one dst.
func runProbelsw02pSalvoConnect(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probel-sw02p-salvo-connect", flag.ContinueOnError)
	var (
		src       = fs.Int("src", 0, "source id to route to every dst (0-1023, narrow salvo)")
		dstsCSV   = fs.String("dsts", "", "destination ids (CSV or N-M range, e.g. '5' or '1,2,3' or '0-7')")
		salvoID   = fs.Int("salvo", 1, "salvo group id (0-127)")
		badSource = fs.Bool("bad-source", false, "set the narrow Multiplier bad-source bit on each staged slot")
		clear     = fs.Bool("clear", false, "send a final rx 36 op=clear to wipe the staged group after firing")
		timeout   = fs.Duration("timeout", 5*time.Second, "overall operation timeout")
	)
	addr, flagArgs := popPositional(args)
	if addr == "" {
		return fmt.Errorf("missing <host:port>")
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	dsts, err := parseDsts(*dstsCSV)
	if err != nil {
		return err
	}
	if len(dsts) == 0 {
		return fmt.Errorf("--dsts is required (e.g. --dsts 5 or --dsts 0-7)")
	}
	if *salvoID < 0 || *salvoID > 127 {
		return fmt.Errorf("--salvo out of range (0-127)")
	}

	cctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	p, closer, err := dialProbelSW02(cctx, addr)
	if err != nil {
		return err
	}
	defer closer()

	fmt.Printf("\n=== salvo-connect: src=%d salvo=%d dsts=%v ===\n\n", *src, *salvoID, dsts)

	// Phase 1: N × CONNECT ON GO GROUP SALVO (rx 35 → tx 37 ack).
	fmt.Printf("phase 1 — CONNECT ON GO GROUP SALVO × %d (one rx 35 per dst)\n", len(dsts))
	for _, dst := range dsts {
		ack, err := p.SendConnectOnGoGroupSalvo(cctx, uint16(dst), uint16(*src), uint8(*salvoID), *badSource)
		if err != nil {
			return fmt.Errorf("salvo-build dst=%d: %w", dst, err)
		}
		fmt.Printf("  dst=%-3d → tx 37 ack dst=%d src=%d salvo=%d\n",
			dst, ack.Destination, ack.Source, ack.SalvoID)
	}

	// Phase 2: GO GROUP SALVO op=set (rx 36 → tx 38 ack).
	fmt.Printf("\nphase 2 — GO GROUP SALVO op=set salvo=%d\n", *salvoID)
	setAck, err := p.SendGoGroupSalvo(cctx, codec02.GoOpSet, uint8(*salvoID))
	if err != nil {
		return fmt.Errorf("salvo-go set: %w", err)
	}
	fmt.Printf("  tx 38 go-done result=%s salvo=%d\n", goGroupResultName(setAck.Result), setAck.SalvoID)

	// Phase 3 (optional): GO GROUP SALVO op=clear.
	if *clear {
		fmt.Printf("\nphase 3 — GO GROUP SALVO op=clear salvo=%d\n", *salvoID)
		clearAck, err := p.SendGoGroupSalvo(cctx, codec02.GoOpClear, uint8(*salvoID))
		if err != nil {
			return fmt.Errorf("salvo-go clear: %w", err)
		}
		fmt.Printf("  tx 38 go-done result=%s salvo=%d\n", goGroupResultName(clearAck.Result), clearAck.SalvoID)
	}
	return nil
}

func runProbelsw02pProtectConnect(ctx context.Context, args []string) error {
	pf, device, err := parseProbelSW02ProtectFlags(args)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, pf.timeout)
	defer cancel()
	p, closer, err := dialProbelSW02(cctx, pf.addr)
	if err != nil {
		return err
	}
	defer closer()
	reply, err := p.SendExtendedProtectConnect(cctx, uint16(pf.dst), uint16(device))
	if err != nil {
		return err
	}
	fmt.Printf("protect connected  dst=%d device=%d state=%s\n",
		reply.Destination, reply.Device, protectStateName(reply.Protect))
	return nil
}

func runProbelsw02pProtectDisconnect(ctx context.Context, args []string) error {
	pf, device, err := parseProbelSW02ProtectFlags(args)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, pf.timeout)
	defer cancel()
	p, closer, err := dialProbelSW02(cctx, pf.addr)
	if err != nil {
		return err
	}
	defer closer()
	reply, err := p.SendExtendedProtectDisconnect(cctx, uint16(pf.dst), uint16(device))
	if err != nil {
		return err
	}
	fmt.Printf("protect disconnected  dst=%d device=%d state=%s\n",
		reply.Destination, reply.Device, protectStateName(reply.Protect))
	return nil
}

func runProbelsw02pProtectInterrogate(ctx context.Context, args []string) error {
	pf, _, err := parseProbelSW02ProtectFlags(args)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, pf.timeout)
	defer cancel()
	p, closer, err := dialProbelSW02(cctx, pf.addr)
	if err != nil {
		return err
	}
	defer closer()
	reply, err := p.SendExtendedProtectInterrogate(cctx, uint16(pf.dst))
	if err != nil {
		return err
	}
	fmt.Printf("protect tally  dst=%d → state=%s device=%d\n",
		reply.Destination, protectStateName(reply.Protect), reply.Device)
	return nil
}

func runProbelsw02pProtectDump(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probel-sw02p-protect-dump", flag.ContinueOnError)
	startDst := fs.Int("first-dst", 0, "first destination id to dump (0-16383)")
	count := fs.Int("count", 32, "number of protect entries to request (1-255; tx 100 fans out 32/frame)")
	collect := fs.Duration("collect", 1*time.Second, "time to collect tx 100 fan-out frames after the request")
	timeout := fs.Duration("timeout", 5*time.Second, "operation timeout")
	addr, flagArgs := popPositional(args)
	if addr == "" {
		return fmt.Errorf("missing <host:port>")
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if *startDst < 0 || *startDst > 0x3FFF {
		return fmt.Errorf("--first-dst out of range (0-16383)")
	}
	if *count < 1 || *count > 255 {
		return fmt.Errorf("--count out of range (1-255)")
	}
	cctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	p, closer, err := dialProbelSW02(cctx, addr)
	if err != nil {
		return err
	}
	defer closer()

	// rx 105 can trigger MULTIPLE tx 100 broadcasts (32 entries each);
	// subscribe to the typed fan-out before sending so it is captured.
	var mu sync.Mutex
	var entries int
	if err := p.SubscribeExtendedProtectTallyDump(func(dump codec02.ExtendedProtectTallyDumpParams) {
		mu.Lock()
		defer mu.Unlock()
		if dump.Reset {
			fmt.Println("  (controller reset sentinel — no entries)")
			return
		}
		for _, it := range dump.Entries {
			fmt.Printf("  dst=%d state=%s device=%d\n",
				it.Destination, protectStateName(it.Protect), it.Device)
			entries++
		}
	}); err != nil {
		return err
	}
	if err := p.SendExtendedProtectTallyDumpRequest(cctx, uint16(*startDst), uint8(*count)); err != nil {
		return err
	}
	// Wait for the fan-out (no synchronous reply on rx 105 itself).
	timer := time.NewTimer(*collect)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-cctx.Done():
	}
	mu.Lock()
	seen := entries
	mu.Unlock()
	fmt.Printf("protect tally-dump  first_dst=%d requested=%d entries_seen=%d\n",
		*startDst, *count, seen)
	return nil
}

func runProbelsw02pProtectName(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probel-sw02p-protect-name", flag.ContinueOnError)
	device := fs.Int("device", 0, "device id (0-1023)")
	timeout := fs.Duration("timeout", 5*time.Second, "operation timeout")
	addr, flagArgs := popPositional(args)
	if addr == "" {
		return fmt.Errorf("missing <host:port>")
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if *device < 0 || *device > 0x3FF {
		return fmt.Errorf("--device out of range (0-1023)")
	}
	cctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	p, closer, err := dialProbelSW02(cctx, addr)
	if err != nil {
		return err
	}
	defer closer()
	reply, err := p.SendProtectDeviceNameRequest(cctx, uint16(*device))
	if err != nil {
		return err
	}
	fmt.Printf("device %d name=%q\n", reply.Device, reply.Name)
	return nil
}

func runProbelsw02pDualStatus(ctx context.Context, args []string) error {
	addr, timeout, err := parseProbelSW02AddrOnly(args, "probel-sw02p-dual-status")
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	p, closer, err := dialProbelSW02(cctx, addr)
	if err != nil {
		return err
	}
	defer closer()
	r, err := p.SendDualControllerStatusRequest(cctx)
	if err != nil {
		return err
	}
	who := "MASTER"
	if r.Active == codec02.ActiveControllerSlave {
		who = "SLAVE"
	}
	fmt.Printf("dual-controller  active=%s idle_faulty=%v\n",
		who, r.IdleStatus == codec02.IdleControllerFaulty)
	return nil
}

func runProbelsw02pLockStatus(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probel-sw02p-lock-status", flag.ContinueOnError)
	controller := fs.String("controller", "lh", "controller to query: lh | rh")
	timeout := fs.Duration("timeout", 5*time.Second, "operation timeout")
	addr, flagArgs := popPositional(args)
	if addr == "" {
		return fmt.Errorf("missing <host:port>")
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	ctrl, err := parseController(*controller)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	p, closer, err := dialProbelSW02(cctx, addr)
	if err != nil {
		return err
	}
	defer closer()
	resp, err := p.SendSourceLockStatusRequest(cctx, ctrl)
	if err != nil {
		return err
	}
	locked := 0
	for _, ok := range resp.Locked {
		if ok {
			locked++
		}
	}
	fmt.Printf("source-lock (read-only)  controller=%s sources=%d locked=%d\n",
		*controller, len(resp.Locked), locked)
	for i, ok := range resp.Locked {
		fmt.Printf("  src=%d locked=%v\n", i, ok)
	}
	return nil
}

func runProbelsw02pStatus(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probel-sw02p-status", flag.ContinueOnError)
	controller := fs.String("controller", "lh", "controller to query: lh | rh")
	timeout := fs.Duration("timeout", 5*time.Second, "operation timeout")
	addr, flagArgs := popPositional(args)
	if addr == "" {
		return fmt.Errorf("missing <host:port>")
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	ctrl, err := parseController(*controller)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	p, closer, err := dialProbelSW02(cctx, addr)
	if err != nil {
		return err
	}
	defer closer()
	s, err := p.SendStatusRequest(cctx, ctrl)
	if err != nil {
		return err
	}
	fmt.Printf("status  controller=%s idle=%v bus_fault=%v overheat=%v\n",
		*controller, s.Idle, s.BusFault, s.Overheat)
	return nil
}

func runProbelsw02pRouterConfig(ctx context.Context, args []string) error {
	addr, timeout, err := parseProbelSW02AddrOnly(args, "probel-sw02p-router-config")
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	p, closer, err := dialProbelSW02(cctx, addr)
	if err != nil {
		return err
	}
	defer closer()
	cfg, err := p.SendRouterConfigRequest(cctx)
	if err != nil {
		return err
	}
	switch {
	case cfg.Response1 != nil:
		r := cfg.Response1
		fmt.Printf("router-config (response-1)  level_map=0x%07x levels=%d\n", r.LevelMap, len(r.Levels))
		for i, lvl := range r.Levels {
			fmt.Printf("  level[%d]  dsts=%d srcs=%d\n", i, lvl.NumDestinations, lvl.NumSources)
		}
	case cfg.Response2 != nil:
		r := cfg.Response2
		fmt.Printf("router-config (response-2)  level_map=0x%07x levels=%d\n", r.LevelMap, len(r.Levels))
		for i, lvl := range r.Levels {
			fmt.Printf("  level[%d]  dsts=%d srcs=%d start_dst=%d start_src=%d\n",
				i, lvl.NumDestinations, lvl.NumSources, lvl.StartDestination, lvl.StartSource)
		}
	default:
		fmt.Println("router-config  (empty response)")
	}
	return nil
}

// --- shared helpers ---

// parseProbelSW02ProtectFlags parses host:port + dst + device for the
// Extended PROTECT family (rx 101/102/104). SW-P-02 protect is keyed on
// (dst, device) only — no matrix/level on the wire.
func parseProbelSW02ProtectFlags(args []string) (probelSW02Flags, int, error) {
	fs := flag.NewFlagSet("probel-sw02p-protect", flag.ContinueOnError)
	var pf probelSW02Flags
	fs.IntVar(&pf.dst, "dst", 0, "destination id (0-16383)")
	device := 0
	fs.IntVar(&device, "device", 0, "device id (0-16383)")
	fs.DurationVar(&pf.timeout, "timeout", 5*time.Second, "operation timeout")
	addr, flagArgs := popPositional(args)
	if addr == "" {
		return pf, 0, fmt.Errorf("missing <host:port>")
	}
	if err := fs.Parse(flagArgs); err != nil {
		return pf, 0, err
	}
	pf.addr = addr
	if pf.dst < 0 || pf.dst > 0x3FFF {
		return pf, 0, fmt.Errorf("--dst out of range (0-16383)")
	}
	if device < 0 || device > 0x3FFF {
		return pf, 0, fmt.Errorf("--device out of range (0-16383)")
	}
	return pf, device, nil
}

// parseProbelSW02AddrOnly handles the zero-arg verbs (dual-status,
// router-config) that take only <host:port> + --timeout.
func parseProbelSW02AddrOnly(args []string, name string) (string, time.Duration, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	timeout := fs.Duration("timeout", 5*time.Second, "operation timeout")
	addr, flagArgs := popPositional(args)
	if addr == "" {
		return "", 0, fmt.Errorf("missing <host:port>")
	}
	if err := fs.Parse(flagArgs); err != nil {
		return "", 0, err
	}
	return addr, *timeout, nil
}

func parseGoOp(s string) (codec02.GoOperation, error) {
	switch s {
	case "set":
		return codec02.GoOpSet, nil
	case "clear":
		return codec02.GoOpClear, nil
	}
	return 0, fmt.Errorf("unknown --op %q (use set | clear)", s)
}

func goOpName(op codec02.GoOperation) string {
	switch op {
	case codec02.GoOpSet:
		return "set"
	case codec02.GoOpClear:
		return "clear"
	}
	return fmt.Sprintf("0x%02x", byte(op))
}

func goGroupResultName(r codec02.GoGroupResult) string {
	switch r {
	case codec02.GoGroupResultSet:
		return "set"
	case codec02.GoGroupResultCleared:
		return "cleared"
	case codec02.GoGroupResultEmpty:
		return "empty"
	}
	return fmt.Sprintf("0x%02x", byte(r))
}

func protectStateName(s codec02.ProtectState) string {
	switch s {
	case codec02.ProtectNone:
		return "none"
	case codec02.ProtectProBel:
		return "probel"
	case codec02.ProtectProBelOverride:
		return "probel-override"
	case codec02.ProtectOEM:
		return "oem"
	}
	return fmt.Sprintf("0x%02x", byte(s))
}

func parseController(s string) (codec02.Controller, error) {
	switch s {
	case "lh", "LH", "0":
		return codec02.ControllerLH, nil
	case "rh", "RH", "1":
		return codec02.ControllerRH, nil
	}
	return 0, fmt.Errorf("unknown --controller %q (use lh | rh)", s)
}
