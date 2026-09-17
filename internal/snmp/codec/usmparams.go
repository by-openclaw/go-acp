package codec

// USMParameters is the blob RFC 3414 §2.4 puts in msgSecurityParameters.
//
// RFC 3412 calls those bytes opaque to the message processing model, and
// [V3] keeps them that way. This type is the exception, and a deliberate
// one: USM is the only security model anything in the field implements,
// its blob is BER, and BER belongs in this package rather than being
// written a second time next to the key handling. What this package
// still does NOT do is hold a key, compute a digest or decrypt anything
// — see internal/snmp/usm.
type USMParameters struct {
	// AuthoritativeEngineID identifies the engine whose clock and boot
	// counter the message is timestamped against: the agent for a
	// request, the sender for a notification.
	AuthoritativeEngineID []byte
	// AuthoritativeEngineBoots and AuthoritativeEngineTime are that
	// engine's restart count and its seconds since the last restart.
	// Together they are what makes a replayed message detectable.
	AuthoritativeEngineBoots int32
	AuthoritativeEngineTime  int32
	// UserName is at most 32 octets (RFC 3414 §2.4).
	UserName string
	// AuthenticationParameters is the truncated HMAC, and MUST be
	// present at its final length and zero-filled while the digest that
	// fills it is computed — the digest covers these bytes.
	AuthenticationParameters []byte
	// PrivacyParameters is the salt the receiver rebuilds the IV from.
	PrivacyParameters []byte
}

// MaxUserNameLen is RFC 3414 §2.4's ceiling on msgUserName.
const MaxUserNameLen = 32

// EncodeUSMParameters renders the blob, and reports where the
// authentication parameters' contents begin inside it.
//
// The offset exists for the same reason [EncodeV3]'s does: the digest is
// computed over the whole message with this field zeroed and then
// written back into it, so the sender has to know exactly where it is.
//
// It renders whatever it is given, the way appendOID does. The 32-octet
// ceiling on a user name is checked where a name ENTERS: at
// configuration, and on the way in off the wire in
// [DecodeUSMParameters]. Checking it a third time here would be a
// branch no caller can reach.
func EncodeUSMParameters(p USMParameters) (raw []byte, authOffset int) {
	var body []byte
	body = appendTLV(body, tagOctetString, p.AuthoritativeEngineID)
	body = appendInt(body, tagInteger, int64(p.AuthoritativeEngineBoots))
	body = appendInt(body, tagInteger, int64(p.AuthoritativeEngineTime))
	body = appendTLV(body, tagOctetString, []byte(p.UserName))

	authOffset = len(body) + 1 + lengthSize(len(p.AuthenticationParameters))
	body = appendTLV(body, tagOctetString, p.AuthenticationParameters)
	body = appendTLV(body, tagOctetString, p.PrivacyParameters)

	out := appendTLV(nil, tagSequence, body)
	authOffset += len(out) - len(body)
	return out, authOffset
}

// DecodeUSMParameters reads the blob and reports where the
// authentication parameters sit, so a receiver can zero them and
// recompute the digest over the message it was handed.
func DecodeUSMParameters(b []byte) (p USMParameters, authOffset int, err error) {
	outer, err := expect(b, tagSequence, "usmSecurityParameters")
	if err != nil {
		return USMParameters{}, 0, err
	}
	if outer.size != len(b) {
		return USMParameters{}, 0, malformed(
			"%d trailing byte(s) after usmSecurityParameters", len(b)-outer.size)
	}
	rest := outer.value
	// consumed is how many bytes of b sit before whatever rest starts
	// at, which is what turns a position inside the sequence into an
	// offset the caller can use against the whole blob.
	consumed := func() int { return len(b) - len(rest) }

	engine, err := expect(rest, tagOctetString, "msgAuthoritativeEngineID")
	if err != nil {
		return USMParameters{}, 0, err
	}
	p.AuthoritativeEngineID = append([]byte(nil), engine.value...)
	rest = rest[engine.size:]

	boots, rest, err := takeInt(rest, "msgAuthoritativeEngineBoots")
	if err != nil {
		return USMParameters{}, 0, err
	}
	p.AuthoritativeEngineBoots = int32(boots)

	engineTime, rest, err := takeInt(rest, "msgAuthoritativeEngineTime")
	if err != nil {
		return USMParameters{}, 0, err
	}
	p.AuthoritativeEngineTime = int32(engineTime)

	name, err := expect(rest, tagOctetString, "msgUserName")
	if err != nil {
		return USMParameters{}, 0, err
	}
	if len(name.value) > MaxUserNameLen {
		return USMParameters{}, 0, malformed("msgUserName of %d bytes, at most %d",
			len(name.value), MaxUserNameLen)
	}
	p.UserName = string(name.value)
	rest = rest[name.size:]

	auth, err := expect(rest, tagOctetString, "msgAuthenticationParameters")
	if err != nil {
		return USMParameters{}, 0, err
	}
	p.AuthenticationParameters = append([]byte(nil), auth.value...)
	// The contents start after this element's own tag and length.
	authOffset = consumed() + (auth.size - len(auth.value))
	rest = rest[auth.size:]

	priv, err := expect(rest, tagOctetString, "msgPrivacyParameters")
	if err != nil {
		return USMParameters{}, 0, err
	}
	p.PrivacyParameters = append([]byte(nil), priv.value...)
	if priv.size != len(rest) {
		return USMParameters{}, 0, malformed(
			"%d trailing byte(s) in usmSecurityParameters", len(rest)-priv.size)
	}
	return p, authOffset, nil
}
