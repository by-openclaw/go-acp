package glow

// Node represents a Glow tree node (container).
type Node struct {
	Number      int32  // path element
	Path        []int32 // full path from root (QualifiedNode only)
	Identifier  string
	Description string
	IsRoot      bool
	IsOnline    bool
	Children    []Element // child nodes/parameters
}

// Parameter represents a Glow parameter (typed value).
type Parameter struct {
	Number           int32   // path element
	Path             []int32 // full path from root (QualifiedParameter only)
	Identifier       string
	Description      string
	Value            interface{} // int64, float64, string, bool, []byte
	Minimum          interface{}
	Maximum          interface{}
	Access           int64 // AccessNone/Read/Write/ReadWrite
	Format           string // printf-style format string
	Enumeration      string // newline-separated enum values
	EnumMap          map[int64]string // integer → label
	Factor           int64
	IsOnline         bool
	Formula          string
	Step             interface{}
	Default          interface{}
	Type             int64 // ParamTypeInteger, ParamTypeReal, etc.
	StreamIdentifier int64
	SchemaID         string
}

// Matrix represents a Glow matrix (signal routing).
type Matrix struct {
	Number              int32   // path element
	Path                []int32 // full path from root (QualifiedMatrix only)
	Identifier          string
	Description         string
	MatrixType          int64   // MatrixTypeOneToN, OneToOne, NToN
	AddressingMode      int64   // MatrixAddrLinear, NonLinear
	TargetCount         int32
	SourceCount         int32
	MaxTotalConnects    int32
	MaxConnectsPerTarget int32
	ParametersLocation  interface{} // int32 or []int32 (RELATIVE-OID)
	GainParameterNumber int32
	Labels              []Label
	SchemaID            string
	Targets             []int32       // target numbers
	Sources             []int32       // source numbers
	Connections         []Connection  // current connections
	Children            []Element
}

// Label is a matrix axis label.
type Label struct {
	BasePath    []int32
	Description string
}

// Connection represents a matrix cross-point connection.
type Connection struct {
	Target      int32
	Sources     []int32
	Operation   int64 // ConnOpAbsolute, Connect, Disconnect
	Disposition int64 // ConnDispTally, Modified, Pending, Locked
}

// Function represents a Glow function (RPC).
type Function struct {
	Number      int32   // path element
	Path        []int32 // full path from root (QualifiedFunction only)
	Identifier  string
	Description string
	Arguments   []TupleItem
	Result      []TupleItem
	Children    []Element
}

// TupleItem describes one argument or result of a function.
type TupleItem struct {
	Type int64  // ParamTypeInteger, ParamTypeReal, etc.
	Name string
}

// Invocation represents a function call.
type Invocation struct {
	InvocationID int32
	Arguments    []interface{} // typed arguments
}

// InvocationResult represents the result of a function call.
type InvocationResult struct {
	InvocationID int32
	Success      bool
	Result       []interface{} // typed results
}

// Command represents a Glow command (GetDirectory, Subscribe, etc.).
type Command struct {
	Number    int64 // CmdGetDirectory, CmdSubscribe, etc.
	DirMask   int64 // field mask for GetDirectory (optional)
	InvID     int32 // invocation ID (for Invoke)
}

// Element is a union type — a tree element is one of Node, Parameter,
// Matrix, or Function. Exactly one field is non-nil.
type Element struct {
	Node      *Node
	Parameter *Parameter
	Matrix    *Matrix
	Function  *Function
	Command   *Command
}

// StreamEntry represents a streamed parameter value.
type StreamEntry struct {
	StreamIdentifier int64
	Value            interface{}
}
