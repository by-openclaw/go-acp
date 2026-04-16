// Package acp2 implements the ACP v2 protocol plugin for the
// internal/protocol interface.
//
// ACP2 runs exclusively over AN2/TCP (port 2072). The AN2 transport
// layer handles framing; ACP2 messages are carried inside AN2 data
// frames with proto=2.
//
// Authoritative spec: docs/protocols/acp2_protocol.pdf.
// Cross-reference: CLAUDE.md ACP2 section, dissector_acp2.lua.
package acp2

import "fmt"

// ---- Ports and sizes ----

const (
	// DefaultPort is the AN2 TCP port used for ACP2 traffic.
	DefaultPort = 2072

	// AN2HeaderSize is the fixed AN2 frame header length.
	AN2HeaderSize = 8

	// AN2Magic is the 16-bit magic validated on every received frame.
	AN2Magic uint16 = 0xC635

	// ACP2HeaderSize is the fixed ACP2 message header length inside
	// an AN2 data frame payload.
	ACP2HeaderSize = 4

	// MaxPayload is a safety cap for AN2 frame payloads to prevent
	// unbounded allocations on corrupted streams. 64 KiB is generous
	// for any single ACP2 response.
	MaxPayload = 65536
)

// ---- AN2 constants ----

// AN2Proto identifies which sub-protocol an AN2 frame carries.
type AN2Proto uint8

const (
	AN2ProtoInternal AN2Proto = 0 // AN2 control messages (GetVersion, GetDeviceInfo, etc.)
	AN2ProtoACP1     AN2Proto = 1
	AN2ProtoACP2     AN2Proto = 2
	AN2ProtoACMP     AN2Proto = 3
)

func (p AN2Proto) String() string {
	switch p {
	case AN2ProtoInternal:
		return "AN2"
	case AN2ProtoACP1:
		return "ACP1"
	case AN2ProtoACP2:
		return "ACP2"
	case AN2ProtoACMP:
		return "ACMP"
	default:
		return fmt.Sprintf("proto(%d)", p)
	}
}

// AN2Type is the AN2 frame type field.
type AN2Type uint8

const (
	AN2TypeRequest AN2Type = 0
	AN2TypeReply   AN2Type = 1
	AN2TypeEvent   AN2Type = 2
	AN2TypeError   AN2Type = 3
	AN2TypeData    AN2Type = 4
)

func (t AN2Type) String() string {
	switch t {
	case AN2TypeRequest:
		return "request"
	case AN2TypeReply:
		return "reply"
	case AN2TypeEvent:
		return "event"
	case AN2TypeError:
		return "error"
	case AN2TypeData:
		return "data"
	default:
		return fmt.Sprintf("type(%d)", t)
	}
}

// AN2Slot constants.
const (
	AN2SlotBroadcast uint8 = 255
)

// ---- AN2 internal function IDs ----

// AN2 internal protocol (proto=0) request function IDs.
const (
	AN2FuncGetVersion            uint8 = 0
	AN2FuncGetDeviceInfo         uint8 = 1
	AN2FuncGetSlotInfo           uint8 = 2
	AN2FuncEnableProtocolEvents  uint8 = 3
)

// ---- ACP2 message types ----

// ACP2MsgType is the type field in the ACP2 message header.
type ACP2MsgType uint8

const (
	ACP2TypeRequest  ACP2MsgType = 0
	ACP2TypeReply    ACP2MsgType = 1
	ACP2TypeAnnounce ACP2MsgType = 2
	ACP2TypeError    ACP2MsgType = 3
)

func (t ACP2MsgType) String() string {
	switch t {
	case ACP2TypeRequest:
		return "request"
	case ACP2TypeReply:
		return "reply"
	case ACP2TypeAnnounce:
		return "announce"
	case ACP2TypeError:
		return "error"
	default:
		return fmt.Sprintf("acp2type(%d)", t)
	}
}

// ---- ACP2 function IDs ----

// ACP2Func is the func field in ACP2 request/reply headers.
type ACP2Func uint8

const (
	ACP2FuncGetVersion  ACP2Func = 0
	ACP2FuncGetObject   ACP2Func = 1
	ACP2FuncGetProperty ACP2Func = 2
	ACP2FuncSetProperty ACP2Func = 3
)

func (f ACP2Func) String() string {
	switch f {
	case ACP2FuncGetVersion:
		return "get_version"
	case ACP2FuncGetObject:
		return "get_object"
	case ACP2FuncGetProperty:
		return "get_property"
	case ACP2FuncSetProperty:
		return "set_property"
	default:
		return fmt.Sprintf("func(%d)", f)
	}
}

// ---- ACP2 object types ----

// ACP2ObjType is returned in pid=1 (object_type) of every object.
type ACP2ObjType uint8

const (
	ObjTypeNode   ACP2ObjType = 0
	ObjTypePreset ACP2ObjType = 1
	ObjTypeEnum   ACP2ObjType = 2
	ObjTypeNumber ACP2ObjType = 3
	ObjTypeIPv4   ACP2ObjType = 4
	ObjTypeString ACP2ObjType = 5
)

func (t ACP2ObjType) String() string {
	switch t {
	case ObjTypeNode:
		return "node"
	case ObjTypePreset:
		return "preset"
	case ObjTypeEnum:
		return "enum"
	case ObjTypeNumber:
		return "number"
	case ObjTypeIPv4:
		return "ipv4"
	case ObjTypeString:
		return "string"
	default:
		return fmt.Sprintf("objtype(%d)", t)
	}
}

// ---- ACP2 property IDs ----

// PID constants for ACP2 property headers.
const (
	PIDObjectType      uint8 = 1
	PIDLabel           uint8 = 2
	PIDAccess          uint8 = 3
	PIDAnnounceDelay   uint8 = 4
	PIDNumberType      uint8 = 5
	PIDStringMaxLength uint8 = 6
	PIDPresetDepth     uint8 = 7
	PIDValue           uint8 = 8
	PIDDefaultValue    uint8 = 9
	PIDMinValue        uint8 = 10
	PIDMaxValue        uint8 = 11
	PIDStepSize        uint8 = 12
	PIDUnit            uint8 = 13
	PIDChildren        uint8 = 14
	PIDOptions         uint8 = 15
	PIDEventTag        uint8 = 16
	PIDEventPrio       uint8 = 17
	PIDEventState      uint8 = 18
	PIDEventMessages   uint8 = 19
	PIDPresetParent    uint8 = 20
)

// ---- ACP2 number types ----

// NumberType encodes the wire type for numeric properties (pid 5 and vtype).
type NumberType uint8

const (
	NumTypeS8     NumberType = 0
	NumTypeS16    NumberType = 1
	NumTypeS32    NumberType = 2
	NumTypeS64    NumberType = 3
	NumTypeU8     NumberType = 4
	NumTypeU16    NumberType = 5
	NumTypeU32    NumberType = 6
	NumTypeU64    NumberType = 7
	NumTypeFloat  NumberType = 8
	NumTypePreset NumberType = 9
	NumTypeIPv4   NumberType = 10
	NumTypeString NumberType = 11
)

func (n NumberType) String() string {
	switch n {
	case NumTypeS8:
		return "s8"
	case NumTypeS16:
		return "s16"
	case NumTypeS32:
		return "s32"
	case NumTypeS64:
		return "s64"
	case NumTypeU8:
		return "u8"
	case NumTypeU16:
		return "u16"
	case NumTypeU32:
		return "u32"
	case NumTypeU64:
		return "u64"
	case NumTypeFloat:
		return "float"
	case NumTypePreset:
		return "preset"
	case NumTypeIPv4:
		return "ipv4"
	case NumTypeString:
		return "string"
	default:
		return fmt.Sprintf("numtype(%d)", n)
	}
}

// ---- ACP2 error status codes ----

// ACP2ErrStatus is the stat field in an ACP2 error message.
type ACP2ErrStatus uint8

const (
	ErrProtocol       ACP2ErrStatus = 0
	ErrInvalidObjID   ACP2ErrStatus = 1
	ErrInvalidIdx     ACP2ErrStatus = 2
	ErrInvalidPID     ACP2ErrStatus = 3
	ErrNoAccess       ACP2ErrStatus = 4
	ErrInvalidValue   ACP2ErrStatus = 5
)

// ---- Error types ----

// ACP2Error represents a device-side ACP2 error reply (type=3).
type ACP2Error struct {
	Status ACP2ErrStatus
	ObjID  uint32
}

func (e *ACP2Error) Error() string {
	var desc string
	switch e.Status {
	case ErrProtocol:
		desc = "protocol error"
	case ErrInvalidObjID:
		desc = "invalid obj-id"
	case ErrInvalidIdx:
		desc = "invalid idx"
	case ErrInvalidPID:
		desc = "invalid pid"
	case ErrNoAccess:
		desc = "no access"
	case ErrInvalidValue:
		desc = "invalid value"
	default:
		desc = fmt.Sprintf("unknown status %d", e.Status)
	}
	return fmt.Sprintf("acp2 error (obj-id %d): %s", e.ObjID, desc)
}

func (e *ACP2Error) acpError() {}

// AN2Error represents an AN2-layer error (proto=0, type=error).
type AN2Error struct {
	Slot uint8
	Func uint8
	Msg  string
}

func (e *AN2Error) Error() string {
	return fmt.Sprintf("an2 error (slot %d, func %d): %s", e.Slot, e.Func, e.Msg)
}

func (e *AN2Error) acpError() {}
