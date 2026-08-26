// Layer-3 IS-05 Connection API provider — HTTP surface.
//
// The endpoint set is fixed by the spec and by what real products
// expose. Checked against an EVS A467910 node (owner capture,
// 2026-08-26), which serves exactly:
//
//	/x-nmos/connection/{ver}/
//	  bulk/senders            GET POST
//	  bulk/receivers          GET POST
//	  single/senders/         GET
//	  single/senders/{id}/    GET  -> constraints active staged transportfile transporttype
//	  single/receivers/       GET
//	  single/receivers/{id}/  GET  -> constraints active staged transporttype
//
// Receivers have no transportfile of their own: a controller PUSHES
// the sender's SDP into a receiver through the staged PATCH body. That
// asymmetry is the one shape mistake worth calling out, because a
// receiver that serves a transportfile looks plausible and no
// controller will ever fetch it.

package provider

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	stdhttp "net/http"
	"sort"
	"strings"

	"dhs/internal/amwa/codec/is05"
	httpsession "dhs/internal/amwa/session/http"
)

// IS05ConnectionConfig configures the Connection API surface.
type IS05ConnectionConfig struct {
	// APIVer is the wire minor to mount, e.g. "v1.2". Empty mounts
	// every registered IS-05 codec in parallel, which is what a real
	// node does — a controller pinned to v1.0 and one pinned to v1.2
	// must both find a tree.
	APIVer string
}

// IS05ConnectionServer serves the Connection API for one Node.
type IS05ConnectionServer struct {
	logger *slog.Logger
	store  *connectionStore
	vers   []string
}

// NewIS05ConnectionServer builds the Connection API over a Node
// bundle. Every Sender and Receiver in the bundle gains an endpoint,
// because IS-05 §4.1 requires the ids to match the Node API exactly.
func NewIS05ConnectionServer(logger *slog.Logger, bundle *NodeConfig, cfg IS05ConnectionConfig) *IS05ConnectionServer {
	st := newConnectionStore()
	st.seedFromBundle(bundle)
	// "auto" on source_ip / interface_ip resolves to an address a
	// controller can actually reach. Taken from the Node's own
	// interface list, because that is what the Node already tells the
	// world about itself.
	st.setNodeIP(firstNodeIP(bundle))

	vers := is05.SupportedVersions()
	if cfg.APIVer != "" {
		vers = []string{cfg.APIVer}
	}
	return &IS05ConnectionServer{logger: logger, store: st, vers: vers}
}

// firstNodeIP picks the address ACTIVE parameters should name.
//
// The Node API already publishes the endpoints it is reachable on, so
// that list is the honest source — inventing an address here could
// name an interface the Node does not serve on.
func firstNodeIP(cfg *NodeConfig) string {
	for _, ep := range cfg.Node.API.Endpoints {
		if ep.Host != "" {
			return ep.Host
		}
	}
	return ""
}

// Store exposes the connection state for tests and for the IS-04
// receiver-target shim.
func (s *IS05ConnectionServer) Store() *connectionStore { return s.store }

// Versions lists the mounted IS-05 minors.
func (s *IS05ConnectionServer) Versions() []string { return s.vers }

// Mount registers every Connection API route on srv.
func (s *IS05ConnectionServer) Mount(srv *httpsession.Server) {
	ok := func(v any) (int, any, error) { return stdhttp.StatusOK, v, nil }

	// /x-nmos/connection/ — the version list. A controller reads this
	// to pick a minor, so it must list every mounted tree.
	srv.Handle(stdhttp.MethodGet, "/x-nmos/connection", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok(withSlashes(s.vers))
	})
	srv.Handle(stdhttp.MethodGet, "/x-nmos/connection/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok(withSlashes(s.vers))
	})

	for _, ver := range s.vers {
		base := "/x-nmos/connection/" + ver
		s.mountVersion(srv, base)
	}
}

func (s *IS05ConnectionServer) mountVersion(srv *httpsession.Server, base string) {
	ok := func(v any) (int, any, error) { return stdhttp.StatusOK, v, nil }

	srv.Handle(stdhttp.MethodGet, base+"/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok([]string{"bulk/", "single/"})
	})
	srv.Handle(stdhttp.MethodGet, base+"/single/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok([]string{"receivers/", "senders/"})
	})
	srv.Handle(stdhttp.MethodGet, base+"/bulk/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok([]string{"receivers/", "senders/"})
	})

	for _, kind := range []string{"senders", "receivers"} {
		s.mountCollection(srv, base, kind)
	}
}

func (s *IS05ConnectionServer) mountCollection(srv *httpsession.Server, base, kind string) {
	ok := func(v any) (int, any, error) { return stdhttp.StatusOK, v, nil }
	root := base + "/single/" + kind

	// The collection listing: ids with a trailing slash, sorted so the
	// response is stable across restarts. An unstable listing makes
	// every capture diff noisy for no reason.
	srv.Handle(stdhttp.MethodGet, root+"/", func(context.Context, *stdhttp.Request) (int, any, error) {
		ids := s.store.ids(kind)
		sort.Strings(ids)
		return ok(withSlashes(ids))
	})

	for _, id := range s.store.ids(kind) {
		s.mountEndpoint(srv, root, kind, id)
	}

	// Bulk: one POST carrying an array of {id, params}. The spec's
	// value is atomicity of INTENT — every endpoint in the array is
	// staged (or activated) from a single request, which is how a
	// controller switches several receivers as one operation.
	srv.Handle(stdhttp.MethodPost, base+"/bulk/"+kind+"/", func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		return s.handleBulk(kind, r)
	})
	srv.Handle(stdhttp.MethodGet, base+"/bulk/"+kind+"/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok([]string{})
	})
}

func (s *IS05ConnectionServer) mountEndpoint(srv *httpsession.Server, root, kind, id string) {
	ok := func(v any) (int, any, error) { return stdhttp.StatusOK, v, nil }
	p := root + "/" + id

	// The per-endpoint index. Receivers omit transportfile — the
	// controller pushes the SDP in, it is never served out.
	index := []string{"constraints/", "staged/", "active/", "transporttype/"}
	if kind == "senders" {
		index = []string{"constraints/", "staged/", "active/", "transportfile/", "transporttype/"}
	}
	srv.Handle(stdhttp.MethodGet, p+"/", func(context.Context, *stdhttp.Request) (int, any, error) {
		return ok(index)
	})

	srv.Handle(stdhttp.MethodGet, p+"/constraints/", func(context.Context, *stdhttp.Request) (int, any, error) {
		e, err := s.store.get(kind, id)
		if err != nil {
			return notFound(err)
		}
		return ok(e.constraints)
	})

	srv.Handle(stdhttp.MethodGet, p+"/transporttype/", func(context.Context, *stdhttp.Request) (int, any, error) {
		e, err := s.store.get(kind, id)
		if err != nil {
			return notFound(err)
		}
		return ok(e.transportType)
	})

	srv.Handle(stdhttp.MethodGet, p+"/staged/", func(context.Context, *stdhttp.Request) (int, any, error) {
		e, err := s.store.get(kind, id)
		if err != nil {
			return notFound(err)
		}
		return ok(viewOf(kind, e.staged))
	})

	srv.Handle(stdhttp.MethodGet, p+"/active/", func(context.Context, *stdhttp.Request) (int, any, error) {
		e, err := s.store.get(kind, id)
		if err != nil {
			return notFound(err)
		}
		return ok(viewOf(kind, e.active))
	})

	if kind == "senders" {
		// transportfile is text/plain SDP, not JSON. A sender with no
		// transport file answers 404 rather than an empty body: the
		// spec distinguishes "this sender has no transport file" from
		// "here is an empty one".
		srv.Handle(stdhttp.MethodGet, p+"/transportfile/", func(context.Context, *stdhttp.Request) (int, any, error) {
			e, err := s.store.get(kind, id)
			if err != nil {
				return notFound(err)
			}
			if e.transportFile == "" {
				return stdhttp.StatusNotFound, httpsession.ErrorBody{
					Code:  404,
					Error: "No transport file",
					Debug: "this sender has no transport file; RTP senders publish SDP here once activated",
				}, nil
			}
			return ok(e.transportFile)
		})
	}

	srv.Handle(stdhttp.MethodPatch, p+"/staged/", func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		return s.handlePatch(kind, id, r)
	})
}

// handlePatch decodes a staged PATCH and applies it.
func (s *IS05ConnectionServer) handlePatch(kind, id string, r *stdhttp.Request) (int, any, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return stdhttp.StatusBadRequest, httpsession.ErrorBody{Code: 400, Error: "Unreadable body", Debug: err.Error()}, nil
	}
	patch, present, err := decodePatch(raw)
	if err != nil {
		return stdhttp.StatusBadRequest, httpsession.ErrorBody{Code: 400, Error: "Invalid staged body", Debug: err.Error()}, nil
	}
	out, status, err := s.store.applyPatch(kind, id, patch, present)
	if err != nil {
		return status, httpsession.ErrorBody{Code: status, Error: stdhttp.StatusText(status), Debug: err.Error()}, nil
	}
	return status, viewOf(kind, out), nil
}

// decodePatch parses a staged PATCH into the canonical struct AND the
// set of fields the body actually carried.
//
// Both are needed: the struct for values, the presence set because
// master_enable is a plain bool and "absent" must not read as "false".
func decodePatch(raw []byte) (is05.StagedSender, patchFields, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return is05.StagedSender{}, patchFields{}, err
	}
	var patch is05.StagedSender
	if err := json.Unmarshal(raw, &patch); err != nil {
		return is05.StagedSender{}, patchFields{}, err
	}
	_, hasSender := probe["sender_id"]
	_, hasReceiver := probe["receiver_id"]
	return patch, patchFields{
		MasterEnable:    hasKey(probe, "master_enable"),
		TransportParams: hasKey(probe, "transport_params"),
		TransportFile:   hasKey(probe, "transport_file"),
		SenderID:        hasSender,
		ReceiverID:      hasReceiver,
	}, nil
}

func hasKey(m map[string]json.RawMessage, k string) bool {
	_, ok := m[k]
	return ok
}

// bulkItem is one entry of a bulk POST body.
type bulkItem struct {
	ID     string          `json:"id"`
	Params json.RawMessage `json:"params"`
}

// handleBulk applies a bulk POST.
//
// The response is an array of per-id {id, code}, NOT a single status:
// a bulk request can partially succeed, and collapsing that into one
// code would leave the controller unable to tell which endpoint
// refused.
func (s *IS05ConnectionServer) handleBulk(kind string, r *stdhttp.Request) (int, any, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<22))
	if err != nil {
		return stdhttp.StatusBadRequest, httpsession.ErrorBody{Code: 400, Error: "Unreadable body", Debug: err.Error()}, nil
	}
	var items []bulkItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return stdhttp.StatusBadRequest, httpsession.ErrorBody{
			Code: 400, Error: "Invalid bulk body",
			Debug: "expected an array of {id, params}: " + err.Error(),
		}, nil
	}
	type result struct {
		ID   string `json:"id"`
		Code int    `json:"code"`
		Err  string `json:"error,omitempty"`
	}
	out := make([]result, 0, len(items))
	for _, it := range items {
		patch, present, derr := decodePatch(it.Params)
		if derr != nil {
			out = append(out, result{ID: it.ID, Code: 400, Err: derr.Error()})
			continue
		}
		_, code, aerr := s.store.applyPatch(kind, it.ID, patch, present)
		res := result{ID: it.ID, Code: code}
		if aerr != nil {
			res.Err = aerr.Error()
		}
		out = append(out, res)
	}
	return stdhttp.StatusOK, out, nil
}

// viewOf renders the stored state as the right shape for the
// collection: a receiver carries sender_id, a sender receiver_id.
// Serving one in place of the other is a schema failure a controller
// will reject outright.
func viewOf(kind string, s is05.StagedSender) any {
	if kind == "senders" {
		return s
	}
	r := is05.StagedReceiver{
		MasterEnableField: s.MasterEnableField,
		Activation:        s.Activation,
		TransportParams:   s.TransportParams,
		TransportFile:     s.TransportFile,
	}
	// The sender feeding this receiver is held in the shared struct's
	// ReceiverID slot; the receiver view names it sender_id.
	r.SenderID = s.ReceiverID
	return r
}

func withSlashes(ids []string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = strings.TrimSuffix(id, "/") + "/"
	}
	return out
}

func notFound(err error) (int, any, error) {
	return stdhttp.StatusNotFound, httpsession.ErrorBody{
		Code: 404, Error: "Not found", Debug: err.Error(),
	}, nil
}

