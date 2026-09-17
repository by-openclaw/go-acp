package is04

import (
	"encoding/json"
	"reflect"
	"testing"
)

// A PTP clock that is unlocked and untraceable must still emit every
// field clock_ptp.json requires. `omitempty` on the two booleans
// dropped them on re-encode, the Registry shipped that shape in a
// Query-WS grain, and AMWA IS-04-02 test_31 failed with
// "'ptp' is not one of ['internal']" — the anyOf validator matching
// neither clock branch. The false values are the interesting case, so
// that is what the test registers.
func TestNodeClockMarshalPTPKeepsRequiredFalse(t *testing.T) {
	in := []byte(`{"name":"clk1","ref_type":"ptp","traceable":false,` +
		`"version":"IEEE1588-2008","gmid":"08-00-11-ff-fe-21-e1-b0","locked":false}`)
	var c NodeClock
	if err := json.Unmarshal(in, &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got, want map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if err := json.Unmarshal(in, &want); err != nil {
		t.Fatalf("reparse want: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round-trip lost fields:\n got %v\nwant %v", got, want)
	}
	for _, k := range []string{"traceable", "version", "gmid", "locked"} {
		if _, ok := got[k]; !ok {
			t.Errorf("required clock_ptp.json field %q missing after re-encode", k)
		}
	}
}

// An internal clock emits exactly the two fields clock_internal.json
// names — the PTP branch's fields must not leak onto it.
func TestNodeClockMarshalInternalIsMinimal(t *testing.T) {
	c := NodeClock{Name: "clk0", RefType: "internal"}
	out, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if len(got) != 2 || got["name"] != "clk0" || got["ref_type"] != "internal" {
		t.Errorf("internal clock should be exactly {name, ref_type}, got %v", got)
	}
}
