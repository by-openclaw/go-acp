package http

import "testing"

// TestLinkRel pins RFC 8288 Link parsing against the two shapes real
// registries emit: one comma-joined header (the AMWA mock builds next,
// prev and first into a single string) and repeated headers.
func TestLinkRel(t *testing.T) {
	amwaStyle := `<http://r:80/x-nmos/query/v1.0/senders/?paging.since=15:0&paging.limit=2>; rel="next",` +
		`<http://r:80/x-nmos/query/v1.0/senders/?paging.until=3:0&paging.limit=2>; rel="prev",` +
		`<http://r:80/x-nmos/query/v1.0/senders/?paging.since=0:0&paging.limit=2>; rel="first"`

	cases := []struct {
		name    string
		headers []string
		rel     string
		want    string
	}{
		{"amwa comma-joined next", []string{amwaStyle}, "next",
			"http://r:80/x-nmos/query/v1.0/senders/?paging.since=15:0&paging.limit=2"},
		{"amwa comma-joined prev", []string{amwaStyle}, "prev",
			"http://r:80/x-nmos/query/v1.0/senders/?paging.until=3:0&paging.limit=2"},
		{"repeated headers", []string{
			`<http://a/1>; rel="prev"`,
			`<http://a/2>; rel="next"`,
		}, "next", "http://a/2"},
		{"single-quoted rel tolerated", []string{`<http://a/3>; rel='next'`}, "next", "http://a/3"},
		{"absent rel", []string{amwaStyle}, "last", ""},
		{"no headers", nil, "next", ""},
		{"garbage ignored", []string{"not-a-link", `<http://a/4>; rel="next"`}, "next", "http://a/4"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := linkRel(tc.headers, tc.rel); got != tc.want {
				t.Fatalf("linkRel(%v, %q) = %q, want %q", tc.headers, tc.rel, got, tc.want)
			}
		})
	}
}
