// Package glow implements the Ember+ Glow DTD — the data schema layer
// that sits on top of BER encoding. Defines Node, Parameter, Matrix,
// Function, Command, and their Qualified variants.
//
// Reference: Ember+ Glow DTD specification (Lawo)
// Cross-reference: github.com/dufourgilles/emberlib (read-only, no fork)
package glow

// Application tags — top-level Glow element types.
// These are APPLICATION class, CONSTRUCTED.
const (
	// Source: Wireshark glow.asn + Lawo glow.h
	TagParameter               uint32 = 1  // GlowParameter
	TagCommand                 uint32 = 2  // GlowCommand
	TagNode                    uint32 = 3  // GlowNode
	TagElementCollection       uint32 = 4  // GlowElementCollection (SET OF)
	TagStreamEntry             uint32 = 5  // GlowStreamEntry
	TagStreamCollection        uint32 = 6  // GlowStreamCollection
	TagStringIntegerPair       uint32 = 7  // GlowStringIntegerPair
	TagStringIntegerCollection uint32 = 8  // GlowStringIntegerCollection
	TagQualifiedParameter      uint32 = 9  // GlowQualifiedParameter
	TagQualifiedNode           uint32 = 10 // GlowQualifiedNode
	TagRootElementCollection   uint32 = 11 // GlowRootElementCollection (SET OF)
	TagMatrix                  uint32 = 13 // GlowMatrix
	TagTarget                  uint32 = 14 // GlowTarget
	TagSource                  uint32 = 15 // GlowSource
	TagConnection              uint32 = 16 // GlowConnection
	TagQualifiedMatrix         uint32 = 17 // GlowQualifiedMatrix
	TagTupleItemDescription    uint32 = 18 // GlowTupleItemDescription
	TagFunction                uint32 = 19 // GlowFunction
	TagQualifiedFunction       uint32 = 20 // GlowQualifiedFunction
	TagInvocation              uint32 = 22 // GlowInvocation
	TagInvocationResult        uint32 = 23 // GlowInvocationResult
	TagTemplate                uint32 = 24 // GlowTemplate
)

// Context tags for GlowNode contents.
// These are CONTEXT class.
const (
	NodeNumber      uint32 = 0  // INTEGER — path element
	NodeContents    uint32 = 1  // SET — identifier, description, ...
	NodeChildren    uint32 = 2  // ElementCollection
)

// Context tags for GlowParameter contents.
const (
	ParamNumber   uint32 = 0 // INTEGER — path element
	ParamContents uint32 = 1 // SET — value, min, max, ...
	ParamChildren uint32 = 2 // ElementCollection
)

// Context tags for GlowQualifiedNode contents.
const (
	QNodePath     uint32 = 0 // RELATIVE-OID — full path from root
	QNodeContents uint32 = 1 // SET
	QNodeChildren uint32 = 2 // ElementCollection
)

// Context tags for GlowQualifiedParameter contents.
const (
	QParamPath     uint32 = 0 // RELATIVE-OID — full path from root
	QParamContents uint32 = 1 // SET
	QParamChildren uint32 = 2 // ElementCollection
)

// Context tags inside Parameter contents SET.
const (
	ParamContentIdentifier        uint32 = 0  // UTF8String
	ParamContentDescription       uint32 = 1  // UTF8String
	ParamContentValue             uint32 = 2  // any (INTEGER, REAL, UTF8String, BOOLEAN, OCTET STRING)
	ParamContentMinimum           uint32 = 3  // any
	ParamContentMaximum           uint32 = 4  // any
	ParamContentAccess            uint32 = 5  // INTEGER (Access enum)
	ParamContentFormat            uint32 = 6  // UTF8String (printf-style format)
	ParamContentEnumeration       uint32 = 7  // UTF8String (newline-separated)
	ParamContentFactor            uint32 = 8  // INTEGER
	ParamContentIsOnline          uint32 = 9  // BOOLEAN
	ParamContentFormula           uint32 = 10 // UTF8String
	ParamContentStep              uint32 = 11 // any
	ParamContentDefault           uint32 = 12 // any
	ParamContentType              uint32 = 13 // INTEGER (ParameterType enum)
	ParamContentStreamIdentifier  uint32 = 14 // INTEGER
	ParamContentEnumMap           uint32 = 15 // StringIntegerCollection
	ParamContentStreamDescriptor  uint32 = 16 // SET
	ParamContentSchemaIdentifiers uint32 = 17 // UTF8String
	ParamContentTemplateReference uint32 = 18 // RELATIVE-OID
)

// Context tags inside Node contents SET.
const (
	NodeContentIdentifier        uint32 = 0 // UTF8String
	NodeContentDescription       uint32 = 1 // UTF8String
	NodeContentIsRoot            uint32 = 2 // BOOLEAN
	NodeContentIsOnline          uint32 = 3 // BOOLEAN
	NodeContentSchemaIdentifiers uint32 = 4 // UTF8String
	NodeContentTemplateReference uint32 = 5 // RELATIVE-OID
)

// Context tags for GlowCommand.
const (
	CmdCtxNumber     uint32 = 0 // INTEGER — command type
	CmdCtxDirMask    uint32 = 1 // INTEGER — field mask for GetDirectory
	CmdCtxInvocation uint32 = 2 // Invocation object (for Invoke command)
)

// Command numbers.
const (
	CmdGetDirectory  int64 = 32 // Discover children
	CmdSubscribe     int64 = 30 // Subscribe to value changes
	CmdUnsubscribe   int64 = 31 // Unsubscribe
	CmdInvoke        int64 = 33 // Invoke a function
)

// Access levels.
const (
	AccessNone      int64 = 0
	AccessRead      int64 = 1
	AccessWrite     int64 = 2
	AccessReadWrite int64 = 3
)

// Parameter types.
const (
	ParamTypeInteger int64 = 1
	ParamTypeReal    int64 = 2
	ParamTypeString  int64 = 3
	ParamTypeBoolean int64 = 4
	ParamTypeTrigger int64 = 5
	ParamTypeEnum    int64 = 6
	ParamTypeOctets  int64 = 7
)

// Context tags for GlowMatrix contents.
const (
	MatrixNumber              uint32 = 0
	MatrixContents            uint32 = 1
	MatrixChildren            uint32 = 2
	MatrixTargets             uint32 = 3
	MatrixSources             uint32 = 4
	MatrixConnections         uint32 = 5
)

// Context tags inside Matrix contents SET.
const (
	MatContentIdentifier          uint32 = 0
	MatContentDescription         uint32 = 1
	MatContentType                uint32 = 2  // INTEGER (MatrixType enum)
	MatContentAddressingMode      uint32 = 3  // INTEGER
	MatContentTargetCount         uint32 = 4  // INTEGER
	MatContentSourceCount         uint32 = 5  // INTEGER
	MatContentMaxTotalConnects    uint32 = 6  // INTEGER
	MatContentMaxConnectsPerTgt   uint32 = 7  // INTEGER
	MatContentParametersLocation  uint32 = 8  // RELATIVE-OID or INTEGER
	MatContentGainParameterNumber uint32 = 9  // INTEGER
	MatContentLabels              uint32 = 10 // SEQUENCE OF GlowLabel
	MatContentSchemaIdentifiers   uint32 = 11 // UTF8String
	MatContentTemplateReference   uint32 = 12 // RELATIVE-OID
)

// Matrix types.
const (
	MatrixTypeOneToN  int64 = 0 // one source → N targets
	MatrixTypeOneToOne int64 = 1 // one source → one target
	MatrixTypeNToN    int64 = 2 // N sources → N targets
)

// Matrix addressing modes.
const (
	MatrixAddrLinear   int64 = 0
	MatrixAddrNonLinear int64 = 1
)

// Context tags for GlowConnection.
const (
	ConnTarget    uint32 = 0 // INTEGER
	ConnSources   uint32 = 1 // RELATIVE-OID (list of source numbers)
	ConnOperation uint32 = 2 // INTEGER (ConnectionOperation)
	ConnDisposition uint32 = 3 // INTEGER (ConnectionDisposition)
)

// Connection operations.
const (
	ConnOpAbsolute   int64 = 0 // Replace all sources
	ConnOpConnect    int64 = 1 // Add source
	ConnOpDisconnect int64 = 2 // Remove source
)

// Connection dispositions.
const (
	ConnDispTally    int64 = 0 // Current state (read)
	ConnDispModified int64 = 1 // Changed by command
	ConnDispPending  int64 = 2 // Queued
	ConnDispLocked   int64 = 3 // Locked by another consumer
)

// Context tags for GlowTarget / GlowSource.
const (
	TargetNumber uint32 = 0 // INTEGER
	SourceNumber uint32 = 0 // INTEGER
)

// Context tags for GlowFunction contents.
const (
	FuncNumber   uint32 = 0
	FuncContents uint32 = 1
	FuncChildren uint32 = 2
)

// Context tags inside Function contents SET.
const (
	FuncContentIdentifier        uint32 = 0
	FuncContentDescription       uint32 = 1
	FuncContentArguments         uint32 = 2 // SEQUENCE OF TupleItemDescription
	FuncContentResult            uint32 = 3 // SEQUENCE OF TupleItemDescription
	FuncContentTemplateReference uint32 = 4 // RELATIVE-OID
)

// Context tags for GlowInvocation.
const (
	InvInvocationID uint32 = 0 // INTEGER
	InvArguments    uint32 = 1 // SEQUENCE
)

// Context tags for GlowInvocationResult.
const (
	InvResInvocationID uint32 = 0 // INTEGER
	InvResSuccess      uint32 = 1 // BOOLEAN
	InvResResult       uint32 = 2 // SEQUENCE
)

// Context tags for TupleItemDescription.
const (
	TupleType uint32 = 0 // INTEGER (ParameterType)
	TupleName uint32 = 1 // UTF8String
)
