package emberplus

import "strings"

// CommandKind groups Ember+ catalogue entries. Unlike byte-cmd
// protocols (Probel, ACP1/2), Ember+ Glow uses BER CHOICE-of-tag —
// there's no "command byte 7" to look up. Instead the protocol model
// surfaces as Glow element kinds + Command verbs + path addressing.
//
// `dhs help-cmd emberplus <addr>` accepts either a `<kind>:<name>`
// shape (e.g. `kind:Parameter`, `cmd:GetDirectory`) OR a numeric OID
// path (`1.2.4.1.0.2`) OR a dotted label path (`root.foo.bar`).
type CommandKind string

const (
	KindElement CommandKind = "kind"  // Glow element kind (Parameter, Node, …)
	KindCommand CommandKind = "cmd"   // Glow Command verb (GetDirectory, Subscribe, …)
	KindOID     CommandKind = "oid"   // numeric OID path (e.g. 1.2.4.1.0.2)
	KindPath    CommandKind = "path"  // dotted-label path (e.g. root.foo.bar)
)

// CatalogueEntry is the structured row used by the CLI catalogue
// renderers (`dhs list-commands emberplus`, `dhs help-cmd emberplus
// <addr>`).
type CatalogueEntry struct {
	Kind    CommandKind
	Name    string
	SpecRef string
	Notes   string
}

// Address returns the canonical "<kind>:<name>" string used by
// `dhs help-cmd emberplus <addr>`. For OID and Path kinds the user
// passes the path directly without a `kind:` prefix.
func (c CatalogueEntry) Address() string {
	return string(c.Kind) + ":" + c.Name
}

// Catalogue returns every Ember+ catalogue entry the consumer knows
// at the protocol-model level — Glow element kinds plus the Command
// verbs. OID paths and dotted-label paths address INSTANCES inside a
// connected device's tree, not protocol-model entries; they're
// resolvable via `dhs help-cmd emberplus <path>` against a captured
// tree.json file (out of scope for this static catalogue).
//
// Source of truth: Ember+ Documentation.pdf §Glow + ASN.1 schema in
// internal/emberplus/codec/glow/tags.go.
func Catalogue() []CatalogueEntry {
	return []CatalogueEntry{
		// Glow element kinds (top-level CHOICE alternatives in the Glow tree)
		{Kind: KindElement, Name: "Parameter", SpecRef: "Glow §3.2.1", Notes: "leaf with typed value (int / real / string / boolean / octets / enum)"},
		{Kind: KindElement, Name: "QualifiedParameter", SpecRef: "Glow §3.2.2", Notes: "Parameter addressed by full OID path"},
		{Kind: KindElement, Name: "Node", SpecRef: "Glow §3.2.3", Notes: "container with children"},
		{Kind: KindElement, Name: "QualifiedNode", SpecRef: "Glow §3.2.4", Notes: "Node addressed by full OID path"},
		{Kind: KindElement, Name: "Function", SpecRef: "Glow §3.2.5", Notes: "RPC-callable; arguments + result"},
		{Kind: KindElement, Name: "QualifiedFunction", SpecRef: "Glow §3.2.6", Notes: "Function addressed by full OID path"},
		{Kind: KindElement, Name: "Matrix", SpecRef: "Glow §3.2.7", Notes: "src/dst routing matrix; sub-elements: Sources / Targets / Connections / Labels / Parameters"},
		{Kind: KindElement, Name: "QualifiedMatrix", SpecRef: "Glow §3.2.8", Notes: "Matrix addressed by full OID path"},
		{Kind: KindElement, Name: "StreamCollection", SpecRef: "Glow §3.3.1", Notes: "stream entries (audio meters, telemetry)"},
		{Kind: KindElement, Name: "StreamEntry", SpecRef: "Glow §3.3.2", Notes: "single stream payload referencing a Parameter by OID"},
		{Kind: KindElement, Name: "Template", SpecRef: "Glow §3.4", Notes: "shared sub-tree referenced by multiple Nodes"},
		{Kind: KindElement, Name: "QualifiedTemplate", SpecRef: "Glow §3.4", Notes: "Template addressed by full OID path"},
		{Kind: KindElement, Name: "Invocation", SpecRef: "Glow §3.2.5", Notes: "Function call descriptor + argument tuple"},
		{Kind: KindElement, Name: "InvocationResult", SpecRef: "Glow §3.2.5", Notes: "Function call return value"},
		{Kind: KindElement, Name: "ElementCollection", SpecRef: "Glow §3.1", Notes: "ordered list of any Glow element"},

		// Glow Command verbs (subscribe / getDirectory / invoke)
		{Kind: KindCommand, Name: "GetDirectory", SpecRef: "Glow §3.5", Notes: "1 — request a node's children (depth-1)"},
		{Kind: KindCommand, Name: "Subscribe", SpecRef: "Glow §3.5", Notes: "30 — register for parameter value-change events"},
		{Kind: KindCommand, Name: "Unsubscribe", SpecRef: "Glow §3.5", Notes: "31 — unregister"},
		{Kind: KindCommand, Name: "Invoke", SpecRef: "Glow §3.5", Notes: "33 — call a Function with an Invocation"},
	}
}

// LookupCatalogue returns the entry matching `<addr>`. Accepted
// shapes:
//
//	kind:Parameter         → static Glow element kind entry
//	cmd:GetDirectory       → static Glow Command verb entry
//	1.2.4.1.0.2            → numeric OID path (synthetic entry)
//	root.foo.bar           → dotted label path (synthetic entry)
//
// Path-based addresses return a synthetic CatalogueEntry pointing the
// user at the live-tree query path; resolving them to actual Glow
// elements requires a connected device or a captured tree.json.
func LookupCatalogue(addr string) (CatalogueEntry, bool) {
	if i := strings.IndexByte(addr, ':'); i >= 0 {
		k := CommandKind(addr[:i])
		name := addr[i+1:]
		for _, e := range Catalogue() {
			if e.Kind == k && e.Name == name {
				return e, true
			}
		}
		return CatalogueEntry{}, false
	}
	// No `kind:` prefix → infer from shape. All-digits-and-dots = OID.
	if isOIDPath(addr) {
		return CatalogueEntry{
			Kind: KindOID, Name: addr,
			SpecRef: "Glow §3 — OID-path addressing",
			Notes:   "live-tree address; resolve via `dhs consumer emberplus walk` or a captured tree.json",
		}, true
	}
	if isDottedPath(addr) {
		return CatalogueEntry{
			Kind: KindPath, Name: addr,
			SpecRef: "canonical-schema dotted path",
			Notes:   "live-tree address; resolve via `dhs consumer emberplus walk` or a captured tree.json",
		}, true
	}
	return CatalogueEntry{}, false
}

// isOIDPath reports whether s contains only ASCII digits and dots.
func isOIDPath(s string) bool {
	if s == "" {
		return false
	}
	hadDigit := false
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
			hadDigit = true
		case c == '.':
			// allowed
		default:
			return false
		}
	}
	return hadDigit
}

// isDottedPath reports whether s looks like a canonical dotted label
// path (alphanumeric segments + underscore / hyphen, separated by
// dots; at least one dot to disambiguate from a single identifier).
func isDottedPath(s string) bool {
	if s == "" {
		return false
	}
	hasDot := false
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '_' || c == '-':
		case c == '.':
			hasDot = true
		default:
			return false
		}
	}
	return hasDot
}
