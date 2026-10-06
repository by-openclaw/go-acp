package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/rrcs/codec"
)

func TestRRCSParseSpy(t *testing.T) {
	if all, panels, err := rrcsParseSpy("none"); err != nil || all || panels != nil {
		t.Errorf("none: %v %v %v", all, panels, err)
	}
	if all, _, err := rrcsParseSpy("all"); err != nil || !all {
		t.Errorf("all: %v %v", all, err)
	}
	_, panels, err := rrcsParseSpy("61.1026, 63.1024")
	if err != nil || len(panels) != 2 || panels[0] != [2]int{61, 1026} || panels[1] != [2]int{63, 1024} {
		t.Errorf("list: %v %v", panels, err)
	}
	for _, bad := range []string{"61", "61.x", "a.1", "61.1026,", "-1.5"} {
		if _, _, err := rrcsParseSpy(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	var val *consumer.ValidationError
	if err := runRRCS(context.Background(), []string{"watch", "h", "--spy", "panel"}); !errors.As(err, &val) {
		t.Errorf("bad --spy: %v", err)
	}
}

// watch --spy all: every port with keys gets its panel spy turned on
// after the registration and off before the unregistration, and a key
// event sent by the gateway is printed.
func TestRRCSWatchSpyAll(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var spy []string
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		k := call.Params[0]
		switch call.Method {
		case "GetAllPorts":
			return rrcsTreeAnswer(call)
		case "ChangePanelSpyRegistry":
			// §8.12: TransKey, TCPPort, URLPath, Node, Port, EventInfo.
			info := call.Params[5]
			on, _ := info.Field("KeyEventsOn")
			mu.Lock()
			spy = append(spy, fmt.Sprintf("%d.%d=%v/%d", call.Params[3].Int, call.Params[4].Int, on.Bool, len(info.Members)))
			first := len(spy) == 1
			mu.Unlock()
			if first {
				url := "http://127.0.0.1:" + strconv.Itoa(int(call.Params[1].Int)) + call.Params[2].Str
				go func() {
					event := codec.Struct(rrcsMember("KeyAction", codec.Int(0)), rrcsMember("Node", codec.Int(61)),
						rrcsMember("Port", codec.Int(1026)), rrcsMember("Key", codec.Int(5)), rrcsMember("IsKeyLatched", codec.Bool(false)))
					doc, _ := codec.EncodeCall("PanelSpyKeyEvent", codec.String("R0000000007"), event)
					if resp, err := http.Post(url, "text/xml", bytes.NewReader(doc)); err == nil {
						_ = resp.Body.Close()
					}
					cancel()
				}()
			}
			return k, true
		}
		return k, true
	})
	out, err := rrcsStdout(t, func() error {
		return runRRCS(ctx, []string{"watch", f.addr(), "--listen", "127.0.0.1:0", "--check", "0", "--spy", "all", "--events", "raw"})
	})
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	if !strings.Contains(out, "PanelSpyKeyEvent") || !strings.Contains(out, `"Key":5`) {
		t.Errorf("output:\n%s", out)
	}
	// The stand-in has one port with keys: 61.1026.
	if got := strings.Join(spy, " "); got != "61.1026=true/4 61.1026=false/4" {
		t.Errorf("panel spy requests: %s", got)
	}
	methods := strings.Join(f.methods(), ",")
	if !strings.HasSuffix(methods, "ChangePanelSpyRegistry,UnregisterForAllEvents") {
		t.Errorf("panel spy was not turned off before the unregistration: %s", methods)
	}
}

// A panel RRCS refuses does not stop the watch.
func TestRRCSWatchSpyRefused(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	asked := 0
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		if call.Method == "ChangePanelSpyRegistry" {
			asked++
			if asked == 2 {
				cancel()
			}
			return codec.Array(call.Params[0], codec.Int(4)), true // port address invalid
		}
		return call.Params[0], true
	})
	_, err := rrcsStdout(t, func() error {
		return runRRCS(ctx, []string{"watch", f.addr(), "--listen", "127.0.0.1:0", "--check", "0", "--spy", "61.1,61.2", "--events", "raw"})
	})
	if err != nil {
		t.Errorf("watch: %v", err)
	}
	if asked != 4 {
		t.Errorf("ChangePanelSpyRegistry sent %d times, want 4 (two on, two off)", asked)
	}
}
