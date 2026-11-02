package virtuoso

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	dhsc "dhs/internal/consumer"
	"dhs/internal/consumer/compliance"
	transporthttp "dhs/internal/transport/http"
	"dhs/internal/virtuoso/codec"
)

// maxBody caps one answer. A card's schema is over a megabyte and the
// shared client's 1 MiB default would cut it.
const maxBody = 64 << 20

// logoutWait bounds the courtesy logout on the way out.
const logoutWait = 2 * time.Second

// Errors of the session.
var (
	ErrUnauthorized = errors.New("virtuoso: 401 unauthorized — set DHS_VIRTUOSO_USER and DHS_VIRTUOSO_PASS")
	ErrLoginRefused = errors.New("virtuoso: login refused (HTTP 401)")
	ErrNoSession    = errors.New("virtuoso: login answered without the NEVION_SESID session cookie")
)

// HTTPError is an answer with a status other than 200: "Anything except
// 200 means the operation was unsuccessful."
type HTTPError struct {
	Method string
	Target string
	Status int
	Text   string
}

func (e *HTTPError) Error() string {
	s := fmt.Sprintf("virtuoso: %s %s: HTTP %d", e.Method, e.Target, e.Status)
	if e.Status >= 300 && e.Status < 400 {
		s += " (the device redirects: set DHS_VIRTUOSO_TLS=1 when HTTP is redirected to HTTPS)"
	}
	if e.Text != "" {
		s += " " + e.Text
	}
	return s
}

// rest is one session: one base URL, one cookie jar, one request at a
// time, so that nothing here ever stacks reads on the device.
type rest struct {
	base string
	http *transporthttp.Client
	user string
	pass string
	log  *slog.Logger
	note compliance.Recorder

	mu sync.Mutex
}

func newREST(base string, client *transporthttp.Client, o Options, log *slog.Logger, note compliance.Recorder) *rest {
	jar, _ := cookiejar.New(nil) // never fails with nil options
	client.HTTP.Jar = jar
	client.MaxBody = maxBody
	client.Proto = Name
	// A POST that follows a redirect turns into a GET and loses its point.
	client.HTTP.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &rest{base: strings.TrimRight(base, "/"), http: client, user: o.User, pass: o.Pass, log: log, note: note}
}

// detach gives a request the caller's cancel without the caller's
// deadline. The wait for one answer is the client's timeout; a deadline
// over a whole command is not a limit this connector accepts, because a
// slow card would cost every card after it.
func detach(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	// AfterFunc runs on its own goroutine: too late for this request when
	// the caller has already cancelled.
	if errors.Is(parent.Err(), context.Canceled) {
		cancel()
	}
	stop := context.AfterFunc(parent, func() {
		if errors.Is(parent.Err(), context.Canceled) {
			cancel()
		}
	})
	return ctx, func() {
		stop()
		cancel()
	}
}

// fetch issues one request. A 401 means the session is gone (the guide
// removes it after 3 idle minutes): log in again once and repeat the
// request once.
func (r *rest) fetch(ctx context.Context, method, target string) (int, []byte, error) {
	ctx, done := detach(ctx)
	defer done()
	r.mu.Lock()
	defer r.mu.Unlock()

	status, raw, err := r.exchange(ctx, method, target)
	if err != nil || status != http.StatusUnauthorized {
		return status, raw, err
	}
	if r.user == "" {
		return 0, nil, ErrUnauthorized
	}
	r.log.Info("virtuoso: session gone, logging in again")
	if err := r.loginLocked(ctx); err != nil {
		return 0, nil, err
	}
	if status, raw, err = r.exchange(ctx, method, target); err != nil {
		return 0, nil, err
	}
	if status == http.StatusUnauthorized {
		return 0, nil, fmt.Errorf("%w: refused again after a fresh login", ErrUnauthorized)
	}
	return status, raw, nil
}

// do issues one request and decodes its envelope.
func (r *rest) do(ctx context.Context, method, target string) (codec.Envelope, error) {
	status, raw, err := r.fetch(ctx, method, target)
	if err != nil {
		return codec.Envelope{}, err
	}
	return r.decode(method, target, status, raw)
}

// schema fetches schema.json, the JSON schema itself with no envelope.
func (r *rest) schema(ctx context.Context, slot int) ([]byte, error) {
	target := codec.SchemaTarget(slot)
	status, raw, err := r.fetch(ctx, "GET", target)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, &HTTPError{Method: "GET", Target: target, Status: status, Text: snippet(raw)}
	}
	return raw, nil
}

func (r *rest) exchange(ctx context.Context, method, target string) (int, []byte, error) {
	headers := map[string]string{"Accept": "application/json"}
	status, raw, err := r.http.Exchange(ctx, method, r.base+target, headers, nil)
	if err != nil {
		return 0, nil, &dhsc.TransportError{Op: method + " " + target, Err: err}
	}
	return status, raw, nil
}

func (r *rest) decode(method, target string, status int, raw []byte) (codec.Envelope, error) {
	env, derr := codec.DecodeEnvelope(raw)
	if status != http.StatusOK {
		text := snippet(raw)
		if derr == nil {
			if serr := env.Err(); serr != nil {
				text = serr.Error()
			}
		}
		return codec.Envelope{}, &HTTPError{Method: method, Target: target, Status: status, Text: text}
	}
	if derr != nil {
		r.note.Note(AnswerNotJSON)
		return codec.Envelope{}, fmt.Errorf("%s %s: %w", method, target, derr)
	}
	r.surface(target, env)
	if err := env.Err(); err != nil {
		return codec.Envelope{}, fmt.Errorf("%s %s: %w", method, target, err)
	}
	return env, nil
}

// surface reports what the info object says beyond its status. Nothing
// the device says is dropped: warnings and stray errors go to the log,
// and a shape the guide does not define fires a compliance event.
func (r *rest) surface(target string, env codec.Envelope) {
	i := env.Info
	switch {
	case !env.HasInfo:
		r.note.Note(NoInfoObject)
	case i.Status != codec.StatusOK && i.Status != codec.StatusFailed:
		r.note.Note(StatusUnknown)
	case i.Status == codec.StatusFailed && len(i.Errors) == 0:
		r.note.Note(FailedWithoutErrors)
	case i.Status == codec.StatusOK && len(i.Errors) > 0:
		r.note.Note(ErrorsWithOK)
		for _, m := range i.Errors {
			r.log.Warn("virtuoso: device error on an ok answer", "target", target, "error", m.String())
		}
	}
	for _, m := range i.Warnings {
		r.log.Warn("virtuoso: device warning", "target", target, "warning", m.String())
	}
	for _, m := range i.Notes {
		r.log.Info("virtuoso: device note", "target", target, "note", m.String())
	}
}

func (r *rest) login(ctx context.Context) error {
	ctx, done := detach(ctx)
	defer done()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.loginLocked(ctx)
}

// loginLocked posts the credentials as HTTP Basic. The exchange goes
// through a copy of the client with no recorder: a --capture file must
// never hold the Authorization header or the session cookie it earns.
func (r *rest) loginLocked(ctx context.Context) error {
	quiet := *r.http
	quiet.Recorder = nil
	token := base64.StdEncoding.EncodeToString([]byte(r.user + ":" + r.pass))
	headers := map[string]string{"Accept": "application/json", "Authorization": "Basic " + token}
	status, raw, err := quiet.Exchange(ctx, "POST", r.base+codec.LoginTarget, headers, nil)
	if err != nil {
		return &dhsc.TransportError{Op: "POST " + codec.LoginTarget, Err: err}
	}
	switch {
	case status == http.StatusUnauthorized:
		return ErrLoginRefused
	case status != http.StatusOK:
		return &HTTPError{Method: "POST", Target: codec.LoginTarget, Status: status, Text: snippet(raw)}
	}
	if env, derr := codec.DecodeEnvelope(raw); derr == nil {
		if err := env.Err(); err != nil {
			return err
		}
	}
	return r.requireCookie()
}

func (r *rest) requireCookie() error {
	u, _ := url.Parse(r.base + codec.LoginTarget) // it just carried a request
	for _, c := range r.http.HTTP.Jar.Cookies(u) {
		if c.Name == codec.SessionCookie {
			return nil
		}
	}
	r.note.Note(NoSessionCookie)
	return ErrNoSession
}

// logout ends the session, with its own short wait and no re-login. It is
// a courtesy: the device drops an idle session after 3 minutes anyway.
func (r *rest) logout() {
	if r.user == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), logoutWait)
	defer cancel()
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, _, err := r.exchange(ctx, "POST", codec.LogoutTarget); err != nil {
		r.log.Debug("virtuoso: logout", "err", err)
	}
}

// noAnswer reports an error that means nobody answered for the module: no
// exchange, a status other than 200, or a body that is no API answer. A
// refused session and a device status are answers.
func noAnswer(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrLoginRefused) {
		return false
	}
	var transportErr *dhsc.TransportError
	var httpErr *HTTPError
	return errors.As(err, &transportErr) || errors.As(err, &httpErr) || errors.Is(err, codec.ErrNotJSON)
}

// absentReason is the operator's view of why a slot counts as absent.
func absentReason(err error) string {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return fmt.Sprintf("HTTP %d", httpErr.Status)
	}
	if errors.Is(err, codec.ErrNotJSON) {
		return "answer is not the API"
	}
	return "no answer"
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}
