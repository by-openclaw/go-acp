package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"dhs/internal/snell-rollcall/codec/router"
	rollcall "dhs/internal/snell-rollcall/consumer"
)

func helpRollcallSalvo() {
	fmt.Println(`dhs consumer rollcall salvo <host> [--fire N] [--slot N] [--width 8|32] [--output text|json]

List what a controller's salvos are called, or fire one.

A salvo is a set of routes made together. What is in one is never on the wire:
the controller holds it, a client fires it by number, and the answer is how
many routes it made. So this lists names and fires numbers, and there is no way
to ask what a salvo would do before doing it.

The names live in a file rather than in commands — a plant with a thousand
salvos would need a thousand commands otherwise — at two widths. The narrow one
is a different, shorter set of names, not a truncation of the wide one.

Examples:
  dhs consumer rollcall salvo 10.6.250.105
  dhs consumer rollcall salvo 10.6.250.105 --width 8
  dhs consumer rollcall salvo 10.6.250.105 --fire 3`)
}

func runRollcallSalvo(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("salvo", flag.ExitOnError)
	fs.Usage = verbUsageFn(fs, helpRollcallSalvo)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", -1, "node to use; without it every node is probed")
	fire := fs.Int("fire", 0, "salvo to fire, counting from one; omit to list them")
	width := fs.Int("width", router.NameWidth32, "name width: 8 or 32")
	output := fs.String("output", "text", "output format: text | json (ADR-0002)")

	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer rollcall salvo <host> [--fire N]")
	}
	_ = parseVerbFlags(fs, rest)

	if *width != router.NameWidth8 && *width != router.NameWidth32 {
		return fmt.Errorf("--width is 8 or 32, not %d", *width)
	}

	p, cleanup, err := rollcallPlugin(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	r, err := findRouter(ctx, p, *slot)
	if err != nil {
		return err
	}

	if *fire > 0 {
		return fireOneSalvo(ctx, p, r, uint32(*fire), *output)
	}
	return listSalvos(ctx, p, r, *width, *output)
}

// fireOneSalvo runs a salvo and says how many routes it made.
//
// Zero is not an error at the protocol level and is not reported as one here:
// the specification says "number of routes made or 0 on error" and gives no
// way to tell an empty salvo from a refused one, so what is printed is what
// was said.
func fireOneSalvo(ctx context.Context, p *rollcall.Plugin, r *rollcall.RouterInterface,
	salvo uint32, output string) error {

	made, err := p.FireSalvo(ctx, r, salvo)
	if err != nil {
		return err
	}

	if strings.EqualFold(output, "json") {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"salvo":  salvo,
			"routes": made,
			"made":   made > 0,
		})
	}

	if made == 0 {
		fmt.Printf("salvo %d made no routes — it may be empty, or every route in it was refused\n", salvo)
		return nil
	}
	fmt.Printf("salvo %d made %d route(s)\n", salvo, made)
	return nil
}

// listSalvos prints what the salvos are called.
func listSalvos(ctx context.Context, p *rollcall.Plugin, r *rollcall.RouterInterface,
	width int, output string) error {

	if r.Salvos == 0 {
		if strings.EqualFold(output, "json") {
			return json.NewEncoder(os.Stdout).Encode(map[string]any{"salvos": []any{}})
		}
		fmt.Println("this controller holds no salvos")
		return nil
	}

	names, err := p.SalvoNames(ctx, r, width)
	if err != nil {
		return err
	}

	if strings.EqualFold(output, "json") {
		list := make([]map[string]any, 0, len(names.Srcs))
		for i, n := range names.Srcs {
			list = append(list, map[string]any{"salvo": i + 1, "name": n})
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"slot":   r.Slot,
			"width":  width,
			"salvos": list,
		})
	}

	// The writer buffers, so a write cannot fail here; Flush is where an error
	// would surface, and that one is returned.
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "SALVO\tNAME")
	for i, n := range names.Srcs {
		_, _ = fmt.Fprintf(w, "%d\t%s\n", i+1, nameOrUnset(n))
	}
	return w.Flush()
}
