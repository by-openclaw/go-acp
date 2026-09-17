package consumer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is04"
	v13 "dhs/internal/amwa/codec/is04/v13"
	"dhs/internal/amwa/codec/spec"
	httpsession "dhs/internal/amwa/session/http"
	"dhs/internal/amwa/session/query"
)

// wsGrainJSON renders an IS-04 §5.2 SYNC grain for one collection. Each
// row's `post` is embedded verbatim, so a caller can mix a codec-encoded
// resource with a deliberately undecodable row.
func wsGrainJSON(topic string, posts map[string]json.RawMessage) []byte {
	type row struct {
		Path string          `json:"path"`
		Post json.RawMessage `json:"post"`
	}
	var rows []row
	for path, post := range posts {
		rows = append(rows, row{Path: path, Post: post})
	}
	g := map[string]any{
		"grain": map[string]any{
			"type":  "urn:x-nmos:format:data.event",
			"topic": topic,
			"data":  rows,
		},
	}
	b, _ := json.Marshal(g)
	return b
}

// subscriptionServer stands up a Query API that answers the /subscriptions
// POST and upgrades /ws, pushing one SYNC grain then holding the socket
// open until the client's quiet-period timeout closes it. grain is the
// SYNC payload; a 500 on the POST is requested via failSubscribe.
func subscriptionServer(t *testing.T, grain []byte, failSubscribe bool) *query.Client {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions"):
			if failSubscribe {
				http.Error(w, "cannot subscribe", http.StatusInternalServerError)
				return
			}
			wsHref := strings.Replace(srv.URL, "http://", "ws://", 1) + "/ws"
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"sub-1","ws_href":"` + wsHref + `","resource_path":"/x","params":{},` +
				`"persist":false,"max_update_rate_ms":100,"secure":false}`))
		case strings.HasSuffix(r.URL.Path, "/ws"):
			ws, err := httpsession.AcceptWebSocket(w, r)
			if err != nil {
				return
			}
			defer func() { _ = ws.Close() }()
			if err := ws.SendText(grain); err != nil {
				return
			}
			// Hold the socket until the client finishes reading and
			// closes (its 800ms quiet-period timeout). The read returns
			// an error then, which is our cue to release the handler.
			for {
				if _, err := ws.ReadText(); err != nil {
					return
				}
			}
		default:
			http.Error(w, "unexpected "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c, err := query.NewClient(srv.URL, v13.New())
	if err != nil {
		t.Fatalf("query.NewClient: %v", err)
	}
	return c
}

func ctrlWith(c *query.Client) *Controller {
	return &Controller{reporter: &spec.SliceReporter{}, client: c}
}

// TestSendersViaSubscription: the v1.0 enumeration path reads the SYNC
// snapshot off a WebSocket subscription. A decodable sender row reaches
// the caller; a row the codec rejects costs only that one resource, not
// the whole listing.
func TestSendersViaSubscription(t *testing.T) {
	good, err := v13.New().EncodeSender(senderOn(uuidN(1), "CAM 1", testUUID, is04.TransportRTPMcast))
	if err != nil {
		t.Fatalf("encode sender: %v", err)
	}
	grain := wsGrainJSON("/senders/", map[string]json.RawMessage{
		uuidN(1): json.RawMessage(good),
		"bad":    json.RawMessage(`"not a sender object"`), // fails DecodeSender
	})
	ctrl := ctrlWith(subscriptionServer(t, grain, false))

	got, err := ctrl.SendersViaSubscription(context.Background())
	if err != nil {
		t.Fatalf("SendersViaSubscription: %v", err)
	}
	if len(got) != 1 || got[0].ID != uuidN(1) {
		t.Fatalf("got %+v, want exactly the one decodable sender", got)
	}
}

// TestReceiversViaSubscription is the receiver twin: same snapshot
// mechanism, same one-bad-row-does-not-sink-the-listing contract.
func TestReceiversViaSubscription(t *testing.T) {
	good, err := v13.New().EncodeReceiver(receiverOn(uuidN(2), testUUID, is04.TransportRTPMcast))
	if err != nil {
		t.Fatalf("encode receiver: %v", err)
	}
	grain := wsGrainJSON("/receivers/", map[string]json.RawMessage{
		uuidN(2): json.RawMessage(good),
		"bad":    json.RawMessage(`12345`), // a number is not a receiver object
	})
	ctrl := ctrlWith(subscriptionServer(t, grain, false))

	got, err := ctrl.ReceiversViaSubscription(context.Background())
	if err != nil {
		t.Fatalf("ReceiversViaSubscription: %v", err)
	}
	if len(got) != 1 || got[0].ID != uuidN(2) {
		t.Fatalf("got %+v, want exactly the one decodable receiver", got)
	}
}

// TestViaSubscriptionErrors: a subscription the Registry refuses is
// surfaced by both enumeration verbs rather than returning an empty
// (and misleading) listing.
func TestViaSubscriptionErrors(t *testing.T) {
	t.Run("senders", func(t *testing.T) {
		ctrl := ctrlWith(subscriptionServer(t, nil, true))
		if _, err := ctrl.SendersViaSubscription(context.Background()); err == nil {
			t.Fatal("a refused subscription must be an error")
		}
	})
	t.Run("receivers", func(t *testing.T) {
		ctrl := ctrlWith(subscriptionServer(t, nil, true))
		if _, err := ctrl.ReceiversViaSubscription(context.Background()); err == nil {
			t.Fatal("a refused subscription must be an error")
		}
	})
}
