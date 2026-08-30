package is05_test

// BCP-005-03 (IPMX/PEP) IS-05 extended transport parameter tests.
// Oracle = the spec's parameter list, value sets, and the
// privacy-attribute/parameter coupling rules.

import (
	"testing"

	"dhs/internal/amwa/codec/is05"
)

func boolp(b bool) *bool { return &b }

func TestPrivacyLegParamsCore(t *testing.T) {
	p := is05.PrivacyLegParams(false)
	// Six core params, all at the NULL sentinel, no ECDH trio.
	want := []string{
		is05.ParamPrivacyProtocol, is05.ParamPrivacyMode, is05.ParamPrivacyIV,
		is05.ParamPrivacyKeyGenerator, is05.ParamPrivacyKeyVersion, is05.ParamPrivacyKeyID,
	}
	for _, k := range want {
		if v, ok := p[k]; !ok || v != is05.PrivacyNull {
			t.Errorf("core param %s = %v (ok=%v), want %q", k, v, ok, is05.PrivacyNull)
		}
	}
	if _, ok := p[is05.ParamPrivacyECDHCurve]; ok {
		t.Errorf("core-only leg must omit the ECDH trio, found %s", is05.ParamPrivacyECDHCurve)
	}
	if len(p) != 6 {
		t.Errorf("core leg has %d params, want 6", len(p))
	}
}

func TestPrivacyLegParamsECDH(t *testing.T) {
	p := is05.PrivacyLegParams(true)
	for _, k := range []string{
		is05.ParamPrivacyECDHSenderPubKey, is05.ParamPrivacyECDHReceiverPubKey, is05.ParamPrivacyECDHCurve,
	} {
		if v, ok := p[k]; !ok || v != is05.PrivacyNull {
			t.Errorf("ecdh param %s = %v (ok=%v), want %q", k, v, ok, is05.PrivacyNull)
		}
	}
	if len(p) != 9 {
		t.Errorf("ecdh leg has %d params, want 9", len(p))
	}
	if got := len(is05.PrivacyParamKeys(p)); got != 9 {
		t.Errorf("PrivacyParamKeys = %d, want 9", got)
	}
}

func TestPrivacyEnums(t *testing.T) {
	for _, v := range []string{"RTP", "RTP_KV", "USB", "USB_KV", "NULL"} {
		if !is05.IsValidPrivacyProtocol(v) {
			t.Errorf("protocol %q should be valid", v)
		}
	}
	if is05.IsValidPrivacyProtocol("AES") {
		t.Errorf("protocol AES should be invalid")
	}
	for _, v := range []string{"secp256r1", "secp521r1", "25519", "448", "NULL"} {
		if !is05.IsValidPrivacyECDHCurve(v) {
			t.Errorf("curve %q should be valid", v)
		}
	}
	if is05.IsValidPrivacyECDHCurve("p256") {
		t.Errorf("curve p256 should be invalid")
	}
}

func TestValidatePrivacyParams(t *testing.T) {
	cases := []struct {
		name    string
		p       is05.TransportParams
		privacy *bool
		wantErr bool
	}{
		{"nil params", nil, boolp(true), false},
		{"off + all NULL", is05.PrivacyLegParams(false), boolp(false), false},
		{
			"off + non-NULL protocol",
			is05.TransportParams{is05.ParamPrivacyProtocol: "RTP", is05.ParamPrivacyMode: "NULL"},
			boolp(false), true,
		},
		{
			"off + non-NULL mode",
			is05.TransportParams{is05.ParamPrivacyProtocol: "NULL", is05.ParamPrivacyMode: "AES-CTR"},
			boolp(false), true,
		},
		{
			"on + protocol NULL",
			is05.TransportParams{is05.ParamPrivacyProtocol: "NULL"},
			boolp(true), true,
		},
		{
			"on + protocol RTP",
			is05.TransportParams{is05.ParamPrivacyProtocol: "RTP", is05.ParamPrivacyMode: "AES-CTR"},
			boolp(true), false,
		},
		{
			"bad protocol enum",
			is05.TransportParams{is05.ParamPrivacyProtocol: "TWOFISH"},
			nil, true,
		},
		{
			"bad curve enum",
			is05.TransportParams{is05.ParamPrivacyECDHCurve: "p256"},
			nil, true,
		},
		{
			"absent attr + all NULL",
			is05.PrivacyLegParams(true), nil, false,
		},
	}
	for _, tc := range cases {
		err := is05.ValidatePrivacyParams(tc.p, tc.privacy)
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: err=%v, wantErr=%v", tc.name, err, tc.wantErr)
		}
	}
}
