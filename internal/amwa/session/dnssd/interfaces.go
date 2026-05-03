package dnssd

import (
	"context"

	"dhs/internal/amwa/codec/dnssd"
)

// Browser scans the link for instances of one or more service types and
// emits a stream of [dnssd.Instance] discoveries on the channel returned
// by [Browse]. Implementations include:
//
//   - [stdlibBrowser]  — pure-Go, opens a multicast socket on
//     224.0.0.251:5353 and decodes raw packets per RFC 6762/6763. Default
//     fallback; works on every platform but introduces sub-second
//     read-deadline jitter that fails AMWA cascade-timing tests under
//     tight cycles.
//
//   - [avahiBrowser]   — Linux only, Avahi via DBus
//     (`org.freedesktop.Avahi.Server`). Sub-millisecond delivery via
//     kernel callback; full RFC 6762/6763 corner-case handling
//     (probe-then-claim, conflict resolution, IP renumbering). Picked at
//     runtime by [NewBrowser] when avahi-daemon is reachable on the
//     system bus.
//
//   - bonjourBrowser — macOS / Windows, CGo to dnssd.dll / libSystem
//     (planned). Same advantages as Avahi via the canonical Bonjour API.
//
// Implementations must be safe to call Browse and Close concurrently,
// and Browse may be called multiple times before Close — each call
// receives its own filtered channel.
type Browser interface {
	// Browse runs a query/listen loop until ctx is cancelled. Discovered
	// instances are sent to the returned channel, which is closed when
	// the browse loop exits. The same instance may be reported multiple
	// times — callers should de-duplicate by FullName().
	Browse(ctx context.Context, service string) (<-chan dnssd.Instance, error)
	// Close stops every active Browse loop and releases the underlying
	// transport (sockets / DBus connection). Idempotent.
	Close() error
}

// Responder advertises one or more [dnssd.Instance] entries on the link,
// answering queries and emitting unsolicited announcements per RFC 6762
// §8.3. Implementations include:
//
//   - [stdlibResponder] — pure-Go reference; opens its own multicast
//     socket and replies to PTR/SRV/TXT queries it sees on the wire.
//
//   - [avahiResponder]  — Linux only, Avahi via DBus EntryGroup. Avahi
//     handles the probe-then-claim conflict resolution dance the
//     stdlib path doesn't. Picked at runtime by [NewResponder].
//
//   - bonjourResponder — macOS / Windows (planned).
type Responder interface {
	// Announce starts emitting an Instance on the link. Returns once the
	// initial announcements are queued; the responder continues to reply
	// to queries until Close is called or ctx is cancelled.
	Announce(ctx context.Context, ins dnssd.Instance) error
	// Update replaces the TXT (and only the TXT) of a previously-Announced
	// Instance, matched by FullName(). Other fields on ins are ignored
	// — Host/Port/IP changes require a Close + fresh Announce. Used by
	// Peer-to-Peer Nodes (IS-04 §3.1.1) to bump the `ver_*` counters
	// without tearing down the announcement; per RFC 6762 §10.2 the
	// re-emission carries the cache-flush bit on the TXT record so peers
	// refresh their cache. Returns an error if the Instance has not been
	// Announced first.
	Update(ctx context.Context, ins dnssd.Instance) error
	// Close emits goodbye packets (TTL=0) on every advertised Instance
	// per RFC 6762 §10.1, then shuts the transport. Idempotent.
	Close() error
}
