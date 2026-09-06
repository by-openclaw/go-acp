package acp2

import (
	"context"
	"dhs/internal/acp2/codec"
	"log/slog"
	"math"
	"time"
)

// RunAnnounceDemo mutates slot=1 / obj=18 (GainFloat) every `interval`
// ticks, walks a sine curve over [min,max] so the viewer sees a live
// value bouncing, and fans an Announce out to every subscribed session.
// Exits when ctx is cancelled.
//
// Purpose: demonstrate that device-initiated state changes (alarms,
// sensor readings, heartbeats) propagate to subscribers exactly like
// client-initiated SetProperty changes — both paths end at
// broadcastAnnounce.
//
// Choose GainFloat because:
//   - slot 1 is the fully-typed stress slot (already present in demo fixture)
//   - obj 18 is RW Number+Float with min=-60 / max=20 declared — the
//     oscillation stays in range and doesn't fight our set.go clamp logic
//   - a continuously-changing float is more visible in VSM than an int counter
func (s *server) RunAnnounceDemo(ctx context.Context, slot uint8, objID uint32, interval time.Duration) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()

	s.logger.Info("acp2 announce demo started",
		slog.Int("slot", int(slot)),
		slog.Int("obj_id", int(objID)),
		slog.Duration("interval", interval),
	)

	phase := 0.0
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		phase += math.Pi / 8 // 16 ticks per full cycle
		s.emitFloatAnnounce(slot, objID, phase)
	}
}

// emitFloatAnnounce computes sin(phase)-mapped value in the entry's
// [min,max], mutates the tree, and broadcasts the announce. Mirrors
// the server half of applySetNumber + handleSetProperty's announce
// fan-out — same bytes on the wire as a client-initiated SetProperty.
func (s *server) emitFloatAnnounce(slot uint8, objID uint32, phase float64) {
	e, ok := s.tree.lookup(slot, objID)
	if !ok {
		s.logger.Debug("acp2 demo: obj not found",
			slog.Int("slot", int(slot)),
			slog.Int("obj_id", int(objID)),
		)
		return
	}
	if e.objType != codec.ObjTypeNumber || e.numType != codec.NumTypeFloat {
		s.logger.Debug("acp2 demo: obj is not Number+Float",
			slog.Int("obj_id", int(objID)),
			slog.Int("obj_type", int(e.objType)),
		)
		return
	}
	minV, hasMin := floatConstraint(e.param.Minimum)
	maxV, hasMax := floatConstraint(e.param.Maximum)
	if !hasMin {
		minV = -100
	}
	if !hasMax {
		maxV = 100
	}
	mid := (minV + maxV) / 2
	amp := (maxV - minV) / 2
	v := mid + amp*math.Sin(phase)

	s.tree.mu.Lock()
	e.param.Value = v
	s.tree.mu.Unlock()

	s.logger.Info("acp2 demo tick",
		slog.Int("slot", int(slot)),
		slog.Int("obj_id", int(objID)),
		slog.Float64("value", v),
	)

	// unreachable: encodeNumericProp(NumTypeFloat, float64) only errors on an
	// unknown NumberType or a non-numeric value; nt is fixed NumTypeFloat and v
	// is a computed float64, so neither failure mode applies.
	prop, _ := encodeNumericProp(codec.PIDValue, codec.NumTypeFloat, v)
	// unreachable: EncodeProperty only errors on a nil *Property; &prop is a
	// guaranteed-non-nil local.
	body, _ := codec.EncodeProperty(&prop)
	// Spec §3.2 ACP2 Announce header: [type=2, mtid=0, stat=0, pid].
	// Byte 2 is stat (always 0) — see handlers.go handleSetProperty for the
	// same constraint on the client-set path.
	announce := &codec.ACP2Message{
		Type:  codec.ACP2TypeAnnounce,
		MTID:  0,
		Func:  0, // stat=0 per spec §3.2
		PID:   codec.PIDValue,
		ObjID: objID,
		Idx:   0,
		Body:  appendObjIDIdx(objID, 0, body),
	}
	s.broadcastAnnounce(slot, announce)
}
