package codec

import "fmt"

// SalvoOp enumerates the rx 121 first-byte values.
type SalvoOp uint8

const (
	SalvoOpSet   SalvoOp = 0x00 // Apply previously received routes
	SalvoOpClear SalvoOp = 0x01 // Clear the stored set without applying
)

// SalvoGoParams: rx 121 CROSSPOINT GO GROUP SALVO — triggers the
// receiving device to set (op=0) or clear (op=1) the previously
// received CROSSPOINT CONNECT ON GO GROUP SALVO messages for a salvo
// group. The matrix replies tx 123 GO DONE.
//
// Reference: SW-P-08 §3.2.30.
type SalvoGoParams struct {
	Op      SalvoOp
	SalvoID uint8 // 0-127
}

// EncodeSalvoGo packs rx 121.
//
// | Byte | Field     | Notes                                     |
// |------|-----------|-------------------------------------------|
// |  1   | Op        | 0x00 = set, 0x01 = clear                  |
// |  2   | Salvo num | 0-127                                     |
//
// Spec: SW-P-08 §3.2.30.
func EncodeSalvoGo(p SalvoGoParams) Frame {
	return Frame{
		ID:      RxCrosspointGoSalvo,
		Payload: []byte{byte(p.Op), p.SalvoID & 0x7F},
	}
}

// DecodeSalvoGo parses rx 121.
func DecodeSalvoGo(f Frame) (SalvoGoParams, error) {
	if f.ID != RxCrosspointGoSalvo {
		return SalvoGoParams{}, ErrWrongCommand
	}
	if len(f.Payload) < 2 {
		return SalvoGoParams{}, ErrShortPayload
	}
	op := SalvoOp(f.Payload[0])
	if op != SalvoOpSet && op != SalvoOpClear {
		return SalvoGoParams{}, fmt.Errorf("probel: rx 121 unknown op %#x", byte(op))
	}
	return SalvoGoParams{Op: op, SalvoID: f.Payload[1] & 0x7F}, nil
}
