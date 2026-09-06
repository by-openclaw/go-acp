// Package certmgr — the BCP-003-03 EST client session: bootstrap the
// network Root CA, enroll for a TLS server certificate, keep it
// renewed, and hand live tls.Certificate handles to the HTTPS
// listeners (BCP-003-01 serving side).
//
// Trust model, per the spec's Security Considerations: the FIRST
// /cacerts request against a unicast-DNS-discovered or manually
// configured EST server is explicitly trusted (no TLS verification) —
// the returned CA then becomes the Explicit Trust Anchor for every
// later exchange with the EST server and the NMOS peers. Explicit
// trust can be disabled, in which case the current trust store must
// already validate the EST server.
package certmgr

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	stdhttp "net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"dhs/internal/amwa/codec/est"
	"dhs/internal/transport"
)

// Options configures the manager.
type Options struct {
	// ESTBase is the full EST API base (est.BaseURL output —
	// https://host:port/.well-known/est[/label]). Required for EST
	// mode; empty means manual-certificate mode.
	ESTBase string
	// Hostnames are this server's DNS identities; [0] becomes the CSR
	// CommonName, all become SANs. Required for EST mode.
	Hostnames []string
	// Serial populates the CSR serialNumber attribute.
	Serial string
	// DataDir is where ca.pem / server.crt / server.key persist.
	// Required.
	DataDir string
	// ExplicitTrust skips TLS verification on the FIRST /cacerts
	// exchange (the spec's bootstrap deviation). Default true.
	ExplicitTrustDisabled bool
	// ClientCert, when set, is presented to the EST server during
	// enrollment (the manufacturer-issued TLS Client Certificate).
	ClientCert *tls.Certificate
	Logger     *slog.Logger
}

// Manager owns the certificate lifecycle.
type Manager struct {
	opts Options
	log  *slog.Logger

	mu       sync.RWMutex
	roots    []*x509.Certificate
	inters   []*x509.Certificate
	rootPool *x509.CertPool
	current  *tls.Certificate
	leaf     *x509.Certificate
	// manualPairs collects manually installed pairs — BCP-003-01 says
	// servers SHOULD support multiple certificates (RSA and ECDSA);
	// Go's TLS stack selects per ClientHello when several are offered.
	manualPairs []tls.Certificate
}

// New builds an unstarted manager.
func New(opts Options) (*Manager, error) {
	if opts.DataDir == "" {
		return nil, fmt.Errorf("certmgr: DataDir required")
	}
	if opts.ESTBase != "" && len(opts.Hostnames) == 0 {
		return nil, fmt.Errorf("certmgr: EST mode requires at least one hostname")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if err := os.MkdirAll(opts.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("certmgr: create data dir: %w", err)
	}
	return &Manager{opts: opts, log: opts.Logger}, nil
}

// LoadManual installs a cert/key pair from files (the spec-mandated
// manual path for plants without an EST server).
func (m *Manager) LoadManual(certFile, keyFile string) error {
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("certmgr: load manual pair: %w", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return fmt.Errorf("certmgr: parse manual leaf: %w", err)
	}
	m.mu.Lock()
	m.manualPairs = append(m.manualPairs, pair)
	if m.current == nil {
		m.current, m.leaf = &pair, leaf
	}
	m.mu.Unlock()
	m.log.Info("certmgr: manual certificate installed",
		"cn", leaf.Subject.CommonName, "not_after", leaf.NotAfter,
		"pairs", len(m.manualPairs))
	return nil
}

// LoadManualRoots installs a trust-root PEM file for outbound client
// verification (BCP-003-01: clients SHALL install a root cert).
func (m *Manager) LoadManualRoots(caFile string) error {
	raw, err := os.ReadFile(caFile)
	if err != nil {
		return fmt.Errorf("certmgr: read CA file: %w", err)
	}
	certs, err := parsePEMCerts(raw)
	if err != nil || len(certs) == 0 {
		return fmt.Errorf("certmgr: no certificates in %s: %v", caFile, err)
	}
	m.setRoots(certs, nil)
	return nil
}

// httpClient builds the transport for one phase. bootstrap toggles
// the explicit-trust (no-verify) mode; clientCert attaches the enrollment
// identity.
//
// The POSTURE is transport's, not ours. Issuing and renewing certificates is
// this package's job (BCP-003-03 EST); deciding a minimum TLS version and
// how a *tls.Config is assembled is not, and a config built here was one
// more place to forget MinVersion. What stays ours is the two decisions that
// are genuinely about enrolment: trusting nothing yet during bootstrap, and
// presenting an identity that lives in memory rather than in a file —
// TLSOptions.Certificates exists for exactly that.
func (m *Manager) httpClient(bootstrap bool, clientCert *tls.Certificate) *stdhttp.Client {
	opts := transport.TLSOptions{Enable: true}
	if bootstrap && !m.opts.ExplicitTrustDisabled {
		opts.Insecure = true // spec bootstrap deviation, see package doc
	} else {
		m.mu.RLock()
		opts.RootCAs = m.rootPool
		m.mu.RUnlock()
	}
	if clientCert != nil {
		opts.Certificates = []tls.Certificate{*clientCert}
	}
	// The error is unreachable here: Client only fails while reading a
	// CAFile or a CertFile/KeyFile pair off disk, and this call passes
	// neither — the trust pool and the identity are both already in memory.
	cfg, _ := opts.Client()
	return &stdhttp.Client{
		Timeout:   15 * time.Second,
		Transport: &stdhttp.Transport{TLSClientConfig: cfg},
	}
}

// Bootstrap fetches /cacerts and establishes the Explicit Trust
// Anchor database, persisting it to DataDir/ca.pem.
func (m *Manager) Bootstrap(ctx context.Context) error {
	if m.opts.ESTBase == "" {
		return fmt.Errorf("certmgr: no EST server configured")
	}
	body, _, err := m.do(ctx, m.httpClient(true, nil), stdhttp.MethodGet,
		m.opts.ESTBase+"/cacerts", "", nil)
	if err != nil {
		return err
	}
	certs, err := est.ParseCertsResponse(body)
	if err != nil {
		return err
	}
	var roots, inters []*x509.Certificate
	for _, c := range certs {
		if c.IsCA && c.CheckSignatureFrom(c) == nil {
			roots = append(roots, c)
		} else {
			inters = append(inters, c)
		}
	}
	if len(roots) == 0 {
		// A chain of intermediates with no self-signed head still
		// anchors trust at its top for our purposes.
		roots, inters = certs, nil
	}
	m.setRoots(roots, inters)
	if err := m.persistRoots(); err != nil {
		return err
	}
	m.log.Info("certmgr: root CA provisioned via EST", "roots", len(roots), "intermediates", len(inters))
	return nil
}

// Enroll requests a TLS server certificate via /simpleenroll. The
// manufacturer client certificate rides the handshake when present.
func (m *Manager) Enroll(ctx context.Context) error {
	return m.enroll(ctx, "/simpleenroll", m.opts.ClientCert)
}

// Renew re-enrolls via /simplereenroll, presenting the CURRENT
// certificate as the TLS client identity (spec requirement).
func (m *Manager) Renew(ctx context.Context) error {
	m.mu.RLock()
	cur := m.current
	m.mu.RUnlock()
	if cur == nil {
		return m.Enroll(ctx)
	}
	return m.enroll(ctx, "/simplereenroll", cur)
}

func (m *Manager) enroll(ctx context.Context, endpoint string, clientCert *tls.Certificate) error {
	if m.opts.ESTBase == "" {
		return fmt.Errorf("certmgr: no EST server configured")
	}
	// RSA is the algorithm every consumer MUST cope with; one RSA CSR
	// keeps the flow lean (the spec's per-algorithm fan-out is a
	// SHOULD — noted as future work).
	csrDER, key, err := est.NewCSR(est.CSROptions{
		CommonName: m.opts.Hostnames[0], DNSNames: m.opts.Hostnames,
		SerialNumber: m.opts.Serial, Algorithm: est.KeyRSA2048,
	})
	if err != nil {
		return err
	}
	hc := m.httpClient(false, clientCert)
	url := m.opts.ESTBase + endpoint
	var body []byte
	for attempt := 0; ; attempt++ {
		var retryAfter string
		body, retryAfter, err = m.do(ctx, hc, stdhttp.MethodPost, url,
			est.ContentTypePKCS10, est.EncodeCSRBody(csrDER))
		if err == nil {
			break
		}
		// 202/503 carry Retry-After: wait it out a bounded number of
		// times (Certificate Request Response rules).
		if retryAfter == "" || attempt >= 3 {
			return err
		}
		secs, cerr := strconv.Atoi(retryAfter)
		if cerr != nil || secs < 1 {
			secs = 1
		}
		m.log.Info("certmgr: EST asked to retry", "endpoint", endpoint, "retry_after_s", secs)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(secs) * time.Second):
		}
	}
	certs, err := est.ParseCertsResponse(body)
	if err != nil {
		return err
	}
	leaf := certs[0]
	m.mu.RLock()
	pool := m.rootPool
	inters := append(append([]*x509.Certificate{}, m.inters...), certs[1:]...)
	m.mu.RUnlock()
	if pool == nil {
		return fmt.Errorf("certmgr: enroll before Bootstrap — no trust anchors")
	}
	if err := est.ValidateIssued(leaf, m.opts.Hostnames[0], pool, inters, time.Now()); err != nil {
		return err
	}
	pair, err := buildPair(leaf, certs[1:], key)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.current, m.leaf = pair, leaf
	m.mu.Unlock()
	if err := m.persistPair(leaf, certs[1:], key); err != nil {
		return err
	}
	m.log.Info("certmgr: certificate enrolled", "endpoint", endpoint,
		"cn", leaf.Subject.CommonName, "not_after", leaf.NotAfter, "serial", leaf.SerialNumber)
	return nil
}

// do performs one EST exchange. Non-200 returns an error; 202/503
// also surface the Retry-After header for the caller's wait loop.
func (m *Manager) do(ctx context.Context, hc *stdhttp.Client, method, url, contentType string, body []byte) ([]byte, string, error) {
	var rdr io.Reader
	if body != nil {
		rdr = readerOf(body)
	}
	req, err := stdhttp.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return nil, "", fmt.Errorf("certmgr: build %s: %w", url, err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Content-Transfer-Encoding", "base64")
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("certmgr: %s %s: %w", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, "", fmt.Errorf("certmgr: read %s: %w", url, err)
	}
	if resp.StatusCode == stdhttp.StatusOK {
		return raw, "", nil
	}
	return nil, resp.Header.Get("Retry-After"),
		fmt.Errorf("certmgr: %s %s: HTTP %d", method, url, resp.StatusCode)
}

// Run keeps the certificate renewed: checks every 10 minutes and
// renews once past the spec's recommended 80% of lifetime; failures
// back off per the halving rule (retry after half the remaining
// validity, floor 1 minute). Blocks until ctx is done.
func (m *Manager) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Minute):
		}
		m.mu.RLock()
		leaf := m.leaf
		m.mu.RUnlock()
		if leaf == nil || !est.RenewalDue(leaf, time.Now(), est.RenewalRecommendedFraction) {
			continue
		}
		if err := m.Renew(ctx); err != nil {
			wait := time.Until(leaf.NotAfter) / 2
			if wait < time.Minute {
				wait = time.Minute
			}
			m.log.Warn("certmgr: renewal failed; keeping current certificate",
				"err", err, "retry_in", wait)
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
	}
}

// Certificate returns the live pair (nil when none yet).
func (m *Manager) Certificate() *tls.Certificate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

// GetCertificate is the tls.Config hook — hot-reloads on renewal.
func (m *Manager) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	if c := m.Certificate(); c != nil {
		return c, nil
	}
	return nil, fmt.Errorf("certmgr: no certificate provisioned yet")
}

// Roots returns the trust-anchor pool for outbound verification.
func (m *Manager) Roots() *x509.CertPool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.rootPool
}

// TLSServerConfig builds the BCP-003-01 serving floor: TLS 1.2
// minimum (1.3 negotiates automatically and uses its own fixed
// suites), the mandatory TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256 plus
// the SHOULD-list suites Go implements, server-preference ordering.
func (m *Manager) TLSServerConfig() *tls.Config {
	m.mu.RLock()
	pairs := append([]tls.Certificate(nil), m.manualPairs...)
	m.mu.RUnlock()
	if len(pairs) > 1 {
		// Multiple manual pairs (RSA + ECDSA): hand the whole set to
		// the TLS stack, which selects per ClientHello.
		return &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: pairs,
			CipherSuites: []uint16{
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			},
		}
	}
	return &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: m.GetCertificate,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		},
	}
}

// ---- internals ----

func (m *Manager) setRoots(roots, inters []*x509.Certificate) {
	pool := x509.NewCertPool()
	for _, c := range roots {
		pool.AddCert(c)
	}
	m.mu.Lock()
	m.roots, m.inters, m.rootPool = roots, inters, pool
	m.mu.Unlock()
}

func (m *Manager) persistRoots() error {
	m.mu.RLock()
	all := append(append([]*x509.Certificate{}, m.roots...), m.inters...)
	m.mu.RUnlock()
	var buf []byte
	for _, c := range all {
		buf = append(buf, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})...)
	}
	return atomicWrite(filepath.Join(m.opts.DataDir, "ca.pem"), buf, 0o644)
}

func (m *Manager) persistPair(leaf *x509.Certificate, chain []*x509.Certificate, key crypto.Signer) error {
	var crt []byte
	for _, c := range append([]*x509.Certificate{leaf}, chain...) {
		crt = append(crt, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})...)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("certmgr: marshal key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	if err := atomicWrite(filepath.Join(m.opts.DataDir, "server.crt"), crt, 0o644); err != nil {
		return err
	}
	return atomicWrite(filepath.Join(m.opts.DataDir, "server.key"), keyPEM, 0o600)
}

func buildPair(leaf *x509.Certificate, chain []*x509.Certificate, key crypto.Signer) (*tls.Certificate, error) {
	switch key.(type) {
	case *rsa.PrivateKey, *ecdsa.PrivateKey:
	default:
		return nil, fmt.Errorf("certmgr: unsupported key type %T", key)
	}
	pair := &tls.Certificate{PrivateKey: key, Leaf: leaf}
	pair.Certificate = append(pair.Certificate, leaf.Raw)
	for _, c := range chain {
		pair.Certificate = append(pair.Certificate, c.Raw)
	}
	return pair, nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return fmt.Errorf("certmgr: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("certmgr: rename %s: %w", path, err)
	}
	return nil
}

func parsePEMCerts(raw []byte) ([]*x509.Certificate, error) {
	var out []*x509.Certificate
	for {
		var blk *pem.Block
		blk, raw = pem.Decode(raw)
		if blk == nil {
			break
		}
		if blk.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(blk.Bytes)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// readerOf avoids importing bytes just for one call site.
type sliceReader struct {
	b []byte
	i int
}

func readerOf(b []byte) io.Reader { return &sliceReader{b: b} }

func (r *sliceReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}
