package alarm

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"dhs/internal/consumer"
)

// Writing a template by hand, model by model, does not scale to a
// plant — and it is not necessary, because a device already says most
// of it. An enum names its own states ("NA|OK|Warning|Error"), a
// measurement declares its own range ("-40..140 C"), an ACP1 alarm
// object carries its own priority and its own on/off text. Suggest
// reads a walked tree and writes the rows that evidence supports,
// with a source naming it, and says out loud what it refused to guess.
//
// The output is a draft for a human: `alarm suggest` prints it,
// someone reads it, `alarm import` installs it. Nothing here decides
// a plant's policy — it only stops the plant from typing what the
// device already knows.

// Verdict words. A device that grades itself does it in one of these
// vocabularies, whatever the protocol: the words below are the ones
// broadcast kit actually ships, and a state that is in none of them is
// left alone rather than guessed at.
var (
	badCritical = []string{
		"error", "fail", "failed", "failure", "fault", "faulty", "alarm",
		"critical", "lost", "loss", "no signal", "nosignal", "absent",
		"missing", "down", "unlocked", "not locked", "disconnected",
		"invalid", "overtemp", "over temperature", "shutdown", "dead",
	}
	badMinor = []string{
		"warning", "warn", "degraded", "partial", "unknown", "na", "n/a",
		"unavailable", "initialising", "initializing", "calibrating",
		"not ready", "pending", "recovering",
	}
	good = []string{
		"ok", "okay", "normal", "good", "locked", "present", "up",
		"online", "enabled", "connected", "active", "running", "valid",
		"pass", "passed", "ready", "healthy", "none",
	}
)

// Report says what Suggest did and, more usefully, what it would not
// do: an operator reading a generated template needs to know which
// objects were left unjudged and why.
type Report struct {
	Objects    int // objects walked
	Rows       int // rows written
	Merged     int // objects folded into an indexed row ("PSU.*.Status")
	Writable   int // settings: a value someone chose is not a symptom
	NoEvidence int // nothing the device said supports a rule
}

// String renders the report as the one line a CLI prints.
func (r Report) String() string {
	return fmt.Sprintf(
		"%d object(s): %d rule(s) (%d object(s) folded into indexed rules), "+
			"%d writable setting(s) skipped, %d without evidence",
		r.Objects, r.Rows, r.Merged, r.Writable, r.NoEvidence)
}

// Suggest builds a draft template from a walked tree. model and proto
// go into the template's header; they name it, they do not affect a
// single rule.
//
// What becomes a rule:
//
//   - an enum whose item list contains a word the industry uses for
//     "broken" — only those items are listed, so every state the
//     device did not call bad stays normal;
//   - a read-only measurement whose declared range is a real range —
//     the limits become critical bands, because a reading outside the
//     range the device published is a fault by the device's own word;
//   - an object carrying the device's own alarm metadata (ACP1's
//     priority + on/off text), whose priority picks the severity.
//
// What never does: anything writable (a setting is a choice, not a
// symptom), and anything the device said nothing about.
func Suggest(objs []consumer.Object, proto, model string) (*Template, Report) {
	var rep Report
	type draft struct {
		row   Row
		count int
	}
	drafts := map[string]*draft{}

	for _, o := range objs {
		if len(o.Path) == 0 || o.SubGroupMarker {
			continue
		}
		rep.Objects++
		if o.Access&2 != 0 { // write bit
			rep.Writable++
			continue
		}
		row, ok := rowFor(o)
		if !ok {
			rep.NoEvidence++
			continue
		}
		// Objects that differ only by an index collapse into one
		// indexed rule: a shelf with two supplies and four fans each
		// is one row, not ten.
		row.Match = indexedMatch(o.Path)
		if strings.HasSuffix(row.Match, "*") {
			row.Text = strings.TrimRight(row.Text, "0123456789 ")
		}
		key := row.Match + "|" + string(row.Kind)
		if d, seen := drafts[key]; seen {
			d.count++
			mergeRow(&d.row, row)
			continue
		}
		drafts[key] = &draft{row: row, count: 1}
	}

	tpl := &Template{Model: model, Protocol: proto}
	for _, d := range drafts {
		if d.count > 1 {
			rep.Merged += d.count - 1
		}
		tpl.Rows = append(tpl.Rows, d.row)
	}
	sort.Slice(tpl.Rows, func(i, j int) bool { return tpl.Rows[i].Match < tpl.Rows[j].Match })
	rep.Rows = len(tpl.Rows)
	return tpl, rep
}

// rowFor is the evidence test for one object.
func rowFor(o consumer.Object) (Row, bool) {
	// 1. The device's own alarm object (ACP1): it already decided this
	// is an alarm and how loud. We only have to say which value is the
	// quiet one.
	if o.AlarmPriority > 0 || o.AlarmOnMsg != "" {
		if row, ok := enumRow(o); ok {
			row.Severity = priorityToSeverity(o.AlarmPriority).String()
			for k := range row.Values {
				row.Values[k] = row.Severity
			}
			row.Text = strings.TrimSpace(o.AlarmOnMsg)
			if row.Text == "" {
				row.Text = o.Label
			}
			row.Source = fmt.Sprintf(
				"the device's own alarm object: priority %d, on=%q, off=%q",
				o.AlarmPriority, o.AlarmOnMsg, o.AlarmOffMsg)
			return row, true
		}
	}

	// 2. An enum that names its own bad states.
	if row, ok := enumRow(o); ok {
		row.Text = o.Label
		row.Source = "the object's own item list: " + strings.Join(o.EnumItems, "|")
		return row, true
	}

	// 3. A measurement with a real declared range.
	if row, ok := rangeRow(o); ok {
		row.Text = o.Label
		return row, true
	}
	return Row{}, false
}

// enumRow lists the items the device itself calls broken. Items it
// does not are left out, so a state nobody classified stays normal —
// a generated file must not invent policy.
func enumRow(o consumer.Object) (Row, bool) {
	if len(o.EnumItems) == 0 {
		return Row{}, false
	}
	values := map[string]string{}
	sawGood := false
	for _, item := range o.EnumItems {
		switch classify(item) {
		case Critical:
			values[item] = Critical.String()
		case Minor:
			values[item] = Minor.String()
		case Normal:
			sawGood = true
		}
	}
	// A list with nothing bad in it is a choice, not a health state;
	// one with nothing good in it was misread.
	if len(values) == 0 || !sawGood {
		return Row{}, false
	}
	return Row{Kind: KindEnum, Values: values, Hold: Duration(10e9)}, true
}

// rangeRow turns a declared range into the two bands that say "the
// device is outside what it told us it does".
func rangeRow(o consumer.Object) (Row, bool) {
	switch o.Kind {
	case consumer.KindInt, consumer.KindUint, consumer.KindFloat:
	default:
		return Row{}, false
	}
	lo, okLo := numberish(o.Min)
	hi, okHi := numberish(o.Max)
	if !okLo || !okHi || hi <= lo {
		return Row{}, false
	}
	// A range that is the type's own width is not a statement about
	// the device: 0..4294967295 means "a number", not "this hot".
	if hi >= 2147483647 || lo <= -2147483648 {
		return Row{}, false
	}
	span := hi - lo
	row := Row{
		Kind: KindNumber,
		High: []Band{{Severity: Critical.String(), Raise: hi, Clear: clearPoint(hi - span*0.02)}},
		Hold: Duration(30e9),
		Source: fmt.Sprintf("the object declares its own range: %s..%s%s",
			trimNum(lo), trimNum(hi), unitSuffix(o.Unit)),
	}
	// A floor of zero is how a count or a percentage is spelled, not a
	// limit anything can cross.
	if lo != 0 {
		row.Low = []Band{{Severity: Critical.String(), Raise: lo, Clear: clearPoint(lo + span*0.02)}}
	}
	return row, true
}

// mergeRow folds a second object's evidence into an indexed rule: the
// union of the bad states, and the wider of the two ranges, so one row
// is right about every instance it covers.
func mergeRow(into *Row, other Row) {
	for k, v := range other.Values {
		if _, seen := into.Values[k]; !seen {
			into.Values[k] = v
		}
	}
	for i := range other.High {
		if len(into.High) > i && other.High[i].Raise > into.High[i].Raise {
			into.High[i] = other.High[i]
		}
	}
	for i := range other.Low {
		if len(into.Low) > i && other.Low[i].Raise < into.Low[i].Raise {
			into.Low[i] = other.Low[i]
		}
	}
}

// An index is a number someone can count: it follows a separator
// ("Fan Health 1", "LOCK_CIRCUIT_1"). A number welded to a word is
// part of the name — a version or a standard — and globbing it would
// turn ROOT_NODE_V2 into ROOT_NODE_V*, which matches a device that
// does not exist.
var indexTail = regexp.MustCompile(`^(.*[ _\-])(\d+)$`)

// indexedMatch turns one object's path into the pattern that covers
// its siblings: a numeric segment becomes "*", and a numeric tail
// inside a segment becomes a glob ("Fan Health 3" → "Fan Health *").
// The leading "**" absorbs whatever root a plugin puts in front of
// every path.
func indexedMatch(path []string) string {
	segs := make([]string, len(path))
	for i, s := range path {
		if _, err := strconv.Atoi(s); err == nil {
			segs[i] = "*"
			continue
		}
		if m := indexTail.FindStringSubmatch(s); m != nil {
			segs[i] = m[1] + "*"
			continue
		}
		segs[i] = s
	}
	return "**." + strings.Join(segs, ".")
}

// classify reads one state name. Longest phrase first, so "not locked"
// is not read as "locked".
func classify(item string) Severity {
	s := strings.ToLower(strings.TrimSpace(item))
	if s == "" {
		return Info
	}
	for _, w := range badCritical {
		if wordish(s, w) {
			return Critical
		}
	}
	for _, w := range badMinor {
		if wordish(s, w) {
			return Minor
		}
	}
	for _, w := range good {
		if wordish(s, w) {
			return Normal
		}
	}
	return Info
}

// wordish matches a whole word or the whole phrase, never a fragment:
// "Standalone" is not "alone", and "Disabled" is not "abled".
func wordish(s, word string) bool {
	if s == word {
		return true
	}
	for _, f := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '_' || r == '-' || r == '.' || r == '/'
	}) {
		if f == word {
			return true
		}
	}
	return strings.Contains(s, word) && strings.Contains(word, " ")
}

// priorityToSeverity maps an ACP1 alarm priority onto the ladder. The
// scale runs 1 (loudest) to 5; anything beyond is minor.
func priorityToSeverity(p uint8) Severity {
	switch {
	case p == 0:
		return Major
	case p <= 2:
		return Critical
	case p == 3:
		return Major
	default:
		return Minor
	}
}

func numberish(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	}
	return 0, false
}

func clearPoint(v float64) *float64 {
	c := v
	return &c
}

func trimNum(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }

func unitSuffix(u string) string {
	if u == "" {
		return ""
	}
	return " " + u
}
