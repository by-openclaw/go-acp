package codec

import (
	"encoding/binary"
	"fmt"
)

// ACP2Message is one decoded ACP2 message carried inside an AN2 data frame.
//
// Wire layout (4-byte header + body):
//
//	byte 0   Type    u8   0=request, 1=reply, 2=announce, 3=error
//	byte 1   MTID    u8   0=announces, 1-255=req/reply
//	byte 2   Func    u8   function id (req/reply) or stat (error)
//	byte 3   PID     u8   property id or version number
//
// For funcs 1-3 (get_object, get_property, set_property), the body follows:
//
//	bytes 4-7    ObjID   u32 BE
//	bytes 8-11   Idx     u32 BE   (0 = active index)
//	bytes 12+    property headers (variable)
type ACP2Message struct {
	Type ACP2MsgType
	MTID uint8
	Func ACP2Func // or stat for errors
	PID  uint8    // multipurpose: pid, padding, or version

	// Body fields (for funcs 1-3):
	ObjID      uint32
	Idx        uint32
	Properties []Property // decoded property headers
	Body       []byte     // raw body bytes after the 4-byte header
}

// EncodeACP2Message serialises an ACP2 message into wire bytes suitable
// for embedding in an AN2 data frame payload.
//
// Header (4 bytes, always emitted):
//
//	| Offset | Field  | Width | Notes                                     |
//	|--------|--------|-------|-------------------------------------------|
//	| 0      | type   | u8    | 0=request, 1=reply, 2=announce, 3=error   |
//	| 1      | mtid   | u8    | 0 for announces; 1-255 req/reply correl.  |
//	| 2      | func   | u8    | function id (req/reply) or stat (error)   |
//	| 3      | pid    | u8    | multipurpose: pid / padding / version     |
//
// Body by Func (request form):
//   - get_version  (0): header only
//   - get_object   (1): obj-id(u32 BE) + idx(u32 BE)
//   - get_property (2): obj-id(u32 BE) + idx(u32 BE) + 4-byte property hdr
//     for the requested pid (pid, data=0, plen=4)
//   - set_property (3): obj-id(u32 BE) + idx(u32 BE) + encoded Property[0]
//   - default: header + raw Body (escape hatch for pre-built payloads)
//
// idx=0 is ACTIVE INDEX on preset children; never "first preset slot".
//
// Spec reference: acp2_protocol.pdf §ACP2 Message Header
func EncodeACP2Message(m *ACP2Message) ([]byte, error) {
	if m == nil {
		return nil, fmt.Errorf("acp2: encode nil message")
	}

	switch m.Func {
	case ACP2FuncGetVersion:
		// get_version: just the 4-byte header
		buf := make([]byte, ACP2HeaderSize)
		buf[0] = byte(m.Type)
		buf[1] = m.MTID
		buf[2] = byte(m.Func)
		buf[3] = m.PID
		return buf, nil

	case ACP2FuncGetObject:
		// get_object: header(4) + obj-id(4) + idx(4) = 12 bytes.
		// Confirmed by Wireshark dissector (dhs_acpv2.lua lines 301-307):
		// obj-id at offset 4, idx at offset 8 for both request and reply.
		buf := make([]byte, ACP2HeaderSize+8)
		buf[0] = byte(m.Type)
		buf[1] = m.MTID
		buf[2] = byte(m.Func)
		buf[3] = m.PID
		binary.BigEndian.PutUint32(buf[4:8], m.ObjID)
		binary.BigEndian.PutUint32(buf[8:12], m.Idx)
		return buf, nil

	case ACP2FuncGetProperty:
		// get_property request per spec §"Get property" Request
		// (acp2_protocol.docx line 990-1020): body = obj-id(u32 BE) +
		// idx(u32 BE). The requested pid is carried in the ACP2
		// header byte 3 — there is NO trailing property header in
		// the request body.
		//
		//	| Offset | Field  | Width  | Notes                          |
		//	|--------|--------|--------|--------------------------------|
		//	| 0      | type   | u8     | 0 = request                    |
		//	| 1      | mtid   | u8     | 1..255                         |
		//	| 2      | func   | u8     | 2 = get_property               |
		//	| 3      | pid    | u8     | property id requested          |
		//	| 4-7    | obj-id | u32 BE | target object id               |
		//	| 8-11   | idx    | u32 BE | preset idx (0 = ACTIVE INDEX)  |
		buf := make([]byte, ACP2HeaderSize+8)
		buf[0] = byte(m.Type)
		buf[1] = m.MTID
		buf[2] = byte(m.Func)
		buf[3] = m.PID
		binary.BigEndian.PutUint32(buf[4:8], m.ObjID)
		binary.BigEndian.PutUint32(buf[8:12], m.Idx)
		return buf, nil

	case ACP2FuncSetProperty:
		// set_property: header + obj-id(4) + idx(4) + encoded property.
		// EncodeProperty only fails on a nil *Property; &m.Properties[0]
		// is an address into a non-empty slice and can never be nil, so
		// encodeProperty (the non-erroring core) is called directly — no
		// unreachable error branch.
		if len(m.Properties) == 0 {
			return nil, fmt.Errorf("acp2: set_property with no properties")
		}
		propBytes := encodeProperty(&m.Properties[0])
		buf := make([]byte, ACP2HeaderSize+8+len(propBytes))
		buf[0] = byte(m.Type)
		buf[1] = m.MTID
		buf[2] = byte(m.Func)
		buf[3] = m.PID
		binary.BigEndian.PutUint32(buf[4:8], m.ObjID)
		binary.BigEndian.PutUint32(buf[8:12], m.Idx)
		copy(buf[ACP2HeaderSize+8:], propBytes)
		return buf, nil

	default:
		// Generic: header + raw body
		buf := make([]byte, ACP2HeaderSize+len(m.Body))
		buf[0] = byte(m.Type)
		buf[1] = m.MTID
		buf[2] = byte(m.Func)
		buf[3] = m.PID
		copy(buf[ACP2HeaderSize:], m.Body)
		return buf, nil
	}
}

// DecodeACP2Message parses an ACP2 message from an AN2 data frame payload.
//
// Header (4 bytes mandatory):
//
//	| Offset | Field  | Width | Notes                                     |
//	|--------|--------|-------|-------------------------------------------|
//	| 0      | type   | u8    | 0=request, 1=reply, 2=announce, 3=error   |
//	| 1      | mtid   | u8    | 0 for announces/events; 1-255 req/reply   |
//	| 2      | func   | u8    | function id, or stat when type=3 (error)  |
//	| 3      | pid    | u8    | pid / padding / version number            |
//
// Body parsing rules:
//   - type=3 error: per spec §"Error" the message is exactly the
//     4-byte ACP2 header; no body. The func byte is reinterpreted as
//     ACP2ErrStatus via ToACP2Error. Any trailing bytes are stashed
//     in m.Body so a caller / compliance check can flag the
//     deviation, but ObjID is NEVER populated from them.
//   - reply + func=get_version: header only; pid holds the version byte.
//   - reply / announce (funcs 1-3): body layout
//     [0..3]   obj-id  u32 BE
//     [4..7]   idx     u32 BE   (0 = ACTIVE INDEX on preset children)
//     [8..]    property headers (pid/data/plen + value + padding), decoded
//     by DecodeProperties.
//
// "announce" is type=2 — never "event" (spec terminology).
//
// Spec reference: acp2_protocol.pdf §ACP2 Message Header
func DecodeACP2Message(data []byte) (*ACP2Message, error) {
	if len(data) < ACP2HeaderSize {
		return nil, fmt.Errorf("acp2: message too short: %d < %d", len(data), ACP2HeaderSize)
	}

	m := &ACP2Message{
		Type: ACP2MsgType(data[0]),
		MTID: data[1],
		Func: ACP2Func(data[2]),
		PID:  data[3],
	}

	body := data[ACP2HeaderSize:]
	m.Body = make([]byte, len(body))
	copy(m.Body, body)

	// For error messages, the spec (§"Error", line 1207-1250 of
	// acp2_protocol.docx) defines the message as exactly the 4-byte
	// ACP2 header — NO body. Do not synthesise ObjID from any
	// trailing bytes; doing so silently masks producer bugs in
	// shipping peers and prevents detection of spec-non-compliant
	// wire shapes.
	if m.Type == ACP2TypeError {
		// Trailing bytes (if any) remain in m.Body for caller-side
		// compliance checks to inspect. ObjID stays 0 — clients
		// needing to correlate the error with a specific request use
		// the mtid, not a body-derived obj-id.
		return m, nil
	}

	// For get_version replies, PID holds the version number. No body parsing.
	// Only applies to actual replies — announces have stat=0 in the func
	// field which collides with ACP2FuncGetVersion(0).
	if m.Type == ACP2TypeReply && m.Func == ACP2FuncGetVersion {
		return m, nil
	}

	// For funcs 1-3 replies, parse obj-id, idx, and properties.
	if m.Type == ACP2TypeReply || m.Type == ACP2TypeAnnounce {
		if len(body) >= 8 {
			m.ObjID = binary.BigEndian.Uint32(body[0:4])
			m.Idx = binary.BigEndian.Uint32(body[4:8])
			if len(body) > 8 {
				props, err := DecodeProperties(body[8:])
				if err != nil {
					return nil, fmt.Errorf("acp2: decode properties: %w", err)
				}
				m.Properties = props
			}
		}
	}

	return m, nil
}

// ToACP2Error converts an error-type ACP2Message into a typed error.
func (m *ACP2Message) ToACP2Error() error {
	if m.Type != ACP2TypeError {
		return nil
	}
	return &ACP2Error{
		Status: ACP2ErrStatus(m.Func), // func field holds stat on errors
		ObjID:  m.ObjID,
	}
}
