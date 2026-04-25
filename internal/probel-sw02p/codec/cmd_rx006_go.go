package codec

// GoOperation enumerates the two rx 06 GO + tx 13 GO DONE operations.
// Wire encoding per §3.2.8 / §3.2.15: a single byte, 00 or 01.
type GoOperation uint8

const (
	// GoOpSet commits every previously received CONNECT ON GO slot
	// into live crosspoints. §3.2.8 byte 1 = 00.
	GoOpSet GoOperation = 0x00

	// GoOpClear drops every previously received CONNECT ON GO slot
	// without routing them. §3.2.8 byte 1 = 01.
	GoOpClear GoOperation = 0x01
)

// GoParams carries rx 06 GO fields. See §3.2.8.
//
//	| Byte | Field     | Notes                                |
//	|------|-----------|--------------------------------------|
//	|  1   | Operation | 00 = set pending, 01 = clear pending |
type GoParams struct {
	Operation GoOperation
}

// PayloadLenGo is the fixed MESSAGE byte count for rx 06.
const PayloadLenGo = 1

// EncodeGo builds rx 06 wire bytes.
func EncodeGo(p GoParams) Frame {
	return Frame{
		ID:      RxGo,
		Payload: []byte{byte(p.Operation)},
	}
}

// DecodeGo parses rx 06. Unknown operation bytes (anything other than
// 0x00 / 0x01) are surfaced verbatim in GoOperation — callers that
// care should check against GoOpSet / GoOpClear.
func DecodeGo(f Frame) (GoParams, error) {
	if f.ID != RxGo {
		return GoParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenGo {
		return GoParams{}, ErrShortPayload
	}
	return GoParams{Operation: GoOperation(f.Payload[0])}, nil
}
