package codec

import (
	"fmt"
	"sort"
	"strings"
)

// Unit-type ids worth naming in code, because protocol behaviour turns on
// them rather than only presentation.
const (
	// TypeIDNone is the null unit id.
	TypeIDNone uint16 = 0

	// TypeIDRouterMatrix marks a unit that exposes the Full Control Command
	// Set. Seeing it is a reason to probe command 100; it is not proof the
	// routing interface is there, as the audit oracle showed by reporting
	// this id and answering Nack to that command.
	TypeIDRouterMatrix uint16 = 636

	// TypeIDRoutingIPShareClient is the identity a 32-bit routing client
	// presents. Our own consumer reports it so that vendor tools show us as
	// something they recognise.
	TypeIDRoutingIPShareClient uint16 = 483
)

// LookupUnitType returns what the vendor database says about a type id.
//
// The table is sorted, so this is a binary search over static data with no
// allocation and no start-up cost.
func LookupUnitType(id uint16) (UnitType, bool) {
	i := sort.Search(len(unitTypes), func(i int) bool { return unitTypes[i].ID >= id })
	if i < len(unitTypes) && unitTypes[i].ID == id {
		return unitTypes[i], true
	}
	return UnitType{}, false
}

// Label returns the best name this entry carries: the vendor's short product
// name, its description if there is no name, and otherwise a rendering of the
// number.
//
// Every entry in the database as it stands today has a name. The fallbacks are
// there because the database is vendor data that we regenerate: a future
// release may add an entry with an id and nothing else, as the file's Vistek
// section already does, and an unnamed device is still a device we can talk to.
func (t UnitType) Label() string {
	switch {
	case t.Name != "":
		return t.Name
	case t.Description != "":
		return t.Description
	default:
		return fmt.Sprintf("unit type %d", t.ID)
	}
}

// UnitTypeName returns the best label available for a type id.
//
// It never returns an empty string. An unknown id is not an error: the vendor
// allocates new ones with every product, and a device we cannot name is still
// a device we can talk to.
func UnitTypeName(id uint16) string {
	t, ok := LookupUnitType(id)
	if !ok {
		return fmt.Sprintf("unit type %d", id)
	}
	return t.Label()
}

// UnitTypeCount reports how many type ids the table holds. Tests and the
// diagnostics tier use it to show that the table was generated rather than
// stubbed.
func UnitTypeCount() int { return len(unitTypes) }

// UnitTypesByCategory returns every type in a category, ordered by id.
func UnitTypesByCategory(c UnitCategory) []UnitType {
	var out []UnitType
	for _, t := range unitTypes {
		if t.Category == c {
			out = append(out, t)
		}
	}
	return out
}

// FindUnitTypes returns every type whose name, enum or description contains
// the given text, case-insensitively. It backs a CLI lookup for an operator
// who knows a product by name and needs its id.
func FindUnitTypes(text string) []UnitType {
	q := strings.ToLower(strings.TrimSpace(text))
	if q == "" {
		return nil
	}
	var out []UnitType
	for _, t := range unitTypes {
		if strings.Contains(strings.ToLower(t.Name), q) ||
			strings.Contains(strings.ToLower(t.Enum), q) ||
			strings.Contains(strings.ToLower(t.Description), q) {
			out = append(out, t)
		}
	}
	return out
}

// IsRouterMatrix reports whether a type id is the router matrix product.
func IsRouterMatrix(id uint16) bool { return id == TypeIDRouterMatrix }

func (t UnitType) String() string {
	if t.ID == 0 && t.Enum == "" {
		return "unknown"
	}
	s := fmt.Sprintf("%d %s", t.ID, t.Name)
	if t.Description != "" && t.Description != t.Name {
		s += fmt.Sprintf(" (%s)", t.Description)
	}
	return s
}
