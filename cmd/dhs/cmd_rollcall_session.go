package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"dhs/internal/snell-rollcall/codec/router"
	rollcall "dhs/internal/snell-rollcall/consumer"
)

func helpRollcallSession() {
	fmt.Println(`dhs consumer rollcall session <host> [--output text|json]

Hold ONE connection open and run many reads over it, taking verbs from stdin.

This is the answer to a RollCall unit not timing its sessions out (spec Rev
14): a monitor that reads a plant every few seconds must not open a new
connection each time — after a handful the device runs out of session slots
and stops answering until it is restarted. A Control Panel holds one
connection for as long as it is up; so does this. The device sees exactly one
connection no matter how long the session runs or how many reads cross it.

Reads one verb per line from stdin until EOF (or "quit"). Each result is
framed with a line naming the verb, so a caller can tell them apart:

  verbs:  probe | info | router [slot] | salvo [slot]

Example — read the plant once a second without ever reconnecting:
  while true; do echo probe; sleep 1; done | dhs consumer rollcall session 10.6.250.105

Example — an operator's interactive session:
  dhs consumer rollcall session 10.6.250.104:2050
  (then type: info, router, salvo, quit)`)
}

func runRollcallSession(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("session", flag.ExitOnError)
	fs.Usage = verbUsageFn(fs, helpRollcallSession)
	cf := addCommonFlags(fs)
	output := fs.String("output", "text", "output format for each read: text | json (ADR-0002)")

	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer rollcall session <host>")
	}
	_ = parseVerbFlags(fs, rest)

	p, cleanup, err := rollcallPlugin(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	// The one connection is open now and stays open for every read below.
	fmt.Fprintf(os.Stderr, "rollcall session on %s — one held connection; verbs on stdin (probe|info|router|salvo), EOF or 'quit' to end\n", host)

	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "quit" || line == "exit" {
			break
		}
		fields := strings.Fields(line)
		verb := fields[0]
		slot := -1
		if len(fields) > 1 {
			if n, e := strconv.Atoi(fields[1]); e == nil {
				slot = n
			}
		}

		fmt.Printf("--- %s ---\n", line)
		if err := runSessionVerb(ctx, p, verb, slot, *output); err != nil {
			// A bad verb or a read that failed does not end the session: the
			// held connection is still good, and a monitor keeps going.
			fmt.Printf("error: %v\n", err)
		}
	}
	return sc.Err()
}

// runSessionVerb runs one read over the already-open connection p.
func runSessionVerb(ctx context.Context, p *rollcall.Plugin, verb string, slot int, output string) error {
	switch verb {
	case "probe":
		return probePlant(ctx, p, slot, output)
	case "info":
		info, err := p.GetDeviceInfo(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("device       %s:%d\nslots        %d\n", info.IP, info.Port, info.NumSlots)
		return nil
	case "router":
		r, err := findRouter(ctx, p, slot)
		if err != nil {
			return err
		}
		return printRouterInterface(r, output)
	case "salvo":
		r, err := findRouter(ctx, p, slot)
		if err != nil {
			return err
		}
		return listSalvos(ctx, p, r, router.NameWidth32, output)
	default:
		return fmt.Errorf("unknown session verb %q (want probe|info|router|salvo)", verb)
	}
}
