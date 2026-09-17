package usm

// RFC 3414 Appendix A.3 publishes the exact key material "maplesyrup"
// derives to, for both protocols, against a named engine ID. Those
// vectors are the whole point of testing this: a key derivation that is
// self-consistent but wrong produces a plant where every device rejects
// every message, with nothing to say why.

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatalf("bad hex in the test itself: %v", err)
	}
	return b
}

// The engine ID RFC 3414 A.3 localises against.
func rfcEngine(t *testing.T) []byte {
	return unhex(t, "00 00 00 00 00 00 00 00 00 00 00 02")
}

// RFC 3414 §A.3.1 and §A.3.2: the intermediate key from the password,
// and the key localised to an engine.
func TestRFC3414KeyVectors(t *testing.T) {
	for _, tc := range []struct {
		proto   AuthProtocol
		ku, kul string
	}{
		{HMACMD5,
			"9f af 32 83 88 4e 92 83 4e bc 98 47 d8 ed d9 63",
			"52 6f 5e ed 9f cc e2 6f 89 64 c2 93 07 87 d8 2b"},
		{HMACSHA,
			"9f b5 cc 03 81 49 7b 37 93 52 89 39 ff 78 8d 5d 79 14 52 11",
			"66 95 fe bc 92 88 e3 62 82 23 5f c7 15 1f 12 84 97 b3 8f 3f"},
	} {
		t.Run(tc.proto.String(), func(t *testing.T) {
			spec := authSpecs[tc.proto]

			ku := passwordToKey(spec.new, "maplesyrup")
			if want := unhex(t, tc.ku); !bytes.Equal(ku, want) {
				t.Fatalf("Ku  = % x\nwant  % x", ku, want)
			}
			kul := localiseKey(spec.new, ku, rfcEngine(t))
			if want := unhex(t, tc.kul); !bytes.Equal(kul, want) {
				t.Fatalf("Kul = % x\nwant  % x", kul, want)
			}
		})
	}
}

// DeriveKeys is the path a caller actually uses, and it has to land on
// the same bytes as the vectors above.
func TestDeriveKeysMatchesTheVectors(t *testing.T) {
	k, err := DeriveKeys(User{
		Name: "authOnlyUser", Auth: HMACSHA, AuthPass: "maplesyrup",
	}, rfcEngine(t))
	if err != nil {
		t.Fatal(err)
	}
	want := unhex(t, "66 95 fe bc 92 88 e3 62 82 23 5f c7 15 1f 12 84 97 b3 8f 3f")
	if !bytes.Equal(k.Auth, want) {
		t.Errorf("auth key = % x", k.Auth)
	}
	if k.Priv != nil {
		t.Errorf("a user with no privacy has no privacy key: % x", k.Priv)
	}
}

// A key is localised to ONE engine: that is what stops a key lifted from
// one device from authenticating anything on the next.
func TestAKeyIsLocalisedToItsEngine(t *testing.T) {
	u := User{Name: "u", Auth: HMACSHA256, AuthPass: "maplesyrup",
		Priv: AES128CFB, PrivPass: "maplesyrup"}

	a, err := DeriveKeys(u, []byte("engine-A"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := DeriveKeys(u, []byte("engine-B"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a.Auth, b.Auth) {
		t.Error("one password produced one key for two engines")
	}
	if bytes.Equal(a.Priv, b.Priv) {
		t.Error("one privacy password produced one key for two engines")
	}
	if len(a.Priv) != 16 {
		t.Errorf("AES key of %d bytes, want 16", len(a.Priv))
	}
}

// An empty password still derives a key rather than panicking on a
// zero-length rotation.
func TestAnEmptyPasswordStillDerives(t *testing.T) {
	if got := passwordToKey(authSpecs[HMACMD5].new, ""); len(got) != 16 {
		t.Errorf("%d bytes, want a digest", len(got))
	}
}

// A user that cannot be used is refused at configuration time rather
// than at the first exchange, where the failure is a timeout.
func TestUserValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		u    User
		want string
	}{
		{"no name", User{}, "needs a name"},
		{"a name longer than the field", User{Name: strings.Repeat("x", 33)}, "at most 32"},
		{"auth with no password", User{Name: "u", Auth: HMACSHA}, "needs an authentication password"},
		{"privacy without authentication",
			User{Name: "u", Priv: AES128CFB, PrivPass: "x"}, "not a security level"},
		{"privacy with no password",
			User{Name: "u", Auth: HMACSHA, AuthPass: "x", Priv: AES128CFB}, "needs a privacy password"},
		{"an authentication protocol that does not exist",
			User{Name: "u", Auth: AuthProtocol(99), AuthPass: "x"}, "unknown authentication protocol"},
		{"a privacy protocol that does not exist",
			User{Name: "u", Auth: HMACSHA, AuthPass: "x", Priv: PrivProtocol(99), PrivPass: "x"},
			"unknown privacy protocol"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.u.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("= %v, want %q", err, tc.want)
			}
		})
	}

	for _, u := range []User{
		{Name: "u"},
		{Name: "u", Auth: HMACMD5, AuthPass: "p"},
		{Name: "u", Auth: HMACSHA512, AuthPass: "p", Priv: DESCBC, PrivPass: "p"},
	} {
		if err := u.Validate(); err != nil {
			t.Errorf("%s: %v", u.SecurityLevel(), err)
		}
	}
}

func TestSecurityLevelNaming(t *testing.T) {
	for _, tc := range []struct {
		u    User
		want string
	}{
		{User{Name: "u"}, "noAuthNoPriv"},
		{User{Name: "u", Auth: HMACSHA}, "authNoPriv"},
		{User{Name: "u", Auth: HMACSHA, Priv: AES128CFB}, "authPriv"},
	} {
		if got := tc.u.SecurityLevel(); got != tc.want {
			t.Errorf("= %q, want %q", got, tc.want)
		}
	}
}

// Deriving without an engine ID would produce a key that works
// everywhere, which is the one thing localisation exists to prevent.
func TestDeriveRefusals(t *testing.T) {
	if _, err := DeriveKeys(User{Name: "u", Auth: HMACSHA, AuthPass: "p"}, nil); err == nil ||
		!strings.Contains(err.Error(), "without an engine ID") {
		t.Errorf("= %v, want the refusal", err)
	}
	if _, err := DeriveKeys(User{}, []byte("e")); err == nil {
		t.Error("an invalid user must be refused")
	}
	// A noAuthNoPriv user derives nothing, and that is not a failure.
	k, err := DeriveKeys(User{Name: "u"}, []byte("engine"))
	if err != nil || k.Auth != nil || k.Priv != nil {
		t.Errorf("= %v / %v, %v; want no keys and no error", k.Auth, k.Priv, err)
	}
}

// ---------------------------------------------------------------------
// digests
// ---------------------------------------------------------------------

// Each protocol puts a DIFFERENT number of bytes on the wire, and it is
// not the hash size: RFC 3414 sends 12 bytes of MD5's 16, RFC 7860 sends
// 24 of SHA-256's 32. A digest of the wrong length is rejected by a peer
// with no clue why.
func TestDigestLengths(t *testing.T) {
	for _, tc := range []struct {
		proto AuthProtocol
		want  int
	}{
		{HMACMD5, 12}, {HMACSHA, 12}, {HMACSHA224, 16},
		{HMACSHA256, 24}, {HMACSHA384, 32}, {HMACSHA512, 48},
		{NoAuth, 0}, {AuthProtocol(99), 0},
	} {
		if got := tc.proto.DigestLen(); got != tc.want {
			t.Errorf("%s = %d, want %d", tc.proto, got, tc.want)
		}
	}
}

// A digest is written into the message it covers, and verifies against
// exactly the bytes that went out.
func TestAuthenticateThenVerify(t *testing.T) {
	for proto := range authSpecs {
		t.Run(proto.String(), func(t *testing.T) {
			key := []byte("0123456789abcdef0123456789abcdef")
			n := proto.DigestLen()
			msg := make([]byte, 64)
			copy(msg, "a message with a hole in it for the digest")
			offset := 8
			for i := offset; i < offset+n; i++ {
				msg[i] = 0
			}

			if err := Authenticate(proto, key, msg, offset); err != nil {
				t.Fatalf("Authenticate: %v", err)
			}
			if bytes.Count(msg[offset:offset+n], []byte{0}) == n {
				t.Fatal("the digest field is still empty")
			}
			if err := Verify(proto, key, msg, offset); err != nil {
				t.Fatalf("Verify: %v", err)
			}

			// A single changed byte anywhere breaks it.
			msg[0] ^= 0xFF
			if err := Verify(proto, key, msg, offset); !errors.Is(err, ErrAuth) {
				t.Errorf("a tampered message verified: %v", err)
			}
			msg[0] ^= 0xFF

			// So does the wrong key.
			if err := Verify(proto, []byte("a different key entirely"), msg, offset); !errors.Is(err, ErrAuth) {
				t.Errorf("the wrong key verified: %v", err)
			}
		})
	}
}

// Verify must not modify what it was handed: a caller that failed
// verification may still want to log the datagram it got.
func TestVerifyLeavesTheMessageAlone(t *testing.T) {
	key := []byte("k")
	msg := make([]byte, 32)
	if err := Authenticate(HMACSHA256, key, msg, 4); err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), msg...)
	if err := Verify(HMACSHA256, key, msg, 4); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, msg) {
		t.Error("Verify modified the message")
	}
}

func TestDigestRefusals(t *testing.T) {
	msg := make([]byte, 32)

	if _, err := Digest(NoAuth, nil, msg); err == nil {
		t.Error("there is no digest without an authentication protocol")
	}
	if err := Authenticate(NoAuth, nil, msg, 0); err == nil {
		t.Error("nothing to authenticate with")
	}
	if err := Verify(NoAuth, nil, msg, 0); !errors.Is(err, ErrAuth) {
		t.Errorf("= %v, want an ErrAuth", err)
	}

	// A field that does not fit in the message.
	if err := Authenticate(HMACSHA256, nil, msg, 30); err == nil ||
		!strings.Contains(err.Error(), "outside") {
		t.Errorf("= %v, want the bounds refusal", err)
	}
	if err := Authenticate(HMACSHA256, nil, msg, -1); err == nil {
		t.Error("a negative offset must be refused")
	}
	if err := Verify(HMACSHA256, nil, msg, 30); !errors.Is(err, ErrAuth) {
		t.Errorf("= %v, want an ErrAuth", err)
	}

	// A field that was not zeroed would produce a digest the receiver
	// cannot reproduce, so it is refused rather than sent.
	dirty := make([]byte, 32)
	dirty[5] = 1
	if err := Authenticate(HMACSHA256, nil, dirty, 4); err == nil ||
		!strings.Contains(err.Error(), "zeroed") {
		t.Errorf("= %v, want the refusal", err)
	}
}

// ---------------------------------------------------------------------
// privacy
// ---------------------------------------------------------------------

// Both ciphers round-trip, and the ciphertext is not the plaintext.
func TestPrivacyRoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef")
	plain := []byte("a scoped PDU, or near enough for a cipher")

	for _, p := range []PrivProtocol{DESCBC, AES128CFB} {
		t.Run(p.String(), func(t *testing.T) {
			ct, params, err := Encrypt(p, key, 7, 1234, 42, plain)
			if err != nil {
				t.Fatalf("Encrypt: %v", err)
			}
			if bytes.Contains(ct, plain) {
				t.Fatal("the plaintext is in the ciphertext")
			}
			if len(params) != 8 {
				t.Fatalf("privacy parameters of %d bytes, want 8", len(params))
			}

			back, err := Decrypt(p, key, 7, 1234, params, ct)
			if err != nil {
				t.Fatalf("Decrypt: %v", err)
			}
			// DES pads to a block boundary, so the plaintext is a PREFIX
			// of what comes back — which is why DecodeScopedPDU tolerates
			// trailing bytes.
			if !bytes.HasPrefix(back, plain) {
				t.Errorf("round trip = %q", back)
			}
		})
	}
}

// The salt makes every message's keystream different, which is the whole
// reason it exists: two identical plaintexts must not encrypt alike.
func TestTheSaltChangesTheCiphertext(t *testing.T) {
	key := []byte("0123456789abcdef")
	plain := bytes.Repeat([]byte("x"), 32)

	for _, p := range []PrivProtocol{DESCBC, AES128CFB} {
		a, _, err := Encrypt(p, key, 1, 1, 1, plain)
		if err != nil {
			t.Fatal(err)
		}
		b, _, err := Encrypt(p, key, 1, 1, 2, plain)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(a, b) {
			t.Errorf("%s: two salts produced one ciphertext", p)
		}
	}

	// And for AES the engine's boots and time are in the IV too, so a
	// message replayed after a restart does not decrypt.
	a, params, err := Encrypt(AES128CFB, key, 1, 1, 1, plain)
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := Decrypt(AES128CFB, key, 2, 1, params, a)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(wrong, plain) {
		t.Error("a different boot count decrypted the same")
	}
}

func TestPrivacyRefusals(t *testing.T) {
	good := []byte("0123456789abcdef")

	if _, _, err := Encrypt(NoPriv, good, 1, 1, 1, []byte("x")); err == nil {
		t.Error("there is no cipher without a privacy protocol")
	}
	if _, err := Decrypt(NoPriv, good, 1, 1, make([]byte, 8), []byte("x")); err == nil {
		t.Error("there is no cipher without a privacy protocol")
	}

	for _, p := range []PrivProtocol{DESCBC, AES128CFB} {
		if _, _, err := Encrypt(p, []byte("short"), 1, 1, 1, []byte("x")); err == nil ||
			!strings.Contains(err.Error(), "localised key") {
			t.Errorf("%s = %v, want the key-length refusal", p, err)
		}
		if _, err := Decrypt(p, []byte("short"), 1, 1, make([]byte, 8), make([]byte, 8)); err == nil {
			t.Errorf("%s: a short key must be refused", p)
		}
		if _, err := Decrypt(p, good, 1, 1, []byte("bad"), make([]byte, 8)); !errors.Is(err, ErrAuth) {
			t.Errorf("%s: a salt of the wrong length = %v", p, err)
		}
	}

	// CBC works in whole blocks, so a ciphertext that is not a multiple
	// of the block size did not come from this cipher.
	if _, err := Decrypt(DESCBC, good, 1, 1, make([]byte, 8), make([]byte, 7)); !errors.Is(err, ErrAuth) {
		t.Errorf("= %v, want the block-size refusal", err)
	}
	if _, err := Decrypt(DESCBC, good, 1, 1, make([]byte, 8), nil); !errors.Is(err, ErrAuth) {
		t.Errorf("= %v, want an empty ciphertext refused", err)
	}
}

func TestProtocolNaming(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{NoAuth.String(), "none"},
		{HMACMD5.String(), "HMAC-MD5-96"},
		{HMACSHA.String(), "HMAC-SHA-96"},
		{HMACSHA224.String(), "HMAC-SHA-224"},
		{HMACSHA256.String(), "HMAC-SHA-256"},
		{HMACSHA384.String(), "HMAC-SHA-384"},
		{HMACSHA512.String(), "HMAC-SHA-512"},
		{AuthProtocol(99).String(), "authProtocol(99)"},
		{NoPriv.String(), "none"},
		{DESCBC.String(), "CBC-DES"},
		{AES128CFB.String(), "CFB128-AES-128"},
		{PrivProtocol(99).String(), "privProtocol(99)"},
	} {
		if tc.got != tc.want {
			t.Errorf("= %q, want %q", tc.got, tc.want)
		}
	}
	if privKeyLen(NoPriv) != 0 {
		t.Error("no privacy protocol consumes no key")
	}
}

// Digest is the one-shot form, for a caller that has no spec in hand.
func TestDigestOneShot(t *testing.T) {
	sum, err := Digest(HMACSHA256, []byte("key"), []byte("message"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sum) != HMACSHA256.DigestLen() {
		t.Errorf("%d bytes, want %d", len(sum), HMACSHA256.DigestLen())
	}
	again, err := Digest(HMACSHA256, []byte("key"), []byte("message"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sum, again) {
		t.Error("a digest of the same bytes must be the same digest")
	}
}
