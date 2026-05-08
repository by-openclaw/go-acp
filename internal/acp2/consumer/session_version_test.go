package acp2

import "testing"

// TestAN2Version_FormatsMajorMinor pins the consumer-side display +
// public API for the AN2 GetVersion reply. Spec §3.3.1 returns
// `func_echo + major + minor`; real Neuron emits 1.0. Before #344
// the connect log dropped the major byte and printed only the minor
// (e.g. an2_version=0). AN2Version() now returns "major.minor".
func TestAN2Version_FormatsMajorMinor(t *testing.T) {
	tests := []struct {
		name  string
		major uint8
		minor uint8
		want  string
	}{
		{"real-Neuron-1.0", 1, 0, "1.0"},
		{"future-2.3", 2, 3, "2.3"},
		{"zero-zero", 0, 0, "0.0"},
		{"max", 0xFF, 0xFF, "255.255"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Session{an2VersionMajor: tt.major, an2VersionMinor: tt.minor}
			if got := s.AN2Version(); got != tt.want {
				t.Errorf("AN2Version() = %q, want %q", got, tt.want)
			}
		})
	}
}
