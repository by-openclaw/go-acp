// Package codec is the SNMP wire format: BER-encoded messages carrying
// v1, v2c and v3 PDUs, and the variable bindings inside them.
//
// # The contract
//
// Encode takes a [Message] and returns the exact bytes that belong in one
// UDP datagram. Decode takes one datagram and returns the [Message] it
// carries, or an error naming what it could not read. The two are
// inverses for every message this package can build.
//
// Nothing here opens a socket, keeps a session, retries, or knows what an
// OID means. A caller that wants those wants internal/snmp/consumer or
// internal/snmp/provider; this package is the alphabet they both spell
// in. Per ADR-0006 it is stdlib-only and imports nothing from dhs/.
//
// # Why the BER is written here rather than borrowed
//
// The tree already has a BER codec at internal/emberplus/codec/ber, and
// this package deliberately does not use it. Two reasons. ADR-0006 keeps
// every codec free of intra-repo imports, so a protocol's wire format can
// never drift because a neighbour refactored. And the two BER subsets
// barely overlap: Ember+ needs relative OIDs, real numbers and indefinite
// lengths, none of which appear in SNMP; SNMP needs application-tagged
// Counter32/Gauge32/TimeTicks/IpAddress/Counter64 and the three
// context-tagged exception values, none of which appear in Ember+. What
// they share is the tag-length-value shape, which is thirty lines.
//
// encoding/asn1 is not used either: it cannot express the
// application-class tags above without a struct tag per type, it rejects
// the non-minimal integers real agents emit, and it allocates a reflect
// walk per varbind on a path that runs per polled OID.
//
// # Versions
//
// v1 (version field 0) and v2c (1) are community-framed: the message is
// SEQUENCE { version, community, PDU }. v3 (3) replaces the community
// with a header and a security-parameters blob and is handled in v3.go.
//
// v1 traps are a DIFFERENT PDU from v2c notifications — enterprise,
// agent-address, generic-trap, specific-trap and time-stamp are fields of
// the PDU itself, where v2c carries the same facts as the first two
// varbinds of an ordinary PDU. They are two types here ([TrapV1] and a
// [PDU] with [PDUTypeTrapV2]), never one with a version flag, because a
// flag is how the two get confused at the one call site that matters.
//
// # What a decoder must tolerate
//
// Real agents are not careful. This decoder accepts non-minimal integer
// encodings and multi-byte lengths that could have been short, because
// refusing them means refusing devices in the field — see
// [ErrMalformed] for what it does refuse. Deviations that are absorbed
// are counted through internal/consumer/compliance by the caller, which
// is why this package returns facts rather than logging them.
package codec
