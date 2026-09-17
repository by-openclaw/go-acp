package emberplus

import (
	"context"
	"encoding/hex"
	"fmt"

	"dhs/internal/consumer"
	"dhs/internal/emberplus/codec/glow"
	"dhs/internal/emberplus/codec/s101"
	"dhs/internal/wiretrace"
)

// Compile-time assertion: emberplus Plugin satisfies consumer.Validator
// so `dhs consumer emberplus validate <frames.jsonl>` resolves cleanly
// at the CLI type-assert (cmd/dhs/cmd_validate.go).
var _ consumer.Validator = (*Plugin)(nil)

// Validate decodes captured Ember+ wire-trace records (Trames per
// ADR-0021) through the S101 framer + BER + Glow stack, asserts S101
// invariants (slot=0, version=1, DTD=Glow, MPM consistency) and
// reports per-direction counts plus any decode failures or invariant
// violations. EmBER payloads are run through glow.DecodeRoot to
// assert codec integrity all the way down; keep-alive frames are
// counted but carry no payload.
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
		return report, fmt.Errorf("emberplus.Validate: --out-tree / --out-params not implemented yet (follow-up PR)")
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

		frame, err := s101.Decode(raw)
		if err != nil {
			report.Errors = append(report.Errors, consumer.ValidateError{
				TrameIndex: i,
				Direction:  t.Direction,
				HexPrefix:  shortHex(raw),
				Err:        fmt.Sprintf("s101 decode: %v", err),
			})
			continue
		}

		report.TramesProcessed++
		report.PerDirection[t.Direction]++

		if frame.Slot != s101.SlotDefault {
			report.Invariants = append(report.Invariants,
				fmt.Sprintf("trame %d: s101 slot=%d, want 0", i, frame.Slot))
		}
		if frame.Version != 0 && frame.Version != s101.VersionS101 {
			report.Invariants = append(report.Invariants,
				fmt.Sprintf("trame %d: s101 version=%d, want 1", i, frame.Version))
		}

		switch frame.MsgType {
		case s101.MsgEmBER:
			// Keep-alives are encoded as MsgEmBER with command 0x01/0x02 —
			// the wire never uses a distinct MsgKeepAlive message type (see
			// s101/frame.go: NewKeepAliveRequest/Response set MsgType=MsgEmBER).
			// They carry no Glow payload and are frequent during streaming,
			// so accept them here rather than flagging "unexpected command".
			if frame.Command == s101.CmdKeepAliveReq || frame.Command == s101.CmdKeepAliveResp {
				// Keep-alive frames are exactly {slot,msgType,command,version}
				// — s101.Encode never emits a payload for them, so there is no
				// payload to validate here. Just accept and move on.
				continue
			}
			if frame.Command != s101.CmdEmBER {
				report.Invariants = append(report.Invariants,
					fmt.Sprintf("trame %d: EmBER msg with unexpected command 0x%02x", i, frame.Command))
				continue
			}
			if frame.DTD != s101.DTDGlow {
				report.Invariants = append(report.Invariants,
					fmt.Sprintf("trame %d: EmBER msg with non-Glow DTD 0x%02x", i, frame.DTD))
			}
			// Mid-flight MPM packets carry partial BER; skip glow decode
			// unless this is a single (first+last) frame.
			if frame.Flags&s101.FlagSingle != s101.FlagSingle {
				continue
			}
			if _, err := glow.DecodeRoot(frame.Payload); err != nil {
				report.Errors = append(report.Errors, consumer.ValidateError{
					TrameIndex: i,
					Direction:  t.Direction,
					HexPrefix:  shortHex(frame.Payload),
					Err:        fmt.Sprintf("glow decode: %v", err),
				})
			}
		case s101.MsgKeepAlive:
			switch frame.Command {
			case s101.CmdKeepAliveReq, s101.CmdKeepAliveResp:
				// expected
			default:
				report.Invariants = append(report.Invariants,
					fmt.Sprintf("trame %d: keep-alive with unknown command 0x%02x", i, frame.Command))
			}
			if len(frame.Payload) != 0 {
				report.Invariants = append(report.Invariants,
					fmt.Sprintf("trame %d: keep-alive carries unexpected payload (%d bytes)", i, len(frame.Payload)))
			}
		default:
			report.Invariants = append(report.Invariants,
				fmt.Sprintf("trame %d: unknown s101 message type 0x%02x", i, frame.MsgType))
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
