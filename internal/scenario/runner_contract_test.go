package scenario

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/acp2/codec"
	acp2 "dhs/internal/acp2/consumer"
)

// fakeT records what the runner reports. Fatalf and Skipf stop the runner
// the way testing.T does — here by panicking with a sentinel the test
// recovers — so code after them is never reached, exactly as in a real
// test.
type fakeT struct {
	errs   []string
	fatal  string
	skip   string
	helper int
}

type stopSentinel struct{}

func (f *fakeT) Helper() { f.helper++ }
func (f *fakeT) Errorf(format string, args ...any) {
	f.errs = append(f.errs, fmt.Sprintf(format, args...))
}
func (f *fakeT) Fatalf(format string, args ...any) {
	f.fatal = fmt.Sprintf(format, args...)
	panic(stopSentinel{})
}
func (f *fakeT) Skipf(format string, args ...any) {
	f.skip = fmt.Sprintf(format, args...)
	panic(stopSentinel{})
}

// drive runs fn against a fakeT and reports what it recorded.
func drive(fn func(t T)) *fakeT {
	f := &fakeT{}
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(stopSentinel); !ok {
					panic(r)
				}
			}
		}()
		fn(f)
	}()
	return f
}

// writeCapture writes one JSONL wire file with the given lines.
func writeCapture(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "wire.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func rxLine(raw []byte) string {
	return fmt.Sprintf(`{"ts":"t","dir":"rx","hex":"%s","len":%d}`, hex.EncodeToString(raw), len(raw))
}

// acp2ErrorFrame builds an AN2 data frame carrying an ACP2 error reply
// with the given status.
func acp2ErrorFrame(t *testing.T, stat codec.ACP2ErrStatus) []byte {
	t.Helper()
	payload, err := codec.EncodeACP2Message(&codec.ACP2Message{Type: codec.ACP2TypeError, Func: codec.ACP2Func(stat)})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := codec.EncodeAN2Frame(&codec.AN2Frame{Proto: codec.AN2ProtoACP2, Type: codec.AN2TypeData, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func an2Frame(t *testing.T, proto codec.AN2Proto, typ codec.AN2Type, payload []byte) []byte {
	t.Helper()
	raw, err := codec.EncodeAN2Frame(&codec.AN2Frame{Proto: proto, Type: typ, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// Run dispatches by protocol: acp2 runs, acp1/emberplus are skipped with
// the scenario named, anything else is a fatal misconfiguration.
func TestRunDispatch(t *testing.T) {
	for _, proto := range []string{"acp1", "emberplus"} {
		f := drive(func(tt T) { Run(tt, &Scenario{Name: "s", Protocol: proto}) })
		if !strings.Contains(f.skip, `"s"`) {
			t.Errorf("%s: skip = %q, want the scenario named", proto, f.skip)
		}
	}
	f := drive(func(tt T) { Run(tt, &Scenario{Name: "s", Protocol: "martian"}) })
	if !strings.Contains(f.fatal, "unknown protocol") {
		t.Errorf("unknown protocol: fatal = %q", f.fatal)
	}
}

// readCapture fails on an unreadable file or a malformed line and skips a
// Git LFS pointer (a file that was never fetched, not a broken capture).
func TestReadCaptureEdges(t *testing.T) {
	f := drive(func(tt T) { readCapture(tt, filepath.Join(t.TempDir(), "missing.jsonl")) })
	if !strings.Contains(f.fatal, "open wire file") {
		t.Errorf("missing file: %q", f.fatal)
	}
	lfs := writeCapture(t, "version https://git-lfs.github.com/spec/v1")
	f = drive(func(tt T) { readCapture(tt, lfs) })
	if !strings.Contains(f.skip, "LFS pointer") {
		t.Errorf("lfs pointer: skip = %q", f.skip)
	}
	bad := writeCapture(t, `{"dir":`)
	f = drive(func(tt T) { readCapture(tt, bad) })
	if !strings.Contains(f.fatal, "unmarshal line") {
		t.Errorf("malformed line: %q", f.fatal)
	}
	long := writeCapture(t, `{"dir":"rx","hex":"`+strings.Repeat("00", 9*1024*1024)+`"}`)
	f = drive(func(tt T) { readCapture(tt, long) })
	if !strings.Contains(f.fatal, "scan") {
		t.Errorf("over-long line: %q", f.fatal)
	}
}

// runACP2 walks the rx frames, skipping what is not an ACP2 data frame,
// and checks the first error reply against every expectation the scenario
// states — reporting each mismatch, and a missing reply, precisely.
func TestRunACP2Expectations(t *testing.T) {
	const stat = codec.ACP2ErrStatus(1)
	errFrame := acp2ErrorFrame(t, stat)
	tx := strings.Replace(rxLine(errFrame), `"dir":"rx"`, `"dir":"tx"`, 1)
	garbage := `{"dir":"rx","hex":"00"}`
	otherProto := rxLine(an2Frame(t, codec.AN2ProtoACP1, codec.AN2TypeData, []byte{1, 2, 3, 4}))
	badACP2 := rxLine(an2Frame(t, codec.AN2ProtoACP2, codec.AN2TypeData, []byte{0xff}))
	wire := writeCapture(t, tx, garbage, otherProto, badACP2, rxLine(errFrame))

	msg, _ := codec.DecodeACP2Message(func() []byte {
		p, _ := codec.EncodeACP2Message(&codec.ACP2Message{Type: codec.ACP2TypeError, Func: codec.ACP2Func(stat)})
		return p
	}())
	typeName := fmt.Sprintf("%T", msg.ToACP2Error())
	short := strings.TrimPrefix(typeName[strings.LastIndex(typeName, ".")+1:], "*")
	status := int(stat)
	wrong := 42

	// Every expectation satisfied: no errors reported.
	f := drive(func(tt T) {
		runACP2(tt, &Scenario{Name: "ok", WireFile: wire, ExpectEvents: []string{codecEvent(stat)},
			ExpectErrorClass: typeName, ExpectErrorStatus: &status})
	})
	if len(f.errs) != 0 || f.fatal != "" {
		t.Errorf("satisfied scenario reported %v / %q", f.errs, f.fatal)
	}
	// Short class name is accepted too.
	f = drive(func(tt T) { runACP2(tt, &Scenario{Name: "short", WireFile: wire, ExpectErrorClass: short}) })
	if len(f.errs) != 0 {
		t.Errorf("short class name rejected: %v", f.errs)
	}
	// Every expectation wrong: three distinct errors.
	f = drive(func(tt T) {
		runACP2(tt, &Scenario{Name: "bad", WireFile: wire, ExpectEvents: []string{"nope"},
			ExpectErrorClass: "Wrong", ExpectErrorStatus: &wrong})
	})
	if len(f.errs) != 3 {
		t.Errorf("mismatching scenario reported %d errors, want 3: %v", len(f.errs), f.errs)
	}
	// Expecting an error reply that the capture does not contain.
	empty := writeCapture(t, tx, garbage)
	f = drive(func(tt T) { runACP2(tt, &Scenario{Name: "none", WireFile: empty, ExpectErrorStatus: &status}) })
	if !strings.Contains(f.fatal, "expected an ACP2 error reply") {
		t.Errorf("missing reply: %q", f.fatal)
	}
	// Unresolvable wire file and undecodable hex are fatal.
	f = drive(func(tt T) {
		runACP2(tt, &Scenario{Name: "nowire", WireFile: "no/such.jsonl", SourcePath: filepath.Join(t.TempDir(), "s.json")})
	})
	if !strings.Contains(f.fatal, "cannot resolve wire_file") {
		t.Errorf("unresolvable wire: %q", f.fatal)
	}
	badHex := writeCapture(t, `{"dir":"rx","hex":"zz"}`)
	f = drive(func(tt T) { runACP2(tt, &Scenario{Name: "hex", WireFile: badHex}) })
	if !strings.Contains(f.fatal, "hex decode") {
		t.Errorf("bad hex: %q", f.fatal)
	}
}

// codecEvent is the event name the consumer maps a status to, so the test
// states the contract without hard-coding a string that lives elsewhere.
func codecEvent(stat codec.ACP2ErrStatus) string { return acp2.EventForErrStatus(stat) }

// Load reads and roots a scenario, and reports each failure; Discover
// lists scenario files and surfaces a bad directory; ResolveWirePath
// accepts an absolute path, a sibling file, or a repo-root-relative file,
// and names what it tried otherwise.
func TestLoadDiscoverResolve(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("missing scenario must error")
	}
	bad := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(bad, []byte("{"), 0o644)
	if _, err := Load(bad); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Errorf("malformed scenario: %v", err)
	}
	good := filepath.Join(dir, "good.json")
	_ = os.WriteFile(good, []byte(`{"name":"g","protocol":"acp2","wire_file":"wire.jsonl"}`), 0o644)
	orig := absPath
	absPath = func(string) (string, error) { return "", errors.New("no cwd") }
	if _, err := Load(good); err == nil || !strings.Contains(err.Error(), "abs") {
		t.Errorf("abs failure: %v", err)
	}
	absPath = orig
	s, err := Load(good)
	if err != nil || s.SourcePath != good {
		t.Fatalf("Load = %+v, %v", s, err)
	}

	_ = os.WriteFile(filepath.Join(dir, "README.json"), []byte("{}"), 0o644)
	found, err := Discover(dir)
	if err != nil || len(found) != 2 {
		t.Errorf("Discover = %v, %v; want bad.json and good.json only", found, err)
	}
	if _, err := Discover(filepath.Join(dir, "nope")); err == nil {
		t.Error("Discover on a missing dir must error")
	}

	abs := filepath.Join(dir, "abs.jsonl")
	if got, err := (&Scenario{WireFile: abs}).ResolveWirePath(); err != nil || got != abs {
		t.Errorf("absolute wire: %q, %v", got, err)
	}
	_ = os.WriteFile(filepath.Join(dir, "wire.jsonl"), []byte(""), 0o644)
	if got, err := s.ResolveWirePath(); err != nil || got != filepath.Join(dir, "wire.jsonl") {
		t.Errorf("sibling wire: %q, %v", got, err)
	}
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644)
	nested := filepath.Join(root, "a", "b")
	_ = os.MkdirAll(nested, 0o755)
	deep := &Scenario{WireFile: "testdata/w.jsonl", SourcePath: filepath.Join(nested, "s.json")}
	if got, err := deep.ResolveWirePath(); err != nil || got != filepath.Join(root, "testdata/w.jsonl") {
		t.Errorf("repo-root wire: %q, %v", got, err)
	}
	orphan := &Scenario{WireFile: "w.jsonl", SourcePath: filepath.Join(t.TempDir(), "s.json")}
	if _, err := orphan.ResolveWirePath(); err == nil || !strings.Contains(err.Error(), "tried scenario-dir and repo root") {
		t.Errorf("unresolvable wire: %v", err)
	}
}
