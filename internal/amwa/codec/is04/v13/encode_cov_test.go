package v13

import (
	"errors"
	"strings"
	"testing"
)

// A marshaller that refuses must surface as an error, not as a payload
// a controller would try to parse. The IS-04 resource types cannot
// make json.Marshal fail, so the arm is proved through the seam.
func TestEncodeReportsAMarshalRefusal(t *testing.T) {
	old := marshal
	marshal = func(any) ([]byte, error) { return nil, errors.New("refused") }
	t.Cleanup(func() { marshal = old })

	_, err := (Codec{}).EncodeDevice(fixtureDevice())
	if err == nil || !strings.Contains(err.Error(), "marshal device") {
		t.Fatalf("want the marshal refusal reported, got %v", err)
	}
}
