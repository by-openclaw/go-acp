package codec

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// maxDepth bounds element nesting. The deepest shape in the
// specification is a member-change list — array, struct, struct — at
// well under twenty levels. The bound exists because the notification
// listener parses documents it did not write.
const maxDepth = 64

// node is one XML element: its name, the character data directly inside
// it, and its child elements in document order. Attributes, comments and
// processing instructions carry nothing in XML-RPC and are dropped.
type node struct {
	name     string
	text     string
	children []*node
}

// child returns the first child element with the given name, or nil.
func (n *node) child(name string) *node {
	for _, c := range n.children {
		if c.name == name {
			return c
		}
	}
	return nil
}

// parseXML reads one XML document into a node tree.
func parseXML(data []byte) (*node, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.CharsetReader = charsetReader

	var root *node
	var stack []*node
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if len(stack) >= maxDepth {
				return nil, fmt.Errorf("%w: nested deeper than %d elements", ErrMalformed, maxDepth)
			}
			n := &node{name: t.Name.Local}
			switch {
			case len(stack) > 0:
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, n)
			case root != nil:
				return nil, fmt.Errorf("%w: more than one root element", ErrMalformed)
			default:
				root = n
			}
			stack = append(stack, n)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text += string(t)
			}
		}
	}
	if root == nil {
		return nil, fmt.Errorf("%w: no root element", ErrMalformed)
	}
	return root, nil
}

// charsetReader accepts the encodings a Windows program plausibly
// declares. The specification's examples declare none, which XML reads
// as UTF-8; ISO-8859-1 is converted because a single byte above 0x7F
// would otherwise make the whole document unreadable.
func charsetReader(label string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "utf-8", "utf8", "us-ascii", "ascii":
		return input, nil
	case "iso-8859-1", "latin1", "latin-1", "windows-1252":
		return &latin1Reader{r: input}, nil
	}
	return nil, fmt.Errorf("unsupported encoding %q", label)
}

// latin1Reader turns ISO-8859-1 bytes into UTF-8. Every byte is one
// code point, so a byte above 0x7F becomes two bytes of output; the
// second is held until the caller has room for it.
type latin1Reader struct {
	r       io.Reader
	pending []byte
}

func (l *latin1Reader) Read(p []byte) (int, error) {
	n := 0
	for n < len(p) && len(l.pending) > 0 {
		p[n] = l.pending[0]
		l.pending = l.pending[1:]
		n++
	}
	if n == len(p) {
		return n, nil
	}
	var in [1]byte
	for n < len(p) {
		if _, err := io.ReadFull(l.r, in[:]); err != nil {
			if n > 0 {
				return n, nil
			}
			return 0, io.EOF
		}
		b := in[0]
		if b < 0x80 {
			p[n] = b
			n++
			continue
		}
		p[n] = 0xC0 | b>>6
		n++
		second := 0x80 | b&0x3F
		if n < len(p) {
			p[n] = second
			n++
		} else {
			l.pending = append(l.pending, second)
		}
	}
	return n, nil
}
