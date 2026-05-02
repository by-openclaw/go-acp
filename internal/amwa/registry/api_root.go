package registry

import (
	"context"
	stdhttp "net/http"

	httpsession "acp/internal/amwa/session/http"
)

// installAPIRootRoutes wires the discovery roots that AMWA IS-04-02
// `auto_query_1/2` and `auto_registration_1/2` exercise:
//
//   - GET /x-nmos                  → ["registration/", "query/"]
//   - GET /x-nmos/registration     → version listing (e.g. ["v1.3/"])
//   - GET /x-nmos/query            → version listing (e.g. ["v1.3/"])
//
// Each list builds from the same `apiVers` set the registry serves —
// the exact `<v>/` strings the per-version GET handlers are mounted at.
func installAPIRootRoutes(srv *httpsession.Server, apiVers []string) {
	versionList := make([]string, 0, len(apiVers))
	for _, v := range apiVers {
		versionList = append(versionList, v+"/")
	}
	roots := []string{"registration/", "query/"}

	// /x-nmos and /x-nmos/ — both, since the test driver hits the
	// non-trailing-slash form and we want the trailing form to match
	// the same listing for human curl users.
	rootHandler := func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		return 0, roots, nil
	}
	srv.Handle(stdhttp.MethodGet, "/x-nmos", rootHandler)
	srv.Handle(stdhttp.MethodGet, "/x-nmos/", rootHandler)

	regHandler := func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		return 0, versionList, nil
	}
	srv.Handle(stdhttp.MethodGet, "/x-nmos/registration", regHandler)
	srv.Handle(stdhttp.MethodGet, "/x-nmos/registration/", regHandler)

	queryHandler := func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		return 0, versionList, nil
	}
	srv.Handle(stdhttp.MethodGet, "/x-nmos/query", queryHandler)
	srv.Handle(stdhttp.MethodGet, "/x-nmos/query/", queryHandler)
}
