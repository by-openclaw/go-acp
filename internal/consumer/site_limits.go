package consumer

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// SiteLimit is one rule of a plant: a property, named by its full path or
// by a suffix that starts with a dot (".PortAes67Input.Channels" applies to
// every port), and what the site allows for it — a narrower range than
// the factory's, or a list of allowed values (ADR-0036, ADR-0037 P7).
type SiteLimit struct {
	Path    string
	Min     *float64
	Max     *float64
	Allowed []string
}

// SiteLimits is the set of a plant's rules, read from a CSV with the
// header path,min,max,allowed (allowed is a|b|c). A property may match
// several rules: the exact path wins over a suffix, a longer suffix over
// a shorter one.
type SiteLimits struct {
	Source string
	rules  []SiteLimit
}

// LoadSiteLimits reads the rules of a file. An empty cell is no bound.
func LoadSiteLimits(path string) (*SiteLimits, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(raw), "\xef\xbb\xbf")))
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	if first, _, _ := strings.Cut(string(raw), "\n"); strings.Count(first, ";") > strings.Count(first, ",") {
		r.Comma = ';'
	}
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("%s: no header: %w", path, err)
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	if _, ok := col["path"]; !ok {
		return nil, fmt.Errorf("%s: no path column (header path,min,max,allowed)", path)
	}
	out := &SiteLimits{Source: path}
	line := 1
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		line++
		get := func(name string) string {
			if i, ok := col[name]; ok && i < len(rec) {
				return strings.TrimSpace(rec[i])
			}
			return ""
		}
		rule := SiteLimit{Path: get("path")}
		if rule.Path == "" {
			continue
		}
		for _, b := range []struct {
			name string
			dst  **float64
		}{{"min", &rule.Min}, {"max", &rule.Max}} {
			if text := get(b.name); text != "" {
				n, err := strconv.ParseFloat(text, 64)
				if err != nil {
					return nil, fmt.Errorf("%s line %d: %s %q is not a number", path, line, b.name, text)
				}
				*b.dst = &n
			}
		}
		if rule.Min != nil && rule.Max != nil && *rule.Min > *rule.Max {
			return nil, fmt.Errorf("%s line %d: min %v is above max %v", path, line, *rule.Min, *rule.Max)
		}
		if text := get("allowed"); text != "" {
			for _, a := range strings.Split(text, "|") {
				if a = strings.TrimSpace(a); a != "" {
					rule.Allowed = append(rule.Allowed, a)
				}
			}
		}
		if rule.Min == nil && rule.Max == nil && len(rule.Allowed) == 0 {
			return nil, fmt.Errorf("%s line %d: %s has no min, max or allowed", path, line, rule.Path)
		}
		out.rules = append(out.rules, rule)
	}
}

// Len is the number of rules.
func (s *SiteLimits) Len() int {
	if s == nil {
		return 0
	}
	return len(s.rules)
}

// For finds the rule of a path: the exact path, else the longest suffix
// rule (a suffix starts with a dot). nil when none applies.
func (s *SiteLimits) For(path string) *SiteLimit {
	if s == nil {
		return nil
	}
	var best *SiteLimit
	for i := range s.rules {
		r := &s.rules[i]
		switch {
		case r.Path == path:
			return r
		case strings.HasPrefix(r.Path, ".") && strings.HasSuffix(path, r.Path):
			if best == nil || len(r.Path) > len(best.Path) {
				best = r
			}
		}
	}
	return best
}

// Check refuses a value the site does not allow, or a rule wider than
// the factory limit (ADR-0036: a site limit narrows, never widens). The
// value is text as the tables read it; factory is the schema of the
// point when known, nil otherwise. A nil receiver allows everything.
func (s *SiteLimits) Check(path, value string, factory *Object) error {
	rule := s.For(path)
	if rule == nil {
		return nil
	}
	fmin, fmax, hasFactory := factoryRange(factory)
	if hasFactory {
		if (rule.Min != nil && *rule.Min < fmin) || (rule.Max != nil && *rule.Max > fmax) {
			return &ValidationError{Field: path, Reason: fmt.Sprintf("site limit wider than the factory limit (site %s, factory %s..%s): a site limit may only narrow (rule of %s)",
				rangeText(rule), num(fmin), num(fmax), s.Source)}
		}
	}
	if factory != nil && len(factory.EnumItems) > 0 && len(rule.Allowed) > 0 {
		for _, a := range rule.Allowed {
			if !enumHas(factory.EnumItems, a) {
				return &ValidationError{Field: path, Reason: fmt.Sprintf("site limit allows %q, which the factory does not list (%s): a site limit may only narrow",
					a, strings.Join(factory.EnumItems, "|"))}
			}
		}
	}
	if rule.Min != nil || rule.Max != nil {
		n, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return &ValidationError{Field: path, Reason: fmt.Sprintf("%q is not a number the site limit %s can bound", value, rangeText(rule))}
		}
		if (rule.Min != nil && n < *rule.Min) || (rule.Max != nil && n > *rule.Max) {
			factoryText := ""
			if hasFactory {
				factoryText = ", factory " + num(fmin) + ".." + num(fmax)
			}
			return &ValidationError{Field: path, Reason: fmt.Sprintf("refused by the site limit (site %s%s)", rangeText(rule), factoryText)}
		}
	}
	if len(rule.Allowed) > 0 && !enumHas(rule.Allowed, value) {
		return &ValidationError{Field: path, Reason: fmt.Sprintf("refused by the site limit (allowed %s)", strings.Join(rule.Allowed, "|"))}
	}
	return nil
}

// factoryRange reads a numeric range off an object's schema.
func factoryRange(o *Object) (lo, hi float64, ok bool) {
	if o == nil {
		return 0, 0, false
	}
	lo, okLo := asFloat(o.Min)
	hi, okHi := asFloat(o.Max)
	return lo, hi, okLo && okHi
}

func asFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case int:
		return float64(t), true
	case int32:
		return float64(t), true
	case int64:
		return float64(t), true
	case uint8:
		return float64(t), true
	case uint16:
		return float64(t), true
	case uint32:
		return float64(t), true
	case uint64:
		return float64(t), true
	case float32:
		return float64(t), true
	case float64:
		return t, true
	case string:
		n, err := strconv.ParseFloat(t, 64)
		return n, err == nil
	}
	return 0, false
}

// enumHas says whether a value is one of the items: the item itself, or
// the key or the name of an item written key=name.
func enumHas(items []string, value string) bool {
	for _, it := range items {
		if it == value {
			return true
		}
		if k, name, ok := strings.Cut(it, "="); ok && (k == value || name == value) {
			return true
		}
	}
	return false
}

func rangeText(r *SiteLimit) string {
	lo, hi := "-", "-"
	if r.Min != nil {
		lo = num(*r.Min)
	}
	if r.Max != nil {
		hi = num(*r.Max)
	}
	return lo + ".." + hi
}

func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
