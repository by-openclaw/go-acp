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
	"net"
	stdhttp "net/http"
	"sort"
	"strings"

	"dhs/internal/amwa/codec/is04"
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
	// bundle is the IS-04 side of the same device. IS-05 §4.1 makes
	// these two views of ONE set of resources, so generating a
	// Sender's SDP and reflecting an activation into
	// `subscription.active` both read from here.
	bundle *NodeConfig
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
	s := &IS05ConnectionServer{logger: logger, store: st, vers: vers, bundle: bundle}
	// Every activation regenerates the Sender's SDP and pushes
	// master_enable back into the IS-04 resource. Both are things a
	// controller reads on the OTHER API, and neither can be computed
	// from connection state alone.
	st.onPromote = s.afterActivation
	return s
}

// afterActivation reflects one activation across both APIs and returns
// the transport file the endpoint serves from now on.
//
// Runs under the store lock, so it must not call back into the store.
func (s *IS05ConnectionServer) afterActivation(kind, id string, active is05.StagedSender) string {
	s.updateIS04Subscription(kind, id, active)
	if kind == "senders" {
		return s.sdpForSender(id, active)
	}
	return ""
}

// updateIS04Subscription writes an IS-05 activation into the IS-04
// resource's `subscription` block.
//
// IS-04 §5 defines `subscription.active` as "the Sender/Receiver is
// currently enabled", and IS-05 §5.1 defines `master_enable` as the
// same fact. They are one state with two spellings, and a controller
// that reads the Query API to render which receivers are live sees
// only the IS-04 one. Leaving it false after a successful activation
// makes an active receiver look idle everywhere except the Connection
// API (IS-05-02 test_07/test_08).
func (s *IS05ConnectionServer) updateIS04Subscription(kind, id string, active is05.StagedSender) {
	if s.bundle == nil {
		return
	}
	// The far end, when one was staged AND the endpoint is enabled.
	//
	// master_enable false is "parked": the endpoint remembers the id it
	// was pointed at, but IS-04 `subscription` describes what is
	// happening, not what was configured. A parked receiver that still
	// names a sender_id reads to a controller as a live connection and
	// makes the route look occupied (IS-05-02 test_08/test_11). An
	// empty string is likewise spelled null on the wire.
	var peer *string
	if active.MasterEnable && active.ReceiverID != nil && *active.ReceiverID != "" {
		v := *active.ReceiverID
		peer = &v
	}
	now := is05.FormatTAINow(s.store.now())
	if kind == "senders" {
		for i := range s.bundle.Senders {
			if s.bundle.Senders[i].ID != id {
				continue
			}
			s.bundle.Senders[i].Subscription = is04.SenderSubscription{
				ReceiverID: peer,
				Active:     active.MasterEnable,
			}
			// A changed resource is a NEW version. A registry
			// deduplicates on this field, so an update that reuses the
			// old version is discarded as a replay and the change
			// never reaches a controller.
			s.bundle.Senders[i].Version = now
			return
		}
		return
	}
	for i := range s.bundle.Receivers {
		if s.bundle.Receivers[i].ID != id {
			continue
		}
		s.bundle.Receivers[i].Subscription = is04.ReceiverSubscription{
			SenderID: peer,
			Active:   active.MasterEnable,
		}
		s.bundle.Receivers[i].Version = now
		return
	}
}

// firstNodeIP picks the address ACTIVE parameters should name.
//
// The Node API already publishes the endpoints it is reachable on, so
// that list is the honest source — inventing an address here could
// name an interface the Node does not serve on.
func firstNodeIP(cfg *NodeConfig) string {
	// An IP LITERAL wins over a hostname, even a hostname listed first.
	//
	// Two callers depend on this being a literal. IS-05 `source_ip`
	// and `interface_ip` are typed as addresses by the constraints
	// schema, so a hostname there fails validation outright. And the
	// sr-ctrl control href is compared by a controller against the
	// address it reached us on, which is an IP. The endpoint list
	// legitimately carries both forms -- IS-04 wants every name the
	// Node answers to -- so picking the first entry would be a coin
	// toss on ordering.
	for _, ep := range cfg.Node.API.Endpoints {
		if ep.Host != "" && net.ParseIP(ep.Host) != nil {
			return ep.Host
		}
	}
	for _, ep := range cfg.Node.API.Endpoints {
		if ep.Host != "" {
			return ep.Host
		}
	}
	return ""
}

// senderSDP renders one Sender's transport file, or "" when there is
// no Connection API to render it from. Used by the IS-04 side so both
// APIs serve one generator's output.
func (s *IS04NodeServer) senderSDP(id string) string {
	if s.connection == nil {
		return ""
	}
	e, err := s.connection.Store().get("senders", id)
	if err != nil {
		return ""
	}
	if sdp := s.connection.sdpForSender(id, e.active); sdp != "" {
		return sdp
	}
	return e.transportFile
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
	// No GET on a bulk endpoint. IS-05 defines bulk as POST-only, and
	// the tool checks for 405 specifically (test_34/35): answering 200
	// with an empty array invents a collection that does not exist.
	// The server derives 405 from "the path is routed for another
	// method", so registering nothing here is what produces it.
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
		// The BASE transport URN, not the IS-04 sub-type.
		//
		// IS-04 distinguishes rtp.mcast from rtp.ucast; IS-05 does not
		// — its transport_params and constraints schemas are keyed on
		// "rtp" alone. Returning the sub-type makes the tool report
		// "Unsupported transport type ... for staged/constraints
		// validation" and skip the checks entirely (test_15/16).
		return ok(is05TransportType(e.transportType))
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
			// Rendered from ACTIVE state on demand, with the value
			// cached at activation as the fallback.
			//
			// Serving only the cached copy would 404 every sender that
			// has not been activated yet, and a controller reads the
			// SDP to DECIDE whether to connect — before any activation
			// has happened. ACTIVE always describes a full set of
			// addresses (auto is resolved at seed time), so there is
			// always an honest file to render.
			sdp := s.sdpForSender(id, e.active)
			if sdp == "" {
				sdp = e.transportFile
			}
			if sdp == "" {
				return stdhttp.StatusNotFound, httpsession.ErrorBody{
					Code:  404,
					Error: "No transport file",
					Debug: "this sender has no transport file; only RTP senders publish SDP here",
				}, nil
			}
			// application/sdp, never JSON: a controller PATCHes this
			// body verbatim into a receiver's transport_file.data, and
			// a JSON-quoted string is not an SDP.
			return stdhttp.StatusOK, &httpsession.RawBody{
				ContentType: "application/sdp",
				Body:        []byte(sdp),
			}, nil
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
	// A receiver PATCH names the far end "sender_id", which the shared
	// StagedSender struct does not have a field for. Lift it into the
	// one id slot the struct does carry; viewOf renames it back on the
	// way out.
	if hasSender {
		var rv is05.StagedReceiver
		if json.Unmarshal(raw, &rv) == nil {
			patch.ReceiverID = rv.SenderID
		}
	}
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
	// A receiver that has never been given an SDP still publishes the
	// object, with both members null. receiver-stage-schema types
	// transport_file as an OBJECT; omitting it, or sending JSON null
	// in its place, fails validation on every read of staged -- which
	// is most of the API.
	if r.TransportFile == nil {
		r.TransportFile = &is05.TransportFile{}
	}
	return r
}

// is05TransportType maps an IS-04 transport URN onto the one IS-05
// knows. IS-04 splits RTP into mcast and ucast; IS-05's schemas are
// keyed on the base URN only.
func is05TransportType(t string) string {
	if isRTP(t) {
		return "urn:x-nmos:transport:rtp"
	}
	return t
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
