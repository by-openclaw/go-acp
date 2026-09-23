package main

// `dhs consumer <proto> alarm <verb>` — the per-model alarm template
// every connector shares (internal/consumer/alarm).
//
// The template is data, not code: one row per object pattern, saying
// what that object's value means. These verbs read and edit it, and
// `test` answers the only question that matters while authoring —
// "what would this value do?" — without touching a device.
//
//	dhs consumer mnset alarm list   --model FusioN6@0x68cd783f
//	dhs consumer mnset alarm get    --path refclk.status
//	dhs consumer mnset alarm set    --path refclk.status --kind enum --normal 3 --values 0=major,2=minor --hold 30s
//	dhs consumer mnset alarm set    --path 'port.*.sfp_ddm_info.temperature.current' --high minor:75/72,major:80/77 --hold 10s
//	dhs consumer mnset alarm test   --path refclk.status --value 0
//	dhs consumer mnset alarm export --out fusion.alarm.json
//	dhs consumer mnset alarm import fusion.alarm.json

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/consumer/alarm"
	"dhs/internal/datastore"
)

// runAlarmNeedsProtocol is the catalogue entry for `alarm`.
// dispatchConsumer routes the verb before the table is consulted,
// because the template is per protocol and touches no device; reaching
// this means the protocol was left out.
func runAlarmNeedsProtocol(context.Context, []string) error {
	return fmt.Errorf("alarm: name the protocol — dhs consumer <proto> alarm <list|get|set|test|export|import>")
}

// runAlarm dispatches the alarm sub-verbs. proto is the connector the
// template belongs to; templates never leak across protocols.
func runAlarm(ctx context.Context, proto string, args []string) error {
	if len(args) == 0 || isHelpToken(args[0]) {
		helpAlarm()
		return nil
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return runAlarmList(proto, rest)
	case "get":
		return runAlarmGet(proto, rest)
	case "set":
		return runAlarmSet(proto, rest)
	case "test":
		return runAlarmTest(proto, rest)
	case "export":
		return runAlarmExport(proto, rest)
	case "import":
		return runAlarmImport(proto, rest)
	}
	return fmt.Errorf("consumer %s alarm: unknown sub-verb %q (list, get, set, test, export, import)", proto, sub)
}

// alarmFlags are what every sub-verb needs: which template.
type alarmFlags struct {
	model    *string
	template *string
}

func addAlarmFlags(fs *flag.FlagSet) *alarmFlags {
	return &alarmFlags{
		model: fs.String("model", alarm.DefaultName,
			"the card this template governs — a DM identity like FusioN6@0x68cd783f; the default governs every card of this protocol"),
		template: fs.String("template", "",
			"read/write this file instead of the cache (.cache/alarm/<proto>/<model>.json)"),
	}
}

// path is where the template lives for these flags.
func (a *alarmFlags) path(proto string) (string, error) {
	if *a.template != "" {
		return *a.template, nil
	}
	root, err := datastore.ProjectRoot()
	if err != nil {
		return "", err
	}
	return alarm.Path(root+"/.cache", proto, *a.model), nil
}

// load reads the template, or returns an empty one when the file does
// not exist yet: authoring starts somewhere.
func (a *alarmFlags) load(proto string) (*alarm.Template, string, error) {
	p, err := a.path(proto)
	if err != nil {
		return nil, "", err
	}
	t, err := alarm.LoadFile(p)
	if os.IsNotExist(err) {
		return &alarm.Template{Model: *a.model, Protocol: proto}, p, nil
	}
	if err != nil {
		return nil, p, err
	}
	return t, p, nil
}

func runAlarmList(proto string, args []string) error {
	fs := flag.NewFlagSet("consumer "+proto+" alarm list", flag.ContinueOnError)
	af := addAlarmFlags(fs)
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	t, path, err := af.load(proto)
	if err != nil {
		return err
	}
	if len(t.Rows) == 0 {
		fmt.Printf("no alarm rules for %s (%s)\n", *af.model, path)
		return nil
	}
	fmt.Printf("%s — %d rule(s) from %s\n\n", t.Model, len(t.Rows), path)
	fmt.Printf("%-44s %-8s %-34s %-8s %s\n", "MATCH", "KIND", "RULE", "HOLD", "TEXT")
	fmt.Println(strings.Repeat("-", 120))
	for _, r := range t.Rows {
		fmt.Printf("%-44s %-8s %-34s %-8s %s\n",
			alarmTrunc(r.Match, 44), r.Kind, alarmTrunc(ruleOf(r), 34), holdOf(r), r.Text)
	}
	return nil
}

func runAlarmGet(proto string, args []string) error {
	fs := flag.NewFlagSet("consumer "+proto+" alarm get", flag.ContinueOnError)
	af := addAlarmFlags(fs)
	path := fs.String("path", "", "the object path to explain (required)")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	if *path == "" {
		return fmt.Errorf("consumer %s alarm get: --path is required", proto)
	}
	t, from, err := af.load(proto)
	if err != nil {
		return err
	}
	row := t.RowFor(*path)
	if row == nil {
		fmt.Printf("%s: no rule covers it — its changes are reported as info\n", *path)
		return nil
	}
	fmt.Printf("path    %s\n", *path)
	fmt.Printf("match   %s\n", row.Match)
	fmt.Printf("kind    %s\n", row.Kind)
	fmt.Printf("rule    %s\n", ruleOf(*row))
	if h := holdOf(*row); h != "" {
		fmt.Printf("hold    %s\n", h)
	}
	if row.FlapCap > 0 {
		fmt.Printf("flap    %d transitions/min before it is silenced\n", row.FlapCap)
	}
	if row.Text != "" {
		fmt.Printf("text    %s\n", row.Text)
	}
	if row.Source != "" {
		fmt.Printf("source  %s\n", row.Source)
	}
	fmt.Printf("file    %s\n", from)
	return nil
}

func runAlarmSet(proto string, args []string) error {
	fs := flag.NewFlagSet("consumer "+proto+" alarm set", flag.ContinueOnError)
	af := addAlarmFlags(fs)
	match := fs.String("path", "", "the path pattern the rule covers (required): '*' one segment, '**' any depth")
	kind := fs.String("kind", "", "number (default) | counter | enum | text")
	normal := fs.String("normal", "", "the expected value: a literal, or ~regex for text; the normal value for an enum")
	values := fs.String("values", "", "enum mapping, e.g. 0=major,2=minor")
	high := fs.String("high", "", "high bands, e.g. minor:75/72,major:80/77 (severity:raise/clear)")
	low := fs.String("low", "", "low bands, e.g. minor:-15/-12,critical:-20/-17")
	severity := fs.String("severity", "", "verdict for a text mismatch or a stalled counter (default major)")
	stalled := fs.Duration("stalled-for", 0, "how long a counter may stand still before it alarms")
	hold := fs.Duration("hold", 0, "how long a verdict must persist before it is adopted")
	clearHold := fs.Duration("clear-hold", 0, "hold when coming back towards normal (default --hold)")
	flapCap := fs.Int("flap-cap", 0, "transitions per minute before the object is reported flapping and silenced")
	text := fs.String("text", "", "what an operator reads in the notification")
	source := fs.String("source", "", "where the numbers come from (a device threshold, a manual, a site rule)")
	remove := fs.Bool("remove", false, "delete the rule with this exact --path instead of writing one")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	if *match == "" {
		return fmt.Errorf("consumer %s alarm set: --path is required", proto)
	}
	t, path, err := af.load(proto)
	if err != nil {
		return err
	}

	if *remove {
		kept := t.Rows[:0]
		for _, r := range t.Rows {
			if r.Match != *match {
				kept = append(kept, r)
			}
		}
		if len(kept) == len(t.Rows) {
			fmt.Printf("changed=false  no rule matches %q exactly\n", *match)
			return nil
		}
		t.Rows = kept
		return saveTemplate(path, t, *match)
	}

	row := alarm.Row{
		Match: *match, Kind: alarm.Kind(*kind), Normal: *normal, Severity: *severity,
		StalledFor: alarm.Duration(*stalled), Hold: alarm.Duration(*hold),
		ClearHold: alarm.Duration(*clearHold), FlapCap: *flapCap,
		Text: *text, Source: *source,
	}
	if row.Values, err = parseValues(*values); err != nil {
		return err
	}
	if row.High, err = parseBands(*high); err != nil {
		return fmt.Errorf("--high: %w", err)
	}
	if row.Low, err = parseBands(*low); err != nil {
		return fmt.Errorf("--low: %w", err)
	}

	// Editing an existing rule keeps its place: row order is precedence.
	replaced := false
	for i := range t.Rows {
		if t.Rows[i].Match == *match {
			t.Rows[i], replaced = row, true
			break
		}
	}
	if !replaced {
		t.Rows = append(t.Rows, row)
	}
	if err := t.Validate(); err != nil {
		return err
	}
	return saveTemplate(path, t, *match)
}

// saveTemplate writes and reports whether anything changed, so a
// repeated set is visibly a no-op.
func saveTemplate(path string, t *alarm.Template, match string) error {
	changed, err := alarm.Save(path, t)
	if err != nil {
		return err
	}
	fmt.Printf("changed=%t  %s  (%d rule(s) in %s)\n", changed, match, len(t.Rows), path)
	return nil
}

func runAlarmTest(proto string, args []string) error {
	fs := flag.NewFlagSet("consumer "+proto+" alarm test", flag.ContinueOnError)
	af := addAlarmFlags(fs)
	path := fs.String("path", "", "the object path to evaluate (required)")
	value := fs.String("value", "", "the value to evaluate (required)")
	unit := fs.String("unit", "", "the value's unit, for the printed line")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	if *path == "" || *value == "" {
		return fmt.Errorf("consumer %s alarm test: --path and --value are required", proto)
	}
	t, _, err := af.load(proto)
	if err != nil {
		return err
	}
	if len(t.Rows) == 0 {
		return fmt.Errorf("consumer %s alarm test: no rules to test against (write one with `alarm set`)", proto)
	}
	sev, band, row := alarm.New(t, nil).Explain(consumer.Event{
		Path: *path, Unit: *unit,
		Value: consumer.Value{Kind: consumer.KindString, Str: *value},
	})
	if row == nil {
		fmt.Printf("info      %s: no rule covers it — its changes are reported as info\n", *path)
		return nil
	}
	line := fmt.Sprintf("%-9s %s = %s%s  (%s)", sev, *path, *value, unitSuffix(*unit), band)
	if row.Text != "" {
		line += " — " + row.Text
	}
	fmt.Println(line)
	// The verdict is what the value MEANS; the hold is how long the
	// device must keep saying it before the alarm is raised.
	if h := holdOf(*row); h != "" && sev != alarm.Normal {
		fmt.Printf("%-9s raised only after %s of the same verdict (match %s)\n", "", h, row.Match)
	}
	if row.Kind == alarm.KindCounter {
		fmt.Printf("%-9s a counter is judged over time: normal while it rises, %s after %s standing still\n",
			"", sevOr(row.Severity), durOr(time.Duration(row.StalledFor), "the hold"))
	}
	return nil
}

func runAlarmExport(proto string, args []string) error {
	fs := flag.NewFlagSet("consumer "+proto+" alarm export", flag.ContinueOnError)
	af := addAlarmFlags(fs)
	out := fs.String("out", "", "write here instead of stdout")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	t, _, err := af.load(proto)
	if err != nil {
		return err
	}
	if *out == "" {
		return t.Encode(os.Stdout)
	}
	changed, err := alarm.Save(*out, t)
	if err != nil {
		return err
	}
	fmt.Printf("changed=%t  %d rule(s) → %s\n", changed, len(t.Rows), *out)
	return nil
}

func runAlarmImport(proto string, args []string) error {
	fs := flag.NewFlagSet("consumer "+proto+" alarm import", flag.ContinueOnError)
	af := addAlarmFlags(fs)
	file := ""
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		file, args = args[0], args[1:]
	}
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	if file == "" {
		return fmt.Errorf("consumer %s alarm import: a template file is required", proto)
	}
	t, err := alarm.LoadFile(file)
	if err != nil {
		return err
	}
	dst, err := af.path(proto)
	if err != nil {
		return err
	}
	changed, err := alarm.Save(dst, t)
	if err != nil {
		return err
	}
	fmt.Printf("changed=%t  %d rule(s) → %s\n", changed, len(t.Rows), dst)
	return nil
}

// --- rendering + parsing helpers -------------------------------------

// ruleOf renders a row's rule in one column.
func ruleOf(r alarm.Row) string {
	switch r.Kind {
	case alarm.KindCounter:
		return "rising, else " + sevOr(r.Severity) + " after " + durOr(time.Duration(r.StalledFor), "the hold")
	case alarm.KindEnum:
		keys := make([]string, 0, len(r.Values))
		for k := range r.Values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys)+1)
		// A row may name the good value instead of tabulating them all
		// ("locked, or not"); say what happens to the others.
		if r.Normal != "" {
			parts = append(parts, "expect "+r.Normal+", else "+sevOr(r.Severity))
		}
		for _, k := range keys {
			parts = append(parts, k+"="+r.Values[k])
		}
		return strings.Join(parts, " ")
	case alarm.KindText:
		return "expect " + r.Normal + ", else " + sevOr(r.Severity)
	}
	parts := make([]string, 0, len(r.Low)+len(r.High))
	for _, b := range r.Low {
		parts = append(parts, "low "+bandStr(b))
	}
	for _, b := range r.High {
		parts = append(parts, "high "+bandStr(b))
	}
	return strings.Join(parts, " ")
}

func bandStr(b alarm.Band) string {
	s := fmt.Sprintf("%s:%g", b.Severity, b.Raise)
	if b.Clear != nil {
		s += fmt.Sprintf("/%g", *b.Clear)
	}
	return s
}

// alarmTrunc shortens a column value; the nmos verb has its own trunc
// with different semantics, so this one is named for its caller.
func alarmTrunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func sevOr(s string) string {
	if s == "" {
		return "major"
	}
	return s
}

func durOr(d time.Duration, fallback string) string {
	if d <= 0 {
		return fallback
	}
	return d.String()
}

func holdOf(r alarm.Row) string {
	if r.Hold <= 0 {
		return ""
	}
	s := time.Duration(r.Hold).String()
	if r.ClearHold > 0 {
		s += "/" + time.Duration(r.ClearHold).String()
	}
	return s
}

// parseValues reads "0=major,2=minor".
func parseValues(s string) (map[string]string, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	out := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || k == "" || v == "" {
			return nil, fmt.Errorf("--values: %q is not value=severity", part)
		}
		out[k] = v
	}
	return out, nil
}

// parseBands reads "minor:75/72,major:80/77" — severity:raise/clear,
// the clear point optional.
func parseBands(s string) ([]alarm.Band, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	var out []alarm.Band
	for _, part := range strings.Split(s, ",") {
		sev, rest, ok := strings.Cut(strings.TrimSpace(part), ":")
		if !ok {
			return nil, fmt.Errorf("%q is not severity:raise[/clear]", part)
		}
		raiseStr, clearStr, hasClear := strings.Cut(rest, "/")
		raise, err := strconv.ParseFloat(strings.TrimSpace(raiseStr), 64)
		if err != nil {
			return nil, fmt.Errorf("%q: raise %q is not a number", part, raiseStr)
		}
		b := alarm.Band{Severity: sev, Raise: raise}
		if hasClear {
			c, err := strconv.ParseFloat(strings.TrimSpace(clearStr), 64)
			if err != nil {
				return nil, fmt.Errorf("%q: clear %q is not a number", part, clearStr)
			}
			b.Clear = &c
		}
		out = append(out, b)
	}
	return out, nil
}

func helpAlarm() {
	fmt.Println(`dhs consumer <proto> alarm <verb> — the per-model alarm template

A template says what an object's VALUE means: which band is minor,
major or critical, what the expected value is, how long a verdict must
hold before it is raised. It is data (.cache/alarm/<proto>/<model>.json),
shared by every connector, and every verb below is idempotent.

  list                      show the rules in force
  get    --path P           explain the rule that covers one object
  set    --path PATTERN …   write or replace one rule (--remove deletes it)
  test   --path P --value V evaluate a value against the rules, no device
  export [--out FILE]       write the template (stdout by default)
  import FILE               install a template, reporting changed=true/false

Common flags: --model <identity> (default _default, which governs every
card of the protocol), --template FILE (bypass the cache).

Rule flags on set:
  --kind number|counter|enum|text
  --high minor:75/72,major:80/77     high bands as severity:raise/clear
  --low  minor:-15/-12,critical:-20  low bands
  --normal V | ~regex                the expected value (text / enum)
  --values 0=major,2=minor           enum value → severity
  --severity S                       verdict for a mismatch or a stall
  --stalled-for D                    a counter may stand still this long
  --hold D  --clear-hold D           anti-flap: persist before adopting
  --flap-cap N                       transitions/min before silencing
  --text T  --source S               what an operator reads, and why

Examples:
  dhs consumer mnset alarm set --path 'port.*.sfp_ddm_info.temperature.current' \
      --high minor:75/72,major:80/77,critical:85/82 --hold 10s \
      --text 'SFP temperature' --source 'module DDM thresholds'
  dhs consumer mnset alarm set --path '**.network.pkt_cnt' --kind counter \
      --stalled-for 10s --severity major --text 'stream stopped' --source 'site rule'
  dhs consumer mnset alarm test --path refclk.status --value 0`)
}

// loadAlarmEvaluator builds the evaluator a watch runs every change
// through: the template named by --alarm, else the one cached for this
// card (.cache/alarm/<proto>/<Model@SwRev>.json), else the protocol
// default. It returns nil when there are no rules — a plant that has
// written none sees exactly the watch it saw before.
//
// The identity comes from the plugin when it can answer without a walk
// (consumer.Identifier / IdentityProbe); a connector that cannot is not
// asked twice.
func loadAlarmEvaluator(ctx context.Context, plug consumer.Protocol, proto string, slot int, file string, disabled bool) *alarm.Evaluator {
	if disabled {
		return nil
	}
	var (
		tpl  *alarm.Template
		from string
		err  error
	)
	if file != "" {
		from = file
		tpl, err = alarm.LoadFile(file)
	} else {
		root, rerr := datastore.ProjectRoot()
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "alarm: %v — watching without alarms\n", rerr)
			return nil
		}
		tpl, from, err = alarm.Resolve(root+"/.cache", proto, alarmIdentity(ctx, plug, slot))
	}
	if err != nil {
		// A broken template is the operator's mistake and must be loud,
		// but it does not take the watch down with it.
		fmt.Fprintf(os.Stderr, "alarm: %v — watching without alarms\n", err)
		return nil
	}
	if tpl == nil {
		return nil
	}
	fmt.Printf("alarms: %d rule(s) from %s\n", len(tpl.Rows), from)
	return alarm.New(tpl, nil)
}

// alarmIdentity asks the plugin which card it is talking to, so the
// per-model template can be found. An empty answer is normal: the
// protocol default then governs.
func alarmIdentity(ctx context.Context, plug consumer.Protocol, slot int) string {
	probe, ok := plug.(interface {
		IdentityProbe(context.Context, int) (string, error)
	})
	if !ok {
		return ""
	}
	if slot < 0 {
		slot = 0
	}
	id, err := probe.IdentityProbe(ctx, slot)
	if err != nil {
		return ""
	}
	return id
}

// reportAlarm prints one verdict and mirrors it to the structured sink
// with its RFC 5424 severity, so the same line reaches the terminal and
// the collector. nil (no change of verdict) prints nothing.
// A swept verdict has no event behind it, so the line is stamped with
// the moment the engine adopted it.
func reportAlarm(tr *alarm.Transition, host string, cf *commonFlags) {
	if tr == nil {
		return
	}
	fmt.Printf("%s  %-18s  %s\n", tr.At.Format("15:04:05"), "[alarm]", tr.String())
	if !cf.logHasSink || cf.eventLogger == nil {
		return
	}
	cf.eventLogger.Info("alarm",
		slog.String("proto", cf.protocol),
		slog.String("dev", host),
		slog.Int("slot", tr.Slot),
		slog.String("path", tr.Path),
		slog.String("severity", tr.Severity.String()),
		slog.Int("syslog_severity", tr.Severity.Syslog()),
		slog.String("prior", tr.Prior.String()),
		slog.String("band", tr.Band),
		slog.String("value", tr.Value),
		slog.String("prev", tr.Prev),
		slog.String("unit", tr.Unit),
		slog.Bool("flapping", tr.Flapping),
		slog.String("text", tr.Text))
}
