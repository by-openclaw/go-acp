package consumer

import (
	"bytes"
	"context"
	"errors"
	"slices"

	"dhs/internal/snmp/codec"
	"dhs/internal/snmp/mib"
)

// rowBatch is how many objects one GET asks for. Twenty fits a reply in
// one datagram on the DR5000, and it is the walk's own order of size.
const rowBatch = 20

var errBoundedTable = errors.New("snmp: a bounded table")

// WalkBounded is Walk for a model: a table the MIB says a parent counts is
// read through that count, so a table of 4 096 rows with 17 real ones costs
// 17 rows. Everything else is walked as usual, and the raw walk stays Walk,
// which reads what an agent has and decides nothing.
//
// The count is a reading of names in a MIB, and it is proven on the device
// before it is believed (see [Session.provenCount]). A count that fails, or
// that nothing could read, costs the plain walk of that one table and never
// a row: a wrong rule on an unknown device may be slow, it may not hide
// data.
func (s *Session) WalkBounded(ctx context.Context, root codec.OID, tables []mib.Bounded,
	fn func(codec.VarBind) error) error {
	// A count already read is not read twice: the walk comes to a parent
	// table first, and a nested table's count is a column of counted rows.
	counts := make(map[int][]codec.VarBind)
	seen := func(vb codec.VarBind) error {
		for i := range tables {
			if vb.Name.HasPrefix(tables[i].Count) {
				counts[i] = append(counts[i], vb)
			}
		}
		return fn(vb)
	}
	cursor := root
	for {
		hit := -1
		err := s.walkFrom(ctx, root, cursor, func(vb codec.VarBind) error {
			for i := range tables {
				if vb.Name.HasPrefix(tables[i].Table) {
					hit = i
					return errBoundedTable
				}
			}
			return seen(vb)
		})
		if hit < 0 {
			return err
		}
		if err := s.readTable(ctx, root, tables[hit], counts[hit], seen); err != nil {
			return err
		}
		cursor = pastTable(tables[hit].Table)
	}
}

// readTable hands over one table's rows, through its count when the count
// holds up and by a plain walk when it does not. Rows are held until the
// whole table has answered, so a plain walk never repeats one.
func (s *Session) readTable(ctx context.Context, root codec.OID, b mib.Bounded,
	counts []codec.VarBind, fn func(codec.VarBind) error) error {
	if len(counts) == 0 {
		// A count column read in part is no count of the rest.
		var err error
		if counts, err = s.WalkAll(ctx, b.Count, 0); err != nil {
			counts = nil
		}
	}
	rows, ok := s.countedRows(ctx, root, b, counts)
	if !ok {
		scope := b.Table
		if root.HasPrefix(scope) {
			scope = root
		}
		return s.Walk(ctx, scope, fn)
	}
	for _, vb := range rows {
		if err := fn(vb); err != nil {
			return err
		}
	}
	return nil
}

// countedRows reads the rows the count column promises, column by column,
// the order a walk would have given them. It says no when there are no
// counts, which is not an event, or when they do not hold up, which is.
func (s *Session) countedRows(ctx context.Context, root codec.OID, b mib.Bounded,
	counts []codec.VarBind) ([]codec.VarBind, bool) {
	if len(counts) == 0 {
		return nil, false
	}
	if !s.provenCount(ctx, b, counts) {
		s.prof.Note(CountNotTrusted)
		return nil, false
	}
	var names []codec.OID
	for _, col := range b.Columns {
		for _, c := range counts {
			index := c.Name[len(b.Count):]
			for row := int64(1); row <= rowCount(c.Value); row++ {
				if name := col.OID.Append(index...).Append(uint32(row)); name.HasPrefix(root) {
					names = append(names, name)
				}
			}
		}
	}
	binds, err := s.getRows(ctx, names)
	if err != nil {
		s.prof.Note(CountNotTrusted)
		return nil, false
	}
	// A row may lack a column, as any SNMP row may. A row with nothing in
	// any column of its own was not there, and a count that promised it
	// says nothing about the rows it did not.
	holds := map[string]bool{}
	var rows []codec.VarBind
	for _, vb := range binds {
		if col := columnOf(b, vb.Name); !col.Index {
			key := rowKey(b, vb.Name)
			holds[key] = holds[key] || !isBlank(col, vb.Value)
		}
		if !isMissing(vb.Value) {
			rows = append(rows, vb)
		}
	}
	for _, there := range holds {
		if !there {
			s.prof.Note(CountNotTrusted)
			return nil, false
		}
	}
	return rows, true
}

// proofColumns is how many columns the proof reads at each row: enough that
// a real row cannot be blank in all of them by chance, few enough that a
// GET proves many indexes. The columns are ones the last counted row holds
// data in, which is what a real row looks like on this device.
const proofColumns = 3

// provenCount tests a table's count on the device, before any row is read,
// so that a count that runs away costs a handful of reads:
//
//   - every count must be one: not negative, not over the table's limit;
//   - the last row the largest count promises must hold something, or the
//     count promised what the table does not have, which is also how rows
//     that number from 0 show: their last one is one short of the count;
//   - the row past the last counted one must hold nothing, for every index
//     with rows and for the first with none, or the count stopped short.
//
// Only non-index columns can say so: a device answers any index with
// made-up values in its index columns, the DR5000 does.
func (s *Session) provenCount(ctx context.Context, b mib.Bounded, counts []codec.VarBind) bool {
	top := counts[0]
	for _, c := range counts {
		n := rowCount(c.Value)
		if n < 0 || n > b.Max {
			return false
		}
		if n > rowCount(top.Value) {
			top = c
		}
	}
	columns := proving(b)
	if n := rowCount(top.Value); n > 0 {
		binds, err := s.getRows(ctx, cells(columns, top.Name[len(b.Count):], n))
		if err != nil {
			return false
		}
		if columns = holding(b, binds); len(columns) == 0 {
			return false
		}
	}
	var names []codec.OID
	zero := false
	for _, c := range counts {
		n := rowCount(c.Value)
		if n == 0 && zero {
			continue
		}
		zero = zero || n == 0
		names = append(names, cells(columns, c.Name[len(b.Count):], n+1)...)
	}
	binds, err := s.getRows(ctx, names)
	return err == nil && len(holding(b, binds)) == 0
}

// proving lists the columns that can tell a real row from an empty one.
func proving(b mib.Bounded) []mib.Column {
	var out []mib.Column
	for _, col := range b.Columns {
		if !col.Index {
			out = append(out, col)
		}
	}
	return out
}

// holding lists the columns of b with something in the binds, at most
// proofColumns of them.
func holding(b mib.Bounded, binds []codec.VarBind) []mib.Column {
	var out []mib.Column
	for _, vb := range binds {
		if col := columnOf(b, vb.Name); !isBlank(col, vb.Value) && len(out) < proofColumns {
			out = append(out, col)
		}
	}
	return out
}

// cells names one row of an index in each column.
func cells(columns []mib.Column, index codec.OID, row int64) []codec.OID {
	out := make([]codec.OID, len(columns))
	for i, col := range columns {
		out[i] = col.OID.Append(index...).Append(uint32(row))
	}
	return out
}

// getRows reads names in batches. v1 has no per-object exception: one
// absent name fails the request, so a batch that fails that way is read one
// name at a time and an absent one comes back as noSuchInstance.
func (s *Session) getRows(ctx context.Context, names []codec.OID) ([]codec.VarBind, error) {
	var out []codec.VarBind
	for start := 0; start < len(names); start += rowBatch {
		batch := names[start:min(start+rowBatch, len(names))]
		binds, err := s.Get(ctx, batch...)
		if err == nil {
			out = append(out, binds...)
			continue
		}
		if !isEndOfTree(err) {
			return nil, err
		}
		for _, name := range batch {
			one, err := s.Get(ctx, name)
			switch {
			case isEndOfTree(err):
				out = append(out, codec.VarBind{Name: name, Value: codec.NoSuchInstance()})
			case err != nil:
				return nil, err
			default:
				out = append(out, one...)
			}
		}
	}
	return out, nil
}

// rowCount is a count column's value, or -1 for what cannot be a count.
func rowCount(v codec.Value) int64 {
	switch v.Type {
	case codec.TypeInteger:
		return v.Int
	case codec.TypeGauge32:
		return int64(v.Uint)
	}
	return -1
}

func isMissing(v codec.Value) bool {
	return v.Type == codec.TypeNoSuchInst || v.Type == codec.TypeNoSuchObject || v.Type == codec.TypeEndOfMIBView
}

// isBlank says a value is what an empty row holds: nothing, a zero, an
// empty string, or the enumeration's "unknown".
func isBlank(col mib.Column, v codec.Value) bool {
	switch v.Type {
	case codec.TypeInteger:
		return v.Int == 0 || slices.Contains(col.Unknown, v.Int)
	case codec.TypeCounter32, codec.TypeGauge32, codec.TypeTimeTicks, codec.TypeCounter64:
		return v.Uint == 0
	case codec.TypeOctetString, codec.TypeOpaque:
		return len(bytes.Trim(v.Bytes, "\x00 ")) == 0
	case codec.TypeIPAddress:
		return v.IP.IsUnspecified()
	case codec.TypeOID:
		return len(v.OID) == 0 || v.OID.String() == "0.0"
	}
	return true
}

// columnOf finds the column a read name belongs to.
func columnOf(b mib.Bounded, name codec.OID) mib.Column {
	for _, col := range b.Columns {
		if name.HasPrefix(col.OID) {
			return col
		}
	}
	return mib.Column{}
}

// rowKey is what the columns of one row share: the arcs after the column.
func rowKey(b mib.Bounded, name codec.OID) string {
	return name[len(b.Table)+1:].String()
}

// pastTable is a name after every row of the table and before whatever
// follows it, which is where the walk picks up.
func pastTable(table codec.OID) codec.OID {
	next := append(codec.OID{}, table...)
	next[len(next)-1]++
	return next
}
