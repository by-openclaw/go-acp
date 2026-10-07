package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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
		case "RegisterForAllEvents":
			// As a real RRCS opens a registration: one PortActive per
			// port that is on line. Here the panel, not the 4-wire.
			url := "http://127.0.0.1:" + strconv.Itoa(int(call.Params[1].Int)) + call.Params[2].Str
			go func() {
				doc, _ := codec.EncodeCall("PortActive", codec.String("R0000000001"), codec.Int(1), codec.Int(61), codec.Int(1026))
				if resp, err := http.Post(url, "text/xml", bytes.NewReader(doc)); err == nil {
					_ = resp.Body.Close()
				}
			}()
			return k, true
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
		return runRRCS(ctx, []string{"watch", f.addr(), "--spy", "none", "--volume", "no", "--listen", "127.0.0.1:0", "--check", "0", "--spy", "all", "--events", "raw"})
	})
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	if !strings.Contains(out, "PanelSpyKeyEvent") || !strings.Contains(out, `"Key":5`) {
		t.Errorf("output:\n%s", out)
	}
	// The stand-in has one port with keys: 61.1026.
	// Key and rotary only by default: two members, not four.
	if got := strings.Join(spy, " "); got != "61.1026=true/2 61.1026=false/2" {
		t.Errorf("panel spy requests: %s", got)
	}
	methods := strings.Join(f.methods(), ",")
	// The panel spy goes off first: RRCS refuses it once the receiver is
	// unregistered.
	if !strings.HasSuffix(methods, "ChangePanelSpyRegistry,UnregisterForAllEvents") {
		t.Errorf("order of the goodbye: %s", methods)
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
		return runRRCS(ctx, []string{"watch", f.addr(), "--spy", "none", "--volume", "no", "--listen", "127.0.0.1:0", "--check", "0", "--spy", "61.1,61.2", "--events", "raw"})
	})
	if err != nil {
		t.Errorf("watch: %v", err)
	}
	if asked != 4 {
		t.Errorf("ChangePanelSpyRegistry sent %d times, want 4 (two on, two off)", asked)
	}
}

// --spy-events chooses the kinds; an unknown one is refused.
func TestRRCSWatchSpyEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var members []string
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		if call.Method == "ChangePanelSpyRegistry" {
			for _, m := range call.Params[5].Members {
				members = append(members, m.Name)
			}
			cancel()
		}
		return call.Params[0], true
	})
	if _, err := rrcsStdout(t, func() error {
		return runRRCS(ctx, []string{"watch", f.addr(), "--spy", "none", "--volume", "no", "--listen", "127.0.0.1:0", "--check", "0", "--spy", "61.1026",
			"--spy-events", "key,func,num", "--events", "raw"})
	}); err != nil {
		t.Fatalf("watch: %v", err)
	}
	if got := strings.Join(members[:3], ","); got != "KeyEventsOn,FuncKeyEventsOn,NumKeyEventsOn" {
		t.Errorf("members %v", members)
	}
	var val *consumer.ValidationError
	if err := runRRCS(context.Background(), []string{"watch", "h", "--spy-events", "key,knob"}); !errors.As(err, &val) {
		t.Errorf("bad --spy-events: %v", err)
	}
}

// With --spy all and --volume yes, watch asks for everything RRCS can
// send. A panel spy error that several panels give is printed once.
func TestRRCSWatchAskForAll(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	asked := map[string]int{}
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		mu.Lock()
		asked[call.Method]++
		mu.Unlock()
		k := call.Params[0]
		switch call.Method {
		case "RegisterForAllEvents":
			url := "http://127.0.0.1:" + strconv.Itoa(int(call.Params[1].Int)) + call.Params[2].Str
			go func() {
				doc, _ := codec.EncodeCall("PortActive", codec.String("R0000000001"), codec.Int(1), codec.Int(61), codec.Int(1026))
				if resp, err := http.Post(url, "text/xml", bytes.NewReader(doc)); err == nil {
					_ = resp.Body.Close()
				}
			}()
			return k, true
		case "RegisterForEventsEx":
			return codec.Array(k, codec.Int(0)), true
		case "ChangePanelSpyRegistry":
			url := "http://127.0.0.1:" + strconv.Itoa(int(call.Params[1].Int)) + call.Params[2].Str
			go func() {
				state := func(port int32) codec.Value {
					failed := codec.Struct(rrcsMember("State", codec.Int(3)), rrcsMember("ErrorCode", codec.Int(99)),
						rrcsMember("ErrorDescription", codec.String("No client card acknowledge received (time-out=5000 msec).")))
					return codec.Struct(rrcsMember("Node", codec.Int(61)), rrcsMember("Port", codec.Int(port)), rrcsMember("Key", failed))
				}
				for _, port := range []int32{2, 1041, 7} {
					doc, _ := codec.EncodeCall("PanelSpyStateChanged", codec.String("R0000000002"), codec.Array(state(port)))
					if resp, err := http.Post(url, "text/xml", bytes.NewReader(doc)); err == nil {
						_ = resp.Body.Close()
					}
				}
				cancel()
			}()
			return k, true
		case "UnregisterForAllEvents", "UnregisterForEventsEx":
			return codec.Array(k, codec.Int(0)), true
		}
		return rrcsTreeAnswer(call)
	})
	capture := filepath.Join(t.TempDir(), "watch.jsonl")
	out, err := rrcsStdout(t, func() error {
		return runRRCS(ctx, []string{"watch", f.addr(), "--listen", "127.0.0.1:0", "--check", "0", "--capture", capture, "--spy", "all", "--volume", "yes"})
	})
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	mu.Lock()
	if asked["ChangePanelSpyRegistry"] == 0 || asked["RegisterForEventsEx"] != 1 {
		t.Errorf("the defaults did not ask for everything: %v", asked)
	}
	mu.Unlock()
	if n := strings.Count(out, "No client card acknowledge"); n != 1 {
		t.Errorf("the same panel spy error was printed %d times:\n%s", n, out)
	}
	raw, _ := os.ReadFile(capture)
	if !strings.Contains(string(raw), "panel spy Key not active on 3 panel(s): No client card acknowledge") {
		t.Errorf("the summary of the panel spy errors is missing from the capture")
	}
}
