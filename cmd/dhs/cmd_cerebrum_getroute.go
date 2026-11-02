package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"time"

	"dhs/internal/cerebrum-nb/codec"
)

// cerebrumRouteRow is one level of one destination. Unrouted cells are
// listed: a level with no source is an answer, not a missing row.
type cerebrumRouteRow struct {
	Dest      string `json:"dest"`
	DestName  string `json:"dest_name,omitempty"`
	Level     string `json:"level"`
	LevelName string `json:"level_name,omitempty"`
	Srce      string `json:"srce,omitempty"`
	SrceName  string `json:"srce_name,omitempty"`
	Unrouted  bool   `json:"unrouted"`
}

// cerebrumRouteRows joins the crosspoints read for one destination with the
// level and source names. The server's "no route" markers never reach Routes,
// so a level absent from it is an unrouted level.
func cerebrumRouteRows(dest, level string, st *cerebrumState) []cerebrumRouteRow {
	names := func(rows []cerebrumMneRow) map[string]string {
		m := make(map[string]string, len(rows))
		for _, r := range rows {
			m[r.ID] = r.Mnemonic
		}
		return m
	}
	srcNames, dstNames := names(st.Src), names(st.Dst)
	routed := map[string]string{}
	for _, r := range st.Routes {
		if r.Dest == dest {
			routed[r.Level] = r.Srce
		}
	}
	rows := make([]cerebrumRouteRow, 0, len(st.Lvl))
	for _, l := range st.Lvl {
		if level != "" && l.ID != level {
			continue
		}
		row := cerebrumRouteRow{Dest: dest, DestName: dstNames[dest], Level: l.ID, LevelName: l.Mnemonic, Unrouted: true}
		if src, ok := routed[l.ID]; ok {
			row.Srce, row.SrceName, row.Unrouted = src, srcNames[src], false
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		a, _ := strconv.Atoi(rows[i].Level)
		b, _ := strconv.Atoi(rows[j].Level)
		return a < b
	})
	return rows
}

// cerebrumGetRoute answers "what feeds this destination?", one read, one
// destination, every level: `export` for one row. --dest takes an id or a
// mnemonic, --level an id.
func cerebrumGetRoute(ctx context.Context, args []string) error {
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb get-route", flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	dest := fs.String("dest", "", "destination id or mnemonic (required)")
	level := fs.String("level", "", "one level id; default: every level")
	router := fs.String("router", "0.0.0.0", "router IP_ADDRESS; default: the RouteMaster")
	output := fs.String("output", "text", "output format: text | json (ADR-0002)")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	if *dest == "" {
		return cerebrumValErr("get-route", "--dest is required")
	}
	jsonOut, oerr := resolveEnsureOutput(*output, false)
	if oerr != nil {
		return oerr
	}
	p, sess, _, err := dialCerebrumAuth(cf, fs.Args(), "get-route")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()

	id := *dest
	st, err := cerebrumObtainState(ctx, sess, *router, string(codec.DeviceType("ROUTER")), 15*time.Second, cerebrumStateWant{
		SrcMne: true, DstMne: true, LvlMne: true, StrictMne: true, Verb: "get-route",
	})
	if err != nil {
		return err
	}
	if !cerebrumIsID(id) {
		rs, rerr := cerebrumResolveRoutes([]routeSpec{{Dest: id, Srce: "*", Level: "*"}}, st, os.Stderr)
		if rerr != nil {
			return rerr
		}
		id = rs[0].Dest
	}
	routes, err := cerebrumObtainState(ctx, sess, *router, "ROUTER", 15*time.Second, cerebrumStateWant{
		Routes: true, RouteDest: id, Verb: "get-route",
	})
	if err != nil {
		return err
	}
	st.Routes, st.CrossLevel = routes.Routes, routes.CrossLevel
	rows := cerebrumRouteRows(id, *level, st)
	if len(rows) == 0 {
		return fmt.Errorf("cerebrum-nb get-route: level %q is not on router %s", *level, *router)
	}
	if st.CrossLevel > 0 {
		fmt.Fprintf(os.Stderr, "cerebrum-nb get-route: %d cross-level route(s) on this destination are not shown\n", st.CrossLevel)
	}
	if jsonOut {
		return json.NewEncoder(os.Stdout).Encode(rows)
	}
	for _, r := range rows {
		from := "unrouted"
		if !r.Unrouted {
			from = fmt.Sprintf("%s %q", r.Srce, r.SrceName)
		}
		fmt.Printf("dest %s %q level %s %q <- %s\n", r.Dest, r.DestName, r.Level, r.LevelName, from)
	}
	return nil
}
