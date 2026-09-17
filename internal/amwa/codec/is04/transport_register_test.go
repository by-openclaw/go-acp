package is04

import "testing"

// TestTransportRegisterIsComplete pins the register against
// specs.amwa.tv/nmos-parameter-registers/branches/main/transports/
// as read on 2026-08-26.
//
// The list lived here as six constants for long enough that ndi and
// usb reached production without anything noticing. Nothing in a spec
// DOCUMENT announces them — from IS-05 v1.2 the transport catalogue
// lives in the Parameter Registers, so a spec-version check can never
// catch a new transport. Only this list can.
func TestTransportRegisterIsComplete(t *testing.T) {
	want := []string{
		"urn:x-nmos:transport:rtp",
		"urn:x-nmos:transport:rtp.mcast",
		"urn:x-nmos:transport:rtp.ucast",
		"urn:x-nmos:transport:dash",
		"urn:x-nmos:transport:websocket",
		"urn:x-nmos:transport:mqtt",
		"urn:x-nmos:transport:ndi",
		"urn:x-nmos:transport:usb",
		"urn:x-nmos:transport:mxl",
	}
	for _, u := range want {
		if !IsNMOSTransport(u) {
			t.Errorf("registered transport %q is not recognised", u)
		}
		if !IsValidTransportURN(u) {
			t.Errorf("registered transport %q fails URN validation", u)
		}
	}
	if len(transportMinIS05) != len(want) {
		t.Errorf("register holds %d transports, expected %d — update this test WITH the register, never around it",
			len(transportMinIS05), len(want))
	}
	// A urn:x-nmos:transport: value that is NOT registered stays
	// invalid: the namespace is reserved, so an unknown sub-token is a
	// typo or an invention, not a vendor extension.
	if IsValidTransportURN("urn:x-nmos:transport:carrier-pigeon") {
		t.Error("an unregistered urn:x-nmos:transport: value must be refused")
	}
	// A vendor URI outside the reserved namespace is allowed.
	if !IsValidTransportURN("urn:x-example:transport:custom") {
		t.Error("a non-NMOS URI is a legitimate vendor extension")
	}
}

// TestTransportVersionGate pins the Upgrade Path rule: an earlier API
// version "MUST NOT list any Senders or Receivers which make use of
// this new transport type". A v1.1 tree offering NDI is
// non-conformant even though the URN itself is perfectly valid.
func TestTransportVersionGate(t *testing.T) {
	for _, tc := range []struct {
		transport string
		apiVer    string
		want      bool
	}{
		// RTP has been there since the beginning.
		{TransportRTP, "v1.0", true},
		{TransportRTP, "v1.2", true},
		// websocket / mqtt arrived with IS-05 v1.1.
		{TransportWebSocket, "v1.0", false},
		{TransportWebSocket, "v1.1", true},
		{TransportMQTT, "v1.0", false},
		{TransportMQTT, "v1.1", true},
		// ndi / usb arrived with IS-05 v1.2 — the gap this closes.
		{TransportNDI, "v1.1", false},
		{TransportNDI, "v1.2", true},
		{TransportUSB, "v1.1", false},
		{TransportUSB, "v1.2", true},
		// mxl (BCP-007-03) arrived with IS-05 v1.2.
		{TransportMXL, "v1.1", false},
		{TransportMXL, "v1.2", true},
	} {
		if got := IsNMOSTransportAt(tc.transport, tc.apiVer); got != tc.want {
			t.Errorf("IsNMOSTransportAt(%q, %q) = %v, want %v", tc.transport, tc.apiVer, got, tc.want)
		}
	}

	// An unrecognised transport is refused at every version.
	if IsNMOSTransportAt("urn:x-nmos:transport:nope", "v1.2") {
		t.Error("an unregistered transport must be refused at every version")
	}

	// An unparseable version degrades to ALLOW, never to silently
	// dropping transports: a caller that cannot say which minor it is
	// speaking should get everything, and be wrong loudly rather than
	// quietly serve a narrowed set.
	if !IsNMOSTransportAt(TransportNDI, "not-a-version") {
		t.Error("an unknown minor must not silently narrow the transport set")
	}
}

// TestNMOSTransportsAtGrowsWithTheMinor: the per-version list is what
// a Node advertises and what a Controller filters against, so its
// length is a spec claim, not a convenience.
func TestNMOSTransportsAtGrowsWithTheMinor(t *testing.T) {
	for _, tc := range []struct {
		apiVer string
		want   int
	}{
		{"v1.0", 4}, // rtp, rtp.mcast, rtp.ucast, dash
		{"v1.1", 6}, // + websocket, mqtt
		{"v1.2", 9}, // + ndi, usb, mxl
	} {
		if got := len(NMOSTransportsAt(tc.apiVer)); got != tc.want {
			t.Errorf("NMOSTransportsAt(%q) has %d transports, want %d: %v",
				tc.apiVer, got, tc.want, NMOSTransportsAt(tc.apiVer))
		}
	}
}
