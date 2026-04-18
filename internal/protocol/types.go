// Package protocol defines the protocol-agnostic contract that CLI, API,
// and device registry use to talk to any ACP-family protocol plugin.
//
// Wire-format specifics live in sibling packages (acp1/, acp2/, ...).
// Nothing outside cmd/ and the plugin's own package may import those.
package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// DeviceInfo is the protocol-agnostic summary of a connected device.
type DeviceInfo struct {
	// IP is the device address the plugin is talking to.
	IP string

	// Port actually used (may differ from the protocol default).
	Port int

	// NumSlots is how many slots the device reports. For ACP1 this is the
	// length of the Frame Status slot-status array returned by
	// getValue(group=frame, id=0) at slot 0.
	NumSlots int

	// ProtocolVersion is the PVER byte for ACP1 (always 1 for v1.4 devices)
	// or the ACP2 version number returned by get_version.
	ProtocolVersion int
}

// SlotStatus mirrors the ACP1 Frame Status byte values (spec p. 24).
// ACP2 reuses the same enumeration via AN2 slot-info events.
type SlotStatus uint8

// MarshalJSON renders a SlotStatus as its human-readable name so
// exported snapshots are easy to read ("present" instead of 2).
func (s SlotStatus) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

// UnmarshalJSON accepts both name strings and numeric codes.
func (s *SlotStatus) UnmarshalJSON(data []byte) error {
	if len(data) >= 2 && data[0] == '"' {
		name := string(data[1 : len(data)-1])
		switch name {
		case "no_card":
			*s = SlotNoCard
		case "power_up":
			*s = SlotPowerUp
		case "present":
			*s = SlotPresent
		case "error":
			*s = SlotError
		case "removed":
			*s = SlotRemoved
		case "boot_mode":
			*s = SlotBootMode
		}
		return nil
	}
	var n uint8
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	*s = SlotStatus(n)
	return nil
}

const (
	SlotNoCard    SlotStatus = 0
	SlotPowerUp   SlotStatus = 1
	SlotPresent   SlotStatus = 2
	SlotError     SlotStatus = 3
	SlotRemoved   SlotStatus = 4
	SlotBootMode  SlotStatus = 5
)

func (s SlotStatus) String() string {
	switch s {
	case SlotNoCard:
		return "no_card"
	case SlotPowerUp:
		return "power_up"
	case SlotPresent:
		return "present"
	case SlotError:
		return "error"
	case SlotRemoved:
		return "removed"
	case SlotBootMode:
		return "boot_mode"
	default:
		return "unknown"
	}
}

// SlotInfo describes a single card slot within a device frame.
type SlotInfo struct {
	Slot   int
	Status SlotStatus

	// Identity is the decoded mandatory identity objects (label, description,
	// serial, etc.) populated after Walk. May be empty before walk.
	Identity map[string]string
}

// ValueKind is the high-level type classifier the rest of the system uses
// for validation, export, and UI rendering. It intentionally hides the
// per-protocol object-type numbering (ACP1 Integer=1, Byte=10, ACP2
// number_type enum, etc.) behind a stable vocabulary.
type ValueKind uint8

const (
	KindUnknown ValueKind = iota
	KindBool
	KindInt      // signed integer, any width
	KindUint     // unsigned integer, any width
	KindFloat    // IEEE-754 float32 or float64
	KindEnum     // ordinal with named items
	KindString
	KindIPAddr
	KindAlarm    // alarm priority + event strings
	KindFrame    // frame-status slot array
	KindRaw      // opaque bytes
)

// String returns the canonical lowercase name for a ValueKind. Also
// used by MarshalJSON so exported snapshots carry readable type tags
// ("float") instead of numeric codes (4).
func (k ValueKind) String() string {
	switch k {
	case KindBool:
		return "bool"
	case KindInt:
		return "int"
	case KindUint:
		return "uint"
	case KindFloat:
		return "float"
	case KindEnum:
		return "enum"
	case KindString:
		return "string"
	case KindIPAddr:
		return "ipaddr"
	case KindAlarm:
		return "alarm"
	case KindFrame:
		return "frame"
	case KindRaw:
		return "raw"
	default:
		return "unknown"
	}
}

// MarshalJSON emits the human-readable name.
func (k ValueKind) MarshalJSON() ([]byte, error) {
	return []byte(`"` + k.String() + `"`), nil
}

// UnmarshalJSON accepts either a string ("float") or a numeric kind
// index for forward compatibility with older snapshots.
func (k *ValueKind) UnmarshalJSON(data []byte) error {
	if len(data) >= 2 && data[0] == '"' {
		name := string(data[1 : len(data)-1])
		*k = parseKind(name)
		return nil
	}
	var n uint8
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	*k = ValueKind(n)
	return nil
}

func parseKind(name string) ValueKind {
	switch name {
	case "bool":
		return KindBool
	case "int":
		return KindInt
	case "uint":
		return KindUint
	case "float":
		return KindFloat
	case "enum":
		return KindEnum
	case "string":
		return KindString
	case "ipaddr":
		return KindIPAddr
	case "alarm":
		return KindAlarm
	case "frame":
		return KindFrame
	case "raw":
		return KindRaw
	}
	return KindUnknown
}

// Object is the protocol-agnostic representation of one controllable item
// on a device slot. Constructed by a plugin's Walk() implementation.
//
// For ACP1 the (Group, ID) pair comes straight from the wire; for ACP2 the
// ObjectID is the u32 obj-id and Group is always empty.
type Object struct {
	Slot  int    `json:"slot"`
	Group string `json:"group,omitempty"` // ACP1 group name; empty for ACP2
	// Path is the logical hierarchical location of the object. Single
	// element for ACP1 (the group name); multi-element for ACP2 where
	// the tree has real depth (e.g. ["root", "outputs", "ch1"]). Both
	// plugins populate it; Group stays for ACP1 backward compat.
	Path []string `json:"path,omitempty"`
	ID   int      `json:"id"` // ACP1 ObjectID byte; ACP2 obj-id u32

	Label string    `json:"label"`
	Unit  string    `json:"unit,omitempty"`
	Kind  ValueKind `json:"kind"`

	// Access bits: read=1, write=2, setDef=4 (matches ACP1 access byte).
	Access uint8 `json:"access"`

	// Numeric constraints (populated only for numeric kinds).
	Min  any `json:"min,omitempty"`
	Max  any `json:"max,omitempty"`
	Step any `json:"step,omitempty"`
	Def  any `json:"default,omitempty"`

	EnumItems []string `json:"enum_items,omitempty"`

	MaxLen int `json:"max_len,omitempty"`

	AlarmPriority uint8  `json:"alarm_priority,omitempty"`
	AlarmTag      uint8  `json:"alarm_tag,omitempty"`
	AlarmOnMsg    string `json:"alarm_on,omitempty"`
	AlarmOffMsg   string `json:"alarm_off,omitempty"`

	// SubGroupMarker is true when this object is a device-convention
	// "section header". Walker sets this flag.
	SubGroupMarker bool `json:"sub_group_marker,omitempty"`

	// Value is the current object value as captured during Walk.
	Value Value `json:"value,omitempty"`
}

// HasRead reports whether the object permits getValue/getObject.
func (o Object) HasRead() bool { return o.Access&0x01 != 0 }

// HasWrite reports whether the object permits setValue.
func (o Object) HasWrite() bool { return o.Access&0x02 != 0 }

// HasSetDef reports whether the object permits setDefValue.
func (o Object) HasSetDef() bool { return o.Access&0x04 != 0 }

// ValueRequest addresses a single object on a single slot for a read or
// write operation. Plugins accept Path OR Label OR (Group+ID/ObjectID).
//
// Resolution priority:
//  1. Path — dot-separated tree path (e.g. "router.oneToN.parameters.sourceGain")
//     Unambiguous for all protocols. Required for Ember+ (labels collide).
//  2. Label — object label string. ACP1/ACP2 labels are unique within group/tree.
//  3. Group + ID — ACP1 wire address (group byte + object byte).
type ValueRequest struct {
	Slot int

	// Path is the dot-separated tree path. Preferred for Ember+.
	// For ACP1: "control.labelname". For ACP2: "BOARD.Card Name".
	// For Ember+: "router.oneToN.parameters.sourceGain".
	Path string

	// Label is the object label. Used by ACP1/ACP2 when Path is empty.
	Label string

	// Group + ID address an ACP1 object when Label and Path are empty.
	Group string
	ID    int
}

// Value is a decoded object value. Exactly one of the typed fields is set
// according to Kind. Raw holds the wire bytes for round-trip fidelity.
type Value struct {
	Kind ValueKind `json:"kind"`
	Raw  []byte    `json:"raw,omitempty"`

	Bool   bool    `json:"-"`
	Int    int64   `json:"-"`
	Uint   uint64  `json:"-"`
	Float  float64 `json:"-"`
	Str    string  `json:"-"`
	IPAddr [4]byte `json:"-"`
	Enum   uint8   `json:"-"`

	// SlotStatus is populated for KindFrame — one entry per slot in
	// the frame, indexed from 0.
	SlotStatus []SlotStatus `json:"-"`
}

// UnmarshalJSON parses the envelope shape produced by MarshalJSON and
// populates the right typed field based on Kind. Unknown fields are
// ignored to allow forward-compatible additions.
func (v *Value) UnmarshalJSON(data []byte) error {
	type envelope struct {
		Kind       ValueKind    `json:"kind"`
		Raw        []byte       `json:"raw"`
		Bool       *bool        `json:"bool"`
		Int        *int64       `json:"int"`
		Uint       *uint64      `json:"uint"`
		Float      *float64     `json:"float"`
		Str        *string      `json:"str"`
		IP         string       `json:"ip"`
		Enum       *uint8       `json:"enum"`
		SlotStatus []SlotStatus `json:"slot_status"`
	}
	var e envelope
	if err := json.Unmarshal(data, &e); err != nil {
		return err
	}
	v.Kind = e.Kind
	v.Raw = e.Raw
	if e.Bool != nil {
		v.Bool = *e.Bool
	}
	if e.Int != nil {
		v.Int = *e.Int
	}
	if e.Uint != nil {
		v.Uint = *e.Uint
	}
	if e.Float != nil {
		v.Float = *e.Float
	}
	if e.Str != nil {
		v.Str = *e.Str
	}
	if e.IP != "" {
		var a, b, c, d uint8
		if _, err := fmt.Sscanf(e.IP, "%d.%d.%d.%d", &a, &b, &c, &d); err == nil {
			v.IPAddr = [4]byte{a, b, c, d}
		}
	}
	if e.Enum != nil {
		v.Enum = *e.Enum
	}
	v.SlotStatus = e.SlotStatus
	return nil
}

// MarshalJSON emits a clean payload with only the fields relevant to
// the Value's Kind. This keeps JSON / YAML output readable and small.
// IsZero reports whether the Value is empty (no kind set).
func (v Value) IsZero() bool {
	return v.Kind == KindUnknown && v.Raw == nil
}

func (v Value) MarshalJSON() ([]byte, error) {
	// Omit zero values entirely — used by cache files where values are stripped.
	if v.IsZero() {
		return []byte("null"), nil
	}
	type envelope struct {
		Kind       string       `json:"kind"`
		Raw        []byte       `json:"raw,omitempty"`
		Bool       *bool        `json:"bool,omitempty"`
		Int        *int64       `json:"int,omitempty"`
		Uint       *uint64      `json:"uint,omitempty"`
		Float      *float64     `json:"float,omitempty"`
		Str        *string      `json:"str,omitempty"`
		IP         string       `json:"ip,omitempty"`
		Enum       *uint8       `json:"enum,omitempty"`
		SlotStatus []SlotStatus `json:"slot_status,omitempty"`
	}
	e := envelope{Kind: v.Kind.String(), Raw: v.Raw}
	switch v.Kind {
	case KindBool:
		b := v.Bool
		e.Bool = &b
	case KindInt:
		n := v.Int
		e.Int = &n
	case KindUint:
		n := v.Uint
		e.Uint = &n
	case KindFloat:
		f := v.Float
		e.Float = &f
	case KindEnum:
		idx := v.Enum
		e.Enum = &idx
		if v.Str != "" {
			s := v.Str
			e.Str = &s
		}
	case KindString:
		s := v.Str
		e.Str = &s
	case KindIPAddr:
		e.IP = fmt.Sprintf("%d.%d.%d.%d",
			v.IPAddr[0], v.IPAddr[1], v.IPAddr[2], v.IPAddr[3])
	case KindAlarm:
		n := v.Uint
		e.Uint = &n
	case KindFrame:
		e.SlotStatus = v.SlotStatus
	}
	return json.Marshal(e)
}

// Event is a decoded announcement forwarded by Subscribe.
type Event struct {
	Slot      int
	Group     string
	ID        int
	Label     string
	Value     Value
	Timestamp time.Time
}

// EventFunc is the callback signature used by Subscribe. It must not block.
type EventFunc func(Event)

// Protocol is the only interface the rest of the system sees. Every plugin
// in internal/protocol/{name}/ implements this.
type Protocol interface {
	// Connect establishes the transport to the device. For ACP1 UDP this
	// just binds and peer-filters the socket; for TCP/AN2 it completes the
	// handshake. Must be callable more than once (reconnect).
	Connect(ctx context.Context, ip string, port int) error

	// Disconnect tears down the transport. Safe to call on a disconnected
	// instance.
	Disconnect() error

	// GetDeviceInfo fetches high-level device metadata (slot count, pver).
	GetDeviceInfo(ctx context.Context) (DeviceInfo, error)

	// GetSlotInfo returns per-slot status + identity for one slot.
	GetSlotInfo(ctx context.Context, slot int) (SlotInfo, error)

	// Walk enumerates every object on the given slot, building the label
	// map used for all subsequent label-addressed calls. Caller-visible
	// order: identity, control, status, alarm, frame.
	Walk(ctx context.Context, slot int) ([]Object, error)

	// GetValue reads one object value. The plugin resolves the request's
	// Label (preferred) or (Group, ID) against the walker's label map.
	GetValue(ctx context.Context, req ValueRequest) (Value, error)

	// SetValue writes one object value. Returns the device-confirmed value
	// (set methods echo the stored value per spec method overview).
	SetValue(ctx context.Context, req ValueRequest, val Value) (Value, error)

	// Subscribe registers a listener for announcements matching req.
	// An empty Label/Group/ID means "all announcements on this slot".
	Subscribe(req ValueRequest, fn EventFunc) error

	// Unsubscribe removes a previously-registered listener.
	Unsubscribe(req ValueRequest) error
}

// ProtocolMeta is the static descriptor a plugin publishes at registration.
type ProtocolMeta struct {
	Name        string
	DefaultPort int
	Description string
}

// ProtocolFactory builds Protocol instances. Registered via Register() in
// each plugin's init().
type ProtocolFactory interface {
	Meta() ProtocolMeta
	New(logger *slog.Logger) Protocol
}
