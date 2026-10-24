package main

// `dhs consumer ccp <verb>` — Axon Cortex Control Panel Protocol (CCPP
// 1.18), TCP 2071. The neutral verbs (info, walk, get, set, watch) are
// served by the registered connector; `panel` is this protocol's own:
// it holds the panel online, draws a layout and logs every action.

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"time"

	"dhs/internal/ccp/codec"
	ccpc "dhs/internal/ccp/consumer"
	dhsc "dhs/internal/consumer"
	"dhs/internal/plugin"
)

func runCCP(ctx context.Context, args []string) (handled bool, remaining []string, err error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		fmt.Print(ccpHelp)
		return true, nil, nil
	}
	switch args[0] {
	case "panel":
		return true, nil, runCCPPanel(ctx, args[1:])
	case "colours", "colors":
		names := make([]string, 0, len(codec.NamedColours))
		for n := range codec.NamedColours {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Printf("  %-10s 0x%02x\n", n, codec.NamedColours[n])
		}
		fmt.Println("  any index 0x00..0x3f = 0b00RRGGBB (two bits per colour)")
		return true, nil, nil
	}
	return false, args, nil
}

const ccpHelp = `dhs consumer ccp — Axon Cortex Control Panel Protocol (CCP-1601 …), TCP 2071

The panel accepts at least two clients: dhs connects next to Cerebrum.
With no client connected it shows its IP address and an off-line screen
(§1.0.0), so labels stay only while a client holds the connection.

NEUTRAL VERBS
  info   <host>                                    model, protocol version, firmware, PSUs
  walk   <host>                                    status.*, network.*, buttons.N.label|colour
  get    <host> --path status.psu_b                one value (network.subnet, network.dhcp, …)
  set    <host> --path buttons.3.label --value "CAM|3"   one write (the panel goes off-line when dhs exits)
  watch  <host> [--path buttons.17]                button down/up, rotary +n/-n, T-bar, GPI

PANEL VERBS
  panel  <host> [--label N=TEXT]… [--colour N=NAME]… [--layout FILE.csv] [--duration D]
         holds the panel online (poll every second), draws the layout, redraws when the panel
         asks for a refresh or after a reconnect, prints every button and rotary action.
         TEXT: up to 2 lines of 5 characters at double size, or 4 lines of 10; '|' starts a line.
         CSV columns: button,label,colour
         --knob N=NAME,MIN,MAX,STEP,UNIT[,INIT]: press button N to select it (green, framed),
         turn the rotary to change it within MIN..MAX (step x rotary speed); --frame frames every label.
  colours                                          the named colours (or 0x00..0x3f = 0b00RRGGBB)

EXAMPLES
  dhs consumer ccp panel 10.6.255.105 --label "1=SAT|1" --colour 1=green --label "2=CAM 2" --colour 2=red
  dhs consumer ccp panel 10.6.255.105 --layout panel.csv
  dhs consumer ccp panel 10.6.255.105 --knob "1=GAIN,-12,12,0.5,dB,0" --knob "2=DELAY,0,200,1,ms,40"
  dhs consumer ccp watch 10.6.255.105
`

type ccpButton struct {
	label  string
	colour byte
	hasCol bool
	frame  bool
	image  *codec.Bitmap // a logo; the label, if any, is drawn over it
	blink  bool          // toggled between its colour and off every --blink-rate
}

// ccpLoadImage reads "FILE[,invert][,dither][,margin=N]" into a bitmap.
func ccpLoadImage(spec string) (*codec.Bitmap, error) {
	parts := strings.Split(spec, ",")
	opts := codec.ImageOptions{}
	for _, o := range parts[1:] {
		o = strings.TrimSpace(strings.ToLower(o))
		switch {
		case o == "invert":
			opts.Invert = true
		case o == "dither":
			opts.Dither = true
		case strings.HasPrefix(o, "margin="):
			n, err := strconv.Atoi(strings.TrimPrefix(o, "margin="))
			if err != nil {
				return nil, fmt.Errorf("image %q: margin=%s", spec, o)
			}
			opts.Margin = n
		case strings.HasPrefix(o, "threshold="):
			n, err := strconv.Atoi(strings.TrimPrefix(o, "threshold="))
			if err != nil || n < 1 || n > 255 {
				return nil, fmt.Errorf("image %q: threshold 1..255", spec)
			}
			opts.Threshold = uint8(n)
		case o == "":
		default:
			return nil, fmt.Errorf("image %q: unknown option %q (invert, dither, margin=N, threshold=N)", spec, o)
		}
	}
	f, err := os.Open(strings.TrimSpace(parts[0]))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	img, err := codec.LoadImage(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", parts[0], err)
	}
	b := codec.FromImage(img, opts)
	return &b, nil
}

// ccpColumns is how many buttons a row holds per model, buttons numbered
// left to right then top to bottom (§1.4.0 drawings). The CCP-1601 (one
// row of 16) and CCP-4200B (two rows of 21) are from the drawings; the
// other multi-row models are taken as 16 per row until one is measured.
var ccpColumns = map[string]int{"CCP-1601": 16, "CCP-1601B": 16, "CCP-4200B": 21}

// ccpPosition returns the control kind and its row and column (1-based);
// the rotary has no row/column (0, 0).
func ccpPosition(model string, id, rotary int) (string, int, int) {
	if id == rotary {
		return "rotary", 0, 0
	}
	cols := ccpColumns[model]
	if cols == 0 {
		cols = 16
	}
	return "button", (id-1)/cols + 1, (id-1)%cols + 1
}

// ccpKnob is one value a button selects and the rotary changes.
type ccpKnob struct {
	button              int
	name, unit          string
	min, max, step, val float64
	decimals            int
}

func (k *ccpKnob) text() string {
	return fmt.Sprintf("%s|%s%s", k.name, strconv.FormatFloat(k.val, 'f', k.decimals, 64), k.unit)
}

func (k *ccpKnob) num(v float64) string { return strconv.FormatFloat(v, 'f', k.decimals, 64) }

// scale keeps the knob's text one size over its whole range: double size
// only when the name and the widest value (at min or max) fit framed.
func (k *ccpKnob) scale() int {
	widest := len(k.name)
	for _, v := range []float64{k.min, k.max} {
		if n := len(k.num(v) + k.unit); n > widest {
			widest = n
		}
	}
	if widest <= 4 {
		return 2
	}
	return 1
}

// parseCCPKnob reads "N=NAME,MIN,MAX,STEP,UNIT[,INIT]".
func parseCCPKnob(s string) (*ccpKnob, error) {
	id, rest, ok := strings.Cut(s, "=")
	n, err := strconv.Atoi(strings.TrimSpace(id))
	f := strings.Split(rest, ",")
	if !ok || err != nil || len(f) < 5 {
		return nil, fmt.Errorf("--knob %q: expected N=NAME,MIN,MAX,STEP,UNIT[,INIT]", s)
	}
	k := &ccpKnob{button: n, name: strings.TrimSpace(f[0]), unit: strings.TrimSpace(f[4])}
	for i, v := range []*float64{&k.min, &k.max, &k.step} {
		if *v, err = strconv.ParseFloat(strings.TrimSpace(f[1+i]), 64); err != nil {
			return nil, fmt.Errorf("--knob %q: %s is not a number", s, f[1+i])
		}
	}
	if k.min >= k.max || k.step <= 0 {
		return nil, fmt.Errorf("--knob %q: need MIN < MAX and STEP > 0", s)
	}
	k.val = k.min
	if len(f) > 5 {
		if k.val, err = strconv.ParseFloat(strings.TrimSpace(f[5]), 64); err != nil {
			return nil, fmt.Errorf("--knob %q: INIT %s is not a number", s, f[5])
		}
	}
	if i := strings.IndexByte(strings.TrimSpace(f[3]), '.'); i >= 0 {
		k.decimals = len(strings.TrimSpace(f[3])) - i - 1
	}
	k.clamp()
	return k, nil
}

func (k *ccpKnob) clamp() {
	if k.val < k.min {
		k.val = k.min
	}
	if k.val > k.max {
		k.val = k.max
	}
}

func runCCPPanel(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("consumer ccp panel", flag.ContinueOnError)
	var labels, colours stringSliceFlag
	fs.Var(&labels, "label", "repeatable: N=TEXT ('|' = new line)")
	fs.Var(&colours, "colour", "repeatable: N=NAME or N=0xNN")
	layout := fs.String("layout", "", "CSV file: button,label,colour[,frame][,image]")
	var blinkFlags stringSliceFlag
	fs.Var(&blinkFlags, "blink", "repeatable: button number to blink (colour / off)")
	blinkRate := fs.Duration("blink-rate", 400*time.Millisecond, "blink half-period")
	var imageFlags stringSliceFlag
	fs.Var(&imageFlags, "image", "repeatable: N=FILE[,invert][,dither][,margin=N][,threshold=N] — PNG/JPEG/GIF logo, scaled to 64x32")
	var knobFlags stringSliceFlag
	fs.Var(&knobFlags, "knob", "repeatable: N=NAME,MIN,MAX,STEP,UNIT[,INIT] — press button N to select, turn the rotary to change")
	rotary := fs.Int("rotary", 17, "button id the rotary reports as (CCP-1601: 17)")
	frameAll := fs.Bool("frame", false, "draw a 1-pixel frame on every label")
	syslogAddr := fs.String("syslog-addr", "", "also send every action as an RFC 5424 record to host:port (UDP)")
	logPath := fs.String("log", "", "also append every action as one JSON line to this file")
	cerebrumTarget := fs.String("cerebrum", "", "XY mode: Cerebrum Northbound host[:port] (account from DHS_CEREBRUM_USER / DHS_CEREBRUM_PASS)")
	var destFlags, srcFlags stringSliceFlag
	fs.Var(&destFlags, "dest", "XY mode, repeatable: BUTTON=CEREBRUM_DEST_ID (green; press to select)")
	fs.Var(&srcFlags, "src", "XY mode, repeatable: BUTTON=CEREBRUM_SRC_ID (purple; press to take onto the selected destination)")
	xyLevels := fs.String("levels", "", "XY mode: level IDs to take, e.g. 1;2 (empty = every level the source is bound on)")
	destColour := fs.String("dest-colour", "green", "XY mode: destination colour")
	srcColour := fs.String("src-colour", "lavender", "XY mode: source colour")
	tallyColour := fs.String("tally-colour", "yellow", "XY mode: colour of the source feeding the selected destination")
	labelSet := fs.Int("label-set", 0, "XY mode: Cerebrum label set shown on the keys: 0 = primary mnemonic, N = alternate set N (e.g. Panels)")
	duration := fs.Duration("duration", 0, "stop after this long (0 = until Ctrl+C)")
	pollEvery := fs.Duration("poll", time.Second, "poll interval (keeps the session live, detects refresh requests)")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: dhs consumer ccp panel <host>[:port] [--label N=TEXT]… [--colour N=NAME]… [--layout FILE]")
	}
	buttons := map[int]*ccpButton{}
	get := func(id int) *ccpButton {
		if buttons[id] == nil {
			buttons[id] = &ccpButton{}
		}
		return buttons[id]
	}
	if *layout != "" {
		f, err := os.Open(*layout)
		if err != nil {
			return err
		}
		rows, err := csv.NewReader(f).ReadAll()
		_ = f.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", *layout, err)
		}
		for i, r := range rows {
			if i == 0 && len(r) > 0 && strings.EqualFold(strings.TrimSpace(r[0]), "button") {
				continue
			}
			if len(r) < 2 {
				continue
			}
			id, err := strconv.Atoi(strings.TrimSpace(r[0]))
			if err != nil {
				return fmt.Errorf("%s row %d: button %q", *layout, i+1, r[0])
			}
			get(id).label = r[1]
			if len(r) > 5 && strings.TrimSpace(r[5]) != "" && strings.TrimSpace(r[5]) != "0" {
				get(id).blink = true
			}
			if len(r) > 4 && strings.TrimSpace(r[4]) != "" {
				img, err := ccpLoadImage(r[4])
				if err != nil {
					return fmt.Errorf("%s row %d: %w", *layout, i+1, err)
				}
				get(id).image = img
			}
			if len(r) > 3 && strings.TrimSpace(r[3]) != "" && strings.TrimSpace(r[3]) != "0" {
				get(id).frame = true
			}
			if len(r) > 2 && strings.TrimSpace(r[2]) != "" {
				c, err := ccpc.ParseColour(r[2])
				if err != nil {
					return fmt.Errorf("%s row %d: %w", *layout, i+1, err)
				}
				get(id).colour, get(id).hasCol = c, true
			}
		}
	}
	for _, l := range labels {
		id, text, ok := strings.Cut(l, "=")
		n, err := strconv.Atoi(strings.TrimSpace(id))
		if !ok || err != nil {
			return fmt.Errorf("--label %q: expected N=TEXT", l)
		}
		get(n).label = text
	}
	for _, c := range colours {
		id, name, ok := strings.Cut(c, "=")
		n, err := strconv.Atoi(strings.TrimSpace(id))
		if !ok || err != nil {
			return fmt.Errorf("--colour %q: expected N=NAME", c)
		}
		v, err := ccpc.ParseColour(name)
		if err != nil {
			return err
		}
		get(n).colour, get(n).hasCol = v, true
	}

	knobs := map[int]*ccpKnob{}
	for _, im := range imageFlags {
		id, spec, ok := strings.Cut(im, "=")
		n, err := strconv.Atoi(strings.TrimSpace(id))
		if !ok || err != nil {
			return fmt.Errorf("--image %q: expected N=FILE[,invert][,dither][,margin=N][,threshold=N]", im)
		}
		img, err := ccpLoadImage(spec)
		if err != nil {
			return err
		}
		get(n).image = img
	}
	for _, bf := range blinkFlags {
		n, err := strconv.Atoi(strings.TrimSpace(bf))
		if err != nil {
			return fmt.Errorf("--blink %q: a button number", bf)
		}
		get(n).blink = true
	}
	for _, kf := range knobFlags {
		k, err := parseCCPKnob(kf)
		if err != nil {
			return err
		}
		knobs[k.button] = k
		b := get(k.button)
		b.label = k.text()
		if !b.hasCol {
			b.colour, b.hasCol = codec.NamedColours["amber"], true
		}
	}
	var xy *ccpXY
	if *cerebrumTarget != "" {
		for _, c := range []struct {
			v   string
			dst *byte
		}{{*destColour, &xyDestColour}, {*srcColour, &xySrcColour}, {*tallyColour, &xyTallyColour}} {
			v, err := ccpc.ParseColour(c.v)
			if err != nil {
				return err
			}
			*c.dst = v
		}
		dm, err := parseXYMap("dest", destFlags)
		if err != nil {
			return err
		}
		sm, err := parseXYMap("src", srcFlags)
		if err != nil {
			return err
		}
		var lv []string
		for _, l := range strings.FieldsFunc(*xyLevels, func(r rune) bool { return r == ';' || r == ',' }) {
			lv = append(lv, strings.TrimSpace(l))
		}
		xy, err = newCCPXY(ctx, *cerebrumTarget, dm, sm, lv, *labelSet)
		if err != nil {
			return err
		}
		defer xy.close()
		for b := range dm {
			bt := get(b)
			if bt.label == "" {
				bt.label = xy.label(b)
			}
			bt.colour, bt.hasCol = xyDestColour, true
		}
		for b := range sm {
			bt := get(b)
			if bt.label == "" {
				bt.label = xy.label(b)
			}
			bt.colour, bt.hasCol = xySrcColour, true
		}
		fmt.Printf("XY: %d destination(s), %d source(s) from Cerebrum %s, label set %d\n", len(dm), len(sm), *cerebrumTarget, *labelSet)
	}
	xySelected := 0
	// xyTally lights the source feeding the selected destination yellow
	// and every other source purple; the selected destination is framed.
	xyTally := func(p *ccpc.Plugin) {
		if xy == nil {
			return
		}
		feeding := ""
		if xySelected != 0 {
			feeding = xy.sourceOf(xy.dests[xySelected])
		}
		for b, id := range xy.srcs {
			c := xySrcColour
			if id == feeding && feeding != "" {
				c = xyTallyColour
			}
			if buttons[b].colour != c {
				buttons[b].colour = c
				_ = p.SetColour(ctx, b, c)
			}
		}
		for b := range xy.dests {
			sel := b == xySelected
			if buttons[b].frame != sel {
				buttons[b].frame = sel
				_ = p.SetLabelOpts(ctx, b, buttons[b].label, sel)
			}
			if buttons[b].blink && !sel {
				_ = p.SetColour(ctx, b, buttons[b].colour) // stop blinking on its colour, not off
			}
			buttons[b].blink = sel
		}
	}

	if *frameAll {
		for _, b := range buttons {
			b.frame = true
		}
	}
	selected := 0

	// Structured sinks: one record per action, the button's position and
	// what it shows included, so a collector can tell which key on which
	// panel was pressed.
	var sinks []slog.Handler
	if *syslogAddr != "" {
		udp, err := dialSyslogUDP(*syslogAddr)
		if err != nil {
			return fmt.Errorf("--syslog-addr %s: %w", *syslogAddr, err)
		}
		defer udp.Close()
		sinks = append(sinks, udp.Handler(slog.LevelInfo))
	}
	if *logPath != "" {
		f, err := os.OpenFile(*logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("--log %s: %w", *logPath, err)
		}
		defer func() { _ = f.Close() }()
		sinks = append(sinks, slog.NewJSONHandler(f, nil))
	}
	var events *slog.Logger
	if len(sinks) > 0 {
		events = slog.New(teeHandler(sinks))
	}

	host, port, err := splitHostPort(fs.Arg(0), codec.DefaultPort)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	if *duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *duration)
		defer cancel()
	}

	draw := func(p *ccpc.Plugin) {
		ids := make([]int, 0, len(buttons))
		for id := range buttons {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		for _, id := range ids {
			b := buttons[id]
			if b.hasCol {
				if err := p.SetColour(ctx, id, b.colour); err != nil {
					fmt.Fprintf(os.Stderr, "button %d colour: %v\n", id, err)
				}
			}
			switch {
			case b.image != nil:
				bm := *b.image
				if b.label != "" {
					bm.Overlay(codec.TextScaled(b.label, b.frame, codec.SizeAuto))
				}
				if err := p.SetBitmap(ctx, id, &bm); err != nil {
					fmt.Fprintf(os.Stderr, "button %d image: %v\n", id, err)
				}
			case b.label != "":
				scale := 0
				if k := knobs[id]; k != nil {
					scale = k.scale()
				}
				if err := p.SetLabelScaled(ctx, id, b.label, b.frame, scale); err != nil {
					fmt.Fprintf(os.Stderr, "button %d label: %v\n", id, err)
				}
			}
		}
		fmt.Printf("%s drew %d button(s)\n", time.Now().Format("15:04:05"), len(ids))
	}

	for ctx.Err() == nil {
		p := (&ccpc.Factory{}).New(plugin.Deps{}).(*ccpc.Plugin)
		if err := p.Connect(ctx, host, port); err != nil {
			fmt.Fprintf(os.Stderr, "%s connect %s:%d: %v — retry in 5 s\n", time.Now().Format("15:04:05"), host, port, err)
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
			}
			continue
		}
		pr, fw, n := p.Status()
		fmt.Printf("%s connected: %s, protocol %d, firmware %s, %d buttons, PSU A %v B %v\n",
			time.Now().Format("15:04:05"), pr.Model, pr.ProtocolVersion, fw, n, pr.PSUA, pr.PSUB)
		if events != nil {
			events.Info("ccp connected", "panel", host, "model", pr.Model, "protocol", int(pr.ProtocolVersion),
				"firmware", fw, "buttons", n, "psu_a", pr.PSUA, "psu_b", pr.PSUB)
		}
		_ = p.Subscribe(dhsc.ValueRequest{Path: "gpi"}, func(e dhsc.Event) {
			fmt.Printf("%s GPI %-3d %s\n", time.Now().Format("15:04:05.000"), e.ID, e.Value.Str)
			if events != nil {
				events.Info("ccp gpi", "panel", host, "model", pr.Model, "input", e.ID, "state", e.Value.Str)
			}
		})
		knobEvents := make(chan codec.Button, 64)
		p.OnFrame(func(f codec.Frame) {
			if f.Cmd == codec.RspButton {
				b, err := codec.ParseButton(f)
				if err == nil {
					fmt.Printf("%s button %-3d %s %d\n", time.Now().Format("15:04:05.000"), b.ID, b.ActionName(), b.Value)
					if events != nil {
						control, row, col := ccpPosition(pr.Model, int(b.ID), *rotary)
						label, colour := "", ""
						if bt := buttons[int(b.ID)]; bt != nil {
							label = strings.ReplaceAll(bt.label, "|", " ")
							if bt.hasCol {
								colour = fmt.Sprintf("0x%02x", bt.colour)
							}
						}
						events.Info("ccp "+control, "panel", host, "model", pr.Model, "button", int(b.ID), "row", row, "col", col,
							"action", b.ActionName(), "value", int(b.Value), "label", label, "colour", colour)
					}
					if len(knobs) > 0 || xy != nil {
						select {
						case knobEvents <- b:
						default:
						}
					}
				}
				return
			}
			fmt.Printf("%s %s % x\n", time.Now().Format("15:04:05.000"), f.Name(), f.Data)
		})
		draw(p)
		tick := time.NewTicker(*pollEvery)
		blinkTick := time.NewTicker(*blinkRate)
		blinkOn := true
	loop:
		for {
			select {
			case <-ctx.Done():
				break loop
			case <-p.Done():
				fmt.Fprintf(os.Stderr, "%s connection lost — reconnecting\n", time.Now().Format("15:04:05"))
				break loop
			case b := <-knobEvents:
				switch {
				case xy != nil && b.Action == codec.ActionDown && xy.dests[int(b.ID)] != "":
					xySelected = int(b.ID)
					_ = xy.refresh(ctx, false)
					xyTally(p)
					d := xy.dests[xySelected]
					fmt.Printf("%s XY destination %s (%s), fed by source %s\n", time.Now().Format("15:04:05"), d, xy.dstName[d], xy.sourceOf(d))
				case xy != nil && b.Action == codec.ActionDown && xy.srcs[int(b.ID)] != "" && xySelected != 0:
					d, src := xy.dests[xySelected], xy.srcs[int(b.ID)]
					err := xy.take(ctx, d, src)
					got := xy.sourceOf(d)
					fmt.Printf("%s XY take %s (%s) <- %s (%s): err=%v, Cerebrum now reads source %s\n", time.Now().Format("15:04:05"),
						d, xy.dstName[d], src, xy.srcName[src], err, got)
					if events != nil {
						events.Info("ccp take", "panel", host, "dest", d, "dest_label", xy.dstName[d], "src", src,
							"src_label", xy.srcName[src], "levels", strings.Join(xy.levels, ";"), "confirmed", got == src, "err", fmt.Sprint(err))
					}
					if err != nil {
						// Any failed take may mean a dead Northbound session
						// (seen live: a timeout, then "broken pipe" on every
						// later write). Reconnect and retry the take once.
						if cerr := xy.connect(ctx); cerr != nil {
							fmt.Fprintf(os.Stderr, "%s XY reconnect to Cerebrum failed: %v\n", time.Now().Format("15:04:05"), cerr)
						} else if err = xy.take(ctx, d, src); err == nil {
							got = xy.sourceOf(d)
							fmt.Printf("%s XY retake after reconnect: Cerebrum now reads source %s\n", time.Now().Format("15:04:05"), got)
						}
					}
					xyTally(p)
				case b.Action == codec.ActionDown && knobs[int(b.ID)] != nil:
					prev := selected
					selected = int(b.ID)
					if prev != 0 && prev != selected {
						buttons[prev].frame, buttons[prev].colour = false, codec.NamedColours["amber"]
						_ = p.SetColour(ctx, prev, buttons[prev].colour)
						_ = p.SetLabelScaled(ctx, prev, knobs[prev].text(), false, knobs[prev].scale())
					}
					k := knobs[selected]
					buttons[selected].frame, buttons[selected].colour = true, codec.NamedColours["green"]
					_ = p.SetColour(ctx, selected, buttons[selected].colour)
					_ = p.SetLabelScaled(ctx, selected, k.text(), true, k.scale())
					fmt.Printf("%s selected %s = %s%s\n", time.Now().Format("15:04:05"), k.name, k.num(k.val), k.unit)
				case int(b.ID) == *rotary && selected != 0 && (b.Action == codec.ActionIncrement || b.Action == codec.ActionDecrement):
					k := knobs[selected]
					steps := float64(b.Value)
					if steps == 0 {
						steps = 1
					}
					if b.Action == codec.ActionDecrement {
						steps = -steps
					}
					k.val += steps * k.step
					k.clamp()
					buttons[selected].label = k.text()
					_ = p.SetLabelScaled(ctx, selected, k.text(), true, k.scale())
					fmt.Printf("%s %s = %s%s (min %s, max %s)\n", time.Now().Format("15:04:05"), k.name, k.num(k.val), k.unit, k.num(k.min), k.num(k.max))
					if events != nil {
						events.Info("ccp knob", "panel", host, "button", selected, "name", k.name, "value", k.val,
							"unit", k.unit, "min", k.min, "max", k.max, "step", k.step)
					}
				}
			case <-blinkTick.C:
				blinkOn = !blinkOn
				for id, b := range buttons {
					if !b.blink {
						continue
					}
					c := codec.NamedColours["off"]
					if blinkOn {
						c = b.colour
					}
					_ = p.SetColour(ctx, id, c)
				}
			case <-tick.C:
				pr, err := p.Poll(ctx)
				if err != nil {
					if ctx.Err() == nil {
						fmt.Fprintf(os.Stderr, "poll: %v\n", err)
					}
					continue
				}
				if pr.RefreshRequired {
					draw(p)
				}
			}
		}
		tick.Stop()
		blinkTick.Stop()
		_ = p.Disconnect()
	}
	fmt.Println("released the panel")
	return nil
}
