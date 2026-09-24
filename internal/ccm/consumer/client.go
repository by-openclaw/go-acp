// Package consumer is the outbound client for the EVS Neuron REST API
// (issue #975). It walks the https://<neuron>/api/v1/ tree and returns
// the device's streams keyed by UUID — the identifier that lines up
// with the plant's NMOS registry and (via leg stream ids) the ACP2
// view of the same box.
//
// The REST API is unauthenticated on the lab device and served over a
// self-signed cert; the client skips cert verification by default
// (a media-plane device on a closed VLAN), overridable.
package consumer

import (
	"context"
	"fmt"
	stdhttp "net/http"
	"strings"
	"time"

	"dhs/internal/ccm/codec"
	"dhs/internal/transport"
	transporthttp "dhs/internal/transport/http"
)

// Client talks to one Neuron REST API.
type Client struct {
	base string
	// host and resolved support [Client.Resolve]: which device to
	// re-base against, and whether the base is already settled.
	host     string
	resolved bool
	// specPath is the OpenAPI document's path relative to the base,
	// when the operator named one. Empty means try [SpecPaths].
	specPath string
	http     *transporthttp.Client
}

// Base is the API root every path is relative to, for provenance and
// for an operator who needs to know which one was chosen.
func (c *Client) Base() string { return c.base }

// Resolve finds the base this device's API hangs off, when none was
// given.
//
// It asks for /self under each candidate and keeps the first that
// answers. That is one extra round trip on a device whose firmware
// moved, none on the one it did not, and it is the difference between
// a connector that works on this fleet and one that works on the half
// of it that has not been upgraded.
func (c *Client) Resolve(ctx context.Context) error {
	if c.resolved {
		return nil
	}
	// Keep whatever scheme this client was built with. Production is
	// always https; a test server on loopback has no certificate.
	scheme := "https://"
	if i := strings.Index(c.base, "://"); i >= 0 {
		scheme = c.base[:i+3]
	}

	var tried []string
	for _, candidate := range APIBases {
		base := scheme + c.host + candidate
		tried = append(tried, base)
		if _, err := c.http.GetBytes(ctx, base+"/self"); err != nil {
			continue
		}
		c.base, c.resolved = base, true
		return nil
	}
	return fmt.Errorf("ccm: %s answered none of %s — name the right one with --api-base",
		c.host, strings.Join(tried, ", "))
}

// Options configures the client.
type Options struct {
	// Host is the Neuron address (host or host:port). https is assumed.
	Host string
	// Timeout bounds each request. 0 → 8s.
	Timeout time.Duration
	// Insecure skips TLS verification (default true — lab self-signed).
	Insecure bool
	// VerifyTLS, when set, forces verification on (overrides Insecure).
	VerifyTLS bool
	// APIBase is the path the API hangs off, "/api/v1" or "/api".
	// Empty means find it — see [Client.Resolve].
	APIBase string
	// APISpec is the OpenAPI document, relative to the base
	// ("/docs/openapi.yml"). Empty tries [SpecPaths] in order.
	APISpec string
}

// APIBases are the bases tried, in order, when none was given.
//
// EVS moved it. BRIDGE 7.0.3 serves /api/v1 and its documents at
// /api/v1/docs/; the firmware on the shuffler dropped the version
// segment and serves /api with its documents at /api/docs/. A
// connector that hardcoded either one is a connector that works on
// half the fleet, so it asks the device instead — and an operator can
// still name a base outright, for a deployment behind a proxy prefix
// that no probe would guess.
var APIBases = []string{"/api/v1", "/api"}

// MaxBody caps a single Neuron response. The api.yml OpenAPI document is
// the largest thing this client fetches — a few hundred KiB on BRIDGE
// 6.7.4 — so 8 MiB leaves room for a much richer firmware while still
// refusing a device that answers with something absurd.
const MaxBody = 8 << 20

// tlsClientConfig builds the client TLS posture, behind a package
// variable. Its only failure modes are reading a CA or client
// certificate FILE, and this client configures neither — so the arm
// below is unreachable from here, and the variable is what proves the
// answer it gives anyway. Production never reassigns it.
var tlsClientConfig = transport.TLSOptions.Client

// New builds a client.
func New(opts Options) *Client {
	if opts.Timeout == 0 {
		opts.Timeout = 8 * time.Second
	}
	// Posture only — the *tls.Config is built once in the transport layer,
	// so this client picks up transport.MinTLSVersion instead of the
	// version-floor-less config it used to assemble here. Skip-verify stays
	// the default because the media-plane device is self-signed by design;
	// VerifyTLS opts back in.
	cfg, err := tlsClientConfig(transport.TLSOptions{
		Enable:   true,
		Insecure: !opts.VerifyTLS,
	})
	if err != nil {
		// Unreachable: no CA or client-certificate file is configured, and
		// those are Client's only failure modes. A nil config is the safe
		// answer if that ever stops being true — net/http then applies its
		// own defaults, which verify.
		cfg = nil
	}
	// Normalise FIRST, then decide whether anything was given: an
	// environment variable set to whitespace is not a base, and
	// treating it as one would skip the probe and then ask the device
	// for "/self" at its web root.
	apiBase := normalizeAPIBase(opts.APIBase)
	base := "https://" + opts.Host + apiBase
	if apiBase == "" {
		// Unresolved until Resolve runs; APIBases[0] is what a caller
		// that never resolves falls back to, which is the firmware
		// this connector was written against.
		base = "https://" + opts.Host + APIBases[0]
	}
	return &Client{
		base:     base,
		resolved: apiBase != "",
		host:     opts.Host,
		specPath: normalizeAPIBase(opts.APISpec),
		http: &transporthttp.Client{
			HTTP: &stdhttp.Client{
				Timeout:   opts.Timeout,
				Transport: &stdhttp.Transport{TLSClientConfig: cfg},
			},
			MaxBody: MaxBody,
		},
	}
}

// get fetches one API path (relative to /api/v1) as raw bytes.
//
// Raw rather than decoded because the paths this client walks are not all
// JSON — /docs/api.yml is the device's own OpenAPI document — and the JSON
// ones are decoded by the ccm codec, which absorbs deviations rather than
// failing on them.
func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	body, err := c.http.GetBytes(ctx, c.base+path)
	if err != nil {
		return nil, fmt.Errorf("neuron GET %s: %w", path, err)
	}
	return body, nil
}

// SpecPaths are the document names tried, in order, relative to the
// API base.
//
// EVS renamed it along with the base: BRIDGE 7.0.3 serves
// /api/v1/docs/api.yml, and the firmware on the shuffler serves
// /api/docs/openapi.yml. Both are the same document — the device's own
// OpenAPI schema, served unauthenticated, and the artefact to diff
// across a firmware upgrade — so the connector asks for each in turn
// rather than knowing which fleet it is on.
var SpecPaths = []string{"/docs/api.yml", "/docs/openapi.yml"}

// FetchSpec downloads the device's own OpenAPI schema.
//
// It returns the document and the path it came from, so a caller can
// say which one this device serves — a firmware diff wants to know
// that the name moved, not just that the content did.
func (c *Client) FetchSpec(ctx context.Context) ([]byte, string, error) {
	paths := SpecPaths
	if c.specPath != "" {
		paths = []string{c.specPath}
	}
	var (
		tried []string
		first error
	)
	for _, p := range paths {
		body, err := c.get(ctx, p)
		if err == nil {
			return body, p, nil
		}
		tried = append(tried, c.base+p)
		if first == nil {
			first = err
		}
	}
	return nil, "", fmt.Errorf("ccm: no OpenAPI document at %s — name it with --api-spec: %w",
		strings.Join(tried, ", "), first)
}

// Walk reads /self plus every io/ip sender and receiver, returning the
// device with its streams keyed by UUID. Deviations (a stream with no
// uuid) are returned, never swallowed.
func (c *Client) Walk(ctx context.Context) (*codec.Device, []string, error) {
	selfBody, err := c.get(ctx, "/self")
	if err != nil {
		return nil, nil, err
	}
	dev, err := codec.DecodeSelf(selfBody)
	if err != nil {
		return nil, nil, err
	}
	var deviations []string
	for _, spec := range []struct {
		path    string
		kind    codec.Kind
		essence codec.Essence
	}{
		{"/io/ip/senders/video", codec.KindSender, codec.EssenceVideo},
		{"/io/ip/senders/audio", codec.KindSender, codec.EssenceAudio},
		{"/io/ip/senders/data", codec.KindSender, codec.EssenceData},
		{"/io/ip/receivers/video", codec.KindReceiver, codec.EssenceVideo},
		{"/io/ip/receivers/audio", codec.KindReceiver, codec.EssenceAudio},
		{"/io/ip/receivers/data", codec.KindReceiver, codec.EssenceData},
	} {
		body, gerr := c.get(ctx, spec.path)
		if gerr != nil {
			// A tree a device does not serve is not fatal — record and
			// keep walking, so a partial Neuron still yields what it has.
			deviations = append(deviations, fmt.Sprintf("%s: %v", spec.path, gerr))
			continue
		}
		streams, skipped, derr := codec.DecodeStreams(body, spec.kind, spec.essence)
		if derr != nil {
			deviations = append(deviations, derr.Error())
			continue
		}
		deviations = append(deviations, skipped...)
		for _, s := range streams {
			dev.Streams[s.UUID] = s
		}
	}
	return &dev, deviations, nil
}

// normalizeAPIBase makes a caller-supplied base a usable path prefix:
// leading slash, no trailing slash, empty stays empty.
//
// "api/v1" is what somebody types when they are reading the URL off a
// browser, and turning it into "https://host" + "api/v1" would produce
// a host that does not exist and an error about DNS. The leading slash
// is added rather than demanded.
func normalizeAPIBase(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}
	base = strings.TrimSuffix(base, "/")
	if base == "" {
		return ""
	}
	if !strings.HasPrefix(base, "/") {
		base = "/" + base
	}
	return base
}
