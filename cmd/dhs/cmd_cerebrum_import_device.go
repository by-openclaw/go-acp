package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"dhs/internal/cerebrum-nb/codec"
	cerebrum "dhs/internal/cerebrum-nb/consumer"
	"dhs/internal/consumer"
	"dhs/internal/export"
)

// cerebrumDeviceRow is one object of a device snapshot the import converges.
type cerebrumDeviceRow struct {
	Object string // dotted object path, as export writes it
	Want   string // the value the file asks for
	IsEnum bool   // Want is an enumeration name (IS_ENUM, §4.4.1)
}

// cerebrumDeviceRows reads the rows of an `export --device` snapshot (json,
// yaml or csv) that the import may write. The file may hold any subset of
// the device: only its rows are in scope. Read-only rows are left out and
// counted; a writable row with no value is an error.
func cerebrumDeviceRows(snap *export.Snapshot) (rows []cerebrumDeviceRow, readOnly int, err error) {
	for _, sd := range snap.Slots {
		for _, o := range sd.Objects {
			// The path is the object: json and yaml nest the rows by it and
			// keep only the leaf as the label.
			name := strings.Join(o.Path, ".")
			if name == "" {
				name = o.Label
			}
			if o.Access&2 == 0 {
				readOnly++
				continue
			}
			r := cerebrumDeviceRow{Object: name}
			switch o.Value.Kind {
			case consumer.KindEnum:
				// The server reports an ENUM by its name; the name goes back
				// with IS_ENUM. A row carrying only an index is named from the
				// file's own enumeration.
				r.Want, r.IsEnum = o.Value.Str, true
				if r.Want == "" && int(o.Value.Enum) < len(o.EnumItems) {
					r.Want = o.EnumItems[o.Value.Enum]
				}
			case consumer.KindUnknown:
				return nil, 0, &consumer.ValidationError{Field: name, Reason: "writable row without a value"}
			default:
				r.Want = valueText(o.Value)
			}
			rows = append(rows, r)
		}
	}
	return rows, readOnly, nil
}

// cerebrumDeviceChange is one row that differs from the live object.
type cerebrumDeviceChange struct {
	row  cerebrumDeviceRow
	from string
	to   string // the bounded value that is sent
}

// cerebrumImportDevice converges one sub-device on a snapshot file: every
// row is read live with its descriptor, judged by MIN / MAX / ENUM_LIST
// (cerebrumBoundValue), and sent only when it differs. A row the object
// cannot hold refuses the whole file before anything is sent; a clamped row
// is written as its bound and the run exits 2.
func cerebrumImportDevice(sess *cerebrum.Session, cf *cerebrumFlags, device string, byName bool, subDev, path string, check, jsonOut bool, logw io.Writer) error {
	snap, err := export.LoadSnapshot(path)
	if err != nil {
		return err
	}
	rows, readOnly, err := cerebrumDeviceRows(snap)
	if err != nil {
		return err
	}
	addr := func(dc *codec.DeviceChange) *codec.DeviceChange {
		if byName {
			dc.DeviceName = device
		} else {
			dc.IPAddress = device
		}
		return dc
	}
	var changes []cerebrumDeviceChange
	var refused, clamped []string
	read := func(object string) (*codec.DeviceObjectValue, bool) {
		got, gerr := obtainSingleDeviceChange(sess, cf.timeout, addr(&codec.DeviceChange{Type: "VALUE", SubDevice: subDev, Object: object}), "VALUE")
		if gerr != nil || got == nil || got.Device == nil || got.Device.ObjectValue == nil || !got.Device.ObjectValue.Available {
			return nil, false
		}
		return got.Device.ObjectValue, true
	}
	for _, r := range rows {
		ov, ok := read(r.Object)
		if !ok {
			refused = append(refused, fmt.Sprintf("%s: not readable on the device", r.Object))
			continue
		}
		b, berr := cerebrumBoundValue(ov, r.Want, r.IsEnum)
		if berr != nil {
			refused = append(refused, berr.Error())
			continue
		}
		// A clamped row is reported even when the bound is already live:
		// the file still asks for a value the object cannot hold.
		if b.Clamped != "" {
			clamped = append(clamped, r.Object+": "+b.Clamped)
		}
		if cerebrumSameValue(ov.Value, b.Value) {
			continue
		}
		changes = append(changes, cerebrumDeviceChange{row: r, from: ov.Value, to: b.Value})
	}
	if len(refused) > 0 {
		return &consumer.ValidationError{Field: "import --device",
			Reason: fmt.Sprintf("%d row(s) cannot be written as given; nothing sent:\n  %s", len(refused), strings.Join(refused, "\n  "))}
	}

	diffs := make([]ensureDiff, 0, len(changes))
	for _, c := range changes {
		diffs = append(diffs, ensureDiff{Field: fmt.Sprintf("value.%s.%s.%s", strings.TrimSpace(device), subDev, c.row.Object), From: c.from, To: c.to})
	}
	changed := len(changes) > 0
	summary := fmt.Sprintf("%d of %d writable row(s), %d read-only row(s) skipped", len(changes), len(rows), readOnly)
	clampErr := func(dry bool) error {
		if len(clamped) == 0 {
			return nil
		}
		verb := "wrote"
		if dry {
			verb = "would write"
		}
		return &consumer.ValidationError{Field: "import --device",
			Reason: fmt.Sprintf("%d row(s) out of bounds, %s the bound instead:\n  %s", len(clamped), verb, strings.Join(clamped, "\n  "))}
	}

	if check {
		for _, c := range changes {
			_, _ = fmt.Fprintf(logw, "[would-set] %s: %q -> %q\n", c.row.Object, c.from, c.to)
		}
		_, _ = fmt.Fprintf(logw, "cerebrum-nb import --device --check: would_change=%s — nothing sent\n", summary)
		if jsonOut {
			if err := emitEnsure(true, ensureResult{WouldChange: &changed, Diff: diffs}); err != nil {
				return err
			}
		}
		return clampErr(true)
	}
	fails, unapplied := 0, []string{}
	for _, c := range changes {
		body := &codec.DeviceAction{Type: "SET_VALUE", SubDevice: subDev, Object: c.row.Object, Value: c.to}
		if byName {
			body.DeviceName = device
		} else {
			body.IPAddress = device
		}
		if c.row.IsEnum {
			body.IsEnum = true
		}
		ctx, cancel := context.WithTimeout(context.Background(), cf.timeout)
		err := sess.Action(ctx, body)
		cancel()
		if err != nil {
			_, _ = fmt.Fprintf(logw, "[set] NACK %s: %q -> %q reason=%s\n", c.row.Object, c.from, c.to, err)
			fails++
			continue
		}
		// The ACK says the action was understood, not applied: the object
		// is read back right after it.
		if now, ok := read(c.row.Object); !ok || !cerebrumSameValue(now.Value, c.to) {
			live := "unreadable"
			if ok {
				live = fmt.Sprintf("%q", now.Value)
			}
			_, _ = fmt.Fprintf(logw, "[set] ACK  %s: %q -> %q, read back %s: not applied\n", c.row.Object, c.from, c.to, live)
			unapplied = append(unapplied, c.row.Object)
			continue
		}
		_, _ = fmt.Fprintf(logw, "[set] OK   %s: %q -> %q\n", c.row.Object, c.from, c.to)
	}
	if fails > 0 || len(unapplied) > 0 {
		return fmt.Errorf("cerebrum-nb import --device: of %d write(s), %d refused by the server, %d acknowledged and not applied (%s)",
			len(changes), fails, len(unapplied), strings.Join(unapplied, ", "))
	}
	_, _ = fmt.Fprintf(logw, "cerebrum-nb import --device: changed=%s; run again to verify 0\n", summary)
	if jsonOut {
		if err := emitEnsure(true, ensureResult{Changed: &changed, Diff: diffs}); err != nil {
			return err
		}
	}
	return clampErr(false)
}

// cerebrumSameValue compares a live value with a wanted one, numerically
// when both are numbers: the server sends "5.000000" for a 5.
func cerebrumSameValue(a, b string) bool {
	if a == b {
		return true
	}
	fa, ea := strconv.ParseFloat(strings.TrimSpace(a), 64)
	fb, eb := strconv.ParseFloat(strings.TrimSpace(b), 64)
	return ea == nil && eb == nil && fa == fb
}
