// Package sdp is a stdlib-only parser for the SDP transport files
// ST 2110 senders publish over IS-05 (issue #827).
//
// SDP is where the wire truth lives: the multicast address per 2022-7
// leg, the PTP grandmaster and domain, the essence parameters
// (rtpmap/fmtp), the source filter. The parser's posture mirrors the
// connector compliance model: PRESERVE, NEVER INVENT — unknown
// attributes and lines are kept verbatim in order, absent fields stay
// zero and are never defaulted, and a malformed-but-recoverable line
// becomes a Deviation while only a structurally impossible document
// is an error.
//
// ADR-0006: this package imports the standard library only and is
// lift-to-own-repo ready. It never imports dhs/*.
package sdp

// Session is one parsed SDP document.
type Session struct {
	Version    int         // v=
	Origin     Origin      // o=
	Name       string      // s=
	Timing     []Timing    // t=
	Connection *Connection // session-level c=, if present
	Groups     []Group     // a=group:<semantics> <tags...>
	Attributes []Attribute // every other session-level a=, in order
	Extra      []string    // non-modelled session-level lines (b=, i=, u=, …), verbatim
	Media      []Media     // m= sections in order
}

// Origin is the o= line.
type Origin struct {
	Username    string
	SessID      string
	SessVersion string
	NetType     string
	AddrType    string
	Addr        string
}

// Timing is one t= line.
type Timing struct {
	Start string
	Stop  string
}

// Connection is a c= line.
type Connection struct {
	NetType  string
	AddrType string
	Addr     string // address with any /ttl or /count suffix stripped
	TTL      string // multicast TTL suffix when present, verbatim
}

// Group is a session-level a=group line — ST 2022-7 uses
// "a=group:DUP primary secondary".
type Group struct {
	Semantics string
	Tags      []string
}

// Attribute is one a= line kept verbatim: Name is the token before
// the first ':', Value everything after it ("" for flag attributes).
type Attribute struct {
	Name  string
	Value string
}

// Media is one m= section.
type Media struct {
	Type       string // audio | video | application
	Port       int
	Proto      string   // e.g. RTP/AVP
	Formats    []string // payload types
	Connection *Connection
	Mid        string            // a=mid → "primary" / "secondary"
	RTPMap     map[string]RTPMap // by payload type
	FMTP       map[string]FMTP   // by payload type
	TSRefClk   *TSRefClk
	MediaClk   string // a=mediaclk value
	SourceFilt *SourceFilter
	PTime      string      // a=ptime value
	Attributes []Attribute // everything else, raw, in order
	Extra      []string    // non-modelled lines in this section (b=, …), verbatim
}

// RTPMap is one a=rtpmap entry: "<payload> <encoding>/<clock>[/<params>]".
type RTPMap struct {
	Encoding  string
	ClockRate int
	Params    string // e.g. audio channel count ("8")
	Raw       string
}

// FMTPParam is one key=value (or bare flag) inside a=fmtp, in source
// order — ST 2110 parameter order is meaningful to some receivers.
type FMTPParam struct {
	Key   string
	Value string
}

// FMTP is one a=fmtp entry.
type FMTP struct {
	Params []FMTPParam
	Raw    string
}

// Get returns the first value for key ("" when absent) plus presence.
func (f FMTP) Get(key string) (string, bool) {
	for _, p := range f.Params {
		if p.Key == key {
			return p.Value, true
		}
	}
	return "", false
}

// TSRefClk is a=ts-refclk with the ptp= form:
// "ptp=IEEE1588-2008:<gmid>:<domain>" or "ptp=IEEE1588-2008:traceable".
type TSRefClk struct {
	Version string // e.g. IEEE1588-2008
	GMID    string // grandmaster id, or "traceable"
	Domain  int    // -1 when absent (traceable form)
	Raw     string // the full attribute value, always
}

// SourceFilter is a=source-filter (RFC 4570):
// "incl IN IP4 <dest> <src>...".
type SourceFilter struct {
	Mode     string // incl | excl
	NetType  string
	AddrType string
	Dest     string
	Srcs     []string
}

// Deviation is one recoverable defect found while parsing — reported,
// never swallowed (compliance posture).
type Deviation struct {
	Line   int    // 1-based line number in the document
	Text   string // the offending line, verbatim
	Reason string
}

// Leg is one ST 2022-7 leg: a group:DUP tag paired with its media
// section by a=mid.
type Leg struct {
	Mid   string
	Media *Media
	Dest  string // media c= address, falling back to the session c=
	Src   string // first source-filter source, "" when absent
	Port  int
}

// Legs pairs the a=group:DUP tags with their m= sections in group
// order. Sessions without a DUP group return every media section as
// one leg each (single-path senders), preserving order.
func (s *Session) Legs() []Leg {
	byMid := map[string]*Media{}
	for i := range s.Media {
		if s.Media[i].Mid != "" {
			byMid[s.Media[i].Mid] = &s.Media[i]
		}
	}
	mkLeg := func(mid string, m *Media) Leg {
		l := Leg{Mid: mid, Media: m, Port: m.Port}
		if m.Connection != nil {
			l.Dest = m.Connection.Addr
		} else if s.Connection != nil {
			l.Dest = s.Connection.Addr
		}
		if m.SourceFilt != nil && len(m.SourceFilt.Srcs) > 0 {
			l.Src = m.SourceFilt.Srcs[0]
		}
		return l
	}
	for _, g := range s.Groups {
		if g.Semantics != "DUP" {
			continue
		}
		out := make([]Leg, 0, len(g.Tags))
		for _, tag := range g.Tags {
			if m, ok := byMid[tag]; ok {
				out = append(out, mkLeg(tag, m))
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	out := make([]Leg, 0, len(s.Media))
	for i := range s.Media {
		out = append(out, mkLeg(s.Media[i].Mid, &s.Media[i]))
	}
	return out
}
