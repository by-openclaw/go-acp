package codec

import (
	"bytes"
	"fmt"
)

// Fault is the XML-RPC failure answer: a code and a text (§5.6.5). It is
// distinct from an RRCS error code, which travels inside a normal answer.
// Several methods are documented as returning a fault instead of an
// answer when they fail (§8.9, §8.10, §8.11).
type Fault struct {
	Code   int32
	String string
}

func (f *Fault) Error() string {
	return fmt.Sprintf("rrcs: XML-RPC fault %d: %s", f.Code, f.String)
}

// Response is one XML-RPC answer: a value, or a fault, never both
// (§5.6.5).
type Response struct {
	Value Value
	Fault *Fault
}

// EncodeResponse renders an answer carrying one value, in the standard
// XML-RPC form.
func EncodeResponse(v Value) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(xmlHeader)
	b.WriteString("<methodResponse><params><param>")
	if err := writeValue(&b, v); err != nil {
		return nil, err
	}
	b.WriteString("</param></params></methodResponse>")
	return b.Bytes(), nil
}

// EncodeFault renders a fault answer.
func EncodeFault(code int32, text string) []byte {
	var b bytes.Buffer
	b.WriteString(xmlHeader)
	b.WriteString("<methodResponse><fault>")
	// The two members are a struct of an int and a string, which cannot
	// fail to encode.
	_ = writeValue(&b, Struct(
		Member{Name: "faultCode", Value: Int(code)},
		Member{Name: "faultString", Value: String(text)},
	))
	b.WriteString("</fault></methodResponse>")
	return b.Bytes()
}

// DecodeResponse parses an answer.
func DecodeResponse(data []byte) (Response, error) {
	root, err := parseXML(data)
	if err != nil {
		return Response{}, err
	}
	if root.name != "methodResponse" {
		return Response{}, fmt.Errorf("%w: root is <%s>, want <methodResponse>", ErrMalformed, root.name)
	}
	fault, params := root.child("fault"), root.child("params")
	if len(root.children) != 1 {
		// "A <methodResponse> can not contain both a <fault> and a
		// <params>" (§5.6.5), and it holds a single value.
		return Response{}, fmt.Errorf("%w: <methodResponse> holds %d elements, want one", ErrMalformed, len(root.children))
	}

	switch {
	case fault != nil:
		f, err := parseFault(fault)
		if err != nil {
			return Response{}, err
		}
		return Response{Fault: f}, nil
	case params != nil:
		if len(params.children) != 1 || params.children[0].name != "param" {
			return Response{}, fmt.Errorf("%w: <params> of an answer holds one <param>", ErrMalformed)
		}
		v, err := paramValue(params.children[0])
		if err != nil {
			return Response{}, err
		}
		return Response{Value: v}, nil
	}
	return Response{}, fmt.Errorf("%w: <%s> inside <methodResponse>", ErrMalformed, root.children[0].name)
}

// parseFault reads the struct of a <fault>: faultCode and faultString.
func parseFault(n *node) (*Fault, error) {
	val := n.child("value")
	if val == nil || len(n.children) != 1 {
		return nil, fmt.Errorf("%w: <fault> without a single <value>", ErrMalformed)
	}
	v, err := parseValue(val)
	if err != nil {
		return nil, err
	}
	code, okCode := v.Field("faultCode")
	text, okText := v.Field("faultString")
	if !okCode || !okText || code.Kind != KindInt || text.Kind != KindString {
		return nil, fmt.Errorf("%w: <fault> needs an int faultCode and a string faultString", ErrMalformed)
	}
	return &Fault{Code: code.Int, String: text.Str}, nil
}
