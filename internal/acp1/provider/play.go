package acp1

import (
	"context"
	"log/slog"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"dhs/internal/acp1/codec"
)

// RunStatusPlay drives a continuous server-side oscillator on the
// supplied object paths. Every tick a fresh random value within the
// object's declared [min, max] is committed via applyMutation and a
// spontaneous status announce (MTYPE=0 MCODE=0 per ACP1 spec p.14)
// fires on the broadcast socket. This models a real Synapse rack
// where temperature sensors drift, PSU rails fluctuate, and packet
// counters tick on their own — no external SetValue request required.
//
// Supported types per object:
//
//	Integer / Long / Byte  → uniform random within [min, max]
//	Enum                   → uniform random index across items
//	IPAddr                 → random 4-octet (mIP / mGW are useful here)
//
// Other types (String, Float, File, Frame, Alarm) are skipped with a
// warning. Caller can mix any number of paths; each runs independently
// with the same tick interval.
//
// Stops cleanly on ctx cancellation.
func (s *server) RunStatusPlay(ctx context.Context, paths []string, interval time.Duration) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	for _, p := range paths {
		path := strings.TrimSpace(p)
		if path == "" {
			continue
		}
		key, err := parsePath(path)
		if err != nil {
			s.logger.Warn("acp1 play: bad path",
				slog.String("path", path),
				slog.String("err", err.Error()))
			continue
		}
		e, ok := s.tree.lookup(key)
		if !ok {
			s.logger.Warn("acp1 play: object not found",
				slog.String("path", path))
			continue
		}
		go s.playLoop(ctx, key, e, interval)
	}
}

// playLoop oscillates one entry. A separate goroutine per path so each
// can pick its own random sequence without synchronising with peers.
//
// nominal is captured ONCE at start (schema's initial value) and the
// walk mean-reverts to it: 70% chance the next step pulls toward
// nominal by 1, 30% chance it walks ±1 randomly. Values stay within
// the realistic operating band of the object — temperatures hover
// near 24°C, packet counters near 0, etc. — instead of drifting to
// the int16 extremes the schema's wide min/max would otherwise allow.
func (s *server) playLoop(ctx context.Context, key objectKey, e *entry, interval time.Duration) {
	r := rand.New(rand.NewSource(time.Now().UnixNano() ^ int64(key.slot)<<16 ^ int64(key.id)))
	nominal, _ := readIntBound(e.param.Value)
	t := time.NewTicker(interval)
	defer t.Stop()
	s.logger.Info("acp1 play started",
		slog.String("path", e.param.Path),
		slog.Int("slot", int(key.slot)),
		slog.Int("group", int(key.group)),
		slog.Int("id", int(key.id)),
		slog.Int("acp_type", int(e.acpType)),
		slog.Int64("nominal", nominal),
		slog.Duration("interval", interval),
	)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		raw, ok := s.randomBytesFor(e, r, nominal)
		if !ok {
			continue
		}
		stored, err := s.applyMutation(e, codec.MethodSetValue, raw)
		if err != nil {
			s.logger.Debug("acp1 play apply failed",
				slog.String("path", e.param.Path),
				slog.String("err", err.Error()))
			continue
		}
		announce := &codec.Message{
			MTID:     0,
			PVER:     1,
			MType:    codec.MTypeAnnounce,
			MAddr:    key.slot,
			MCode:    0,
			ObjGroup: key.group,
			ObjID:    key.id,
			Value:    stored,
		}
		s.broadcastAnnounce(announce)
	}
}

// randomBytesFor synthesises a wire-encoded value for the next tick.
// Numeric types follow a mean-reverting random walk: drift toward
// nominal (the schema's initial value) by 1 most of the time, with
// occasional ±1 random jitter. This keeps Temp_Left near 24°C,
// Rx_Packet_Loss near 0, etc. — never wandering to the int16
// extremes the schema's wide min/max would otherwise allow.
//
// Enums pick a random item index (PSU states flip NA/OK on each
// tick to demonstrate alarm propagation).
//
// Returns ok=false for types that don't oscillate naturally (Strings,
// Floats — dedicated per-type oscillators TBD if needed).
func (s *server) randomBytesFor(e *entry, r *rand.Rand, nominal int64) ([]byte, bool) {
	switch e.acpType {
	case codec.TypeInteger:
		minV, maxV := intBounds(e.param.Minimum, int64(-32768)), intBounds(e.param.Maximum, int64(32767))
		cur, _ := readIntBound(e.param.Value)
		v := walkInt(r, cur, nominal, minV, maxV)
		return []byte{byte(v >> 8), byte(v)}, true
	case codec.TypeLong:
		minV, maxV := intBounds(e.param.Minimum, int64(-2147483648)), intBounds(e.param.Maximum, int64(2147483647))
		cur, _ := readIntBound(e.param.Value)
		v := walkInt(r, cur, nominal, minV, maxV)
		return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}, true
	case codec.TypeByte:
		minV, maxV := intBounds(e.param.Minimum, int64(0)), intBounds(e.param.Maximum, int64(255))
		cur, _ := readIntBound(e.param.Value)
		v := walkInt(r, cur, nominal, minV, maxV)
		return []byte{byte(v)}, true
	case codec.TypeEnum:
		n := len(e.param.EnumMap)
		if n == 0 {
			return nil, false
		}
		return []byte{byte(r.Intn(n))}, true
	case codec.TypeIPAddr:
		return []byte{byte(r.Intn(256)), byte(r.Intn(256)), byte(r.Intn(256)), byte(r.Intn(256))}, true
	}
	return nil, false
}

// intBounds returns the canonical bound or fallback if absent.
func intBounds(v any, fallback int64) int64 {
	if n, ok := readIntBound(v); ok {
		return n
	}
	return fallback
}

// walkInt picks the next value of a mean-reverting random walk:
// 70% of ticks pull cur one step toward nominal (the schema's
// initial value), 30% are random ±1 jitter. Clamped to [min, max].
// The result hovers tightly around nominal — temperature objects
// stay realistic, packet counters tick only by a few units.
func walkInt(r *rand.Rand, cur, nominal, minV, maxV int64) int64 {
	var v int64
	if r.Intn(10) < 7 {
		switch {
		case cur < nominal:
			v = cur + 1
		case cur > nominal:
			v = cur - 1
		default:
			v = cur
		}
	} else {
		v = cur + int64(r.Intn(3)-1)
	}
	if v < minV {
		v = minV
	}
	if v > maxV {
		v = maxV
	}
	return v
}

// readIntBound coerces a canonical numeric bound (any) to int64.
func readIntBound(v any) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int64:
		return x, true
	case float64:
		return int64(x), true
	case string:
		n, err := strconv.ParseInt(x, 10, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	return 0, false
}
