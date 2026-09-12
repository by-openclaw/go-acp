package rollcall

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"dhs/internal/consumer"
	"dhs/internal/export"
	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/wiretrace"
)

// Validate decodes a captured wire trace offline, with no peer.
//
// This is what makes a committed capture worth committing. A fixture nothing
// can read is a file; a fixture the codec reads back, frame for frame, is a
// test that runs in CI on a clean checkout and fails the day a decoder changes
// its mind about bytes a real device actually sent.
//
// The trace is the device's own words. Every frame here came off an IQ frame
// or a Centra, so a decode failure means the codec is wrong about hardware
// rather than that a test needs updating — which is the whole reason to keep
// the bytes rather than a summary of them.
func (p *Plugin) Validate(ctx context.Context, trames []wiretrace.Trame,
	opts consumer.ValidateOpts) (*consumer.ValidateReport, error) {

	report := &consumer.ValidateReport{
		PerDirection: make(map[wiretrace.Direction]int),
	}

	// What the trace says about itself, which is checked as it is read: a
	// session opened in one generation and answered in the other is the
	// mismatch this connector exists to notice, and it is visible in a
	// capture as plainly as on a wire.
	var sessions int

	for i, t := range trames {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if opts.StopAt != "" && t.Note == opts.StopAt {
			report.StoppedAt = opts.StopAt
			break
		}

		raw, err := hex.DecodeString(t.Hex)
		if err != nil {
			report.Errors = append(report.Errors, consumer.ValidateError{
				TrameIndex: i, Direction: t.Direction,
				HexPrefix: hexPrefix(t.Hex), Err: "not hexadecimal: " + err.Error(),
			})
			continue
		}

		f, used, err := codec.DecodeFrame(raw)
		if err != nil {
			report.Errors = append(report.Errors, consumer.ValidateError{
				TrameIndex: i, Direction: t.Direction,
				HexPrefix: hexPrefix(t.Hex), Err: err.Error(),
			})
			continue
		}
		if used != len(raw) {
			// One record holds one frame. More bytes than the frame claims
			// means the capture ran two together, and a replay that silently
			// dropped the second would test half of what was recorded.
			report.Invariants = append(report.Invariants, fmt.Sprintf(
				"trame %d carries %d bytes after the frame ended", i, len(raw)-used))
		}

		report.TramesProcessed++
		report.PerDirection[t.Direction]++

		if f.Type == codec.MsgCall {
			sessions++
		}
		report.Invariants = append(report.Invariants, frameInvariants(i, f)...)
	}

	if sessions == 0 && report.TramesProcessed > 0 {
		report.Invariants = append(report.Invariants,
			"no session was opened in this trace: every service but status and identity needs one")
	}

	if opts.OutTree != "" {
		if err := p.writeValidatedTree(ctx, opts.OutTree); err != nil {
			return nil, err
		}
	}
	return report, nil
}

// frameInvariants reports what one frame says that cannot be true.
//
// These are the things a decode cannot catch because each field is legal on
// its own: what is wrong is the combination, and only a reader that knows the
// protocol can say so.
func frameInvariants(i int, f codec.Frame) []string {
	var out []string

	// A message of the newer generation on a session that never negotiated it
	// is the mismatch this connector exists to notice. In a trace the
	// negotiation is elsewhere, so what is checked is the weaker thing that
	// still catches a broken peer: a 32-bit message addressed to the
	// broadcast index, which no session can own.
	if f.Type.Generation() == codec.Gen32 && f.Dst.Index == codec.IndexUnknown &&
		f.Type != codec.MsgGetDevInfo {
		out = append(out, fmt.Sprintf(
			"trame %d: %s is a 32-bit message sent outside a session", i, f.Type))
	}

	// Every reply carries the index of the session it belongs to, and a
	// zeroed one on a message that requires a session is a peer answering
	// into nowhere.
	if f.Type == codec.MsgNack || f.Type == codec.MsgAck {
		if f.Src.Index == codec.IndexUnknown && f.Dst.Index == codec.IndexUnknown {
			out = append(out, fmt.Sprintf(
				"trame %d: %s names no session at either end", i, f.Type))
		}
	}
	return out
}

// writeValidatedTree writes what has been walked, which is nothing unless the
// same plugin also walked a device.
//
// A trace is frames rather than a tree: reconstructing one would mean
// replaying a walk against a recording, which is the replay capability
// ADR-0021 defers. What is written here is the canonical export this plugin
// holds, so the flag is honest about being a dump rather than a reconstruction.
func (p *Plugin) writeValidatedTree(ctx context.Context, path string) error {
	tree, err := p.Canonicalize(ctx)
	if err != nil {
		return err
	}

	// Rendered whole before anything is opened, so a failure leaves no file
	// rather than half of one. Encoding a tree this plugin just built cannot
	// fail: the only writer is the buffer, and the tree came from here.
	var buf bytes.Buffer
	_ = export.WriteCanonicalJSON(ctx, &buf, tree)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("rollcall: validate: %w", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("rollcall: validate: %w", err)
	}
	return nil
}

// hexPrefix is the first sixteen bytes of a trame, so a reader can orient
// without the whole thing.
func hexPrefix(h string) string {
	const prefix = 32 // sixteen bytes
	if len(h) <= prefix {
		return h
	}
	return h[:prefix]
}
