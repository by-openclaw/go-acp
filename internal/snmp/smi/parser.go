package smi

import (
	"bytes"
	"fmt"
	"strconv"
)

type parser struct {
	toks     []token
	pos      int
	file     string
	findings []Finding
}

func (p *parser) peek() token { return p.toks[p.pos] }

func (p *parser) peekN(k int) token {
	if p.pos+k < len(p.toks) {
		return p.toks[p.pos+k]
	}
	return p.toks[len(p.toks)-1]
}

func (p *parser) next() token {
	t := p.toks[p.pos]
	if t.kind != tEOF {
		p.pos++
	}
	return t
}

func (p *parser) eof() bool { return p.peek().kind == tEOF }

// is reports whether the next token is the keyword or symbol text. A
// string token never matches, so "SYNTAX" inside a DESCRIPTION is inert.
func (p *parser) is(text string) bool {
	t := p.peek()
	return t.kind != tString && t.kind != tEOF && t.text == text
}

func (p *parser) accept(text string) bool {
	if p.is(text) {
		p.pos++
		return true
	}
	return false
}

func (p *parser) note(line int, kind, format string, args ...any) {
	p.findings = append(p.findings, Finding{p.file, line, kind, fmt.Sprintf(format, args...)})
}

// add records a node, reporting a second definition of the same name.
func (p *parser) add(m *Module, n *Node) {
	if !m.addNode(n) {
		p.note(n.Line, "duplicate-symbol", "%s is defined twice in %s; the first definition is kept",
			n.Name, m.Name)
	}
}

// Parse reads every module in one file. A file may hold several — the IETF
// SMI files do — and anything before the first DEFINITIONS is skipped.
//
// The error is for source that cannot be tokenised at all; everything the
// parser could tokenise but not understand comes back as findings.
func Parse(file string, src []byte) ([]*Module, []Finding, error) {
	toks, err := lex(string(src))
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", file, err)
	}
	p := &parser{toks: toks, file: file}
	var mods []*Module
	for !p.eof() {
		if p.peek().kind == tIdent && p.peekN(1).text == "DEFINITIONS" {
			name := p.next()
			p.next() // DEFINITIONS
			for !p.eof() && !p.is("::=") {
				p.next() // a tag default, e.g. IMPLICIT TAGS
			}
			p.accept("::=")
			if !p.accept("BEGIN") {
				p.note(name.line, "syntax", "module %s: DEFINITIONS ::= without BEGIN", name.text)
				continue
			}
			mods = append(mods, p.parseModule(name))
			continue
		}
		p.next()
	}
	return mods, p.findings, nil
}

func (p *parser) parseModule(name token) *Module {
	m := newModule(name.text, p.file)
	for {
		switch {
		case p.eof():
			p.note(name.line, "syntax", "module %s: no END before the end of the file", m.Name)
			return m
		case p.accept("END"):
			return m
		case p.accept("IMPORTS"):
			p.parseImports(m)
		case p.accept("EXPORTS"):
			for !p.eof() && !p.accept(";") {
				p.next()
			}
		default:
			p.parseAssignment(m)
		}
	}
}

// parseImports reads `sym, sym FROM Mod sym FROM Mod ;`.
func (p *parser) parseImports(m *Module) {
	var pending []string
	for !p.eof() && !p.accept(";") {
		t := p.next()
		switch {
		case t.text == "FROM" && t.kind == tIdent:
			mod := p.next().text
			for _, s := range pending {
				m.Imports[s] = mod
			}
			pending = pending[:0]
		case t.kind == tIdent:
			pending = append(pending, t.text)
		}
	}
}

// macros is every assignment keyword that ends in ::= { oid }.
var macros = map[string]NodeKind{
	"OBJECT-TYPE":        KindObjectType,
	"OBJECT-IDENTITY":    KindObjectIdentity,
	"MODULE-IDENTITY":    KindModuleIdentity,
	"NOTIFICATION-TYPE":  KindNotification,
	"OBJECT-GROUP":       KindGroup,
	"NOTIFICATION-GROUP": KindGroup,
	"MODULE-COMPLIANCE":  KindCompliance,
	"AGENT-CAPABILITIES": KindCapabilities,
}

func (p *parser) parseAssignment(m *Module) {
	name := p.next()
	if name.kind != tIdent {
		p.note(name.line, "syntax", "unexpected %q at the start of an assignment", name.text)
		return
	}
	switch {
	case p.accept("MACRO"):
		// A macro definition lives in the IETF SMI modules and is
		// grammar for humans; its body ends at the first END.
		for !p.eof() && !p.accept("END") {
			p.next()
		}
	case p.accept("::="):
		p.parseTypeAssignment(m, name)
	case p.is("OBJECT") && p.peekN(1).text == "IDENTIFIER":
		p.next()
		p.next()
		n := &Node{Name: name.text, Kind: KindObjectIdentifier, Line: name.line}
		if !p.accept("::=") {
			p.note(name.line, "syntax", "%s OBJECT IDENTIFIER without a value", name.text)
			return
		}
		n.Value = p.parseOIDValue(name)
		p.add(m, n)
	case p.is("TRAP-TYPE"):
		p.next()
		p.parseTrap(m, name)
	default:
		kind, ok := macros[p.peek().text]
		if !ok {
			p.skipValueAssignment(name)
			return
		}
		p.next()
		n := &Node{Name: name.text, Kind: kind, Line: name.line}
		p.clauses(m, n)
		if !p.accept("::=") {
			p.note(name.line, "syntax", "%s has no ::= value", name.text)
			return
		}
		n.Value = p.parseOIDValue(name)
		p.add(m, n)
	}
}

// skipValueAssignment passes over an assignment this compiler does not
// model — `foo INTEGER ::= 5` — without losing the next one.
func (p *parser) skipValueAssignment(name token) {
	for !p.eof() && !p.is("::=") && !p.is("END") {
		p.next()
	}
	if !p.accept("::=") {
		return
	}
	if p.is("{") {
		p.skipBalanced()
		return
	}
	p.next()
}

// clauses reads a macro's clauses up to its ::=.
func (p *parser) clauses(m *Module, n *Node) {
	for !p.eof() && !p.is("::=") {
		t := p.next()
		if t.kind == tString {
			continue
		}
		switch t.text {
		case "SYNTAX", "WRITE-SYNTAX":
			s := p.parseSyntax()
			if t.text == "SYNTAX" && n.Syntax == nil {
				n.Syntax = s
			}
		case "ACCESS", "MAX-ACCESS":
			if n.Access == "" {
				n.Access = p.next().text
			}
		case "STATUS":
			n.Status = p.next().text
		case "UNITS":
			n.Units = p.next().text
		case "LAST-UPDATED":
			if lu := p.next(); m.LastUpdated == "" && lu.kind == tString {
				m.LastUpdated = lu.text
			}
		case "INDEX":
			n.Index = p.parseNameList()
		case "AUGMENTS":
			if l := p.parseNameList(); len(l) > 0 {
				n.Augments = l[0]
			}
		case "ENTERPRISE":
			n.Enterprise = p.next().text
		case "{":
			// DEFVAL, OBJECTS, NOTIFICATIONS, MANDATORY-GROUPS, ... — the
			// operand of a clause this compiler does not model.
			p.pos--
			p.skipBalanced()
		}
	}
}

// parseSyntax reads a type: a primitive, an application or TC name, a
// SEQUENCE, with an optional enumeration or constraint.
func (p *parser) parseSyntax() *Syntax {
	s := &Syntax{}
	if p.is("[") {
		for !p.eof() && !p.accept("]") {
			p.next()
		}
	}
	p.accept("IMPLICIT")
	p.accept("EXPLICIT")
	t := p.next()
	switch t.text {
	case "SEQUENCE":
		if p.accept("OF") {
			s.Base = "SEQUENCE OF"
			s.Of = p.next().text
			return s
		}
		s.Base = "SEQUENCE"
		p.skipBalanced()
		return s
	case "CHOICE":
		s.Base = "CHOICE"
		p.skipBalanced()
		return s
	case "OCTET":
		p.accept("STRING")
		s.Base = "OCTET STRING"
	case "OBJECT":
		p.accept("IDENTIFIER")
		s.Base = "OBJECT IDENTIFIER"
	case "BIT":
		p.accept("STRING")
		s.Base = "BIT STRING"
	default:
		s.Base = t.text
	}
	if p.is("{") {
		s.Enums = p.parseEnums()
	}
	if p.is("(") {
		s.Constraint = p.captureBalanced()
	}
	return s
}

// clauseWords end an enumeration that never closed. The RX8200's S15142
// MIB writes a member as ---(0), and "--" opens a comment that eats the
// rest of the line, closing brace included. Without a stop the list would
// swallow the following clauses up to the next ::= and lose the object;
// with it, one member is lost and the object keeps its OID and access.
var clauseWords = map[string]bool{
	"MAX-ACCESS": true, "ACCESS": true, "STATUS": true, "DESCRIPTION": true,
	"UNITS": true, "DEFVAL": true, "INDEX": true, "::=": true,
}

// parseEnums reads `{ name(1), name(2) }`. A member that is not that shape
// is skipped to the next comma rather than ending the list.
func (p *parser) parseEnums() []Enum {
	var out []Enum
	open := p.next() // {
	for !p.eof() {
		if p.accept("}") {
			return out
		}
		if t := p.peek(); t.kind != tString && clauseWords[t.text] {
			p.note(open.line, "syntax", "enumeration is not closed before %s", t.text)
			return out
		}
		if p.accept(",") {
			continue
		}
		name := p.next()
		// A member named by a number — 188(188), for a 188-byte packet —
		// is not an ASN.1 identifier, but it is unambiguous, so it is
		// kept rather than dropped.
		if (name.kind == tIdent || name.kind == tNumber) && p.accept("(") {
			v := p.next()
			p.accept(")")
			if n, err := strconv.ParseInt(v.text, 10, 64); err == nil {
				out = append(out, Enum{name.text, n})
				continue
			}
			p.note(v.line, "syntax", "enumeration member %s has value %q", name.text, v.text)
			continue
		}
		p.note(name.line, "syntax", "unexpected %q in an enumeration", name.text)
	}
	return out
}

// parseTypeAssignment reads what follows `Name ::=`.
func (p *parser) parseTypeAssignment(m *Module, name token) {
	if p.accept("TEXTUAL-CONVENTION") {
		ty := &Type{Name: name.text, IsTC: true}
		for !p.eof() && !p.is("SYNTAX") {
			if t := p.next(); t.text == "DISPLAY-HINT" && t.kind == tIdent {
				ty.DisplayHint = p.next().text
			}
		}
		p.accept("SYNTAX")
		ty.Syntax = p.parseSyntax()
		m.Types[name.text] = ty
		return
	}
	m.Types[name.text] = &Type{Name: name.text, Syntax: p.parseSyntax()}
}

// parseTrap reads a TRAP-TYPE, whose value is a number rather than an OID.
func (p *parser) parseTrap(m *Module, name token) {
	n := &Node{Name: name.text, Kind: KindTrap, Line: name.line}
	p.clauses(m, n)
	if !p.accept("::=") {
		p.note(name.line, "syntax", "%s has no ::= value", name.text)
		return
	}
	v := p.next()
	num, err := strconv.ParseUint(v.text, 10, 32)
	if err != nil {
		p.note(v.line, "syntax", "trap %s has value %q", name.text, v.text)
		return
	}
	n.TrapNumber = uint32(num)
	p.add(m, n)
}

// parseOIDValue reads `{ parent 5 }`, `{ iso(1) org(3) 6 }` or `{ 1 3 6 }`.
func (p *parser) parseOIDValue(owner token) []OIDComponent {
	if !p.accept("{") {
		p.note(owner.line, "syntax", "%s: an OID value must be in braces", owner.text)
		return nil
	}
	var out []OIDComponent
	for !p.eof() && !p.accept("}") {
		t := p.next()
		switch t.kind {
		case tNumber:
			if n, err := strconv.ParseUint(t.text, 10, 32); err == nil {
				out = append(out, OIDComponent{Num: uint32(n), HasNum: true})
				continue
			}
			p.note(t.line, "syntax", "%s: arc %q is not a 32-bit number", owner.text, t.text)
		case tIdent:
			c := OIDComponent{Name: t.text}
			if p.accept("(") {
				if n, err := strconv.ParseUint(p.next().text, 10, 32); err == nil {
					c.Num, c.HasNum = uint32(n), true
				}
				p.accept(")")
			}
			out = append(out, c)
		default:
			p.note(t.line, "syntax", "%s: unexpected %q in an OID value", owner.text, t.text)
		}
	}
	return out
}

// parseNameList reads `{ [IMPLIED] name, name }`.
func (p *parser) parseNameList() []string {
	var out []string
	if !p.accept("{") {
		return nil
	}
	for !p.eof() && !p.accept("}") {
		t := p.next()
		if t.kind == tIdent && t.text != "IMPLIED" {
			out = append(out, t.text)
		}
	}
	return out
}

// skipBalanced passes over one {...} block, nested braces included.
func (p *parser) skipBalanced() {
	if !p.accept("{") {
		return
	}
	depth := 1
	for !p.eof() && depth > 0 {
		switch t := p.next(); {
		case t.kind == tString:
		case t.text == "{":
			depth++
		case t.text == "}":
			depth--
		}
	}
}

// captureBalanced returns one (...) group as written, spaced token by
// token — it is kept for display, not re-parsed.
func (p *parser) captureBalanced() string {
	var b []byte
	depth := 0
	for !p.eof() {
		t := p.next()
		if t.kind != tString {
			switch t.text {
			case "(":
				depth++
			case ")":
				depth--
			}
		}
		if needsSpace(b, t.text) {
			b = append(b, ' ')
		}
		b = append(b, t.text...)
		if depth == 0 {
			break
		}
	}
	return string(b)
}

// needsSpace keeps a captured constraint readable: "(SIZE (0..32))"
// rather than "( SIZE ( 0 .. 32 ) )".
func needsSpace(b []byte, next string) bool {
	if len(b) == 0 || next == ")" || next == ".." {
		return false
	}
	return b[len(b)-1] != '(' && !bytes.HasSuffix(b, []byte(".."))
}
