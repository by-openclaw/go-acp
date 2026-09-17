package emberplus

import (
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/emberplus/codec/glow"
)

func TestExtractFormatUnit(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"%0.1f°dB", "dB"},
		{"%d°%", "%"},
		{"%d°\nunits", "units"},
		{"%d", ""},
		{"", ""},
		{"° dB ", "dB"},
		{"%0.1f° dBu", "dBu"},
	}
	for _, c := range cases {
		if got := extractFormatUnit(c.in); got != c.want {
			t.Errorf("extractFormatUnit(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestApplyFactor(t *testing.T) {
	cases := []struct {
		name   string
		in     consumer.Value
		factor int64
		want   consumer.Value
	}{
		{
			name:   "int with factor 32 — Lawo metering centidB/32 → -128",
			in:     consumer.Value{Kind: consumer.KindInt, Int: -4096},
			factor: 32,
			want:   consumer.Value{Kind: consumer.KindFloat, Float: -128.0},
		},
		{
			name:   "int with factor 100 — generic centi-units",
			in:     consumer.Value{Kind: consumer.KindInt, Int: -1234},
			factor: 100,
			want:   consumer.Value{Kind: consumer.KindFloat, Float: -12.34},
		},
		{
			name:   "factor 1 — no scaling",
			in:     consumer.Value{Kind: consumer.KindInt, Int: 42},
			factor: 1,
			want:   consumer.Value{Kind: consumer.KindInt, Int: 42},
		},
		{
			name:   "factor 0 — no scaling",
			in:     consumer.Value{Kind: consumer.KindInt, Int: 42},
			factor: 0,
			want:   consumer.Value{Kind: consumer.KindInt, Int: 42},
		},
		{
			name:   "float input — scales as float",
			in:     consumer.Value{Kind: consumer.KindFloat, Float: 6.4},
			factor: 2,
			want:   consumer.Value{Kind: consumer.KindFloat, Float: 3.2},
		},
		{
			name:   "string input — unchanged",
			in:     consumer.Value{Kind: consumer.KindString, Str: "hello"},
			factor: 100,
			want:   consumer.Value{Kind: consumer.KindString, Str: "hello"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := applyFactor(c.in, c.factor)
			if got.Kind != c.want.Kind {
				t.Errorf("Kind = %v, want %v", got.Kind, c.want.Kind)
			}
			if got.Int != c.want.Int {
				t.Errorf("Int = %d, want %d", got.Int, c.want.Int)
			}
			if got.Float != c.want.Float {
				t.Errorf("Float = %g, want %g", got.Float, c.want.Float)
			}
			if got.Str != c.want.Str {
				t.Errorf("Str = %q, want %q", got.Str, c.want.Str)
			}
		})
	}
}

func TestDisplayValueAndUnit_MeteringParam(t *testing.T) {
	// Real Lawo metering Parameter: raw int with factor 32, no
	// format string (no unit).
	entry := &treeEntry{
		obj: consumer.Object{
			Value: consumer.Value{Kind: consumer.KindInt, Int: -4096},
			Unit:  "", // pre-scaled obj.Unit empty (format empty)
		},
		glowParam: &glow.Parameter{
			Factor: 32,
			Format: "",
		},
	}
	val, unit := displayValueAndUnit(entry)
	if val.Kind != consumer.KindFloat {
		t.Errorf("Kind = %v, want KindFloat after factor", val.Kind)
	}
	if val.Float != -128.0 {
		t.Errorf("Float = %g, want -128.0", val.Float)
	}
	if unit != "" {
		t.Errorf("Unit = %q, want empty (no format)", unit)
	}
}

func TestDisplayValueAndUnit_WithFormatUnit(t *testing.T) {
	// Parameter with format "°dB" — unit should be "dB", factor
	// applied if > 1.
	entry := &treeEntry{
		obj: consumer.Object{
			Value: consumer.Value{Kind: consumer.KindInt, Int: -1234},
		},
		glowParam: &glow.Parameter{
			Factor: 100,
			Format: "%0.2f°dB",
		},
	}
	val, unit := displayValueAndUnit(entry)
	if val.Kind != consumer.KindFloat || val.Float != -12.34 {
		t.Errorf("Value = {%v %g}, want {KindFloat -12.34}", val.Kind, val.Float)
	}
	if unit != "dB" {
		t.Errorf("Unit = %q, want dB", unit)
	}
}

func TestDisplayValueAndUnit_NoFactor_NoFormat(t *testing.T) {
	entry := &treeEntry{
		obj: consumer.Object{
			Value: consumer.Value{Kind: consumer.KindInt, Int: 5},
			Unit:  "",
		},
		glowParam: &glow.Parameter{
			Factor: 0,
			Format: "",
		},
	}
	val, unit := displayValueAndUnit(entry)
	if val.Kind != consumer.KindInt || val.Int != 5 {
		t.Errorf("Value = {%v %d}, want raw {KindInt 5}", val.Kind, val.Int)
	}
	if unit != "" {
		t.Errorf("Unit = %q, want empty", unit)
	}
}

func TestDisplayValueAndUnit_NoGlowParam_Fallback(t *testing.T) {
	entry := &treeEntry{
		obj: consumer.Object{
			Value: consumer.Value{Kind: consumer.KindString, Str: "Lawo"},
			Unit:  "fallback-unit",
		},
		glowParam: nil,
	}
	val, unit := displayValueAndUnit(entry)
	if val.Str != "Lawo" {
		t.Errorf("Value.Str = %q, want Lawo", val.Str)
	}
	if unit != "fallback-unit" {
		t.Errorf("Unit = %q, want fallback-unit", unit)
	}
}
