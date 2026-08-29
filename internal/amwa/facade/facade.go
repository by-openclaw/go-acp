// Package facade implements the AMWA NMOS Testing Facade — the piece
// that lets the AMWA Testing Tool score a CONTROLLER.
//
// The tool cannot drive a controller the way it drives a Node. A Node
// has APIs the tool calls directly; a controller is the thing that does
// the calling, so the tool instead asks questions ("select the Senders
// you can see", "perform an immediate activation between these two")
// and waits for answers. Normally a human answers them in a UI. This
// package answers them by actually driving [consumer.Controller], which
// is what makes the run reproducible instead of an operator exercise.
//
// Protocol, from the tool's TestingFacadeUtils:
//
//	tool  -> POST question to us          we MUST reply 202 immediately
//	us    -> POST answer to answer_uri    {question_id, answer_response}
//
// The 202-then-answer split matters: the tool blocks on an async queue
// waiting for the answer, so doing the work before replying would
// deadlock the run against its own HTTP timeout. It matters twice over
// for the monitor questions ("press Next as soon as the NCuT detects
// ..."), where the right answer arrives minutes after the question.
//
// The question/answer semantics live in decide.go; this file is only
// the HTTP transport.
package facade

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"dhs/internal/amwa/consumer"
)

// Question is what the tool POSTs to the facade.
type Question struct {
	TestType    string   `json:"test_type"` // single_choice | multi_choice | action
	QuestionID  string   `json:"question_id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Question    string   `json:"question"`
	Answers     []Answer `json:"answers"`
	Timeout     int      `json:"timeout"`
	AnswerURI   string   `json:"answer_uri"`

	// Metadata carries the machine-readable form of what the prose
	// question asks. The tool documents it as "Test details to assist
	// fully automated testing" — it is the difference between guessing
	// at English and doing exactly what was asked.
	Metadata *Metadata `json:"metadata"`
}

// Answer is one option the tool offers.
type Answer struct {
	AnswerID      string   `json:"answer_id"`
	DisplayAnswer string   `json:"display_answer"`
	Resource      Resource `json:"resource"`
}

// Resource identifies the NMOS resource an answer stands for. The id is
// the only field worth matching on — labels are mutable and non-unique.
type Resource struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// Metadata names the resources an "action" question wants acted upon.
type Metadata struct {
	Sender   *Resource `json:"sender"`
	Receiver *Resource `json:"receiver"`
}

// Reply is what we POST back to answer_uri.
type Reply struct {
	QuestionID     string `json:"question_id"`
	AnswerResponse any    `json:"answer_response"`
}

// Options configures the facade.
type Options struct {
	Logger *slog.Logger

	// Bind is the listen address, e.g. ":5001".
	Bind string

	// Controller supplies a Controller pointed at whatever registry the
	// tool stood up. Called per question rather than held, because the
	// tool re-registers resources between tests and a cached catalogue
	// would answer about a registry state that no longer exists.
	Controller func(context.Context) (*consumer.Controller, error)
}

// Server is the facade HTTP server.
type Server struct {
	logger *slog.Logger
	opts   Options
	client *http.Client
}

// New builds a facade Server.
func New(opts Options) (*Server, error) {
	if opts.Controller == nil {
		return nil, fmt.Errorf("nmos/facade: Controller factory is required")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Bind == "" {
		opts.Bind = ":5001"
	}
	return &Server{
		logger: opts.Logger,
		opts:   opts,
		client: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// Handler returns the HTTP handler. The tool POSTs to
// /x-nmos/testquestion/<version>; we accept any path so a version bump
// in the tool does not silently 404 every question.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleQuestion)
	return mux
}

// ListenAndServe runs the facade until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	srv := &http.Server{Addr: s.opts.Bind, Handler: s.Handler()}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()
	s.logger.Info("nmos/facade: listening", "bind", s.opts.Bind)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) handleQuestion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var q Question
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		s.logger.Warn("nmos/facade: undecodable question", "err", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// 202 FIRST, work second. The tool blocks on its answer queue; if we
	// held the connection open while walking a registry it would time
	// out the POST and fail the test on a transport error rather than
	// on anything we answered.
	w.WriteHeader(http.StatusAccepted)

	s.logger.Info("nmos/facade: question",
		"id", q.QuestionID, "type", q.TestType, "answers", len(q.Answers))

	go s.answer(context.Background(), q)
}

// answer works out the response and posts it back.
func (s *Server) answer(ctx context.Context, q Question) {
	// Generous ceiling: the monitor questions legitimately take up to
	// 60s of tool-side delay plus a 30s answer window.
	ctx, cancel := context.WithTimeout(ctx, 150*time.Second)
	defer cancel()

	resp, err := s.decide(ctx, q)
	if err != nil {
		// Still answer. A silent facade times the tool out after
		// CONTROLLER_TESTING_TIMEOUT and reports nothing useful; an
		// empty answer at least fails the specific test with a result
		// the operator can read next to our log line.
		s.logger.Error("nmos/facade: could not answer", "id", q.QuestionID, "err", err)
		resp = emptyFor(q.TestType)
	}
	s.logger.Info("nmos/facade: answering", "id", q.QuestionID, "response", resp)

	body, _ := json.Marshal(Reply{QuestionID: q.QuestionID, AnswerResponse: resp})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, q.AnswerURI, bytes.NewReader(body))
	if err != nil {
		s.logger.Error("nmos/facade: build answer request", "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	r, err := s.client.Do(req)
	if err != nil {
		s.logger.Error("nmos/facade: post answer", "uri", q.AnswerURI, "err", err)
		return
	}
	defer func() { _ = r.Body.Close() }()
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		s.logger.Error("nmos/facade: answer refused", "status", r.StatusCode)
	}
}

// emptyFor is the shape of "I could not answer" for each question type.
// multi_choice must be a list, not null — the tool treats null as a
// protocol error rather than as "none selected".
func emptyFor(testType string) any {
	if testType == "multi_choice" {
		return []string{}
	}
	return nil
}
