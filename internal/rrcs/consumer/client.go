package consumer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"dhs/internal/rrcs/codec"
	"dhs/internal/wiretrace"
)

const (
	// DefaultPort is the TCP port RRCS listens on (§5.4).
	DefaultPort = 8193
	// DefaultPrefix starts the transaction keys of our requests. Any
	// character but 'R', which RRCS keeps for its own (§6.5).
	DefaultPrefix = 'C'
	// DefaultTimeout bounds one request. Queries are "typically
	// answered within 10ms" (§12); the list answers of a full system
	// are far larger ("approximately 5 kByte per port", §8.8).
	DefaultTimeout = 10 * time.Second

	// maxBody bounds an answer read from RRCS or a notification read
	// from it.
	maxBody = 64 << 20

	userAgent   = "dhs-rrcs"
	contentType = "text/xml"
)

// ErrTransKey reports an answer that echoes another transaction key than
// the one of the request (§6.5).
var ErrTransKey = errors.New("rrcs: answer carries another transaction key")

// ErrHTTP reports an answer that is not HTTP 200.
var ErrHTTP = errors.New("rrcs: unexpected HTTP status")

// Tap receives every XML document exchanged, as sent or received.
type Tap func(dir wiretrace.Direction, peer string, doc []byte)

// Config describes one RRCS gateway.
type Config struct {
	// Addr is host or host:port. The port defaults to DefaultPort.
	Addr string
	// Timeout bounds one request. Zero means DefaultTimeout.
	Timeout time.Duration
	// Prefix starts our transaction keys. Zero means DefaultPrefix.
	Prefix byte
	// Seed is the first sequence number of the transaction keys. Zero
	// means the wall clock, so two runs do not reuse the same keys.
	Seed uint64
	// Tap, when set, sees every document in both directions.
	Tap Tap
	// HTTPClient replaces the default client. Its Timeout is not used;
	// Timeout above is.
	HTTPClient *http.Client
}

// Client sends requests to one RRCS gateway. It is safe for concurrent
// use.
type Client struct {
	url     string
	peer    string
	timeout time.Duration
	prefix  byte
	seq     atomic.Uint64
	tap     Tap
	http    *http.Client
}

// NewClient validates cfg. It does not open a connection.
func NewClient(cfg Config) (*Client, error) {
	peer, err := hostPort(cfg.Addr)
	if err != nil {
		return nil, err
	}
	c := &Client{
		url:     "http://" + peer + "/",
		peer:    peer,
		timeout: cfg.Timeout,
		prefix:  cfg.Prefix,
		tap:     cfg.Tap,
		http:    cfg.HTTPClient,
	}
	if c.timeout <= 0 {
		c.timeout = DefaultTimeout
	}
	if c.prefix == 0 {
		c.prefix = DefaultPrefix
	}
	if c.prefix == codec.RRCSPrefix {
		return nil, fmt.Errorf("rrcs: transaction key prefix %q is the one RRCS uses", string(c.prefix))
	}
	if c.http == nil {
		c.http = &http.Client{}
	}
	seed := cfg.Seed
	if seed == 0 {
		seed = uint64(time.Now().UnixMilli())
	}
	c.seq.Store(seed)
	return c, nil
}

// Peer is the host:port the client talks to.
func (c *Client) Peer() string { return c.peer }

// hostPort completes addr with the default port.
func hostPort(addr string) (string, error) {
	if addr == "" {
		return "", errors.New("rrcs: empty address")
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		// No port: a host name, an IPv4 address or a bare IPv6 address.
		host, port = addr, strconv.Itoa(DefaultPort)
		if len(host) > 1 && host[0] == '[' && host[len(host)-1] == ']' {
			host = host[1 : len(host)-1]
		}
	}
	if host == "" {
		return "", fmt.Errorf("rrcs: address %q has no host", addr)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("rrcs: address %q has an invalid port", addr)
	}
	return net.JoinHostPort(host, port), nil
}

// Reply is one answer of RRCS.
//
// The specification prints several shapes for an answer: an array that
// opens with the key and an error code (§11.1), an array that opens with
// the key and carries no code (§8.8 GetAllPorts), a struct with members
// TransKey and ErrorCode (§8.7), a struct with the key only (§8.8
// IsConnectedToArtist). Value is the whole answer; the other fields are
// what could be read out of it.
type Reply struct {
	// Key is the transaction key of the request.
	Key string
	// Value is the answer as RRCS sent it.
	Value codec.Value
	// Echo is the transaction key found in the answer, empty if none.
	Echo string
	// Code is the error code found in the answer. It means nothing
	// when HasCode is false.
	Code    codec.ErrorCode
	HasCode bool
}

// Payload is what the answer holds besides the key and the code: the
// remaining elements of an array, or the remaining members of a struct.
func (r Reply) Payload() codec.Value {
	switch r.Value.Kind {
	case codec.KindArray:
		skip := 0
		if r.Echo != "" {
			skip++
		}
		if r.HasCode {
			skip++
		}
		return codec.Array(r.Value.Items[skip:]...)
	case codec.KindStruct:
		var rest []codec.Member
		for _, m := range r.Value.Members {
			if m.Name != "TransKey" && m.Name != "ErrorCode" {
				rest = append(rest, m)
			}
		}
		return codec.Struct(rest...)
	}
	if r.Echo != "" {
		return codec.Array()
	}
	return r.Value
}

// readOutcome fills Echo, Code and HasCode from Value.
func (r *Reply) readOutcome() {
	v := r.Value
	switch v.Kind {
	case codec.KindArray:
		if len(v.Items) == 0 || v.Items[0].Kind != codec.KindString || !codec.ValidTransKey(v.Items[0].Str) {
			return
		}
		r.Echo = v.Items[0].Str
		if len(v.Items) > 1 && v.Items[1].Kind == codec.KindInt {
			r.Code, r.HasCode = codec.ErrorCode(v.Items[1].Int), true
		}
	case codec.KindStruct:
		if k, ok := v.Field("TransKey"); ok && k.Kind == codec.KindString {
			r.Echo = k.Str
		}
		if c, ok := v.Field("ErrorCode"); ok && c.Kind == codec.KindInt {
			r.Code, r.HasCode = codec.ErrorCode(c.Int), true
		}
	case codec.KindString:
		if codec.ValidTransKey(v.Str) {
			r.Echo = v.Str
		}
	}
}

// Call sends one request. The transaction key is generated and placed in
// front of params, as every request of the specification has it.
//
// The Reply is returned together with the error when RRCS answered and
// the answer is the error: a fault (*codec.Fault), an error code
// (*codec.CodeError) or a foreign transaction key (ErrTransKey).
func (c *Client) Call(ctx context.Context, method string, params ...codec.Value) (Reply, error) {
	key := codec.NewTransKey(c.prefix, c.seq.Add(1))
	all := make([]codec.Value, 0, len(params)+1)
	all = append(all, codec.String(key))
	all = append(all, params...)
	return c.call(ctx, key, method, all)
}

// CallNoKey sends a request without a transaction key, for the few
// methods the specification prints with no parameter at all.
func (c *Client) CallNoKey(ctx context.Context, method string, params ...codec.Value) (Reply, error) {
	return c.call(ctx, "", method, params)
}

func (c *Client) call(ctx context.Context, key, method string, params []codec.Value) (Reply, error) {
	reply := Reply{Key: key}
	doc, err := codec.EncodeCall(method, params...)
	if err != nil {
		return reply, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(doc))
	if err != nil {
		return reply, fmt.Errorf("rrcs: %s: %w", method, err)
	}
	// "A User-Agent and Host must be specified. The Content-Type must
	// be text/xml. The Content-Length must be specified" (§5.5).
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", userAgent)
	req.ContentLength = int64(len(doc))

	c.trace(wiretrace.DirectionTx, doc)
	resp, err := c.http.Do(req)
	if err != nil {
		return reply, fmt.Errorf("rrcs: %s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return reply, fmt.Errorf("rrcs: %s: read answer: %w", method, err)
	}
	c.trace(wiretrace.DirectionRx, body)
	if len(body) > maxBody {
		return reply, fmt.Errorf("rrcs: %s: answer larger than %d bytes", method, maxBody)
	}
	if resp.StatusCode != http.StatusOK {
		return reply, fmt.Errorf("%w: %s: %s", ErrHTTP, method, resp.Status)
	}
	decoded, err := codec.DecodeResponse(body)
	if err != nil {
		return reply, fmt.Errorf("%s: %w", method, err)
	}
	if decoded.Fault != nil {
		return reply, fmt.Errorf("%s: %w", method, decoded.Fault)
	}
	reply.Value = decoded.Value
	reply.readOutcome()
	if key != "" && reply.Echo != "" && reply.Echo != key {
		return reply, fmt.Errorf("%w: %s: sent %s, got %s", ErrTransKey, method, key, reply.Echo)
	}
	if reply.HasCode {
		if err := reply.Code.Err(); err != nil {
			return reply, fmt.Errorf("%s: %w", method, err)
		}
	}
	return reply, nil
}

func (c *Client) trace(dir wiretrace.Direction, doc []byte) {
	if c.tap != nil {
		c.tap(dir, c.peer, doc)
	}
}
