package ms05

// NcId is the per-sequence-item identity handler. NcUint32 alias.
// Spec: datatypes/NcId.json.
type NcId = uint32

// NcOid is the per-instance object identifier — every block member
// has a unique OID inside its Device. NcUint32 alias.
type NcOid = uint32

// NcClassId is the hierarchical class identifier. The first level
// is always 1 (NcObject root); subsequent levels narrow toward the
// concrete class.
//
// Examples:
//
//	[1]            — NcObject
//	[1, 1]         — NcBlock
//	[1, 2]         — NcWorker (abstract)
//	[1, 3]         — NcManager (abstract)
//	[1, 3, 1]     — NcDeviceManager
//	[1, 3, 2]     — NcClassManager
//
// Negative values mark authority-private extensions per MS-05-02
// §10.4.
type NcClassId = []int32

// NcElementId is the {level, index} identifier shared by methods,
// properties, and events. Level 1 = NcObject members; level 2 =
// the first subclass that declares the element; etc.
//
// Spec: datatypes/NcElementId.json.
type NcElementId struct {
	Level uint16 `json:"level"`
	Index uint16 `json:"index"`
}

// NcMethodId / NcPropertyId / NcEventId are structurally identical
// to NcElementId — separate aliases keep call sites self-documenting.
type NcMethodId = NcElementId
type NcPropertyId = NcElementId
type NcEventId = NcElementId

// NcMethodStatus is the integer status code returned in every
// NcMethodResult. 200 = OK; 4xx and 5xx mirror HTTP semantics.
//
// Spec: datatypes/NcMethodStatus.json.
type NcMethodStatus uint16

// Recognised NcMethodStatus values per MS-05-02 v1.0.0.
const (
	NcMethodStatusOk                    NcMethodStatus = 200
	NcMethodStatusPropertyDeprecated    NcMethodStatus = 298
	NcMethodStatusMethodDeprecated      NcMethodStatus = 299
	NcMethodStatusBadCommandFormat      NcMethodStatus = 400
	NcMethodStatusUnauthorized          NcMethodStatus = 401
	NcMethodStatusBadOid                NcMethodStatus = 404
	NcMethodStatusReadonly              NcMethodStatus = 405
	NcMethodStatusInvalidRequest        NcMethodStatus = 406
	NcMethodStatusConflict              NcMethodStatus = 409
	NcMethodStatusBufferOverflow        NcMethodStatus = 413
	NcMethodStatusIndexOutOfBounds      NcMethodStatus = 414
	NcMethodStatusParameterError        NcMethodStatus = 417
	NcMethodStatusLocked                NcMethodStatus = 423
	NcMethodStatusDeviceError           NcMethodStatus = 500
	NcMethodStatusMethodNotImplemented  NcMethodStatus = 501
	NcMethodStatusPropertyNotImplemented NcMethodStatus = 502
	NcMethodStatusNotReady              NcMethodStatus = 503
	NcMethodStatusTimeout               NcMethodStatus = 504
	NcMethodStatusProtocolVersionError  NcMethodStatus = 505
)

// NcDatatypeType is the kind discriminator on NcDatatypeDescriptor.
type NcDatatypeType uint8

const (
	NcDatatypeTypePrimitive NcDatatypeType = 0
	NcDatatypeTypeTypedef   NcDatatypeType = 1
	NcDatatypeTypeStruct    NcDatatypeType = 2
	NcDatatypeTypeEnum      NcDatatypeType = 3
)

// NcResetCause enumerates the reason a Device last restarted.
type NcResetCause uint8

const (
	NcResetCauseUnknown           NcResetCause = 0
	NcResetCausePowerOn           NcResetCause = 1
	NcResetCauseInternalError     NcResetCause = 2
	NcResetCauseUpgrade           NcResetCause = 3
	NcResetCauseControllerRequest NcResetCause = 4
	NcResetCauseManualReset       NcResetCause = 5
)

// NcDeviceGenericState classifies whole-device operational status.
type NcDeviceGenericState uint8

const (
	NcDeviceGenericStateUnknown         NcDeviceGenericState = 0
	NcDeviceGenericStateNormalOperation NcDeviceGenericState = 1
	NcDeviceGenericStateInitializing    NcDeviceGenericState = 2
	NcDeviceGenericStateUpdating        NcDeviceGenericState = 3
	NcDeviceGenericStateLicensingError  NcDeviceGenericState = 4
	NcDeviceGenericStateInternalError   NcDeviceGenericState = 5
)

// NcPropertyChangeType classifies the kind of property update fired
// in NcPropertyChangedEventData.changeType.
type NcPropertyChangeType uint8

const (
	NcPropertyChangeTypeValueChanged        NcPropertyChangeType = 0
	NcPropertyChangeTypeSequenceItemAdded   NcPropertyChangeType = 1
	NcPropertyChangeTypeSequenceItemChanged NcPropertyChangeType = 2
	NcPropertyChangeTypeSequenceItemRemoved NcPropertyChangeType = 3
)
