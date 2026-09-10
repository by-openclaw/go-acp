package usm

// Seal and Open are inverses, and the interesting part is everything
// that must NOT be an inverse: a downgraded flag, a replayed timestamp,
// a message for another engine, a forged digest. Each of those is a way
// a plant gets written to by somebody who should not be able to.

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/snmp/codec"
)

func testEngine(t *testing.T, users ...User) (*Engine, *clock.Fake) {
	t.Helper()
	id, err := NewEngineID(Enterprise, "dhs-test")
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFake(time.Unix(1_700_000_000, 0))
	e, err := NewEngine(id, 3, clk)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		if err := e.AddUser(u); err != nil {
			t.Fatalf("AddUser %q: %v", u.Name, err)
		}
	}
	return e, clk
}

// aTrap is the message these tests carry: an SNMPv2-Trap, which is what
// a v3 notification is.
func aTrap() codec.Message {
	return codec.Message{
		Version: codec.Version3,
		PDU: &codec.PDU{
			Type: codec.PDUTypeTrapV2, RequestID: 1,
			VarBinds: []codec.VarBind{
				{Name: codec.MustParseOID("1.3.6.1.2.1.1.3.0"), Value: codec.TimeTicks(360000)},
				{Name: codec.MustParseOID("1.3.6.1.4.1.7995.1.1.1"), Value: codec.Int(1)},
			},
		},
	}
}

// An engine ID has a structure — enterprise, format, payload — and a
// peer that stores engine IDs by that structure rejects a bare string.
func TestEngineIDFormat(t *testing.T) {
	id, err := NewEngineID(Enterprise, "dhs-agent")
	if err != nil {
		t.Fatal(err)
	}
	if len(id) < 5 || len(id) > 32 {
		t.Fatalf("engine ID of %d bytes; RFC 3411 §5 allows 5 to 32", len(id))
	}
	if id[0]&0x80 == 0 {
		t.Error("the enterprise number must carry the high bit")
	}
	if id[4] != 5 {
		t.Errorf("format byte = %d, want 5 (administratively assigned text)", id[4])
	}
	if string(id[5:]) != "dhs-agent" {
		t.Errorf("payload = %q", id[5:])
	}

	if _, err := NewEngineID(Enterprise, ""); err == nil {
		t.Error("an engine ID needs a name")
	}
	if _, err := NewEngineID(Enterprise, strings.Repeat("x", 28)); err == nil ||
		!strings.Contains(err.Error(), "32-octet") {
		t.Errorf("= %v, want the length refusal", err)
	}
}

func TestNewEngineRefusals(t *testing.T) {
	for _, tc := range []struct {
		name  string
		id    []byte
		boots int32
		want  string
	}{
		{"an engine ID too short to be one", []byte{1, 2}, 0, "5 to 32"},
		{"an engine ID longer than the field", bytes.Repeat([]byte{1}, 33), 0, "5 to 32"},
		{"a negative boot counter", []byte("engine"), -1, "cannot be negative"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewEngine(tc.id, tc.boots, nil); err == nil ||
				!strings.Contains(err.Error(), tc.want) {
				t.Fatalf("= %v, want %q", err, tc.want)
			}
		})
	}

	// A nil clock is the system clock, not a panic on first use.
	e, err := NewEngine([]byte("engine-id"), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if e.Time() < 0 {
		t.Error("a fresh engine's time runs forward")
	}
}

// Every security level, and every protocol pair, seals and opens.
func TestSealAndOpenAtEveryLevel(t *testing.T) {
	for _, auth := range []AuthProtocol{
		NoAuth, HMACMD5, HMACSHA, HMACSHA224, HMACSHA256, HMACSHA384, HMACSHA512,
	} {
		for _, priv := range []PrivProtocol{NoPriv, DESCBC, AES128CFB} {
			if auth == NoAuth && priv != NoPriv {
				continue // not a security level; Validate refuses it
			}
			u := User{Name: "operator", Auth: auth, AuthPass: "maplesyrup",
				Priv: priv, PrivPass: "maplesyrup"}
			t.Run(auth.String()+"/"+priv.String(), func(t *testing.T) {
				e, _ := testEngine(t, u)

				raw, err := e.Seal(aTrap(), "operator")
				if err != nil {
					t.Fatalf("Seal: %v", err)
				}
				// The PDU must not be readable off the wire when privacy
				// is on — that is the whole point of it.
				if priv != NoPriv {
					plain, _ := codec.Encode(codec.Message{
						Version: codec.Version2c, Community: "x", PDU: aTrap().PDU})
					if bytes.Contains(raw, plain[len(plain)-16:]) {
						t.Error("the PDU is legible in an encrypted message")
					}
				}

				got, user, err := e.Open(raw)
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				if user.Name != "operator" {
					t.Errorf("opened as %q", user.Name)
				}
				if got.PDU == nil || got.PDU.Type != codec.PDUTypeTrapV2 {
					t.Fatalf("PDU = %+v", got.PDU)
				}
				if len(got.PDU.VarBinds) != 2 {
					t.Fatalf("%d varbinds", len(got.PDU.VarBinds))
				}
				if got.PDU.VarBinds[1].Value.Int != 1 {
					t.Errorf("varbind = %s", got.PDU.VarBinds[1].Value)
				}
				if lvl := got.V3.Flags.SecurityLevel(); lvl != u.SecurityLevel() {
					t.Errorf("flags say %s, the user is %s", lvl, u.SecurityLevel())
				}
			})
		}
	}
}

// The flags are the USER's, not the caller's: a message claiming less
// protection than it has is refused by a peer, and one claiming more
// cannot be verified.
func TestTheFlagsComeFromTheUser(t *testing.T) {
	e, _ := testEngine(t, User{Name: "operator", Auth: HMACSHA256, AuthPass: "p",
		Priv: AES128CFB, PrivPass: "p"})

	m := aTrap()
	m.V3 = &codec.V3{ID: 1, Flags: codec.FlagReportable} // no auth, no priv claimed
	raw, err := e.Seal(m, "operator")
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := e.Open(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !got.V3.Flags.Auth() || !got.V3.Flags.Priv() {
		t.Errorf("flags = %s, want the user's authPriv", got.V3.Flags.SecurityLevel())
	}
	// Reportable is the caller's and survives.
	if !got.V3.Flags.Reportable() {
		t.Error("the caller's reportable flag was dropped")
	}
}

// A tampered message does not open, wherever it was tampered with.
func TestATamperedMessageDoesNotOpen(t *testing.T) {
	e, _ := testEngine(t, User{Name: "operator", Auth: HMACSHA256, AuthPass: "p"})
	raw, err := e.Seal(aTrap(), "operator")
	if err != nil {
		t.Fatal(err)
	}

	for i := range raw {
		bad := append([]byte(nil), raw...)
		bad[i] ^= 0x01
		if _, _, err := e.Open(bad); err == nil {
			t.Fatalf("byte %d could be flipped without detection", i)
		}
	}
}

// Clearing the auth flag is the downgrade an attacker reaches for first:
// strip the protection and the message reads as one that never had any.
func TestADowngradedFlagIsRefused(t *testing.T) {
	e, _ := testEngine(t,
		User{Name: "secure", Auth: HMACSHA256, AuthPass: "p"},
		User{Name: "open"})

	raw, err := e.Seal(aTrap(), "secure")
	if err != nil {
		t.Fatal(err)
	}
	// Find the flags octet and clear its protection bits.
	m, err := codec.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	idx := bytes.Index(raw, []byte{0x04, 0x01, byte(m.V3.Flags)})
	if idx < 0 {
		t.Fatal("could not find the flags octet")
	}
	raw[idx+2] &^= byte(codec.FlagAuth | codec.FlagPriv)

	if _, _, err := e.Open(raw); !errors.Is(err, ErrAuth) {
		t.Errorf("= %v, want the downgrade refused", err)
	}

	// And the other way: a noAuthNoPriv user's message that claims to be
	// authenticated cannot be verified, so it is refused too.
	raw, err = e.Seal(aTrap(), "open")
	if err != nil {
		t.Fatal(err)
	}
	m, err = codec.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	idx = bytes.Index(raw, []byte{0x04, 0x01, byte(m.V3.Flags)})
	if idx < 0 {
		t.Fatal("could not find the flags octet")
	}
	raw[idx+2] |= byte(codec.FlagAuth)
	if _, _, err := e.Open(raw); !errors.Is(err, ErrAuth) {
		t.Errorf("= %v, want the upgrade refused", err)
	}
}

// A recorded message replayed later is what the boots-and-time window
// exists to stop: without it a captured SET is a re-route anybody can
// repeat.
func TestAReplayedMessageIsRefused(t *testing.T) {
	e, clk := testEngine(t, User{Name: "operator", Auth: HMACSHA256, AuthPass: "p"})

	raw, err := e.Seal(aTrap(), "operator")
	if err != nil {
		t.Fatal(err)
	}
	// Inside the window it still opens.
	clk.Advance(TimeWindow / 2 * time.Second)
	if _, _, err := e.Open(raw); err != nil {
		t.Fatalf("a message inside the window: %v", err)
	}
	// Outside it, it does not.
	clk.Advance((TimeWindow + 10) * time.Second)
	if _, _, err := e.Open(raw); !errors.Is(err, ErrAuth) {
		t.Errorf("= %v, want the replay refused", err)
	}
}

// A message from another engine cannot verify against keys localised to
// this one, and saying so early is clearer than a digest mismatch.
func TestAMessageForAnotherEngineIsRefused(t *testing.T) {
	u := User{Name: "operator", Auth: HMACSHA256, AuthPass: "p"}
	a, _ := testEngine(t, u)

	otherID, err := NewEngineID(Enterprise, "somebody-else")
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewEngine(otherID, 3, clock.NewFake(time.Unix(1_700_000_000, 0)))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.AddUser(u); err != nil {
		t.Fatal(err)
	}

	raw, err := b.Seal(aTrap(), "operator")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Open(raw); !errors.Is(err, ErrAuth) {
		t.Errorf("= %v, want the wrong-engine refusal", err)
	}
}

// A message from a boot this engine has not reached is as stale as one
// from a boot it has left behind.
func TestAMessageFromAnotherBootIsRefused(t *testing.T) {
	u := User{Name: "operator", Auth: HMACSHA256, AuthPass: "p"}
	e, _ := testEngine(t, u)
	raw, err := e.Seal(aTrap(), "operator")
	if err != nil {
		t.Fatal(err)
	}

	// Same identity and keys, one reboot later.
	rebooted, err := NewEngine(e.ID(), e.Boots()+1, clock.NewFake(time.Unix(1_700_000_000, 0)))
	if err != nil {
		t.Fatal(err)
	}
	if err := rebooted.AddUser(u); err != nil {
		t.Fatal(err)
	}
	if _, _, err := rebooted.Open(raw); !errors.Is(err, ErrAuth) {
		t.Errorf("= %v, want the boot-counter refusal", err)
	}
}

// An unknown user is refused, and the refusal says no more than that:
// telling a peer which check failed is how a decoder becomes an oracle.
func TestAnUnknownUserIsRefused(t *testing.T) {
	sender, _ := testEngine(t, User{Name: "operator", Auth: HMACSHA256, AuthPass: "p"})
	raw, err := sender.Seal(aTrap(), "operator")
	if err != nil {
		t.Fatal(err)
	}

	receiver, err := NewEngine(sender.ID(), sender.Boots(),
		clock.NewFake(time.Unix(1_700_000_000, 0)))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := receiver.Open(raw); !errors.Is(err, ErrAuth) {
		t.Errorf("= %v, want the unknown user refused", err)
	}
}

func TestSealRefusals(t *testing.T) {
	e, _ := testEngine(t, User{Name: "operator", Auth: HMACSHA256, AuthPass: "p"})

	if _, err := e.Seal(aTrap(), "nobody"); err == nil ||
		!strings.Contains(err.Error(), "no user") {
		t.Errorf("= %v, want the unknown user refused", err)
	}
	if _, err := e.Seal(codec.Message{Version: codec.Version3}, "operator"); err == nil ||
		!strings.Contains(err.Error(), "carries a PDU") {
		t.Errorf("= %v, want a message with no PDU refused", err)
	}
	// A PDU no v3 message may carry.
	bad := aTrap()
	bad.PDU = &codec.PDU{Type: codec.PDUTypeTrapV1}
	if _, err := e.Seal(bad, "operator"); err == nil {
		t.Error("a v1 Trap-PDU cannot travel in a v3 message")
	}
}

func TestOpenRefusals(t *testing.T) {
	e, _ := testEngine(t, User{Name: "operator", Auth: HMACSHA256, AuthPass: "p"})

	if _, _, err := e.Open([]byte{0xFF}); err == nil {
		t.Error("rubbish must be refused")
	}

	// A v2c message is not a v3 message, whatever else is true of it.
	v2, err := codec.Encode(codec.Message{Version: codec.Version2c, Community: "public",
		PDU: &codec.PDU{Type: codec.PDUTypeGet}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.Open(v2); err == nil || !strings.Contains(err.Error(), "not a v3 message") {
		t.Errorf("= %v, want the version refusal", err)
	}
}

// A security model this engine does not implement is refused rather than
// treated as USM, which would mean parsing somebody else's blob as ours.
func TestAnotherSecurityModelIsRefused(t *testing.T) {
	e, _ := testEngine(t, User{Name: "operator"})

	m := aTrap()
	m.V3 = &codec.V3{ID: 1, SecurityModel: 9, ContextEngineID: e.ID()}
	raw, err := codec.Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.Open(raw); !errors.Is(err, ErrAuth) {
		t.Errorf("= %v, want the model refused", err)
	}
}

// A user is configured once. Two with one name would be two sets of keys
// for one identity, and which one verified would depend on map order.
func TestAUserIsConfiguredOnce(t *testing.T) {
	e, _ := testEngine(t)
	u := User{Name: "operator", Auth: HMACSHA256, AuthPass: "p"}
	if err := e.AddUser(u); err != nil {
		t.Fatal(err)
	}
	if err := e.AddUser(u); err == nil || !strings.Contains(err.Error(), "already configured") {
		t.Errorf("= %v, want the duplicate refused", err)
	}
	if err := e.AddUser(User{}); err == nil {
		t.Error("an invalid user must be refused")
	}

	if got, ok := e.User("operator"); !ok || got.Auth != HMACSHA256 {
		t.Errorf("User = %+v, %v", got, ok)
	}
	if _, ok := e.User("nobody"); ok {
		t.Error("an unconfigured user must not resolve")
	}
}

// Salts do not repeat within a boot: two messages sharing one would
// share a keystream.
func TestSaltsDoNotRepeat(t *testing.T) {
	e, _ := testEngine(t, User{Name: "operator", Auth: HMACSHA256, AuthPass: "p",
		Priv: AES128CFB, PrivPass: "p"})

	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		raw, err := e.Seal(aTrap(), "operator")
		if err != nil {
			t.Fatal(err)
		}
		m, err := codec.Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		p, _, err := codec.DecodeUSMParameters(m.V3.SecurityParameters)
		if err != nil {
			t.Fatal(err)
		}
		key := string(p.PrivacyParameters)
		if seen[key] {
			t.Fatalf("salt % x repeated at message %d", p.PrivacyParameters, i)
		}
		seen[key] = true
	}
}

// The engine reports the identity and counters a peer needs to talk to
// it, and ID hands back a copy so a caller cannot edit it in place.
func TestEngineIdentity(t *testing.T) {
	e, clk := testEngine(t)

	id := e.ID()
	id[0] ^= 0xFF
	if bytes.Equal(id, e.ID()) {
		t.Error("ID handed out the engine's own slice")
	}
	if e.Boots() != 3 {
		t.Errorf("Boots = %d", e.Boots())
	}
	if e.Time() != 0 {
		t.Errorf("a fresh engine's time = %d, want 0", e.Time())
	}
	clk.Advance(90 * time.Second)
	if e.Time() != 90 {
		t.Errorf("time = %d after 90s", e.Time())
	}
}

// The digest is verified against the bytes that ARRIVED, at the offset
// the decoder reported — not at one found by re-encoding or by searching
// for the security parameters, either of which would break the moment an
// agent in the field encoded a length differently or the same bytes
// appeared twice.
func TestTheOffsetComesFromTheDecoder(t *testing.T) {
	e, _ := testEngine(t, User{Name: "operator", Auth: HMACSHA256, AuthPass: "p"})
	raw, err := e.Seal(aTrap(), "operator")
	if err != nil {
		t.Fatal(err)
	}
	m, err := codec.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	off := m.V3.SecurityParametersOffset
	if off <= 0 || off+len(m.V3.SecurityParameters) > len(raw) {
		t.Fatalf("offset %d is outside a %d-byte message", off, len(raw))
	}
	if !bytes.Equal(raw[off:off+len(m.V3.SecurityParameters)], m.V3.SecurityParameters) {
		t.Error("the reported offset does not point at the parameters")
	}
}

// An engine that cannot seed its salt refuses to start rather than
// running with salts that begin at zero — which on a device that also
// loses its boot counter means two boots sharing a keystream.
func TestAnEngineThatCannotSeedItsSaltDoesNotStart(t *testing.T) {
	prev := randRead
	randRead = func([]byte) (int, error) { return 0, errors.New("no entropy") }
	t.Cleanup(func() { randRead = prev })

	if _, err := NewEngine([]byte("engine-id"), 0, nil); err == nil ||
		!strings.Contains(err.Error(), "seeding the salt") {
		t.Fatalf("= %v, want the refusal", err)
	}
}

// The arms below are defensive: nothing a caller can pass through the
// public API reaches them, because AddUser validates. They are driven by
// corrupting the engine's own user table from inside the package, which
// is what a future edit that loosened AddUser would do by accident.
func corrupt(t *testing.T, e *Engine, name string, f func(*engineUser)) {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	eu := e.users[name]
	f(&eu)
	e.users[name] = eu
}

func TestSealDefensiveArms(t *testing.T) {
	t.Run("a PDU the scope cannot carry, on the privacy path", func(t *testing.T) {
		e, _ := testEngine(t, User{Name: "u", Auth: HMACSHA256, AuthPass: "p",
			Priv: AES128CFB, PrivPass: "p"})
		bad := aTrap()
		bad.PDU = &codec.PDU{Type: codec.PDUTypeTrapV1}
		if _, err := e.Seal(bad, "u"); err == nil {
			t.Error("a v1 Trap-PDU cannot travel in a v3 message")
		}
	})

	t.Run("a cipher that cannot encrypt", func(t *testing.T) {
		e, _ := testEngine(t, User{Name: "u", Auth: HMACSHA256, AuthPass: "p",
			Priv: AES128CFB, PrivPass: "p"})
		corrupt(t, e, "u", func(eu *engineUser) { eu.user.Priv = PrivProtocol(99) })
		if _, err := e.Seal(aTrap(), "u"); err == nil ||
			!strings.Contains(err.Error(), "cannot encrypt") {
			t.Errorf("= %v, want the cipher refusal", err)
		}
	})

	t.Run("a digest that cannot be computed", func(t *testing.T) {
		e, _ := testEngine(t, User{Name: "u", Auth: HMACSHA256, AuthPass: "p"})
		corrupt(t, e, "u", func(eu *engineUser) { eu.user.Auth = AuthProtocol(99) })
		if _, err := e.Seal(aTrap(), "u"); err == nil ||
			!strings.Contains(err.Error(), "cannot authenticate") {
			t.Errorf("= %v, want the digest refusal", err)
		}
	})

	t.Run("a message too large for a datagram", func(t *testing.T) {
		e, _ := testEngine(t, User{Name: "u"})
		big := aTrap()
		for i := 0; i < 4096; i++ {
			big.PDU.VarBinds = append(big.PDU.VarBinds, codec.VarBind{
				Name: codec.MustParseOID("1.3.6.1.4.1.7995.1"), Value: codec.Bytes(make([]byte, 32))})
		}
		if _, err := e.Seal(big, "u"); err == nil ||
			!strings.Contains(err.Error(), "datagram limit") {
			t.Errorf("= %v, want the size refusal", err)
		}
	})
}

func TestOpenDefensiveArms(t *testing.T) {
	u := User{Name: "u", Auth: HMACSHA256, AuthPass: "p", Priv: AES128CFB, PrivPass: "p"}

	t.Run("a cipher that cannot decrypt", func(t *testing.T) {
		e, _ := testEngine(t, u)
		raw, err := e.Seal(aTrap(), "u")
		if err != nil {
			t.Fatal(err)
		}
		corrupt(t, e, "u", func(eu *engineUser) { eu.user.Priv = PrivProtocol(99) })
		if _, _, err := e.Open(raw); err == nil ||
			!strings.Contains(err.Error(), "cannot decrypt") {
			t.Errorf("= %v, want the cipher refusal", err)
		}
	})

	t.Run("a plaintext that is not a scoped PDU", func(t *testing.T) {
		e, _ := testEngine(t, u)
		raw, err := e.Seal(aTrap(), "u")
		if err != nil {
			t.Fatal(err)
		}
		// The digest still verifies — the auth key is untouched — so the
		// wrong privacy key produces rubbish that has to be reported as
		// an authentication failure rather than a parse error.
		corrupt(t, e, "u", func(eu *engineUser) {
			eu.keys.Priv = []byte("0123456789abcdef")
		})
		if _, _, err := e.Open(raw); !errors.Is(err, ErrAuth) {
			t.Errorf("= %v, want an ErrAuth", err)
		}
	})
}

// A message timestamped ahead of this engine's clock is as far outside
// the window as one behind it — a drift check that only looked one way
// would accept anything dated in the future.
func TestAMessageFromTheFutureIsRefused(t *testing.T) {
	u := User{Name: "u", Auth: HMACSHA256, AuthPass: "p"}

	// One engine identity, two clocks: the sender's is ten minutes ahead
	// of the receiver's, which is what a plant with no NTP looks like.
	ahead, aheadClk := testEngine(t, u)
	aheadClk.Advance(10 * time.Minute)
	raw, err := ahead.Seal(aTrap(), "u")
	if err != nil {
		t.Fatal(err)
	}

	behind, err := NewEngine(ahead.ID(), ahead.Boots(),
		clock.NewFake(time.Unix(1_700_000_000, 0)))
	if err != nil {
		t.Fatal(err)
	}
	if err := behind.AddUser(u); err != nil {
		t.Fatal(err)
	}
	if _, _, err := behind.Open(raw); !errors.Is(err, ErrAuth) {
		t.Errorf("= %v, want the future message refused", err)
	}
}
