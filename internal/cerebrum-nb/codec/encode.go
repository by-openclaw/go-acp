package codec

import (
	"strconv"
	"strings"
)

// Attr is a single XML attribute. Both Key and Value are emitted
// verbatim (Value is escaped for the XML attribute syntax).
type Attr struct {
	Key   string
	Value string
}

// AttrsBuilder accumulates Attrs in declared order. Used to keep
// attribute order deterministic on the wire (matters for byte-exact
// tests + Wireshark Info-column diffability).
type AttrsBuilder []Attr

// Add appends key=value if value is non-empty. Use ForceAdd for
// attributes that must appear even when empty (rare in this spec).
func (a AttrsBuilder) Add(key, value string) AttrsBuilder {
	if value == "" {
		return a
	}
	return append(a, Attr{key, value})
}

func (a AttrsBuilder) ForceAdd(key, value string) AttrsBuilder {
	return append(a, Attr{key, value})
}

// emitElement writes <name k1="v1" k2="v2"> ... </name>. If the body
// closure is nil AND no text/children are produced, emits the
// self-closing form <name k1="v1"/>.
func emitElement(b *strings.Builder, name string, attrs []Attr, body func()) {
	b.WriteByte('<')
	b.WriteString(name)
	for _, a := range attrs {
		b.WriteByte(' ')
		b.WriteString(a.Key)
		b.WriteString(`="`)
		xmlEscapeAttr(b, a.Value)
		b.WriteByte('"')
	}
	if body == nil {
		b.WriteString("/>")
		return
	}
	b.WriteByte('>')
	body()
	b.WriteString("</")
	b.WriteString(name)
	b.WriteByte('>')
}

func formatMTID(mtid uint32) string {
	return strconv.FormatUint(uint64(mtid), 10)
}

// Wire-actual canonical case is UPPERCASE for both element names AND
// attribute names — verified 2026-04-26 against a live Cerebrum
// (lowercase TX is rejected with MTID_ERROR because the server
// doesn't recognise lowercase keys). Spec §2/§4.2-4.4/§5 examples
// show lowercase but real servers don't accept it.

// ----------------------------------------------------------------------
// §2 — top-level commands
// ----------------------------------------------------------------------

// EncodeLogin builds <LOGIN USERNAME="…" PASSWORD="…" MTID="…"/>.
func EncodeLogin(mtid uint32, username, password string) []byte {
	var b strings.Builder
	emitElement(&b, "LOGIN", []Attr{
		{"USERNAME", username},
		{"PASSWORD", password},
		{"MTID", formatMTID(mtid)},
	}, nil)
	return []byte(b.String())
}

// EncodePoll builds <POLL MTID="…"/>.
func EncodePoll(mtid uint32) []byte {
	var b strings.Builder
	emitElement(&b, "POLL", []Attr{{"MTID", formatMTID(mtid)}}, nil)
	return []byte(b.String())
}

// EncodeUnsubscribeAll builds <UNSUBSCRIBE_ALL MTID="…"/>.
func EncodeUnsubscribeAll(mtid uint32) []byte {
	var b strings.Builder
	emitElement(&b, "UNSUBSCRIBE_ALL", []Attr{{"MTID", formatMTID(mtid)}}, nil)
	return []byte(b.String())
}

// ActionBody is one §4 action body. Implementations come from
// actions.go (Routing, Category, Salvo, Device).
type ActionBody interface {
	encodeAction(b *strings.Builder)
}

// EncodeAction wraps body in <ACTION MTID="…">…</ACTION>.
func EncodeAction(mtid uint32, body ActionBody) []byte {
	var b strings.Builder
	emitElement(&b, "ACTION", []Attr{{"MTID", formatMTID(mtid)}}, func() {
		body.encodeAction(&b)
	})
	return []byte(b.String())
}

// SubItem is one §5 subscribe / obtain / unsubscribe child.
// Implementations come from events.go (RoutingChange, CategoryChange, ...).
// Same shape on TX in all three verbs (§5 preamble: "TX/RX symmetry").
type SubItem interface {
	encodeSubItem(b *strings.Builder)
}

// EncodeSubscribe wraps items in <SUBSCRIBE MTID="…">…</SUBSCRIBE>.
func EncodeSubscribe(mtid uint32, items []SubItem) []byte {
	return encodeSubVerb("SUBSCRIBE", mtid, items)
}

// EncodeObtain wraps items in <OBTAIN MTID="…">…</OBTAIN>.
func EncodeObtain(mtid uint32, items []SubItem) []byte {
	return encodeSubVerb("OBTAIN", mtid, items)
}

// EncodeUnsubscribe wraps items in <UNSUBSCRIBE MTID="…">…</UNSUBSCRIBE>.
// Per spec §2 the children must match a prior subscribe.
func EncodeUnsubscribe(mtid uint32, items []SubItem) []byte {
	return encodeSubVerb("UNSUBSCRIBE", mtid, items)
}

func encodeSubVerb(verb string, mtid uint32, items []SubItem) []byte {
	var b strings.Builder
	emitElement(&b, verb, []Attr{{"MTID", formatMTID(mtid)}}, func() {
		for _, it := range items {
			it.encodeSubItem(&b)
		}
	})
	return []byte(b.String())
}
