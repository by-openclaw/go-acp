package codec

// GoGroupResult enumerates the possible tx 38 statuses per §3.2.39.
// Adds a third value over tx 13 (GoDoneAck) — "no crosspoints to set
// / clear" — to distinguish "set an empty group" from "set a real
// group" on the controller side.
type GoGroupResult uint8

const (
	GoGroupResultSet    GoGroupResult = 0x00 // Crosspoints set
	GoGroupResultCleared GoGroupResult = 0x01 // Stored crosspoints cleared
	GoGroupResultEmpty  GoGroupResult = 0x02 // No crosspoints to set / clear
)

// GoDoneGroupSalvoAckParams carries tx 38 GO DONE GROUP SALVO
// ACKNOWLEDGE fields. §3.2.39.
//
//	| Byte | Field   | Notes                                    |
//	|------|---------|------------------------------------------|
//	|  1   | Result  | 00 / 01 / 02 per GoGroupResult           |
//	|  2   | SalvoID | salvo group number echoed from rx 36     |
type GoDoneGroupSalvoAckParams struct {
	Result  GoGroupResult
	SalvoID uint8
}

// PayloadLenGoDoneGroupSalvoAck is the fixed MESSAGE byte count for
// tx 38.
const PayloadLenGoDoneGroupSalvoAck = 2

// EncodeGoDoneGroupSalvoAck builds tx 38 wire bytes.
func EncodeGoDoneGroupSalvoAck(p GoDoneGroupSalvoAckParams) Frame {
	return Frame{
		ID:      TxGoDoneGroupSalvoAck,
		Payload: []byte{byte(p.Result), p.SalvoID & 0x7F},
	}
}

// DecodeGoDoneGroupSalvoAck parses tx 38.
func DecodeGoDoneGroupSalvoAck(f Frame) (GoDoneGroupSalvoAckParams, error) {
	if f.ID != TxGoDoneGroupSalvoAck {
		return GoDoneGroupSalvoAckParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenGoDoneGroupSalvoAck {
		return GoDoneGroupSalvoAckParams{}, ErrShortPayload
	}
	return GoDoneGroupSalvoAckParams{
		Result:  GoGroupResult(f.Payload[0]),
		SalvoID: f.Payload[1] & 0x7F,
	}, nil
}
