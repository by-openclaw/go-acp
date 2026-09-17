// The DhsGainControl worker — the device model's non-standard class
// (MS-05-02 authority-key 0, experimental) and the carrier of its
// constraints surface.
//
// MS-05-02 freezes the standard classes: adding a constraint to, say,
// NcObject.userLabel would make our ClassManager catalogue disagree
// with the published models (the AMWA suite compares them verbatim).
// Constrained properties therefore live on a vendor worker, exactly
// as real controlled devices declare theirs. All three constraint
// levels are declared AND enforced (setProperty / restore), each
// level strictly tighter than the one it overrides:
//
//	gainDb (4p2, DhsGainDb):  datatype −120..12 /0.5 → property −90..12 /0.5 → runtime −60..6 /0.5
//	channelLabel (4p1):       property ≤32 chars      → runtime ≤16 chars     (same pattern)
//
// SetGainDb (4m1) carries the parameter-constraints declaration and
// routes through the same setProperty enforcement.

package provider

import (
	"sync"

	"dhs/internal/amwa/codec/ms05"
)

const (
	vendorClassName    = "DhsGainControl"
	vendorRole         = "GainControl"
	vendorDatatypeName = "DhsGainDb"
)

// vendorClassID: {1,2} = NcWorker, authority key 0 (no registered
// OUI), class 1 under that authority.
var vendorClassID = ms05.NcClassId{1, 2, 0, 1}

var vendorGainPattern = "^[A-Za-z0-9 _-]*$"

func strp(s string) *string { return &s }
func u32p(v uint32) *uint32 { return &v }

// vendorGainParamConstraints is the SetGainDb parameter constraint —
// the property-level range, restated at the parameter (MS-05-02
// permits either; the AMWA suite validates both declarations).
func vendorGainParamConstraints() *ms05.NcParameterConstraintsNumber {
	return &ms05.NcParameterConstraintsNumber{
		NcParameterConstraints: ms05.NcParameterConstraints{DefaultValue: 0.0},
		Minimum:                -90.0,
		Maximum:                12.0,
		Step:                   0.5,
	}
}

// vendorGainClass is the DhsGainControl descriptor (own elements
// only — inheritance stays in the catalogue via classId prefixes,
// like every published model file).
func vendorGainClass() ms05.NcClassDescriptor {
	return ms05.NcClassDescriptor{
		NcDescriptor: ms05.NcDescriptor{Description: strp("dhs example gain control (constraints reference)")},
		ClassID:      vendorClassID,
		Name:         vendorClassName,
		FixedRole:    strp(vendorRole),
		Properties: []ms05.NcPropertyDescriptor{
			{
				NcDescriptor: ms05.NcDescriptor{Description: strp("Channel label")},
				ID:           ms05.NcPropertyId{Level: 4, Index: 1},
				Name:         "channelLabel",
				TypeName:     strp("NcString"),
				Constraints: &ms05.NcParameterConstraintsString{
					NcParameterConstraints: ms05.NcParameterConstraints{DefaultValue: "Gain"},
					MaxCharacters:          u32p(32),
					Pattern:                &vendorGainPattern,
				},
			},
			{
				NcDescriptor: ms05.NcDescriptor{Description: strp("Gain in dB")},
				ID:           ms05.NcPropertyId{Level: 4, Index: 2},
				Name:         "gainDb",
				TypeName:     strp(vendorDatatypeName),
				Constraints: &ms05.NcParameterConstraintsNumber{
					NcParameterConstraints: ms05.NcParameterConstraints{DefaultValue: 0.0},
					Minimum:                -90.0,
					Maximum:                12.0,
					Step:                   0.5,
				},
			},
		},
		Methods: []ms05.NcMethodDescriptor{
			{
				NcDescriptor:   ms05.NcDescriptor{Description: strp("Set the gain")},
				ID:             ms05.NcMethodId{Level: 4, Index: 1},
				Name:           "SetGainDb",
				ResultDatatype: "NcMethodResult",
				Parameters: []ms05.NcParameterDescriptor{
					{
						NcDescriptor: ms05.NcDescriptor{Description: strp("Gain in dB")},
						Name:         "gainDb",
						TypeName:     strp(vendorDatatypeName),
						Constraints:  vendorGainParamConstraints(),
					},
				},
			},
		},
		Events: []ms05.NcEventDescriptor{},
	}
}

// vendorGainDatatype: DhsGainDb, a typedef of NcFloat64 carrying the
// widest (datatype-level) range.
func vendorGainDatatype() ms05.NcDatatypeDescriptor {
	return ms05.NcDatatypeDescriptor{
		NcDescriptor: ms05.NcDescriptor{Description: strp("Gain in decibels")},
		Name:         vendorDatatypeName,
		Type:         ms05.NcDatatypeTypeTypedef,
		ParentType:   strp("NcFloat64"),
		Constraints: &ms05.NcParameterConstraintsNumber{
			NcParameterConstraints: ms05.NcParameterConstraints{DefaultValue: 0.0},
			Minimum:                -120.0,
			Maximum:                12.0,
			Step:                   0.5,
		},
	}
}

// vendorRuntimeConstraints seeds NcObject.runtimePropertyConstraints
// on the gain worker — the tightest level of the hierarchy.
func vendorRuntimeConstraints() []any {
	return []any{
		&ms05.NcPropertyConstraintsString{
			NcPropertyConstraints: ms05.NcPropertyConstraints{
				PropertyId:   ms05.NcPropertyId{Level: 4, Index: 1},
				DefaultValue: "Gain",
			},
			MaxCharacters: u32p(16),
			Pattern:       &vendorGainPattern,
		},
		&ms05.NcPropertyConstraintsNumber{
			NcPropertyConstraints: ms05.NcPropertyConstraints{
				PropertyId:   ms05.NcPropertyId{Level: 4, Index: 2},
				DefaultValue: 0.0,
			},
			Minimum: -60.0,
			Maximum: 6.0,
			Step:    0.5,
		},
	}
}

var vendorRegisterOnce sync.Once

// registerVendorModels puts the gain class + datatype into the ms05
// catalogue — before any ClassManager snapshot or model object is
// built from it.
func registerVendorModels() {
	vendorRegisterOnce.Do(func() {
		if err := ms05.RegisterClass(vendorGainClass()); err != nil {
			panic("provider: vendor class registration: " + err.Error())
		}
		if err := ms05.RegisterDatatype(vendorGainDatatype()); err != nil {
			panic("provider: vendor datatype registration: " + err.Error())
		}
		if err := ms05.RegisterClass(vendorFaultClass()); err != nil {
			panic("provider: fault class registration: " + err.Error())
		}
	})
}
