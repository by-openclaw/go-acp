package est_test

// Tests for the BCP-003-03 / RFC 7030 payload codec. Expected shapes
// come from the specs (certs-only PKCS#7, PKCS#10, the base64
// robustness clause, the renewal fractions), not from working code.

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"

	"dhs/internal/amwa/codec/est"
)

// makeCA builds a self-signed test CA + a leaf it signs.
func makeCA(t *testing.T) (caCert *x509.Certificate, leaf *x509.Certificate) {
	t.Helper()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "dhs Test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create CA: %v", err)
	}
	caCert, _ = x509.ParseCertificate(caDER)

	leafKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "node.test.local"},
		DNSNames:     []string{"node.test.local"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(10 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create leaf: %v", err)
	}
	leaf, _ = x509.ParseCertificate(leafDER)
	return caCert, leaf
}

func TestPKCS7RoundTrip(t *testing.T) {
	ca, leaf := makeCA(t)
	der, err := est.EncodeCertsOnlyPKCS7([]*x509.Certificate{leaf, ca})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	certs, err := est.ParseCertsOnlyPKCS7(der)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(certs) != 2 || !certs[0].Equal(leaf) || !certs[1].Equal(ca) {
		t.Errorf("round trip lost certificates: got %d", len(certs))
	}
	if _, err := est.ParseCertsOnlyPKCS7([]byte{0x30, 0x03, 0x02, 0x01, 0x01}); err == nil {
		t.Error("garbage DER must be rejected")
	}
}

func TestCertsResponseBase64Robustness(t *testing.T) {
	ca, leaf := makeCA(t)
	body, err := est.EncodeCertsResponse([]*x509.Certificate{leaf, ca})
	if err != nil {
		t.Fatalf("encode response: %v", err)
	}
	if !bytes.Contains(body, []byte("\n")) {
		t.Fatal("test premise: encoded response should carry line breaks")
	}
	// LF-wrapped (as encoded), CRLF, extra spaces, stripped padding —
	// all must decode (RFC 4648 §3 clause in the spec).
	variants := [][]byte{
		body,
		bytes.ReplaceAll(body, []byte("\n"), []byte("\r\n")),
		bytes.ReplaceAll(body, []byte("\n"), []byte(" \n ")),
		bytes.TrimRight(bytes.ReplaceAll(body, []byte("="), nil), "\n"),
	}
	for i, v := range variants {
		certs, err := est.ParseCertsResponse(v)
		if err != nil {
			t.Errorf("variant %d: %v", i, err)
			continue
		}
		if len(certs) != 2 {
			t.Errorf("variant %d: %d certs", i, len(certs))
		}
	}
}

func TestNewCSR(t *testing.T) {
	for _, alg := range []est.KeyAlgorithm{est.KeyRSA2048, est.KeyECDSAP256} {
		der, key, err := est.NewCSR(est.CSROptions{
			CommonName: "node.test.local", DNSNames: []string{"node.test.local", "node"},
			SerialNumber: "SN-1234", Algorithm: alg,
		})
		if err != nil {
			t.Fatalf("alg %d: %v", alg, err)
		}
		if key == nil {
			t.Fatalf("alg %d: nil key", alg)
		}
		req, err := x509.ParseCertificateRequest(der)
		if err != nil {
			t.Fatalf("alg %d parse: %v", alg, err)
		}
		if err := req.CheckSignature(); err != nil {
			t.Errorf("alg %d signature: %v", alg, err)
		}
		if req.Subject.CommonName != "node.test.local" || len(req.DNSNames) != 2 {
			t.Errorf("alg %d subject/SANs: %+v %v", alg, req.Subject, req.DNSNames)
		}
		switch req.SignatureAlgorithm {
		case x509.MD5WithRSA, x509.SHA1WithRSA, x509.ECDSAWithSHA1:
			t.Errorf("alg %d: forbidden signature algorithm %v", alg, req.SignatureAlgorithm)
		}
	}
	// IP SANs are refused outright.
	if _, _, err := est.NewCSR(est.CSROptions{CommonName: "10.6.250.101"}); err == nil {
		t.Error("IP CommonName must be rejected")
	}
	if _, _, err := est.NewCSR(est.CSROptions{CommonName: "n.local", DNSNames: []string{"10.0.0.1"}}); err == nil {
		t.Error("IP SAN must be rejected")
	}
	// Fresh key pair per CSR.
	_, k1, _ := est.NewCSR(est.CSROptions{CommonName: "n.local", Algorithm: est.KeyRSA2048})
	_, k2, _ := est.NewCSR(est.CSROptions{CommonName: "n.local", Algorithm: est.KeyRSA2048})
	if k1.(*rsa.PrivateKey).N.Cmp(k2.(*rsa.PrivateKey).N) == 0 {
		t.Error("CSR generation must mint a fresh key pair every time")
	}
}

func TestRenewalDue(t *testing.T) {
	_, leaf := makeCA(t) // NotBefore -1h, NotAfter +10h → life 11h
	if est.RenewalDue(leaf, leaf.NotBefore.Add(time.Hour), est.RenewalRecommendedFraction) {
		t.Error("1h into an 11h life is not 80%")
	}
	if !est.RenewalDue(leaf, leaf.NotBefore.Add(9*time.Hour), est.RenewalRecommendedFraction) {
		t.Error("9h into an 11h life is past 80%")
	}
	// Life is 11h → the 50% floor sits at 5.5h.
	if est.RenewalDue(leaf, leaf.NotBefore.Add(5*time.Hour), 0.5) {
		t.Error("5h into an 11h life is before the 50% floor")
	}
	if !est.RenewalDue(leaf, leaf.NotBefore.Add(6*time.Hour), 0.5) {
		t.Error("6h into an 11h life is past the 50% floor")
	}
}

func TestBaseURLAndValidate(t *testing.T) {
	if got := est.BaseURL("ca.example.com:8443", ""); got != "https://ca.example.com:8443/.well-known/est" {
		t.Errorf("BaseURL = %s", got)
	}
	if got := est.BaseURL("ca.example.com:8443", "/arb/"); got != "https://ca.example.com:8443/.well-known/est/arb" {
		t.Errorf("BaseURL selector = %s", got)
	}
	ca, leaf := makeCA(t)
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	if err := est.ValidateIssued(leaf, "node.test.local", roots, nil, time.Now()); err != nil {
		t.Errorf("valid issued cert rejected: %v", err)
	}
	if err := est.ValidateIssued(leaf, "other.host", roots, nil, time.Now()); err == nil {
		t.Error("hostname mismatch must be rejected")
	}
	if err := est.ValidateIssued(leaf, "node.test.local", x509.NewCertPool(), nil, time.Now()); err == nil {
		t.Error("unchained cert must be rejected")
	}
	if err := est.ValidateIssued(leaf, "node.test.local", roots, nil, leaf.NotBefore.Add(-time.Minute)); err == nil {
		t.Error("notBefore in the future must be rejected")
	}
}
