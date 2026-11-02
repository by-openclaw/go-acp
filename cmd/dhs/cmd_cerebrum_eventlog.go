package main

import (
	"encoding/json"
	"log/slog"
	"strings"
	"sync/atomic"

	"dhs/internal/cerebrum-nb/codec"
)

// cerebrumEventLog sends subscribed events to the log sinks (file, syslog):
// one record per change, the same record never twice. The terminal keeps
// printing every row; a sink is what an operator searches, so a repeat of
// the state it already holds is noise there. Rows answering a subscribe arrive
// before its ACK and carry no MTID: they are the state at that moment, not a
// change, so until start they set the baseline and log nothing.
//
// A record is keyed by what it is about, so the memory is one short string per
// crosspoint or lock actually seen, however large the matrix.
type cerebrumEventLog struct {
	logger *slog.Logger
	live   atomic.Bool
	last   map[string]string
}

// baseline holds the log back: a session is about to subscribe.
func (l *cerebrumEventLog) baseline() {
	if l != nil {
		l.live.Store(false)
	}
}

// start opens the log: every subscribe has been answered.
func (l *cerebrumEventLog) start() {
	if l != nil {
		l.live.Store(true)
	}
}

// newCerebrumEventLog is nil, which records nothing, when no sink exists.
func newCerebrumEventLog(cf *cerebrumFlags) *cerebrumEventLog {
	if cf == nil || cf.logger == nil || !cf.hasLogSink() {
		return nil
	}
	return &cerebrumEventLog{logger: cf.logger, last: map[string]string{}}
}

func (l *cerebrumEventLog) record(f *codec.Frame, srcNames map[string]string) {
	if l == nil {
		return
	}
	msg, key, state, attrs := cerebrumEventRecord(f, srcNames)
	if msg == "" {
		return
	}
	prev, seen := l.last[key]
	l.last[key] = state
	if !l.live.Load() || (seen && prev == state) {
		return
	}
	l.logger.Info(msg, attrs...)
}

// cerebrumEventRecord turns one frame into a log record: an empty msg means
// the frame is not an event a sink wants.
func cerebrumEventRecord(f *codec.Frame, srcNames map[string]string) (msg, key, state string, attrs []any) {
	switch f.Kind {
	case codec.KindRoutingChange:
		rc := f.Routing
		src := rc.SrceID
		if rc.Type == "ROUTE" && rc.RouteSourceID != "" {
			src = rc.RouteSourceID
		}
		name := rc.SrceName
		if name == "" {
			name = srcNames[src]
		}
		key = strings.Join([]string{rc.Type, rc.IPAddress, rc.DeviceName, rc.DestID, rc.LevelID}, "|")
		if rc.Type == "ROUTE" && cerebrumNoRouteSentinel(src) {
			src = "unrouted"
		}
		state = src
		attrs = []any{
			slog.String("type", rc.Type), slog.String("router", rc.IPAddress),
			slog.String("dest", rc.DestID), slog.String("dest_name", rc.DestName),
			slog.String("level", rc.LevelID), slog.String("level_name", rc.LevelName),
			slog.String("srce", src), slog.String("srce_name", name),
		}
		if rc.Type != "ROUTE" {
			// A lock or mnemonic row names its source in the key, not the state.
			key += "|" + src
			state = ""
		}
		if rc.Lock != nil {
			state += "|" + string(rc.Lock.LockState) + "|" + rc.Lock.LockedBy
			attrs = append(attrs, slog.String("lock", string(rc.Lock.LockState)), slog.String("locked_by", rc.Lock.LockedBy))
		}
		return "cerebrum_routing_change", key, state, attrs
	case codec.KindCategoryChange:
		return cerebrumStateRecord("cerebrum_category_change", f.Category.Type+"|"+f.Category.Category, f.Category)
	case codec.KindSalvoChange:
		return cerebrumStateRecord("cerebrum_salvo_change", f.Salvo.Type+"|"+f.Salvo.Group+"|"+f.Salvo.Instance, f.Salvo)
	case codec.KindDeviceChange:
		return cerebrumStateRecord("cerebrum_device_change", f.Device.Type+"|"+f.Device.IPAddress+"|"+f.Device.DeviceName, f.Device)
	}
	return "", "", "", nil
}

// cerebrumStateRecord compares a whole event by its JSON form, nested rows
// included.
func cerebrumStateRecord(msg, key string, body any) (string, string, string, []any) {
	raw, err := json.Marshal(body)
	if err != nil {
		return "", "", "", nil
	}
	return msg, key, string(raw), []any{slog.String("key", key), slog.String("event", string(raw))}
}
