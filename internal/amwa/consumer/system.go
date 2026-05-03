// Layer-3 IS-09 System API consumer — discovery + selection rule + GET
// /global. Used by Nodes during bootstrap to pick up PTP / heartbeat /
// syslog parameters from the network.
//
// Spec: AMWA NMOS IS-09 v1.0.0
// (https://specs.amwa.tv/is-09/releases/v1.0.0/docs/3.1._Discovery_-_Operation.html).
//
// Selection rule (spec-strict):
//
//   1. Filter out instances whose api_proto != requested.
//   2. Filter out instances whose api_ver list does not include the
//      requested version.
//   3. Sort by `pri` ascending.
//   4. Randomised tie-break among the lowest-pri survivors.
//   5. GET /x-nmos/system/<api-ver>/global, validate, return Global.

package consumer

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	dnssdcodec "dhs/internal/amwa/codec/dnssd"
	"dhs/internal/amwa/codec/is09"
	dnssdsession "dhs/internal/amwa/session/dnssd"
	httpsession "dhs/internal/amwa/session/http"
)

// IS09FetchOptions configures a single Fetch call.
type IS09FetchOptions struct {
	// Logger to emit progress messages (mDNS bind, selection
	// outcome). May be nil for quiet mode.
	Logger *slog.Logger

	// APIVer is the IS-09 wire version requested. Default v1.0.
	APIVer string

	// APIProto is the requested protocol — "http" or "https".
	// Default "http".
	APIProto string

	// HTTPClient lets callers pre-configure timeouts, transport, etc.
	// Default = httpsession.NewClient().
	HTTPClient *httpsession.Client

	// Direct, when non-empty, bypasses discovery and fetches
	// /global from this `host:port` directly. Useful for unicast Mode B
	// targets we already have in hand.
	Direct string

	// Discovered, when non-nil, is the pre-discovered instance set —
	// caller already ran discover via mDNS / unicast / peer-list.
	// When nil, Fetch returns ErrNoInstances; the CLI runs discovery
	// itself.
	Discovered []dnssdcodec.Instance
}

// ErrNoInstances signals that no usable instance survived selection.
var ErrNoInstances = fmt.Errorf("nmos/system: no instance matched api_proto/api_ver filters")

// FetchResult bundles the selected instance with the validated Global
// resource.
type FetchResult struct {
	Selected dnssdcodec.Instance
	URL      string
	Global   *is09.Global
}

// Fetch picks the highest-priority IS-09 instance from opts.Discovered
// (or opts.Direct), GETs /global, validates, returns the result.
func Fetch(ctx context.Context, opts IS09FetchOptions) (*FetchResult, error) {
	if opts.APIVer == "" {
		opts.APIVer = is09.APIVersion
	}
	if opts.APIProto == "" {
		opts.APIProto = "http"
	}
	client := opts.HTTPClient
	if client == nil {
		client = httpsession.NewClient()
	}

	// Direct override — skip discovery entirely.
	if opts.Direct != "" {
		host, port, err := parseDirect(opts.Direct)
		if err != nil {
			return nil, err
		}
		ins := dnssdcodec.Instance{
			Name:    "direct",
			Service: dnssdcodec.ServiceSystem,
			Domain:  "",
			Host:    host,
			Port:    port,
			TXT: map[string]string{
				dnssdcodec.TXTKeyAPIProto: opts.APIProto,
				dnssdcodec.TXTKeyAPIVer:   opts.APIVer,
			},
		}
		return fetchFromInstance(ctx, client, ins, opts)
	}

	if len(opts.Discovered) == 0 {
		return nil, ErrNoInstances
	}
	selected, err := SelectInstance(opts.Discovered, opts.APIProto, opts.APIVer, opts.Logger)
	if err != nil {
		return nil, err
	}
	return fetchFromInstance(ctx, client, selected, opts)
}

// SelectInstance applies the spec-strict IS-09 §3.1 selection rule.
// Exposed for tests and CLI listings.
func SelectInstance(insts []dnssdcodec.Instance, apiProto, apiVer string, logger *slog.Logger) (dnssdcodec.Instance, error) {
	type rated struct {
		ins dnssdcodec.Instance
		pri int
	}
	var matches []rated
	for _, ins := range insts {
		// Service-type filter — only keep _nmos-system._tcp.
		if ins.Service != dnssdcodec.ServiceSystem && ins.Service != "" {
			continue
		}
		gotProto := strings.ToLower(strings.TrimSpace(ins.TXT[dnssdcodec.TXTKeyAPIProto]))
		if gotProto != strings.ToLower(apiProto) {
			continue
		}
		if !versionListContains(ins.TXT[dnssdcodec.TXTKeyAPIVer], apiVer) {
			continue
		}
		pri, ok := dnssdcodec.PriorityFromTXT(ins.TXT)
		if !ok {
			pri = 99 // peers without `pri` get the lowest production priority
		}
		matches = append(matches, rated{ins, pri})
	}
	if len(matches) == 0 {
		return dnssdcodec.Instance{}, ErrNoInstances
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].pri < matches[j].pri })

	// Identify the tied-lowest set, randomised tie-break per §3.1.
	bestPri := matches[0].pri
	end := 0
	for end < len(matches) && matches[end].pri == bestPri {
		end++
	}
	tied := matches[:end]
	pick := tied[secureRandIndex(end)]

	if logger != nil && end > 1 {
		logger.Info("nmos/system: tie-break among lowest pri",
			"pri", bestPri, "candidates", end, "selected", pick.ins.FullName())
	}
	return pick.ins, nil
}

// versionListContains checks whether `csv` (e.g. "v1.0,v1.1") lists
// `want` exactly. Whitespace tolerated; case-insensitive on the `v`
// prefix only.
func versionListContains(csv, want string) bool {
	if csv == "" {
		return false
	}
	want = strings.TrimSpace(want)
	for _, tok := range strings.Split(csv, ",") {
		if strings.TrimSpace(tok) == want {
			return true
		}
	}
	return false
}

// fetchFromInstance does the HTTP GET, validates, and returns.
func fetchFromInstance(ctx context.Context, client *httpsession.Client, ins dnssdcodec.Instance, opts IS09FetchOptions) (*FetchResult, error) {
	// Use the instance Host (DNS name from SRV) when available,
	// falling back to the first IPv4. The session/http client uses
	// the system resolver, so DNS names from the .arpa zone work.
	hostport := ins.Host + ":" + strconv.Itoa(int(ins.Port))
	if ins.Host == "" && len(ins.IPv4) > 0 {
		hostport = ins.IPv4[0].String() + ":" + strconv.Itoa(int(ins.Port))
	}
	url := opts.APIProto + "://" + hostport + "/x-nmos/system/" + opts.APIVer + "/global"

	// Apply a deadline if the caller didn't already set one.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}

	var raw is09.Global
	if err := client.GetJSON(ctx, url, &raw); err != nil {
		return nil, fmt.Errorf("nmos/system: GET %s: %w", url, err)
	}
	if err := raw.Validate(); err != nil {
		return nil, fmt.Errorf("nmos/system: peer /global failed validation: %w", err)
	}
	return &FetchResult{Selected: ins, URL: url, Global: &raw}, nil
}

// DiscoverMDNS browses _nmos-system._tcp on the local link.
func DiscoverMDNS(ctx context.Context, timeout time.Duration, logger *slog.Logger) ([]dnssdcodec.Instance, error) {
	br, err := dnssdsession.NewBrowser(logger)
	if err != nil {
		return nil, err
	}
	defer func() { _ = br.Close() }()

	dctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ch, err := br.Browse(dctx, dnssdcodec.ServiceSystem)
	if err != nil {
		return nil, err
	}
	seen := map[string]dnssdcodec.Instance{}
	for ins := range ch {
		seen[ins.FullName()] = ins
	}
	out := make([]dnssdcodec.Instance, 0, len(seen))
	for _, v := range seen {
		out = append(out, v)
	}
	return out, nil
}

// DiscoverUnicast resolves _nmos-system._tcp via authoritative DNS-SD
// (RFC 6763 §10) against the given resolver. Domain may be "" to use
// the codec default.
func DiscoverUnicast(ctx context.Context, resolver, domain string, timeout time.Duration) ([]dnssdcodec.Instance, error) {
	if domain == "" {
		domain = dnssdcodec.DefaultDomain
	}
	return dnssdsession.ResolveUnicast(ctx, resolver, dnssdcodec.ServiceSystem, domain, timeout)
}

// parseDirect splits "host:port" into its components and validates the
// port range. IPv6 literals must be wrapped in `[...]`.
func parseDirect(s string) (string, uint16, error) {
	host, portStr, err := net.SplitHostPort(s)
	if err != nil {
		return "", 0, fmt.Errorf("nmos/system: bad --direct %q: %w", s, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, fmt.Errorf("nmos/system: bad --direct port %q", portStr)
	}
	return host, uint16(port), nil
}

// secureRandIndex returns a uniformly-random integer in [0, n) using
// crypto/rand. Falls back to 0 on read failure (deterministic but
// safe).
func secureRandIndex(n int) int {
	if n <= 1 {
		return 0
	}
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return 0
	}
	v := binary.BigEndian.Uint64(buf[:])
	return int(v % uint64(n))
}
