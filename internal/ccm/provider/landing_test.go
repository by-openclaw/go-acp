package ccm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dhs/internal/plugin"
)

const landingSpec = "openapi: '3.1.2'\npaths:\n  /v1/self:\n    get:\n  /v1/io/ip/senders/video/{uuid}:\n    get:\n    put:\n"

func landingServer(t *testing.T, treeDoc string, spec, readme []byte, tls bool) *httptest.Server {
	t.Helper()
	tr, err := LoadTree([]byte(treeDoc))
	if err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	s := NewServer(plugin.Deps{}, tr, spec)
	if tls {
		if err := s.WithTLS(selfSignedOptions(t)); err != nil {
			t.Fatalf("WithTLS: %v", err)
		}
	}
	s.MountLanding(readme)
	hs := httptest.NewServer(s.Handler())
	t.Cleanup(hs.Close)
	return hs
}

// The landing shows the device identity from /self, both namespaces, the API
// table read from the served OpenAPI, and links to the README and
// capabilities — all self-contained HTML.
func TestLandingRendersIdentityNamespacesAndAPITable(t *testing.T) {
	hs := landingServer(t, sampleTree, []byte(landingSpec), nil, true)
	resp, body := get(t, hs, ExtensionPrefix+"/")
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("landing = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	page := string(body)
	for _, want := range []string{
		"<h1>dhs CCM device</h1>",
		"<b>BRIDGE</b> 7.0.2 · model 3",
		"5 resources across 6 nodes · TLS on",
		"<code>/api/v1</code>", "<code>/x-dhs</code>",
		`href="/x-dhs/readme"`, `href="/x-dhs/capabilities"`,
		`href="/api/v1/docs/api.yml"`,
		"<td><code>/v1/self</code></td><td>GET</td>",
		"<td><code>/v1/io/ip/senders/video/{uuid}</code></td><td>GET PUT</td>",
		"Writes: PUT, PATCH",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("landing lacks %q", want)
		}
	}
	if strings.Contains(page, "<script") || strings.Contains(page, "http://") || strings.Contains(page, "https://") {
		t.Error("landing must be self-contained: no scripts, no external URLs")
	}
}

// Without a spec the API section says so; with a spec that declares no paths
// the table is simply omitted; a capture with no /self shows no identity, and
// a /self that is not an object is treated as absent.
func TestLandingDegradesWithoutSpecOrIdentity(t *testing.T) {
	// No spec, no /self, TLS off.
	hs := landingServer(t, `{"processing/mixer":{"name":"m"}}`, nil, nil, false)
	_, body := get(t, hs, ExtensionPrefix+"/")
	page := string(body)
	if !strings.Contains(page, "No OpenAPI document is being served") || strings.Contains(page, `class=id`) || !strings.Contains(page, "TLS off") {
		t.Errorf("degraded landing wrong:\n%s", page)
	}
	// Spec with no paths -> no table; /self without version/model -> name only.
	hs = landingServer(t, `{"self":{"productName":"X"}}`, []byte("openapi: '3.1.2'\n"), nil, false)
	_, body = get(t, hs, ExtensionPrefix+"/")
	page = string(body)
	if strings.Contains(page, "<th>Path</th>") || !strings.Contains(page, "<b>X</b></p>") {
		t.Errorf("landing with pathless spec / bare identity wrong:\n%s", page)
	}
	// /self that is an array is not an identity.
	hs = landingServer(t, `{"self":[1,2]}`, nil, nil, false)
	_, body = get(t, hs, ExtensionPrefix+"/")
	if strings.Contains(string(body), `class=id`) {
		t.Error("a non-object /self must not render as identity")
	}
}

// The README is served rendered: the embedded provider README by default, or
// the operator's own document when one is supplied.
func TestLandingReadmeDefaultAndOverride(t *testing.T) {
	hs := landingServer(t, sampleTree, nil, nil, false)
	resp, body := get(t, hs, ExtensionPrefix+"/readme")
	if resp.StatusCode != 200 || !strings.Contains(string(body), "<h1>dhs CCM device (provider)</h1>") {
		t.Errorf("default README not rendered: %d\n%s", resp.StatusCode, body)
	}
	hs = landingServer(t, sampleTree, nil, []byte("# Site runbook\n\nStep **one**.\n"), false)
	_, body = get(t, hs, ExtensionPrefix+"/readme")
	if !strings.Contains(string(body), "<h1>Site runbook</h1>") || !strings.Contains(string(body), "<strong>one</strong>") {
		t.Errorf("override README not rendered:\n%s", body)
	}
}

// Capabilities is a factual JSON document of what the emulation does.
func TestLandingCapabilitiesJSON(t *testing.T) {
	hs := landingServer(t, sampleTree, []byte(landingSpec), nil, false)
	resp, body := get(t, hs, ExtensionPrefix+"/capabilities")
	if resp.StatusCode != 200 {
		t.Fatalf("capabilities = %d", resp.StatusCode)
	}
	var c capabilities
	if err := json.Unmarshal(body, &c); err != nil {
		t.Fatalf("capabilities not JSON: %v", err)
	}
	if c.Protocol != "ccm" || c.CCMPrefix != "/api/v1" || c.ExtensionPrefix != "/x-dhs" ||
		!c.OpenAPIServed || c.OpenAPIPath != "/api/v1/docs/api.yml" ||
		c.Resources != 5 || c.Nodes != 6 || c.TLS || len(c.Writes) != 2 {
		t.Errorf("capabilities = %+v", c)
	}
}

// The landing pages are dhs additions and live under /x-dhs only: the same
// names under the CCM namespace are ordinary tree lookups (404), so mounting
// the landing changes nothing a controller sees.
func TestLandingNeverLeaksIntoCCMNamespace(t *testing.T) {
	hs := landingServer(t, sampleTree, []byte(landingSpec), nil, false)
	for _, p := range []string{"/api/v1/readme", "/api/v1/capabilities"} {
		resp, body := get(t, hs, p)
		var msg GenericApiMessage
		_ = json.Unmarshal(body, &msg)
		if resp.StatusCode != http.StatusNotFound || msg.Code != 404 {
			t.Errorf("%s = %d %s, want a §12 404 — the landing leaked into /api/v1", p, resp.StatusCode, body)
		}
	}
}
