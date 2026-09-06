package acp1

import (
	"context"
	"dhs/internal/transport"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// adminDialTimeout bounds a single admin request's connect. The admin socket
// is on loopback, so this only ever fires when the producer is gone.
const adminDialTimeout = 5 * time.Second

// AdminRequest is one JSON command from a CLI client. Verbs map 1:1 to
// the issue's `dhs producer acp1 admin <verb> ...` surface. The Args map
// carries verb-specific parameters.
type AdminRequest struct {
	Verb string         `json:"verb"`
	Args map[string]any `json:"args,omitempty"`
}

// AdminResponse is the reply envelope. Status is "ok" or "error";
// when "error", Message carries the diagnostic. Result is verb-specific
// success payload (e.g. value-get returns {"value": ...}).
type AdminResponse struct {
	Status  string         `json:"status"`
	Message string         `json:"message,omitempty"`
	Result  map[string]any `json:"result,omitempty"`
}

// AdminDiscovery is the file written by the running provider so the
// admin CLI can find the listening port without hard-coding it.
type AdminDiscovery struct {
	Name      string    `json:"name"`
	Addr      string    `json:"addr"`
	StartedAt time.Time `json:"started_at"`
	PID       int       `json:"pid"`
}

// adminMaxFrame caps one length-prefixed JSON request to 64 KiB.
// Bigger payloads fall outside the admin surface (slot load with a
// massive DM schema goes through the DM library, not the wire).
const adminMaxFrame = 64 * 1024

// ServeAdmin runs the admin RPC server. It listens on TCP loopback at a
// kernel-assigned port and writes a discovery file at
// $TMPDIR/dhs-acp1-<name>.json so the matching CLI can find it.
//
// Returns when ctx is cancelled. The discovery file is removed on exit.
func (s *server) ServeAdmin(ctx context.Context, name string) error {
	if name == "" {
		name = "dhs-acp1"
	}
	// Loopback-only by design: the admin socket is a local control channel,
	// never a network surface. The bind goes through transport; the shared
	// socket policy is applied to each accepted connection below, which is
	// also what covers a listener injected by adminListenHook.
	listen := func() (net.Listener, error) {
		ln, lerr := transport.ListenTCP(ctx, "tcp4", "127.0.0.1:0", transport.SocketOptions{})
		if lerr != nil {
			return nil, lerr
		}
		return ln.Listener, nil
	}
	if s.adminListenHook != nil {
		listen = s.adminListenHook
	}
	ln, err := listen()
	if err != nil {
		return fmt.Errorf("acp1 admin: listen: %w", err)
	}
	addr := ln.Addr().String()

	disc := AdminDiscovery{
		Name:      name,
		Addr:      addr,
		StartedAt: time.Now().UTC(),
		PID:       os.Getpid(),
	}
	discPath := AdminDiscoveryPath(name)
	writeDisc := writeAdminDiscovery
	if s.adminWriteDiscoveryHook != nil {
		writeDisc = s.adminWriteDiscoveryHook
	}
	if err := writeDisc(discPath, &disc); err != nil {
		_ = ln.Close()
		return fmt.Errorf("acp1 admin: discovery file: %w", err)
	}
	defer func() { _ = os.Remove(discPath) }()

	s.logger.Info("acp1 admin listening",
		slog.String("name", name),
		slog.String("addr", addr),
		slog.String("discovery", discPath))

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if isClosed(err) || errors.Is(ctx.Err(), context.Canceled) {
				return nil
			}
			s.logger.Warn("acp1 admin accept", slog.String("err", err.Error()))
			continue
		}
		go s.handleAdminConn(ctx, conn)
	}
}

// AdminDiscoveryPath returns the platform-specific discovery file path
// for the named instance. Exported so the admin CLI can locate the
// running provider.
func AdminDiscoveryPath(name string) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("dhs-acp1-%s.json", sanitiseName(name)))
}

func writeAdminDiscovery(path string, d *AdminDiscovery) error {
	// unreachable error: AdminDiscovery is all primitive/string/time fields,
	// which json.MarshalIndent never fails to encode.
	raw, _ := json.MarshalIndent(d, "", "  ")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ReadAdminDiscovery is used by the admin CLI client to find a running
// provider's address.
func ReadAdminDiscovery(name string) (*AdminDiscovery, error) {
	raw, err := os.ReadFile(AdminDiscoveryPath(name))
	if err != nil {
		return nil, err
	}
	var d AdminDiscovery
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("admin discovery decode: %w", err)
	}
	return &d, nil
}

// handleAdminConn serves one admin client connection: read a single
// length-prefixed JSON request, dispatch via handleAdminRequest, write
// the reply, close. Keep-alive isn't needed — admin calls are rare.
func (s *server) handleAdminConn(ctx context.Context, conn net.Conn) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	var lenBuf [4]byte
	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		return
	}
	mlen := binary.BigEndian.Uint32(lenBuf[:])
	if mlen == 0 || mlen > adminMaxFrame {
		s.writeAdminErr(conn, fmt.Errorf("frame size %d out of range", mlen))
		return
	}
	body := make([]byte, mlen)
	if _, err := io.ReadFull(conn, body); err != nil {
		return
	}

	var req AdminRequest
	if err := json.Unmarshal(body, &req); err != nil {
		s.writeAdminErr(conn, fmt.Errorf("decode: %w", err))
		return
	}
	resp := s.handleAdminRequest(ctx, &req)
	s.writeAdminResp(conn, resp)
}

// writeAdminResp emits a length-prefixed JSON reply.
func (s *server) writeAdminResp(conn net.Conn, resp *AdminResponse) {
	raw, err := json.Marshal(resp)
	if err != nil {
		raw, _ = json.Marshal(&AdminResponse{Status: "error", Message: err.Error()})
	}
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(raw)))
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, _ = conn.Write(lenBuf[:])
	_, _ = conn.Write(raw)
}

func (s *server) writeAdminErr(conn net.Conn, err error) {
	s.writeAdminResp(conn, &AdminResponse{Status: "error", Message: err.Error()})
}

// AdminClient is the in-process side of the admin RPC. The CLI uses
// this; tests use it too. One round-trip per call.
type AdminClient struct {
	addr string

	// dial overrides the transport dial. Nil ⇒ transport.TCPDialer. Test-only
	// injection seam so the request write-error and reply read-error arms of
	// Call are deterministically reachable with a scripted net.Conn (a real
	// loopback peer almost never fails a write synchronously).
	dial func() (net.Conn, error)
}

// NewAdminClient resolves a discovery file and returns a client.
func NewAdminClient(name string) (*AdminClient, error) {
	d, err := ReadAdminDiscovery(name)
	if err != nil {
		return nil, err
	}
	return &AdminClient{addr: d.Addr}, nil
}

// Call sends one request and returns the response. Error reflects
// transport failures; AdminResponse.Status carries application-level
// outcome.
func (c *AdminClient) Call(ctx context.Context, req *AdminRequest) (*AdminResponse, error) {
	dial := c.dial
	if dial == nil {
		dial = func() (net.Conn, error) {
			return transport.TCPDialer{Timeout: adminDialTimeout}.
				DialContext(ctx, "tcp4", c.addr)
		}
	}
	conn, err := dial()
	if err != nil {
		return nil, fmt.Errorf("admin dial: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	} else {
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(body)))
	if _, err := conn.Write(lenBuf[:]); err != nil {
		return nil, fmt.Errorf("admin write: %w", err)
	}
	if _, err := conn.Write(body); err != nil {
		return nil, fmt.Errorf("admin write body: %w", err)
	}

	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		return nil, fmt.Errorf("admin read len: %w", err)
	}
	rlen := binary.BigEndian.Uint32(lenBuf[:])
	if rlen > adminMaxFrame {
		return nil, fmt.Errorf("admin reply size %d > max %d", rlen, adminMaxFrame)
	}
	respBody := make([]byte, rlen)
	if _, err := io.ReadFull(conn, respBody); err != nil {
		return nil, fmt.Errorf("admin read body: %w", err)
	}
	var resp AdminResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("admin decode: %w", err)
	}
	return &resp, nil
}

// sanitiseName trims a free-text instance name into a filesystem-safe
// component. Falls back to "dhs-acp1" if nothing usable remains.
func sanitiseName(name string) string {
	var b []byte
	for _, c := range []byte(name) {
		switch {
		case c >= 'A' && c <= 'Z',
			c >= 'a' && c <= 'z',
			c >= '0' && c <= '9',
			c == '.', c == '_', c == '-':
			b = append(b, c)
		}
	}
	if len(b) == 0 {
		return "dhs-acp1"
	}
	return string(b)
}

// adminHandlers is the registry of verb -> handler functions. Single
// global so tests + tree-load reload share the same set.
type adminHandler func(ctx context.Context, s *server, args map[string]any) (*AdminResponse, error)

var adminHandlers = map[string]adminHandler{}
var adminHandlersMu sync.RWMutex

func registerAdminHandler(verb string, h adminHandler) {
	adminHandlersMu.Lock()
	adminHandlers[verb] = h
	adminHandlersMu.Unlock()
}

func lookupAdminHandler(verb string) (adminHandler, bool) {
	adminHandlersMu.RLock()
	defer adminHandlersMu.RUnlock()
	h, ok := adminHandlers[verb]
	return h, ok
}

// handleAdminRequest dispatches one decoded request to the right
// handler. Returns a non-nil AdminResponse for every request.
func (s *server) handleAdminRequest(ctx context.Context, req *AdminRequest) *AdminResponse {
	h, ok := lookupAdminHandler(req.Verb)
	if !ok {
		return &AdminResponse{
			Status:  "error",
			Message: fmt.Sprintf("unknown verb %q", req.Verb),
		}
	}
	resp, err := h(ctx, s, req.Args)
	if err != nil {
		return &AdminResponse{Status: "error", Message: err.Error()}
	}
	if resp == nil {
		return &AdminResponse{Status: "ok"}
	}
	if resp.Status == "" {
		resp.Status = "ok"
	}
	return resp
}
