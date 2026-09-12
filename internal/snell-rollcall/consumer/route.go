package rollcall

import (
	"context"
	"fmt"

	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/codec/dtp"
	"dhs/internal/snell-rollcall/codec/router"
)

// Reading and changing crosspoints, once the model in router.go says where
// they live.

// Crosspoint is what a destination currently has, as the controller sees it.
type Crosspoint struct {
	// Dest identifies the destination this describes.
	Dest router.SourcePin

	// Source is what is routed to it. Zero means nothing is.
	//
	// It is the *final upstream* source: where a route crosses a tieline the
	// controller reports the far end, so this is often not the pin that was
	// written. That is the protocol working, not a fault.
	Source router.SourcePin

	// Result is the outcome of the last change to this destination, when the
	// controller supplied one. It is always present in the answer to a set and
	// optional on a read.
	Result router.RouteResult

	// HasResult says whether Result came from the controller or is a zero
	// value nobody set.
	HasResult bool
}

// Protect is the protect state of a destination.
type Protect struct {
	// On says whether the destination is protected.
	On bool

	// ID is the panel that protected it, in the range 1..1023, or zero when it
	// is not protected. A panel may only release a protect carrying its own
	// id; a master panel may release any.
	ID uint16

	// By is the name of the device holding the protect, when the controller
	// supplied one.
	By string
}

// Route reads what is routed to one destination.
//
// The destination is named by matrix, level and number, which is how the
// command space is addressed: the same destination number means a different
// thing on each level.
func (p *Plugin) Route(ctx context.Context, r *RouterInterface,
	matrix, level, dest uint32) (Crosspoint, error) {

	cmd, err := r.destCommand(matrix, level, dest, router.OffDestRoutedSrc)
	if err != nil {
		return Crosspoint{}, err
	}

	v, err := p.readValue(ctx, r.Slot, cmd)
	if err != nil {
		return Crosspoint{}, err
	}
	return decodeCrosspoint(v, router.SourcePin{
		Matrix: uint8(matrix), Level: uint8(level), Source: uint16(dest),
	})
}

// SetRoute routes a source to a destination and returns what the controller
// said about it.
//
// What comes back is not the new crosspoint. The controller answers with the
// pin that was routed *before* the change, plus a result code saying whether
// the change worked; the new pin arrives afterwards as a back-channel push. A
// caller that wants to see the result rather than believe it should subscribe
// before setting, or read again after.
//
// The protect id is optional and rarely wanted: it is a temporary override for
// local routing, and passing zero asks for an ordinary route.
func (p *Plugin) SetRoute(ctx context.Context, r *RouterInterface,
	matrix, level, dest uint32, src router.SourcePin, protectID uint16) (Crosspoint, error) {

	cmd, err := r.destCommand(matrix, level, dest, router.OffDestRoutedSrc)
	if err != nil {
		return Crosspoint{}, err
	}

	// The pin goes in a uint array, not as a bare uint: one entry to route,
	// two when a protect id comes with it.
	pins := []uint32{src.Pack()}
	if protectID != 0 {
		pins = append(pins, uint32(protectID))
	}
	// Neither encode can fail: Data Transfer Params refuse only strings that
	// are too long and more items than a packet holds, and this is one or two
	// numbers; a value in data mode has no field that can overflow.
	payload, _ := dtp.Encode(dtp.Params{dtp.Uints(pins...)}, false)
	req, _ := codec.Value{Command: cmd, Mode: codec.ModeData, Data: payload}.AppendTo(nil)

	s, err := p.session(ctx, r.Slot)
	if err != nil {
		return Crosspoint{}, err
	}

	reply, err := s.Do(ctx, codec.MsgSetValue, req)
	if err != nil {
		return Crosspoint{}, fmt.Errorf("rollcall: route %s to matrix %d level %d dest %d: %w",
			src, matrix, level, dest, err)
	}
	v, err := codec.DecodeValue(reply.Payload)
	if err != nil {
		return Crosspoint{}, fmt.Errorf("rollcall: route reply: %w", err)
	}

	xpt, err := decodeCrosspoint(v, router.SourcePin{
		Matrix: uint8(matrix), Level: uint8(level), Source: uint16(dest),
	})
	if err != nil {
		return Crosspoint{}, err
	}
	if xpt.HasResult && !xpt.Result.OK() {
		return xpt, fmt.Errorf("rollcall: route %s to matrix %d level %d dest %d: %s",
			src, matrix, level, dest, xpt.Result)
	}
	return xpt, nil
}

// Protect reads a destination's protect state.
func (p *Plugin) Protect(ctx context.Context, r *RouterInterface,
	matrix, level, dest uint32) (Protect, error) {

	cmd, err := r.destCommand(matrix, level, dest, router.OffDestProtect)
	if err != nil {
		return Protect{}, err
	}

	v, err := p.readValue(ctx, r.Slot, cmd)
	if err != nil {
		return Protect{}, err
	}

	st := router.UnpackProtectState(uint32(v.Val))
	return Protect{On: st.Protected, ID: st.DeviceID, By: v.Text}, nil
}

// SetProtect protects or releases a destination.
//
// The id is the panel's own, and it decides who may release: an ordinary panel
// may only release a protect carrying the same id, while a master panel may
// release any. A protect with no id is refused rather than sent, because a
// controller cannot attribute it and no panel could ever release it.
func (p *Plugin) SetProtect(ctx context.Context, r *RouterInterface,
	matrix, level, dest uint32, on bool, id uint16, master bool) (Protect, error) {

	if id == 0 || id > router.MaxProtectID {
		// Zero cannot be attributed and nothing could ever release it; past
		// the configured range means the word was built wrongly, and the
		// controller would attribute the protect to a panel that cannot exist.
		return Protect{}, fmt.Errorf(
			"rollcall: a protect needs a panel id in 1..%d, not %d", router.MaxProtectID, id)
	}

	cmd, err := r.destCommand(matrix, level, dest, router.OffDestProtect)
	if err != nil {
		return Protect{}, err
	}

	state := router.PackProtectState(router.ProtectState{Protected: on, DeviceID: id, Master: master})

	s, err := p.session(ctx, r.Slot)
	if err != nil {
		return Protect{}, err
	}
	// A numeric value has no field that can overflow.
	req, _ := codec.Value{Command: cmd, Mode: codec.ModeValue, Val: int32(state)}.AppendTo(nil)

	reply, err := s.Do(ctx, codec.MsgSetValue, req)
	if err != nil {
		return Protect{}, fmt.Errorf("rollcall: protect matrix %d level %d dest %d: %w",
			matrix, level, dest, err)
	}
	v, err := codec.DecodeValue(reply.Payload)
	if err != nil {
		return Protect{}, fmt.Errorf("rollcall: protect reply: %w", err)
	}

	st := router.UnpackProtectState(uint32(v.Val))
	return Protect{On: st.Protected, ID: st.DeviceID, By: v.Text}, nil
}

// FireSalvo asks the controller to run a salvo, counting from one, and reports
// how many routes it made.
//
// The count is the whole of the answer: the specification says "number of
// routes made or 0 on error" and does not distinguish an empty salvo from one
// whose routes were all refused. It was previously thrown away, which made a
// salvo that did nothing indistinguishable from one that worked.
func (p *Plugin) FireSalvo(ctx context.Context, r *RouterInterface, salvo uint32) (uint32, error) {
	if r.Version < router.VersionSalvos {
		return 0, fmt.Errorf("rollcall: this router's interface is version %d; salvos arrived at %d",
			r.Version, router.VersionSalvos)
	}
	if salvo < 1 || salvo > r.Salvos {
		return 0, fmt.Errorf("rollcall: salvo %d is outside the %d this router holds", salvo, r.Salvos)
	}

	// One number, in data mode: the encode has no way to fail.
	body, _ := router.FireSalvo{Salvo: salvo}.AppendTo(nil)

	v, err := p.writeData(ctx, r.Slot, uint32(router.CmdFireSalvo), body)
	if err != nil {
		return 0, fmt.Errorf("rollcall: fire salvo %d: %w", salvo, err)
	}

	// A controller that answers with something other than a salvo result has
	// still fired it; the count is what is unknown, not the firing.
	fired, err := router.DecodeSalvoFired(v.Data)
	if err != nil {
		return 0, fmt.Errorf("rollcall: fire salvo %d: %w", salvo, err)
	}
	return fired.Routes, nil
}

// destCommand finds the command at an offset in a destination's own table.
func (r *RouterInterface) destCommand(matrix, level, dest, offset uint32) (uint32, error) {
	lv, err := r.Level(matrix, level)
	if err != nil {
		return 0, err
	}
	cmd, ok := lv.Dsts.Field(dest, offset)
	if !ok {
		return 0, fmt.Errorf(
			"rollcall: matrix %d level %d has %d destinations, so %d is not one of them",
			matrix, level, lv.Dsts.Count, dest)
	}
	return uint32(cmd), nil
}

// srcCommand finds the command at an offset in a source's own table.
func (r *RouterInterface) srcCommand(matrix, level, src, offset uint32) (uint32, error) {
	lv, err := r.Level(matrix, level)
	if err != nil {
		return 0, err
	}
	cmd, ok := lv.Srcs.Field(src, offset)
	if !ok {
		return 0, fmt.Errorf(
			"rollcall: matrix %d level %d has %d sources, so %d is not one of them",
			matrix, level, lv.Srcs.Count, src)
	}
	return uint32(cmd), nil
}

// Matrix returns one matrix by its number, counting from one.
func (r *RouterInterface) Matrix(n uint32) (*RouterMatrix, error) {
	if n < 1 || int(n) > len(r.Matrices) {
		return nil, fmt.Errorf("rollcall: this router has %d matrices, so %d is not one of them",
			len(r.Matrices), n)
	}
	return &r.Matrices[n-1], nil
}

// Level returns one level of one matrix, both counting from one.
func (r *RouterInterface) Level(matrix, level uint32) (*RouterLevel, error) {
	m, err := r.Matrix(matrix)
	if err != nil {
		return nil, err
	}
	if level < 1 || int(level) > len(m.Levels) {
		return nil, fmt.Errorf("rollcall: matrix %d has %d levels, so %d is not one of them",
			matrix, len(m.Levels), level)
	}
	return &m.Levels[level-1], nil
}

// decodeCrosspoint reads the Data Transfer Params a routed-source command
// carries: the pin, and optionally the result of the last change.
func decodeCrosspoint(v codec.Value, dest router.SourcePin) (Crosspoint, error) {
	xpt := Crosspoint{Dest: dest}

	if len(v.Data) == 0 {
		// A destination with nothing routed to it and nothing to report.
		return xpt, nil
	}

	items, err := dtp.Decode(v.Data)
	if err != nil {
		return Crosspoint{}, fmt.Errorf("rollcall: crosspoint: %w", err)
	}

	for i, it := range items {
		switch {
		case i == 0 && it.Type == dtp.TypeUint:
			xpt.Source = router.UnpackSourcePin(it.Uint)
		case i == 0 && it.Type == dtp.TypeUintArray && len(it.Uints) > 0:
			// A controller may answer with the array form it accepts.
			xpt.Source = router.UnpackSourcePin(it.Uints[0])
		case i == 1 && it.Type == dtp.TypeUint:
			xpt.Result = router.RouteResult(it.Uint)
			xpt.HasResult = true
		}
	}
	return xpt, nil
}
