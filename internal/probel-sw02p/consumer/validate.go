package probelsw02p

import (
	"context"
	"encoding/hex"
	"fmt"

	"dhs/internal/probel-sw02p/codec"
	"dhs/internal/consumer"
	"dhs/internal/wiretrace"
)

// Compile-time assertion: probel-sw02p Plugin satisfies
// consumer.Validator so `dhs consumer probel-sw02p validate
// <frames.jsonl>` resolves cleanly at the CLI type-assert
// (cmd/dhs/cmd_validate.go).
var _ consumer.Validator = (*Plugin)(nil)

// Validate decodes captured SW-P-02 wire-trace records (Trames per
// ADR-0021) through the codec.Unpack framer + 7-bit two's-complement
// checksum verification, asserts that every command byte is in the
// codec's catalogue, and reports per-direction counts plus any
// decode failures or invariant violations.
//
// SW-P-02 §3.1 has transparent bytes (no DLE-stuffing) and no
// framing-layer ACK / NAK, so unlike SW-P-08's Validate() this one
// has no control-frame branch — every trame goes through Unpack.
//
// This is the offline counterpart to the live session: same codec,
// no socket.
//
// --out-tree / --out-params (per ADR-0002) are not yet wired here —
// they'll dispatch to Canonicalize() in a follow-up PR once the
// consumer aggregates per-crosspoint state from tx 003 / tx 004 /
// tx 067 / tx 068 traffic.
func (p *Plugin) Validate(ctx context.Context, trames []wiretrace.Trame, opts consumer.ValidateOpts) (*consumer.ValidateReport, error) {
	report := &consumer.ValidateReport{
		PerDirection: map[wiretrace.Direction]int{},
	}

	if opts.OutTree != "" || opts.OutParams != "" {
		return report, fmt.Errorf("probel-sw02p.Validate: --out-tree / --out-params not implemented yet (follow-up PR)")
	}

	for i, t := range trames {
		if err := ctx.Err(); err != nil {
			return report, err
		}

		raw, err := hex.DecodeString(t.Hex)
		if err != nil {
			report.Errors = append(report.Errors, consumer.ValidateError{
				TrameIndex: i,
				Direction:  t.Direction,
				Err:        fmt.Sprintf("hex decode: %v", err),
			})
			continue
		}

		frame, _, err := codec.Unpack(raw)
		if err != nil {
			report.Errors = append(report.Errors, consumer.ValidateError{
				TrameIndex: i,
				Direction:  t.Direction,
				HexPrefix:  shortHex(raw),
				Err:        fmt.Sprintf("sw-p-02 decode: %v", err),
			})
			continue
		}

		report.TramesProcessed++
		report.PerDirection[t.Direction]++

		if name := commandName(frame.ID); name == "" {
			report.Invariants = append(report.Invariants,
				fmt.Sprintf("trame %d: unknown SW-P-02 command id 0x%02x (%d) — not in codec catalogue", i, byte(frame.ID), byte(frame.ID)))
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
