package acp1

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Message is the decoded form of one ACP1 datagram (UDP direct mode).
//
// Wire layout per spec §"ACP Header" p. 9 and §"AxonNet - ACP mapping" p. 32:
//
//	offset 0..3   MTID     u32 big-endian
//	offset 4      PVER     u8 (always 1 for v1.4 devices)
//	offset 5      MTYPE    u8 (0=announce, 1=request, 2=reply, 3=error)
//	offset 6      MADDR    u8 (slot 0..31; 0 = rack controller)
//	offset 7..    MDATA    ≤134 bytes; first 1-3 bytes depend on MType
//
// MDATA layout for MType < 3 (request / reply / non-error announce):
//
//	MDATA[0]      MCODE    method id (see Method)
//	MDATA[1]      ObjGrp   object group id
//	MDATA[2]      ObjId    object id
//	MDATA[3..]    Value    method args / return value (up to 131 bytes)
//
// MDATA layout for MType = 3 (error reply):
//
//	MDATA[0]      MCODE    error code (TransportErrCode if <16, ObjectErrCode if ≥16)
//	MDATA[1..]    (optional) the original ObjGrp/ObjId if the device includes them
//
// The C# reference driver unconditionally reads ObjGrp/ObjId on error
// replies — that works in practice because Axon firmware always echoes
// them. Our decoder follows the spec letter: ObjGrp/ObjId are only
// meaningful when MType<3. For error replies, use the Err* accessors.
type Message struct {
	MTID     uint32
	PVER     byte
	MType    MType
	MAddr    byte
	MCode    byte
	ObjGroup ObjGroup
	ObjID    byte
	Value    []byte // method args / return value; excludes MCODE/ObjGroup/ObjID
}

// ErrTruncated is returned by Decode when the input is shorter than the
// minimum valid ACP1 datagram for its MType.
var ErrTruncated = errors.New("acp1: truncated datagram")

// ErrOversized is returned by Decode when the input exceeds MaxPacket.
var ErrOversized = errors.New("acp1: datagram exceeds 141 bytes")

// ErrBadPVer is returned by Decode when PVER != 1.
var ErrBadPVer = errors.New("acp1: unsupported PVER (expected 1)")

// Encode serialises m into a freshly allocated byte slice ready for
// UDPConn.Send. Returns an error if any field is out of spec range
// (MType > 3, MADDR > 31 for request messages, value data too large).
//
// Wire rules applied:
//   - MTID written big-endian
//   - PVER forced to 1 if zero (v1.4 only)
//   - For MType < 3: MDATA = [MCODE, ObjGroup, ObjID, Value...]
//   - For MType = 3: MDATA = [MCode] only (caller supplies the error code
//     via MCode and leaves Value nil)
//
// Output wire layout (≤ 141 bytes total):
//
//	| Offset | Field    | Width | Notes                                      |
//	|--------|----------|-------|--------------------------------------------|
//	|   0    | MTID     |   4   | u32 big-endian; 0 = announcement           |
//	|   4    | PVER     |   1   | 1 (ACP1 v1.4); forced if caller passes 0   |
//	|   5    | MTYPE    |   1   | 0=announce, 1=request, 2=reply, 3=error    |
//	|   6    | MADDR    |   1   | slot 0..31 (0 = rack controller)           |
//	|   7    | MCODE    |   1   | method id (MTYPE<3) or error code (MTYPE=3)|
//	|   8    | ObjGroup |   1   | present only when MTYPE < 3                |
//	|   9    | ObjID    |   1   | present only when MTYPE < 3                |
//	|  10..  | Value    |  ≤131 | method args / return value (MTYPE < 3)     |
//
// Spec reference: AXON-ACP_v1_4.pdf §"ACP Header" p. 9 and
// §"AxonNet - ACP mapping" p. 32.
func (m *Message) Encode() ([]byte, error) {
	if m == nil {
		return nil, errors.New("acp1: Encode nil message")
	}
	if m.MType > MTypeError {
		return nil, fmt.Errorf("acp1: invalid MType %d", m.MType)
	}
	// Spec p. 11: "The address should be in the range 0..31." For
	// announcements MADDR carries the source slot which can also be in
	// that range. We enforce it uniformly.
	if m.MAddr > 31 {
		return nil, fmt.Errorf("acp1: MADDR out of range: %d", m.MAddr)
	}

	// MDATA size budget check. For MType<3 we write 3 preamble bytes
	// (MCODE/ObjGroup/ObjID) + Value. For MType=3 we write 1 byte (MCode).
	var mdataLen int
	switch m.MType {
	case MTypeError:
		mdataLen = 1
	default:
		mdataLen = 3 + len(m.Value)
	}
	if mdataLen > MaxMDATA {
		return nil, fmt.Errorf("acp1: MDATA too large: %d > %d", mdataLen, MaxMDATA)
	}
	if len(m.Value) > MaxValueData {
		return nil, fmt.Errorf("acp1: value data too large: %d > %d", len(m.Value), MaxValueData)
	}

	pver := m.PVER
	if pver == 0 {
		pver = PVER
	}

	out := make([]byte, HeaderSize+mdataLen)
	binary.BigEndian.PutUint32(out[0:4], m.MTID)
	out[4] = pver
	out[5] = byte(m.MType)
	out[6] = m.MAddr

	switch m.MType {
	case MTypeError:
		out[HeaderSize] = m.MCode
	default:
		out[HeaderSize+0] = m.MCode
		out[HeaderSize+1] = byte(m.ObjGroup)
		out[HeaderSize+2] = m.ObjID
		if len(m.Value) > 0 {
			copy(out[HeaderSize+3:], m.Value)
		}
	}
	return out, nil
}

// Decode parses one ACP1 datagram. Safe for garbled input — every length
// check is explicit and every return path sets a non-nil error on failure.
//
// Input wire layout (minimum 8 bytes, maximum 141 bytes):
//
//	| Offset | Field    | Width | Notes                                      |
//	|--------|----------|-------|--------------------------------------------|
//	|   0    | MTID     |   4   | u32 big-endian; 0 = announcement           |
//	|   4    | PVER     |   1   | must be 1; otherwise ErrBadPVer            |
//	|   5    | MTYPE    |   1   | 0..3; otherwise "invalid MType"            |
//	|   6    | MADDR    |   1   | slot id (0..31)                            |
//	|   7    | MCODE    |   1   | always present (method id or error code)   |
//	|   8    | ObjGroup |   1   | required for MTYPE<3; optional on error    |
//	|   9    | ObjID    |   1   | required for MTYPE<3; optional on error    |
//	|  10..  | Value    |  ≤131 | present only for MTYPE < 3                 |
//
// Spec reference: AXON-ACP_v1_4.pdf §"ACP Header" p. 9 and
// §"AxonNet - ACP mapping" p. 32.
func Decode(buf []byte) (*Message, error) {
	if len(buf) < HeaderSize+1 {
		// Smallest valid ACP1 datagram: 7-byte header + 1-byte MDATA (an
		// error reply with just MCODE). Anything shorter is truncated.
		return nil, fmt.Errorf("%w: %d bytes < 8", ErrTruncated, len(buf))
	}
	if len(buf) > MaxPacket {
		return nil, fmt.Errorf("%w: %d > %d", ErrOversized, len(buf), MaxPacket)
	}

	m := &Message{
		MTID:  binary.BigEndian.Uint32(buf[0:4]),
		PVER:  buf[4],
		MType: MType(buf[5]),
		MAddr: buf[6],
	}
	if m.PVER != PVER {
		return nil, fmt.Errorf("%w: got %d", ErrBadPVer, m.PVER)
	}
	if m.MType > MTypeError {
		return nil, fmt.Errorf("acp1: invalid MType %d", m.MType)
	}

	mdata := buf[HeaderSize:]
	// MCODE is always present (byte 0 of MDATA), regardless of MType.
	m.MCode = mdata[0]

	if m.MType == MTypeError {
		// Error replies *may* omit ObjGrp/ObjId. Accept both forms and
		// surface whatever is present so callers can use them for context
		// if the device included them. No Value on error replies.
		if len(mdata) >= 2 {
			m.ObjGroup = ObjGroup(mdata[1])
		}
		if len(mdata) >= 3 {
			m.ObjID = mdata[2]
		}
		return m, nil
	}

	// MType < 3: expect MCODE + ObjGrp + ObjId + optional value bytes.
	if len(mdata) < 3 {
		return nil, fmt.Errorf("%w: MDATA %d bytes < 3 for MType %d", ErrTruncated, len(mdata), m.MType)
	}
	m.ObjGroup = ObjGroup(mdata[1])
	m.ObjID = mdata[2]

	if len(mdata) > 3 {
		m.Value = make([]byte, len(mdata)-3)
		copy(m.Value, mdata[3:])
	}
	return m, nil
}

// IsAnnouncement reports whether the message is an unsolicited broadcast.
// Announcements have MTID=0 and MType ∈ {0, 2} per spec Reply Message
// Matrix p. 14. MType=0 is a status/event/frame-status change;
// MType=2 with MCode in {1,2,3,4} is a value-change transaction echo.
func (m *Message) IsAnnouncement() bool {
	if m.MTID != 0 {
		return false
	}
	return m.MType == MTypeAnnounce || m.MType == MTypeReply
}

// IsError reports whether the message is an error reply (MType=3).
func (m *Message) IsError() bool { return m.MType == MTypeError }

// ErrCode returns a typed interpretation of MCode when the message is an
// error reply. Returns nil for non-error messages.
func (m *Message) ErrCode() error {
	if m.MType != MTypeError {
		return nil
	}
	if m.MCode < 16 {
		return TransportErr{Code: TransportErrCode(m.MCode)}
	}
	return ObjectErr{Code: ObjectErrCode(m.MCode), Group: m.ObjGroup, ID: m.ObjID}
}

// TransportErr is returned by ErrCode for MCODE < 16.
type TransportErr struct {
	Code TransportErrCode
}

func (e TransportErr) Error() string {
	switch e.Code {
	case TErrUndefined:
		return "acp1 transport: undefined error"
	case TErrInternalBusComm:
		return "acp1 transport: internal bus communication error"
	case TErrInternalBusTimeout:
		return "acp1 transport: internal bus timeout"
	case TErrTransactionTimeout:
		return "acp1 transport: transaction timeout"
	case TErrOutOfResources:
		return "acp1 transport: out of resources"
	default:
		return fmt.Sprintf("acp1 transport: unknown code %d", e.Code)
	}
}

// ObjectErr is returned by ErrCode for MCODE >= 16.
type ObjectErr struct {
	Code  ObjectErrCode
	Group ObjGroup
	ID    byte
}

func (e ObjectErr) Error() string {
	var desc string
	switch e.Code {
	case OErrGroupNoExist:
		desc = "object group does not exist"
	case OErrInstanceNoExist:
		desc = "object instance does not exist"
	case OErrPropertyNoExist:
		desc = "object property does not exist"
	case OErrNoWriteAccess:
		desc = "no write access"
	case OErrNoReadAccess:
		desc = "no read access"
	case OErrNoSetDefAccess:
		desc = "no setDefault access"
	case OErrTypeNoExist:
		desc = "object type does not exist"
	case OErrIllegalMethod:
		desc = "illegal method"
	case OErrIllegalForType:
		desc = "illegal method for this object type"
	case OErrFile:
		desc = "file error"
	case OErrSPFConstraint:
		desc = "SPF file constraint violation"
	case OErrSPFBufferFull:
		desc = "SPF buffer full — retry fragment later"
	default:
		desc = fmt.Sprintf("unknown code %d", e.Code)
	}
	return fmt.Sprintf("acp1 object error at %s[%d]: %s", e.Group, e.ID, desc)
}
