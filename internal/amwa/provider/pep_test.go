package provider

// BCP-005-03 (NMOS With IPMX/PEP) provider rules: a Sender that declares
// the `privacy` attribute emits an `a=privacy` marker in its SDP and
// publishes the ext_privacy_* IS-05 transport parameters (staged +
// constraints); a Sender without the attribute is byte-for-byte
// unchanged.

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is05"
)

func pepServer(t *testing.T, privacy *bool) *IS05ConnectionServer {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	b := audioBundle()
	b.Senders[0].Privacy = privacy
	cs := NewIS05ConnectionServer(logger, b, IS05ConnectionConfig{APIVer: "v1.2"})
	cs.Store().setNodeIP("10.0.0.7")
	cs.Store().reresolveActive()
	return cs
}

func TestPEPSDPMarkerPresentWhenPrivacyTrue(t *testing.T) {
	on := true
	cs := pepServer(t, &on)
	id := cs.bundle.Senders[0].ID
	e, _ := cs.Store().get("senders", id)
	sdp := cs.sdpForSender(id, e.active)
	if !strings.Contains(sdp, "a=privacy\r\n") {
		t.Errorf("privacy=true sender must emit a=privacy:\n%s", sdp)
	}
}

func TestPEPSDPMarkerAbsentOtherwise(t *testing.T) {
	off := false
	for _, tc := range []struct {
		name string
		priv *bool
	}{
		{"attr false", &off},
		{"attr absent", nil},
	} {
		cs := pepServer(t, tc.priv)
		id := cs.bundle.Senders[0].ID
		e, _ := cs.Store().get("senders", id)
		sdp := cs.sdpForSender(id, e.active)
		if strings.Contains(sdp, "a=privacy") {
			t.Errorf("%s: must NOT emit a=privacy:\n%s", tc.name, sdp)
		}
	}
}

func TestPEPParamsInjectedForPrivacySender(t *testing.T) {
	off := false
	cs := pepServer(t, &off) // attribute present ⇒ PEP-capable ⇒ params published
	id := cs.bundle.Senders[0].ID
	e, _ := cs.Store().get("senders", id)

	coreKeys := []string{
		is05.ParamPrivacyProtocol, is05.ParamPrivacyMode, is05.ParamPrivacyIV,
		is05.ParamPrivacyKeyGenerator, is05.ParamPrivacyKeyVersion, is05.ParamPrivacyKeyID,
	}
	for _, leg := range e.staged.TransportParams {
		for _, k := range coreKeys {
			v, ok := leg[k]
			if !ok {
				t.Errorf("staged leg missing %s", k)
				continue
			}
			// Disabled PEP sender ⇒ every param at the NULL sentinel.
			if v != is05.PrivacyNull {
				t.Errorf("%s = %v, want %q", k, v, is05.PrivacyNull)
			}
		}
	}
	// Constraints must carry the same keys (empty objects, no auto).
	for i, c := range e.constraints {
		for _, k := range coreKeys {
			cv, ok := c[k]
			if !ok {
				t.Errorf("constraints[%d] missing %s", i, k)
				continue
			}
			if m, ok := cv.(map[string]any); !ok || len(m) != 0 {
				t.Errorf("constraints[%d][%s] = %v, want empty object", i, k, cv)
			}
		}
	}
	// The disabled PEP params validate against the attribute.
	for _, leg := range e.staged.TransportParams {
		if err := is05.ValidatePrivacyParams(leg, &off); err != nil {
			t.Errorf("ValidatePrivacyParams: %v", err)
		}
	}
}

func TestPEPParamsAbsentForNonPEPSender(t *testing.T) {
	cs := pepServer(t, nil) // no privacy attribute ⇒ no PEP params
	id := cs.bundle.Senders[0].ID
	e, _ := cs.Store().get("senders", id)
	for _, leg := range e.staged.TransportParams {
		if _, ok := leg[is05.ParamPrivacyProtocol]; ok {
			t.Errorf("non-PEP sender must not carry %s: %v", is05.ParamPrivacyProtocol, leg)
		}
	}
}
