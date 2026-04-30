package spec

// Versioned is the contract every AMWA NMOS codec implementation
// satisfies, regardless of which IS-* / MS-* / BCP-* specification it
// codifies.
//
// SpecID identifies the specification in the AMWA NMOS catalogue using
// its lower-case slug — e.g. "is-04", "is-05", "is-07", "is-08",
// "is-09", "is-12", "ms-05-01", "ms-05-02", "bcp-002-01",
// "bcp-004-02". One SpecID maps to many APIVer values.
//
// APIVer is the wire identifier — major.minor only — as it appears in
// DNS-SD `api_ver` TXT records and in `/x-nmos/<api>/<APIVer>/...`
// URL paths. Always written with a leading "v": "v1.0", "v1.1",
// "v1.3". The plugin layer uses this string verbatim for URL
// dispatch and TXT advertisement.
//
// SpecPatch is the spec-text revision the codec strictly complies
// with — the latest patch within the APIVer minor. It carries three
// components, e.g. "v1.0.2", "v1.1.3", "v1.3.3". Wire URLs do not
// expose the patch level (the spec itself doesn't propagate it), but
// it appears in [ComplianceEvent] reports and in conformance
// audit logs so we can prove which spec text we're testing against.
//
// Implementations must be value-typed and stateless — same instance
// safe to share across goroutines. All three returned strings are
// const for the lifetime of the process.
type Versioned interface {
	SpecID() string
	APIVer() string
	SpecPatch() string
}
