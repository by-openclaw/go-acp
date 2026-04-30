package codec

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Element is a case-folded XML AST node. All Name + Attrs keys are
// lowercased on parse so callers don't need to spell-check against
// spec §4.1 (UPPERCASE) vs §4.2-4.4 / §5 (lowercase) vs the
// wire-actual UPPERCASE form. Attribute *values* preserve their
// original case — only the keys are normalised.
type Element struct {
	Name     string
	Attrs    map[string]string
	Children []*Element
	Text     string

	// CaseChanged is true if the parser observed a non-lowercase
	// element or attribute name and rewrote it. The consumer surfaces
	// this as the cerebrum_case_normalized compliance event.
	CaseChanged bool
}

// Attr returns the value of the named attribute, or "" if absent. Lookup
// is case-insensitive on the key.
func (e *Element) Attr(key string) string {
	if e == nil {
		return ""
	}
	if v, ok := e.Attrs[strings.ToLower(key)]; ok {
		return v
	}
	return ""
}

// Child returns the first child whose Name matches (case-insensitive),
// or nil.
func (e *Element) Child(name string) *Element {
	if e == nil {
		return nil
	}
	want := strings.ToLower(name)
	for _, c := range e.Children {
		if c.Name == want {
			return c
		}
	}
	return nil
}

// ChildrenNamed returns all children whose Name matches
// (case-insensitive).
func (e *Element) ChildrenNamed(name string) []*Element {
	if e == nil {
		return nil
	}
	want := strings.ToLower(name)
	var out []*Element
	for _, c := range e.Children {
		if c.Name == want {
			out = append(out, c)
		}
	}
	return out
}

// ParseElement decodes one XML document into a case-folded Element AST.
// Any leading / trailing whitespace + <?xml ?> declarations are ignored.
func ParseElement(data []byte) (*Element, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false        // tolerate trailing whitespace
	dec.AutoClose = nil       // no implicit close behaviour
	dec.CharsetReader = nil   // UTF-8 only per spec
	return parseRoot(dec)
}

func parseRoot(dec *xml.Decoder) (*Element, error) {
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, errors.New("cerebrum-nb: empty XML")
			}
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			return parseElement(dec, t, false)
		case xml.ProcInst, xml.Directive, xml.Comment, xml.CharData:
			// skip
		default:
			// keep going
		}
	}
}

func parseElement(dec *xml.Decoder, start xml.StartElement, parentCaseChanged bool) (*Element, error) {
	rawName := start.Name.Local
	lower := strings.ToLower(rawName)
	upper := strings.ToUpper(rawName)
	// CaseChanged fires when the wire form is NOT the canonical
	// UPPERCASE — flags lowercase or mixed-case servers (rare /
	// defensive against future spec-strict implementations).
	caseChanged := parentCaseChanged || rawName != upper

	e := &Element{
		Name:  lower,
		Attrs: make(map[string]string, len(start.Attr)),
	}
	for _, a := range start.Attr {
		ak := strings.ToLower(a.Name.Local)
		if a.Name.Local != strings.ToUpper(a.Name.Local) {
			caseChanged = true
		}
		e.Attrs[ak] = a.Value
	}

	var textBuf strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("cerebrum-nb: parse <%s>: %w", lower, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			child, err := parseElement(dec, t, caseChanged)
			if err != nil {
				return nil, err
			}
			if child.CaseChanged {
				caseChanged = true
			}
			e.Children = append(e.Children, child)
		case xml.EndElement:
			e.Text = strings.TrimSpace(textBuf.String())
			e.CaseChanged = caseChanged
			return e, nil
		case xml.CharData:
			textBuf.Write(t)
		case xml.Comment, xml.ProcInst, xml.Directive:
			// skip
		}
	}
}

// String renders e back to XML for debugging. Not the canonical TX
// path — TX builders in encode.go produce the wire form. Lossy: drops
// CaseChanged.
func (e *Element) String() string {
	if e == nil {
		return ""
	}
	var b strings.Builder
	writeElement(&b, e)
	return b.String()
}

func writeElement(b *strings.Builder, e *Element) {
	b.WriteByte('<')
	b.WriteString(e.Name)
	for k, v := range e.Attrs {
		b.WriteByte(' ')
		b.WriteString(k)
		b.WriteString(`="`)
		xmlEscapeAttr(b, v)
		b.WriteByte('"')
	}
	if len(e.Children) == 0 && e.Text == "" {
		b.WriteString("/>")
		return
	}
	b.WriteByte('>')
	for _, c := range e.Children {
		writeElement(b, c)
	}
	if e.Text != "" {
		xmlEscapeText(b, e.Text)
	}
	b.WriteString("</")
	b.WriteString(e.Name)
	b.WriteByte('>')
}

func xmlEscapeAttr(b *strings.Builder, s string) {
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		case '\n':
			b.WriteString("&#10;")
		case '\r':
			b.WriteString("&#13;")
		case '\t':
			b.WriteString("&#9;")
		default:
			b.WriteRune(r)
		}
	}
}

func xmlEscapeText(b *strings.Builder, s string) {
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		default:
			b.WriteRune(r)
		}
	}
}
