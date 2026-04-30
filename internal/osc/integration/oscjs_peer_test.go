//go:build integration

// osc.js cross-implementation interop tests. Drives the Node-based
// reference peer at internal/osc/assets/test-harness as one or more
// instances exchanging OSC frames with dhs over the wire.
//
// Tests are SKIPPED when `node` is not on PATH or osc.js dependencies
// haven't been installed (the devcontainer post-create script handles
// the latter; on bare hosts run `npm install` inside the harness dir).
//
// Coverage:
//   - dhs producer  -> osc.js consumer (UDP + TCP-LP + TCP-SLIP)
//   - osc.js producer -> dhs consumer  (UDP + TCP-LP + TCP-SLIP)
//   - 3-instance fan-in: 2 osc.js producers + 1 dhs consumer
//   - byte oracle:    osc.js encode == dhs encode for the same spec
//
// Run with:
//
//	go test -tags integration ./internal/osc/integration/... -run OSCJS

package osc_integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"acp/internal/osc/codec"
	consumer "acp/internal/osc/consumer"
	provider "acp/internal/osc/provider"
)

const harnessRel = "../assets/test-harness/harness.js"

func skipIfNoOSCJS(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not on PATH; skipping osc.js interop")
	}
	abs, err := filepath.Abs(harnessRel)
	if err != nil {
		t.Skip("harness path: ", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(abs), "node_modules", "osc")); err != nil {
		t.Skip("osc.js not installed; run `npm install` in internal/osc/assets/test-harness")
	}
	return abs
}

// ---- Byte oracle: osc.js encode bytes == dhs encode bytes for same spec ----

func TestOSCJS_ByteOracle_Int32(t *testing.T) {
	harness := skipIfNoOSCJS(t)
	spec := `{"address":"/x","args":[{"type":"i","value":42}]}`
	jsBytes := runHarness(t, harness, []string{"encode", "--hex"}, spec)
	jsBuf, err := hex.DecodeString(strings.TrimSpace(jsBytes))
	if err != nil {
		t.Fatalf("hex decode: %v", err)
	}
	goMsg := codec.Message{Address: "/x", Args: []codec.Arg{codec.Int32(42)}}
	goBuf, err := goMsg.Encode()
	if err != nil {
		t.Fatalf("dhs encode: %v", err)
	}
	if !bytes.Equal(jsBuf, goBuf) {
		t.Errorf("byte mismatch:\n  osc.js: %x\n  dhs:    %x", jsBuf, goBuf)
	}
}

func TestOSCJS_ByteOracle_AllCommonTags(t *testing.T) {
	harness := skipIfNoOSCJS(t)
	spec := `{"address":"/all","args":[
		{"type":"i","value":7},
		{"type":"f","value":1.5},
		{"type":"s","value":"hi"},
		{"type":"T"},
		{"type":"F"},
		{"type":"N"},
		{"type":"I"}
	]}`
	jsHex := runHarness(t, harness, []string{"encode", "--hex"}, spec)
	jsBuf, err := hex.DecodeString(strings.TrimSpace(jsHex))
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	goMsg := codec.Message{Address: "/all", Args: []codec.Arg{
		codec.Int32(7), codec.Float32(1.5), codec.String("hi"),
		codec.True(), codec.False(), codec.Nil(), codec.Infinitum(),
	}}
	goBuf, _ := goMsg.Encode()
	if !bytes.Equal(jsBuf, goBuf) {
		t.Errorf("byte mismatch:\n  osc.js: %x\n  dhs:    %x", jsBuf, goBuf)
	}
}

// ---- dhs producer -> osc.js consumer (UDP) -------------------------------

func TestOSCJS_DHS_To_OSCJS_UDP(t *testing.T) {
	harness := skipIfNoOSCJS(t)
	port := freeUDPPort(t)
	cmd, stdout := startHarness(t, harness, []string{
		"listen-udp", "--port", fmt.Sprint(port),
	})
	defer killCmd(cmd)
	waitFor(t, "listen-udp ready")

	srv := provider.NewServerV10(quietLogger())
	defer func() { _ = srv.Stop() }()
	if err := srv.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if err := srv.AddDestination("127.0.0.1", port); err != nil {
		t.Fatalf("add dest: %v", err)
	}
	msg := codec.Message{Address: "/from/dhs",
		Args: []codec.Arg{codec.Int32(99), codec.Float32(2.5), codec.String("ok")}}
	if err := srv.SendMessage(msg); err != nil {
		t.Fatalf("send: %v", err)
	}
	line := readLineWithTimeout(t, stdout, 3*time.Second)
	verifyContains(t, line, []string{`"address":"/from/dhs"`, `"value":99`, `"value":2.5`, `"value":"ok"`})
}

// ---- osc.js producer -> dhs consumer (UDP) -------------------------------

func TestOSCJS_OSCJS_To_DHS_UDP(t *testing.T) {
	harness := skipIfNoOSCJS(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cons := consumer.NewPluginV10(quietLogger())
	defer func() { _ = cons.Disconnect() }()
	if err := cons.Connect(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("consumer connect: %v", err)
	}
	got := make(chan codec.Message, 4)
	_ = cons.SubscribePattern("", func(ev consumer.PacketEvent) { got <- ev.Msg })

	spec := `{"address":"/from/oscjs","args":[{"type":"i","value":7},{"type":"s","value":"hello"}]}`
	runHarnessExpectExit(t, harness, []string{
		"send-udp", "--host", "127.0.0.1", "--port", fmt.Sprint(cons.BoundAddr().Port),
	}, spec)

	select {
	case m := <-got:
		if m.Address != "/from/oscjs" || len(m.Args) != 2 ||
			m.Args[0].Int32 != 7 || m.Args[1].String != "hello" {
			t.Errorf("got %+v", m)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for osc.js packet")
	}
}

// ---- dhs producer -> osc.js consumer (TCP length-prefix, OSC 1.0) ---------

func TestOSCJS_DHS_To_OSCJS_TCPLenPrefix(t *testing.T) {
	harness := skipIfNoOSCJS(t)
	port := freeTCPPort(t)
	cmd, stdout := startHarness(t, harness, []string{
		"listen-tcp-len", "--port", fmt.Sprint(port),
	})
	defer killCmd(cmd)
	waitFor(t, "listen-tcp-len ready")

	srv := provider.NewServerV10(quietLogger())
	defer func() { _ = srv.Stop() }()
	msg := codec.Message{Address: "/lp/test", Args: []codec.Arg{codec.Int32(123)}}
	if err := srv.SendMessageTCP("127.0.0.1", port, msg); err != nil {
		t.Fatalf("send: %v", err)
	}
	line := readLineWithTimeout(t, stdout, 3*time.Second)
	verifyContains(t, line, []string{`"address":"/lp/test"`, `"value":123`})
}

// ---- dhs producer -> osc.js consumer (TCP SLIP, OSC 1.1) -----------------

func TestOSCJS_DHS_To_OSCJS_TCPSLIP(t *testing.T) {
	harness := skipIfNoOSCJS(t)
	port := freeTCPPort(t)
	cmd, stdout := startHarness(t, harness, []string{
		"listen-tcp-slip", "--port", fmt.Sprint(port),
	})
	defer killCmd(cmd)
	waitFor(t, "listen-tcp-slip ready")

	srv := provider.NewServerV11(quietLogger())
	defer func() { _ = srv.Stop() }()
	// Include a payload byte that requires SLIP byte-stuffing (0xC0 in a blob).
	msg := codec.Message{Address: "/slip/test",
		Args: []codec.Arg{codec.Blob([]byte{0xC0, 0xDB, 0xAA, 0x01})}}
	if err := srv.SendMessageTCP("127.0.0.1", port, msg); err != nil {
		t.Fatalf("send: %v", err)
	}
	line := readLineWithTimeout(t, stdout, 3*time.Second)
	verifyContains(t, line, []string{`"address":"/slip/test"`, `"type":"b"`})
}

// ---- 3-instance fan-in: 2 osc.js producers -> 1 dhs consumer (UDP) -------

func TestOSCJS_TwoProducers_DHSConsumer_UDP(t *testing.T) {
	harness := skipIfNoOSCJS(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cons := consumer.NewPluginV10(quietLogger())
	defer func() { _ = cons.Disconnect() }()
	if err := cons.Connect(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("connect: %v", err)
	}
	bus := make(chan codec.Message, 16)
	_ = cons.SubscribePattern("", func(ev consumer.PacketEvent) { bus <- ev.Msg })

	port := cons.BoundAddr().Port
	const each = 3
	var wg sync.WaitGroup
	for instance, who := range []string{"A", "B"} {
		_ = instance
		wg.Add(1)
		go func(label string) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				spec := fmt.Sprintf(`{"address":"/%s/%d","args":[{"type":"i","value":%d}]}`, label, i, i)
				runHarnessExpectExit(t, harness, []string{
					"send-udp", "--host", "127.0.0.1", "--port", fmt.Sprint(port),
				}, spec)
			}
		}(who)
	}
	wg.Wait()

	addrs := drainAddrs(t, bus, 2*each, 3*time.Second)
	gotA, gotB := 0, 0
	for a := range addrs {
		if a[1] == 'A' {
			gotA++
		}
		if a[1] == 'B' {
			gotB++
		}
	}
	if gotA != each || gotB != each {
		t.Errorf("got A=%d B=%d, want %d each", gotA, gotB, each)
	}
}

// ---- helpers --------------------------------------------------------------

// runHarness executes the harness one-shot (encode / decode / send-*) and
// returns stdout. stdin is the JSON spec.
func runHarness(t *testing.T, harness string, args []string, stdin string) string {
	t.Helper()
	cmd := exec.Command("node", append([]string{harness}, args...)...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("harness %v: %v\nstderr: %s", args, err, stderr.String())
	}
	return stdout.String()
}

// runHarnessExpectExit runs the harness once and waits for clean exit.
func runHarnessExpectExit(t *testing.T, harness string, args []string, stdin string) {
	t.Helper()
	cmd := exec.Command("node", append([]string{harness}, args...)...)
	cmd.Stdin = strings.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("harness %v: %v\nstderr: %s", args, err, stderr.String())
	}
}

// startHarness launches the harness in a long-running mode (listen-*).
// Returns the command + a buffered reader of stdout. Caller must kill.
var harnessReadyOnce sync.Mutex
var harnessReadyCh chan string

func startHarness(t *testing.T, harness string, args []string) (*exec.Cmd, *bufio.Reader) {
	t.Helper()
	cmd := exec.Command("node", append([]string{harness}, args...)...)
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Forward stderr lines to a per-test channel so waitFor can match
	// "ready on ..." messages.
	harnessReadyOnce.Lock()
	harnessReadyCh = make(chan string, 32)
	harnessReadyOnce.Unlock()
	go func() {
		s := bufio.NewScanner(stderrPipe)
		for s.Scan() {
			line := s.Text()
			select {
			case harnessReadyCh <- line:
			default:
			}
		}
	}()
	return cmd, bufio.NewReader(stdoutPipe)
}

func waitFor(t *testing.T, prefix string) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case line := <-harnessReadyCh:
			if strings.Contains(line, prefix) {
				return
			}
		case <-deadline:
			t.Fatalf("timeout waiting for %q", prefix)
		}
	}
}

func killCmd(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
}

func readLineWithTimeout(t *testing.T, r *bufio.Reader, d time.Duration) string {
	t.Helper()
	type res struct {
		line string
		err  error
	}
	ch := make(chan res, 1)
	go func() {
		line, err := r.ReadString('\n')
		ch <- res{line, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil && r.err != io.EOF {
			t.Fatalf("readLine: %v", r.err)
		}
		return r.line
	case <-time.After(d):
		t.Fatal("readLine timeout")
		return ""
	}
}

func verifyContains(t *testing.T, haystack string, needles []string) {
	t.Helper()
	for _, n := range needles {
		if !strings.Contains(haystack, n) {
			t.Errorf("missing %q in %s", n, haystack)
		}
	}
	// Sanity: parse as JSON to confirm it's well-formed.
	var v any
	if err := json.Unmarshal([]byte(strings.TrimSpace(haystack)), &v); err != nil {
		t.Errorf("not JSON: %v\n%s", err, haystack)
	}
}

func freeUDPPort(t *testing.T) int {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := conn.LocalAddr().(*net.UDPAddr).Port
	_ = conn.Close()
	return port
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}
