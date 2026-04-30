package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"acp/internal/tsl/codec"
	tslprov "acp/internal/tsl/provider"
)

// runTSLProducer dispatches `dhs producer <tsl-vXX> <verb> [args]`.
//
// Verbs:
//
//	send       encode one frame from --flags and push to every --dest
//	serve      same as send, but loop on --refresh DURATION until ctx fires
//
// `proto` is one of `tsl-v31` / `tsl-v40` / `tsl-v50`.
func runTSLProducer(ctx context.Context, proto string, args []string) error {
	if len(args) == 0 || hasHelpFlag(args) {
		printTSLProducerHelp(os.Stdout, proto)
		return nil
	}
	verb := args[0]
	rest := args[1:]
	switch verb {
	case "send":
		return runTSLSend(ctx, proto, rest, false)
	case "serve":
		return runTSLSend(ctx, proto, rest, true)
	}
	return fmt.Errorf("producer %s: unknown verb %q (expected: send | serve)", proto, verb)
}

// destFlag is a repeatable --dest flag.
type destFlag []string

func (d *destFlag) String() string     { return strings.Join(*d, ",") }
func (d *destFlag) Set(s string) error { *d = append(*d, s); return nil }

// tslSendFlags holds every CLI knob across versions. Version-specific
// flags are validated/applied based on the proto.
type tslSendFlags struct {
	bind      string
	dests     destFlag
	tcp       bool
	keepalive time.Duration
	refresh   time.Duration

	// v3.1/v4.0 + v5.0 shared
	addr       int
	text       string
	brightness int

	// v3.1/v4.0
	textPad string
	tally1  bool
	tally2  bool
	tally3  bool
	tally4  bool

	// v4.0
	displayLeft  string
	displayRight string

	// v5.0
	screen    int
	utf16     bool
	broadcast bool
	index     int
	lh        string
	textTally string
	rh        string
	dmsgs     destFlag // repeatable --dmsg "index=N,lh=...,text-tally=...,rh=...,brightness=N,umd=STR"
}

func registerTSLSendFlags(fs *flag.FlagSet, version tslprov.Version, f *tslSendFlags) {
	fs.StringVar(&f.bind, "bind", "0.0.0.0:0", "local UDP egress bind (':0' = ephemeral)")
	fs.Var(&f.dests, "dest", "destination MV host:port (repeatable; required for UDP)")
	fs.DurationVar(&f.refresh, "refresh", 0, "if >0 (serve only), re-emit the frame every DURATION")

	fs.StringVar(&f.text, "text", "", "UMD label text (≤16 ASCII for v3.1/v4.0, free for v5.0)")
	fs.IntVar(&f.brightness, "brightness", 3, "brightness 0=off 1=1/7 2=1/2 3=full")

	switch version {
	case tslprov.V31, tslprov.V40:
		fs.IntVar(&f.addr, "addr", 0, "display address 0..126")
		fs.StringVar(&f.textPad, "text-pad", "spaces", "DATA pad: spaces (spec) | nul (TallyArbiter off-spec)")
		fs.BoolVar(&f.tally1, "tally1", false, "binary tally bit 1 (CTRL bit 0)")
		fs.BoolVar(&f.tally2, "tally2", false, "binary tally bit 2 (CTRL bit 1)")
		fs.BoolVar(&f.tally3, "tally3", false, "binary tally bit 3 (CTRL bit 2)")
		fs.BoolVar(&f.tally4, "tally4", false, "binary tally bit 4 (CTRL bit 3)")
	}
	if version == tslprov.V40 {
		fs.StringVar(&f.displayLeft, "display-left", "off:off:off", "v4.0 XDATA Display L LH:Text:RH (off|red|green|amber)")
		fs.StringVar(&f.displayRight, "display-right", "off:off:off", "v4.0 XDATA Display R LH:Text:RH (off|red|green|amber)")
	}
	if version == tslprov.V50 {
		fs.IntVar(&f.screen, "screen", 0, "v5.0 screen 0..65534")
		fs.BoolVar(&f.utf16, "utf16", false, "encode TEXT as UTF-16LE (FLAGS bit 0)")
		fs.BoolVar(&f.broadcast, "broadcast", false, "set SCREEN=0xFFFF (broadcast all screens)")
		fs.IntVar(&f.index, "index", 0, "v5.0 display Index 0..65534 (use --broadcast for 0xFFFF)")
		fs.StringVar(&f.lh, "lh", "off", "LH tally colour (off|red|green|amber)")
		fs.StringVar(&f.textTally, "text-tally", "off", "Text tally colour (off|red|green|amber)")
		fs.StringVar(&f.rh, "rh", "off", "RH tally colour (off|red|green|amber)")
		fs.Var(&f.dmsgs, "dmsg", "repeatable v5.0 DMSG: index=N,lh=COL,text-tally=COL,rh=COL,brightness=0..3,umd=STR (overrides singular flags)")
		fs.BoolVar(&f.tcp, "tcp", false, "send via TCP (DLE/STX wrapper) instead of UDP")
		fs.DurationVar(&f.keepalive, "keepalive", tslprov.DefaultTCPKeepalivePeriod, "TCP SO_KEEPALIVE period (default 30s; --tcp only)")
	}
}

// runTSLSend handles the send verb (one-shot) and serve verb (with
// --refresh loop). loop=true → serve; loop=false → send.
func runTSLSend(ctx context.Context, proto string, args []string, loop bool) error {
	version, err := parseTSLProducerVersion(proto)
	if err != nil {
		return err
	}

	verbName := "send"
	if loop {
		verbName = "serve"
	}
	fs := flag.NewFlagSet(proto+"-"+verbName, flag.ContinueOnError)
	f := &tslSendFlags{}
	registerTSLSendFlags(fs, version, f)
	if err := fs.Parse(args); err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if !f.tcp && len(f.dests) == 0 {
		return fmt.Errorf("producer %s %s: at least one --dest is required for UDP", proto, verbName)
	}

	srv := newTSLServer(version, logger)
	defer func() { _ = srv.Stop() }()

	if !f.tcp {
		if err := srv.Bind(f.bind); err != nil {
			return fmt.Errorf("bind %q: %w", f.bind, err)
		}
		for _, d := range f.dests {
			host, port, err := splitHostPort(d, defaultTSLProducerPort(version))
			if err != nil {
				return fmt.Errorf("--dest %q: %w", d, err)
			}
			if err := srv.AddDestination(host, port); err != nil {
				return err
			}
		}
	}

	emit := func() error { return tslEmitOnce(srv, version, f) }
	if err := emit(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s %s emitted to %d destination(s)\n", proto, verbName, len(f.dests))

	if !loop || f.refresh <= 0 {
		return nil
	}
	t := time.NewTicker(f.refresh)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if err := emit(); err != nil {
				slog.Default().Error("tsl serve refresh emit failed", "err", err)
			}
		}
	}
}

// tslEmitOnce builds and sends one frame per the current flags.
func tslEmitOnce(srv *tslprov.Server, version tslprov.Version, f *tslSendFlags) error {
	switch version {
	case tslprov.V31:
		frame, err := buildV31Frame(f)
		if err != nil {
			return err
		}
		return srv.SendV31(frame)
	case tslprov.V40:
		frame, err := buildV40Frame(f)
		if err != nil {
			return err
		}
		return srv.SendV40(frame)
	case tslprov.V50:
		pkt, err := buildV50Packet(f)
		if err != nil {
			return err
		}
		if f.tcp {
			if len(f.dests) == 0 {
				return fmt.Errorf("--tcp requires at least one --dest")
			}
			var first error
			for _, d := range f.dests {
				host, port, err := splitHostPort(d, defaultTSLProducerPort(version))
				if err != nil {
					return fmt.Errorf("--dest %q: %w", d, err)
				}
				if err := srv.SendV50TCP(host, port, pkt); err != nil && first == nil {
					first = err
				}
			}
			return first
		}
		return srv.SendV50(pkt)
	}
	return fmt.Errorf("tsl producer: unknown version")
}

func buildV31Frame(f *tslSendFlags) (codec.V31Frame, error) {
	if f.addr < 0 || f.addr > codec.V31AddressMax {
		return codec.V31Frame{}, fmt.Errorf("--addr %d out of range 0..126", f.addr)
	}
	bri, err := parseBrightness(f.brightness)
	if err != nil {
		return codec.V31Frame{}, err
	}
	if err := requireTextPad(f.textPad); err != nil {
		return codec.V31Frame{}, err
	}
	return codec.V31Frame{
		Address:    uint8(f.addr),
		Tally1:     f.tally1,
		Tally2:     f.tally2,
		Tally3:     f.tally3,
		Tally4:     f.tally4,
		Brightness: bri,
		Text:       f.text,
	}, nil
}

func buildV40Frame(f *tslSendFlags) (codec.V40Frame, error) {
	v31, err := buildV31Frame(f)
	if err != nil {
		return codec.V40Frame{}, err
	}
	left, err := parseXByte(f.displayLeft)
	if err != nil {
		return codec.V40Frame{}, fmt.Errorf("--display-left: %w", err)
	}
	right, err := parseXByte(f.displayRight)
	if err != nil {
		return codec.V40Frame{}, fmt.Errorf("--display-right: %w", err)
	}
	return codec.V40Frame{
		V31:          v31,
		DisplayLeft:  left,
		DisplayRight: right,
	}, nil
}

func buildV50Packet(f *tslSendFlags) (codec.V50Packet, error) {
	if f.screen < 0 || f.screen > 0xFFFE {
		return codec.V50Packet{}, fmt.Errorf("--screen %d out of range 0..65534", f.screen)
	}
	screen := uint16(f.screen)
	if f.broadcast {
		screen = codec.V50BroadcastIdx
	}

	var dmsgs []codec.DMSG
	if len(f.dmsgs) > 0 {
		for i, raw := range f.dmsgs {
			d, err := parseDMSGSpec(raw)
			if err != nil {
				return codec.V50Packet{}, fmt.Errorf("--dmsg #%d %q: %w", i+1, raw, err)
			}
			dmsgs = append(dmsgs, d)
		}
	} else {
		bri, err := parseBrightness(f.brightness)
		if err != nil {
			return codec.V50Packet{}, err
		}
		lh, err := parseTallyColor(f.lh)
		if err != nil {
			return codec.V50Packet{}, fmt.Errorf("--lh: %w", err)
		}
		tt, err := parseTallyColor(f.textTally)
		if err != nil {
			return codec.V50Packet{}, fmt.Errorf("--text-tally: %w", err)
		}
		rh, err := parseTallyColor(f.rh)
		if err != nil {
			return codec.V50Packet{}, fmt.Errorf("--rh: %w", err)
		}
		if f.index < 0 || f.index > 0xFFFE {
			return codec.V50Packet{}, fmt.Errorf("--index %d out of range 0..65534", f.index)
		}
		dmsgs = []codec.DMSG{{
			Index:      uint16(f.index),
			LH:         lh,
			TextTally:  tt,
			RH:         rh,
			Brightness: bri,
			Text:       f.text,
		}}
	}

	return codec.V50Packet{
		UTF16LE: f.utf16,
		Screen:  screen,
		DMSGs:   dmsgs,
	}, nil
}

// parseDMSGSpec parses one --dmsg key=val,key=val,... spec into a DMSG.
// Required: index. Defaults: tallies=off, brightness=3, umd="".
func parseDMSGSpec(spec string) (codec.DMSG, error) {
	d := codec.DMSG{Brightness: codec.BrightnessFull}
	hasIndex := false
	for _, kv := range strings.Split(spec, ",") {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			return codec.DMSG{}, fmt.Errorf("expected key=value, got %q", kv)
		}
		key := strings.ToLower(strings.TrimSpace(kv[:eq]))
		val := strings.TrimSpace(kv[eq+1:])
		switch key {
		case "index":
			n, err := strconv.Atoi(val)
			if err != nil || n < 0 || n > 0xFFFE {
				return codec.DMSG{}, fmt.Errorf("index=%q out of range 0..65534", val)
			}
			d.Index = uint16(n)
			hasIndex = true
		case "lh":
			c, err := parseTallyColor(val)
			if err != nil {
				return codec.DMSG{}, fmt.Errorf("lh=%q: %w", val, err)
			}
			d.LH = c
		case "text-tally", "text", "tt":
			if key == "text" {
				d.Text = val
				continue
			}
			c, err := parseTallyColor(val)
			if err != nil {
				return codec.DMSG{}, fmt.Errorf("%s=%q: %w", key, val, err)
			}
			d.TextTally = c
		case "rh":
			c, err := parseTallyColor(val)
			if err != nil {
				return codec.DMSG{}, fmt.Errorf("rh=%q: %w", val, err)
			}
			d.RH = c
		case "brightness":
			n, err := strconv.Atoi(val)
			if err != nil {
				return codec.DMSG{}, fmt.Errorf("brightness=%q: %w", val, err)
			}
			b, berr := parseBrightness(n)
			if berr != nil {
				return codec.DMSG{}, berr
			}
			d.Brightness = b
		case "umd", "label":
			d.Text = val
		default:
			return codec.DMSG{}, fmt.Errorf("unknown key %q (want index|lh|text-tally|rh|brightness|umd)", key)
		}
	}
	if !hasIndex {
		return codec.DMSG{}, fmt.Errorf("missing index=N")
	}
	return d, nil
}

func parseBrightness(n int) (codec.Brightness, error) {
	if n < 0 || n > 3 {
		return 0, fmt.Errorf("--brightness %d out of range 0..3", n)
	}
	return codec.Brightness(n), nil
}

func parseTallyColor(s string) (codec.TallyColor, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "off", "0":
		return codec.TallyOff, nil
	case "red", "1":
		return codec.TallyRed, nil
	case "green", "2":
		return codec.TallyGreen, nil
	case "amber", "3":
		return codec.TallyAmber, nil
	}
	return 0, fmt.Errorf("invalid tally colour %q (want off|red|green|amber)", s)
}

func parseXByte(s string) (codec.XByte, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return codec.XByte{}, fmt.Errorf("expected lh:text:rh, got %q", s)
	}
	lh, err := parseTallyColor(parts[0])
	if err != nil {
		return codec.XByte{}, err
	}
	tt, err := parseTallyColor(parts[1])
	if err != nil {
		return codec.XByte{}, err
	}
	rh, err := parseTallyColor(parts[2])
	if err != nil {
		return codec.XByte{}, err
	}
	return codec.XByte{LH: lh, Text: tt, RH: rh}, nil
}

func requireTextPad(s string) error {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "spaces", "":
		return nil
	case "nul", "null":
		return fmt.Errorf("--text-pad nul is reserved for off-spec emission and is not allowed on tx (spec is spaces); rx tolerates and fires tsl_v31_null_pad")
	}
	return fmt.Errorf("--text-pad %q (want spaces)", s)
}

func parseTSLProducerVersion(proto string) (tslprov.Version, error) {
	switch proto {
	case "tsl-v31":
		return tslprov.V31, nil
	case "tsl-v40":
		return tslprov.V40, nil
	case "tsl-v50":
		return tslprov.V50, nil
	}
	return 0, fmt.Errorf("unknown TSL version %q (want tsl-v31, tsl-v40, tsl-v50)", proto)
}

// defaultTSLProducerPort returns the spec port for a producer version.
func defaultTSLProducerPort(v tslprov.Version) int {
	switch v {
	case tslprov.V31, tslprov.V40:
		return 4000
	case tslprov.V50:
		return 8901
	}
	return 0
}

func newTSLServer(v tslprov.Version, logger *slog.Logger) *tslprov.Server {
	switch v {
	case tslprov.V31:
		return tslprov.NewServerV31(logger)
	case tslprov.V40:
		return tslprov.NewServerV40(logger)
	case tslprov.V50:
		return tslprov.NewServerV50(logger)
	}
	return nil
}

func printTSLProducerHelp(w io.Writer, proto string) {
	_, _ = fmt.Fprintln(w, strings.TrimSpace(`
dhs producer `+proto+` — push TSL UMD frames to one or more multiviewers

USAGE
  dhs producer `+proto+` send  --dest HOST:PORT [--bind ...] [version flags]
  dhs producer `+proto+` serve --dest HOST:PORT --refresh DURATION [version flags]

VERBS
  send            encode one frame from the flags and push once
  serve           encode + push, then re-emit every --refresh DURATION until Ctrl-C

COMMON FLAGS
  --bind HOST:PORT       local egress bind (default 0.0.0.0:0 ephemeral)
  --dest HOST:PORT       destination MV (repeatable; required for UDP)
  --refresh DURATION     periodic re-emit (serve only; e.g. 1s)
  --text "STR"           UMD label
  --brightness 0..3      0=off 1=1/7 2=1/2 3=full

V3.1 / V4.0 FLAGS
  --addr N               display address 0..126
  --tally1..4            binary tally bits (CTRL bits 0-3)
  --text-pad spaces      DATA padding (spec; nul is rx-tolerated, not for tx)

V4.0 EXTRA
  --display-left  lh:text:rh    XDATA Xbyte L (off|red|green|amber)
  --display-right lh:text:rh    XDATA Xbyte R

V5.0 FLAGS
  --screen N             screen 0..65534
  --broadcast            override screen with 0xFFFF
  --index N              display index 0..65534
  --utf16                encode TEXT as UTF-16LE (FLAGS bit 0)
  --lh OFF|RED|GREEN|AMBER
  --text-tally OFF|RED|GREEN|AMBER
  --rh OFF|RED|GREEN|AMBER
  --tcp                  push via TCP DLE/STX wrapper instead of UDP
  --keepalive DURATION   TCP SO_KEEPALIVE period (default 30s; --tcp only)

EXAMPLES
  dhs producer tsl-v31 send  --dest 10.0.0.5:4000 --addr 3 --tally1 --text "CAM 1"
  dhs producer tsl-v40 serve --dest 10.0.0.5:4000 --refresh 1s --addr 3 --display-left red:green:off
  dhs producer tsl-v50 send  --dest 10.0.0.5:8901 --screen 0 --index 2 --lh red --text "PGM"
  dhs producer tsl-v50 send  --dest 10.0.0.5:8902 --tcp --screen 0 --index 2 --rh amber
`))
}
