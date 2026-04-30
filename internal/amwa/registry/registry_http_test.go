package registry

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"acp/internal/amwa/codec/is04"
	httpsession "acp/internal/amwa/session/http"
)

// startRegistryHTTP boots only the HTTP face (no mDNS, no GC) for
// unit testing. Returns the bind address.
func startRegistryHTTP(t *testing.T, store *Store, mgr *SubscriptionManager) (string, func()) {
	t.Helper()
	srv := httpsession.NewServer(nil)
	installRegistrationRoutes(srv, store, "/x-nmos/registration/v1.3")
	installQueryRoutes(srv, store, mgr, "/x-nmos/query/v1.3")

	mux := stdhttp.NewServeMux()
	routeTable := srv.MuxHandler()
	if mgr != nil {
		// Subtree match `/subscriptions/` covers BOTH the upgrade path
		// (`/subscriptions/<id>/ws`) AND the REST endpoints
		// (`/subscriptions` + `/subscriptions`). The upgrade handler
		// delegates to the route table whenever the URL doesn't end in
		// `/ws`, so REST + WS share the same prefix without redirect
		// shenanigans.
		upgrade := mgr.UpgradeHandler("/x-nmos/query/v1.3")
		mux.HandleFunc("/x-nmos/query/v1.3/subscriptions/", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if strings.HasSuffix(r.URL.Path, "/ws") {
				upgrade(w, r)
				return
			}
			// ServeMux redirects no-trailing-slash → trailing-slash for
			// our subtree pattern. The route table has the exact-match
			// path without the slash, so strip before delegating.
			if strings.HasSuffix(r.URL.Path, "/") {
				r2 := r.Clone(r.Context())
				r2.URL.Path = strings.TrimRight(r.URL.Path, "/")
				routeTable.ServeHTTP(w, r2)
				return
			}
			routeTable.ServeHTTP(w, r)
		})
	}
	mux.Handle("/", routeTable)

	ts := httptest.NewServer(mux)
	return strings.TrimPrefix(ts.URL, "http://"), ts.Close
}

func TestRegistrationAPIRoundTrip(t *testing.T) {
	store := NewStore()
	addr, stop := startRegistryHTTP(t, store, nil)
	defer stop()
	base := "http://" + addr + "/x-nmos/registration/v1.3"

	// POST a Node.
	n := validNode("f47ac10b-58cc-4372-a567-0e02b2c3d479")
	body, err := is04.EncodeRegistration(is04.ResourceNode, n)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	resp, err := stdhttp.Post(base+"/resource", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != stdhttp.StatusCreated {
		bb, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST status %d: %s", resp.StatusCode, bb)
	}

	r2, err := stdhttp.Get(base + "/resource/nodes/" + n.ID)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = r2.Body.Close() }()
	if r2.StatusCode != 200 {
		t.Fatalf("GET status %d", r2.StatusCode)
	}

	r3, err := stdhttp.Post(base+"/health/nodes/"+n.ID, "application/json", nil)
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	defer func() { _ = r3.Body.Close() }()
	if r3.StatusCode != 200 {
		t.Fatalf("heartbeat status %d", r3.StatusCode)
	}

	req, _ := stdhttp.NewRequest(stdhttp.MethodDelete, base+"/resource/nodes/"+n.ID, nil)
	r4, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer func() { _ = r4.Body.Close() }()
	if r4.StatusCode != stdhttp.StatusNoContent {
		t.Fatalf("DELETE status %d", r4.StatusCode)
	}
	if len(store.ListNodes()) != 0 {
		t.Fatalf("expected store empty after DELETE")
	}
}

func TestQueryAPIList(t *testing.T) {
	store := NewStore()
	mgr := NewSubscriptionManager(nil, store, "127.0.0.1:8235", "v1.3")
	addr, stop := startRegistryHTTP(t, store, mgr)
	defer stop()

	_ = store.PutNode(validNode("f47ac10b-58cc-4372-a567-0e02b2c3d479"))

	resp, err := stdhttp.Get("http://" + addr + "/x-nmos/query/v1.3/nodes")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var nodes []is04.Node
	if err := json.Unmarshal(body, &nodes); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("nodes = %v", nodes)
	}
}

func TestQueryAPILabelFilter(t *testing.T) {
	store := NewStore()
	mgr := NewSubscriptionManager(nil, store, "127.0.0.1:8235", "v1.3")
	addr, stop := startRegistryHTTP(t, store, mgr)
	defer stop()

	a := validNode("f47ac10b-58cc-4372-a567-0e02b2c3d479")
	a.Label = "alpha"
	b := validNode("e5d3a888-2222-4333-8444-555555555555")
	b.Label = "beta"
	_ = store.PutNode(a)
	_ = store.PutNode(b)

	resp, err := stdhttp.Get("http://" + addr + "/x-nmos/query/v1.3/nodes?label=alpha")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	var nodes []is04.Node
	_ = json.Unmarshal(body, &nodes)
	if len(nodes) != 1 || nodes[0].Label != "alpha" {
		t.Fatalf("filter result = %v", nodes)
	}
}

func TestSubscriptionPostReturnsWSHref(t *testing.T) {
	store := NewStore()
	mgr := NewSubscriptionManager(nil, store, "10.6.239.113:8235", "v1.3")
	addr, stop := startRegistryHTTP(t, store, mgr)
	defer stop()

	body, _ := json.Marshal(SubscriptionRequest{ResourcePath: "/nodes", Persist: false})
	resp, err := stdhttp.Post("http://"+addr+"/x-nmos/query/v1.3/subscriptions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 201 {
		bb, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, bb)
	}
	bb, _ := io.ReadAll(resp.Body)
	var res SubscriptionResource
	if err := json.Unmarshal(bb, &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(res.WSHref, "ws://10.6.239.113:8235/x-nmos/query/v1.3/subscriptions/") {
		t.Fatalf("ws_href = %q", res.WSHref)
	}
	if !strings.HasSuffix(res.WSHref, "/ws") {
		t.Fatalf("ws_href should end with /ws: %q", res.WSHref)
	}
}

func TestWebSocketSubscriptionDelivers(t *testing.T) {
	store := NewStore()
	mgr := NewSubscriptionManager(nil, store, "127.0.0.1:0", "v1.3")
	addr, stop := startRegistryHTTP(t, store, mgr)
	defer stop()

	_ = store.PutNode(validNode("f47ac10b-58cc-4372-a567-0e02b2c3d479"))

	// Create a subscription via REST first.
	body, _ := json.Marshal(SubscriptionRequest{ResourcePath: "/nodes"})
	resp, err := stdhttp.Post("http://"+addr+"/x-nmos/query/v1.3/subscriptions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST sub: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	bb, _ := io.ReadAll(resp.Body)
	var sub SubscriptionResource
	_ = json.Unmarshal(bb, &sub)
	if sub.ID == "" {
		t.Fatalf("sub create body = %s", bb)
	}

	// Open a raw TCP conn + perform the upgrade handshake by hand.
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	wsPath := "/x-nmos/query/v1.3/subscriptions/" + sub.ID + "/ws"
	upgrade := "GET " + wsPath + " HTTP/1.1\r\n" +
		"Host: " + addr + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := conn.Write([]byte(upgrade)); err != nil {
		t.Fatalf("write upgrade: %v", err)
	}

	// Read the upgrade response (status + headers up to blank line).
	buf := make([]byte, 4096)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read upgrade: %v", err)
	}
	respHead := string(buf[:n])
	if !strings.Contains(respHead, "101 Switching Protocols") {
		t.Fatalf("upgrade response = %s", respHead)
	}
	expected := wsAcceptKey("dGhlIHNhbXBsZSBub25jZQ==")
	if !strings.Contains(respHead, expected) {
		t.Fatalf("upgrade missing accept key %q: %s", expected, respHead)
	}

	// Compute where the upgrade headers ended; the rest is the first
	// frame (the sync grain for the existing Node).
	hdrEnd := strings.Index(respHead, "\r\n\r\n")
	if hdrEnd < 0 {
		t.Fatalf("upgrade response missing terminator")
	}
	remaining := append([]byte(nil), buf[hdrEnd+4:n]...)
	// Pull additional bytes off the wire until we have enough for a
	// frame header + payload. A 1.5 s deadline is plenty for a
	// loopback read of a sync grain (~1 KB).
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(remaining) >= 2 {
			plen := int(remaining[1] & 0x7F)
			off := 2
			switch plen {
			case 126:
				if len(remaining) >= 4 {
					plen = int(binary.BigEndian.Uint16(remaining[2:4]))
					off = 4
				}
			case 127:
				if len(remaining) >= 10 {
					plen = int(binary.BigEndian.Uint64(remaining[2:10]))
					off = 10
				}
			}
			if len(remaining) >= off+plen && plen > 0 {
				break
			}
		}
		extra := make([]byte, 4096)
		_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		k, err := conn.Read(extra)
		if k > 0 {
			remaining = append(remaining, extra[:k]...)
		}
		if err != nil {
			break
		}
	}

	// Decode the first frame manually — server frames are unmasked.
	opcode, payload, err := readUnmaskedFrame(remaining)
	if err != nil {
		t.Fatalf("frame decode: %v (have %d bytes)", err, len(remaining))
	}
	if opcode != 0x1 {
		t.Fatalf("first frame opcode = 0x%x, want text", opcode)
	}
	if !strings.Contains(string(payload), `"topic":"/nodes/"`) {
		t.Fatalf("sync grain missing /nodes/ topic: %s", payload)
	}
}

// readUnmaskedFrame parses one server-side text frame from raw bytes.
func readUnmaskedFrame(b []byte) (byte, []byte, error) {
	if len(b) < 2 {
		return 0, nil, io.ErrUnexpectedEOF
	}
	opcode := b[0] & 0x0F
	plen := int(b[1] & 0x7F)
	off := 2
	switch plen {
	case 126:
		if len(b) < off+2 {
			return 0, nil, io.ErrUnexpectedEOF
		}
		plen = int(binary.BigEndian.Uint16(b[off : off+2]))
		off += 2
	case 127:
		if len(b) < off+8 {
			return 0, nil, io.ErrUnexpectedEOF
		}
		plen = int(binary.BigEndian.Uint64(b[off : off+8]))
		off += 8
	}
	if len(b) < off+plen {
		return 0, nil, io.ErrUnexpectedEOF
	}
	return opcode, b[off : off+plen], nil
}

// wsAcceptKey is duplicated from the http package (it's lowercase
// there) for test convenience.
func wsAcceptKey(secKey string) string {
	const guid = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	h := sha1.New()
	h.Write([]byte(secKey + guid))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
