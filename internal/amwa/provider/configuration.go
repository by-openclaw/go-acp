// Layer-3 IS-14 Device Configuration provider — HTTP surface (AMWA
// IS-14 v1.0.0).
//
// IS-14 publishes the MS-05-02 Device Model over REST. Every object
// is addressed by its role path (roles joined with ".", starting at
// the root block) and offers, per the ConfigurationAPI RAML:
//
//	/x-nmos/configuration/{ver}/
//	  rolePaths/                                     GET  (all role paths)
//	  rolePaths/{rolePath}/                          GET  (subtree index)
//	  rolePaths/{rolePath}/descriptor/               GET  (flattened class descriptor)
//	  rolePaths/{rolePath}/methods/                  GET  (method id list)
//	  rolePaths/{rolePath}/methods/{methodId}/       PATCH (invoke)
//	  rolePaths/{rolePath}/properties/               GET  (property id list)
//	  rolePaths/{rolePath}/properties/{propertyId}/  GET  -> descriptor/ value/
//	  .../properties/{propertyId}/descriptor/        GET  (datatype descriptor)
//	  .../properties/{propertyId}/value/             GET PUT
//	  rolePaths/{rolePath}/bulkProperties/           GET PUT PATCH (backup / restore / validate)
//
// The device model is the honest minimum MS-05-01 requires: a root
// block (oid 1) owning the two mandatory managers — DeviceManager
// (oid 2) and ClassManager (oid 3). Class + datatype descriptors are
// served from the embedded MS-05-02 framework models (ms05
// StandardClasses / StandardDatatypes), never hand-typed copies.
//
// Error doctrine per the RAML: every error body is an
// NcMethodResultError (ms05-error.json). 404 = role path / element
// missing; 400 = client-side validation (including readonly writes,
// surfaced as NcMethodStatus 405 inside the body); 500 = ours.

package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	stdhttp "net/http"
	"sort"
	"strings"
	"sync"

	"dhs/internal/amwa/codec/is14"
	"dhs/internal/amwa/codec/ms05"
)

// IS14ConfigurationConfig configures the surface.
type IS14ConfigurationConfig struct {
	// APIVer pins one wire minor. Empty mounts every registered IS-14
	// codec (v1.0 today).
	APIVer string
}

// configProperty is one property slot of a model object: the
// framework descriptor plus its live value. Writability follows the
// descriptor — IS-14 adds no access rules of its own.
type configProperty struct {
	desc  ms05.NcPropertyDescriptor
	value any
}

// configObject is one Device Model object, addressed by role path.
type configObject struct {
	classID  ms05.NcClassId
	oid      ms05.NcOid
	role     string
	path     []string // role path as array, ["root", ...]
	class    ms05.NcClassDescriptor
	props    []*configProperty // flattened-descriptor order (own first)
}

// IS14ConfigurationServer serves the Configuration API for one Node.
type IS14ConfigurationServer struct {
	logger *slog.Logger
	vers   []string

	mu      sync.RWMutex
	objects map[string]*configObject // key = dotted role path
	order   []string                 // stable rolePaths listing order

	// onModelChanged reports a successful property write so the IS-04
	// side can bump Device versions (IS-04 interactions doc).
	onModelChanged func()
}

// propKey renders a property id in the {level}p{index} URL form.
func propKey(id ms05.NcPropertyId) string { return fmt.Sprintf("%dp%d", id.Level, id.Index) }

// methodKey renders a method id in the {level}m{index} URL form.
func methodKey(id ms05.NcMethodId) string { return fmt.Sprintf("%dm%d", id.Level, id.Index) }

// newConfigObject builds one model object from the embedded framework
// class, seeding property values from the provided name→value table.
// Every flattened property gets a slot so Get / backup answer for all
// of them; absent seeds stay nil (the framework marks those nullable).
func newConfigObject(classID ms05.NcClassId, oid ms05.NcOid, path []string, seed map[string]any) (*configObject, error) {
	class, ok := ms05.FlattenedClass(classID)
	if !ok {
		return nil, fmt.Errorf("provider/is14: no framework class %v", classID)
	}
	o := &configObject{
		classID: classID,
		oid:     oid,
		role:    path[len(path)-1],
		path:    path,
		class:   class,
	}
	var owner any
	if len(path) > 1 {
		owner = ms05.NcOid(1) // flat model: everything lives in root
	}
	base := map[string]any{
		"classId":                    classID,
		"oid":                        oid,
		"constantOid":                true,
		"owner":                      owner,
		"role":                       o.role,
		"touchpoints":                nil,
		"runtimePropertyConstraints": nil,
	}
	for _, d := range class.Properties {
		p := &configProperty{desc: d}
		if v, ok := seed[d.Name]; ok {
			p.value = v
		} else if v, ok := base[d.Name]; ok {
			p.value = v
		}
		o.props = append(o.props, p)
	}
	return o, nil
}

// findProp locates a property slot by its {level}p{index} key.
func (o *configObject) findProp(key string) *configProperty {
	for _, p := range o.props {
		if propKey(p.desc.ID) == key {
			return p
		}
	}
	return nil
}

// memberDescriptor renders the object as a block-member entry.
func (o *configObject) memberDescriptor() ms05.NcBlockMemberDescriptor {
	var label *string
	for _, p := range o.props {
		if p.desc.Name == "userLabel" {
			if s, ok := p.value.(string); ok {
				label = &s
			}
		}
	}
	return ms05.NcBlockMemberDescriptor{
		Role:        o.role,
		Oid:         o.oid,
		ConstantOid: true,
		ClassID:     o.classID,
		UserLabel:   label,
		Owner:       1,
	}
}

// NewIS14ConfigurationServer builds the Device Model from the Node
// bundle: root block + DeviceManager + ClassManager, labelled from
// the Node's own identity.
func NewIS14ConfigurationServer(logger *slog.Logger, bundle *NodeConfig, cfg IS14ConfigurationConfig) *IS14ConfigurationServer {
	vers := is14.SupportedVersions()
	if cfg.APIVer != "" {
		vers = []string{cfg.APIVer}
	}
	s := &IS14ConfigurationServer{
		logger:  logger,
		vers:    vers,
		objects: map[string]*configObject{},
	}

	nodeLabel, nodeID := "dhs-node", ""
	if bundle != nil {
		if bundle.Node.Label != "" {
			nodeLabel = bundle.Node.Label
		}
		nodeID = bundle.Node.ID
	}

	website := "https://github.com/by-openclaw/go-acp"
	prodDesc := "dhs AMWA NMOS reference node"
	dm, err := newConfigObject(ms05.NcClassId{1, 3, 1}, 2, []string{"root", "DeviceManager"}, map[string]any{
		"userLabel": "Device manager",
		"ncVersion": "v1.0.0",
		"manufacturer": ms05.NcManufacturer{
			Name:    "BY-Systems",
			Website: &website,
		},
		"product": ms05.NcProduct{
			Name:          "dhs",
			Key:           "dhs",
			RevisionLevel: "1.0.0",
			Description:   &prodDesc,
		},
		"serialNumber": nodeID,
		"deviceName":   nodeLabel,
		"operationalState": ms05.NcDeviceOperationalState{
			Generic: ms05.NcDeviceGenericStateNormalOperation,
		},
		"resetCause": ms05.NcResetCausePowerOn,
	})
	cm, err2 := newConfigObject(ms05.NcClassId{1, 3, 2}, 3, []string{"root", "ClassManager"}, map[string]any{
		"userLabel":      "Class manager",
		"controlClasses": ms05.StandardClasses(),
		"datatypes":      ms05.StandardDatatypes(),
	})
	root, err3 := newConfigObject(ms05.NcClassId{1, 1}, 1, []string{"root"}, map[string]any{
		"userLabel": nodeLabel,
		"enabled":   true,
	})
	if err != nil || err2 != nil || err3 != nil {
		// The framework models are compiled in; failing to load them is
		// a build defect, not a runtime condition.
		panic(fmt.Sprintf("provider/is14: framework model load: %v %v %v", err, err2, err3))
	}
	if p := root.findProp("2p2"); p != nil { // NcBlock.members
		p.value = []ms05.NcBlockMemberDescriptor{dm.memberDescriptor(), cm.memberDescriptor()}
	}

	for _, o := range []*configObject{root, dm, cm} {
		key := strings.Join(o.path, ".")
		s.objects[key] = o
		s.order = append(s.order, key)
	}
	return s
}

// Versions lists the mounted IS-14 minors.
func (s *IS14ConfigurationServer) Versions() []string { return s.vers }

// SetOnModelChanged installs the IS-04 version-bump hook.
func (s *IS14ConfigurationServer) SetOnModelChanged(fn func()) { s.onModelChanged = fn }

// ---- HTTP mounting ----

// ms05Err renders the ms05-error.json body with a matching HTTP code.
func ms05Err(httpStatus int, ncStatus ms05.NcMethodStatus, msg string) (int, any, error) {
	return httpStatus, ms05.NcMethodResultError{Status: ncStatus, ErrorMessage: msg}, nil
}

// Mount + attachConfigurationAPI live in configuration_mount.go,
// keeping this file focused on the model + handlers.

// rolePathsList renders the ordered role path listing with trailing
// slashes, per rolePaths-base-get-200.json.
func (s *IS14ConfigurationServer) rolePathsList() []string {
	out := make([]string, 0, len(s.order))
	for _, k := range s.order {
		out = append(out, k+"/")
	}
	return out
}

// dispatch resolves one request under {ver}/rolePaths/. tail is the
// URL remainder after "rolePaths/" with any trailing slash removed.
func (s *IS14ConfigurationServer) dispatch(method, tail string, r *stdhttp.Request) (int, any, error) {
	segs := []string{}
	if tail != "" {
		segs = strings.Split(tail, "/")
	}
	if len(segs) == 0 {
		if method != stdhttp.MethodGet {
			return ms05Err(405, ms05.NcMethodStatusInvalidRequest, "rolePaths supports GET only")
		}
		s.mu.RLock()
		defer s.mu.RUnlock()
		return 200, s.rolePathsList(), nil
	}

	s.mu.RLock()
	obj, ok := s.objects[segs[0]]
	s.mu.RUnlock()
	if !ok {
		return ms05Err(404, ms05.NcMethodStatusBadOid, fmt.Sprintf("role path %q does not exist", segs[0]))
	}

	rest := segs[1:]
	switch {
	case len(rest) == 0:
		if method != stdhttp.MethodGet {
			return ms05Err(405, ms05.NcMethodStatusInvalidRequest, "role path index supports GET only")
		}
		return 200, []string{"bulkProperties/", "descriptor/", "methods/", "properties/"}, nil

	case rest[0] == "descriptor" && len(rest) == 1:
		if method != stdhttp.MethodGet {
			return ms05Err(405, ms05.NcMethodStatusInvalidRequest, "descriptor supports GET only")
		}
		return 200, ms05.NcMethodResultClassDescriptor{Status: ms05.NcMethodStatusOk, Value: obj.class}, nil

	case rest[0] == "properties":
		return s.dispatchProperties(method, obj, rest[1:], r)

	case rest[0] == "methods":
		return s.dispatchMethods(method, obj, rest[1:], r)

	case rest[0] == "bulkProperties" && len(rest) == 1:
		return s.dispatchBulk(method, obj, r)
	}
	return ms05Err(404, ms05.NcMethodStatusBadOid, "no such resource under role path "+segs[0])
}

// ---- properties ----

func (s *IS14ConfigurationServer) dispatchProperties(method string, obj *configObject, rest []string, r *stdhttp.Request) (int, any, error) {
	if len(rest) == 0 {
		if method != stdhttp.MethodGet {
			return ms05Err(405, ms05.NcMethodStatusInvalidRequest, "properties supports GET only")
		}
		ids := make([]string, 0, len(obj.props))
		for _, p := range obj.props {
			ids = append(ids, propKey(p.desc.ID)+"/")
		}
		sort.Strings(ids)
		return 200, ids, nil
	}
	p := obj.findProp(rest[0])
	if p == nil {
		return ms05Err(404, ms05.NcMethodStatusPropertyNotImplemented,
			fmt.Sprintf("property %q does not exist on %s", rest[0], strings.Join(obj.path, ".")))
	}
	switch {
	case len(rest) == 1:
		if method != stdhttp.MethodGet {
			return ms05Err(405, ms05.NcMethodStatusInvalidRequest, "property index supports GET only")
		}
		return 200, []string{"descriptor/", "value/"}, nil

	case rest[1] == "descriptor" && len(rest) == 2:
		if method != stdhttp.MethodGet {
			return ms05Err(405, ms05.NcMethodStatusInvalidRequest, "property descriptor supports GET only")
		}
		dt, ok := flattenedDatatype(p.desc.TypeName)
		if !ok {
			return ms05Err(500, ms05.NcMethodStatusDeviceError,
				fmt.Sprintf("no datatype descriptor for %v", p.desc.TypeName))
		}
		return 200, ms05.NcMethodResultDatatypeDescriptor{Status: ms05.NcMethodStatusOk, Value: dt}, nil

	case rest[1] == "value" && len(rest) == 2:
		switch method {
		case stdhttp.MethodGet:
			s.mu.RLock()
			raw, err := json.Marshal(p.value)
			s.mu.RUnlock()
			if err != nil {
				return ms05Err(500, ms05.NcMethodStatusDeviceError, err.Error())
			}
			return 200, ms05.NcMethodResultPropertyValue{Status: ms05.NcMethodStatusOk, Value: raw}, nil
		case stdhttp.MethodPut:
			body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err != nil {
				return ms05Err(400, ms05.NcMethodStatusBadCommandFormat, err.Error())
			}
			req, err := is14.DecodePropertyValuePutRequest(body)
			if err != nil {
				return ms05Err(400, ms05.NcMethodStatusBadCommandFormat, err.Error())
			}
			if st, err := s.setProperty(obj, p, req.Value); err != nil {
				return ms05Err(400, st, err.Error())
			}
			return 200, ms05.NcMethodResult{Status: ms05.NcMethodStatusOk}, nil
		}
		return ms05Err(405, ms05.NcMethodStatusInvalidRequest, "value supports GET and PUT")
	}
	return ms05Err(404, ms05.NcMethodStatusPropertyNotImplemented, "no such resource under property "+rest[0])
}

// setProperty validates + applies one write. Readonly properties
// answer NcMethodStatus 405; a null on a non-nullable property 417.
func (s *IS14ConfigurationServer) setProperty(obj *configObject, p *configProperty, raw json.RawMessage) (ms05.NcMethodStatus, error) {
	if p.desc.IsReadOnly {
		return ms05.NcMethodStatusReadonly,
			fmt.Errorf("property %s (%s) is readonly", propKey(p.desc.ID), p.desc.Name)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return ms05.NcMethodStatusBadCommandFormat, err
	}
	if v == nil && !p.desc.IsNullable {
		return ms05.NcMethodStatusParameterError,
			fmt.Errorf("property %s (%s) is not nullable", propKey(p.desc.ID), p.desc.Name)
	}
	s.mu.Lock()
	p.value = v
	// A userLabel write must show up in the parent block's members
	// list too — both are views of the same object.
	if p.desc.Name == "userLabel" && len(obj.path) > 1 {
		if parent, ok := s.objects[strings.Join(obj.path[:len(obj.path)-1], ".")]; ok {
			if mp := parent.findProp("2p2"); mp != nil {
				if members, ok := mp.value.([]ms05.NcBlockMemberDescriptor); ok {
					for i := range members {
						if members[i].Oid == obj.oid {
							members[i] = obj.memberDescriptor()
						}
					}
				}
			}
		}
	}
	s.mu.Unlock()
	if s.onModelChanged != nil {
		s.onModelChanged()
	}
	return ms05.NcMethodStatusOk, nil
}

// flattenedDatatype resolves a typeName to its framework descriptor
// with inherited struct fields merged in (parent fields first, the
// order the published instances themselves use).
func flattenedDatatype(name *string) (ms05.NcDatatypeDescriptor, bool) {
	if name == nil {
		generic := "any"
		return ms05.NcDatatypeDescriptor{
			NcDescriptor: ms05.NcDescriptor{Description: &generic},
			Name:         "NcAny",
			Type:         ms05.NcDatatypeTypePrimitive,
		}, true
	}
	d, ok := ms05.StandardDatatype(*name)
	if !ok {
		return ms05.NcDatatypeDescriptor{}, false
	}
	if d.Type != ms05.NcDatatypeTypeStruct || d.ParentType == nil {
		return d, true
	}
	out := d
	out.Fields = nil
	seen := []ms05.NcFieldDescriptor{}
	if parent, ok := ms05.StandardDatatype(*d.ParentType); ok {
		seen = append(seen, parent.Fields...)
	}
	out.Fields = append(seen, d.Fields...)
	return out, true
}

// ---- methods ----

func (s *IS14ConfigurationServer) dispatchMethods(method string, obj *configObject, rest []string, r *stdhttp.Request) (int, any, error) {
	if len(rest) == 0 {
		if method != stdhttp.MethodGet {
			return ms05Err(405, ms05.NcMethodStatusInvalidRequest, "methods supports GET only")
		}
		ids := make([]string, 0, len(obj.class.Methods))
		for _, m := range obj.class.Methods {
			ids = append(ids, methodKey(m.ID)+"/")
		}
		sort.Strings(ids)
		return 200, ids, nil
	}
	if len(rest) != 1 {
		return ms05Err(404, ms05.NcMethodStatusMethodNotImplemented, "no such resource under methods/")
	}
	var md *ms05.NcMethodDescriptor
	for i := range obj.class.Methods {
		if methodKey(obj.class.Methods[i].ID) == rest[0] {
			md = &obj.class.Methods[i]
			break
		}
	}
	if md == nil {
		return ms05Err(404, ms05.NcMethodStatusMethodNotImplemented,
			fmt.Sprintf("method %q does not exist on %s", rest[0], strings.Join(obj.path, ".")))
	}
	if method != stdhttp.MethodPatch {
		return ms05Err(405, ms05.NcMethodStatusInvalidRequest, "methods are invoked with PATCH")
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return ms05Err(400, ms05.NcMethodStatusBadCommandFormat, err.Error())
	}
	req, err := is14.DecodeMethodPatchRequest(body)
	if err != nil {
		return ms05Err(400, ms05.NcMethodStatusBadCommandFormat, err.Error())
	}
	return s.invoke(obj, md, req.Arguments)
}

// methodArgs is the union of argument shapes the standard methods
// take; unknown members are rejected by the strict decode.
type methodArgs struct {
	ID             *ms05.NcElementId `json:"id,omitempty"`
	Value          json.RawMessage   `json:"value,omitempty"`
	Index          *ms05.NcId        `json:"index,omitempty"`
	Recurse        *bool             `json:"recurse,omitempty"`
	Path           []string          `json:"path,omitempty"`
	Role           *string           `json:"role,omitempty"`
	CaseSensitive  *bool             `json:"caseSensitive,omitempty"`
	MatchWholeStr  *bool             `json:"matchWholeString,omitempty"`
	ClassID        ms05.NcClassId    `json:"classId,omitempty"`
	IncludeDerived *bool             `json:"includeDerived,omitempty"`
	Name           *string           `json:"name,omitempty"`
	IncludeInherit *bool             `json:"includeInherited,omitempty"`
}

// invoke dispatches one method by NAME (the framework fixes names per
// id; matching on the name keeps the table readable).
func (s *IS14ConfigurationServer) invoke(obj *configObject, md *ms05.NcMethodDescriptor, rawArgs json.RawMessage) (int, any, error) {
	var args methodArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return ms05Err(400, ms05.NcMethodStatusParameterError, err.Error())
	}

	needProp := func() (*configProperty, *ms05.NcMethodResultError) {
		if args.ID == nil {
			return nil, &ms05.NcMethodResultError{Status: ms05.NcMethodStatusParameterError, ErrorMessage: "id argument required"}
		}
		p := obj.findProp(propKey(*args.ID))
		if p == nil {
			return nil, &ms05.NcMethodResultError{Status: ms05.NcMethodStatusPropertyNotImplemented,
				ErrorMessage: fmt.Sprintf("no property %s on %s", propKey(*args.ID), strings.Join(obj.path, "."))}
		}
		return p, nil
	}

	switch md.Name {
	case "Get":
		p, e := needProp()
		if e != nil {
			return 400, *e, nil
		}
		s.mu.RLock()
		raw, err := json.Marshal(p.value)
		s.mu.RUnlock()
		if err != nil {
			return ms05Err(500, ms05.NcMethodStatusDeviceError, err.Error())
		}
		return 200, ms05.NcMethodResultPropertyValue{Status: ms05.NcMethodStatusOk, Value: raw}, nil

	case "Set":
		p, e := needProp()
		if e != nil {
			return 400, *e, nil
		}
		if args.Value == nil {
			args.Value = json.RawMessage("null")
		}
		if st, err := s.setProperty(obj, p, args.Value); err != nil {
			return ms05Err(400, st, err.Error())
		}
		return 200, ms05.NcMethodResult{Status: ms05.NcMethodStatusOk}, nil

	case "GetSequenceItem", "GetSequenceLength", "SetSequenceItem", "AddSequenceItem", "RemoveSequenceItem":
		return s.invokeSequence(obj, md.Name, args)

	case "GetMemberDescriptors":
		return 200, ms05.NcMethodResultBlockMemberDescriptors{
			Status: ms05.NcMethodStatusOk, Value: s.membersOf(obj, args.Recurse != nil && *args.Recurse),
		}, nil

	case "FindMembersByPath":
		if len(args.Path) == 0 {
			return ms05Err(400, ms05.NcMethodStatusParameterError, "path argument required")
		}
		s.mu.RLock()
		target, ok := s.objects[strings.Join(append(append([]string{}, obj.path...), args.Path...), ".")]
		s.mu.RUnlock()
		if !ok {
			return ms05Err(400, ms05.NcMethodStatusParameterError, "no member at path "+strings.Join(args.Path, "."))
		}
		return 200, ms05.NcMethodResultBlockMemberDescriptors{
			Status: ms05.NcMethodStatusOk, Value: []ms05.NcBlockMemberDescriptor{target.memberDescriptor()},
		}, nil

	case "FindMembersByRole":
		if args.Role == nil {
			return ms05Err(400, ms05.NcMethodStatusParameterError, "role argument required")
		}
		out := []ms05.NcBlockMemberDescriptor{}
		for _, m := range s.membersOf(obj, args.Recurse == nil || *args.Recurse) {
			hay, needle := m.Role, *args.Role
			if args.CaseSensitive != nil && !*args.CaseSensitive {
				hay, needle = strings.ToLower(hay), strings.ToLower(needle)
			}
			whole := args.MatchWholeStr == nil || *args.MatchWholeStr
			if (whole && hay == needle) || (!whole && strings.Contains(hay, needle)) {
				out = append(out, m)
			}
		}
		return 200, ms05.NcMethodResultBlockMemberDescriptors{Status: ms05.NcMethodStatusOk, Value: out}, nil

	case "FindMembersByClassId":
		if len(args.ClassID) == 0 {
			return ms05Err(400, ms05.NcMethodStatusParameterError, "classId argument required")
		}
		out := []ms05.NcBlockMemberDescriptor{}
		for _, m := range s.membersOf(obj, args.Recurse == nil || *args.Recurse) {
			if classIDMatches(m.ClassID, args.ClassID, args.IncludeDerived != nil && *args.IncludeDerived) {
				out = append(out, m)
			}
		}
		return 200, ms05.NcMethodResultBlockMemberDescriptors{Status: ms05.NcMethodStatusOk, Value: out}, nil

	case "GetControlClass":
		if len(args.ClassID) == 0 {
			return ms05Err(400, ms05.NcMethodStatusParameterError, "classId argument required")
		}
		var (
			cd ms05.NcClassDescriptor
			ok bool
		)
		if args.IncludeInherit == nil || *args.IncludeInherit {
			cd, ok = ms05.FlattenedClass(args.ClassID)
		} else {
			cd, ok = ms05.StandardClass(args.ClassID)
		}
		if !ok {
			return ms05Err(400, ms05.NcMethodStatusParameterError, fmt.Sprintf("no class %v", args.ClassID))
		}
		return 200, ms05.NcMethodResultClassDescriptor{Status: ms05.NcMethodStatusOk, Value: cd}, nil

	case "GetDatatype":
		if args.Name == nil {
			return ms05Err(400, ms05.NcMethodStatusParameterError, "name argument required")
		}
		var (
			dt ms05.NcDatatypeDescriptor
			ok bool
		)
		if args.IncludeInherit == nil || *args.IncludeInherit {
			dt, ok = flattenedDatatype(args.Name)
		} else {
			dt, ok = ms05.StandardDatatype(*args.Name)
		}
		if !ok {
			return ms05Err(400, ms05.NcMethodStatusParameterError, "no datatype "+*args.Name)
		}
		return 200, ms05.NcMethodResultDatatypeDescriptor{Status: ms05.NcMethodStatusOk, Value: dt}, nil
	}
	return ms05Err(400, ms05.NcMethodStatusMethodNotImplemented, "method "+md.Name+" not implemented")
}

// invokeSequence handles the five NcObject sequence methods. The
// model's only sequence-typed properties are readonly, so the mutating
// three answer 405 honestly.
func (s *IS14ConfigurationServer) invokeSequence(obj *configObject, name string, args methodArgs) (int, any, error) {
	if args.ID == nil {
		return ms05Err(400, ms05.NcMethodStatusParameterError, "id argument required")
	}
	p := obj.findProp(propKey(*args.ID))
	if p == nil {
		return ms05Err(400, ms05.NcMethodStatusPropertyNotImplemented, "no property "+propKey(*args.ID))
	}
	if !p.desc.IsSequence {
		return ms05Err(400, ms05.NcMethodStatusParameterError, p.desc.Name+" is not a sequence")
	}
	s.mu.RLock()
	raw, err := json.Marshal(p.value)
	s.mu.RUnlock()
	if err != nil {
		return ms05Err(500, ms05.NcMethodStatusDeviceError, err.Error())
	}
	var items []json.RawMessage
	if string(raw) != "null" {
		if err := json.Unmarshal(raw, &items); err != nil {
			return ms05Err(500, ms05.NcMethodStatusDeviceError, err.Error())
		}
	}
	switch name {
	case "GetSequenceLength":
		return 200, ms05.NcMethodResultLength{Status: ms05.NcMethodStatusOk, Value: uint32(len(items))}, nil
	case "GetSequenceItem":
		if args.Index == nil {
			return ms05Err(400, ms05.NcMethodStatusParameterError, "index argument required")
		}
		if int(*args.Index) >= len(items) {
			return ms05Err(400, ms05.NcMethodStatusIndexOutOfBounds,
				fmt.Sprintf("index %d out of bounds (length %d)", *args.Index, len(items)))
		}
		return 200, ms05.NcMethodResultPropertyValue{Status: ms05.NcMethodStatusOk, Value: items[*args.Index]}, nil
	}
	// SetSequenceItem / AddSequenceItem / RemoveSequenceItem: every
	// sequence property in this model is readonly.
	return ms05Err(400, ms05.NcMethodStatusReadonly, p.desc.Name+" is readonly")
}

// classIDMatches reports whether have equals want, or (derived) has
// want as a prefix.
func classIDMatches(have, want ms05.NcClassId, includeDerived bool) bool {
	if len(have) < len(want) {
		return false
	}
	if !includeDerived && len(have) != len(want) {
		return false
	}
	for i := range want {
		if have[i] != want[i] {
			return false
		}
	}
	return true
}

// membersOf lists the member descriptors of a block (empty for
// non-blocks). recurse walks nested blocks — flat model today, but
// the walk is real so a deeper model needs no change here.
func (s *IS14ConfigurationServer) membersOf(obj *configObject, recurse bool) []ms05.NcBlockMemberDescriptor {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []ms05.NcBlockMemberDescriptor{}
	prefix := strings.Join(obj.path, ".") + "."
	for _, key := range s.order {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if !recurse && strings.Contains(key[len(prefix):], ".") {
			continue
		}
		out = append(out, s.objects[key].memberDescriptor())
	}
	return out
}

// ---- bulkProperties (backup / restore / validate) ----

func (s *IS14ConfigurationServer) dispatchBulk(method string, obj *configObject, r *stdhttp.Request) (int, any, error) {
	switch method {
	case stdhttp.MethodGet:
		q := r.URL.Query()
		recurse := q.Get("recurse") != "false"
		includeDesc := q.Get("includeDescriptors") != "false"
		return 200, is14.ResultBulkPropertiesHolder{
			Status: ms05.NcMethodStatusOk,
			Value:  s.backup(obj, recurse, includeDesc),
		}, nil
	case stdhttp.MethodPut, stdhttp.MethodPatch:
		body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
		if err != nil {
			return ms05Err(400, ms05.NcMethodStatusBadCommandFormat, err.Error())
		}
		req, err := is14.DecodeBulkPropertiesSetRequest(body)
		if err != nil {
			return ms05Err(400, ms05.NcMethodStatusBadCommandFormat, err.Error())
		}
		apply := method == stdhttp.MethodPut
		return 200, is14.ResultObjectPropertiesSetValidation{
			Status: ms05.NcMethodStatusOk,
			Value:  s.restore(obj, req.Arguments, apply),
		}, nil
	}
	return ms05Err(405, ms05.NcMethodStatusInvalidRequest, "bulkProperties supports GET, PUT and PATCH")
}

// scopePaths lists the dotted role paths a target + recurse flag
// covers, in listing order.
func (s *IS14ConfigurationServer) scopePaths(obj *configObject, recurse bool) []string {
	target := strings.Join(obj.path, ".")
	out := []string{target}
	if !recurse {
		return out
	}
	prefix := target + "."
	for _, key := range s.order {
		if strings.HasPrefix(key, prefix) {
			out = append(out, key)
		}
	}
	return out
}

// backup builds the NcBulkPropertiesHolder for one scope. The
// includeDescriptors=false form nulls every descriptor AND omits the
// ClassManager role path entirely (API requests doc) — unless the
// ClassManager itself is the target, which yields its holder with an
// empty values collection.
func (s *IS14ConfigurationServer) backup(obj *configObject, recurse, includeDesc bool) is14.BulkPropertiesHolder {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fp := "dhs|" + is14.SpecID + "|v1.0"
	holder := is14.BulkPropertiesHolder{
		ValidationFingerprint: &fp,
		Values:                []is14.ObjectPropertiesHolder{},
	}
	targetIsCM := obj.role == "ClassManager"
	for _, key := range s.scopePaths(obj, recurse) {
		o := s.objects[key]
		isCM := o.role == "ClassManager"
		if isCM && !includeDesc && !targetIsCM {
			continue
		}
		oph := is14.ObjectPropertiesHolder{
			Path:                  o.path,
			DependencyPaths:       [][]string{},
			AllowedMembersClasses: []ms05.NcClassId{},
			Values:                []is14.PropertyHolder{},
			IsRebuildable:         false,
		}
		if !isCM || includeDesc {
			for _, p := range o.props {
				ph := is14.PropertyHolder{ID: p.desc.ID, Value: p.value}
				if includeDesc {
					d := p.desc
					ph.Descriptor = &d
				}
				oph.Values = append(oph.Values, ph)
			}
		}
		holder.Values = append(holder.Values, oph)
	}
	return holder
}

// restore applies (or, apply=false, only validates) a backup data set
// against the scope. Per Backup & restore.md: every in-scope object
// offered in the data set gets a validation entry; readonly members
// produce Warning (300) notices and are left untouched; unknown paths
// report NotFound; and a Rebuild request on this non-rebuildable
// model behaves as a Modify with notices (the doc's interoperability
// floor).
func (s *IS14ConfigurationServer) restore(obj *configObject, args *is14.BulkPropertiesSetArgs, apply bool) []is14.ObjectPropertiesSetValidation {
	scope := map[string]bool{}
	for _, key := range s.scopePaths(obj, *args.Recurse) {
		scope[key] = true
	}

	out := []is14.ObjectPropertiesSetValidation{}
	changed := false
	for _, oph := range args.DataSet.Values {
		key := strings.Join(oph.Path, ".")
		entry := is14.ObjectPropertiesSetValidation{
			Path:    oph.Path,
			Status:  ms05.NcMethodStatusOk,
			Notices: []is14.PropertyRestoreNotice{},
		}
		s.mu.RLock()
		target, exists := s.objects[key]
		s.mu.RUnlock()
		if !exists || !scope[key] {
			entry.Status = ms05.NcMethodStatusBadOid
			msg := "role path not found in restore scope"
			entry.StatusMessage = &msg
			out = append(out, entry)
			continue
		}
		for _, ph := range oph.Values {
			p := target.findProp(propKey(ph.ID))
			if p == nil {
				entry.Notices = append(entry.Notices, is14.PropertyRestoreNotice{
					ID: ph.ID, Name: "unknown", NoticeType: is14.NoticeWarning,
					NoticeMessage: "Property does not exist and will be ignored",
				})
				continue
			}
			if p.desc.IsReadOnly {
				entry.Notices = append(entry.Notices, is14.PropertyRestoreNotice{
					ID: ph.ID, Name: p.desc.Name, NoticeType: is14.NoticeWarning,
					NoticeMessage: "Property is readonly",
				})
				continue
			}
			if apply {
				raw, err := json.Marshal(ph.Value)
				if err == nil {
					if _, err := s.setProperty(target, p, raw); err == nil {
						changed = true
					}
				}
			}
		}
		if len(entry.Notices) > 0 {
			msg := "Some properties have notices"
			entry.StatusMessage = &msg
		}
		out = append(out, entry)
	}
	if changed && s.onModelChanged != nil {
		s.onModelChanged()
	}
	return out
}
