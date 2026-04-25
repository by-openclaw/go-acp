package codec

// GoDoneAckParams carries tx 13 GO DONE ACKNOWLEDGE fields. Matrices
// emit this on all ports after executing a rx 06 GO command. See
// §3.2.15.
//
//	| Byte | Field     | Notes                                       |
//	|------|-----------|---------------------------------------------|
//	|  1   | Operation | 00 = Crosspoints set, 01 = Stored cleared   |
//
// The Operation field echoes the GoOperation from the GO request so a
// listening controller can match the ack to its triggering command.
type GoDoneAckParams struct {
	Operation GoOperation
}

// PayloadLenGoDoneAck is the fixed MESSAGE byte count for tx 13.
const PayloadLenGoDoneAck = 1

// EncodeGoDoneAck builds tx 13 wire bytes.
func EncodeGoDoneAck(p GoDoneAckParams) Frame {
	return Frame{
		ID:      TxGoDoneAck,
		Payload: []byte{byte(p.Operation)},
	}
}

// DecodeGoDoneAck parses tx 13.
func DecodeGoDoneAck(f Frame) (GoDoneAckParams, error) {
	if f.ID != TxGoDoneAck {
		return GoDoneAckParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenGoDoneAck {
		return GoDoneAckParams{}, ErrShortPayload
	}
	return GoDoneAckParams{Operation: GoOperation(f.Payload[0])}, nil
}
