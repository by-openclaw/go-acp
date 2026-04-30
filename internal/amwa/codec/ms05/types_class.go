package ms05

// NcObject is the root class — every other live-tree entity inherits
// from it. The struct mirrors classes/1.json.
//
// Wire form: when an NcObject (or descendant) is the value returned
// by a Get on a property, this struct is what gets marshalled.
//
// Spec: classes/1.json.
type NcObject struct {
	ClassID                    NcClassId               `json:"classId"`
	Oid                        NcOid                   `json:"oid"`
	ConstantOid                bool                    `json:"constantOid"`
	Owner                      *NcOid                  `json:"owner"`
	Role                       string                  `json:"role"`
	UserLabel                  *string                 `json:"userLabel"`
	Touchpoints                []NcTouchpoint          `json:"touchpoints"`
	RuntimePropertyConstraints []NcPropertyConstraints `json:"runtimePropertyConstraints"`
}

// NcBlock is the container class — parents zero-or-more child
// NcObject instances. ClassID always begins with [1, 1].
//
// Spec: classes/1.1.json.
type NcBlock struct {
	NcObject
	Enabled bool      `json:"enabled"`
	Members []NcOid   `json:"members,omitempty"`
}

// NcWorker is the abstract base of "worker" classes (sound
// processing, signalling, etc.) — concrete workers extend this.
//
// Spec: classes/1.2.json.
type NcWorker struct {
	NcObject
	Enabled bool `json:"enabled"`
}

// NcManager is the abstract base of singleton "manager" classes
// (DeviceManager, ClassManager, …).
//
// Spec: classes/1.3.json.
type NcManager struct {
	NcObject
}

// NcDeviceManager carries device-wide metadata + operational state.
// Always at OID 1 inside the Device's root block.
//
// Spec: classes/1.3.1.json.
type NcDeviceManager struct {
	NcManager
	NcVersion         string                   `json:"ncVersion"`
	Manufacturer      NcManufacturer           `json:"manufacturer"`
	Product           NcProduct                `json:"product"`
	SerialNumber      string                   `json:"serialNumber"`
	UserInventoryCode *string                  `json:"userInventoryCode"`
	DeviceName        *string                  `json:"deviceName"`
	DeviceRole        *string                  `json:"deviceRole"`
	OperationalState  NcDeviceOperationalState `json:"operationalState"`
	ResetCause        NcResetCause             `json:"resetCause"`
	MessageNotice     *string                  `json:"messageNotice"`
}

// NcClassManager publishes the class + datatype catalogue. Lives at
// OID 2 inside the Device's root block.
//
// Spec: classes/1.3.2.json.
type NcClassManager struct {
	NcManager
	ControlClasses []NcClassDescriptor    `json:"controlClasses"`
	Datatypes      []NcDatatypeDescriptor `json:"datatypes"`
}

// NcPropertyChangedEventData is the payload carried by every
// PropertyChanged notification.
//
// Spec: datatypes/NcPropertyChangedEventData.json.
type NcPropertyChangedEventData struct {
	PropertyID        NcPropertyId         `json:"propertyId"`
	ChangeType        NcPropertyChangeType `json:"changeType"`
	Value             any                  `json:"value"`
	SequenceItemIndex *NcId                `json:"sequenceItemIndex"`
}
