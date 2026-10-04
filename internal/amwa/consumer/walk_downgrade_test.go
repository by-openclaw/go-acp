package consumer

// A walk of a Registry asks for the lower minors too (IS-04 §6.1.5,
// query.downgrade). Without it a v1.3 walk of a plant with one v1.2
// Node shows the plant without that Node, and its Receivers cannot be
// named in a route.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"dhs/internal/amwa/codec/is04"
	v10 "dhs/internal/amwa/codec/is04/v10"
	v13 "dhs/internal/amwa/codec/is04/v13"
	"dhs/internal/amwa/codec/spec"
	"dhs/internal/amwa/session/query"
)

// askedServer answers every collection with an empty list and records
// the query each one was asked with.
func askedServer(t *testing.T) (*httptest.Server, func() map[string]string) {
	t.Helper()
	var mu sync.Mutex
	asked := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked[r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:]] = r.URL.RawQuery
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "[]")
	}))
	t.Cleanup(srv.Close)
	return srv, func() map[string]string {
		mu.Lock()
		defer mu.Unlock()
		out := map[string]string{}
		for k, v := range asked {
			out[k] = v
		}
		return out
	}
}

func TestWalkAsksTheRegistryForLowerMinorsToo(t *testing.T) {
	collections := []string{"nodes", "devices", "sources", "flows", "senders", "receivers"}
	for name, tc := range map[string]struct {
		codec is04.Codec
		node  bool
		want  string
	}{
		"a Registry at v1.3":                   {codec: v13.New(), want: "query.downgrade=v1.0"},
		"a Registry at v1.0 has no such query": {codec: v10.New(), want: ""},
		"a Node has one view":                  {codec: v13.New(), node: true, want: ""},
	} {
		srv, asked := askedServer(t)
		var cl *query.Client
		var err error
		if tc.node {
			cl, err = query.NewNodeClient(srv.URL, tc.codec)
		} else {
			cl, err = query.NewClient(srv.URL, tc.codec)
		}
		if err != nil {
			t.Fatal(err)
		}
		ctrl := &Controller{reporter: &spec.SliceReporter{}, client: cl}
		// A Node serves itself at /self, not a list: this server's empty
		// list is no Node, and that read failing is not what is tested.
		if _, errs := ctrl.Walk(context.Background()); len(errs) != 0 && !tc.node {
			t.Fatalf("%s: walk: %v", name, errs)
		}
		got := asked()
		for _, coll := range collections {
			if coll == "nodes" && tc.node {
				coll = "self"
			}
			q, seen := got[coll]
			if !seen {
				t.Errorf("%s: %s was not read", name, coll)
			}
			if q != tc.want {
				t.Errorf("%s: %s was asked with %q, want %q", name, coll, q, tc.want)
			}
		}
	}
}
