package provider

// IS-14 Configuration API provider tests. Expected wire shapes come
// from the AMWA v1.0.0 RAML + published examples (rolePaths dotted
// from "root", {level}p{index} / {level}m{index} ids, NcMethodResult*
// envelopes), not from working code.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	stdhttp "net/http"
	"strings"
	"testing"
	"time"

	_ "dhs/internal/amwa/codec/is04/v13"
	_ "dhs/internal/amwa/codec/is05/v12"
	_ "dhs/internal/amwa/codec/is14/v10"
)

// serveConfigNode starts a full Node server (static discovery) and
// returns its base address.
func serveConfigNode(t *testing.T) string {
	t.Helper()
	addr := freeAddr(t)
	s, err := NewIS04NodeServer(nil, validBundle(), IS04NodeConfig{
		Bind:          addr,
		DiscoveryMode: "static",
	})
	if err != nil {
		t.Fatalf("NewIS04NodeServer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); _ = s.Stop() })
	go func() { _ = s.Serve(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := stdhttp.Get("http://" + addr + "/x-nmos/configuration/")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				return addr
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("config node did not come up")
	return ""
}

func doJSON(t *testing.T, method, url string, body string) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := stdhttp.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func TestConfigurationTreeAndRolePaths(t *testing.T) {
	addr := serveConfigNode(t)
	base := "http://" + addr

	// The API tree must list configuration/ (served-but-unlisted is
	// absent) and the version index must resolve.
	st, raw := doJSON(t, "GET", base+"/x-nmos/", "")
	if st != 200 || !strings.Contains(string(raw), `"configuration/"`) {
		t.Fatalf("/x-nmos/ = %d %s", st, raw)
	}
	st, raw = doJSON(t, "GET", base+"/x-nmos/configuration/v1.0/", "")
	if st != 200 || !strings.Contains(string(raw), `"rolePaths/"`) {
		t.Fatalf("version index = %d %s", st, raw)
	}

	st, raw = doJSON(t, "GET", base+"/x-nmos/configuration/v1.0/rolePaths/", "")
	if st != 200 {
		t.Fatalf("rolePaths = %d %s", st, raw)
	}
	var paths []string
	if err := json.Unmarshal(raw, &paths); err != nil {
		t.Fatalf("rolePaths decode: %v", err)
	}
	want := []string{"root/", "root.DeviceManager/", "root.ClassManager/", "root.BulkPropertiesManager/", "root.GainControl/"}
	if len(paths) != len(want) {
		t.Fatalf("rolePaths = %v, want %v", paths, want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Errorf("rolePaths[%d] = %q, want %q", i, paths[i], want[i])
		}
	}

	// Unknown role path: 404 with the ms05-error body.
	st, raw = doJSON(t, "GET", base+"/x-nmos/configuration/v1.0/rolePaths/nope/", "")
	if st != 404 || !bytes.Contains(raw, []byte(`"status":404`)) {
		t.Errorf("unknown role path = %d %s, want 404 NcMethodResultError", st, raw)
	}

	// Role path index per rolePath-get-200.json.
	st, raw = doJSON(t, "GET", base+"/x-nmos/configuration/v1.0/rolePaths/root/", "")
	if st != 200 || !strings.Contains(string(raw), `"bulkProperties/"`) || !strings.Contains(string(raw), `"descriptor/"`) {
		t.Errorf("role path index = %d %s", st, raw)
	}
}

func TestConfigurationDescriptorAndProperties(t *testing.T) {
	addr := serveConfigNode(t)
	rp := "http://" + addr + "/x-nmos/configuration/v1.0/rolePaths/root"

	// Class descriptor: NcBlock with own + inherited elements.
	st, raw := doJSON(t, "GET", rp+"/descriptor/", "")
	if st != 200 {
		t.Fatalf("descriptor = %d %s", st, raw)
	}
	var desc struct {
		Status int `json:"status"`
		Value  struct {
			Name       string  `json:"name"`
			ClassID    []int32 `json:"classId"`
			Properties []struct {
				Name string `json:"name"`
			} `json:"properties"`
			Methods []any `json:"methods"`
		} `json:"value"`
	}
	if err := json.Unmarshal(raw, &desc); err != nil {
		t.Fatalf("descriptor decode: %v", err)
	}
	if desc.Value.Name != "NcBlock" || len(desc.Value.ClassID) != 2 {
		t.Errorf("descriptor = %+v", desc.Value.Name)
	}
	if len(desc.Value.Methods) != 11 { // 2m1..2m4 + 1m1..1m7
		t.Errorf("flattened methods = %d, want 11", len(desc.Value.Methods))
	}

	// Property list carries own + inherited ids.
	st, raw = doJSON(t, "GET", rp+"/properties/", "")
	if st != 200 || !strings.Contains(string(raw), `"1p6/"`) || !strings.Contains(string(raw), `"2p1/"`) {
		t.Fatalf("properties = %d %s", st, raw)
	}

	// userLabel round trip: read, write, read back.
	st, raw = doJSON(t, "GET", rp+"/properties/1p6/value/", "")
	if st != 200 || !bytes.Contains(raw, []byte(`"status":200`)) {
		t.Fatalf("1p6 get = %d %s", st, raw)
	}
	st, raw = doJSON(t, "PUT", rp+"/properties/1p6/value/", `{"value":"renamed by test"}`)
	if st != 200 || !bytes.Contains(raw, []byte(`"status":200`)) {
		t.Fatalf("1p6 put = %d %s", st, raw)
	}
	st, raw = doJSON(t, "GET", rp+"/properties/1p6/value/", "")
	if st != 200 || !strings.Contains(string(raw), "renamed by test") {
		t.Errorf("1p6 after put = %d %s", st, raw)
	}

	// Readonly write refused with NcMethodStatus 405 in the body.
	st, raw = doJSON(t, "PUT", rp+"/properties/1p1/value/", `{"value":[9,9]}`)
	if st != 400 || !bytes.Contains(raw, []byte(`"status":405`)) {
		t.Errorf("readonly put = %d %s, want 400 + body status 405", st, raw)
	}

	// Unknown property is a 404.
	st, raw = doJSON(t, "GET", rp+"/properties/9p9/value/", "")
	if st != 404 {
		t.Errorf("unknown property = %d %s", st, raw)
	}

	// Datatype descriptor of a struct property includes fields.
	st, raw = doJSON(t, "GET", rp+"/properties/2p2/descriptor/", "")
	if st != 200 || !strings.Contains(string(raw), `"fields"`) || !strings.Contains(string(raw), "NcBlockMemberDescriptor") {
		t.Errorf("2p2 datatype descriptor = %d %s", st, raw)
	}
}

func TestConfigurationMethods(t *testing.T) {
	addr := serveConfigNode(t)
	v1 := "http://" + addr + "/x-nmos/configuration/v1.0/rolePaths/"

	// Method list per methods-base-get-200.json.
	st, raw := doJSON(t, "GET", v1+"root/methods/", "")
	if st != 200 || !strings.Contains(string(raw), `"1m1/"`) || !strings.Contains(string(raw), `"2m1/"`) {
		t.Fatalf("methods = %d %s", st, raw)
	}

	// 1m1 Get(userLabel).
	st, raw = doJSON(t, "PATCH", v1+"root/methods/1m1/", `{"arguments":{"id":{"level":1,"index":6}}}`)
	if st != 200 || !bytes.Contains(raw, []byte(`"status":200`)) {
		t.Fatalf("1m1 = %d %s", st, raw)
	}

	// 1m2 Set(userLabel) then read back via property value.
	st, raw = doJSON(t, "PATCH", v1+"root/methods/1m2/", `{"arguments":{"id":{"level":1,"index":6},"value":"set by method"}}`)
	if st != 200 || !bytes.Contains(raw, []byte(`"status":200`)) {
		t.Fatalf("1m2 = %d %s", st, raw)
	}
	st, raw = doJSON(t, "GET", v1+"root/properties/1p6/value/", "")
	if !strings.Contains(string(raw), "set by method") {
		t.Errorf("1m2 did not stick: %d %s", st, raw)
	}

	// 2m1 GetMemberDescriptors: the three managers.
	st, raw = doJSON(t, "PATCH", v1+"root/methods/2m1/", `{"arguments":{"recurse":false}}`)
	if st != 200 || !strings.Contains(string(raw), "DeviceManager") || !strings.Contains(string(raw), "ClassManager") ||
		!strings.Contains(string(raw), "BulkPropertiesManager") {
		t.Fatalf("2m1 = %d %s", st, raw)
	}

	// 1m7 GetSequenceLength on members (3 managers + gain worker).
	st, raw = doJSON(t, "PATCH", v1+"root/methods/1m7/", `{"arguments":{"id":{"level":2,"index":2}}}`)
	if st != 200 || !bytes.Contains(raw, []byte(`"value":4`)) {
		t.Errorf("1m7 = %d %s", st, raw)
	}

	// Bulk properties manager 3m1 GetPropertiesByPath — the method
	// face of the REST backup.
	st, raw = doJSON(t, "PATCH", v1+"root.BulkPropertiesManager/methods/3m1/",
		`{"arguments":{"path":["root","DeviceManager"],"recurse":false,"includeDescriptors":false}}`)
	if st != 200 || !strings.Contains(string(raw), `"validationFingerprint"`) {
		t.Fatalf("3m1 GetPropertiesByPath = %d %s", st, raw)
	}

	// ClassManager 3m1 GetControlClass.
	st, raw = doJSON(t, "PATCH", v1+"root.ClassManager/methods/3m1/", `{"arguments":{"classId":[1,1],"includeInherited":true}}`)
	if st != 200 || !strings.Contains(string(raw), `"NcBlock"`) {
		t.Fatalf("3m1 = %d %s", st, raw)
	}

	// ClassManager 3m2 GetDatatype.
	st, raw = doJSON(t, "PATCH", v1+"root.ClassManager/methods/3m2/", `{"arguments":{"name":"NcBlockMemberDescriptor","includeInherited":true}}`)
	if st != 200 || !strings.Contains(string(raw), `"fields"`) {
		t.Fatalf("3m2 = %d %s", st, raw)
	}

	// Unknown method id: 404 ms05-error.
	st, raw = doJSON(t, "PATCH", v1+"root/methods/9m9/", `{"arguments":{}}`)
	if st != 404 || !bytes.Contains(raw, []byte(`"status":501`)) {
		t.Errorf("unknown method = %d %s", st, raw)
	}

	// Malformed body: 400.
	st, _ = doJSON(t, "PATCH", v1+"root/methods/1m1/", `{}`)
	if st != 400 {
		t.Errorf("missing arguments = %d, want 400", st)
	}
}

func TestConfigurationBulkProperties(t *testing.T) {
	addr := serveConfigNode(t)
	v1 := "http://" + addr + "/x-nmos/configuration/v1.0/rolePaths/"

	// Full backup: root + both managers, with descriptors + fingerprint.
	st, raw := doJSON(t, "GET", v1+"root/bulkProperties/", "")
	if st != 200 {
		t.Fatalf("backup = %d", st)
	}
	var backup struct {
		Status int `json:"status"`
		Value  struct {
			ValidationFingerprint *string `json:"validationFingerprint"`
			Values                []struct {
				Path   []string `json:"path"`
				Values []struct {
					Descriptor *json.RawMessage `json:"descriptor"`
				} `json:"values"`
				IsRebuildable bool `json:"isRebuildable"`
			} `json:"values"`
		} `json:"value"`
	}
	if err := json.Unmarshal(raw, &backup); err != nil {
		t.Fatalf("backup decode: %v", err)
	}
	if backup.Value.ValidationFingerprint == nil || len(backup.Value.Values) != 5 {
		t.Fatalf("backup holders = %d fp=%v, want 5 + fingerprint", len(backup.Value.Values), backup.Value.ValidationFingerprint)
	}
	if backup.Value.Values[0].Values[0].Descriptor == nil {
		t.Error("includeDescriptors default must attach descriptors")
	}

	// holderPaths decodes just the role paths of a backup response.
	holderPaths := func(raw []byte) []string {
		var b struct {
			Value struct {
				Values []struct {
					Path   []string `json:"path"`
					Values []struct {
						Descriptor *json.RawMessage `json:"descriptor"`
					} `json:"values"`
				} `json:"values"`
			} `json:"value"`
		}
		if err := json.Unmarshal(raw, &b); err != nil {
			t.Fatalf("backup decode: %v", err)
		}
		out := []string{}
		for _, h := range b.Value.Values {
			out = append(out, strings.Join(h.Path, "."))
			for _, ph := range h.Values {
				if ph.Descriptor != nil && string(*ph.Descriptor) != "null" {
					t.Errorf("includeDescriptors=false left a descriptor on %v", h.Path)
				}
			}
		}
		return out
	}

	// includeDescriptors=false: descriptors null, ClassManager role
	// path omitted (API requests doc).
	st, raw = doJSON(t, "GET", v1+"root/bulkProperties/?includeDescriptors=false", "")
	if st != 200 {
		t.Fatalf("backup nodesc = %d", st)
	}
	paths := holderPaths(raw)
	if len(paths) != 4 || paths[0] != "root" || paths[1] != "root.DeviceManager" ||
		paths[2] != "root.BulkPropertiesManager" || paths[3] != "root.GainControl" {
		t.Errorf("includeDescriptors=false holders = %v, want [root root.DeviceManager root.BulkPropertiesManager root.GainControl]", paths)
	}

	// recurse=false: root only.
	st, raw = doJSON(t, "GET", v1+"root/bulkProperties/?recurse=false", "")
	if st != 200 {
		t.Fatalf("backup norecurse = %d", st)
	}
	var norec struct {
		Value struct {
			Values []struct {
				Path []string `json:"path"`
			} `json:"values"`
		} `json:"value"`
	}
	if err := json.Unmarshal(raw, &norec); err != nil {
		t.Fatalf("norecurse decode: %v", err)
	}
	if len(norec.Value.Values) != 1 || strings.Join(norec.Value.Values[0].Path, ".") != "root" {
		t.Errorf("recurse=false must scope to the target, got %+v", norec.Value.Values)
	}

	// Validate (PATCH): a data set touching userLabel + a readonly
	// property — Ok with a warning notice, and NO state change.
	dataSet := fmt.Sprintf(`{"arguments":{"dataSet":{"validationFingerprint":null,"values":[{"path":["root"],"dependencyPaths":[],"allowedMembersClasses":[],"values":[{"id":{"level":1,"index":6},"descriptor":null,"value":"bulk label"},{"id":{"level":2,"index":1},"descriptor":null,"value":false}],"isRebuildable":false}]},"recurse":true,"restoreMode":%d}}`, 0)
	st, raw = doJSON(t, "PATCH", v1+"root/bulkProperties/", dataSet)
	if st != 200 || !bytes.Contains(raw, []byte(`"noticeType":300`)) || !bytes.Contains(raw, []byte(`"status":200`)) {
		t.Fatalf("validate = %d %s", st, raw)
	}
	_, raw = doJSON(t, "GET", v1+"root/properties/1p6/value/", "")
	if strings.Contains(string(raw), "bulk label") {
		t.Error("PATCH (validate) must not change the device model")
	}

	// Restore (PUT): same data set applies the writable property.
	st, raw = doJSON(t, "PUT", v1+"root/bulkProperties/", dataSet)
	if st != 200 || !bytes.Contains(raw, []byte(`"noticeType":300`)) {
		t.Fatalf("restore = %d %s", st, raw)
	}
	st, raw = doJSON(t, "GET", v1+"root/properties/1p6/value/", "")
	if !strings.Contains(string(raw), "bulk label") {
		t.Errorf("restore did not apply: %d %s", st, raw)
	}

	// Unknown path in the data set: per-entry 404 verdict, HTTP 200.
	badSet := `{"arguments":{"dataSet":{"validationFingerprint":null,"values":[{"path":["root","nope"],"dependencyPaths":[],"allowedMembersClasses":[],"values":[],"isRebuildable":false}]},"recurse":true,"restoreMode":0}}`
	st, raw = doJSON(t, "PATCH", v1+"root/bulkProperties/", badSet)
	if st != 200 || !bytes.Contains(raw, []byte(`"status":404`)) {
		t.Errorf("unknown path verdict = %d %s", st, raw)
	}

	// Bad request shape: HTTP 400 ms05-error.
	st, _ = doJSON(t, "PUT", v1+"root/bulkProperties/", `{"arguments":{}}`)
	if st != 400 {
		t.Errorf("bad restore body = %d, want 400", st)
	}
}

func TestConfigurationControlAdvertised(t *testing.T) {
	addr := serveConfigNode(t)
	st, raw := doJSON(t, "GET", "http://"+addr+"/x-nmos/node/v1.3/devices", "")
	if st != 200 {
		t.Fatalf("devices = %d", st)
	}
	if !strings.Contains(string(raw), "urn:x-nmos:control:configuration/v1.0") {
		t.Error("devices must advertise the Configuration API control")
	}
	if !strings.Contains(string(raw), "/x-nmos/configuration/v1.0/") {
		t.Error("control href must point at the configuration tree")
	}
}
