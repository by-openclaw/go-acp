// Package is04 implements the AMWA NMOS IS-04 v1.3.3 codec — the
// Discovery & Registration layer. Stdlib-only (encoding/json, regexp,
// strings, fmt, net, time), spec-strict per
// https://specs.amwa.tv/is-04/releases/v1.3.3/.
//
// Resources covered (one Go struct per top-level resource):
//
//   Node      — the device itself; Node API root
//   Device    — coherent control surface inside a Node
//   Source    — essence origin (audio/video/data/mux)
//   Flow      — specific encoding of a Source
//   Sender    — network egress carrying a Flow
//   Receiver  — network ingress consuming someone else's Sender
//
// Plus the Registration envelope (`{type, data}`) used to POST resources
// to a Registry.
//
// Validation: required-field enforcement, UUID v1-5 pattern,
// `<sec>:<nsec>` version pattern, format/transport URN enums,
// MAC-address pattern for interfaces. Polymorphic format-specific
// constraints (BCP-002 grouphint, BCP-004 receiver caps,
// BCP-006-* media_type rules) land alongside their respective specs.
//
// IS-04 v1.2 / v1.1 back-compat is intentionally deferred — the v1.3
// schemas form a strict superset of the prior versions for our
// purposes, and v1.2-only peers will be added when Phase 1 #4b lands.
package is04
