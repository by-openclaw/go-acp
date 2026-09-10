package smi

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

// Patch defines one symbol a vendor module should define and does not.
//
// It is the narrowest correction possible — a name, the node it hangs
// off, one arc — and it carries its evidence, which is mandatory: a patch
// without evidence is an invented OID, and the table would then label a
// real device's objects with a name nobody defined. Vendor files are never
// edited; a patch is applied to the parsed module, so the source stays
// exactly what the vendor shipped.
type Patch struct {
	Module string
	Name   string
	Parent string
	Arc    uint32
	Why    string
	Line   int
}

// ParsePatches reads the patch format, one symbol a line:
//
//	MODULE  SYMBOL  PARENT  ARC  -- evidence
//
// Blank lines and lines starting with # are ignored. The evidence after
// -- is required.
func ParsePatches(src string) ([]Patch, error) {
	var out []Patch
	sc := bufio.NewScanner(strings.NewReader(src))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		spec, why, ok := strings.Cut(line, "--")
		why = strings.TrimSpace(why)
		if !ok || why == "" {
			return nil, fmt.Errorf("patch line %d: no evidence after --", n)
		}
		f := strings.Fields(spec)
		if len(f) != 4 {
			return nil, fmt.Errorf("patch line %d: want MODULE SYMBOL PARENT ARC, got %d fields", n, len(f))
		}
		arc, err := strconv.ParseUint(f[3], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("patch line %d: arc %q is not a 32-bit number", n, f[3])
		}
		out = append(out, Patch{Module: f[0], Name: f[1], Parent: f[2], Arc: uint32(arc), Why: why, Line: n})
	}
	return out, sc.Err()
}

// Apply adds each patch's symbol to its module, before Compile.
//
// A patch is skipped, and reported, when its module is not in the set or
// already defines the symbol. The second is how a patch announces that it
// has gone stale: the vendor fixed their file, and the correction should
// be deleted rather than kept shadowing the real definition.
func Apply(mods []*Module, patches []Patch) []Finding {
	byName := map[string]*Module{}
	for _, m := range mods {
		byName[m.Name] = m
	}
	var out []Finding
	for _, p := range patches {
		m := byName[p.Module]
		switch {
		case m == nil:
			out = append(out, Finding{"patches", p.Line, "patch-unused",
				fmt.Sprintf("module %s is not in the set, so %s was not defined", p.Module, p.Name)})
		case m.byName[p.Name] != nil:
			out = append(out, Finding{"patches", p.Line, "patch-stale",
				fmt.Sprintf("%s already defines %s; delete this patch", p.Module, p.Name)})
		default:
			m.addNode(&Node{Name: p.Name, Kind: KindObjectIdentifier,
				Value: []OIDComponent{{Name: p.Parent}, {Num: p.Arc, HasNum: true}}})
			out = append(out, Finding{"patches", p.Line, "patched",
				fmt.Sprintf("defined %s ::= { %s %d } in %s: %s", p.Name, p.Parent, p.Arc, p.Module, p.Why)})
		}
	}
	return out
}
