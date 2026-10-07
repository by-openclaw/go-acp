package consumer

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"dhs/internal/rrcs/codec"
	"dhs/internal/wiretrace"
)

// DefaultPath is the URL path RRCS posts its notifications to when the
// registration names none (§8.15.1).
const DefaultPath = "/RPC2"

// MethodGetAlive is the ping RRCS sends on an idle notification channel
// (§9.8).
const MethodGetAlive = "GetAlive"

// Event is one request RRCS sent us: a notification, or its ping.
type Event struct {
	Time   time.Time
	Remote string
	Method string
	// TransKey is the first parameter when it has the shape of a
	// transaction key, empty otherwise (GetAlive carries none, §9.8).
	TransKey string
	// Params holds every parameter, the transaction key included.
	Params []codec.Value
}

// Listener is the HTTP endpoint RRCS calls. Every well-formed request is
// answered at once with the key echoed and error code 0 (§11.2), then
// handed to OnEvent: an unanswered GetAlive makes RRCS remove the
// registration without a word (§9.8), so the answer never waits for the
// handler's caller.
//
// The zero value serves DefaultPath and drops the events.
type Listener struct {
	// Path is the URL path given at registration. Empty means
	// DefaultPath. A leading slash is optional, as in §8.15.1.
	Path string
	// AnyPath accepts a request on every path. The older registration
	// (RegisterForEventsEx, §8.15) names a receiver by address and port
	// only, so its notifications do not come on Path.
	AnyPath bool
	// OnEvent receives every request, GetAlive included, after the
	// answer has been written. It runs on the HTTP server's goroutine
	// of that request and must not block for long.
	OnEvent func(Event)
	// OnReject receives what could not be read as a request.
	OnReject func(remote string, err error)
	// Tap, when set, sees every document in both directions.
	Tap Tap
	// Now replaces time.Now.
	Now func() time.Time

	events atomic.Uint64
	alives atomic.Uint64
}

// Events is the number of notifications received, GetAlive excluded.
func (l *Listener) Events() uint64 { return l.events.Load() }

// Alives is the number of GetAlive pings answered.
func (l *Listener) Alives() uint64 { return l.alives.Load() }

// NormalizePath gives a registration path its leading slash.
func NormalizePath(p string) string {
	if p == "" {
		return DefaultPath
	}
	if !strings.HasPrefix(p, "/") {
		return "/" + p
	}
	return p
}

// ValidPath applies the §8.15.1 conditions: ASCII, no control character,
// no space.
func ValidPath(p string) bool {
	for i := 0; i < len(p); i++ {
		if p[i] <= ' ' || p[i] >= 127 {
			return false
		}
	}
	return true
}

func (l *Listener) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !l.AnyPath && r.URL.Path != NormalizePath(l.Path) {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		http.Error(w, "unreadable request", http.StatusBadRequest)
		return
	}
	l.trace(wiretrace.DirectionRx, r.RemoteAddr, body)

	call, err := codec.DecodeCall(body)
	if err != nil {
		if l.OnReject != nil {
			l.OnReject(r.RemoteAddr, err)
		}
		l.reply(w, r, codec.EncodeFault(1, "malformed request"))
		return
	}
	ev := Event{Remote: r.RemoteAddr, Method: call.Method, Params: call.Params}
	if l.Now != nil {
		ev.Time = l.Now()
	} else {
		ev.Time = time.Now()
	}
	if len(call.Params) > 0 && call.Params[0].Kind == codec.KindString && codec.ValidTransKey(call.Params[0].Str) {
		ev.TransKey = call.Params[0].Str
	}
	// The array of §11.2. Every notification is documented with this
	// answer or with "no return value (ignored by RRCS)" (§9.4, §9.5,
	// §9.8), so the same answer serves them all.
	doc, err := codec.EncodeResponse(codec.Array(codec.String(ev.TransKey), codec.Int(int32(codec.CodeSuccess))))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	l.reply(w, r, doc)

	if call.Method == MethodGetAlive {
		l.alives.Add(1)
	} else {
		l.events.Add(1)
	}
	if l.OnEvent != nil {
		l.OnEvent(ev)
	}
}

func (l *Listener) reply(w http.ResponseWriter, r *http.Request, doc []byte) {
	// "The Content-Length must be specified and must be correct" (§5.5).
	// Without it the answer leaves chunked, which a real RRCS (9.0)
	// takes for a failed notification: it then drops the registration.
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(doc)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(doc)
	l.trace(wiretrace.DirectionTx, r.RemoteAddr, doc)
}

func (l *Listener) trace(dir wiretrace.Direction, peer string, doc []byte) {
	if l.Tap != nil {
		l.Tap(dir, peer, doc)
	}
}
