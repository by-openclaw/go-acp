// Package sdp decodes SDP transport files (RFC 4566) as used by AMWA NMOS
// IS-04 (sender manifest_href) and IS-05 (transport_file).
//
// Scope is the ST 2110 profile a broadcast node actually publishes: the
// multicast address per SMPTE 2022-7 leg, the PTP reference clock, the RTP
// payload mapping and the essence parameters. Everything the parser does not
// model is preserved verbatim rather than discarded, so a caller can always
// see what the device really sent.
//
// Stdlib only (ADR-0006): this package never imports dhs/*.
package sdp

import "strings"

// Attribute is one `a=` line. Value is empty for a property attribute
// (`a=recvonly`); for `a=<name>:<value>` Value holds everything after the
// first colon, untrimmed on the right so nothing is silently altered.
type Attribute struct {
	Name  string
	Value string
	Raw   string // the line as received, without the leading "a="
}

// Origin is the `o=` line.
type Origin struct {
	Username       string
	SessionID      string
	SessionVersion string
	NetType        string
	AddrType       string
	Address        string
}

// Connection is a `c=` line. For IPv4 multicast the address carries a TTL
// (`239.0.0.1/32`) and optionally an address count (`239.0.0.1/32/2`).
type Connection struct {
	NetType  string
	AddrType string
	Address  string // address only, TTL and count stripped
	TTL      int    // 0 when absent
	Count    int    // 0 when absent
	Raw      string
}

// Timing is a `t=` line.
type Timing struct {
	Start string
	Stop  string
}

// Group is an `a=group:<semantics> <tag>...` line. ST 2022-7 redundancy uses
// `a=group:DUP primary secondary`, whose tags refer to `a=mid` values.
type Group struct {
	Semantics string
	Tags      []string
}

// RTPMap is an `a=rtpmap:<pt> <encoding>/<clock>[/<channels>]` line.
type RTPMap struct {
	PayloadType string
	Encoding    string // raw, L24, smpte291, ...
	ClockRate   int
	Channels    int // 0 when absent (video)
}

// FMTP is an `a=fmtp:<pt> <k=v>; <k=v>; ...` line. Params keeps source order
// because the order carries meaning when comparing two devices' output.
type FMTP struct {
	PayloadType string
	Params      []Param
	Raw         string
}

// Param is one `key=value` from an fmtp line. A bare flag has an empty Value.
type Param struct {
	Key   string
	Value string
}

// Get returns the first value for key and whether it was present.
func (f FMTP) Get(key string) (string, bool) {
	for _, p := range f.Params {
		if strings.EqualFold(p.Key, key) {
			return p.Value, true
		}
	}
	return "", false
}

// TSRefClk is an `a=ts-refclk:ptp=<version>:<gmid>:<domain>` line — the check
// that tells you whether a sender is locked to the fabric grandmaster or is
// quoting its own clock identity.
type TSRefClk struct {
	Version string // IEEE1588-2008
	GMID    string // grandmaster clock identity, e.g. 00-90-56-FF-FE-08-6D-42
	Domain  int    // -1 when absent
	Raw     string
}

// SourceFilter is an `a=source-filter: <incl|excl> <nettype> <addrtype>
// <dest> <src>...` line (RFC 4570), i.e. source-specific multicast.
type SourceFilter struct {
	Mode     string // incl | excl
	NetType  string
	AddrType string
	Dest     string
	Sources  []string
	Raw      string
}

// Media is one `m=` section.
type Media struct {
	Type       string // audio | video | application
	Port       int
	PortCount  int // from `m=video 5004/2`, 0 when absent
	Proto      string
	Formats    []string
	Connection *Connection
	Mid        string // a=mid — the tag a group refers to
	PTime      string // a=ptime
	MediaClk   string // a=mediaclk
	RTPMap     map[string]RTPMap
	FMTP       map[string]FMTP
	TSRefClk   *TSRefClk
	SourceFilt *SourceFilter
	Attributes []Attribute // every a= line in this section, in order
}

// Session is a parsed SDP document.
type Session struct {
	Version    int
	Origin     Origin
	Name       string
	Info       string
	Timing     []Timing
	Connection *Connection // session-level c=, if any
	Groups     []Group
	Attributes []Attribute // session-level a= lines, in order
	Media      []Media
}

// Leg is one side of an SMPTE 2022-7 pair, resolved from a=group + a=mid.
type Leg struct {
	Mid      string // primary | secondary (whatever the device used)
	Index    int    // position of the m= section in the document
	Dest     string // multicast destination from the section's c=
	Port     int
	Sources  []string // from a=source-filter, when present
	MediaIdx int      // index into Session.Media
}

// Legs pairs the tags of the first `a=group:DUP` with their `m=` sections.
// A document with no DUP group yields one Leg per media section, so callers
// do not need a special case for an unprotected stream.
func (s *Session) Legs() []Leg {
	byMid := make(map[string]int, len(s.Media))
	for i, m := range s.Media {
		if m.Mid != "" {
			byMid[m.Mid] = i
		}
	}

	leg := func(idx, pos int) Leg {
		m := s.Media[idx]
		l := Leg{Mid: m.Mid, Index: pos, Port: m.Port, MediaIdx: idx}
		if m.Connection != nil {
			l.Dest = m.Connection.Address
		} else if s.Connection != nil {
			l.Dest = s.Connection.Address
		}
		if m.SourceFilt != nil {
			l.Sources = m.SourceFilt.Sources
		}
		return l
	}

	for _, g := range s.Groups {
		if !strings.EqualFold(g.Semantics, "DUP") {
			continue
		}
		out := make([]Leg, 0, len(g.Tags))
		for pos, tag := range g.Tags {
			idx, ok := byMid[tag]
			if !ok {
				continue // a tag with no matching m= is reported as a Deviation at parse time
			}
			out = append(out, leg(idx, pos))
		}
		if len(out) > 0 {
			return out
		}
	}

	out := make([]Leg, 0, len(s.Media))
	for i := range s.Media {
		out = append(out, leg(i, i))
	}
	return out
}
