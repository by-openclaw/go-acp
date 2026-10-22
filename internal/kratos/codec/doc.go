// Package codec is the wire layer of the Kratos NGC2 IDU CORE API
// (Kratos Antenna Solutions, manual 7592827 Rev. E, June 2023) — the
// antenna controller of a satellite dish.
//
// The API has two halves (§2):
//
//   - a REST API: HTTP (port 3000) or HTTPS (port 4000) GET/POST queries
//     answering a JSON envelope {customerId, system, query, timeMs,
//     parameters[]} (§4.3) whose parameters are table rows;
//   - an MQTT broker for real-time changes (§2.2, §7): topics are the
//     UIDs of the value tables, payloads are the CVT Value text.
//
// There is no WebSocket anywhere in the document.
//
// This package holds what crosses the wire and nothing else: the
// envelope, the rows of the tables a client reads (SVT, CVT,
// enumerations, dependencies, boundaries, alarm points, array labels),
// the postMessage bodies (SET §5.39 and command verbs §9), the UID ↔
// query-path rules, and the MQTT 3.1.1 packets a subscriber exchanges.
// It is stdlib-only and imports nothing from dhs (ADR-0006).
package codec
