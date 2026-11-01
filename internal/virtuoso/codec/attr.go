package codec

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

// Reasons EncodeAttr refuses a value, checked against the schema before
// anything is sent.
var (
	ErrBelowMinimum = errors.New("virtuoso: below the schema minimum")
	ErrAboveMaximum = errors.New("virtuoso: above the schema maximum")
	ErrNotInEnum    = errors.New("virtuoso: not one of the schema's enum values")
	ErrBadValue     = errors.New("virtuoso: value does not fit the schema type")
)

// EncodeAttr turns an operator's text into the text of a PATCH query
// attribute for the field n describes. The guide's examples fix the
// shapes: a number in digits (dstudpp=12000), a boolean as true/false
// (enable=true), a string as itself (label=office%20network). An enum
// field takes its label or its number and sends the number.
func EncodeAttr(n *Node, text string) (string, error) {
	switch {
	case len(n.Enum) > 0:
		return encodeEnum(n, text)
	case n.Type == "boolean":
		return encodeBool(text)
	case n.Type == "integer":
		v, ok := new(big.Int).SetString(text, 10)
		if !ok {
			return "", fmt.Errorf("%w: %q is not an integer", ErrBadValue, text)
		}
		if err := inRange(n, new(big.Rat).SetInt(v), text); err != nil {
			return "", err
		}
		return v.String(), nil
	case n.Type == "number":
		f, err := strconv.ParseFloat(text, 64)
		if err != nil || f != f {
			return "", fmt.Errorf("%w: %q is not a number", ErrBadValue, text)
		}
		r, ok := new(big.Rat).SetString(text)
		if !ok {
			return "", fmt.Errorf("%w: %q is not a finite number", ErrBadValue, text)
		}
		if err := inRange(n, r, text); err != nil {
			return "", err
		}
		return text, nil
	}
	return text, checkString(n, text)
}

func encodeEnum(n *Node, text string) (string, error) {
	for i, raw := range n.Enum {
		wire := strings.Trim(string(raw), `"`)
		if wire == text || (i < len(n.Labels) && n.Labels[i] == text) {
			return wire, nil
		}
	}
	return "", fmt.Errorf("%w: %q (labels %s)", ErrNotInEnum, text, strings.Join(n.Labels, ", "))
}

func encodeBool(text string) (string, error) {
	switch strings.ToLower(text) {
	case "true", "1":
		return "true", nil
	case "false", "0":
		return "false", nil
	}
	return "", fmt.Errorf("%w: %q is not a boolean", ErrBadValue, text)
}

func inRange(n *Node, v *big.Rat, text string) error {
	if r, ok := rat(n.Minimum); ok && v.Cmp(r) < 0 {
		return fmt.Errorf("%w: %s < %s", ErrBelowMinimum, text, n.Minimum)
	}
	if r, ok := rat(n.Maximum); ok && v.Cmp(r) > 0 {
		return fmt.Errorf("%w: %s > %s", ErrAboveMaximum, text, n.Maximum)
	}
	return nil
}

func rat(n *json.Number) (*big.Rat, bool) {
	if n == nil {
		return nil, false
	}
	return new(big.Rat).SetString(n.String())
}

// checkString applies the format and the pattern a schema gives a string.
// A pattern Go's regexp cannot compile is left to the device. An empty
// string is left to the device too: it may be how a field is cleared.
func checkString(n *Node, text string) error {
	if text == "" {
		return nil
	}
	if n.Format == "ipv4" || n.Format == "ipv6" {
		a, err := netip.ParseAddr(text)
		if err != nil || (n.Format == "ipv4") != a.Is4() {
			return fmt.Errorf("%w: %q is not an %s address", ErrBadValue, text, n.Format)
		}
	}
	if n.Pattern != "" {
		if re, err := regexp.Compile(n.Pattern); err == nil && !re.MatchString(text) {
			return fmt.Errorf("%w: %q does not match %s", ErrBadValue, text, n.Pattern)
		}
	}
	return nil
}

// SameValue reports whether a value read back is the wire text written.
func SameValue(wire string, got any) bool {
	switch g := got.(type) {
	case string:
		return g == wire
	case bool:
		return strconv.FormatBool(g) == wire
	case json.Number:
		a, aok := new(big.Rat).SetString(g.String())
		b, bok := new(big.Rat).SetString(wire)
		return aok && bok && a.Cmp(b) == 0
	}
	return false
}
