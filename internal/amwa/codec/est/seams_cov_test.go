package est

import (
	"crypto/rand"
	"crypto/x509"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// readerFunc adapts a function to io.Reader.
type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

// starveAfter hands out real entropy for n reads and then refuses
// forever, so a test can starve one stage of NewCSR while letting the
// stages before it succeed.
func starveAfter(t *testing.T, n int) {
	t.Helper()
	prev := randReader
	left := n
	randReader = readerFunc(func(p []byte) (int, error) {
		if left <= 0 {
			return 0, errors.New("no entropy")
		}
		left--
		return rand.Reader.Read(p)
	})
	t.Cleanup(func() { randReader = prev })
}

// An enrolment must not proceed on a key the platform could not
// generate. Both algorithms are checked because each is its own call
// into crypto, and a seam that only proved one would say nothing
// about the other.
func TestNewCSRReportsAKeyItCannotGenerate(t *testing.T) {
	for _, alg := range []KeyAlgorithm{KeyRSA2048, KeyECDSAP256} {
		starveAfter(t, 0)
		_, _, err := NewCSR(CSROptions{CommonName: "node.local", Algorithm: alg})
		if err == nil || !strings.Contains(err.Error(), "generate key") {
			t.Errorf("algorithm %d = %v, want the key generation failure reported", alg, err)
		}
	}
}

// The CSR is signed from the same source. A signature that could not
// be produced is a refusal, never an unsigned request on the wire.
//
// How many reads the key generation itself takes is a crypto
// implementation detail, so the test walks the budget up until the
// failure lands on the signing stage instead of pinning a number that
// a Go release would quietly change.
func TestNewCSRReportsACSRItCannotSign(t *testing.T) {
	for budget := 1; budget < 64; budget++ {
		func() {
			starveAfter(t, budget)
			_, _, err := NewCSR(CSROptions{CommonName: "node.local", Algorithm: KeyECDSAP256})
			if err != nil && strings.Contains(err.Error(), "create CSR") {
				budget = 1 << 30 // found it
			}
		}()
		if budget == 1<<30 {
			return
		}
	}
	t.Fatal("no entropy budget made the signing stage the one that fails")
}

var _ io.Reader = readerFunc(nil)

// A DER encoder that refuses is reported at whichever of the two
// stages refuses, rather than shipping a truncated PKCS#7 that a peer
// would have to guess at.
func TestEncodeCertsOnlyPKCS7ReportsAMarshalRefusal(t *testing.T) {
	now := time.Now()
	leaf, _, _ := issueCert(t, "node.local", now, now.Add(time.Hour))

	for _, tc := range []struct {
		name string
		let  int // marshal calls allowed through before the refusal
		want string
	}{
		{"SignedData", 0, "marshal SignedData"},
		{"ContentInfo", 1, "marshal ContentInfo"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prev := asn1Marshal
			left := tc.let
			asn1Marshal = func(v any) ([]byte, error) {
				if left == 0 {
					return nil, errors.New("refused")
				}
				left--
				return prev(v)
			}
			t.Cleanup(func() { asn1Marshal = prev })

			if _, err := EncodeCertsOnlyPKCS7([]*x509.Certificate{leaf}); err == nil ||
				!strings.Contains(err.Error(), tc.want) {
				t.Fatalf("= %v, want %s reported", err, tc.want)
			}
		})
	}
}
