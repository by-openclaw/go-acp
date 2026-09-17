package osc

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/export"
	"dhs/internal/osc/codec"
	"dhs/internal/wiretrace"
)

// Compile-time assertion: the OSC Plugin satisfies consumer.Validator so
// `dhs consumer osc-vXX validate <frames.jsonl>` resolves at the CLI
// type-assert (cmd/dhs/cmd_validate.go). Closes the osc half of #243.
var _ consumer.Validator = (*Plugin)(nil)

// Validate decodes captured OSC wire-trace records (Trames per
// ADR-0021) through the stdlib-only codec, offline — the replay
// counterpart of the live listener. Coverage:
//
//   - UDP captures: one OSC packet (message or bundle) per trame.
//   - TCP captures: OSC 1.0 length-prefix and OSC 1.1 SLIP framings are
//     unwrapped transparently (a trame is one recorded read, so it
//     carries exactly one framed packet).
//   - Bundles recurse; every contained message counts.
//
// --out-tree aggregates the OBSERVED ADDRESS SPACE (last value wins per
// address) into a canonical snapshot tree.json — turning a live capture
// into a replayable address catalogue; --out-params emits the same rows
// as a flat dump (.csv by extension, JSON otherwise).
func (p *Plugin) Validate(ctx context.Context, trames []wiretrace.Trame, opts consumer.ValidateOpts) (*consumer.ValidateReport, error) {
	report := &consumer.ValidateReport{
		PerDirection: map[wiretrace.Direction]int{},
	}

	collect := opts.OutTree != "" || opts.OutParams != ""
	objects := map[string]consumer.Object{}
	var order []string

	record := func(m codec.Message) {
		if !collect {
			return
		}
		if _, seen := objects[m.Address]; !seen {
			order = append(order, m.Address)
		}
		obj := consumer.Object{
			ID:     len(order) - 1,
			Path:   splitOSCAddress(m.Address),
			Label:  m.Address,
			Access: 0x01, // observed = readable; OSC declares no access model
			Meta: map[string]any{
				"address": m.Address,
				"typetag": string(argTags(m.Args)),
				"args":    len(m.Args),
			},
		}
		if o, seen := objects[m.Address]; seen {
			obj.ID = o.ID // keep the first-seen ID stable
		}
		if len(m.Args) > 0 {
			obj.Kind, obj.Value = oscArgToValue(m.Args[0])
		}
		objects[m.Address] = obj
	}

	var walk func(pkt codec.Packet, idx int)
	walk = func(pkt codec.Packet, idx int) {
		switch v := pkt.(type) {
		case codec.Message:
			record(v)
			for _, n := range v.Notes {
				report.Invariants = append(report.Invariants,
					fmt.Sprintf("trame %d: %s%s", idx, n.Kind, noteDetail(n.Detail)))
			}
		case codec.Bundle:
			// Bundle-level Notes don't exist in the codec today — only
			// messages carry deviations. Recurse into the elements.
			for _, el := range v.Elements {
				walk(el, idx)
			}
		}
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
		pkt, err := decodeOSCTrame(raw)
		if err != nil {
			report.Errors = append(report.Errors, consumer.ValidateError{
				TrameIndex: i,
				Direction:  t.Direction,
				HexPrefix:  oscShortHex(raw),
				Err:        fmt.Sprintf("osc decode: %v", err),
			})
			continue
		}

		report.TramesProcessed++
		report.PerDirection[t.Direction]++
		walk(pkt, i)

		if opts.StopAt != "" && t.Note == opts.StopAt {
			report.StoppedAt = t.Note
			break
		}
	}

	if collect {
		ordered := make([]consumer.Object, 0, len(order))
		for _, addr := range order {
			ordered = append(ordered, objects[addr])
		}
		snap := &export.Snapshot{
			Device: export.DeviceInfo{
				Protocol: p.version.name(),
			},
			Generator: "dhs validate --out-tree (osc)",
			CreatedAt: time.Now().UTC(),
			Slots: []export.SlotDump{{
				Slot:     0,
				Status:   consumer.SlotPresent.String(),
				WalkedAt: time.Now().UTC(),
				Objects:  ordered,
			}},
		}
		if opts.OutTree != "" {
			if err := writeOSCSnapshot(opts.OutTree, snap, true); err != nil {
				return report, fmt.Errorf("write out-tree: %w", err)
			}
		}
		if opts.OutParams != "" {
			if err := writeOSCSnapshot(opts.OutParams, snap, false); err != nil {
				return report, fmt.Errorf("write out-params: %w", err)
			}
		}
	}

	return report, nil
}

// decodeOSCTrame decodes one recorded read: a bare UDP packet, an OSC
// 1.0 length-prefixed TCP read, or an OSC 1.1 SLIP-framed TCP read.
func decodeOSCTrame(raw []byte) (codec.Packet, error) {
	if pkt, err := codec.DecodePacket(raw); err == nil {
		return pkt, nil
	} else if len(raw) == 0 {
		return nil, err
	}
	// SLIP framing (OSC 1.1 over TCP): frames delimited by 0xC0.
	if raw[0] == 0xC0 {
		r := codec.NewSLIPReader(bytes.NewReader(raw), len(raw))
		inner, rerr := r.ReadPacket()
		if rerr != nil {
			return nil, fmt.Errorf("slip unwrap: %w", rerr)
		}
		return codec.DecodePacket(inner)
	}
	// Length-prefix framing (OSC 1.0 over TCP): int32 size + packet.
	r := codec.NewLenPrefixReader(bytes.NewReader(raw), len(raw))
	inner, rerr := r.ReadPacket()
	if rerr != nil {
		return nil, fmt.Errorf("len-prefix unwrap: %w", rerr)
	}
	return codec.DecodePacket(inner)
}

// splitOSCAddress maps "/mixer/ch/1/gain" onto the canonical path
// segments the tree renderer / provider expect.
func splitOSCAddress(addr string) []string {
	parts := strings.Split(strings.TrimPrefix(addr, "/"), "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{addr}
	}
	return out
}

// argTags renders the observed type-tag string ("ifs" …).
func argTags(args []codec.Arg) []byte {
	tags := make([]byte, 0, len(args))
	for _, a := range args {
		tags = append(tags, a.Tag)
	}
	return tags
}

// oscArgToValue maps one OSC argument onto the canonical Value model.
func oscArgToValue(a codec.Arg) (consumer.ValueKind, consumer.Value) {
	switch a.Tag {
	case 'i':
		return consumer.KindInt, consumer.Value{Kind: consumer.KindInt, Int: int64(a.Int32)}
	case 'h':
		return consumer.KindInt, consumer.Value{Kind: consumer.KindInt, Int: a.Int64}
	case 'f':
		return consumer.KindFloat, consumer.Value{Kind: consumer.KindFloat, Float: float64(a.Float32)}
	case 'd':
		return consumer.KindFloat, consumer.Value{Kind: consumer.KindFloat, Float: a.Float64}
	case 's', 'S':
		return consumer.KindString, consumer.Value{Kind: consumer.KindString, Str: a.String}
	case 'b':
		return consumer.KindRaw, consumer.Value{Kind: consumer.KindRaw, Raw: a.Blob}
	case 't':
		return consumer.KindUint, consumer.Value{Kind: consumer.KindUint, Uint: a.Uint64}
	case 'T':
		return consumer.KindBool, consumer.Value{Kind: consumer.KindBool, Bool: true}
	case 'F':
		return consumer.KindBool, consumer.Value{Kind: consumer.KindBool, Bool: false}
	}
	// N / I / arrays and future tags: observed but valueless.
	return consumer.KindUnknown, consumer.Value{Kind: consumer.KindUnknown}
}

// writeOSCSnapshot writes the aggregated snapshot: forceJSON pins the
// canonical tree.json shape; otherwise the extension picks the format
// (.csv → CSV, anything else → JSON), mirroring the export verb.
func writeOSCSnapshot(path string, snap *export.Snapshot, forceJSON bool) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if !forceJSON && strings.EqualFold(filepath.Ext(path), ".csv") {
		return export.WriteCSV(f, snap)
	}
	return export.WriteJSON(f, snap)
}

func noteDetail(detail string) string {
	if detail == "" {
		return ""
	}
	return " (" + detail + ")"
}

func oscShortHex(b []byte) string {
	if len(b) > 16 {
		b = b[:16]
	}
	return hex.EncodeToString(b)
}
