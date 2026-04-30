package codec

// StatusResponse2Params carries tx 09 STATUS RESPONSE - 2 fields
// emitted by a 5007/5107 TDM controller card in reply to rx 07 STATUS
// REQUEST. See §3.2.11.
//
//	| Byte | Field  | Notes                                           |
//	|------|--------|-------------------------------------------------|
//	|  1   | Status | bit 7  : 0                                      |
//	|      |        | bit 6  : 0 = Active system / 1 = Idle system   |
//	|      |        | bit 5  : 0 = No bus fault  / 1 = Bus fault     |
//	|      |        | bit 4  : 0 = No overheat   / 1 = Overheat      |
//	|      |        | bit 0-3: always 0                              |
//
// Spec: SW-P-02 Issue 26 §3.2.11.
type StatusResponse2Params struct {
	Idle     bool // bit 6
	BusFault bool // bit 5
	Overheat bool // bit 4
}

// PayloadLenStatusResponse2 is the fixed MESSAGE byte count for tx 09.
const PayloadLenStatusResponse2 = 1

// EncodeStatusResponse2 builds tx 09 wire bytes.
func EncodeStatusResponse2(p StatusResponse2Params) Frame {
	var status byte
	if p.Idle {
		status |= 1 << 6
	}
	if p.BusFault {
		status |= 1 << 5
	}
	if p.Overheat {
		status |= 1 << 4
	}
	return Frame{
		ID:      TxStatusResponse2,
		Payload: []byte{status},
	}
}

// DecodeStatusResponse2 parses tx 09.
func DecodeStatusResponse2(f Frame) (StatusResponse2Params, error) {
	if f.ID != TxStatusResponse2 {
		return StatusResponse2Params{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenStatusResponse2 {
		return StatusResponse2Params{}, ErrShortPayload
	}
	s := f.Payload[0]
	return StatusResponse2Params{
		Idle:     s&(1<<6) != 0,
		BusFault: s&(1<<5) != 0,
		Overheat: s&(1<<4) != 0,
	}, nil
}
