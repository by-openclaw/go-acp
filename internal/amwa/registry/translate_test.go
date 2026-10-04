package registry

// The keys removed are the ones IS-04 v1.3.3 docs/Upgrade Path lists
// under "Version Translations", minor by minor; everything else of the
// document stays, a vendor's key included.

import (
	"encoding/json"
	"reflect"
	"testing"

	"dhs/internal/amwa/codec/is04"
)

func same(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("the translation is not JSON: %v\n%s", err, got)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("the expectation is not JSON: %v", err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("translated to\n  %s\nwant\n  %s", got, want)
	}
}

func TestTranslateDownRemovesWhatEachMinorIntroduced(t *testing.T) {
	const node = `{"id":"n1","version":"1:2","label":"n","description":"d","tags":{},"href":"http://h/",
	  "hostname":"h","caps":{},"vendor_x":{"keep":1},
	  "api":{"versions":["v1.3"],"endpoints":[{"host":"h","port":80,"protocol":"http","authorization":false}]},
	  "services":[{"href":"http://h/s","type":"urn:x","authorization":true}],
	  "clocks":[{"name":"clk0","ref_type":"internal"}],
	  "interfaces":[{"name":"eth0","chassis_id":null,"port_id":"aa-bb-cc-dd-ee-ff","attached_network_device":{"chassis_id":"x","port_id":"y"}}]}`

	for _, tc := range []struct {
		name     string
		t        is04.ResourceType
		doc      string
		from, to string
		want     string
	}{
		{"a node at v1.2: the three v1.3 keys go, wherever they sit", is04.ResourceNode, node, "v1.3", "v1.2",
			`{"id":"n1","version":"1:2","label":"n","description":"d","tags":{},"href":"http://h/","hostname":"h","caps":{},"vendor_x":{"keep":1},
			  "api":{"versions":["v1.3"],"endpoints":[{"host":"h","port":80,"protocol":"http"}]},
			  "services":[{"href":"http://h/s","type":"urn:x"}],
			  "clocks":[{"name":"clk0","ref_type":"internal"}],
			  "interfaces":[{"name":"eth0","chassis_id":null,"port_id":"aa-bb-cc-dd-ee-ff"}]}`},
		{"a node at v1.1: interfaces go too", is04.ResourceNode, node, "v1.3", "v1.1",
			`{"id":"n1","version":"1:2","label":"n","description":"d","tags":{},"href":"http://h/","hostname":"h","caps":{},"vendor_x":{"keep":1},
			  "api":{"versions":["v1.3"],"endpoints":[{"host":"h","port":80,"protocol":"http"}]},
			  "services":[{"href":"http://h/s","type":"urn:x"}],
			  "clocks":[{"name":"clk0","ref_type":"internal"}]}`},
		{"a node at v1.0: api, clocks, description and tags go", is04.ResourceNode, node, "v1.3", "v1.0",
			`{"id":"n1","version":"1:2","label":"n","href":"http://h/","hostname":"h","caps":{},"vendor_x":{"keep":1},
			  "services":[{"href":"http://h/s","type":"urn:x"}]}`},
		{"a v1.2 node at v1.1: only what v1.2 introduced", is04.ResourceNode, node, "v1.2", "v1.1",
			`{"id":"n1","version":"1:2","label":"n","description":"d","tags":{},"href":"http://h/","hostname":"h","caps":{},"vendor_x":{"keep":1},
			  "api":{"versions":["v1.3"],"endpoints":[{"host":"h","port":80,"protocol":"http","authorization":false}]},
			  "services":[{"href":"http://h/s","type":"urn:x","authorization":true}],
			  "clocks":[{"name":"clk0","ref_type":"internal"}]}`},
		{"a device", is04.ResourceDevice,
			`{"id":"d1","description":"d","tags":{},"controls":[{"href":"http://h/c","type":"urn:c","authorization":false}],"senders":[]}`, "v1.3", "v1.2",
			`{"id":"d1","description":"d","tags":{},"controls":[{"href":"http://h/c","type":"urn:c"}],"senders":[]}`},
		{"a device at v1.0", is04.ResourceDevice,
			`{"id":"d1","description":"d","tags":{},"controls":[{"href":"http://h/c","type":"urn:c","authorization":false}],"senders":[]}`, "v1.3", "v1.0",
			`{"id":"d1","senders":[]}`},
		{"a source", is04.ResourceSource,
			`{"id":"s1","event_type":"boolean","channels":[{"label":""}],"clock_name":"clk0","grain_rate":{"numerator":25},"format":"urn:x-nmos:format:data"}`, "v1.3", "v1.0",
			`{"id":"s1","format":"urn:x-nmos:format:data"}`},
		{"a flow", is04.ResourceFlow,
			`{"id":"f1","event_type":"boolean","device_id":"d1","media_type":"video/raw","frame_width":1920,"DID_SDID":[],"source_id":"s1","grain_rate":{"numerator":30000,"denominator":1001}}`, "v1.3", "v1.0",
			`{"id":"f1","source_id":"s1"}`},
		{"a sender", is04.ResourceSender,
			`{"id":"x1","caps":{},"interface_bindings":["eth0"],"subscription":{"receiver_id":null,"active":false},"flow_id":"f1"}`, "v1.3", "v1.1",
			`{"id":"x1","flow_id":"f1"}`},
		{"a receiver: subscription stays, its active goes", is04.ResourceReceiver,
			`{"id":"r1","interface_bindings":["eth0"],"subscription":{"sender_id":null,"active":false},"caps":{}}`, "v1.3", "v1.1",
			`{"id":"r1","subscription":{"sender_id":null},"caps":{}}`},
		{"a receiver at v1.2: nothing v1.3 added", is04.ResourceReceiver,
			`{"id":"r1","interface_bindings":["eth0"],"subscription":{"sender_id":null,"active":false}}`, "v1.3", "v1.2",
			`{"id":"r1","interface_bindings":["eth0"],"subscription":{"sender_id":null,"active":false}}`},
		{"keys of another shape than the path expects are left", is04.ResourceNode,
			`{"id":"n1","api":"oops","services":"oops","interfaces":[5,{"name":"eth0","attached_network_device":{}}]}`, "v1.3", "v1.2",
			`{"id":"n1","api":"oops","services":"oops","interfaces":[5,{"name":"eth0"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			same(t, translateDown(tc.t, json.RawMessage(tc.doc), tc.from, tc.to), tc.want)
		})
	}
}

// A number comes out as it went in, and what is not an object is not
// touched.
func TestTranslateDownLeavesWhatItDoesNotUnderstand(t *testing.T) {
	big := translateDown(is04.ResourceFlow, json.RawMessage(`{"id":"f1","bit_rate":12345678901234567890,"event_type":"x"}`), "v1.3", "v1.2")
	if string(big) != `{"bit_rate":12345678901234567890,"id":"f1"}` {
		t.Errorf("a large number was rewritten: %s", big)
	}
	for _, doc := range []string{`[1,2]`, `not json`} {
		if got := translateDown(is04.ResourceNode, json.RawMessage(doc), "v1.3", "v1.0"); string(got) != doc {
			t.Errorf("%s was translated to %s", doc, got)
		}
	}
}
