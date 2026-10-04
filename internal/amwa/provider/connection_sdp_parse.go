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
	"strings"

	"dhs/internal/amwa/codec/is05"
	"dhs/internal/amwa/codec/sdp"
)

// sdpReceiverLegs extracts the receiver-side transport parameters an
// SDP carries, one set per leg, in leg order.
//
// Leg order is the SDP's: ST 2022-7 senders name it with
// a=group:DUP, and without a group the media sections are the legs as
// written (codec/sdp.Session.Legs). The caller pairs set i with the
// receiver's leg i — a one-leg receiver takes the primary, a two-leg
// receiver takes primary and secondary — rather than folding every
// section into one map where the last c= won and landed on every leg
// (issue #1270: a receiver joining the secondary group twice).
//
// Each set holds only the keys the SDP determined, so the caller
// merges rather than replaces: an SDP that omits a source-filter says
// nothing about source_ip, and overwriting a staged value with ""
// would be reading absence as a decision.
func sdpReceiverLegs(text string) []is05.TransportParams {
	sess, _, err := sdp.Parse(text)
	if err != nil || sess == nil {
		return nil
	}
	// The source the session names, for legs without their own
	// filter: a session-level a=source-filter, else o=, which names
	// whoever wrote the description — usually the transmitter.
	sessionSrc := ""
	for _, a := range sess.Attributes {
		if a.Name != "source-filter" {
			continue
		}
		f := strings.Fields(a.Value)
		if len(f) >= 5 && strings.EqualFold(f[0], "incl") {
			sessionSrc = f[4]
			break
		}
	}
	if sessionSrc == "" && net.ParseIP(sess.Origin.Addr) != nil {
		sessionSrc = sess.Origin.Addr
	}

	legs := sess.Legs()
	out := make([]is05.TransportParams, 0, len(legs))
	for _, l := range legs {
		p := is05.TransportParams{}
		if l.Port > 0 {
			p["destination_port"] = l.Port
		}
		src := l.Src
		if src == "" {
			src = sessionSrc
		}
		if src != "" {
			p["source_ip"] = src
		}
		if l.Dest != "" {
			// A multicast connection address is the GROUP to join; a
			// unicast one is simply where the stream lands. IS-05
			// gives the two different parameters, and putting a
			// unicast address in multicast_ip would have the receiver
			// try to join a group that does not exist.
			if ip := net.ParseIP(l.Dest); ip != nil && ip.IsMulticast() {
				p["multicast_ip"] = l.Dest
			} else {
				p["multicast_ip"] = nil
				p["interface_ip"] = l.Dest
			}
		}
		if len(p) > 0 {
			// An SDP arriving at all means the far end is transmitting
			// RTP. Leaving rtp_enabled false would stage a receiver
			// that has been told everything and will still not listen.
			p["rtp_enabled"] = true
		}
		out = append(out, p)
	}
	return out
}

// sdpReceiverParams is the first leg of sdpReceiverLegs — what a
// single-path receiver takes from an SDP.
func sdpReceiverParams(text string) is05.TransportParams {
	legs := sdpReceiverLegs(text)
	if len(legs) == 0 {
		return is05.TransportParams{}
	}
	return legs[0]
}

// sdpMediaType is the IANA media type the SDP's first media section
// carries — "<m= type>/<rtpmap encoding>", so video/raw, audio/L24,
// video/smpte291, video/jxsv, video/SMPTE2022-6 — or video/MP2T for
// the static payload type 33 an MPEG-TS stream uses with no rtpmap.
// "" when the SDP does not parse or names no media.
func sdpMediaType(text string) string {
	sess, _, err := sdp.Parse(text)
	if err != nil || sess == nil || len(sess.Media) == 0 {
		return ""
	}
	m := sess.Media[0]
	for _, pt := range m.Formats {
		if rm, ok := m.RTPMap[pt]; ok && rm.Encoding != "" {
			return m.Type + "/" + rm.Encoding
		}
		if pt == "33" {
			return m.Type + "/MP2T"
		}
	}
	return ""
}
