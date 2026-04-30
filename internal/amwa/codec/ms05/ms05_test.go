package ms05

import (
	"encoding/json"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func TestNcMethodStatusValues(t *testing.T) {
	cases := map[NcMethodStatus]string{
		NcMethodStatusOk:                 "200",
		NcMethodStatusBadCommandFormat:   "400",
		NcMethodStatusBadOid:             "404",
		NcMethodStatusDeviceError:        "500",
		NcMethodStatusProtocolVersionError: "505",
	}
	for s, want := range cases {
		got, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("marshal %v: %v", s, err)
		}
		if string(got) != want {
			t.Errorf("status %d marshalled as %s, want %s", int(s), got, want)
		}
	}
}

func TestNcMethodResultRoundTrip(t *testing.T) {
	in := NcMethodResultPropertyValue{
		Status: NcMethodStatusOk,
		Value:  json.RawMessage(`42`),
	}
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got NcMethodResultPropertyValue
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Status != NcMethodStatusOk {
		t.Fatalf("status mismatch")
	}
	if string(got.Value) != "42" {
		t.Fatalf("value mismatch: %s", got.Value)
	}
}

func TestNcMethodResultErrorRoundTrip(t *testing.T) {
	in := NcMethodResultError{
		Status:       NcMethodStatusBadOid,
		ErrorMessage: "no such object",
	}
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got NcMethodResultError
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Status != NcMethodStatusBadOid {
		t.Fatalf("status mismatch")
	}
	if got.ErrorMessage != "no such object" {
		t.Fatalf("error message mismatch")
	}
}

func TestNcElementIdMarshalling(t *testing.T) {
	in := NcElementId{Level: 1, Index: 2}
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got NcElementId
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got != in {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestNcClassDescriptorRoundTrip(t *testing.T) {
	in := NcClassDescriptor{
		NcDescriptor: NcDescriptor{Description: ptr("NcObject class descriptor")},
		ClassID:      NcClassId{1},
		Name:         "NcObject",
		Properties: []NcPropertyDescriptor{
			{
				NcDescriptor: NcDescriptor{Description: ptr("Class id")},
				ID:           NcPropertyId{Level: 1, Index: 1},
				Name:         "classId",
				TypeName:     ptr("NcClassId"),
				IsReadOnly:   true,
			},
		},
		Methods: []NcMethodDescriptor{
			{
				NcDescriptor:   NcDescriptor{Description: ptr("Get property")},
				ID:             NcMethodId{Level: 1, Index: 1},
				Name:           "Get",
				ResultDatatype: "NcMethodResultPropertyValue",
				Parameters: []NcParameterDescriptor{
					{Name: "id", TypeName: ptr("NcPropertyId")},
				},
			},
		},
		Events: []NcEventDescriptor{
			{
				NcDescriptor:  NcDescriptor{Description: ptr("Property changed")},
				ID:            NcEventId{Level: 1, Index: 1},
				Name:          "PropertyChanged",
				EventDatatype: "NcPropertyChangedEventData",
			},
		},
	}
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got NcClassDescriptor
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Name != "NcObject" {
		t.Fatalf("name mismatch")
	}
	if len(got.ClassID) != 1 || got.ClassID[0] != 1 {
		t.Fatalf("classId mismatch: %+v", got.ClassID)
	}
	if len(got.Properties) != 1 {
		t.Fatalf("properties length mismatch")
	}
}

func TestNcDatatypeDescriptorEnum(t *testing.T) {
	in := NcDatatypeDescriptor{
		Name: "NcMethodStatus",
		Type: NcDatatypeTypeEnum,
		Items: []NcEnumItemDescriptor{
			{Name: "Ok", Value: 200},
			{Name: "BadOid", Value: 404},
		},
	}
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got NcDatatypeDescriptor
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Type != NcDatatypeTypeEnum {
		t.Fatalf("type mismatch")
	}
	if len(got.Items) != 2 {
		t.Fatalf("items length mismatch")
	}
}

func TestNcDatatypeDescriptorTypedef(t *testing.T) {
	in := NcDatatypeDescriptor{
		Name:       "NcId",
		Type:       NcDatatypeTypeTypedef,
		ParentType: ptr("NcUint32"),
		IsSequence: false,
	}
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got NcDatatypeDescriptor
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.ParentType == nil || *got.ParentType != "NcUint32" {
		t.Fatalf("parent type mismatch")
	}
}

func TestNcBlockMemberDescriptorRoundTrip(t *testing.T) {
	in := NcBlockMemberDescriptor{
		Role:        "deviceManager",
		Oid:         1,
		ConstantOid: true,
		ClassID:     NcClassId{1, 3, 1},
		UserLabel:   ptr("Device Manager"),
		Owner:       0,
	}
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got NcBlockMemberDescriptor
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(got.ClassID) != 3 || got.ClassID[2] != 1 {
		t.Fatalf("classId mismatch: %+v", got.ClassID)
	}
}

func TestNcDeviceManagerRoundTrip(t *testing.T) {
	in := NcDeviceManager{
		NcManager: NcManager{
			NcObject: NcObject{
				ClassID:     NcClassId{1, 3, 1},
				Oid:         1,
				ConstantOid: true,
				Owner:       func() *NcOid { v := NcOid(0); return &v }(),
				Role:        "deviceManager",
			},
		},
		NcVersion:    "v1.0",
		Manufacturer: NcManufacturer{Name: "BY-SYSTEMS"},
		Product: NcProduct{
			Name:          "dhs",
			Key:           "dhs-1",
			RevisionLevel: "1.0",
		},
		SerialNumber: "SN-001",
		OperationalState: NcDeviceOperationalState{
			Generic: NcDeviceGenericStateNormalOperation,
		},
		ResetCause: NcResetCausePowerOn,
	}
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got NcDeviceManager
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.OperationalState.Generic != NcDeviceGenericStateNormalOperation {
		t.Fatalf("operational state mismatch")
	}
	if got.Manufacturer.Name != "BY-SYSTEMS" {
		t.Fatalf("manufacturer mismatch")
	}
}

func TestNcPropertyChangedEventDataRoundTrip(t *testing.T) {
	idx := NcId(0)
	in := NcPropertyChangedEventData{
		PropertyID:        NcPropertyId{Level: 1, Index: 6},
		ChangeType:        NcPropertyChangeTypeValueChanged,
		Value:             "new label",
		SequenceItemIndex: &idx,
	}
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got NcPropertyChangedEventData
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.SequenceItemIndex == nil || *got.SequenceItemIndex != 0 {
		t.Fatalf("sequence_item_index mismatch")
	}
	if got.ChangeType != NcPropertyChangeTypeValueChanged {
		t.Fatalf("change type mismatch")
	}
}

func TestNcResetCauseValues(t *testing.T) {
	if int(NcResetCausePowerOn) != 1 {
		t.Fatalf("PowerOn != 1")
	}
	if int(NcResetCauseManualReset) != 5 {
		t.Fatalf("ManualReset != 5")
	}
}

func TestNcDatatypeTypeValues(t *testing.T) {
	cases := map[NcDatatypeType]int{
		NcDatatypeTypePrimitive: 0,
		NcDatatypeTypeTypedef:   1,
		NcDatatypeTypeStruct:    2,
		NcDatatypeTypeEnum:      3,
	}
	for v, want := range cases {
		if int(v) != want {
			t.Errorf("%v != %d", v, want)
		}
	}
}
