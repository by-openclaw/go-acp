// Package consumer is the outbound client for the EVS Cerebrum RCP API
// (assets/Cerebrum RCP API-2_6_1.json): the session, and the RouteMaster
// sources and destinations — local, virtual and federation.
//
// One Client is one session. Login obtains the token every other call
// carries; Logout gives it back. The API document's servers are
// http://<host>:8080/v2 and https://<host>:443/v2; the port is set in
// Cerebrum's own configuration, so the caller names it.
package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"dhs/internal/consumer/compliance"
	"dhs/internal/rcp/codec"
	"dhs/internal/transport"
	transporthttp "dhs/internal/transport/http"
)

// DefaultPort is the HTTP port of the API document's development server.
const DefaultPort = 8080

// DefaultTimeout bounds one request.
const DefaultTimeout = 8 * time.Second

// MaxBody caps one answer.
const MaxBody = 8 << 20

// ErrNotLoggedIn is returned by a call that needs the token before
// Login obtained one.
var ErrNotLoggedIn = errors.New("rcp: not logged in")

// RequestError is an answer the server gave with a non-2xx status.
// Code and Message are the document's error object; they are zero when
// the answer was not that object.
type RequestError struct {
	Method  string
	Path    string
	Status  int
	Code    int
	Message string
}

func (e *RequestError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("rcp: %s %s: HTTP %d", e.Method, e.Path, e.Status)
	}
	return fmt.Sprintf("rcp: %s %s: HTTP %d: %s (code %d)", e.Method, e.Path, e.Status, e.Message, e.Code)
}

// Options configures a Client.
type Options struct {
	// Host is the Cerebrum address, "host" or "host:port".
	Host string
	// TLS selects https.
	TLS bool
	// VerifyTLS verifies the server certificate (default: skip — the
	// lab servers are self-signed).
	VerifyTLS bool
	// Timeout bounds each request. 0 → DefaultTimeout.
	Timeout time.Duration
	// HTTP, when set, is the client used instead of one built from the
	// options above — a test substitutes its own.
	HTTP *transporthttp.Client
	// Profile counts the deviations absorbed. Nil is allowed.
	Profile *compliance.Profile
}

// Client is one RCP session.
type Client struct {
	base    string
	http    *transporthttp.Client
	profile *compliance.Profile
	reqid   atomic.Int64
	token   string
	wsPort  int
}

// New builds a Client. Nothing is sent until a method is called.
func New(o Options) *Client {
	host := o.Host
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(host, strconv.Itoa(DefaultPort))
	}
	scheme := "http://"
	if o.TLS {
		scheme = "https://"
	}
	hc := o.HTTP
	if hc == nil {
		timeout := o.Timeout
		if timeout <= 0 {
			timeout = DefaultTimeout
		}
		// Posture only: the transport layer builds the client. Skip-verify
		// is the default because the lab servers are self-signed; VerifyTLS
		// opts back in.
		var err error
		hc, err = transporthttp.NewTLSClient(transport.TLSOptions{Enable: o.TLS, Insecure: !o.VerifyTLS}, timeout)
		if err != nil {
			// Unreachable: no CA or client-certificate file is configured,
			// and those are the only ways building a posture fails.
			hc = transporthttp.NewClient()
		}
		hc.MaxBody = MaxBody
		hc.Proto = "rcp"
	}
	return &Client{base: scheme + host + "/v2", http: hc, profile: o.Profile}
}

// Base is the API root every path is relative to.
func (c *Client) Base() string { return c.base }

// WebsocketPort is the back-channel port the login answer named, 0
// before Login.
func (c *Client) WebsocketPort() int { return c.wsPort }

// LoggedIn reports whether the session holds a token.
func (c *Client) LoggedIn() bool { return c.token != "" }

// do sends one request and returns the answer's envelope. Every request
// carries the mandatory reqid header; auth adds the session's token.
func (c *Client) do(ctx context.Context, method, path string, body any, auth bool) (map[string]json.RawMessage, error) {
	if auth && c.token == "" {
		return nil, ErrNotLoggedIn
	}
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			return nil, fmt.Errorf("rcp: marshal %s %s: %w", method, path, err)
		}
	}
	id := c.reqid.Add(1)
	// The header name is written in lower case, as given: Cerebrum 2.5.3
	// matches it case-sensitively and answers "reqid missing from message
	// headers" to the canonical "Reqid".
	headers := map[string]string{"reqid": strconv.FormatInt(id, 10), "Accept": "application/json"}
	if body != nil {
		headers["Content-Type"] = "application/json"
	}
	if auth {
		headers["Authorization"] = "Bearer " + c.token
	}

	status, answer, err := c.http.Exchange(ctx, method, c.base+path, headers, raw)
	if err != nil {
		return nil, fmt.Errorf("rcp: %s %s: %w", method, path, err)
	}

	var env map[string]json.RawMessage
	enveloped := json.Unmarshal(answer, &env) == nil

	if status < 200 || status > 299 {
		re := &RequestError{Method: method, Path: path, Status: status}
		var ae codec.APIError
		if enveloped && env["error"] != nil && json.Unmarshal(env["error"], &ae) == nil {
			re.Code, re.Message = ae.Code, ae.Message
		} else {
			c.profile.Note(ErrorNotEnveloped)
		}
		return nil, re
	}
	if !enveloped {
		return nil, fmt.Errorf("rcp: %s %s: HTTP %d with a body that is not a JSON object", method, path, status)
	}
	var echoed int64
	if env["reqid"] == nil || json.Unmarshal(env["reqid"], &echoed) != nil || echoed != id {
		c.profile.Note(ReqIDNotEchoed)
	}
	return env, nil
}

// field decodes one member of an envelope.
func field(env map[string]json.RawMessage, key string, dst any) error {
	raw, ok := env[key]
	if !ok {
		return fmt.Errorf("rcp: the answer has no %q", key)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("rcp: decode %q: %w", key, err)
	}
	return nil
}

// API confirms connectivity and returns the server's API version. It
// needs no login.
func (c *Client) API(ctx context.Context) (codec.APIVersion, error) {
	var v codec.APIVersion
	env, err := c.do(ctx, http.MethodGet, "/api", nil, false)
	if err != nil {
		return v, err
	}
	return v, field(env, "api", &v)
}

// Login opens the session.
func (c *Client) Login(ctx context.Context, user, pass string) error {
	env, err := c.do(ctx, http.MethodPost, "/login", map[string]string{"username": user, "password": pass}, false)
	if err != nil {
		return err
	}
	var l codec.Login
	if err := field(env, "login", &l); err != nil {
		return err
	}
	if l.Token == "" {
		return errors.New("rcp: the login answer carries no token")
	}
	c.token, c.wsPort = l.Token, l.WebsocketPort
	return nil
}

// WhoAmI returns the user the session is logged in as.
func (c *Client) WhoAmI(ctx context.Context) (string, error) {
	env, err := c.do(ctx, http.MethodGet, "/login", nil, true)
	if err != nil {
		return "", err
	}
	var l struct {
		Username string `json:"username"`
	}
	return l.Username, field(env, "login", &l)
}

// Heartbeat tells the server the client is still there.
func (c *Client) Heartbeat(ctx context.Context) error {
	_, err := c.do(ctx, http.MethodPost, "/heartbeat", nil, true)
	return err
}

// Logout closes the session. The token is dropped whatever the server
// answers: a session that could not be closed is not one to keep using.
func (c *Client) Logout(ctx context.Context) error {
	if c.token == "" {
		return nil
	}
	_, err := c.do(ctx, http.MethodDelete, "/login", nil, true)
	c.token, c.wsPort = "", 0
	return err
}

// ioPath is the path of a collection, or of one IO in it.
func ioPath(col codec.Collection, id string) string {
	p := "/routemaster/" + string(col)
	if id != "" {
		p += "/" + id
	}
	return p
}

// List returns the ids of a collection, in the order the IOs are
// configured. The order is a display order and nothing else.
func (c *Client) List(ctx context.Context, col codec.Collection) ([]string, error) {
	env, err := c.do(ctx, http.MethodGet, ioPath(col, ""), nil, true)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	return ids, field(env, "ids", &ids)
}

// Get returns one IO in full.
func (c *Client) Get(ctx context.Context, col codec.Collection, id string) (codec.IO, error) {
	var io codec.IO
	if strings.TrimSpace(id) == "" {
		return io, errors.New("rcp: an id is required")
	}
	env, err := c.do(ctx, http.MethodGet, ioPath(col, id), nil, true)
	if err != nil {
		return io, err
	}
	// The document's key is "source" / "destination". Cerebrum 2.5.3
	// answers under the collection's name instead.
	if _, ok := env[col.SingleKey()]; ok {
		return io, field(env, col.SingleKey(), &io)
	}
	for _, key := range []string{string(col), col.SingleKey() + "s"} {
		if _, ok := env[key]; ok {
			c.profile.Note(SingleIOKey)
			return io, field(env, key, &io)
		}
	}
	return io, fmt.Errorf("rcp: the answer has no %q", col.SingleKey())
}

// Create appends IOs to a collection. The server allocates the ids and
// does not return them: List again to learn them.
func (c *Client) Create(ctx context.Context, col codec.Collection, u codec.Update) error {
	if err := u.Validate(col, true); err != nil {
		return err
	}
	_, err := c.do(ctx, http.MethodPost, ioPath(col, ""), u, true)
	return err
}

// Update modifies the supplied fields of one IO and leaves the rest.
func (c *Client) Update(ctx context.Context, col codec.Collection, id string, u codec.Update) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("rcp: an id is required")
	}
	if err := u.Validate(col, false); err != nil {
		return err
	}
	_, err := c.do(ctx, http.MethodPatch, ioPath(col, id), u, true)
	return err
}

// Delete removes one IO.
func (c *Client) Delete(ctx context.Context, col codec.Collection, id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("rcp: an id is required")
	}
	_, err := c.do(ctx, http.MethodDelete, ioPath(col, id), nil, true)
	return err
}
