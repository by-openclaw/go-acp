package consumer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"dhs/internal/amwa/codec/is14"
	"dhs/internal/amwa/codec/spec"
	"dhs/internal/amwa/session/configuration"
)

// ConfigRequest asks the Controller to read or change the model of one
// Device over IS-14. Exactly one operation is carried out, chosen by
// the fields set: Restore, Backup, Invoke, Set, Get, or — with only a
// RolePath — describing that object; with none, listing the role paths.
type ConfigRequest struct {
	// DeviceID is the IS-04 Device whose Configuration API is driven.
	DeviceID string

	// RolePath names one object of the Device model ("root",
	// "root.gain", …). Empty lists every role path.
	RolePath string

	// Get is a property id ("3p1") to read.
	Get string

	// Set is a property id to write with SetValue (any JSON the
	// property's datatype takes).
	Set      string
	SetValue json.RawMessage

	// Invoke is a method id ("3m1") to call with Arguments.
	Invoke    string
	Arguments json.RawMessage

	// Backup reads the bulk properties of RolePath (and, with Recurse,
	// of everything under it).
	Backup  bool
	Recurse bool

	// Restore is a data set to put back. The Device is asked to
	// validate it first; it is applied only if every object validates,
	// and not at all with ValidateOnly.
	Restore      *is14.BulkPropertiesHolder
	RestoreMode  is14.RestoreMode
	ValidateOnly bool

	// DryRun reads and validates, and changes nothing.
	DryRun bool
}

// ConfigResult reports what the Device answered.
type ConfigResult struct {
	Endpoint string // the configuration href resolved from IS-04
	RolePath string

	RolePaths   []string // listing
	PropertyIDs []string // describing an object
	MethodIDs   []string
	Descriptor  json.RawMessage

	// Result is the Device's method result for a get, set or invoke;
	// after a set, Value is the property read back.
	Result configuration.Result
	Value  json.RawMessage

	Holder      *is14.BulkPropertiesHolder           // a backup
	Validations []is14.ObjectPropertiesSetValidation // a restore, or its validation
	Restored    bool

	DryRun bool
}

// Configure is the IS-14 half of the Controller role.
//
// The endpoint comes from the Device's IS-04 `controls`
// (urn:x-nmos:control:configuration). A set is read back from the
// Device, and a Device that answered OK and holds another value is
// reported. A restore is validated by the Device first and applied only
// when every object validates: a half-applied data set is the outcome
// IS-14's validation call exists to prevent.
func (c *Controller) Configure(ctx context.Context, req ConfigRequest) (*ConfigResult, error) {
	if err := checkConfigRequest(req); err != nil {
		return nil, err
	}
	snap, _ := c.Walk(ctx)
	href := ""
	found := false
	for _, d := range snap.Devices {
		if d.ID == req.DeviceID {
			found = true
			href = pickControl(d.Controls, is14.ControlType)
		}
	}
	switch {
	case !found:
		return nil, fmt.Errorf("nmos config: no device %s in this catalogue "+
			"(walk it first to see what is there)", req.DeviceID)
	case href == "":
		return nil, fmt.Errorf("nmos config: device %s advertises no "+
			"urn:x-nmos:control:configuration control, so it has no IS-14 endpoint to drive", req.DeviceID)
	}
	cl, err := configuration.NewClient(href)
	if err != nil {
		return nil, err
	}
	res := &ConfigResult{Endpoint: cl.Base, RolePath: req.RolePath, DryRun: req.DryRun}

	switch {
	case req.RolePath == "":
		res.RolePaths, err = cl.RolePaths(ctx)
		return res, err
	case req.Restore != nil:
		return res, c.restoreConfig(ctx, cl, req, res)
	case req.Backup:
		holder, result, err := cl.Backup(ctx, req.RolePath, req.Recurse)
		if err != nil {
			return nil, err
		}
		if res.Result = result; !result.OK() {
			return nil, refusedResult("backup of "+req.RolePath, result)
		}
		res.Holder = &holder
		return res, nil
	case req.Invoke != "":
		if req.DryRun {
			return res, nil
		}
		if res.Result, err = cl.Invoke(ctx, req.RolePath, req.Invoke, req.Arguments); err != nil {
			return nil, err
		}
		if !res.Result.OK() {
			return nil, refusedResult("method "+req.Invoke+" on "+req.RolePath, res.Result)
		}
		res.Value = res.Result.Value
		return res, nil
	case req.Set != "":
		return res, c.setConfig(ctx, cl, req, res)
	case req.Get != "":
		if res.Result, err = cl.GetProperty(ctx, req.RolePath, req.Get); err != nil {
			return nil, err
		}
		if !res.Result.OK() {
			return nil, refusedResult("property "+req.Get+" of "+req.RolePath, res.Result)
		}
		res.Value = res.Result.Value
		return res, nil
	}

	// Describing one object: its class, its properties, its methods.
	desc, err := cl.Descriptor(ctx, req.RolePath)
	if err != nil {
		return nil, err
	}
	if !desc.OK() {
		return nil, refusedResult("descriptor of "+req.RolePath, desc)
	}
	res.Descriptor = desc.Value
	if res.PropertyIDs, err = cl.PropertyIDs(ctx, req.RolePath); err != nil {
		return nil, err
	}
	res.MethodIDs, err = cl.MethodIDs(ctx, req.RolePath)
	return res, err
}

func checkConfigRequest(req ConfigRequest) error {
	if req.DeviceID == "" {
		return fmt.Errorf("nmos config: a device id is required " +
			"(run `dhs consumer nmos walk -l` to list them)")
	}
	ops := 0
	for _, set := range []bool{req.Get != "", req.Set != "", req.Invoke != "", req.Backup, req.Restore != nil} {
		if set {
			ops++
		}
	}
	switch {
	case ops > 1:
		return fmt.Errorf("nmos config: get, set, invoke, backup and restore are one request each")
	case ops == 1 && req.RolePath == "":
		return fmt.Errorf("nmos config: name the object with a role path (list them by giving none)")
	case req.Set != "" && len(req.SetValue) == 0:
		return fmt.Errorf("nmos config: setting %s needs a value", req.Set)
	case req.Set != "" && !json.Valid(req.SetValue):
		return fmt.Errorf("nmos config: the value for %s is not JSON", req.Set)
	case len(req.Arguments) > 0 && !json.Valid(req.Arguments):
		return fmt.Errorf("nmos config: the arguments for %s are not JSON", req.Invoke)
	}
	if req.Restore != nil {
		return is14.ValidateRestoreMode(req.RestoreMode)
	}
	return nil
}

// refusedResult words a method result the Device answered no with.
func refusedResult(what string, r configuration.Result) error {
	if r.ErrorMessage != "" {
		return fmt.Errorf("nmos config: %s: the Device answered %d: %s", what, r.Status, r.ErrorMessage)
	}
	return fmt.Errorf("nmos config: %s: the Device answered %d", what, r.Status)
}

// setConfig writes one property and reads it back.
func (c *Controller) setConfig(ctx context.Context, cl *configuration.Client, req ConfigRequest, res *ConfigResult) error {
	if req.DryRun {
		cur, err := cl.GetProperty(ctx, req.RolePath, req.Set)
		if err != nil {
			return err
		}
		res.Result, res.Value = cur, cur.Value
		return nil
	}
	var err error
	if res.Result, err = cl.SetProperty(ctx, req.RolePath, req.Set, req.SetValue); err != nil {
		return err
	}
	if !res.Result.OK() {
		return refusedResult("setting "+req.Set+" on "+req.RolePath, res.Result)
	}
	back, err := cl.GetProperty(ctx, req.RolePath, req.Set)
	if err != nil {
		return err
	}
	res.Value = back.Value
	if !sameJSON(back.Value, req.SetValue) {
		c.fire(spec.SeverityError, "nmos_is14_set_not_applied",
			fmt.Sprintf("device %s answered OK to %s.%s = %s and holds %s",
				req.DeviceID, req.RolePath, req.Set, req.SetValue, back.Value), req.DeviceID)
	}
	return nil
}

// sameJSON compares two JSON values by content, not by spelling:
// -6 and -6.0 are one number, key order is not a difference.
func sameJSON(a, b json.RawMessage) bool {
	var va, vb any
	if json.Unmarshal(a, &va) != nil || json.Unmarshal(b, &vb) != nil {
		return false
	}
	ca, _ := json.Marshal(va)
	cb, _ := json.Marshal(vb)
	return bytes.Equal(ca, cb)
}

// restoreConfig validates the data set with the Device and applies it
// only when every object validates.
func (c *Controller) restoreConfig(ctx context.Context, cl *configuration.Client, req ConfigRequest, res *ConfigResult) error {
	validations, result, err := cl.Validate(ctx, req.RolePath, *req.Restore, req.Recurse, req.RestoreMode)
	if err != nil {
		return err
	}
	res.Result, res.Validations = result, validations
	if !result.OK() {
		return refusedResult("validating the restore of "+req.RolePath, result)
	}
	for _, v := range validations {
		if v.Status < 200 || v.Status >= 300 {
			if req.ValidateOnly || req.DryRun {
				return nil // the caller asked what would happen: that is the answer
			}
			return fmt.Errorf("nmos config: the Device would refuse %v (status %d); nothing was restored",
				v.Path, v.Status)
		}
	}
	if req.ValidateOnly || req.DryRun {
		return nil
	}
	if res.Validations, res.Result, err = cl.Restore(ctx, req.RolePath, *req.Restore, req.Recurse, req.RestoreMode); err != nil {
		return err
	}
	if !res.Result.OK() {
		return refusedResult("restoring "+req.RolePath, res.Result)
	}
	res.Restored = true
	return nil
}
