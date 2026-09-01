// Layer-3 IS-12 Control Protocol provider — the NCP WebSocket.
//
// IS-12 is MS-05-02 over a WebSocket: a Controller opens the socket a
// Device advertises as urn:x-nmos:control:ncp/v1.0 and drives the SAME
// device model IS-14 serves over REST — Commands address objects by
// oid where IS-14 addresses them by role path, but they are one model
// with one state. Serving them from the same store is not an
// optimisation, it is the spec's own consistency requirement: a
// property written over IS-12 must read back changed over IS-14.
//
// The codec (dhs/internal/amwa/codec/is12) owns the six wire frames;
// this file owns dispatch: which oid, which method, what result — and
// the subscription fan-out that turns a successful Set into
// PropertyChanged notifications for every subscribed socket.

package provider

import (
	"encoding/json"
	"fmt"
	"log/slog"
	stdhttp "net/http"
	"reflect"
	"strings"
	"sync"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is12"
	"dhs/internal/amwa/codec/ms05"
	httpsession "dhs/internal/amwa/session/http"
)

// ncpWireVersion is the IS-12 minor this endpoint serves.
const ncpWireVersion = "v1.0"

// NCPControlType is the device-control URN for IS-12.
const NCPControlType = "urn:x-nmos:control:ncp/"

// IS12NCPServer serves the NCP WebSocket over an IS-14 configuration
// store.
type IS12NCPServer struct {
	logger *slog.Logger
	config *IS14ConfigurationServer

	mu    sync.Mutex
	conns map[*ncpConn]struct{}
}

// ncpConn is one connected Controller with its subscription set.
type ncpConn struct {
	ws   *httpsession.WebSocket
	subs map[int]struct{} // subscribed oids
	// sendMu serialises writes: responses from the read loop and
	// notifications from other connections' Sets share one socket.
	sendMu sync.Mutex
}

// NewIS12NCPServer wires the NCP endpoint to the shared device model.
func NewIS12NCPServer(logger *slog.Logger, config *IS14ConfigurationServer) *IS12NCPServer {
	if logger == nil {
		logger = slog.Default()
	}
	s := &IS12NCPServer{logger: logger, config: config, conns: map[*ncpConn]struct{}{}}
	config.SetOnPropertyChanged(s.notifyPropertyChanged)
	return s
}

// Mount registers the WebSocket route. IS-12 has no REST surface —
// the version path IS the socket.
func (s *IS12NCPServer) Mount(srv *httpsession.Server) {
	srv.HandleRaw("/x-nmos/ncp/"+ncpWireVersion, stdhttp.HandlerFunc(s.serveWS))
}

func (s *IS12NCPServer) serveWS(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	ws, err := httpsession.AcceptWebSocket(w, r)
	if err != nil {
		s.logger.Warn("provider/ncp: upgrade", "err", err)
		return
	}
	c := &ncpConn{ws: ws, subs: map[int]struct{}{}}
	s.mu.Lock()
	s.conns[c] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.conns, c)
		s.mu.Unlock()
		_ = ws.Close()
	}()

	for {
		raw, err := ws.ReadText()
		if err != nil {
			return
		}
		msg, err := is12.Decode(raw)
		if err != nil {
			// IS-12 §6.4: a frame the Node cannot parse is answered
			// with a protocol Error message, not a dropped socket — the
			// Controller keeps its subscriptions.
			s.send(c, is12.ErrorMessage{
				Status:       int(ms05.NcMethodStatusBadCommandFormat),
				ErrorMessage: err.Error(),
			})
			continue
		}
		switch m := msg.(type) {
		case is12.CommandMessage:
			s.send(c, s.handleCommands(m))
		case is12.SubscriptionMessage:
			s.send(c, s.handleSubscription(c, m))
		default:
			// A Node receives Commands and Subscriptions; the other
			// four types travel Node→Controller only.
			s.send(c, is12.ErrorMessage{
				Status:       int(ms05.NcMethodStatusInvalidRequest),
				ErrorMessage: fmt.Sprintf("message type %s is not valid towards a Node", msg.Kind()),
			})
		}
	}
}

func (s *IS12NCPServer) send(c *ncpConn, m is12.Message) {
	raw, err := is12.Encode(m)
	if err != nil {
		s.logger.Warn("provider/ncp: encode", "err", err)
		return
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if err := c.ws.SendText(raw); err != nil {
		s.logger.Warn("provider/ncp: send", "err", err)
	}
}

// handleSubscription records the requested oids that name real objects
// and answers with the accepted set (IS-12 §6.3: unknown oids are
// simply not subscribed).
func (s *IS12NCPServer) handleSubscription(c *ncpConn, m is12.SubscriptionMessage) is12.SubscriptionResponseMessage {
	accepted := make([]int, 0, len(m.Subscriptions))
	next := map[int]struct{}{}
	for _, oid := range m.Subscriptions {
		if s.config.objectByOid(oid) == nil {
			continue
		}
		next[oid] = struct{}{}
		accepted = append(accepted, oid)
	}
	// The message carries the WHOLE desired set — a subscription list
	// replaces the previous one rather than accumulating.
	c.subs = next
	return is12.SubscriptionResponseMessage{Subscriptions: accepted}
}

// notifyPropertyChanged fans one successful property write out to
// every socket subscribed to that oid.
func (s *IS12NCPServer) notifyPropertyChanged(oid ms05.NcOid, id ms05.NcPropertyId, value any) {
	raw, err := json.Marshal(value)
	if err != nil {
		return
	}
	n := is12.NotificationMessage{Notifications: []is12.Notification{{
		OID:     int(oid),
		EventID: is12.EventID{Level: 1, Index: 1}, // NcObject PropertyChanged
		EventData: is12.PropertyChangedEventData{
			PropertyID: is12.PropertyID{Level: int(id.Level), Index: int(id.Index)},
			ChangeType: 0, // ValueChanged
			Value:      raw,
		},
	}}}
	s.mu.Lock()
	targets := make([]*ncpConn, 0, len(s.conns))
	for c := range s.conns {
		if _, ok := c.subs[int(oid)]; ok {
			targets = append(targets, c)
		}
	}
	s.mu.Unlock()
	for _, c := range targets {
		s.send(c, n)
	}
}

// handleCommands runs every command and answers each handle.
func (s *IS12NCPServer) handleCommands(m is12.CommandMessage) is12.CommandResponseMessage {
	out := is12.CommandResponseMessage{Responses: make([]is12.CommandResponseEntry, 0, len(m.Commands))}
	for _, cmd := range m.Commands {
		out.Responses = append(out.Responses, is12.CommandResponseEntry{
			Handle: cmd.Handle,
			Result: s.runCommand(cmd),
		})
	}
	return out
}

// runCommand dispatches one command to the shared model. The result is
// rendered to the wire MethodResult here so every arm can return the
// typed ms05 result the schemas define.
func (s *IS12NCPServer) runCommand(cmd is12.Command) is12.MethodResult {
	obj := s.config.objectByOid(cmd.OID)
	if obj == nil {
		return ncpErr(ms05.NcMethodStatusBadOid, fmt.Sprintf("no object with oid %d", cmd.OID))
	}
	switch (is12.MethodID{Level: cmd.MethodID.Level, Index: cmd.MethodID.Index}) {
	case is12.MethodID{Level: 1, Index: 1}: // NcObject.Get
		return s.methodGet(obj, cmd.Arguments)
	case is12.MethodID{Level: 1, Index: 2}: // NcObject.Set
		return s.methodSet(obj, cmd.Arguments)
	case is12.MethodID{Level: 1, Index: 3}: // GetSequenceItem
		return s.methodSequence(obj, cmd.Arguments, "get")
	case is12.MethodID{Level: 1, Index: 4}: // SetSequenceItem
		return s.methodSequence(obj, cmd.Arguments, "set")
	case is12.MethodID{Level: 1, Index: 5}: // AddSequenceItem
		return s.methodSequence(obj, cmd.Arguments, "add")
	case is12.MethodID{Level: 1, Index: 6}: // RemoveSequenceItem
		return s.methodSequence(obj, cmd.Arguments, "remove")
	case is12.MethodID{Level: 1, Index: 7}: // GetSequenceLength
		return s.methodSequence(obj, cmd.Arguments, "length")
	case is12.MethodID{Level: 2, Index: 1}: // NcBlock.GetMemberDescriptors
		return s.methodMembers(obj, func(*configObject) bool { return true })
	case is12.MethodID{Level: 2, Index: 2}: // FindMembersByPath
		return s.methodFindByPath(obj, cmd.Arguments)
	case is12.MethodID{Level: 2, Index: 3}: // FindMembersByRole
		return s.methodFindByRole(obj, cmd.Arguments)
	case is12.MethodID{Level: 2, Index: 4}: // FindMembersByClassId
		return s.methodFindByClass(obj, cmd.Arguments)
	case is12.MethodID{Level: 3, Index: 1}: // ClassManager.GetControlClass
		return s.methodGetControlClass(obj, cmd.Arguments)
	case is12.MethodID{Level: 3, Index: 2}: // ClassManager.GetDatatype
		return s.methodGetDatatype(obj, cmd.Arguments)
	case is12.MethodID{Level: 4, Index: 1}, is12.MethodID{Level: 4, Index: 2}, is12.MethodID{Level: 4, Index: 3}:
		// BCP-008 monitor methods (NcReceiverMonitor 4m1/4m2 counter
		// getters + 4m3 reset; NcSenderMonitor 4m1 getter + 4m2 reset).
		if classDerivedFrom(obj.classID, ms05.NcClassId{1, 2, 2}) {
			return s.methodMonitor(obj, cmd.MethodID)
		}
		// DhsGainControl.SetGainDb shares the 4m1 slot on a different
		// class branch.
		if cmd.MethodID == (is12.MethodID{Level: 4, Index: 1}) && classKey(obj.classID) == classKey(vendorClassID) {
			return s.methodSetGainDb(obj, cmd.Arguments)
		}
	}
	return ncpErr(ms05.NcMethodStatusMethodNotImplemented,
		fmt.Sprintf("method %dm%d is not implemented on oid %d", cmd.MethodID.Level, cmd.MethodID.Index, cmd.OID))
}

// classKey renders an NcClassId ([]int32 alias — not comparable) as a
// stable map/compare key.
func classKey(id ms05.NcClassId) string { return fmt.Sprint(id) }

// classDerivedFrom reports whether id equals or descends from base —
// MS-05 class ids inherit by prefix ([1,3,1] derives from [1] and
// [1,3]).
func classDerivedFrom(id, base ms05.NcClassId) bool {
	if len(base) > len(id) {
		return false
	}
	for i := range base {
		if id[i] != base[i] {
			return false
		}
	}
	return true
}

// asSequence views any slice-valued property as []any. The model seeds
// sequences as TYPED slices ([]NcBlockMemberDescriptor,
// []NcClassDescriptor, …) — the wire does not care, so the generic
// sequence methods must not either.
func asSequence(v any) ([]any, bool) {
	if v == nil {
		return nil, false
	}
	if s, ok := v.([]any); ok {
		return s, true
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		return nil, false
	}
	out := make([]any, rv.Len())
	for i := range out {
		out[i] = rv.Index(i).Interface()
	}
	return out, true
}

// ncpErr renders an error MethodResult with the message the schemas
// require alongside every non-2xx status.
func ncpErr(st ms05.NcMethodStatus, msg string) is12.MethodResult {
	raw, _ := json.Marshal(ms05.NcMethodResultError{Status: st, ErrorMessage: msg})
	return wireResult(int(st), raw)
}

// wireResult re-packs a fully-rendered ms05 result body into the is12
// MethodResult frame slot. The ms05 body already carries `status`;
// is12.MethodResult mirrors it so transports can short-circuit.
func wireResult(status int, body json.RawMessage) is12.MethodResult {
	var peek struct {
		Value        json.RawMessage `json:"value"`
		ErrorMessage string          `json:"errorMessage"`
	}
	_ = json.Unmarshal(body, &peek)
	return is12.MethodResult{Status: status, Value: peek.Value, ErrorMessage: peek.ErrorMessage}
}

func ncpOKValue(v any) is12.MethodResult {
	raw, err := json.Marshal(v)
	if err != nil {
		return ncpErr(ms05.NcMethodStatusDeviceError, err.Error())
	}
	return is12.MethodResult{Status: int(ms05.NcMethodStatusOk), Value: raw}
}

func ncpOK() is12.MethodResult {
	return is12.MethodResult{Status: int(ms05.NcMethodStatusOk)}
}

type ncpIDArg struct {
	ID is12.PropertyID `json:"id"`
}

func (s *IS12NCPServer) methodGet(obj *configObject, args json.RawMessage) is12.MethodResult {
	var a ncpIDArg
	if err := json.Unmarshal(args, &a); err != nil {
		return ncpErr(ms05.NcMethodStatusParameterError, "Get: "+err.Error())
	}
	p := obj.findProp(fmt.Sprintf("%dp%d", a.ID.Level, a.ID.Index))
	if p == nil {
		return ncpErr(ms05.NcMethodStatusPropertyNotImplemented,
			fmt.Sprintf("no property %dp%d on oid %d", a.ID.Level, a.ID.Index, obj.oid))
	}
	s.config.mu.RLock()
	v := p.value
	s.config.mu.RUnlock()
	return ncpOKValue(v)
}

func (s *IS12NCPServer) methodSet(obj *configObject, args json.RawMessage) is12.MethodResult {
	var a struct {
		ID    is12.PropertyID `json:"id"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return ncpErr(ms05.NcMethodStatusParameterError, "Set: "+err.Error())
	}
	p := obj.findProp(fmt.Sprintf("%dp%d", a.ID.Level, a.ID.Index))
	if p == nil {
		return ncpErr(ms05.NcMethodStatusPropertyNotImplemented,
			fmt.Sprintf("no property %dp%d on oid %d", a.ID.Level, a.ID.Index, obj.oid))
	}
	// setProperty locks the store itself and fires the model-changed +
	// property-changed hooks on success.
	if st, err := s.config.setProperty(obj, p, a.Value); err != nil {
		return ncpErr(st, err.Error())
	}
	return ncpOK()
}

// methodSetGainDb implements DhsGainControl.SetGainDb (4m1) — a named
// write of gainDb (4p2) through the shared setProperty gate, so the
// parameter constraints it declares are enforced for real.
func (s *IS12NCPServer) methodSetGainDb(obj *configObject, args json.RawMessage) is12.MethodResult {
	var a struct {
		GainDb json.RawMessage `json:"gainDb"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return ncpErr(ms05.NcMethodStatusParameterError, "SetGainDb: "+err.Error())
	}
	if a.GainDb == nil {
		return ncpErr(ms05.NcMethodStatusParameterError, "SetGainDb: gainDb argument required")
	}
	p := obj.findProp("4p2")
	if p == nil {
		return ncpErr(ms05.NcMethodStatusPropertyNotImplemented,
			fmt.Sprintf("no gainDb property on oid %d", obj.oid))
	}
	if st, err := s.config.setProperty(obj, p, a.GainDb); err != nil {
		return ncpErr(st, err.Error())
	}
	return ncpOK()
}

// methodSequence implements the five NcObject sequence methods over a
// []any property value.
func (s *IS12NCPServer) methodSequence(obj *configObject, args json.RawMessage, op string) is12.MethodResult {
	var a struct {
		ID    is12.PropertyID `json:"id"`
		Index *int            `json:"index"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return ncpErr(ms05.NcMethodStatusParameterError, op+": "+err.Error())
	}
	p := obj.findProp(fmt.Sprintf("%dp%d", a.ID.Level, a.ID.Index))
	if p == nil {
		return ncpErr(ms05.NcMethodStatusPropertyNotImplemented,
			fmt.Sprintf("no property %dp%d on oid %d", a.ID.Level, a.ID.Index, obj.oid))
	}
	s.config.mu.Lock()
	defer s.config.mu.Unlock()
	seq, isSeq := asSequence(p.value)
	if !isSeq {
		return ncpErr(ms05.NcMethodStatusInvalidRequest,
			fmt.Sprintf("property %s is not a sequence", p.desc.Name))
	}
	inBounds := func() bool { return a.Index != nil && *a.Index >= 0 && *a.Index < len(seq) }
	switch op {
	case "length":
		return ncpOKValue(len(seq))
	case "get":
		if !inBounds() {
			return ncpErr(ms05.NcMethodStatusIndexOutOfBounds, "GetSequenceItem: index out of bounds")
		}
		return ncpOKValue(seq[*a.Index])
	case "set":
		if p.desc.IsReadOnly {
			return ncpErr(ms05.NcMethodStatusReadonly, "sequence property is readonly")
		}
		if !inBounds() {
			return ncpErr(ms05.NcMethodStatusIndexOutOfBounds, "SetSequenceItem: index out of bounds")
		}
		var v any
		if err := json.Unmarshal(a.Value, &v); err != nil {
			return ncpErr(ms05.NcMethodStatusParameterError, err.Error())
		}
		seq[*a.Index] = v
		p.value = seq
		return ncpOK()
	case "add":
		if p.desc.IsReadOnly {
			return ncpErr(ms05.NcMethodStatusReadonly, "sequence property is readonly")
		}
		var v any
		if err := json.Unmarshal(a.Value, &v); err != nil {
			return ncpErr(ms05.NcMethodStatusParameterError, err.Error())
		}
		p.value = append(seq, v)
		return ncpOKValue(len(seq))
	case "remove":
		if p.desc.IsReadOnly {
			return ncpErr(ms05.NcMethodStatusReadonly, "sequence property is readonly")
		}
		if !inBounds() {
			return ncpErr(ms05.NcMethodStatusIndexOutOfBounds, "RemoveSequenceItem: index out of bounds")
		}
		p.value = append(seq[:*a.Index], seq[*a.Index+1:]...)
		return ncpOK()
	}
	return ncpErr(ms05.NcMethodStatusMethodNotImplemented, op)
}

// methodMembers renders the member descriptors of a block. Only the
// root block (oid 1) contains members in this flat model — matching
// what IS-14's rolePaths listing serves.
func (s *IS12NCPServer) methodMembers(obj *configObject, keep func(*configObject) bool) is12.MethodResult {
	if int(obj.oid) != 1 {
		return ncpErr(ms05.NcMethodStatusInvalidRequest,
			fmt.Sprintf("oid %d is not a block", obj.oid))
	}
	s.config.mu.RLock()
	defer s.config.mu.RUnlock()
	out := []ms05.NcBlockMemberDescriptor{}
	for _, key := range s.config.order {
		o := s.config.objects[key]
		if int(o.oid) == 1 {
			continue // the block does not list itself
		}
		if keep(o) {
			out = append(out, o.memberDescriptor())
		}
	}
	return ncpOKValue(out)
}

func (s *IS12NCPServer) methodFindByPath(obj *configObject, args json.RawMessage) is12.MethodResult {
	var a struct {
		Path []string `json:"path"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return ncpErr(ms05.NcMethodStatusParameterError, "FindMembersByPath: "+err.Error())
	}
	if len(a.Path) == 0 {
		return ncpErr(ms05.NcMethodStatusParameterError, "FindMembersByPath: empty path")
	}
	return s.methodMembers(obj, func(o *configObject) bool {
		// The argument path is relative to the block: ["DeviceManager"].
		rel := o.path
		if len(rel) > 0 && rel[0] == "root" {
			rel = rel[1:]
		}
		if len(rel) != len(a.Path) {
			return false
		}
		for i := range rel {
			if rel[i] != a.Path[i] {
				return false
			}
		}
		return true
	})
}

func (s *IS12NCPServer) methodFindByRole(obj *configObject, args json.RawMessage) is12.MethodResult {
	var a struct {
		Role            string `json:"role"`
		CaseSensitive   *bool  `json:"caseSensitive"`
		MatchWholeString *bool `json:"matchWholeString"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return ncpErr(ms05.NcMethodStatusParameterError, "FindMembersByRole: "+err.Error())
	}
	if a.Role == "" {
		return ncpErr(ms05.NcMethodStatusParameterError, "FindMembersByRole: empty role")
	}
	// MS-05-02 NcBlock.FindMembersByRole: caseSensitive defaults true,
	// matchWholeString defaults false (substring match).
	caseSensitive := a.CaseSensitive == nil || *a.CaseSensitive
	whole := a.MatchWholeString != nil && *a.MatchWholeString
	want := a.Role
	if !caseSensitive {
		want = strings.ToLower(want)
	}
	return s.methodMembers(obj, func(o *configObject) bool {
		role := o.role
		if !caseSensitive {
			role = strings.ToLower(role)
		}
		if whole {
			return role == want
		}
		return strings.Contains(role, want)
	})
}

func (s *IS12NCPServer) methodFindByClass(obj *configObject, args json.RawMessage) is12.MethodResult {
	var a struct {
		ClassID        ms05.NcClassId `json:"classId"`
		IncludeDerived *bool          `json:"includeDerived"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return ncpErr(ms05.NcMethodStatusParameterError, "FindMembersByClassId: "+err.Error())
	}
	derived := a.IncludeDerived != nil && *a.IncludeDerived
	want := classKey(a.ClassID)
	return s.methodMembers(obj, func(o *configObject) bool {
		if derived {
			return classDerivedFrom(o.classID, a.ClassID)
		}
		return classKey(o.classID) == want
	})
}

func (s *IS12NCPServer) methodGetControlClass(obj *configObject, args json.RawMessage) is12.MethodResult {
	var a struct {
		ClassID          ms05.NcClassId `json:"classId"`
		IncludeInherited *bool          `json:"includeInherited"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return ncpErr(ms05.NcMethodStatusParameterError, "GetControlClass: "+err.Error())
	}
	// includeInherited=false answers the class's OWN elements only —
	// inheritance stays expressed via classId/parent (test_ms05_13).
	lookup := ms05.FlattenedClass
	if a.IncludeInherited != nil && !*a.IncludeInherited {
		lookup = ms05.StandardClass
	}
	class, ok := lookup(a.ClassID)
	if !ok {
		return ncpErr(ms05.NcMethodStatusParameterError,
			fmt.Sprintf("no class %s", classKey(a.ClassID)))
	}
	return ncpOKValue(class)
}

func (s *IS12NCPServer) methodGetDatatype(obj *configObject, args json.RawMessage) is12.MethodResult {
	var a struct {
		Name             string `json:"name"`
		IncludeInherited *bool  `json:"includeInherited"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return ncpErr(ms05.NcMethodStatusParameterError, "GetDatatype: "+err.Error())
	}
	lookup := ms05.FlattenedDatatype
	if a.IncludeInherited != nil && !*a.IncludeInherited {
		lookup = ms05.StandardDatatype
	}
	dt, ok := lookup(a.Name)
	if !ok {
		return ncpErr(ms05.NcMethodStatusParameterError, fmt.Sprintf("no datatype %q", a.Name))
	}
	return ncpOKValue(dt)
}

// methodMonitor implements the BCP-008 monitor methods. A reference
// node carries no media plane, so the packet/error counter getters
// truthfully answer an empty counter list; reset clears the transition
// counters and messages the model does carry.
func (s *IS12NCPServer) methodMonitor(obj *configObject, id is12.MethodID) is12.MethodResult {
	isReceiver := classDerivedFrom(obj.classID, ms05.NcClassId{1, 2, 2, 1})
	resetIdx := 2 // NcSenderMonitor.ResetCountersAndMessages = 4m2
	if isReceiver {
		resetIdx = 3 // NcReceiverMonitor.ResetCountersAndMessages = 4m3
	}
	if id.Index == resetIdx {
		s.config.mu.Lock()
		for _, p := range obj.props {
			if strings.HasSuffix(p.desc.Name, "TransitionCounter") {
				p.value = uint64(0)
			}
			if strings.HasSuffix(p.desc.Name, "Message") && p.desc.IsNullable {
				p.value = nil
			}
		}
		s.config.mu.Unlock()
		return ncpOK()
	}
	// Remaining indices are counter getters.
	return ncpOKValue([]any{})
}

// attachNCPAPI mounts IS-12 and advertises it on every Device. The
// href scheme is ws — the control endpoint IS the socket.
func (s *IS04NodeServer) attachNCPAPI(srv *httpsession.Server) {
	if s.configuration == nil {
		return
	}
	s.ncp = NewIS12NCPServer(s.logger, s.configuration)
	s.ncp.Mount(srv)
	host := s.controlHost()
	wsScheme := "ws"
	if s.scheme() == "https" {
		wsScheme = "wss"
	}
	for i := range s.bundle.Devices {
		d := &s.bundle.Devices[i]
		upsertControl(&d.Controls, is04.DeviceControl{
			Type:          NCPControlType + ncpWireVersion,
			Href:          wsScheme + "://" + host + "/x-nmos/ncp/" + ncpWireVersion,
			Authorization: s.authOn(),
		})
	}
}
