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
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"time"

	"dhs/internal/ccm/codec"
	"dhs/internal/transport"
)

// Client talks to one Neuron REST API.
type Client struct {
	base string
	http *http.Client
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
}

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
	cfg, err := transport.TLSOptions{
		Enable:   true,
		Insecure: !opts.VerifyTLS,
	}.Client()
	if err != nil {
		// Unreachable: no CA or client-certificate file is configured, and
		// those are Client's only failure modes. Falling back to a verifying
		// config is the safe answer if that ever stops being true.
		cfg = &tls.Config{MinVersion: transport.MinTLSVersion}
	}
	tr := &http.Transport{TLSClientConfig: cfg}
	return &Client{
		base: "https://" + opts.Host + "/api/v1",
		http: &http.Client{Timeout: opts.Timeout, Transport: tr},
	}
}

// get fetches one API path (relative to /api/v1) as raw bytes.
func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("neuron GET %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("neuron GET %s: HTTP %d", path, resp.StatusCode)
	}
	return body, nil
}

// FetchSpec downloads the device's own OpenAPI schema (the CCM DM
// contract) from /api/v1/docs/api.yml. It is served unauthenticated and
// is the artifact to diff across firmware upgrades.
func (c *Client) FetchSpec(ctx context.Context) ([]byte, error) {
	return c.get(ctx, "/docs/api.yml")
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
