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
	classID ms05.NcClassId
	oid     ms05.NcOid
	role    string
	path    []string // role path as array, ["root", ...]
	class   ms05.NcClassDescriptor
	props   []*configProperty // flattened-descriptor order (own first)

	// rebuildable marks an object a Rebuild restore may reconstruct —
	// its own read-only properties accept the backup's values (IS-14
	// Backup & restore). Set at model build; the holder reports it as
	// isRebuildable.
	rebuildable bool
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

	// onPropertyChanged reports a successful write with its identity —
	// the IS-12 side turns it into PropertyChanged notifications for
	// subscribed Controllers. Separate from onModelChanged because the
	// IS-04 hook needs no detail and the IS-12 hook needs all of it.
	onPropertyChanged func(ms05.NcOid, ms05.NcPropertyId, propertyChange)

	// monitorByResource maps an IS-04 sender/receiver id to its status
	// monitor's role-path key (BCP-008 touchpoint, inverted).
	monitorByResource map[string]string

	// monHealth is the per-monitor runtime state of the BCP-008 health
	// engine (monitor_health.go), keyed like objects.
	monHealth map[string]*monitorHealth
}

// propertyChange is what one successful write reports to the IS-12
// side — NcPropertyChangedEventData minus the property id: the kind
// of change, the item index for the three sequence kinds, and the
// value (the whole property for ValueChanged, the item for
// SequenceItemAdded / Changed, nil for Removed).
type propertyChange struct {
	Type  ms05.NcPropertyChangeType
	Index *int
	Value any
}

// valueChanged is the plain-write change: the property's new value.
func valueChanged(v any) propertyChange {
	return propertyChange{Type: ms05.NcPropertyChangeTypeValueChanged, Value: v}
}

// SetOnPropertyChanged installs the IS-12 notification hook.
func (s *IS14ConfigurationServer) SetOnPropertyChanged(fn func(ms05.NcOid, ms05.NcPropertyId, propertyChange)) {
	s.onPropertyChanged = fn
}

// changed reports one applied write to both hooks — the IS-04 side
// (Device version bump) and the IS-12 side (notification).
func (s *IS14ConfigurationServer) changed(obj *configObject, p *configProperty, c propertyChange) {
	if s.onModelChanged != nil {
		s.onModelChanged()
	}
	if s.onPropertyChanged != nil {
		s.onPropertyChanged(obj.oid, p.desc.ID, c)
	}
}

// objectByOid resolves a model object by its oid — the IS-12 address
// form (IS-14 addresses the same objects by role path).
func (s *IS14ConfigurationServer) objectByOid(oid int) *configObject {
	if oid < 0 {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, o := range s.objects {
		if o.oid == ms05.NcOid(oid) {
			return o
		}
	}
	return nil
}

// marshalJSON is encoding/json.Marshal behind a package variable.
// Every value handed to it here is a type this package built — a
// property value decoded from JSON, a counter list, a member
// descriptor — so a refusal is impossible in production. It is still
// answered rather than served: a controller reading a null where a
// value belongs has been told the property is unset, which is a
// different fact. Production never reassigns it.
var marshalJSON = json.Marshal

// propKey renders a property id in the {level}p{index} URL form.
func propKey(id ms05.NcPropertyId) string { return fmt.Sprintf("%dp%d", id.Level, id.Index) }

// methodKey renders a method id in the {level}m{index} URL form.
func methodKey(id ms05.NcMethodId) string { return fmt.Sprintf("%dm%d", id.Level, id.Index) }

// mustObject builds one model object and refuses to continue if it
// cannot. The framework models are compiled into the binary, so a
// failure here is a build defect and not a runtime condition — and a
// device model missing an object a controller is entitled to walk is
// worse than a process that will not start.
func mustObject(classID ms05.NcClassId, oid ms05.NcOid, path []string, seed map[string]any) *configObject {
	o, err := newConfigObject(classID, oid, path, seed)
	if err != nil {
		panic(fmt.Sprintf("provider/is14: framework model load: %v", err))
	}
	return o
}

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
	// The vendor gain class + datatype must be in the ms05 catalogue
	// before the ClassManager snapshots it or an instance is built.
	registerVendorModels()

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
	dm := mustObject(ms05.NcClassId{1, 3, 1}, 2, []string{"root", "DeviceManager"}, map[string]any{
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
	cm := mustObject(ms05.NcClassId{1, 3, 2}, 3, []string{"root", "ClassManager"}, map[string]any{
		"userLabel": "Class manager",
		// The catalogues publish the RAW spec models — own elements
		// only, inheritance via parentType/classId. The AMWA suite
		// compares these against the published model files verbatim
		// (round 3 failed 33 auto_ms05 checks on flattened entries);
		// flattening belongs ONLY to the /descriptor endpoints and
		// includeInherited method variants.
		"controlClasses": ms05.StandardClasses(),
		"datatypes":      ms05.StandardDatatypes(),
	})
	// IS-14 phrases the bulkProperties endpoints as invocations on the
	// Bulk properties manager object (device-configuration feature
	// set, class 1.3.3) — so the object exists in the model, and its
	// three methods run the SAME backup/restore code as the REST
	// routes.
	bpm := mustObject(ms05.NcClassId{1, 3, 3}, 4, []string{"root", "BulkPropertiesManager"}, map[string]any{
		"userLabel": "Bulk properties manager",
	})
	root := mustObject(ms05.NcClassId{1, 1}, 1, []string{"root"}, map[string]any{
		"userLabel": nodeLabel,
		"enabled":   true,
	})
	// BCP-008-01/-02: one status monitor per stream endpoint, tied to
	// its IS-04 resource through a touchpoint. Statuses boot Inactive —
	// nothing transmits until IS-05 activates — and flip on activation
	// via SetMonitorActive.
	objs := []*configObject{root, dm, cm, bpm}
	s.monitorByResource = map[string]string{}
	nextOid := ms05.NcOid(5)
	addMonitor := func(classID ms05.NcClassId, role, resourceType, resourceID, label string, statusProps []string) {
		seed := map[string]any{
			"userLabel": label,
			// NcObject.enabled is non-nullable — IS-14's bulkProperties
			// round walks every model object and fails a null here
			// (test_10/test_11 caught the omission on monitors).
			"enabled": true,
			"touchpoints": []any{map[string]any{
				"contextNamespace": "x-nmos",
				"resource": map[string]any{
					"resourceType": resourceType,
					"id":           resourceID,
				},
			}},
			"statusReportingDelay":         uint32(3),
			"autoResetCountersAndMessages": true,
			"overallStatus":                0, // Inactive
			"linkStatus":                   1, // AllUp
			"linkStatusTransitionCounter":  uint64(0),
			// A reference node has no external sync source: NotUsed —
			// and a concrete value ALWAYS. The AMWA BCP-008 checker
			// feeds every status into max(); a null crashes it.
			"externalSynchronizationStatus":                  0, // NotUsed
			"externalSynchronizationStatusTransitionCounter": uint64(0),
		}
		for _, p := range statusProps {
			seed[p] = 0
			seed[p+"TransitionCounter"] = uint64(0)
		}
		mon := mustObject(classID, nextOid, []string{"root", role}, seed)
		nextOid++
		objs = append(objs, mon)
		s.monitorByResource[resourceID] = strings.Join(mon.path, ".")
	}
	if bundle != nil {
		for i := range bundle.Receivers {
			r := &bundle.Receivers[i]
			addMonitor(ms05.NcClassId{1, 2, 2, 1}, fmt.Sprintf("ReceiverMonitor-%02d", i),
				"receiver", r.ID, "Receiver monitor "+r.Label,
				[]string{"connectionStatus", "streamStatus"})
		}
		for i := range bundle.Senders {
			snd := &bundle.Senders[i]
			addMonitor(ms05.NcClassId{1, 2, 2, 2}, fmt.Sprintf("SenderMonitor-%02d", i),
				"sender", snd.ID, "Sender monitor "+snd.Label,
				[]string{"transmissionStatus", "essenceStatus"})
		}
	}

	// The DhsGainControl worker carries the model's constraint surface
	// (all three MS-05 levels, declared AND enforced — vendor_gain.go).
	gainSeed := map[string]any{
		"userLabel":                  "Gain control",
		"enabled":                    true,
		"channelLabel":               "Gain",
		"gainDb":                     0.0,
		"legacyTrim":                 0.0,
		"runtimePropertyConstraints": vendorRuntimeConstraints(),
	}
	for name, v := range vendorGainSequences() {
		gainSeed[name] = v
	}
	gain := mustObject(vendorClassID, nextOid, []string{"root", vendorRole}, gainSeed)
	objs = append(objs, gain)
	nextOid++

	// The DhsFaultControl worker: the operator/Ansible seam into the
	// BCP-008 health engine (vendor_fault.go).
	fault := mustObject(faultClassID, nextOid, []string{"root", faultRole}, map[string]any{
		"userLabel": "Fault injection control",
		"enabled":   true,
		"armed":     true,
	})
	fault.rebuildable = true
	objs = append(objs, fault)

	if p := root.findProp("2p2"); p != nil { // NcBlock.members
		members := make([]ms05.NcBlockMemberDescriptor, 0, len(objs)-1)
		for _, o := range objs[1:] {
			members = append(members, o.memberDescriptor())
		}
		p.value = members
	}

	for _, o := range objs {
		key := strings.Join(o.path, ".")
		s.objects[key] = o
		s.order = append(s.order, key)
	}
	return s
}

// SetMonitorActive lives in monitor_health.go — the BCP-008 health
// engine drives every monitor status from IS-05 activation state.

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
			raw, err := marshalJSON(p.value)
			s.mu.RUnlock()
			if err != nil {
				return ms05Err(500, ms05.NcMethodStatusDeviceError, err.Error())
			}
			return 200, ms05.NcMethodResultPropertyValue{Status: successStatus(p), Value: raw}, nil
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
			return 200, ms05.NcMethodResult{Status: successStatus(p)}, nil
		}
		return ms05Err(405, ms05.NcMethodStatusInvalidRequest, "value supports GET and PUT")
	}
	return ms05Err(404, ms05.NcMethodStatusPropertyNotImplemented, "no such resource under property "+rest[0])
}

// setProperty validates + applies one write. Readonly properties
// answer NcMethodStatus 405; a null on a non-nullable property 417.
func (s *IS14ConfigurationServer) setProperty(obj *configObject, p *configProperty, raw json.RawMessage) (ms05.NcMethodStatus, error) {
	return s.writeProperty(obj, p, raw, false)
}

// writeProperty is setProperty with the one door a Rebuild restore
// opens: reconstruct=true lets a rebuildable object's own read-only
// property take the backup's value. Every other check still applies.
func (s *IS14ConfigurationServer) writeProperty(obj *configObject, p *configProperty, raw json.RawMessage, reconstruct bool) (ms05.NcMethodStatus, error) {
	if p.desc.IsReadOnly && !reconstruct {
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
	if msg := typeMismatch(&p.desc, v); msg != "" {
		return ms05.NcMethodStatusParameterError,
			fmt.Errorf("property %s (%s): %s", propKey(p.desc.ID), p.desc.Name, msg)
	}
	if err := constraintViolation(obj, p, v); err != nil {
		return ms05.NcMethodStatusParameterError,
			fmt.Errorf("property %s (%s): %v", propKey(p.desc.ID), p.desc.Name, err)
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
	s.changed(obj, p, valueChanged(v))
	return ms05.NcMethodStatusOk, nil
}

// effectiveConstraint resolves the constraint governing one property
// write per the MS-05-02 hierarchy: a runtime constraint (the
// object's runtimePropertyConstraints entry for this property id)
// overrides the property descriptor's constraint, which overrides the
// datatype's. Nil when no level declares one.
func effectiveConstraint(obj *configObject, p *configProperty) any {
	for _, rp := range obj.props {
		if rp.desc.Name != "runtimePropertyConstraints" {
			continue
		}
		list, ok := rp.value.([]any)
		if !ok {
			break
		}
		for _, c := range list {
			if id, ok := ms05.ConstraintPropertyID(c); ok && id == p.desc.ID {
				return c
			}
		}
	}
	if p.desc.Constraints != nil {
		return p.desc.Constraints
	}
	if p.desc.TypeName != nil {
		if dt, ok := ms05.StandardDatatype(*p.desc.TypeName); ok && dt.Constraints != nil {
			return dt.Constraints
		}
	}
	return nil
}

// successStatus is the status of a method that touched p and
// succeeded: Ok, or PropertyDeprecated (298) when the descriptor flags
// the property — MS-05-02 NcMethodStatus keeps serving a deprecated
// property and says so in the status; a controller that ignores it
// is one firmware generation from a PropertyNotImplemented.
func successStatus(p *configProperty) ms05.NcMethodStatus {
	if p.desc.IsDeprecated {
		return ms05.NcMethodStatusPropertyDeprecated
	}
	return ms05.NcMethodStatusOk
}

// constraintViolation applies the property's effective constraint to
// one decoded value — per item for a sequence, since a constraint on
// a sequence property constrains its items (MS-05-02
// Constraints.html). typeMismatch has already proved a non-null
// sequence value is an array; a value it could not classify is
// handed to the checker as is.
func constraintViolation(obj *configObject, p *configProperty, v any) error {
	c := effectiveConstraint(obj, p)
	if c == nil {
		return nil
	}
	if items, ok := v.([]any); ok && p.desc.IsSequence {
		for i, item := range items {
			if err := ms05.CheckConstraintValue(item, c); err != nil {
				return fmt.Errorf("item %d: %v", i, err)
			}
		}
		return nil
	}
	return ms05.CheckConstraintValue(v, c)
}

// typeMismatch reports (as a non-empty message) a value the
// property's declared datatype cannot hold. Deliberately shallow —
// it classifies by JSON kind against the resolved datatype family and
// never rejects what it cannot classify. The AMWA suite's restore
// round offers a wrong-kind value and expects an error or warning
// (test_26); silently applying it was the defect.
func typeMismatch(desc *ms05.NcPropertyDescriptor, v any) string {
	if v == nil || desc.TypeName == nil {
		return ""
	}
	if desc.IsSequence {
		items, ok := v.([]any)
		if !ok {
			return fmt.Sprintf("value for sequence %s must be an array", *desc.TypeName)
		}
		for i, item := range items {
			if item == nil {
				return fmt.Sprintf("item %d of sequence %s is null", i, *desc.TypeName)
			}
			if msg := valueMismatch(*desc.TypeName, item); msg != "" {
				return fmt.Sprintf("item %d: %s", i, msg)
			}
		}
		return ""
	}
	return valueMismatch(*desc.TypeName, v)
}

// valueMismatch classifies one non-null value against one datatype
// name: the JSON kind first, then what the two datatype shapes that
// mean more than a kind add — an enum's members, a struct's fields.
func valueMismatch(typeName string, v any) string {
	switch jsonKindFor(typeName) {
	case "string":
		if _, ok := v.(string); !ok {
			return fmt.Sprintf("value is not a %s (string expected)", typeName)
		}
	case "bool":
		if _, ok := v.(bool); !ok {
			return fmt.Sprintf("value is not a %s (boolean expected)", typeName)
		}
	case "number":
		if _, ok := v.(float64); !ok {
			return fmt.Sprintf("value is not a %s (number expected)", typeName)
		}
	case "object":
		if _, ok := v.(map[string]any); !ok {
			return fmt.Sprintf("value is not a %s (object expected)", typeName)
		}
	}
	dt, ok := ms05.StandardDatatype(typeName)
	if !ok {
		return ""
	}
	switch dt.Type {
	case ms05.NcDatatypeTypeEnum:
		n := v.(float64) // the kind check above has run
		for _, item := range dt.Items {
			if float64(item.Value) == n {
				return ""
			}
		}
		return fmt.Sprintf("%v is not a member of enum %s", n, typeName)
	case ms05.NcDatatypeTypeStruct:
		return structMismatch(typeName, v.(map[string]any))
	}
	return ""
}

// structMismatch checks one object against a struct datatype's
// flattened fields: every non-nullable field present, every field of
// its own datatype, inside its own constraint (declared on the field,
// else inherited from the field's datatype), and nothing the struct
// does not declare — MS-05-02 structs are closed.
func structMismatch(typeName string, obj map[string]any) string {
	dt, ok := flattenedDatatype(&typeName)
	if !ok {
		return ""
	}
	declared := make(map[string]bool, len(dt.Fields))
	for _, f := range dt.Fields {
		declared[f.Name] = true
		fv, present := obj[f.Name]
		if !present || fv == nil {
			if f.IsNullable {
				continue
			}
			return fmt.Sprintf("field %s of %s is required", f.Name, typeName)
		}
		if f.TypeName == nil {
			continue
		}
		c := f.Constraints
		if c == nil {
			if fdt, ok := ms05.StandardDatatype(*f.TypeName); ok {
				c = fdt.Constraints
			}
		}
		items := []any{fv}
		if f.IsSequence {
			if items, ok = fv.([]any); !ok {
				return fmt.Sprintf("field %s of %s must be an array", f.Name, typeName)
			}
		}
		for i, item := range items {
			if item == nil {
				return fmt.Sprintf("field %s of %s: item %d is null", f.Name, typeName, i)
			}
			if msg := valueMismatch(*f.TypeName, item); msg != "" {
				return fmt.Sprintf("field %s of %s: %s", f.Name, typeName, msg)
			}
			if err := ms05.CheckConstraintValue(item, c); err != nil {
				return fmt.Sprintf("field %s of %s: %v", f.Name, typeName, err)
			}
		}
	}
	for k := range obj {
		if !declared[k] {
			return fmt.Sprintf("%s has no field %s", typeName, k)
		}
	}
	return ""
}

// jsonKindFor maps an MS-05 datatype name to the JSON kind it
// serialises as, resolving typedefs through the framework catalogue.
// Unknown names return "" (no check).
func jsonKindFor(name string) string {
	switch name {
	case "NcString", "NcName", "NcUri", "NcUuid", "NcRegex", "NcVersionCode", "NcOrganizationId":
		return "string"
	case "NcBoolean":
		return "bool"
	case "NcInt16", "NcInt32", "NcInt64", "NcUint16", "NcUint32", "NcUint64",
		"NcFloat32", "NcFloat64", "NcId", "NcOid", "NcTimeInterval":
		return "number"
	}
	dt, ok := ms05.StandardDatatype(name)
	if !ok {
		return ""
	}
	switch dt.Type {
	case ms05.NcDatatypeTypeEnum:
		return "number"
	case ms05.NcDatatypeTypeStruct:
		return "object"
	case ms05.NcDatatypeTypeTypedef:
		if dt.IsSequence || dt.ParentType == nil {
			return ""
		}
		return jsonKindFor(*dt.ParentType)
	}
	return ""
}

// flattenedDatatype resolves a typeName to its framework descriptor
// with ALL inherited struct fields merged in (ms05 walks the whole
// parentType chain — one level was 1 field short on the descriptor
// family, IS-14-01 test_ms05_14).
func flattenedDatatype(name *string) (ms05.NcDatatypeDescriptor, bool) {
	if name == nil {
		generic := "any"
		return ms05.NcDatatypeDescriptor{
			NcDescriptor: ms05.NcDescriptor{Description: &generic},
			Name:         "NcAny",
			Type:         ms05.NcDatatypeTypePrimitive,
		}, true
	}
	return ms05.FlattenedDatatype(*name)
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
	// Bulk properties manager methods (feature set 1.3.3).
	IncludeDescriptors *bool                      `json:"includeDescriptors,omitempty"`
	DataSet            *is14.BulkPropertiesHolder `json:"dataSet,omitempty"`
	RestoreMode        *is14.RestoreMode          `json:"restoreMode,omitempty"`
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
		raw, err := marshalJSON(p.value)
		s.mu.RUnlock()
		if err != nil {
			return ms05Err(500, ms05.NcMethodStatusDeviceError, err.Error())
		}
		return 200, ms05.NcMethodResultPropertyValue{Status: successStatus(p), Value: raw}, nil

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
		return 200, ms05.NcMethodResult{Status: successStatus(p)}, nil

	case "GetSequenceItem", "GetSequenceLength", "SetSequenceItem", "AddSequenceItem", "RemoveSequenceItem":
		return s.invokeSequence(obj, md.Name, args)

	case "SetGainDb":
		// DhsGainControl 4m1 — a named write of gainDb (4p2), through
		// the same setProperty gate (constraints included).
		var gainArgs struct {
			GainDb json.RawMessage `json:"gainDb"`
		}
		if err := json.Unmarshal(rawArgs, &gainArgs); err != nil || gainArgs.GainDb == nil {
			return ms05Err(400, ms05.NcMethodStatusParameterError, "gainDb argument required")
		}
		p := obj.findProp("4p2")
		if p == nil {
			return ms05Err(400, ms05.NcMethodStatusPropertyNotImplemented, "no gainDb property on this object")
		}
		if st, err := s.setProperty(obj, p, gainArgs.GainDb); err != nil {
			return ms05Err(400, st, err.Error())
		}
		return 200, ms05.NcMethodResult{Status: ms05.NcMethodStatusOk}, nil

	case "InjectMonitorFault", "ClearMonitorFault", "SetMonitorSyncSource", "AddMonitorPacketCounters":
		// DhsFaultControl — the BCP-008 fault-injection seam, invocable
		// over IS-14 REST so the Ansible verify plays reach it with
		// plain HTTP.
		if err := s.invokeFaultMethod(md.Name, rawArgs); err != nil {
			return ms05Err(400, faultMethodStatus(err), err.Error())
		}
		return 200, ms05.NcMethodResult{Status: ms05.NcMethodStatusOk}, nil

	case "ResetCountersAndMessages":
		// BCP-008 monitor reset over the IS-14 face — same body as the
		// IS-12 method (transition counters, messages AND injected
		// packet counters clear together).
		s.mu.Lock()
		changes := s.resetCountersLocked(strings.Join(obj.path, "."), obj)
		s.mu.Unlock()
		s.fire(changes)
		return 200, ms05.NcMethodResult{Status: ms05.NcMethodStatusOk}, nil

	case "GetLostPacketCounters", "GetLatePacketCounters", "GetTransmissionErrorCounters":
		kind := map[string]string{
			"GetLostPacketCounters":        "lost",
			"GetLatePacketCounters":        "late",
			"GetTransmissionErrorCounters": "transmission",
		}[md.Name]
		counters := s.MonitorPacketCounters(obj.oid, kind)
		if counters == nil {
			counters = []ncCounter{}
		}
		raw, err := marshalJSON(counters)
		if err != nil {
			return ms05Err(500, ms05.NcMethodStatusDeviceError, err.Error())
		}
		return 200, ms05.NcMethodResultPropertyValue{Status: ms05.NcMethodStatusOk, Value: raw}, nil

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

	case "GetPropertiesByPath", "ValidateSetPropertiesByPath", "SetPropertiesByPath":
		if len(args.Path) == 0 {
			return ms05Err(400, ms05.NcMethodStatusParameterError, "path argument required")
		}
		s.mu.RLock()
		target, ok := s.objects[strings.Join(args.Path, ".")]
		s.mu.RUnlock()
		if !ok {
			return ms05Err(400, ms05.NcMethodStatusBadOid, "no object at path "+strings.Join(args.Path, "."))
		}
		recurse := args.Recurse == nil || *args.Recurse
		if md.Name == "GetPropertiesByPath" {
			includeDesc := args.IncludeDescriptors == nil || *args.IncludeDescriptors
			return 200, is14.ResultBulkPropertiesHolder{
				Status: ms05.NcMethodStatusOk,
				Value:  s.backup(target, recurse, includeDesc),
			}, nil
		}
		if args.DataSet == nil || args.RestoreMode == nil {
			return ms05Err(400, ms05.NcMethodStatusParameterError, "dataSet and restoreMode arguments required")
		}
		if err := is14.ValidateRestoreMode(*args.RestoreMode); err != nil {
			return ms05Err(400, ms05.NcMethodStatusParameterError, err.Error())
		}
		setArgs := &is14.BulkPropertiesSetArgs{DataSet: args.DataSet, Recurse: &recurse, RestoreMode: args.RestoreMode}
		return 200, is14.ResultObjectPropertiesSetValidation{
			Status: ms05.NcMethodStatusOk,
			Value:  s.restore(target, setArgs, md.Name == "SetPropertiesByPath"),
		}, nil

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

// invokeSequence handles the five NcObject sequence methods; the
// mutating three go through the gate configuration_sequence.go shares
// with the IS-12 server.
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
	raw, err := marshalJSON(p.value)
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
		return 200, ms05.NcMethodResultLength{Status: successStatus(p), Value: uint32(len(items))}, nil
	case "GetSequenceItem":
		if args.Index == nil {
			return ms05Err(400, ms05.NcMethodStatusParameterError, "index argument required")
		}
		if int(*args.Index) >= len(items) {
			return ms05Err(400, ms05.NcMethodStatusIndexOutOfBounds,
				fmt.Sprintf("index %d out of bounds (length %d)", *args.Index, len(items)))
		}
		return 200, ms05.NcMethodResultPropertyValue{Status: successStatus(p), Value: items[*args.Index]}, nil
	case "AddSequenceItem":
		if args.Value == nil {
			return ms05Err(400, ms05.NcMethodStatusParameterError, "value argument required")
		}
		index, st, err := s.sequenceAdd(obj, p, args.Value)
		if err != nil {
			return ms05Err(400, st, err.Error())
		}
		return 200, ms05.NcMethodResultId{Status: successStatus(p), Value: ms05.NcId(index)}, nil
	case "SetSequenceItem":
		if args.Index == nil || args.Value == nil {
			return ms05Err(400, ms05.NcMethodStatusParameterError, "index and value arguments required")
		}
		if st, err := s.sequenceSet(obj, p, int(*args.Index), args.Value); err != nil {
			return ms05Err(400, st, err.Error())
		}
		return 200, ms05.NcMethodResult{Status: successStatus(p)}, nil
	case "RemoveSequenceItem":
		if args.Index == nil {
			return ms05Err(400, ms05.NcMethodStatusParameterError, "index argument required")
		}
		if st, err := s.sequenceRemove(obj, p, int(*args.Index)); err != nil {
			return ms05Err(400, st, err.Error())
		}
		return 200, ms05.NcMethodResult{Status: successStatus(p)}, nil
	}
	return ms05Err(400, ms05.NcMethodStatusMethodNotImplemented, "method "+name+" not implemented")
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
			IsRebuildable:         o.rebuildable,
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
// report NotFound. A Rebuild reconstructs the model's rebuildable
// objects: their own read-only properties take the backup's values,
// while NcObject's members (classId, oid, constantOid, owner, role,
// touchpoints, runtimePropertyConstraints) stay — the doc lets a
// structural property change only when the PARENT block is
// rebuildable, and no block here is. On everything else a Rebuild
// behaves as a Modify with notices (the doc's interoperability floor).
func (s *IS14ConfigurationServer) restore(obj *configObject, args *is14.BulkPropertiesSetArgs, apply bool) []is14.ObjectPropertiesSetValidation {
	rebuild := args.RestoreMode != nil && *args.RestoreMode == is14.RestoreModeRebuild
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
		if !exists {
			// NcRestoreValidationStatus NotFound: the path is not in
			// the device model at all.
			entry.Status = is14.RestoreValidationNotFound
			msg := "role path not found in the device model"
			entry.StatusMessage = &msg
			out = append(out, entry)
			continue
		}
		if !scope[key] {
			// In the model but outside the target+recurse scope: the
			// restore scope is the INTERSECTION, and objects outside it
			// get NO validation entry (Backup & restore.md; the suite's
			// recurse=false rounds count exactly the in-scope paths).
			continue
		}
		hasError := false
		for _, ph := range oph.Values {
			p := target.findProp(propKey(ph.ID))
			if p == nil {
				entry.Notices = append(entry.Notices, is14.PropertyRestoreNotice{
					ID: ph.ID, Name: "unknown", NoticeType: is14.NoticeWarning,
					NoticeMessage: "Property does not exist and will be ignored",
				})
				continue
			}
			reconstruct := rebuild && target.rebuildable && p.desc.IsReadOnly
			if p.desc.IsReadOnly && !reconstruct {
				entry.Notices = append(entry.Notices, is14.PropertyRestoreNotice{
					ID: ph.ID, Name: p.desc.Name, NoticeType: is14.NoticeWarning,
					NoticeMessage: "Property is readonly",
				})
				continue
			}
			if reconstruct && p.desc.ID.Level == 1 {
				// NcObject's own members are the object's identity,
				// structure and declared constraints: a Rebuild of the
				// object does not change them (its parent block would
				// have to be rebuildable).
				entry.Notices = append(entry.Notices, is14.PropertyRestoreNotice{
					ID: ph.ID, Name: p.desc.Name, NoticeType: is14.NoticeWarning,
					NoticeMessage: "Structural property: the parent block is not rebuildable",
				})
				continue
			}
			if ph.Value == nil && !p.desc.IsNullable {
				// Null into a non-nullable property is the restore twin
				// of setProperty's nullability rule — typeMismatch
				// passes nil through by design, so without this notice
				// the validation response reads as acceptance (IS-14
				// test_26 on the monitor objects).
				entry.Notices = append(entry.Notices, is14.PropertyRestoreNotice{
					ID: ph.ID, Name: p.desc.Name, NoticeType: is14.NoticeError,
					NoticeMessage: "property is not nullable",
				})
				hasError = true
				continue
			}
			if msg := typeMismatch(&p.desc, ph.Value); msg != "" {
				// A value the property's datatype cannot hold is an
				// ERROR notice, and one error notice fails the object
				// (Backup & restore.md validation rules).
				entry.Notices = append(entry.Notices, is14.PropertyRestoreNotice{
					ID: ph.ID, Name: p.desc.Name, NoticeType: is14.NoticeError,
					NoticeMessage: msg,
				})
				hasError = true
				continue
			}
			if err := constraintViolation(target, p, ph.Value); err != nil {
				// The restore twin of setProperty's constraint check
				// (IS-14 test_27 offers out-of-range values here and
				// expects them noticed, not accepted).
				entry.Notices = append(entry.Notices, is14.PropertyRestoreNotice{
					ID: ph.ID, Name: p.desc.Name, NoticeType: is14.NoticeError,
					NoticeMessage: err.Error(),
				})
				hasError = true
				continue
			}
			if apply {
				raw, err := json.Marshal(ph.Value)
				if err == nil {
					if _, err := s.writeProperty(target, p, raw, reconstruct); err == nil {
						changed = true
					}
				}
			}
		}
		if hasError {
			// The verdict enum is NcRestoreValidationStatus — Failed is
			// 400; a 417 here fails the response schema (round 4).
			entry.Status = is14.RestoreValidationFailed
			msg := "Some properties failed validation"
			entry.StatusMessage = &msg
		} else if len(entry.Notices) > 0 {
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
