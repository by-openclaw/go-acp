package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/consumer/compliance"
	"dhs/internal/consumer/monitor"
	"dhs/internal/snmp/codec"
	"dhs/internal/snmp/mib"
	snmpmon "dhs/internal/snmp/monitor"
)

// runMonitor routes `dhs monitor <verb>`. The monitor polls a device on a
// per-OID schedule and prints a value only when it changes — the neutral
// device monitor (ADR-0030) with SNMP as its first user.
func runMonitor(ctx context.Context, args []string) error {
	if len(args) == 0 || isHelpToken(args[0]) {
		printMonitorHelp()
		return nil
	}
	verb := args[0]
	rest := args[1:]
	switch verb {
	case "watch":
		return runMonitorWatch(ctx, rest)
	case "validate":
		return runMonitorValidate(ctx, rest)
	}
	return fmt.Errorf("monitor: unknown verb %q (expected: watch | validate)", verb)
}

// runMonitorValidate loads a poll profile and reports what it schedules,
// failing on a duplicate address, a missing address, or a bad interval.
func runMonitorValidate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	profPath := fs.String("profile", "", "path to the JSON poll profile")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	if *profPath == "" {
		return fmt.Errorf("monitor validate: --profile is required")
	}
	prof, err := loadProfile(*profPath)
	if err != nil {
		return err
	}
	fmt.Printf("profile OK: model %q, %d objects\n", prof.Model, len(prof.Entries))
	ivs := prof.Intervals()
	strs := make([]string, len(ivs))
	for i, d := range ivs {
		strs[i] = d.String()
	}
	fmt.Printf("intervals: %s\n", strings.Join(strs, ", "))
	return nil
}

// runMonitorWatch dials the device, schedules the profile, and prints a
// live line every time a value moves. It runs until interrupted or until
// --duration elapses.
func runMonitorWatch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	var f snmpFlags
	f.register(fs)
	proto := fs.String("proto", "snmp", "device protocol (snmp only for now)")
	profPath := fs.String("profile", "", "path to the JSON poll profile")
	filter := fs.String("filter", "", "only show objects whose OID/name starts with this prefix")
	minGap := fs.Duration("min-gap", 0, "minimum gap between wire operations (lets a fragile agent breathe)")
	duration := fs.Duration("duration", 0, "stop after this long (0 = run until interrupted)")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	if *proto != "snmp" {
		return fmt.Errorf("monitor watch: --proto %q not supported yet (snmp only)", *proto)
	}
	if *profPath == "" {
		return fmt.Errorf("monitor watch: --profile is required")
	}
	host, err := hostArg(fs, "watch")
	if err != nil {
		return err
	}
	prof, err := loadProfile(*profPath)
	if err != nil {
		return err
	}

	logger, _, logClean, _ := consumerLogger(ctx, "monitor", host, "watch")
	defer logClean()

	sess, err := dialSNMP(ctx, &f, host, &compliance.Profile{})
	if err != nil {
		return err
	}
	adapter := snmpmon.New(sess)

	m := monitor.New(monitor.WithLogger(logger))
	id, events := m.Subscribe(1024, prefixFilter(*filter))
	defer m.Unsubscribe(id)

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *duration > 0 {
		var stop context.CancelFunc
		ctx, stop = context.WithTimeout(ctx, *duration)
		defer stop()
	}

	if err := m.Add(ctx, monitor.Device{
		Name:    host,
		Proto:   adapter,
		Profile: prof,
		MinGap:  *minGap,
	}); err != nil {
		return err
	}

	fmt.Printf("watching %s — %d objects, %d distinct intervals (Ctrl-C to stop)\n",
		host, len(prof.Entries), len(prof.Intervals()))

	for {
		select {
		case <-ctx.Done():
			s, _ := m.Stats(host) // read counters before Stop removes the device
			m.Stop()
			fmt.Printf("stopped: %d reads, %d changes, %d errors\n", s.Reads, s.Changes, s.Errors)
			return nil
		case ev := <-events:
			fmt.Println(renderMonitorEvent(ev))
		}
	}
}

// prefixFilter builds a bus filter that keeps only events whose OID or
// resolved name starts with prefix. Empty prefix keeps everything.
func prefixFilter(prefix string) func(consumer.Event) bool {
	if prefix == "" {
		return nil
	}
	return func(ev consumer.Event) bool {
		if strings.HasPrefix(ev.Path, prefix) {
			return true
		}
		if o, err := codec.ParseOID(ev.Path); err == nil {
			name, _ := mib.Describe(o)
			return strings.HasPrefix(name, prefix)
		}
		return false
	}
}

// renderMonitorEvent formats one change as a line: time, MIB name, value,
// and what it moved from.
func renderMonitorEvent(ev consumer.Event) string {
	label := ev.Path
	var obj *mib.Object
	if o, err := codec.ParseOID(ev.Path); err == nil {
		label, obj = mib.Describe(o)
	}
	line := fmt.Sprintf("%s  %-30s = %s",
		time.Now().UTC().Format("15:04:05"), label, monitorValueString(ev.Value, obj))
	if len(ev.Changes) > 0 {
		line += fmt.Sprintf("   (was %s)", ev.Changes[0].Old)
	}
	return line
}

// monitorValueString renders a neutral value, resolving an integer to its
// MIB enum name when the object has one (e.g. major(5)).
func monitorValueString(v consumer.Value, obj *mib.Object) string {
	switch v.Kind {
	case consumer.KindInt:
		if obj != nil {
			if name, ok := obj.EnumName(v.Int); ok {
				return fmt.Sprintf("%s(%d)", name, v.Int)
			}
		}
		return fmt.Sprintf("%d", v.Int)
	case consumer.KindUint:
		return fmt.Sprintf("%d", v.Uint)
	case consumer.KindString:
		return v.Str
	case consumer.KindIPAddr:
		return net.IP(v.IPAddr[:]).String()
	case consumer.KindBool:
		return fmt.Sprintf("%t", v.Bool)
	case consumer.KindEnum:
		return fmt.Sprintf("%d", v.Enum)
	default:
		return "?"
	}
}

// loadProfile reads and parses a JSON poll profile from disk.
func loadProfile(path string) (*monitor.Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("monitor: read profile: %w", err)
	}
	return monitor.Load(data)
}

func printMonitorHelp() {
	fmt.Println(`dhs monitor — poll a device on a per-OID schedule and print what changes

The neutral device monitor (ADR-0030): one connection per device, per-OID
intervals with jitter and coalescing, change-only output, and a rate gap
that keeps a fragile agent from being charged. SNMP is the first user.

VERBS
  watch <target>    live: poll the profile and print every value change
  validate          load a profile and report what it would schedule

WATCH FLAGS
  --profile FILE    JSON poll profile (required)
  --proto snmp      device protocol (snmp only for now)
  --filter PREFIX   only show objects whose OID or name starts with PREFIX
  --min-gap DUR     minimum gap between wire operations (e.g. 20ms)
  --duration DUR    stop after this long (0 = until Ctrl-C)
  plus the SNMP flags: --version, --community, --timeout, --retries, ...

EXAMPLES
  dhs monitor validate --profile rx1290.json
  dhs monitor watch 10.6.255.111 --version 1 --community private --profile rx1290.json
  dhs monitor watch 10.6.255.111 --profile rx1290.json --filter alarm --duration 2m

PROFILE (JSON)
  {
    "model": "RX1290",
    "defaults": { "interval": "30s", "on_change": true },
    "oids": [
      { "oid": "1.3.6.1.4.1.1773.1.1.10", "interval": "1s" },
      { "oid": "1.3.6.1.4.1.1773.1.1.1.7", "interval": "5m", "on_change": false }
    ]
  }`)
}
