package provider

// The BCP-003-03 certificate-provisioning path: a Node that enrols its
// own certificate from an EST server before it serves anything, and a
// Node whose enrolment is refused and must not serve at all.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dhs/internal/amwa/codec/est"
)

// filepathJoin and osStat keep the import list of this fixture file
// small; both are the stdlib calls they name.
var (
	filepathJoin = filepath.Join
	osStat       = os.Stat
)

// estServer is a fake EST CA: it publishes its own root at /cacerts
// and signs whatever CSR arrives at /simpleenroll, serving TLS under a
// certificate its own CA issued so the enrolment leg — which verifies
// against the bootstrapped roots — trusts it.
func estServer(t *testing.T) string {
	t.Helper()
	return estServerRefusing(t, false)
}

// estServerRefusing is estServer, optionally refusing every enrolment
// so the client-side failure can be driven.
func estServerRefusing(t *testing.T, refuseEnroll bool) string {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "dhs test EST CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}

	mux := stdhttp.NewServeMux()
	mux.HandleFunc("/.well-known/est/cacerts", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		body, err := est.EncodeCertsResponse([]*x509.Certificate{ca})
		if err != nil {
			stdhttp.Error(w, err.Error(), stdhttp.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", est.ContentTypePKCS7)
		_, _ = w.Write(body)
	})
	enroll := func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if refuseEnroll {
			stdhttp.Error(w, "no certificates for you", stdhttp.StatusInternalServerError)
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			stdhttp.Error(w, err.Error(), stdhttp.StatusBadRequest)
			return
		}
		der, err := est.DecodeBase64Robust(raw)
		if err != nil {
			stdhttp.Error(w, err.Error(), stdhttp.StatusBadRequest)
			return
		}
		csr, err := x509.ParseCertificateRequest(der)
		if err != nil {
			stdhttp.Error(w, err.Error(), stdhttp.StatusBadRequest)
			return
		}
		leaf := &x509.Certificate{
			SerialNumber: big.NewInt(time.Now().UnixNano()),
			Subject:      csr.Subject,
			DNSNames:     csr.DNSNames,
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(24 * time.Hour),
			KeyUsage:     x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}
		leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, csr.PublicKey, caKey)
		if err != nil {
			stdhttp.Error(w, err.Error(), stdhttp.StatusInternalServerError)
			return
		}
		issued, err := x509.ParseCertificate(leafDER)
		if err != nil {
			stdhttp.Error(w, err.Error(), stdhttp.StatusInternalServerError)
			return
		}
		body, err := est.EncodeCertsResponse([]*x509.Certificate{issued})
		if err != nil {
			stdhttp.Error(w, err.Error(), stdhttp.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", est.ContentTypePKCS7)
		_, _ = w.Write(body)
	}
	mux.HandleFunc("/.well-known/est/simpleenroll", enroll)
	mux.HandleFunc("/.well-known/est/simplereenroll", enroll)

	// The server presents a certificate signed by the CA it publishes,
	// so the enrolment leg — which verifies against the bootstrapped
	// roots, unlike the bootstrap leg — trusts it.
	srvKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	srvTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "est.local"},
		DNSNames:     []string{"est.local", "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	srvDER, err := x509.CreateCertificate(rand.Reader, srvTmpl, ca, &srvKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewUnstartedServer(mux)
	ts.TLS = &tls.Config{ //nolint:gosec // the floor is set by the code under test, not here
		Certificates: []tls.Certificate{{
			Certificate: [][]byte{srvDER, caDER},
			PrivateKey:  srvKey,
		}},
	}
	ts.StartTLS()
	t.Cleanup(ts.Close)
	return strings.TrimPrefix(ts.URL, "https://")
}

// BCP-003-03 end to end: a Node pointed at an EST server bootstraps
// the CA, enrols its own certificate, serves HTTPS under it, and says
// so in its own tags — certprov is how a controller learns the Node
// can be re-provisioned rather than replaced when the certificate
// expires.
func TestNodeEnrolsViaESTAndAdvertisesCertprov(t *testing.T) {
	host := estServer(t)

	// A Node that carries no tags at all still gets the certprov one:
	// the tag says the certificate can be re-provisioned rather than
	// the Node replaced, and a bundle that declared no tags is exactly
	// the case where nobody put it there by hand.
	b := validBundle()
	b.Node.Tags = nil

	n := startNodeWith(t, b, func(c *IS04NodeConfig) {
		c.ESTHost = host
		c.TLSDataDir = filepathJoin(t.TempDir(), "tls")
		c.AdvertiseHost = "127.0.0.1"
	})

	n.s.mu.Lock()
	tags := n.s.bundle.Node.Tags["urn:x-nmos:tag:certprov"]
	secure := n.s.secure
	n.s.mu.Unlock()

	if len(tags) == 0 || tags[0] != "v1.0" {
		t.Errorf("certprov tag = %v, want [v1.0]", tags)
	}
	if !secure {
		t.Error("an enrolled Node serves TLS")
	}
}

// An EST server that bootstraps but refuses to issue leaves the Node
// with no certificate. Serving anyway would mean serving plaintext
// under an https advertisement, which BCP-003-01 forbids outright.
func TestNodeRefusesToServeWhenEnrolmentIsRefused(t *testing.T) {
	host := estServerRefusing(t, true)

	err := serveRefusal(t, func(c *IS04NodeConfig) {
		c.ESTHost = host
		c.TLSDataDir = filepathJoin(t.TempDir(), "tls")
		c.AdvertiseHost = "127.0.0.1"
	})
	if err == nil || !strings.Contains(err.Error(), "EST enrollment") {
		t.Fatalf("= %v, want the enrolment refusal reported", err)
	}
}

// With no --tls-data-dir the Node keeps its provisioned material in a
// cache under the working directory, so a restart re-uses the
// certificate it already holds instead of enrolling again.
func TestNodeDefaultsItsTLSDataDir(t *testing.T) {
	// The cache lands under the working directory, so the test runs
	// from one of its own. Tests in this package are sequential, and
	// the directory is put back either way.
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(prev); err != nil {
			t.Fatal(err)
		}
	})
	host := estServer(t)

	// A Node that carries no tags at all still gets the certprov one:
	// the tag says the certificate can be re-provisioned rather than
	// the Node replaced, and a bundle that declared no tags is exactly
	// the case where nobody put it there by hand.
	b := validBundle()
	b.Node.Tags = nil

	n := startNodeWith(t, b, func(c *IS04NodeConfig) {
		c.ESTHost = host
		c.AdvertiseHost = "127.0.0.1"
	})
	if n.s.certs == nil {
		t.Fatal("the Node must hold its certificate manager")
	}
	if _, err := osStat(".cache/nmos-tls"); err != nil {
		t.Errorf("the default cache was not used: %v", err)
	}
}
