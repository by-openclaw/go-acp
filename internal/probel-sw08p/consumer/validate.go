package probelsw08p

import (
	"context"
	"encoding/hex"
	"fmt"

	"dhs/internal/probel-sw08p/codec"
	"dhs/internal/protocol"
	"dhs/internal/wiretrace"
)

// Compile-time assertion: probel-sw08p Plugin satisfies
// protocol.Validator so `dhs consumer probel-sw08p validate
// <frames.jsonl>` resolves cleanly at the CLI type-assert
// (cmd/dhs/cmd_validate.go).
var _ protocol.Validator = (*Plugin)(nil)

// Validate decodes captured SW-P-08 wire-trace records (Trames per
// ADR-0021) through the codec.Unpack framer + checksum verification,
// asserts that every recognised command byte is in the codec's
// catalogue, and reports per-direction counts plus any decode
// failures or invariant violations.
//
// DLE ACK / DLE NAK pseudo-frames are recognised as control trames
// (no DATA, no checksum). NAK is not fatal — per CLAUDE.md "NAK from
// a peer = command unsupported, NOT a fatal error" — but is recorded
// as an informational invariant so the report surfaces unsupported-
// command counts.
//
// This is the offline counterpart to the live session: same codec,
// no socket.
//
// --out-tree / --out-params (per ADR-0002) are not yet wired here —
// they'll dispatch to Canonicalize() in a follow-up PR once the
// consumer aggregates per-crosspoint state from cmd 003 / cmd 022 /
// cmd 023 tally traffic.
func (p *Plugin) Validate(ctx context.Context, trames []wiretrace.Trame, opts protocol.ValidateOpts) (*protocol.ValidateReport, error) {
	report := &protocol.ValidateReport{
		PerDirection: map[wiretrace.Direction]int{},
	}

	if opts.OutTree != "" || opts.OutParams != "" {
		return report, fmt.Errorf("probel-sw08p.Validate: --out-tree / --out-params not implemented yet (follow-up PR)")
	}

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

		// Recognise DLE ACK / DLE NAK pseudo-frames before attempting
		// Unpack — these are §2 control sequences, no SOM/DATA/CHK.
		if codec.IsACK(raw) {
			report.TramesProcessed++
			report.PerDirection[t.Direction]++
			continue
		}
		if codec.IsNAK(raw) {
			report.TramesProcessed++
			report.PerDirection[t.Direction]++
			report.Invariants = append(report.Invariants,
				fmt.Sprintf("trame %d: DLE NAK observed (peer rejected previous frame; per CLAUDE.md NAK = unsupported command, not fatal)", i))
			continue
		}

		frame, _, err := codec.Unpack(raw)
		if err != nil {
			report.Errors = append(report.Errors, protocol.ValidateError{
				TrameIndex: i,
				Direction:  t.Direction,
				HexPrefix:  shortHex(raw),
				Err:        fmt.Sprintf("sw-p-08 decode: %v", err),
			})
			continue
		}

		report.TramesProcessed++
		report.PerDirection[t.Direction]++

		if name := codec.CommandName(frame.ID); name == "" {
			report.Invariants = append(report.Invariants,
				fmt.Sprintf("trame %d: unknown SW-P-08 command id 0x%02x (%d) — not in codec catalogue", i, byte(frame.ID), byte(frame.ID)))
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
