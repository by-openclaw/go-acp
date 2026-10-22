package main

import (
	"context"
	"flag"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"dhs/internal/cerebrum-nb/codec"
)

// list-tags — the Northbound read of a RouteMaster source's or destination's
// tags (v0.17 §5.1.7 / §5.1.8): ONE one-shot OBTAIN of RM_SRCE_TAGS /
// RM_DEST_TAGS; the reply is one <TAGS AVAILABLE LIST="a|b"/> row per level
// (LEVEL_ID="*") or the one level asked for.
//
//	dhs consumer cerebrum-nb list-tags 10.44.55.39 --kind source --id 16 --level 1
//	dhs consumer cerebrum-nb list-tags 10.44.55.39 --kind destination
//
// It is the read-back of set-tags over the same interface — a probe that
// verifies a tag write without a second protocol.
func cerebrumListTags(_ context.Context, args []string) error {
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb list-tags", flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	router := fs.String("router", "0.0.0.0", "router IP target (route-master sentinel 0.0.0.0) or a physical Router IP")
	deviceType := fs.String("device-type", "ROUTER", "route-master device type")
	kind := fs.String("kind", "source", "source | destination")
	id := fs.String("id", "*", "ONE source / destination ID, or * for every one")
	level := fs.String("level", "*", "ONE level ID, or * for every level (one row per level)")
	idle := fs.Duration("idle", 5*time.Second, "stop collecting this long after the last row if no WILDCARD_COMPLETE arrives")
	output := fs.String("output", "text", "stdout format: text | json")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return cerebrumValErr("list-tags", "missing host[:port] argument")
	}
	var tagType string
	switch strings.ToLower(*kind) {
	case "source", "sources", "srce":
		tagType = "RM_SRCE_TAGS"
	case "destination", "destinations", "dest":
		tagType = "RM_DEST_TAGS"
	default:
		return cerebrumValErr("list-tags", "--kind must be source or destination")
	}
	switch *output {
	case "text", "json":
	default:
		return cerebrumValErr("list-tags", "--output must be text or json")
	}
	host, portArg, err := splitHostPort(rest[0], cf.port)
	if err != nil {
		return err
	}
	cf.port = portArg
	cerebrumExpandAutoPaths(cf, "list-tags", host)

	p, err := cerebrumDial(cf, host, "list-tags")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()
	sess := p.Session()

	var mu sync.Mutex
	var rows []cerebrumTagRow
	done := make(chan struct{})
	var once sync.Once
	tick := make(chan struct{}, 1)

	sess.OnEvent(codec.KindRoutingChange, func(f *codec.Frame) {
		rc := f.Routing
		if rc == nil || rc.Type != tagType {
			return
		}
		row := cerebrumTagRow{ID: rc.SrceID + rc.DestID, Level: rc.LevelID, Available: rc.TagsAvailable, Tags: rc.TagList}
		if row.ID == "" {
			return
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
		if f.MTID == "" {
			return
		}
		once.Do(func() { close(done) })
	})

	item := &codec.RoutingChange{Type: tagType, IPAddress: *router, DeviceType: codec.DeviceType(*deviceType), LevelID: *level}
	if tagType == "RM_SRCE_TAGS" {
		item.SrceID = *id
	} else {
		item.DestID = *id
	}
	obtCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sess.Obtain(obtCtx, []codec.SubItem{item}); err != nil {
		return fmt.Errorf("cerebrum-nb list-tags: %s obtain refused: %w", tagType, err)
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
	list := append([]cerebrumTagRow(nil), rows...)
	mu.Unlock()
	sortCerebrumTagRows(list)

	if *output == "json" {
		type row struct {
			ID        string   `json:"id"`
			Level     string   `json:"level"`
			Available bool     `json:"available"`
			Tags      []string `json:"tags"`
		}
		jrows := []row{}
		for _, r := range list {
			tags := r.Tags
			if tags == nil {
				tags = []string{}
			}
			jrows = append(jrows, row{r.ID, r.Level, r.Available, tags})
		}
		return printCerebrumJSON(map[string]any{"kind": tagType, "tags": jrows})
	}
	fmt.Printf("count %d\n", len(list))
	for _, r := range list {
		fmt.Println(formatCerebrumTagRow(r))
	}
	return nil
}

// cerebrumTagRow is one <TAGS/> reply row of an RM_*_TAGS obtain: the
// source / destination ID and level echoed from the request (§5.1.7
// "attributes as per TX") with the LIST split on '|'.
type cerebrumTagRow struct {
	ID        string
	Level     string
	Available bool
	Tags      []string
}

// formatCerebrumTagRow renders one row for the terminal: the tags joined by
// ';' (the same convention as the capability levels), or "-" when the level
// carries none.
func formatCerebrumTagRow(r cerebrumTagRow) string {
	tags := "-"
	if len(r.Tags) > 0 {
		tags = strings.Join(r.Tags, ";")
	}
	return fmt.Sprintf("%8s  level %-4s  available=%-5v  tags=%s", r.ID, r.Level, r.Available, tags)
}

// sortCerebrumTagRows orders by numeric ID then numeric level; non-numeric
// values (a wildcard echo) sort after the numbers, by string.
func sortCerebrumTagRows(rows []cerebrumTagRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].ID != rows[j].ID {
			return cerebrumIDLess(rows[i].ID, rows[j].ID)
		}
		return cerebrumIDLess(rows[i].Level, rows[j].Level)
	})
}

// cerebrumIDLess compares two wire IDs numerically when both parse, else by
// string with numbers first.
func cerebrumIDLess(a, b string) bool {
	var ai, bi int
	_, errA := fmt.Sscanf(a, "%d", &ai)
	_, errB := fmt.Sscanf(b, "%d", &bi)
	switch {
	case errA == nil && errB == nil:
		return ai < bi
	case errA == nil:
		return true
	case errB == nil:
		return false
	}
	return a < b
}
