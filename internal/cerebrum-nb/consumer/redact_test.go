package cerebrumnb

import (
	"strings"
	"testing"
)

func TestRedactSecrets(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "the LOGIN frame — the one that actually leaked",
			in:   `<LOGIN USERNAME="dhs-staging" PASSWORD="s3cr3t123" MTID="1"/>`,
			want: `<LOGIN USERNAME="dhs-staging" PASSWORD="*********" MTID="1"/>`,
		},
		{
			name: "lower-case attribute is redacted too (decoder accepts any case)",
			in:   `<login username="u" password="pw" mtid="1"/>`,
			want: `<login username="u" password="**" mtid="1"/>`,
		},
		{
			name: "mixed case",
			in:   `<LOGIN Password="abc"/>`,
			want: `<LOGIN Password="***"/>`,
		},
		{
			name: "token and secret attributes",
			in:   `<AUTH TOKEN="abcd" SECRET="xy"/>`,
			want: `<AUTH TOKEN="****" SECRET="**"/>`,
		},
		{
			name: "empty value stays empty and well-formed",
			in:   `<LOGIN PASSWORD=""/>`,
			want: `<LOGIN PASSWORD=""/>`,
		},
		{
			name: "whitespace around the equals sign",
			in:   `<LOGIN PASSWORD = "abc"/>`,
			want: `<LOGIN PASSWORD = "***"/>`,
		},
		{
			name: "several secrets in one document",
			in:   `<A PASSWORD="one"/><B PASSWORD="two"/>`,
			want: `<A PASSWORD="***"/><B PASSWORD="***"/>`,
		},
		{
			name: "a frame with no secret is returned untouched",
			in:   `<DEVICE_CHANGE OBJECT="ComputerOverview.ProcessorTime" VALUE="4"/>`,
			want: `<DEVICE_CHANGE OBJECT="ComputerOverview.ProcessorTime" VALUE="4"/>`,
		},
		{
			name: "an attribute merely CONTAINING the word is still masked (fail safe)",
			in:   `<X USER_PASSWORD="abc"/>`,
			want: `<X USER_PASSWORD="***"/>`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := string(redactSecrets([]byte(tc.in)))
			if got != tc.want {
				t.Errorf("redactSecrets:\n got %s\nwant %s", got, tc.want)
			}
		})
	}
}

// The input must never be mutated — the caller still has to put the real
// bytes on the wire after logging them.
func TestRedactSecretsDoesNotMutateInput(t *testing.T) {
	orig := `<LOGIN USERNAME="u" PASSWORD="realpw" MTID="1"/>`
	buf := []byte(orig)
	_ = redactSecrets(buf)
	if string(buf) != orig {
		t.Fatalf("redactSecrets mutated its input: %s", buf)
	}
}

// Belt and braces: whatever the shape, the literal secret must not survive.
func TestRedactSecretsLeavesNoPlaintext(t *testing.T) {
	const pw = "BySyst3ms-example"
	frames := []string{
		`<LOGIN USERNAME="dhs-staging" PASSWORD="` + pw + `" MTID="1"/>`,
		`<login password="` + pw + `"/>`,
		`<ERROR ECHO="&lt;LOGIN&gt;" PASSWORD="` + pw + `"/>`,
	}
	for _, f := range frames {
		got := string(redactSecrets([]byte(f)))
		if strings.Contains(got, pw) {
			t.Errorf("plaintext secret survived redaction in %q -> %q", f, got)
		}
	}
}

func TestRedactMalformedIsSafe(t *testing.T) {
	// An unterminated value must not panic or run off the end.
	in := `<LOGIN PASSWORD="unterminated`
	_ = redactSecrets([]byte(in))

	// Attribute name with no value at all.
	in2 := `<LOGIN PASSWORD/>`
	if got := string(redactSecrets([]byte(in2))); got != in2 {
		t.Errorf("bare attribute changed: %q", got)
	}
}
