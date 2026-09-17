package router

import (
	"testing"

	"dhs/internal/snell-rollcall/codec/dtp"
)

// A salvo is a set of routes made together. What is in one is never on the
// wire: a client fires it by number and is told how many routes it made.

func TestAFireSurvivesTheWire(t *testing.T) {
	b, err := FireSalvo{Salvo: 7}.AppendTo(nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeFireSalvo(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Salvo != 7 {
		t.Errorf("salvo = %d, want 7", got.Salvo)
	}
}

func TestAFireThatIsNotOne(t *testing.T) {
	notANumber, err := dtp.Encode(dtp.Params{dtp.String("two")}, false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"nothing at all", nil},
		{"not parameters", []byte{0xFF, 0xFF}},
		{"a name where the number goes", notANumber},
	} {
		if _, err := DecodeFireSalvo(tc.body); err == nil {
			t.Errorf("%s was decoded as a fire", tc.name)
		}
	}
}

func TestWhatAFireIsAnsweredWith(t *testing.T) {
	// The count is the whole of the result: "number of routes made or 0 on
	// error", with nothing to say which error.
	b, err := SalvoFired{Salvo: 3, Routes: 12}.AppendTo(nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeSalvoFired(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Salvo != 3 || got.Routes != 12 {
		t.Errorf("salvo %d made %d routes, want 3 and 12", got.Salvo, got.Routes)
	}
	if !got.OK() {
		t.Error("a salvo that made twelve routes reads as having done nothing")
	}

	none, err := SalvoFired{Salvo: 3}.AppendTo(nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	empty, err := DecodeSalvoFired(none)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if empty.OK() {
		t.Error("a salvo that made no routes reads as having worked")
	}
}

func TestAnAnswerThatIsNotOne(t *testing.T) {
	tooFew, err := dtp.Encode(dtp.Params{dtp.Uint(1)}, false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	notNumbers, err := dtp.Encode(dtp.Params{dtp.Uint(1), dtp.String("twelve")}, false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"nothing at all", nil},
		{"not parameters", []byte{0xFF, 0xFF}},
		{"the count left off", tooFew},
		{"a word where the count goes", notNumbers},
	} {
		if _, err := DecodeSalvoFired(tc.body); err == nil {
			t.Errorf("%s was decoded as an answer", tc.name)
		}
	}
}
