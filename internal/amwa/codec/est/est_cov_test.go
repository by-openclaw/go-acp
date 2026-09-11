package est

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"math/big"
	"strings"
	"testing"
	"time"
)

// issueCert mints a self-signed CA and one certificate under it, so
// the client-side checks can be exercised against a real chain.
func issueCert(t *testing.T, cn string, notBefore, notAfter time.Time) (leaf, ca *x509.Certificate, pool *x509.CertPool) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "dhs test CA"},
		NotBefore:             notBefore.Add(-time.Hour),
		NotAfter:              notAfter.Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err = x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     []string{cn},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err = x509.ParseCertificate(leafDER)
	if err != nil {
		t.Fatal(err)
	}
	pool = x509.NewCertPool()
	pool.AddCert(ca)
	return leaf, ca, pool
}

// The EST base URL is composed from what discovery found; the
// api_selector, when the server published one, is a path segment.
func TestBaseURL(t *testing.T) {
	for name, tc := range map[string]struct {
		hostPort, selector, want string
	}{
		"no selector": {"est.local:443", "", "https://est.local:443/.well-known/est"},
		"a selector":  {"est.local:443", "arq", "https://est.local:443/.well-known/est/arq"},
		"a selector the server over-slashed": {
			"est.local:443", "/arq/", "https://est.local:443/.well-known/est/arq",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := BaseURL(tc.hostPort, tc.selector); got != tc.want {
				t.Errorf("BaseURL = %q, want %q", got, tc.want)
			}
		})
	}
}

// Real EST servers wrap their base64 in line breaks and sometimes
// drop the padding; the decoder accepts both and still refuses what
// is not base64 at all.
func TestDecodeBase64Robust(t *testing.T) {
	// A length that is NOT a multiple of 3, so the canonical form
	// carries padding and the trimmed form only decodes on the raw
	// (unpadded) alphabet — which is the fallback under test.
	payload := []byte("the certificate bytes!")
	padded := base64.StdEncoding.EncodeToString(payload)

	for name, raw := range map[string]string{
		"canonical":            padded,
		"wrapped in line ends": padded[:4] + "\r\n" + padded[4:] + "\n",
		"spaced out":           padded[:4] + " " + padded[4:],
		"tabbed":               "\t" + padded,
		"unpadded":             strings.TrimRight(padded, "="),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := DecodeBase64Robust([]byte(raw))
			if err != nil {
				t.Fatalf("= %v", err)
			}
			if string(got) != string(payload) {
				t.Errorf("decoded %q, want %q", got, payload)
			}
		})
	}

	if _, err := DecodeBase64Robust([]byte("not base64 !!")); err == nil ||
		!strings.Contains(err.Error(), "base64 decode") {
		t.Errorf("a body that is not base64 = %v", err)
	}
}

// A certs-only PKCS#7 round-trips, and every malformed shape is
// reported for what it is rather than yielding a partial chain.
func TestCertsOnlyPKCS7RoundTripAndRefusals(t *testing.T) {
	leaf, ca, _ := issueCert(t, "device.local", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))

	der, err := EncodeCertsOnlyPKCS7([]*x509.Certificate{leaf, ca})
	if err != nil {
		t.Fatalf("EncodeCertsOnlyPKCS7: %v", err)
	}
	certs, err := ParseCertsOnlyPKCS7(der)
	if err != nil {
		t.Fatalf("ParseCertsOnlyPKCS7: %v", err)
	}
	if len(certs) != 2 || certs[0].Subject.CommonName != "device.local" {
		t.Errorf("round-tripped %d certs: %+v", len(certs), certs)
	}

	if _, err := EncodeCertsOnlyPKCS7(nil); err == nil ||
		!strings.Contains(err.Error(), "at least one certificate") {
		t.Errorf("an empty chain = %v", err)
	}

	for name, tc := range map[string]struct {
		der  []byte
		want string
	}{
		"bytes that are not ASN.1": {[]byte{0x00, 0x01}, "ContentInfo"},
		"trailing bytes past the ContentInfo": {
			append(append([]byte{}, der...), 0x00), "trailing bytes",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseCertsOnlyPKCS7(tc.der); err == nil ||
				!strings.Contains(err.Error(), tc.want) {
				t.Errorf("= %v, want an error mentioning %q", err, tc.want)
			}
		})
	}

	// A ContentInfo that is not signedData.
	notSigned, err := asn1.Marshal(contentInfo{ContentType: oidData})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseCertsOnlyPKCS7(notSigned); err == nil ||
		!strings.Contains(err.Error(), "not signedData") {
		t.Errorf("a ContentInfo that is not signedData = %v", err)
	}

	// signedData present but carrying no certificates.
	emptySet := asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSet, IsCompound: true}
	sdDER, err := asn1.Marshal(signedData{
		Version: 1, DigestAlgorithms: emptySet,
		ContentInfo: contentInfo{ContentType: oidData}, SignerInfos: emptySet,
	})
	if err != nil {
		t.Fatal(err)
	}
	empty, err := asn1.Marshal(contentInfo{
		ContentType: oidSignedData,
		Content: asn1.RawValue{
			Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: sdDER,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseCertsOnlyPKCS7(empty); err == nil ||
		!strings.Contains(err.Error(), "no certificates") {
		t.Errorf("a SignedData with no certificates = %v", err)
	}

	// A [0] wrapper holding something that is not a certificate.
	junkDER, err := asn1.Marshal(signedData{
		Version: 1, DigestAlgorithms: emptySet,
		ContentInfo: contentInfo{ContentType: oidData},
		Certificates: asn1.RawValue{
			Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: []byte{0x30, 0x01, 0x00},
		},
		SignerInfos: emptySet,
	})
	if err != nil {
		t.Fatal(err)
	}
	junk, err := asn1.Marshal(contentInfo{
		ContentType: oidSignedData,
		Content: asn1.RawValue{
			Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: junkDER,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseCertsOnlyPKCS7(junk); err == nil ||
		!strings.Contains(err.Error(), "certificates") {
		t.Errorf("a certificates wrapper holding junk = %v", err)
	}

	// A SignedData whose inner bytes are not a SignedData at all.
	badInner, err := asn1.Marshal(contentInfo{
		ContentType: oidSignedData,
		Content: asn1.RawValue{
			Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: []byte{0x05, 0x00},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseCertsOnlyPKCS7(badInner); err == nil ||
		!strings.Contains(err.Error(), "SignedData") {
		t.Errorf("an inner value that is not a SignedData = %v", err)
	}
}

// The response body is what an EST server actually sends: base64 with
// line breaks. It round-trips through the encoder, and a body the
// client cannot read is reported.
func TestCertsResponseRoundTrip(t *testing.T) {
	leaf, ca, _ := issueCert(t, "device.local", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	body, err := EncodeCertsResponse([]*x509.Certificate{leaf, ca})
	if err != nil {
		t.Fatalf("EncodeCertsResponse: %v", err)
	}
	if !strings.Contains(string(body), "\n") {
		t.Error("the encoded body must carry the line breaks real servers emit")
	}
	certs, err := ParseCertsResponse(body)
	if err != nil {
		t.Fatalf("ParseCertsResponse: %v", err)
	}
	if len(certs) != 2 {
		t.Errorf("parsed %d certs, want 2", len(certs))
	}

	if _, err := EncodeCertsResponse(nil); err == nil {
		t.Error("EncodeCertsResponse accepted an empty chain")
	}
	if _, err := ParseCertsResponse([]byte("not base64 !!")); err == nil {
		t.Error("ParseCertsResponse accepted a body that is not base64")
	}
	if _, err := ParseCertsResponse([]byte(base64.StdEncoding.EncodeToString([]byte("junk")))); err == nil {
		t.Error("ParseCertsResponse accepted base64 of something that is not PKCS#7")
	}
}

// A CSR carries a fresh key per request, names the device by DNS
// only, and refuses what BCP-003-01 forbids.
func TestNewCSR(t *testing.T) {
	for name, alg := range map[string]KeyAlgorithm{
		"RSA":   KeyRSA2048,
		"ECDSA": KeyECDSAP256,
	} {
		t.Run(name, func(t *testing.T) {
			der, key, err := NewCSR(CSROptions{
				CommonName: "device.local", SerialNumber: "SN-1", Algorithm: alg,
			})
			if err != nil {
				t.Fatalf("NewCSR: %v", err)
			}
			if key == nil {
				t.Fatal("a CSR must come with the key it was signed by")
			}
			csr, err := x509.ParseCertificateRequest(der)
			if err != nil {
				t.Fatalf("the CSR does not parse: %v", err)
			}
			if err := csr.CheckSignature(); err != nil {
				t.Errorf("the CSR is not self-signed: %v", err)
			}
			if csr.Subject.CommonName != "device.local" {
				t.Errorf("subject = %+v", csr.Subject)
			}
			// With no explicit SANs the common name becomes the one SAN.
			if len(csr.DNSNames) != 1 || csr.DNSNames[0] != "device.local" {
				t.Errorf("SANs = %v", csr.DNSNames)
			}
			if csr.SignatureAlgorithm == x509.SHA1WithRSA || csr.SignatureAlgorithm == x509.ECDSAWithSHA1 {
				t.Errorf("signature algorithm = %v, which the spec forbids", csr.SignatureAlgorithm)
			}
		})
	}

	// Explicit SANs are kept as given.
	der, _, err := NewCSR(CSROptions{
		CommonName: "device.local", DNSNames: []string{"a.local", "b.local"},
	})
	if err != nil {
		t.Fatal(err)
	}
	csr, err := x509.ParseCertificateRequest(der)
	if err != nil {
		t.Fatal(err)
	}
	if len(csr.DNSNames) != 2 {
		t.Errorf("SANs = %v, want both", csr.DNSNames)
	}

	for name, opts := range map[string]CSROptions{
		"no common name":              {},
		"a common name that is an IP": {CommonName: "10.6.239.113"},
		"a SAN that is an IP": {
			CommonName: "device.local", DNSNames: []string{"10.6.239.113"},
		},
		"an algorithm we do not implement": {CommonName: "device.local", Algorithm: KeyAlgorithm(9)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := NewCSR(opts); err == nil {
				t.Error("was accepted")
			}
		})
	}

	// The enroll body is the DER, base64'd — no line breaks, the
	// endpoints take it as one token.
	body := EncodeCSRBody(der)
	back, err := base64.StdEncoding.DecodeString(string(body))
	if err != nil || len(back) != len(der) {
		t.Errorf("EncodeCSRBody = %d bytes, %v", len(body), err)
	}
}

// Renewal is a fraction of the certificate's own lifetime, so a
// short-lived certificate renews sooner in wall-clock terms — and a
// certificate with no lifetime at all is due immediately.
func TestRenewalDue(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cert := &x509.Certificate{NotBefore: start, NotAfter: start.Add(100 * time.Hour)}

	if RenewalDue(cert, start.Add(50*time.Hour), RenewalRecommendedFraction) {
		t.Error("half-way through is before the recommended point")
	}
	if !RenewalDue(cert, start.Add(80*time.Hour), RenewalRecommendedFraction) {
		t.Error("the recommended point must be due")
	}
	if !RenewalDue(cert, start.Add(50*time.Hour), 0.5) {
		t.Error("the spec's earliest point must be due at half life")
	}
	inverted := &x509.Certificate{NotBefore: start, NotAfter: start}
	if !RenewalDue(inverted, start, RenewalRecommendedFraction) {
		t.Error("a certificate with no lifetime is due immediately")
	}
}

// Before a returned certificate is used it must be in its window,
// cover the identity we asked for, and chain to a provisioned root.
func TestValidateIssued(t *testing.T) {
	now := time.Now()
	leaf, ca, roots := issueCert(t, "device.local", now.Add(-time.Hour), now.Add(time.Hour))

	if err := ValidateIssued(leaf, "device.local", roots, []*x509.Certificate{ca}, now); err != nil {
		t.Fatalf("a valid certificate = %v", err)
	}

	if err := ValidateIssued(leaf, "device.local", roots, nil, now.Add(-2*time.Hour)); err == nil ||
		!strings.Contains(err.Error(), "not valid before") {
		t.Errorf("a certificate not yet valid = %v", err)
	}
	if err := ValidateIssued(leaf, "device.local", roots, nil, now.Add(2*time.Hour)); err == nil ||
		!strings.Contains(err.Error(), "expired") {
		t.Errorf("an expired certificate = %v", err)
	}
	if err := ValidateIssued(leaf, "other.local", roots, nil, now); err == nil ||
		!strings.Contains(err.Error(), "does not cover") {
		t.Errorf("a certificate for another name = %v", err)
	}
	if err := ValidateIssued(leaf, "device.local", x509.NewCertPool(), nil, now); err == nil ||
		!strings.Contains(err.Error(), "does not chain") {
		t.Errorf("a certificate from an unknown CA = %v", err)
	}
}
