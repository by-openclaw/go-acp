package main

// discover for SW-P-02 (#751 G8) — matrix-model parity with the
// SW-P-08 one-shot survey: dual-controller state + controller status
// + router configuration + the full crosspoint table, over one
// session. Pure composition of existing Send* calls; --output json
// emits one document reusing the G1b shapes.

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	codec02 "dhs/internal/probel-sw02p/codec"
)

type sw02DiscoverJSON struct {
	DualStatus   *sw02DualStatusJSON `json:"dual_status,omitempty"`
	Status       *sw02StatusJSON     `json:"status,omitempty"`
	RouterConfig *sw02RouterCfgJSON  `json:"router_config,omitempty"`
	Matrix       int                 `json:"matrix"`
	Level        int                 `json:"level"`
	Xpoints      []probelXpointJSON  `json:"xpoints"`
}

type sw02RouterCfgJSON struct {
	Response int                 `json:"response"` // 1 | 2
	LevelMap uint32              `json:"level_map"`
	Levels   []sw02RouterLvlJSON `json:"levels"`
}

type sw02RouterLvlJSON struct {
	Dests int `json:"dests"`
	Srcs  int `json:"srcs"`
}

// runProbelSW02Discover drives `dhs consumer probel-sw02p discover`.
func runProbelSW02Discover(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probel-sw02p-discover", flag.ContinueOnError)
	controller := fs.String("controller", "lh", "controller to query for status: lh | rh")
	extended := fs.Bool("extended", false, "force extended forms (rx 65) for the crosspoint sweep")
	output := fs.String("output", "text", "output format: text | json (ADR-0002)")
	timeout := fs.Duration("timeout", 60*time.Second, "overall timeout")
	addr, flagArgs := popPositional(args)
	if addr == "" {
		return fmt.Errorf("missing <host:port>")
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	jsonOut, oerr := resolveEnsureOutput(*output, false)
	if oerr != nil {
		return oerr
	}
	ctrl, err := parseController(*controller)
	if err != nil {
		return err
	}

	cctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	p, closer, err := dialProbelSW02(cctx, addr)
	if err != nil {
		return err
	}
	defer closer()

	doc := sw02DiscoverJSON{Matrix: int(p.MatrixConfig().MatrixID)}

	// Best-effort sections: a matrix that NAKs one command (spec: NAK =
	// unsupported, absorbed via compliance) must not kill the survey.
	if r, derr := p.SendDualControllerStatusRequest(cctx); derr == nil {
		who := "MASTER"
		if r.Active == codec02.ActiveControllerSlave {
			who = "SLAVE"
		}
		doc.DualStatus = &sw02DualStatusJSON{Who: who, IdleFaulty: r.IdleStatus == codec02.IdleControllerFaulty}
	} else {
		fmt.Fprintf(os.Stderr, "discover: dual-status unsupported: %v\n", derr)
	}
	if s, serr := p.SendStatusRequest(cctx, ctrl); serr == nil {
		doc.Status = &sw02StatusJSON{Controller: *controller, Idle: s.Idle, BusFault: s.BusFault, Overheat: s.Overheat}
	} else {
		fmt.Fprintf(os.Stderr, "discover: status unsupported: %v\n", serr)
	}
	if rc, rerr := p.SendRouterConfigRequest(cctx); rerr == nil {
		cfg := &sw02RouterCfgJSON{}
		switch {
		case rc.Response1 != nil:
			cfg.Response, cfg.LevelMap = 1, rc.Response1.LevelMap
			for _, l := range rc.Response1.Levels {
				cfg.Levels = append(cfg.Levels, sw02RouterLvlJSON{Dests: int(l.NumDestinations), Srcs: int(l.NumSources)})
			}
		case rc.Response2 != nil:
			cfg.Response, cfg.LevelMap = 2, rc.Response2.LevelMap
			for _, l := range rc.Response2.Levels {
				cfg.Levels = append(cfg.Levels, sw02RouterLvlJSON{Dests: int(l.NumDestinations), Srcs: int(l.NumSources)})
			}
		}
		if cfg.Response != 0 {
			doc.RouterConfig = cfg
		}
	} else {
		fmt.Fprintf(os.Stderr, "discover: router-config unsupported: %v\n", rerr)
	}

	// Crosspoint sweep — the survey's core, NOT best-effort.
	count, level, err := sw02UsageDstCount(cctx, p)
	if err != nil {
		return err
	}
	doc.Level = level
	rows, err := sw02Interrogations(cctx, p, count, level, *extended)
	if err != nil {
		return err
	}
	for _, r := range rows {
		doc.Xpoints = append(doc.Xpoints, probelXpointJSON{Dest: r.Dst, Srce: r.Src})
	}

	if jsonOut {
		return emitReadJSON(doc)
	}
	if doc.DualStatus != nil {
		fmt.Printf("dual-controller  active=%s idle_faulty=%v\n", doc.DualStatus.Who, doc.DualStatus.IdleFaulty)
	}
	if doc.Status != nil {
		fmt.Printf("status  controller=%s idle=%v bus_fault=%v overheat=%v\n",
			doc.Status.Controller, doc.Status.Idle, doc.Status.BusFault, doc.Status.Overheat)
	}
	if doc.RouterConfig != nil {
		fmt.Printf("router-config (response-%d)  level_map=0x%07x levels=%d\n",
			doc.RouterConfig.Response, doc.RouterConfig.LevelMap, len(doc.RouterConfig.Levels))
	}
	fmt.Printf("crosspoints matrix=%d level=%d (%d routed):\n", doc.Matrix, doc.Level, len(doc.Xpoints))
	for _, x := range doc.Xpoints {
		fmt.Printf("  dst=%d <- src=%d\n", x.Dest, x.Srce)
	}
	return nil
}
