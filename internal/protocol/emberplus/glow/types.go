// Package glow implements the Ember+ Glow DTD types.
// All types match the Ember+ specification v2.50 (Lawo GmbH).
// Reference: assets/emberplus/Ember+ Documentation.pdf
package glow

// --- Element types (spec p16-50) ---

// Node represents a Glow tree node (container).
// Spec p23-25: Node properties.
type Node struct {
	Number      int32   // Context(0) — unique within parent scope
	Path        []int32 // Context(0) for QualifiedNode — RELATIVE-OID from root
	Identifier  string  // Contents Context(0)
	Description string  // Contents Context(1)
	IsRoot      bool    // Contents Context(2)
	IsOnline    bool    // Contents Context(3) — default true
	SchemaID    string  // Contents Context(4)
	Children    []Element
}

// Parameter represents a Glow parameter (typed value).
// Spec p25-30: Parameter properties — all 18 contents fields.
type Parameter struct {
	Number           int32              // Context(0)
	Path             []int32            // Context(0) for QualifiedParameter
	Identifier       string             // Contents[0]
	Description      string             // Contents[1]
	Value            interface{}        // Contents[2] — int64, float64, string, bool, []byte
	Minimum          interface{}        // Contents[3]
	Maximum          interface{}        // Contents[4]
	Access           int64              // Contents[5] — 0=None, 1=Read, 2=Write, 3=RW
	Format           string             // Contents[6] — printf-style (d, f, x), ° = unit start
	Enumeration      string             // Contents[7] — \n separated, ~ prefix = hidden
	Factor           int64              // Contents[8] — divide for display, multiply for set
	IsOnline         bool               // Contents[9]
	Formula          string             // Contents[10] — two formulas separated by \n
	Step             interface{}        // Contents[11]
	Default          interface{}        // Contents[12]
	Type             int64              // Contents[13] — ParameterType enum
	StreamIdentifier int64              // Contents[14] — >= 0, globally unique
	EnumMap          map[int64]string   // Contents[15] — StringIntegerCollection
	StreamDescriptor *StreamDescriptor  // Contents[16]
	SchemaID         string             // Contents[17]
	Children         []Element
}

// StreamDescriptor describes the format of a stream value.
// Spec p30: used when a single stream entry contains multiple values.
type StreamDescriptor struct {
	Format int64 // stream value format
	Offset int64 // byte offset in stream buffer
}

// Matrix represents a Glow matrix (signal routing).
// Spec p33-46: Matrix Extensions.
type Matrix struct {
	Number              int32       // Context(0)
	Path                []int32     // Context(0) for QualifiedMatrix
	Identifier          string      // Contents[0]
	Description         string      // Contents[1]
	MatrixType          int64       // Contents[2] — 0=1:N, 1=1:1, 2=N:N
	AddressingMode      int64       // Contents[3] — 0=linear, 1=non-linear
	TargetCount         int32       // Contents[4]
	SourceCount         int32       // Contents[5]
	MaxTotalConnects    int32       // Contents[6] — N:N only, default=tgt*src
	MaxConnectsPerTarget int32      // Contents[7] — N:N only, default=sourceCount
	ParametersLocation  interface{} // Contents[8] — int32 (inline) or []int32 (basePath RELATIVE-OID)
	GainParameterNumber int32       // Contents[9] — which param number is the gain
	Labels              []Label     // Contents[10] — LabelCollection
	SchemaID            string      // Contents[11]
	Targets             []int32     // Context(3) — non-linear only, target signal numbers
	Sources             []int32     // Context(4) — non-linear only, source signal numbers
	Connections         []Connection // Context(5) — current connections
	Children            []Element   // Context(2)
}

// Label is a matrix axis label reference.
// Spec p38: Label.basePath points to a subtree with target/source labels.
type Label struct {
	BasePath    []int32 // RELATIVE-OID to label subtree
	Description string  // free-text description (e.g. "Primary", "Internal")
}

// Connection represents a matrix cross-point connection.
// Spec p39: one Connection per target.
type Connection struct {
	Target      int32   // Context(0) — target signal number
	Sources     []int32 // Context(1) — PackedNumbers (RELATIVE-OID encoding)
	Operation   int64   // Context(2) — 0=absolute, 1=connect, 2=disconnect
	Disposition int64   // Context(3) — 0=tally, 1=modified, 2=pending, 3=locked
}

// Function represents a Glow function (RPC).
// Spec p47-50: Function Extensions.
type Function struct {
	Number      int32       // Context(0)
	Path        []int32     // Context(0) for QualifiedFunction
	Identifier  string      // Contents[0]
	Description string      // Contents[1]
	Arguments   []TupleItem // Contents[2] — TupleDescription
	Result      []TupleItem // Contents[3] — TupleDescription
	Children    []Element   // Context(2)
}

// TupleItem describes one argument or result of a function.
// Spec p48: TupleItemDescription.
type TupleItem struct {
	Type int64  // Context(0) — ParameterType enum
	Name string // Context(1) — optional, may be omitted for single arg
}

// Command represents a Glow command.
// Spec p31-32: GetDirectory(32), Subscribe(30), Unsubscribe(31), Invoke(33).
type Command struct {
	Number     int64       // Context(0) — command ID
	DirMask    int64       // Context(1) — dirFieldMask (optional)
	Invocation *Invocation // Context(2) — for Invoke command only
}

// Invocation represents a function call request.
// Spec p48: sent inside Command(33).
type Invocation struct {
	InvocationID int32         // Context(0) — optional (omit for fire-and-forget)
	Arguments    []interface{} // Context(1) — Tuple (SEQUENCE of typed values)
}

// InvocationResult represents the result of a function call.
// Spec p48-49: sent as top-level message (not in RootElementCollection).
type InvocationResult struct {
	InvocationID int32         // Context(0)
	Success      bool          // Context(1) — default true when omitted
	Result       []interface{} // Context(2) — Tuple
}

// Element is a union type — exactly one field is non-nil.
type Element struct {
	Node             *Node
	Parameter        *Parameter
	Matrix           *Matrix
	Function         *Function
	Command          *Command
	InvocationResult *InvocationResult
}

// StreamEntry represents a streamed parameter value.
// Spec p29-30: sent inside StreamCollection (APPLICATION[6]).
type StreamEntry struct {
	StreamIdentifier int64       // Context(0) — matches Parameter.StreamIdentifier
	Value            interface{} // Context(1) — typed value
}
