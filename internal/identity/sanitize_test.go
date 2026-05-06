package identity

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathSegment(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr error
	}{
		{name: "nul-padded", in: "RRS18\x00\x00\x00", want: "RRS18"},
		{name: "interior-nul", in: "RR\x00S18", want: "RRS18"},
		{name: "crlf-replaced", in: "RRS18\r\nFW=1.0", want: "RRS18--FW-1.0"},
		{name: "shell-injection-trailing-slash", in: "RRS18; rm -rf /", want: "RRS18--rm--rf--"},
		{name: "path-traversal-rejected", in: "../../etc/passwd", wantErr: ErrUnsafe},
		{name: "single-dot-dot-rejected", in: "..", wantErr: ErrUnsafe},
		{name: "single-dot-rejected", in: ".", wantErr: ErrUnsafe},
		{name: "leading-dot-rejected", in: ".hidden", wantErr: ErrUnsafe},
		{name: "leading-dash-rejected", in: "-flaglike", wantErr: ErrUnsafe},
		{name: "empty-rejected", in: "", wantErr: ErrUnsafe},
		{name: "pure-nul-rejected", in: "\x00\x00\x00", wantErr: ErrUnsafe},
		{name: "truncate-64", in: strings.Repeat("a", 600), want: strings.Repeat("a", 64)},
		{name: "non-utf8-replaced", in: "RR\xff\xfeS18", want: "RR--S18"},
		{name: "backslash-replaced", in: "win\\path", want: "win-path"},
		{name: "spaces-replaced", in: "card name", want: "card-name"},
		{name: "model-with-underscore", in: "RRS18_v2", want: "RRS18_v2"},
		{name: "alphanum-passthrough", in: "RRS18-1601", want: "RRS18-1601"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PathSegment(tc.in)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPathSegment_StaysUnderRoot(t *testing.T) {
	root := filepath.FromSlash("tests/fixtures/products")
	inputs := []string{
		"RRS18",
		"RRS18-1601",
		"some.product.v1",
		"a_b-c.d",
	}
	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			seg, err := PathSegment(in)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			full := filepath.Clean(filepath.Join(root, seg))
			if !strings.HasPrefix(full, root) {
				t.Fatalf("path escapes root: %q", full)
			}
		})
	}
}

func TestYAMLValue(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "nul-padded", in: "RRS18\x00\x00\x00", want: "RRS18"},
		{name: "crlf-collapsed", in: "RRS18\r\nFW=1.0", want: "RRS18 FW=1.0"},
		{name: "tab-as-space", in: "card\tname", want: "card name"},
		{name: "path-traversal-flattened", in: "../../etc/passwd", want: "......etc.passwd"},
		{name: "shell-meta-verbatim", in: "RRS18; rm -rf /", want: "RRS18; rm -rf ."},
		{name: "empty-stays-empty", in: "", want: ""},
		{name: "pure-nul-empty", in: "\x00\x00", want: ""},
		{name: "non-utf8-replaced", in: "RR\xffS18", want: "RR?S18"},
		{name: "control-chars-dropped", in: "RR\x01\x02S18", want: "RRS18"},
		{name: "truncate-256", in: strings.Repeat("a", 600), want: strings.Repeat("a", 256)},
		{name: "trim-trailing-spaces", in: "RRS18  \r\n", want: "RRS18"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := YAMLValue(tc.in)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestJSONString(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "nul-padded", in: "RRS18\x00\x00\x00", want: "RRS18"},
		{name: "crlf-verbatim", in: "RRS18\r\nFW=1.0", want: "RRS18\r\nFW=1.0"},
		{name: "shell-meta-verbatim", in: "RRS18; rm -rf /", want: "RRS18; rm -rf /"},
		{name: "non-utf8-replaced", in: "RR\xff\xfeS18", want: "RR??S18"},
		{name: "empty-stays-empty", in: "", want: ""},
		{name: "truncate-256", in: strings.Repeat("a", 600), want: strings.Repeat("a", 256)},
		{name: "truncate-utf8-safe", in: strings.Repeat("a", 254) + "é", want: strings.Repeat("a", 254) + "é"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := JSONString(tc.in)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
