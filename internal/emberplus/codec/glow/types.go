// Glow element types — one Go struct per spec type.
// All names and field orderings mirror the Glow DTD ASN.1 Notation
// (Ember+ Documentation.pdf v2.50 pp. 83–93). Cross-reference the
// CTX tag and spec page for each field; keep the struct spec-pure.
package glow

// Node mirrors Node APPLICATION[3] (p.87) and QualifiedNode APPLICATION[10] (p.87).
// A container for parameters, matrices, functions, and nested nodes.
//
// Wire layout:
//
//	Node          ::= [APPLICATION 3] SEQUENCE { [0] number Int32, [1] contents, [2] children }
//	QualifiedNode ::= [APPLICATION 10] SEQUENCE { [0] path RelOID,  [1] contents, [2] children }
//	NodeContents  ::= SET { identifier, description, isRoot, isOnline, schemaIdentifiers, templateReference }
type Node struct {
	Number            int32     // Node [0] / number within parent
	Path              []int32   // QualifiedNode [0] — absolute path from root
	Identifier        string    // Contents [0]
	Description       string    // Contents [1]
	IsRoot            bool      // Contents [2]
	IsOnline          bool      // Contents [3] — default true
	SchemaIdentifiers string    // Contents [4] — newline-separated list
	TemplateReference []int32   // Contents [5] — RelOID path to Template
	Children          []Element // [2] ElementCollection
}

// Parameter mirrors Parameter APPLICATION[1] (p.85) and QualifiedParameter APPLICATION[9] (p.85).
// A typed, optionally writable, optionally streamed value.
//
// ParameterContents covers all 18 optional fields from the spec; every field
// is OPTIONAL on the wire and left at its Go zero value when absent.
type Parameter struct {
	Number            int32             // Parameter [0]
	Path              []int32           // QualifiedParameter [0]
	Identifier        string            // Contents [0]
	Description       string            // Contents [1]
	Value             any               // Contents [2] Value CHOICE — int64/float64/string/bool/[]byte/nil
	Minimum           any               // Contents [3]
	Maximum           any               // Contents [4]
	Access            int64             // Contents [5] ParameterAccess
	Format            string            // Contents [6] printf-style; '°' introduces unit
	Enumeration       string            // Contents [7] newline-separated (legacy)
	Factor            int64             // Contents [8]
	IsOnline          bool              // Contents [9]
	Formula           string            // Contents [10] provider|consumer split
	Step              any               // Contents [11]
	Default           any               // Contents [12]
	Type              int64             // Contents [13] ParameterType enum
	StreamIdentifier  int64             // Contents [14] globally-unique stream id
	EnumMap           map[int64]string  // Contents [15] StringIntegerCollection
	StreamDescriptor  *StreamDescription // Contents [16]
	SchemaIdentifiers string            // Contents [17]
	TemplateReference []int32           // Contents [18] RelOID
	Children          []Element
}

// StreamDescription mirrors StreamDescription APPLICATION[12] (p.86).
// Describes how to extract a typed value from a shared stream blob.
type StreamDescription struct {
	Format int64 // [0] StreamFormat enum (see tags.go StreamFmt*)
	Offset int64 // [1] byte offset of the value inside the streamed blob
}

// Matrix mirrors Matrix APPLICATION[13] (p.88) and QualifiedMatrix APPLICATION[17] (p.89).
// A signal-routing object. targets/sources/connections live outside the contents SET.
type Matrix struct {
	Number               int32     // Matrix [0]
	Path                 []int32   // QualifiedMatrix [0]
	Identifier           string    // Contents [0]
	Description          string    // Contents [1]
	MatrixType           int64     // Contents [2] 0=oneToN, 1=oneToOne, 2=nToN
	AddressingMode       int64     // Contents [3] 0=linear, 1=nonLinear
	TargetCount          int32     // Contents [4]
	SourceCount          int32     // Contents [5]
	MaxTotalConnects     int32     // Contents [6] nToN global cap
	MaxConnectsPerTarget int32     // Contents [7] nToN per-target cap
	ParametersLocation   any       // Contents [8] ParametersLocation CHOICE — []int32 (basePath) or int32 (inline)
	GainParameterNumber  int32     // Contents [9] sub-identifier of gain parameter under parametersLocation/connections/<t>/<s>
	Labels               []Label   // Contents [10] LabelCollection
	SchemaIdentifiers    string    // Contents [11]
	TemplateReference    []int32   // Contents [12]
	Targets              []int32   // [3] TargetCollection — signal numbers (nonLinear) or implicit (linear)
	Sources              []int32   // [4] SourceCollection
	Connections          []Connection // [5] ConnectionCollection
	Children             []Element
}

// Label mirrors Label APPLICATION[18] (p.89).
// Points to a subtree of labels for targets/sources/connections. Multiple
// labels are permitted (e.g. "Primary", "External").
type Label struct {
	BasePath    []int32 // [0] RelOID — path to a Node containing target/source label parameters
	Description string  // [1] human-readable qualifier
}

// Connection mirrors Connection APPLICATION[16] (p.89).
// Wire-pure. Client UI wraps this in a richer record (see plugin matrix state).
type Connection struct {
	Target      int32   // [0]
	Sources     []int32 // [1] PackedNumbers RelOID of source numbers (empty = none)
	Operation   int64   // [2] ConnectionOperation (default absolute)
	Disposition int64   // [3] ConnectionDisposition (default tally)
}

// Function mirrors Function APPLICATION[19] (p.91) and QualifiedFunction APPLICATION[20] (p.91).
type Function struct {
	Number            int32       // Function [0]
	Path              []int32     // QualifiedFunction [0]
	Identifier        string      // Contents [0]
	Description       string      // Contents [1]
	Arguments         []TupleItem // Contents [2] TupleDescription — arg name + type
	Result            []TupleItem // Contents [3] TupleDescription — result tuple shape
	TemplateReference []int32     // Contents [4]
	Children          []Element
}

// TupleItem mirrors TupleItemDescription APPLICATION[21] (p.91).
type TupleItem struct {
	Type int64  // [0] ParameterType
	Name string // [1] optional
}

// Command mirrors Command APPLICATION[2] (p.86).
// Sent as a child element to trigger Subscribe/Unsubscribe/GetDirectory/Invoke.
type Command struct {
	Number     int64       // [0] CommandType (30/31/32/33)
	DirMask    int64       // [1] FieldFlags — only when Number == 32
	Invocation *Invocation // [2] Invocation — only when Number == 33
}

// Invocation mirrors Invocation APPLICATION[22] (p.91).
type Invocation struct {
	InvocationID int32 // [0]
	Arguments    []any // [1] Tuple = SEQUENCE OF [0] Value
}

// InvocationResult mirrors InvocationResult APPLICATION[23] (p.92).
// Note: success defaults to true when the field is omitted on the wire.
type InvocationResult struct {
	InvocationID int32 // [0]
	Success      bool  // [1] — default true
	Result       []any // [2] Tuple
}

// Template mirrors Template APPLICATION[24] (p.84).
// Acts as a prototype for Node/Parameter/Matrix/Function references.
type Template struct {
	Number      int32           // [0] (Template only)
	Path        []int32         // [0] (QualifiedTemplate only)
	Element     *TemplateElement // [1] CHOICE — exactly one of {Parameter, Node, Matrix, Function}
	Description string          // [2]
	Qualified   bool            // true when wire tag was APPLICATION[25]
}

// TemplateElement is the CHOICE from spec p.84: Parameter | Node | Matrix | Function.
// Exactly one field is non-nil.
type TemplateElement struct {
	Parameter *Parameter
	Node      *Node
	Matrix    *Matrix
	Function  *Function
}

// StreamEntry mirrors StreamEntry APPLICATION[5] (p.93).
// Delivered inside a StreamCollection.
type StreamEntry struct {
	StreamIdentifier int64 // [0]
	Value            any   // [1] Value CHOICE
}

// Element is a tagged union for one element of an ElementCollection, or one
// root-level message entry. Exactly one pointer field is non-nil.
type Element struct {
	Node             *Node
	Parameter        *Parameter
	Matrix           *Matrix
	Function         *Function
	Command          *Command
	Template         *Template
	InvocationResult *InvocationResult
	Streams          []StreamEntry // populated only for top-level StreamCollection messages
}
