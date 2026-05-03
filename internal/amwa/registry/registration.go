package registry

import (
	"context"
	"errors"
	"fmt"
	"io"
	stdhttp "net/http"
	"strings"

	"dhs/internal/amwa/codec/is04"
	httpsession "dhs/internal/amwa/session/http"
)

// installRegistrationRoutes wires the Registration API endpoints onto
// the given HTTP server, base = `/x-nmos/registration/<api-ver>`.
// apiVer must match the URL prefix's wire minor (e.g. "v1.0", "v1.3").
func installRegistrationRoutes(srv *httpsession.Server, store *Store, base, apiVer string) {
	// Index — `["resource/", "health/"]`.
	srv.Handle(stdhttp.MethodGet, base+"/", func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		return 0, []string{"resource/", "health/"}, nil
	})

	// POST /resource — accept registrations.
	// Spec IS-04 §6.1.1: response MUST include `Location` header pointing
	// at /x-nmos/registration/<api-ver>/resource/<plural>/<id>. AMWA
	// IS-04-02 test_03/15/21*/23/24/27-31 fail the suite without it.
	srv.Handle(stdhttp.MethodPost, base+"/resource", func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if err != nil {
			return stdhttp.StatusBadRequest, httpsession.ErrorBody{Code: 400, Error: "Bad Request", Debug: "read body: " + err.Error()}, nil
		}
		env, err := is04.DecodeRegistration(body)
		if err != nil {
			return stdhttp.StatusBadRequest, httpsession.ErrorBody{Code: 400, Error: "Bad Request", Debug: err.Error()}, nil
		}
		id := idFromEnvelope(env)
		hadPrev := preExists(store, env.Type, id)
		if err := store.IngestRegistrationVersioned(env, apiVer); err != nil {
			if errors.Is(err, ErrAPIVerConflict) {
				return stdhttp.StatusConflict, httpsession.ErrorBody{Code: 409, Error: "Conflict", Debug: err.Error()}, nil
			}
			return stdhttp.StatusBadRequest, httpsession.ErrorBody{Code: 400, Error: "Bad Request", Debug: err.Error()}, nil
		}
		// Echo the data; spec says response body is the registered resource.
		var raw any
		_ = decodeRaw(env.Data, &raw)
		status := stdhttp.StatusCreated
		if hadPrev {
			status = stdhttp.StatusOK
		}
		loc := base + "/resource/" + env.Type.Plural() + "/" + id
		return status, &httpsession.WithHeaders{
			Body:    raw,
			Headers: map[string]string{"Location": loc},
		}, nil
	})

	// GET /resource/{type}/{id} — read-back.
	// DELETE /resource/{type}/{id} — explicit deregistration.
	// We can't register parameterised paths via the exact-match
	// session/http server, so use a helper handler for the resource
	// subtree by registering each combo at runtime is impractical.
	// Instead, install a single "/resource/" prefix handler that
	// dispatches by method + path remainder.
	resourcePrefix := base + "/resource/"
	prefixHandler := func(method string) httpsession.HandlerFunc {
		return func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
			rest := strings.TrimPrefix(r.URL.Path, resourcePrefix)
			parts := strings.Split(rest, "/")
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return stdhttp.StatusNotFound, httpsession.ErrorBody{Code: 404, Error: "Not Found", Debug: r.URL.Path}, nil
			}
			plural, id := parts[0], parts[1]
			t, ok := singularFromPlural(plural)
			if !ok {
				return stdhttp.StatusNotFound, httpsession.ErrorBody{Code: 404, Error: "Not Found", Debug: "unknown resource type " + plural}, nil
			}
			switch method {
			case stdhttp.MethodGet:
				body, ok := getResource(store, t, id)
				if !ok {
					return stdhttp.StatusNotFound, httpsession.ErrorBody{Code: 404, Error: "Not Found", Debug: id}, nil
				}
				return 0, body, nil
			case stdhttp.MethodDelete:
				if err := store.DeleteResource(t, id); err != nil {
					if errors.Is(err, ErrNotFound) {
						return stdhttp.StatusNotFound, httpsession.ErrorBody{Code: 404, Error: "Not Found", Debug: id}, nil
					}
					return stdhttp.StatusBadRequest, httpsession.ErrorBody{Code: 400, Error: "Bad Request", Debug: err.Error()}, nil
				}
				return stdhttp.StatusNoContent, nil, nil
			}
			return stdhttp.StatusMethodNotAllowed, httpsession.ErrorBody{Code: 405, Error: "Method Not Allowed", Debug: method}, nil
		}
	}
	srv.HandlePrefix(resourcePrefix, stdhttp.MethodGet, prefixHandler(stdhttp.MethodGet))
	srv.HandlePrefix(resourcePrefix, stdhttp.MethodDelete, prefixHandler(stdhttp.MethodDelete))

	// POST /health/nodes/{id} + GET /health/nodes/{id}
	healthPrefix := base + "/health/nodes/"
	healthHandler := func(method string) httpsession.HandlerFunc {
		return func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
			id := strings.TrimPrefix(r.URL.Path, healthPrefix)
			if id == "" || strings.Contains(id, "/") {
				return stdhttp.StatusNotFound, httpsession.ErrorBody{Code: 404, Error: "Not Found", Debug: r.URL.Path}, nil
			}
			switch method {
			case stdhttp.MethodPost:
				if err := store.Heartbeat(id); err != nil {
					return stdhttp.StatusNotFound, httpsession.ErrorBody{Code: 404, Error: "Not Found", Debug: id}, nil
				}
				t, _ := store.HealthFor(id)
				return 0, is04.HealthResponse{Health: fmt.Sprintf("%d", t.Unix())}, nil
			case stdhttp.MethodGet:
				t, err := store.HealthFor(id)
				if err != nil {
					return stdhttp.StatusNotFound, httpsession.ErrorBody{Code: 404, Error: "Not Found", Debug: id}, nil
				}
				return 0, is04.HealthResponse{Health: fmt.Sprintf("%d", t.Unix())}, nil
			}
			return stdhttp.StatusMethodNotAllowed, httpsession.ErrorBody{Code: 405, Error: "Method Not Allowed", Debug: method}, nil
		}
	}
	srv.HandlePrefix(healthPrefix, stdhttp.MethodPost, healthHandler(stdhttp.MethodPost))
	srv.HandlePrefix(healthPrefix, stdhttp.MethodGet, healthHandler(stdhttp.MethodGet))
}

// preExists reports whether a resource of the given (type, id) is
// already in the store — used to choose 200 vs 201 on POST /resource.
func preExists(s *Store, t is04.ResourceType, id string) bool {
	if id == "" {
		return false
	}
	_, ok := getResource(s, t, id)
	return ok
}

// idFromEnvelope reads the inner `id` field without fully decoding
// the resource.
func idFromEnvelope(env *is04.RegistrationRequest) string {
	var head struct {
		ID string `json:"id"`
	}
	if err := decodeRaw(env.Data, &head); err != nil {
		return ""
	}
	return head.ID
}

// decodeRaw is the only path that reads json.RawMessage into a
// concrete type; isolated so callers don't import encoding/json.
func decodeRaw(data []byte, dst any) error {
	return jsonUnmarshal(data, dst)
}
