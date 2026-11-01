package mib

import (
	"sort"
	"strings"

	"dhs/internal/snmp/codec"
)

// MaxRows is the most rows one count may promise when the MIB states no
// range: the 16-bit field size this repo sizes everything to. A count above
// it is not a count, and no amount of reading should be spent on it.
const MaxRows = 65535

// Column is one column of a [Bounded] table.
type Column struct {
	OID codec.OID
	// Index says the column is one of the table's INDEX objects. A device
	// can answer those with made-up values at any row, so only the others
	// can tell a real row from an empty one.
	Index bool
	// Unknown lists the numbers its enumeration names "unknown": an empty
	// row's value, like a zero.
	Unknown []int64
}

// Bounded is a table a parent table counts, the shape a device with a filled
// table uses: the DR5000 keeps 4 096 stream rows, 128 programmes by 32
// streams, and 17 are real. A walk cannot tell without reading all of them;
// the parent's column can. This is a reading of the MIB's names and nothing
// more, so the walk proves it on the device, table by table, before it is
// believed: see Session.WalkBounded.
type Bounded struct {
	Table   codec.OID // the conceptual row: columns sit one arc below it
	Count   codec.OID // the parent's column saying how many rows each index has
	Max     int64     // the most rows a count can mean
	Columns []Column
}

// Bounded lists the tables of this device that a parent table counts,
// derived from the compiled table alone: no device name decides it.
func (t *Table) Bounded() []Bounded { return t.bounded() }

// deriveBounded finds every table T and parent P where P's INDEX is T's
// without its last object, and P has an integer column named for T: see
// [countsRows] for the name, and [indexTails] for when two INDEX objects
// are one.
func (t *Table) deriveBounded() []Bounded {
	children := map[string][]*Object{}
	var entries []*Object
	done := map[string]bool{}
	for i := range t.objs {
		o := &t.objs[i]
		key := o.OID.String()
		if done[key] {
			continue
		}
		done[key] = true
		parent := o.OID[:len(o.OID)-1].String()
		children[parent] = append(children[parent], o)
		if o.Kind == "object-type" && len(o.Index) > 0 {
			entries = append(entries, o)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].OID.Compare(entries[j].OID) < 0 })

	parents := map[string][]*Object{}
	for _, e := range entries {
		for _, key := range indexKeys(e, len(e.Index)) {
			parents[key] = append(parents[key], e)
		}
	}

	var out []Bounded
	for _, entry := range entries {
		n := len(entry.Index) - 1
		rows := columnsOf(entry, children[entry.OID.String()])
		if n == 0 || rows == nil {
			continue
		}
		if col := countColumn(entry, indexKeys(entry, n), parents, children); col != nil {
			out = append(out, Bounded{Table: entry.OID, Count: col.OID, Max: t.rowLimit(entry), Columns: rows})
		}
	}
	return out
}

// countColumn finds the column of a parent table that counts entry.
func countColumn(entry *Object, keys []string, parents, children map[string][]*Object) *Object {
	for _, key := range keys {
		for _, parent := range parents[key] {
			for _, col := range children[parent.OID.String()] {
				if countsRows(col, parent, entry) {
					return col
				}
			}
		}
	}
	return nil
}

// stem is a row's name without the Entry every MIB gives it.
func stem(entry *Object) string { return strings.TrimSuffix(entry.Name, "Entry") }

// indexKeys are the two ways the first n INDEX objects of a row can be the
// same as another row's: by name, or by what follows each row's own name.
// One MIB generator declares the index again in every table, so
// ...ProgramStreamProgramIdx and ...ProgramProgramIdx are one index, and
// only the tail says so.
func indexKeys(entry *Object, n int) []string {
	head := stem(entry)
	tails := make([]string, n)
	for i, name := range entry.Index[:n] {
		tails[i] = strings.TrimPrefix(name, head)
	}
	return []string{strings.Join(entry.Index[:n], ","), strings.Join(tails, ",")}
}

// countsRows says col, a column of parent, counts the rows entry holds under
// each parent row, by one of the two names a MIB gives such a column:
//
//	<entry's name>Count   ...ProgramStreamCount for ...ProgramStreamEntry
//	<parent>N<the rest>   ...ProgramStreamNDvbSubService for
//	                      ...ProgramStreamDvbSubServiceEntry
func countsRows(col, parent, entry *Object) bool {
	if len(col.Enums) > 0 {
		return false
	}
	switch col.Base {
	case "INTEGER", "Integer32", "Unsigned32", "Gauge32":
	default:
		return false
	}
	own, up := stem(entry), stem(parent)
	rest, nested := strings.CutPrefix(own, up)
	if !nested || rest == "" {
		return false
	}
	return col.Name == own+"Count" || col.Name == up+"N"+rest
}

// columnsOf lists a row's columns. Access is not asked: the DR5000 answers
// index columns its MIB calls not-accessible, and a walk would have read
// them. A table with no column but its index cannot show that a row is
// empty, so it cannot be proven, so it is not listed.
func columnsOf(entry *Object, cols []*Object) []Column {
	index := map[string]bool{}
	for _, name := range entry.Index {
		index[name] = true
	}
	var out []Column
	proof := false
	for _, c := range cols {
		col := Column{OID: c.OID, Index: index[c.Name]}
		for _, e := range c.Enums {
			if strings.EqualFold(e.Name, "unknown") {
				col.Unknown = append(col.Unknown, e.Value)
			}
		}
		proof = proof || !col.Index
		out = append(out, col)
	}
	if !proof {
		return nil
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OID.Compare(out[j].OID) < 0 })
	return out
}

// rowLimit is the top of the row index's range, kept at or under [MaxRows].
func (t *Table) rowLimit(entry *Object) int64 {
	for _, i := range t.byName[entry.Index[len(entry.Index)-1]] {
		if m := t.objs[i].Max; m > 0 && m < MaxRows {
			return m
		}
	}
	return MaxRows
}
