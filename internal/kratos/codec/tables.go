package codec

// Rows of the tables a monitoring client reads. Column names are the
// document's, verbatim (they differ in case between tables: "UID" in
// the CVT, "uid" in dependencies). Every column is Text: the document
// sends the same column as a string, a number or null.

// SVTRow is one row of the Scalable Value Table, valueNodeTbl (§3.1,
// §5.57.3): the metadata of one value — name, type, range, units,
// scaling. NodeType 2 is a tree node (a group), not a value (§5.59). A
// UID ending in "#" is an array whose CVT rows replace the "#" with an
// index (§2.2 note 2). Volatility -1 means the value is never written
// to the CVT and can only be read from MQTT (§2 note 1).
type SVTRow struct {
	UID                 string `json:"UID"`
	EnName              Text   `json:"enName"`
	EnAbbreviation      Text   `json:"enAbbreviation"`
	EnDescription       Text   `json:"enDescription"`
	NodeType            Text   `json:"NodeType"`
	ValueGroup          Text   `json:"ValueGroup"`
	ValueType           Text   `json:"ValueType"`
	RangeMin            Text   `json:"RangeMin"`
	RangeMax            Text   `json:"RangeMax"`
	DefaultValue        Text   `json:"DefaultValue"`
	Units               Text   `json:"Units"`
	AccessType          Text   `json:"AccessType"`
	AccessLevel         Text   `json:"AccessLevel"`
	FormatType          Text   `json:"FormatType"`
	Persistent          Text   `json:"Persistent"`
	Dependency          Text   `json:"Dependency"`
	InstanceMax         Text   `json:"instanceMax"`
	EnumerationMatchKey Text   `json:"enumerationMatchKey"`
	Deprecated          Text   `json:"deprecated"`
	InCVT               Text   `json:"inCVT"`
	Volatility          Text   `json:"volatility"`
	IsExpress           Text   `json:"isExpress"`
	GUIDisplayPrecision Text   `json:"guiDisplayPrecision"`
	UnitScale           Text   `json:"unitScale"`
	SNMPMibOid          Text   `json:"snmpMibOid"`
}

// NodeTypeNode is the SVT NodeType of a tree node (§5.59.1).
const NodeTypeNode = "2"

// IsNode reports whether the row is a tree node rather than a value.
func (r SVTRow) IsNode() bool { return string(r.NodeType) == NodeTypeNode }

// MQTTOnly reports whether the value is never written to the CVT
// (volatility -1, §2 note 1): its current value comes from MQTT only.
func (r SVTRow) MQTTOnly() bool { return string(r.Volatility) == "-1" }

// IsArray reports whether the UID is an array template (ends in "#").
func (r SVTRow) IsArray() bool { return len(r.UID) > 0 && r.UID[len(r.UID)-1] == '#' }

// Placeholder values the SVT uses instead of null (§5.57.4).
const (
	NoMin     = "@NOMIN"
	NoMax     = "@NOMAX"
	NoDefault = "@NODEFAULT"
	NoUnits   = "@NOUNITS"
	NoDep     = "@NODEP"
	Invalid   = "@INVALID" // CVT ConfigValue never configured (§5.15.4)
)

// CVTRow is one row of the Current Value Table (§5.15.3).
type CVTRow struct {
	UID           string `json:"UID"`
	Value         Text   `json:"Value"`
	GroupID       Text   `json:"GroupID"`
	LastUTS       Text   `json:"LastUTS"`
	ConfigValue   Text   `json:"ConfigValue"`
	ConfigLastUTS Text   `json:"configLastUTS"`
	AutoInserted  Text   `json:"autoInserted"`
	IsExpress     Text   `json:"isExpress"`
	IsPrivate     Text   `json:"isPrivate"`
}

// Enumeration is one row of enumerationDefs (§5.27.3): the display text
// of one numeric value of one UID.
type Enumeration struct {
	UID           string `json:"UID"`
	EnumIndex     Text   `json:"enumIndex"`
	EnDisplayText Text   `json:"enDisplayText"`
}

// Dependency is one row of dynamicDependenciesTbl (§5.26.3): a UID with
// isHidden nonzero has no meaning in the current configuration.
type Dependency struct {
	UID      string `json:"uid"`
	IsHidden Text   `json:"isHidden"`
}

// Hidden reports whether the UID is not valid in this configuration.
func (d Dependency) Hidden() bool {
	n, ok := d.IsHidden.Int()
	return ok && n != 0
}

// Boundary is one row of boundariesTbl (§5.10.3): a subtree of the SVT
// that this NGC2 realises (activate 1) or not (0).
type Boundary struct {
	CustomerID string `json:"customerId"`
	UID        string `json:"UID"`
	Activate   Text   `json:"activate"`
	Comment    Text   `json:"Comment"`
	Hierarchy  Text   `json:"hierarchy"`
}

// AlarmPoint is one row of alarmPoint (§5.1.3).
type AlarmPoint struct {
	UID                string `json:"uid"`
	EnAlarmName        Text   `json:"enAlarmName"`
	EnNotificationText Text   `json:"enNotificationText"`
	AlarmMinLevel      Text   `json:"alarmMinLevel"`
	AlarmMajLevel      Text   `json:"alarmMajLevel"`
	AlarmCritLevel     Text   `json:"alarmCritLevel"`
	AlarmClearLevel    Text   `json:"alarmClearLevel"`
	AlarmUsualLevel    Text   `json:"alarmUsualLevel"`
	UnderlyingUID      Text   `json:"underlyingUID"`
	Deprecated         Text   `json:"deprecated"`
}

// ArrayLabel is one row of arrayIndexLabelTbl (§5.5.3): the label of
// one index of an array UID. The column header says "Index", the
// example sends "index" (§5.5.4); JSON matching is case-insensitive.
type ArrayLabel struct {
	UIDRoot string `json:"UIDroot"`
	Index   Text   `json:"index"`
	EnLabel Text   `json:"enLabel"`
}

// LoginReply is the parameters row of security/login (§6.2.3).
type LoginReply struct {
	Operator    string `json:"Operator"`
	AccessLevel Text   `json:"AccessLevel"`
	Language    Text   `json:"language"`
}

// PositionRow is one row of positionLog (§5.38.3): the antenna's
// pointing as logged.
type PositionRow struct {
	Sequence        Text `json:"positionLogSequence"`
	Timestamp       Text `json:"positionLogTimestamp"`
	DateTime        Text `json:"positionLogDateTime"`
	Antenna         Text `json:"positionLogAntenna"`
	Flags           Text `json:"positionLogFlags"`
	Azimuth         Text `json:"positionLogAzimuth"`
	AzimuthCmd      Text `json:"positionLogAzimuthCmd"`
	Elevation       Text `json:"positionLogElevation"`
	ElevationCmd    Text `json:"positionLogElevationCmd"`
	Polarization    Text `json:"positionLogPolarization"`
	PolarizationCmd Text `json:"positionLogPolarizationCmd"`
	BeaconLevel     Text `json:"positionLogBeaconLevel"`
	AvgBeaconLevel  Text `json:"positionLogAvgBeaconLevel"`
	SatelliteNumber Text `json:"positionLogSatelliteNumber"`
	Subsystem       Text `json:"positionLogAntennaSubsystem"`
}
