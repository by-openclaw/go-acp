package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"dhs/internal/cerebrum-nb/codec"
)

// list-sources / list-dests / list-levels — first-class inventory verbs.
//
// Each issues ONE one-shot OBTAIN (0v16 §2.4) of the corresponding *_MNE
// wildcard row (§5.1.4-§5.1.6) against the route-master and prints every
// configured ID with its label — the full catalogue a controller needs to
// make any crosspoint by ID or by name (§1.8). No SUBSCRIBE anywhere: the
// request ends with the MTID-carrying WILDCARD_COMPLETE (§1.6), with --idle
// as the quiet-stream fallback.
//
//	dhs consumer cerebrum-nb list-sources 10.44.55.39 --port 40009 --user U --pass P
//	dhs consumer cerebrum-nb list-levels  10.44.55.39 --port 40009 --out levels.csv
//
// --out writes the same CSV shape the export set uses (srce|dest|level +
// mnemonic), so the file feeds straight into `import`.
func cerebrumListMne(_ context.Context, args []string, mneType, keyCol string) error {
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb list-"+keyCol, flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	router := fs.String("router", "0.0.0.0", "router IP target (route-master sentinel 0.0.0.0) or a physical Router IP")
	deviceType := fs.String("device-type", "ROUTER", "route-master device type")
	id := fs.String("id", "*", "query ONE resource ID instead of the wildcard (diagnostic single-row OBTAIN)")
	level := fs.String("level", "*", "LEVEL_ID filter to send (server requires the attribute present; a concrete value is echoed back in the RX row)")
	idle := fs.Duration("idle", 5*time.Second, "stop collecting this long after the last row if no WILDCARD_COMPLETE arrives")
	out := fs.String("out", "", "write the list as a CSV here (default: print a table)")
	output := fs.String("output", "text", "stdout format: text | json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return cerebrumValErr("list-"+keyCol, "missing host[:port] argument")
	}
	host, portArg, err := splitHostPort(rest[0], cf.port)
	if err != nil {
		return err
	}
	cf.port = portArg

	p, err := cerebrumDial(cf, host)
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()
	sess := p.Session()

	var mu sync.Mutex
	var rows []cerebrumMneRow
	done := make(chan struct{})
	var once sync.Once
	tick := make(chan struct{}, 1)

	sess.OnEvent(codec.KindRoutingChange, func(f *codec.Frame) {
		rc := f.Routing
		if rc == nil || rc.Type != mneType {
			return
		}
		var id string
		switch mneType {
		case "SRCE_MNE":
			id = rc.SrceID
		case "DEST_MNE":
			id = rc.DestID
		case "LEVEL_MNE":
			id = rc.LevelID
		}
		m := primaryMnemonic(rc)
		if id == "" || m == "" {
			return
		}
		row := cerebrumMneRow{ID: id, Mnemonic: m, Alts: altMnemonics(rc)}
		if mneType != "LEVEL_MNE" {
			// Capability: which levels this src/dst EXISTS on (associations;
			// row LEVEL_ID fallback) — the shuffle-decision input.
			row.Levels = mneLevelsFromChange(rc)
		}
		mu.Lock()
		rows = append(rows, row)
		mu.Unlock()
		select {
		case tick <- struct{}{}:
		default:
		}
	})
	sess.OnEvent(codec.KindWildcardComplete, func(f *codec.Frame) {
		// Only the MTID-carrying sentinel ends the request (§1.6); live NOC
		// Cerebrum also emits spurious MTID-less ones per event.
		if f.MTID == "" {
			return
		}
		once.Do(func() { close(done) })
	})

	// Live NOC Cerebrum (2026-08-15) NACKs 10:ONE_OR_MORE_OBTAINS_INVALID when
	// LEVEL_ID is absent from the SRCE_MNE / DEST_MNE filter, even though the
	// spec marks it router-ignored (§5.1.5/§5.1.6): this server requires every
	// wildcardable attribute to be present. LEVEL_ID="*" is spec-harmless on
	// routers, so send it always. (Same pattern: ROUTE/DEST_LOCK with
	// LEVEL_ID="*" were granted; SRCE_LOCK without it was refused.)
	item := &codec.RoutingChange{Type: mneType, IPAddress: *router, DeviceType: codec.DeviceType(*deviceType)}
	switch mneType {
	case "SRCE_MNE":
		item.SrceID = *id
		item.LevelID = *level
	case "DEST_MNE":
		item.DestID = *id
		item.LevelID = *level
	case "LEVEL_MNE":
		item.LevelID = *id
	}
	obtCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sess.Obtain(obtCtx, []codec.SubItem{item}); err != nil {
		return fmt.Errorf("cerebrum-nb list-%s: %s obtain refused: %w", keyCol, mneType, err)
	}

	idleTimer := time.NewTimer(*idle)
	defer idleTimer.Stop()
collect:
	for {
		select {
		case <-done:
			break collect
		case <-tick:
			if !idleTimer.Stop() {
				select {
				case <-idleTimer.C:
				default:
				}
			}
			idleTimer.Reset(*idle)
		case <-idleTimer.C:
			break collect
		}
	}

	mu.Lock()
	list := dedupeCerebrumMnes(rows)
	mu.Unlock()

	if *out != "" {
		if err := cerebrumWriteFile(*out, []byte(formatCerebrumMneCSV(keyCol, list))); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "cerebrum-nb list-%s: wrote %d row(s) to %s\n", keyCol, len(list), *out)
		return nil
	}
	switch *output {
	case "json":
		type row struct {
			ID       string         `json:"id"`
			Mnemonic string         `json:"mnemonic"`
			Alts     map[int]string `json:"alts,omitempty"`
			Levels   []string       `json:"levels,omitempty"`
		}
		jrows := []row{}
		for _, r := range list {
			jrows = append(jrows, row{r.ID, r.Mnemonic, r.Alts, r.Levels})
		}
		return printCerebrumJSON(map[string][]row{keyCol: jrows})
	case "text":
	default:
		return cerebrumValErr("list-"+keyCol, "--output must be text or json")
	}
	fmt.Printf("count %d\n", len(list))
	for _, r := range list {
		if mneType == "LEVEL_MNE" {
			fmt.Printf("%8s  %-24s  %s\n", r.ID, r.Mnemonic, formatAlts(r.Alts))
			continue
		}
		lv := "-"
		if len(r.Levels) > 0 {
			lv = joinSemis(r.Levels)
		}
		fmt.Printf("%8s  %-16s  %-28s  %s\n", r.ID, lv, r.Mnemonic, formatAlts(r.Alts))
	}
	return nil
}

// joinSemis joins level IDs with ';' (the same convention as the xpoint CSV).
func joinSemis(ids []string) string {
	out := ""
	for i, s := range ids {
		if i > 0 {
			out += ";"
		}
		out += s
	}
	return out
}
