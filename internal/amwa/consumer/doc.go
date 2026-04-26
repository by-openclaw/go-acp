// Package consumer is the Layer-3 NMOS Controller plugin (IS-04 Query
// API client + Node API walker + IS-05/07/08/12 control verbs).
//
// Phase 1 step #1 ships this package empty — no protocol.Factory
// registration yet. The Controller verbs land in Phase 1 step #5
// once IS-04 codec + session/registration are in place.
//
// Until then `dhs consumer nmos discover` is wired directly under
// cmd/dhs/cmd_nmos.go against internal/amwa/session/dnssd — it does
// not need the protocol-plugin layer.
package consumer
