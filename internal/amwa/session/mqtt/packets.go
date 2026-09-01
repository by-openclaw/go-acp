// Package mqtt is a minimal MQTT 3.1.1 PUBLISHING client — exactly
// the wire surface an IS-07 MQTT event sender needs: CONNECT,
// PUBLISH (QoS 0, retained), PINGREQ, DISCONNECT, and the CONNACK /
// PINGRESP answers. Nothing else, deliberately: subscriptions are the
// CONSUMER side of IS-07 and a broker implementation is neither.
//
// Spec: MQTT Version 3.1.1, OASIS Standard, 29 October 2014
// (mqtt-v3.1.1-os). Packet bytes in the tests come from that document,
// not from this code.
package mqtt

import (
	"encoding/binary"
	"fmt"
)

// Control packet types (§2.2.1, four high bits of the fixed header).
const (
	packetCONNECT    byte = 1
	packetCONNACK    byte = 2
	packetPUBLISH    byte = 3
	packetPINGREQ    byte = 12
	packetPINGRESP   byte = 13
	packetDISCONNECT byte = 14
)

// protocolLevel4 is MQTT 3.1.1 (§3.1.2.2).
const protocolLevel4 = 4

// encodeRemainingLength writes the §2.2.3 variable-length encoding
// (7 bits per byte, continuation bit 0x80, up to 4 bytes).
func encodeRemainingLength(n int) []byte {
	if n < 0 || n > 268435455 {
		return nil
	}
	var out []byte
	for {
		b := byte(n % 128)
		n /= 128
		if n > 0 {
			b |= 0x80
		}
		out = append(out, b)
		if n == 0 {
			return out
		}
	}
}

// encodeString writes a §1.5.3 UTF-8 encoded string (2-byte length
// prefix, big-endian).
func encodeString(s string) []byte {
	out := make([]byte, 2+len(s))
	binary.BigEndian.PutUint16(out, uint16(len(s)))
	copy(out[2:], s)
	return out
}

// connectPacket builds a CONNECT with a clean session and no will —
// retained messages are how IS-07 handles late joiners, so a will
// would only duplicate the connection-status mechanism the provider
// already publishes explicitly.
func connectPacket(clientID string, keepAliveSec uint16, username, password string) []byte {
	var body []byte
	body = append(body, encodeString("MQTT")...)
	body = append(body, protocolLevel4)
	var flags byte = 0x02 // clean session (§3.1.2.4)
	if username != "" {
		flags |= 0x80
		if password != "" {
			flags |= 0x40
		}
	}
	body = append(body, flags)
	ka := make([]byte, 2)
	binary.BigEndian.PutUint16(ka, keepAliveSec)
	body = append(body, ka...)
	body = append(body, encodeString(clientID)...)
	if username != "" {
		body = append(body, encodeString(username)...)
		if password != "" {
			body = append(body, encodeString(password)...)
		}
	}
	out := []byte{packetCONNECT << 4}
	out = append(out, encodeRemainingLength(len(body))...)
	return append(out, body...)
}

// publishPacket builds a QoS 0 PUBLISH (§3.3). QoS 0 carries no
// packet identifier; retain asks the broker to hand the message to
// every future subscriber — IS-07's late-joiner behaviour.
func publishPacket(topic string, payload []byte, retain bool) []byte {
	fixed := packetPUBLISH << 4
	if retain {
		fixed |= 0x01
	}
	body := encodeString(topic)
	body = append(body, payload...)
	out := []byte{fixed}
	out = append(out, encodeRemainingLength(len(body))...)
	return append(out, body...)
}

func pingreqPacket() []byte    { return []byte{packetPINGREQ << 4, 0} }
func disconnectPacket() []byte { return []byte{packetDISCONNECT << 4, 0} }

// parseConnack checks a CONNACK (§3.2): 4 bytes, return code 0 =
// accepted. Any other return code is the broker refusing us and is
// surfaced verbatim.
func parseConnack(b []byte) error {
	if len(b) != 4 || b[0] != packetCONNACK<<4 || b[1] != 2 {
		return fmt.Errorf("mqtt: malformed CONNACK % x", b)
	}
	if rc := b[3]; rc != 0 {
		return fmt.Errorf("mqtt: connection refused, return code %d", rc)
	}
	return nil
}
