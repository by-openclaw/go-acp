package consumer

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/consumer/compliance"
	"dhs/internal/snmp/codec"
	"dhs/internal/snmp/mib"
)

// A device shaped like the DR5000: a scalar, a programme table with a count
// column, a stream table, and an object after it. Column 1 is the index,
// which this device answers at any row; column 2 holds data on real rows.
var (
	boundTable = codec.MustParseOID("1.3.6.1.4.1.9.2.1")
	boundCount = codec.MustParseOID("1.3.6.1.4.1.9.1.1.3")
	boundRoot  = codec.MustParseOID("1.3.6.1.4.1.9")
	boundAfter = codec.MustParseOID("1.3.6.1.4.1.9.3.0")
)

func boundedTable() mib.Bounded {
	return mib.Bounded{
		Table: boundTable, Count: boundCount, Max: 8,
		Columns: []mib.Column{
			{OID: boundTable.Append(1), Index: true},
			{OID: boundTable.Append(2), Unknown: []int64{99}},
		},
	}
}

// filledAgent answers from a sorted tree and records every name it served.
type filledAgent struct {
	mu     sync.Mutex
	tree   map[string]codec.VarBind
	sorted []codec.VarBind
	asked  []string
	failOn string // a GET of this name is refused
	v1     bool   // one absent name fails the whole GET, as v1 does
}

// newFilledAgent holds counts[p] real rows for programme p+1 and fills the
// rest of a 3 × 4 table with what an empty row answers on this device.
func newFilledAgent(counts ...int64) *filledAgent {
	a := &filledAgent{tree: map[string]codec.VarBind{}}
	a.add(codec.MustParseOID("1.3.6.1.4.1.9.0.0"), codec.Int(int64(len(counts))))
	for i, n := range counts {
		a.add(boundCount.Append(uint32(i+1)), codec.Int(n))
	}
	for p := uint32(1); p <= 3; p++ {
		for s := uint32(1); s <= 4; s++ {
			a.add(boundTable.Append(1, p, s), codec.Int(int64(s)))
			blank := codec.Int(0)
			if int(p) <= len(counts) && int64(s) <= counts[p-1] {
				blank = codec.Int(int64(s * 10))
			}
			a.add(boundTable.Append(2, p, s), blank)
		}
	}
	a.add(boundAfter, codec.Int(99))
	return a
}

func (a *filledAgent) add(name codec.OID, v codec.Value) {
	vb := codec.VarBind{Name: name, Value: v}
	a.tree[name.String()] = vb
	a.sorted = append(a.sorted, vb)
	sort.Slice(a.sorted, func(i, j int) bool { return a.sorted[i].Name.Compare(a.sorted[j].Name) < 0 })
}

func (a *filledAgent) remove(name codec.OID) {
	delete(a.tree, name.String())
	for i := range a.sorted {
		if a.sorted[i].Name.Compare(name) == 0 {
			a.sorted = append(a.sorted[:i], a.sorted[i+1:]...)
			return
		}
	}
}

func (a *filledAgent) set(name codec.OID, v codec.Value) {
	a.remove(name)
	a.add(name, v)
}

func (a *filledAgent) answer(req codec.Message) *codec.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	if req.PDU.Type == codec.PDUTypeGetBulk || req.PDU.Type == codec.PDUTypeGetNext {
		var out []codec.VarBind
		from := req.PDU.VarBinds[0].Name
		for _, vb := range a.sorted {
			if vb.Name.Compare(from) > 0 && len(out) < max(req.PDU.MaxRepetitions, 1) {
				out = append(out, vb)
				a.asked = append(a.asked, vb.Name.String())
			}
		}
		if len(out) == 0 {
			out = []codec.VarBind{{Name: from, Value: codec.EndOfMIBView()}}
		}
		return respondWith(req, codec.NoError, out...)
	}
	var out []codec.VarBind
	for _, want := range req.PDU.VarBinds {
		name := want.Name.String()
		if name == a.failOn {
			return respondWith(req, codec.GenErr)
		}
		a.asked = append(a.asked, name)
		vb, ok := a.tree[name]
		if !ok {
			if a.v1 {
				return respondWith(req, codec.NoSuchName)
			}
			vb = codec.VarBind{Name: want.Name, Value: codec.NoSuchInstance()}
		}
		out = append(out, vb)
	}
	return respondWith(req, codec.NoError, out...)
}

func boundedSession(t *testing.T, a *filledAgent) (*Session, *compliance.Profile) {
	t.Helper()
	addr := scriptedAgent(t, a.answer)
	prof := &compliance.Profile{}
	return dial(t, addr, Options{Version: codec.Version2c, Compliance: prof, MaxRepetitions: 3, Timeout: time.Second}), prof
}

func walkNames(t *testing.T, s *Session, root codec.OID, tables ...mib.Bounded) []string {
	t.Helper()
	var got []string
	err := s.WalkBounded(context.Background(), root, tables, func(vb codec.VarBind) error {
		got = append(got, vb.Name.String())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// plainWalk is the truth a bounded walk must never fall short of.
func plainWalk(t *testing.T, s *Session, root codec.OID) []string {
	t.Helper()
	binds, err := s.WalkAll(context.Background(), root, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, vb := range binds {
		got = append(got, vb.Name.String())
	}
	return got
}

func TestABoundedTableCostsOnlyItsRealRows(t *testing.T) {
	a := newFilledAgent(2, 1, 0)
	s, prof := boundedSession(t, a)
	got := walkNames(t, s, boundRoot, boundedTable())
	want := []string{"1.3.6.1.4.1.9.0.0", "1.3.6.1.4.1.9.1.1.3.1", "1.3.6.1.4.1.9.1.1.3.2", "1.3.6.1.4.1.9.1.1.3.3",
		"1.3.6.1.4.1.9.2.1.1.1.1", "1.3.6.1.4.1.9.2.1.1.1.2", "1.3.6.1.4.1.9.2.1.1.2.1",
		"1.3.6.1.4.1.9.2.1.2.1.1", "1.3.6.1.4.1.9.2.1.2.1.2", "1.3.6.1.4.1.9.2.1.2.2.1",
		boundAfter.String()}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("read\n%v\nwant\n%v", got, want)
	}
	if n := prof.Snapshot()[CountNotTrusted]; n != 0 {
		t.Errorf("%s fired %d times on a true count", CountNotTrusted, n)
	}
}

// The heart of it: a rule read off a MIB's names found a "count" that is not
// one. The table must come back whole, not cut to what the column said.
func TestACountThatLiesLosesNoData(t *testing.T) {
	for _, tc := range []struct {
		name string
		make func() *filledAgent
	}{
		{"the row past the count holds data", func() *filledAgent {
			a := newFilledAgent(2)
			a.set(boundTable.Append(2, 1, 3), codec.Int(5))
			return a
		}},
		{"zero everywhere, and the table has rows", func() *filledAgent {
			a := newFilledAgent(0)
			a.set(boundTable.Append(2, 1, 1), codec.String("real"))
			return a
		}},
		{"the rows number from 0", func() *filledAgent {
			a := newFilledAgent(1)
			a.add(boundTable.Append(2, 1, 0), codec.Int(7))
			a.set(boundTable.Append(2, 1, 1), codec.Int(7))
			a.set(boundCount.Append(1), codec.Int(2))
			return a
		}},
		{"the last counted row is empty", func() *filledAgent {
			a := newFilledAgent(2)
			a.set(boundTable.Append(2, 1, 2), codec.Int(0))
			return a
		}},
		{"a counted row is not there", func() *filledAgent {
			a := newFilledAgent(3)
			a.remove(boundTable.Append(2, 1, 2))
			a.remove(boundTable.Append(1, 1, 2))
			return a
		}},
		{"a negative count", func() *filledAgent { return newFilledAgent(-1) }},
		{"a count that cannot be a count", func() *filledAgent {
			a := newFilledAgent(1)
			a.set(boundCount.Append(1), codec.String("x"))
			return a
		}},
		{"a count beyond the table's size", func() *filledAgent { return newFilledAgent(9) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := tc.make()
			s, prof := boundedSession(t, a)
			if got, want := walkNames(t, s, boundRoot, boundedTable()), plainWalk(t, s, boundRoot); strings.Join(got, " ") != strings.Join(want, " ") {
				t.Fatalf("lost data:\n%v\nwant\n%v", got, want)
			}
			if prof.Snapshot()[CountNotTrusted] != 1 {
				t.Errorf("%s fired %d times", CountNotTrusted, prof.Snapshot()[CountNotTrusted])
			}
		})
	}
}

// A runaway count costs no row read at all: it fails before anything else.
func TestABadCountReadsNoRows(t *testing.T) {
	a := newFilledAgent(4_000_000)
	s, prof := boundedSession(t, a)
	walkNames(t, s, boundTable, boundedTable())
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, name := range a.asked {
		if name == boundTable.Append(2, 1, 1_000_000).String() {
			t.Fatal("it walked up to the count")
		}
	}
	if prof.Snapshot()[CountNotTrusted] != 1 {
		t.Error("a count over the limit must be noted")
	}
}

// Every value an empty row is known to hold, whichever its type.
func TestWhatABlankRowHolds(t *testing.T) {
	col := mib.Column{Unknown: []int64{99}}
	for _, v := range []codec.Value{
		codec.NoSuchInstance(), codec.Null(), codec.Int(0), codec.Int(99), codec.Gauge32(0),
		codec.String(""), codec.Bytes([]byte{0, 0}), codec.String("  "), codec.IPAddress([]byte{0, 0, 0, 0}),
		codec.ObjectID(codec.MustParseOID("0.0")), {Type: codec.TypeOID},
	} {
		if !isBlank(col, v) {
			t.Errorf("%+v is what an empty row holds", v)
		}
	}
	for _, v := range []codec.Value{
		codec.Int(-1), codec.Gauge32(1), codec.String("x"), codec.IPAddress([]byte{10, 0, 0, 1}),
		codec.ObjectID(codec.MustParseOID("1.3")),
	} {
		if isBlank(col, v) {
			t.Errorf("%+v is data", v)
		}
	}
}

func TestANoCountTableIsWalkedAndIsNotAnError(t *testing.T) {
	a := newFilledAgent()
	s, prof := boundedSession(t, a)
	if got, want := walkNames(t, s, boundRoot, boundedTable()), plainWalk(t, s, boundRoot); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("%v\nwant\n%v", got, want)
	}
	if prof.Snapshot()[CountNotTrusted] != 0 {
		t.Error("no counts is not a count that lied")
	}
}

// A count column read in part counts nothing: the table is walked.
func TestACountColumnThatFailsIsNotACount(t *testing.T) {
	calls := 0
	a := newFilledAgent(1)
	addr := scriptedAgent(t, func(req codec.Message) *codec.Message {
		calls++
		if calls == 2 { // the first hop found the table; this reads the counts
			return respondWith(req, codec.GenErr)
		}
		return a.answer(req)
	})
	s := dial(t, addr, Options{Version: codec.Version2c, MaxRepetitions: 3, Timeout: time.Second})
	if got := walkNames(t, s, boundTable, boundedTable()); len(got) != 24 {
		t.Fatalf("%d rows: %v", len(got), got)
	}
}

// A branch inside the table reads only that branch.
func TestAWalkOfOneColumnReadsOneColumn(t *testing.T) {
	s, _ := boundedSession(t, newFilledAgent(2))
	if got := walkNames(t, s, boundTable.Append(2), boundedTable()); len(got) != 2 {
		t.Fatalf("%v", got)
	}
}

// A plain walk that replaces a table inside a scoped walk stays in scope.
func TestAFailedCountInsideABranchStaysInTheBranch(t *testing.T) {
	s, _ := boundedSession(t, newFilledAgent(9))
	if got := walkNames(t, s, boundTable.Append(2), boundedTable()); len(got) != 12 {
		t.Fatalf("%d rows: %v", len(got), got)
	}
}

// An absent column in a real row is an ordinary SNMP row.
func TestARowMissingAColumnIsStillARow(t *testing.T) {
	a := newFilledAgent(1)
	third := boundTable.Append(3)
	a.add(third.Append(1, 1), codec.Int(4))
	b := boundedTable()
	b.Columns = append(b.Columns, mib.Column{OID: third})
	// Row 1 of programme 1 lacks column 3 on the next device.
	a.remove(third.Append(1, 1))
	s, prof := boundedSession(t, a)
	got := walkNames(t, s, boundTable, b)
	if strings.Join(got, " ") != boundTable.Append(1, 1, 1).String()+" "+boundTable.Append(2, 1, 1).String() {
		t.Fatalf("%v", got)
	}
	if prof.Snapshot()[CountNotTrusted] != 0 {
		t.Error("a row with a column missing is not a missing row")
	}
}

// A refused read proves nothing, so the plain walk decides.
func TestARefusedRowReadFallsBackToTheWalk(t *testing.T) {
	a := newFilledAgent(1)
	a.failOn = boundTable.Append(2, 1, 1).String()
	s, prof := boundedSession(t, a)
	if got := walkNames(t, s, boundTable, boundedTable()); len(got) != 24 {
		t.Fatalf("%d rows", len(got))
	}
	if prof.Snapshot()[CountNotTrusted] != 1 {
		t.Error("a count that could not be proven must be noted")
	}
}

// Past the proof, the row read itself can fail too.
func TestARefusedReadAfterTheProofFallsBackToTheWalk(t *testing.T) {
	a := newFilledAgent(2)
	a.failOn = boundTable.Append(1, 1, 1).String() // an index column: the proof does not read it
	s, prof := boundedSession(t, a)
	if got := walkNames(t, s, boundTable, boundedTable()); len(got) != 24 {
		t.Fatalf("%d rows", len(got))
	}
	if prof.Snapshot()[CountNotTrusted] != 1 {
		t.Error("must be noted")
	}
}

// v1 has no per-object exception: an absent row fails the whole GET.
func TestTheProofWorksUnderV1(t *testing.T) {
	a := newFilledAgent(2)
	a.v1 = true
	addr := scriptedAgent(t, a.answer)
	prof := &compliance.Profile{}
	s := dial(t, addr, Options{Version: codec.Version1, Compliance: prof, Timeout: time.Second})
	if got := walkNames(t, s, boundTable, boundedTable()); len(got) != 4 {
		t.Fatalf("%v", got)
	}
	if prof.Snapshot()[CountNotTrusted] != 0 {
		t.Error("the count is true")
	}
}

// A device that dies between the proof and one name stops being read.
func TestAV1RetryThatFailsIsAFailure(t *testing.T) {
	a := newFilledAgent(2)
	a.v1 = true
	a.failOn = boundTable.Append(2, 1, 2).String()
	addr := scriptedAgent(t, a.answer)
	s := dial(t, addr, Options{Version: codec.Version1, Timeout: time.Second})
	names := []codec.OID{boundTable.Append(2, 1, 0), boundTable.Append(2, 1, 2)}
	if _, err := s.getRows(context.Background(), names); err == nil {
		t.Fatal("a refused name must fail the read")
	}
}

func TestACallbackErrorStopsAtARow(t *testing.T) {
	s, _ := boundedSession(t, newFilledAgent(1))
	stop := errors.New("stop")
	err := s.WalkBounded(context.Background(), boundTable, []mib.Bounded{boundedTable()}, func(codec.VarBind) error { return stop })
	if !errors.Is(err, stop) {
		t.Fatalf("%v", err)
	}
}

// A nested table's count is a column of the rows just read: no extra walk.
func TestANestedTableCountsFromRowsAlreadyRead(t *testing.T) {
	inner := codec.MustParseOID("1.3.6.1.4.1.9.2.2")
	outer := boundedTable()
	nested := mib.Bounded{Table: inner, Count: boundTable.Append(3), Max: 4, Columns: []mib.Column{{OID: inner.Append(1)}}}
	a := newFilledAgent(1)
	a.add(boundTable.Append(3, 1, 1), codec.Int(1))
	a.add(inner.Append(1, 1, 1, 1), codec.Int(5))
	outer.Columns = append(outer.Columns, mib.Column{OID: boundTable.Append(3)})
	s, prof := boundedSession(t, a)
	got := strings.Join(walkNames(t, s, boundRoot, outer, nested), " ")
	if !strings.Contains(got, inner.Append(1, 1, 1, 1).String()) {
		t.Fatalf("the nested row was not read: %v", got)
	}
	if prof.Snapshot()[CountNotTrusted] != 0 {
		t.Error("both counts are true")
	}
}

// Only the first index with no rows is proven: a filled parent has a
// hundred of them, and they would all say the same.
func TestOneEmptyIndexStandsForTheRest(t *testing.T) {
	a := newFilledAgent(1, 0, 0)
	s, _ := boundedSession(t, a)
	walkNames(t, s, boundTable, boundedTable())
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, name := range a.asked {
		if name == boundTable.Append(2, 3, 1).String() {
			t.Error("the second empty index was read")
		}
	}
}

func TestAGaugeIsACount(t *testing.T) {
	if n := rowCount(codec.Gauge32(4)); n != 4 {
		t.Errorf("%d", n)
	}
	if n := rowCount(codec.String("4")); n != -1 {
		t.Errorf("%d", n)
	}
}

func TestANameOutsideTheTableHasNoColumn(t *testing.T) {
	if got := columnOf(boundedTable(), boundAfter); got.OID != nil {
		t.Errorf("%+v", got)
	}
}

// A session with no profile counts nothing, and still falls back.
func TestAFallbackNeedsNoProfile(t *testing.T) {
	a := newFilledAgent(9)
	addr := scriptedAgent(t, a.answer)
	s := dial(t, addr, Options{Version: codec.Version2c, MaxRepetitions: 3, Timeout: time.Second})
	if got := walkNames(t, s, boundTable, boundedTable()); len(got) != 24 {
		t.Fatalf("%d rows", len(got))
	}
}

// All counts zero and nothing in the table: an empty table, and a true one.
func TestAllZeroCountsOverAnEmptyTableAreTrue(t *testing.T) {
	a := newFilledAgent(0, 0)
	s, prof := boundedSession(t, a)
	got := walkNames(t, s, boundTable, boundedTable())
	if len(got) != 0 || prof.Snapshot()[CountNotTrusted] != 0 {
		t.Fatalf("%v, %d", got, prof.Snapshot()[CountNotTrusted])
	}
}

// A second index whose last row is blank fails after the rows are read: the
// proof only read the largest count's last row.
func TestAnotherIndexWithABlankRowFails(t *testing.T) {
	a := newFilledAgent(3, 2)
	a.set(boundTable.Append(2, 2, 2), codec.Int(0))
	s, prof := boundedSession(t, a)
	if got, want := walkNames(t, s, boundTable, boundedTable()), plainWalk(t, s, boundTable); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("%v", got)
	}
	if prof.Snapshot()[CountNotTrusted] != 1 {
		t.Error("must be noted")
	}
}

// The proof stops at three columns, and only reads ones that hold data.
func TestTheProofReadsColumnsThatHoldData(t *testing.T) {
	b := mib.Bounded{Columns: []mib.Column{{OID: boundTable.Append(1)}}}
	binds := []codec.VarBind{}
	for i := 0; i < 5; i++ {
		binds = append(binds, codec.VarBind{Name: boundTable.Append(1, uint32(i)), Value: codec.Int(1)})
	}
	if got := holding(b, binds); len(got) != proofColumns {
		t.Errorf("%d", len(got))
	}
}

// The largest count need not come first.
func TestTheLargestCountIsFound(t *testing.T) {
	s, prof := boundedSession(t, newFilledAgent(1, 2))
	if got := walkNames(t, s, boundTable, boundedTable()); len(got) != 6 {
		t.Fatalf("%v", got)
	}
	if prof.Snapshot()[CountNotTrusted] != 0 {
		t.Error("the counts are true")
	}
}

// A v1 batch with one absent name still gives the others.
func TestAV1BatchKeepsWhatItHas(t *testing.T) {
	a := newFilledAgent(2)
	a.v1 = true
	s := dial(t, scriptedAgent(t, a.answer), Options{Version: codec.Version1, Timeout: time.Second})
	binds, err := s.getRows(context.Background(), []codec.OID{boundTable.Append(2, 1, 1), boundTable.Append(2, 1, 99)})
	if err != nil || len(binds) != 2 || binds[0].Value.Type != codec.TypeInteger || !isMissing(binds[1].Value) {
		t.Fatalf("%+v, %v", binds, err)
	}
}
