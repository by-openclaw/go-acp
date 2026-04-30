package ms05

// NcManufacturer identifies the device's manufacturer.
type NcManufacturer struct {
	Name             string  `json:"name"`
	OrganizationID   *int32  `json:"organizationId"`
	Website          *string `json:"website"`
}

// NcProduct describes the product / model.
type NcProduct struct {
	Name           string  `json:"name"`
	Key            string  `json:"key"`
	RevisionLevel  string  `json:"revisionLevel"`
	BrandName      *string `json:"brandName"`
	UUID           *string `json:"uuid"`
	Description    *string `json:"description"`
}

// NcDeviceOperationalState carries the device's current operational
// state for NcDeviceManager.operationalState.
type NcDeviceOperationalState struct {
	Generic               NcDeviceGenericState `json:"generic"`
	DeviceSpecificDetails *string              `json:"deviceSpecificDetails"`
}

// NcTouchpoint is the abstract base of every touchpoint. Concrete
// variants (NcTouchpointNmos, NcTouchpointNmosChannelMapping) embed
// this with their own resource entries.
type NcTouchpoint struct {
	ContextNamespace string `json:"contextNamespace"`
}

// NcTouchpointResource is the abstract resource shape inside a
// concrete touchpoint.
type NcTouchpointResource struct {
	ResourceType string `json:"resourceType"`
}

// NcTouchpointNmos is the IS-04 touchpoint variant — links an
// NcObject to an IS-04 resource by UUID.
type NcTouchpointNmos struct {
	NcTouchpoint
	Resource NcTouchpointResourceNmos `json:"resource"`
}

// NcTouchpointResourceNmos extends NcTouchpointResource with the
// IS-04 resource UUID.
type NcTouchpointResourceNmos struct {
	NcTouchpointResource
	ID string `json:"id"`
}

// NcTouchpointNmosChannelMapping is the IS-08 touchpoint variant.
type NcTouchpointNmosChannelMapping struct {
	NcTouchpoint
	Resource NcTouchpointResourceNmosChannelMapping `json:"resource"`
}

// NcTouchpointResourceNmosChannelMapping extends
// NcTouchpointResourceNmos with the IS-08 io-id pair.
type NcTouchpointResourceNmosChannelMapping struct {
	NcTouchpointResourceNmos
	IoID string `json:"ioId"`
}

// NcParameterConstraints is the abstract base of every parameter
// constraint. Concrete variants narrow per type (number / string).
//
// `defaultValue` is left polymorphic — callers cast per the bound
// datatype.
type NcParameterConstraints struct {
	DefaultValue any `json:"defaultValue"`
}

// NcParameterConstraintsNumber narrows for numeric parameters.
type NcParameterConstraintsNumber struct {
	NcParameterConstraints
	Maximum any `json:"maximum"`
	Minimum any `json:"minimum"`
	Step    any `json:"step"`
}

// NcParameterConstraintsString narrows for string parameters.
type NcParameterConstraintsString struct {
	NcParameterConstraints
	MaxCharacters *uint32 `json:"maxCharacters"`
	Pattern       *string `json:"pattern"`
}

// NcPropertyConstraints is the abstract base of every property-level
// constraint (mirrors NcParameterConstraints but applied to property
// values).
type NcPropertyConstraints struct {
	PropertyId   NcPropertyId `json:"propertyId"`
	DefaultValue any          `json:"defaultValue"`
}

// NcPropertyConstraintsNumber narrows for numeric properties.
type NcPropertyConstraintsNumber struct {
	NcPropertyConstraints
	Maximum any `json:"maximum"`
	Minimum any `json:"minimum"`
	Step    any `json:"step"`
}

// NcPropertyConstraintsString narrows for string properties.
type NcPropertyConstraintsString struct {
	NcPropertyConstraints
	MaxCharacters *uint32 `json:"maxCharacters"`
	Pattern       *string `json:"pattern"`
}

// NcRegex is a typedef alias for a regex pattern string. Kept named
// so call sites read against the spec.
type NcRegex = string
