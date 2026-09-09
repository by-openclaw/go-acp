package is04_test

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is04"
	v13 "dhs/internal/amwa/codec/is04/v13"
)

// A registration envelope names one of the six IS-04 resource types;
// anything else is refused rather than sent to a registry that would
// have to guess.
func TestEncodeRegistrationVersionedRefusesAnUnknownType(t *testing.T) {
	_, err := is04.EncodeRegistrationVersioned(v13.New(), is04.ResourceType("gizmo"), struct{}{})
	if err == nil || !strings.Contains(err.Error(), "invalid resource type") {
		t.Errorf("= %v, want the type refused", err)
	}
}
