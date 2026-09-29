package mnset

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	"dhs/internal/consumer"
)

// The module spells nearly every value as a JSON string — a flag is
// "1", a port "20000", a group "239.1.0.1" — and refuses or corrupts
// what does not fit: a bad octet is wrapped modulo 256 (999.1.1.1 is
// stored as 231.1.1.1), an out-of-range DSCP or payload type is taken
// as given. So the dictionary says what each leaf MEANS, a walk shows
// that meaning, and a write is checked and spelled here before it goes
// out; the wire spelling itself is kept by coerce.

// applyKind gives a walked leaf the kind its dictionary type says,
// keeping the module's own spelling in Value.Str.
func applyKind(o *consumer.Object, t fieldType) {
	if t.note != "" {
		setMeta(o, "note", t.note)
	}
	if t.format != "" {
		setMeta(o, "format", t.format)
	}
	if t.readOnly {
		o.Access &^= accessWrite
	}
	raw := wireText(o.Value)
	switch t.kind {
	case "bool", "action":
		if t.kind == "action" {
			setMeta(o, "action", "true")
		}
		if o.Value.Kind == consumer.KindBool {
			break
		}
		if b, ok := parseBool(raw); ok {
			o.Value = consumer.Value{Kind: consumer.KindBool, Bool: b, Str: raw}
			o.Kind = consumer.KindBool
		}
	case "int":
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			o.Value = consumer.Value{Kind: consumer.KindInt, Int: n, Str: raw}
			o.Kind = consumer.KindInt
		}
	case "uint":
		if n, err := strconv.ParseUint(raw, 10, 64); err == nil {
			o.Value = consumer.Value{Kind: consumer.KindUint, Uint: n, Str: raw}
			o.Kind = consumer.KindUint
		}
	case "ip":
		if ip := net.ParseIP(raw).To4(); ip != nil {
			o.Value = consumer.Value{Kind: consumer.KindIPAddr, IPAddr: [4]byte{ip[0], ip[1], ip[2], ip[3]}, Str: raw}
			o.Kind = consumer.KindIPAddr
		}
	case "enum":
		// A REST enum is not wire-coded: the value stays the module's
		// word and the items list the words it accepts (the neutral
		// KindEnum is an ordinal, which these are not).
		items := make([]string, len(t.values))
		for i, v := range t.values {
			items[i] = v
			if l, ok := t.labels[v]; ok {
				items[i] = v + "=" + l
			}
		}
		o.EnumItems = items
		if l, ok := t.labels[raw]; ok {
			setMeta(o, "value_name", l)
		}
	}
}

// wireText is a leaf's value as the module spelled it.
func wireText(v consumer.Value) string {
	switch v.Kind {
	case consumer.KindBool:
		return strconv.FormatBool(v.Bool)
	case consumer.KindInt:
		return strconv.FormatInt(v.Int, 10)
	case consumer.KindFloat:
		if v.Str != "" {
			return v.Str
		}
		return strconv.FormatFloat(v.Float, 'f', -1, 64)
	}
	return v.Str
}

func parseBool(s string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "on", "yes":
		return true, true
	case "0", "false", "off", "no":
		return false, true
	}
	return false, false
}

var (
	reUUID     = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	reHex32    = regexp.MustCompile(`^0x[0-9a-fA-F]{8}$`)
	reAudioMap = regexp.MustCompile(`^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})?:\d+:\d+$`)
	reHostname = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
	reHostPort = regexp.MustCompile(`^(\d{1,3}(?:\.\d{1,3}){3}):(\d{1,5})$`)
)

// requestText is the operator's value as text, whatever kind it came as.
func requestText(v consumer.Value) string {
	switch v.Kind {
	case consumer.KindBool:
		return strconv.FormatBool(v.Bool)
	case consumer.KindInt:
		return strconv.FormatInt(v.Int, 10)
	case consumer.KindUint:
		return strconv.FormatUint(v.Uint, 10)
	case consumer.KindIPAddr:
		return fmt.Sprintf("%d.%d.%d.%d", v.IPAddr[0], v.IPAddr[1], v.IPAddr[2], v.IPAddr[3])
	}
	return strings.TrimSpace(v.Str)
}

// normalize checks a requested value against the field's type and
// returns it spelled the way the module stores it. A field with no
// type passes unchanged: nothing is invented for it.
func normalize(field string, t fieldType, v consumer.Value) (consumer.Value, error) {
	if t.readOnly {
		return v, &consumer.ValidationError{Field: field, Reason: "read-only: the module ignores or refuses writes to it"}
	}
	if t.kind == "" && t.format == "" {
		return v, nil
	}
	s := requestText(v)
	bad := func(reason string) (consumer.Value, error) {
		return v, fmt.Errorf("%w: %w", consumer.ErrValidationFailed, &consumer.ValidationError{Field: field, Reason: reason})
	}
	switch t.kind {
	case "bool", "action":
		b, ok := parseBool(s)
		if !ok {
			return bad(fmt.Sprintf("%q is not 0/1 (true/false)", s))
		}
		if b {
			s = "1"
		} else {
			s = "0"
		}
	case "int", "uint":
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return bad(fmt.Sprintf("%q is not a whole number", s))
		}
		if t.kind == "uint" && n < 0 {
			return bad(fmt.Sprintf("%d is negative", n))
		}
		if t.min != nil && float64(n) < *t.min {
			return v, fmt.Errorf("%w: %s: %d < %v", consumer.ErrOutOfRangeLow, field, n, *t.min)
		}
		if t.max != nil && float64(n) > *t.max {
			return v, fmt.Errorf("%w: %s: %d > %v", consumer.ErrOutOfRangeHigh, field, n, *t.max)
		}
		s = strconv.FormatInt(n, 10)
	case "enum":
		found := ""
		for _, want := range t.values {
			if s == want || strings.EqualFold(s, t.labels[want]) {
				found = want
				break
			}
		}
		if found == "" {
			return bad(fmt.Sprintf("%q is not one of %s", s, strings.Join(t.values, ", ")))
		}
		s = found
	case "ip":
		ip := net.ParseIP(s).To4()
		if ip == nil {
			return bad(fmt.Sprintf("%q is not an IPv4 address", s))
		}
		s = ip.String()
	}
	switch t.format {
	case "mac":
		hw, err := net.ParseMAC(s)
		if err != nil || len(hw) != 6 {
			return bad(fmt.Sprintf("%q is not a MAC address", s))
		}
		s = hw.String()
	case "uuid":
		if !reUUID.MatchString(s) {
			return bad(fmt.Sprintf("%q is not a UUID", s))
		}
	case "cidr":
		ip, _, err := net.ParseCIDR(s)
		if err != nil || ip.To4() == nil {
			return bad(fmt.Sprintf("%q is not an IPv4 prefix a.b.c.d/n", s))
		}
	case "hex32":
		if !reHex32.MatchString(s) {
			return bad(fmt.Sprintf("%q is not 0x followed by 8 hex digits", s))
		}
	case "audio_map":
		if !reAudioMap.MatchString(s) {
			return bad(fmt.Sprintf("%q is not <audio flow uuid>:<channel>:<n>", s))
		}
	case "hostport":
		m := reHostPort.FindStringSubmatch(s)
		if m == nil || net.ParseIP(m[1]).To4() == nil {
			return bad(fmt.Sprintf("%q is not <IPv4>:<port>", s))
		}
		if p, _ := strconv.Atoi(m[2]); p > 65535 {
			return bad(fmt.Sprintf("%q: port above 65535", s))
		}
	case "hostname":
		if !reHostname.MatchString(s) {
			return bad(fmt.Sprintf("%q is not a host name (RFC 1123 label)", s))
		}
	}
	return consumer.Value{Kind: consumer.KindString, Str: s}, nil
}

// dict is the embedded dictionary. A broken embed is a build error of
// ours; it leaves every field untyped, as annotate does, rather than
// failing a read.
func dict() dictionary {
	d, _, _ := parseDictionary(fusion6Dictionary, videoFormatsJSON)
	return d
}

// typedValue is one leaf read by path, with its dictionary type, so a
// get or a set's read-back answers in the same kind a walk shows.
func typedValue(path string, leaf any) consumer.Value {
	o := consumer.Object{Path: strings.Split(path, "."), Value: leafValue(leaf)}
	o.Kind = o.Value.Kind
	applyKind(&o, dict().typeOf(o.Path))
	return o.Value
}
