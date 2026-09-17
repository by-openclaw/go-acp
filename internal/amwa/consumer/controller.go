package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"dhs/internal/amwa/codec/bcp"
	_ "dhs/internal/amwa/codec/bcp/bcp00401" // register the BCP-004-01 receiver-caps validator
	"dhs/internal/amwa/codec/dnssd"
	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/spec"
	dnssdsession "dhs/internal/amwa/session/dnssd"
	httpsession "dhs/internal/amwa/session/http"
	"dhs/internal/amwa/session/query"
)

// ControllerOptions configures the IS-04 Controller. The pattern
// mirrors IS09FetchOptions in system.go: explicit dependency injection,
// no globals, every knob a constructor parameter.
type ControllerOptions struct {
	// Logger is optional; nil = silent.
	Logger *slog.Logger

	// Reporter receives compliance events. nil = spec.NopReporter{}.
	Reporter spec.Reporter

	// One of three modes (mirrors the Registry's mode A/B/C/D
	// deployment matrix from internal/amwa/CLAUDE.md):
	//
	//   RegistryURL set       — Mode B unicast: skip discovery,
	//                            speak straight to the named host
	//                            (e.g. http://10.6.239.113:8235).
	//   DiscoveryMode "mdns"  — Mode A: browse _nmos-query._tcp,
	//                            select highest-pri instance, talk
	//                            to it.
	//   DiscoveryMode "unicast" — Mode B via authoritative DNS:
	//                              resolve _nmos-query._tcp at the
	//                              named resolver / domain, pick
	//                              highest-pri.
	//
	// At least one of (RegistryURL, DiscoveryMode) must be set.
	RegistryURL   string
	DiscoveryMode string

	// NodeURL points the Controller at ONE Node's own API instead of a
	// Registry — the IS-04 peer-to-peer path (Mode C/D). Mutually
	// exclusive with RegistryURL and DiscoveryMode, and it wins when
	// set, because naming a specific device is never ambiguous.
	//
	// This is the only way to reach a device no Registry can see: one
	// that has not registered, or one on a segment with no route back
	// to the Registry. A real EVS Neuron is in exactly that state for
	// us today, and without this the Controller could not touch it.
	NodeURL string

	// Discovery knobs (used when RegistryURL is empty).
	DiscoveryTimeout time.Duration // default 5s
	UnicastResolver  string        // e.g. "10.100.0.1"
	UnicastDomain    string        // e.g. "by-systems.arpa"

	// APIVer constrains version selection. Empty = pick highest
	// from is04.SupportedVersions() ∩ peer's api_ver TXT.
	APIVer string
}

// Controller is the IS-04 Controller — high-level wrapper around the
// Query API client + a chosen Codec. Hold one per Registry.
//
// Stateless after construction — every method is safe to call
// concurrently.
type Controller struct {
	logger   *slog.Logger
	reporter spec.Reporter
	client   *query.Client
}

// NewController resolves the Registry per ControllerOptions and
// constructs a Controller bound to the negotiated wire-version codec.
//
// Returns the typed [spec.ErrNoCommonVersion] when the peer's
// `api_ver` TXT shares no minor with our registered codecs (caller
// fires the compliance event).
func NewController(ctx context.Context, opts ControllerOptions) (*Controller, error) {
	rep := opts.Reporter
	if rep == nil {
		rep = spec.NopReporter{}
	}

	if opts.NodeURL != "" {
		return newNodeController(ctx, opts, rep)
	}

	base, peerVers, err := resolveRegistry(ctx, opts)
	if err != nil {
		return nil, err
	}

	codec, err := pickCodec(opts.APIVer, peerVers, rep)
	if err != nil {
		return nil, err
	}

	c, err := query.NewClient(base, codec)
	if err != nil {
		return nil, err
	}
	return &Controller{logger: opts.Logger, reporter: rep, client: c}, nil
}

// newNodeController binds a Controller straight to one Node.
//
// Version selection still happens, but from the Node's OWN advertised
// list at /x-nmos/node/ rather than a DNS-SD api_ver TXT: with no
// Registry in the path there is no TXT record to read. Asking the
// device is strictly better information anyway.
func newNodeController(ctx context.Context, opts ControllerOptions, rep spec.Reporter) (*Controller, error) {
	base := strings.TrimRight(opts.NodeURL, "/")

	peerVers, err := nodeAPIVersions(ctx, base)
	if err != nil {
		return nil, err
	}

	codec, err := pickCodec(opts.APIVer, peerVers, rep)
	if err != nil {
		return nil, err
	}

	c, err := query.NewNodeClient(base, codec)
	if err != nil {
		return nil, err
	}
	return &Controller{logger: opts.Logger, reporter: rep, client: c}, nil
}

// nodeAPIVersions asks a Node which IS-04 minors it serves, by GETting
// /x-nmos/node/ — the index every Node MUST expose.
func nodeAPIVersions(ctx context.Context, base string) ([]string, error) {
	var raw []string
	if err := httpsession.NewClient().GetJSON(ctx, base+"/x-nmos/node/", &raw); err != nil {
		return nil, fmt.Errorf("nmos: read %s/x-nmos/node/: %w", base, err)
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if v = strings.Trim(v, "/"); v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("nmos: %s/x-nmos/node/ advertises no API versions", base)
	}
	return out, nil
}

// Codec returns the wire-version codec bound to this Controller.
// Useful for callers that need to encode requests in the same minor
// (e.g. POSTing a subscription).
func (c *Controller) Codec() is04.Codec { return c.client.Codec }

// BaseURL returns the Registry origin the Controller speaks to.
func (c *Controller) BaseURL() string { return c.client.Base }

// IsNodeFace reports whether this Controller is bound straight to one
// Node rather than to a Registry. Callers label output truthfully with
// it — "Node http://..." vs "Registry http://..." — because the two
// mean very different things to an operator reading a walk.
func (c *Controller) IsNodeFace() bool { return c.client.Face == query.FaceNode }

// Walk fetches every catalogue collection under the Registry's Query
// API and returns a single CatalogueSnapshot. Compliance events fire
// for any collection that 404s (per the EVS Cerebrum behaviour
// observed 2026-04-30 — query face surface but every catalogue
// collection 404s).
type CatalogueSnapshot struct {
	APIVer    string
	Nodes     []is04.Node
	Devices   []is04.Device
	Sources   []is04.Source
	Flows     []is04.Flow
	Senders   []is04.Sender
	Receivers []is04.Receiver
}

// Walk fetches every catalogue collection. Errors fire compliance
// events but do not abort the walk — callers see the partial snapshot
// + an error per failed collection.
func (c *Controller) Walk(ctx context.Context) (*CatalogueSnapshot, []error) {
	snap := &CatalogueSnapshot{APIVer: c.client.Codec.APIVer()}
	var errs []error

	collect := func(name string, run func() error) {
		if err := run(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			c.fire(spec.SeverityWarn, "nmos_query_collection_failed",
				fmt.Sprintf("%s: %v", name, err), "")
		}
	}

	collect("nodes", func() error {
		v, err := c.client.ListNodes(ctx, nil)
		snap.Nodes = v
		return err
	})
	collect("devices", func() error {
		v, err := c.client.ListDevices(ctx, nil)
		snap.Devices = v
		return err
	})
	collect("sources", func() error {
		v, err := c.client.ListSources(ctx, nil)
		snap.Sources = v
		return err
	})
	collect("flows", func() error {
		v, err := c.client.ListFlows(ctx, nil)
		snap.Flows = v
		return err
	})
	collect("senders", func() error {
		v, err := c.client.ListSenders(ctx, nil)
		snap.Senders = v
		return err
	})
	collect("receivers", func() error {
		v, err := c.client.ListReceivers(ctx, nil)
		snap.Receivers = v
		return err
	})

	// BCP-004-01: run the receiver-caps validators over each receiver
	// as the controller reads it (#851) — a receiver whose caps
	// reference a cap URN outside the AMWA register is one a filtering
	// controller silently drops. Events flow to the same Reporter as
	// every other walk finding.
	for i := range snap.Receivers {
		body, err := json.Marshal(snap.Receivers[i])
		if err != nil {
			continue
		}
		for _, v := range bcp.ForKind(bcp.KindReceiver) {
			for _, ev := range v.Validate(body) {
				ev.PeerHost = trimURLScheme(c.client.Base)
				if ev.At.IsZero() {
					ev.At = time.Now()
				}
				c.reporter.Report(ev)
			}
		}
	}

	return snap, errs
}

// fire is the DI seam to the spec.Reporter.
func (c *Controller) fire(sev spec.Severity, code, detail, resource string) {
	c.reporter.Report(spec.ComplianceEvent{
		SpecID:    is04.SpecID,
		APIVer:    c.client.Codec.APIVer(),
		SpecPatch: c.client.Codec.SpecPatch(),
		Code:      code,
		Severity:  sev,
		Detail:    detail,
		Resource:  resource,
		PeerHost:  trimURLScheme(c.client.Base),
		At:        time.Now(),
	})
}

// resolveRegistry maps ControllerOptions to a (baseURL, peerAPIVers)
// pair. Walks Mode-B unicast first, then Mode-A mDNS. Returns an
// explicit error when no path is configured.
func resolveRegistry(ctx context.Context, opts ControllerOptions) (string, []string, error) {
	if opts.RegistryURL != "" {
		// Direct unicast — peer api_ver unknown until probe.
		// Caller will negotiate against opts.APIVer or our supported set.
		return strings.TrimRight(opts.RegistryURL, "/"), nil, nil
	}
	timeout := opts.DiscoveryTimeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	// NOT the DiscoverMDNS / DiscoverUnicast aliases: those are the
	// IS-09 helpers and browse `_nmos-system._tcp`. A controller needs
	// `_nmos-query._tcp`, and feeding system instances into
	// pickQueryInstance filtered every one of them out — so discovery
	// always reported "no _nmos-query._tcp instances discovered" and
	// the only mode that ever worked was an explicit --registry URL.
	switch strings.ToLower(opts.DiscoveryMode) {
	case "mdns", "":
		insts, err := browseQueryMDNS(ctx, timeout, opts.Logger)
		if err != nil {
			return "", nil, fmt.Errorf("nmos/controller: mDNS browse: %w", err)
		}
		return pickQueryInstance(insts)
	case "unicast", "static":
		insts, err := dnssdsession.ResolveUnicast(ctx, opts.UnicastResolver,
			dnssd.ServiceQuery, opts.UnicastDomain, timeout)
		if err != nil {
			return "", nil, fmt.Errorf("nmos/controller: unicast browse: %w", err)
		}
		return pickQueryInstance(insts)
	default:
		return "", nil, fmt.Errorf("nmos/controller: unknown DiscoveryMode %q", opts.DiscoveryMode)
	}
}

// browseQueryMDNS browses `_nmos-query._tcp` on the local link. The
// shared session helpers only browse the IS-09 system service, which
// is the wrong record set for a controller looking for a Registry.
func browseQueryMDNS(ctx context.Context, timeout time.Duration, logger *slog.Logger) ([]dnssd.Instance, error) {
	br, err := dnssdsession.NewBrowser(logger)
	if err != nil {
		return nil, err
	}
	defer func() { _ = br.Close() }()

	dctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ch, err := br.Browse(dctx, dnssd.ServiceQuery)
	if err != nil {
		return nil, err
	}
	seen := map[string]dnssd.Instance{}
	for ins := range ch {
		seen[ins.FullName()] = ins
	}
	out := make([]dnssd.Instance, 0, len(seen))
	for _, v := range seen {
		out = append(out, v)
	}
	return out, nil
}

// pickQueryInstance filters the discovery result to `_nmos-query._tcp`
// records, picks highest-pri (lowest pri integer wins per IS-04 §3),
// and constructs a base URL. Returns the per-pri-tied selection in
// definition order.
func pickQueryInstance(insts []dnssd.Instance) (string, []string, error) {
	var queries []dnssd.Instance
	for _, in := range insts {
		if in.Service == dnssd.ServiceQuery {
			queries = append(queries, in)
		}
	}
	if len(queries) == 0 {
		return "", nil, fmt.Errorf("nmos/controller: no _nmos-query._tcp instances discovered")
	}
	best := queries[0]
	bestPri := readPri(best)
	for _, in := range queries[1:] {
		p := readPri(in)
		if p < bestPri {
			best = in
			bestPri = p
		}
	}
	apiProto := best.TXT[dnssd.TXTKeyAPIProto]
	if apiProto == "" {
		apiProto = "http"
	}
	// The ADVERTISED ADDRESS wins over the SRV hostname. A DNS-SD zone
	// names its targets inside its own domain (the AMWA tool's zone is
	// `*.testsuite.nmos.tv`, mDNS's is `.local`), and the system
	// resolver on this host knows neither — the A record travelled with
	// the announcement, so using the name asks a second resolver to
	// rediscover what discovery just told us, and fail.
	host := strings.TrimSuffix(best.Host, ".")
	if len(best.IPv4) > 0 {
		host = best.IPv4[0].String()
	}
	base := fmt.Sprintf("%s://%s:%d", apiProto, host, best.Port)
	apiVers := splitAPIVerTXT(best.TXT[dnssd.TXTKeyAPIVer])
	return base, apiVers, nil
}

// pickCodec runs the peer-version intersection rule with our
// registered IS-04 codecs.
func pickCodec(override string, peerVers []string, rep spec.Reporter) (is04.Codec, error) {
	if override != "" {
		c, ok := is04.Get(override)
		if !ok {
			return nil, fmt.Errorf("nmos/controller: APIVer override %q not registered", override)
		}
		return c, nil
	}
	if len(peerVers) == 0 {
		// Direct unicast with no discovery info — assume our highest.
		return is04.Default(), nil
	}
	c, err := is04.SelectHighest(peerVers)
	if err != nil {
		// Fire the compliance event before bubbling up.
		rep.Report(spec.ComplianceEvent{
			SpecID:   is04.SpecID,
			Code:     "nmos_no_common_api_ver",
			Severity: spec.SeverityError,
			Detail:   err.Error(),
			At:       time.Now(),
		})
		return nil, err
	}
	return c, nil
}

// readPri returns the `pri` TXT integer or maxInt32 when absent /
// malformed. Same semantics as the IS-04 §3 selection rule:
// missing pri = lowest priority (won't be picked over a sibling).
func readPri(in dnssd.Instance) int {
	v := in.TXT[dnssd.TXTKeyPriority]
	if v == "" {
		return 1 << 30
	}
	n := 0
	for _, ch := range v {
		if ch < '0' || ch > '9' {
			return 1 << 30
		}
		n = n*10 + int(ch-'0')
	}
	return n
}

// splitAPIVerTXT parses the comma-separated `api_ver` TXT into a list
// of normalised version strings. Whitespace + case tolerated.
func splitAPIVerTXT(txt string) []string {
	if txt == "" {
		return nil
	}
	parts := strings.Split(txt, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// trimURLScheme returns host:port from a base like "http://h:p".
func trimURLScheme(u string) string {
	if i := strings.Index(u, "://"); i >= 0 {
		return u[i+3:]
	}
	return u
}
