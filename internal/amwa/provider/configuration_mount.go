// Layer-3 — joining the IS-14 Configuration API to the IS-04 Node
// API. Same doctrine as IS-05/IS-08/IS-11: an API that is served but
// not advertised in device.controls[] is, to every controller,
// absent. IS-14's "IS-04 interactions" doc fixes the URN
// (urn:x-nmos:control:configuration) and requires IS-04 v1.1+.

package provider

import (
	"context"
	stdhttp "net/http"
	"strings"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is14"
	httpsession "dhs/internal/amwa/session/http"
)

// Mount registers every Configuration API route on srv. The static
// index endpoints are exact routes; everything under rolePaths/ is a
// prefix route because role paths, property ids and method ids are
// model-dependent path parameters.
func (s *IS14ConfigurationServer) Mount(srv *httpsession.Server) {
	ok := func(v any) (int, any, error) { return stdhttp.StatusOK, v, nil }
	srv.Handle(stdhttp.MethodGet, "/x-nmos/configuration", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok(withSlashes(s.vers))
	})
	srv.Handle(stdhttp.MethodGet, "/x-nmos/configuration/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok(withSlashes(s.vers))
	})
	for _, ver := range s.vers {
		s.mountVersion(srv, "/x-nmos/configuration/"+ver)
	}
}

func (s *IS14ConfigurationServer) mountVersion(srv *httpsession.Server, base string) {
	srv.Handle(stdhttp.MethodGet, base+"/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return stdhttp.StatusOK, []string{"rolePaths/"}, nil
	})

	// One dispatcher for the whole rolePaths subtree; the exact route
	// above still wins for the version index (exact beats prefix in
	// the server's route table).
	handler := func(_ context.Context, r *stdhttp.Request) (int, any, error) {
		tail := strings.TrimPrefix(r.URL.Path, base+"/rolePaths")
		tail = strings.Trim(tail, "/")
		return s.dispatch(r.Method, tail, r)
	}
	for _, m := range []string{stdhttp.MethodGet, stdhttp.MethodPut, stdhttp.MethodPatch} {
		srv.HandlePrefix(base+"/rolePaths", m, handler)
	}
}

// attachConfigurationAPI mounts IS-14 and advertises it on every
// Device, wiring the model-change hook so a property write bumps the
// Devices' IS-04 versions (the doc's version-increment duty).
func (s *IS04NodeServer) attachConfigurationAPI(srv *httpsession.Server) {
	if s.configuration == nil {
		return
	}
	s.configuration.SetOnModelChanged(s.bumpDeviceVersions)
	s.configuration.Mount(srv)
	host := s.controlHost()
	for i := range s.bundle.Devices {
		d := &s.bundle.Devices[i]
		for _, ver := range s.configuration.Versions() {
			ctrl := is04.DeviceControl{
				Type:          is14.ControlType + ver,
				Href:          s.scheme() + "://" + host + "/x-nmos/configuration/" + ver + "/",
				Authorization: s.authOn(),
			}
			upsertControl(&d.Controls, ctrl)
		}
	}
}
