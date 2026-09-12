package codec

import (
	"reflect"
	"testing"
)

func TestClassifyBody(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantKind NodeKind
		wantKids []string
	}{
		// Real shapes captured from BRIDGE@7.0.2.
		{"api root", `["self","misc","io","processing","matrix"]`, NodeBranch, []string{"self", "misc", "io", "processing", "matrix"}},
		{"io node", `["madi","ip","sdi"]`, NodeBranch, []string{"madi", "ip", "sdi"}},
		{"madi node", `["inputs","outputs"]`, NodeBranch, []string{"inputs", "outputs"}},
		{"processing node", `["audio","data","video"]`, NodeBranch, []string{"audio", "data", "video"}},
		{"sdi resource (array of objects)", `[{"name":"SDI Input 7","uuid":"8fd150f9-883f-421c-b568-808e5fbf9712"}]`, NodeResource, nil},
		{"self resource (object)", `{"app":{"productName":"BRIDGE","productVersion":"7.0.2"}}`, NodeResource, nil},
		// Edge cases.
		{"empty array is a resource", `[]`, NodeResource, nil},
		{"whitespace around branch", "  \n [\"a\",\"b\"]\n", NodeBranch, []string{"a", "b"}},
		{"single-name branch", `["ip"]`, NodeBranch, []string{"ip"}},
		{"array of numbers is a resource", `[1,2,3]`, NodeResource, nil},
		{"mixed array is a resource", `["a",{"x":1}]`, NodeResource, nil},
		{"scalar string is a resource", `"hello"`, NodeResource, nil},
		{"scalar number is a resource", `42`, NodeResource, nil},
		{"null is a resource", `null`, NodeResource, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, kids, err := ClassifyBody([]byte(tc.body))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if kind != tc.wantKind {
				t.Fatalf("kind = %v, want %v", kind, tc.wantKind)
			}
			if !reflect.DeepEqual(kids, tc.wantKids) {
				t.Fatalf("children = %v, want %v", kids, tc.wantKids)
			}
		})
	}
}

func TestClassifyBodyErrors(t *testing.T) {
	for _, body := range []string{"", "   ", "{not json", `[1,2`} {
		if _, _, err := ClassifyBody([]byte(body)); err == nil {
			t.Fatalf("ClassifyBody(%q) = nil error, want error", body)
		}
	}
}
