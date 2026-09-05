package http

// The JSON client moved to internal/transport/http — it was never NMOS
// specific, and keeping it here meant the CCM connector could not use it
// (cross-plugin imports are forbidden) and hand-rolled a weaker one.
//
// What stays in this package is the part that IS NMOS: the routed Server
// with the IS-04 §4.4 error body, and the BCP-003-02 auth gate.
//
// Aliased rather than wrapped so the ~30 call sites across consumer,
// provider, registry and session/events keep compiling unchanged —
// including the ones that build a Client with a struct literal.

import (
	transporthttp "dhs/internal/transport/http"
)

// Client is the shared HTTP client transport. See internal/transport/http.
type Client = transporthttp.Client

// DefaultMaxBody caps a single response body.
const DefaultMaxBody = transporthttp.DefaultMaxBody

// DefaultTimeout caps a single HTTP exchange.
const DefaultTimeout = transporthttp.DefaultTimeout

// NewClient returns a Client with the shared defaults.
func NewClient() *Client { return transporthttp.NewClient() }
