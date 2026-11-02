package main

// XY routing from an Axon panel through Cerebrum Northbound: destination
// buttons and source buttons, press a destination then a source to take.
// Colours follow the Lawo convention the operator uses: destinations green,
// sources purple, the source that feeds the selected destination yellow.

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	nbcodec "dhs/internal/cerebrum-nb/codec"
	cerebrum "dhs/internal/cerebrum-nb/consumer"
)

// XY colours (indexes picked on the lab CCP-1601 chart), settable with
// --dest-colour / --src-colour / --tally-colour. The deep purple 0x33 made
// the source text hard to read (operator, 2026-10-25): lavender 0x3A is the
// readable purple.
var (
	xyDestColour  byte = 0x0C // green
	xySrcColour   byte = 0x3A // lavender
	xyTallyColour byte = 0x24 // yellow: feeds the selected destination
)

const xyRouterTarget = "0.0.0.0"

type ccpXY struct {
	target string
	dests  map[int]string // button -> Cerebrum destination ID
	srcs   map[int]string // button -> Cerebrum source ID
	levels []string       // empty = every level (ROUTE without LEVEL_ID)
	altSet int            // 0 = primary mnemonic, N = alternate set N (ALT_MNE_N)

	p       *cerebrum.Plugin
	routes  map[string]map[string]string // dest -> level -> src
	dstName map[string]string
	srcName map[string]string
}

// parseXYMap reads "N=ID" items.
func parseXYMap(flagName string, items []string) (map[int]string, error) {
	out := map[int]string{}
	for _, it := range items {
		b, id, ok := strings.Cut(it, "=")
		n, err := strconv.Atoi(strings.TrimSpace(b))
		if !ok || err != nil || strings.TrimSpace(id) == "" {
			return nil, fmt.Errorf("--%s %q: expected BUTTON=CEREBRUM_ID", flagName, it)
		}
		out[n] = strings.TrimSpace(id)
	}
	return out, nil
}

func newCCPXY(ctx context.Context, target string, dests, srcs map[int]string, levels []string, altSet int) (*ccpXY, error) {
	x := &ccpXY{target: target, dests: dests, srcs: srcs, levels: levels, altSet: altSet}
	if err := x.connect(ctx); err != nil {
		return nil, err
	}
	return x, nil
}

func (x *ccpXY) connect(ctx context.Context) error {
	host, port, err := splitHostPort(x.target, 40007)
	if err != nil {
		return err
	}
	p := cerebrum.NewPlugin(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	p.Username = os.Getenv("DHS_CEREBRUM_USER")
	p.Password = os.Getenv("DHS_CEREBRUM_PASS")
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := p.Connect(dctx, host, port); err != nil {
		return fmt.Errorf("cerebrum %s: %w", x.target, err)
	}
	if x.p != nil {
		_ = x.p.Disconnect()
	}
	x.p = p
	return x.refresh(ctx, true)
}

// refresh reads the crosspoints (and, with names, the mnemonics) back
// from Cerebrum: the tally shows what Cerebrum holds, not what was asked.
func (x *ccpXY) refresh(ctx context.Context, names bool) error {
	st, err := cerebrumObtainState(ctx, x.p.Session(), xyRouterTarget, "ROUTER", 5*time.Second, cerebrumStateWant{
		Routes: true, SrcMne: names, DstMne: names, Verb: "ccp-xy",
	})
	if err != nil {
		return err
	}
	routes := map[string]map[string]string{}
	for _, r := range st.Routes {
		if routes[r.Dest] == nil {
			routes[r.Dest] = map[string]string{}
		}
		routes[r.Dest][r.Level] = r.Srce
	}
	x.routes = routes
	if names {
		x.dstName, x.srcName = map[string]string{}, map[string]string{}
		for _, m := range dedupeCerebrumMnes(st.Dst) {
			x.dstName[m.ID] = x.pick(m)
		}
		for _, m := range dedupeCerebrumMnes(st.Src) {
			x.srcName[m.ID] = x.pick(m)
		}
	}
	return nil
}

// pick returns the label of the chosen set: alternate set N when it has a
// value for this resource, the primary mnemonic otherwise.
func (x *ccpXY) pick(m cerebrumMneRow) string {
	if x.altSet > 0 {
		if v := strings.TrimSpace(m.Alts[x.altSet]); v != "" {
			return v
		}
	}
	return m.Mnemonic
}

// sourceOf returns the source feeding dest on the first routed level
// ("" when unrouted).
func (x *ccpXY) sourceOf(dest string) string {
	lv := x.routes[dest]
	if len(lv) == 0 {
		return ""
	}
	for _, l := range append(append([]string{}, x.levels...), "1", "2", "3", "4") {
		if s, ok := lv[l]; ok {
			return s
		}
	}
	for _, s := range lv {
		return s
	}
	return ""
}

// take routes src to dest: one ROUTE per configured level, or one ROUTE
// without LEVEL_ID (every level the source is bound on), then reads back.
func (x *ccpXY) take(ctx context.Context, dest, src string) error {
	send := func(level string) error {
		a := &nbcodec.RoutingAction{Type: "ROUTE", IPAddress: xyRouterTarget, DeviceType: nbcodec.DeviceType("ROUTER"),
			DestID: dest, SrceID: src, LevelID: level}
		actx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return x.p.Session().Action(actx, a)
	}
	if len(x.levels) == 0 {
		if err := send(""); err != nil {
			return err
		}
	}
	for _, l := range x.levels {
		if err := send(l); err != nil {
			return fmt.Errorf("level %s: %w", l, err)
		}
	}
	time.Sleep(time.Second)
	return x.refresh(ctx, false)
}

func (x *ccpXY) label(button int) string {
	if id, ok := x.dests[button]; ok {
		if n := x.dstName[id]; n != "" {
			return n
		}
		return "DST|" + id
	}
	if id, ok := x.srcs[button]; ok {
		if n := x.srcName[id]; n != "" {
			return n
		}
		return "SRC|" + id
	}
	return ""
}

func (x *ccpXY) close() {
	if x.p != nil {
		_ = x.p.Disconnect()
	}
}
