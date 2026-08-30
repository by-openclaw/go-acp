// Package edid implements AMWA BCP-005-01: mapping E-EDID (VESA
// Enhanced EDID, base block 1.3/1.4 + CTA-861 extension) into NMOS
// Receiver Capabilities — BCP-004-01 Constraint Sets keyed by the
// urn:x-nmos:cap:* parameter-constraint URNs.
//
// The mapping is what BCP-005-01 specifies for a Receiver associated
// with an IS-11 Output whose downstream counterpart provides an EDID:
// each supported video mode / audio descriptor becomes a Constraint
// Set a controller can match a Sender against.
//
// stdlib-only (ADR-0006). The AMWA Testing Tool ships no automated
// BCP-005-01 suite (BCP0050101Test is a stub), so the oracle is the
// spec's own Examples.md byte vectors, pinned in edid_test.go.
//
// Coverage (with spec citations in the code):
//   - Base EDID header validation + Established Timings I/II/III,
//     Standard Timings, Detailed Timing Descriptors, base-block
//     colour subsampling (feature byte) and colour bit depth;
//   - CTA-861 extension: Short Video Descriptors (VIC subset),
//     Short Audio Descriptors, basic-audio default, CTA colour
//     subsampling header.
//
// Where the spec leaves a mapping "implementation specific" (e.g.
// fractional vs integer vertical rates) the emitter takes the
// integer rate and documents the choice at the call site.
package edid
