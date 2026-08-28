package is04

import (
	"encoding/json"
	"strings"
	"testing"
)

// A control registered with `"authorization": false` must re-encode
// with the field present — v1.3's control schema requires it, and
// omitempty on the bool is how a real Neuron's controls lost it in
// the Registry's Query API, blanking Cerebrum's IS-05 panels with no
// error anywhere. The false value is the interesting case.
func TestDeviceControlKeepsAuthorizationFalse(t *testing.T) {
	in := []byte(`{"href":"http://10.6.255.102:3000/x-nmos/connection/v1.1","type":"urn:x-nmos:control:sr-ctrl/v1.1","authorization":false}`)
	var c DeviceControl
	if err := json.Unmarshal(in, &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"authorization":false`) {
		t.Errorf("re-encode dropped required authorization:false: %s", out)
	}
}
