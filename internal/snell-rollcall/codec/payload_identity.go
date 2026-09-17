package codec

import (
	"encoding/binary"
	"fmt"
)

// Wire sizes of the identity structures.
const (
	VersionSize    = 4  // spec 11.3.3 VERSION_STR
	IDSize         = 28 // spec 11.3.2 ID_STR
	StatusSize     = 4  // spec 11.3.1 STATUS_STR
	DeviceInfoSize = 40 // spec 11.3.4 DEVICEINFO_STR
)

// ProtocolVersion is the value every unit reports in DeviceInfo. The vendor
// library writes it and never reads it, so it is informational only: there is
// no version negotiation in RollCall. Capability is carried by service bits.
const ProtocolVersion uint16 = 3

// Version identifies a unit's software (spec 11.3.3).
//
// Major, Minor and Alpha match the version shown in the unit's own menus.
// CmdSet is the one that matters to us: a unit's ID plus CmdSet uniquely
// determines its command set, so together they key the device-model cache.
// Alpha is a character, a space for a purely numeric version.
type Version struct {
	Major  uint8
	Minor  uint8
	Alpha  uint8
	CmdSet uint8
}

func (v Version) String() string {
	a := v.Alpha
	if a < 0x20 || a > 0x7E {
		a = ' '
	}
	return fmt.Sprintf("%d.%d%c.cs%d", v.Major, v.Minor, a, v.CmdSet)
}

func (v Version) appendTo(dst []byte) []byte {
	return append(dst, v.Major, v.Minor, v.Alpha, v.CmdSet)
}

// versionAt reads a version from a slice the caller has already sized.
func versionAt(b []byte) Version {
	return Version{Major: b[0], Minor: b[1], Alpha: b[2], CmdSet: b[3]}
}

// ID is a unit's identity: what it offers, what type it is, what it runs and
// what the user called it (spec 11.3.2, payload of RetID).
type ID struct {
	Services Service
	TypeID   uint16 // assigned by the vendor; ~700 are allocated in rc3id.h
	Version  Version
	Name     string // user-editable, 19 usable bytes
}

func (d ID) String() string {
	return fmt.Sprintf("id=%d %s %q svc=%s", d.TypeID, d.Version, d.Name, d.Services)
}

// AppendTo appends the 28-byte wire form.
func (d ID) AppendTo(dst []byte) ([]byte, error) {
	var head [4]byte
	binary.BigEndian.PutUint16(head[0:2], uint16(d.Services))
	binary.BigEndian.PutUint16(head[2:4], d.TypeID)
	dst = append(dst, head[:]...)
	dst = d.Version.appendTo(dst)
	return appendFixedString(dst, d.Name, MaxTextSize)
}

// DecodeID reads an ID_STR from the front of b.
func DecodeID(b []byte) (ID, error) {
	if err := need(b, IDSize, "ID", ""); err != nil {
		return ID{}, err
	}
	return idAt(b), nil
}

// idAt reads an ID_STR from a slice the caller has already sized.
func idAt(b []byte) ID {
	return ID{
		Services: Service(binary.BigEndian.Uint16(b[0:2])),
		TypeID:   binary.BigEndian.Uint16(b[2:4]),
		Version:  versionAt(b[4:8]),
		Name:     fixedString(b[8:28]),
	}
}

// UnitStatus reports which services are busy and the unit's own state
// (spec 11.3.1, payload of RetStat).
type UnitStatus struct {
	ServiceStatus Service // a set bit means that service is busy
	Status        Status
}

func (s UnitStatus) String() string {
	return fmt.Sprintf("busy=%s status=%s", s.ServiceStatus, s.Status)
}

// AppendTo appends the 4-byte wire form.
func (s UnitStatus) AppendTo(dst []byte) []byte {
	var b [StatusSize]byte
	binary.BigEndian.PutUint16(b[0:2], uint16(s.ServiceStatus))
	binary.BigEndian.PutUint16(b[2:4], uint16(s.Status))
	return append(dst, b[:]...)
}

// DecodeUnitStatus reads a STATUS_STR from the front of b.
func DecodeUnitStatus(b []byte) (UnitStatus, error) {
	if err := need(b, StatusSize, "UnitStatus", ""); err != nil {
		return UnitStatus{}, err
	}
	return unitStatusAt(b), nil
}

// unitStatusAt reads a STATUS_STR from a slice the caller has already sized.
func unitStatusAt(b []byte) UnitStatus {
	return UnitStatus{
		ServiceStatus: Service(binary.BigEndian.Uint16(b[0:2])),
		Status:        Status(binary.BigEndian.Uint16(b[2:4])),
	}
}

// DeviceInfo describes one device (spec 11.3.4). It is the payload of
// RetDevInfo and Iam, and is embedded in Call.
//
// The Address here is never rewritten as the message crosses a bridge, so its
// Net is always zero and must not be used as a route. Take the route from the
// message header instead (spec 11.3.4 note).
type DeviceInfo struct {
	ProtocolVersion uint16
	Address         Address
	ID              ID
	Status          UnitStatus
}

func (d DeviceInfo) String() string {
	return fmt.Sprintf("%s %s %s", d.Address, d.ID, d.Status)
}

// AppendTo appends the 40-byte wire form.
func (d DeviceInfo) AppendTo(dst []byte) ([]byte, error) {
	var pv [2]byte
	binary.BigEndian.PutUint16(pv[:], d.ProtocolVersion)
	dst = append(dst, pv[:]...)
	dst = d.Address.AppendTo(dst)
	dst, err := d.ID.AppendTo(dst)
	if err != nil {
		return nil, err
	}
	return d.Status.AppendTo(dst), nil
}

// DecodeDeviceInfo reads a DEVICEINFO_STR from the front of b.
func DecodeDeviceInfo(b []byte) (DeviceInfo, error) {
	if err := need(b, DeviceInfoSize, "DeviceInfo", ""); err != nil {
		return DeviceInfo{}, err
	}
	return deviceInfoAt(b), nil
}

// deviceInfoAt reads a DEVICEINFO_STR from a slice the caller has already
// sized. Connect embeds one, and reads it through here.
func deviceInfoAt(b []byte) DeviceInfo {
	return DeviceInfo{
		ProtocolVersion: binary.BigEndian.Uint16(b[0:2]),
		Address:         addressAt(b[2:8]),
		ID:              idAt(b[8:36]),
		Status:          unitStatusAt(b[36:40]),
	}
}
