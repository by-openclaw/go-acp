package rollcall

import (
	"context"
	"fmt"

	"dhs/internal/consumer"
	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
)

// GetValue reads one object.
//
// The request is resolved against the walked menu, which is walked on demand
// if this is the first thing asked of the slot. That is what lets a caller
// name an object by label without knowing its command number.
func (p *Plugin) GetValue(ctx context.Context, req consumer.ValueRequest) (consumer.Value, error) {
	line, s, err := p.locate(ctx, req)
	if err != nil {
		return consumer.Value{}, err
	}

	if s.Uses32Bit() {
		return p.get32(ctx, s, line)
	}
	return p.get16(ctx, s, line)
}

func (p *Plugin) get16(ctx context.Context, s *session.Session, line *menuLine) (consumer.Value, error) {
	if line.Command > 0xFFFF {
		return consumer.Value{}, fmt.Errorf(
			"rollcall: command %d needs the 32-bit generation, which this session did not negotiate",
			line.Command)
	}

	reply, err := s.Do(ctx, codec.MsgGetFStat,
		codec.GetFStat{Command: uint16(line.Command)}.AppendTo(nil))
	if err != nil {
		return consumer.Value{}, err
	}
	fs, err := codec.DecodeFuncStatus(reply.Payload)
	if err != nil {
		return consumer.Value{}, fmt.Errorf("rollcall: get %q: %w", line.Text, err)
	}
	return p.valueFrom(line, fs.Mode, fs.Value, fs.Text, fs.Data), nil
}

func (p *Plugin) get32(ctx context.Context, s *session.Session, line *menuLine) (consumer.Value, error) {
	reply, err := s.Do(ctx, codec.MsgGetValue,
		codec.GetValue{Command: line.Command}.AppendTo(nil))
	if err != nil {
		return consumer.Value{}, err
	}
	v, err := codec.DecodeValue(reply.Payload)
	if err != nil {
		return consumer.Value{}, fmt.Errorf("rollcall: get %q: %w", line.Text, err)
	}
	return p.valueFrom(line, v.Mode, v.Val, v.Text, v.Data), nil
}

// SetValue writes one object and returns what the device confirmed.
//
// The reply carries the device's own value rather than an acknowledgement, so
// a caller that asked for something out of range learns what it actually got
// without a second read. That is a property of the protocol worth relying on:
// a device clamps silently, and only the reply says so.
func (p *Plugin) SetValue(ctx context.Context, req consumer.ValueRequest, val consumer.Value) (consumer.Value, error) {
	line, s, err := p.locate(ctx, req)
	if err != nil {
		return consumer.Value{}, err
	}

	mode, num, text, err := p.encodeValue(line, val)
	if err != nil {
		return consumer.Value{}, err
	}

	if s.Uses32Bit() {
		return p.set32(ctx, s, line, mode, num, text)
	}
	return p.set16(ctx, s, line, mode, num, text)
}

func (p *Plugin) set16(ctx context.Context, s *session.Session, line *menuLine,
	mode codec.Mode, num int32, text string) (consumer.Value, error) {

	if line.Command > 0xFFFF {
		return consumer.Value{}, fmt.Errorf(
			"rollcall: command %d needs the 32-bit generation, which this session did not negotiate",
			line.Command)
	}

	payload, err := codec.FuncStatus{
		Command: uint16(line.Command),
		Mode:    mode,
		Value:   num,
		Text:    codec.TruncateFixed(text, codec.MaxTextSize),
	}.AppendTo(nil)
	if err != nil {
		return consumer.Value{}, err
	}

	reply, err := s.Do(ctx, codec.MsgSetParam, payload)
	if err != nil {
		return consumer.Value{}, err
	}
	fs, err := codec.DecodeFuncStatus(reply.Payload)
	if err != nil {
		return consumer.Value{}, fmt.Errorf("rollcall: set %q: %w", line.Text, err)
	}
	return p.valueFrom(line, fs.Mode, fs.Value, fs.Text, fs.Data), nil
}

func (p *Plugin) set32(ctx context.Context, s *session.Session, line *menuLine,
	mode codec.Mode, num int32, text string) (consumer.Value, error) {

	payload, err := codec.Value{
		Command: line.Command,
		Mode:    mode,
		Val:     num,
		Text:    text,
	}.AppendTo(nil)
	if err != nil {
		return consumer.Value{}, err
	}

	reply, err := s.Do(ctx, codec.MsgSetValue, payload)
	if err != nil {
		return consumer.Value{}, err
	}
	v, err := codec.DecodeValue(reply.Payload)
	if err != nil {
		return consumer.Value{}, fmt.Errorf("rollcall: set %q: %w", line.Text, err)
	}
	return p.valueFrom(line, v.Mode, v.Val, v.Text, v.Data), nil
}

// SetDefault asks a device to restore an object to its own default.
//
// It is a distinct operation rather than a write of a known value, because the
// default is the device's to decide. The preset flag is what asks for it, and
// the numeric field is ignored: measured against a live device, the vendor's
// own Control Panel sends zero with the flag set and gets the default back.
func (p *Plugin) SetDefault(ctx context.Context, req consumer.ValueRequest) (consumer.Value, error) {
	line, s, err := p.locate(ctx, req)
	if err != nil {
		return consumer.Value{}, err
	}
	if s.Uses32Bit() {
		return p.set32(ctx, s, line, codec.ModePreset, 0, "")
	}
	return p.set16(ctx, s, line, codec.ModePreset, 0, "")
}

// locate resolves a request to a menu line and the session to reach it on.
func (p *Plugin) locate(ctx context.Context, req consumer.ValueRequest) (*menuLine, *session.Session, error) {
	t, err := p.tree(ctx, req.Slot)
	if err != nil {
		return nil, nil, err
	}
	line, err := t.resolve(req)
	if err != nil {
		return nil, nil, err
	}
	s, err := p.session(ctx, uint8(req.Slot))
	if err != nil {
		return nil, nil, err
	}
	return line, s, nil
}

// valueFrom builds a neutral value from a reply.
//
// Both flags may be set at once and that is normal rather than exceptional: a
// device returns a checksum as the number -1686180113 and the string
// "0x9B7EEEEF", and a caller wants the string. So when a line is numeric the
// number wins, and the string is kept as the raw form for anyone who wants
// what the device meant to display.
func (p *Plugin) valueFrom(line *menuLine, mode codec.Mode, num int32, text string, data []byte) consumer.Value {
	kind := consumer.KindInt
	if line != nil && line.Style != 0 {
		kind = styleKind(line.Style)
	}

	switch {
	case mode.Has(codec.ModeData):
		return consumer.Value{Kind: consumer.KindRaw, Raw: append([]byte(nil), data...)}

	case kind == consumer.KindBool && mode.Has(codec.ModeValue):
		return consumer.Value{Kind: consumer.KindBool, Bool: num != 0, Int: int64(num)}

	case kind == consumer.KindString && mode.Has(codec.ModeString):
		return consumer.Value{Kind: consumer.KindString, Str: text}

	case mode.Has(codec.ModeValue):
		v := consumer.Value{Kind: consumer.KindInt, Int: int64(num)}
		if mode.Has(codec.ModeString) {
			// The device supplied its own rendering, which is what a display
			// should show; keep it beside the number rather than choosing.
			v.Str = text
			v.Raw = []byte(text)
		}
		return v

	case mode.Has(codec.ModeString):
		return consumer.Value{Kind: consumer.KindString, Str: text}

	default:
		// A reply that sets no flag carries nothing. Report it and return an
		// empty value of the right kind rather than an error: the object
		// exists, the device simply said nothing about it.
		if line != nil {
			p.fire(EventValueModeEmpty, fmt.Sprintf(
				"command %d answered with mode %s and no value", line.Command, mode))
		}
		return consumer.Value{Kind: kind}
	}
}

// encodeValue turns a neutral value into the mode, number and text a write
// carries.
func (p *Plugin) encodeValue(line *menuLine, val consumer.Value) (codec.Mode, int32, string, error) {
	switch val.Kind {
	case consumer.KindBool:
		n := int32(0)
		if val.Bool {
			n = 1
		}
		return codec.ModeValue, n, "", nil

	case consumer.KindInt:
		return codec.ModeValue, int32(val.Int), "", nil

	case consumer.KindUint:
		return codec.ModeValue, int32(val.Uint), "", nil

	case consumer.KindFloat:
		// A menu line carries an integer scaled by its divisor, so a caller
		// working in the displayed units is converted back into wire units
		// here rather than having to know the divisor.
		return codec.ModeValue, int32(val.Float * float64(line.Scale())), "", nil

	case consumer.KindString:
		return codec.ModeString, 0, val.Str, nil

	case consumer.KindRaw:
		return 0, 0, "", fmt.Errorf("rollcall: writing raw data is not supported on command %d", line.Command)

	case consumer.KindUnknown:
		// A caller that supplied no kind but did supply a number means the
		// number, which is the common case from a command line.
		if val.Int != 0 || val.Str == "" {
			return codec.ModeValue, int32(val.Int), "", nil
		}
		return codec.ModeString, 0, val.Str, nil

	default:
		return 0, 0, "", fmt.Errorf("rollcall: cannot write a %s value", val.Kind)
	}
}

// Display returns the unit's status lines, which are the four lines its front
// panel shows.
//
// They are not menu objects: they are a separate service whose lines are
// numbered rather than named, with two negative numbers reserved for an error
// and a warning. That is why they have their own accessor rather than
// appearing in a walk.
func (p *Plugin) Display(ctx context.Context, slot int) (map[int16]string, error) {
	s, err := p.session(ctx, uint8(slot))
	if err != nil {
		return nil, err
	}

	out := make(map[int16]string, 4)
	for line := int16(0); line < 4; line++ {
		reply, err := s.Do(ctx, codec.MsgGetDispData,
			[]byte{byte(uint16(line) >> 8), byte(uint16(line))})
		if err != nil {
			// A unit without a display service refuses the first line; that
			// is an answer rather than a failure.
			if line == 0 {
				return nil, err
			}
			break
		}
		d, err := codec.DecodeDisp(reply.Payload)
		if err != nil {
			return nil, fmt.Errorf("rollcall: display line %d: %w", line, err)
		}
		out[d.Line] = d.Text
	}
	return out, nil
}
