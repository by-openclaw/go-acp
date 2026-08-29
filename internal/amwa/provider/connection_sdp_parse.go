// Layer-3 -- reading an SDP a controller PATCHed into a Receiver.
//
// This is the other half of connection_sdp.go, and the asymmetry is
// the spec's: a Sender PUBLISHES an SDP describing what it transmits,
// and a Receiver is GIVEN that same SDP and must work out its own
// transport_params from it. A controller does not translate between
// the two -- IS-05 §4.3 has it copy `transport_file.data` verbatim --
// so a Receiver that accepts the file and leaves its parameters unset
// has accepted a connection it will not make.
//
// Only the fields a Receiver actually needs are read. This is not a
// general SDP parser and should not become one: the Receiver needs to
// know where the stream comes from, where it arrives, and on what
// port, and every other line in an ST 2110 SDP describes the essence,
// which IS-04 already told it.

package provider

import (
	"net"
	"strconv"
	"strings"

	"dhs/internal/amwa/codec/is05"
)

// sdpReceiverParams extracts the receiver-side transport parameters
// carried by an SDP.
//
// Returns only the keys it could determine, so the caller merges
// rather than replaces: an SDP that omits a source-filter says nothing
// about source_ip, and overwriting a staged value with "" would be
// reading absence as a decision.
func sdpReceiverParams(sdp string) is05.TransportParams {
	out := is05.TransportParams{}
	var connIP string
	var filterSource string

	for _, raw := range strings.Split(sdp, "\n") {
		line := strings.TrimRight(raw, "\r")
		switch {
		case strings.HasPrefix(line, "c="):
			// c=IN IP4 <address>[/ttl[/count]]
			f := strings.Fields(strings.TrimPrefix(line, "c="))
			if len(f) >= 3 {
				connIP = strings.SplitN(f[2], "/", 2)[0]
			}

		case strings.HasPrefix(line, "m="):
			// m=<media> <port> <proto> <fmt...>
			f := strings.Fields(strings.TrimPrefix(line, "m="))
			if len(f) >= 2 {
				if p, err := strconv.Atoi(strings.SplitN(f[1], "/", 2)[0]); err == nil && p > 0 {
					out["destination_port"] = p
				}
			}

		case strings.HasPrefix(line, "a=source-filter:"):
			// a=source-filter: incl IN IP4 <dest> <src> [<src>...]
			//
			// This is the authoritative source address when present:
			// it is what an SSM join actually filters on, so it beats
			// anything inferred from o= (which names whoever WROTE the
			// session description, not necessarily the transmitter).
			f := strings.Fields(strings.TrimPrefix(line, "a=source-filter:"))
			if len(f) >= 5 && strings.EqualFold(f[0], "incl") {
				filterSource = f[4]
			}

		case strings.HasPrefix(line, "o=") && filterSource == "":
			// o=<user> <sess-id> <sess-ver> IN IP4 <address>
			f := strings.Fields(strings.TrimPrefix(line, "o="))
			if len(f) >= 6 && net.ParseIP(f[5]) != nil {
				filterSource = f[5]
			}
		}
	}

	if filterSource != "" {
		out["source_ip"] = filterSource
	}
	if connIP != "" {
		// A multicast connection address is the GROUP to join; a
		// unicast one is simply where the stream lands. IS-05 gives
		// the two different parameters, and putting a unicast address
		// in multicast_ip would have the receiver try to join a group
		// that does not exist.
		if ip := net.ParseIP(connIP); ip != nil && ip.IsMulticast() {
			out["multicast_ip"] = connIP
		} else {
			out["multicast_ip"] = nil
			out["interface_ip"] = connIP
		}
	}
	if len(out) > 0 {
		// An SDP arriving at all means the far end is transmitting
		// RTP. Leaving rtp_enabled false would stage a receiver that
		// has been told everything and will still not listen.
		out["rtp_enabled"] = true
	}
	return out
}
