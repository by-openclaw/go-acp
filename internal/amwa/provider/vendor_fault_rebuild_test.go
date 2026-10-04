package provider

// The rebuildable fault worker: IS-14 Backup & restore lets a Rebuild
// restore set a rebuildable object's own read-only properties, and
// DhsFaultControl.armed is the one that matters — the interlock that
// is a construction-time decision, carried by the backup. IS-14-01
// test_23 drives exactly this sequence.

import (
	"encoding/json"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is14"
	"dhs/internal/amwa/codec/ms05"
)

func rebuildDataSet(path string, values string) string {
	return `{"arguments":{"dataSet":{"validationFingerprint":null,"values":[{"path":["root","` + path +
		`"],"dependencyPaths":[],"allowedMembersClasses":[],"isRebuildable":true,"values":[` + values +
		`]}]},"recurse":true,"restoreMode":1}}`
}

func noticesOf(t *testing.T, raw []byte) (status ms05.NcMethodStatus, notices map[string]is14.PropertyRestoreNotice) {
	t.Helper()
	var resp struct {
		Status ms05.NcMethodStatus                  `json:"status"`
		Value  []is14.ObjectPropertiesSetValidation `json:"value"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	if len(resp.Value) != 1 {
		t.Fatalf("validations = %+v, want one entry", resp.Value)
	}
	notices = map[string]is14.PropertyRestoreNotice{}
	for _, n := range resp.Value[0].Notices {
		notices[n.Name] = n
	}
	return resp.Value[0].Status, notices
}

// The backup names the one rebuildable object, and only that one.
func TestFaultWorkerIsTheRebuildableObject(t *testing.T) {
	s := configFixture(t)
	holder := s.backup(s.objectByOid(1), true, false)
	seen := map[string]bool{}
	for _, oph := range holder.Values {
		key := strings.Join(oph.Path, ".")
		seen[key] = oph.IsRebuildable
		if oph.IsRebuildable != (key == "root."+faultRole) {
			t.Errorf("%s isRebuildable = %v", key, oph.IsRebuildable)
		}
	}
	if !seen["root."+faultRole] {
		t.Fatal("the fault worker is missing from the backup")
	}
	fault := s.objects["root."+faultRole]
	if p := findPropByName(fault, "armed"); p == nil || !p.desc.IsReadOnly || p.value != true {
		t.Errorf("armed = %+v, want a read-only property seeded true", p)
	}
}

// test_23's sequence: the tool offers every read-only property of the
// rebuildable object with a changed value. Validation notices the
// structural ones (NcObject's members) as Warnings and accepts the
// object's own; the restore applies the accepted one and the interlock
// takes effect; a Modify cannot undo it, a Rebuild can.
func TestRebuildRestoreReconstructsTheFaultWorker(t *testing.T) {
	// The audio bundle carries receivers, hence status monitors to
	// inject into.
	addr := serveNCPBundleNode(t, audioBundle())
	cfg := "http://" + addr + "/x-nmos/configuration/v1.0/rolePaths/root.FaultControl/"
	bp := cfg + "bulkProperties/"
	armed := cfg + "properties/4p1/value/"
	inject := cfg + "methods/4m1/"
	// Any status monitor of the served bundle will do as the target.
	st0, paths := doJSON(t, "GET", "http://"+addr+"/x-nmos/configuration/v1.0/rolePaths/", "")
	var rolePaths []string
	if err := json.Unmarshal(paths, &rolePaths); err != nil || st0 != 200 {
		t.Fatalf("rolePaths = %d %s (%v)", st0, paths, err)
	}
	monitor := ""
	for _, p := range rolePaths {
		if strings.Contains(p, "Monitor-") {
			monitor = strings.TrimSuffix(strings.TrimPrefix(p, "root."), "/")
			break
		}
	}
	if monitor == "" {
		t.Fatalf("no status monitor in the served model: %v", rolePaths)
	}
	injectBody := `{"arguments":{"monitorRole":"` + monitor + `","domain":"linkStatus","status":3,"message":"rebuild test"}}`

	ph := func(level, index int, value string) string {
		return `{"id":{"level":` + string(rune('0'+level)) + `,"index":` + string(rune('0'+index)) + `},"descriptor":null,"value":` + value + `}`
	}
	// The tool's generated values: every integer +1, booleans flipped,
	// strings replaced, nulls kept.
	everyReadOnly := strings.Join([]string{
		ph(1, 1, `[2,3,1,3]`), ph(1, 2, `99`), ph(1, 3, `false`), ph(1, 4, `2`), ph(1, 5, `"XKCD"`),
		ph(1, 7, `null`), ph(1, 8, `null`), ph(4, 1, `false`),
	}, ",")

	// Validate only: Warnings on NcObject's seven, none on armed, and
	// nothing applied.
	st, raw := doJSON(t, "PATCH", bp, rebuildDataSet(faultRole, everyReadOnly))
	if st != 200 {
		t.Fatalf("validate = %d %s", st, raw)
	}
	vst, notices := noticesOf(t, raw)
	if vst != ms05.NcMethodStatusOk {
		t.Errorf("validation status = %d, want Ok with warnings", vst)
	}
	for _, name := range []string{"classId", "oid", "constantOid", "owner", "role", "touchpoints", "runtimePropertyConstraints"} {
		n, ok := notices[name]
		if !ok || n.NoticeType != is14.NoticeWarning || !strings.Contains(n.NoticeMessage, "parent block is not rebuildable") {
			t.Errorf("%s notice = %+v, want the structural warning", name, n)
		}
	}
	if n, ok := notices["armed"]; ok {
		t.Errorf("armed drew a notice on a Rebuild: %+v", n)
	}
	if st, raw := doJSON(t, "GET", armed, ""); st != 200 || !strings.Contains(string(raw), "true") {
		t.Errorf("validate-only changed armed: %d %s", st, raw)
	}

	// Restore (the tool re-sends only the accepted property): applied,
	// and the interlock is live on the methods.
	st, raw = doJSON(t, "PUT", bp, rebuildDataSet(faultRole, ph(4, 1, `false`)))
	if st != 200 {
		t.Fatalf("rebuild = %d %s", st, raw)
	}
	if vst, notices := noticesOf(t, raw); vst != ms05.NcMethodStatusOk || len(notices) != 0 {
		t.Errorf("rebuild validation = %d %+v, want Ok and no notices", vst, notices)
	}
	if st, raw := doJSON(t, "GET", armed, ""); st != 200 || !strings.Contains(string(raw), "false") {
		t.Errorf("armed after the rebuild = %d %s, want false", st, raw)
	}
	st, raw = doJSON(t, "PATCH", inject, injectBody)
	if st != 400 || !strings.Contains(string(raw), `"status":423`) {
		t.Errorf("InjectMonitorFault while disarmed = %d %s, want Locked", st, raw)
	}

	// A Modify restore cannot re-arm it (readonly warning, unchanged);
	// a Rebuild can, and the methods work again.
	modify := strings.Replace(rebuildDataSet(faultRole, ph(4, 1, `true`)), `"restoreMode":1`, `"restoreMode":0`, 1)
	st, raw = doJSON(t, "PUT", bp, modify)
	if st != 200 {
		t.Fatalf("modify = %d %s", st, raw)
	}
	if _, notices := noticesOf(t, raw); notices["armed"].NoticeMessage != "Property is readonly" {
		t.Errorf("modify notices = %+v, want the readonly warning on armed", notices)
	}
	if st, raw := doJSON(t, "GET", armed, ""); st != 200 || !strings.Contains(string(raw), "false") {
		t.Errorf("a Modify re-armed the worker: %d %s", st, raw)
	}
	if st, raw := doJSON(t, "PUT", bp, rebuildDataSet(faultRole, ph(4, 1, `true`))); st != 200 {
		t.Fatalf("re-arm = %d %s", st, raw)
	}
	// Re-armed, the method is back in the engine's hands: whatever it
	// says about an inactive monitor, it is no longer the interlock.
	if st, raw := doJSON(t, "PATCH", inject, injectBody); strings.Contains(string(raw), `"status":423`) || strings.Contains(string(raw), "disarmed") {
		t.Errorf("InjectMonitorFault once re-armed = %d %s, still the interlock", st, raw)
	}
}

// A Rebuild on an object that is not rebuildable is a Modify with
// notices: the gain worker's deprecated read-only legacyTrim stays.
func TestRebuildRestoreLeavesNonRebuildableObjects(t *testing.T) {
	s := configFixture(t)
	gain := s.objects["root.GainControl"]
	mode := is14.RestoreModeRebuild
	recurse := true
	trim := ms05.NcPropertyId{Level: 4, Index: 3}
	out := s.restore(s.objectByOid(1), &is14.BulkPropertiesSetArgs{
		DataSet: &is14.BulkPropertiesHolder{Values: []is14.ObjectPropertiesHolder{{
			Path:   gain.path,
			Values: []is14.PropertyHolder{{ID: trim, Value: 7.5}},
		}}},
		Recurse: &recurse, RestoreMode: &mode,
	}, true)
	if len(out) != 1 || len(out[0].Notices) != 1 || out[0].Notices[0].NoticeMessage != "Property is readonly" {
		t.Errorf("validations = %+v, want one readonly warning", out)
	}
	if v := findPropByName(gain, "legacyTrim").value; v != 0.0 {
		t.Errorf("legacyTrim = %v after a Rebuild on a non-rebuildable object", v)
	}
}

// The interlock on the IS-12 face: Locked (423), not a parameter
// error, while disarmed; and the model without the worker, or with
// one missing the property, counts as armed.
func TestFaultInterlockOverIS12AndWithoutTheWorker(t *testing.T) {
	s := ncpFixture(t)
	fault := s.config.objects["root."+faultRole]
	armed := findPropByName(fault, "armed")
	if st, err := s.config.writeProperty(fault, armed, json.RawMessage(`false`), true); err != nil {
		t.Fatalf("disarm = %d %v", st, err)
	}
	args := map[string]any{"monitorRole": "ReceiverMonitor-00", "domain": "linkStatus", "status": 3}
	if r := ncpCall(t, s, int(fault.oid), 4, 1, args); r.Status != int(ms05.NcMethodStatusLocked) {
		t.Errorf("InjectMonitorFault while disarmed = %d (%s), want 423", r.Status, r.ErrorMessage)
	}
	// setProperty keeps the door shut: armed is readonly to a Set.
	if st, err := s.config.setProperty(fault, armed, json.RawMessage(`true`)); err == nil || st != ms05.NcMethodStatusReadonly {
		t.Errorf("Set armed = %d %v, want Readonly", st, err)
	}

	// Without the property, and without the worker.
	kept := fault.props[:0:0]
	for _, p := range fault.props {
		if p.desc.Name != "armed" {
			kept = append(kept, p)
		}
	}
	fault.props = kept
	if !s.config.faultArmed() {
		t.Error("a worker without the interlock property counts as armed")
	}
	delete(s.config.objects, "root."+faultRole)
	if !s.config.faultArmed() {
		t.Error("a model without the worker counts as armed")
	}
}
