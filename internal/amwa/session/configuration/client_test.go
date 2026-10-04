package configuration

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is14"
)

const holderBody = `{"validationFingerprint":null,"values":[
  {"path":["root","gain"],"dependencyPaths":[],"allowedMembersClasses":[],"isRebuildable":false,
   "values":[{"id":{"level":3,"index":1},"descriptor":null,"value":-6.0}]}]}`

// device serves the Configuration API of one Device and records what it
// was asked.
type device struct {
	srv     *httptest.Server
	asked   []string          // "<METHOD> <path>?<query>"
	bodies  []string          // request bodies, in order
	answers map[string]answer // "<METHOD> <path>" -> answer, replacing the default
}

type answer struct {
	code int
	body string
}

func newDevice(t *testing.T) *device {
	t.Helper()
	d := &device{answers: map[string]answer{}}
	d.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/x-nmos/configuration/v1.0/")
		key := r.Method + " " + rest
		asked := key
		if r.URL.RawQuery != "" {
			asked += "?" + r.URL.RawQuery
		}
		d.asked = append(d.asked, asked)
		body, _ := io.ReadAll(r.Body)
		d.bodies = append(d.bodies, string(body))
		if a, ok := d.answers[key]; ok {
			w.WriteHeader(a.code)
			_, _ = io.WriteString(w, a.body)
			return
		}
		switch key {
		case "GET rolePaths/":
			_, _ = io.WriteString(w, `["root/","root.gain/"]`)
		case "GET rolePaths/root.gain/properties/":
			_, _ = io.WriteString(w, `["1p1/","3p1/"]`)
		case "GET rolePaths/root.gain/methods/":
			_, _ = io.WriteString(w, `["1m1/","3m1/"]`)
		case "GET rolePaths/root.gain/descriptor/":
			_, _ = io.WriteString(w, `{"status":200,"value":{"classId":[1,2,1],"name":"GainControl"}}`)
		case "GET rolePaths/root.gain/properties/3p1/descriptor/":
			_, _ = io.WriteString(w, `{"status":200,"value":{"name":"NcFloat32","type":0}}`)
		case "GET rolePaths/root.gain/properties/3p1/value/":
			_, _ = io.WriteString(w, `{"status":200,"value":-6.0}`)
		case "PUT rolePaths/root.gain/properties/3p1/value/":
			_, _ = io.WriteString(w, `{"status":200}`)
		case "PATCH rolePaths/root.gain/methods/3m1/":
			_, _ = io.WriteString(w, `{"status":299,"value":true}`)
		case "GET rolePaths/root/bulkProperties/":
			_, _ = io.WriteString(w, `{"status":200,"value":`+holderBody+`}`)
		case "PATCH rolePaths/root/bulkProperties/", "PUT rolePaths/root/bulkProperties/":
			_, _ = io.WriteString(w, `{"status":200,"value":[{"path":["root","gain"],"status":200,"notices":[],"statusMessage":null}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(d.srv.Close)
	return d
}

func (d *device) client(t *testing.T) *Client {
	t.Helper()
	c, err := NewClient(d.srv.URL + "/x-nmos/configuration/v1.0/")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNewClientReadsTheVersionFromTheControlHref(t *testing.T) {
	c, err := NewClient("http://10.6.255.102:3000/x-nmos/configuration/v1.0/")
	if err != nil {
		t.Fatal(err)
	}
	if c.APIVer != "v1.0" || c.Base != "http://10.6.255.102:3000/x-nmos/configuration/v1.0" {
		t.Errorf("client = %+v", c)
	}
	for _, bad := range []string{
		"http://bad host/x-nmos/configuration/v1.0",
		"/x-nmos/configuration/v1.0",
		"http://10.6.255.102:3000/x-nmos/configuration",
	} {
		if _, err := NewClient(bad); err == nil {
			t.Errorf("NewClient(%q) must be refused", bad)
		}
	}
}

func TestClientReadsTheModel(t *testing.T) {
	d := newDevice(t)
	c := d.client(t)
	ctx := context.Background()

	paths, err := c.RolePaths(ctx)
	if err != nil || len(paths) != 2 || paths[1] != "root.gain" {
		t.Errorf("role paths = %v, %v — want them without their trailing slash", paths, err)
	}
	props, err := c.PropertyIDs(ctx, "root.gain")
	if err != nil || len(props) != 2 || props[1] != "3p1" {
		t.Errorf("properties = %v, %v", props, err)
	}
	methods, err := c.MethodIDs(ctx, "root.gain")
	if err != nil || len(methods) != 2 || methods[1] != "3m1" {
		t.Errorf("methods = %v, %v", methods, err)
	}

	desc, err := c.Descriptor(ctx, "root.gain")
	if err != nil || !desc.OK() || !strings.Contains(string(desc.Value), "GainControl") {
		t.Errorf("descriptor = %+v, %v", desc, err)
	}
	pd, err := c.PropertyDescriptor(ctx, "root.gain", "3p1")
	if err != nil || !strings.Contains(string(pd.Value), "NcFloat32") {
		t.Errorf("property descriptor = %+v, %v", pd, err)
	}
	val, err := c.GetProperty(ctx, "root.gain", "3p1")
	if err != nil || !val.OK() || string(val.Value) != "-6.0" {
		t.Errorf("value = %+v, %v", val, err)
	}
}

func TestClientSetsAndInvokes(t *testing.T) {
	d := newDevice(t)
	c := d.client(t)
	ctx := context.Background()

	res, err := c.SetProperty(ctx, "root.gain", "3p1", json.RawMessage(`-3.5`))
	if err != nil || !res.OK() {
		t.Fatalf("set = %+v, %v", res, err)
	}
	if got := d.bodies[len(d.bodies)-1]; got != `{"value":-3.5}` {
		t.Errorf("the Device was sent %s", got)
	}

	// 299 MethodDeprecated is done, with a note — still OK.
	res, err = c.Invoke(ctx, "root.gain", "3m1", json.RawMessage(`{"reason":"test"}`))
	if err != nil || !res.OK() || res.Status != 299 {
		t.Errorf("invoke = %+v, %v", res, err)
	}
	if got := d.bodies[len(d.bodies)-1]; got != `{"arguments":{"reason":"test"}}` {
		t.Errorf("the Device was sent %s", got)
	}
	// No arguments is an empty object, not null.
	if _, err := c.Invoke(ctx, "root.gain", "3m1", nil); err != nil {
		t.Fatal(err)
	}
	if got := d.bodies[len(d.bodies)-1]; got != `{"arguments":{}}` {
		t.Errorf("an argument-less invoke sent %s", got)
	}
}

// A Device says no in MS-05's vocabulary, with an HTTP status that
// mirrors it. That is a Result to read, not a transport error.
func TestARefusedSetIsAResultNotAnError(t *testing.T) {
	d := newDevice(t)
	d.answers["PUT rolePaths/root.gain/properties/3p1/value/"] = answer{
		http.StatusBadRequest, `{"status":417,"errorMessage":"gain must be within -60..12 dB"}`}
	res, err := d.client(t).SetProperty(context.Background(), "root.gain", "3p1", json.RawMessage(`99`))
	if err != nil {
		t.Fatalf("a method result on a 400 must not be an error: %v", err)
	}
	if res.OK() || res.Status != 417 || !strings.Contains(res.ErrorMessage, "-60..12") {
		t.Errorf("result = %+v", res)
	}
}

func TestClientBacksUpValidatesAndRestores(t *testing.T) {
	d := newDevice(t)
	c := d.client(t)
	ctx := context.Background()

	holder, res, err := c.Backup(ctx, "root", true)
	if err != nil || !res.OK() || len(holder.Values) != 1 || holder.Values[0].Path[1] != "gain" {
		t.Fatalf("backup = %+v, %+v, %v", holder, res, err)
	}
	if got := d.asked[len(d.asked)-1]; got != "GET rolePaths/root/bulkProperties/?recurse=true" {
		t.Errorf("backup asked %s", got)
	}

	validations, res, err := c.Validate(ctx, "root", holder, true, is14.RestoreModeModify)
	if err != nil || !res.OK() || len(validations) != 1 || validations[0].Status != 200 {
		t.Errorf("validate = %+v, %+v, %v", validations, res, err)
	}
	if got := d.asked[len(d.asked)-1]; got != "PATCH rolePaths/root/bulkProperties/" {
		t.Errorf("validate asked %s", got)
	}
	if got := d.bodies[len(d.bodies)-1]; !strings.Contains(got, `"restoreMode":0`) || !strings.Contains(got, `"recurse":true`) {
		t.Errorf("validate sent %s", got)
	}

	if _, _, err := c.Restore(ctx, "root", holder, false, is14.RestoreModeRebuild); err != nil {
		t.Fatal(err)
	}
	if got := d.asked[len(d.asked)-1]; got != "PUT rolePaths/root/bulkProperties/" {
		t.Errorf("restore asked %s", got)
	}
	if got := d.bodies[len(d.bodies)-1]; !strings.Contains(got, `"restoreMode":1`) || !strings.Contains(got, `"recurse":false`) {
		t.Errorf("restore sent %s", got)
	}

	// A restore mode IS-14 does not define never leaves.
	before := len(d.asked)
	if _, _, err := c.Restore(ctx, "root", holder, true, is14.RestoreMode(7)); err == nil {
		t.Error("an undefined restore mode must be refused before it is sent")
	}
	if len(d.asked) != before {
		t.Error("the refused restore reached the Device")
	}
}

func TestAnAnswerThatIsNotWhatIS14DefinesIsRefused(t *testing.T) {
	d := newDevice(t)
	c := d.client(t)
	ctx := context.Background()

	// A backup the Device refuses is a Result with no holder.
	d.answers["GET rolePaths/root/bulkProperties/"] = answer{http.StatusNotFound, `{"status":404,"errorMessage":"no such role path"}`}
	if _, res, err := c.Backup(ctx, "root", false); err != nil || res.OK() {
		t.Errorf("a refused backup = %+v, %v", res, err)
	}
	// One whose value is not a holder.
	d.answers["GET rolePaths/root/bulkProperties/"] = answer{http.StatusOK, `{"status":200,"value":{"values":null}}`}
	if _, _, err := c.Backup(ctx, "root", false); err == nil {
		t.Error("a holder with no values must be refused")
	}
	// A restore the Device refuses, and one whose value is not a list.
	d.answers["PUT rolePaths/root/bulkProperties/"] = answer{http.StatusBadRequest, `{"status":400,"errorMessage":"fingerprint mismatch"}`}
	if _, res, err := c.Restore(ctx, "root", is14.BulkPropertiesHolder{}, true, is14.RestoreModeModify); err != nil || res.OK() {
		t.Errorf("a refused restore = %+v, %v", res, err)
	}
	d.answers["PUT rolePaths/root/bulkProperties/"] = answer{http.StatusOK, `{"status":200,"value":"done"}`}
	if _, _, err := c.Restore(ctx, "root", is14.BulkPropertiesHolder{}, true, is14.RestoreModeModify); err == nil {
		t.Error("validations that are not a list must be refused")
	}

	// A 200 that carries no method result.
	d.answers["GET rolePaths/root.gain/properties/3p1/value/"] = answer{http.StatusOK, `{"hello":"world"}`}
	if _, err := c.GetProperty(ctx, "root.gain", "3p1"); err == nil || !strings.Contains(err.Error(), "not a method result") {
		t.Errorf("a result-less 200: %v", err)
	}
	// A non-2xx that carries none is a StatusError with the body.
	d.answers["GET rolePaths/root.gain/properties/3p1/value/"] = answer{http.StatusBadGateway, `upstream gone`}
	_, err := c.GetProperty(ctx, "root.gain", "3p1")
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusBadGateway || !strings.Contains(se.Error(), "upstream gone") {
		t.Errorf("a result-less 502: %v", err)
	}

	// An index that is refused, and one that is not a list.
	d.answers["GET rolePaths/"] = answer{http.StatusInternalServerError, ``}
	if _, err := c.RolePaths(ctx); !errors.As(err, &se) || se.Error() != "nmos/configuration: rolePaths/: HTTP 500" {
		t.Errorf("a refused index: %v", err)
	}
	d.answers["GET rolePaths/"] = answer{http.StatusOK, `{"not":"a list"}`}
	if _, err := c.RolePaths(ctx); err == nil {
		t.Error("an index that is not a list must be refused")
	}
	if got := (&StatusError{What: "x", Code: 500, Body: strings.Repeat("x", 400)}).Error(); !strings.HasSuffix(got, "…") {
		t.Errorf("long error not cut: %d bytes", len(got))
	}
}

func TestAClientWithNoDeviceSaysSo(t *testing.T) {
	d := newDevice(t)
	c := d.client(t)
	d.srv.Close()
	ctx := context.Background()
	if _, err := c.RolePaths(ctx); err == nil || !strings.Contains(err.Error(), "nmos/configuration: rolePaths/") {
		t.Errorf("unreachable Device (index): %v", err)
	}
	if _, err := c.GetProperty(ctx, "root", "1p1"); err == nil {
		t.Error("unreachable Device (result) must fail")
	}
	if _, _, err := c.do(ctx, "bad method", "rolePaths/", nil); err == nil {
		t.Error("an unbuildable request must fail")
	}
}
