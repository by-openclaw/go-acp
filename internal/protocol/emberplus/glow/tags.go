// Package glow implements the Ember+ Glow DTD — the Ember+ data model layer
// built on top of the BER encoding. Contains tag constants, type structs,
// and encode/decode routines for every element defined by the spec.
//
// Authoritative reference: Ember+ Protocol Specification v2.50 (rev. 15,
// 2017-11-09), Lawo GmbH. PDF at assets/emberplus/Ember+ Documentation.pdf.
// The Glow DTD ASN.1 Notation (pp. 83–93) defines every type below. Each
// constant cites the spec page where it is introduced.
package glow

// Application-defined tags (spec pp. 83–93, "Glow DTD ASN.1 Notation").
// Every Glow type carries its APPLICATION tag in the BER header; these
// are constructed (SEQUENCE or SET under the hood).
const (
	TagRoot                    uint32 = 0  // [APPLICATION 0] outer CHOICE (elements/streams/invocationResult), p.93
	TagParameter               uint32 = 1  // [APPLICATION 1] Parameter, p.85
	TagCommand                 uint32 = 2  // [APPLICATION 2] Command, p.86
	TagNode                    uint32 = 3  // [APPLICATION 3] Node, p.87
	TagElementCollection       uint32 = 4  // [APPLICATION 4] ElementCollection SEQUENCE OF Element, p.92
	TagStreamEntry             uint32 = 5  // [APPLICATION 5] StreamEntry, p.93
	TagStreamCollection        uint32 = 6  // [APPLICATION 6] StreamCollection SEQUENCE OF StreamEntry, p.93
	TagStringIntegerPair       uint32 = 7  // [APPLICATION 7] StringIntegerPair, p.86
	TagStringIntegerCollection uint32 = 8  // [APPLICATION 8] StringIntegerCollection, p.86
	TagQualifiedParameter      uint32 = 9  // [APPLICATION 9] QualifiedParameter, p.85
	TagQualifiedNode           uint32 = 10 // [APPLICATION 10] QualifiedNode, p.87
	TagRootElementCollection   uint32 = 11 // [APPLICATION 11] RootElementCollection SEQUENCE OF RootElement, p.93
	TagStreamDescription       uint32 = 12 // [APPLICATION 12] StreamDescription, p.86
	TagMatrix                  uint32 = 13 // [APPLICATION 13] Matrix, p.88
	TagTarget                  uint32 = 14 // [APPLICATION 14] Target (Signal), p.89
	TagSource                  uint32 = 15 // [APPLICATION 15] Source (Signal), p.89
	TagConnection              uint32 = 16 // [APPLICATION 16] Connection, p.89
	TagQualifiedMatrix         uint32 = 17 // [APPLICATION 17] QualifiedMatrix, p.89
	TagLabel                   uint32 = 18 // [APPLICATION 18] Label (basePath/description), p.89
	TagFunction                uint32 = 19 // [APPLICATION 19] Function, p.91
	TagQualifiedFunction       uint32 = 20 // [APPLICATION 20] QualifiedFunction, p.91
	TagTupleItemDescription    uint32 = 21 // [APPLICATION 21] TupleItemDescription, p.91
	TagInvocation              uint32 = 22 // [APPLICATION 22] Invocation, p.91
	TagInvocationResult        uint32 = 23 // [APPLICATION 23] InvocationResult, p.92
	TagTemplate                uint32 = 24 // [APPLICATION 24] Template, p.84
	TagQualifiedTemplate       uint32 = 25 // [APPLICATION 25] QualifiedTemplate, p.84
)

// Context tags inside a Node wrapper SEQUENCE (APPLICATION[3]). Spec p.87.
const (
	NodeNumber   uint32 = 0 // [0] number Integer32 (non-qualified only)
	NodeContents uint32 = 1 // [1] contents NodeContents OPTIONAL (wrapped SET)
	NodeChildren uint32 = 2 // [2] children ElementCollection OPTIONAL
)

// Context tags inside a QualifiedNode wrapper SEQUENCE (APPLICATION[10]). Spec p.87.
const (
	QNodePath     uint32 = 0 // [0] path RELATIVE-OID — absolute path from root
	QNodeContents uint32 = 1 // [1] contents NodeContents OPTIONAL
	QNodeChildren uint32 = 2 // [2] children ElementCollection OPTIONAL
)

// Context tags inside a Parameter wrapper SEQUENCE (APPLICATION[1]). Spec p.85.
const (
	ParamNumber   uint32 = 0 // [0] number Integer32
	ParamContents uint32 = 1 // [1] contents ParameterContents OPTIONAL (wrapped SET)
	ParamChildren uint32 = 2 // [2] children ElementCollection OPTIONAL
)

// Context tags inside a QualifiedParameter wrapper SEQUENCE (APPLICATION[9]). Spec p.85.
const (
	QParamPath     uint32 = 0 // [0] path RELATIVE-OID
	QParamContents uint32 = 1 // [1] contents
	QParamChildren uint32 = 2 // [2] children
)

// Context tags inside the NodeContents SET. Spec p.87.
//
// NodeContents ::= SET {
//   identifier         [0] EmberString OPTIONAL,
//   description        [1] EmberString OPTIONAL,
//   isRoot             [2] BOOLEAN     OPTIONAL,  -- spec CTX 2 is isRoot, not isOnline
//   isOnline           [3] BOOLEAN     OPTIONAL,  -- default true
//   schemaIdentifiers  [4] EmberString OPTIONAL,
//   templateReference  [5] RELATIVE-OID OPTIONAL
// }
const (
	NodeContentIdentifier        uint32 = 0
	NodeContentDescription       uint32 = 1
	NodeContentIsRoot            uint32 = 2
	NodeContentIsOnline          uint32 = 3
	NodeContentSchemaIdentifiers uint32 = 4
	NodeContentTemplateReference uint32 = 5
)

// Context tags inside the ParameterContents SET. Spec p.85 (the full 0..18 list).
//
// ParameterContents ::= SET {
//   identifier         [ 0] EmberString,
//   description        [ 1] EmberString,
//   value              [ 2] Value,              -- CHOICE int/real/string/bool/octets/null
//   minimum            [ 3] MinMax,
//   maximum            [ 4] MinMax,
//   access             [ 5] ParameterAccess,
//   format             [ 6] EmberString,        -- printf-style; '°' introduces unit
//   enumeration        [ 7] EmberString,        -- newline-separated (legacy)
//   factor             [ 8] Integer32,
//   isOnline           [ 9] BOOLEAN,
//   formula            [10] EmberString,        -- provider|consumer split
//   step               [11] Integer32,
//   default            [12] Value,
//   type               [13] ParameterType,
//   streamIdentifier   [14] Integer32,
//   enumMap            [15] StringIntegerCollection,
//   streamDescriptor   [16] StreamDescription,
//   schemaIdentifiers  [17] EmberString,
//   templateReference  [18] RELATIVE-OID
// }
const (
	ParamContentIdentifier        uint32 = 0
	ParamContentDescription       uint32 = 1
	ParamContentValue             uint32 = 2
	ParamContentMinimum           uint32 = 3
	ParamContentMaximum           uint32 = 4
	ParamContentAccess            uint32 = 5
	ParamContentFormat            uint32 = 6
	ParamContentEnumeration       uint32 = 7
	ParamContentFactor            uint32 = 8
	ParamContentIsOnline          uint32 = 9
	ParamContentFormula           uint32 = 10
	ParamContentStep              uint32 = 11
	ParamContentDefault           uint32 = 12
	ParamContentType              uint32 = 13
	ParamContentStreamIdentifier  uint32 = 14
	ParamContentEnumMap           uint32 = 15
	ParamContentStreamDescriptor  uint32 = 16
	ParamContentSchemaIdentifiers uint32 = 17
	ParamContentTemplateReference uint32 = 18
)

// Context tags inside a Command wrapper (APPLICATION[2]). Spec p.86.
//
// Command ::= SEQUENCE {
//   number   [0] CommandType,
//   options  CHOICE { dirFieldMask [1] FieldFlags | invocation [2] Invocation } OPTIONAL
// }
const (
	CmdCtxNumber     uint32 = 0
	CmdCtxDirMask    uint32 = 1
	CmdCtxInvocation uint32 = 2
)

// Command numbers (spec p.31 / p.86 CommandType enum).
const (
	CmdSubscribe    int64 = 30
	CmdUnsubscribe  int64 = 31
	CmdGetDirectory int64 = 32
	CmdInvoke       int64 = 33
)

// ParameterAccess enum (spec p.85). Default read.
const (
	AccessNone      int64 = 0
	AccessRead      int64 = 1
	AccessWrite     int64 = 2
	AccessReadWrite int64 = 3
)

// ParameterType enum (spec p.85).
const (
	ParamTypeNull    int64 = 0
	ParamTypeInteger int64 = 1
	ParamTypeReal    int64 = 2
	ParamTypeString  int64 = 3
	ParamTypeBoolean int64 = 4
	ParamTypeTrigger int64 = 5
	ParamTypeEnum    int64 = 6
	ParamTypeOctets  int64 = 7
)

// Context tags on the Matrix SEQUENCE wrapper (APPLICATION[13]). Spec p.88.
//
// Matrix ::= SEQUENCE {
//   number       [0] Integer32,
//   contents     [1] MatrixContents         OPTIONAL,
//   children     [2] ElementCollection      OPTIONAL,
//   targets      [3] TargetCollection       OPTIONAL,
//   sources      [4] SourceCollection       OPTIONAL,
//   connections  [5] ConnectionCollection   OPTIONAL
// }
const (
	MatrixNumber      uint32 = 0
	MatrixContents    uint32 = 1
	MatrixChildren    uint32 = 2
	MatrixTargets     uint32 = 3
	MatrixSources     uint32 = 4
	MatrixConnections uint32 = 5
)

// Context tags inside the MatrixContents SET. Spec p.88.
//
// MatrixContents ::= SET {
//   identifier              [ 0] EmberString,
//   description             [ 1] EmberString            OPTIONAL,
//   type                    [ 2] MatrixType             OPTIONAL,
//   addressingMode          [ 3] MatrixAddressingMode   OPTIONAL,
//   targetCount             [ 4] Integer32,
//   sourceCount             [ 5] Integer32,
//   maximumTotalConnects    [ 6] Integer32              OPTIONAL, -- nToN
//   maximumConnectsPerTarget[ 7] Integer32              OPTIONAL, -- nToN
//   parametersLocation      [ 8] ParametersLocation     OPTIONAL, -- basePath RelOID OR inline Int32
//   gainParameterNumber     [ 9] Integer32              OPTIONAL,
//   labels                  [10] LabelCollection        OPTIONAL,
//   schemaIdentifiers       [11] EmberString            OPTIONAL,
//   templateReference       [12] RELATIVE-OID           OPTIONAL
// }
const (
	MatContentIdentifier          uint32 = 0
	MatContentDescription         uint32 = 1
	MatContentType                uint32 = 2
	MatContentAddressingMode      uint32 = 3
	MatContentTargetCount         uint32 = 4
	MatContentSourceCount         uint32 = 5
	MatContentMaxTotalConnects    uint32 = 6
	MatContentMaxConnectsPerTgt   uint32 = 7
	MatContentParametersLocation  uint32 = 8
	MatContentGainParameterNumber uint32 = 9
	MatContentLabels              uint32 = 10
	MatContentSchemaIdentifiers   uint32 = 11
	MatContentTemplateReference   uint32 = 12
)

// MatrixType enum (spec p.88). Default oneToN.
const (
	MatrixTypeOneToN   int64 = 0
	MatrixTypeOneToOne int64 = 1
	MatrixTypeNToN     int64 = 2
)

// MatrixAddressingMode enum (spec p.88). Default linear.
const (
	MatrixAddrLinear    int64 = 0
	MatrixAddrNonLinear int64 = 1
)

// Context tags on the Connection SEQUENCE (APPLICATION[16]). Spec p.89.
//
// Connection ::= SEQUENCE {
//   target       [0] Integer32,
//   sources      [1] PackedNumbers        OPTIONAL, -- RELATIVE-OID of source numbers
//   operation    [2] ConnectionOperation  OPTIONAL,
//   disposition  [3] ConnectionDisposition OPTIONAL
// }
const (
	ConnTarget      uint32 = 0
	ConnSources     uint32 = 1
	ConnOperation   uint32 = 2
	ConnDisposition uint32 = 3
)

// ConnectionOperation enum (spec p.89). Default absolute.
const (
	ConnOpAbsolute   int64 = 0 // sources contains absolute information
	ConnOpConnect    int64 = 1 // nToN only — sources to add to connection
	ConnOpDisconnect int64 = 2 // nToN only — sources to remove from connection
)

// ConnectionDisposition enum (spec p.89). Default tally.
const (
	ConnDispTally    int64 = 0 // current state report
	ConnDispModified int64 = 1 // sources contains new current state
	ConnDispPending  int64 = 2 // sources contains future state
	ConnDispLocked   int64 = 3 // target locked — sources contains current state
)

// Context tags on a Signal (Target APP[14] / Source APP[15]). Spec p.89.
const (
	SignalNumber uint32 = 0
)

// Legacy aliases retained for code that still uses them.
const (
	TargetNumber uint32 = 0
	SourceNumber uint32 = 0
)

// Context tags on a Label SEQUENCE (APPLICATION[18]). Spec p.89.
//
// Label ::= SEQUENCE { basePath [0] RELATIVE-OID, description [1] EmberString }
const (
	LabelBasePath    uint32 = 0
	LabelDescription uint32 = 1
)

// Context tags on a Function wrapper SEQUENCE (APPLICATION[19]). Spec p.91.
const (
	FuncNumber   uint32 = 0
	FuncContents uint32 = 1
	FuncChildren uint32 = 2
)

// Context tags on a QualifiedFunction wrapper (APPLICATION[20]).
const (
	QFuncPath     uint32 = 0
	QFuncContents uint32 = 1
	QFuncChildren uint32 = 2
)

// Context tags inside FunctionContents SET. Spec p.91.
//
// FunctionContents ::= SET {
//   identifier        [0] EmberString       OPTIONAL,
//   description       [1] EmberString       OPTIONAL,
//   arguments         [2] TupleDescription  OPTIONAL,
//   result            [3] TupleDescription  OPTIONAL,
//   templateReference [4] RELATIVE-OID      OPTIONAL
// }
const (
	FuncContentIdentifier        uint32 = 0
	FuncContentDescription       uint32 = 1
	FuncContentArguments         uint32 = 2
	FuncContentResult            uint32 = 3
	FuncContentTemplateReference uint32 = 4
)

// Context tags on the Invocation SEQUENCE (APPLICATION[22]). Spec p.91.
const (
	InvInvocationID uint32 = 0
	InvArguments    uint32 = 1
)

// Context tags on the InvocationResult SEQUENCE (APPLICATION[23]). Spec p.92.
// success defaults to true when omitted (spec: "True or omitted if no errors").
const (
	InvResInvocationID uint32 = 0
	InvResSuccess      uint32 = 1
	InvResResult       uint32 = 2
)

// Context tags on a TupleItemDescription SEQUENCE (APPLICATION[21]). Spec p.91.
const (
	TupleType uint32 = 0
	TupleName uint32 = 1
)

// Context tags on a StreamEntry SEQUENCE (APPLICATION[5]). Spec p.93.
const (
	StreamEntryIdentifier uint32 = 0
	StreamEntryValue      uint32 = 1
)

// Context tags on a StreamDescription SEQUENCE (APPLICATION[12]). Spec p.86.
const (
	StreamDescFormat uint32 = 0
	StreamDescOffset uint32 = 1
)

// StreamFormat enum (spec p.86, DTD). Encoded bits: TTTTT SS E
// where TTTTT = data kind (uint/int/float), SS = byte width, E = endianness.
const (
	StreamFmtUnsignedInt8              int64 = 0
	StreamFmtUnsignedInt16BigEndian    int64 = 2
	StreamFmtUnsignedInt16LittleEndian int64 = 3
	StreamFmtUnsignedInt32BigEndian    int64 = 4
	StreamFmtUnsignedInt32LittleEndian int64 = 5
	StreamFmtUnsignedInt64BigEndian    int64 = 6
	StreamFmtUnsignedInt64LittleEndian int64 = 7
	StreamFmtSignedInt8                int64 = 8
	StreamFmtSignedInt16BigEndian      int64 = 10
	StreamFmtSignedInt16LittleEndian   int64 = 11
	StreamFmtSignedInt32BigEndian      int64 = 12
	StreamFmtSignedInt32LittleEndian   int64 = 13
	StreamFmtSignedInt64BigEndian      int64 = 14
	StreamFmtSignedInt64LittleEndian   int64 = 15
	StreamFmtFloat32BigEndian          int64 = 20
	StreamFmtFloat32LittleEndian       int64 = 21
	StreamFmtFloat64BigEndian          int64 = 22
	StreamFmtFloat64LittleEndian       int64 = 23
)

// Context tags on Template / QualifiedTemplate SET. Spec p.84.
//
// Template ::= SET { number [0] Integer32, element [1] TemplateElement OPTIONAL, description [2] EmberString OPTIONAL }
// QualifiedTemplate ::= SET { path   [0] RELATIVE-OID, element [1] TemplateElement OPTIONAL, description [2] EmberString OPTIONAL }
const (
	TemplateNumber      uint32 = 0
	TemplatePath        uint32 = 0 // alias for QualifiedTemplate
	TemplateElementCtx  uint32 = 1
	TemplateDescription uint32 = 2
)
