// Package consumer talks to an RRCS gateway.
//
// The protocol runs in two directions over two separate HTTP channels
// (§5.4, §9):
//
//   - Client sends requests to RRCS (HTTP POST, TCP 8193 by default).
//   - Listener is the HTTP endpoint RRCS calls back with its
//     notifications and its GetAlive ping, once the control system has
//     registered with RegisterForAllEvents (§8.15.1).
//
// Section numbers refer to "RRCS Interface Specification" 9.0.1 Rev 1.
package consumer
