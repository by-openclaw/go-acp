package is04

import "testing"

// IS-04 v1.0 defines three formats; mux arrived with v1.1.
func TestIsFormatAtIS04(t *testing.T) {
	for _, tc := range []struct {
		format, ver string
		want        bool
	}{
		{FormatVideo, "v1.0", true}, {FormatAudio, "v1.0", true}, {FormatData, "v1.0", true},
		{FormatMux, "v1.0", false}, {FormatMux, "v1.1", true}, {FormatMux, "v1.2", true}, {FormatMux, "v1.3", true},
		{"urn:x-vendor:format:other", "v1.0", true},
	} {
		if got := IsFormatAtIS04(tc.format, tc.ver); got != tc.want {
			t.Errorf("IsFormatAtIS04(%s, %s) = %v, want %v", tc.format, tc.ver, got, tc.want)
		}
	}
}
