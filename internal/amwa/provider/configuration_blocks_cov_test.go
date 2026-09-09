package provider

import (
	"encoding/json"
	"testing"

	"dhs/internal/amwa/codec/is14"
	"dhs/internal/amwa/codec/ms05"
)

// invokeNamed calls one method by name on an object, through the same
// IS-14 REST path a controller uses.
func invokeNamed(t *testing.T, s *IS14ConfigurationServer, obj *configObject, name, args string) (int, any) {
	t.Helper()
	md := methodNamed(t, obj, name)
	status, body, err := s.invoke(obj, md, json.RawMessage(args))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return status, body
}

// descriptorsOf reads a block-member result, failing the test when the
// call did not answer with one.
func descriptorsOf(t *testing.T, body any) []ms05.NcBlockMemberDescriptor {
	t.Helper()
	res, ok := body.(ms05.NcMethodResultBlockMemberDescriptors)
	if !ok {
		t.Fatalf("body = %T, want block member descriptors", body)
	}
	return res.Value
}

// The block-search methods are how a controller finds anything in the
// device model: by path, by role and by class, each with its own
// required argument.
func TestInvokeBlockSearches(t *testing.T) {
	s := configFixture(t)
	root := s.objectByOid(1)

	status, body := invokeNamed(t, s, root, "GetMemberDescriptors", `{}`)
	if status != 200 {
		t.Fatalf("GetMemberDescriptors = %d (%+v)", status, body)
	}
	direct := descriptorsOf(t, body)
	if len(direct) == 0 {
		t.Fatal("the root block has members")
	}
	_, body = invokeNamed(t, s, root, "GetMemberDescriptors", `{"recurse":true}`)
	if len(descriptorsOf(t, body)) < len(direct) {
		t.Error("a recursive walk cannot return fewer members than a direct one")
	}

	// By path: the segments are relative to the block asked.
	role := direct[0].Role
	status, body = invokeNamed(t, s, root, "FindMembersByPath",
		`{"path":["`+role+`"]}`)
	if status != 200 || len(descriptorsOf(t, body)) != 1 {
		t.Errorf("FindMembersByPath = %d (%+v)", status, body)
	}
	if status, _ := invokeNamed(t, s, root, "FindMembersByPath", `{}`); status != 400 {
		t.Error("FindMembersByPath must require a path")
	}
	if status, _ := invokeNamed(t, s, root, "FindMembersByPath", `{"path":["nowhere"]}`); status != 400 {
		t.Error("a path naming no member must be refused")
	}

	// By role: whole-string and case-sensitive by default, both
	// relaxable.
	status, body = invokeNamed(t, s, root, "FindMembersByRole", `{"role":"`+role+`"}`)
	if status != 200 || len(descriptorsOf(t, body)) != 1 {
		t.Errorf("FindMembersByRole = %d (%+v)", status, body)
	}
	_, body = invokeNamed(t, s, root, "FindMembersByRole",
		`{"role":"`+role[:2]+`","matchWholeString":false}`)
	if len(descriptorsOf(t, body)) == 0 {
		t.Error("a substring search must match")
	}
	_, body = invokeNamed(t, s, root, "FindMembersByRole", `{"role":"`+role+`","caseSensitive":false}`)
	if len(descriptorsOf(t, body)) != 1 {
		t.Error("a case-insensitive search must still match the exact role")
	}
	if status, _ := invokeNamed(t, s, root, "FindMembersByRole", `{}`); status != 400 {
		t.Error("FindMembersByRole must require a role")
	}

	// By class: exact by default, derived on request.
	classID := direct[0].ClassID
	raw, err := json.Marshal(map[string]any{"classId": classID})
	if err != nil {
		t.Fatal(err)
	}
	status, body = invokeNamed(t, s, root, "FindMembersByClassId", string(raw))
	if status != 200 || len(descriptorsOf(t, body)) == 0 {
		t.Errorf("FindMembersByClassId = %d (%+v)", status, body)
	}
	raw, err = json.Marshal(map[string]any{"classId": ms05.NcClassId{1}, "includeDerived": true})
	if err != nil {
		t.Fatal(err)
	}
	_, body = invokeNamed(t, s, root, "FindMembersByClassId", string(raw))
	if len(descriptorsOf(t, body)) == 0 {
		t.Error("every object derives from NcObject")
	}
	if status, _ := invokeNamed(t, s, root, "FindMembersByClassId", `{}`); status != 400 {
		t.Error("FindMembersByClassId must require a classId")
	}
}

// The ClassManager publishes the model's vocabulary: a class
// descriptor with or without inherited elements, and a datatype with
// or without inherited fields.
func TestInvokeClassManagerLookups(t *testing.T) {
	s := configFixture(t)
	cm := objectOfClass(t, s, ms05.NcClassId{1, 3, 2})

	flat, err := json.Marshal(map[string]any{"classId": ms05.NcClassId{1, 1}})
	if err != nil {
		t.Fatal(err)
	}
	status, body := invokeNamed(t, s, cm, "GetControlClass", string(flat))
	if status != 200 {
		t.Fatalf("GetControlClass = %d (%+v)", status, body)
	}
	inherited := body.(ms05.NcMethodResultClassDescriptor).Value

	own, err := json.Marshal(map[string]any{
		"classId": ms05.NcClassId{1, 1}, "includeInherited": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, body = invokeNamed(t, s, cm, "GetControlClass", string(own))
	ownDesc := body.(ms05.NcMethodResultClassDescriptor).Value
	if len(inherited.Properties) <= len(ownDesc.Properties) {
		t.Errorf("the inherited descriptor must carry more properties (%d vs %d)",
			len(inherited.Properties), len(ownDesc.Properties))
	}

	for name, args := range map[string]string{
		"no classId":                       `{}`,
		"a class the model does not carry": `{"classId":[9,9,9]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if status, _ := invokeNamed(t, s, cm, "GetControlClass", args); status != 400 {
				t.Error("was accepted")
			}
		})
	}

	status, body = invokeNamed(t, s, cm, "GetDatatype", `{"name":"NcDatatypeDescriptorEnum"}`)
	if status != 200 {
		t.Fatalf("GetDatatype = %d (%+v)", status, body)
	}
	merged := body.(ms05.NcMethodResultDatatypeDescriptor).Value
	_, body = invokeNamed(t, s, cm, "GetDatatype",
		`{"name":"NcDatatypeDescriptorEnum","includeInherited":false}`)
	bare := body.(ms05.NcMethodResultDatatypeDescriptor).Value
	if len(merged.Fields) <= len(bare.Fields) {
		t.Errorf("the merged datatype must carry more fields (%d vs %d)",
			len(merged.Fields), len(bare.Fields))
	}

	for name, args := range map[string]string{
		"no name":                             `{}`,
		"a datatype the model does not carry": `{"name":"NcNotADatatype","includeInherited":false}`,
	} {
		t.Run(name, func(t *testing.T) {
			if status, _ := invokeNamed(t, s, cm, "GetDatatype", args); status != 400 {
				t.Error("was accepted")
			}
		})
	}
}

// The bulk-properties methods are the device-configuration feature
// set: read a subtree, validate a restore against it, then apply.
func TestInvokeBulkPropertiesByPath(t *testing.T) {
	s := configFixture(t)
	bulk := objectOfClass(t, s, ms05.NcClassId{1, 3, 3})

	status, body := invokeNamed(t, s, bulk, "GetPropertiesByPath", `{"path":["root"]}`)
	if status != 200 {
		t.Fatalf("GetPropertiesByPath = %d (%+v)", status, body)
	}
	res, ok := body.(is14.ResultBulkPropertiesHolder)
	if !ok {
		t.Fatalf("body = %T, want a bulk properties holder", body)
	}
	holder := res.Value
	if len(holder.Values) == 0 {
		t.Fatal("a backup must carry the object's properties")
	}
	// The same read without descriptors is smaller but still a backup.
	if status, _ := invokeNamed(t, s, bulk, "GetPropertiesByPath",
		`{"path":["root"],"includeDescriptors":false}`); status != 200 {
		t.Error("a descriptor-free backup must still be served")
	}

	dataSet, err := json.Marshal(holder)
	if err != nil {
		t.Fatal(err)
	}
	validate := `{"path":["root"],"dataSet":` + string(dataSet) + `,"restoreMode":0}`
	if status, _ := invokeNamed(t, s, bulk, "ValidateSetPropertiesByPath", validate); status != 200 {
		t.Error("validating a backup taken from the same model must be served")
	}
	if status, _ := invokeNamed(t, s, bulk, "SetPropertiesByPath", validate); status != 200 {
		t.Error("restoring a backup taken from the same model must be served")
	}

	for name, args := range map[string]string{
		"no path":                    `{}`,
		"a path naming no object":    `{"path":["nowhere"]}`,
		"a restore with no data set": `{"path":["root"],"restoreMode":0}`,
		"a restore mode the spec does not define": `{"path":["root"],"dataSet":` +
			string(dataSet) + `,"restoreMode":9}`,
	} {
		t.Run(name, func(t *testing.T) {
			if status, _ := invokeNamed(t, s, bulk, "SetPropertiesByPath", args); status != 400 {
				t.Error("was accepted")
			}
		})
	}
}
