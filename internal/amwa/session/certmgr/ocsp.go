package certmgr

// OCSP stapling (RFC 6960, BCP-003-01 "X.509 Certificates and
// Certificate Authority": a server SHOULD staple). The server asks its
// issuer's responder — the AIA URL in its own certificate — for the
// status of its leaf, verifies the answer, and hands the DER to the
// TLS stack as the staple a client receives in the handshake. Refreshed
// before the response's nextUpdate, so a client never sees a stale one.
//
// Stdlib only (ADR-0005): the two ASN.1 shapes this needs are small, and
// the response is verified here before it is ever stapled — a server
// that staples an unverified blob would hand its clients a "revoked"
// or a forgery with its own authority behind it.

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // RFC 5019 §2.1: CertID hashes are SHA-1 by convention
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"io"
	"math/big"
	stdhttp "net/http"
	"time"
)

// ---- ASN.1 shapes, RFC 6960 §4.1 (request) and §4.2 (response) ----

var (
	oidSHA1          = asn1.ObjectIdentifier{1, 3, 14, 3, 2, 26}
	oidOCSPBasic     = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 1}
	oidSHA256RSA     = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 11}
	oidSHA384RSA     = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 12}
	oidSHA512RSA     = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 13}
	oidSHA1RSA       = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 5}
	oidECDSAWithSHA2 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
	oidECDSAWithSHA3 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 3}
	oidECDSAWithSHA5 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 4}
)

type ocspCertID struct {
	HashAlgorithm  pkix.AlgorithmIdentifier
	IssuerNameHash []byte
	IssuerKeyHash  []byte
	SerialNumber   *big.Int
}

type ocspSingleRequest struct {
	CertID ocspCertID
}

type ocspTBSRequest struct {
	RequestList []ocspSingleRequest
}

type ocspRequest struct {
	TBSRequest ocspTBSRequest
}

type ocspResponseBytes struct {
	ResponseType asn1.ObjectIdentifier
	Response     []byte
}

type ocspResponse struct {
	Status        asn1.Enumerated
	ResponseBytes ocspResponseBytes `asn1:"explicit,tag:0,optional"`
}

type ocspBasicResponse struct {
	TBSResponseData    asn1.RawValue
	SignatureAlgorithm pkix.AlgorithmIdentifier
	Signature          asn1.BitString
	Certificates       []asn1.RawValue `asn1:"explicit,tag:0,optional"`
}

type ocspResponseData struct {
	Raw         asn1.RawContent
	Version     int           `asn1:"optional,explicit,default:0,tag:0"`
	ResponderID asn1.RawValue // CHOICE byName [1] / byKey [2]
	ProducedAt  time.Time     `asn1:"generalized"`
	Responses   []ocspSingleResponse
	Extensions  []pkix.Extension `asn1:"explicit,tag:1,optional"`
}

type ocspSingleResponse struct {
	CertID     ocspCertID
	CertStatus asn1.RawValue    // CHOICE good [0] / revoked [1] / unknown [2], IMPLICIT
	ThisUpdate time.Time        `asn1:"generalized"`
	NextUpdate time.Time        `asn1:"generalized,explicit,tag:0,optional"`
	Extensions []pkix.Extension `asn1:"explicit,tag:1,optional"`
}

// ocspStatusSuccessful is OCSPResponseStatus successful (0).
const ocspStatusSuccessful = 0

// issuerKeyHash is the SHA-1 of the issuer's subjectPublicKey BIT STRING
// contents (not of the whole SubjectPublicKeyInfo), per RFC 6960 §4.1.1.
func issuerKeyHash(issuer *x509.Certificate) ([]byte, error) {
	var spki struct {
		Algorithm pkix.AlgorithmIdentifier
		PublicKey asn1.BitString
	}
	if _, err := asn1.Unmarshal(issuer.RawSubjectPublicKeyInfo, &spki); err != nil {
		return nil, fmt.Errorf("issuer public key: %w", err)
	}
	h := sha1.Sum(spki.PublicKey.RightAlign()) //nolint:gosec // CertID hash, RFC 5019
	return h[:], nil
}

// certIDFor builds the CertID a responder answers about: the leaf's
// serial under its issuer's name and key hashes.
func certIDFor(leaf, issuer *x509.Certificate) (ocspCertID, error) {
	kh, err := issuerKeyHash(issuer)
	if err != nil {
		return ocspCertID{}, err
	}
	nh := sha1.Sum(issuer.RawSubject) //nolint:gosec // CertID hash, RFC 5019
	return ocspCertID{
		HashAlgorithm:  pkix.AlgorithmIdentifier{Algorithm: oidSHA1, Parameters: asn1.NullRawValue},
		IssuerNameHash: nh[:],
		IssuerKeyHash:  kh,
		SerialNumber:   leaf.SerialNumber,
	}, nil
}

// encodeOCSPRequest is the DER a responder takes in a POST body.
func encodeOCSPRequest(leaf, issuer *x509.Certificate) ([]byte, error) {
	id, err := certIDFor(leaf, issuer)
	if err != nil {
		return nil, err
	}
	return asn1.Marshal(ocspRequest{TBSRequest: ocspTBSRequest{
		RequestList: []ocspSingleRequest{{CertID: id}},
	}})
}

// ocspStaple is a verified response ready to staple.
type ocspStaple struct {
	der        []byte
	thisUpdate time.Time
	nextUpdate time.Time // zero when the responder set none
}

// errOCSP names a response that cannot be stapled.
var errOCSP = errors.New("certmgr: ocsp")

// verifyOCSPResponse checks a responder's DER answer for leaf: a
// successful basic response, signed by the issuer or by a responder
// certificate the issuer signed, carrying a `good` status for exactly
// this CertID, and current at now.
func verifyOCSPResponse(der []byte, leaf, issuer *x509.Certificate, now time.Time) (ocspStaple, error) {
	var resp ocspResponse
	rest, err := asn1.Unmarshal(der, &resp)
	if err != nil || len(rest) != 0 {
		return ocspStaple{}, fmt.Errorf("%w: response is not OCSPResponse DER", errOCSP)
	}
	if resp.Status != ocspStatusSuccessful {
		return ocspStaple{}, fmt.Errorf("%w: responder status %d", errOCSP, resp.Status)
	}
	if !resp.ResponseBytes.ResponseType.Equal(oidOCSPBasic) {
		return ocspStaple{}, fmt.Errorf("%w: response type %v is not id-pkix-ocsp-basic", errOCSP, resp.ResponseBytes.ResponseType)
	}
	var basic ocspBasicResponse
	rest, err = asn1.Unmarshal(resp.ResponseBytes.Response, &basic)
	if err != nil || len(rest) != 0 {
		return ocspStaple{}, fmt.Errorf("%w: response is not BasicOCSPResponse DER", errOCSP)
	}
	var data ocspResponseData
	rest, err = asn1.Unmarshal(basic.TBSResponseData.FullBytes, &data)
	if err != nil || len(rest) != 0 {
		return ocspStaple{}, fmt.Errorf("%w: ResponseData does not parse", errOCSP)
	}

	// Who signed it: the issuer itself, or a responder the issuer
	// delegated to (a certificate carried in the response, signed by
	// the issuer, with the OCSP signing EKU).
	sigAlgo, err := signatureAlgorithm(basic.SignatureAlgorithm.Algorithm)
	if err != nil {
		return ocspStaple{}, err
	}
	signed := basic.TBSResponseData.FullBytes
	sig := basic.Signature.RightAlign()
	if issuer.CheckSignature(sigAlgo, signed, sig) != nil {
		if err := delegatedSignature(basic.Certificates, issuer, sigAlgo, signed, sig, now); err != nil {
			return ocspStaple{}, err
		}
	}

	want, err := certIDFor(leaf, issuer)
	if err != nil {
		return ocspStaple{}, err
	}
	for _, r := range data.Responses {
		if !sameCertID(r.CertID, want) {
			continue
		}
		// good is [0] IMPLICIT NULL: context-specific tag 0.
		if r.CertStatus.Class != asn1.ClassContextSpecific || r.CertStatus.Tag != 0 {
			return ocspStaple{}, fmt.Errorf("%w: certificate status is not good (tag %d)", errOCSP, r.CertStatus.Tag)
		}
		if r.ThisUpdate.After(now.Add(time.Minute)) {
			return ocspStaple{}, fmt.Errorf("%w: thisUpdate %s is in the future", errOCSP, r.ThisUpdate.Format(time.RFC3339))
		}
		if !r.NextUpdate.IsZero() && !r.NextUpdate.After(now) {
			return ocspStaple{}, fmt.Errorf("%w: response expired at %s", errOCSP, r.NextUpdate.Format(time.RFC3339))
		}
		return ocspStaple{der: der, thisUpdate: r.ThisUpdate, nextUpdate: r.NextUpdate}, nil
	}
	return ocspStaple{}, fmt.Errorf("%w: no status for this certificate in the response", errOCSP)
}

// delegatedSignature accepts a response signed by a responder
// certificate the issuer signed for OCSP signing (RFC 6960 §4.2.2.2).
func delegatedSignature(certs []asn1.RawValue, issuer *x509.Certificate, algo x509.SignatureAlgorithm, signed, sig []byte, now time.Time) error {
	for _, raw := range certs {
		c, err := x509.ParseCertificate(raw.FullBytes)
		if err != nil {
			continue
		}
		if c.CheckSignature(algo, signed, sig) != nil {
			continue
		}
		if issuer.CheckSignature(c.SignatureAlgorithm, c.RawTBSCertificate, c.Signature) != nil {
			return fmt.Errorf("%w: responder certificate is not signed by the issuer", errOCSP)
		}
		if !hasOCSPSigning(c) {
			return fmt.Errorf("%w: responder certificate lacks the OCSP signing extended key usage", errOCSP)
		}
		if now.Before(c.NotBefore) || now.After(c.NotAfter) {
			return fmt.Errorf("%w: responder certificate is not valid at %s", errOCSP, now.Format(time.RFC3339))
		}
		return nil
	}
	return fmt.Errorf("%w: response is signed by neither the issuer nor a delegated responder", errOCSP)
}

func hasOCSPSigning(c *x509.Certificate) bool {
	for _, u := range c.ExtKeyUsage {
		if u == x509.ExtKeyUsageOCSPSigning {
			return true
		}
	}
	return false
}

func sameCertID(a, b ocspCertID) bool {
	return a.HashAlgorithm.Algorithm.Equal(b.HashAlgorithm.Algorithm) &&
		bytes.Equal(a.IssuerNameHash, b.IssuerNameHash) &&
		bytes.Equal(a.IssuerKeyHash, b.IssuerKeyHash) &&
		a.SerialNumber.Cmp(b.SerialNumber) == 0
}

// signatureAlgorithm maps the response's AlgorithmIdentifier onto the
// x509 enum CheckSignature takes.
func signatureAlgorithm(oid asn1.ObjectIdentifier) (x509.SignatureAlgorithm, error) {
	switch {
	case oid.Equal(oidSHA256RSA):
		return x509.SHA256WithRSA, nil
	case oid.Equal(oidSHA384RSA):
		return x509.SHA384WithRSA, nil
	case oid.Equal(oidSHA512RSA):
		return x509.SHA512WithRSA, nil
	case oid.Equal(oidSHA1RSA):
		return x509.SHA1WithRSA, nil
	case oid.Equal(oidECDSAWithSHA2):
		return x509.ECDSAWithSHA256, nil
	case oid.Equal(oidECDSAWithSHA3):
		return x509.ECDSAWithSHA384, nil
	case oid.Equal(oidECDSAWithSHA5):
		return x509.ECDSAWithSHA512, nil
	}
	return x509.UnknownSignatureAlgorithm, fmt.Errorf("%w: unsupported signature algorithm %v", errOCSP, oid)
}

// ---- fetching and stapling ----

// ocspClient posts requests to responders. Plain HTTP by RFC 6960
// §A.1; the response carries its own signature.
var ocspClient = &stdhttp.Client{Timeout: 10 * time.Second}

// fetchOCSP asks the leaf's first AIA responder for its status.
func fetchOCSP(ctx context.Context, leaf, issuer *x509.Certificate, now time.Time) (ocspStaple, error) {
	if len(leaf.OCSPServer) == 0 {
		return ocspStaple{}, fmt.Errorf("%w: certificate names no responder (no AIA OCSP URL)", errOCSP)
	}
	body, err := encodeOCSPRequest(leaf, issuer)
	if err != nil {
		return ocspStaple{}, fmt.Errorf("%w: %v", errOCSP, err)
	}
	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodPost, leaf.OCSPServer[0], bytes.NewReader(body))
	if err != nil {
		return ocspStaple{}, fmt.Errorf("%w: %v", errOCSP, err)
	}
	req.Header.Set("Content-Type", "application/ocsp-request")
	req.Header.Set("Accept", "application/ocsp-response")
	resp, err := ocspClient.Do(req)
	if err != nil {
		return ocspStaple{}, fmt.Errorf("%w: %v", errOCSP, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != stdhttp.StatusOK {
		return ocspStaple{}, fmt.Errorf("%w: responder answered HTTP %d", errOCSP, resp.StatusCode)
	}
	der, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return ocspStaple{}, fmt.Errorf("%w: %v", errOCSP, err)
	}
	return verifyOCSPResponse(der, leaf, issuer, now)
}

// issuerOf finds the certificate that issued leaf: the next one in the
// served chain, else one of the trust anchors or intermediates the
// manager holds.
func (m *Manager) issuerOf(pair *tls.Certificate, leaf *x509.Certificate) *x509.Certificate {
	if len(pair.Certificate) > 1 {
		if c, err := x509.ParseCertificate(pair.Certificate[1]); err == nil && leaf.CheckSignatureFrom(c) == nil {
			return c
		}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, c := range append(append([]*x509.Certificate(nil), m.inters...), m.roots...) {
		if leaf.CheckSignatureFrom(c) == nil {
			return c
		}
	}
	return nil
}

// ocspRefreshFloor keeps a failing responder from being hammered, and
// ocspRefreshCeiling keeps a long-lived response fresh in any case.
const (
	ocspRefreshFloor   = 5 * time.Minute
	ocspRefreshCeiling = 12 * time.Hour
)

// refreshStaples fetches a staple for every served pair whose leaf
// names a responder, installs the verified ones, and returns when the
// next refresh is due: halfway to the earliest nextUpdate, bounded.
func (m *Manager) refreshStaples(ctx context.Context) time.Duration {
	now := time.Now()
	next := ocspRefreshCeiling
	m.mu.RLock()
	pairs := m.servedPairsLocked()
	m.mu.RUnlock()
	for _, p := range pairs {
		leaf, err := x509.ParseCertificate(p.Certificate[0])
		if err != nil || len(leaf.OCSPServer) == 0 {
			continue
		}
		issuer := m.issuerOf(p, leaf)
		if issuer == nil {
			m.log.Warn("certmgr: ocsp: issuer of the served certificate is not known; nothing stapled",
				"cn", leaf.Subject.CommonName)
			next = min(next, ocspRefreshFloor)
			continue
		}
		st, err := fetchOCSP(ctx, leaf, issuer, now)
		if err != nil {
			m.log.Warn("certmgr: ocsp: responder refused or unreachable; keeping the previous staple",
				"cn", leaf.Subject.CommonName, "responder", leaf.OCSPServer[0], "err", err)
			next = min(next, ocspRefreshFloor)
			continue
		}
		m.setStaple(leaf, st.der)
		m.log.Info("certmgr: ocsp: response stapled",
			"cn", leaf.Subject.CommonName, "responder", leaf.OCSPServer[0],
			"this_update", st.thisUpdate, "next_update", st.nextUpdate)
		if !st.nextUpdate.IsZero() {
			next = min(next, max(st.nextUpdate.Sub(now)/2, ocspRefreshFloor))
		}
	}
	return next
}

// setStaple installs the DER on every served pair with that leaf.
func (m *Manager) setStaple(leaf *x509.Certificate, der []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.manualPairs {
		if bytes.Equal(m.manualPairs[i].Certificate[0], leaf.Raw) {
			m.manualPairs[i].OCSPStaple = der
		}
	}
	if m.current != nil && bytes.Equal(m.current.Certificate[0], leaf.Raw) {
		c := *m.current
		c.OCSPStaple = der
		m.current = &c
	}
}

// servedPairsLocked is every certificate the TLS stack may hand out:
// the manual pairs, or the EST-provisioned one.
func (m *Manager) servedPairsLocked() []*tls.Certificate {
	out := make([]*tls.Certificate, 0, len(m.manualPairs)+1)
	for i := range m.manualPairs {
		out = append(out, &m.manualPairs[i])
	}
	if len(out) == 0 && m.current != nil {
		out = append(out, m.current)
	}
	return out
}

// RunStapler keeps OCSP staples fresh for the life of the server. It
// returns at once when no served certificate names a responder.
func (m *Manager) RunStapler(ctx context.Context) {
	if !m.anyResponder() {
		return
	}
	for {
		wait := m.refreshStaples(ctx)
		select {
		case <-ctx.Done():
			return
		case <-after(wait):
		}
	}
}

// anyResponder reports whether a served certificate names an OCSP
// responder — the only case RunStapler has work to do.
func (m *Manager) anyResponder() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.servedPairsLocked() {
		if leaf, err := x509.ParseCertificate(p.Certificate[0]); err == nil && len(leaf.OCSPServer) > 0 {
			return true
		}
	}
	return false
}

// selectCertificate is the tls.Config hook when several pairs are
// served: the first the client can use (RSA or ECDSA by its signature
// algorithms), read live so a refreshed staple reaches the next
// handshake.
func (m *Manager) selectCertificate(chi *tls.ClientHelloInfo) (*tls.Certificate, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for i := range m.manualPairs {
		if chi.SupportsCertificate(&m.manualPairs[i]) == nil {
			c := m.manualPairs[i]
			return &c, nil
		}
	}
	if len(m.manualPairs) > 0 {
		c := m.manualPairs[0]
		return &c, nil
	}
	return nil, fmt.Errorf("certmgr: no certificate provisioned yet")
}
