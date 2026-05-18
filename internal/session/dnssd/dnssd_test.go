package dnssd

import (
	"context"
	"strings"
	"testing"
)

// TestParseTXT covers avahi-browse's quoted TXT format.
func TestParseTXT(t *testing.T) {
	cases := []struct {
		in   string
		want map[string]string
	}{
		{"", nil},
		{`"txtvers=1"`, map[string]string{"txtvers": "1"}},
		{`"txtvers=1" "path=/" "name=dhs"`, map[string]string{"txtvers": "1", "path": "/", "name": "dhs"}},
		{`"dtdVersion=2.60"`, map[string]string{"dtdVersion": "2.60"}},
	}
	for _, tc := range cases {
		t.Run(strings.ReplaceAll(tc.in, " ", "_"), func(t *testing.T) {
			got := parseTXT(tc.in)
			if len(got) != len(tc.want) {
				t.Errorf("got %v; want %v", got, tc.want)
				return
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("key %q = %q; want %q (full=%v)", k, got[k], v, got)
				}
			}
		})
	}
}

// TestToolBrowser_ServiceTypeRequired asserts the browser rejects an
// empty ServiceType (the dispatcher must not invoke avahi/dns-sd with
// no filter — would dump the entire mDNS namespace).
func TestToolBrowser_ServiceTypeRequired(t *testing.T) {
	b := NewToolBrowser()
	_, err := b.Browse(context.Background(), BrowseOptions{})
	if err == nil {
		t.Fatal("want error; got nil")
	}
	if !strings.Contains(err.Error(), "ServiceType is required") {
		t.Errorf("unexpected error: %v", err)
	}
}
