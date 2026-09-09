package usm

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"dhs/internal/clock"
	"dhs/internal/snmp/codec"
)

// randRead is crypto/rand.Read behind a package variable, so the one
// arm a working machine never takes — an entropy source that refuses —
// can be shown to be a refusal to start rather than an engine whose
// salts begin at zero. Production never reassigns it.
var randRead = rand.Read

// ExampleEnterprise is IANA's enterprise number reserved for
// documentation and examples (RFC 5612). It is the default only so that
// a lab rig works out of the box; a plant that puts two of these on one
// network has two devices claiming one identity, and an engine ID is the
// thing every localised key on both is derived from.
const ExampleEnterprise uint32 = 32473

// NewEngineID builds an RFC 3411 §5 engine ID in the
// administratively-assigned-text format (format 5).
//
// The format matters: an engine ID is not free-form. The first four
// octets are the enterprise number with the high bit set, the fifth
// names the format, and the rest is the format's payload. A peer that
// stores engine IDs by their structure — and net-snmp does — rejects a
// bare string.
func NewEngineID(enterprise uint32, text string) ([]byte, error) {
	if text == "" {
		return nil, fmt.Errorf("snmp/usm: an engine ID needs a name")
	}
	// RFC 3411 §5: the whole identifier is between 5 and 32 octets.
	if len(text) > 27 {
		return nil, fmt.Errorf(
			"snmp/usm: engine name of %d bytes; at most 27 fit in a 32-octet engine ID", len(text))
	}
	out := make([]byte, 5, 5+len(text))
	binary.BigEndian.PutUint32(out, enterprise|0x8000_0000)
	out[4] = 5 // administratively assigned text
	return append(out, text...), nil
}

// Engine is THIS process's authoritative engine: the identity every
// localised key here is derived against, plus the boot counter and clock
// that make a replayed message detectable.
//
// It covers the two cases where we are the authoritative engine — an
// agent answering requests, and a notification originator sending traps.
// A manager polling somebody else's agent is authoritative for nothing
// and needs discovery instead; that is a separate piece of work and this
// type does not pretend to do it.
type Engine struct {
	id    []byte
	boots int32
	clk   clock.Clock

	mu      sync.Mutex
	started time.Time
	// salt must not repeat within a boot, or two messages share a
	// keystream. It starts random rather than at zero so a device that
	// loses its boot counter does not restart the sequence either.
	salt  uint64
	users map[string]engineUser
}

// engineUser is a configured user with its keys already localised — the
// derivation costs a megabyte of hashing per password, so it happens
// once at configuration rather than per message.
type engineUser struct {
	user User
	keys Keys
}

// NewEngine builds the local engine. boots is how many times this engine
// has restarted, and MUST be persisted and incremented across restarts:
// a device that always says zero accepts messages recorded before its
// last reboot.
func NewEngine(id []byte, boots int32, clk clock.Clock) (*Engine, error) {
	if len(id) < 5 || len(id) > 32 {
		return nil, fmt.Errorf(
			"snmp/usm: engine ID of %d bytes; RFC 3411 §5 allows 5 to 32", len(id))
	}
	if boots < 0 {
		return nil, fmt.Errorf("snmp/usm: a boot counter cannot be negative")
	}
	if clk == nil {
		clk = clock.System()
	}
	var seed [8]byte
	if _, err := randRead(seed[:]); err != nil {
		return nil, fmt.Errorf("snmp/usm: seeding the salt: %w", err)
	}
	return &Engine{
		id:      append([]byte(nil), id...),
		boots:   boots,
		clk:     clk,
		started: clk.Now(),
		salt:    binary.BigEndian.Uint64(seed[:]),
		users:   map[string]engineUser{},
	}, nil
}

// ID is the engine identifier peers key their localised keys on.
func (e *Engine) ID() []byte { return append([]byte(nil), e.id...) }

// Boots is the restart count.
func (e *Engine) Boots() int32 { return e.boots }

// Time is seconds since this engine started, which with Boots is what a
// peer checks a message's freshness against.
func (e *Engine) Time() int32 {
	e.mu.Lock()
	started := e.started
	e.mu.Unlock()
	return int32(e.clk.Now().Sub(started).Seconds())
}

// AddUser configures a user and derives its keys for this engine.
func (e *Engine) AddUser(u User) error {
	keys, err := DeriveKeys(u, e.id)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, exists := e.users[u.Name]; exists {
		return fmt.Errorf("snmp/usm: user %q is already configured", u.Name)
	}
	e.users[u.Name] = engineUser{user: u, keys: keys}
	return nil
}

// User returns a configured user by name.
func (e *Engine) User(name string) (User, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	u, ok := e.users[name]
	return u.user, ok
}

// nextSalt hands out the next per-message salt.
func (e *Engine) nextSalt() uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.salt++
	return e.salt
}

// Seal renders a message as an authenticated (and optionally encrypted)
// v3 datagram, as the user named.
//
// m carries the PDU and, in m.V3, the message ID and flags the caller
// wants; everything else — the engine identity, the security parameters,
// the digest and the ciphertext — is filled in here. The flags are
// DERIVED from the user rather than taken from the caller, because a
// message whose flags claim less protection than it has is one a peer
// refuses, and one that claims more is one it cannot verify.
func (e *Engine) Seal(m codec.Message, userName string) ([]byte, error) {
	e.mu.Lock()
	eu, ok := e.users[userName]
	e.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("snmp/usm: no user %q", userName)
	}
	if m.PDU == nil {
		return nil, fmt.Errorf("snmp/usm: a v3 message carries a PDU")
	}

	v3 := codec.V3{}
	if m.V3 != nil {
		v3 = *m.V3
	}
	v3.SecurityModel = codec.SecurityModelUSM
	// Reportable stays the caller's — a request wants a Report back, a
	// trap does not — but the protection bits are the user's.
	v3.Flags &= codec.FlagReportable
	if eu.user.Auth != NoAuth {
		v3.Flags |= codec.FlagAuth
	}
	if eu.user.Priv != NoPriv {
		v3.Flags |= codec.FlagPriv
	}
	if len(v3.ContextEngineID) == 0 {
		v3.ContextEngineID = e.id
	}

	params := codec.USMParameters{
		AuthoritativeEngineID:    e.id,
		AuthoritativeEngineBoots: e.boots,
		AuthoritativeEngineTime:  e.Time(),
		UserName:                 eu.user.Name,
		// Present at its final length and zeroed: the digest covers
		// these bytes, so both ends have to agree they are zero while it
		// is computed.
		AuthenticationParameters: make([]byte, eu.user.Auth.DigestLen()),
	}

	if eu.user.Priv != NoPriv {
		plain, err := codec.EncodeScopedPDU(codec.ScopedPDU{
			ContextEngineID: v3.ContextEngineID,
			ContextName:     v3.ContextName,
			PDU:             m.PDU,
		})
		if err != nil {
			return nil, err
		}
		ct, privParams, err := Encrypt(eu.user.Priv, eu.keys.Priv,
			e.boots, params.AuthoritativeEngineTime, e.nextSalt(), plain)
		if err != nil {
			return nil, err
		}
		v3.EncryptedPDU = ct
		params.PrivacyParameters = privParams
	}

	secRaw, authInSec := codec.EncodeUSMParameters(params)
	v3.SecurityParameters = secRaw

	out := m
	out.Version = codec.Version3
	out.V3 = &v3
	raw, secOffset, err := codec.EncodeV3(out)
	if err != nil {
		return nil, err
	}

	if eu.user.Auth == NoAuth {
		return raw, nil
	}
	if err := Authenticate(eu.user.Auth, eu.keys.Auth, raw, secOffset+authInSec); err != nil {
		return nil, err
	}
	return raw, nil
}

// Open verifies and decrypts a v3 datagram addressed to this engine.
//
// It returns the message with its PDU filled in, and the user it came
// from. Every failure is an ErrAuth: telling a peer WHICH check failed —
// unknown user, bad digest, stale timestamp — is how a decoder becomes
// an oracle, and a caller has the same thing to do in all three cases.
func (e *Engine) Open(raw []byte) (codec.Message, User, error) {
	m, err := codec.Decode(raw)
	if err != nil {
		return codec.Message{}, User{}, err
	}
	if m.Version != codec.Version3 || m.V3 == nil {
		return codec.Message{}, User{}, fmt.Errorf("snmp/usm: not a v3 message (%s)", m.Version)
	}
	v3 := m.V3

	if v3.SecurityModel != codec.SecurityModelUSM {
		return codec.Message{}, User{}, fmt.Errorf("%w: security model %d is not USM",
			ErrAuth, v3.SecurityModel)
	}

	params, authInSec, err := codec.DecodeUSMParameters(v3.SecurityParameters)
	if err != nil {
		return codec.Message{}, User{}, err
	}

	e.mu.Lock()
	eu, known := e.users[params.UserName]
	e.mu.Unlock()
	if !known {
		return codec.Message{}, User{}, fmt.Errorf("%w: unknown user", ErrAuth)
	}

	// The message's own claim about protection has to match what this
	// user is configured for, or an attacker downgrades by clearing a
	// flag bit.
	if v3.Flags.Auth() != (eu.user.Auth != NoAuth) || v3.Flags.Priv() != (eu.user.Priv != NoPriv) {
		return codec.Message{}, User{}, fmt.Errorf("%w: %s does not match the user's %s",
			ErrAuth, v3.Flags.SecurityLevel(), eu.user.SecurityLevel())
	}

	if eu.user.Auth != NoAuth {
		if !engineIDsMatch(params.AuthoritativeEngineID, e.id) {
			// The keys were localised to OUR engine, so a message
			// naming another engine cannot verify against them however
			// well-formed it is.
			return codec.Message{}, User{}, fmt.Errorf("%w: addressed to another engine", ErrAuth)
		}
		// The decoder reported where it read those bytes, so there is
		// nothing to search for and nothing to be ambiguous about.
		if err := Verify(eu.user.Auth, eu.keys.Auth, raw,
			v3.SecurityParametersOffset+authInSec); err != nil {
			return codec.Message{}, User{}, err
		}
		if err := e.checkTimeliness(params); err != nil {
			return codec.Message{}, User{}, err
		}
	}

	if eu.user.Priv != NoPriv {
		plain, err := Decrypt(eu.user.Priv, eu.keys.Priv,
			params.AuthoritativeEngineBoots, params.AuthoritativeEngineTime,
			params.PrivacyParameters, v3.EncryptedPDU)
		if err != nil {
			return codec.Message{}, User{}, err
		}
		scoped, err := codec.DecodeScopedPDU(plain)
		if err != nil {
			// The plaintext is rubbish, which on this path means the key
			// was wrong rather than that the sender is broken.
			return codec.Message{}, User{}, fmt.Errorf("%w: %s", ErrAuth, err)
		}
		v3.ContextEngineID, v3.ContextName = scoped.ContextEngineID, scoped.ContextName
		m.PDU = scoped.PDU
	}

	return m, eu.user, nil
}

// TimeWindow is RFC 3414 §3.2's freshness window: a message more than
// 150 seconds off this engine's clock is outside it.
const TimeWindow = 150

// checkTimeliness is the replay check. Boots and time together are what
// make a recorded message unusable later — without it a captured SET is
// a re-route anybody can repeat.
func (e *Engine) checkTimeliness(p codec.USMParameters) error {
	if p.AuthoritativeEngineBoots != e.boots {
		return fmt.Errorf("%w: message is from boot %d, this engine is on %d",
			ErrAuth, p.AuthoritativeEngineBoots, e.boots)
	}
	drift := int(e.Time()) - int(p.AuthoritativeEngineTime)
	if drift < 0 {
		drift = -drift
	}
	if drift > TimeWindow {
		return fmt.Errorf("%w: message is %ds outside the %ds window",
			ErrAuth, drift, TimeWindow)
	}
	return nil
}

// engineIDsMatch compares two engine IDs.
func engineIDsMatch(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
