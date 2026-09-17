package main

import (
	"context"
	"strings"
	"testing"
)

// TestTSLValidateRouted pins that `validate` is a dispatched tsl verb (not the
// switch's "unknown verb" default) for every tsl version. It uses a nonexistent
// capture so the call fails on file-open inside runValidate — proving the
// dispatch reached the generic offline validator, which is the routing fix.
func TestTSLValidateRouted(t *testing.T) {
	for _, proto := range []string{"tsl-v31", "tsl-v40", "tsl-v50"} {
		t.Run(proto, func(t *testing.T) {
			err := runTSLConsumer(context.Background(), proto, []string{"validate", "does-not-exist-capture.jsonl"})
			if err == nil {
				t.Fatalf("%s validate on a missing file returned nil; expected a file-open error (routing still proven)", proto)
			}
			if strings.Contains(err.Error(), "unknown verb") {
				t.Fatalf("%s: validate is not routed: %v", proto, err)
			}
		})
	}
}
