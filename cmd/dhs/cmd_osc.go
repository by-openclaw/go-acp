package main

// OSC-specific CLI verbs. OSC is symmetric (no client/server), so the
// generic walk/get/set/info verbs don't apply. Instead:
//
//   dhs consumer osc-v10  watch   --listen <transport>:<port>
//   dhs consumer osc-v11  watch   --listen <transport>:<port>
//   dhs producer osc-v10  send    --to <host:port> --transport <kind> --address /foo --types ifs --args 42 3.14 hello
//   dhs producer osc-v11  send    --to <host:port> --transport <kind> --address /foo --types T
//   dhs producer osc-v10  fader   --to <host:port> --transport <kind> --address /fader --rate 60Hz --duration 10s [--min --max]
//   dhs producer osc-v10  serve   --bind   <transport>:<port>
//
// transport kinds: udp | tcp-len | tcp-slip
//   - tcp-len is OSC 1.0 length-prefix (int32 BE size + packet)
//   - tcp-slip is OSC 1.1 SLIP framing (RFC 1055 double-END)
//   - udp works for both versions; default port 8000

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"acp/internal/osc/codec"
	osccons "acp/internal/osc/consumer"
	oscprov "acp/internal/osc/provider"
)

// runOSCConsumer dispatches `dhs consumer osc-vXX <verb> [args]`.
func runOSCConsumer(ctx context.Context, proto string, args []string) error {
	if len(args) == 0 || hasHelpFlag(args) {
		printOSCConsumerHelp(proto)
		return nil
	}
	verb := args[0]
	rest := args[1:]
	switch verb {
	case "watch":
		return runOSCWatch(ctx, proto, rest)
	}
	return fmt.Errorf("consumer %s: unknown verb %q (expected: watch)", proto, verb)
}

// runOSCProducer dispatches `dhs producer osc-vXX <verb> [args]`.
func runOSCProducer(ctx context.Context, proto string, args []string) error {
	if len(args) == 0 || hasHelpFlag(args) {
		printOSCProducerHelp(proto)
		return nil
	}
	verb := args[0]
	rest := args[1:]
	switch verb {
	case "send":
		return runOSCSend(ctx, proto, rest)
	case "fader":
		return runOSCFader(ctx, proto, rest)
	case "serve":
		return runOSCServe(ctx, proto, rest)
	}
	return fmt.Errorf("producer %s: unknown verb %q (expected: send | fader | serve)", proto, verb)
}

// ---- watch -------------------------------------------------------------------

func runOSCWatch(ctx context.Context, proto string, args []string) error {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	listen := fs.String("listen", "udp:8000", "transport:port to bind, e.g. udp:8000, tcp-len:8000, tcp-slip:8001")
	pattern := fs.String("pattern", "", "OSC address pattern to subscribe to (empty = match all)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	transport, port, err := parseListenAddr(*listen)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	plugin := newOSCConsumer(proto, logger)

	switch transport {
	case "udp":
		if err := plugin.Connect(ctx, "0.0.0.0", port); err != nil {
			return err
		}
	case "tcp-len", "tcp-slip":
		if err := requireVersion(proto, transport); err != nil {
			return err
		}
		if err := plugin.ConnectTCP(ctx, "0.0.0.0", port); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown transport %q", transport)
	}
	defer func() { _ = plugin.Disconnect() }()

	if err := plugin.SubscribePattern(*pattern, func(ev osccons.PacketEvent) {
		printPacket(transport, &ev)
	}); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s watching %s:%d (pattern=%q) — Ctrl-C to stop\n", proto, transport, port, *pattern)
	<-ctx.Done()
	return nil
}

// ---- send --------------------------------------------------------------------

func runOSCSend(ctx context.Context, proto string, args []string) error {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	to := fs.String("to", "127.0.0.1:8000", "destination host:port")
	transport := fs.String("transport", "udp", "udp | tcp-len | tcp-slip")
	address := fs.String("address", "/test", "OSC address")
	types := fs.String("types", "", "OSC type-tag string without leading comma (e.g. ifs)")
	bind := fs.String("bind", "0.0.0.0:0", "local bind for UDP (use :PORT to share with watch)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	host, port, err := oscSplitHostPort(*to)
	if err != nil {
		return err
	}
	if err := requireVersion(proto, *transport); err != nil {
		return err
	}
	rawArgs := fs.Args()
	cargs, err := buildArgs(*types, rawArgs)
	if err != nil {
		return err
	}
	msg := codec.Message{Address: *address, Args: cargs}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	srv := newOSCServer(proto, logger)

	switch *transport {
	case "udp":
		if err := srv.Bind(*bind); err != nil {
			return err
		}
		defer func() { _ = srv.Stop() }()
		if err := srv.AddDestination(host, port); err != nil {
			return err
		}
		if err := srv.SendMessage(msg); err != nil {
			return err
		}
	case "tcp-len", "tcp-slip":
		defer func() { _ = srv.Stop() }()
		if err := srv.SendMessageTCP(host, port, msg); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown transport %q", *transport)
	}
	fmt.Fprintf(os.Stderr, "%s sent %s [,%s] to %s://%s:%d\n", proto, *address, *types, *transport, host, port)
	_ = ctx
	return nil
}

// ---- fader -------------------------------------------------------------------

func runOSCFader(ctx context.Context, proto string, args []string) error {
	fs := flag.NewFlagSet("fader", flag.ContinueOnError)
	to := fs.String("to", "127.0.0.1:8000", "destination host:port")
	transport := fs.String("transport", "udp", "udp | tcp-len | tcp-slip")
	address := fs.String("address", "/fader", "OSC address")
	rate := fs.Int("rate", 60, "frames per second")
	duration := fs.Duration("duration", 10*time.Second, "test duration")
	min := fs.Float64("min", 0, "minimum fader value")
	max := fs.Float64("max", 1, "maximum fader value")
	pattern := fs.String("pattern", "ramp", "ramp | sine | random")
	if err := fs.Parse(args); err != nil {
		return err
	}
	host, port, err := oscSplitHostPort(*to)
	if err != nil {
		return err
	}
	if err := requireVersion(proto, *transport); err != nil {
		return err
	}
	if *rate <= 0 {
		return fmt.Errorf("--rate must be positive")
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	srv := newOSCServer(proto, logger)

	useUDP := *transport == "udp"
	if useUDP {
		if err := srv.Bind("0.0.0.0:0"); err != nil {
			return err
		}
		if err := srv.AddDestination(host, port); err != nil {
			return err
		}
	}
	defer func() { _ = srv.Stop() }()

	tickInterval := time.Second / time.Duration(*rate)
	totalFrames := int((*duration).Seconds() * float64(*rate))

	fmt.Fprintf(os.Stderr,
		"%s fader → %s://%s:%d  rate=%dHz  duration=%s  pattern=%s  frames≈%d\n",
		proto, *transport, host, port, *rate, duration.Round(time.Millisecond), *pattern, totalFrames)

	tick := time.NewTicker(tickInterval)
	defer tick.Stop()
	stop := time.NewTimer(*duration)
	defer stop.Stop()

	rng := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0xC0FFEE))
	frames := 0
	errs := 0
	latencies := make([]time.Duration, 0, totalFrames)
	span := *max - *min
	startWall := time.Now()

	for {
		select {
		case <-ctx.Done():
			goto done
		case <-stop.C:
			goto done
		case t := <-tick.C:
			elapsed := t.Sub(startWall).Seconds()
			var v float64
			switch *pattern {
			case "sine":
				v = *min + span*0.5*(1+math.Sin(2*math.Pi*elapsed))
			case "random":
				v = *min + span*rng.Float64()
			default: // ramp
				v = *min + span*math.Mod(elapsed, 1)
			}
			msg := codec.Message{Address: *address, Args: []codec.Arg{codec.Float32(float32(v))}}
			sendStart := time.Now()
			var serr error
			if useUDP {
				serr = srv.SendMessage(msg)
			} else {
				serr = srv.SendMessageTCP(host, port, msg)
			}
			lat := time.Since(sendStart)
			if serr != nil {
				errs++
				continue
			}
			frames++
			latencies = append(latencies, lat)
		}
	}
done:
	wall := time.Since(startWall)
	printFaderStats(*transport, frames, errs, wall, latencies)
	return nil
}

func printFaderStats(transport string, frames, errs int, wall time.Duration, lats []time.Duration) {
	if len(lats) == 0 {
		fmt.Fprintf(os.Stderr, "%s: no frames sent\n", transport)
		return
	}
	sort.Slice(lats, func(i, j int) bool { return lats[i] < lats[j] })
	p := func(q float64) time.Duration { return lats[int(float64(len(lats)-1)*q)] }
	var sum time.Duration
	for _, l := range lats {
		sum += l
	}
	mean := sum / time.Duration(len(lats))
	throughput := float64(frames) / wall.Seconds()
	fmt.Fprintf(os.Stderr,
		"\n=== fader perf (%s) ===\n  frames     : %d  (errors: %d)\n  wall       : %s\n  throughput : %.0f frames/s\n  send-call latency  mean=%s  p50=%s  p95=%s  p99=%s  max=%s\n",
		transport, frames, errs, wall.Round(time.Microsecond), throughput,
		mean.Round(time.Microsecond), p(0.50).Round(time.Microsecond), p(0.95).Round(time.Microsecond),
		p(0.99).Round(time.Microsecond), lats[len(lats)-1].Round(time.Microsecond))
}

// ---- serve -------------------------------------------------------------------

func runOSCServe(ctx context.Context, proto string, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	bind := fs.String("bind", "udp:8000", "transport:port to bind, e.g. udp:8000, tcp-len:8000, tcp-slip:8001")
	pattern := fs.String("pattern", "", "OSC address pattern to log (empty = log all)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	transport, port, err := parseListenAddr(*bind)
	if err != nil {
		return err
	}
	if err := requireVersion(proto, transport); err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	plugin := newOSCConsumer(proto, logger)

	switch transport {
	case "udp":
		if err := plugin.Connect(ctx, "0.0.0.0", port); err != nil {
			return err
		}
	case "tcp-len", "tcp-slip":
		if err := plugin.ConnectTCP(ctx, "0.0.0.0", port); err != nil {
			return err
		}
	}
	defer func() { _ = plugin.Disconnect() }()
	if err := plugin.SubscribePattern(*pattern, func(ev osccons.PacketEvent) {
		printPacket(transport, &ev)
	}); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s serving (logging) %s:%d (pattern=%q) — Ctrl-C to stop\n", proto, transport, port, *pattern)
	<-ctx.Done()
	return nil
}

// ---- helpers -----------------------------------------------------------------

func newOSCConsumer(proto string, logger *slog.Logger) *osccons.Plugin {
	if proto == "osc-v11" {
		return osccons.NewPluginV11(logger)
	}
	return osccons.NewPluginV10(logger)
}

func newOSCServer(proto string, logger *slog.Logger) *oscprov.Server {
	if proto == "osc-v11" {
		return oscprov.NewServerV11(logger)
	}
	return oscprov.NewServerV10(logger)
}

func requireVersion(proto, transport string) error {
	if transport == "tcp-slip" && proto != "osc-v11" {
		return fmt.Errorf("transport=tcp-slip requires --protocol osc-v11 (SLIP is OSC 1.1 only)")
	}
	if transport == "tcp-len" && proto != "osc-v10" {
		return fmt.Errorf("transport=tcp-len requires --protocol osc-v10 (length-prefix is OSC 1.0 only)")
	}
	return nil
}

func parseListenAddr(s string) (string, int, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("--listen / --bind must be transport:port (got %q)", s)
	}
	port, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, fmt.Errorf("port: %w", err)
	}
	return parts[0], port, nil
}

func oscSplitHostPort(s string) (string, int, error) {
	host, p, err := net.SplitHostPort(s)
	if err != nil {
		return "", 0, fmt.Errorf("--to %q: %w", s, err)
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		return "", 0, fmt.Errorf("port: %w", err)
	}
	return host, port, nil
}

// buildArgs converts a type-tag string + raw token list into typed Args.
// Each non-payload tag (T/F/N/I/[/]) consumes ZERO tokens.
func buildArgs(types string, tokens []string) ([]codec.Arg, error) {
	args := make([]codec.Arg, 0, len(types))
	ti := 0
	for i := 0; i < len(types); i++ {
		tag := types[i]
		switch tag {
		case codec.TagInt32:
			if ti >= len(tokens) {
				return nil, fmt.Errorf("--types '%c' wants an int", tag)
			}
			n, err := strconv.ParseInt(tokens[ti], 10, 32)
			if err != nil {
				return nil, fmt.Errorf("arg[%d] int32: %w", ti, err)
			}
			args = append(args, codec.Int32(int32(n)))
			ti++
		case codec.TagFloat32:
			if ti >= len(tokens) {
				return nil, fmt.Errorf("--types '%c' wants a float", tag)
			}
			f, err := strconv.ParseFloat(tokens[ti], 32)
			if err != nil {
				return nil, fmt.Errorf("arg[%d] float32: %w", ti, err)
			}
			args = append(args, codec.Float32(float32(f)))
			ti++
		case codec.TagInt64:
			if ti >= len(tokens) {
				return nil, fmt.Errorf("--types '%c' wants an int64", tag)
			}
			n, err := strconv.ParseInt(tokens[ti], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("arg[%d] int64: %w", ti, err)
			}
			args = append(args, codec.Int64(n))
			ti++
		case codec.TagFloat64:
			if ti >= len(tokens) {
				return nil, fmt.Errorf("--types '%c' wants a float64", tag)
			}
			f, err := strconv.ParseFloat(tokens[ti], 64)
			if err != nil {
				return nil, fmt.Errorf("arg[%d] float64: %w", ti, err)
			}
			args = append(args, codec.Float64(f))
			ti++
		case codec.TagString, codec.TagSymbol:
			if ti >= len(tokens) {
				return nil, fmt.Errorf("--types '%c' wants a string", tag)
			}
			if tag == codec.TagSymbol {
				args = append(args, codec.Symbol(tokens[ti]))
			} else {
				args = append(args, codec.String(tokens[ti]))
			}
			ti++
		case codec.TagBlob:
			if ti >= len(tokens) {
				return nil, fmt.Errorf("--types '%c' wants a hex blob", tag)
			}
			b, err := hexBytes(tokens[ti])
			if err != nil {
				return nil, fmt.Errorf("arg[%d] blob: %w", ti, err)
			}
			args = append(args, codec.Blob(b))
			ti++
		case codec.TagTimetag:
			if ti >= len(tokens) {
				return nil, fmt.Errorf("--types '%c' wants a u64 timetag", tag)
			}
			n, err := strconv.ParseUint(tokens[ti], 0, 64)
			if err != nil {
				return nil, fmt.Errorf("arg[%d] timetag: %w", ti, err)
			}
			args = append(args, codec.Timetag(n))
			ti++
		case codec.TagChar:
			if ti >= len(tokens) || len(tokens[ti]) == 0 {
				return nil, fmt.Errorf("--types '%c' wants a char", tag)
			}
			args = append(args, codec.Char(int32(tokens[ti][0])))
			ti++
		case codec.TagRGBA32, codec.TagMIDI:
			if ti >= len(tokens) {
				return nil, fmt.Errorf("--types '%c' wants a 4-byte hex blob", tag)
			}
			b, err := hexBytes(tokens[ti])
			if err != nil || len(b) != 4 {
				return nil, fmt.Errorf("arg[%d] %c: must be 4 hex bytes", ti, tag)
			}
			if tag == codec.TagRGBA32 {
				args = append(args, codec.RGBA(b))
			} else {
				args = append(args, codec.MIDI(b))
			}
			ti++
		case codec.TagTrue:
			args = append(args, codec.True())
		case codec.TagFalse:
			args = append(args, codec.False())
		case codec.TagNil:
			args = append(args, codec.Nil())
		case codec.TagInfinitum:
			args = append(args, codec.Infinitum())
		case codec.TagArrayBegin:
			args = append(args, codec.ArrayBegin())
		case codec.TagArrayEnd:
			args = append(args, codec.ArrayEnd())
		default:
			return nil, fmt.Errorf("--types: unknown tag %q", tag)
		}
	}
	if ti != len(tokens) {
		return nil, fmt.Errorf("--types consumed %d tokens; got %d (extra: %v)", ti, len(tokens), tokens[ti:])
	}
	return args, nil
}

func hexBytes(s string) ([]byte, error) {
	s = strings.TrimPrefix(s, "0x")
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("hex must be even-length")
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(s)/2; i++ {
		n, err := strconv.ParseUint(s[i*2:i*2+2], 16, 8)
		if err != nil {
			return nil, err
		}
		out[i] = byte(n)
	}
	return out, nil
}

// printPacket renders a received OSC PacketEvent in a single line for
// the watch / serve verbs.
func printPacket(transport string, ev *osccons.PacketEvent) {
	addr := ev.Msg.Address
	tags := []byte{','}
	for _, a := range ev.Msg.Args {
		tags = append(tags, a.Tag)
	}
	parts := make([]string, 0, len(ev.Msg.Args))
	for _, a := range ev.Msg.Args {
		parts = append(parts, formatArg(a))
	}
	fmt.Printf("[%-9s] %s  %s  %s\n", transport, addr, string(tags), strings.Join(parts, " "))
}

// formatArg renders one Arg in the same shape as the Wireshark dhs_osc
// dissector's Info column, so the terminal `watch` output and a live
// Wireshark capture can be compared line-for-line.
func formatArg(a codec.Arg) string {
	switch a.Tag {
	case codec.TagInt32:
		return strconv.FormatInt(int64(a.Int32), 10)
	case codec.TagChar:
		c := byte(a.Int32 & 0xff)
		if c >= 32 && c < 127 {
			return fmt.Sprintf("'%c'", c)
		}
		return fmt.Sprintf("0x%02x", c)
	case codec.TagFloat32:
		return strconv.FormatFloat(float64(a.Float32), 'g', -1, 32)
	case codec.TagInt64:
		return strconv.FormatInt(a.Int64, 10)
	case codec.TagFloat64:
		return strconv.FormatFloat(a.Float64, 'g', -1, 64)
	case codec.TagString, codec.TagSymbol:
		return strconv.Quote(a.String)
	case codec.TagBlob:
		return fmt.Sprintf("<%db>", len(a.Blob))
	case codec.TagRGBA32:
		if len(a.Blob) == 4 {
			return fmt.Sprintf("#%02x%02x%02x%02x", a.Blob[0], a.Blob[1], a.Blob[2], a.Blob[3])
		}
		return fmt.Sprintf("rgba[%d]", len(a.Blob))
	case codec.TagMIDI:
		if len(a.Blob) == 4 {
			return fmt.Sprintf("midi:%02x.%02x.%02x.%02x", a.Blob[0], a.Blob[1], a.Blob[2], a.Blob[3])
		}
		return fmt.Sprintf("midi[%d]", len(a.Blob))
	case codec.TagTimetag:
		if a.Uint64 == 1 {
			return "tt=now"
		}
		return fmt.Sprintf("tt=%08x.%08x", uint32(a.Uint64>>32), uint32(a.Uint64))
	case codec.TagTrue:
		return "T"
	case codec.TagFalse:
		return "F"
	case codec.TagNil:
		return "N"
	case codec.TagInfinitum:
		return "I"
	case codec.TagArrayBegin:
		return "["
	case codec.TagArrayEnd:
		return "]"
	}
	return fmt.Sprintf("?(%c)", a.Tag)
}

// ---- help text ---------------------------------------------------------------

func printOSCConsumerHelp(proto string) {
	fmt.Printf(`dhs consumer %s — OSC consumer (listener / monitor)

VERBS
  watch  bind a port and print every received message in the form
         "[transport] /address ,tags arg1 arg2 ..."

USAGE
  dhs consumer %s watch --listen <transport>:<port> [--pattern PAT]

FLAGS
  --listen   transport:port to bind. Examples:
               udp:8000           UDP listener (any version)
               tcp-len:8000       OSC 1.0 length-prefix TCP (osc-v10 only)
               tcp-slip:8001      OSC 1.1 SLIP TCP (osc-v11 only)
  --pattern  OSC address pattern to subscribe to (default: all).
             Full OSC 1.0 wildcard syntax: '*', '?', '[abc]', '{a,b}'.

EXAMPLES
  dhs consumer %s watch --listen udp:8000
  dhs consumer %s watch --listen udp:8000 --pattern "/mixer/*/gain"
  dhs consumer %s watch --listen tcp-slip:8001 --pattern "/{pgm,pvw}"
`, proto, proto, proto, proto, proto)
}

func printOSCProducerHelp(proto string) {
	fmt.Printf(`dhs producer %s — OSC producer (sender / responder)

VERBS
  send    emit one OSC message and exit
  fader   continuous high-rate fader simulator (perf measurement)
  serve   bind a port and log incoming messages (act-as-OSC-device, no echo)

USAGE
  dhs producer %s send  --to HOST:PORT --transport KIND --address /A --types TAGS [args...]
  dhs producer %s fader --to HOST:PORT --transport KIND --address /A [--rate N] [--duration D] [--min --max] [--pattern ramp|sine|random]
  dhs producer %s serve --bind <transport>:<port>

TRANSPORTS
  udp        UDP datagrams (default; both versions)
  tcp-len    OSC 1.0 length-prefix (osc-v10 only)
  tcp-slip   OSC 1.1 SLIP (osc-v11 only)

TYPE TAGS
  Each char in --types maps to one Arg (or zero for T/F/N/I/[/]):
    i  int32       f  float32       s  string         b  blob (hex)
    h  int64       d  float64       S  symbol         c  char (1-byte)
    t  timetag     r  RGBA (4 hex)  m  MIDI (4 hex)
    T  true        F  false         N  nil            I  infinitum
    [  array-begin  ]  array-end                       (1.1 only)

EXAMPLES
  dhs producer %s send --to 127.0.0.1:8000 --address /test --types ifs --args 42 3.14 hello
  dhs producer %s send --to 127.0.0.1:8000 --transport tcp-slip --address /flag --types T
  dhs producer %s send --to 127.0.0.1:8000 --address /color --types r --args FF8800FF
  dhs producer %s send --to 127.0.0.1:8000 --address /array --types "i[ii]" 1 10 20

  dhs producer %s fader --to 127.0.0.1:8000 --rate 1000 --duration 5s --pattern sine
  dhs producer %s serve --bind udp:8000
`, proto, proto, proto, proto, proto, proto, proto, proto, proto, proto)
}
