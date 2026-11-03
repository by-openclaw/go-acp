// Package codec is the stdlib-only XML-RPC wire codec for the Riedel
// Router Control Software (RRCS) interface, specification 7.40 Rev 2.
// It owns:
//
//   - The XML-RPC value model of §5.6: int, boolean, string, double,
//     dateTime.iso8601, base64, struct and array.
//   - Both directions of the exchange: the requests we send and the
//     answers RRCS gives (§8), and the notifications RRCS sends us and
//     the answers we give (§9). So each of call and response has an
//     encoder and a decoder.
//   - The helper types of §6: TPortAddress, TGroupPortAddress,
//     TConferencePortAddress and TMemberChangeList.
//   - The transaction key of §6.5 and the error codes of §7.
//
// It emits and consumes XML bytes only. Sockets, HTTP and sessions live
// in the consumer.
//
// Library independence: stdlib-only, no dhs/* imports. Lift-ready per
// ADR-0006.
package codec
