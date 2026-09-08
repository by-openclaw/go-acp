package ccm

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"

	thttp "dhs/internal/transport/http"
)

// defaultReadme is the provider's own tech doc, served rendered at
// /x-dhs/readme when the operator does not point --readme at another file.
//
//go:embed README.md
var defaultReadme []byte

// MountLanding mounts the dhs-only pages under ExtensionPrefix:
//
//	/x-dhs/              the landing — identity, namespaces, the API table
//	                     read from the served OpenAPI, links
//	/x-dhs/readme        the tech doc, Markdown rendered to HTML
//	/x-dhs/capabilities  what this emulation supports, as JSON
//
// readme nil serves the embedded provider README. Everything here is a dhs
// addition: it is mounted through HandleExtension, so it can never appear
// under /api/v1 and a CCM controller's view of the device is unchanged. The
// pages are self-contained (inline CSS, no external assets) because a plant
// network has no internet to fetch a Swagger UI from.
func (s *Server) MountLanding(readme []byte) {
	if readme == nil {
		readme = defaultReadme
	}
	rendered := renderMarkdown(readme)
	s.HandleExtension(http.MethodGet, "", s.handleLanding)
	s.HandleExtension(http.MethodGet, "readme", func(context.Context, *http.Request) (int, any, error) {
		return http.StatusOK, htmlBody(page("dhs CCM device — README", rendered)), nil
	})
	s.HandleExtension(http.MethodGet, "capabilities", s.handleCapabilities)
}

// identity reads the device identity the tree serves at /self, if the capture
// has one. Fields absent from the capture are simply not shown.
func (s *Server) identity() (name, version string, model any) {
	body, kind := s.tree.Get("self")
	if kind != KindResource {
		return "", "", nil
	}
	var self map[string]any
	if err := json.Unmarshal(body, &self); err != nil {
		return "", "", nil
	}
	name, _ = self["productName"].(string)
	version, _ = self["productVersion"].(string)
	return name, version, self["modelVersion"]
}

// capabilities is the /x-dhs/capabilities document: the facts an integrator
// needs to know what this emulation does and does not do, stated from the
// running configuration rather than from prose.
type capabilities struct {
	Protocol        string   `json:"protocol"`
	CCMPrefix       string   `json:"ccm_prefix"`
	ExtensionPrefix string   `json:"extension_prefix"`
	OpenAPIServed   bool     `json:"openapi_served"`
	OpenAPIPath     string   `json:"openapi_path"`
	Resources       int      `json:"resources"`
	Nodes           int      `json:"nodes"`
	TLS             bool     `json:"tls"`
	Writes          []string `json:"writes"`
	WriteResponse   string   `json:"write_response"`
	StatusEndpoints string   `json:"status_endpoints"`
	Matrix          string   `json:"matrix"`
	Events          string   `json:"events"`
}

func (s *Server) caps() capabilities {
	return capabilities{
		Protocol:        "ccm",
		CCMPrefix:       s.prefix,
		ExtensionPrefix: ExtensionPrefix,
		OpenAPIServed:   s.spec != nil,
		OpenAPIPath:     s.prefix + "/" + specPath,
		Resources:       s.tree.Len(),
		Nodes:           s.tree.Nodes(),
		TLS:             s.http.TLS != nil,
		Writes:          []string{"PUT", "PATCH"},
		WriteResponse:   "202 empty (§11.1); confirm by reading the resource or its /status back (§11.3)",
		StatusEndpoints: "GET only (§11.1)",
		Matrix:          "out of scope — routing stays with the controller and the router protocols",
		Events:          "WebSocket /ws not served by this emulation (later unit)",
	}
}

func (s *Server) handleCapabilities(context.Context, *http.Request) (int, any, error) {
	return http.StatusOK, s.caps(), nil
}

// handleLanding renders the landing page.
func (s *Server) handleLanding(context.Context, *http.Request) (int, any, error) {
	var b strings.Builder
	name, version, model := s.identity()
	c := s.caps()

	b.WriteString("<h1>dhs CCM device</h1>\n")
	if name != "" {
		b.WriteString("<p class=id><b>" + html.EscapeString(name) + "</b>")
		if version != "" {
			b.WriteString(" " + html.EscapeString(version))
		}
		if model != nil {
			fmt.Fprintf(&b, " · model %v", model)
		}
		b.WriteString("</p>\n")
	}
	fmt.Fprintf(&b, "<p>%d resources across %d nodes · TLS %s</p>\n",
		c.Resources, c.Nodes, onOff(c.TLS))

	b.WriteString("<h2>Namespaces</h2>\n<table><thead><tr><th>Prefix</th><th>What</th></tr></thead><tbody>\n")
	b.WriteString("<tr><td><code>" + html.EscapeString(s.prefix) + "</code></td><td>The CCM protocol, 100% to the spec — the self-describing tree, the OpenAPI document, §11 PUT/PATCH, §12 errors. A CCM controller sees exactly a CCM device here.</td></tr>\n")
	b.WriteString("<tr><td><code>" + ExtensionPrefix + "</code></td><td>dhs additions only — this page, <a href=\"" + ExtensionPrefix + "/readme\">the README</a>, <a href=\"" + ExtensionPrefix + "/capabilities\">capabilities</a>. Never touches the protocol.</td></tr>\n")
	b.WriteString("</tbody></table>\n")

	b.WriteString("<h2>API</h2>\n")
	if s.spec == nil {
		b.WriteString("<p>No OpenAPI document is being served (start with <code>--api-spec</code> to publish the device's api.yml).</p>\n")
	} else {
		b.WriteString("<p>OpenAPI 3.1 document: <a href=\"" + html.EscapeString(c.OpenAPIPath) + "\"><code>" + html.EscapeString(c.OpenAPIPath) + "</code></a></p>\n")
		rows := specPaths(s.spec)
		if len(rows) > 0 {
			b.WriteString("<table><thead><tr><th>Path</th><th>Methods</th></tr></thead><tbody>\n")
			for _, r := range rows {
				b.WriteString("<tr><td><code>" + html.EscapeString(r.Path) + "</code></td><td>" + html.EscapeString(strings.Join(r.Methods, " ")) + "</td></tr>\n")
			}
			b.WriteString("</tbody></table>\n")
		}
	}

	b.WriteString("<h2>Behaviour</h2>\n<ul>\n")
	b.WriteString("<li>Writes: " + html.EscapeString(strings.Join(c.Writes, ", ")) + " → " + html.EscapeString(c.WriteResponse) + "</li>\n")
	b.WriteString("<li>Status endpoints: " + html.EscapeString(c.StatusEndpoints) + "</li>\n")
	b.WriteString("<li>Matrix: " + html.EscapeString(c.Matrix) + "</li>\n")
	b.WriteString("<li>Events: " + html.EscapeString(c.Events) + "</li>\n")
	b.WriteString("</ul>\n")

	return http.StatusOK, htmlBody(page("dhs CCM device", []byte(b.String()))), nil
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// htmlBody wraps a rendered document as a text/html response.
func htmlBody(doc []byte) *thttp.RawBody {
	return &thttp.RawBody{ContentType: "text/html; charset=utf-8", Body: doc}
}

// page wraps an HTML fragment in a self-contained document. The CSS is inline
// and there are no external assets: the page must render on a plant network
// with no route to the internet.
func page(title string, body []byte) []byte {
	var b strings.Builder
	b.WriteString("<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><title>")
	b.WriteString(html.EscapeString(title))
	b.WriteString("</title><style>")
	b.WriteString("body{font:15px/1.5 system-ui,sans-serif;max-width:900px;margin:32px auto;padding:0 20px;color:#1c2431;background:#f6f7f9}")
	b.WriteString("h1,h2,h3{line-height:1.2}code,pre{font-family:ui-monospace,Menlo,Consolas,monospace;font-size:.92em}")
	b.WriteString("pre{background:#0f1722;color:#d7e2ea;padding:12px 14px;border-radius:8px;overflow-x:auto}")
	b.WriteString("table{border-collapse:collapse;width:100%;margin:8px 0 16px}th,td{text-align:left;padding:6px 8px;border-bottom:1px solid #d9dee6;vertical-align:top}")
	b.WriteString("th{font-size:.85em;text-transform:uppercase;letter-spacing:.06em;color:#4a5568}.id{color:#4a5568}a{color:#0e7c86}")
	b.WriteString("</style></head><body>")
	b.Write(body)
	b.WriteString("</body></html>")
	return []byte(b.String())
}
