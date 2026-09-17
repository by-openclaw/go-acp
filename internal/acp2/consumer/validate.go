package acp2

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"

	"dhs/internal/acp2/codec"
	"dhs/internal/consumer"
	"dhs/internal/wiretrace"
)

// Validate decodes captured ACP2-over-AN2 wire-trace records (Trames
// per ADR-0021) through the AN2 framer + ACP2 message codec, asserts
// AN2 magic and ACP2 mtid / type / func / stat invariants, and returns
// a report with per-direction counts plus any decode failures or
// invariant violations.
//
// This is the offline counterpart to Walk: same codec stack, no
// live peer.
//
// --out-tree / --out-params (per ADR-0002) are not yet wired here —
// they land in a follow-up PR that integrates canonicalize.go.
func (p *Plugin) Validate(ctx context.Context, trames []wiretrace.Trame, opts consumer.ValidateOpts) (*consumer.ValidateReport, error) {
	report := &consumer.ValidateReport{
		PerDirection: map[wiretrace.Direction]int{},
	}

	if opts.OutTree != "" || opts.OutParams != "" {
		return report, fmt.Errorf("acp2.Validate: --out-tree / --out-params not implemented yet (follow-up PR)")
	}

	var lastReqMTID uint8
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

		frame, err := codec.ReadAN2Frame(bytes.NewReader(raw))
		if err != nil {
			report.Errors = append(report.Errors, consumer.ValidateError{
				TrameIndex: i,
				Direction:  t.Direction,
				HexPrefix:  shortHex(raw),
				Err:        fmt.Sprintf("an2 decode: %v", err),
			})
			continue
		}

		report.TramesProcessed++
		report.PerDirection[t.Direction]++

		switch frame.Proto {
		case codec.AN2ProtoInternal:
			if frame.Type != codec.AN2TypeData && frame.MTID == 0 {
				report.Invariants = append(report.Invariants,
					fmt.Sprintf("frame %d: an2 internal req/reply with mtid=0 (must be 1-255)", i))
			}
		case codec.AN2ProtoACP2:
			if frame.Type != codec.AN2TypeData {
				report.Invariants = append(report.Invariants,
					fmt.Sprintf("frame %d: ACP2 frame must use AN2TypeData (4), got %d", i, frame.Type))
				continue
			}
			msg, err := codec.DecodeACP2Message(frame.Payload)
			if err != nil {
				report.Errors = append(report.Errors, consumer.ValidateError{
					TrameIndex: i,
					Direction:  t.Direction,
					HexPrefix:  shortHex(frame.Payload),
					Err:        fmt.Sprintf("acp2 decode: %v", err),
				})
				continue
			}
			switch msg.Type {
			case codec.ACP2TypeRequest:
				if msg.MTID == 0 {
					report.Invariants = append(report.Invariants,
						fmt.Sprintf("frame %d: ACP2 request with mtid=0 (spec: 1-255)", i))
				}
				lastReqMTID = msg.MTID
			case codec.ACP2TypeReply:
				if msg.MTID != lastReqMTID {
					report.Invariants = append(report.Invariants,
						fmt.Sprintf("frame %d: ACP2 reply mtid=%d does not match last request mtid=%d", i, msg.MTID, lastReqMTID))
				}
			case codec.ACP2TypeAnnounce:
				if msg.MTID != 0 {
					report.Invariants = append(report.Invariants,
						fmt.Sprintf("frame %d: ACP2 announce with mtid=%d (spec: must be 0)", i, msg.MTID))
				}
			case codec.ACP2TypeError:
				stat := codec.ACP2ErrStatus(msg.Func)
				if stat > codec.ErrInvalidValue {
					report.Invariants = append(report.Invariants,
						fmt.Sprintf("frame %d: ACP2 error stat=%d outside spec range [0..5]", i, stat))
				}
			default:
				report.Invariants = append(report.Invariants,
					fmt.Sprintf("frame %d: ACP2 unknown msg type %d (spec: 0..3)", i, msg.Type))
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
