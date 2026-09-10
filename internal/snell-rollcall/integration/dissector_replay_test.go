//go:build integration

// Replay regression for the dhs_snell_rollcall Wireshark dissector.
//
// The fixture is a live capture of our own client driving the vendor Centra
// controller as a Sirius 800: a handshake, a device map walk, a menu walk in
// both generations, the router's command space, a crosspoint set and the
// back-channel tally that follows it. Every claim the dissector makes about
// those bytes is checked here rather than by eye.
//
// It is a regression test in the strict sense: the dissector is a second
// implementation of the same wire format, and this is what stops the two
// drifting apart. When the codec and the dissector disagree, one of them is
// wrong about a device that is not in the room.
//
// Skips when tshark is not installed. Run with:
//
//	go test -tags integration ./internal/snell-rollcall/integration/... -run DissectorReplay

package rollcall_integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const (
	fixtureRel   = "../../../tests/fixtures/snell-rollcall/centra-sirius800.pcapng"
	dissectorRel = "../wireshark/dhs_snell_rollcall.lua"
)

func findTshark(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("tshark"); err == nil {
		return p
	}
	if runtime.GOOS == "windows" {
		for _, p := range []string{
			`C:\Program Files\Wireshark\tshark.exe`,
			`C:\Program Files (x86)\Wireshark\tshark.exe`,
		} {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	t.Skip("tshark not found; install Wireshark or skip")
	return ""
}

func mustAbs(t *testing.T, rel string) string {
	t.Helper()
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Skip(rel, ": ", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Skip("not found: ", abs)
	}
	return abs
}

// runTshark reads the fixture with our dissector loaded.
func runTshark(t *testing.T, extra ...string) string {
	t.Helper()

	tshark := findTshark(t)
	args := []string{
		"-r", mustAbs(t, fixtureRel),
		"-X", "lua_script:" + mustAbs(t, dissectorRel),
	}
	args = append(args, extra...)

	cmd := exec.Command(tshark, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("tshark %v: %v\nstderr: %s", args, err, stderr.String())
	}
	// A Lua error does not fail tshark; it appears in the dissection.
	if strings.Contains(stdout.String(), "Lua Error") {
		t.Fatalf("the dissector raised a Lua error:\n%s", stdout.String())
	}
	return stdout.String()
}

// fields runs a field extraction and returns the non-empty values.
func fields(t *testing.T, filter string, field string) []string {
	t.Helper()

	out := runTshark(t, "-Y", filter, "-T", "fields", "-e", field)

	var vals []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// One frame may carry several messages, and tshark joins repeated
		// fields with commas.
		vals = append(vals, strings.Split(line, ",")...)
	}
	return vals
}

func TestDissectorReplay_EveryMessageTypeInTheCapture(t *testing.T) {
	got := map[int]int{}
	for _, v := range fields(t, "dhs_snell_rollcall", "dhs_snell_rollcall.type") {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("type %q: %v", v, err)
		}
		got[n]++
	}

	// What a session actually does, from opening one to routing a crosspoint.
	// Each is named rather than counted, so a failure says which decode went.
	want := map[int]string{
		0:  "NACK",
		1:  "ACK",
		2:  "CALL",
		3:  "TERM",
		4:  "GETSTAT",
		5:  "RETSTAT",
		6:  "GETID",
		7:  "RETID",
		10: "DISPDATA",
		19: "GETDEVLIST",
		20: "RETDEVINFO",
		21: "GETDEVINFO",
		27: "BKCHNREADY",
		35: "GETNEXTPKT",
		36: "REPFCHG",
		39: "BLOCKHEADER",
		65: "GETMENUCOUNT",
		66: "RETMENUCOUNT",
		67: "GETMENUITEM",
		68: "RETMENUITEM",
		69: "GETVALUE",
		70: "SETVALUE",
		71: "RETVALUE",
	}
	for typ, name := range want {
		if got[typ] == 0 {
			t.Errorf("no %s (type %d) decoded; the fixture has them", name, typ)
		}
	}
}

func TestDissectorReplay_NothingIsMalformed(t *testing.T) {
	// Expert info on a RollCall frame means the dissector could not account
	// for bytes a real device sent, which is the finding this test exists for.
	out := runTshark(t, "-Y", "dhs_snell_rollcall && _ws.expert")
	if strings.TrimSpace(out) != "" {
		t.Errorf("the dissector flagged live frames:\n%s", out)
	}
}

func TestDissectorReplay_TheDeviceMapIsRead(t *testing.T) {
	// The controller enumerated fifteen nodes, and the block header says so
	// before any of them arrives.
	counts := fields(t, "dhs_snell_rollcall.type==39", "dhs_snell_rollcall.block.count")
	if len(counts) == 0 {
		t.Fatal("no block header decoded")
	}
	found := false
	for _, c := range counts {
		if c == "15" {
			found = true
		}
	}
	if !found {
		t.Errorf("block header counts = %v, want one of 15 nodes", counts)
	}
}

func TestDissectorReplay_TheRoutingInterfaceIsNamed(t *testing.T) {
	// Command 100 is the fixed root of the router command space, and naming it
	// is what turns a wall of GETVALUE into something readable.
	names := fields(t, "dhs_snell_rollcall.router_command", "dhs_snell_rollcall.router_command")
	if len(names) == 0 {
		t.Fatal("no routing command was named")
	}

	want := map[string]bool{
		"CMD_INTERFACE_VERSION": false,
		"CMD_NUM_MATRICES":      false,
		"CMD_MATRIX_BASE":       false,
	}
	for _, n := range names {
		if _, ok := want[n]; ok {
			want[n] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("%s was never named; the capture reads it", name)
		}
	}
}

func TestDissectorReplay_ACrosspointIsDecoded(t *testing.T) {
	// The routing interface carries a crosspoint as Data Transfer Params
	// holding a packed source pin and a result. Decoding it is the difference
	// between a readable capture and a wall of hex.
	//
	// What this fixture cannot show is the pin NAMED as a crosspoint. The
	// command that carries it is 316, and 316 means nothing without the level
	// table it belongs to — a controller publishes the bases and steps once,
	// as a client connects, and this capture holds five TCP streams of which
	// none carries them. The client had them already.
	//
	// So the dissector says it cannot name the command, which is true, and
	// offers the reading it would have if it could, which is useful. Asserting
	// a named pin here would be asserting that the dissector guesses — a
	// protect word is the same width and the same shape as a pin, and there is
	// nothing in these bytes to tell them apart.
	//
	// The resolved path — where the tables ARE on the wire and the pin becomes
	// a fact in dhs_snell_rollcall.source_pin — is proved against a complete
	// capture by ansible/playbooks/snell-rollcall-dissector.yml.
	//
	// The capture holds one whole crosspoint transaction, and it is the
	// specification's sequence written in a vendor's own bytes:
	//
	//   SETVALUE          0x01010004   take m1/l1/s4
	//   RETVALUE          0x01010006, 0   the pin from BEFORE, plus the result
	//   RETVALUE [back]   0x01010004   the tally: s4 is on it now
	//
	// The middle one is the part everybody gets wrong. A reply to a route
	// carries the crosspoint as it was before the change, not after — what was
	// actually routed arrives afterwards on the back channel. Our provider had
	// this backwards once; this is the vendor evidence that settles it.
	const (
		took   = "16842756" // 0x01010004 — m1/l1/s4, the source asked for
		before = "16842758" // 0x01010006 — m1/l1/s6, what was on it beforehand
	)

	set := fields(t, "dhs_snell_rollcall.type == 70 && dhs_snell_rollcall.dtp.uint",
		"dhs_snell_rollcall.dtp.uint")
	if len(set) == 0 {
		t.Fatal("the crosspoint set decoded no parameters; the capture sets a route")
	}
	if !slices.Contains(set, took) {
		t.Errorf("the set carried %v, want %s (m1/l1/s4) — the packing is what makes "+
			"a crosspoint one integer, so the integer is what has to be checked", set, took)
	}

	// fields() splits a frame's repeated values, so the reply arrives as the
	// two parameters it carries: the pin, then the result.
	reply := fields(t, "dhs_snell_rollcall.type == 71 && dhs_snell_rollcall.command == 316"+
		" && dhs_snell_rollcall.flags.back_channel == 0", "dhs_snell_rollcall.dtp.uint")
	if len(reply) != 2 {
		t.Fatalf("the reply carried %v, want a pin and a result", reply)
	}
	if reply[0] != before {
		t.Errorf("the reply carried %s, want %s — a route is answered with the "+
			"crosspoint from before it, not after", reply[0], before)
	}
	if reply[1] != "0" {
		t.Errorf("the reply's result was %s, want 0 (ok)", reply[1])
	}

	tally := fields(t, "dhs_snell_rollcall.command == 316"+
		" && dhs_snell_rollcall.flags.back_channel == 1", "dhs_snell_rollcall.dtp.uint")
	if !slices.Contains(tally, took) {
		t.Errorf("the back channel reported %v, want %s — what was actually routed "+
			"arrives there rather than in the reply", tally, took)
	}

	// And the reading is offered, hedged, rather than withheld: a capture that
	// joined a session late is the ordinary case, not a broken one.
	out := runTshark(t, "-Y", "dhs_snell_rollcall.command == 316", "-V")
	if !strings.Contains(out, "m1/l1/s6") {
		t.Error("the crosspoint's source was not offered as a reading")
	}
	if !strings.Contains(out, "unconfirmed") {
		t.Error("a reading the dissector cannot confirm was not marked as one")
	}
}

func TestDissectorReplay_TheBackChannelIsMarked(t *testing.T) {
	// A tally arrives on the back channel and a reply does not. Telling them
	// apart in the capture is how the "the reply carries the previous value"
	// behaviour is seen at all.
	flags := fields(t, "dhs_snell_rollcall.flags.back_channel==1", "dhs_snell_rollcall.type")
	if len(flags) == 0 {
		t.Error("no back-channel frame was marked; the capture has tally pushes")
	}
}

func TestDissectorReplay_SessionIndicesAreFollowed(t *testing.T) {
	// Two indices matter and confusing them is the defect the session layer
	// exists to prevent. The dissector shows both, and the unconnected one is
	// named rather than printed as 255.
	out := runTshark(t, "-Y", "dhs_snell_rollcall.type==2")
	if !strings.Contains(out, "CALL") {
		t.Fatalf("no CALL decoded:\n%s", out)
	}
	if !strings.Contains(out, "none") {
		t.Errorf("a call is addressed to the unconnected index, which should be named:\n%s", out)
	}
}
