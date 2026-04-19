package emberplus

import (
	"errors"
	"testing"
)

func TestClassifyError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want Layer
	}{
		{"nil", nil, ""},
		{"s101 wrap", WrapS101("dial failed", errors.New("connection refused")), LayerS101},
		{"ber wrap", WrapBER("truncated tlv", nil), LayerBER},
		{"glow wrap", WrapGlow("unknown app tag", nil), LayerGlow},
		{"proto wrap", WrapProto("path not found", nil), LayerProto},
		{"plain error", errors.New("something else"), "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyError(tc.err); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestErrorsIs(t *testing.T) {
	inner := errors.New("inner cause")
	err := WrapS101("dial", inner)

	if !errors.Is(err, ErrS101) {
		t.Error("wrapped error should match ErrS101 via errors.Is")
	}
	if !errors.Is(err, inner) {
		t.Error("wrapped error should preserve inner cause for errors.Is")
	}
	if errors.Is(err, ErrBER) {
		t.Error("wrapped error should not match unrelated sentinels")
	}
}
