package main

// `dhs consumer kratos <verb>` — the Kratos NGC2 IDU antenna controller
// (REST/JSON + MQTT, API control document 7592827 Rev. E).
//
// The neutral verbs (info, walk, get, set, watch, export, …) are served
// by the registered connector. Two verbs are this controller's own:
//
//   command  post a command verb (§9): STOW, STANDBY, PEAK-NOW, …
//   table    read one REST table (§5, §6): positionLog, noteLog,
//            alarmPointsList, satelliteSettings, serverStatus, …

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	kratosc "dhs/internal/kratos/consumer"
	"dhs/internal/kratos/codec"
	"dhs/internal/plugin"
)

// runKratos answers the controller's own verbs and hands every other
// verb to the neutral dispatcher.
func runKratos(ctx context.Context, args []string) (handled bool, remaining []string, err error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		fmt.Print(kratosHelp)
		return true, nil, nil
	}
	switch args[0] {
	case "command":
		return true, nil, runKratosCommand(ctx, args[1:])
	case "table":
		return true, nil, runKratosTable(ctx, args[1:])
	}
	return false, args, nil
}

const kratosHelp = `dhs consumer kratos — Kratos NGC2 IDU antenna controller (REST/JSON + MQTT)

USAGE
  dhs consumer kratos <verb> <host>[:port] [flags]

NEUTRAL VERBS (served by the registered connector)
  info     <host>                              identity: system id, customer, UID root
  walk     <host>                              SVT + CVT + enumerations + dependencies → every value
  get      <host> --path antenna/motor/status/targetPlatAz   one value (cvtItem, no walk needed)
  set      <host> --path move/command/targetAz --value 120000  postMessage SET, then read back
  watch    <host> [--path SUBTREE|UID]         MQTT: every change under the path (empty = the whole tree)
  export   <host> --format json|yaml|csv       the walked tree to a file

CONTROLLER VERBS
  command  <host> --verb STOW [--arg A]…       postMessage command verb (§9); no acknowledge exists —
                                               the effect shows in the values it changes and in the noteLog
  command  --list                              the §9 verb list (preliminary in the document)
  table    <host> --query positionLog[/FROM[/TO]]   read one REST table, rows as JSON
                                               (alarmPointsList, noteLog, systemLog, satelliteSettings,
                                                serverStatus, systemConnections, security/clientTbl, …)

PATHS
  A path is a UID with or without its root, slashed or dotted:
  "antenna/motor/status/targetPlatAz" = "cpiasd/antenna/motor/status/targetPlatAz" = "antenna.motor.status.targetPlatAz".

PORTS AND SECURITY (§2.4, §6.1)
  3000  HTTP  (low security: no login; medium: login)      MQTT 1883
  4000  HTTPS (high security: login, certificates)         MQTT over TLS 8883
  The document's §7 names 1822 as the broker port: set DHS_KRATOS_MQTT_PORT when the broker is not on 1883.

ENVIRONMENT (secrets never as flags)
  DHS_KRATOS_USER / DHS_KRATOS_PASS   login (medium/high security); unset = no login
  DHS_KRATOS_TLS=1                    HTTPS + MQTT/TLS (implied by port 4000)
  DHS_KRATOS_INSECURE=1               accept a self-signed certificate
  DHS_KRATOS_MQTT_PORT=N              broker port override
`

func kratosConnect(ctx context.Context, target string) (*kratosc.Plugin, error) {
	host, port, err := splitHostPort(target, kratosc.DefaultPort)
	if err != nil {
		return nil, err
	}
	p := (&kratosc.Factory{}).New(plugin.Deps{}).(*kratosc.Plugin)
	if err := p.Connect(ctx, host, port); err != nil {
		return nil, err
	}
	return p, nil
}

func runKratosCommand(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("consumer kratos command", flag.ContinueOnError)
	verb := fs.String("verb", "", "command verb of §9, e.g. STOW, STANDBY, PEAK-NOW, POINT-AT-GLOBAL-SAT")
	var cmdArgs stringSliceFlag
	fs.Var(&cmdArgs, "arg", "repeatable: one argument of the verb (no spaces)")
	list := fs.Bool("list", false, "print the §9 verb list and exit")
	check := fs.Bool("check", false, "print the body that would be posted, send nothing")
	timeout := fs.Duration("timeout", 10*time.Second, "request timeout")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	if *list {
		names := make([]string, 0, len(codec.CommandVerbs))
		for v := range codec.CommandVerbs {
			names = append(names, v)
		}
		sort.Strings(names)
		for _, v := range names {
			a := codec.CommandVerbs[v]
			if a == "" {
				a = "-"
			}
			fmt.Printf("  %-28s %s\n", v, a)
		}
		return nil
	}
	msg, err := codec.CommandMessage(strings.ToUpper(*verb), cmdArgs...)
	if err != nil {
		return err
	}
	if _, known := codec.CommandVerbs[strings.ToUpper(*verb)]; !known {
		fmt.Fprintf(os.Stderr, "kratos command: %q is not in the §9 list (which the document calls preliminary) — sent as given\n", strings.ToUpper(*verb))
	}
	if *check {
		fmt.Printf("would post consumer/postMessage %s — nothing sent\n", msg.Encode())
		return nil
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("consumer kratos command: a host is required")
	}
	cctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	p, err := kratosConnect(cctx, fs.Arg(0))
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()
	if err := p.Post(cctx, msg); err != nil {
		return err
	}
	fmt.Printf("posted consumer/postMessage %s\n", msg.Encode())
	fmt.Println("no acknowledge is defined for command verbs (§9): read the values it changes, or `table --query noteLog`")
	return nil
}

func runKratosTable(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("consumer kratos table", flag.ContinueOnError)
	query := fs.String("query", "", "table query, e.g. positionLog, positionLog/17000, alarmPointsList/antenna, security/clientTbl")
	timeout := fs.Duration("timeout", 30*time.Second, "request timeout")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() < 1 || *query == "" {
		return fmt.Errorf("usage: dhs consumer kratos table <host> --query TABLE[/ARGS]")
	}
	q := strings.Trim(*query, "/")
	if !strings.HasPrefix(q, "consumer/") && !strings.HasPrefix(q, "security/") {
		q = "consumer/" + q
	}
	cctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	p, err := kratosConnect(cctx, fs.Arg(0))
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()
	env, err := p.Table(cctx, q)
	if err != nil {
		return err
	}
	var rows []map[string]any
	if _, err := env.Rows(&rows); err != nil {
		return err
	}
	out := map[string]any{"query": env.Query, "system": env.System, "timeMs": env.Time(), "rows": rows}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%d row(s)\n", len(rows))
	return nil
}
