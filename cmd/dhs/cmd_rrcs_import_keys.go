package main

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	rrcs "dhs/internal/rrcs/consumer"
)

// The key rows of an export — what sits on each key of a panel, its label
// and its mode — go through the key path of ensure, proven on the plant:
// a function is created or removed as a command object, the label and
// the mode are an edit of the key. The other key properties stay read
// only through import.
//
//	net.1.node.61.port.1024.key.0.1.14.cmd.CommandType   call-to-port-cmd   ("" empties the key)
//	net.1.node.61.port.1024.key.0.1.14.cmd.Target        net.1.node.63.port.1043 | conference.ID | group.ID | ifb.ID
//	net.1.node.61.port.1024.key.0.1.14.LabelValue        HYP1
//	net.1.node.61.port.1024.key.0.1.14.KeyMode           1 | 2 | 3 as read (Auto, Momentary, Latching), or the word

// rrcsKeyRowFields are the fields of a key row import takes, relative to
// the key.
var rrcsKeyRowFields = map[string]bool{"cmd.CommandType": true, "cmd.Target": true, "LabelValue": true, "KeyMode": true}

// rrcsSplitKeyRows takes the key rows out of an export: those import
// turns into desired keys, those it cannot (skipped), and the rest.
func rrcsSplitKeyRows(rows []rrcsRow, keep func(string) bool) (keys []rrcsDesiredKey, skips []rrcsSkip, rest []rrcsRow) {
	type keyRows struct {
		panel, key string
		fields     map[string]rrcsRow
	}
	byKey := map[string]*keyRows{}
	var order []string
	for _, row := range rows {
		panel, key, field, isKey := rrcsKeyRow(row.Path)
		if !isKey {
			rest = append(rest, row)
			continue
		}
		if !keep(row.Path) {
			continue
		}
		if !rrcsKeyRowFields[field] {
			skips = append(skips, rrcsSkip{"read_only", row.Path, "a key takes cmd.CommandType, cmd.Target, LabelValue and KeyMode through import"})
			continue
		}
		pos := panel + "|" + key
		kr := byKey[pos]
		if kr == nil {
			kr = &keyRows{panel: panel, key: key, fields: map[string]rrcsRow{}}
			byKey[pos] = kr
			order = append(order, pos)
		}
		if _, twice := kr.fields[field]; twice {
			skips = append(skips, rrcsSkip{"duplicate", row.Path, "the file gives this value twice"})
			continue
		}
		kr.fields[field] = row
	}
	for _, pos := range order {
		kr := byKey[pos]
		want := rrcsDesiredKey{Panel: kr.panel, Key: kr.key}
		ok := true
		if row, has := kr.fields["cmd.CommandType"]; has {
			switch strings.TrimSpace(row.Value) {
			case "":
				want.State = "absent"
			default:
				word := rrcsKeyFunctionWord(row.Value)
				if word == "" {
					skips = append(skips, rrcsSkip{"bad_value", row.Path, row.Value + " is not a function import can set (call-to-port-cmd, call-to-conference, call-to-group, call-to-ifb, reply-cmd)"})
					ok = false
				}
				want.Function = word
			}
		}
		if row, has := kr.fields["cmd.Target"]; has {
			want.Target = strings.TrimSpace(row.Value)
			if want.Function == "" && want.State != "absent" {
				skips = append(skips, rrcsSkip{"bad_value", row.Path, "a target needs the cmd.CommandType row of the same key"})
				ok = false
			}
		}
		if row, has := kr.fields["LabelValue"]; has {
			label := row.Value
			want.Label = &label
		}
		if row, has := kr.fields["KeyMode"]; has {
			mode := rrcsKeyModeWord(row.Value, row.ValueName)
			if mode == "" {
				skips = append(skips, rrcsSkip{"bad_value", row.Path, row.Value + " is not a key mode (1 Auto, 2 Momentary, 3 Latching as read, or the word)"})
				ok = false
			}
			want.Mode = mode
		}
		if ok {
			keys = append(keys, want)
		}
	}
	return keys, skips, rest
}

// rrcsKeyRow reads a key row's path into the panel, the key as ensure
// names it (EXPANSION.PAGE.KEY or vfunc.TYPE.N) and the field.
func rrcsKeyRow(path string) (panel, key, field string, ok bool) {
	for _, mark := range []string{".key.", ".vfunc."} {
		i := strings.Index(path, mark)
		if i < 0 {
			continue
		}
		panel = path[:i]
		parts := strings.Split(path[i+1:], ".") // key.E.P.K.field | vfunc.TYPE.N.field
		if mark == ".key." && len(parts) >= 5 {
			return panel, strings.Join(parts[1:4], "."), strings.Join(parts[4:], "."), true
		}
		if mark == ".vfunc." && len(parts) >= 4 {
			return panel, strings.Join(parts[0:3], "."), strings.Join(parts[3:], "."), true
		}
		return "", "", "", false
	}
	return "", "", "", false
}

// rrcsKeyFunctionWord gives the word of ensure for a command type as
// the export prints it (call-to-port-cmd → call-to-port).
func rrcsKeyFunctionWord(commandType string) string {
	for word, typ := range rrcsKeyFunctions {
		if typ == commandType || word == commandType {
			return word
		}
	}
	return ""
}

// rrcsKeyModeWord gives the word of ensure for a KeyMode as the export
// prints it: the read value (one more than the written one), or its name.
func rrcsKeyModeWord(value, name string) string {
	words := map[int]string{}
	for word, n := range rrcsKeyModes {
		words[n+1] = word
	}
	if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
		return words[n]
	}
	text := strings.ToLower(strings.TrimSpace(value + name))
	for _, word := range []string{"auto", "momentary", "latching"} {
		if strings.HasPrefix(text, word) {
			return word
		}
	}
	return ""
}

// rrcsImportKeys brings the key rows to the system through the key path
// of ensure and reports them as import does: one entry per key field
// that changes, one per key that could not be brought; a key already as
// wanted counts as unchanged.
func rrcsImportKeys(ctx context.Context, cf *rrcsFlags, client *rrcs.Client, keys []rrcsDesiredKey, dryRun bool) (applied []*rrcsApplied, unchanged, failed int, err error) {
	if len(keys) == 0 {
		return nil, 0, 0, nil
	}
	diff, failures, err := rrcsEnsureConfig(ctx, cf, client, &rrcsDesired{Keys: keys}, dryRun)
	if err != nil {
		return nil, 0, 0, err
	}
	result := "applied"
	if dryRun {
		result = "would_apply"
	}
	touched := map[string]bool{}
	for _, d := range diff {
		applied = append(applied, &rrcsApplied{Path: d.Field, Live: fmt.Sprint(d.From), Wanted: fmt.Sprint(d.To), Result: result})
		touched[d.Field] = true
	}
	for _, f := range failures {
		applied = append(applied, &rrcsApplied{Path: f.Field, Result: "failed", Error: f.Reason})
		touched[f.Field] = true
		failed++
	}
	sort.SliceStable(applied, func(i, j int) bool { return applied[i].Path < applied[j].Path })
	for _, k := range keys {
		pos := k.Panel + ".key." + k.Key
		if strings.HasPrefix(k.Key, "vfunc.") {
			pos = k.Panel + "." + k.Key
		}
		seen := false
		for field := range touched {
			if field == pos || strings.HasPrefix(field, pos+".") {
				seen = true
				break
			}
		}
		if !seen {
			unchanged++
		}
	}
	return applied, unchanged, failed, nil
}
