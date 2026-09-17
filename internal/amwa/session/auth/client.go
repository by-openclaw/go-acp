// Package auth — the OAuth 2.0 session layer for BCP-003-02 / IS-10.
//
// Two roles live here:
//
//   - TokenClient: the CLIENT half — obtains access tokens from an
//     Authorization Server via the client_credentials grant (the
//     machine-to-machine grant IS-10 mandates AS support for) and
//     caches them until shortly before expiry;
//   - KeyCache: the RESOURCE-SERVER half — fetches and caches the
//     Authorization Server's JWKS per Behaviour - Resource Servers.md
//     (periodic refresh with jitter, stale keys kept when the server
//     is unreachable), satisfying the http session's KeyProvider.
//
// token.go completes the picture: the NMOS reading of a verified
// token. The keys and the JWS live in internal/auth (imported as jwt),
// because neither is NMOS-specific; only the claim policy is.
//
// Both start from the RFC 8414 server-metadata document at the
// Authorization Server's .well-known endpoint; endpoint URLs are
// never assumed.
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	stdhttp "net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"dhs/internal/amwa/codec/is10"
	jwt "dhs/internal/auth"
)

// httpTimeout caps every exchange with the Authorization Server.
const httpTimeout = 5 * time.Second

// MetadataURL builds the RFC 8414 well-known URL for an Authorization
// Server base (scheme://host[:port]) and optional api_selector TXT
// value.
func MetadataURL(base, apiSelector string) string {
	u := strings.TrimSuffix(base, "/") + is10.WellKnownPath
	if apiSelector != "" {
		u += "/" + strings.Trim(apiSelector, "/")
	}
	return u
}

// fetchJSON GETs url and decodes tolerantly (RFC 8414 documents and
// JWKS carry members beyond the schema minimum).
func fetchJSON(ctx context.Context, hc *stdhttp.Client, u string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()
	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("nmos/auth: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nmos/auth: GET %s: %w", u, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("nmos/auth: read %s: %w", u, err)
	}
	if resp.StatusCode != stdhttp.StatusOK {
		return nil, fmt.Errorf("nmos/auth: GET %s: HTTP %d: %s", u, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// ---- TokenClient ----

// TokenClientOptions configures the client_credentials flow.
type TokenClientOptions struct {
	// MetadataURL is the full RFC 8414 well-known URL (MetadataURL()
	// builds it from a discovered host). Required.
	MetadataURL string
	// ClientID + ClientSecret authenticate the confidential client
	// (client_secret_basic). Required.
	ClientID     string
	ClientSecret string
	// Scope is the space-separated scope list to request — clients
	// MUST include it (Behaviour - Token Requests.md).
	Scope  string
	Logger *slog.Logger
}

// TokenClient obtains + caches access tokens.
type TokenClient struct {
	opts TokenClientOptions
	hc   *stdhttp.Client

	mu    sync.Mutex
	meta  *is10.Metadata
	token string
	exp   time.Time
}

// NewTokenClient builds a client; nothing is fetched until Token.
func NewTokenClient(opts TokenClientOptions) *TokenClient {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &TokenClient{opts: opts, hc: &stdhttp.Client{Timeout: httpTimeout}}
}

// refreshMargin renews tokens shortly before expiry so a token never
// dies in transit (the spec's minimum lifetime is 30 s).
const refreshMargin = 15 * time.Second

// Token returns a valid access token, fetching or refreshing as
// needed. Safe for concurrent use.
func (c *TokenClient) Token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.exp.Add(-refreshMargin)) {
		return c.token, nil
	}
	if c.meta == nil {
		raw, err := fetchJSON(ctx, c.hc, c.opts.MetadataURL)
		if err != nil {
			return "", err
		}
		m, err := is10.DecodeMetadata(raw)
		if err != nil {
			return "", err
		}
		c.meta = &m
	}
	form := url.Values{
		"grant_type": {"client_credentials"},
		"scope":      {c.opts.Scope},
	}
	tctx, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()
	req, err := stdhttp.NewRequestWithContext(tctx, stdhttp.MethodPost, c.meta.TokenEndpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("nmos/auth: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(c.opts.ClientID, c.opts.ClientSecret)
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("nmos/auth: POST %s: %w", c.meta.TokenEndpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("nmos/auth: read token response: %w", err)
	}
	if resp.StatusCode != stdhttp.StatusOK {
		var te is10.TokenError
		if json.Unmarshal(body, &te) == nil && te.Error != "" {
			return "", fmt.Errorf("nmos/auth: token endpoint: %s (%s)", te.Error, te.ErrorDescription)
		}
		return "", fmt.Errorf("nmos/auth: token endpoint HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tr is10.TokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("nmos/auth: decode token response: %w", err)
	}
	if err := tr.Validate(); err != nil {
		return "", err
	}
	c.token = tr.AccessToken
	c.exp = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	c.opts.Logger.Info("nmos/auth: access token obtained",
		"scope", tr.Scope, "expires_in", tr.ExpiresIn)
	return c.token, nil
}

// ---- KeyCache ----

// KeyCache maintains the Authorization Server's public keys for token
// verification. Zero value is unusable — construct with NewKeyCache.
type KeyCache struct {
	metadataURL string
	logger      *slog.Logger
	hc          *stdhttp.Client

	mu   sync.RWMutex
	keys []jwt.JWK
}

// NewKeyCache builds a cache for one Authorization Server.
func NewKeyCache(metadataURL string, logger *slog.Logger) *KeyCache {
	if logger == nil {
		logger = slog.Default()
	}
	return &KeyCache{metadataURL: metadataURL, logger: logger,
		hc: &stdhttp.Client{Timeout: httpTimeout}}
}

// Keys returns the current snapshot (satisfies the http session's
// KeyProvider). Stale keys are deliberately served while the
// Authorization Server is unreachable — the spec says currently held
// keys remain valid until a connection is re-established.
func (k *KeyCache) Keys() []jwt.JWK {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return append([]jwt.JWK(nil), k.keys...)
}

// Fetch resolves metadata → jwks_uri → key set once.
func (k *KeyCache) Fetch(ctx context.Context) error {
	raw, err := fetchJSON(ctx, k.hc, k.metadataURL)
	if err != nil {
		return err
	}
	m, err := is10.DecodeMetadata(raw)
	if err != nil {
		return err
	}
	raw, err = fetchJSON(ctx, k.hc, m.JwksURI)
	if err != nil {
		return err
	}
	set, err := jwt.DecodeJWKS(raw)
	if err != nil {
		return err
	}
	k.mu.Lock()
	k.keys = set.Keys
	k.mu.Unlock()
	k.logger.Info("nmos/auth: JWKS refreshed", "keys", len(set.Keys))
	return nil
}

// FetchIssuer pulls the key set of a so-far-unknown issuer (RFC 8414
// metadata at the issuer's well-known path) and MERGES it into the
// cache — the resource-server answer to a token whose signing key we
// do not hold (Behaviour - Resource Servers.md: obtain the missing
// key via the token's iss claim). Existing keys stay.
func (k *KeyCache) FetchIssuer(ctx context.Context, issuer string) error {
	raw, err := fetchJSON(ctx, k.hc, MetadataURL(issuer, ""))
	if err != nil {
		return err
	}
	m, err := is10.DecodeMetadata(raw)
	if err != nil {
		return err
	}
	raw, err = fetchJSON(ctx, k.hc, m.JwksURI)
	if err != nil {
		return err
	}
	set, err := jwt.DecodeJWKS(raw)
	if err != nil {
		return err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	have := map[string]bool{}
	for _, existing := range k.keys {
		have[existing.Kid+"|"+existing.N] = true
	}
	added := 0
	for _, nk := range set.Keys {
		if !have[nk.Kid+"|"+nk.N] {
			k.keys = append(k.keys, nk)
			added++
		}
	}
	k.logger.Info("nmos/auth: issuer keys merged", "issuer", issuer, "added", added)
	return nil
}

// Run refreshes the key set on the spec cadence: at least once every
// hour, jittered by 0–60 s so a fleet of resource servers does not
// synchronise its fetches. Failures keep the stale set. Blocks until
// ctx is done.
func (k *KeyCache) Run(ctx context.Context) {
	for {
		jitter := time.Duration(rand.Intn(60)) * time.Second
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Hour + jitter):
		}
		if err := k.Fetch(ctx); err != nil {
			k.logger.Warn("nmos/auth: JWKS refresh failed; keeping cached keys", "err", err)
		}
	}
}
