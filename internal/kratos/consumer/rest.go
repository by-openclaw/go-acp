package kratos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/cookiejar"
	"strings"

	"dhs/internal/kratos/codec"
	transporthttp "dhs/internal/transport/http"
)

// ErrUnauthorized is the 401 every query but security/login answers
// without a valid session cookie in the medium and high security models
// (§6.1).
var ErrUnauthorized = errors.New("kratos: 401 unauthorized — this NGC2 requires a login (set DHS_KRATOS_USER / DHS_KRATOS_PASS)")

// maxBody caps one answer. The SVT and the enumerations table are "quite
// large" (§5.27.1); the shared client's 1 MiB default would cut them.
const maxBody = 64 << 20

// rest is the REST half of the API (§4): one base URL, one cookie jar.
type rest struct {
	base string // "http://host:3000" or "https://host:4000"
	http *transporthttp.Client
}

func newREST(base string, client *transporthttp.Client) *rest {
	jar, _ := cookiejar.New(nil) // never fails with nil options
	client.HTTP.Jar = jar
	client.MaxBody = maxBody
	client.Proto = Name
	return &rest{base: strings.TrimRight(base, "/"), http: client}
}

// get issues GET <base>/<query> and decodes the envelope.
func (r *rest) get(ctx context.Context, query string) (codec.Envelope, error) {
	return r.do(ctx, "GET", query, nil)
}

// post issues POST <base>/<query> with a JSON body.
func (r *rest) post(ctx context.Context, query string, body []byte) (codec.Envelope, error) {
	return r.do(ctx, "POST", query, body)
}

func (r *rest) do(ctx context.Context, method, query string, body []byte) (codec.Envelope, error) {
	url := r.base + "/" + strings.TrimLeft(query, "/")
	headers := map[string]string{"Accept": "application/json"}
	if body != nil {
		headers["Content-Type"] = "application/json"
	}
	status, raw, err := r.http.Exchange(ctx, method, url, headers, body)
	if err != nil {
		return codec.Envelope{}, err
	}
	switch {
	case status == 401:
		return codec.Envelope{}, ErrUnauthorized
	case status < 200 || status > 299:
		return codec.Envelope{}, fmt.Errorf("kratos: %s %s: HTTP %d %s", method, query, status, snippet(raw))
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		// putData answers "the HTTP code only" (§5.40.3).
		return codec.Envelope{Query: query}, nil
	}
	env, err := codec.DecodeEnvelope(raw)
	if err != nil {
		return codec.Envelope{}, fmt.Errorf("kratos: %s %s: %w", method, query, err)
	}
	return env, nil
}

// login posts security/login (§6.2): {"Operator":…, "Password":…}. The
// session cookie lands in the jar.
func (r *rest) login(ctx context.Context, user, pass string) (codec.LoginReply, error) {
	body, _ := json.Marshal(map[string]string{"Operator": user, "Password": pass})
	env, err := r.post(ctx, "security/login", body)
	if err != nil {
		if errors.Is(err, ErrUnauthorized) {
			return codec.LoginReply{}, fmt.Errorf("kratos: login refused for %q (HTTP 401)", user)
		}
		return codec.LoginReply{}, err
	}
	var rows []codec.LoginReply
	if _, err := env.Rows(&rows); err != nil {
		return codec.LoginReply{}, err
	}
	if len(rows) == 0 {
		return codec.LoginReply{Operator: user}, nil
	}
	return rows[0], nil
}

// logout posts security/logout (§6.4).
func (r *rest) logout(ctx context.Context) error {
	_, err := r.post(ctx, "security/logout", []byte("{}"))
	return err
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}
