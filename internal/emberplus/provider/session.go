package emberplus

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dhs/internal/emberplus/codec/glow"
	"dhs/internal/emberplus/codec/s101"
	"dhs/internal/export/canonical"
)

// session is one live consumer connection. Single read loop; a send
// channel serialises outgoing S101 frames so the encoder + keepalive +
// announcement paths can fan in without synchronising a raw net.Conn.
type session struct {
	id        string
	conn      net.Conn
	reader    *s101.Reader
	writer    *s101.Writer
	srv       *server
	logger    *slog.Logger
	out       chan []byte
	closeOnce sync.Once
	closed    chan struct{}

	// subs is the set of OIDs this consumer subscribed to. Protected by
	// server.mu (subscription table fan-out is server-wide).
	subs map[string]struct{}

	// lastActive is the UnixNano of the most recent frame we *read*
	// from this peer (any frame — keepalive counts as activity, see
	// session.run). Updated lock-free via atomic; sampled by the
	// server's idle-sweeper. Default value 0 means "never active"; we
	// stamp it in newSession so the first sweep doesn't kill a session
	// that just connected.
	lastActive atomic.Int64
}

func newSession(srv *server, conn net.Conn) *session {
	s := &session{
		id:     conn.RemoteAddr().String(),
		conn:   conn,
		reader: s101.NewReader(conn),
		writer: s101.NewWriter(conn),
		srv:    srv,
		logger: srv.logger.With(slog.String("peer", conn.RemoteAddr().String())),
		out:    make(chan []byte, 32),
		closed: make(chan struct{}),
		subs:   map[string]struct{}{},
	}
	s.lastActive.Store(time.Now().UnixNano())
	return s
}

// run is the session lifecycle: start write pump, read frames until EOF
// or error, close on exit. Returns once the session is fully torn down.
func (s *session) run(ctx context.Context) {
	defer s.close()

	go s.writePump(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		frame, err := s.reader.ReadFrame()
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				s.logger.Debug("read frame", slog.String("err", err.Error()))
			}
			return
		}
		// Activity stamp for the idle-sweeper. Any successful frame
		// read (including keepalive) keeps the session alive — only
		// truly silent peers get swept.
		s.lastActive.Store(time.Now().UnixNano())
		if s.srv != nil && s.srv.metrics != nil {
			s.srv.metrics.ObserveCmdRx(frame.Command, len(frame.Payload))
		}
		if err := s.handleFrame(frame); err != nil {
			s.logger.Debug("handle frame", slog.String("err", err.Error()))
			// non-fatal — keep reading.
		}
	}
}

// writePump drains s.out onto the wire. Closes the conn on ctx-cancel or
// channel close so the read side unblocks too.
// maxS101Payload caps each S101 frame's BER payload. Spec p.10
// recommends ≤1024; most shipping consumers (libember-cpp,
// EmberViewer, Lawo VSM) handle up to 1290, but staying at 1024
// keeps us inside every implementation's safe window. Payloads
// larger than this are split across FlagFirst / middle / FlagLast
// frames (consumer reassembles per session.go).
const maxS101Payload = 1024

func (s *session) writePump(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.closed:
			return
		case payload, ok := <-s.out:
			if !ok {
				return
			}
			if err := s.writeEmBERChunks(payload); err != nil {
				s.logger.Debug("write frame", slog.String("err", err.Error()))
				return
			}
		}
	}
}

// writeEmBERChunks splits payload into one or more S101 frames per
// spec p.10 (FlagSingle for ≤maxS101Payload, FlagFirst / middle /
// FlagLast for larger). Fixes "frame too large" rejection observed
// against the 1000×1000 dynamic-matrix walk reply.
func (s *session) writeEmBERChunks(payload []byte) error {
	// Counted once per logical message rather than per S101 chunk: the
	// chunking is a transport detail, and a consumer sees one message.
	if s.srv != nil && s.srv.metrics != nil {
		s.srv.metrics.ObserveCmdTx(s101.CmdEmBER, len(payload), 0)
	}
	if len(payload) <= maxS101Payload {
		return s.writer.WriteFrame(&s101.Frame{
			Slot:    s101.SlotDefault,
			MsgType: s101.MsgEmBER,
			Command: s101.CmdEmBER,
			Version: s101.VersionS101,
			Flags:   s101.FlagSingle,
			DTD:     s101.DTDGlow,
			Payload: payload,
		})
	}
	// Multi-packet. First chunk: FlagFirst. Middle: flags=0.
	// Last chunk: FlagLast.
	first := true
	for offset := 0; offset < len(payload); offset += maxS101Payload {
		end := offset + maxS101Payload
		if end > len(payload) {
			end = len(payload)
		}
		last := end == len(payload)
		var flags byte
		switch {
		case first:
			flags = s101.FlagFirst
		case last:
			flags = s101.FlagLast
		default:
			flags = 0
		}
		first = false
		if err := s.writer.WriteFrame(&s101.Frame{
			Slot:    s101.SlotDefault,
			MsgType: s101.MsgEmBER,
			Command: s101.CmdEmBER,
			Version: s101.VersionS101,
			Flags:   flags,
			DTD:     s101.DTDGlow,
			Payload: payload[offset:end],
		}); err != nil {
			return err
		}
	}
	return nil
}

// send enqueues a BER-encoded Glow payload for transmission. Non-blocking
// if the queue has room; drops the frame with a warning if the consumer
// is slow enough to fill 32 entries — prevents a stuck consumer from
// ballooning memory or blocking the broadcast fan-out.
func (s *session) send(payload []byte) {
	select {
	case s.out <- payload:
	default:
		s.logger.Warn("send queue full, dropping frame")
	}
}

func (s *session) close() {
	s.closeOnce.Do(func() {
		close(s.closed)
		_ = s.conn.Close()
		s.srv.dropSession(s)
	})
}

// handleFrame processes one incoming S101 frame. Keepalives get an
// immediate response; EmBER frames are decoded as Glow and dispatched.
func (s *session) handleFrame(f *s101.Frame) error {
	switch f.Command {
	case s101.CmdKeepAliveReq:
		// Use the canonical KeepAlive-response builder rather than
		// hand-rolling the frame; the hand-rolled version above had
		// MsgType=MsgKeepAlive(0x01) which EmberPlusView decoded as
		// "Unexpected message: msg=1" and rejected.
		// NewKeepAliveResponse sets MsgType=MsgEmBER(0x0E) like every
		// shipping S101 stack expects (libember, EmberViewer, Lawo).
		if err := s.writer.WriteFrame(s101.NewKeepAliveResponse()); err != nil {
			return err
		}
		if s.srv != nil && s.srv.metrics != nil {
			s.srv.metrics.ObserveCmdTx(s101.CmdKeepAliveResp, 0, 0)
		}
		return nil
	case s101.CmdKeepAliveResp:
		return nil
	case s101.CmdEmBER:
		return s.handleEmber(f.Payload)
	default:
		return nil
	}
}

// handleEmber parses a Glow-encoded request and dispatches it.
//
// Three inbound shapes are handled:
//
//  1. Command request — GetDirectory (32) / Subscribe (30) / Unsubscribe (31)
//     / Invoke (33). Invoke extracts the Invocation, runs the registered
//     function callback, and replies with InvocationResult.
//
//  2. SetValue request — a QualifiedParameter (or nested Node → Parameter)
//     carrying `contents.value`. Applied via server.SetValue; the resulting
//     announcement is broadcast to subscribers including the sender.
//
//  3. Matrix connection request — a QualifiedMatrix (or nested Node → Matrix)
//     carrying [CTX 5] connections with operation=absolute|connect|disconnect.
//     Applied to the tree and echoed back + broadcast to subscribers.
func (s *session) handleEmber(payload []byte) error {
	els, err := glow.DecodeRoot(payload)
	if err != nil {
		return fmt.Errorf("decode glow: %w", err)
	}

	if cmd, path := findCommandInElements(els); cmd != nil {
		oid := oidFromPath(path)
		switch cmd.Number {
		case glow.CmdGetDirectory:
			s.logger.Debug("get directory", slog.String("oid", oid))
			return s.replyGetDirectory(oid)
		case glow.CmdSubscribe:
			s.srv.subscribe(s, oid)
			return nil
		case glow.CmdUnsubscribe:
			s.srv.unsubscribe(s, oid)
			return nil
		case glow.CmdInvoke:
			s.handleInvoke(oid, cmd.Invocation)
			return nil
		}
		return nil
	}

	if path, conns, ok := findMatrixConnectionsInElements(els); ok {
		oid := oidFromPath(path)
		for _, c := range conns {
			s.logger.Debug("matrix connection in",
				slog.String("oid", oid),
				slog.Int64("target", c.Target),
				slog.Any("sources", c.Sources),
				slog.String("op", c.Operation))
		}
		post, err := s.srv.applyMatrixConnections(oid, conns)
		if err != nil {
			s.logger.Debug("matrix connection failed", slog.String("oid", oid), slog.String("err", err.Error()))
			return nil
		}
		for _, c := range post {
			s.logger.Debug("matrix connection post",
				slog.String("oid", oid),
				slog.Int64("target", c.Target),
				slog.Any("sources", c.Sources),
				slog.String("disp", c.Disposition))
		}
		s.srv.broadcastMatrixConnections(oid, post, s)
		return nil
	}

	if path, val, ok := findSetValueInElements(els); ok {
		oid := oidFromPath(path)
		s.logger.Debug("set value", slog.String("oid", oid), slog.Any("val", val))
		if _, err := s.srv.SetValue(context.TODO(), oid, val); err != nil {
			s.logger.Debug("set value failed", slog.String("oid", oid), slog.String("err", err.Error()))
			return nil
		}
		// Strict consumers (EmberViewer) don't auto-subscribe — they expect
		// the provider to echo every SetValue with a QualifiedParameter
		// announcement carrying the confirmed value. Without the echo
		// they keep retrying the set. Send it regardless of subscription.
		if e, eok := s.srv.tree.lookupOID(oid); eok {
			if p, pok := e.el.(*canonical.Parameter); pok {
				s.send(s.srv.encodeParamAnnouncement(e, p))
			}
		}
		return nil
	}
	// Fall-through: payload decoded but matched none of {command,
	// matrix-connection, set-value}. Surface the element kinds so we can
	// SEE what unknown client gestures (Cerebrum's "Change..." button
	// being a current suspect) put on the wire. Without this trace,
	// unrecognised frames vanish silently and look identical to "client
	// sent nothing at all".
	kinds := make([]string, 0, len(els))
	for _, e := range els {
		switch {
		case e.Parameter != nil:
			kinds = append(kinds, fmt.Sprintf("param(num=%d,path=%v,hasVal=%v)", e.Parameter.Number, e.Parameter.Path, e.Parameter.Value != nil))
		case e.Node != nil:
			kinds = append(kinds, fmt.Sprintf("node(num=%d,path=%v,kids=%d)", e.Node.Number, e.Node.Path, len(e.Node.Children)))
		case e.Matrix != nil:
			kinds = append(kinds, fmt.Sprintf("matrix(num=%d,conns=%d)", e.Matrix.Number, len(e.Matrix.Connections)))
		case e.Function != nil:
			kinds = append(kinds, fmt.Sprintf("function(num=%d)", e.Function.Number))
		default:
			kinds = append(kinds, "unknown")
		}
	}
	s.logger.Debug("inbound frame unhandled", slog.Any("kinds", kinds), slog.Int("payload_bytes", len(payload)))
	return nil
}

// handleInvoke runs the registered function callback for oid and replies
// with an InvocationResult. A missing function or callback error yields
// success=false with an empty result tuple (spec p.92).
func (s *session) handleInvoke(oid string, inv *glow.Invocation) {
	var invID int32
	var args []any
	if inv != nil {
		invID = inv.InvocationID
		args = inv.Arguments
	}
	s.logger.Debug("invoke", slog.String("oid", oid), slog.Int("invocation_id", int(invID)), slog.Int("argc", len(args)))

	result, ok := s.srv.invokeFunction(oid, args)
	payload := s.srv.encodeInvocationResult(invID, ok, result)
	s.logger.Debug("invoke reply", slog.Int("invocation_id", int(invID)), slog.Bool("success", ok), slog.Int("bytes", len(payload)))
	s.send(payload)
}

func (s *session) replyGetDirectory(oid string) error {
	// Empty oid means consumer sent a bare Command(32) at the root of the
	// element collection — spec pattern: reply with the single top-level
	// element (identifier only). Non-empty oid means consumer sent the
	// Command nested in QualifiedNode(P); reply with P's children flat.
	bareRoot := oid == ""
	var e *entry
	if bareRoot {
		e = s.srv.tree.rootEntry()
	} else {
		var ok bool
		e, ok = s.srv.tree.lookupOID(oid)
		if !ok {
			return fmt.Errorf("unknown path %q", oid)
		}
	}
	payload, err := s.srv.encodeGetDirReply(e, bareRoot)
	if err != nil {
		return fmt.Errorf("encode reply: %w", err)
	}
	s.send(payload)

	// Spec p.88: "As soon as a consumer issues a GetDirectory command
	// on a matrix object, it implicitly subscribes to matrix connection
	// changes." Without this, salvo recalls / external crosspoint
	// changes broadcast to nobody — cells don't update on the client.
	//
	// Strict reading: only the direct GetDir(path=matrix) triggers. But
	// most consumers (EmberViewer included) read matrix contents from a
	// parent node's reply instead of re-walking, so we also subscribe
	// the session to any matrix child in the reply — keeps crosspoint
	// tallies reaching the client regardless of walking style.
	if !bareRoot {
		if _, ok := e.el.(*canonical.Matrix); ok {
			s.srv.subscribe(s, oid)
		}
		for _, child := range e.el.Common().Children {
			if _, ok := child.(*canonical.Matrix); ok {
				s.srv.subscribe(s, child.Common().OID)
			}
		}
	}
	return nil
}

// findCommandInElements walks the decoded element tree and returns the
// first Command found along with the path to its containing element.
// Path is built from QualifiedNode.Path / QualifiedParameter.Path if
// present, else from chained Node.Number values for the non-qualified
// form.
//
// All four container kinds (Node, Parameter, Matrix, Function) can hold
// a child Command per spec p.86 — Function is particularly important
// because Invoke commands arrive as QualifiedFunction(path){children:
// [Command(Invoke)]}; if we don't descend into Function.Children the
// Invoke gets silently dropped and the consumer times out.
func findCommandInElements(els []glow.Element) (*glow.Command, []uint32) {
	for _, e := range els {
		if e.Command != nil {
			return e.Command, nil
		}
		if e.Node != nil {
			base := nodeBasePath(e.Node)
			if c, sub := findCommandInElements(e.Node.Children); c != nil {
				return c, append(append([]uint32{}, base...), sub...)
			}
		}
		if e.Parameter != nil {
			base := paramBasePath(e.Parameter)
			if c, sub := findCommandInElements(e.Parameter.Children); c != nil {
				return c, append(append([]uint32{}, base...), sub...)
			}
		}
		if e.Function != nil {
			base := funcBasePath(e.Function)
			if c, sub := findCommandInElements(e.Function.Children); c != nil {
				return c, append(append([]uint32{}, base...), sub...)
			}
		}
		if e.Matrix != nil {
			base := matrixBasePath(e.Matrix)
			if c, sub := findCommandInElements(e.Matrix.Children); c != nil {
				return c, append(append([]uint32{}, base...), sub...)
			}
		}
	}
	return nil, nil
}

func funcBasePath(f *glow.Function) []uint32 {
	if len(f.Path) > 0 {
		return toUint32(f.Path)
	}
	return []uint32{uint32(f.Number)}
}

// findMatrixConnectionsInElements walks the decoded tree looking for a
// Matrix (or QualifiedMatrix) that carries at least one Connection entry.
// Returns the absolute matrix path plus the connection list translated to
// canonical form. Like the Parameter walker, it concatenates wrapping
// Node / QualifiedNode path segments if the consumer nested the Matrix.
func findMatrixConnectionsInElements(els []glow.Element) ([]uint32, []canonical.MatrixConnection, bool) {
	for _, e := range els {
		if e.Matrix != nil && len(e.Matrix.Connections) > 0 {
			return matrixBasePath(e.Matrix), convertConnections(e.Matrix.Connections), true
		}
		if e.Node != nil {
			base := nodeBasePath(e.Node)
			if sub, conns, ok := findMatrixConnectionsInElements(e.Node.Children); ok {
				return append(append([]uint32{}, base...), sub...), conns, true
			}
		}
		if e.Matrix != nil {
			base := matrixBasePath(e.Matrix)
			if sub, conns, ok := findMatrixConnectionsInElements(e.Matrix.Children); ok {
				return append(append([]uint32{}, base...), sub...), conns, true
			}
		}
	}
	return nil, nil, false
}

func matrixBasePath(m *glow.Matrix) []uint32 {
	if len(m.Path) > 0 {
		return toUint32(m.Path)
	}
	if m.Number != 0 {
		return []uint32{uint32(m.Number)}
	}
	return nil
}

// convertConnections translates glow.Connection entries (wire form) to
// canonical.MatrixConnection entries the provider state machine consumes.
// operation / disposition enums become their string equivalents.
func convertConnections(conns []glow.Connection) []canonical.MatrixConnection {
	out := make([]canonical.MatrixConnection, 0, len(conns))
	for _, c := range conns {
		sources := make([]int64, 0, len(c.Sources))
		for _, s := range c.Sources {
			sources = append(sources, int64(s))
		}
		out = append(out, canonical.MatrixConnection{
			Target:      int64(c.Target),
			Sources:     sources,
			Operation:   connOperationName(c.Operation),
			Disposition: connDispositionName(c.Disposition),
		})
	}
	return out
}

func connOperationName(op int64) string {
	switch op {
	case glow.ConnOpConnect:
		return canonical.ConnOpConnect
	case glow.ConnOpDisconnect:
		return canonical.ConnOpDisconnect
	}
	return canonical.ConnOpAbsolute
}

func connDispositionName(d int64) string {
	switch d {
	case glow.ConnDispModified:
		return canonical.ConnDispModified
	case glow.ConnDispPending:
		return canonical.ConnDispPending
	case glow.ConnDispLocked:
		return canonical.ConnDispLocked
	}
	return canonical.ConnDispTally
}

// findSetValueInElements walks the decoded tree looking for a Parameter
// (or QualifiedParameter) that carries a non-nil contents.value. Returns
// the absolute path and the new value. Consumers may wrap the Parameter
// in any depth of Node / QualifiedNode / QualifiedParameter layers — the
// walker concatenates their Path / Number segments in order.
func findSetValueInElements(els []glow.Element) ([]uint32, any, bool) {
	for _, e := range els {
		if e.Parameter != nil {
			base := paramBasePath(e.Parameter)
			if e.Parameter.Value != nil {
				return base, e.Parameter.Value, true
			}
			if sub, v, ok := findSetValueInElements(e.Parameter.Children); ok {
				return append(append([]uint32{}, base...), sub...), v, true
			}
		}
		if e.Node != nil {
			base := nodeBasePath(e.Node)
			if sub, v, ok := findSetValueInElements(e.Node.Children); ok {
				return append(append([]uint32{}, base...), sub...), v, true
			}
			_ = base
		}
	}
	return nil, nil, false
}

// nodeBasePath / paramBasePath return the path segment an element
// contributes when the walker concatenates nested wrappers.
//
// Number=0 is a LEGAL sub-identifier (e.g. label/target "t-0" sits at
// .0 under its parent). We deliberately include it — stripping 0 broke
// SetValue on any parameter whose number is 0. The cost of including
// a genuinely-absent Number=0 is a path like "0" that fails lookup
// cleanly; the cost of dropping a real .0 is a silently wrong SetValue.
func nodeBasePath(n *glow.Node) []uint32 {
	if len(n.Path) > 0 {
		return toUint32(n.Path)
	}
	return []uint32{uint32(n.Number)}
}

func paramBasePath(p *glow.Parameter) []uint32 {
	if len(p.Path) > 0 {
		return toUint32(p.Path)
	}
	return []uint32{uint32(p.Number)}
}

func oidFromPath(parts []uint32) string {
	if len(parts) == 0 {
		return ""
	}
	sb := strings.Builder{}
	for i, p := range parts {
		if i > 0 {
			sb.WriteByte('.')
		}
		fmt.Fprintf(&sb, "%d", p)
	}
	return sb.String()
}

func toUint32(in []int32) []uint32 {
	out := make([]uint32, len(in))
	for i, v := range in {
		out[i] = uint32(v)
	}
	return out
}
