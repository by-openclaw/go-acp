package acp2

import "time"

// acp2StaleAfter is the rolling-window threshold past which the session is
// judged not Live. Mirrors ACP1's 90 s default — the broadcast-industry
// baseline for slow announce cadence; per-protocol tuning is available via
// the cross-protocol --keepalive-timeout flag.
const acp2StaleAfter = 90 * time.Second

// Plugin reports health through the embedded *consumer.Health (see
// plugin.go). This file is all that is ACP2-specific about it.
//
// It used to be seventy lines: a SessionHealth method and a private
// probeReachable that were byte-for-byte the same as ACP1's, in a package
// that could not share them. Both copies also dialled TCP to decide
// Reachable, which is why this file was on the transport allowlist.
//
// The time source is the Session rather than the injected metrics.Connector,
// because ACP2 does not yet count frames through it. Session.LastRx and
// Session.LastTx are stored by readLoop and sendFrame on every frame, so
// announces, replies and keep-alive responses all refresh liveness.
