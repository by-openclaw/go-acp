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
		// BCP-003-02 resource ownership: an authenticated client may
		// only update resources IT registered (IS-04-02 test_33 /
		// test_33_1 — azp is normalised into client_id by the codec).
		// A resource registered before auth was armed has no owner and
		// is claimed by the first authenticated writer.
		client := httpsession.ClientIDFrom(ctx)
		if client != "" && hadPrev {
			if owner := store.Owner(id); owner != "" && owner != client {
				return stdhttp.StatusForbidden, httpsession.ErrorBody{
					Code: 403, Error: "Forbidden",
					Debug: fmt.Sprintf("resource %s is owned by another client", id)}, nil
			}
		}
		if err := store.IngestRegistrationVersioned(env, apiVer); err != nil {
			if errors.Is(err, ErrAPIVerConflict) {
				return stdhttp.StatusConflict, httpsession.ErrorBody{Code: 409, Error: "Conflict", Debug: err.Error()}, nil
			}
			return stdhttp.StatusBadRequest, httpsession.ErrorBody{Code: 400, Error: "Bad Request", Debug: err.Error()}, nil
		}
		if client != "" {
			store.SetOwner(id, client)
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
	//
	// The session/http server matches exact paths, so the {type}/{id}
	// pair is served by one prefix handler per verb. Each verb gets
	// its OWN handler rather than one that switches on the method: the
	// method is fixed at registration time, so a switch could only
	// ever have a dead default arm.
	resourcePrefix := base + "/resource/"
	srv.HandlePrefix(resourcePrefix, stdhttp.MethodGet,
		func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
			t, id, errResp := resourceTarget(resourcePrefix, r)
			if errResp != nil {
				return stdhttp.StatusNotFound, *errResp, nil
			}
			body, ok := getResource(store, t, id)
			if !ok {
				return stdhttp.StatusNotFound, httpsession.ErrorBody{Code: 404, Error: "Not Found", Debug: id}, nil
			}
			return 0, body, nil
		})
	srv.HandlePrefix(resourcePrefix, stdhttp.MethodDelete,
		func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
			t, id, errResp := resourceTarget(resourcePrefix, r)
			if errResp != nil {
				return stdhttp.StatusNotFound, *errResp, nil
			}
			// DeleteResource refuses only two ways: the resource is
			// not there, or the type is not one IS-04 defines — and
			// the type came from singularFromPlural above, so what
			// reaches here is always the former.
			if err := store.DeleteResource(t, id); err != nil {
				return stdhttp.StatusNotFound, httpsession.ErrorBody{Code: 404, Error: "Not Found", Debug: id}, nil
			}
			return stdhttp.StatusNoContent, nil, nil
		})

	// POST /health/nodes/{id} + GET /health/nodes/{id}
	healthPrefix := base + "/health/nodes/"
	srv.HandlePrefix(healthPrefix, stdhttp.MethodPost,
		func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
			id, errResp := healthTarget(healthPrefix, r)
			if errResp != nil {
				return stdhttp.StatusNotFound, *errResp, nil
			}
			if err := store.Heartbeat(id); err != nil {
				return stdhttp.StatusNotFound, httpsession.ErrorBody{Code: 404, Error: "Not Found", Debug: id}, nil
			}
			t, _ := store.HealthFor(id)
			return 0, is04.HealthResponse{Health: fmt.Sprintf("%d", t.Unix())}, nil
		})
	srv.HandlePrefix(healthPrefix, stdhttp.MethodGet,
		func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
			id, errResp := healthTarget(healthPrefix, r)
			if errResp != nil {
				return stdhttp.StatusNotFound, *errResp, nil
			}
			t, err := store.HealthFor(id)
			if err != nil {
				return stdhttp.StatusNotFound, httpsession.ErrorBody{Code: 404, Error: "Not Found", Debug: id}, nil
			}
			return 0, is04.HealthResponse{Health: fmt.Sprintf("%d", t.Unix())}, nil
		})
}

// resourceTarget reads the {type}/{id} pair out of a resource-subtree
// path. A non-nil error body is the 404 the caller must answer with:
// a path that names no pair, or a collection IS-04 does not define.
func resourceTarget(prefix string, r *stdhttp.Request) (is04.ResourceType, string, *httpsession.ErrorBody) {
	rest := strings.TrimPrefix(r.URL.Path, prefix)
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", &httpsession.ErrorBody{Code: 404, Error: "Not Found", Debug: r.URL.Path}
	}
	t, ok := singularFromPlural(parts[0])
	if !ok {
		return "", "", &httpsession.ErrorBody{
			Code: 404, Error: "Not Found", Debug: "unknown resource type " + parts[0]}
	}
	return t, parts[1], nil
}

// healthTarget reads the Node id out of a health path, or answers the
// 404 a path naming no single id earns.
func healthTarget(prefix string, r *stdhttp.Request) (string, *httpsession.ErrorBody) {
	id := strings.TrimPrefix(r.URL.Path, prefix)
	if id == "" || strings.Contains(id, "/") {
		return "", &httpsession.ErrorBody{Code: 404, Error: "Not Found", Debug: r.URL.Path}
	}
	return id, nil
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
