package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"dhs/internal/amwa/codec/is12"
	"dhs/internal/amwa/codec/ms05"
	"dhs/internal/amwa/codec/spec"
	"dhs/internal/amwa/session/control"
)

// ControlRequest asks the Controller to read or change the model of one
// Device over IS-12. Exactly one operation is carried out, chosen by
// the fields set: Watch, Invoke, Set, Get, or — with only a RolePath —
// describing that object; with none, listing every object of the model.
type ControlRequest struct {
	// DeviceID is the IS-04 Device whose control protocol is driven.
	DeviceID string

	// RolePath names one object of the Device model ("root",
	// "root.receivers.rx1", …). Empty addresses the whole model.
	RolePath string

	// Get is a property id ("1p6") to read.
	Get string

	// Set is a property id to write with SetValue (any JSON the
	// property's datatype takes).
	Set      string
	SetValue json.RawMessage

	// Invoke is a method id ("3m1") to call with Arguments.
	Invoke    string
	Arguments json.RawMessage

	// Watch subscribes to the property changes of RolePath's object — of
	// every object when RolePath is empty — and hands each to OnChange
	// until the context ends.
	Watch    bool
	OnChange func(ControlChange)

	// DryRun reads, and changes nothing.
	DryRun bool
}

// ControlObject is one object of a Device model.
type ControlObject struct {
	RolePath  string
	OID       int
	ClassID   ms05.NcClassId
	UserLabel string
}

// ControlChange is one property-changed notification.
type ControlChange struct {
	RolePath          string
	OID               int
	Property          string // "1p6"
	ChangeType        int
	Value             json.RawMessage
	SequenceItemIndex *int
}

// ControlResult reports what the Device answered.
type ControlResult struct {
	Endpoint string // the control href resolved from IS-04
	RolePath string
	OID      int

	Objects []ControlObject // listing
	Class   json.RawMessage // describing an object: its class descriptor

	// Result is the Device's method result for a get, set or invoke;
	// after a set, Value is the property read back.
	Result is12.MethodResult
	Value  json.RawMessage

	Changes int // notifications a watch handed over

	DryRun bool
}

// The MS-05-02 properties and class this half reads by name.
var (
	propertyClassID = is12.PropertyID{Level: 1, Index: 1}
	propertyRole    = is12.PropertyID{Level: 1, Index: 5}
	classManager    = ms05.NcClassId{1, 3, 2}
)

// Control is the IS-12 half of the Controller role.
//
// The endpoint comes from the Device's IS-04 `controls`
// (urn:x-nmos:control:ncp). Objects are named by role path — the same
// names IS-14 uses — and resolved to the oid the protocol wants from
// the Device's own block tree. A set is read back from the Device, and
// a Device that answered OK and holds another value is reported.
func (c *Controller) Control(ctx context.Context, req ControlRequest) (*ControlResult, error) {
	if err := checkControlRequest(req); err != nil {
		return nil, err
	}
	snap, _ := c.Walk(ctx)
	href := ""
	found := false
	for _, d := range snap.Devices {
		if d.ID == req.DeviceID {
			found = true
			href = pickControl(d.Controls, control.ControlType)
		}
	}
	switch {
	case !found:
		return nil, fmt.Errorf("nmos control: no device %s in this catalogue "+
			"(walk it first to see what is there)", req.DeviceID)
	case href == "":
		return nil, fmt.Errorf("nmos control: device %s advertises no "+
			"urn:x-nmos:control:ncp control, so it has no IS-12 endpoint to drive", req.DeviceID)
	}
	cl, err := control.Dial(ctx, href)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cl.Close() }()
	res := &ControlResult{Endpoint: href, RolePath: req.RolePath, DryRun: req.DryRun}

	objects, err := c.controlModel(ctx, cl, req.DeviceID)
	if err != nil {
		return nil, err
	}
	if req.RolePath == "" {
		res.Objects = objects
		if req.Watch {
			return res, watchControl(ctx, cl, objects, objects, req.OnChange, res)
		}
		return res, nil
	}
	var obj *ControlObject
	for i := range objects {
		if objects[i].RolePath == req.RolePath {
			obj = &objects[i]
		}
	}
	if obj == nil {
		return nil, fmt.Errorf("nmos control: device %s has no object %s (list them by giving no role path)",
			req.DeviceID, req.RolePath)
	}
	res.OID = obj.OID

	switch {
	case req.Watch:
		return res, watchControl(ctx, cl, objects, []ControlObject{*obj}, req.OnChange, res)
	case req.Invoke != "":
		id, _ := parseElementID(req.Invoke, 'm')
		if req.DryRun {
			return res, nil
		}
		var arguments any
		if len(req.Arguments) > 0 {
			arguments = req.Arguments
		}
		if res.Result, err = cl.Invoke(ctx, obj.OID, id, arguments); err != nil {
			return nil, err
		}
		if !control.OK(res.Result) {
			return nil, refusedControl("method "+req.Invoke+" on "+req.RolePath, res.Result)
		}
		res.Value = res.Result.Value
		return res, nil
	case req.Set != "":
		return res, c.setControl(ctx, cl, req, obj.OID, res)
	case req.Get != "":
		id, _ := parseElementID(req.Get, 'p')
		if res.Result, err = cl.Get(ctx, obj.OID, id); err != nil {
			return nil, err
		}
		if !control.OK(res.Result) {
			return nil, refusedControl("property "+req.Get+" of "+req.RolePath, res.Result)
		}
		res.Value = res.Result.Value
		return res, nil
	}

	// Describing one object: its class, as the Device's class manager
	// gives it.
	manager := 0
	for _, o := range objects {
		if isClass(o.ClassID, classManager) {
			manager = o.OID
		}
	}
	if manager == 0 {
		return nil, fmt.Errorf("nmos control: device %s has no class manager in its model, so %s cannot be described",
			req.DeviceID, req.RolePath)
	}
	if res.Result, err = cl.Class(ctx, manager, obj.ClassID); err != nil {
		return nil, err
	}
	if !control.OK(res.Result) {
		return nil, refusedControl("class of "+req.RolePath, res.Result)
	}
	res.Class = res.Result.Value
	return res, nil
}

func checkControlRequest(req ControlRequest) error {
	if req.DeviceID == "" {
		return fmt.Errorf("nmos control: a device id is required " +
			"(run `dhs consumer nmos walk -l` to list them)")
	}
	ops := 0
	for _, set := range []bool{req.Get != "", req.Set != "", req.Invoke != "", req.Watch} {
		if set {
			ops++
		}
	}
	switch {
	case ops > 1:
		return fmt.Errorf("nmos control: get, set, invoke and watch are one request each")
	case ops == 1 && !req.Watch && req.RolePath == "":
		return fmt.Errorf("nmos control: name the object with a role path (list them by giving none)")
	case req.Watch && req.OnChange == nil:
		return fmt.Errorf("nmos control: a watch needs somewhere to hand the changes")
	case req.Set != "" && len(req.SetValue) == 0:
		return fmt.Errorf("nmos control: setting %s needs a value", req.Set)
	case req.Set != "" && !json.Valid(req.SetValue):
		return fmt.Errorf("nmos control: the value for %s is not JSON", req.Set)
	case len(req.Arguments) > 0 && !json.Valid(req.Arguments):
		return fmt.Errorf("nmos control: the arguments for %s are not JSON", req.Invoke)
	}
	for _, id := range []struct {
		text string
		kind byte
	}{{req.Get, 'p'}, {req.Set, 'p'}, {req.Invoke, 'm'}} {
		if id.text == "" {
			continue
		}
		if _, err := parseElementID(id.text, id.kind); err != nil {
			return err
		}
	}
	return nil
}

// parseElementID reads an MS-05-02 element id in the spelling IS-14
// gives it: "1p6" is property 6 of level 1, "3m1" method 1 of level 3.
func parseElementID(text string, kind byte) (is12.MethodID, error) {
	what := map[byte]string{'p': "property", 'm': "method"}[kind]
	level, index, ok := strings.Cut(text, string(kind))
	l, errL := strconv.Atoi(level)
	i, errI := strconv.Atoi(index)
	if !ok || errL != nil || errI != nil || l < 1 || i < 1 {
		return is12.MethodID{}, fmt.Errorf("nmos control: %q is not a %s id (want <level>%c<index>, e.g. 1%c1)", text, what, kind, kind)
	}
	return is12.MethodID{Level: l, Index: i}, nil
}

// elementID spells an id back the same way.
func elementID(id is12.MethodID, kind byte) string {
	return strconv.Itoa(id.Level) + string(kind) + strconv.Itoa(id.Index)
}

// isClass reports whether id is the class base or one derived from it.
func isClass(id, base ms05.NcClassId) bool {
	if len(id) < len(base) {
		return false
	}
	for i := range base {
		if id[i] != base[i] {
			return false
		}
	}
	return true
}

// refusedControl words a method result the Device answered no with.
func refusedControl(what string, r is12.MethodResult) error {
	if r.ErrorMessage != "" {
		return fmt.Errorf("nmos control: %s: the Device answered %d: %s", what, r.Status, r.ErrorMessage)
	}
	return fmt.Errorf("nmos control: %s: the Device answered %d", what, r.Status)
}

// controlModel reads the Device's block tree and names every object by
// its role path: the roles from the root block down to it.
func (c *Controller) controlModel(ctx context.Context, cl *control.Client, deviceID string) ([]ControlObject, error) {
	root := ControlObject{RolePath: "root", OID: control.RootBlockOID}
	if role, err := cl.Get(ctx, root.OID, propertyRole); err != nil {
		return nil, err
	} else if control.OK(role) {
		_ = json.Unmarshal(role.Value, &root.RolePath)
	}
	if class, err := cl.Get(ctx, root.OID, propertyClassID); err != nil {
		return nil, err
	} else if control.OK(class) {
		_ = json.Unmarshal(class.Value, &root.ClassID)
	}
	members, result, err := cl.Members(ctx, root.OID, true)
	if err != nil {
		return nil, err
	}
	if !control.OK(result) {
		return nil, refusedControl("members of the root block", result)
	}

	byOID := make(map[int]ms05.NcBlockMemberDescriptor, len(members))
	for _, m := range members {
		byOID[int(m.Oid)] = m
	}
	// path climbs the owners to the root block. A member whose chain
	// does not reach it — an owner the Device did not list, or a loop —
	// keeps its own role as its name, and the Device is reported.
	path := func(m ms05.NcBlockMemberDescriptor) (string, bool) {
		parts := []string{m.Role}
		for hops := 0; int(m.Owner) != root.OID; hops++ {
			owner, ok := byOID[int(m.Owner)]
			if !ok || hops > len(members) {
				return m.Role, false
			}
			parts = append([]string{owner.Role}, parts...)
			m = owner
		}
		return root.RolePath + "." + strings.Join(parts, "."), true
	}
	objects := []ControlObject{root}
	for _, m := range members {
		o := ControlObject{OID: int(m.Oid), ClassID: m.ClassID}
		if m.UserLabel != nil {
			o.UserLabel = *m.UserLabel
		}
		var rooted bool
		if o.RolePath, rooted = path(m); !rooted {
			c.fire(spec.SeverityWarn, "nmos_is12_member_outside_the_tree",
				fmt.Sprintf("device %s lists object %d (%s) under owner %d, which does not lead to the root block",
					deviceID, m.Oid, m.Role, m.Owner), deviceID)
		}
		objects = append(objects, o)
	}
	sort.SliceStable(objects, func(i, j int) bool { return objects[i].RolePath < objects[j].RolePath })
	return objects, nil
}

// setControl writes one property and reads it back.
func (c *Controller) setControl(ctx context.Context, cl *control.Client, req ControlRequest, oid int, res *ControlResult) error {
	id, _ := parseElementID(req.Set, 'p')
	if req.DryRun {
		cur, err := cl.Get(ctx, oid, id)
		if err != nil {
			return err
		}
		res.Result, res.Value = cur, cur.Value
		return nil
	}
	var err error
	if res.Result, err = cl.Set(ctx, oid, id, req.SetValue); err != nil {
		return err
	}
	if !control.OK(res.Result) {
		return refusedControl("setting "+req.Set+" on "+req.RolePath, res.Result)
	}
	back, err := cl.Get(ctx, oid, id)
	if err != nil {
		return err
	}
	res.Value = back.Value
	if !sameJSON(back.Value, req.SetValue) {
		c.fire(spec.SeverityError, "nmos_is12_set_not_applied",
			fmt.Sprintf("device %s answered OK to %s.%s = %s and holds %s",
				req.DeviceID, req.RolePath, req.Set, req.SetValue, back.Value), req.DeviceID)
	}
	return nil
}

// watchControl subscribes to the watched objects and hands over their
// property changes until the context ends. all names the objects of
// the whole model: a notification says which object by oid.
func watchControl(ctx context.Context, cl *control.Client, all, watched []ControlObject, onChange func(ControlChange), res *ControlResult) error {
	names := make(map[int]string, len(all))
	for _, o := range all {
		names[o.OID] = o.RolePath
	}
	oids := make([]int, len(watched))
	for i, o := range watched {
		oids[i] = o.OID
	}
	if _, err := cl.Subscribe(ctx, oids); err != nil {
		return err
	}
	return cl.Notifications(ctx, func(n is12.Notification) {
		res.Changes++
		onChange(ControlChange{
			RolePath:          names[n.OID],
			OID:               n.OID,
			Property:          elementID(n.EventData.PropertyID, 'p'),
			ChangeType:        n.EventData.ChangeType,
			Value:             n.EventData.Value,
			SequenceItemIndex: n.EventData.SequenceItemIndex,
		})
	})
}
