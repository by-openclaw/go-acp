// Package usm is SNMPv3's User-based Security Model (RFC 3414): the
// keys, the digests and the ciphers that turn a v3 message envelope into
// an authenticated and optionally encrypted one.
//
// It is separate from internal/snmp/codec because the two jobs are
// different. The codec turns bytes into structure and holds nothing; USM
// holds key material, the authoritative engine's boots and time, and the
// discovery state that has to happen before a first authenticated
// exchange. Keeping them apart is also what lets the codec stay
// stdlib-only with no dhs/ imports, per ADR-0006.
//
// # On the algorithms
//
// MD5, SHA-1 and DES are here because they are what the protocol
// specifies and what devices in the field implement — the Snell frames
// and the Tandberg receivers among them. They are not a recommendation.
// A deployment that can choose should choose SHA-256 with AES-128, which
// this package also speaks; a deployment that cannot choose is why the
// weak ones are implemented rather than refused.
package usm

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/des" //nolint:gosec // RFC 3414's privacy protocol; see the package doc
	"crypto/hmac"
	"crypto/md5"  //nolint:gosec // RFC 3414's HMAC-MD5-96; see the package doc
	"crypto/sha1" //nolint:gosec // RFC 3414's HMAC-SHA-96; see the package doc
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
)

// ErrAuth is every failure to prove a message came from who it says.
// Callers match on it: a wrong digest, an unknown user and a stale
// timestamp are all one thing to a caller — this message is not to be
// acted on — and telling them apart to a peer is how an oracle is built.
var ErrAuth = errors.New("snmp/usm: authentication failed")

// AuthProtocol is how a message proves who sent it.
type AuthProtocol int

const (
	// NoAuth is RFC 3414's usmNoAuthProtocol.
	NoAuth AuthProtocol = iota
	// HMACMD5 is RFC 3414 §6, HMAC-MD5-96.
	HMACMD5
	// HMACSHA is RFC 3414 §7, HMAC-SHA-96.
	HMACSHA
	// HMACSHA224 and the three below are RFC 7860's SHA-2 family.
	HMACSHA224
	HMACSHA256
	HMACSHA384
	HMACSHA512
)

// authSpec is what each protocol needs: a hash, and how many bytes of
// the HMAC travel in the message. The truncation length is NOT the hash
// size — RFC 3414 sends 12 bytes of a 16-byte MD5 HMAC, and RFC 7860
// sends 24 of SHA-256's 32 — and getting it wrong produces a digest a
// peer rejects with no clue why.
type authSpec struct {
	name   string
	new    func() hash.Hash
	digest int // bytes of the HMAC placed in msgAuthenticationParameters
}

var authSpecs = map[AuthProtocol]authSpec{
	HMACMD5:    {"HMAC-MD5-96", md5.New, 12},
	HMACSHA:    {"HMAC-SHA-96", sha1.New, 12},
	HMACSHA224: {"HMAC-SHA-224", sha256.New224, 16},
	HMACSHA256: {"HMAC-SHA-256", sha256.New, 24},
	HMACSHA384: {"HMAC-SHA-384", sha512.New384, 32},
	HMACSHA512: {"HMAC-SHA-512", sha512.New, 48},
}

func (a AuthProtocol) String() string {
	if a == NoAuth {
		return "none"
	}
	if s, ok := authSpecs[a]; ok {
		return s.name
	}
	return fmt.Sprintf("authProtocol(%d)", int(a))
}

// DigestLen is how many bytes of digest this protocol puts on the wire.
// Zero for NoAuth.
func (a AuthProtocol) DigestLen() int {
	if s, ok := authSpecs[a]; ok {
		return s.digest
	}
	return 0
}

// PrivProtocol is how the scoped PDU is encrypted.
type PrivProtocol int

const (
	// NoPriv is RFC 3414's usmNoPrivProtocol.
	NoPriv PrivProtocol = iota
	// DESCBC is RFC 3414 §8, CBC-DES.
	DESCBC
	// AES128CFB is RFC 3826, AES-128 in CFB mode.
	AES128CFB
)

func (p PrivProtocol) String() string {
	switch p {
	case NoPriv:
		return "none"
	case DESCBC:
		return "CBC-DES"
	case AES128CFB:
		return "CFB128-AES-128"
	default:
		return fmt.Sprintf("privProtocol(%d)", int(p))
	}
}

// User is one USM identity.
//
// Passwords are kept rather than only their derived keys because a key
// is localised to ONE authoritative engine: a manager talking to four
// devices needs four different keys from the same password, and it does
// not learn a device's engine ID until it has spoken to it.
type User struct {
	Name     string
	Auth     AuthProtocol
	AuthPass string
	Priv     PrivProtocol
	PrivPass string
}

// SecurityLevel is what this user's configuration amounts to.
func (u User) SecurityLevel() string {
	switch {
	case u.Priv != NoPriv:
		return "authPriv"
	case u.Auth != NoAuth:
		return "authNoPriv"
	default:
		return "noAuthNoPriv"
	}
}

// Validate refuses a user that cannot be used, rather than letting the
// first exchange fail somewhere less obvious.
func (u User) Validate() error {
	switch {
	case u.Name == "":
		// A user with no name is usmNoAuthNoPriv's "initial" user, and
		// this implementation does not serve it.
		return fmt.Errorf("snmp/usm: a user needs a name")
	case len(u.Name) > 32:
		// RFC 3414 §2.4: msgUserName is at most 32 octets.
		return fmt.Errorf("snmp/usm: user name of %d bytes, at most 32", len(u.Name))
	case u.Auth != NoAuth && u.AuthPass == "":
		return fmt.Errorf("snmp/usm: %s needs an authentication password", u.Auth)
	case u.Priv != NoPriv && u.Auth == NoAuth:
		// RFC 3414 §1.4.2: there is nothing to bind the ciphertext to a
		// sender, so this is not a security level.
		return fmt.Errorf("snmp/usm: privacy without authentication is not a security level")
	case u.Priv != NoPriv && u.PrivPass == "":
		return fmt.Errorf("snmp/usm: %s needs a privacy password", u.Priv)
	}
	if u.Auth != NoAuth {
		if _, ok := authSpecs[u.Auth]; !ok {
			return fmt.Errorf("snmp/usm: unknown authentication protocol %d", int(u.Auth))
		}
	}
	if u.Priv != NoPriv && u.Priv != DESCBC && u.Priv != AES128CFB {
		return fmt.Errorf("snmp/usm: unknown privacy protocol %d", int(u.Priv))
	}
	return nil
}

// passwordToKey is RFC 3414 §A.2.1: hash one megabyte of the password
// repeated, and take the digest.
//
// The megabyte is the point. It is a deliberate work factor, chosen in
// 1998 so that deriving a key from a guessed password costs something —
// and it is why this must be done ONCE per (password, protocol) and not
// per message. Everything here caches its result.
func passwordToKey(h func() hash.Hash, password string) []byte {
	const total = 1 << 20 // 1048576, RFC 3414's exact count
	digest := h()
	if password == "" {
		return digest.Sum(nil)
	}
	pw := []byte(password)

	// Feed it in whole buffers rather than a byte at a time: the RFC
	// writes the loop per byte, and a per-byte Write is a thousand times
	// slower for the same bytes in the same order.
	buf := make([]byte, 0, 64*len(pw))
	for len(buf) < 64*len(pw) {
		buf = append(buf, pw...)
	}
	written := 0
	for written < total {
		n := len(buf)
		if remaining := total - written; n > remaining {
			n = remaining
		}
		_, _ = digest.Write(buf[:n])
		written += n
		// Rotate so the next block continues the repetition rather than
		// restarting it, which is what the per-byte loop does.
		shift := n % len(pw)
		if shift != 0 {
			buf = append(buf[shift:], buf[:shift]...)
		}
	}
	return digest.Sum(nil)
}

// localiseKey is RFC 3414 §2.6: Kul = H(Ku || engineID || Ku).
//
// This is what stops one password from unlocking a plant: a key stolen
// from one device authenticates nothing on the next, because the engine
// ID is baked into it.
func localiseKey(h func() hash.Hash, ku, engineID []byte) []byte {
	d := h()
	_, _ = d.Write(ku)
	_, _ = d.Write(engineID)
	_, _ = d.Write(ku)
	return d.Sum(nil)
}

// Keys are one user's derived material for ONE authoritative engine.
type Keys struct {
	Auth []byte
	Priv []byte
}

// DeriveKeys produces the localised keys for a user and an engine.
//
// Deriving costs a megabyte of hashing per password, so callers cache by
// (user, engine) — [KeyStore] does.
func DeriveKeys(u User, engineID []byte) (Keys, error) {
	if err := u.Validate(); err != nil {
		return Keys{}, err
	}
	if len(engineID) == 0 {
		// Without an engine ID there is nothing to localise against, and
		// a key that is not localised is one that works everywhere.
		return Keys{}, fmt.Errorf("snmp/usm: cannot localise a key without an engine ID")
	}
	if u.Auth == NoAuth {
		return Keys{}, nil
	}

	spec := authSpecs[u.Auth]
	var k Keys
	k.Auth = localiseKey(spec.new, passwordToKey(spec.new, u.AuthPass), engineID)

	if u.Priv != NoPriv {
		// The shortest key any protocol here produces is MD5's 16 bytes,
		// and the longest either cipher takes is also 16, so the key is
		// always long enough. RFC 3826 §3.1.2.1's extension rule — hash
		// the key forward until there is enough — belongs here if
		// AES-192 or AES-256 is ever added, and nowhere else.
		priv := localiseKey(spec.new, passwordToKey(spec.new, u.PrivPass), engineID)
		k.Priv = priv[:privKeyLen(u.Priv)]
	}
	return k, nil
}

// privKeyLen is how many bytes of localised key each cipher consumes.
// DES takes 16: eight for the key and eight as the "pre-IV" that is
// XORed with the salt (RFC 3414 §8.1.1.1).
func privKeyLen(p PrivProtocol) int {
	switch p {
	case DESCBC:
		return 16
	case AES128CFB:
		return 16
	default:
		return 0
	}
}

// Digest computes the message authentication code over a whole message
// whose authentication-parameters field is zeroed.
//
// The zeroing is the part that is easy to get wrong: the digest covers
// the field's own bytes, so both sides have to agree that they are zero
// while it is computed. See [Authenticate].
func Digest(proto AuthProtocol, key, message []byte) ([]byte, error) {
	spec, ok := authSpecs[proto]
	if !ok {
		return nil, fmt.Errorf("snmp/usm: cannot digest with %s", proto)
	}
	return digestWith(spec, key, message), nil
}

// digestWith is Digest once the protocol is known, so a caller that has
// already looked the spec up does not look it up again — and does not
// carry an error return it has already ruled out.
func digestWith(spec authSpec, key, message []byte) []byte {
	mac := hmac.New(spec.new, key)
	_, _ = mac.Write(message)
	return mac.Sum(nil)[:spec.digest]
}

// Authenticate writes the digest into an already-encoded message.
//
// offset is where the authentication-parameters field's contents start,
// which the caller gets from codec.EncodeV3 plus the field's position in
// the security parameters it built. The field must already be present
// and zero-filled at its final length, because the digest covers it.
func Authenticate(proto AuthProtocol, key, message []byte, offset int) error {
	spec, ok := authSpecs[proto]
	if !ok {
		return fmt.Errorf("snmp/usm: cannot authenticate with %s", proto)
	}
	if offset < 0 || offset+spec.digest > len(message) {
		return fmt.Errorf("snmp/usm: digest field at %d is outside a %d-byte message",
			offset, len(message))
	}
	for _, b := range message[offset : offset+spec.digest] {
		if b != 0 {
			return fmt.Errorf("snmp/usm: the digest field must be zeroed before signing")
		}
	}
	copy(message[offset:], digestWith(spec, key, message))
	return nil
}

// Verify checks a received message's digest.
//
// The received digest is lifted out and the field zeroed before the
// recomputation, exactly mirroring how it was produced. The comparison
// is constant-time: a byte-by-byte one leaks how much of a forged digest
// was right, which is enough to build the rest of it.
func Verify(proto AuthProtocol, key, message []byte, offset int) error {
	spec, ok := authSpecs[proto]
	if !ok {
		return fmt.Errorf("%w: cannot verify with %s", ErrAuth, proto)
	}
	if offset < 0 || offset+spec.digest > len(message) {
		return fmt.Errorf("%w: digest field at %d is outside a %d-byte message",
			ErrAuth, offset, len(message))
	}

	received := append([]byte(nil), message[offset:offset+spec.digest]...)
	// Work on a copy: a caller may still want the message it was handed.
	work := append([]byte(nil), message...)
	for i := offset; i < offset+spec.digest; i++ {
		work[i] = 0
	}

	if !hmac.Equal(digestWith(spec, key, work), received) {
		return fmt.Errorf("%w: digest mismatch", ErrAuth)
	}
	return nil
}

// Encrypt enciphers a scoped PDU and returns the ciphertext plus the
// privacy parameters that travel with it.
//
// boots and time are the authoritative engine's, and they are part of
// the DES salt rather than decoration: they make the IV unique across
// reboots, which is what stops two messages sent at the same counter
// value from sharing a keystream.
func Encrypt(p PrivProtocol, key []byte, boots, engineTime int32, salt uint64,
	plaintext []byte) (ciphertext, privParams []byte, err error) {
	switch p {
	case DESCBC:
		return encryptDES(key, boots, salt, plaintext)
	case AES128CFB:
		return encryptAES(key, boots, engineTime, salt, plaintext)
	default:
		return nil, nil, fmt.Errorf("snmp/usm: cannot encrypt with %s", p)
	}
}

// Decrypt is Encrypt's inverse.
func Decrypt(p PrivProtocol, key []byte, boots, engineTime int32,
	privParams, ciphertext []byte) ([]byte, error) {
	switch p {
	case DESCBC:
		return decryptDES(key, privParams, ciphertext)
	case AES128CFB:
		return decryptAES(key, boots, engineTime, privParams, ciphertext)
	default:
		return nil, fmt.Errorf("snmp/usm: cannot decrypt with %s", p)
	}
}

// encryptDES is RFC 3414 §8.1.1.1. The first eight bytes of the
// localised key are the DES key; the second eight are the pre-IV, XORed
// with the salt to make the actual IV. The salt itself travels as the
// privacy parameters, so the receiver can rebuild the IV.
func encryptDES(key []byte, boots int32, salt uint64, plaintext []byte) ([]byte, []byte, error) {
	if len(key) < 16 {
		return nil, nil, fmt.Errorf("snmp/usm: CBC-DES needs a 16-byte localised key, got %d", len(key))
	}
	// key[:8] is exactly DES's key size, so NewCipher cannot refuse it;
	// the error it returns exists for callers that pass another length.
	block, _ := des.NewCipher(key[:8]) //nolint:gosec // RFC 3414's privacy protocol

	// The salt is the engine's boot count and a per-message counter, so
	// no two messages from one engine share an IV even across a restart.
	params := make([]byte, 8)
	binary.BigEndian.PutUint32(params[:4], uint32(boots))
	binary.BigEndian.PutUint32(params[4:], uint32(salt))

	iv := make([]byte, 8)
	for i := range iv {
		iv[i] = key[8+i] ^ params[i]
	}

	// CBC needs whole blocks. RFC 3414 §8.1.1.2 pads with whatever is
	// convenient because the plaintext's own SEQUENCE length says where
	// it ends — which is why DecodeScopedPDU tolerates trailing bytes.
	padded := plaintext
	if r := len(padded) % des.BlockSize; r != 0 {
		padded = append(append([]byte(nil), padded...), make([]byte, des.BlockSize-r)...)
	}
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, padded)
	return out, params, nil
}

func decryptDES(key, privParams, ciphertext []byte) ([]byte, error) {
	if len(key) < 16 {
		return nil, fmt.Errorf("snmp/usm: CBC-DES needs a 16-byte localised key, got %d", len(key))
	}
	if len(privParams) != 8 {
		return nil, fmt.Errorf("%w: CBC-DES salt of %d bytes, want 8", ErrAuth, len(privParams))
	}
	if len(ciphertext) == 0 || len(ciphertext)%des.BlockSize != 0 {
		return nil, fmt.Errorf("%w: CBC-DES ciphertext of %d bytes is not whole blocks",
			ErrAuth, len(ciphertext))
	}
	block, _ := des.NewCipher(key[:8]) //nolint:gosec // RFC 3414's privacy protocol; see encryptDES
	iv := make([]byte, 8)
	for i := range iv {
		iv[i] = key[8+i] ^ privParams[i]
	}
	out := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, ciphertext)
	return out, nil
}

// encryptAES is RFC 3826 §3.1.2.1. The IV is the engine's boots and
// time followed by a 64-bit salt, and only the salt travels — the
// receiver already knows the other two from the message header, which is
// what keeps the privacy parameters to eight bytes.
func encryptAES(key []byte, boots, engineTime int32, salt uint64,
	plaintext []byte) ([]byte, []byte, error) {
	block, iv, params, err := aesSetup(key, boots, engineTime, saltBytes(salt))
	if err != nil {
		return nil, nil, err
	}
	out := make([]byte, len(plaintext))
	cipher.NewCFBEncrypter(block, iv).XORKeyStream(out, plaintext) //nolint:staticcheck // RFC 3826 specifies CFB
	return out, params, nil
}

func decryptAES(key []byte, boots, engineTime int32, privParams, ciphertext []byte) ([]byte, error) {
	if len(privParams) != 8 {
		return nil, fmt.Errorf("%w: AES salt of %d bytes, want 8", ErrAuth, len(privParams))
	}
	block, iv, _, err := aesSetup(key, boots, engineTime, privParams)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(ciphertext))
	cipher.NewCFBDecrypter(block, iv).XORKeyStream(out, ciphertext) //nolint:staticcheck // RFC 3826 specifies CFB
	return out, nil
}

func aesSetup(key []byte, boots, engineTime int32, salt []byte) (cipher.Block, []byte, []byte, error) {
	if len(key) < 16 {
		return nil, nil, nil, fmt.Errorf(
			"snmp/usm: AES-128 needs a 16-byte localised key, got %d", len(key))
	}
	// key[:16] is exactly AES-128's key size, so this cannot refuse it.
	block, _ := aes.NewCipher(key[:16])
	iv := make([]byte, 16)
	binary.BigEndian.PutUint32(iv[:4], uint32(boots))
	binary.BigEndian.PutUint32(iv[4:8], uint32(engineTime))
	copy(iv[8:], salt)
	return block, iv, salt, nil
}

func saltBytes(salt uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, salt)
	return b
}
