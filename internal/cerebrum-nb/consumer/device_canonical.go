package cerebrumnb

// D2 — the DEVICE domain on the canonical Tree/DM contract (ADR-0022,
// owner-agreed design 2026-08-17): GetValue / Subscribe / Unsubscribe
// speak the ONE dotted path grammar every protocol uses:
//
//	DEVICE.SUB.OBJECT.PATH…   e.g. "bm-n-nncvt-001 .1.PROCESSING AUDIO.AUDIO DELAY.BANK 1.Delay"
//
// Wire addressing stays internal: exact DEVICE_NAME (incl. the live
// trailing-whitespace quirk — names are taken verbatim, never trimmed),
// SUB_DEVICE index, root-stripped OBJECT. The RM/router domain rides the
// Matrix template (probel model) — this file is the Tree/DM half.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"dhs/internal/cerebrum-nb/codec"
	"dhs/internal/consumer"
)

// deviceValueTimeout bounds the VALUE obtain / subscribe round-trips
// issued through the canonical interface (which carries no context on
// Subscribe/Unsubscribe). Package seam so tests can shrink it.
var deviceValueTimeout = 10 * time.Second

// GetValue reads one device object through the §5.4.3 VALUE obtain.
// req.Path is the canonical DEVICE.SUB.OBJECT… form.
func (p *Plugin) GetValue(ctx context.Context, req consumer.ValueRequest) (consumer.Value, error) {
	sess := p.Session()
	if sess == nil {
		return consumer.Value{}, fmt.Errorf("cerebrum-nb: not connected")
	}
	if req.Path == "" {
		return consumer.Value{}, fmt.Errorf("cerebrum-nb: get-value requires Path of form 'device.sub_device.object'")
	}
	devName, sub, obj, err := splitDevicePath(req.Path)
	if err != nil {
		return consumer.Value{}, err
	}
	ov, err := obtainDeviceValue(ctx, sess, devName, sub, obj)
	if err != nil {
		return consumer.Value{}, err
	}
	if !ov.Available {
		return consumer.Value{}, fmt.Errorf("cerebrum-nb: object %q not available on %s.%s", obj, devName, sub)
	}
	return deviceValueToCanonical(ov), nil
}

// deviceSub tracks one canonical VALUE subscription so Unsubscribe can
// cancel both the local dispatch and the wire row.
type deviceSub struct {
	sub  *Subscription
	item *codec.DeviceChange
}

// Subscribe registers fn for one device object's VALUE changes (§5.4
// exact-leaf rule: wildcards are refused by live servers; group
// subscribes do not cascade — subscribe the leaf you want).
func (p *Plugin) Subscribe(req consumer.ValueRequest, fn consumer.EventFunc) error {
	sess := p.Session()
	if sess == nil {
		return fmt.Errorf("cerebrum-nb: not connected")
	}
	if req.Path == "" {
		return fmt.Errorf("cerebrum-nb: subscribe requires Path of form 'device.sub_device.object'")
	}
	devName, sub, obj, err := splitDevicePath(req.Path)
	if err != nil {
		return err
	}

	path := req.Path
	watch := sess.OnEvent(codec.KindDeviceChange, func(f *codec.Frame) {
		d := f.Device
		if d == nil || d.Type != "VALUE" || d.DeviceName != devName {
			return
		}
		for i := range d.ObjectValues {
			ov := &d.ObjectValues[i]
			if ov.Object != obj {
				continue
			}
			fn(consumer.Event{
				Path:      path,
				Label:     ov.Label,
				Unit:      ov.Units,
				Access:    deviceAccessBits(ov),
				Value:     deviceValueToCanonical(ov),
				Timestamp: time.Now(),
			})
		}
	})

	item := &codec.DeviceChange{Type: "VALUE", DeviceName: devName, SubDevice: sub, Object: obj}
	ctx, cancel := context.WithTimeout(context.Background(), deviceValueTimeout)
	defer cancel()
	if err := sess.Subscribe(ctx, []codec.SubItem{item}); err != nil {
		sess.Cancel(watch)
		return err
	}

	p.devSubMu.Lock()
	if p.devSubs == nil {
		p.devSubs = map[string]*deviceSub{}
	}
	if old, dup := p.devSubs[path]; dup {
		// Replace: cancel the previous local dispatch (wire row is
		// identical, the server keeps one subscription per row).
		sess.Cancel(old.sub)
	}
	p.devSubs[path] = &deviceSub{sub: watch, item: item}
	p.devSubMu.Unlock()
	return nil
}

// Unsubscribe cancels the canonical VALUE subscription for req.Path.
func (p *Plugin) Unsubscribe(req consumer.ValueRequest) error {
	sess := p.Session()
	if sess == nil {
		return fmt.Errorf("cerebrum-nb: not connected")
	}
	p.devSubMu.Lock()
	ds := p.devSubs[req.Path]
	delete(p.devSubs, req.Path)
	p.devSubMu.Unlock()
	if ds == nil {
		return fmt.Errorf("cerebrum-nb: no subscription for path %q", req.Path)
	}
	sess.Cancel(ds.sub)
	ctx, cancel := context.WithTimeout(context.Background(), deviceValueTimeout)
	defer cancel()
	return sess.Unsubscribe(ctx, []codec.SubItem{ds.item})
}

// obtainDeviceValue issues one §5.4.3 VALUE obtain and returns the
// OBJECT_VALUE matching obj (or the first one — a scalar leaf echoes a
// single self row, live-verified 2026-08-16).
func obtainDeviceValue(ctx context.Context, sess *Session, devName, sub, obj string) (*codec.DeviceObjectValue, error) {
	deadline := deviceValueTimeout
	if dl, ok := ctx.Deadline(); ok {
		if d := time.Until(dl); d < deadline {
			deadline = d
		}
	}

	got := make(chan *codec.DeviceObjectValue, 1)
	watch := sess.OnEvent(codec.KindDeviceChange, func(f *codec.Frame) {
		d := f.Device
		if d == nil || d.Type != "VALUE" || d.DeviceName != devName {
			return
		}
		for i := range d.ObjectValues {
			if d.ObjectValues[i].Object == obj {
				select {
				case got <- &d.ObjectValues[i]:
				default:
				}
				return
			}
		}
		if len(d.ObjectValues) > 0 {
			select {
			case got <- &d.ObjectValues[0]:
			default:
			}
		}
	})
	defer sess.Cancel(watch)

	obCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	item := &codec.DeviceChange{Type: "VALUE", DeviceName: devName, SubDevice: sub, Object: obj}
	if err := sess.Obtain(obCtx, []codec.SubItem{item}); err != nil {
		return nil, err
	}
	select {
	case ov := <-got:
		return ov, nil
	case <-obCtx.Done():
		return nil, fmt.Errorf("cerebrum-nb: no VALUE reply for %s.%s.%s: %w", devName, sub, obj, obCtx.Err())
	}
}

// deviceAccessBits maps the descriptor's Readable/Writable onto the
// canonical access bitmask (read=1, write=2).
func deviceAccessBits(ov *codec.DeviceObjectValue) uint8 {
	var a uint8
	if ov.Readable {
		a |= 0x01
	}
	if ov.Writable {
		a |= 0x02
	}
	return a
}

// deviceValueToCanonical maps one OBJECT_VALUE onto the canonical Value
// model by its wire DATA_TYPE. Unparsable numerics degrade to the raw
// string (KindString) rather than fabricating a zero.
func deviceValueToCanonical(ov *codec.DeviceObjectValue) consumer.Value {
	switch strings.ToUpper(ov.DataType) {
	case "FLOAT", "DOUBLE":
		if f, err := strconv.ParseFloat(strings.TrimSpace(ov.Value), 64); err == nil {
			return consumer.Value{Kind: consumer.KindFloat, Float: f}
		}
	case "INTEGER", "INT", "LONG":
		if n, err := strconv.ParseInt(strings.TrimSpace(ov.Value), 10, 64); err == nil {
			return consumer.Value{Kind: consumer.KindInt, Int: n}
		}
	case "BOOL", "BOOLEAN":
		switch strings.TrimSpace(ov.Value) {
		case "1", "true", "TRUE", "True":
			return consumer.Value{Kind: consumer.KindBool, Bool: true}
		case "0", "false", "FALSE", "False":
			return consumer.Value{Kind: consumer.KindBool, Bool: false}
		}
	case "ENUM":
		v := consumer.Value{Kind: consumer.KindEnum, Str: ov.Value}
		if n, err := strconv.ParseUint(strings.TrimSpace(ov.Value), 10, 8); err == nil {
			v.Enum = uint8(n)
		}
		return v
	}
	return consumer.Value{Kind: consumer.KindString, Str: ov.Value}
}
