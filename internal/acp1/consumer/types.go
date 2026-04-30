// Package acp1 implements the Axon Control Protocol v1.4 wire format and
// plugin for the internal/protocol interface.
//
// Authoritative spec: assets/AXON-ACP_v1_4.pdf.
// Cross-reference: the Axon Wireshark dissector in wireshark/dhs_acpv1.lua
// and the C# reference driver (ByResearch.DHS.AxonACP.DeviceDriver).
//
// Scope of this package:
//   - UDP direct mode (port 2071) — current target
//   - TCP direct mode with MLEN prefix — later
//   - AN2 transport — out of scope for v1 (see CLAUDE.md)
//
// Nothing outside cmd/ and this package's own files may import acp1. The
// rest of the system talks to the Protocol interface only.
package acp1

// Spec §"ACP Port Number" p. 7.
const (
	// DefaultPort is the IANA-assigned ACP port (both UDP and TCP direct).
	DefaultPort = 2071

	// HeaderSize is the fixed ACP header length in bytes:
	//   MTID(4) + PVER(1) + MTYPE(1) + MADDR(1) = 7.
	HeaderSize = 7

	// MaxMDATA is the maximum AxonNet payload length per spec §"IP / UDP
	// issues" p. 7 and §"Object details" p. 20.
	MaxMDATA = 134

	// MaxPacket is the maximum total ACP datagram: header + MDATA.
	// Spec p. 7 explicitly states 169 = 20 IP + 8 UDP + 7 ACP + 134 data,
	// so the ACP-layer packet is at most 141 bytes.
	MaxPacket = HeaderSize + MaxMDATA

	// MaxValueData is the maximum bytes left for method args / return
	// values after the MCODE + ObjGroup + ObjId triplet inside MDATA.
	// Spec p. 20: "Objects are limited in size to 131 bytes."
	MaxValueData = MaxMDATA - 3
)

// PVER is the ACP protocol version byte. Spec p. 11 defines exactly one
// value for v1.4 devices.
const PVER byte = 1

// MType is the ACP message type field. Spec §"ACP Message Types" p. 8.
type MType uint8

const (
	MTypeAnnounce MType = 0 // Announcement (broadcast)
	MTypeRequest  MType = 1 // Client → server request
	MTypeReply    MType = 2 // Server → client reply
	MTypeError    MType = 3 // Server → client error reply
)

// Method is the MCODE byte when MType < 3. Spec §"Methods" p. 28.
// Spec v1.4 defines exactly six methods — no others exist.
type Method uint8

const (
	MethodGetValue    Method = 0
	MethodSetValue    Method = 1
	MethodSetIncValue Method = 2
	MethodSetDecValue Method = 3
	MethodSetDefValue Method = 4
	MethodGetObject   Method = 5
)

// ObjGroup is the AxonNet object-group ID byte. Spec §"Object Groups" p. 17.
type ObjGroup uint8

const (
	GroupRoot     ObjGroup = 0
	GroupIdentity ObjGroup = 1
	GroupControl  ObjGroup = 2
	GroupStatus   ObjGroup = 3
	GroupAlarm    ObjGroup = 4
	GroupFile     ObjGroup = 5
	GroupFrame    ObjGroup = 6
)

// String returns the canonical lower-case group name used by the CLI
// (--group) and by the label-map walker.
func (g ObjGroup) String() string {
	switch g {
	case GroupRoot:
		return "root"
	case GroupIdentity:
		return "identity"
	case GroupControl:
		return "control"
	case GroupStatus:
		return "status"
	case GroupAlarm:
		return "alarm"
	case GroupFile:
		return "file"
	case GroupFrame:
		return "frame"
	default:
		return "unknown"
	}
}

// ParseGroup maps a CLI/API group name back to its enum. Case-insensitive.
func ParseGroup(name string) (ObjGroup, bool) {
	switch name {
	case "root", "ROOT", "Root":
		return GroupRoot, true
	case "identity", "IDENTITY", "Identity":
		return GroupIdentity, true
	case "control", "CONTROL", "Control":
		return GroupControl, true
	case "status", "STATUS", "Status":
		return GroupStatus, true
	case "alarm", "ALARM", "Alarm":
		return GroupAlarm, true
	case "file", "FILE", "File":
		return GroupFile, true
	case "frame", "FRAME", "Frame":
		return GroupFrame, true
	}
	return 0, false
}

// ObjectType is the first property byte of every AxonNet object. Spec
// §"Object details" p. 19 and the per-type tables p. 21–27.
type ObjectType uint8

const (
	TypeRoot     ObjectType = 0
	TypeInteger  ObjectType = 1
	TypeIPAddr   ObjectType = 2
	TypeFloat    ObjectType = 3
	TypeEnum     ObjectType = 4
	TypeString   ObjectType = 5
	TypeFrame    ObjectType = 6
	TypeAlarm    ObjectType = 7
	TypeFile     ObjectType = 8
	TypeLong     ObjectType = 9
	TypeByte     ObjectType = 10
	TypeReserved ObjectType = 11 // v1.4 reserved slot
)

// Access bit flags. Spec §"Property Access" p. 20.
const (
	AccessRead   uint8 = 1 << 0 // bit 0: read access
	AccessWrite  uint8 = 1 << 1 // bit 1: write access
	AccessSetDef uint8 = 1 << 2 // bit 2: setDefValue access
)

// String field maximums per spec §"Object details" p. 20.
const (
	MaxLabelLen   = 16 // max chars excluding the NUL terminator
	MaxUnitLen    = 4  // max chars excluding NUL
	MaxAlarmMsg   = 32 // alarm on/off event message, excluding NUL
)

// ACP transport errors (MType=3, MCODE < 16). Spec §"ACP Header" p. 11.
type TransportErrCode uint8

const (
	TErrUndefined         TransportErrCode = 0
	TErrInternalBusComm   TransportErrCode = 1
	TErrInternalBusTimeout TransportErrCode = 2
	TErrTransactionTimeout TransportErrCode = 3
	TErrOutOfResources    TransportErrCode = 4
)

// AxonNet object errors (MType=3, MCODE >= 16). Spec §"AxonNet error codes" p. 29.
type ObjectErrCode uint8

const (
	OErrGroupNoExist    ObjectErrCode = 16
	OErrInstanceNoExist ObjectErrCode = 17
	OErrPropertyNoExist ObjectErrCode = 18
	OErrNoWriteAccess   ObjectErrCode = 19
	OErrNoReadAccess    ObjectErrCode = 20
	OErrNoSetDefAccess  ObjectErrCode = 21
	OErrTypeNoExist     ObjectErrCode = 22
	OErrIllegalMethod   ObjectErrCode = 23
	OErrIllegalForType  ObjectErrCode = 24
	OErrFile            ObjectErrCode = 32
	OErrSPFConstraint   ObjectErrCode = 39
	OErrSPFBufferFull   ObjectErrCode = 40
)
