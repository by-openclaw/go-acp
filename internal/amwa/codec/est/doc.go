// Package est — canonical payloads + client rules for AMWA
// BCP-003-03 Certificate Provisioning v1.0
// (https://specs.amwa.tv/bcp-003-03/releases/v1.0.0/), which profiles
// EST (RFC 7030) for NMOS systems.
//
// dhs acts as EST CLIENT only (NMOS Servers enrolling for TLS server
// certificates; NMOS Clients fetching the network Root CA). The EST
// server side belongs to the plant's certificate infrastructure.
//
// This package carries the wire-level pieces both the client session
// (session/certmgr) and its tests need:
//
//   - the DNS-SD service type (_nmos-certs._tcp), TXT keys and the
//     RFC 5785 well-known path composition incl. api_selector;
//   - certs-only (degenerate) PKCS#7 SignedData encode/decode — the
//     shape /cacerts and the enroll endpoints speak — implemented on
//     encoding/asn1 because the stdlib has no PKCS#7;
//   - robust base64 handling (the spec requires tolerating line
//     feeds and other RFC 4648 §3 looseness);
//   - PKCS#10 CSR generation per the spec's rules: fresh key pair per
//     CSR, SHA-256+, DNS-resolvable CN/SANs (never IPs), optional
//     serialNumber attribute;
//   - the renewal timing rules (no sooner than 50%, recommended 80%
//     of the certificate lifetime).
package est
