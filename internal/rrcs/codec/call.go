package codec

import (
	"bytes"
	"fmt"
	"strings"
)

// Call is one XML-RPC request: a method name and its parameters (§5.6.1).
// It is what we send to RRCS, and what RRCS sends us as a notification.
type Call struct {
	Method string
	Params []Value
}

// EncodeCall renders a request in the standard XML-RPC form.
func EncodeCall(method string, params ...Value) ([]byte, error) {
	if !validMethodName(method) {
		return nil, fmt.Errorf("%w: method name %q", ErrMalformed, method)
	}
	var b bytes.Buffer
	b.WriteString(xmlHeader)
	b.WriteString("<methodCall><methodName>")
	b.WriteString(method)
	b.WriteString("</methodName>")
	// "If the procedure call has parameters, the <methodCall> must
	// contain a <params> sub-item" (§5.6.1).
	if len(params) > 0 {
		b.WriteString("<params>")
		for _, p := range params {
			b.WriteString("<param>")
			if err := writeValue(&b, p); err != nil {
				return nil, err
			}
			b.WriteString("</param>")
		}
		b.WriteString("</params>")
	}
	b.WriteString("</methodCall>")
	return b.Bytes(), nil
}

// DecodeCall parses a request.
func DecodeCall(data []byte) (Call, error) {
	root, err := parseXML(data)
	if err != nil {
		return Call{}, err
	}
	if root.name != "methodCall" {
		return Call{}, fmt.Errorf("%w: root is <%s>, want <methodCall>", ErrMalformed, root.name)
	}
	name := root.child("methodName")
	if name == nil {
		return Call{}, fmt.Errorf("%w: <methodCall> without <methodName>", ErrMalformed)
	}
	call := Call{Method: strings.TrimSpace(name.text)}
	if !validMethodName(call.Method) {
		return Call{}, fmt.Errorf("%w: method name %q", ErrMalformed, call.Method)
	}

	for _, c := range root.children {
		switch c.name {
		case "methodName":
		case "params":
			for _, p := range c.children {
				if p.name != "param" {
					return Call{}, fmt.Errorf("%w: <%s> inside <params>", ErrMalformed, p.name)
				}
				v, err := paramValue(p)
				if err != nil {
					return Call{}, err
				}
				call.Params = append(call.Params, v)
			}
		default:
			return Call{}, fmt.Errorf("%w: <%s> inside <methodCall>", ErrMalformed, c.name)
		}
	}
	return call, nil
}

// validMethodName applies §5.6.1: "identifier characters, upper and
// lower-case A-Z, the numeric characters, 0-9, underscore, dot, colon
// and slash".
func validMethodName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '_', c == '.', c == ':', c == '/':
		default:
			return false
		}
	}
	return true
}
