package consumer

// The IS-14 half of the Controller: the endpoint comes from the
// Device's configuration control, a set is read back from the Device,
// and a restore is applied only when the Device validates every object.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is14"
)

const configHolder = `{"validationFingerprint":null,"values":[
  {"path":["root","gain"],"dependencyPaths":[],"allowedMembersClasses":[],"isRebuildable":false,
   "values":[{"id":{"level":3,"index":1},"descriptor":null,"value":-6.0}]}]}`

// configDevice is the Configuration API of one Device with one object,
// root.gain, holding one property 3p1.
type configDevice struct {
	value    string // 3p1, as JSON
	ignore   bool   // answer OK to a set and keep the old value
	invalid  bool   // the restore validation refuses root.gain
	puts     int    // bulkProperties PUTs
	patches  int    // bulkProperties PATCHes
	invoked  []string
	answers  map[string]string // "<METHOD> <rest>" -> body, replacing the default
	failures map[string]bool   // "<METHOD> <rest>" answering HTTP 500
}

func (d *configDevice) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rest := r.URL.Path[strings.Index(r.URL.Path, "/v1.0/")+len("/v1.0/"):]
		key := r.Method + " " + rest
		if d.failures[key] {
			http.Error(w, "device fault", http.StatusInternalServerError)
			return
		}
		if body, ok := d.answers[key]; ok {
			_, _ = io.WriteString(w, body)
			return
		}
		switch key {
		case "GET rolePaths/":
			_, _ = io.WriteString(w, `["root/","root.gain/"]`)
		case "GET rolePaths/root.gain/descriptor/":
			_, _ = io.WriteString(w, `{"status":200,"value":{"name":"GainControl"}}`)
		case "GET rolePaths/root.gain/properties/":
			_, _ = io.WriteString(w, `["1p1/","3p1/"]`)
		case "GET rolePaths/root.gain/methods/":
			_, _ = io.WriteString(w, `["3m1/"]`)
		case "GET rolePaths/root.gain/properties/3p1/value/":
			_, _ = io.WriteString(w, `{"status":200,"value":`+d.value+`}`)
		case "PUT rolePaths/root.gain/properties/3p1/value/":
			var req is14.PropertyValuePutRequest
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &req)
			if !d.ignore {
				d.value = string(req.Value)
			}
			_, _ = io.WriteString(w, `{"status":200}`)
		case "PATCH rolePaths/root.gain/methods/3m1/":
			raw, _ := io.ReadAll(r.Body)
			d.invoked = append(d.invoked, string(raw))
			_, _ = io.WriteString(w, `{"status":200,"value":"reset"}`)
		case "GET rolePaths/root/bulkProperties/":
			_, _ = io.WriteString(w, `{"status":200,"value":`+configHolder+`}`)
		case "PATCH rolePaths/root/bulkProperties/", "PUT rolePaths/root/bulkProperties/":
			if r.Method == http.MethodPut {
				d.puts++
			} else {
				d.patches++
			}
			status := "200"
			if d.invalid {
				status = "400"
			}
			_, _ = io.WriteString(w, `{"status":200,"value":[{"path":["root","gain"],"status":`+status+`,"notices":[],"statusMessage":null}]}`)
		default:
			http.NotFound(w, r)
		}
	}
}

// configHarness is a catalogue with one Device that advertises IS-14,
// and that API behind it.
func configHarness(t *testing.T) (*harness, *configDevice, string) {
	t.Helper()
	h := newHarness(t)
	d := &configDevice{value: "-6.0", answers: map[string]string{}, failures: map[string]bool{}}
	h.is14 = d.handler()
	dev := deviceWith(uuidN(1), "")
	dev.Controls = []is04.DeviceControl{{
		Href: strings.Replace(h.controlHref, "/x-nmos/connection/v1.1", "/x-nmos/configuration/v1.0", 1),
		Type: "urn:x-nmos:control:configuration/v1.0",
	}}
	h.cat.devices = []is04.Device{dev}
	return h, d, dev.ID
}

func holder(t *testing.T) *is14.BulkPropertiesHolder {
	t.Helper()
	hd, err := is14.DecodeBulkPropertiesHolder([]byte(configHolder))
	if err != nil {
		t.Fatal(err)
	}
	return &hd
}

func TestConfigureReadsTheModel(t *testing.T) {
	h, _, dev := configHarness(t)
	ctx := context.Background()

	res, err := h.ctrl.Configure(ctx, ConfigRequest{DeviceID: dev})
	if err != nil || len(res.RolePaths) != 2 || !strings.HasSuffix(res.Endpoint, "/x-nmos/configuration/v1.0") {
		t.Fatalf("listing = %+v, %v", res, err)
	}

	res, err = h.ctrl.Configure(ctx, ConfigRequest{DeviceID: dev, RolePath: "root.gain"})
	if err != nil || len(res.PropertyIDs) != 2 || len(res.MethodIDs) != 1 || !strings.Contains(string(res.Descriptor), "GainControl") {
		t.Errorf("describing = %+v, %v", res, err)
	}

	res, err = h.ctrl.Configure(ctx, ConfigRequest{DeviceID: dev, RolePath: "root.gain", Get: "3p1"})
	if err != nil || string(res.Value) != "-6.0" {
		t.Errorf("get = %+v, %v", res, err)
	}

	res, err = h.ctrl.Configure(ctx, ConfigRequest{DeviceID: dev, RolePath: "root", Backup: true, Recurse: true})
	if err != nil || res.Holder == nil || len(res.Holder.Values) != 1 {
		t.Errorf("backup = %+v, %v", res, err)
	}
}

func TestConfigureSetsAndReadsBack(t *testing.T) {
	h, d, dev := configHarness(t)
	ctx := context.Background()

	// -3 and -3.0 are one number: no event.
	res, err := h.ctrl.Configure(ctx, ConfigRequest{DeviceID: dev, RolePath: "root.gain", Set: "3p1", SetValue: json.RawMessage(`-3`)})
	if err != nil || string(res.Value) != "-3" {
		t.Fatalf("set = %+v, %v", res, err)
	}
	d.value = "-3.0"
	if _, err := h.ctrl.Configure(ctx, ConfigRequest{DeviceID: dev, RolePath: "root.gain", Set: "3p1", SetValue: json.RawMessage(`-3`)}); err != nil {
		t.Fatal(err)
	}
	if evs := h.rep.Snapshot(); len(evs) != 0 {
		t.Errorf("an applied set fires nothing, got %+v", evs)
	}

	// A Device that answers OK and keeps the old value is reported.
	d.ignore = true
	if _, err := h.ctrl.Configure(ctx, ConfigRequest{DeviceID: dev, RolePath: "root.gain", Set: "3p1", SetValue: json.RawMessage(`0`)}); err != nil {
		t.Fatal(err)
	}
	evs := h.rep.Snapshot()
	if len(evs) != 1 || evs[0].Code != "nmos_is14_set_not_applied" || !strings.Contains(evs[0].Detail, "= 0 and holds -3") {
		t.Errorf("events = %+v", evs)
	}

	// A value that is not JSON on the way back compares as different.
	if sameJSON(json.RawMessage(`{`), json.RawMessage(`1`)) {
		t.Error("an unreadable value must not compare equal")
	}
}

func TestConfigureInvokes(t *testing.T) {
	h, d, dev := configHarness(t)

	res, err := h.ctrl.Configure(context.Background(), ConfigRequest{
		DeviceID: dev, RolePath: "root.gain", Invoke: "3m1", Arguments: json.RawMessage(`{"hard":true}`),
	})
	if err != nil || string(res.Value) != `"reset"` {
		t.Fatalf("invoke = %+v, %v", res, err)
	}
	if len(d.invoked) != 1 || !strings.Contains(d.invoked[0], `"hard":true`) {
		t.Errorf("the Device was sent %v", d.invoked)
	}
}

func TestConfigureRestoresOnlyWhatTheDeviceValidates(t *testing.T) {
	h, d, dev := configHarness(t)
	ctx := context.Background()
	req := ConfigRequest{DeviceID: dev, RolePath: "root", Restore: holder(t), Recurse: true}

	res, err := h.ctrl.Configure(ctx, req)
	if err != nil || !res.Restored || d.patches != 1 || d.puts != 1 {
		t.Fatalf("restore = %+v, %v (validated %d, applied %d)", res, err, d.patches, d.puts)
	}

	// Validate only, and a dry run: asked, never applied.
	for _, alter := range []func(*ConfigRequest){
		func(r *ConfigRequest) { r.ValidateOnly = true },
		func(r *ConfigRequest) { r.DryRun = true },
	} {
		r := req
		alter(&r)
		res, err := h.ctrl.Configure(ctx, r)
		if err != nil || res.Restored || len(res.Validations) != 1 {
			t.Errorf("validation only = %+v, %v", res, err)
		}
	}
	if d.puts != 1 {
		t.Errorf("a validation applied the data set: %d PUTs", d.puts)
	}

	// The Device would refuse an object: nothing is restored…
	d.invalid = true
	if _, err := h.ctrl.Configure(ctx, req); err == nil || !strings.Contains(err.Error(), "nothing was restored") {
		t.Errorf("a refused validation: %v", err)
	}
	if d.puts != 1 {
		t.Errorf("an invalid data set was applied: %d PUTs", d.puts)
	}
	// …and when only the validation was asked for, that is the answer.
	r := req
	r.ValidateOnly = true
	res, err = h.ctrl.Configure(ctx, r)
	if err != nil || res.Restored || res.Validations[0].Status != 400 {
		t.Errorf("validation of an invalid data set = %+v, %v", res, err)
	}
}

func TestConfigureDryRunChangesNothing(t *testing.T) {
	h, d, dev := configHarness(t)
	ctx := context.Background()

	res, err := h.ctrl.Configure(ctx, ConfigRequest{DeviceID: dev, RolePath: "root.gain", Set: "3p1", SetValue: json.RawMessage(`0`), DryRun: true})
	if err != nil || string(res.Value) != "-6.0" || d.value != "-6.0" {
		t.Errorf("dry-run set = %+v, %v (device holds %s)", res, err, d.value)
	}
	if _, err := h.ctrl.Configure(ctx, ConfigRequest{DeviceID: dev, RolePath: "root.gain", Invoke: "3m1", DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if len(d.invoked) != 0 {
		t.Errorf("a dry run invoked %v", d.invoked)
	}
}

func TestConfigureRequestIsCheckedBeforeAnythingIsWalked(t *testing.T) {
	h, _, dev := configHarness(t)
	ctx := context.Background()

	for name, req := range map[string]ConfigRequest{
		"no device":             {RolePath: "root"},
		"two operations":        {DeviceID: dev, RolePath: "root.gain", Get: "3p1", Backup: true},
		"an op without a path":  {DeviceID: dev, Get: "3p1"},
		"a set without a value": {DeviceID: dev, RolePath: "root.gain", Set: "3p1"},
		"a value not JSON":      {DeviceID: dev, RolePath: "root.gain", Set: "3p1", SetValue: json.RawMessage(`{`)},
		"arguments not JSON":    {DeviceID: dev, RolePath: "root.gain", Invoke: "3m1", Arguments: json.RawMessage(`{`)},
		"an undefined mode":     {DeviceID: dev, RolePath: "root", Restore: holder(t), RestoreMode: 7},
		"a device not listed":   {DeviceID: uuidN(9), RolePath: "root"},
	} {
		if _, err := h.ctrl.Configure(ctx, req); err == nil {
			t.Errorf("%s: must be refused", name)
		}
	}

	h.cat.devices[0].Controls = []is04.DeviceControl{}
	if _, err := h.ctrl.Configure(ctx, ConfigRequest{DeviceID: dev}); err == nil || !strings.Contains(err.Error(), "configuration control") {
		t.Errorf("a Device without IS-14: %v", err)
	}
	h.cat.devices[0].Controls = []is04.DeviceControl{{Href: "http://h/x-nmos/configuration", Type: "urn:x-nmos:control:configuration/v1.0"}}
	if _, err := h.ctrl.Configure(ctx, ConfigRequest{DeviceID: dev}); err == nil {
		t.Error("a control href with no version must be refused")
	}
}

// Every call can fail, and a Device can answer any of them with a
// method status that says no.
func TestConfigureCarriesTheDevicesFailuresAndRefusals(t *testing.T) {
	ctx := context.Background()
	set := func(dev string) ConfigRequest {
		return ConfigRequest{DeviceID: dev, RolePath: "root.gain", Set: "3p1", SetValue: json.RawMessage(`0`)}
	}
	describe := func(dev string) ConfigRequest { return ConfigRequest{DeviceID: dev, RolePath: "root.gain"} }
	get := func(dev string) ConfigRequest { return ConfigRequest{DeviceID: dev, RolePath: "root.gain", Get: "3p1"} }
	invoke := func(dev string) ConfigRequest {
		return ConfigRequest{DeviceID: dev, RolePath: "root.gain", Invoke: "3m1"}
	}
	backup := func(dev string) ConfigRequest { return ConfigRequest{DeviceID: dev, RolePath: "root", Backup: true} }
	restore := func(dev string) ConfigRequest {
		return ConfigRequest{DeviceID: dev, RolePath: "root", Restore: holder(t)}
	}
	dryRunSet := func(dev string) ConfigRequest { r := set(dev); r.DryRun = true; return r }

	const refused = `{"status":404,"errorMessage":"no such member"}`
	const refusedBare = `{"status":500}`
	for _, tc := range []struct {
		name string
		req  func(string) ConfigRequest
		fail string // a call answering HTTP 500
		say  string // a call answering this body instead
		body string
	}{
		{"listing fails", func(dev string) ConfigRequest { return ConfigRequest{DeviceID: dev} }, "GET rolePaths/", "", ""},
		{"descriptor fails", describe, "GET rolePaths/root.gain/descriptor/", "", ""},
		{"descriptor refused", describe, "", "GET rolePaths/root.gain/descriptor/", refused},
		{"properties fail", describe, "GET rolePaths/root.gain/properties/", "", ""},
		{"methods fail", describe, "GET rolePaths/root.gain/methods/", "", ""},
		{"get fails", get, "GET rolePaths/root.gain/properties/3p1/value/", "", ""},
		{"get refused", get, "", "GET rolePaths/root.gain/properties/3p1/value/", refusedBare},
		{"set fails", set, "PUT rolePaths/root.gain/properties/3p1/value/", "", ""},
		{"set refused", set, "", "PUT rolePaths/root.gain/properties/3p1/value/", refused},
		{"read-back fails", set, "GET rolePaths/root.gain/properties/3p1/value/", "", ""},
		{"dry-run read fails", dryRunSet, "GET rolePaths/root.gain/properties/3p1/value/", "", ""},
		{"invoke fails", invoke, "PATCH rolePaths/root.gain/methods/3m1/", "", ""},
		{"invoke refused", invoke, "", "PATCH rolePaths/root.gain/methods/3m1/", refused},
		{"backup fails", backup, "GET rolePaths/root/bulkProperties/", "", ""},
		{"backup refused", backup, "", "GET rolePaths/root/bulkProperties/", refused},
		{"validation fails", restore, "PATCH rolePaths/root/bulkProperties/", "", ""},
		{"validation refused", restore, "", "PATCH rolePaths/root/bulkProperties/", refused},
		{"restore fails", restore, "PUT rolePaths/root/bulkProperties/", "", ""},
		{"restore refused", restore, "", "PUT rolePaths/root/bulkProperties/", refused},
	} {
		h, d, dev := configHarness(t)
		if tc.fail != "" {
			d.failures[tc.fail] = true
		}
		if tc.say != "" {
			d.answers[tc.say] = tc.body
		}
		_, err := h.ctrl.Configure(ctx, tc.req(dev))
		if err == nil {
			t.Errorf("%s: must fail", tc.name)
			continue
		}
		if tc.body == refused && !strings.Contains(err.Error(), "the Device answered 404: no such member") {
			t.Errorf("%s: the Device's own words must come through, got %v", tc.name, err)
		}
		if tc.body == refusedBare && !strings.HasSuffix(err.Error(), "the Device answered 500") {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}
