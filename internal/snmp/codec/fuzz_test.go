package codec

import "testing"

// The decoder is the SNMP agent's attack surface: it runs on every
// datagram from anybody, before any community or USM check. It must never
// panic, hang or read out of bounds on hostile input — a crash there is a
// remote DoS. These fuzz targets assert exactly that. Without -fuzz they
// run the seed corpus as ordinary regression tests; `go test -fuzz` drives
// the real search.

func seedMessages() [][]byte {
	msgs := []Message{
		{Version: Version1, Community: "public", PDU: &PDU{
			Type: PDUTypeGet, RequestID: 1,
			VarBinds: []VarBind{{Name: MustParseOID("1.3.6.1.2.1.1.1.0"), Value: Null()}}}},
		{Version: Version2c, Community: "public", PDU: &PDU{
			Type: PDUTypeGetBulk, RequestID: 2, MaxRepetitions: 10,
			VarBinds: []VarBind{{Name: MustParseOID("1.3.6.1"), Value: Null()}}}},
		{Version: Version1, Community: "public", TrapV1: &TrapV1{
			Enterprise: MustParseOID("1.3.6.1.4.1.1773"), Generic: ColdStart}},
	}
	var out [][]byte
	for _, m := range msgs {
		if raw, err := Encode(m); err == nil {
			out = append(out, raw)
		}
	}
	// Plus the pathological shapes a decoder must not choke on.
	out = append(out,
		[]byte{},
		[]byte{0x30, 0x00},                   // empty SEQUENCE
		[]byte{0x30, 0x84, 0xff, 0xff, 0xff}, // a length claiming more than exists
		[]byte{0xff, 0xff, 0xff, 0xff},
	)
	return out
}

func FuzzDecode(f *testing.F) {
	for _, s := range seedMessages() {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := Decode(data)
		if err != nil {
			return // a rejected datagram is the expected outcome, not a bug
		}
		// If it decoded, re-encoding must not panic either.
		_, _ = Encode(m)
	})
}

func FuzzDecodeUSMParameters(f *testing.F) {
	valid, _ := EncodeUSMParameters(USMParameters{
		AuthoritativeEngineID: []byte{1, 2, 3, 4, 5}, UserName: "operator",
	})
	f.Add(valid)
	f.Add([]byte{})
	f.Add([]byte{0x30, 0x80})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = DecodeUSMParameters(data)
	})
}

func FuzzDecodeScopedPDU(f *testing.F) {
	valid, _ := EncodeScopedPDU(ScopedPDU{
		ContextEngineID: []byte{1, 2, 3, 4, 5},
		PDU:             &PDU{Type: PDUTypeGet, RequestID: 1},
	})
	f.Add(valid)
	f.Add([]byte{})
	f.Add([]byte{0x30, 0x03, 0x04, 0x81, 0x00})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = DecodeScopedPDU(data)
	})
}
