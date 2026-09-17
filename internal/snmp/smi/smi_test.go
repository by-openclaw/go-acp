package smi

// Each test is a few lines of MIB source and the resolved shape it must
// produce. The real sets — 278 files — are exercised by tools/mibc; these
// pin the rules one at a time, including every way vendor source is known
// to be wrong, because those are the rules a refactor would quietly break.

import (
	"strings"
	"testing"
)

func mustParse(t *testing.T, src string) ([]*Module, []Finding) {
	t.Helper()
	mods, f, err := Parse("t.mib", []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return mods, f
}

func findingKinds(fs []Finding) map[string]int {
	out := map[string]int{}
	for _, f := range fs {
		out[f.Kind]++
	}
	return out
}

func byName(objs []Object) map[string]Object {
	out := map[string]Object{}
	for _, o := range objs {
		if _, ok := out[o.Name]; !ok {
			out[o.Name] = o
		}
	}
	return out
}

// base is a minimal SMI to hang modules off, as RFC1155-SMI and
// SNMPv2-SMI do in the real sets.
const base = `
SNMPv2-SMI DEFINITIONS ::= BEGIN
  OBJECT-TYPE MACRO ::= BEGIN TYPE NOTATION ::= "SYNTAX" VALUE NOTATION ::= value(VALUE ObjectName) END
  org OBJECT IDENTIFIER ::= { iso 3 }
  dod OBJECT IDENTIFIER ::= { org 6 }
  internet OBJECT IDENTIFIER ::= { dod 1 }
  enterprises OBJECT IDENTIFIER ::= { internet 4 1 }
  Integer32 ::= [APPLICATION 2] IMPLICIT INTEGER (-2147483648..2147483647)
END
SNMPv2-TC DEFINITIONS ::= BEGIN
  IMPORTS Integer32 FROM SNMPv2-SMI;
  DisplayString ::= TEXTUAL-CONVENTION
      DISPLAY-HINT "255a"
      STATUS current
      DESCRIPTION "text, with the word SYNTAX in it"
      SYNTAX OCTET STRING (SIZE (0..255))
  TruthValue ::= TEXTUAL-CONVENTION
      STATUS current
      DESCRIPTION "boolean"
      SYNTAX INTEGER { true(1), false(2) }
  Wrapped ::= TruthValue
END
`

func compileSrc(t *testing.T, srcs ...string) *Compiled {
	t.Helper()
	var mods []*Module
	for _, s := range srcs {
		ms, _ := mustParse(t, s)
		mods = append(mods, ms...)
	}
	kept, _ := Select(mods, nil)
	return Compile(kept)
}

// ---------------------------------------------------------------------
// the lexer
// ---------------------------------------------------------------------

func TestLexer(t *testing.T) {
	toks, err := lex("OBJECT-TYPE foo--comment\n\"a \"\"q\"\"\nb\" 'FF'H '01'B 'x' -12 7 ::= .. { } foo- bar")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tk := range toks {
		got = append(got, tk.text)
	}
	want := []string{"OBJECT-TYPE", "foo", `a "q"` + "\nb", "'FF'H", "'01'B", "'x'", "-12", "7",
		"::=", "..", "{", "}", "foo", "-", "bar", ""}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("tokens\n got %q\nwant %q", got, want)
	}
	// A multi-line string still leaves the line count right for what
	// follows it, so findings point at the right place.
	if toks[3].line != 3 {
		t.Errorf("token after a two-line string is on line %d, want 3", toks[3].line)
	}
}

func TestLexerRefusals(t *testing.T) {
	if _, err := lex(`"never closed`); err == nil || !strings.Contains(err.Error(), "unterminated string") {
		t.Errorf("= %v", err)
	}
	if _, err := lex("'never\nclosed"); err == nil || !strings.Contains(err.Error(), "quoted literal") {
		t.Errorf("= %v", err)
	}
	if _, _, err := Parse("bad.mib", []byte(`X DEFINITIONS ::= BEGIN "open`)); err == nil ||
		!strings.Contains(err.Error(), "bad.mib") {
		t.Errorf("Parse must name the file it could not tokenise: %v", err)
	}
}

// ---------------------------------------------------------------------
// the parser
// ---------------------------------------------------------------------

func TestParseEveryConstruct(t *testing.T) {
	src := `junk before the module is ignored
TEST-MIB DEFINITIONS IMPLICIT TAGS ::= BEGIN
IMPORTS
    OBJECT-TYPE, MODULE-IDENTITY, NOTIFICATION-TYPE, enterprises FROM SNMPv2-SMI
    DisplayString, TruthValue FROM SNMPv2-TC;
EXPORTS everything;

testMIB MODULE-IDENTITY
    LAST-UPDATED "0903100925Z"
    ORGANIZATION "o" CONTACT-INFO "c" DESCRIPTION "d"
    REVISION "0903100925Z" DESCRIPTION "r"
    ::= { enterprises 99 }

testRoot OBJECT IDENTIFIER ::= { testMIB 1 }

testTable OBJECT-TYPE
    SYNTAX SEQUENCE OF TestEntry
    MAX-ACCESS not-accessible
    STATUS current
    DESCRIPTION "t"
    ::= { testRoot 1 }

testEntry OBJECT-TYPE
    SYNTAX TestEntry
    MAX-ACCESS not-accessible
    STATUS current
    DESCRIPTION "e"
    INDEX { IMPLIED testName, testIndex }
    ::= { testTable 1 }

TestEntry ::= SEQUENCE { testName DisplayString, testIndex INTEGER }

testName OBJECT-TYPE
    SYNTAX DisplayString (SIZE (0..32))
    MAX-ACCESS read-only
    STATUS current
    UNITS "chars"
    DESCRIPTION "n"
    REFERENCE "r"
    DEFVAL { "a { nested } value" }
    ::= { testEntry 1 }

testMode OBJECT-TYPE
    SYNTAX INTEGER { off(0), on(1), 188(188), -neg(2) }
    ACCESS read-write
    STATUS mandatory
    DESCRIPTION "m"
    ::= { testRoot 2 }

testAug OBJECT-TYPE
    SYNTAX TruthValue
    MAX-ACCESS read-write
    STATUS current
    DESCRIPTION "a"
    AUGMENTS { testEntry }
    ::= { testRoot 3 }

testNote NOTIFICATION-TYPE
    OBJECTS { testName }
    STATUS current
    DESCRIPTION "n"
    ::= { testRoot 4 }

testTrap TRAP-TYPE
    ENTERPRISE testRoot
    VARIABLES { testName }
    DESCRIPTION "v1"
    ::= 7

anInteger INTEGER ::= 5
aValue SomeType ::= { 1 2 }

Choosey ::= CHOICE { a INTEGER, b OCTET STRING }
Bits ::= BITS { a(0), b(1) }
Old ::= BIT STRING
Oid ::= OBJECT IDENTIFIER

testCaps AGENT-CAPABILITIES
    PRODUCT-RELEASE "1" STATUS current DESCRIPTION "c"
    SUPPORTS TEST-MIB INCLUDES { testGroup }
    VARIATION testName WRITE-SYNTAX DisplayString (SIZE (0..8)) DESCRIPTION "v"
    ::= { testRoot 5 }
END
`
	mods, findings := mustParse(t, src)
	if len(mods) != 1 {
		t.Fatalf("%d modules", len(mods))
	}
	m := mods[0]
	if m.Name != "TEST-MIB" || m.LastUpdated != "0903100925Z" {
		t.Errorf("module = %s / %q", m.Name, m.LastUpdated)
	}
	if m.Imports["DisplayString"] != "SNMPv2-TC" || m.Imports["enterprises"] != "SNMPv2-SMI" {
		t.Errorf("imports = %v", m.Imports)
	}
	if m.Imports["everything"] != "" {
		t.Error("EXPORTS must not be read as an import")
	}
	for _, name := range []string{"TestEntry", "Choosey", "Bits", "Old", "Oid"} {
		if m.Types[name] == nil {
			t.Errorf("type %s was not recorded", name)
		}
	}
	if m.Types["Choosey"].Syntax.Base != "CHOICE" || m.Types["Old"].Syntax.Base != "BIT STRING" {
		t.Errorf("CHOICE/BIT STRING = %+v / %+v", m.Types["Choosey"].Syntax, m.Types["Old"].Syntax)
	}

	nodes := map[string]*Node{}
	for _, n := range m.Nodes {
		nodes[n.Name] = n
	}
	for _, want := range []string{"testMIB", "testRoot", "testTable", "testEntry", "testName",
		"testMode", "testAug", "testNote", "testTrap", "testCaps"} {
		if nodes[want] == nil {
			t.Errorf("%s was not parsed", want)
		}
	}
	if s := nodes["testTable"].Syntax; s.Base != "SEQUENCE OF" || s.Of != "TestEntry" {
		t.Errorf("SEQUENCE OF = %+v", s)
	}
	if got := nodes["testEntry"].Index; strings.Join(got, ",") != "testName,testIndex" {
		t.Errorf("INDEX = %v (IMPLIED must be dropped)", got)
	}
	n := nodes["testName"]
	if n.Syntax.Constraint != "(SIZE (0..32))" || n.Units != "chars" || n.Access != "read-only" {
		t.Errorf("testName = %+v / %+v", n, n.Syntax)
	}
	mode := nodes["testMode"]
	// `-neg(2)` is not an identifier: the stray `-` is reported and the
	// member after it is kept, which is the recovery a typo deserves.
	if mode.Access != "read-write" || mode.Status != "mandatory" || len(mode.Syntax.Enums) != 4 {
		t.Fatalf("testMode = %+v, enums %v", mode, mode.Syntax.Enums)
	}
	if mode.Syntax.Enums[2] != (Enum{"188", 188}) || mode.Syntax.Enums[3] != (Enum{"neg", 2}) {
		t.Errorf("enums = %v", mode.Syntax.Enums)
	}
	if nodes["testAug"].Augments != "testEntry" {
		t.Errorf("AUGMENTS = %q", nodes["testAug"].Augments)
	}
	if tr := nodes["testTrap"]; tr.Kind != KindTrap || tr.Enterprise != "testRoot" || tr.TrapNumber != 7 {
		t.Errorf("trap = %+v", tr)
	}
	// The one malformed enum member is the only finding.
	if k := findingKinds(findings); k["syntax"] != 1 || len(findings) != 1 {
		t.Errorf("findings = %v", findings)
	}
}

// A SET-able TC with an enum refinement, and a type written as an ASN.1
// tagged application type, both parse to something the compiler can use.
func TestParseTaggedAndRefinedTypes(t *testing.T) {
	mods, _ := mustParse(t, `M DEFINITIONS ::= BEGIN
  Counter ::= [APPLICATION 1] IMPLICIT INTEGER (0..4294967295)
  Explicit ::= [1] EXPLICIT OCTET STRING
END`)
	if s := mods[0].Types["Counter"].Syntax; s.Base != "INTEGER" || s.Constraint != "(0..4294967295)" {
		t.Errorf("Counter = %+v", s)
	}
	if s := mods[0].Types["Explicit"].Syntax; s.Base != "OCTET STRING" {
		t.Errorf("Explicit = %+v", s)
	}
}

// Every way a file can be malformed produces a finding and a module, not
// a failure of the whole set.
func TestParseRecovers(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		{"no BEGIN", `A DEFINITIONS ::= nope`, "without BEGIN"},
		{"no END", `A DEFINITIONS ::= BEGIN x OBJECT IDENTIFIER ::= { iso 1 }`, "no END"},
		{"OBJECT IDENTIFIER with no value", `A DEFINITIONS ::= BEGIN x OBJECT IDENTIFIER END`, "without a value"},
		{"a macro with no value", `A DEFINITIONS ::= BEGIN x OBJECT-TYPE SYNTAX INTEGER END`, "no ::= value"},
		{"a trap with no value", `A DEFINITIONS ::= BEGIN x TRAP-TYPE ENTERPRISE y END`, "no ::= value"},
		{"a trap with a bad number", `A DEFINITIONS ::= BEGIN x TRAP-TYPE ENTERPRISE y ::= z END`, `value "z"`},
		{"an assignment that starts with a symbol", `A DEFINITIONS ::= BEGIN , END`, "unexpected"},
		{"an OID value with no braces", `A DEFINITIONS ::= BEGIN x OBJECT IDENTIFIER ::= iso END`, "in braces"},
		{"an OID arc wider than 32 bits", `A DEFINITIONS ::= BEGIN x OBJECT IDENTIFIER ::= { iso 99999999999 } END`, "32-bit"},
		{"punctuation inside an OID value", `A DEFINITIONS ::= BEGIN x OBJECT IDENTIFIER ::= { iso , 1 } END`, "unexpected"},
		{"an enum value that is not a number", `A DEFINITIONS ::= BEGIN x OBJECT-TYPE SYNTAX INTEGER { a(b) } ::= { iso 1 } END`, "has value"},
		{"an enumeration that never closed", `A DEFINITIONS ::= BEGIN x OBJECT-TYPE SYNTAX INTEGER { a(1), MAX-ACCESS read-only ::= { iso 1 } END`, "not closed"},
		{"an enumeration cut off by the end of the file", `A DEFINITIONS ::= BEGIN x OBJECT-TYPE SYNTAX INTEGER { a(1)`, "no END"},
		{"the same symbol defined twice", `A DEFINITIONS ::= BEGIN x OBJECT IDENTIFIER ::= { iso 1 } x OBJECT IDENTIFIER ::= { iso 2 } END`, "defined twice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, findings := mustParse(t, tc.src)
			var msgs []string
			for _, f := range findings {
				msgs = append(msgs, f.Msg)
			}
			if !strings.Contains(strings.Join(msgs, " | "), tc.want) {
				t.Errorf("findings %q, want one containing %q", msgs, tc.want)
			}
		})
	}
}

// The unclosed enumeration keeps its object: the one member is lost and
// the OID and access survive, which is the point of stopping early.
func TestAnUnclosedEnumerationKeepsItsObject(t *testing.T) {
	mods, _ := mustParse(t, `A DEFINITIONS ::= BEGIN
  x OBJECT-TYPE SYNTAX INTEGER { a(1), MAX-ACCESS read-only STATUS current ::= { iso 9 }
END`)
	n := mods[0].Nodes[0]
	if n.Access != "read-only" || len(n.Value) != 2 {
		t.Errorf("node = %+v", n)
	}
}

// Assignments this compiler does not model are passed over without losing
// the next one, including one that runs into END.
func TestValueAssignmentsAreSkipped(t *testing.T) {
	mods, _ := mustParse(t, `A DEFINITIONS ::= BEGIN
  one INTEGER ::= 5
  two Thing ::= { 1 2 }
  kept OBJECT IDENTIFIER ::= { iso 1 }
  dangling Thing
END`)
	if len(mods[0].Nodes) != 1 || mods[0].Nodes[0].Name != "kept" {
		t.Errorf("nodes = %+v", mods[0].Nodes)
	}
}

func TestParserHelpers(t *testing.T) {
	p := &parser{toks: []token{{tIdent, "x", 1}, {tEOF, "", 1}}}
	if p.peekN(5).kind != tEOF {
		t.Error("peekN past the end must be EOF")
	}
	if got := p.parseNameList(); got != nil {
		t.Errorf("a name list with no brace = %v", got)
	}
	p.skipBalanced() // no brace: a no-op
	if p.pos != 0 {
		t.Error("skipBalanced moved without a brace")
	}
	// A string inside a skipped block is inert even if it holds braces.
	toks, _ := lex(`{ "}" { } } after`)
	p = &parser{toks: toks}
	p.skipBalanced()
	if p.peek().text != "after" {
		t.Errorf("skipBalanced stopped at %q", p.peek().text)
	}
	for _, tc := range []struct {
		b    string
		next string
		want bool
	}{
		{"", "x", false}, {"(", "SIZE", false}, {"0..", "32", false},
		{"x", ")", false}, {"x", "..", false}, {"SIZE", "(", true},
	} {
		if got := needsSpace([]byte(tc.b), tc.next); got != tc.want {
			t.Errorf("needsSpace(%q, %q) = %v", tc.b, tc.next, got)
		}
	}
	// A string token is never mistaken for the keyword it spells.
	toks, _ = lex(`"SYNTAX"`)
	if (&parser{toks: toks}).is("SYNTAX") {
		t.Error("a string matched a keyword")
	}
}

// ---------------------------------------------------------------------
// selection
// ---------------------------------------------------------------------

func TestSelect(t *testing.T) {
	a := &Module{Name: "X", File: "b/X.mib", LastUpdated: "0709191610Z"}
	b := &Module{Name: "X", File: "a/X.mib", LastUpdated: "1305161501Z"}
	c := &Module{Name: "X", File: "c/X.mib", LastUpdated: "1305161501Z"}
	solo := &Module{Name: "Y", File: "Y.mib"}

	kept, findings := Select([]*Module{a, b, solo}, nil)
	if len(kept) != 2 || kept[0] != b || kept[1] != solo {
		t.Fatalf("kept = %v", kept)
	}
	if len(findings) != 1 || !strings.Contains(findings[0].Msg, "newest LAST-UPDATED 1305161501Z") {
		t.Errorf("findings = %v", findings)
	}

	// Equal dates: the first path wins, and the reason says so.
	kept, findings = Select([]*Module{c, b}, nil)
	if kept[0] != b || !strings.Contains(findings[0].Msg, "same revision") {
		t.Errorf("tie = %v / %v", kept, findings)
	}

	// A pin beats a date; a pin that matches nothing does not.
	kept, findings = Select([]*Module{a, b}, map[string]string{"X": "b/"})
	if kept[0] != a || !strings.Contains(findings[0].Msg, "pinned") {
		t.Errorf("pin = %v / %v", kept, findings)
	}
	kept, _ = Select([]*Module{a, b}, map[string]string{"X": "nowhere"})
	if kept[0] != b {
		t.Errorf("an unmatched pin must fall back to the date: %v", kept)
	}
}

// Vendors write 2013 as 13; below 70 that is read as 20YY, which is the
// deviation from RFC 2578 that keeps the newest copy the newest.
func TestRevision(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"1305161501Z", "201305161501"},
		{"9801010000Z", "199801010000"},
		{"201305161501Z", "201305161501"},
		{"", ""},
	} {
		if got := revision(tc.in); got != tc.want {
			t.Errorf("revision(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------
// compilation
// ---------------------------------------------------------------------

func TestCompileResolvesThroughImportsAndTCs(t *testing.T) {
	c := compileSrc(t, base, `V DEFINITIONS ::= BEGIN
  IMPORTS enterprises, Integer32 FROM SNMPv2-SMI DisplayString, TruthValue, Wrapped FROM SNMPv2-TC;
  vendor OBJECT IDENTIFIER ::= { enterprises 1773 }
  absolute OBJECT IDENTIFIER ::= { iso(1) org(3) 6 }
  numeric OBJECT IDENTIFIER ::= { 1 3 6 1 2 }
  name OBJECT-TYPE SYNTAX DisplayString ACCESS read-only STATUS mandatory ::= { vendor 1 }
  flag OBJECT-TYPE SYNTAX Wrapped MAX-ACCESS read-write STATUS current ::= { vendor 2 }
  own OBJECT-TYPE SYNTAX TruthValue { yes(1) } MAX-ACCESS read-write STATUS current ::= { vendor 3 }
  count OBJECT-TYPE SYNTAX Counter MAX-ACCESS read-only STATUS current ::= { vendor 4 }
  gauge OBJECT-TYPE SYNTAX Unsigned32 MAX-ACCESS read-only STATUS current ::= { vendor 5 }
  plain OBJECT-TYPE SYNTAX INTEGER (0..7) MAX-ACCESS read-only STATUS current ::= { vendor 6 }
  alien OBJECT-TYPE SYNTAX NotDefinedAnywhere MAX-ACCESS read-only STATUS current ::= { vendor 7 }
  alarm TRAP-TYPE ENTERPRISE vendor ::= 4
END`)
	got := byName(c.Objects)
	check := func(name, oid, baseType, tc string) {
		t.Helper()
		o, ok := got[name]
		if !ok {
			t.Fatalf("%s did not resolve; findings %v", name, c.Findings)
		}
		if o.Dotted() != oid || o.Base != baseType || o.TC != tc {
			t.Errorf("%s = %s %q %q, want %s %q %q", name, o.Dotted(), o.Base, o.TC, oid, baseType, tc)
		}
	}
	check("vendor", "1.3.6.1.4.1.1773", "", "")
	check("absolute", "1.3.6", "", "")
	check("numeric", "1.3.6.1.2", "", "")
	check("name", "1.3.6.1.4.1.1773.1", "OCTET STRING", "DisplayString")
	check("flag", "1.3.6.1.4.1.1773.2", "INTEGER", "Wrapped")
	check("own", "1.3.6.1.4.1.1773.3", "INTEGER", "TruthValue")
	check("count", "1.3.6.1.4.1.1773.4", "Counter32", "")
	check("gauge", "1.3.6.1.4.1.1773.5", "Gauge32", "")
	check("plain", "1.3.6.1.4.1.1773.6", "INTEGER", "")
	check("alien", "1.3.6.1.4.1.1773.7", "", "NotDefinedAnywhere")
	// RFC 3584 §3.1: enterprise, 0, trap number.
	check("alarm", "1.3.6.1.4.1.1773.0.4", "", "")

	// Enumerations are inherited through a TC chain, and the object's
	// own refinement wins over the type's.
	if e := got["flag"].Enums; len(e) != 2 || e[0].Name != "true" {
		t.Errorf("inherited enums = %v", e)
	}
	if e := got["own"].Enums; len(e) != 1 || e[0].Name != "yes" {
		t.Errorf("own enums = %v", e)
	}
	// Objects come out in OID order.
	for i := 1; i < len(c.Objects); i++ {
		if compareOID(c.Objects[i-1].OID, c.Objects[i].OID) > 0 {
			t.Fatalf("not in OID order at %d", i)
		}
	}
}

// A symbol re-imported through a module that only imported it itself
// still resolves, which is how some vendor MIBs are written.
func TestCompileFollowsReImports(t *testing.T) {
	c := compileSrc(t, base,
		`MID DEFINITIONS ::= BEGIN IMPORTS enterprises FROM SNMPv2-SMI; END`,
		`LEAF DEFINITIONS ::= BEGIN IMPORTS enterprises FROM MID; x OBJECT IDENTIFIER ::= { enterprises 5 } END`)
	if o, ok := byName(c.Objects)["x"]; !ok || o.Dotted() != "1.3.6.1.4.1.5" {
		t.Errorf("x = %+v, findings %v", o, c.Findings)
	}
}

func TestCompileReportsWhatItCannotResolve(t *testing.T) {
	c := compileSrc(t, base, `U DEFINITIONS ::= BEGIN
  IMPORTS ghost FROM NOWHERE, enterprises FROM SNMPv2-SMI;
  orphan OBJECT IDENTIFIER ::= { ghost 1 }
  child OBJECT IDENTIFIER ::= { orphan 2 }
  loopA OBJECT IDENTIFIER ::= { loopB 1 }
  loopB OBJECT IDENTIFIER ::= { loopA 1 }
  bareArc OBJECT IDENTIFIER ::= { enterprises named }
  lostTrap TRAP-TYPE ENTERPRISE nobody ::= 1
  deadTrap TRAP-TYPE ENTERPRISE orphan ::= 2
END`)
	got := byName(c.Objects)
	for _, n := range []string{"orphan", "child", "loopA", "loopB", "bareArc", "lostTrap", "deadTrap"} {
		if _, ok := got[n]; ok {
			t.Errorf("%s resolved and must not have", n)
		}
	}
	var msgs []string
	for _, f := range c.Findings {
		msgs = append(msgs, f.Msg)
	}
	all := strings.Join(msgs, " | ")
	for _, want := range []string{"neither defined nor imported", "did not resolve",
		"in terms of itself", "has no number", "names enterprise"} {
		if !strings.Contains(all, want) {
			t.Errorf("no finding containing %q in %q", want, msgs)
		}
	}
}

// An empty OID value — which the parser reports — does not panic the
// compiler either.
func TestCompileSurvivesAnEmptyValue(t *testing.T) {
	m := newModule("E", "e.mib")
	m.addNode(&Node{Name: "empty", Kind: KindObjectIdentifier})
	c := Compile([]*Module{m})
	if len(c.Objects) != 0 || len(c.Findings) != 1 || !strings.Contains(c.Findings[0].Msg, "empty OID") {
		t.Errorf("= %+v", c)
	}
}

// A type defined in terms of itself stops rather than recursing forever,
// and a type imported from a module that is not in the set is kept as a
// name.
func TestCompileTypeLoopsAndMissingTypes(t *testing.T) {
	c := compileSrc(t, base, `L DEFINITIONS ::= BEGIN
  IMPORTS enterprises FROM SNMPv2-SMI Far FROM ABSENT;
  Loop ::= Loop
  a OBJECT-TYPE SYNTAX Loop MAX-ACCESS read-only STATUS current ::= { enterprises 1 }
  b OBJECT-TYPE SYNTAX Far MAX-ACCESS read-only STATUS current ::= { enterprises 2 }
END`)
	got := byName(c.Objects)
	if got["a"].Base != "" || got["a"].TC != "Loop" {
		t.Errorf("a = %+v", got["a"])
	}
	if got["b"].Base != "" || got["b"].TC != "Far" {
		t.Errorf("b = %+v", got["b"])
	}
}

// One name per OID from one family; different names are both kept, the
// first by module as the default, because which is right depends on the
// device.
func TestConflictingNamesAreKept(t *testing.T) {
	c := compileSrc(t, base,
		`TT DEFINITIONS ::= BEGIN IMPORTS enterprises FROM SNMPv2-SMI;
		   shared OBJECT IDENTIFIER ::= { enterprises 9 }
		   userLsdPid OBJECT IDENTIFIER ::= { enterprises 9 4 } END`,
		`RX DEFINITIONS ::= BEGIN IMPORTS enterprises FROM SNMPv2-SMI;
		   shared OBJECT IDENTIFIER ::= { enterprises 9 }
		   userAudio3 OBJECT IDENTIFIER ::= { enterprises 9 4 } END`)
	var at94 []string
	for _, o := range c.Objects {
		if o.Dotted() == "1.3.6.1.4.1.9.4" {
			at94 = append(at94, o.Module+":"+o.Name)
		}
	}
	if strings.Join(at94, ",") != "RX:userAudio3,TT:userLsdPid" {
		t.Errorf("at .9.4 = %v, want both, RX first", at94)
	}
	n := 0
	for _, o := range c.Objects {
		if o.Dotted() == "1.3.6.1.4.1.9" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the same name from two modules must collapse to one row, got %d", n)
	}
	if k := findingKinds(c.Findings); k["conflicting-name"] != 1 {
		t.Errorf("findings = %v", c.Findings)
	}
}

// One module naming one OID twice — an alias left behind by a rename —
// keeps both too, in name order, so the default does not depend on the
// order the file happened to list them.
func TestAliasesInOneModuleAreOrderedByName(t *testing.T) {
	c := compileSrc(t, base, `A DEFINITIONS ::= BEGIN IMPORTS enterprises FROM SNMPv2-SMI;
	  zNew OBJECT IDENTIFIER ::= { enterprises 3 }
	  aOld OBJECT IDENTIFIER ::= { enterprises 3 } END`)
	var names []string
	for _, o := range c.Objects {
		if o.Dotted() == "1.3.6.1.4.1.3" {
			names = append(names, o.Name)
		}
	}
	if strings.Join(names, ",") != "aOld,zNew" {
		t.Errorf("at .3 = %v", names)
	}
}

func TestCompareOID(t *testing.T) {
	for _, tc := range []struct {
		a, b []uint32
		want int
	}{
		{[]uint32{1, 2}, []uint32{1, 2}, 0},
		{[]uint32{1, 2}, []uint32{1, 3}, -1},
		{[]uint32{1, 3}, []uint32{1, 2}, 1},
		{[]uint32{1}, []uint32{1, 2}, -1},
		{[]uint32{1, 2}, []uint32{1}, 1},
	} {
		if got := compareOID(tc.a, tc.b); got != tc.want {
			t.Errorf("compareOID(%v, %v) = %d", tc.a, tc.b, got)
		}
	}
}

func TestNaming(t *testing.T) {
	if KindTrap.String() != "trap" || NodeKind(99).String() != "kind(99)" {
		t.Error("NodeKind names")
	}
	if got := (Finding{"f", 3, "k", "m"}).String(); got != "f:3: k: m" {
		t.Errorf("= %q", got)
	}
	if got := (Finding{"f", 0, "k", "m"}).String(); got != "f: k: m" {
		t.Errorf("= %q", got)
	}
}

// ---------------------------------------------------------------------
// patches
// ---------------------------------------------------------------------

func TestParsePatches(t *testing.T) {
	ps, err := ParsePatches("# comment\n\nMOD sym parent 532 -- because line 338\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 || ps[0] != (Patch{"MOD", "sym", "parent", 532, "because line 338", 3}) {
		t.Errorf("= %+v", ps)
	}
	for _, tc := range []struct{ src, want string }{
		{"MOD sym parent 1", "no evidence"},
		{"MOD sym parent 1 --  ", "no evidence"},
		{"MOD sym 1 -- why", "got 3 fields"},
		{"MOD sym parent x -- why", "not a 32-bit number"},
	} {
		if _, err := ParsePatches(tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q = %v, want %q", tc.src, err, tc.want)
		}
	}
	// A line longer than the scanner will hold is an error, not a silent
	// truncation of the patch list.
	if _, err := ParsePatches(strings.Repeat("x", 70000)); err == nil {
		t.Error("an over-long line must be refused")
	}
}

func TestApplyPatches(t *testing.T) {
	mods, _ := mustParse(t, base+`REG DEFINITIONS ::= BEGIN
  IMPORTS enterprises FROM SNMPv2-SMI;
  modular OBJECT IDENTIFIER ::= { enterprises 7995 }
  moduleIQDMX30 OBJECT IDENTIFIER ::= { modular 531 }
END`)
	findings := Apply(mods, []Patch{
		{Module: "REG", Name: "moduleIQDMX31", Parent: "modular", Arc: 532, Why: "twin", Line: 1},
		{Module: "REG", Name: "moduleIQDMX30", Parent: "modular", Arc: 999, Why: "stale", Line: 2},
		{Module: "ABSENT", Name: "x", Parent: "y", Arc: 1, Why: "unused", Line: 3},
	})
	k := findingKinds(findings)
	if k["patched"] != 1 || k["patch-stale"] != 1 || k["patch-unused"] != 1 {
		t.Fatalf("findings = %v", findings)
	}
	c := Compile(mods)
	if o := byName(c.Objects)["moduleIQDMX31"]; o.Dotted() != "1.3.6.1.4.1.7995.532" {
		t.Errorf("patched symbol = %+v", o)
	}
	// The stale patch did not move the real definition.
	if o := byName(c.Objects)["moduleIQDMX30"]; o.Dotted() != "1.3.6.1.4.1.7995.531" {
		t.Errorf("real definition moved: %+v", o)
	}
}
