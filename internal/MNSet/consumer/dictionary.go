package mnset

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"dhs/internal/consumer"
)

// The module publishes values and structure only. Units, ranges, enum
// names and descriptions come from two places, and every entry says
// which:
//
//   - the module itself, where it publishes a threshold next to a value
//     (SFP DDM low_alarm / high_alarm);
//   - MN SET's own tables and labels, extracted from its web bundle
//     (testdata/scrape_mnset_ui.py, testdata/video-formats.json).
//
// Nothing is guessed: a field with no evidenced unit exports without
// one. The dictionary is data (dm/fusion6.json), edited without code.

//go:embed dm/fusion6.json
var fusion6Dictionary []byte

//go:embed dm/video-formats.json
var videoFormatsJSON []byte

// dictEntry is one dictionary rule.
type dictEntry struct {
	Match       string            `json:"match"`
	Unit        string            `json:"unit,omitempty"`
	Min         *float64          `json:"min,omitempty"`
	Max         *float64          `json:"max,omitempty"`
	Enum        map[string]string `json:"enum,omitempty"`
	Description string            `json:"description,omitempty"`
	Source      string            `json:"source"`
}

// siblingRule takes min/max from fields the module publishes beside
// the value ("…temperature.current" ← "…temperature.low_alarm").
type siblingRule struct {
	Match   string `json:"match"`
	MinFrom string `json:"min_from"`
	MaxFrom string `json:"max_from"`
	Source  string `json:"source"`
}

type dictionary struct {
	Model    string        `json:"model"`
	Entries  []dictEntry   `json:"entries"`
	Siblings []siblingRule `json:"sibling_thresholds"`
}

// videoFormat is one row of MN SET's format table: the six codes and
// the name the UI shows for them.
type videoFormat struct {
	Name     string  `json:"name"`
	TScan    int     `json:"t_scan"`
	PScan    int     `json:"p_scan"`
	Mode     int     `json:"code_mode"`
	Format   int     `json:"code_format"`
	Rate     int     `json:"code_rate"`
	Sampling int     `json:"code_sampling"`
	Width    int     `json:"width,omitempty"`
	Height   int     `json:"height,omitempty"`
	FPS      float64 `json:"frame_rate,omitempty"`
}

// parseDictionary decodes the two embedded tables. A broken embed is a
// build error of ours; annotate then leaves the objects bare rather
// than failing a walk.
func parseDictionary(dict, formatsJSON []byte) (dictionary, []videoFormat, error) {
	var d dictionary
	if err := json.Unmarshal(dict, &d); err != nil {
		return d, nil, fmt.Errorf("mnset: embedded dictionary: %w", err)
	}
	var tables map[string][]videoFormat
	if err := json.Unmarshal(formatsJSON, &tables); err != nil {
		return d, nil, fmt.Errorf("mnset: embedded video formats: %w", err)
	}
	// The ST 2110 program list first (what this module runs), the 2022-6
	// list after it for the names only it knows (525i / 625i).
	formats := append([]videoFormat{}, tables["VIDEO_FORMATS"]...)
	formats = append(formats, tables["VIDEO_FORMATS_2022"]...)
	return d, formats, nil
}

// matchPath reports whether a dotted pattern matches a dotted path:
// "*" matches exactly one segment; a leading "**" matches any number of
// leading segments ("**.dst_udp_port" = every leaf named dst_udp_port).
func matchPath(pattern string, path []string) bool {
	pat := strings.Split(pattern, ".")
	if pat[0] == "**" {
		pat = pat[1:]
		if len(path) < len(pat) {
			return false
		}
		path = path[len(path)-len(pat):]
	}
	if len(pat) != len(path) {
		return false
	}
	for i, p := range pat {
		if p != "*" && p != path[i] {
			return false
		}
	}
	return true
}

// annotate applies the dictionary to the objects of one walk, in place:
// unit / min / max / enum items / value name / description. It runs
// after labelObjects, on the leaves only.
func annotate(objs []consumer.Object) {
	d, formats, err := parseDictionary(fusion6Dictionary, videoFormatsJSON)
	if err != nil {
		return
	}
	byPath := make(map[string]*consumer.Object, len(objs))
	for i := range objs {
		byPath[strings.Join(objs[i].Path, ".")] = &objs[i]
	}

	for i := range objs {
		o := &objs[i]
		for _, e := range d.Entries {
			if !matchPath(e.Match, o.Path) {
				continue
			}
			if e.Unit != "" {
				o.Unit = e.Unit
			}
			if e.Min != nil {
				o.Min = *e.Min
			}
			if e.Max != nil {
				o.Max = *e.Max
			}
			if e.Description != "" {
				setMeta(o, "description", e.Description)
				setMeta(o, "source", e.Source)
			}
			if len(e.Enum) > 0 {
				keys := make([]string, 0, len(e.Enum))
				for k := range e.Enum {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				items := make([]string, 0, len(keys))
				for _, k := range keys {
					items = append(items, k+"="+e.Enum[k])
				}
				o.EnumItems = items
				if name, ok := e.Enum[o.Value.Str]; ok {
					setMeta(o, "value_name", name)
				}
			}
		}
		for _, r := range d.Siblings {
			if !matchPath(r.Match, o.Path) || !isNumber(o.Value) {
				continue
			}
			parent := strings.Join(o.Path[:len(o.Path)-1], ".")
			if lo, ok := byPath[parent+"."+r.MinFrom]; ok && isNumber(lo.Value) {
				o.Min = numberOf(lo.Value)
			}
			if hi, ok := byPath[parent+"."+r.MaxFrom]; ok && isNumber(hi.Value) {
				o.Max = numberOf(hi.Value)
			}
		}
	}
	nameVideoFormats(objs, byPath, formats)
}

// intOf reads an integer leaf whether the module spelled it as a number
// or as a digit string — the FusioN6 serves format_code_* as "9216".
func intOf(o *consumer.Object) (int, bool) {
	if o == nil {
		return 0, false
	}
	switch o.Value.Kind {
	case consumer.KindInt:
		return int(o.Value.Int), true
	case consumer.KindString:
		n, err := strconv.Atoi(strings.TrimSpace(o.Value.Str))
		return n, err == nil
	}
	return 0, false
}

func isNumber(v consumer.Value) bool {
	return v.Kind == consumer.KindInt || v.Kind == consumer.KindFloat
}

// numberOf returns an int or float leaf as float64, so min/max compare
// alike whatever the module's spelling was.
func numberOf(v consumer.Value) float64 {
	if v.Kind == consumer.KindInt {
		return float64(v.Int)
	}
	return v.Float
}

func setMeta(o *consumer.Object, k, v string) {
	if o.Meta == nil {
		o.Meta = map[string]any{}
	}
	o.Meta[k] = v
}

// nameVideoFormats resolves each flow's six format_code_* into the
// format name MN SET shows, and puts that name on all six leaves; the
// list of every format the program accepts goes on format_code_rate.
func nameVideoFormats(objs []consumer.Object, byPath map[string]*consumer.Object, formats []videoFormat) {
	names := make([]string, 0, len(formats))
	seen := map[string]bool{}
	for _, f := range formats {
		if !seen[f.Name] {
			seen[f.Name] = true
			names = append(names, f.Name)
		}
	}
	codes := []string{"format_code_t_scan", "format_code_p_scan", "format_code_mode", "format_code_format", "format_code_rate", "format_code_sampling"}
	for i := range objs {
		o := &objs[i]
		if len(o.Path) != 4 || o.Path[0] != "flows" || o.Path[2] != "format" || o.Path[3] != "format_code_rate" {
			continue
		}
		o.EnumItems = names
		prefix := "flows." + o.Path[1] + ".format."
		var got [6]int
		complete := true
		for k, c := range codes {
			leaf, ok := byPath[prefix+c]
			n, isInt := intOf(leaf)
			if !ok || !isInt {
				complete = false
				break
			}
			got[k] = n
		}
		if !complete {
			continue
		}
		name := ""
		for _, f := range formats {
			if [6]int{f.TScan, f.PScan, f.Mode, f.Format, f.Rate, f.Sampling} == got {
				name = f.Name
				break
			}
		}
		if name == "" {
			name = "(not a listed format)"
		}
		for _, c := range codes {
			setMeta(byPath[prefix+c], "value_name", name)
		}
	}
}
