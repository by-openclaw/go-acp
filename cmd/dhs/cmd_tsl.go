package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	tslconsumer "acp/internal/tsl/consumer"
)

// runTSLConsumer dispatches `dhs consumer <tsl-vXX> <verb> [args]`.
// TSL UMD is push-only: the "consumer" plugin listens for tally feeds
// from a transmitter (Lawo VSM, Miranda Kaleido, TallyArbiter, etc.).
//
// Supported verbs:
//
//	listen [--bind HOST:PORT] [--tcp]   bind a UDP (or v5.0 TCP) listener
//	                                    and print every decoded frame.
//
// `proto` is one of `tsl-v31` / `tsl-v40` / `tsl-v50`.
func runTSLConsumer(ctx context.Context, proto string, args []string) error {
	if len(args) == 0 || hasHelpFlag(args) {
		printTSLConsumerHelp(os.Stdout, proto)
		return nil
	}
	verb := args[0]
	rest := args[1:]
	switch verb {
	case "listen":
		return runTSLListen(ctx, proto, rest)
	}
	return fmt.Errorf("consumer %s: unknown verb %q (expected: listen)", proto, verb)
}

// runTSLListen binds a UDP (or v5.0 TCP) listener and prints every
// decoded frame to stdout until ctx fires.
func runTSLListen(ctx context.Context, proto string, args []string) error {
	fs := flag.NewFlagSet(proto+"-listen", flag.ContinueOnError)
	bind := fs.String("bind", "", "bind address e.g. ':4000' or '0.0.0.0:4000' (default: protocol's standard port on all interfaces)")
	tcp := fs.Bool("tcp", false, "v5.0 only — listen on TCP with DLE/STX wrapper instead of UDP")
	_ = fs.Duration("keepalive", 30*time.Second, "v5.0 TCP only — SO_KEEPALIVE period (default 30s; ignored on UDP)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	version, err := parseTSLVersion(proto)
	if err != nil {
		return err
	}
	if *tcp && version != tslconsumer.V50 {
		return fmt.Errorf("consumer %s: --tcp is only supported for tsl-v50", proto)
	}

	host, port, err := parseTSLBind(*bind, defaultTSLPort(version))
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	plugin := newTSLPlugin(version, logger)

	if *tcp {
		if err := plugin.ConnectV50TCP(ctx, host, port); err != nil {
			return fmt.Errorf("listen tcp %s:%d: %w", host, port, err)
		}
	} else {
		if err := plugin.Connect(ctx, host, port); err != nil {
			return fmt.Errorf("listen udp %s:%d: %w", host, port, err)
		}
	}
	defer func() { _ = plugin.Disconnect() }()

	transport := "udp"
	if *tcp {
		transport = "tcp"
	}
	_, _ = fmt.Fprintf(os.Stdout, "tsl-%s consumer listening on %s://%s:%d (Ctrl-C to stop)\n",
		versionShortName(version), transport, host, port)

	switch version {
	case tslconsumer.V31:
		err = plugin.SubscribeV31(func(ev tslconsumer.FrameV31Event) {
			f := ev.Frame
			_, _ = fmt.Fprintf(os.Stdout,
				"v3.1  remote=%s  addr=%d  T1=%s T2=%s T3=%s T4=%s  brightness=%s  UMD=%q\n",
				ev.Remote, f.Address,
				onOff(f.Tally1), onOff(f.Tally2), onOff(f.Tally3), onOff(f.Tally4),
				f.Brightness, f.Text)
		})
	case tslconsumer.V40:
		err = plugin.SubscribeV40(func(ev tslconsumer.FrameV40Event) {
			f := ev.Frame
			_, _ = fmt.Fprintf(os.Stdout,
				"v4.0  remote=%s  addr=%d  T1=%s T2=%s T3=%s T4=%s  brightness=%s  UMD=%q\n"+
					"      DisplayL  LH=%s Text=%s RH=%s\n"+
					"      DisplayR  LH=%s Text=%s RH=%s\n",
				ev.Remote, f.V31.Address,
				onOff(f.V31.Tally1), onOff(f.V31.Tally2), onOff(f.V31.Tally3), onOff(f.V31.Tally4),
				f.V31.Brightness, f.V31.Text,
				f.DisplayLeft.LH, f.DisplayLeft.Text, f.DisplayLeft.RH,
				f.DisplayRight.LH, f.DisplayRight.Text, f.DisplayRight.RH)
		})
	case tslconsumer.V50:
		err = plugin.SubscribeV50(func(ev tslconsumer.FrameV50Event) {
			p := ev.Frame
			charset := "ASCII"
			if p.UTF16LE {
				charset = "UTF-16LE"
			}
			_, _ = fmt.Fprintf(os.Stdout,
				"v5.0  remote=%s  screen=%d  charset=%s  dmsgs=%d\n",
				ev.Remote, p.Screen, charset, len(p.DMSGs))
			for _, d := range p.DMSGs {
				_, _ = fmt.Fprintf(os.Stdout,
					"      display=%d  LH=%s  Text=%s  RH=%s  brightness=%s  UMD=%q\n",
					d.Index, d.LH, d.TextTally, d.RH, d.Brightness, d.Text)
			}
		})
	}
	if err != nil {
		return err
	}

	<-ctx.Done()
	return nil
}

// parseTSLVersion maps a CLI proto name to the consumer Version enum.
func parseTSLVersion(proto string) (tslconsumer.Version, error) {
	switch proto {
	case "tsl-v31":
		return tslconsumer.V31, nil
	case "tsl-v40":
		return tslconsumer.V40, nil
	case "tsl-v50":
		return tslconsumer.V50, nil
	}
	return 0, fmt.Errorf("unknown TSL version %q (want tsl-v31, tsl-v40, tsl-v50)", proto)
}

// newTSLPlugin returns a fresh consumer Plugin bound to the given
// version. Mirrors the per-version constructors in consumer/plugin.go.
func newTSLPlugin(v tslconsumer.Version, logger *slog.Logger) *tslconsumer.Plugin {
	switch v {
	case tslconsumer.V31:
		return tslconsumer.NewPluginV31(logger)
	case tslconsumer.V40:
		return tslconsumer.NewPluginV40(logger)
	case tslconsumer.V50:
		return tslconsumer.NewPluginV50(logger)
	}
	return nil
}

// onOff renders a boolean tally bit as "ON"/"off" — matches the
// Miranda TSL over IP Emulator UI labels for v3.1 / v4.0 binary
// tallies (which carry no colour, only on/off + brightness).
func onOff(b bool) string {
	if b {
		return "ON"
	}
	return "off"
}

// versionShortName returns "v31" / "v40" / "v50" for log lines.
func versionShortName(v tslconsumer.Version) string {
	switch v {
	case tslconsumer.V31:
		return "v31"
	case tslconsumer.V40:
		return "v40"
	case tslconsumer.V50:
		return "v50"
	}
	return "unknown"
}

// defaultTSLPort returns the standard port for the given version.
func defaultTSLPort(v tslconsumer.Version) int {
	switch v {
	case tslconsumer.V31, tslconsumer.V40:
		return 4000
	case tslconsumer.V50:
		return 8901
	}
	return 0
}

// parseTSLBind splits "host:port" into (host, port). Empty host = bind
// all interfaces. Empty bind = bind all interfaces on the default port.
func parseTSLBind(bind string, defaultPort int) (string, int, error) {
	if bind == "" {
		return "", defaultPort, nil
	}
	host, port, err := splitHostPort(bind, defaultPort)
	if err != nil {
		return "", 0, err
	}
	if host == "" {
		host = "0.0.0.0"
	}
	return host, port, nil
}

func printTSLConsumerHelp(w io.Writer, proto string) {
	_, _ = fmt.Fprintln(w, strings.TrimSpace(`
dhs consumer `+proto+` — TSL UMD listener (push protocol from a switcher / VSM / Kaleido)

USAGE
  dhs consumer `+proto+` listen [--bind HOST:PORT] [--tcp]

VERBS
  listen          bind a UDP listener (or v5.0 TCP listener with --tcp)
                  and print every decoded frame until Ctrl-C

DEFAULT PORTS
  tsl-v31, tsl-v40   UDP 4000
  tsl-v50            UDP 8901 (or TCP 8901 with --tcp)

EXAMPLES
  dhs consumer tsl-v31 listen
  dhs consumer tsl-v40 listen --bind 0.0.0.0:4040
  dhs consumer tsl-v50 listen --bind 0.0.0.0:8901
  dhs consumer tsl-v50 listen --bind 0.0.0.0:8901 --tcp
`))
}
