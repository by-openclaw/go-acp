package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"dhs/internal/cerebrum-nb/codec"
)

func routeFrame(mtid, src string) *codec.Frame {
	return &codec.Frame{Kind: codec.KindRoutingChange, MTID: mtid, Routing: &codec.RoutingChange{
		Type: "ROUTE", IPAddress: "0.0.0.0", DestID: "16", LevelID: "1", RouteSourceID: src,
	}}
}

func TestEventLogOneRecordPerChange(t *testing.T) {
	var buf bytes.Buffer
	l := &cerebrumEventLog{logger: slog.New(slog.NewTextHandler(&buf, nil)), last: map[string]string{}}
	count := func() int { return strings.Count(buf.String(), "cerebrum_routing_change") }

	l.record(routeFrame("", "1"), nil) // the subscribe's answer: baseline
	if count() != 0 {
		t.Fatalf("a snapshot row was logged: %s", buf.String())
	}
	l.start()
	l.record(routeFrame("", "1"), nil) // the same state again
	if count() != 0 {
		t.Fatalf("a repeat of the baseline was logged: %s", buf.String())
	}
	l.record(routeFrame("", "2"), map[string]string{"2": "CONVERT 2"})
	l.record(routeFrame("", "2"), nil)
	if count() != 1 {
		t.Fatalf("records = %d, want 1: %s", count(), buf.String())
	}
	if !strings.Contains(buf.String(), "CONVERT 2") {
		t.Errorf("the source name is missing: %s", buf.String())
	}
	l.record(routeFrame("", "1"), nil)
	if count() != 2 {
		t.Fatalf("records = %d, want 2", count())
	}
}

func TestEventLogLevelsAreSeparateCrosspoints(t *testing.T) {
	var buf bytes.Buffer
	l := &cerebrumEventLog{logger: slog.New(slog.NewTextHandler(&buf, nil)), last: map[string]string{}}
	l.start()
	a := routeFrame("", "1")
	b := routeFrame("", "1")
	b.Routing.LevelID = "2"
	l.record(a, nil)
	l.record(b, nil)
	if n := strings.Count(buf.String(), "cerebrum_routing_change"); n != 2 {
		t.Fatalf("records = %d, want 2", n)
	}
}

func TestEventLogTheUnroutedMarkerMovingIsNotAChange(t *testing.T) {
	var buf bytes.Buffer
	l := &cerebrumEventLog{logger: slog.New(slog.NewTextHandler(&buf, nil)), last: map[string]string{}}
	l.start()
	l.record(routeFrame("", "4294967292"), nil)
	l.record(routeFrame("", "4294967294"), nil)
	if n := strings.Count(buf.String(), "cerebrum_routing_change"); n != 1 {
		t.Fatalf("records = %d, want 1: %s", n, buf.String())
	}
}

func TestEventLogWithoutASinkRecordsNothing(t *testing.T) {
	var l *cerebrumEventLog
	l.record(routeFrame("", "1"), nil)
	if newCerebrumEventLog(nil) != nil {
		t.Error("no flags, no sink: want nil")
	}
}
