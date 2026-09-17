package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadPolicyRejectsUntrustworthyFiles: a policy nobody can trust
// is worse than none, so every malformed input is a load error that
// names what is wrong.
func TestLoadPolicyRejectsUntrustworthyFiles(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		body string // "" means the file is not written at all
		want string
	}{
		{"missing file is an error", "", "audit policy"},
		{"malformed JSON is an error", `{"multicast_classes": [`, "audit policy"},
		{"domain above 127 is out of range", `{"expected_domain": 128}`, "out of range"},
		{"negative domain is out of range", `{"expected_domain": -1}`, "out of range"},
		{"grandmaster must be EUI-64", `{"expected_grandmaster": "aa-bb-cc"}`, "not EUI-64"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "_")+".json")
			if tc.body != "" {
				if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := LoadPolicy(path)
			if err == nil {
				t.Fatal("LoadPolicy accepted the file")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q should mention %q", err, tc.want)
			}
		})
	}
}

func boolp(b bool) *bool { return &b }

// TestPrivateMediaPlanePolicy: with an expectation stated, a source
// address on the wrong side of it is a finding; without one the check
// stands down as SKIP, never PASS.
func TestPrivateMediaPlanePolicy(t *testing.T) {
	private := netHarvest(map[string]activeEndpoint{"aaaa": ep("239.10.0.5", "10.20.30.40")}, nil, nil)
	public := netHarvest(map[string]activeEndpoint{"bbbb": ep("239.10.0.6", "198.51.100.7")}, nil, nil)

	wantPrivate := &Policy{PrivateMediaPlane: boolp(true)}
	f := has(t, public.checkUnicastSourceIPs(wantPrivate), "NMOS-NET-SRC-SCOPE")
	if f.Severity != SevError || strings.HasPrefix(f.Detail, "SKIP:") {
		t.Errorf("a routable source on a plant declared private = %+v, want ERROR", f)
	}
	for _, x := range private.checkUnicastSourceIPs(wantPrivate) {
		if x.Code == "NMOS-NET-SRC-SCOPE" {
			t.Errorf("a private source on a plant declared private was reported: %s", x.Detail)
		}
	}

	wantPublic := &Policy{PrivateMediaPlane: boolp(false)}
	f = has(t, private.checkUnicastSourceIPs(wantPublic), "NMOS-NET-SRC-SCOPE")
	if f.Severity != SevWarn {
		t.Errorf("an RFC 1918 source on a plant declared routable = %s, want WARN", f.Severity)
	}

	// A policy that says nothing about scope skips, like no policy.
	f = has(t, private.checkUnicastSourceIPs(&Policy{}), "NMOS-NET-SRC-SCOPE")
	if !strings.HasPrefix(f.Detail, "SKIP:") {
		t.Errorf("unspecified scope must SKIP, got %q", f.Detail)
	}
}

// TestMixedScopeIsAlwaysFlagged: one device sourcing from both RFC 1918
// and globally-routable addresses is worth a flag with or without a
// policy — a media network is one or the other.
func TestMixedScopeIsAlwaysFlagged(t *testing.T) {
	h := netHarvest(map[string]activeEndpoint{
		"aaaa": ep("239.10.0.5", "10.20.30.40"),
		"bbbb": ep("239.10.0.6", "198.51.100.7"),
	}, nil, nil)
	has(t, h.checkUnicastSourceIPs(nil), "NMOS-NET-SRC-MIXED-SCOPE")
}

// TestUnclassedAdminScopedDestination: a policy with a class map is a
// statement that every admin-scoped group belongs to a class. One that
// falls outside every range is admitted nowhere by the fabric.
func TestUnclassedAdminScopedDestination(t *testing.T) {
	pol := &Policy{MulticastClasses: []MulticastClass{
		{Range: "239.6.0.0/16", Class: "uhd", MaxBitrateGbps: 12, ipnet: mustCIDR("239.6.0.0/16")},
	}}
	if got := pol.classify(mustCIDR("239.10.0.5/32").IP); got != nil {
		t.Fatalf("239.10.0.5 classified as %q, want no class", got.Class)
	}
	h := netHarvest(map[string]activeEndpoint{"aaaa": ep("239.10.0.5", "10.20.30.40")}, nil, nil)
	fs := h.checkMulticastRanges(pol)
	f := has(t, fs, "NMOS-NET-MCAST-UNCLASSED")
	if f.Severity != SevWarn || !strings.Contains(f.Detail, "239.10.0.5") {
		t.Errorf("unclassed finding = %+v", f)
	}
	// The policy was consulted, so the class check did not stand down.
	for _, x := range fs {
		if strings.HasPrefix(x.Detail, "SKIP:") {
			t.Errorf("a policy-backed run must not SKIP: %s", x.Detail)
		}
	}
}
