package dnssd

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// NMOS-defined TXT keys (IS-04 §3.1.1).
const (
	TXTKeyAPIProto = "api_proto" // "http" | "https"
	TXTKeyAPIVer   = "api_ver"   // comma-separated, e.g. "v1.2,v1.3"
	TXTKeyAPIAuth  = "api_auth"  // "true" | "false"
	TXTKeyPriority = "pri"       // integer 0-99 prod, 100+ dev
)

// EncodeTXT serialises a key=value map to RFC 6763 §6 TXT segments.
// Keys are emitted in lexicographic order for deterministic output;
// callers that need a specific order should encode segments
// directly. An empty map produces a single zero-length segment per
// RFC 6763 §6.1.
func EncodeTXT(kv map[string]string) ([]string, error) {
	if len(kv) == 0 {
		return []string{""}, nil
	}
	keys := make([]string, 0, len(kv))
	for k := range kv {
		if err := validateTXTKey(k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		v := kv[k]
		seg := k + "=" + v
		if len(seg) > 255 {
			return nil, fmt.Errorf("dnssd: TXT segment %q exceeds 255 bytes", k)
		}
		out = append(out, seg)
	}
	return out, nil
}

// DecodeTXT parses RFC 6763 §6 segments into a key=value map.
// Boolean attributes (no `=`) decode as empty-string values. Later
// segments with the same key are ignored per RFC 6763 §6.4.
func DecodeTXT(segs []string) map[string]string {
	out := make(map[string]string, len(segs))
	for _, s := range segs {
		if s == "" {
			continue
		}
		eq := strings.IndexByte(s, '=')
		var k, v string
		if eq < 0 {
			k = s
			v = ""
		} else {
			k = s[:eq]
			v = s[eq+1:]
		}
		// Keys are case-insensitive per RFC 6763 §6.4.
		lk := strings.ToLower(k)
		if _, exists := out[lk]; exists {
			continue
		}
		out[lk] = v
	}
	return out
}

// PriorityFromTXT parses the NMOS `pri` TXT value. Returns the
// priority and true on success; (0, false) on missing or malformed.
func PriorityFromTXT(kv map[string]string) (int, bool) {
	raw, ok := kv[TXTKeyPriority]
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, false
	}
	return n, true
}

// validateTXTKey enforces RFC 6763 §6.4: printable ASCII excluding
// `=` and control characters; length 1-9 recommended but we allow up
// to 32.
func validateTXTKey(k string) error {
	if len(k) == 0 {
		return fmt.Errorf("dnssd: empty TXT key")
	}
	if len(k) > 32 {
		return fmt.Errorf("dnssd: TXT key %q exceeds 32 bytes", k)
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		if c < 0x20 || c > 0x7E || c == '=' {
			return fmt.Errorf("dnssd: invalid TXT key byte 0x%02x in %q", c, k)
		}
	}
	return nil
}
