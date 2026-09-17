package sdp

import (
	"fmt"
	"strconv"
	"strings"
)

// Parse reads one SDP document. Recoverable defects come back as
// Deviations; only a structurally impossible document (empty, or not
// starting with v=) is an error. The returned Session preserves every
// line: modelled fields are extracted, everything else lands verbatim
// in Attributes / Extra.
func Parse(text string) (*Session, []Deviation, error) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	// Drop one trailing empty line from the final newline; keep inner
	// blanks visible as deviations below.
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	if len(lines) == 0 {
		return nil, nil, fmt.Errorf("sdp: empty document")
	}
	if !strings.HasPrefix(lines[0], "v=") {
		return nil, nil, fmt.Errorf("sdp: document must start with v=, got %q", lines[0])
	}

	s := &Session{}
	var devs []Deviation
	dev := func(n int, line, reason string) {
		devs = append(devs, Deviation{Line: n + 1, Text: line, Reason: reason})
	}
	var cur *Media // nil while in the session part

	for i, line := range lines {
		if line == "" {
			dev(i, line, "blank line inside the document")
			continue
		}
		if len(line) < 2 || line[1] != '=' {
			dev(i, line, "not a <type>=<value> line")
			continue
		}
		typ, val := line[0], line[2:]

		switch typ {
		case 'v':
			n, err := strconv.Atoi(strings.TrimSpace(val))
			if err != nil {
				dev(i, line, "non-numeric version")
				continue
			}
			s.Version = n

		case 'o':
			f := strings.Fields(val)
			if len(f) != 6 {
				dev(i, line, "o= wants 6 fields")
				continue
			}
			s.Origin = Origin{Username: f[0], SessID: f[1], SessVersion: f[2],
				NetType: f[3], AddrType: f[4], Addr: f[5]}

		case 's':
			s.Name = val

		case 't':
			f := strings.Fields(val)
			if len(f) != 2 {
				dev(i, line, "t= wants 2 fields")
				continue
			}
			s.Timing = append(s.Timing, Timing{Start: f[0], Stop: f[1]})

		case 'c':
			c, err := parseConnection(val)
			if err != nil {
				dev(i, line, err.Error())
				continue
			}
			if cur != nil {
				cur.Connection = c
			} else {
				s.Connection = c
			}

		case 'm':
			m, err := parseMediaHeader(val)
			if err != nil {
				dev(i, line, err.Error())
				// Still open a section so following lines have a home
				// and nothing is silently dropped.
				m = &Media{Type: val}
			}
			s.Media = append(s.Media, *m)
			cur = &s.Media[len(s.Media)-1]

		case 'a':
			name, value := val, ""
			if idx := strings.Index(val, ":"); idx >= 0 {
				name, value = val[:idx], val[idx+1:]
			}
			if cur == nil {
				if name == "group" {
					f := strings.Fields(value)
					if len(f) < 1 {
						dev(i, line, "a=group without semantics")
						continue
					}
					s.Groups = append(s.Groups, Group{Semantics: f[0], Tags: f[1:]})
				} else {
					s.Attributes = append(s.Attributes, Attribute{Name: name, Value: value})
				}
				continue
			}
			if reason := applyMediaAttr(cur, name, value); reason != "" {
				dev(i, line, reason)
			}

		default:
			// b=, i=, u=, e=, p=, z=, r=, k=, … — preserved verbatim.
			if cur != nil {
				cur.Extra = append(cur.Extra, line)
			} else {
				s.Extra = append(s.Extra, line)
			}
		}
	}
	return s, devs, nil
}

func parseConnection(val string) (*Connection, error) {
	f := strings.Fields(val)
	if len(f) != 3 {
		return nil, fmt.Errorf("c= wants 3 fields")
	}
	addr, ttl := f[2], ""
	if idx := strings.Index(addr, "/"); idx >= 0 {
		addr, ttl = addr[:idx], f[2][idx+1:]
	}
	return &Connection{NetType: f[0], AddrType: f[1], Addr: addr, TTL: ttl}, nil
}

func parseMediaHeader(val string) (*Media, error) {
	f := strings.Fields(val)
	if len(f) < 4 {
		return nil, fmt.Errorf("m= wants at least 4 fields")
	}
	port, err := strconv.Atoi(f[1])
	if err != nil {
		return nil, fmt.Errorf("m= port is not a number")
	}
	return &Media{Type: f[0], Port: port, Proto: f[2], Formats: f[3:]}, nil
}

// applyMediaAttr routes one media-level a= line into its modelled
// slot, or preserves it verbatim. A non-empty return is a Deviation
// reason — the raw line is preserved in Attributes regardless, so a
// malformed modelled attribute is reported AND kept.
func applyMediaAttr(m *Media, name, value string) string {
	switch name {
	case "mid":
		m.Mid = value
		return ""

	case "rtpmap":
		f := strings.Fields(value)
		if len(f) != 2 {
			m.Attributes = append(m.Attributes, Attribute{Name: name, Value: value})
			return "a=rtpmap wants '<payload> <encoding>/<clock>[/<params>]'"
		}
		enc := strings.SplitN(f[1], "/", 3)
		if len(enc) < 2 {
			m.Attributes = append(m.Attributes, Attribute{Name: name, Value: value})
			return "a=rtpmap encoding wants '<name>/<clock>'"
		}
		clock, err := strconv.Atoi(enc[1])
		if err != nil {
			m.Attributes = append(m.Attributes, Attribute{Name: name, Value: value})
			return "a=rtpmap clock rate is not a number"
		}
		r := RTPMap{Encoding: enc[0], ClockRate: clock, Raw: value}
		if len(enc) == 3 {
			r.Params = enc[2]
		}
		if m.RTPMap == nil {
			m.RTPMap = map[string]RTPMap{}
		}
		m.RTPMap[f[0]] = r
		return ""

	case "fmtp":
		payload, rest, ok := strings.Cut(value, " ")
		if !ok {
			m.Attributes = append(m.Attributes, Attribute{Name: name, Value: value})
			return "a=fmtp wants '<payload> <params>'"
		}
		fm := FMTP{Raw: rest}
		for _, part := range strings.Split(rest, ";") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			k, v, _ := strings.Cut(part, "=")
			fm.Params = append(fm.Params, FMTPParam{Key: k, Value: v})
		}
		if m.FMTP == nil {
			m.FMTP = map[string]FMTP{}
		}
		m.FMTP[payload] = fm
		return ""

	case "ts-refclk":
		if !strings.HasPrefix(value, "ptp=") {
			// localmac= and friends: preserved, not modelled.
			m.Attributes = append(m.Attributes, Attribute{Name: name, Value: value})
			return ""
		}
		parts := strings.Split(strings.TrimPrefix(value, "ptp="), ":")
		t := &TSRefClk{Raw: value, Domain: -1}
		switch len(parts) {
		case 2: // IEEE1588-2008:traceable
			t.Version, t.GMID = parts[0], parts[1]
		case 3: // IEEE1588-2008:<gmid>:<domain>
			t.Version, t.GMID = parts[0], parts[1]
			d, err := strconv.Atoi(parts[2])
			if err != nil {
				m.Attributes = append(m.Attributes, Attribute{Name: name, Value: value})
				return "a=ts-refclk ptp domain is not a number"
			}
			t.Domain = d
		default:
			m.Attributes = append(m.Attributes, Attribute{Name: name, Value: value})
			return "a=ts-refclk ptp= wants '<version>:<gmid>[:<domain>]'"
		}
		m.TSRefClk = t
		return ""

	case "mediaclk":
		m.MediaClk = value
		return ""

	case "ptime":
		m.PTime = value
		return ""

	case "source-filter":
		f := strings.Fields(value)
		if len(f) < 5 {
			m.Attributes = append(m.Attributes, Attribute{Name: name, Value: value})
			return "a=source-filter wants '<mode> <net> <addr-type> <dest> <src>...'"
		}
		m.SourceFilt = &SourceFilter{Mode: f[0], NetType: f[1], AddrType: f[2],
			Dest: f[3], Srcs: f[4:]}
		return ""
	}
	m.Attributes = append(m.Attributes, Attribute{Name: name, Value: value})
	return ""
}
