package codec

import (
	_ "embed"
	"fmt"
	"strconv"
	"strings"
)

//go:embed catalogue.tsv
var catalogueTSV string

// Mode is the MODE column of the page.
type Mode uint8

// The three modes the page uses.
const (
	ModeGet Mode = 1 << iota
	ModePost
	ModeGetPost = ModeGet | ModePost
)

// CanGet reports a call that answers GET.
func (m Mode) CanGet() bool { return m&ModeGet != 0 }

// CanPost reports a call that takes POST.
func (m Mode) CanPost() bool { return m&ModePost != 0 }

func (m Mode) String() string {
	switch m {
	case ModeGet:
		return "GET"
	case ModePost:
		return "POST"
	}
	return "GET / POST"
}

// DataType is the DATATYPE column of the page.
type DataType string

// Every datatype the page uses.
const (
	TypeInts     DataType = "ints"
	TypeObject   DataType = "object"
	TypeString   DataType = "string"
	TypeFloats   DataType = "floats"
	TypeDoubles  DataType = "doubles"
	TypeQuantity DataType = "quantity"
	TypeSysio    DataType = "sysio"
	TypeNone     DataType = "none"
)

// Scope is a family of scopes one call takes. The zero Scope takes none.
type Scope string

// The families the page names.
const (
	ScopeNone   Scope = ""
	ScopeTile   Scope = "tile"   // tile1 .. tile8
	ScopeConfig Scope = "config" // config0 .. config5
	ScopePort   Scope = "port"   // IP1, IP2
	ScopeSlot   Scope = "slot"   // slot0, slot1
	ScopeSlot0  Scope = "slot0"  // slot0 only
	ScopeIndex  Scope = "index"  // index1, index2
	ScopeIndex0 Scope = "index0" // index0 only
	ScopeTiming Scope = "timing" // IP1, IP2, SDI1 .. SDI4, SDI1-4, EXTREF
	ScopeInput  Scope = "input"  // the query ?input=0 .. 5, not a path scope
)

// Values lists the scopes of the family, in page order.
func (s Scope) Values() []string {
	switch s {
	case ScopeTile:
		return []string{"tile1", "tile2", "tile3", "tile4", "tile5", "tile6", "tile7", "tile8"}
	case ScopeConfig:
		return []string{"config0", "config1", "config2", "config3", "config4", "config5"}
	case ScopePort:
		return []string{"IP1", "IP2"}
	case ScopeSlot:
		return []string{"slot0", "slot1"}
	case ScopeSlot0:
		return []string{"slot0"}
	case ScopeIndex:
		return []string{"index1", "index2"}
	case ScopeIndex0:
		return []string{"index0"}
	case ScopeTiming:
		return []string{"IP1", "IP2", "SDI1", "SDI2", "SDI3", "SDI4", "SDI1-4", "EXTREF"}
	case ScopeInput:
		return []string{"0", "1", "2", "3", "4", "5"}
	}
	return nil
}

// Entry is one call of the catalogue.
type Entry struct {
	Name    string
	Mode    Mode
	Type    DataType
	Scope   Scope
	Options []string // the enum of the description, empty when it has none

	ScopeOptional bool // the call also answers with no scope
	NoRead        bool // never GET: an action, or the answer is no value
	Guarded       bool // POST only with the guard open
	BareBody      bool // the JSON body stands alone, not under "object"
}

// Numeric reports an enum whose options are the integers themselves.
func (e Entry) Numeric() bool {
	if len(e.Options) == 0 {
		return false
	}
	for _, o := range e.Options {
		if _, err := strconv.Atoi(o); err != nil {
			return false
		}
	}
	return true
}

// Targets lists what walk reads for this call: one per scope, plus the
// bare call when the scope is optional.
func (e Entry) Targets() []Target {
	if e.Scope == ScopeNone {
		return []Target{{Name: e.Name}}
	}
	var out []Target
	if e.ScopeOptional {
		out = append(out, Target{Name: e.Name})
	}
	for _, v := range e.Scope.Values() {
		if e.Scope == ScopeInput {
			out = append(out, Target{Name: e.Name, Input: v})
		} else {
			out = append(out, Target{Name: e.Name, Scope: v})
		}
	}
	return out
}

// Catalogue is every call of the page, in page order.
type Catalogue struct {
	entries []Entry
	byName  map[string]int
}

// Load parses the embedded catalogue.
func Load() (*Catalogue, error) { return parseCatalogue(catalogueTSV) }

func parseCatalogue(tsv string) (*Catalogue, error) {
	c := &Catalogue{byName: map[string]int{}}
	for i, line := range strings.Split(tsv, "\n") {
		line = strings.TrimSuffix(line, "\r") // a Windows checkout embeds CRLF
		if line == "" || line[0] == '#' {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			return nil, fmt.Errorf("prism: catalogue line %d: %d columns", i+1, len(f))
		}
		e := Entry{Name: f[0], Type: DataType(f[2]), Scope: Scope(f[3])}
		switch f[1] {
		case "GET":
			e.Mode = ModeGet
		case "POST":
			e.Mode = ModePost
		case "GET/POST":
			e.Mode = ModeGetPost
		default:
			return nil, fmt.Errorf("prism: catalogue line %d: mode %q", i+1, f[1])
		}
		if f[4] != "" {
			for _, flag := range strings.Split(f[4], ",") {
				switch flag {
				case "scope-optional":
					e.ScopeOptional = true
				case "no-read":
					e.NoRead = true
				case "guarded":
					e.Guarded = true
				case "bare-body":
					e.BareBody = true
				default:
					return nil, fmt.Errorf("prism: catalogue line %d: flag %q", i+1, flag)
				}
			}
		}
		if f[5] != "" {
			e.Options = strings.Split(f[5], "|")
		}
		if _, dup := c.byName[e.Name]; dup {
			return nil, fmt.Errorf("prism: catalogue line %d: %s twice", i+1, e.Name)
		}
		c.byName[e.Name] = len(c.entries)
		c.entries = append(c.entries, e)
	}
	return c, nil
}

// Entries returns the calls in page order. The slice is the catalogue's
// own: read it, do not change it.
func (c *Catalogue) Entries() []Entry { return c.entries }

// Lookup finds a call by its exact name.
func (c *Catalogue) Lookup(name string) (Entry, bool) {
	i, ok := c.byName[name]
	if !ok {
		return Entry{}, false
	}
	return c.entries[i], true
}
