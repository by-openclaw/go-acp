// Package codec is the wire layer of the Nevion Virtuoso MI REST API
// (Nevion Virtuoso MI User Guide, "Guide to the REST API").
//
// One chassis serves one API per module: the Element Manager under /api
// and each extension card under /slot/N/api (N 1..8). A URL path maps 1:1
// to the JSON document. Every answer is the envelope {"data":…,"info":…};
// the object model is served by the device as a JSON schema.
//
// This package holds what crosses the wire and nothing else: the
// envelope, the product identity, the schema and its join with a data
// document, the URL paths, and the query-attribute encoding of a PATCH.
// It is stdlib-only and imports nothing from dhs (ADR-0006).
package codec
