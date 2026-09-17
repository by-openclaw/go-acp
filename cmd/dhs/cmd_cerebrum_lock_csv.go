package main

import (
	"fmt"
	"sort"
	"strings"

	"dhs/internal/cerebrum-nb/codec"
)

// -lock.csv — the DEST_LOCK half of a snapshot (SRCE_LOCK state is not
// readable on any live Cerebrum, so it has no file). Columns follow the
// xpoint grammar:
//
//	dest,state,levels[,locked_by]
//	12,LOCKED,1;2;3,Admin
//
// Export writes only SET cells (state != RELEASED) — an unlocked cell is
// simply absent. Import converges per (dest, level): a row's state is the
// desired state, an ABSENT dest/level is UNTOUCHED, and clearing a lock
// requires an EXPLICIT state=RELEASED row (never inferred from absence —
// locks protect production, absence must stay safe). locked_by is
// informational (the wire offers no way to set it) and ignored on import.

// cerebrumLockRow is one parsed -lock.csv row (levels still grouped).
type cerebrumLockRow struct {
	Dest     string
	State    string // canonical UPPERCASE wire value
	Levels   []string
	LockedBy string
}

// cerebrumLockStates is the importable state set: the 0v16 §3.2 set values
// plus the wire-actual clearing value RELEASED (live 2026-08-16; the spec's
// RELEASE/UNLOCKED clears NACK 8 on every Cerebrum tested).
var cerebrumLockStates = map[string]bool{
	"LOCKED": true, "PROTECTED": true,
	"LOCKED_PATH": true, "PROTECTED_PATH": true,
	"RELEASED": true,
}

// formatCerebrumLockCSV renders the DEST_LOCK snapshot. Cells group by
// (dest, state, locked_by) with levels collapsed ';'-joined; rows sort
// dest-then-state numeric-aware so exports diff cleanly.
func formatCerebrumLockCSV(locks []cerebrumLockSpec) string {
	type acc struct {
		dest, state, by string
		levels          []string
	}
	groups := map[string]*acc{}
	var order []string
	for _, l := range locks {
		state := strings.ToUpper(strings.TrimSpace(l.State))
		// Absent/RELEASED cells are the unlocked baseline — never exported.
		if l.Dest == "" || state == "" || state == "RELEASED" {
			continue
		}
		key := l.Dest + "\x00" + state + "\x00" + l.LockedBy
		g, ok := groups[key]
		if !ok {
			g = &acc{dest: l.Dest, state: state, by: l.LockedBy}
			groups[key] = g
			order = append(order, key)
		}
		seen := false
		for _, lv := range g.levels {
			if lv == l.Level {
				seen = true
				break
			}
		}
		if !seen && l.Level != "" {
			g.levels = append(g.levels, l.Level)
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := groups[order[i]], groups[order[j]]
		if c := cmpCerebrumID(a.dest, b.dest); c != 0 {
			return c < 0
		}
		return a.state < b.state
	})
	var b strings.Builder
	b.WriteString("dest,state,levels,locked_by\n")
	for _, key := range order {
		g := groups[key]
		sortCerebrumIDs(g.levels)
		b.WriteString(g.dest)
		b.WriteByte(',')
		b.WriteString(g.state)
		b.WriteByte(',')
		b.WriteString(strings.Join(g.levels, ";"))
		b.WriteByte(',')
		b.WriteString(g.by)
		b.WriteByte('\n')
	}
	return b.String()
}

// parseCerebrumLockCSV reads a -lock.csv. Header needs dest,state,levels;
// locked_by is optional (and ignored on import). State values validate
// against the importable enum so a typo can never fabricate a wire value.
func parseCerebrumLockCSV(data []byte, srcName string) ([]cerebrumLockRow, error) {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	start := 0
	for start < len(lines) && (strings.TrimSpace(lines[start]) == "" || strings.HasPrefix(strings.TrimSpace(lines[start]), "#")) {
		start++
	}
	if start >= len(lines) {
		return nil, fmt.Errorf("%s: empty (no header)", srcName)
	}
	header := strings.Split(strings.ToLower(strings.TrimSpace(lines[start])), ",")
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.TrimSpace(h)] = i
	}
	for _, col := range []string{"dest", "state", "levels"} {
		if _, ok := idx[col]; !ok {
			return nil, fmt.Errorf("%s: missing column %q", srcName, col)
		}
	}
	var out []cerebrumLockRow
	for n, line := range lines[start+1:] {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, ",")
		if len(f) < 3 {
			return nil, fmt.Errorf("%s line %d: need at least dest,state,levels", srcName, start+n+2)
		}
		row := cerebrumLockRow{
			Dest:   strings.TrimSpace(f[idx["dest"]]),
			State:  strings.ToUpper(strings.TrimSpace(f[idx["state"]])),
			Levels: splitLevelCell(f[idx["levels"]]),
		}
		if i, ok := idx["locked_by"]; ok && i < len(f) {
			row.LockedBy = strings.TrimSpace(f[i])
		}
		if row.Dest == "" {
			return nil, fmt.Errorf("%s line %d: empty dest", srcName, start+n+2)
		}
		if !cerebrumLockStates[row.State] {
			return nil, fmt.Errorf("%s line %d: state %q (want LOCKED|PROTECTED|LOCKED_PATH|PROTECTED_PATH|RELEASED — RELEASED is the wire-actual clear; UNLOCKED/RELEASE NACK on live Cerebrums)", srcName, start+n+2, row.State)
		}
		if len(row.Levels) == 0 {
			return nil, fmt.Errorf("%s line %d: empty levels (';'-separated level IDs)", srcName, start+n+2)
		}
		out = append(out, row)
	}
	return out, nil
}

// cerebrumLockChange is one converging DEST_LOCK cell.
type cerebrumLockChange struct {
	Dest, Level, From, To string
}

// diffCerebrumLocks compares desired rows against the live DEST_LOCK
// snapshot per (dest, level). Live-absent cell = RELEASED baseline.
// CSV-absent cell = untouched (not visited at all).
func diffCerebrumLocks(live []cerebrumLockSpec, want []cerebrumLockRow) []cerebrumLockChange {
	cur := map[string]string{}
	for _, l := range live {
		state := strings.ToUpper(strings.TrimSpace(l.State))
		if state == "" {
			state = "RELEASED"
		}
		cur[l.Dest+"\x00"+l.Level] = state
	}
	var out []cerebrumLockChange
	for _, w := range want {
		for _, lvl := range w.Levels {
			from, ok := cur[w.Dest+"\x00"+lvl]
			if !ok {
				from = "RELEASED"
			}
			if from != w.State {
				out = append(out, cerebrumLockChange{Dest: w.Dest, Level: lvl, From: from, To: w.State})
			}
		}
	}
	return out
}

// cerebrumLockStateMode maps a canonical CSV state to the codec LockKind
// the ROUTING LOCK action carries.
func cerebrumLockStateMode(state string) (codec.LockKind, error) {
	switch state {
	case "LOCKED":
		return codec.LockLocked, nil
	case "PROTECTED":
		return codec.LockProtected, nil
	case "LOCKED_PATH":
		return codec.LockLockedPath, nil
	case "PROTECTED_PATH":
		return codec.LockProtectedPath, nil
	case "RELEASED":
		return codec.LockReleased, nil
	}
	return "", fmt.Errorf("unmapped lock state %q", state)
}
