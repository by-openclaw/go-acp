package emberplus

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"acp/internal/protocol"
	"acp/internal/protocol/emberplus/glow"
)

func init() {
	protocol.Register(&Factory{})
}

// Factory creates Ember+ plugin instances.
type Factory struct{}

func (f *Factory) Meta() protocol.ProtocolMeta {
	return protocol.ProtocolMeta{
		Name:        "emberplus",
		DefaultPort: DefaultPort,
		Description: "Ember+ (Glow/S101/TCP) consumer",
	}
}

func (f *Factory) New(logger *slog.Logger) protocol.Protocol {
	return &Plugin{
		logger: logger,
	}
}

// Plugin implements protocol.Protocol for Ember+ providers.
type Plugin struct {
	logger  *slog.Logger
	session *Session
	mu      sync.Mutex

	// Tree state built from received Glow elements.
	tree     map[string]*treeEntry // path string → entry
	treeMu   sync.RWMutex
	treeReady chan struct{} // closed when initial GetDirectory completes

	// Numeric path → identifier mapping for reconstructing string paths
	// from QualifiedNode/QualifiedParameter responses.
	// Key: numeric path like "1.1.2", Value: identifier string.
	numPath  map[string]string
	numPathMu sync.RWMutex

	// Subscription callbacks.
	subs   map[string]protocol.EventFunc // path string → callback
	subsMu sync.RWMutex
}

type treeEntry struct {
	obj     protocol.Object
	glowNode *glow.Node
	glowParam *glow.Parameter
	glowMatrix *glow.Matrix
	glowFunc *glow.Function
}

func (p *Plugin) Connect(ctx context.Context, ip string, port int) error {
	s := NewSession(p.logger)
	p.mu.Lock()
	p.session = s
	p.tree = make(map[string]*treeEntry)
	p.numPath = make(map[string]string)
	p.treeReady = make(chan struct{})
	p.subs = make(map[string]protocol.EventFunc)
	p.mu.Unlock()

	s.SetOnElement(p.handleElements)

	if err := s.Connect(ctx, ip, port); err != nil {
		return err
	}

	// Don't send GetDirectory on connect — wait for Walk() to do it.
	// The provider sends keep-alives first; we must respond before
	// it accepts our commands.
	return nil
}

func (p *Plugin) Disconnect() error {
	p.mu.Lock()
	s := p.session
	p.session = nil
	p.mu.Unlock()
	if s != nil {
		return s.Disconnect()
	}
	return nil
}

func (p *Plugin) GetDeviceInfo(ctx context.Context) (protocol.DeviceInfo, error) {
	return protocol.DeviceInfo{
		ProtocolVersion: 1,
	}, nil
}

func (p *Plugin) GetSlotInfo(ctx context.Context, slot int) (protocol.SlotInfo, error) {
	// Ember+ doesn't have slots — treat the whole tree as slot 0.
	if slot != 0 {
		return protocol.SlotInfo{}, fmt.Errorf("emberplus: only slot 0 supported")
	}
	return protocol.SlotInfo{
		Slot:   0,
		Status: protocol.SlotPresent,
	}, nil
}

func (p *Plugin) Walk(ctx context.Context, slot int) ([]protocol.Object, error) {
	if slot != 0 {
		return nil, fmt.Errorf("emberplus: only slot 0")
	}

	p.mu.Lock()
	s := p.session
	p.mu.Unlock()
	if s == nil {
		return nil, protocol.ErrNotConnected
	}

	// Send GetDirectory and wait for tree to populate.
	if err := s.SendGetDirectory(); err != nil {
		return nil, err
	}

	// Brief pause to let the readLoop start processing responses.
	time.Sleep(500 * time.Millisecond)

	// Wait for tree data. Use a settle timer: after receiving data,
	// wait 3 seconds of silence before returning.
	settle := time.NewTimer(15 * time.Second) // initial timeout
	defer settle.Stop()
	lastCount := 0
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-settle.C:
			goto done
		default:
			p.treeMu.RLock()
			count := len(p.tree)
			p.treeMu.RUnlock()
			if count > lastCount {
				lastCount = count
				settle.Reset(2 * time.Second) // got new data, wait for more
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
done:

	// Collect all objects from tree.
	p.treeMu.RLock()
	defer p.treeMu.RUnlock()
	var objs []protocol.Object
	for _, entry := range p.tree {
		objs = append(objs, entry.obj)
	}
	return objs, nil
}

// findEntry looks up a tree entry by path (preferred), label, or ID.
// Path is the dot-separated tree key — unambiguous.
func (p *Plugin) findEntry(req protocol.ValueRequest) (string, *treeEntry) {
	p.treeMu.RLock()
	defer p.treeMu.RUnlock()

	// 1. Path — direct map lookup, O(1), unambiguous.
	if req.Path != "" {
		if entry, ok := p.tree[req.Path]; ok {
			return req.Path, entry
		}
	}

	// 2. Label or ID — linear scan, first match (may be ambiguous).
	for key, entry := range p.tree {
		if (req.Label != "" && entry.obj.Label == req.Label) ||
			(req.ID >= 0 && entry.obj.ID == req.ID) {
			return key, entry
		}
	}
	return "", nil
}

func (p *Plugin) GetValue(ctx context.Context, req protocol.ValueRequest) (protocol.Value, error) {
	// Ember+ needs the tree populated. Walk if empty.
	p.treeMu.RLock()
	empty := len(p.tree) == 0
	p.treeMu.RUnlock()
	if empty {
		if _, err := p.Walk(ctx, 0); err != nil {
			return protocol.Value{}, fmt.Errorf("emberplus: walk for value lookup: %w", err)
		}
	}

	key, entry := p.findEntry(req)
	if entry == nil {
		return protocol.Value{}, fmt.Errorf("emberplus: object not found (tree has %d entries)", len(p.tree))
	}
	p.logger.Debug("emberplus: GetValue found", "key", key, "label", entry.obj.Label)
	return entry.obj.Value, nil
}

func (p *Plugin) SetValue(ctx context.Context, req protocol.ValueRequest, val protocol.Value) (protocol.Value, error) {
	p.mu.Lock()
	s := p.session
	p.mu.Unlock()
	if s == nil {
		return protocol.Value{}, protocol.ErrNotConnected
	}

	// Ember+ needs the tree populated to get the Glow path.
	p.treeMu.RLock()
	empty := len(p.tree) == 0
	p.treeMu.RUnlock()
	if empty {
		if _, err := p.Walk(ctx, 0); err != nil {
			return protocol.Value{}, fmt.Errorf("emberplus: walk for path resolution: %w", err)
		}
	}

	// Find the parameter by path (preferred), label, or ID.
	_, entry := p.findEntry(req)
	if entry == nil || entry.glowParam == nil {
		return protocol.Value{}, fmt.Errorf("emberplus: parameter not found")
	}
	path := entry.glowParam.Path
	paramKind := entry.obj.Kind

	if len(path) == 0 {
		return protocol.Value{}, fmt.Errorf("emberplus: parameter has no path")
	}

	// If caller didn't set Kind, use the parameter's known Kind.
	if val.Kind == protocol.KindUnknown {
		val.Kind = paramKind
	}

	// If the value was passed as a string (from CLI --value flag),
	// parse it into the correct typed field based on Kind.
	if val.Str != "" && val.Kind != protocol.KindString {
		switch val.Kind {
		case protocol.KindInt:
			if n, err := strconv.ParseInt(val.Str, 10, 64); err == nil {
				val.Int = n
				val.Str = "" // clear so display shows typed value
			}
		case protocol.KindUint:
			if n, err := strconv.ParseUint(val.Str, 10, 64); err == nil {
				val.Uint = n
				val.Str = ""
			}
		case protocol.KindFloat:
			if f, err := strconv.ParseFloat(val.Str, 64); err == nil {
				val.Float = f
				val.Str = ""
			}
		case protocol.KindBool:
			val.Bool = val.Str == "true" || val.Str == "1" || val.Str == "yes"
			val.Str = ""
		case protocol.KindEnum:
			if n, err := strconv.ParseUint(val.Str, 10, 8); err == nil {
				val.Enum = uint8(n)
				val.Uint = n
				val.Str = ""
			}
		}
	}

	// Convert protocol.Value to Glow value.
	var glowVal interface{}
	switch val.Kind {
	case protocol.KindInt:
		glowVal = val.Int
	case protocol.KindUint:
		glowVal = int64(val.Uint)
	case protocol.KindFloat:
		glowVal = val.Float
	case protocol.KindString:
		glowVal = val.Str
	case protocol.KindBool:
		glowVal = val.Bool
	default:
		if val.Str != "" {
			glowVal = val.Str
		} else {
			glowVal = val.Int
		}
	}

	if err := s.SendSetValue(path, glowVal); err != nil {
		return protocol.Value{}, err
	}

	// Return the value we sent (confirmed value comes via notification).
	return val, nil
}

func (p *Plugin) Subscribe(req protocol.ValueRequest, fn protocol.EventFunc) error {
	p.mu.Lock()
	s := p.session
	p.mu.Unlock()
	if s == nil {
		return protocol.ErrNotConnected
	}

	// "Subscribe all" — register a wildcard callback.
	if req.Path == "" && req.Label == "" && req.ID < 0 {
		p.subsMu.Lock()
		p.subs["*"] = fn
		p.subsMu.Unlock()
		return nil
	}

	// Ember+ needs the tree for path resolution. Walk if empty.
	p.treeMu.RLock()
	empty := len(p.tree) == 0
	p.treeMu.RUnlock()
	if empty {
		if _, err := p.Walk(context.Background(), 0); err != nil {
			return fmt.Errorf("emberplus: walk for subscribe: %w", err)
		}
	}

	key, entry := p.findEntry(req)
	if entry == nil || entry.glowParam == nil || len(entry.glowParam.Path) == 0 {
		return fmt.Errorf("emberplus: parameter not found for subscribe")
	}

	p.subsMu.Lock()
	p.subs[key] = fn
	p.subsMu.Unlock()

	return s.SendSubscribe(entry.glowParam.Path)
}

func (p *Plugin) Unsubscribe(req protocol.ValueRequest) error {
	p.mu.Lock()
	s := p.session
	p.mu.Unlock()
	if s == nil {
		return nil
	}

	p.treeMu.RLock()
	var path []int32
	var key string
	for k, entry := range p.tree {
		if (req.Label != "" && entry.obj.Label == req.Label) ||
			(req.ID >= 0 && entry.obj.ID == req.ID) {
			if entry.glowParam != nil {
				path = entry.glowParam.Path
				key = k
			}
			break
		}
	}
	p.treeMu.RUnlock()

	p.subsMu.Lock()
	delete(p.subs, key)
	p.subsMu.Unlock()

	if len(path) > 0 {
		return s.SendUnsubscribe(path)
	}
	return nil
}

// MatrixConnect sends a matrix crosspoint connection command.
// matrixPath is the dot-separated path to the matrix (e.g. "router.oneToN.matrix").
// target is the target number, sources is the list of source numbers.
// operation: 0=absolute (replace all), 1=connect (add), 2=disconnect (remove).
func (p *Plugin) MatrixConnect(ctx context.Context, matrixPath string, target int32, sources []int32, operation int64) error {
	p.mu.Lock()
	s := p.session
	p.mu.Unlock()
	if s == nil {
		return protocol.ErrNotConnected
	}

	// Walk if tree empty to resolve paths.
	p.treeMu.RLock()
	empty := len(p.tree) == 0
	p.treeMu.RUnlock()
	if empty {
		if _, err := p.Walk(ctx, 0); err != nil {
			return fmt.Errorf("emberplus: walk for matrix: %w", err)
		}
	}

	// Find the matrix entry to get its numeric path.
	p.treeMu.RLock()
	var numPath []int32
	if entry, ok := p.tree[matrixPath]; ok && entry.glowMatrix != nil {
		numPath = entry.glowMatrix.Path
	}
	p.treeMu.RUnlock()

	if len(numPath) == 0 {
		return fmt.Errorf("emberplus: matrix not found at path %q", matrixPath)
	}

	p.logger.Debug("emberplus: MatrixConnect",
		"matrix_path", matrixPath,
		"numeric_path", numPath,
		"target", target,
		"sources", sources,
		"operation", operation)

	return s.SendMatrixConnect(numPath, target, sources, operation)
}

// InvokeFunction calls an Ember+ function by path with typed arguments.
// Returns the InvocationResult (invocation ID, success, result tuple).
// Per spec: invocationId is optional (fire-and-forget for void functions).
func (p *Plugin) InvokeFunction(ctx context.Context, funcPath string, args []interface{}) (*glow.InvocationResult, error) {
	p.mu.Lock()
	s := p.session
	p.mu.Unlock()
	if s == nil {
		return nil, protocol.ErrNotConnected
	}

	// Walk if tree empty to resolve paths.
	p.treeMu.RLock()
	empty := len(p.tree) == 0
	p.treeMu.RUnlock()
	if empty {
		if _, err := p.Walk(ctx, 0); err != nil {
			return nil, fmt.Errorf("emberplus: walk for invoke: %w", err)
		}
	}

	// Find the function entry to get its numeric path.
	p.treeMu.RLock()
	var numPath []int32
	if entry, ok := p.tree[funcPath]; ok && entry.glowFunc != nil {
		numPath = entry.glowFunc.Path
	}
	p.treeMu.RUnlock()

	if len(numPath) == 0 {
		return nil, fmt.Errorf("emberplus: function not found at path %q", funcPath)
	}

	// Allocate invocation ID and set up result channel.
	invID := s.NextInvocationID()
	resultCh := make(chan *glow.InvocationResult, 1)
	s.RegisterInvocation(invID, resultCh)
	defer s.UnregisterInvocation(invID)

	p.logger.Debug("emberplus: InvokeFunction",
		"path", funcPath,
		"numeric_path", numPath,
		"invocation_id", invID,
		"args", args)

	if err := s.SendInvoke(numPath, invID, args); err != nil {
		return nil, err
	}

	// Wait for result with timeout.
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resultCh:
		return result, nil
	}
}

// handleElements processes incoming Glow elements from the provider.
func (p *Plugin) handleElements(elements []glow.Element) {
	for _, el := range elements {
		p.processElement(el, nil)
	}
}

func (p *Plugin) processElement(el glow.Element, parentPath []string) {
	if el.Node != nil {
		p.processNode(el.Node, parentPath)
	}
	if el.Parameter != nil {
		p.processParameter(el.Parameter, parentPath)
	}
	if el.Matrix != nil {
		p.processMatrix(el.Matrix, parentPath)
	}
	if el.Function != nil {
		p.processFunction(el.Function, parentPath)
	}
	if el.InvocationResult != nil {
		p.mu.Lock()
		s := p.session
		p.mu.Unlock()
		if s != nil {
			s.deliverInvocationResult(el.InvocationResult)
		}
	}
}

func (p *Plugin) processNode(n *glow.Node, parentPath []string) {
	// Register numeric path → identifier mapping for QualifiedNode resolution.
	if len(n.Path) > 0 {
		p.registerNumericPath(n.Path, n.Identifier)
	}

	// Build string path: prefer numeric path resolution (full hierarchy),
	// fall back to parent+identifier for non-qualified nodes.
	var path []string
	if len(n.Path) > 0 {
		path = p.resolveStringPath(n.Path, n.Identifier)
	} else {
		path = buildPath(parentPath, n.Identifier, n.Number)
	}
	key := pathKey(path)

	entry := &treeEntry{
		glowNode: n,
		obj: protocol.Object{
			Slot:   0,
			ID:     int(n.Number),
			Label:  n.Identifier,
			Kind:   protocol.KindRaw, // nodes are containers
			Path:   path,
			Access: 1, // read
		},
	}
	if len(path) > 1 {
		entry.obj.Group = path[0]
	}

	p.treeMu.Lock()
	p.tree[key] = entry
	p.treeMu.Unlock()

	// Recurse into children and send GetDirectory for this node.
	for _, child := range n.Children {
		p.processElement(child, path)
	}

	// Request children if none received yet.
	if len(n.Children) == 0 && len(n.Path) > 0 {
		p.mu.Lock()
		s := p.session
		p.mu.Unlock()
		if s != nil {
			path := make([]int32, len(n.Path))
			copy(path, n.Path)
			go func() {
				p.logger.Debug("emberplus: requesting children", "path", path, "identifier", n.Identifier)
				_ = s.SendGetDirectoryFor(path)
			}()
		}
	}
}

func (p *Plugin) processParameter(param *glow.Parameter, parentPath []string) {
	if len(param.Path) > 0 {
		p.registerNumericPath(param.Path, param.Identifier)
	}

	var path []string
	if len(param.Path) > 0 {
		path = p.resolveStringPath(param.Path, param.Identifier)
	} else {
		path = buildPath(parentPath, param.Identifier, param.Number)
	}
	key := pathKey(path)

	obj := protocol.Object{
		Slot:   0,
		ID:     int(param.Number),
		Label:  param.Identifier,
		Path:   path,
	}
	if len(path) > 1 {
		obj.Group = path[0]
	}
	if param.Format != "" {
		obj.Unit = param.Format
	}

	// Map access.
	switch param.Access {
	case glow.AccessRead:
		obj.Access = 1
	case glow.AccessWrite:
		obj.Access = 2
	case glow.AccessReadWrite:
		obj.Access = 3
	}

	// Map type and value. Always set both obj.Kind AND obj.Value.Kind
	// so GetValue returns a properly typed Value even when the provider
	// doesn't send the value field in the initial response.
	switch param.Type {
	case glow.ParamTypeInteger:
		obj.Kind = protocol.KindInt
		obj.Value.Kind = protocol.KindInt
	case glow.ParamTypeReal:
		obj.Kind = protocol.KindFloat
		obj.Value.Kind = protocol.KindFloat
	case glow.ParamTypeString:
		obj.Kind = protocol.KindString
		obj.Value.Kind = protocol.KindString
	case glow.ParamTypeBoolean:
		obj.Kind = protocol.KindBool
		obj.Value.Kind = protocol.KindBool
	case glow.ParamTypeEnum:
		obj.Kind = protocol.KindEnum
		obj.Value.Kind = protocol.KindEnum
	}

	// Populate value from whatever the provider sent.
	if param.Value != nil {
		switch v := param.Value.(type) {
		case int64:
			if obj.Kind == protocol.KindUnknown {
				obj.Kind = protocol.KindInt
			}
			obj.Value.Kind = obj.Kind
			switch obj.Kind {
			case protocol.KindInt:
				obj.Value.Int = v
			case protocol.KindUint:
				obj.Value.Uint = uint64(v)
			case protocol.KindEnum:
				obj.Value.Enum = uint8(v)
				obj.Value.Uint = uint64(v)
			case protocol.KindFloat:
				obj.Value.Float = float64(v)
			default:
				obj.Value.Int = v
			}
		case float64:
			if obj.Kind == protocol.KindUnknown {
				obj.Kind = protocol.KindFloat
			}
			obj.Value.Kind = obj.Kind
			obj.Value.Float = v
		case string:
			if obj.Kind == protocol.KindUnknown {
				obj.Kind = protocol.KindString
			}
			obj.Value.Kind = obj.Kind
			obj.Value.Str = v
		case bool:
			if obj.Kind == protocol.KindUnknown {
				obj.Kind = protocol.KindBool
			}
			obj.Value.Kind = obj.Kind
			obj.Value.Bool = v
		}
	}

	// Enum items from enumeration string or map.
	if param.Type == glow.ParamTypeEnum || obj.Kind == protocol.KindEnum {
		if param.Enumeration != "" {
			obj.EnumItems = strings.Split(param.Enumeration, "\n")
		}
		if param.EnumMap != nil {
			for _, label := range param.EnumMap {
				obj.EnumItems = append(obj.EnumItems, label)
			}
		}
	}

	// Constraints.
	obj.Min = param.Minimum
	obj.Max = param.Maximum
	obj.Step = param.Step
	obj.Def = param.Default

	entry := &treeEntry{
		glowParam: param,
		obj:       obj,
	}

	p.treeMu.Lock()
	p.tree[key] = entry
	p.treeMu.Unlock()

	// Notify subscribers (specific key or wildcard).
	p.subsMu.RLock()
	fn := p.subs[key]
	wildcard := p.subs["*"]
	p.subsMu.RUnlock()
	if fn == nil {
		fn = wildcard
	}
	if fn != nil {
		fn(protocol.Event{
			Slot:      0,
			ID:        obj.ID,
			Label:     obj.Label,
			Group:     obj.Group,
			Value:     obj.Value,
			Timestamp: time.Now(),
		})
	}
}

func (p *Plugin) processMatrix(m *glow.Matrix, parentPath []string) {
	if len(m.Path) > 0 {
		p.registerNumericPath(m.Path, m.Identifier)
	}
	var path []string
	if len(m.Path) > 0 {
		path = p.resolveStringPath(m.Path, m.Identifier)
	} else {
		path = buildPath(parentPath, m.Identifier, m.Number)
	}
	key := pathKey(path)

	entry := &treeEntry{
		glowMatrix: m,
		obj: protocol.Object{
			Slot:   0,
			ID:     int(m.Number),
			Label:  m.Identifier,
			Kind:   protocol.KindRaw, // matrix is a special container
			Path:   path,
			Access: 3, // read-write
		},
	}
	if len(path) > 1 {
		entry.obj.Group = path[0]
	}

	p.treeMu.Lock()
	p.tree[key] = entry
	p.treeMu.Unlock()

	for _, child := range m.Children {
		p.processElement(child, path)
	}
}

func (p *Plugin) processFunction(f *glow.Function, parentPath []string) {
	if len(f.Path) > 0 {
		p.registerNumericPath(f.Path, f.Identifier)
	}
	var path []string
	if len(f.Path) > 0 {
		path = p.resolveStringPath(f.Path, f.Identifier)
	} else {
		path = buildPath(parentPath, f.Identifier, f.Number)
	}
	key := pathKey(path)

	entry := &treeEntry{
		glowFunc: f,
		obj: protocol.Object{
			Slot:   0,
			ID:     int(f.Number),
			Label:  f.Identifier,
			Kind:   protocol.KindRaw, // function is a special type
			Path:   path,
			Access: 2, // write (invoke)
		},
	}
	if len(path) > 1 {
		entry.obj.Group = path[0]
	}

	p.treeMu.Lock()
	p.tree[key] = entry
	p.treeMu.Unlock()

	for _, child := range f.Children {
		p.processElement(child, path)
	}
}

// --- helpers ---

func buildPath(parent []string, identifier string, number int32) []string {
	name := identifier
	if name == "" {
		name = fmt.Sprintf("%d", number)
	}
	path := make([]string, len(parent)+1)
	copy(path, parent)
	path[len(parent)] = name
	return path
}

// numericPathKey converts a numeric path []int32 to a dot-separated string
// for use as a map key: [1,2,3] → "1.2.3"
func numericPathKey(nums []int32) string {
	parts := make([]string, len(nums))
	for i, n := range nums {
		parts[i] = fmt.Sprintf("%d", n)
	}
	return strings.Join(parts, ".")
}

// resolveStringPath reconstructs the full string path from a numeric path
// by looking up each ancestor's identifier in the numPath map.
// For path [1,1,2,190]: looks up "1"→"router", "1.1"→"oneToN",
// "1.1.2"→"parameters", "1.1.2.190"→"t-190" → ["router","oneToN","parameters","t-190"]
func (p *Plugin) resolveStringPath(nums []int32, identifier string) []string {
	path := make([]string, len(nums))
	p.numPathMu.RLock()
	for i := range nums {
		prefix := numericPathKey(nums[:i+1])
		if name, ok := p.numPath[prefix]; ok {
			path[i] = name
		} else {
			path[i] = fmt.Sprintf("%d", nums[i])
		}
	}
	p.numPathMu.RUnlock()
	// Override the last element with the current identifier if available.
	if identifier != "" && len(path) > 0 {
		path[len(path)-1] = identifier
	}
	return path
}

// registerNumericPath records a numeric path → identifier mapping.
func (p *Plugin) registerNumericPath(nums []int32, identifier string) {
	if len(nums) == 0 {
		return
	}
	key := numericPathKey(nums)
	name := identifier
	if name == "" {
		name = fmt.Sprintf("%d", nums[len(nums)-1])
	}
	p.numPathMu.Lock()
	p.numPath[key] = name
	p.numPathMu.Unlock()
}

func pathKey(path []string) string {
	return strings.Join(path, ".")
}
