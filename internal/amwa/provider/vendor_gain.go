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
//
// The worker also carries the model's three writable sequences — one
// per datatype shape the MS-05-02 sequence methods must cope with —
// because every sequence on the standard classes is readonly and a
// controller (or the AMWA suite's SetSequenceItem / AddSequenceItem /
// RemoveSequenceItem and writable-sequence rounds) would otherwise
// find nothing to exercise:
//
//	channelLabels (4p4):  NcString sequence, constrained per item (≤16 chars, same pattern)
//	channelModes  (4p5):  DhsChannelMode enum sequence
//	presets       (4p6):  DhsGainPreset struct sequence

package provider

import (
	"sync"

	"dhs/internal/amwa/codec/ms05"
)

const (
	vendorClassName    = "DhsGainControl"
	vendorRole         = "GainControl"
	vendorDatatypeName = "DhsGainDb"

	vendorChannelModeName = "DhsChannelMode"
	vendorGainPresetName  = "DhsGainPreset"
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
			{
				// A deprecated property, as a real device carries one
				// after a firmware generation: the suite's deprecation
				// rounds (IS-12-01 / IS-14-01 "No deprecated properties
				// found") have something to score, and a controller
				// sees how we flag one. Read-only; gainDb supersedes it.
				NcDescriptor: ms05.NcDescriptor{Description: strp("Legacy trim in dB (deprecated: use gainDb)")},
				ID:           ms05.NcPropertyId{Level: 4, Index: 3},
				Name:         "legacyTrim",
				TypeName:     strp("NcFloat64"),
				IsReadOnly:   true,
				IsDeprecated: true,
			},
			{
				// A constrained writable sequence: the constraint
				// governs every item (MS-05-02 Constraints.html), so a
				// whole-sequence Set, SetSequenceItem and AddSequenceItem
				// all answer ParameterError for an item outside it.
				NcDescriptor: ms05.NcDescriptor{Description: strp("Per-channel labels")},
				ID:           ms05.NcPropertyId{Level: 4, Index: 4},
				Name:         "channelLabels",
				TypeName:     strp("NcString"),
				IsSequence:   true,
				Constraints: &ms05.NcParameterConstraintsString{
					MaxCharacters: u32p(16),
					Pattern:       &vendorGainPattern,
				},
			},
			{
				NcDescriptor: ms05.NcDescriptor{Description: strp("Per-channel mode")},
				ID:           ms05.NcPropertyId{Level: 4, Index: 5},
				Name:         "channelModes",
				TypeName:     strp(vendorChannelModeName),
				IsSequence:   true,
			},
			{
				NcDescriptor: ms05.NcDescriptor{Description: strp("Recallable gain presets")},
				ID:           ms05.NcPropertyId{Level: 4, Index: 6},
				Name:         "presets",
				TypeName:     strp(vendorGainPresetName),
				IsSequence:   true,
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

// vendorChannelModeDatatype: DhsChannelMode, the enum behind the
// channelModes sequence. A value outside its items is a ParameterError
// on every write path (the kind check alone would let any number in).
func vendorChannelModeDatatype() ms05.NcDatatypeDescriptor {
	return ms05.NcDatatypeDescriptor{
		NcDescriptor: ms05.NcDescriptor{Description: strp("Operating mode of one channel")},
		Name:         vendorChannelModeName,
		Type:         ms05.NcDatatypeTypeEnum,
		Items: []ms05.NcEnumItemDescriptor{
			{NcDescriptor: ms05.NcDescriptor{Description: strp("Audio passes at gainDb")}, Name: "Normal", Value: 0},
			{NcDescriptor: ms05.NcDescriptor{Description: strp("Channel muted")}, Name: "Muted", Value: 1},
			{NcDescriptor: ms05.NcDescriptor{Description: strp("Only this channel passes")}, Name: "Solo", Value: 2},
		},
	}
}

// vendorGainPresetDatatype: DhsGainPreset, the struct behind the
// presets sequence. Its gainDb field inherits the DhsGainDb datatype
// range; its name field carries a field-level string constraint — the
// two places a struct item's constraints can come from.
func vendorGainPresetDatatype() ms05.NcDatatypeDescriptor {
	return ms05.NcDatatypeDescriptor{
		NcDescriptor: ms05.NcDescriptor{Description: strp("A named gain setting")},
		Name:         vendorGainPresetName,
		Type:         ms05.NcDatatypeTypeStruct,
		Fields: []ms05.NcFieldDescriptor{
			{
				NcDescriptor: ms05.NcDescriptor{Description: strp("Preset name")},
				Name:         "name",
				TypeName:     strp("NcString"),
				Constraints: &ms05.NcParameterConstraintsString{
					MaxCharacters: u32p(32),
					Pattern:       &vendorGainPattern,
				},
			},
			{
				NcDescriptor: ms05.NcDescriptor{Description: strp("Gain the preset recalls")},
				Name:         "gainDb",
				TypeName:     strp(vendorDatatypeName),
			},
		},
	}
}

// vendorGainSequences seeds the three writable sequences. Values are
// stored exactly as a JSON decode would produce them ([]any of
// float64 / string / map[string]any) so a controller's item, written
// back through SetSequenceItem, compares equal to what Get returns.
func vendorGainSequences() map[string]any {
	return map[string]any{
		"channelLabels": []any{"L", "R"},
		"channelModes":  []any{0.0, 0.0},
		"presets": []any{
			map[string]any{"name": "Unity", "gainDb": 0.0},
			map[string]any{"name": "Dim", "gainDb": -20.0},
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
		mustRegister("vendor class", ms05.RegisterClass(vendorGainClass()))
		mustRegister("vendor datatype", ms05.RegisterDatatype(vendorGainDatatype()))
		mustRegister("vendor enum", ms05.RegisterDatatype(vendorChannelModeDatatype()))
		mustRegister("vendor struct", ms05.RegisterDatatype(vendorGainPresetDatatype()))
		mustRegister("fault class", ms05.RegisterClass(vendorFaultClass()))
	})
}

// mustRegister refuses to continue when a compiled-in vendor model
// will not go into the catalogue. Like the framework models, these are
// built into the binary: a failure is a build defect, and a device
// model missing a class a controller is entitled to walk is worse than
// a process that will not start.
func mustRegister(what string, err error) {
	if err != nil {
		panic("provider: " + what + " registration: " + err.Error())
	}
}
