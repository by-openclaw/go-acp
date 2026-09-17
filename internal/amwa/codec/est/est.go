package est

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"time"
)

// ServiceCerts is the DNS-SD service type the EST Server MUST be
// advertised under (BCP-003-03 "DNS-SD Advertisement"). Unicast
// DNS-SD only — clients MUST NOT trust an mDNS advertisement unless
// explicitly configured to.
const ServiceCerts = "_nmos-certs._tcp"

// WellKnownESTPath is the RFC 5785 path prefix with the registered
// name "est".
const WellKnownESTPath = "/.well-known/est"

// Content types EST exchanges use (RFC 7030 §4).
const (
	ContentTypePKCS10 = "application/pkcs10"
	ContentTypePKCS7  = "application/pkcs7-mime"
)

// BaseURL composes the EST API base from a discovered/configured
// host:port and the optional api_selector TXT value ("" appends
// nothing, per the spec).
func BaseURL(hostPort, apiSelector string) string {
	u := "https://" + hostPort + WellKnownESTPath
	if apiSelector != "" {
		u += "/" + strings.Trim(apiSelector, "/")
	}
	return u
}

// DecodeBase64Robust decodes EST body base64 tolerating line feeds,
// carriage returns, spaces and missing padding — the spec requires
// accepting the RFC 4648 §3 looseness real servers emit.
func DecodeBase64Robust(raw []byte) ([]byte, error) {
	clean := strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', ' ', '\t':
			return -1
		}
		return r
	}, string(raw))
	if b, err := base64.StdEncoding.DecodeString(clean); err == nil {
		return b, nil
	}
	b, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(clean, "="))
	if err != nil {
		return nil, fmt.Errorf("est: base64 decode: %w", err)
	}
	return b, nil
}

// ParseCertsResponse decodes an EST response body (base64 of a DER
// certs-only PKCS#7 SignedData) into certificates. Used for both
// /cacerts and the enroll endpoints.
func ParseCertsResponse(body []byte) ([]*x509.Certificate, error) {
	der, err := DecodeBase64Robust(body)
	if err != nil {
		return nil, err
	}
	return ParseCertsOnlyPKCS7(der)
}

// EncodeCertsResponse is the inverse — DER certs-only PKCS#7,
// base64-encoded with line breaks (what real EST servers emit; our
// own tests then exercise the robust decode). Test/mock-server side.
func EncodeCertsResponse(certs []*x509.Certificate) ([]byte, error) {
	der, err := EncodeCertsOnlyPKCS7(certs)
	if err != nil {
		return nil, err
	}
	b64 := base64.StdEncoding.EncodeToString(der)
	var out strings.Builder
	for len(b64) > 64 {
		out.WriteString(b64[:64])
		out.WriteByte('\n')
		b64 = b64[64:]
	}
	out.WriteString(b64)
	out.WriteByte('\n')
	return []byte(out.String()), nil
}

// KeyAlgorithm selects the CSR key type. The spec has clients create
// a CSR per supported digital-signature algorithm; RSA support is
// mandatory on the certificate-consuming side.
type KeyAlgorithm int

const (
	KeyRSA2048 KeyAlgorithm = iota
	KeyECDSAP256
)

// CSROptions describes one certificate request.
type CSROptions struct {
	// CommonName MUST be DNS-resolvable on the current domain.
	CommonName string
	// DNSNames are the SANs. DNS names only — BCP-003-01 says
	// certificates SHOULD NOT use IP addresses, and the builder
	// enforces it (an IP here is an error, not a silent SAN).
	DNSNames []string
	// SerialNumber, when non-empty, populates the subject
	// serialNumber attribute with the device's unique serial.
	SerialNumber string
	Algorithm    KeyAlgorithm
}

// oidSerialNumber is the X.520 serialNumber attribute (RFC 5280
// §4.1.2.2 standard attribute set).
var oidSerialNumber = asn1.ObjectIdentifier{2, 5, 4, 5}

// NewCSR generates a FRESH key pair (mandated per CSR) and a PKCS#10
// request in DER. The signature uses SHA-256-family algorithms —
// MD5/SHA-1 are forbidden.
func NewCSR(opts CSROptions) (csrDER []byte, key crypto.Signer, err error) {
	if opts.CommonName == "" {
		return nil, nil, fmt.Errorf("est: CSR requires a CommonName")
	}
	for _, n := range append([]string{opts.CommonName}, opts.DNSNames...) {
		if net.ParseIP(n) != nil {
			return nil, nil, fmt.Errorf("est: %q is an IP address — certificates use DNS names only", n)
		}
	}
	switch opts.Algorithm {
	case KeyRSA2048:
		key, err = rsa.GenerateKey(rand.Reader, 2048)
	case KeyECDSAP256:
		key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	default:
		return nil, nil, fmt.Errorf("est: unknown key algorithm %d", opts.Algorithm)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("est: generate key: %w", err)
	}
	subject := pkix.Name{CommonName: opts.CommonName}
	if opts.SerialNumber != "" {
		subject.ExtraNames = append(subject.ExtraNames, pkix.AttributeTypeAndValue{
			Type: oidSerialNumber, Value: opts.SerialNumber,
		})
	}
	dns := opts.DNSNames
	if len(dns) == 0 {
		dns = []string{opts.CommonName}
	}
	tmpl := x509.CertificateRequest{
		Subject:  subject,
		DNSNames: dns,
	}
	// SignatureAlgorithm zero value lets x509 pick the SHA-256 family
	// for the key type (never SHA-1 with these key types).
	csrDER, err = x509.CreateCertificateRequest(rand.Reader, &tmpl, key)
	if err != nil {
		return nil, nil, fmt.Errorf("est: create CSR: %w", err)
	}
	return csrDER, key, nil
}

// EncodeCSRBody base64-wraps a DER CSR the way the enroll endpoints
// expect (application/pkcs10, base64 transfer encoding).
func EncodeCSRBody(csrDER []byte) []byte {
	return []byte(base64.StdEncoding.EncodeToString(csrDER))
}

// RenewalDue reports whether a certificate has crossed the given
// lifetime fraction. The spec: attempt renewal no sooner than 50%,
// RECOMMENDED after 80%.
func RenewalDue(cert *x509.Certificate, now time.Time, fraction float64) bool {
	life := cert.NotAfter.Sub(cert.NotBefore)
	if life <= 0 {
		return true
	}
	return now.Sub(cert.NotBefore) >= time.Duration(float64(life)*fraction)
}

// RenewalRecommendedFraction is the spec's RECOMMENDED renewal point.
const RenewalRecommendedFraction = 0.8

// ValidateIssued applies the client-side checks the spec lists before
// a returned certificate is used: time window (notBefore MUST be
// checked), name coverage for our identity, and chain of trust to the
// provisioned roots.
func ValidateIssued(cert *x509.Certificate, hostname string, roots *x509.CertPool, intermediates []*x509.Certificate, now time.Time) error {
	if now.Before(cert.NotBefore) {
		return fmt.Errorf("est: issued certificate not valid before %v", cert.NotBefore)
	}
	if now.After(cert.NotAfter) {
		return fmt.Errorf("est: issued certificate expired %v", cert.NotAfter)
	}
	if err := cert.VerifyHostname(hostname); err != nil {
		return fmt.Errorf("est: issued certificate does not cover %q: %w", hostname, err)
	}
	inter := x509.NewCertPool()
	for _, ic := range intermediates {
		inter.AddCert(ic)
	}
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots: roots, Intermediates: inter, CurrentTime: now,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		return fmt.Errorf("est: issued certificate does not chain to the provisioned CA: %w", err)
	}
	return nil
}
