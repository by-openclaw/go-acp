package sdp

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Deviation is a recoverable defect in the document: the parser keeps going
// and reports it, rather than silently normalising the device's output. Same
// posture as the connector compliance model — absorb and surface, never patch.
type Deviation struct {
	Line int    // 1-based line number in the source
	Text string // the offending line, trimmed
	Msg  string
}

func (d Deviation) String() string { return fmt.Sprintf("line %d: %s (%q)", d.Line, d.Msg, d.Text) }

// ErrEmpty is returned when there is nothing to parse.
var ErrEmpty = errors.New("sdp: empty document")

// ErrNoVersion is returned when the document does not start with `v=`.
var ErrNoVersion = errors.New("sdp: missing v= line")

// Parse decodes an SDP document.
//
// It returns the session, any recoverable deviations, and an error only when
// the document is not an SDP document at all. A caller that wants strictness
// treats a non-empty deviation slice as failure.
func Parse(b []byte) (*Session, []Deviation, error) {
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if strings.TrimSpace(text) == "" {
		return nil, nil, ErrEmpty
	}

	var (
		s    Session
		devs []Deviation
		cur  *Media // nil until the first m=
	)

	for i, raw := range strings.Split(text, "\n") {
		lineNo := i + 1
		line := strings.TrimRight(raw, " \t")
		if line == "" {
			continue
		}
		if len(line) < 2 || line[1] != '=' {
			devs = append(devs, Deviation{lineNo, strings.TrimSpace(line), "not a <type>=<value> line"})
			continue
		}
		typ, val := line[0], line[2:]

		switch typ {
		case 'v':
			n, err := strconv.Atoi(strings.TrimSpace(val))
			if err != nil {
				devs = append(devs, Deviation{lineNo, line, "v= is not a number"})
				continue
			}
			s.Version = n
		case 'o':
			o, d := parseOrigin(val, lineNo)
			if d != nil {
				devs = append(devs, *d)
			}
			s.Origin = o
		case 's':
			s.Name = val
		case 'i':
			s.Info = val
		case 't':
			f := strings.Fields(val)
			if len(f) != 2 {
				devs = append(devs, Deviation{lineNo, line, "t= expects <start> <stop>"})
				continue
			}
			s.Timing = append(s.Timing, Timing{Start: f[0], Stop: f[1]})
		case 'c':
			c, d := parseConnection(val, lineNo)
			if d != nil {
				devs = append(devs, *d)
			}
			if cur != nil {
				cur.Connection = c
			} else {
				s.Connection = c
			}
		case 'm':
			m, d := parseMedia(val, lineNo)
			if d != nil {
				devs = append(devs, *d)
			}
			s.Media = append(s.Media, m)
			cur = &s.Media[len(s.Media)-1]
		case 'a':
			attr := splitAttribute(val)
			if cur != nil {
				cur.Attributes = append(cur.Attributes, attr)
				if d := applyMediaAttribute(cur, attr, lineNo); d != nil {
					devs = append(devs, *d)
				}
				continue
			}
			s.Attributes = append(s.Attributes, attr)
			if strings.EqualFold(attr.Name, "group") {
				g, d := parseGroup(attr.Value, lineNo)
				if d != nil {
					devs = append(devs, *d)
				}
				s.Groups = append(s.Groups, g)
			}
		default:
			// b=, u=, e=, p=, r=, z=, k= and anything else: not modelled here,
			// but kept so nothing is lost.
			if cur != nil {
				cur.Attributes = append(cur.Attributes, Attribute{Name: string(typ), Raw: line})
			} else {
				s.Attributes = append(s.Attributes, Attribute{Name: string(typ), Raw: line})
			}
		}
	}

	if s.Version == 0 && !strings.HasPrefix(strings.TrimSpace(text), "v=") {
		return nil, devs, ErrNoVersion
	}

	// A DUP tag that names no m= section is a real defect: a controller would
	// look for a leg that is not described.
	for _, g := range s.Groups {
		for _, tag := range g.Tags {
			found := false
			for _, m := range s.Media {
				if m.Mid == tag {
					found = true
					break
				}
			}
			if !found {
				devs = append(devs, Deviation{0, "a=group:" + g.Semantics, "group tag " + tag + " has no matching a=mid"})
			}
		}
	}

	return &s, devs, nil
}

func parseOrigin(val string, lineNo int) (Origin, *Deviation) {
	f := strings.Fields(val)
	if len(f) != 6 {
		return Origin{}, &Deviation{lineNo, "o=" + val, "o= expects 6 fields"}
	}
	return Origin{f[0], f[1], f[2], f[3], f[4], f[5]}, nil
}

func parseConnection(val string, lineNo int) (*Connection, *Deviation) {
	f := strings.Fields(val)
	if len(f) != 3 {
		return nil, &Deviation{lineNo, "c=" + val, "c= expects <nettype> <addrtype> <address>"}
	}
	c := Connection{NetType: f[0], AddrType: f[1], Raw: val}
	parts := strings.Split(f[2], "/")
	c.Address = parts[0]
	if len(parts) > 1 {
		if n, err := strconv.Atoi(parts[1]); err == nil {
			c.TTL = n
		}
	}
	if len(parts) > 2 {
		if n, err := strconv.Atoi(parts[2]); err == nil {
			c.Count = n
		}
	}
	return &c, nil
}

func parseMedia(val string, lineNo int) (Media, *Deviation) {
	f := strings.Fields(val)
	if len(f) < 3 {
		return Media{}, &Deviation{lineNo, "m=" + val, "m= expects <media> <port> <proto> [fmt...]"}
	}
	m := Media{
		Type:    f[0],
		Proto:   f[2],
		Formats: append([]string(nil), f[3:]...),
		RTPMap:  map[string]RTPMap{},
		FMTP:    map[string]FMTP{},
	}
	port := f[1]
	if i := strings.IndexByte(port, '/'); i >= 0 {
		if n, err := strconv.Atoi(port[i+1:]); err == nil {
			m.PortCount = n
		}
		port = port[:i]
	}
	if n, err := strconv.Atoi(port); err == nil {
		m.Port = n
	} else {
		return m, &Deviation{lineNo, "m=" + val, "m= port is not a number"}
	}
	return m, nil
}

func splitAttribute(val string) Attribute {
	a := Attribute{Raw: val}
	if i := strings.IndexByte(val, ':'); i >= 0 {
		a.Name = val[:i]
		a.Value = val[i+1:]
	} else {
		a.Name = val
	}
	return a
}

func parseGroup(val string, lineNo int) (Group, *Deviation) {
	f := strings.Fields(val)
	if len(f) == 0 {
		return Group{}, &Deviation{lineNo, "a=group:" + val, "group has no semantics"}
	}
	return Group{Semantics: f[0], Tags: append([]string(nil), f[1:]...)}, nil
}

func applyMediaAttribute(m *Media, a Attribute, lineNo int) *Deviation {
	switch strings.ToLower(a.Name) {
	case "mid":
		m.Mid = strings.TrimSpace(a.Value)
	case "ptime":
		m.PTime = strings.TrimSpace(a.Value)
	case "mediaclk":
		m.MediaClk = strings.TrimSpace(a.Value)
	case "rtpmap":
		r, d := parseRTPMap(a.Value, lineNo)
		if d != nil {
			return d
		}
		m.RTPMap[r.PayloadType] = r
	case "fmtp":
		f, d := parseFMTP(a.Value, lineNo)
		if d != nil {
			return d
		}
		m.FMTP[f.PayloadType] = f
	case "ts-refclk":
		t, d := parseTSRefClk(a.Value, lineNo)
		if d != nil {
			return d
		}
		m.TSRefClk = t
	case "source-filter":
		sf, d := parseSourceFilter(a.Value, lineNo)
		if d != nil {
			return d
		}
		m.SourceFilt = sf
	}
	return nil
}

func parseRTPMap(val string, lineNo int) (RTPMap, *Deviation) {
	f := strings.Fields(val)
	if len(f) < 2 {
		return RTPMap{}, &Deviation{lineNo, "a=rtpmap:" + val, "rtpmap expects <pt> <encoding>/<clock>[/<channels>]"}
	}
	r := RTPMap{PayloadType: f[0]}
	parts := strings.Split(f[1], "/")
	r.Encoding = parts[0]
	if len(parts) > 1 {
		n, err := strconv.Atoi(parts[1])
		if err != nil {
			return r, &Deviation{lineNo, "a=rtpmap:" + val, "clock rate is not a number"}
		}
		r.ClockRate = n
	}
	if len(parts) > 2 {
		if n, err := strconv.Atoi(parts[2]); err == nil {
			r.Channels = n
		}
	}
	return r, nil
}

func parseFMTP(val string, lineNo int) (FMTP, *Deviation) {
	i := strings.IndexAny(val, " \t")
	if i < 0 {
		return FMTP{}, &Deviation{lineNo, "a=fmtp:" + val, "fmtp expects <pt> <params>"}
	}
	f := FMTP{PayloadType: strings.TrimSpace(val[:i]), Raw: val}
	for _, kv := range strings.Split(val[i+1:], ";") {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}
		if j := strings.IndexByte(kv, '='); j >= 0 {
			f.Params = append(f.Params, Param{Key: strings.TrimSpace(kv[:j]), Value: strings.TrimSpace(kv[j+1:])})
			continue
		}
		f.Params = append(f.Params, Param{Key: kv})
	}
	return f, nil
}

// parseTSRefClk handles the PTP form. Other forms (localmac, ntp) are kept as
// Raw with no parsed fields rather than being reported as defects.
func parseTSRefClk(val string, lineNo int) (*TSRefClk, *Deviation) {
	t := &TSRefClk{Raw: val, Domain: -1}
	v := strings.TrimSpace(val)
	if !strings.HasPrefix(strings.ToLower(v), "ptp=") {
		return t, nil
	}
	parts := strings.Split(v[len("ptp="):], ":")
	if len(parts) < 2 {
		return t, &Deviation{lineNo, "a=ts-refclk:" + val, "ptp expects <version>:<gmid>[:<domain>]"}
	}
	t.Version = parts[0]
	t.GMID = parts[1]
	if len(parts) > 2 {
		n, err := strconv.Atoi(parts[2])
		if err != nil {
			return t, &Deviation{lineNo, "a=ts-refclk:" + val, "ptp domain is not a number"}
		}
		t.Domain = n
	}
	return t, nil
}

func parseSourceFilter(val string, lineNo int) (*SourceFilter, *Deviation) {
	// Devices commonly emit "a=source-filter: incl IN IP4 <dst> <src>" with a
	// space after the colon; Fields handles both forms.
	f := strings.Fields(val)
	if len(f) < 5 {
		return nil, &Deviation{lineNo, "a=source-filter:" + val, "source-filter expects <mode> <nettype> <addrtype> <dest> <src>..."}
	}
	return &SourceFilter{
		Mode:     f[0],
		NetType:  f[1],
		AddrType: f[2],
		Dest:     f[3],
		Sources:  append([]string(nil), f[4:]...),
		Raw:      val,
	}, nil
}
