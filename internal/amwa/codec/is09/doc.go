// Package is09 implements the AMWA NMOS IS-09 v1.0.0 System API
// codec. Stdlib-only (encoding/json + regexp + strings), spec-strict
// per https://specs.amwa.tv/is-09/releases/v1.0.0/.
//
// Scope:
//
//   - The /global resource (resource_core + is04 + ptp + optional
//     syslog/syslogv2).
//   - Schema validation: required fields, integer ranges, regex
//     patterns for id (uuid) and version (TAI timestamp).
//   - Encoder rejects unknown keys; decoder rejects required missing
//     and out-of-range values.
//
// IS-09 v1.0 predates IS-10, so the DNS-SD TXT layer for
// `_nmos-system._tcp` MUST NOT advertise `api_auth` (per
// https://specs.amwa.tv/is-09/releases/v1.0.0/docs/3.1._Discovery_-_Operation.html).
// That rule lives in the provider plugin, not in this package.
package is09
