package main

import (
	"errors"
	"strings"
	"testing"

	"dhs/internal/cerebrum-nb/codec"
	"dhs/internal/consumer"
)

// The descriptors are the shapes the 2.9.3 server returns (§5.4.3): ranges
// as "0.000000" / "65536.000000", MIN=MAX on unbounded objects.
func TestCerebrumBoundValue(t *testing.T) {
	intRange := &codec.DeviceObjectValue{Object: "Port", DataType: "INTEGER", Min: "0.000000", Max: "65536.000000"}
	floatRange := &codec.DeviceObjectValue{Object: "Gain", DataType: "FLOAT", Min: "-12.500000", Max: "100.000000"}
	unbounded := &codec.DeviceObjectValue{Object: "Count", DataType: "INTEGER", Min: "0.000000", Max: "0.000000"}
	enum := &codec.DeviceObjectValue{Object: "Mode", DataType: "ENUM", EnumList: []string{"Off", "On", "Auto"}}
	str := &codec.DeviceObjectValue{Object: "UserLabel", DataType: "STRING"}
	cases := []struct {
		name    string
		ov      *codec.DeviceObjectValue
		want    string
		isEnum  bool
		value   string
		clamped bool
		refused string
	}{
		{"no descriptor: sent as is", nil, "999999", false, "999999", false, ""},
		{"integer inside the range", intRange, "8080", false, "8080", false, ""},
		{"integer at MAX", intRange, "65536", false, "65536", false, ""},
		{"integer above MAX: MAX", intRange, "70000", false, "65536", true, ""},
		{"integer below MIN: MIN", intRange, "-1", false, "0", true, ""},
		{"integer not whole", intRange, "1.5", false, "", false, "not a whole number"},
		{"integer not a number", intRange, "abc", false, "", false, "not a number"},
		{"float above MAX: MAX", floatRange, "101.25", false, "100", true, ""},
		{"float below MIN: MIN", floatRange, "-20", false, "-12.5", true, ""},
		{"float inside the range", floatRange, "-3.75", false, "-3.75", false, ""},
		{"MIN equals MAX: no range", unbounded, "123456", false, "123456", false, ""},
		{"enum index", enum, "2", false, "2", false, ""},
		{"enum index past the list", enum, "3", false, "", false, "not an index 0..2"},
		{"enum name", enum, "Auto", true, "Auto", false, ""},
		{"enum unknown name", enum, "Fast", true, "", false, "not one of Off,On,Auto"},
		{"string untouched", str, "", false, "", false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, err := cerebrumBoundValue(c.ov, c.want, c.isEnum)
			if c.refused != "" {
				var verr *consumer.ValidationError
				if !errors.As(err, &verr) || !strings.Contains(err.Error(), c.refused) {
					t.Fatalf("want a validation refusal %q, got %v", c.refused, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected refusal: %v", err)
			}
			if b.Value != c.value || (b.Clamped != "") != c.clamped {
				t.Fatalf("got value %q clamped %q, want %q clamped=%t", b.Value, b.Clamped, c.value, c.clamped)
			}
		})
	}
}

// A clamped write ends with a validation error (exit 2) naming the wanted
// value, the bound and what was written.
func TestCerebrumClampError(t *testing.T) {
	err := cerebrumClampError("Cerebrum.0.Port", "70000 above MAX 65536", "65536", false)
	var verr *consumer.ValidationError
	if !errors.As(err, &verr) || exitCode(err) != 2 {
		t.Fatalf("want a validation error, exit 2; got %v (exit %d)", err, exitCode(err))
	}
	if !strings.Contains(err.Error(), "70000 above MAX 65536: wrote 65536 instead") {
		t.Fatalf("message: %v", err)
	}
	if !strings.Contains(cerebrumClampError("x", "r", "1", true).Error(), "would write 1 instead") {
		t.Fatal("dry run must say would write")
	}
}
