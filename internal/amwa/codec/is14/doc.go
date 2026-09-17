// Package is14 — canonical types for AMWA IS-14 Device Configuration
// v1.0 (https://specs.amwa.tv/is-14/releases/v1.0.0/).
//
// IS-14 exposes the MS-05-02 Device Model over REST: every object is
// addressed by its role path (roles joined with "." starting at the
// root block), and the API offers per-property get/set, per-method
// invocation, class/datatype descriptors, and a bulkProperties
// endpoint that carries backup & restore (the device-configuration
// feature set's NcBulkPropertiesHolder machinery).
//
// The object-model vocabulary (NcMethodResult*, descriptors, ids)
// lives in the sibling ms05 package; this package carries only what
// IS-14 and the device-configuration feature set add on top: the
// bulk-properties holders, the restore validation shapes, and the
// three request bodies the RAML defines.
//
// Validation doctrine is the repo-wide one: AMWA's own JSON Schemas
// (schemas/v1.0.0/, verbatim) are the authority; Go code carries the
// canonical shapes and only enforces structure the schemas fix.
package is14
