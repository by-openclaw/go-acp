package emberplus

import (
	"context"
	"fmt"
	"log/slog"
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

	// Wait for tree data. Use a settle timer: after receiving data,
	// wait 2 seconds of silence before returning.
	settle := time.NewTimer(10 * time.Second) // initial timeout
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

func (p *Plugin) GetValue(ctx context.Context, req protocol.ValueRequest) (protocol.Value, error) {
	// Look up from tree.
	p.treeMu.RLock()
	defer p.treeMu.RUnlock()

	for _, entry := range p.tree {
		if (req.Label != "" && entry.obj.Label == req.Label) ||
			(req.ID >= 0 && entry.obj.ID == req.ID) {
			return entry.obj.Value, nil
		}
	}
	return protocol.Value{}, fmt.Errorf("emberplus: object not found")
}

func (p *Plugin) SetValue(ctx context.Context, req protocol.ValueRequest, val protocol.Value) (protocol.Value, error) {
	p.mu.Lock()
	s := p.session
	p.mu.Unlock()
	if s == nil {
		return protocol.Value{}, protocol.ErrNotConnected
	}

	// Find the parameter path.
	p.treeMu.RLock()
	var path []int32
	for _, entry := range p.tree {
		if (req.Label != "" && entry.obj.Label == req.Label) ||
			(req.ID >= 0 && entry.obj.ID == req.ID) {
			if entry.glowParam != nil {
				path = entry.glowParam.Path
			}
			break
		}
	}
	p.treeMu.RUnlock()

	if len(path) == 0 {
		return protocol.Value{}, fmt.Errorf("emberplus: parameter not found")
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

	// Find the parameter path.
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

	if len(path) == 0 {
		return fmt.Errorf("emberplus: parameter not found for subscribe")
	}

	p.subsMu.Lock()
	p.subs[key] = fn
	p.subsMu.Unlock()

	return s.SendSubscribe(path)
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
}

func (p *Plugin) processNode(n *glow.Node, parentPath []string) {
	path := buildPath(parentPath, n.Identifier, n.Number)
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
	path := buildPath(parentPath, param.Identifier, param.Number)
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

	// Map type and value.
	switch param.Type {
	case glow.ParamTypeInteger:
		obj.Kind = protocol.KindInt
		if v, ok := param.Value.(int64); ok {
			obj.Value = protocol.Value{Kind: protocol.KindInt, Int: v}
		}
	case glow.ParamTypeReal:
		obj.Kind = protocol.KindFloat
		if v, ok := param.Value.(float64); ok {
			obj.Value = protocol.Value{Kind: protocol.KindFloat, Float: v}
		}
	case glow.ParamTypeString:
		obj.Kind = protocol.KindString
		if v, ok := param.Value.(string); ok {
			obj.Value = protocol.Value{Kind: protocol.KindString, Str: v}
		}
	case glow.ParamTypeBoolean:
		obj.Kind = protocol.KindBool
		if v, ok := param.Value.(bool); ok {
			obj.Value = protocol.Value{Kind: protocol.KindBool, Bool: v}
		}
	case glow.ParamTypeEnum:
		obj.Kind = protocol.KindEnum
		if v, ok := param.Value.(int64); ok {
			obj.Value = protocol.Value{Kind: protocol.KindEnum, Enum: uint8(v), Uint: uint64(v)}
		}
		// Parse enum items from enumeration string.
		if param.Enumeration != "" {
			obj.EnumItems = strings.Split(param.Enumeration, "\n")
		}
		if param.EnumMap != nil {
			for _, label := range param.EnumMap {
				obj.EnumItems = append(obj.EnumItems, label)
			}
		}
	default:
		// Infer from value type if Type is not set.
		if param.Value != nil {
			switch param.Value.(type) {
			case int64:
				obj.Kind = protocol.KindInt
				obj.Value = protocol.Value{Kind: protocol.KindInt, Int: param.Value.(int64)}
			case float64:
				obj.Kind = protocol.KindFloat
				obj.Value = protocol.Value{Kind: protocol.KindFloat, Float: param.Value.(float64)}
			case string:
				obj.Kind = protocol.KindString
				obj.Value = protocol.Value{Kind: protocol.KindString, Str: param.Value.(string)}
			case bool:
				obj.Kind = protocol.KindBool
				obj.Value = protocol.Value{Kind: protocol.KindBool, Bool: param.Value.(bool)}
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

	// Notify subscribers.
	p.subsMu.RLock()
	fn := p.subs[key]
	p.subsMu.RUnlock()
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
	path := buildPath(parentPath, m.Identifier, m.Number)
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
	path := buildPath(parentPath, f.Identifier, f.Number)
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

func pathKey(path []string) string {
	return strings.Join(path, "/")
}
