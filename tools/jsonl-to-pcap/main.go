// tools/jsonl-to-pcap — one-shot CLI wrapper around
// wiretrace.SynthesisePcap. Used during #62 fixture-capture sessions
// to materialise pcap files from extracted frames.jsonl subsets so
// committed fixtures stay replayable in Wireshark without needing
// a live capture.
//
// Usage:
//
//	go run ./tools/jsonl-to-pcap <frames.jsonl> <out.pcap> [providerPort]
//
// providerPort defaults to 9000 (Ember+ default). Use 9100 for the
// integration-test manifest, 2008 for Probel, etc.
package main

import (
	"fmt"
	"os"
	"strconv"

	"dhs/internal/wiretrace"
)

func main() {
	if len(os.Args) < 3 || len(os.Args) > 4 {
		fmt.Fprintln(os.Stderr, "usage: jsonl-to-pcap <frames.jsonl> <out.pcap> [providerPort]")
		os.Exit(2)
	}
	port := uint16(9000)
	if len(os.Args) == 4 {
		n, err := strconv.ParseUint(os.Args[3], 10, 16)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bad providerPort %q: %v\n", os.Args[3], err)
			os.Exit(2)
		}
		port = uint16(n)
	}
	in, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "open %s: %v\n", os.Args[1], err)
		os.Exit(1)
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(os.Args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "create %s: %v\n", os.Args[2], err)
		os.Exit(1)
	}
	defer func() { _ = out.Close() }()
	if err := wiretrace.SynthesisePcap(in, out, port); err != nil {
		fmt.Fprintf(os.Stderr, "synthesise: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "wrote %s (port=%d)\n", os.Args[2], port)
}
