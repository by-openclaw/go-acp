package acp1

import (
	"context"
	"log/slog"
	"math/rand"
	"sort"
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
func (s *server) RunStatusPlay(ctx context.Context, paths []string, interval time.Duration, fullRange bool) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	started := 0
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
		go s.playLoop(ctx, key, e, interval, fullRange)
		started++
	}
	s.logger.Info("acp1 play started",
		slog.Int("objects", started),
		slog.Bool("full_range", fullRange),
		slog.Duration("interval", interval))
}

// RunStatusPlayAll is the auto-discovery form of RunStatusPlay: instead of
// naming each object path, it walks the served tree and oscillates EVERY
// oscillatable object on EVERY slot — including slot 0, the rack controller —
// with a spontaneous status announce per change. Read-only status objects and
// writable control objects alike are driven (the provider owns the tree);
// String / Float / File / Frame / Alarm objects have no natural random form
// and are skipped. Models a fully live rack where every sensor, counter, and
// enum on every card drifts on its own — the wire-traffic load a consumer's
// announce handling must cope with.
//
// Stops cleanly on ctx cancellation.
func (s *server) RunStatusPlayAll(ctx context.Context, interval time.Duration, fullRange bool) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	// oscillatableTargetsHook is a test-only seam: it lets a test supply a
	// key list containing an entry that no longer exists, so the per-key
	// lookup-miss guard below (otherwise only hit by a collect-vs-delete
	// race) is deterministically reachable. Nil in production.
	keys := s.oscillatableTargets()
	if s.oscillatableTargetsHook != nil {
		keys = s.oscillatableTargetsHook()
	}
	s.logger.Info("acp1 play all started",
		slog.Int("objects", len(keys)),
		slog.Bool("full_range", fullRange),
		slog.Duration("interval", interval))
	for _, k := range keys {
		e, ok := s.tree.lookup(k)
		if !ok {
			continue
		}
		go s.playLoop(ctx, k, e, interval, fullRange)
	}
}

// RunFrameStatusPlay oscillates the rack-controller frame-status object on
// slot 0: each tick one random slot in the array is flipped to a random state
// (no_card/powerup/boot/present/error/removed) and a frame-status announce is
// broadcast. A consumer subscribed to slot 0 frame-status therefore sees a
// stream of card insert / remove / error events to detect — the dynamic the
// rack controller exposes. No-op if the served tree has no frame-status object.
//
// Stops cleanly on ctx cancellation.
func (s *server) RunFrameStatusPlay(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	if !s.hasFrameStatus() {
		s.logger.Debug("acp1 play frame-status: no frame-status object, skipping")
		return
	}
	s.logger.Info("acp1 play frame-status started", slog.Duration("interval", interval))
	go func() {
		r := rand.New(rand.NewSource(time.Now().UnixNano() ^ 0x5f1a))
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			s.frameStatusTick(r)
		}
	}()
}

// hasFrameStatus reports whether the served tree carries the rack-controller
// frame-status object.
func (s *server) hasFrameStatus() bool {
	s.tree.mu.RLock()
	defer s.tree.mu.RUnlock()
	e, ok := s.tree.entries[objectKey{slot: 0, group: codec.GroupFrame, id: 0}]
	return ok && e != nil && e.param != nil
}

// frameStatusTick flips one random slot in the frame-status array to a random
// state (0..5) and broadcasts the change via setSlotStatus. Returns the slot
// and state it set, or ok=false when no frame-status object exists. Extracted
// from RunFrameStatusPlay so it is unit-testable without goroutines/timers.
func (s *server) frameStatusTick(r *rand.Rand) (uint8, uint8, bool) {
	s.tree.mu.RLock()
	e, ok := s.tree.entries[objectKey{slot: 0, group: codec.GroupFrame, id: 0}]
	n := 0
	if ok && e != nil && e.param != nil {
		if statuses, ok2 := e.param.Value.([]any); ok2 {
			n = len(statuses)
		}
	}
	s.tree.mu.RUnlock()
	if n == 0 {
		return 0, 0, false
	}
	slot := uint8(r.Intn(n))
	state := uint8(r.Intn(6)) // 0=no_card .. 5=boot
	if err := s.setStatus(slot, state); err != nil {
		s.logger.Debug("acp1 play frame-status: set failed",
			slog.Int("slot", int(slot)), slog.String("err", err.Error()))
		return slot, state, false
	}
	return slot, state, true
}

// oscillatableTargets returns every object key in the served tree whose type
// can be driven by the oscillator (mirrors randomBytesFor), sorted by
// slot/group/id for deterministic startup. Spans all slots, slot 0 included.
func (s *server) oscillatableTargets() []objectKey {
	s.tree.mu.RLock()
	defer s.tree.mu.RUnlock()
	keys := make([]objectKey, 0, len(s.tree.entries))
	for k, e := range s.tree.entries {
		if e != nil && oscillatable(e.acpType) {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].slot != keys[j].slot {
			return keys[i].slot < keys[j].slot
		}
		if keys[i].group != keys[j].group {
			return keys[i].group < keys[j].group
		}
		return keys[i].id < keys[j].id
	})
	return keys
}

// oscillatable reports whether an object type has a random form the oscillator
// can produce. Mirrors the type switch in randomBytesFor exactly.
func oscillatable(t codec.ObjectType) bool {
	switch t {
	case codec.TypeInteger, codec.TypeLong, codec.TypeByte, codec.TypeEnum, codec.TypeIPAddr:
		return true
	}
	return false
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
func (s *server) playLoop(ctx context.Context, key objectKey, e *entry, interval time.Duration, fullRange bool) {
	r := rand.New(rand.NewSource(time.Now().UnixNano() ^ int64(key.slot)<<16 ^ int64(key.id)))
	nominal, _ := readIntBound(e.param.Value)
	t := time.NewTicker(interval)
	defer t.Stop()
	// Per-object start is Debug, not Info: --play all spins up one loop per
	// object across every slot, so an Info line each would bury the single
	// "play all started" summary under hundreds of lines.
	s.logger.Debug("acp1 play object started",
		slog.String("path", e.param.Path),
		slog.Int("slot", int(key.slot)),
		slog.Int("group", int(key.group)),
		slog.Int("id", int(key.id)),
		slog.Int("acp_type", int(e.acpType)),
		slog.Int64("nominal", nominal),
		slog.Bool("full_range", fullRange),
		slog.Duration("interval", interval),
	)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		raw, ok := s.randomBytesFor(e, r, nominal, fullRange)
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
func (s *server) randomBytesFor(e *entry, r *rand.Rand, nominal int64, fullRange bool) ([]byte, bool) {
	// nextInt picks the next value: a full-span uniform draw across [min,max]
	// in random mode (the "force a swing" behaviour), or the mean-reverting
	// walk in the default realistic mode.
	nextInt := func(cur, minV, maxV int64) int64 {
		if fullRange {
			return uniformInt(r, minV, maxV)
		}
		return walkInt(r, cur, nominal, minV, maxV)
	}
	switch e.acpType {
	case codec.TypeInteger:
		minV, maxV := intBounds(e.param.Minimum, int64(-32768)), intBounds(e.param.Maximum, int64(32767))
		cur, _ := readIntBound(e.param.Value)
		v := nextInt(cur, minV, maxV)
		return []byte{byte(v >> 8), byte(v)}, true
	case codec.TypeLong:
		minV, maxV := intBounds(e.param.Minimum, int64(-2147483648)), intBounds(e.param.Maximum, int64(2147483647))
		cur, _ := readIntBound(e.param.Value)
		v := nextInt(cur, minV, maxV)
		return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}, true
	case codec.TypeByte:
		minV, maxV := intBounds(e.param.Minimum, int64(0)), intBounds(e.param.Maximum, int64(255))
		cur, _ := readIntBound(e.param.Value)
		v := nextInt(cur, minV, maxV)
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

// uniformInt picks a uniform random value across the full [min, max] range —
// the "force random from min-max" behaviour, in contrast to walkInt's gentle
// mean-reverting drift. Bounds are swapped if inverted; an empty range returns
// the single value. ACP1 numeric spans (int16 / int32 / uint8) never overflow
// span+1 in int64, so Int63n is safe.
func uniformInt(r *rand.Rand, minV, maxV int64) int64 {
	if maxV < minV {
		minV, maxV = maxV, minV
	}
	span := maxV - minV
	if span <= 0 {
		return minV
	}
	return minV + r.Int63n(span+1)
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
