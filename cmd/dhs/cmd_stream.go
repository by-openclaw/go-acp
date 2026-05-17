// acp ember stream — subscribe to every parameter that carries a
// streamIdentifier and print each delivered value. Ember+ spec v2.50
// pp. 30-31 define stream parameters as "high-frequency updates carried
// via StreamCollection"; CPU / network cost is why they must be opt-in.
//
// Usage:
//
//	acp stream <host> --port 9092
//	acp stream <host> --port 9092 --id 45     # only this stream identifier
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"dhs/internal/consumer"
	emberplus "dhs/internal/emberplus/consumer"
)

// parseStreamIDList parses the --id CSV form for cmd stream (R10 multi-subscribe).
// "" → nil (subscribe to every stream).
// "0" → [0].
// "0,1001" → [0, 1001].
// "0,0,1001" → [0, 1001] (de-duped).
// Bad tokens return a "validation:" error so the CLI exits non-zero.
func parseStreamIDList(csv string) ([]int64, error) {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return nil, nil
	}
	seen := make(map[int64]struct{})
	var out []int64
	for _, tok := range strings.Split(csv, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			return nil, fmt.Errorf("validation: --id: empty token in %q", csv)
		}
		n, perr := strconv.ParseInt(tok, 10, 64)
		if perr != nil {
			return nil, fmt.Errorf("validation: --id: bad token %q", tok)
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out, nil
}

func runStream(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("stream", flag.ExitOnError)
	cf := addCommonFlags(fs)
	streamIDs := fs.String("id", "", `streamIdentifier filter — comma-separated list (e.g. "0,1001,1002"); empty subscribes to every stream`)
	dmIdentity := fs.String("dm", "", `Ember+ only: identity-keyed DM hot-load (e.g. "Tiny Ember+ Router@1.6.2"). When set, the tree is seeded from .cache/dm/emberplus/<identity>.json and the per-call walk is skipped — refs #438, ADR-0022.`)
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer <proto> stream <host> [--id N[,M,...]]")
	}
	_ = fs.Parse(rest)

	filter, err := parseStreamIDList(*streamIDs)
	if err != nil {
		return err
	}

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	// Ember+ DM hot-load (refs #438): seed the in-RAM tree from
	// .cache/dm/emberplus/<identity>.json so StreamParameterPaths
	// finds the streamed parameters without a fresh wire walk.
	dmSeeded, err := hotLoadEmberplusDM(plug, *dmIdentity, 0, false)
	if err != nil {
		return err
	}
	if !dmSeeded {
		if _, err := plug.Walk(ctx, 0); err != nil {
			return fmt.Errorf("walk: %w", err)
		}
	}

	ep, ok := plug.(*emberplus.Plugin)
	if !ok {
		return fmt.Errorf("stream command is only supported for Ember+ protocol")
	}

	// Find every parameter with a streamIdentifier. If --id is set,
	// filter to only that one; otherwise subscribe to all streamed
	// parameters. The walker has already populated the tree.
	paths := ep.StreamParameterPaths(filter)
	if len(paths) == 0 {
		return fmt.Errorf("emberplus: no parameters with streamIdentifier found (walk a provider that exposes streams)")
	}
	fmt.Fprintf(os.Stderr, "subscribing to %d stream parameter(s)\n", len(paths))

	for _, path := range paths {
		p := path
		err := ep.Subscribe(consumer.ValueRequest{Path: p, ID: -1}, func(ev consumer.Event) {
			fmt.Printf("%s %s = %s\n",
				ev.Timestamp.Format("15:04:05.000"), p, formatStreamValue(ev.Value))
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "subscribe %s: %v\n", p, err)
		}
	}

	<-ctx.Done()
	return nil
}

// formatStreamValue renders a Value for stream output. Terse form: one line,
// no Kind prefix (the path already identifies the parameter).
func formatStreamValue(v consumer.Value) string {
	switch v.Kind {
	case consumer.KindInt:
		return fmt.Sprintf("%d", v.Int)
	case consumer.KindUint:
		return fmt.Sprintf("%d", v.Uint)
	case consumer.KindFloat:
		return fmt.Sprintf("%g", v.Float)
	case consumer.KindBool:
		return fmt.Sprintf("%t", v.Bool)
	case consumer.KindString:
		return v.Str
	}
	if len(v.Raw) > 0 {
		return fmt.Sprintf("%x", v.Raw)
	}
	return ""
}

func helpStream() {
	fmt.Println(`acp stream — subscribe to Ember+ stream parameters

IN   acp stream 127.0.0.1 --protocol emberplus --port 9092
OUT  subscribing to 12 stream parameter(s)
     14:23:45.123  router.streams.fader01 = -3.5
     14:23:45.223  router.streams.fader02 =  0.0
     …  (runs until Ctrl-C)

USAGE
  acp stream <host> [--port 9092] [--id N[,M,...]]

FLAGS
  --id LIST       streamIdentifier filter — comma-separated list of int64
                  (e.g. "0,1001,1002"). Empty subscribes to every stream.
  [global flags accepted: --port, --timeout, --verbose]

BEHAVIOUR
  Walks the tree once, then sends Command 30 (Subscribe) for every
  parameter with a non-zero streamIdentifier. Delivered values are
  decoded per each parameter's StreamDescription (format + offset,
  spec p.86) and printed one per line.

  Blocks until Ctrl-C; sends Command 31 (Unsubscribe) on exit.

EXAMPLES
  acp stream 127.0.0.1 --port 9092
  acp stream 127.0.0.1 --port 9092 --id 45
  acp stream 127.0.0.1 --port 9092 --id 0,1001
  acp stream 127.0.0.1 --port 9092 --id 0,1001,1002

References: Ember+ Documentation v2.50 pp. 22, 29-31, 86, 93.`)
}
