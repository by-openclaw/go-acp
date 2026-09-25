package mnset

import "strings"

// The node API is a tree the module publishes itself: GET on the root
// (/emsfp/node/v1/) answers a LISTING — a JSON array of "name/" strings
// — and so does every collection under it (self/, flows/, port/,
// route/bulk/sender/ …), down to a DOCUMENT (a JSON object or array of
// values) or a TEXT body (the sdp/, receivers_sdp/, senders_sdp/ items
// are raw SDP). The walk is driven by those listings, never by a
// hardcoded catalogue, so a firmware that adds a resource is exported
// without a code change. Verified on a FusioN6 (fw 0x68cd783f),
// 2026-09-20: 19 roots, listings nested up to four deep
// (route/bulk/sender/<id>).

// maxListingDepth bounds the descent; four is the deepest seen, six
// leaves room without letting a mis-served listing recurse forever.
const maxListingDepth = 6

// writable names the resources the module accepts PUT on, keyed by the
// first path segment (or "self/<x>" for the system tree). Everything
// else is read-only state — identity, licence, telemetry, the SDP
// renderings — and Object.Access says so, so an import never PUTs a
// status. Sourced from the MN SET Rest page (GET/PUT enabled) and the
// site procedure.
var writable = map[string]bool{
	"flows": true, "receivers": true, "senders": true, "route": true,
	"sdi": true, "sdi_output": true, "sdi_input": true, "sdi_audio": true,
	"clean_switch": true, "refclk": true,
	"self/ipconfig": true, "self/syslog": true, "self/protocols": true,
	"self/static_route": true, "self/system": true, "self/phy": true,
	"self/interfaces": true,
}

// isWritable reports whether a resource URL ("flows/<id>",
// "self/ipconfig") is under a writable root.
func isWritable(url string) bool {
	segs := strings.SplitN(url, "/", 3)
	if segs[0] == "self" && len(segs) > 1 {
		return writable["self/"+segs[1]]
	}
	return writable[segs[0]]
}

// listing reports whether a decoded body is a directory listing — a
// non-empty array whose every element is a "name/" string — and returns
// the member names without the slash.
func listing(v any) ([]string, bool) {
	arr, ok := v.([]any)
	if !ok || len(arr) == 0 {
		return nil, false
	}
	names := make([]string, 0, len(arr))
	for _, e := range arr {
		s, ok := e.(string)
		if !ok || !strings.HasSuffix(s, "/") {
			return nil, false
		}
		names = append(names, strings.TrimSuffix(s, "/"))
	}
	return names, true
}

// pathElems returns the Object.Path prefix for a resource URL:
// "self/ipconfig" → ["self","ipconfig"], "flows/<id>" → ["flows","<id>"].
func pathElems(url string) []string {
	if url == "" {
		return nil
	}
	return strings.Split(url, "/")
}

// splitPath turns an operator path into its tokens. The dotted form and
// the [n] index form are both accepted, so a path copied from an export
// and one typed by hand resolve the same:
//
//	"flows.<id>.network.1.dst_ip_addr" and "flows.<id>.network[1].dst_ip_addr"
func splitPath(p string) []string {
	p = strings.NewReplacer("[", ".", "]", "").Replace(strings.TrimSpace(p))
	var out []string
	for _, s := range strings.Split(p, ".") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
