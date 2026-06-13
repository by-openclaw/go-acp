package acp1

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"

	"dhs/internal/acp1/codec"
)

// FuzzConfig parametrises the value-change stress generator.
type FuzzConfig struct {
	Seed         int64         // 0 = use time-derived seed
	Rate         float64       // events per second; <=0 = 1.0
	Duration     time.Duration // 0 = run until ctx is cancelled
	Slot         int           // -1 = all slots
	Group        codec.ObjGroup
	GroupSet     bool
	ID           int  // -1 = all ids
	IncludeEdges bool // bias toward min/max/default once per cycle
}

// fuzzTarget is one writable object that the fuzzer can drive.
type fuzzTarget struct {
	key  objectKey
	e    *entry
}

// RunFuzz drives random valid value-changes against the writable
// objects in the served tree, generating broadcast announces consumers
// can stress-test against. Reproducible per --seed; rate-limited per
// --rate.
//
// Returns when ctx is cancelled or --duration elapses. Errors only
// surface for catastrophic conditions (no eligible targets); per-tick
// failures log and continue.
func (s *server) RunFuzz(ctx context.Context, cfg FuzzConfig) error {
	if cfg.Rate <= 0 {
		cfg.Rate = 1.0
	}
	seed := cfg.Seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	//nolint:gosec // non-crypto fuzz seed is fine for value generation
	rng := rand.New(rand.NewSource(seed))

	targets := s.collectFuzzTargets(cfg)
	if len(targets) == 0 {
		return fmt.Errorf("acp1 fuzz: no eligible writable targets (slot=%d, group=%v, id=%d)",
			cfg.Slot, cfg.Group, cfg.ID)
	}

	s.logger.Info("acp1 fuzz starting",
		"seed", seed,
		"rate", cfg.Rate,
		"duration", cfg.Duration,
		"targets", len(targets),
		"include_edges", cfg.IncludeEdges)

	interval := time.Duration(float64(time.Second) / cfg.Rate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var deadline <-chan time.Time
	if cfg.Duration > 0 {
		t := time.NewTimer(cfg.Duration)
		defer t.Stop()
		deadline = t.C
	}

	cycle := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-deadline:
			return nil
		case <-ticker.C:
			s.fuzzTick(rng, targets, cfg, cycle)
			cycle++
		}
	}
}

// collectFuzzTargets walks the tree once and returns every writable
// entry matching the (slot, group, id) filter.
func (s *server) collectFuzzTargets(cfg FuzzConfig) []fuzzTarget {
	s.tree.mu.RLock()
	defer s.tree.mu.RUnlock()
	var out []fuzzTarget
	for k, e := range s.tree.entries {
		if cfg.Slot >= 0 && int(k.slot) != cfg.Slot {
			continue
		}
		if cfg.GroupSet && k.group != cfg.Group {
			continue
		}
		if cfg.ID >= 0 && int(k.id) != cfg.ID {
			continue
		}
		if e.access&2 == 0 {
			continue // not writable
		}
		if !fuzzableType(e.acpType) {
			continue
		}
		out = append(out, fuzzTarget{key: k, e: e})
	}
	return out
}

// fuzzableType reports whether the fuzzer can synthesise a valid value
// for this ACP1 type. String fuzzing is left out for now — it would
// need a phrase pool the user supplies, and the issue's table cycles
// "fixture phrases" which we don't have without a fixtures contract.
// Frame / Alarm / File / Reserved are read-only or out-of-scope.
func fuzzableType(t codec.ObjectType) bool {
	switch t {
	case codec.TypeInteger, codec.TypeLong, codec.TypeByte,
		codec.TypeFloat, codec.TypeIPAddr, codec.TypeEnum:
		return true
	}
	return false
}

// fuzzTick picks one target and applies a synthesised mutation.
func (s *server) fuzzTick(rng *rand.Rand, targets []fuzzTarget, cfg FuzzConfig, cycle int) {
	t := targets[rng.Intn(len(targets))]
	bytes, err := s.synthesiseValue(rng, t.e, cfg.IncludeEdges, cycle)
	if err != nil {
		s.logger.Debug("fuzz: skip target", "key", fmt.Sprintf("%+v", t.key), "err", err.Error())
		return
	}
	stored, err := s.applyMutation(t.e, codec.MethodSetValue, bytes)
	if err != nil {
		s.logger.Debug("fuzz: applyMutation failed",
			"key", fmt.Sprintf("%+v", t.key), "err", err.Error())
		return
	}
	ann := &codec.Message{
		MTID:     0,
		MType:    codec.MTypeReply,
		MAddr:    t.key.slot,
		MCode:    byte(codec.MethodSetValue),
		ObjGroup: t.key.group,
		ObjID:    t.key.id,
		Value:    stored,
	}
	s.broadcastAnnounce(ann)
}

// synthesiseValue produces wire bytes for a single mutation. When
// includeEdges is set, the synthesiser biases every 4th cycle to a
// boundary value (min / max / default) so clamp paths get exercised.
func (s *server) synthesiseValue(rng *rand.Rand, e *entry, includeEdges bool, cycle int) ([]byte, error) {
	useEdge := includeEdges && cycle%4 == 0
	switch e.acpType {
	case codec.TypeInteger:
		return synthInteger(rng, e, useEdge, 2), nil
	case codec.TypeLong:
		return synthInteger(rng, e, useEdge, 4), nil
	case codec.TypeByte:
		return synthInteger(rng, e, useEdge, 1), nil
	case codec.TypeFloat:
		return synthFloat(rng, e, useEdge), nil
	case codec.TypeIPAddr:
		// Random IPv4: 4 bytes, no min/max in spec metadata.
		out := make([]byte, 4)
		rng.Read(out)
		return out, nil
	case codec.TypeEnum:
		return synthEnum(rng, e), nil
	}
	return nil, fmt.Errorf("acp1 fuzz: unsupported type %d", e.acpType)
}

func synthInteger(rng *rand.Rand, e *entry, useEdge bool, width int) []byte {
	min, max := readIntBounds(e)
	var v int64
	switch {
	case useEdge && rng.Intn(2) == 0:
		v = min
	case useEdge:
		v = max
	default:
		// readIntBounds guarantees max > min (it clamps a degenerate range
		// to 0..1), so span is always >= 2 here — rng.Int63n never sees a
		// non-positive argument. No span<=0 guard is needed.
		span := max - min + 1
		v = min + rng.Int63n(span)
	}
	out := make([]byte, width)
	switch width {
	case 1:
		out[0] = byte(v)
	case 2:
		out[0] = byte(v >> 8)
		out[1] = byte(v)
	case 4:
		out[0] = byte(v >> 24)
		out[1] = byte(v >> 16)
		out[2] = byte(v >> 8)
		out[3] = byte(v)
	}
	return out
}

func synthFloat(rng *rand.Rand, e *entry, useEdge bool) []byte {
	min, max := readFloatBounds(e)
	var v float64
	switch {
	case useEdge && rng.Intn(2) == 0:
		v = min
	case useEdge:
		v = max
	default:
		v = min + rng.Float64()*(max-min)
	}
	bits := math.Float32bits(float32(v))
	return []byte{byte(bits >> 24), byte(bits >> 16), byte(bits >> 8), byte(bits)}
}

func synthEnum(rng *rand.Rand, e *entry) []byte {
	// num_items lives on the entry indirectly via the canonical
	// EnumMap. Fall through to a single-byte pick from [0, len-1].
	if e.param != nil && len(e.param.EnumMap) > 0 {
		idx := rng.Intn(len(e.param.EnumMap))
		return []byte{byte(e.param.EnumMap[idx].Value)}
	}
	// No metadata: pick a random byte.
	return []byte{byte(rng.Intn(256))}
}

func readIntBounds(e *entry) (int64, int64) {
	min, max := int64(0), int64(0)
	if e.param != nil {
		switch v := e.param.Minimum.(type) {
		case int64:
			min = v
		case int:
			min = int64(v)
		case float64:
			min = int64(v)
		}
		switch v := e.param.Maximum.(type) {
		case int64:
			max = v
		case int:
			max = int64(v)
		case float64:
			max = int64(v)
		}
	}
	if max <= min {
		// Defensive: parameters with no declared range get a tiny one.
		min, max = 0, 1
	}
	return min, max
}

func readFloatBounds(e *entry) (float64, float64) {
	min, max := 0.0, 0.0
	if e.param != nil {
		switch v := e.param.Minimum.(type) {
		case float64:
			min = v
		case int64:
			min = float64(v)
		case int:
			min = float64(v)
		}
		switch v := e.param.Maximum.(type) {
		case float64:
			max = v
		case int64:
			max = float64(v)
		case int:
			max = float64(v)
		}
	}
	if max <= min {
		min, max = 0.0, 1.0
	}
	return min, max
}
