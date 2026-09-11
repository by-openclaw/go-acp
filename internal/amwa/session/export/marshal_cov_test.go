package export

// What a capture does when the encoder refuses. Most of what this
// package writes is strings and integers that json cannot fail on, so
// the refusals are driven through the package's own marshal seams —
// the behaviour they guard is real, and a capture that writes a
// truncated file which looks complete is worse than one that stops.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

// refuseMarshalIndent makes the indenting encoder refuse the nth call
// and every one after it, so a test can pick which file fails.
func refuseMarshalIndent(t *testing.T, after int) {
	t.Helper()
	prev := marshalIndent
	left := after
	marshalIndent = func(v any, prefix, indent string) ([]byte, error) {
		if left == 0 {
			return nil, errors.New("refused")
		}
		left--
		return prev(v, prefix, indent)
	}
	t.Cleanup(func() { marshalIndent = prev })
}

// device.json is the first thing a capture writes, so an encoder that
// refuses it stops the run: a capture folder with no device.json is
// not attributable to any device.
func TestDeviceFileThatCannotBeEncodedStopsTheCapture(t *testing.T) {
	srv := httptest.NewServer(anonymousDevice(t))
	defer srv.Close()

	refuseMarshalIndent(t, 0)
	if _, err := Run(context.Background(), baseOpts(t, hostOf(srv))); err == nil ||
		!strings.Contains(err.Error(), "refused") {
		t.Fatalf("Run = %v, want the encoder's refusal", err)
	}
}

// manifest.json is written last, from the finished tree. It is the
// front page of the capture — which devices answered, which did not —
// so a capture without one is not a capture either.
func TestManifestThatCannotBeEncodedStopsTheCapture(t *testing.T) {
	srv := httptest.NewServer(anonymousDevice(t))
	defer srv.Close()

	// device.json, device.json again with its role, tree.json, then
	// the manifest.
	refuseMarshalIndent(t, 3)
	_, err := Run(context.Background(), baseOpts(t, hostOf(srv)))
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("Run = %v, want the encoder's refusal", err)
	}
}

// A collection the encoder will not re-serialise is dropped rather
// than written half-formed: the pages are already recorded in
// report.txt, so the loss is visible, and a raw file holding a broken
// array would be read back as truth.
func TestAPageThatCannotBeReEncodedIsDropped(t *testing.T) {
	p := newPlant(t)
	p.paths["/x-nmos"] = []string{"query/"}
	p.paths["/x-nmos/query/"] = []string{"v1.3/"}
	p.paths["/x-nmos/query/v1.3/"] = []string{"nodes/"}
	p.paths["/x-nmos/query/v1.3/nodes"] = []any{map[string]any{"id": "n1"}}
	srv := httptest.NewServer(p)
	defer srv.Close()

	prev := marshalJSON
	marshalJSON = func(v any) ([]byte, error) {
		if _, ok := v.([]json.RawMessage); ok {
			return nil, errors.New("refused")
		}
		return prev(v)
	}
	t.Cleanup(func() { marshalJSON = prev })

	res, err := Run(context.Background(), baseOpts(t, hostOf(srv)))
	if err != nil {
		t.Fatalf("a dropped collection must not stop the capture: %v", err)
	}
	if res == nil {
		t.Fatal("Run returned no result")
	}
}
