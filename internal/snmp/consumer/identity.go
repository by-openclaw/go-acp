package consumer

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"strconv"
	"strings"

	"dhs/internal/snmp/codec"
	"dhs/internal/snmp/mib"
)

// deviceIdentity is what a device says it is, from the best source that
// answered: its vendor's objects, then the standard ENTITY-MIB chassis row,
// then the system group. Any device gets as far as its source allows, and
// the constructor comes from its enterprise number, with no MIB at all, so
// that a device nobody has a table for still says whose MIB to go and find.
type deviceIdentity struct {
	constructor string
	pen         uint32
	hasPEN      bool
	model       string
	serial      string
	software    string
	firmware    string
	description string
	objectID    codec.OID
	table       string
	// noMIB says no table in use names this device's enterprise branch.
	noMIB bool
}

// key is Model@SwRev, the name of the device's DM (ADR-0022), or the model
// alone when no version was found.
func (d *deviceIdentity) key() string {
	if d.model != "" && d.software != "" {
		return d.model + "@" + d.software
	}
	return d.model
}

// next is the line that sends an operator to the missing MIB.
func (d *deviceIdentity) next() string {
	who := d.constructor
	if who == "" {
		who = "not in the IANA registry"
	}
	return fmt.Sprintf("%s (PEN %d): no MIB loaded. Look in github.com/by-protocol/mib, "+
		"then https://mibbrowser.online/mibs/ (search page mibdb_search.php)", who, d.pen)
}

// fields is everything known, as `info` prints it for the live device. An
// empty field is left out: a device that does not say is not a device that
// said "".
func (d *deviceIdentity) fields() map[string]string {
	out := d.modelFields()
	maps.Copy(out, d.unitFields())
	if d.noMIB {
		out["next"] = d.next()
	}
	return out
}

// modelFields is what every unit of one Model@SwRev shares, so it is what
// the DM may carry: ADR-0022, the DM answers what.
func (d *deviceIdentity) modelFields() map[string]string {
	out := map[string]string{}
	addField(out, "constructor", d.constructor)
	addField(out, "model", d.model)
	addField(out, "software", d.software)
	addField(out, "table", d.table)
	if len(d.objectID) > 0 {
		addField(out, "sys_object_id", d.objectID.String())
	}
	if d.hasPEN {
		addField(out, "pen", strconv.FormatUint(uint64(d.pen), 10))
	}
	return out
}

// unitFields is what one box says of itself and the next box of the same
// model need not: a serial, the boot firmware the switch was built with, a
// sysDescr that may name its host. It goes in the device's manifest.
func (d *deviceIdentity) unitFields() map[string]string {
	out := map[string]string{}
	addField(out, "serial", d.serial)
	addField(out, "firmware", d.firmware)
	addField(out, "description", d.description)
	return out
}

func addField(out map[string]string, key, value string) {
	if value != "" {
		out[key] = value
	}
}

// identify reads the identity once a connection. An agent that did not
// answer sysDescr is not remembered: that was the path, not the device.
func (p *Plugin) identify(ctx context.Context) (*deviceIdentity, error) {
	sess, err := p.session()
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	known, oid, table := p.ident, p.sysOID, p.tableName
	p.mu.Unlock()
	if known != nil {
		return known, nil
	}
	id := &deviceIdentity{objectID: oid, table: table}
	if id.pen, id.hasPEN = mib.EnterpriseOf(oid); id.hasPEN {
		id.constructor, _ = mib.Enterprise(id.pen)
		branch := append(append(codec.OID{}, mib.Enterprises...), id.pen)
		id.noMIB = len(p.Table().Under(branch)) == 0
	}
	descr, answered := readText(ctx, sess, sysDescr)
	id.description = descr

	p.fromVendorTable(ctx, sess, id)
	p.fromChassis(ctx, sess, id)
	p.fromSystemGroup(id)

	if answered {
		p.mu.Lock()
		p.ident = id
		p.mu.Unlock()
	}
	return id, nil
}

// fromVendorTable is the vendor's own branch, read by the convention every
// MIB in this lab follows: an object whose name ends in Model, one ending in
// a version, one in a serial number. What comes back is the device's own
// words. With no model there is no vendor identity: a version alone would
// be some version of something.
func (p *Plugin) fromVendorTable(ctx context.Context, sess *Session, id *deviceIdentity) {
	id.model = p.firstNamed(ctx, sess, "UnitModel", "Model", "UnitName", "ProductName")
	if id.model == "" {
		return
	}
	id.software = p.firstNamed(ctx, sess, "SoftwareCurrentVersion", "CurrentVersion",
		"SoftwareVersion", "FirmwareVersion", "Version")
	id.serial = p.firstNamed(ctx, sess, "SerialNumber", "SerialNum", "SerialNo")
}

var errChassisFound = errors.New("snmp: chassis row found")

// fromChassis is the standard table a device with no vendor table may still
// keep: the ENTITY-MIB row of class chassis. It fills what the vendor's
// objects left empty, the model and its software together, because a
// version only means something beside the model it came from.
func (p *Plugin) fromChassis(ctx context.Context, sess *Session, id *deviceIdentity) {
	if id.model != "" && id.software != "" && id.serial != "" {
		return
	}
	var row codec.OID
	_ = sess.Walk(ctx, mib.EntPhysicalClass, func(vb codec.VarBind) error {
		if vb.Value.Type != codec.TypeInteger || vb.Value.Int != mib.EntPhysicalChassis {
			return nil
		}
		row = vb.Name[len(mib.EntPhysicalClass):]
		return errChassisFound
	})
	if row == nil {
		return
	}
	read := func(column codec.OID) string {
		text, _ := readText(ctx, sess, column.Append(row...))
		return text
	}
	if id.model == "" {
		id.model = read(mib.EntPhysicalModelName)
		id.software = read(mib.EntPhysicalSoftwareRev)
	}
	if id.serial == "" {
		id.serial = read(mib.EntPhysicalSerialNum)
	}
	id.firmware = read(mib.EntPhysicalFirmwareRev)
	if id.constructor == "" {
		id.constructor = read(mib.EntPhysicalMfgName)
	}
	if id.software == "" {
		id.software = id.firmware
	}
}

// versionInDescr finds the version sysDescr states in words, the one thing
// about it that can be read without knowing the vendor's sentence.
var versionInDescr = regexp.MustCompile(`(?i)\bversion\s+([0-9A-Za-z][^\s,;]*)`)

// fromSystemGroup is the last source, and every agent has it. sysObjectID is
// the one name of the model that is the same on every unit of it: the
// table's word for it where a table has one, the number where none does.
func (p *Plugin) fromSystemGroup(id *deviceIdentity) {
	if id.model == "" && len(id.objectID) > 0 {
		id.model = id.objectID.String()
		if def, rest := p.Table().Lookup(id.objectID); def != nil && len(rest) == 0 {
			id.model = def.Name
		}
	}
	if id.software == "" {
		if m := versionInDescr.FindStringSubmatch(id.description); m != nil {
			id.software = m[1]
		}
	}
}

// readText reads one text object. The second result says the agent answered
// at all, whatever it answered with.
func readText(ctx context.Context, sess *Session, name codec.OID) (string, bool) {
	binds, err := sess.Get(ctx, name)
	if err != nil {
		return "", errors.Is(err, &StatusError{Status: codec.NoSuchName})
	}
	if len(binds) == 0 || binds[0].Value.Type != codec.TypeOctetString {
		return "", true
	}
	return strings.TrimSpace(string(binds[0].Value.Bytes)), true
}
