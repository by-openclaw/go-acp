package main

import (
	"fmt"

	"dhs/internal/errcode"
)

// R13 #474 v1: named bench profile registry. Each profile carries
// the n + op defaults for a recognised benchmark scenario. v1
// dispatches the defaults; v2 layers RFC 2544 ramp-up logic +
// percentile latency capture + recovery-time measurement.
var errBenchProfileUnknown = errcode.New(errcode.LayerValidation, "bench-profile-unknown", errcode.ClassUsage)

type benchProfile struct {
	N    int
	Op   string
	Note string // human-readable description shown in errors / help
}

var benchProfiles = map[string]benchProfile{
	"rfc2544-throughput": {N: 10000, Op: "connect", Note: "RFC 2544 throughput — 10k connects, measure ops/sec"},
	"rfc2544-latency":    {N: 1000, Op: "absolute", Note: "RFC 2544 latency — 1k absolute writes, measure per-op tail"},
	"rfc2544-recovery":   {N: 500, Op: "disconnect", Note: "RFC 2544 recovery — 500 disconnects, measure post-burst stabilisation"},
}

// resolveBenchProfile maps the --profile flag onto its (n, op)
// defaults. Unknown name returns validation:bench-profile-unknown
// (CLI exit 2).
func resolveBenchProfile(name string) (n int, op string, err error) {
	p, ok := benchProfiles[name]
	if !ok {
		known := make([]string, 0, len(benchProfiles))
		for k := range benchProfiles {
			known = append(known, k)
		}
		return 0, "", fmt.Errorf("%w: --profile=%q; known: %v", errBenchProfileUnknown, name, known)
	}
	return p.N, p.Op, nil
}
