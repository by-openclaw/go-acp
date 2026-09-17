package acp2

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"dhs/internal/acp2/codec"
)

// AnnounceItem is one recorded announce from a real device: the raw
// ACP2 payload (header + body, exactly as captured) plus the delay
// since the previous announce, so a replay reproduces the real
// cadence. Records live in a .jsonl file, one item per line:
//
//	{"dt_ms": 391, "slot": 1, "hex": "0200000800011218..."}
//
// A real EVS Neuron pushes value announces continuously (~2.6/s
// measured on CONVERT Hybrid 6.7.4, 2026-08-20); controllers derive
// module liveness from that chatter, so an announce-silent emulator
// reads as "no connection" even while answering every request
// (Cerebrum Neuron driver, wire-proven on staging).
type AnnounceItem struct {
	DtMs int    `json:"dt_ms"`
	Slot uint8  `json:"slot"`
	Hex  string `json:"hex"`

	payload []byte
}

// LoadAnnounceReplay reads a .jsonl announce recording. Malformed
// lines are an error — the file is a committed fixture, not tolerant
// input.
func LoadAnnounceReplay(path string) ([]AnnounceItem, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("announce replay: %w", err)
	}
	defer func() { _ = f.Close() }()

	var items []AnnounceItem
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	line := 0
	for sc.Scan() {
		line++
		var it AnnounceItem
		if err := json.Unmarshal(sc.Bytes(), &it); err != nil {
			return nil, fmt.Errorf("announce replay line %d: %w", line, err)
		}
		it.payload, err = hex.DecodeString(it.Hex)
		if err != nil {
			return nil, fmt.Errorf("announce replay line %d: hex: %w", line, err)
		}
		if len(it.payload) < codec.ACP2HeaderSize {
			return nil, fmt.Errorf("announce replay line %d: payload %dB < ACP2 header", line, len(it.payload))
		}
		items = append(items, it)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("announce replay: %w", err)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("announce replay %s: no records", path)
	}
	return items, nil
}

// RunAnnounceReplay loops the recorded announce stream to every
// session with ACP2 events enabled until ctx is cancelled. Purely
// additive — nothing runs unless the producer was started with a
// replay file.
func (s *server) RunAnnounceReplay(ctx context.Context, items []AnnounceItem) {
	s.logger.Info("acp2 announce replay started",
		slog.Int("items", len(items)))
	for {
		if done := s.replayPass(ctx, items); done {
			return
		}
	}
}

// replayPass plays the stream once, honouring each item's recorded
// delay. Returns true when ctx was cancelled mid-pass.
func (s *server) replayPass(ctx context.Context, items []AnnounceItem) bool {
	for _, it := range items {
		timer := time.NewTimer(time.Duration(it.DtMs) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return true
		case <-timer.C:
		}
		p := it.payload
		s.broadcastAnnounce(it.Slot, &codec.ACP2Message{
			Type: codec.ACP2MsgType(p[0]),
			MTID: p[1],
			Func: codec.ACP2Func(p[2]),
			PID:  p[3],
			Body: p[4:],
		})
	}
	return false
}
