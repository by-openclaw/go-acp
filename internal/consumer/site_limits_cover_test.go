package consumer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The edges of the site limits (ADR-0036, ADR-0037 P7): a nil set allows
// everything, every refusal of the loader names its line, a rule finds its
// path by exact match first and by the longest suffix after.
func TestSiteLimitsEdges(t *testing.T) {
	var none *SiteLimits
	if none.Len() != 0 || none.For("a.b") != nil || none.Check("a.b", "1", nil) != nil {
		t.Fatal("a nil set must allow everything")
	}
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for _, c := range []struct{ name, body, want string }{
		{"empty.csv", "", "no header"},
		{"nopath.csv", "min,max\n1,2\n", "no path column"},
		{"badmin.csv", "path,min,max,allowed\n.X,one,8,\n", `min "one" is not a number`},
		{"inverted.csv", "path,min,max,allowed\n.X,9,1,\n", "min 9 is above max 1"},
		{"nobound.csv", "path,min,max,allowed\n.X,,,\n", "has no min, max or allowed"},
		{"ragged.csv", "path,min,max,allowed\n\"unterminated,1,2,\n", "ragged.csv"},
	} {
		if _, err := LoadSiteLimits(write(c.name, c.body)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
	if _, err := LoadSiteLimits(filepath.Join(dir, "absent.csv")); err == nil {
		t.Error("an absent file loads")
	}
	// A BOM, semicolons, a blank path to skip, spaces around the allowed values.
	// A BOM, semicolons, a blank path to skip, a short row, spaces around
	// the allowed values.
	s, err := LoadSiteLimits(write("site.csv", "\xef\xbb\xbfpath;min;max;allowed\n;;;\n.A.B;;8;\n.B;1;;\nx.A.B;2;4;\n.P;;; 2 | 5 \n.S;1\n"))
	if err != nil || s.Len() != 5 {
		t.Fatalf("load: %v, %d rules", err, s.Len())
	}
	if r := s.For("x.A.B"); r == nil || r.Path != "x.A.B" {
		t.Errorf("exact path: %+v", r)
	}
	if r := s.For("y.A.B"); r == nil || r.Path != ".A.B" {
		t.Errorf("longest suffix: %+v", r)
	}
	if r := s.For("z.P"); r == nil || len(r.Allowed) != 2 || r.Allowed[0] != "2" || r.Allowed[1] != "5" {
		t.Errorf("allowed trimmed: %+v", r)
	}
	var v *ValidationError
	// A text where the rule wants a number.
	if err := s.Check("y.A.B", "eight", nil); !errors.As(err, &v) || !strings.Contains(err.Error(), "is not a number the site limit -..8 can bound") {
		t.Errorf("text against a range: %v", err)
	}
	// Above the max with no factory to name.
	if err := s.Check("y.A.B", "9", nil); !errors.As(err, &v) || !strings.Contains(err.Error(), "refused by the site limit (site -..8)") {
		t.Errorf("no factory: %v", err)
	}
	// Below the min of a min-only rule.
	if err := s.Check("y.B", "0", &Object{Min: 0, Max: 10}); !errors.As(err, &v) || !strings.Contains(err.Error(), "(site 1..-, factory 0..10)") {
		t.Errorf("min only: %v", err)
	}
	// Named values: the factory lists key=name items, the site a key.
	factory := &Object{EnumItems: []string{"2=Manual", "3=RTSP", "5=NMOS"}}
	if err := s.Check("q.P", "5", factory); err != nil {
		t.Errorf("an allowed key: %v", err)
	}
	if err := s.Check("q.P", "3", factory); !errors.As(err, &v) || !strings.Contains(err.Error(), "allowed 2|5") {
		t.Errorf("a key the site refuses: %v", err)
	}
	// A site value the factory does not list is a widening.
	if err := s.Check("q.P", "2", &Object{EnumItems: []string{"2=Manual"}}); !errors.As(err, &v) || !strings.Contains(err.Error(), "which the factory does not list") {
		t.Errorf("allowed beyond the factory: %v", err)
	}
}

// asFloat reads every numeric kind a schema may carry, and nothing else.
func TestAsFloat(t *testing.T) {
	for _, c := range []struct {
		in   any
		want float64
		ok   bool
	}{
		{1, 1, true}, {int32(2), 2, true}, {int64(3), 3, true}, {uint8(4), 4, true}, {uint16(5), 5, true},
		{uint32(6), 6, true}, {uint64(7), 7, true}, {float32(8.5), 8.5, true}, {9.25, 9.25, true},
		{"10", 10, true}, {"ten", 0, false}, {struct{}{}, 0, false}, {nil, 0, false},
	} {
		got, ok := asFloat(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("asFloat(%#v) = %v, %v", c.in, got, ok)
		}
	}
	if _, _, ok := factoryRange(&Object{Min: "1", Max: "x"}); ok {
		t.Error("half a range is no range")
	}
}

// A DM reference is empty until something names the model.
func TestDMRefIsZero(t *testing.T) {
	if !(DMRef{}).IsZero() || (DMRef{Model: "AES67"}).IsZero() || (DMRef{Path: "in.X"}).IsZero() {
		t.Fatal("IsZero")
	}
}

// The marker methods that make the error types dhs errors.
func TestErrorMarkers(t *testing.T) {
	(&TransportError{Op: "dial"}).dhsError()
	(&ValidationError{Field: "f", Reason: "r"}).dhsError()
}
