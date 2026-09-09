package router

import (
	"testing"

	"dhs/internal/snell-rollcall/codec/dtp"
)

// An association is one logical thing spread across the levels of a matrix: a
// camera is a picture, two audio levels and some ancillary data, each on a
// level of its own with its own numbering.

func TestAnAssociationTableIsAsLongAsTheMatrixHasLevels(t *testing.T) {
	// Three names and then one member per level.
	for _, tc := range []struct {
		levels int
		want   uint32
	}{
		{0, 3},
		{1, 4},
		{4, 7},
		{-1, 3}, // a negative count is not a matrix; it is a bug upstream
	} {
		if got := AssocTableSize(tc.levels); got != tc.want {
			t.Errorf("AssocTableSize(%d) = %d, want %d", tc.levels, got, tc.want)
		}
	}
}

func TestTheMemberOfAnAssociationOnOneLevel(t *testing.T) {
	const base Command = 1000

	for _, tc := range []struct {
		level int
		want  Command
		ok    bool
	}{
		{1, base + OffAssocSrcDst, true},
		{4, base + OffAssocSrcDst + 3, true},
		{0, 0, false},
		{-1, 0, false},
	} {
		got, ok := AssocMember(base, tc.level)
		if ok != tc.ok || got != tc.want {
			t.Errorf("AssocMember(%d, %d) = %d, %v; want %d, %v",
				base, tc.level, got, ok, tc.want, tc.ok)
		}
	}
}

func TestARouteRequestSurvivesTheWire(t *testing.T) {
	// The case this command exists for: a camera to a monitor with its picture
	// and its audio, leaving the ancillary data where it was.
	levels := dtp.NewBitmap(8)
	levels.Set(0)
	levels.Set(1)
	levels.Set(2)

	want := MakeRoute{
		DestMatrix: 1, DestAssoc: 12, SourceMatrix: 1, SourceAssoc: 4,
		Levels: levels,
	}

	b, err := want.AppendTo(nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeMakeRoute(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.DestMatrix != want.DestMatrix || got.DestAssoc != want.DestAssoc ||
		got.SourceMatrix != want.SourceMatrix || got.SourceAssoc != want.SourceAssoc {
		t.Errorf("addresses = %+v, want %+v", got, want)
	}
	for i := range 4 {
		if got.Levels.Get(i) != want.Levels.Get(i) {
			t.Errorf("level %d selected = %v, want %v",
				i+1, got.Levels.Get(i), want.Levels.Get(i))
		}
	}
}

func TestARouteRequestThatIsNotOne(t *testing.T) {
	tooFew, err := dtp.Encode(dtp.Params{dtp.Uints(1, 2, 3, 4)}, false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	wrongAddresses, err := dtp.Encode(dtp.Params{
		dtp.Uints(1, 2), dtp.Bits(dtp.NewBitmap(4)),
	}, false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	notAnArray, err := dtp.Encode(dtp.Params{
		dtp.Uint(1), dtp.Bits(dtp.NewBitmap(4)),
	}, false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	notABitmap, err := dtp.Encode(dtp.Params{
		dtp.Uints(1, 2, 3, 4), dtp.Uint(7),
	}, false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"nothing at all", nil},
		{"not parameters", []byte{0xFF, 0xFF}},
		{"the levels left off", tooFew},
		{"two addresses instead of four", wrongAddresses},
		{"one address instead of an array", notAnArray},
		{"a number where the levels go", notABitmap},
	} {
		if _, err := DecodeMakeRoute(tc.body); err == nil {
			t.Errorf("%s was decoded as a route request", tc.name)
		}
	}
}

func TestTheResultOfARoute(t *testing.T) {
	for _, want := range []RouteResult{
		RouteOK, RouteProtected, RouteNoTieline, RouteBadParameters,
	} {
		b, err := AppendRouteResult(nil, want)
		if err != nil {
			t.Fatalf("encode %s: %v", want, err)
		}
		got, err := DecodeRouteResult(b)
		if err != nil {
			t.Fatalf("decode %s: %v", want, err)
		}
		if got != want {
			t.Errorf("result = %s, want %s", got, want)
		}
	}
}

func TestAResultThatIsNotOne(t *testing.T) {
	notANumber, err := dtp.Encode(dtp.Params{dtp.String("fine")}, false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"nothing at all", nil},
		{"not parameters", []byte{0xFF, 0xFF}},
		{"a string where the result goes", notANumber},
	} {
		if _, err := DecodeRouteResult(tc.body); err == nil {
			t.Errorf("%s was decoded as a result", tc.name)
		}
	}
}
