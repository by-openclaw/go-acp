package registry

import (
	"context"
	"reflect"
	"strings"

	stdhttp "net/http"

	"acp/internal/amwa/codec/is04"
	httpsession "acp/internal/amwa/session/http"
)

// installQueryRoutes wires the Query API endpoints onto the server.
// base = `/x-nmos/query/<api-ver>`.
//
// Spec: https://specs.amwa.tv/is-04/releases/v1.3.3/APIs/QueryAPI.html
func installQueryRoutes(srv *httpsession.Server, store *Store, mgr *SubscriptionManager, base string) {
	srv.Handle(stdhttp.MethodGet, base+"/", func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		return 0, []string{"nodes/", "devices/", "sources/", "flows/", "senders/", "receivers/", "subscriptions/"}, nil
	})

	for _, t := range is04.AllResourceTypes {
		plural := t.Plural()
		// Capture loop variables.
		listType := t
		// GET /<plural>
		srv.Handle(stdhttp.MethodGet, base+"/"+plural, func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
			body := listResource(store, listType)
			body = applyFilter(body, r.URL.Query())
			return 0, body, nil
		})
	}

	// Per-id GETs use a prefix dispatcher (path = .../<plural>/<id>).
	for _, t := range is04.AllResourceTypes {
		plural := t.Plural()
		typeCopy := t
		prefix := base + "/" + plural + "/"
		srv.HandlePrefix(prefix, stdhttp.MethodGet, func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
			id := strings.TrimPrefix(r.URL.Path, prefix)
			if id == "" || strings.Contains(id, "/") {
				return stdhttp.StatusNotFound, httpsession.ErrorBody{Code: 404, Error: "Not Found", Debug: r.URL.Path}, nil
			}
			body, ok := getResource(store, typeCopy, id)
			if !ok {
				return stdhttp.StatusNotFound, httpsession.ErrorBody{Code: 404, Error: "Not Found", Debug: id}, nil
			}
			return 0, body, nil
		})
	}

	// Subscriptions — POST returns ws_href; GET /subscriptions
	// returns the active list.
	srv.Handle(stdhttp.MethodPost, base+"/subscriptions", mgr.HandlePost(base))
	srv.Handle(stdhttp.MethodGet, base+"/subscriptions", mgr.HandleList())
}

// applyFilter is the IS-04 §4.2 RQL-lite filter. Only equality on the
// `label`, `description`, `id`, and `version` top-level fields is
// supported; richer RQL lands later. Empty query returns body
// unchanged.
func applyFilter(body any, q map[string][]string) any {
	if len(q) == 0 {
		return body
	}
	rv := reflect.ValueOf(body)
	if rv.Kind() != reflect.Slice {
		return body
	}
	out := reflect.MakeSlice(rv.Type(), 0, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		item := rv.Index(i)
		if matchesAll(item, q) {
			out = reflect.Append(out, item)
		}
	}
	return out.Interface()
}

func matchesAll(item reflect.Value, q map[string][]string) bool {
	for k, vs := range q {
		if !matchesField(item, k, vs) {
			return false
		}
	}
	return true
}

// matchesField reads the named JSON field from item (via reflect) and
// compares against any of the requested values.
func matchesField(item reflect.Value, name string, vs []string) bool {
	if item.Kind() == reflect.Pointer {
		item = item.Elem()
	}
	if item.Kind() != reflect.Struct {
		return false
	}
	got := readJSONField(item, name)
	for _, v := range vs {
		if got == v {
			return true
		}
	}
	return false
}

// readJSONField walks the struct's fields looking for one whose
// `json:` tag matches name. Returns "" on miss.
func readJSONField(s reflect.Value, name string) string {
	t := s.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		if tag == "" {
			tag = f.Name
		}
		if tag != name {
			// Walk embedded structs (resource_core).
			if f.Anonymous && f.Type.Kind() == reflect.Struct {
				if got := readJSONField(s.Field(i), name); got != "" {
					return got
				}
			}
			continue
		}
		fv := s.Field(i)
		if fv.Kind() == reflect.String {
			return fv.String()
		}
	}
	return ""
}
