// Package hyperdeck implements a provider/simulator for the Blackmagic
// HyperDeck Ethernet Protocol.
package hyperdeck

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"

	"acp/internal/blackmagic-hyperdeck/codec"
	"acp/internal/export/canonical"
	"acp/internal/provider"
)

const DefaultPort = codec.DefaultPort

func init() {
	provider.Register(&Factory{})
}

type Factory struct{}

func (f *Factory) Meta() provider.Meta {
	return provider.Meta{
		Name:        "blackmagic-hyperdeck",
		DefaultPort: DefaultPort,
		Description: "Blackmagic HyperDeck Ethernet Protocol provider/simulator",
	}
}

func (f *Factory) New(logger *slog.Logger, tree *canonical.Export) provider.Provider {
	return NewServer(logger, tree)
}

type Server struct {
	logger *slog.Logger
	tree   *canonical.Export

	mu       sync.Mutex
	ln       net.Listener
	closed   bool
	state    state
	sessions map[*session]struct{}
}

type state struct {
	ProtocolVersion string
	Model           string
	UniqueID        string
	SlotCount       int
	SoftwareVersion string
	Name            string

	RemoteEnabled  bool
	RemoteOverride bool
	Transport      string
	Speed          int
	Timecode       string
	VideoFormat    string
	SlotID         int
	SlotName       string
	SlotStatus     string
	VolumeName     string
	RemainingSize  int64
	TotalSize      int64
	Notify         map[string]bool
}

func NewServer(logger *slog.Logger, tree *canonical.Export) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		logger:   logger,
		tree:     tree,
		state:    defaultState(),
		sessions: map[*session]struct{}{},
	}
}

func defaultState() state {
	return state{
		ProtocolVersion: "1.14",
		Model:           "HyperDeck Studio",
		UniqueID:        "dhs-hyperdeck-sim",
		SlotCount:       1,
		SoftwareVersion: "sim",
		Name:            "dhs HyperDeck Simulator",
		RemoteEnabled:   true,
		Transport:       "stopped",
		Speed:           0,
		Timecode:        "00:00:00:00",
		VideoFormat:     "1080p25",
		SlotID:          1,
		SlotName:        "slot 1",
		SlotStatus:      "mounted",
		VolumeName:      "DHS",
		RemainingSize:   60 * 60,
		TotalSize:       120 * 60,
		Notify: map[string]bool{
			"transport": false, "slot": false, "remote": false, "configuration": false,
			"dropped frames": false, "display timecode": false, "timeline position": false,
			"playrange": false, "cache": false, "dynamic range": false, "slate": false,
			"clips": false, "disk": false, "device info": false, "nas": false,
		},
	}
}

func (s *Server) Serve(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	go func() {
		<-ctx.Done()
		_ = s.Stop()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || s.isClosed() {
				return ctx.Err()
			}
			return err
		}
		sess := &session{srv: s, conn: conn, r: bufio.NewReader(conn)}
		s.mu.Lock()
		s.sessions[sess] = struct{}{}
		s.mu.Unlock()
		go sess.run()
	}
}

func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	var err error
	if s.ln != nil {
		err = s.ln.Close()
		s.ln = nil
	}
	for sess := range s.sessions {
		_ = sess.conn.Close()
	}
	return err
}

func (s *Server) SetValue(ctx context.Context, path string, val any) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch strings.ToLower(path) {
	case "remote.enabled":
		s.state.RemoteEnabled = asBool(val)
		return s.state.RemoteEnabled, nil
	case "remote.override":
		s.state.RemoteOverride = asBool(val)
		return s.state.RemoteOverride, nil
	case "transport.status":
		s.state.Transport = fmt.Sprint(val)
		return s.state.Transport, nil
	default:
		return nil, fmt.Errorf("blackmagic-hyperdeck: unsupported provider path %q", path)
	}
}

func (s *Server) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

type session struct {
	srv  *Server
	conn net.Conn
	r    *bufio.Reader
}

func (s *session) run() {
	defer func() {
		s.srv.mu.Lock()
		delete(s.srv.sessions, s)
		s.srv.mu.Unlock()
		_ = s.conn.Close()
	}()
	// PDF p.11, "Connection response": server delivers 500 connection
	// info immediately after connection.
	_ = s.write(500, "connection info", s.srv.connectionInfo())
	for {
		line, err := readClientLine(s.r)
		if err != nil {
			return
		}
		if line == "" {
			continue
		}
		if s.handle(line) {
			return
		}
	}
}

func (s *session) handle(line string) bool {
	name, params := parseCommand(line)
	switch name {
	case "quit":
		_ = s.conn.Close()
		return true
	case "ping", "playrange clear", "clips clear", "clips rebuild":
		_ = s.write(200, "ok", nil)
	case "help", "?":
		_ = s.writeLines(201, "help", []string{"device info", "remote", "transport info", "slot info", "notify", "play", "stop", "record", "ping", "quit"})
	case "commands":
		_ = s.writeLines(212, "commands", []string{`<commands>`, `  <command name="device info"/>`, `  <command name="transport info"/>`, `</commands>`})
	case "device info":
		_ = s.write(204, "device info", s.srv.deviceInfo())
	case "remote":
		_ = s.handleRemote(params)
	case "transport info":
		_ = s.write(208, "transport info", s.srv.transportInfo())
	case "slot info":
		_ = s.write(202, "slot info", s.srv.slotInfo(params))
	case "notify":
		_ = s.handleNotify(params)
	case "play":
		s.srv.setTransport("play", 100)
		_ = s.write(200, "ok", nil)
	case "stop":
		s.srv.setTransport("stopped", 0)
		_ = s.write(200, "ok", nil)
	case "record":
		s.srv.setTransport("record", 100)
		_ = s.write(200, "ok", nil)
	case "clips count":
		_ = s.write(214, "clips count", map[string]string{"clip count": "0"})
	default:
		_ = s.write(103, "unsupported", nil)
	}
	return false
}

func (s *session) handleRemote(params map[string]string) error {
	s.srv.mu.Lock()
	defer s.srv.mu.Unlock()
	if v, ok := params["enable"]; ok {
		s.srv.state.RemoteEnabled = parseBool(v)
		return codec.WriteResponse(s.conn, codec.Response{Code: 200, Text: "ok"})
	}
	if v, ok := params["override"]; ok {
		s.srv.state.RemoteOverride = parseBool(v)
		return codec.WriteResponse(s.conn, codec.Response{Code: 200, Text: "ok"})
	}
	return codec.WriteResponse(s.conn, codec.Response{Code: 210, Text: "remote info", Params: map[string]string{
		"enabled":  boolString(s.srv.state.RemoteEnabled),
		"override": boolString(s.srv.state.RemoteOverride),
	}})
}

func (s *session) handleNotify(params map[string]string) error {
	s.srv.mu.Lock()
	defer s.srv.mu.Unlock()
	for k, v := range params {
		if _, ok := s.srv.state.Notify[k]; ok {
			s.srv.state.Notify[k] = parseBool(v)
		}
	}
	out := map[string]string{}
	for k, v := range s.srv.state.Notify {
		out[k] = boolString(v)
	}
	if len(params) > 0 {
		return codec.WriteResponse(s.conn, codec.Response{Code: 200, Text: "ok"})
	}
	return codec.WriteResponse(s.conn, codec.Response{Code: 209, Text: "notify", Params: out})
}

func (s *session) write(code int, text string, params map[string]string) error {
	return codec.WriteResponse(s.conn, codec.Response{Code: code, Text: text, Params: params})
}

func (s *session) writeLines(code int, text string, lines []string) error {
	return codec.WriteResponse(s.conn, codec.Response{Code: code, Text: text, Lines: lines})
}

func (s *Server) connectionInfo() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]string{"protocol version": s.state.ProtocolVersion, "model": s.state.Model}
}

func (s *Server) deviceInfo() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]string{
		"protocol version": s.state.ProtocolVersion,
		"model":            s.state.Model,
		"unique id":        s.state.UniqueID,
		"slot count":       strconv.Itoa(s.state.SlotCount),
		"software version": s.state.SoftwareVersion,
		"name":             s.state.Name,
	}
}

func (s *Server) slotInfo(params map[string]string) map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	slot := s.state.SlotID
	if v := params["slot id"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			slot = n
		}
	}
	return map[string]string{
		"slot id":        strconv.Itoa(slot),
		"slot name":      s.state.SlotName,
		"device name":    "simulated disk",
		"status":         s.state.SlotStatus,
		"volume name":    s.state.VolumeName,
		"recording time": strconv.FormatInt(s.state.RemainingSize, 10),
		"video format":   s.state.VideoFormat,
		"blocked":        "false",
		"remaining size": strconv.FormatInt(s.state.RemainingSize, 10),
		"total size":     strconv.FormatInt(s.state.TotalSize, 10),
	}
}

func (s *Server) transportInfo() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]string{
		"status":             s.state.Transport,
		"speed":              strconv.Itoa(s.state.Speed),
		"slot id":            strconv.Itoa(s.state.SlotID),
		"slot name":          s.state.SlotName,
		"device name":        "simulated disk",
		"clip id":            "none",
		"single clip":        "false",
		"display timecode":   s.state.Timecode,
		"timecode":           s.state.Timecode,
		"video format":       s.state.VideoFormat,
		"loop":               "false",
		"timeline":           "0",
		"input video format": s.state.VideoFormat,
		"dynamic range":      "off",
		"reference locked":   "false",
	}
}

func (s *Server) setTransport(status string, speed int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Transport = status
	s.state.Speed = speed
}

func readClientLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		if err == io.EOF {
			return "", err
		}
		return "", err
	}
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	return strings.TrimSpace(line), nil
}

func parseCommand(line string) (string, map[string]string) {
	line = strings.TrimSpace(line)
	params := map[string]string{}
	i := strings.IndexByte(line, ':')
	if i < 0 {
		return strings.ToLower(line), params
	}
	name := strings.ToLower(strings.TrimSpace(line[:i]))
	rest := strings.TrimSpace(line[i+1:])
	for rest != "" {
		j := strings.IndexByte(rest, ':')
		if j < 0 {
			break
		}
		key := strings.TrimSpace(rest[:j])
		rest = strings.TrimSpace(rest[j+1:])
		next := nextParam(rest)
		val := strings.TrimSpace(rest[:next])
		params[key] = val
		rest = strings.TrimSpace(rest[next:])
	}
	return name, params
}

func nextParam(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' {
			continue
		}
		tail := s[i+1:]
		j := strings.IndexByte(tail, ':')
		if j > 0 && !strings.Contains(tail[:j], " ") {
			return i
		}
	}
	return len(s)
}

func parseBool(s string) bool { return strings.EqualFold(strings.TrimSpace(s), "true") }
func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func asBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return parseBool(x)
	default:
		return false
	}
}
