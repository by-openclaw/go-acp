// Package ms05 implements the AMWA NMOS MS-05-01 (Control
// Architecture) + MS-05-02 (Control Framework) codec. Stdlib-only,
// spec-strict per https://specs.amwa.tv/ms-05-01/ and
// https://specs.amwa.tv/ms-05-02/.
//
// Required versions per `internal/amwa/CLAUDE.md` "Versioning":
//
//   - v1.0 (latest patch v1.0.0) — single track. The control
//     framework defines NcObject (root class), NcBlock, NcWorker,
//     NcManager, NcDeviceManager, NcClassManager, plus the datatype
//     library (~58 NcSomething types) used as the typed value
//     vocabulary in IS-12 commands and notifications.
//
// Layer contract:
//
//   - This package is Layer 1 (codec). It depends only on
//     `internal/amwa/codec/spec/` and Go stdlib.
//   - IS-12 (`internal/amwa/codec/is12/`) carries this package's
//     types as `json.RawMessage` payloads — the IS-12 wire codec
//     deliberately stays datatype-agnostic so dhs can extend the
//     library with private datatypes / classes without re-touching
//     the transport layer. Plug your own ms05.Codec impl into
//     IS-12 message handling and you have a full IS-12 + MS-05-02
//     server.
//
// Type tiers shipped today (everything authored for is12/ts1
// integration; future tiers as integration tests demand them):
//
//	Tier 1 (IDs + status)     — NcId, NcOid, NcClassId, NcElementId,
//	                            NcMethodId, NcPropertyId, NcEventId,
//	                            NcMethodStatus.
//	Tier 2 (results)          — NcMethodResult + variants (Error /
//	                            PropertyValue / Id / Length /
//	                            ClassDescriptor / DatatypeDescriptor /
//	                            BlockMemberDescriptors).
//	Tier 3 (descriptors)      — NcDescriptor base + NcClassDescriptor +
//	                            NcDatatypeDescriptor (+ Primitive /
//	                            Enum / Struct / Typedef variants) +
//	                            NcPropertyDescriptor /
//	                            NcMethodDescriptor /
//	                            NcEventDescriptor /
//	                            NcParameterDescriptor /
//	                            NcFieldDescriptor /
//	                            NcEnumItemDescriptor /
//	                            NcBlockMemberDescriptor.
//	Tier 4 (classes)          — NcObject / NcBlock / NcWorker /
//	                            NcManager / NcDeviceManager /
//	                            NcClassManager.
//	Tier 5 (misc value types) — NcManufacturer, NcProduct,
//	                            NcDeviceOperationalState (+ generic
//	                            state enum), NcResetCause,
//	                            NcTouchpoint variants,
//	                            NcPropertyConstraints + variants,
//	                            NcParameterConstraints +
//	                            number/string variants,
//	                            NcPropertyChangedEventData,
//	                            NcPropertyChangeType.
//
// All 64 reference JSON schemas (58 datatypes + 6 classes) are
// bundled at `testdata/schemas/v1.0.0/` for audit traceability —
// the canonical Go types in this package are byte-encoded against
// them and round-trip clean.
package ms05
