// tools/scan-glow-tags — one-off scanner that reads a wiretrace
// frames.jsonl and reports which Glow APPLICATION tag appears in
// every frame's S101-EmBER payload. Used by R12 #473 / #62 fixture
// integration: split captured walk frames by protocol type so the
// 5 missing protocol_types/<type>/ buckets can be populated from a
// real producer run.
//
// Usage:
//
//	go run ./tools/scan-glow-tags <path/to/walk.jsonl>
package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"

	"dhs/internal/emberplus/codec/ber"
	"dhs/internal/emberplus/codec/s101"
	"dhs/internal/wiretrace"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: scan-glow-tags <walk.jsonl>")
		os.Exit(2)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "open: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = f.Close() }()

	trames, err := wiretrace.ReadTrames(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read trames: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "scanning %d frames...\n", len(trames))

	// Per APP-tag-number → list of frame indices.
	perTag := map[uint32][]int{}
	emberFrames := 0
	for idx, t := range trames {
		raw, err := hex.DecodeString(t.Hex)
		if err != nil {
			continue
		}
		// Walk every S101 frame in the raw bytes (one TCP segment may
		// carry multiple keepalives + EmBER frames).
		r := s101.NewReader(bytes.NewReader(raw))
		for {
			frame, err := r.ReadFrame()
			if err != nil {
				break
			}
			if frame.Command != s101.CmdEmBER {
				continue
			}
			emberFrames++
			tlvs, err := ber.DecodeAll(frame.Payload)
			if err != nil {
				continue
			}
			for _, top := range tlvs {
				collectAPPTags(top, idx, perTag)
			}
		}
	}
	fmt.Fprintf(os.Stderr, "ember frames decoded: %d\n", emberFrames)

	names := map[uint32]string{
		0:  "Root",
		1:  "Parameter",
		2:  "Command",
		3:  "Node",
		4:  "ElementCollection",
		5:  "StreamEntry",
		6:  "StreamCollection",
		9:  "QualifiedParameter",
		10: "QualifiedNode",
		11: "RootElementCollection",
		12: "StreamDescription",
		13: "Matrix",
		14: "Target",
		15: "Source",
		16: "Connection",
		17: "QualifiedMatrix",
		18: "Label",
		19: "Function",
		20: "QualifiedFunction",
		21: "TupleItemDescription",
		22: "Invocation",
		23: "InvocationResult",
		24: "Template",
		25: "QualifiedTemplate",
	}
	// Print in tag-number order.
	for tag := uint32(0); tag <= 25; tag++ {
		ids := perTag[tag]
		if len(ids) == 0 {
			continue
		}
		name := names[tag]
		if name == "" {
			name = "?"
		}
		display := ids
		more := ""
		if len(display) > 10 {
			display = display[:10]
			more = fmt.Sprintf(" (... %d more)", len(ids)-10)
		}
		fmt.Printf("APP %2d  %-22s  frames=%d  first=%v%s\n", tag, name, len(ids), display, more)
	}
}

func collectAPPTags(tlv ber.TLV, frameIdx int, out map[uint32][]int) {
	if tlv.Tag.Class == ber.ClassApplication {
		out[tlv.Tag.Number] = append(out[tlv.Tag.Number], frameIdx)
	}
	for _, ch := range tlv.Children {
		collectAPPTags(ch, frameIdx, out)
	}
}
