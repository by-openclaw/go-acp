package prism

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	dhsc "dhs/internal/consumer"
	transporthttp "dhs/internal/transport/http"
)

// ErrUnauthorized is a call that needs the remote password: the page's
// script says such a call takes it in the Authorization header.
var ErrUnauthorized = errors.New("prism: not authorized (set " + EnvPass + ")")

// maxBody caps one answer; eye_histogram_data is raw data.
const maxBody = 16 << 20

// RefusedError is an HTTP answer outside 2xx. The page defines no error
// shape, so the status and the start of the body are all there is.
type RefusedError struct {
	Method, Call string
	Status       int
	Body         string
}

func (e *RefusedError) Error() string {
	return fmt.Sprintf("prism: %s %s: HTTP %d %s", e.Method, e.Call, e.Status, e.Body)
}

// Unwrap lets a caller test for ErrUnauthorized.
func (e *RefusedError) Unwrap() error {
	if e.Status == 401 || e.Status == 403 {
		return ErrUnauthorized
	}
	return nil
}

// rest is the HTTP side: one base URL, one exchange at a time.
type rest struct {
	base string
	http *transporthttp.Client
	auth string
	wait time.Duration
}

func newREST(base string, client *transporthttp.Client, password string, wait time.Duration) *rest {
	client.HTTP.Timeout = 0 // the context carries the one wait
	client.MaxBody = maxBody
	client.Proto = Name
	if wait <= 0 {
		wait = transporthttp.DefaultTimeout
	}
	r := &rest{base: strings.TrimRight(base, "/"), http: client, wait: wait}
	if password != "" {
		r.auth = "Basic " + password
	}
	return r
}

// exchange is one request and its answer. The wait is the context's
// deadline, or the configured answer wait when it has none (walk, watch).
func (r *rest) exchange(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.wait)
		defer cancel()
	}
	headers := map[string]string{"Accept": "application/json"}
	if body != nil {
		headers["Content-Type"] = "application/json"
	}
	if r.auth != "" {
		headers["Authorization"] = r.auth
	}
	status, raw, err := r.http.Exchange(ctx, method, r.base+path, headers, body)
	if err != nil {
		return nil, &dhsc.TransportError{Op: method + " " + path, Err: err}
	}
	if status < 200 || status > 299 {
		return nil, &RefusedError{Method: method, Call: path, Status: status, Body: snippet(raw)}
	}
	return raw, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}
