package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	codec "dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/codec/router"
	rollcall "dhs/internal/snell-rollcall/consumer"
)

func helpRollcallProbe() {
	fmt.Println(`dhs consumer rollcall probe <host> [--slot N] [--output text|json]

Read a plant over ONE held connection: device info, the routing interface,
and the salvo list, in a single session rather than one connection per verb.

A RollCall unit does not time its sessions out well (spec Rev 14): a client
that opens a fresh connection for info, then another for the router, then
another for the salvos exhausts a real frame's session slots after a handful
of verbs. A Control Panel holds ONE connection and reads everything over it,
and this verb does the same — the connection is opened once, every read runs
on it, and it is closed at the end.

This is the verb the integration play drives against the emulator and a real
device, so the whole read sweep costs one connection, not several.

Examples:
  dhs consumer rollcall probe 10.6.250.105
  dhs consumer rollcall probe 10.6.250.104:2050 --output json`)
}

// probeReport is the JSON shape: everything one connection read.
type probeReport struct {
	Device string           `json:"device"`
	Slots  int              `json:"slots"`
	Router map[string]any   `json:"router,omitempty"`
	Salvos []map[string]any `json:"salvos,omitempty"`
	Note   string           `json:"note,omitempty"`
}

func runRollcallProbe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probe", flag.ExitOnError)
	fs.Usage = verbUsageFn(fs, helpRollcallProbe)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", -1, "router node to read; without it every node is probed")
	output := fs.String("output", "text", "output format: text | json (ADR-0002)")

	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer rollcall probe <host> [--slot N]")
	}
	_ = parseVerbFlags(fs, rest)

	p, cleanup, err := rollcallPlugin(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	// One connection, every read. Device info first, then the router
	// interface and its salvos — all on the link opened above.
	info, err := p.GetDeviceInfo(ctx)
	if err != nil {
		return fmt.Errorf("probe: device info: %w", err)
	}

	r, rErr := findRouter(ctx, p, *slot)

	if strings.EqualFold(*output, "json") {
		rep := probeReport{Device: fmt.Sprintf("%s:%d", info.IP, info.Port), Slots: info.NumSlots}
		if rErr != nil {
			rep.Note = "no router reachable: " + rErr.Error()
		} else {
			rep.Router = routerJSON(r)
			rep.Salvos = probeSalvoList(ctx, p, r)
		}
		return json.NewEncoder(os.Stdout).Encode(rep)
	}

	fmt.Printf("device       %s:%d\n", info.IP, info.Port)
	fmt.Printf("slots        %d\n", info.NumSlots)
	fmt.Println()
	if rErr != nil {
		fmt.Printf("router       none reachable: %v\n", rErr)
		return nil
	}
	if err := printRouterInterface(r, "text"); err != nil {
		return err
	}
	fmt.Println()
	// The salvo list over the same connection, degrading to numbers when the
	// controller ships no names file, exactly as the salvo verb does.
	return listSalvos(ctx, p, r, router.NameWidth32, "text")
}

// probeSalvoList returns the salvos as JSON entries, degrading to numbers with
// unset names when the controller ships no names file (the vendor Centra).
func probeSalvoList(ctx context.Context, p *rollcall.Plugin, r *rollcall.RouterInterface) []map[string]any {
	out := make([]map[string]any, 0, r.Salvos)
	if r.Salvos == 0 {
		return out
	}
	names, err := p.SalvoNames(ctx, r, router.NameWidth32)
	if err != nil {
		var fe *codec.FileError
		if errors.As(err, &fe) && fe.NotFound() {
			for i := 0; i < int(r.Salvos); i++ {
				out = append(out, map[string]any{"salvo": i + 1, "name": ""})
			}
		}
		return out
	}
	for i, n := range names.Srcs {
		out = append(out, map[string]any{"salvo": i + 1, "name": n})
	}
	return out
}
