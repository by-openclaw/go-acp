package acp1

import (
	"context"
	"encoding/hex"
	"fmt"

	"dhs/internal/protocol"
	"dhs/internal/wiretrace"
)

// Validate decodes captured ACP1 wire-trace records (Trames per ADR-0021)
// through the message decoder, asserts PVER=1 and MTID rules, and returns
// a report with per-direction counts plus any decode failures or
// invariant violations.
//
// This is the offline counterpart to Walk: same codec, no live peer.
//
// `--out-tree` and `--out-params` (per ADR-0002) are not yet wired
// here — Validate returns a clear error rather than silently ignoring
// them. They land in a follow-up PR that integrates canonicalize.go.
func (p *Plugin) Validate(ctx context.Context, trames []wiretrace.Trame, opts protocol.ValidateOpts) (*protocol.ValidateReport, error) {
	report := &protocol.ValidateReport{
		PerDirection: map[wiretrace.Direction]int{},
	}

	if opts.OutTree != "" || opts.OutParams != "" {
		return report, fmt.Errorf("acp1.Validate: --out-tree / --out-params not implemented yet (follow-up PR)")
	}

	var lastReqMTID uint32
	for i, t := range trames {
		if err := ctx.Err(); err != nil {
			return report, err
		}

		raw, err := hex.DecodeString(t.Hex)
		if err != nil {
			report.Errors = append(report.Errors, protocol.ValidateError{
				TrameIndex: i,
				Direction:  t.Direction,
				Err:        fmt.Sprintf("hex decode: %v", err),
			})
			continue
		}

		msg, err := Decode(raw)
		if err != nil {
			report.Errors = append(report.Errors, protocol.ValidateError{
				TrameIndex: i,
				Direction:  t.Direction,
				HexPrefix:  shortHex(raw),
				Err:        fmt.Sprintf("acp1 decode: %v", err),
			})
			continue
		}

		report.TramesProcessed++
		report.PerDirection[t.Direction]++

		// PVER must always be 1 for v1.4 devices.
		if msg.PVER != 1 {
			report.Invariants = append(report.Invariants,
				fmt.Sprintf("trame %d: PVER %d, want 1", i, msg.PVER))
		}

		switch msg.MType {
		case MTypeRequest:
			if msg.MTID == 0 {
				report.Invariants = append(report.Invariants,
					fmt.Sprintf("trame %d: request with MTID=0 (spec: must be non-zero)", i))
			}
			lastReqMTID = msg.MTID
		case MTypeReply:
			if msg.MTID != lastReqMTID {
				report.Invariants = append(report.Invariants,
					fmt.Sprintf("trame %d: reply MTID=%d does not match last request MTID=%d", i, msg.MTID, lastReqMTID))
			}
		}

		if opts.StopAt != "" && t.Note == opts.StopAt {
			report.StoppedAt = t.Note
			break
		}
	}

	return report, nil
}

func shortHex(b []byte) string {
	if len(b) > 16 {
		b = b[:16]
	}
	return hex.EncodeToString(b)
}
