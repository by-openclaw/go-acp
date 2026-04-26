// Package provider is the Layer-3 NMOS Node plugin (IS-04 Node API
// server + outbound registration client + IS-05/07/08/12 server-side
// surfaces).
//
// Phase 1 step #1 ships this package empty — no provider.Factory
// registration yet. Node implementation lands in Phase 1 step #3 once
// the IS-04 codec is in place.
//
// Until then `dhs producer nmos serve --mdns-only` is wired directly
// under cmd/dhs/cmd_nmos.go against internal/amwa/session/dnssd — it
// announces a placeholder Node instance via mDNS but does not yet
// serve the Node API.
package provider
