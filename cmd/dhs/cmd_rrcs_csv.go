package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"dhs/internal/rrcs/codec"
)

// export and import follow the contract of the acp connectors: one
// snapshot file, one row per value, `export --format --out`, `import
// --file [--path] [--dry-run]`. The CSV carries the same header as
// theirs (internal/export/csv.go); the columns this protocol has nothing
// for stay empty.
var rrcsCSVHeader = []string{
	"ip", "protocol", "slot", "oid", "path",
	"id", "label", "kind", "access",
	"value", "value_name",
	"unit", "min", "max", "step", "default",
	"enum_items",
	"max_len",
	"alarm_priority", "alarm_tag", "alarm_on", "alarm_off",
	"slot_status",
}

// rrcsEnums are the numbers RRCS uses, with their word (§8.10.4.6
// Protocol, §8.10.4.34 PtpMode and RegistrationMode).
var rrcsEnums = map[string]map[int]string{
	"Protocol":         {2: "Manual", 3: "RTSP", 5: "NMOS"},
	"PtpMode":          {0: "Multicast", 1: "Hybrid"},
	"RegistrationMode": {0: "Automatic", 1: "Peer2Peer", 2: "Manual"},
}

// rrcsWritable are the properties import sends, by block. Everything
// else is exported for reading and skipped as read_only on import. The
// list is what the codeowner asked to provision; it grows as edits are
// verified against a real RRCS.
var rrcsWritable = map[string]map[string]bool{
	"PortAes67Input": {"Protocol": true, "Multicast": true, "MulticastPort": true, "Multicast2": true, "MulticastPort2": true,
		"SourceIp": true, "SourceIp2": true, "RTSPUri": true, "RTSPUri2": true,
		"Channels": true, "BitDepth": true, "PacketTime": true, "PayloadType": true},
	"PortAes67Output": {"Protocol": true, "Multicast": true, "MulticastPort": true, "Multicast2": true, "MulticastPort2": true,
		"Channels": true, "BitDepth": true, "PacketTime": true, "PayloadType": true},
	"Ptp":     {"PTP": true, "PtpPriority": true, "PtpPriority2": true, "PtpMode": true, "PtpAnnounceInterval": true},
	"Nmos":    {"Enable": true, "RegistrationMode": true, "RegistrationIp": true, "RegistrationPort": true},
	"Media_1": {"IpAddress": true, "DefaultGateway": true},
	"Media_2": {"IpAddress": true, "DefaultGateway": true},
}

// rrcsRow is one value of the system.
type rrcsRow struct {
	IP        string `json:"ip"`
	Protocol  string `json:"protocol"`
	Path      string `json:"path"`
	ID        string `json:"id,omitempty"`
	Label     string `json:"label"`
	Kind      string `json:"kind"`
	Access    string `json:"access"`
	Value     string `json:"value"`
	ValueName string `json:"value_name,omitempty"`
	EnumItems string `json:"enum_items,omitempty"`
}

func (r rrcsRow) record() []string {
	rec := make([]string, len(rrcsCSVHeader))
	for i, col := range rrcsCSVHeader {
		switch col {
		case "ip":
			rec[i] = r.IP
		case "protocol":
			rec[i] = r.Protocol
		case "path":
			rec[i] = r.Path
		case "id":
			rec[i] = r.ID
		case "label":
			rec[i] = r.Label
		case "kind":
			rec[i] = r.Kind
		case "access":
			rec[i] = r.Access
		case "value":
			rec[i] = r.Value
		case "value_name":
			rec[i] = r.ValueName
		case "enum_items":
			rec[i] = r.EnumItems
		}
	}
	return rec
}

// rrcsScalar prints a value RRCS gave, and names its kind.
func rrcsScalar(field string, v any) (value, kind, name, items string, ok bool) {
	switch t := v.(type) {
	case string:
		return t, "string", "", "", true
	case bool:
		return strconv.FormatBool(t), "bool", "", "", true
	case float64:
		n := int(t)
		if words, isEnum := rrcsEnums[field]; isEnum {
			keys := make([]int, 0, len(words))
			for k := range words {
				keys = append(keys, k)
			}
			sort.Ints(keys)
			parts := make([]string, 0, len(keys))
			for _, k := range keys {
				parts = append(parts, strconv.Itoa(k)+"="+words[k])
			}
			return strconv.Itoa(n), "enum", words[n], strings.Join(parts, "|"), true
		}
		return strconv.Itoa(n), "int", "", "", true
	}
	return "", "", "", "", false
}

// rrcsRowsOf flattens one object: its own values, and those of the
// blocks one level down.
func rrcsRowsOf(target, path string, id int, props map[string]any) []rrcsRow {
	var rows []rrcsRow
	add := func(block, field string, v any) {
		value, kind, name, items, ok := rrcsScalar(field, v)
		if !ok {
			return
		}
		prop, access := field, "R--"
		if block != "" {
			prop = block + "." + field
			if rrcsWritable[block][field] {
				access = "RW-"
			}
		}
		rows = append(rows, rrcsRow{IP: target, Protocol: rrcsProto, Path: path + "." + prop, ID: strconv.Itoa(id),
			Label: field, Kind: kind, Access: access, Value: value, ValueName: name, EnumItems: items})
	}
	names := make([]string, 0, len(props))
	for k := range props {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if block := jMap(props[k]); block != nil {
			fields := make([]string, 0, len(block))
			for f := range block {
				fields = append(fields, f)
			}
			sort.Strings(fields)
			for _, f := range fields {
				add(k, f, block[f])
			}
			continue
		}
		add("", k, props[k])
	}
	return rows
}

// rrcsRows flattens the ports and the client cards of a model.
func rrcsRows(m *rrcsModel) []rrcsRow {
	target := hostOnly(m.Target)
	var rows []rrcsRow
	for _, c := range m.Cards {
		rows = append(rows, rrcsRowsOf(target, c.Path, c.ObjectID, c.Raw)...)
	}
	for _, p := range m.Ports {
		rows = append(rows, rrcsRowsOf(target, p.Path, p.ObjectID, p.Raw)...)
	}
	return rows
}

// rrcsPathFilter reads --path: comma-separated texts, a row is kept when
// its path contains one of them. Empty keeps everything.
func rrcsPathFilter(spec []string) func(path string) bool {
	var parts []string
	for _, s := range spec {
		for _, p := range strings.Split(s, ",") {
			if p = strings.TrimSpace(p); p != "" {
				parts = append(parts, strings.ToLower(p))
			}
		}
	}
	return func(path string) bool {
		if len(parts) == 0 {
			return true
		}
		low := strings.ToLower(path)
		for _, p := range parts {
			if strings.Contains(low, p) {
				return true
			}
		}
		return false
	}
}

func rrcsFormat(format, file string) (string, error) {
	if format == "" {
		format = strings.TrimPrefix(strings.ToLower(filepath.Ext(file)), ".")
		if format == "" {
			format = "json"
		}
	}
	if format != "json" && format != "csv" {
		return "", fmt.Errorf("format %q: want json or csv", format)
	}
	return format, nil
}

// rrcsExport writes the values of the ports and client cards to one file.
func rrcsExport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("rrcs export", flag.ContinueOnError)
	src := newRRCSSource(fs)
	format := fs.String("format", "", "json | csv (default: from the --out extension, else json). One row per value: path, label, kind, access, value, value_name")
	out := fs.String("out", "", "output file (default: stdout)")
	var paths rrcsProps
	fs.Var(&paths, "path", "keep the rows whose path contains this text; comma-separated or repeated (e.g. PortAes67Output, card, Ptp, node.63.port.1045)")
	if err := parseVerbFlags(fs, reorderFlagsFirst(args)); err != nil {
		return err
	}
	fmtName, err := rrcsFormat(*format, *out)
	if err != nil {
		return rrcsValErr("export", err.Error())
	}
	m, err := src.model(ctx, "export", fs.Args(), false, nil)
	if err != nil {
		return err
	}
	keep := rrcsPathFilter(paths)
	rows := []rrcsRow{}
	for _, r := range rrcsRows(m) {
		if keep(r.Path) {
			rows = append(rows, r)
		}
	}

	w := io.Writer(os.Stdout)
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			return fmt.Errorf("rrcs export: %w", err)
		}
		defer func() { _ = f.Close() }()
		w = f
	}
	if fmtName == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", " ")
		if err := enc.Encode(rows); err != nil {
			return fmt.Errorf("rrcs export: %w", err)
		}
	} else {
		cw := csv.NewWriter(w)
		_ = cw.Write(rrcsCSVHeader)
		for _, r := range rows {
			_ = cw.Write(r.record())
		}
		cw.Flush()
		if err := cw.Error(); err != nil {
			return fmt.Errorf("rrcs export: %w", err)
		}
	}
	if *out != "" {
		writable := 0
		for _, r := range rows {
			if r.Access == "RW-" {
				writable++
			}
		}
		fmt.Fprintf(os.Stderr, "exported %d values (%d writable) to %s (%s)\n", len(rows), writable, *out, fmtName)
	}
	return nil
}

// rrcsReadRows reads a file export wrote, json or csv by its extension.
func rrcsReadRows(file string) ([]rrcsRow, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(filepath.Ext(file), ".json") {
		var rows []rrcsRow
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		return rows, nil
	}
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(raw), "\xef\xbb\xbf")))
	r.FieldsPerRecord = -1
	// A spreadsheet saved in a locale that uses the comma for decimals
	// writes semicolons.
	if first, _, _ := strings.Cut(string(raw), "\n"); strings.Count(first, ";") > strings.Count(first, ",") {
		r.Comma = ';'
	}
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("%s: no header: %w", file, err)
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.TrimSpace(h)] = i
	}
	if _, ok := col["path"]; !ok {
		return nil, fmt.Errorf("%s: no path column", file)
	}
	if _, ok := col["value"]; !ok {
		return nil, fmt.Errorf("%s: no value column", file)
	}
	var rows []rrcsRow
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			return rows, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		get := func(name string) string {
			if i, ok := col[name]; ok && i < len(rec) {
				return strings.TrimSpace(rec[i])
			}
			return ""
		}
		rows = append(rows, rrcsRow{Path: get("path"), Label: get("label"), Kind: get("kind"), Access: get("access"),
			Value: get("value"), ValueName: get("value_name")})
	}
}

// rrcsSplitPath cuts a row path into the object and the property: the
// property starts at the first element that begins with a capital.
func rrcsSplitPath(path string) (object, block, field string, ok bool) {
	parts := strings.Split(path, ".")
	for i, p := range parts {
		if p != "" && p[0] >= 'A' && p[0] <= 'Z' {
			object = strings.Join(parts[:i], ".")
			switch len(parts) - i {
			case 1:
				return object, "", parts[i], object != ""
			case 2:
				return object, parts[i], parts[i+1], object != ""
			}
			return "", "", "", false
		}
	}
	return "", "", "", false
}

// rrcsSkip is one row import did not attempt.
type rrcsSkip struct {
	Reason string `json:"reason"`
	Path   string `json:"path"`
	Detail string `json:"detail,omitempty"`
}

// rrcsApplied is one value import sends.
type rrcsApplied struct {
	Path   string `json:"path"`
	Live   string `json:"live"`
	Wanted string `json:"wanted"`
	Result string `json:"result"` // would_apply | applied | failed | not_taken
	Error  string `json:"error,omitempty"`
}

// rrcsImport writes the values of a snapshot file back to the gateway.
func rrcsImport(ctx context.Context, args []string) error {
	// --dry-run is a flag without a value, as on the acp connectors; it
	// is taken out before the flags are reordered around the host.
	dryRun := false
	rest := make([]string, 0, len(args))
	for _, a := range args {
		switch a {
		case "--dry-run", "-dry-run", "--dry-run=true", "-dry-run=true":
			dryRun = true
		default:
			rest = append(rest, a)
		}
	}
	fs := flag.NewFlagSet("rrcs import", flag.ContinueOnError)
	fs.Usage = func() {
		_, _ = fmt.Fprint(fs.Output(), `Usage: dhs consumer rrcs import <host>[:port] --file SNAPSHOT [--path P ...] [--dry-run] [flags]

Reads a file written by export (json or csv, by its extension) and WRITES
every writable value that differs from the live system, one
ConfigurationChangeEx per port or client card, then reads everything back.
Rows whose value equals the live one are left alone; read-only rows are
skipped and listed.

  --dry-run   compare and report, send nothing
`)
		fs.PrintDefaults()
	}
	cf := newRRCSFlags(fs)
	file := fs.String("file", "", "snapshot file written by export, json or csv (required)")
	var paths rrcsProps
	fs.Var(&paths, "path", "apply only the rows whose path contains this text; comma-separated or repeated")
	if err := parseVerbFlags(fs, reorderFlagsFirst(rest)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return rrcsValErr("import", "want exactly one host[:port] argument")
	}
	if *file == "" {
		return rrcsValErr("import", "want --file SNAPSHOT")
	}
	rows, err := rrcsReadRows(*file)
	if err != nil {
		return fmt.Errorf("rrcs import: %w", err)
	}
	client, _, closeFn, err := cf.open("import", fs.Arg(0))
	if err != nil {
		return err
	}
	defer closeFn()
	read := func() (*rrcsModel, error) {
		snap, _, err := rrcsCollect(ctx, client, rrcsCollectOpts{})
		if err != nil {
			return nil, err
		}
		return rrcsModelOf(snap)
	}
	live, err := read()
	if err != nil {
		return err
	}

	keep := rrcsPathFilter(paths)
	var skips []rrcsSkip
	var applied []*rrcsApplied
	type plan struct {
		change *rrcsChange
		items  []*rrcsApplied
	}
	plans := map[string]*plan{}
	var order []*plan
	unchanged := 0
	current := func(m *rrcsModel, object, block, field string) (string, bool) {
		props, ok := m.lookup(object)
		if !ok {
			return "", false
		}
		holder := props
		if block != "" {
			holder = jMap(props[block])
		}
		v, _, _, _, ok := rrcsScalar(field, holder[field])
		return v, ok
	}
	for _, row := range rows {
		if !keep(row.Path) {
			continue
		}
		object, block, field, ok := rrcsSplitPath(row.Path)
		if !ok {
			skips = append(skips, rrcsSkip{"bad_path", row.Path, "not OBJECT.Property or OBJECT.Block.Property"})
			continue
		}
		if !rrcsWritable[block][field] {
			skips = append(skips, rrcsSkip{"read_only", row.Path, ""})
			continue
		}
		now, ok := current(live, object, block, field)
		if !ok {
			skips = append(skips, rrcsSkip{"not_on_device", row.Path, "the live system reports no such value"})
			continue
		}
		// An enum row may carry the word alone.
		wanted := row.Value
		if wanted == "" && row.ValueName != "" {
			for n, word := range rrcsEnums[field] {
				if strings.EqualFold(word, row.ValueName) {
					wanted = strconv.Itoa(n)
				}
			}
			if wanted == "" {
				skips = append(skips, rrcsSkip{"bad_value", row.Path, "value_name " + row.ValueName + " is not one of its values"})
				continue
			}
		}
		if wanted == now {
			unchanged++
			continue
		}
		var value codec.Value
		switch v := live.mustRaw(object, block, field).(type) {
		case bool:
			b, err := strconv.ParseBool(strings.ToLower(wanted))
			if err != nil {
				skips = append(skips, rrcsSkip{"bad_value", row.Path, wanted + " is not true or false"})
				continue
			}
			value = codec.Bool(b)
		case float64:
			n, err := strconv.ParseInt(wanted, 10, 32)
			if err != nil {
				skips = append(skips, rrcsSkip{"bad_value", row.Path, wanted + " is not a whole number"})
				continue
			}
			value = codec.Int(int32(n))
		default:
			_ = v
			value = codec.String(wanted)
		}
		p := plans[object]
		if p == nil {
			change, err := rrcsChangeFor(object)
			if err != nil {
				skips = append(skips, rrcsSkip{"bad_path", row.Path, err.Error()})
				continue
			}
			p = &plan{change: change}
			plans[object] = p
			order = append(order, p)
		}
		if err := p.change.add(block+"."+field, value); err != nil {
			skips = append(skips, rrcsSkip{"duplicate", row.Path, "the file gives this value twice"})
			continue
		}
		item := &rrcsApplied{Path: row.Path, Live: now, Wanted: wanted, Result: "would_apply"}
		p.items = append(p.items, item)
		applied = append(applied, item)
	}

	failed := 0
	if !dryRun && len(order) > 0 {
		for _, p := range order {
			if ctx.Err() != nil {
				break
			}
			result, text := "applied", ""
			if _, err := client.Call(ctx, "ConfigurationChangeEx", p.change.request()); err != nil {
				result, text = "failed", err.Error()
			}
			for _, it := range p.items {
				it.Result, it.Error = result, text
			}
		}
		// Applied means it reads back.
		after, err := read()
		if err != nil {
			return fmt.Errorf("rrcs import: sent, but the read back failed: %w", err)
		}
		for _, it := range applied {
			if it.Result != "applied" {
				failed++
				continue
			}
			object, block, field, _ := rrcsSplitPath(it.Path)
			if now, _ := current(after, object, block, field); now != it.Wanted {
				it.Result, it.Error = "not_taken", "RRCS accepted the request and still reports "+now
				failed++
			}
		}
	}

	if cf.output == "json" {
		doc := struct {
			Target    string         `json:"target"`
			DryRun    bool           `json:"dry_run"`
			Unchanged int            `json:"unchanged"`
			Failed    int            `json:"failed"`
			Diff      []*rrcsApplied `json:"diff"`
			Skipped   []rrcsSkip     `json:"skipped"`
		}{client.Peer(), dryRun, unchanged, failed, applied, skips}
		if doc.Diff == nil {
			doc.Diff = []*rrcsApplied{}
		}
		if doc.Skipped == nil {
			doc.Skipped = []rrcsSkip{}
		}
		b, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
	} else {
		verb := "applied"
		if dryRun {
			verb = "would apply"
		}
		for _, it := range applied {
			fmt.Printf("%-11s %-62s %s -> %s  %s\n", it.Result, it.Path, it.Live, it.Wanted, it.Error)
		}
		fmt.Printf("%s %d, unchanged %d, skipped %d, failed %d\n", verb, len(applied)-failed, unchanged, len(skips), failed)
		reasons := map[string]int{}
		for _, s := range skips {
			reasons[s.Reason]++
		}
		names := make([]string, 0, len(reasons))
		for r := range reasons {
			names = append(names, r)
		}
		sort.Strings(names)
		for _, reason := range names {
			fmt.Printf("  %s (%d)\n", reason, reasons[reason])
			if reason == "read_only" {
				continue // thousands on a full export; the count says it
			}
			for _, s := range skips {
				if s.Reason == reason {
					fmt.Printf("    %s  %s\n", s.Path, s.Detail)
				}
			}
		}
	}
	if failed > 0 {
		return fmt.Errorf("rrcs import: %d value(s) failed", failed)
	}
	return nil
}

// mustRaw returns the value RRCS reports for a property, nil if none.
func (m *rrcsModel) mustRaw(object, block, field string) any {
	props, _ := m.lookup(object)
	if block != "" {
		return jMap(props[block])[field]
	}
	return props[field]
}
