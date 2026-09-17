package facade

// The HTTP transport contract with the AMWA tool (TestingFacadeUtils):
// a POSTed question is acknowledged 202 before any work, and the answer
// arrives later as a POST to the question's answer_uri. A facade that
// cannot decide still answers, with the empty shape of the question's
// type, so the tool fails the one test instead of timing out the run.

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dhs/internal/metrics"
	"dhs/internal/plugin"
)

// answerSink is the tool's answer_uri: it captures every Reply posted
// to it and answers with status.
type answerSink struct {
	srv     *httptest.Server
	replies chan Reply
	status  int
}

func newAnswerSink(t *testing.T, status int) *answerSink {
	t.Helper()
	a := &answerSink{replies: make(chan Reply, 8), status: status}
	a.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var rep Reply
		if err := json.NewDecoder(r.Body).Decode(&rep); err != nil {
			t.Errorf("answer_uri: undecodable reply: %v", err)
		}
		a.replies <- rep
		w.WriteHeader(a.status)
	}))
	t.Cleanup(a.srv.Close)
	return a
}

// wait returns the next reply or fails the test after a deadline —
// never a sleep.
func (a *answerSink) wait(t *testing.T) Reply {
	t.Helper()
	select {
	case rep := <-a.replies:
		return rep
	case <-time.After(20 * time.Second):
		t.Fatal("no reply reached answer_uri")
		return Reply{}
	}
}

func TestNewValidatesAndDefaults(t *testing.T) {
	t.Run("a Controller factory is required", func(t *testing.T) {
		if _, err := New(Options{}); err == nil {
			t.Fatal("New without a Controller factory must fail")
		}
	})
	t.Run("bind and logger default", func(t *testing.T) {
		s, err := New(Options{Controller: failingFactory})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if s.opts.Bind != ":5001" {
			t.Errorf("default bind = %q, want :5001", s.opts.Bind)
		}
		if s.logger == nil {
			t.Error("logger must default, never nil")
		}
	})
	t.Run("an explicit bind is kept", func(t *testing.T) {
		s, err := New(Options{Controller: failingFactory, Bind: "127.0.0.1:6001"})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if s.opts.Bind != "127.0.0.1:6001" {
			t.Errorf("bind = %q, want the one given", s.opts.Bind)
		}
	})
}

func TestMetrics(t *testing.T) {
	t.Run("injected counters are the ones returned", func(t *testing.T) {
		met := metrics.NewConnector()
		s, err := New(Options{Controller: failingFactory, Deps: plugin.Deps{Metrics: met}})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if s.Metrics() != met {
			t.Error("Metrics must return the injected connector")
		}
	})
	t.Run("defaults are never nil and stable", func(t *testing.T) {
		s, err := New(Options{Controller: failingFactory})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		first := s.Metrics()
		if first == nil {
			t.Fatal("Metrics must never be nil")
		}
		if s.Metrics() != first {
			t.Error("Metrics must return the same set on every call")
		}
	})
}

func TestEmptyFor(t *testing.T) {
	cases := []struct {
		name, testType string
		want           any
	}{
		{"multi_choice is an empty list, not null", "multi_choice", []string{}},
		{"single_choice is null", "single_choice", nil},
		{"action is null", "action", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := json.Marshal(emptyFor(tc.testType))
			want, _ := json.Marshal(tc.want)
			if string(got) != string(want) {
				t.Errorf("emptyFor(%s) = %s, want %s", tc.testType, got, want)
			}
		})
	}
}

// TestHandlerRejectsNonQuestions: anything that is not a POSTed JSON
// question is refused up front, never queued.
func TestHandlerRejectsNonQuestions(t *testing.T) {
	s, _ := facadeFor(t, failingFactory)
	front := httptest.NewServer(s.Handler())
	t.Cleanup(front.Close)

	resp, err := http.Get(front.URL + "/x-nmos/testquestion/1.0")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d, want 405", resp.StatusCode)
	}

	resp, err = http.Post(front.URL+"/x-nmos/testquestion/1.0", "application/json", strings.NewReader("{not json"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("undecodable question status = %d, want 400", resp.StatusCode)
	}
}

// TestHandlerAnswersThroughAnswerURI is the protocol end to end: 202 on
// the question, then the decided answer POSTed to answer_uri under the
// same question_id — on any path, so a tool version bump cannot 404 us.
func TestHandlerAnswersThroughAnswerURI(t *testing.T) {
	sink := newAnswerSink(t, http.StatusOK)

	// post sends q to a facade front and insists on the immediate 202.
	post := func(t *testing.T, s *Server, q Question) {
		t.Helper()
		front := httptest.NewServer(s.Handler())
		t.Cleanup(front.Close)
		q.AnswerURI = sink.srv.URL
		body, _ := json.Marshal(q)
		resp, err := http.Post(front.URL+"/x-nmos/testquestion/1.0", "application/json", strings.NewReader(string(body)))
		if err != nil {
			t.Fatalf("POST question: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("question status = %d, want 202", resp.StatusCode)
		}
	}

	t.Run("a decided single_choice reaches answer_uri", func(t *testing.T) {
		p := stdPlant(t)
		s, _ := facadeFor(t, p.controller("v1.3"))
		post(t, s, Question{TestType: "single_choice", QuestionID: "q-1",
			Question: "select the node you can see", Answers: answers(ghostID, nodeID)})
		rep := sink.wait(t)
		if rep.QuestionID != "q-1" || rep.AnswerResponse != "ans-b" {
			t.Errorf("reply = %+v, want q-1 answered ans-b (the registered node)", rep)
		}
	})
	t.Run("an undecidable multi_choice answers an empty list", func(t *testing.T) {
		s, logs := facadeFor(t, failingFactory)
		post(t, s, Question{TestType: "multi_choice", QuestionID: "q-2",
			Question: "select the receivers", Answers: answers(rcvAID)})
		rep := sink.wait(t)
		list, ok := rep.AnswerResponse.([]any)
		if rep.QuestionID != "q-2" || !ok || len(list) != 0 {
			t.Errorf("reply = %+v, want q-2 answered [] (never null)", rep)
		}
		if !strings.Contains(logs.String(), "could not answer") {
			t.Errorf("the failure must be logged next to the empty answer:\n%s", logs.String())
		}
	})
	t.Run("an unknown test_type answers null", func(t *testing.T) {
		s, _ := facadeFor(t, failingFactory)
		post(t, s, Question{TestType: "free_text", QuestionID: "q-3"})
		rep := sink.wait(t)
		if rep.QuestionID != "q-3" || rep.AnswerResponse != nil {
			t.Errorf("reply = %+v, want q-3 answered null", rep)
		}
	})
}

// TestAnswerDeliveryFailuresAreLogged: when the answer cannot be
// delivered the facade has nowhere else to put it, so the log line is
// the contract.
func TestAnswerDeliveryFailuresAreLogged(t *testing.T) {
	q := Question{TestType: "single_choice", QuestionID: "q-x", Question: "select the node", Answers: answers(nodeID)}
	cases := []struct {
		name   string
		uri    func(t *testing.T) string
		wantIn string
	}{
		{"an unusable answer_uri", func(*testing.T) string { return "://not-a-url" }, "build answer request"},
		{"an unreachable answer_uri", func(t *testing.T) string {
			dead := httptest.NewServer(http.NotFoundHandler())
			dead.Close()
			return dead.URL
		}, "post answer"},
		{"a refused answer", func(t *testing.T) string { return newAnswerSink(t, http.StatusInternalServerError).srv.URL }, "answer refused"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := stdPlant(t)
			s, logs := facadeFor(t, p.controller("v1.3"))
			q.AnswerURI = tc.uri(t)
			s.answer(context.Background(), q)
			if !strings.Contains(logs.String(), tc.wantIn) {
				t.Errorf("log lacks %q:\n%s", tc.wantIn, logs.String())
			}
		})
	}
}

// freeAddr returns a loopback address nothing is listening on right now.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func TestListenAndServe(t *testing.T) {
	t.Run("serves questions until the context ends", func(t *testing.T) {
		s, err := New(Options{Controller: failingFactory, Bind: freeAddr(t), Logger: quietLogger()})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- s.ListenAndServe(ctx) }()

		// Up = the facade answers the protocol's own refusal (GET is 405).
		deadline := time.Now().Add(20 * time.Second)
		for {
			resp, err := http.Get("http://" + s.opts.Bind + "/")
			if err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusMethodNotAllowed {
					t.Fatalf("GET status = %d, want 405 from the facade", resp.StatusCode)
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("facade never came up on %s: %v", s.opts.Bind, err)
			}
			<-time.After(20 * time.Millisecond)
		}

		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("ListenAndServe after cancel = %v, want nil", err)
			}
		case <-time.After(20 * time.Second):
			t.Fatal("ListenAndServe did not return after the context ended")
		}
	})
	t.Run("reports a bind it cannot take", func(t *testing.T) {
		taken, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		t.Cleanup(func() { _ = taken.Close() })
		s, err := New(Options{Controller: failingFactory, Bind: taken.Addr().String(), Logger: quietLogger()})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		if err := s.ListenAndServe(ctx); err == nil || errors.Is(err, http.ErrServerClosed) {
			t.Errorf("ListenAndServe on a taken port = %v, want the bind error", err)
		}
	})
}
