package cerebrumnb

import (
	"strings"
	"testing"

	"dhs/internal/cerebrum-nb/codec"
	"dhs/internal/consumer"
)

// TestCanonicalDeviceObject pins the shared row → canonical-Object
// mapper (validate --out-tree + extract): path nesting, access bits,
// kind mapping, and the range-meta rule — a constraining MIN/MAX is
// recorded, a degenerate MIN==MAX (live: ENUMs report 0..0) is not.
func TestCanonicalDeviceObject(t *testing.T) {
	ov := &codec.DeviceObjectValue{
		Object: "A.Delay", Value: "5.5", DataType: "FLOAT",
		Available: true, Readable: true, Writable: true,
		Units: "ms", Min: "0", Max: "100",
	}
	obj := CanonicalDeviceObject("cvt 1", "1", ov, 3)
	if got := strings.Join(obj.Path, "/"); got != "cvt 1/1/A/Delay" {
		t.Fatalf("path = %q", got)
	}
	if obj.ID != 3 || obj.Label != "A.Delay" || obj.Unit != "ms" || obj.Access != 0x03 {
		t.Fatalf("identity fields = %+v", obj)
	}
	if obj.Kind != consumer.KindFloat || obj.Value.Float != 5.5 {
		t.Fatalf("value = %+v", obj.Value)
	}
	if obj.Meta["min"] != "0" || obj.Meta["max"] != "100" {
		t.Fatalf("range meta = %+v", obj.Meta)
	}

	deg := &codec.DeviceObjectValue{
		Object: "A.Mode", Value: "1", DataType: "ENUM",
		Available: true, Readable: true,
		Min: "0", Max: "0", EnumList: []string{"On", "Off"},
	}
	o2 := CanonicalDeviceObject("cvt 1", "1", deg, 0)
	if _, ok := o2.Meta["min"]; ok {
		t.Fatalf("degenerate 0..0 range must not be recorded: %+v", o2.Meta)
	}
	if len(o2.EnumItems) != 2 || o2.Kind != consumer.KindEnum {
		t.Fatalf("enum mapping = %+v", o2)
	}
}
