package main

import (
	"strings"
	"testing"
)

// TestFormatGetSalvoHuman_Cases pins the R11 #482 human-format
// presentation against every shape getSalvo can emit on the wire.
func TestFormatGetSalvoHuman_Cases(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty string", in: "", want: "(empty salvo)"},
		{name: "whitespace only", in: "   ", want: "(empty salvo)"},
		{name: "single tgt single src", in: "0=0", want: "tgt 0 <- Src [0]"},
		{name: "single tgt multi src", in: "5=3,4,5", want: "tgt 5 <- Src [3,4,5]"},
		{name: "multiple tgts", in: "0=0;1=1;5=3,4,5", want: "tgt 0 <- Src [0]\ntgt 1 <- Src [1]\ntgt 5 <- Src [3,4,5]"},
		{name: "empty src list", in: "7=", want: "tgt 7 <- Src []"},
		{name: "mixed empty + filled", in: "0=0;7=;3=3,4", want: "tgt 0 <- Src [0]\ntgt 7 <- Src []\ntgt 3 <- Src [3,4]"},
		{name: "trailing semicolon", in: "0=0;", want: "tgt 0 <- Src [0]"},
		{name: "double semicolon skipped", in: "0=0;;1=1", want: "tgt 0 <- Src [0]\ntgt 1 <- Src [1]"},
		{name: "spaces around tokens", in: " 0 = 0 ; 5 = 3,4,5 ", want: "tgt 0 <- Src [0]\ntgt 5 <- Src [3,4,5]"},
		{name: "malformed row passed through", in: "0=0;bogus;1=1", want: "tgt 0 <- Src [0]\nrow: bogus\ntgt 1 <- Src [1]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatGetSalvoHuman(tc.in); got != tc.want {
				t.Errorf("formatGetSalvoHuman(%q) =\n%s\nwant:\n%s", tc.in, got, tc.want)
			}
		})
	}
}

// TestIsGetSalvoPath pins the path-matching logic that gates when the
// --format human flag triggers the salvo pretty-print.
func TestIsGetSalvoPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{path: "getSalvo", want: true},
		{path: "dhs-emberplus-integration.functions.getSalvo", want: true},
		{path: "router.functions.getSalvo", want: true},
		{path: "functions.setLock", want: false},
		{path: "getSalvoSomethingElse", want: false},
		{path: "", want: false},
		{path: "getSalvo.subItem", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			if got := isGetSalvoPath(tc.path); got != tc.want {
				t.Errorf("isGetSalvoPath(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

// TestFormatGetSalvoHuman_NoTrailingNewline pins that the formatter
// returns a single string with no trailing newline — fmt.Println
// adds one already.
func TestFormatGetSalvoHuman_NoTrailingNewline(t *testing.T) {
	got := formatGetSalvoHuman("0=0;1=1")
	if strings.HasSuffix(got, "\n") {
		t.Errorf("formatGetSalvoHuman trailing newline = %q, want no trailing newline", got)
	}
}
