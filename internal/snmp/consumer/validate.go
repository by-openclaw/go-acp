package consumer

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	dhsc "dhs/internal/consumer"
	"dhs/internal/snmp/codec"
	"dhs/internal/wiretrace"
)

// The SNMP Plugin satisfies consumer.Validator, so `dhs consumer snmp
// validate <frames.jsonl>` — a verb the SNMP help has always listed —
// resolves at the CLI's type assertion instead of answering "does not
// implement consumer.Validator yet".
var _ dhsc.Validator = (*Plugin)(nil)

// ErrValidateOutput is returned for --out-tree / --out-params: a capture
// of SNMP datagrams names objects by OID, and turning those into a tree
// needs the MIB of the device they came from, which a capture file does
// not carry. `walk` against the device (or its DM) is the way to a tree.
var ErrValidateOutput = errors.New("snmp validate: --out-tree and --out-params are not available; a capture carries OIDs, not the device's MIB")

// Validate decodes captured SNMP datagrams (v1 and v2c messages, as
// --capture records them) through the codec, offline. A capture against a
// real agent becomes a decoder oracle.
//
// Invariants surfaced:
//   - a response carrying an error-status (noSuchName, notWritable, …),
//     with the varbind index the agent blamed — every refusal in the
//     capture, at a glance;
//   - a response whose request-id matches no request before it in the
//     capture: a late answer, a duplicate, or somebody else's datagram.
//     The live session counts these as snmp_unsolicited_response.
func (p *Plugin) Validate(ctx context.Context, trames []wiretrace.Trame, opts dhsc.ValidateOpts) (*dhsc.ValidateReport, error) {
	if opts.OutTree != "" || opts.OutParams != "" {
		return nil, ErrValidateOutput
	}
	report := &dhsc.ValidateReport{PerDirection: map[wiretrace.Direction]int{}}
	asked := map[int32]bool{}

	for i, t := range trames {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		raw, err := hex.DecodeString(t.Hex)
		if err != nil {
			report.Errors = append(report.Errors, dhsc.ValidateError{
				TrameIndex: i, Direction: t.Direction, Err: fmt.Sprintf("hex decode: %v", err),
			})
			continue
		}
		m, err := codec.Decode(raw)
		if err = unreadable(m, err); err != nil {
			report.Errors = append(report.Errors, dhsc.ValidateError{
				TrameIndex: i, Direction: t.Direction, HexPrefix: shortHex(raw),
				Err: fmt.Sprintf("snmp decode: %v", err),
			})
			continue
		}
		report.TramesProcessed++
		report.PerDirection[t.Direction]++

		if m.PDU.Type != codec.PDUTypeResponse {
			asked[m.PDU.RequestID] = true
		} else {
			if !asked[m.PDU.RequestID] {
				report.Invariants = append(report.Invariants,
					fmt.Sprintf("trame %d: response to request-id %d, which no request in the capture carries", i, m.PDU.RequestID))
			}
			if m.PDU.ErrorStatus != codec.NoError {
				report.Invariants = append(report.Invariants,
					fmt.Sprintf("trame %d: response error-status %s at varbind %d", i, m.PDU.ErrorStatus, m.PDU.ErrorIndex))
			}
		}

		if opts.StopAt != "" && t.Note == opts.StopAt {
			report.StoppedAt = t.Note
			break
		}
	}
	return report, nil
}

// unreadable is why a datagram cannot be checked offline: it does not
// decode, or it decodes to a message whose PDU is sealed — a v3 message
// under privacy, which only the session that holds the key can open.
func unreadable(m codec.Message, err error) error {
	if err != nil {
		return err
	}
	if m.PDU == nil {
		return errors.New("the PDU is sealed (v3 privacy): it cannot be read without the session's key")
	}
	return nil
}

// shortHex is the first sixteen bytes of a datagram, for an error line a
// reader can orient by.
func shortHex(b []byte) string {
	if len(b) > 16 {
		b = b[:16]
	}
	return hex.EncodeToString(b)
}
