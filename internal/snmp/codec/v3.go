package codec

import "fmt"

// SNMPv3 (RFC 3412) replaces the community string with a header, a
// security-parameters blob, and a scoped PDU.
//
// This package builds and reads the STRUCTURE and treats the security
// parameters as opaque bytes. It holds no keys, computes no digests and
// decrypts nothing: that is a security model's job, and USM is one of
// several the architecture allows. internal/snmp/usm is ours. Keeping
// the split here is also what lets this package stay stdlib-only with no
// dhs/ imports (ADR-0006) while the key handling lives somewhere it can
// depend on the rest of the tree.

// MsgFlags is the one-octet msgFlags field.
type MsgFlags byte

const (
	// FlagAuth says the message is authenticated.
	FlagAuth MsgFlags = 0x01
	// FlagPriv says the scoped PDU is encrypted. Priv without auth is
	// forbidden by RFC 3412 §6.4 — there is nothing to bind the
	// ciphertext to a sender — and is refused here.
	FlagPriv MsgFlags = 0x02
	// FlagReportable asks the receiver to answer errors with a Report
	// PDU rather than silently. A request sets it; a trap does not,
	// because nobody is waiting for the answer.
	FlagReportable MsgFlags = 0x04
)

func (f MsgFlags) Auth() bool       { return f&FlagAuth != 0 }
func (f MsgFlags) Priv() bool       { return f&FlagPriv != 0 }
func (f MsgFlags) Reportable() bool { return f&FlagReportable != 0 }

// SecurityLevel names the three levels of protection, which is how an
// operator and every RFC talk about it.
func (f MsgFlags) SecurityLevel() string {
	switch {
	case f.Priv():
		return "authPriv"
	case f.Auth():
		return "authNoPriv"
	default:
		return "noAuthNoPriv"
	}
}

// SecurityModelUSM is the User-based Security Model (RFC 3414), number 3
// in the IANA registry and the only one anything in the field uses.
const SecurityModelUSM int32 = 3

// DefaultMaxSize is what this implementation advertises it can receive.
// RFC 3412 puts the floor at 484.
const DefaultMaxSize int32 = 65507

// V3 is the v3-only half of a [Message].
//
// The PDU itself stays in Message.PDU, so a caller that only cares what
// was asked — an agent, a trap receiver — reads the same field whatever
// the version carried it.
type V3 struct {
	// ID correlates a response with its request. It is NOT the PDU's
	// request-id: v3 numbers the message as well, so that a Report about
	// a message that could not be decrypted can still be matched.
	ID int32
	// MaxSize is the largest message the sender can receive.
	MaxSize int32
	// Flags is auth / priv / reportable.
	Flags MsgFlags
	// SecurityModel is 3 for USM.
	SecurityModel int32
	// SecurityParameters is the model's own blob, opaque here.
	SecurityParameters []byte
	// SecurityParametersOffset is where those bytes began in the
	// datagram they were read from, set by decoding and zero otherwise.
	//
	// A security model needs it because a USM digest covers the whole
	// message and is written INTO the parameters, so verifying means
	// zeroing exactly those bytes of exactly the datagram that arrived.
	// Re-encoding to find them would not do: an agent in the field may
	// encode a length differently from us, and searching for them is
	// ambiguous the moment the same bytes appear twice.
	SecurityParametersOffset int

	// ContextEngineID and ContextName scope the PDU. An empty engine ID
	// on a request means "whatever this agent is"; on a notification it
	// is the sender's own.
	ContextEngineID []byte
	ContextName     string

	// EncryptedPDU carries the scoped PDU as ciphertext, and is set
	// INSTEAD of Message.PDU when Flags has priv. Decoding stops there
	// because this package has no keys; the security model decrypts it
	// and decodes the plaintext with [DecodeScopedPDU].
	EncryptedPDU []byte
}

// ScopedPDU is the plaintext inside a v3 message.
type ScopedPDU struct {
	ContextEngineID []byte
	ContextName     string
	PDU             *PDU
}

// EncodeScopedPDU renders a scoped PDU. The security model calls it to
// get the plaintext it encrypts.
func EncodeScopedPDU(s ScopedPDU) ([]byte, error) {
	if s.PDU == nil {
		return nil, fmt.Errorf("snmp: scoped PDU carries no PDU")
	}
	if err := checkPDUForVersion(s.PDU.Type, Version3); err != nil {
		return nil, err
	}
	var body []byte
	body = appendTLV(body, tagOctetString, s.ContextEngineID)
	body = appendTLV(body, tagOctetString, []byte(s.ContextName))
	body = appendPDU(body, *s.PDU)
	return appendTLV(nil, tagSequence, body), nil
}

// DecodeScopedPDU reads a scoped PDU, which is what a security model
// hands back after decrypting.
//
// Trailing bytes are TOLERATED here and nowhere else in this package:
// both ciphers RFC 3826 and RFC 3414 specify pad to a block boundary,
// and the padding lands after the SEQUENCE with no length that describes
// it. Refusing it would refuse every encrypted message whose plaintext
// is not a multiple of eight.
func DecodeScopedPDU(b []byte) (ScopedPDU, error) {
	outer, err := expect(b, tagSequence, "scoped PDU")
	if err != nil {
		return ScopedPDU{}, err
	}
	rest := outer.value

	engine, err := expect(rest, tagOctetString, "contextEngineID")
	if err != nil {
		return ScopedPDU{}, err
	}
	out := ScopedPDU{ContextEngineID: append([]byte(nil), engine.value...)}
	rest = rest[engine.size:]

	name, err := expect(rest, tagOctetString, "contextName")
	if err != nil {
		return ScopedPDU{}, err
	}
	out.ContextName = string(name.value)
	rest = rest[name.size:]

	pdu, err := readTLV(rest, "PDU")
	if err != nil {
		return ScopedPDU{}, err
	}
	if pdu.size != len(rest) {
		return ScopedPDU{}, malformed("%d trailing byte(s) after the scoped PDU's PDU",
			len(rest)-pdu.size)
	}
	if err := checkPDUForVersion(PDUType(pdu.tag), Version3); err != nil {
		return ScopedPDU{}, malformed("%s", err)
	}
	p, err := parsePDU(pdu)
	if err != nil {
		return ScopedPDU{}, err
	}
	out.PDU = &p
	return out, nil
}

// EncodeV3 renders a v3 message and reports where the security
// parameters landed.
//
// The offset is not a convenience: a USM digest is computed over the
// WHOLE message with the authentication-parameters field zeroed, and
// then written back into that field — so the sender has to know exactly
// where in the finished datagram those bytes are. Searching for them
// afterwards would work until two identical blobs appeared in one
// message.
//
// secOffset is the index of the first byte of the security parameters'
// CONTENTS, so a model that knows where the field sits inside its own
// blob can add the two.
func EncodeV3(m Message) (raw []byte, secOffset int, err error) {
	if m.V3 == nil {
		return nil, 0, fmt.Errorf("snmp: a v3 message needs its V3 header")
	}
	v := m.V3

	if v.Flags.Priv() && !v.Flags.Auth() {
		// RFC 3412 §6.4: there is nothing to bind the ciphertext to a
		// sender, so the combination is not a security level at all.
		return nil, 0, fmt.Errorf("snmp: priv without auth is not a security level")
	}

	var scoped []byte
	switch {
	case v.EncryptedPDU != nil:
		if !v.Flags.Priv() {
			return nil, 0, fmt.Errorf(
				"snmp: an encrypted scoped PDU in a %s message", v.Flags.SecurityLevel())
		}
		// The ciphertext travels as an OCTET STRING rather than as a
		// SEQUENCE, which is how a receiver knows to decrypt before it
		// parses.
		scoped = appendTLV(nil, tagOctetString, v.EncryptedPDU)
	case v.Flags.Priv():
		return nil, 0, fmt.Errorf("snmp: authPriv message with no ciphertext")
	default:
		scoped, err = EncodeScopedPDU(ScopedPDU{
			ContextEngineID: v.ContextEngineID,
			ContextName:     v.ContextName,
			PDU:             m.PDU,
		})
		if err != nil {
			return nil, 0, err
		}
	}

	maxSize := v.MaxSize
	if maxSize == 0 {
		maxSize = DefaultMaxSize
	}
	model := v.SecurityModel
	if model == 0 {
		model = SecurityModelUSM
	}

	var header []byte
	header = appendInt(header, tagInteger, int64(v.ID))
	header = appendInt(header, tagInteger, int64(maxSize))
	header = appendTLV(header, tagOctetString, []byte{byte(v.Flags)})
	header = appendInt(header, tagInteger, int64(model))

	var body []byte
	body = appendInt(body, tagInteger, int64(Version3))
	body = appendTLV(body, tagSequence, header)
	// Note where the security parameters' contents begin, relative to
	// the body; the outer header is added below and shifts it.
	secOffset = len(body) + 1 + lengthSize(len(v.SecurityParameters))
	body = appendTLV(body, tagOctetString, v.SecurityParameters)
	body = append(body, scoped...)

	out := appendTLV(nil, tagSequence, body)
	secOffset += len(out) - len(body)

	if len(out) > MaxMessageSize {
		return nil, 0, fmt.Errorf("snmp: message of %d bytes exceeds the %d-byte datagram limit",
			len(out), MaxMessageSize)
	}
	return out, secOffset, nil
}

// lengthSize is how many bytes appendLength will write for n.
func lengthSize(n int) int {
	if n < 0x80 {
		return 1
	}
	size := 1
	for n > 0 {
		size++
		n >>= 8
	}
	return size
}

// decodeV3 reads the v3 body, having already consumed the version.
//
// It is reached from [Decode], so a caller reads any version through one
// entry point. What comes back has Message.PDU set for an unencrypted
// message and V3.EncryptedPDU set for an encrypted one — this package
// holds no keys, so that is as far as it goes.
// b is the whole datagram and rest is what is left of it after the
// version, so an offset into rest can be reported as an offset into b.
func decodeV3(b, rest []byte) (Message, error) {
	m := Message{Version: Version3, V3: &V3{}}
	v := m.V3

	header, err := expect(rest, tagSequence, "msgGlobalData")
	if err != nil {
		return Message{}, err
	}
	h := header.value

	id, h, err := takeInt(h, "msgID")
	if err != nil {
		return Message{}, err
	}
	v.ID = int32(id)

	maxSize, h, err := takeInt(h, "msgMaxSize")
	if err != nil {
		return Message{}, err
	}
	v.MaxSize = int32(maxSize)

	flags, err := expect(h, tagOctetString, "msgFlags")
	if err != nil {
		return Message{}, err
	}
	if len(flags.value) != 1 {
		return Message{}, malformed("msgFlags of %d bytes, want 1", len(flags.value))
	}
	v.Flags = MsgFlags(flags.value[0])
	h = h[flags.size:]

	model, h, err := takeInt(h, "msgSecurityModel")
	if err != nil {
		return Message{}, err
	}
	v.SecurityModel = int32(model)
	if len(h) != 0 {
		return Message{}, malformed("%d trailing byte(s) in msgGlobalData", len(h))
	}
	rest = rest[header.size:]

	sec, err := expect(rest, tagOctetString, "msgSecurityParameters")
	if err != nil {
		return Message{}, err
	}
	v.SecurityParameters = append([]byte(nil), sec.value...)
	v.SecurityParametersOffset = (len(b) - len(rest)) + (sec.size - len(sec.value))
	rest = rest[sec.size:]

	data, err := readTLV(rest, "msgData")
	if err != nil {
		return Message{}, err
	}
	if data.size != len(rest) {
		return Message{}, malformed("%d trailing byte(s) after msgData", len(rest)-data.size)
	}

	if data.tag == tagOctetString {
		// Ciphertext. A message that carries it without saying it is
		// encrypted is one whose flags and body disagree, and guessing
		// which of the two is right is how a decoder becomes an oracle.
		if !v.Flags.Priv() {
			return Message{}, malformed(
				"an encrypted scoped PDU in a %s message", v.Flags.SecurityLevel())
		}
		v.EncryptedPDU = append([]byte(nil), data.value...)
		return m, nil
	}
	if v.Flags.Priv() {
		return Message{}, malformed("an authPriv message with a plaintext scoped PDU")
	}

	scoped, err := DecodeScopedPDU(rest)
	if err != nil {
		return Message{}, err
	}
	v.ContextEngineID, v.ContextName = scoped.ContextEngineID, scoped.ContextName
	m.PDU = scoped.PDU
	return m, nil
}
