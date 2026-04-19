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

	"acp/internal/protocol"
	emberplus "acp/internal/protocol/emberplus"
)

func runStream(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("stream", flag.ExitOnError)
	cf := addCommonFlags(fs)
	streamID := fs.Int64("id", -1, "streamIdentifier filter (-1 = any)")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: acp stream <host> [--id N]")
	}
	_ = fs.Parse(rest)

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()

	if _, err := plug.Walk(opCtx, 0); err != nil {
		return fmt.Errorf("walk: %w", err)
	}

	ep, ok := plug.(*emberplus.Plugin)
	if !ok {
		return fmt.Errorf("stream command is only supported for Ember+ protocol")
	}

	// Find every parameter with a streamIdentifier. If --id is set,
	// filter to only that one; otherwise subscribe to all streamed
	// parameters. The walker has already populated the tree.
	paths := ep.StreamParameterPaths(*streamID)
	if len(paths) == 0 {
		return fmt.Errorf("emberplus: no parameters with streamIdentifier found (walk a provider that exposes streams)")
	}
	fmt.Fprintf(os.Stderr, "subscribing to %d stream parameter(s)\n", len(paths))

	for _, path := range paths {
		p := path
		err := ep.Subscribe(protocol.ValueRequest{Path: p, ID: -1}, func(ev protocol.Event) {
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
func formatStreamValue(v protocol.Value) string {
	switch v.Kind {
	case protocol.KindInt:
		return fmt.Sprintf("%d", v.Int)
	case protocol.KindUint:
		return fmt.Sprintf("%d", v.Uint)
	case protocol.KindFloat:
		return fmt.Sprintf("%g", v.Float)
	case protocol.KindBool:
		return fmt.Sprintf("%t", v.Bool)
	case protocol.KindString:
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
  acp stream <host> [--port 9092] [--id N]

FLAGS
  --id N          filter by streamIdentifier (default: any)
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

References: Ember+ Documentation v2.50 pp. 22, 29-31, 86, 93.`)
}
