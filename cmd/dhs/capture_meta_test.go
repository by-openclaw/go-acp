package main

import (
	"strings"
	"testing"
)

// TestRedactCLI pins the secret-flag redaction: both "--pass value"
// and "--pass=value" forms (single-dash included) never reach the
// meta record; everything else survives verbatim.
func TestRedactCLI(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"dhs", "consumer", "cerebrum-nb", "export", "h", "--user", "YOB", "--pass", "s3cret"},
			"dhs consumer cerebrum-nb export h --user YOB --pass ***"},
		{[]string{"dhs", "--pass=s3cret", "--token=abc", "-password", "x"},
			"dhs --pass=*** --token=*** -password ***"},
		{[]string{"dhs", "get", "--path", "a.b.c"},
			"dhs get --path a.b.c"},
	}
	for _, c := range cases {
		if got := redactCLI(c.in); got != c.want {
			t.Fatalf("redactCLI(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCaptureMeta(t *testing.T) {
	m := captureMeta("cerebrum-nb", "10.44.55.39", "export")
	if m.Proto != "cerebrum-nb" || m.Target != "10.44.55.39" || m.Verb != "export" {
		t.Fatalf("meta context = %+v", m)
	}
	if !strings.HasPrefix(m.BinarySHA256, "sha256:") {
		t.Fatalf("binary sha = %q", m.BinarySHA256)
	}
	if m.CLI == "" || m.StartedUTC == "" {
		t.Fatalf("meta incomplete = %+v", m)
	}
}
