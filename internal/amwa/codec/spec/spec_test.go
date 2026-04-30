package spec

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

// fakeCodec is a minimal Versioned impl for testing the registry
// without dragging an actual NMOS spec package in.
type fakeCodec struct {
	specID    string
	apiVer    string
	specPatch string
}

func (f fakeCodec) SpecID() string    { return f.specID }
func (f fakeCodec) APIVer() string    { return f.apiVer }
func (f fakeCodec) SpecPatch() string { return f.specPatch }

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry[fakeCodec]()
	c11 := fakeCodec{"is-04", "v1.1", "v1.1.3"}
	c12 := fakeCodec{"is-04", "v1.2", "v1.2.2"}
	c13 := fakeCodec{"is-04", "v1.3", "v1.3.3"}
	r.Register(c11)
	r.Register(c12)
	r.Register(c13)

	if got, _ := r.Get("v1.2"); got != c12 {
		t.Fatalf("Get v1.2 = %v, want %v", got, c12)
	}
	if got, ok := r.Get("v9.9"); ok {
		t.Fatalf("Get v9.9 should miss, got %v", got)
	}
	if r.SpecID() != "is-04" {
		t.Fatalf("SpecID = %q", r.SpecID())
	}
}

func TestRegistryGetIsCaseInsensitive(t *testing.T) {
	r := NewRegistry[fakeCodec]()
	r.Register(fakeCodec{"is-04", "v1.3", "v1.3.3"})
	for _, k := range []string{"v1.3", "V1.3", "  v1.3  "} {
		if _, ok := r.Get(k); !ok {
			t.Fatalf("Get %q should hit", k)
		}
	}
}

func TestRegistryRegisterIdempotentSameInstance(t *testing.T) {
	r := NewRegistry[fakeCodec]()
	c := fakeCodec{"is-04", "v1.3", "v1.3.3"}
	r.Register(c)
	r.Register(c)
	r.Register(c)
	if got := r.SupportedVersions(); !reflect.DeepEqual(got, []string{"v1.3"}) {
		t.Fatalf("SupportedVersions = %v, want one entry", got)
	}
}

func TestRegistryRegisterPanicsOnDuplicateDifferentInstance(t *testing.T) {
	r := NewRegistry[fakeCodec]()
	r.Register(fakeCodec{"is-04", "v1.3", "v1.3.3"})

	defer func() {
		if rec := recover(); rec == nil {
			t.Fatalf("expected panic on duplicate registration")
		}
	}()
	r.Register(fakeCodec{"is-04", "v1.3", "v1.3.4"}) // different patch
}

func TestRegistryRegisterPanicsOnSpecIDMismatch(t *testing.T) {
	r := NewRegistry[fakeCodec]()
	r.Register(fakeCodec{"is-04", "v1.3", "v1.3.3"})

	defer func() {
		if rec := recover(); rec == nil {
			t.Fatalf("expected panic on SpecID mismatch")
		}
	}()
	r.Register(fakeCodec{"is-05", "v1.0", "v1.0.2"})
}

func TestRegistryRegisterPanicsOnEmptyFields(t *testing.T) {
	cases := []struct {
		name string
		c    fakeCodec
	}{
		{"empty SpecID", fakeCodec{"", "v1.3", "v1.3.3"}},
		{"empty APIVer", fakeCodec{"is-04", "", "v1.3.3"}},
		{"empty SpecPatch", fakeCodec{"is-04", "v1.3", ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRegistry[fakeCodec]()
			defer func() {
				if rec := recover(); rec == nil {
					t.Fatalf("expected panic for %s", tc.name)
				}
			}()
			r.Register(tc.c)
		})
	}
}

func TestRegistrySupportedVersionsSortedAscending(t *testing.T) {
	r := NewRegistry[fakeCodec]()
	// Register out of order to verify sort.
	r.Register(fakeCodec{"is-04", "v1.3", "v1.3.3"})
	r.Register(fakeCodec{"is-04", "v1.1", "v1.1.3"})
	r.Register(fakeCodec{"is-04", "v1.2", "v1.2.2"})
	want := []string{"v1.1", "v1.2", "v1.3"}
	if got := r.SupportedVersions(); !reflect.DeepEqual(got, want) {
		t.Fatalf("SupportedVersions = %v, want %v", got, want)
	}
}

func TestRegistryAllCodecsReturnsSortedCopy(t *testing.T) {
	r := NewRegistry[fakeCodec]()
	r.Register(fakeCodec{"is-04", "v1.3", "v1.3.3"})
	r.Register(fakeCodec{"is-04", "v1.1", "v1.1.3"})
	all := r.AllCodecs()
	if len(all) != 2 || all[0].APIVer() != "v1.1" || all[1].APIVer() != "v1.3" {
		t.Fatalf("AllCodecs = %v", all)
	}
	// Mutating the returned slice must not affect the registry.
	all[0] = fakeCodec{}
	if got, _ := r.Get("v1.1"); got.APIVer() != "v1.1" {
		t.Fatalf("Get v1.1 corrupted by AllCodecs caller")
	}
}

func TestSelectHighestMutualPicksMaxIntersection(t *testing.T) {
	r := NewRegistry[fakeCodec]()
	r.Register(fakeCodec{"is-04", "v1.1", "v1.1.3"})
	r.Register(fakeCodec{"is-04", "v1.2", "v1.2.2"})
	r.Register(fakeCodec{"is-04", "v1.3", "v1.3.3"})

	cases := []struct {
		peer []string
		want string
	}{
		{[]string{"v1.1", "v1.2", "v1.3"}, "v1.3"},
		{[]string{"v1.1", "v1.2"}, "v1.2"},
		{[]string{"v1.0", "v1.1"}, "v1.1"},
		{[]string{"v1.3"}, "v1.3"},
		{[]string{"V1.2  "}, "v1.2"}, // case + whitespace tolerated
	}
	for _, tc := range cases {
		got, err := SelectHighestMutual(r, tc.peer)
		if err != nil {
			t.Fatalf("peer=%v: unexpected err %v", tc.peer, err)
		}
		if got.APIVer() != tc.want {
			t.Fatalf("peer=%v: APIVer = %q, want %q", tc.peer, got.APIVer(), tc.want)
		}
	}
}

func TestSelectHighestMutualErrorsOnEmptyIntersection(t *testing.T) {
	r := NewRegistry[fakeCodec]()
	r.Register(fakeCodec{"is-04", "v1.3", "v1.3.3"})

	_, err := SelectHighestMutual(r, []string{"v2.0", "v2.1"})
	var nocommon ErrNoCommonVersion
	if !errors.As(err, &nocommon) {
		t.Fatalf("expected ErrNoCommonVersion, got %v", err)
	}
	if nocommon.SpecID != "is-04" {
		t.Fatalf("ErrNoCommonVersion.SpecID = %q", nocommon.SpecID)
	}
}

func TestSelectHighestMutualNilRegistry(t *testing.T) {
	_, err := SelectHighestMutual[fakeCodec](nil, []string{"v1.0"})
	if err == nil {
		t.Fatalf("nil Registry should error")
	}
}

func TestRegistryConcurrentRegisterAndRead(t *testing.T) {
	r := NewRegistry[fakeCodec]()
	r.Register(fakeCodec{"is-04", "v1.3", "v1.3.3"})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			// Idempotent re-register from many goroutines.
			r.Register(fakeCodec{"is-04", "v1.3", "v1.3.3"})
		}()
		go func() {
			defer wg.Done()
			_, _ = r.Get("v1.3")
			_ = r.SupportedVersions()
			_ = r.AllCodecs()
		}()
	}
	wg.Wait()
	if got := r.SupportedVersions(); !reflect.DeepEqual(got, []string{"v1.3"}) {
		t.Fatalf("SupportedVersions = %v after concurrent ops", got)
	}
}

func TestSeverityString(t *testing.T) {
	cases := []struct {
		s    Severity
		want string
	}{
		{SeverityInfo, "info"},
		{SeverityWarn, "warn"},
		{SeverityError, "error"},
		{Severity(99), "severity(99)"},
	}
	for _, tc := range cases {
		if got := tc.s.String(); got != tc.want {
			t.Fatalf("Severity(%d) = %q, want %q", tc.s, got, tc.want)
		}
	}
}

func TestNopReporterDiscardsEvents(t *testing.T) {
	var r Reporter = NopReporter{}
	r.Report(ComplianceEvent{Code: "x"}) // must not panic
}

func TestSliceReporterRecords(t *testing.T) {
	rep := &SliceReporter{}
	rep.Report(ComplianceEvent{Code: "a"})
	rep.Report(ComplianceEvent{Code: "b"})
	snap := rep.Snapshot()
	if len(snap) != 2 || snap[0].Code != "a" || snap[1].Code != "b" {
		t.Fatalf("Snapshot = %v", snap)
	}
}

func TestSliceReporterConcurrent(t *testing.T) {
	rep := &SliceReporter{}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rep.Report(ComplianceEvent{Code: "c"})
		}()
	}
	wg.Wait()
	if got := rep.Snapshot(); len(got) != 16 {
		t.Fatalf("expected 16 events, got %d", len(got))
	}
}

func TestCompareAPIVer(t *testing.T) {
	cases := []struct {
		a, b string
		sign int
	}{
		{"v1.0", "v1.1", -1},
		{"v1.1", "v1.0", 1},
		{"v1.3", "v1.3", 0},
		{"v2.0", "v1.9", 1},
		{"v1.10", "v1.9", 1}, // numeric, not lexicographic
		{"v1.x", "v1.y", -1}, // both invalid → lexicographic fallback
	}
	for _, tc := range cases {
		got := compareAPIVer(tc.a, tc.b)
		want := tc.sign
		if (got < 0 && want >= 0) || (got > 0 && want <= 0) || (got == 0 && want != 0) {
			t.Fatalf("compareAPIVer(%q,%q)=%d, want sign %d", tc.a, tc.b, got, want)
		}
	}
}
