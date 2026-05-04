package tsl

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"

	"dhs/internal/protocol"
	"dhs/internal/tsl/codec"
	"dhs/internal/wiretrace"
)

// Compile-time assertion: TSL Plugin satisfies protocol.Validator so
// `dhs consumer tsl-vXX validate <frames.jsonl>` resolves cleanly at
// the CLI type-assert (cmd/dhs/cmd_validate.go).
var _ protocol.Validator = (*Plugin)(nil)

// Validate decodes captured TSL UMD wire-trace records (Trames per
// ADR-0021) through the version-specific decoder bound to this
// Plugin instance, surfaces every codec ComplianceNote as an
// informational invariant, and reports per-direction counts plus
// any decode failures.
//
// Dispatch by Plugin.version:
//
//	V31  -> codec.DecodeV31  (18-byte UDP frame)
//	V40  -> codec.DecodeV40  (v3.1 + CHKSUM + VBC + XDATA)
//	V50  -> codec.DecodeV50  (PBC | VER | FLAGS | SCREEN | DMSG+)
//
// v5.0 trames captured from a TCP socket arrive wrapped in
// DLE/STX (0xFE 0x02 prefix) -- this function unwraps via
// DLEStreamDecoder before calling DecodeV50. UDP trames are
// passed through verbatim.
//
// This is the offline counterpart to the live UDP/TCP listener:
// same codec, no socket.
//
// --out-tree / --out-params (per ADR-0002) defer to Canonicalize() in
// a follow-up PR once the consumer aggregates per-INDEX display state
// from the V31/V40/V50 frame stream.
func (p *Plugin) Validate(ctx context.Context, trames []wiretrace.Trame, opts protocol.ValidateOpts) (*protocol.ValidateReport, error) {
	report := &protocol.ValidateReport{
		PerDirection: map[wiretrace.Direction]int{},
	}

	if opts.OutTree != "" || opts.OutParams != "" {
		return report, fmt.Errorf("tsl.Validate: --out-tree / --out-params not implemented yet (follow-up PR)")
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

		notes, err := p.decodeOne(raw)
		if err != nil {
			report.Errors = append(report.Errors, protocol.ValidateError{
				TrameIndex: i,
				Direction:  t.Direction,
				HexPrefix:  shortHex(raw),
				Err:        fmt.Sprintf("tsl decode: %v", err),
			})
			continue
		}

		report.TramesProcessed++
		report.PerDirection[t.Direction]++

		for _, n := range notes {
			report.Invariants = append(report.Invariants,
				fmt.Sprintf("trame %d: %s%s", i, n.Kind, formatNoteDetail(n.Detail)))
		}

		if opts.StopAt != "" && t.Note == opts.StopAt {
			report.StoppedAt = t.Note
			break
		}
	}

	return report, nil
}

// decodeOne dispatches one byte buffer through the codec for this
// Plugin's wire version and returns the codec's compliance notes (a
// nil-or-empty slice on a fully-spec-compliant frame).
func (p *Plugin) decodeOne(raw []byte) ([]codec.ComplianceNote, error) {
	switch p.version {
	case V31:
		f, err := codec.DecodeV31(raw)
		if err != nil {
			return nil, err
		}
		return f.Notes, nil
	case V40:
		f, err := codec.DecodeV40(raw)
		if err != nil {
			return nil, err
		}
		return f.Notes, nil
	case V50:
		// TCP-wrapped frames begin with DLE STX (0xFE 0x02). Unwrap
		// before handing the inner packet to DecodeV50. UDP frames
		// arrive raw and DecodeV50 accepts them directly.
		if len(raw) >= 2 && raw[0] == 0xFE && raw[1] == 0x02 {
			dec := codec.NewDLEStreamDecoder(bytes.NewReader(raw), 0)
			inner, err := dec.ReadFrame()
			if err != nil {
				return nil, fmt.Errorf("v5 dle/stx: %w", err)
			}
			pkt, err := codec.DecodeV50(inner)
			if err != nil {
				return nil, err
			}
			return pkt.Notes, nil
		}
		pkt, err := codec.DecodeV50(raw)
		if err != nil {
			return nil, err
		}
		return pkt.Notes, nil
	}
	return nil, fmt.Errorf("tsl validate: unknown plugin version %v", p.version)
}

func formatNoteDetail(detail string) string {
	if detail == "" {
		return ""
	}
	return " (" + detail + ")"
}

func shortHex(b []byte) string {
	if len(b) > 16 {
		b = b[:16]
	}
	return hex.EncodeToString(b)
}
