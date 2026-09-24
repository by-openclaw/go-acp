package consumer

import (
	"strings"
	"testing"

	dhsc "dhs/internal/consumer"
)

// The pure half of building a model: what a JSON body becomes, and
// what it must never become.

func TestFlattenGivesOneObjectPerLeaf(t *testing.T) {
	body := []byte(`{"ptp":{"domain":77,"locked":true,"offset":-0.5},"name":"ref"}`)
	objs := flatten("/misc/reference", body, true)

	got := map[string]dhsc.Value{}
	for _, o := range objs {
		got[strings.Join(o.Path, ".")] = o.Value
		if o.Access&accessWrite == 0 {
			t.Errorf("%v should be writable", o.Path)
		}
	}
	if len(got) != 4 {
		t.Fatalf("objects = %v", got)
	}
	if v := got["misc.reference.ptp.domain"]; v.Kind != dhsc.KindInt || v.Int != 77 {
		t.Errorf("domain = %+v — a whole number is an integer, not a float", v)
	}
	if v := got["misc.reference.ptp.locked"]; v.Kind != dhsc.KindBool || !v.Bool {
		t.Errorf("locked = %+v", v)
	}
	if v := got["misc.reference.ptp.offset"]; v.Kind != dhsc.KindFloat || v.Float != -0.5 {
		t.Errorf("offset = %+v", v)
	}
	if v := got["misc.reference.name"]; v.Kind != dhsc.KindString || v.Str != "ref" {
		t.Errorf("name = %+v", v)
	}
}

func TestAnArrayElementIsAddressedByItsUUIDWhenItHasOne(t *testing.T) {
	// An index moves when the device reorders; a uuid does not. An
	// operator's saved path, an alarm rule and a DM diff all break on
	// the first reorder if the index is the address.
	body := []byte(`[{"uuid":"s-2","name":"b"},{"uuid":"s-1","name":"a"}]`)
	objs := flatten("/io/ip/senders/video", body, false)

	paths := map[string]bool{}
	for _, o := range objs {
		paths[strings.Join(o.Path, ".")] = true
		if o.Access&accessWrite != 0 {
			t.Errorf("%v must be read-only", o.Path)
		}
	}
	for _, want := range []string{"io.ip.senders.video.s-1.name", "io.ip.senders.video.s-2.name"} {
		if !paths[want] {
			t.Errorf("missing %s (got %v)", want, paths)
		}
	}

	// Without a uuid there is nothing better than the index.
	idx := flatten("/misc/list", []byte(`["a","b"]`), false)
	if len(idx) != 2 || strings.Join(idx[0].Path, ".") != "misc.list.0" {
		t.Errorf("indexed = %v", idx)
	}
}

func TestANonJSONBodyIsStillARealResource(t *testing.T) {
	// The device answers /docs/api.yml with YAML, and a firmware could
	// answer anything with anything. Dropping it would hide a resource
	// that exists.
	objs := flatten("/misc/raw", []byte("not json at all"), false)
	if len(objs) != 1 || objs[0].Value.Str != "not json at all" {
		t.Errorf("objects = %+v", objs)
	}
}

func TestANullFieldIsRecordedRatherThanDropped(t *testing.T) {
	// A field the device declares and has no value for is information:
	// its absence should be visible, not inferred from a gap.
	objs := flatten("/misc/x", []byte(`{"unset":null}`), false)
	if len(objs) != 1 || objs[0].Value.Kind != dhsc.KindString || objs[0].Value.Str != "" {
		t.Errorf("objects = %+v", objs)
	}
}

func TestSummariseKeepsAModelReadable(t *testing.T) {
	// The real device puts a JPEG in every video channel.
	long := strings.Repeat("a", maxValueLen+10)
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"short text is itself", "Main", "Main"},
		{"binary is described", "\xff\xd8\xff\xe0JFIF\x00", "<binary, 9 bytes>"},
		{"a control byte is binary", "ok\x00then", "<binary, 7 bytes>"},
		{"long text is cut, with its length", long, long[:maxValueLen] + "… <266 bytes total>"},
		{"tabs and newlines are text", "a\tb\nc", "a\tb\nc"},
	}
	for _, c := range cases {
		if got := summarise(c.in); got != c.want {
			t.Errorf("%s: summarise(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
	// And the length survives even when the bytes do not.
	o := leaf([]string{"x", "thumbnail"}, "\xff\xd8binary", false)
	if o.MaxLen != 8 {
		t.Errorf("MaxLen = %d, want the real length", o.MaxLen)
	}
}

func TestIdentifiersReadsWhicheverShapeACollectionTakes(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"objects with uuids", `[{"uuid":"a"},{"uuid":"b"}]`, []string{"a", "b"}},
		{"objects with numeric ids", `[{"id":0},{"id":7}]`, []string{"0", "7"}},
		{"a list of names", `["main","backup"]`, []string{"main", "backup"}},
		{"empty", `[]`, nil},
		{"not a collection", `{"a":1}`, nil},
		{"not json", `nonsense`, nil},
	}
	for _, c := range cases {
		got := identifiers([]byte(c.body))
		if len(got) != len(c.want) {
			t.Errorf("%s: identifiers = %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: identifiers = %v, want %v", c.name, got, c.want)
				break
			}
		}
	}
}

func TestDedupeRemovesTheRepeatsASpecCreates(t *testing.T) {
	// `/processing/video/channels/{id}` and `…/{uuid}` are one
	// resource under two parameter names, and both expand to the same
	// concrete paths.
	in := []string{"/a", "/a", "/b", "/c", "/c", "/c"}
	got := dedupe(in)
	if len(got) != 3 || got[0] != "/a" || got[1] != "/b" || got[2] != "/c" {
		t.Errorf("dedupe = %v", got)
	}
	if got := dedupe([]string{"/only"}); len(got) != 1 {
		t.Errorf("dedupe(one) = %v", got)
	}
	if got := dedupe(nil); got != nil {
		t.Errorf("dedupe(nil) = %v", got)
	}
}

func TestSegmentsSplitsAPathTheWayAnOperatorReadsIt(t *testing.T) {
	got := segments("/io/ip/senders/video")
	if len(got) != 4 || got[0] != "io" || got[3] != "video" {
		t.Errorf("segments = %v", got)
	}
}
