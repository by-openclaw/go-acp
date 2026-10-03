package certmgr

// OCSP stapling against an in-process responder: the staple reaches a
// real handshake, a response is refused for every reason RFC 6960
// gives, and the refresh loop follows nextUpdate.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"io"
	"log/slog"
	"math/big"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- a responder ----

// responder answers OCSP requests for leaves the CA issued, in whatever
// shape a test asks for.
type responder struct {
	t      *testing.T
	ca     *x509.Certificate
	caKey  *ecdsa.PrivateKey
	srv    *httptest.Server
	mu     sync.Mutex
	status int // 0 good, 1 revoked, 2 unknown
	// signer overrides the CA as the signing identity (a delegated
	// responder) when set.
	signer    *x509.Certificate
	signerKey *ecdsa.PrivateKey
	// attachSigner carries the signer certificate in the response.
	attachSigner bool
	nextUpdate   time.Duration // 0 = none
	thisUpdate   time.Duration // offset from now
	respStatus   int           // OCSPResponseStatus
	respType     asn1.ObjectIdentifier
	sigOID       asn1.ObjectIdentifier
	raw          []byte // served verbatim when set
	httpStatus   int
	shortBody    bool
	asked        int
}

func newResponder(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey) *responder {
	t.Helper()
	r := &responder{t: t, ca: ca, caKey: caKey, nextUpdate: time.Hour, respType: oidOCSPBasic, sigOID: oidECDSAWithSHA2, httpStatus: 200}
	r.srv = httptest.NewServer(stdhttp.HandlerFunc(r.serve))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *responder) serve(w stdhttp.ResponseWriter, req *stdhttp.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asked++
	if r.httpStatus != 200 {
		w.WriteHeader(r.httpStatus)
		return
	}
	if r.shortBody {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("short"))
		return
	}
	if r.raw != nil {
		_, _ = w.Write(r.raw)
		return
	}
	body, _ := io.ReadAll(req.Body)
	var in ocspRequest
	if _, err := asn1.Unmarshal(body, &in); err != nil {
		r.t.Errorf("responder: request does not parse: %v", err)
		w.WriteHeader(400)
		return
	}
	_, _ = w.Write(r.answer(in.TBSRequest.RequestList[0].CertID))
}

// answer builds a BasicOCSPResponse for id.
func (r *responder) answer(id ocspCertID) []byte {
	now := time.Now().UTC()
	status := asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: r.status}
	if r.status == 1 {
		rev, _ := asn1.Marshal(struct {
			RevocationTime time.Time `asn1:"generalized"`
		}{now})
		var seq asn1.RawValue
		_, _ = asn1.Unmarshal(rev, &seq)
		status.IsCompound = true
		status.Bytes = seq.Bytes
	}
	signer, key := r.ca, r.caKey
	if r.signer != nil {
		signer, key = r.signer, r.signerKey
	}
	kh, _ := issuerKeyHash(signer)
	single := struct {
		CertID     ocspCertID
		CertStatus asn1.RawValue
		ThisUpdate time.Time `asn1:"generalized"`
		NextUpdate time.Time `asn1:"generalized,explicit,tag:0,optional"`
	}{CertID: id, CertStatus: status, ThisUpdate: now.Add(r.thisUpdate)}
	if r.nextUpdate != 0 {
		single.NextUpdate = now.Add(r.nextUpdate)
	}
	data, err := asn1.Marshal(struct {
		ResponderID asn1.RawValue
		ProducedAt  time.Time `asn1:"generalized"`
		Responses   []any
	}{
		ResponderID: asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 2, IsCompound: true, Bytes: mustMarshal(kh)},
		ProducedAt:  now,
		Responses:   []any{single},
	})
	if err != nil {
		r.t.Fatalf("responder: marshal ResponseData: %v", err)
	}
	sum := sha256.Sum256(data)
	sig, err := ecdsa.SignASN1(rand.Reader, key, sum[:])
	if err != nil {
		r.t.Fatalf("responder: sign: %v", err)
	}
	basic := struct {
		TBSResponseData    asn1.RawValue
		SignatureAlgorithm pkix.AlgorithmIdentifier
		Signature          asn1.BitString
		Certificates       []asn1.RawValue `asn1:"explicit,tag:0,optional"`
	}{
		TBSResponseData:    asn1.RawValue{FullBytes: data},
		SignatureAlgorithm: pkix.AlgorithmIdentifier{Algorithm: r.sigOID},
		Signature:          asn1.BitString{Bytes: sig, BitLength: len(sig) * 8},
	}
	if r.attachSigner && r.signer != nil {
		basic.Certificates = []asn1.RawValue{{FullBytes: r.signer.Raw}}
	}
	basicDER, err := asn1.Marshal(basic)
	if err != nil {
		r.t.Fatalf("responder: marshal basic: %v", err)
	}
	out, err := asn1.Marshal(ocspResponse{
		Status:        asn1.Enumerated(r.respStatus),
		ResponseBytes: ocspResponseBytes{ResponseType: r.respType, Response: basicDER},
	})
	if err != nil {
		r.t.Fatalf("responder: marshal response: %v", err)
	}
	return out
}

func mustMarshal(v any) []byte {
	b, err := asn1.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// issueLeaf signs a server leaf for host, naming the responder in its
// AIA, with an ECDSA or RSA key.
func issueLeaf(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, host, ocspURL string, useRSA bool) (tls.Certificate, *x509.Certificate) {
	t.Helper()
	var pub any
	var priv any
	if useRSA {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		pub, priv = &k.PublicKey, k
	} else {
		k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		pub, priv = &k.PublicKey, k
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ocspURL != "" {
		tmpl.OCSPServer = []string{ocspURL}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, pub, caKey)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	return tls.Certificate{Certificate: [][]byte{der, ca.Raw}, PrivateKey: priv, Leaf: leaf}, leaf
}

// managerWith installs pairs on a fresh manager.
func managerWith(t *testing.T, pairs ...tls.Certificate) *Manager {
	t.Helper()
	m, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pairs {
		m.manualPairs = append(m.manualPairs, p)
		if m.current == nil {
			c := p
			m.current, m.leaf = &c, p.Leaf
		}
	}
	return m
}

// ---- the handshake ----

// The staple the responder issued is what a client receives in the
// handshake — on an ECDSA pair and on an RSA one, each selected by the
// client's own offer.
func TestOCSPStapleReachesTheHandshake(t *testing.T) {
	ca, caKey := newCA(t, "dhs OCSP test CA")
	r := newResponder(t, ca, caKey)
	ec, ecLeaf := issueLeaf(t, ca, caKey, "node.test", r.srv.URL, false)
	rs, rsLeaf := issueLeaf(t, ca, caKey, "node.test", r.srv.URL, true)
	m := managerWith(t, rs, ec)

	wait := m.refreshStaples(context.Background())
	if wait < ocspRefreshFloor || wait > time.Hour/2 {
		t.Fatalf("refresh in %s, want half of the hour-long validity (floored)", wait)
	}
	for _, p := range m.manualPairs {
		if len(p.OCSPStaple) == 0 {
			t.Fatal("every served pair must carry a staple")
		}
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", m.TLSServerConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_ = c.(*tls.Conn).Handshake()
				_ = c.Close()
			}()
		}
	}()
	roots := x509.NewCertPool()
	roots.AddCert(ca)

	for _, tc := range []struct {
		name  string
		suite uint16
		leaf  *x509.Certificate
	}{
		{"ecdsa", tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256, ecLeaf},
		{"rsa", tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256, rsLeaf},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{
				RootCAs: roots, ServerName: "node.test",
				MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12,
				CipherSuites: []uint16{tc.suite},
			})
			if err != nil {
				t.Fatalf("handshake: %v", err)
			}
			defer func() { _ = conn.Close() }()
			st := conn.ConnectionState()
			if !st.PeerCertificates[0].Equal(tc.leaf) {
				t.Fatalf("the server picked %s, want the %s leaf", st.PeerCertificates[0].Subject, tc.name)
			}
			if len(st.OCSPResponse) == 0 {
				t.Fatal("no OCSP staple in the handshake")
			}
			if _, err := verifyOCSPResponse(st.OCSPResponse, tc.leaf, ca, time.Now()); err != nil {
				t.Fatalf("the stapled response does not verify for this leaf: %v", err)
			}
		})
	}
}

// With one pair the config keeps GetCertificate on the live pair, and
// a staple set later reaches it.
func TestSinglePairStapleIsLive(t *testing.T) {
	ca, caKey := newCA(t, "dhs OCSP test CA")
	r := newResponder(t, ca, caKey)
	ec, leaf := issueLeaf(t, ca, caKey, "node.test", r.srv.URL, false)
	m := managerWith(t, ec)
	cfg := m.TLSServerConfig()
	c, err := cfg.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil || len(c.OCSPStaple) != 0 {
		t.Fatalf("before the refresh: %v, staple %d bytes", err, len(c.OCSPStaple))
	}
	m.refreshStaples(context.Background())
	c, _ = cfg.GetCertificate(&tls.ClientHelloInfo{})
	if _, err := verifyOCSPResponse(c.OCSPStaple, leaf, ca, time.Now()); err != nil {
		t.Fatalf("after the refresh: %v", err)
	}
	// EST mode: no manual pairs, the current pair carries the staple.
	m2 := managerWith(t)
	cp := ec
	m2.current, m2.leaf = &cp, leaf
	m2.refreshStaples(context.Background())
	if len(m2.Certificate().OCSPStaple) == 0 {
		t.Fatal("the EST-provisioned pair must carry the staple")
	}
}

// ---- refusals ----

func TestResponsesThatMustNotBeStapled(t *testing.T) {
	ca, caKey := newCA(t, "dhs OCSP test CA")
	otherCA, otherKey := newCA(t, "someone else")
	r := newResponder(t, ca, caKey)
	ec, leaf := issueLeaf(t, ca, caKey, "node.test", r.srv.URL, false)
	_ = ec
	now := time.Now()

	// A delegated responder certificate, signed by the CA, with and
	// without the OCSP-signing EKU.
	mk := func(eku []x509.ExtKeyUsage, notAfter time.Time) (*x509.Certificate, *ecdsa.PrivateKey) {
		k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(77), Subject: pkix.Name{CommonName: "responder"},
			NotBefore: now.Add(-time.Hour), NotAfter: notAfter,
			KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: eku,
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &k.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		c, _ := x509.ParseCertificate(der)
		return c, k
	}
	okSigner, okKey := mk([]x509.ExtKeyUsage{x509.ExtKeyUsageOCSPSigning}, now.Add(time.Hour))
	noEKU, noEKUKey := mk(nil, now.Add(time.Hour))
	expired, expiredKey := mk([]x509.ExtKeyUsage{x509.ExtKeyUsageOCSPSigning}, now.Add(-time.Minute))
	strangerKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	strangerDER, _ := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: big.NewInt(9), Subject: pkix.Name{CommonName: "stranger"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageOCSPSigning},
	}, otherCA, &strangerKey.PublicKey, otherKey)
	stranger, _ := x509.ParseCertificate(strangerDER)

	id, _ := certIDFor(leaf, ca)
	cases := []struct {
		name string
		set  func()
		der  func() []byte
		want string
	}{
		{"garbage", nil, func() []byte { return []byte{0x30, 0x01} }, "not OCSPResponse DER"},
		{"responder error status", func() { r.respStatus = 1 }, nil, "responder status 1"},
		{"not a basic response", func() { r.respType = oidSHA1 }, nil, "not id-pkix-ocsp-basic"},
		{"basic does not parse", nil, func() []byte {
			return mustMarshal(ocspResponse{ResponseBytes: ocspResponseBytes{ResponseType: oidOCSPBasic, Response: []byte{0x30, 0x01}}})
		}, "not BasicOCSPResponse DER"},
		{"response data does not parse", nil, func() []byte {
			basic := mustMarshal(struct {
				TBS asn1.RawValue
				Alg pkix.AlgorithmIdentifier
				Sig asn1.BitString
			}{asn1.RawValue{FullBytes: mustMarshal(5)}, pkix.AlgorithmIdentifier{Algorithm: oidECDSAWithSHA2}, asn1.BitString{Bytes: []byte{1}, BitLength: 8}})
			return mustMarshal(ocspResponse{ResponseBytes: ocspResponseBytes{ResponseType: oidOCSPBasic, Response: basic}})
		}, "ResponseData does not parse"},
		{"unsupported signature algorithm", func() { r.sigOID = oidSHA1 }, nil, "unsupported signature algorithm"},
		{"signed by a stranger", func() { r.signer, r.signerKey, r.attachSigner = stranger, strangerKey, true }, nil, "not signed by the issuer"},
		{"signed by nobody known", func() { r.signer, r.signerKey, r.attachSigner = stranger, strangerKey, false }, nil, "neither the issuer nor a delegated responder"},
		{"delegated without the EKU", func() { r.signer, r.signerKey, r.attachSigner = noEKU, noEKUKey, true }, nil, "lacks the OCSP signing"},
		{"delegated and expired", func() { r.signer, r.signerKey, r.attachSigner = expired, expiredKey, true }, nil, "not valid at"},
		{"delegated and fine", func() { r.signer, r.signerKey, r.attachSigner = okSigner, okKey, true }, nil, ""},
		{"revoked", func() { r.status = 1 }, nil, "not good (tag 1)"},
		{"unknown", func() { r.status = 2 }, nil, "not good (tag 2)"},
		{"from the future", func() { r.thisUpdate = time.Hour }, nil, "in the future"},
		{"expired", func() { r.nextUpdate = -time.Minute }, nil, "expired at"},
		{"no nextUpdate", func() { r.nextUpdate = 0 }, nil, ""},
		{"about another certificate", nil, func() []byte {
			other := id
			other.SerialNumber = big.NewInt(424242)
			return r.answer(other)
		}, "no status for this certificate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r.mu.Lock()
			r.status, r.signer, r.signerKey, r.attachSigner = 0, nil, nil, false
			r.nextUpdate, r.thisUpdate, r.respStatus = time.Hour, 0, 0
			r.respType, r.sigOID = oidOCSPBasic, oidECDSAWithSHA2
			if tc.set != nil {
				tc.set()
			}
			var der []byte
			if tc.der != nil {
				der = tc.der()
			} else {
				der = r.answer(id)
			}
			r.mu.Unlock()
			_, err := verifyOCSPResponse(der, leaf, ca, now)
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("refused: %v", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("err = %v, want %q", err, tc.want)
			case err != nil && !errors.Is(err, errOCSP):
				t.Fatalf("not an errOCSP: %v", err)
			}
		})
	}
}

// A delegated-responder list may carry certificates that are not the
// signer, or not certificates at all; they are passed over.
func TestDelegatedSignatureSkipsWhatIsNotTheSigner(t *testing.T) {
	ca, caKey := newCA(t, "dhs OCSP test CA")
	_, leaf := issueLeaf(t, ca, caKey, "node.test", "", false)
	err := delegatedSignature([]asn1.RawValue{{FullBytes: []byte{0x30}}, {FullBytes: leaf.Raw}}, ca,
		x509.ECDSAWithSHA256, []byte("signed"), []byte("sig"), time.Now())
	if err == nil || !strings.Contains(err.Error(), "neither") {
		t.Fatalf("err = %v", err)
	}
}

func TestSignatureAlgorithmTable(t *testing.T) {
	for oid, want := range map[string]x509.SignatureAlgorithm{
		"1.2.840.113549.1.1.11": x509.SHA256WithRSA, "1.2.840.113549.1.1.12": x509.SHA384WithRSA,
		"1.2.840.113549.1.1.13": x509.SHA512WithRSA, "1.2.840.113549.1.1.5": x509.SHA1WithRSA,
		"1.2.840.10045.4.3.2": x509.ECDSAWithSHA256, "1.2.840.10045.4.3.3": x509.ECDSAWithSHA384,
		"1.2.840.10045.4.3.4": x509.ECDSAWithSHA512,
	} {
		var o asn1.ObjectIdentifier
		for _, p := range strings.Split(oid, ".") {
			var n int
			for _, ch := range p {
				n = n*10 + int(ch-'0')
			}
			o = append(o, n)
		}
		got, err := signatureAlgorithm(o)
		if err != nil || got != want {
			t.Errorf("%s = %v, %v", oid, got, err)
		}
	}
}

// certIDFor needs a parseable issuer public key.
func TestCertIDNeedsAParseableIssuerKey(t *testing.T) {
	ca, caKey := newCA(t, "dhs OCSP test CA")
	_, leaf := issueLeaf(t, ca, caKey, "node.test", "", false)
	bad := &x509.Certificate{RawSubjectPublicKeyInfo: []byte{0xff}}
	if _, err := encodeOCSPRequest(leaf, bad); err == nil {
		t.Fatal("an unparseable issuer key must refuse")
	}
	if _, err := verifyOCSPResponse(nil, leaf, bad, time.Now()); err == nil {
		t.Fatal("nil DER must refuse")
	}
	// An issuer that verifies the signature but whose key bytes do not
	// parse: the CertID cannot be built, so nothing matches.
	r := newResponder(t, ca, caKey)
	id, _ := certIDFor(leaf, ca)
	odd := *ca
	odd.RawSubjectPublicKeyInfo = []byte{0xff}
	if _, err := verifyOCSPResponse(r.answer(id), leaf, &odd, time.Now()); err == nil {
		t.Fatal("an issuer with unparseable key bytes must refuse")
	}
}

// ---- fetching ----

func TestFetchRefusals(t *testing.T) {
	ca, caKey := newCA(t, "dhs OCSP test CA")
	r := newResponder(t, ca, caKey)
	bad := &x509.Certificate{RawSubjectPublicKeyInfo: []byte{0xff}}
	_, withResponder := issueLeaf(t, ca, caKey, "node.test", r.srv.URL, false)
	_, without := issueLeaf(t, ca, caKey, "node.test", "", false)
	_, badURL := issueLeaf(t, ca, caKey, "node.test", "http://[::1]:bad", false)
	closed := httptest.NewServer(stdhttp.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	_, unreachable := issueLeaf(t, ca, caKey, "node.test", closedURL, false)
	now := time.Now()
	ctx := context.Background()

	if _, err := fetchOCSP(ctx, without, ca, now); err == nil || !strings.Contains(err.Error(), "no AIA") {
		t.Fatalf("no responder: %v", err)
	}
	if _, err := fetchOCSP(ctx, withResponder, bad, now); err == nil {
		t.Fatal("an unparseable issuer must refuse before any request")
	}
	if _, err := fetchOCSP(ctx, badURL, ca, now); err == nil {
		t.Fatal("an invalid responder URL must refuse")
	}
	if _, err := fetchOCSP(ctx, unreachable, ca, now); err == nil {
		t.Fatal("an unreachable responder must refuse")
	}
	r.httpStatus = 503
	if _, err := fetchOCSP(ctx, withResponder, ca, now); err == nil || !strings.Contains(err.Error(), "HTTP 503") {
		t.Fatalf("503: %v", err)
	}
	r.httpStatus, r.shortBody = 200, true
	if _, err := fetchOCSP(ctx, withResponder, ca, now); err == nil {
		t.Fatal("a body cut short must refuse")
	}
	r.shortBody = false
	if _, err := fetchOCSP(ctx, withResponder, ca, now); err != nil {
		t.Fatalf("a good responder: %v", err)
	}
}

// ---- the refresh loop ----

func TestRefreshSkipsAndWarnsWhereItCannotStaple(t *testing.T) {
	ca, caKey := newCA(t, "dhs OCSP test CA")
	r := newResponder(t, ca, caKey)
	withResponder, _ := issueLeaf(t, ca, caKey, "node.test", r.srv.URL, false)
	without, _ := issueLeaf(t, ca, caKey, "node.test", "", false)
	// A leaf whose issuer the manager does not hold: served without
	// its chain and with no roots installed.
	orphanCA, orphanKey := newCA(t, "unknown CA")
	orphanResponder := newResponder(t, orphanCA, orphanKey)
	orphan, _ := issueLeaf(t, orphanCA, orphanKey, "node.test", orphanResponder.srv.URL, false)
	orphan.Certificate = orphan.Certificate[:1]

	m := managerWith(t, without, orphan, tls.Certificate{Certificate: [][]byte{{0x00}}})
	if wait := m.refreshStaples(context.Background()); wait != ocspRefreshFloor {
		t.Fatalf("an unknown issuer retries at the floor, got %s", wait)
	}
	// The issuer known through the manager's trust store instead of
	// the served chain.
	orphan2 := orphan
	m2 := managerWith(t, orphan2)
	m2.setRoots([]*x509.Certificate{orphanCA}, nil)
	m2.refreshStaples(context.Background())
	if len(m2.manualPairs[0].OCSPStaple) == 0 {
		t.Fatal("an issuer found among the roots must yield a staple")
	}
	// A responder that fails keeps the previous staple and retries at
	// the floor.
	m3 := managerWith(t, withResponder)
	m3.refreshStaples(context.Background())
	first := m3.manualPairs[0].OCSPStaple
	r.httpStatus = 500
	if wait := m3.refreshStaples(context.Background()); wait != ocspRefreshFloor {
		t.Fatalf("a failing responder retries at the floor, got %s", wait)
	}
	if string(m3.manualPairs[0].OCSPStaple) != string(first) {
		t.Fatal("a failed refresh must keep the previous staple")
	}
	// No nextUpdate: the ceiling.
	r.httpStatus, r.nextUpdate = 200, 0
	if wait := m3.refreshStaples(context.Background()); wait != ocspRefreshCeiling {
		t.Fatalf("no nextUpdate refreshes at the ceiling, got %s", wait)
	}
}

func TestRunStaplerFollowsTheResponder(t *testing.T) {
	ca, caKey := newCA(t, "dhs OCSP test CA")
	r := newResponder(t, ca, caKey)
	pair, _ := issueLeaf(t, ca, caKey, "node.test", r.srv.URL, false)
	m := managerWith(t, pair)

	// The wait seam: hand back a channel the test fires.
	tick := make(chan time.Time)
	old := after
	after = func(time.Duration) <-chan time.Time { return tick }
	t.Cleanup(func() { after = old })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); m.RunStapler(ctx) }()
	asked := func() int { r.mu.Lock(); defer r.mu.Unlock(); return r.asked }
	deadline := time.Now().Add(5 * time.Second)
	for asked() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	tick <- time.Now()
	for asked() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if asked() < 2 {
		t.Fatal("the stapler must ask again after its wait")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the stapler must stop with its context")
	}

	// Nothing to staple: returns at once.
	plain, _ := issueLeaf(t, ca, caKey, "node.test", "", false)
	m2 := managerWith(t, plain)
	finished := make(chan struct{})
	go func() { defer close(finished); m2.RunStapler(context.Background()) }()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("with no responder named the stapler must return")
	}
	m3 := managerWith(t, tls.Certificate{Certificate: [][]byte{{0x00}}})
	if m3.anyResponder() {
		t.Fatal("an unparseable pair names no responder")
	}
}

// selectCertificate: the first pair the client can use, the first pair
// when it can use none, an error with no pairs at all.
func TestSelectCertificate(t *testing.T) {
	ca, caKey := newCA(t, "dhs OCSP test CA")
	ec, ecLeaf := issueLeaf(t, ca, caKey, "node.test", "", false)
	rs, rsLeaf := issueLeaf(t, ca, caKey, "node.test", "", true)
	m := managerWith(t, ec, rs)
	rsaOnly := &tls.ClientHelloInfo{
		SupportedVersions: []uint16{tls.VersionTLS12},
		CipherSuites:      []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
		SupportedCurves:   []tls.CurveID{tls.CurveP256},
		SupportedPoints:   []uint8{0},
		SignatureSchemes:  []tls.SignatureScheme{tls.PKCS1WithSHA256},
	}
	if c, err := m.selectCertificate(rsaOnly); err != nil || !c.Leaf.Equal(rsLeaf) {
		t.Fatalf("an RSA-only client gets the RSA pair: %v", err)
	}
	none := &tls.ClientHelloInfo{SupportedVersions: []uint16{tls.VersionTLS12}, CipherSuites: []uint16{0x0000}}
	if c, err := m.selectCertificate(none); err != nil || !c.Leaf.Equal(ecLeaf) {
		t.Fatalf("a client that can use nothing gets the first pair, the stack then refuses: %v", err)
	}
	if _, err := managerWith(t).selectCertificate(none); err == nil {
		t.Fatal("no pairs must be an error")
	}
}

