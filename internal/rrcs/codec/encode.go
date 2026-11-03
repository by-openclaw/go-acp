package codec

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"strconv"
)

// xmlHeader is the declaration every example in the specification opens
// with (§5.6.1).
const xmlHeader = `<?xml version="1.0"?>`

// writeValue appends one <value> element. Integers go out as <int>, the
// tag the request tables of §8 and the examples of §11 use; <i4> is its
// synonym (§5.6.2) and is accepted on decode.
func writeValue(b *bytes.Buffer, v Value) error {
	b.WriteString("<value>")
	switch v.Kind {
	case KindString:
		b.WriteString("<string>")
		writeText(b, v.Str)
		b.WriteString("</string>")
	case KindInt:
		b.WriteString("<int>")
		b.WriteString(strconv.FormatInt(int64(v.Int), 10))
		b.WriteString("</int>")
	case KindBool:
		if v.Bool {
			b.WriteString("<boolean>1</boolean>")
		} else {
			b.WriteString("<boolean>0</boolean>")
		}
	case KindDouble:
		b.WriteString("<double>")
		b.WriteString(strconv.FormatFloat(v.Double, 'f', -1, 64))
		b.WriteString("</double>")
	case KindDateTime:
		b.WriteString("<dateTime.iso8601>")
		writeText(b, v.Str)
		b.WriteString("</dateTime.iso8601>")
	case KindBase64:
		b.WriteString("<base64>")
		b.WriteString(base64.StdEncoding.EncodeToString(v.Bytes))
		b.WriteString("</base64>")
	case KindStruct:
		b.WriteString("<struct>")
		for _, m := range v.Members {
			b.WriteString("<member><name>")
			writeText(b, m.Name)
			b.WriteString("</name>")
			if err := writeValue(b, m.Value); err != nil {
				return err
			}
			b.WriteString("</member>")
		}
		b.WriteString("</struct>")
	case KindArray:
		b.WriteString("<array><data>")
		for _, it := range v.Items {
			if err := writeValue(b, it); err != nil {
				return err
			}
		}
		b.WriteString("</data></array>")
	default:
		return fmt.Errorf("%w: cannot encode %s", ErrType, v.Kind)
	}
	b.WriteString("</value>")
	return nil
}

// writeText appends character data with the XML escapes applied. A
// bytes.Buffer never fails a write, so the error is not one that can
// occur.
func writeText(b *bytes.Buffer, s string) {
	_ = xml.EscapeText(b, []byte(s))
}
