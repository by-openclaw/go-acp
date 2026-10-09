package main

import (
	"bytes"
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/rrcs/codec"
)

// What differs between two readings: keys, objects, ports, client cards.
func TestRRCSModelDiff(t *testing.T) {
	port := func(sel float64) *rrcsPort {
		return &rrcsPort{Path: "net.1.node.63.port.1073.in", Label: "I.-7.50", ObjectID: 7,
			Raw: map[string]any{"Label": "I.-7.50", "PortAes67Input": map[string]any{"Selection": sel, "Protocol": float64(5)}}}
	}
	before := &rrcsModel{
		Keys: []*rrcsKey{
			{Path: "p.key.0.1.1", PortLabel: "NOC1", CommandType: "call-to-port-cmd", Target: "a"},
			{Path: "p.key.0.1.2", PortLabel: "NOC1", CommandType: "reply-cmd"},
		},
		Objects: map[string][]*rrcsObject{
			"conference": {{Path: "conference.1", ObjectID: 1, Label: "OLD", LongName: "Conf one"}, {Path: "conference.2", ObjectID: 2, LongName: "Conf two"}},
		},
		Ports: []*rrcsPort{port(1)},
		Cards: []*rrcsCard{{Path: "net.1.node.60.card.1", LongName: "C1", Raw: map[string]any{"Ptp": map[string]any{"PTP": float64(100)}}}},
	}
	after := &rrcsModel{
		Keys: []*rrcsKey{
			{Path: "p.key.0.1.1", PortLabel: "NOC1", CommandType: "call-to-port-cmd", Target: "b"},
			{Path: "p.key.0.1.14", PortLabel: "NOC1", CommandType: "call-to-port-cmd", Target: "c"},
		},
		Objects: map[string][]*rrcsObject{
			"conference": {{Path: "conference.1", ObjectID: 1, Label: "NEW", LongName: "Conf one"}, {Path: "conference.3", ObjectID: 3, LongName: "Conf three"}},
		},
		Ports: []*rrcsPort{port(2)},
		Cards: []*rrcsCard{{Path: "net.1.node.60.card.1", LongName: "C1", Raw: map[string]any{"Ptp": map[string]any{"PTP": float64(101)}}}},
	}
	got := map[string]bool{}
	for _, l := range rrcsModelDiff(before, after) {
		got[l.Path+" "+l.Label+" = "+l.Value+" | "+l.Detail] = true
		if l.Event != "ConfigurationChange" {
			t.Errorf("event %q", l.Event)
		}
	}
	for _, want := range []string{
		"p.key.0.1.1 Function = call-to-port-cmd b | was call-to-port-cmd a",
		"p.key.0.1.2 Function =  | removed: was reply-cmd",
		"p.key.0.1.14 Function = call-to-port-cmd c | added",
		"conference.1 Label = NEW | was OLD",
		"conference.2 Object = deleted | ",
		"conference.3 Object = created | ",
		"net.1.node.63.port.1073.in PortAes67Input.Selection = 2 | was 1",
		"net.1.node.60.card.1 Ptp.PTP = 101 | was 100",
	} {
		if !got[want] {
			t.Errorf("diff lacks %q; got %v", want, got)
		}
	}
	if len(got) != 8 {
		t.Errorf("%d lines, want 8: %v", len(got), got)
	}
	if rrcsModelDiff(before, before) != nil || rrcsModelDiff(nil, after) != nil {
		t.Error("a diff where there is none")
	}
}

// watch: RRCS says the configuration changed; the system is read again
// and what differs is printed.
func TestRRCSWatchChanges(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	reads := 0
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		k := call.Params[0]
		switch call.Method {
		case "GetAllConferences":
			mu.Lock()
			reads++
			label := "Conf 003"
			if reads > 1 {
				label = "RENAMED"
			}
			mu.Unlock()
			return codec.Array(k, codec.Array(codec.Struct(rrcsMember("ObjectID", codec.Int(300)),
				rrcsMember("Label", codec.String(label)), rrcsMember("LongName", codec.String("Conference 003"))))), true
		case "RegisterForAllEvents":
			base := "http://127.0.0.1:" + strconv.Itoa(int(call.Params[1].Int))
			go func() {
				doc := must(codec.EncodeCall("ConfigurationChange", codec.String("R0000000001")))
				if resp, err := http.Post(base+"/RPC2", "text/xml", bytes.NewReader(doc)); err == nil {
					_ = resp.Body.Close()
				}
				time.Sleep(1500 * time.Millisecond)
				cancel()
			}()
			return k, true
		case "UnregisterForAllEvents":
			return codec.Array(k, codec.Int(0)), true
		}
		return rrcsTreeAnswer(call)
	})
	out, err := rrcsStdout(t, func() error {
		return runRRCS(ctx, []string{"watch", f.addr(), "--volume", "no", "--listen", "127.0.0.1:0", "--check", "0"})
	})
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	rrcsWant(t, out, "gateway Configuration = changed", "oid=300 conference.300 Label = RENAMED", "was Conf 003")
	if strings.Count(out, "conference.300 Label") != 1 {
		t.Errorf("the change was printed more than once:\n%s", out)
	}
}
