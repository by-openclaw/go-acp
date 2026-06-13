package acp1

import (
	"context"
	"encoding/binary"
	"log/slog"
	"math"
	"time"
	"dhs/internal/acp1/codec"
)

// RunAnnounceDemo oscillates the integer value at (slot, group, id)
// every `interval` ticks using a sine curve scaled to the entry's
// declared [min, max] range and broadcasts an ACP1 value-change
// announcement (spec §"Announcements", MTID=0, MType=2, MCode=setValue)
// to the LAN on every tick. Exits when ctx is cancelled.
//
// Purpose: demonstrate that device-initiated state changes (alarms,
// sensor readings, level meters) propagate to ACP1 subscribers exactly
// like client-initiated setValue announces — both paths end at
// broadcastAnnounce. Mirrors the acp2 provider's --announce-demo.
//
// The target must be a mutable Integer (type=1) per the ACP1 method
// matrix; Enum/Byte/Long/Float types would need a different oscillator
// path so we reject non-Integer up front with a warning and return.
func (s *server) RunAnnounceDemo(ctx context.Context, slot uint8, group, id uint8, interval time.Duration) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	key := objectKey{slot: slot, group: codec.ObjGroup(group), id: id}
	e, ok := s.tree.lookup(key)
	if !ok {
		s.logger.Warn("acp1 demo: target not found",
			slog.Int("slot", int(slot)),
			slog.Int("group", int(group)),
			slog.Int("id", int(id)),
		)
		return
	}
	if e.acpType != codec.TypeInteger {
		s.logger.Warn("acp1 demo: target is not Integer (s16), skipping",
			slog.Int("acp_type", int(e.acpType)),
			slog.String("path", e.param.Path),
		)
		return
	}

	minV, maxV := int16(-100), int16(100)
	if e.param.Minimum != nil {
		if v, ok := demoAsInt16(e.param.Minimum); ok {
			minV = v
		}
	}
	if e.param.Maximum != nil {
		if v, ok := demoAsInt16(e.param.Maximum); ok {
			maxV = v
		}
	}

	t := time.NewTicker(interval)
	defer t.Stop()

	s.logger.Info("acp1 announce demo started",
		slog.Int("slot", int(slot)),
		slog.Int("group", int(group)),
		slog.Int("id", int(id)),
		slog.String("path", e.param.Path),
		slog.Int("min", int(minV)),
		slog.Int("max", int(maxV)),
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

		mid := (int32(minV) + int32(maxV)) / 2
		amp := (int32(maxV) - int32(minV)) / 2
		// v = mid + round(amp*sin) stays within [mid-amp, mid+amp], which the
		// floor-division mid/amp keep inside [minV, maxV] for every range — so
		// no post-clamp is needed (a former v<minV / v>maxV guard was dead).
		v := int16(mid + int32(math.Round(float64(amp)*math.Sin(phase))))

		// Encode as 2-byte big-endian s16 (spec §"Integer object type").
		valBytes := make([]byte, 2)
		binary.BigEndian.PutUint16(valBytes, uint16(v))

		// Route through the SAME path as a client setValue so the
		// canonical.Parameter.Value update + reply bytes match what
		// getValue would return after the change.
		stored, err := s.applyMutation(e, codec.MethodSetValue, valBytes)
		if err != nil {
			s.logger.Warn("acp1 demo: apply failed", slog.String("err", err.Error()))
			continue
		}

		announce := &codec.Message{
			MTID:     0,
			PVER:     1,
			MType:    codec.MTypeReply,
			MAddr:    slot,
			MCode:    byte(codec.MethodSetValue),
			ObjGroup: codec.ObjGroup(group),
			ObjID:    id,
			Value:    stored,
		}
		s.broadcastAnnounce(announce)
	}
}

// demoAsInt16 reads a canonical numeric constraint as int16. Returns ok=false
// when the value is out of range or not a number.
func demoAsInt16(v any) (int16, bool) {
	switch x := v.(type) {
	case int:
		if x >= math.MinInt16 && x <= math.MaxInt16 {
			return int16(x), true
		}
	case int64:
		if x >= math.MinInt16 && x <= math.MaxInt16 {
			return int16(x), true
		}
	case float64:
		if x >= math.MinInt16 && x <= math.MaxInt16 {
			return int16(x), true
		}
	case float32:
		if x >= math.MinInt16 && x <= math.MaxInt16 {
			return int16(x), true
		}
	}
	return 0, false
}
