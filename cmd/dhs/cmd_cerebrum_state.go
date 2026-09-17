package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"dhs/internal/cerebrum-nb/codec"
	cerebrum "dhs/internal/cerebrum-nb/consumer"
)

// Shared live-state collection + ensure diffing for the cerebrum-nb
// export/import pair. Everything here is OBTAIN-based (0v16 §2.4 one-shot
// request/response — SUBSCRIBE stays watcher-only in `listen`).

// cerebrumStateWant selects which snapshots to OBTAIN.
type cerebrumStateWant struct {
	Routes     bool
	RouteLevel string // crosspoint read restricted to one level; "" = all ("*")
	SrcMne     bool
	DstMne     bool
	LvlMne     bool
	DestLock   bool // DEST_LOCK snapshot (LOCK_STATE + LOCKED_BY per dest x level)
	// StrictMne: a refused *_MNE obtain is an error (import needs live state
	// to diff against) instead of a warn-and-empty (export's degrade).
	StrictMne bool
	Verb      string // message prefix: "export" / "import"
}

// cerebrumLockSpec is one DEST_LOCK cell of the live snapshot.
type cerebrumLockSpec struct {
	Dest     string
	Level    string
	State    string // LOCK_STATE: LOCKED / PROTECTED / RELEASED / ... (live enum)
	LockedBy string
}

// cerebrumState is the raw collected snapshot (un-deduped, un-collapsed).
type cerebrumState struct {
	Routes     []routeSpec
	Src        []cerebrumMneRow
	Dst        []cerebrumMneRow
	Lvl        []cerebrumMneRow
	Locks      []cerebrumLockSpec
	CrossLevel int // routes skipped: SRCE_LEVEL != DEST_LEVEL (shuffle parked)
}

// cerebrumObtainState issues one-shot OBTAINs (one per row kind so a NACK on
// one cannot sink the others), collects RX rows until every granted request
// delivered its MTID-carrying WILDCARD_COMPLETE (live NOC also emits spurious
// MTID-less ones — ignored), with idle as the quiet-stream fallback.
func cerebrumObtainState(ctx context.Context, sess *cerebrum.Session, router, deviceType string, idle time.Duration, want cerebrumStateWant) (*cerebrumState, error) {
	var mu sync.Mutex
	st := &cerebrumState{}
	completes := 0
	tick := make(chan struct{}, 1)
	kick := func() {
		select {
		case tick <- struct{}{}:
		default:
		}
	}

	sess.OnEvent(codec.KindRoutingChange, func(f *codec.Frame) {
		rc := f.Routing
		if rc == nil {
			return
		}
		mu.Lock()
		switch rc.Type {
		case "ROUTE":
			// Undocumented live-wire sentinels (0 / 0xFFFFFFFE / 0xFFFFFFFF)
			// mean "cell unrouted" — not a route, not a warning.
			if cerebrumNoRouteSentinel(rc.RouteSourceID) {
				break
			}
			if crossLevelRoute(rc.LevelID, rc.RouteSourceLevelID) {
				st.CrossLevel++
				break
			}
			st.Routes = append(st.Routes, routeSpec{Dest: rc.DestID, Srce: rc.RouteSourceID, Level: rc.LevelID})
		case "SRCE_MNE":
			if m := primaryMnemonic(rc); m != "" && rc.SrceID != "" {
				st.Src = append(st.Src, cerebrumMneRow{ID: rc.SrceID, Mnemonic: m, Levels: mneLevelsFromChange(rc), Alts: altMnemonics(rc)})
			}
		case "DEST_MNE":
			if m := primaryMnemonic(rc); m != "" && rc.DestID != "" {
				st.Dst = append(st.Dst, cerebrumMneRow{ID: rc.DestID, Mnemonic: m, Levels: mneLevelsFromChange(rc), Alts: altMnemonics(rc)})
			}
		case "LEVEL_MNE":
			if m := primaryMnemonic(rc); m != "" && rc.LevelID != "" {
				st.Lvl = append(st.Lvl, cerebrumMneRow{ID: rc.LevelID, Mnemonic: m, Alts: altMnemonics(rc)})
			}
		case "DEST_LOCK":
			if rc.Lock != nil && rc.DestID != "" {
				st.Locks = append(st.Locks, cerebrumLockSpec{
					Dest: rc.DestID, Level: rc.LevelID,
					State: string(rc.Lock.LockState), LockedBy: rc.Lock.LockedBy,
				})
			}
		}
		mu.Unlock()
		kick()
	})
	sess.OnEvent(codec.KindWildcardComplete, func(f *codec.Frame) {
		if f.MTID == "" { // spurious per-event sentinel (live NOC deviation)
			return
		}
		mu.Lock()
		completes++
		mu.Unlock()
		kick()
	})

	type obtainItem struct {
		name string
		item codec.SubItem
	}
	var plan []obtainItem
	if want.Routes {
		lvl := want.RouteLevel
		if lvl == "" {
			lvl = "*"
		}
		plan = append(plan, obtainItem{"ROUTE", &codec.RoutingChange{Type: "ROUTE", IPAddress: router, DeviceType: codec.DeviceType(deviceType), DestID: "*", LevelID: lvl}})
	}
	// LEVEL_ID="*" on the MNE filters is REQUIRED by live NOC Cerebrum
	// (NACK 10 without it, 2026-08-15) and spec-harmless on routers.
	if want.SrcMne {
		plan = append(plan, obtainItem{"SRCE_MNE", &codec.RoutingChange{Type: "SRCE_MNE", IPAddress: router, DeviceType: codec.DeviceType(deviceType), SrceID: "*", LevelID: "*"}})
	}
	if want.DstMne {
		plan = append(plan, obtainItem{"DEST_MNE", &codec.RoutingChange{Type: "DEST_MNE", IPAddress: router, DeviceType: codec.DeviceType(deviceType), DestID: "*", LevelID: "*"}})
	}
	if want.LvlMne {
		plan = append(plan, obtainItem{"LEVEL_MNE", &codec.RoutingChange{Type: "LEVEL_MNE", IPAddress: router, DeviceType: codec.DeviceType(deviceType), LevelID: "*"}})
	}
	if want.DestLock {
		plan = append(plan, obtainItem{"DEST_LOCK", &codec.RoutingChange{Type: "DEST_LOCK", IPAddress: router, DeviceType: codec.DeviceType(deviceType), DestID: "*", LevelID: "*"}})
	}
	if len(plan) == 0 {
		return st, nil
	}

	obtCtx, obtCancel := context.WithCancel(context.Background())
	defer obtCancel()
	okSubs := 0
	for _, s := range plan {
		if err := sess.Obtain(obtCtx, []codec.SubItem{s.item}); err != nil {
			if s.name == "ROUTE" || want.StrictMne {
				return nil, fmt.Errorf("cerebrum-nb %s: %s obtain failed: %w", want.Verb, s.name, err)
			}
			fmt.Fprintf(os.Stderr, "cerebrum-nb %s: %s obtain refused (%v) — %s CSV will be empty\n", want.Verb, s.name, err, strings.ToLower(s.name))
			continue
		}
		okSubs++
	}

	idleTimer := time.NewTimer(idle)
	defer idleTimer.Stop()
collect:
	for {
		mu.Lock()
		doneAll := okSubs > 0 && completes >= okSubs
		mu.Unlock()
		if doneAll {
			break collect
		}
		select {
		case <-tick:
			if !idleTimer.Stop() {
				select {
				case <-idleTimer.C:
				default:
				}
			}
			idleTimer.Reset(idle)
		case <-idleTimer.C:
			break collect
		case <-ctx.Done():
			break collect
		}
	}

	mu.Lock()
	defer mu.Unlock()
	out := &cerebrumState{
		Routes:     append([]routeSpec(nil), st.Routes...),
		Src:        append([]cerebrumMneRow(nil), st.Src...),
		Dst:        append([]cerebrumMneRow(nil), st.Dst...),
		Lvl:        append([]cerebrumMneRow(nil), st.Lvl...),
		Locks:      append([]cerebrumLockSpec(nil), st.Locks...),
		CrossLevel: st.CrossLevel,
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Ensure diffing (ADR-0007): desired CSV vs live snapshot -> minimal changes.
// ---------------------------------------------------------------------------

// cerebrumMneChange is one label cell to converge: slot 0 = primary mnemonic,
// slot >= 1 = alternate set N (NOC: 1=Panels, 2=Ref_Short_Edit, ...).
type cerebrumMneChange struct {
	Kind string // SRCE_MNE / DEST_MNE / LEVEL_MNE
	ID   string
	Slot int
	From string // live value ("" = unset)
	To   string
}

// diffCerebrumMnemonics computes the label cells whose desired (non-empty)
// value differs from live. Empty desired cells and absent columns are NEVER
// changes by default (empty = unmanaged, not "clear"); resources absent from
// the CSV are untouched. With allowClear, an EMPTY cell in a MANAGED column
// (headerSlots; the primary column is always managed) whose live value is set
// becomes a clear-write (To: ""). Deterministic order: numeric ID, then slot.
func diffCerebrumMnemonics(kind string, live, desired []cerebrumMneRow, headerSlots []int, allowClear bool) []cerebrumMneChange {
	cur := map[string]cerebrumMneRow{}
	for _, r := range dedupeCerebrumMnes(live) {
		if _, dup := cur[r.ID]; !dup {
			cur[r.ID] = r
		}
	}
	var out []cerebrumMneChange
	for _, d := range desired {
		lv := cur[d.ID]
		if d.Mnemonic != "" && d.Mnemonic != lv.Mnemonic {
			out = append(out, cerebrumMneChange{Kind: kind, ID: d.ID, Slot: 0, From: lv.Mnemonic, To: d.Mnemonic})
		} else if allowClear && d.Mnemonic == "" && lv.Mnemonic != "" {
			out = append(out, cerebrumMneChange{Kind: kind, ID: d.ID, Slot: 0, From: lv.Mnemonic, To: ""})
		}
		// Managed alt slots: the row's own values plus (in clear mode) every
		// header column, so an empty managed cell can clear.
		slotSet := map[int]bool{}
		for s := range d.Alts {
			slotSet[s] = true
		}
		if allowClear {
			for _, s := range headerSlots {
				slotSet[s] = true
			}
		}
		slots := make([]int, 0, len(slotSet))
		for s := range slotSet {
			slots = append(slots, s)
		}
		sort.Ints(slots)
		for _, s := range slots {
			want := d.Alts[s]
			var have string
			if lv.Alts != nil {
				have = lv.Alts[s]
			}
			switch {
			case want != "" && want != have:
				out = append(out, cerebrumMneChange{Kind: kind, ID: d.ID, Slot: s, From: have, To: want})
			case want == "" && allowClear && have != "":
				out = append(out, cerebrumMneChange{Kind: kind, ID: d.ID, Slot: s, From: have, To: ""})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if c := cmpCerebrumID(out[i].ID, out[j].ID); c != 0 {
			return c < 0
		}
		return out[i].Slot < out[j].Slot
	})
	return out
}

// diffCerebrumRoutes computes the crosspoint cells (dest, level) whose desired
// source differs from live. Cells absent from the CSV are untouched (the
// import never disconnects). From carries the live source ("" = unrouted).
type cerebrumRouteChange struct {
	Dest, Level, From, To string
}

func diffCerebrumRoutes(live, desired []routeSpec) []cerebrumRouteChange {
	cur := map[string]string{} // dest\x00level -> src
	for _, r := range live {
		cur[r.Dest+"\x00"+r.Level] = r.Srce
	}
	var out []cerebrumRouteChange
	for _, d := range desired {
		if have := cur[d.Dest+"\x00"+d.Level]; have != d.Srce {
			out = append(out, cerebrumRouteChange{Dest: d.Dest, Level: d.Level, From: have, To: d.Srce})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if c := cmpCerebrumID(out[i].Dest, out[j].Dest); c != 0 {
			return c < 0
		}
		return cmpCerebrumID(out[i].Level, out[j].Level) < 0
	})
	return out
}

// altSlotArg renders a change's slot for Session.SetMnemonic: primary = "",
// alternate = its index.
func altSlotArg(slot int) string {
	if slot <= 0 {
		return ""
	}
	return strconv.Itoa(slot)
}
