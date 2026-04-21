package codec

import "fmt"

// SalvoGoDoneStatus enumerates the tx 123 first-byte values.
type SalvoGoDoneStatus uint8

const (
	SalvoDoneSet     SalvoGoDoneStatus = 0x00 // Crosspoints set
	SalvoDoneCleared SalvoGoDoneStatus = 0x01 // Stored crosspoints cleared
	SalvoDoneNone    SalvoGoDoneStatus = 0x02 // No crosspoints to set / clear
)

// SalvoGoDoneAckParams: tx 123 CROSSPOINT GO DONE GROUP SALVO ACKNOWLEDGE.
//
// Reference: SW-P-08 §3.3.25.
type SalvoGoDoneAckParams struct {
	Status  SalvoGoDoneStatus
	SalvoID uint8
}

// EncodeSalvoGoDoneAck packs tx 123.
//
// | Byte | Field     | Notes                                    |
// |------|-----------|------------------------------------------|
// |  1   | Status    | 0=set, 1=cleared, 2=none                  |
// |  2   | Salvo num | 0-127                                     |
//
// Spec: SW-P-08 §3.3.25.
func EncodeSalvoGoDoneAck(p SalvoGoDoneAckParams) Frame {
	return Frame{
		ID:      TxSalvoGoDoneAck,
		Payload: []byte{byte(p.Status), p.SalvoID & 0x7F},
	}
}

// DecodeSalvoGoDoneAck parses tx 123.
func DecodeSalvoGoDoneAck(f Frame) (SalvoGoDoneAckParams, error) {
	if f.ID != TxSalvoGoDoneAck {
		return SalvoGoDoneAckParams{}, ErrWrongCommand
	}
	if len(f.Payload) < 2 {
		return SalvoGoDoneAckParams{}, ErrShortPayload
	}
	st := SalvoGoDoneStatus(f.Payload[0])
	if st != SalvoDoneSet && st != SalvoDoneCleared && st != SalvoDoneNone {
		return SalvoGoDoneAckParams{}, fmt.Errorf("probel: tx 123 unknown status %#x", byte(st))
	}
	return SalvoGoDoneAckParams{Status: st, SalvoID: f.Payload[1] & 0x7F}, nil
}
