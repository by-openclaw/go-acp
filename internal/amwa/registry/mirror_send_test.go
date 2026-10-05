package registry

// The target closed the connection the request went out on, before
// answering (mirror.go, send). Reproduced the way it happens: the
// server hijacks and closes the first connection it is given a request
// on, as a registry does to one that sat past its header read timeout.

import (
	"context"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
)

// closesFirst answers 200 to every request but the first `drops`, whose
// connections it closes without a byte.
func closesFirst(t *testing.T, drops int32) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var seen atomic.Int32
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if seen.Add(1) <= drops {
			conn, _, err := w.(stdhttp.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			_ = conn.Close()
			return
		}
		_, _ = w.Write([]byte(`{"health":"1"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestMirrorSendsAHeartbeatAgainWhenItsConnectionWasClosed(t *testing.T) {
	target, seen := closesFirst(t, 1)
	m, err := NewMirror(MirrorOptions{Source: "http://source.invalid:1", Target: target.URL, APIVer: "v1.3"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.sendHealth(context.Background(), "n1", "v1.3"); err != nil {
		t.Fatalf("heartbeat over a connection the target had closed: %v", err)
	}
	if n := seen.Load(); n != 2 {
		t.Errorf("target saw %d request(s), want the heartbeat and its one repeat", n)
	}
	if st := m.Stats(); st.Failures != 0 || st.Heartbeats != 1 {
		t.Errorf("stats = %+v, want 1 heartbeat and no failure", st)
	}
}

// Once more is once: a target that keeps closing is a failure, counted.
func TestMirrorSendGivesUpAfterOneRepeat(t *testing.T) {
	target, seen := closesFirst(t, 100)
	m, err := NewMirror(MirrorOptions{Source: "http://source.invalid:1", Target: target.URL, APIVer: "v1.3"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.sendHealth(context.Background(), "n1", "v1.3"); err == nil {
		t.Fatal("a target that closes every connection must be reported")
	}
	if n := seen.Load(); n != 2 {
		t.Errorf("target saw %d request(s), want 2", n)
	}
	if st := m.Stats(); st.Failures != 1 {
		t.Errorf("failures = %d, want 1", st.Failures)
	}
}

// A resource POST carries a body: the repeat carries it again, whole.
func TestMirrorSendRepeatsTheBody(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	var n atomic.Int32
	target := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		b, _ := io.ReadAll(r.Body)
		if n.Add(1) == 1 {
			conn, _, _ := w.(stdhttp.Hijacker).Hijack()
			_ = conn.Close()
			return
		}
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		w.WriteHeader(stdhttp.StatusCreated)
	}))
	defer target.Close()
	m, err := NewMirror(MirrorOptions{Source: "http://source.invalid:1", Target: target.URL, APIVer: "v1.3"})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := stdhttp.NewRequest(stdhttp.MethodPost, target.URL+"/x-nmos/registration/v1.3/resource", strings.NewReader(`{"type":"node","data":{"id":"n1"}}`))
	resp, err := m.send(req)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	drainClose(resp)
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 1 || bodies[0] != `{"type":"node","data":{"id":"n1"}}` {
		t.Errorf("the repeat carried %q", bodies)
	}
}

func TestClosedBeforeAnswer(t *testing.T) {
	for _, err := range []error{io.EOF, syscall.ECONNRESET, syscall.EPIPE} {
		if !closedBeforeAnswer(errors.Join(errors.New("Post"), err)) {
			t.Errorf("%v is a connection closed before the answer", err)
		}
	}
	if closedBeforeAnswer(context.DeadlineExceeded) || closedBeforeAnswer(errors.New("no route to host")) {
		t.Error("a timeout or an unreachable target is not one, and is not repeated")
	}
}

// A body that cannot be produced a second time is not sent half: the
// first failure is what is reported.
func TestMirrorSendReportsTheFirstFailureWhenTheBodyCannotBeRepeated(t *testing.T) {
	target, seen := closesFirst(t, 100)
	m, err := NewMirror(MirrorOptions{Source: "http://source.invalid:1", Target: target.URL, APIVer: "v1.3"})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := stdhttp.NewRequest(stdhttp.MethodPost, target.URL+"/x", strings.NewReader("{}"))
	req.GetBody = func() (io.ReadCloser, error) { return nil, errors.New("gone") }
	if _, err := m.send(req); err == nil || !closedBeforeAnswer(err) {
		t.Fatalf("err = %v, want the closed connection", err)
	}
	if n := seen.Load(); n != 1 {
		t.Errorf("target saw %d request(s), want 1", n)
	}
}
