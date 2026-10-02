//go:build integration

package ccm_integration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// What is on the device, found once per test from the device itself.

// firstID is the first member the device lists under a collection.
func firstID(t *testing.T, h, collection string) string {
	t.Helper()
	var ids []string
	device(t, h, collection, &ids)
	if len(ids) == 0 {
		t.Fatalf("FAIL-real: the device lists nothing under %s", collection)
	}
	return ids[0]
}

// shows reports a watch line for path whose value column is value.
func shows(transcript, path, value string) bool {
	for _, line := range strings.Split(transcript, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, path) && (strings.HasSuffix(line, " "+value) || strings.HasSuffix(line, ` "`+value+`"`)) {
			return true
		}
	}
	return false
}

func TestInfoNamesTheDeviceAsItNamesItself(t *testing.T) {
	h := host(t)
	var self struct {
		ProductVersion string `json:"productVersion"`
	}
	device(t, h, "/self", &self)
	out := mustRun(t, 60*time.Second, "consumer", "ccm", "info", h)
	if !strings.Contains(out, self.ProductVersion) || !strings.Contains(out, "slot  0") {
		t.Errorf("FAIL-real: info does not report firmware %q and slot 0:\n%s", self.ProductVersion, out)
	}
}

func TestGetReadsOneValueWithoutAWalk(t *testing.T) {
	h := host(t)
	var ref struct {
		PTP struct {
			Domain int `json:"domain"`
		} `json:"ptp"`
	}
	device(t, h, "/reference", &ref)
	// One resource, one round trip: well inside a minute where a walk
	// of this device is several.
	out := mustRun(t, 60*time.Second, "consumer", "ccm", "get", h, "--path", "reference.ptp.domain")
	if want := fmt.Sprintf("value = %d", ref.PTP.Domain); !strings.Contains(out, want) {
		t.Errorf("FAIL-real: get printed %q, the device says %q", strings.TrimSpace(out), want)
	}
}

func TestAValueTheSpecForbidsNeverReachesTheDevice(t *testing.T) {
	h := host(t)
	bank := firstID(t, h, "/processing/audio/delay")
	path := "processing.audio.delay." + bank + ".delay"
	var before, after struct {
		Delay float64 `json:"delay"`
	}
	device(t, h, "/processing/audio/delay/"+bank, &before)
	// The device's own api.yml bounds a delay; 99999 is outside it.
	out, err := run(t, "", 60*time.Second, "consumer", "ccm", "set", h, "--path", path, "--value", "99999")
	if err == nil || !strings.Contains(out, "out-of-range") {
		t.Fatalf("FAIL-real: an out-of-range delay was not refused:\n%s", out)
	}
	device(t, h, "/processing/audio/delay/"+bank, &after)
	if after.Delay != before.Delay {
		t.Fatalf("FAIL-real: the refused write changed the device: %v -> %v", before.Delay, after.Delay)
	}
	t.Logf("FAIL-expected: %s", strings.TrimSpace(out))
}

// The three changes an operator makes on a Shuffle — a delay, a gain, a
// crosspoint — each reach a running watch from the device's event
// channel: nothing is polled.

func TestADelayChangeReachesAWatchWithoutPolling(t *testing.T) {
	h := host(t)
	bank := firstID(t, h, "/processing/audio/delay")
	path := "processing.audio.delay." + bank + ".delay"
	var doc struct {
		Delay float64 `json:"delay"`
	}
	device(t, h, "/processing/audio/delay/"+bank, &doc)
	was := strconv.Itoa(int(doc.Delay))
	next := strconv.Itoa((int(doc.Delay) + 7) % 3000)

	w := startWatch(t, "", h, "--path", "processing.audio.delay."+bank)
	w.until("the subscription", 60*time.Second, func(s string) bool { return strings.Contains(s, pushed) })
	w.until("the delay as it stands", 30*time.Second, func(s string) bool { return shows(s, path, was) })

	start := time.Now()
	setAndRestore(t, h, path, next, was)
	w.until("the new delay", 30*time.Second, func(s string) bool { return shows(s, path, next) })
	// A polled watch re-reads every 15 s. This one was told.
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("FAIL-real: the change took %s to reach the watch", took.Round(time.Millisecond))
	}
	if strings.Contains(w.text(), polled) {
		t.Errorf("FAIL-real: the watch polled a device that serves the event channel:\n%s", tail(w.text()))
	}
}

func TestACrosspointChangeReachesAWatchAndTheWholeMatrixIsShown(t *testing.T) {
	h := host(t)
	var state map[string]string
	device(t, h, "/matrices/audio/state/main", &state)
	sender := firstID(t, h, "/io/ip/senders/audio")
	dest := firstID(t, h, "/io/ip/senders/audio/"+sender+"/channels")
	was, ok := state[dest]
	if !ok {
		t.Fatalf("FAIL-real: the matrix state has no destination %s", dest)
	}
	// Any other source the matrix already routes somewhere.
	next := ""
	for _, src := range state {
		if src != was && src != "" {
			next = src
			break
		}
	}
	if next == "" {
		t.Fatal("FAIL-real: the matrix routes one source only — nothing to switch to")
	}
	path := "matrices.audio.state.main." + dest

	w := startWatch(t, "", h, "--path", "matrices.audio.state.main")
	w.until("the subscription", 60*time.Second, func(s string) bool { return strings.Contains(s, pushed) })
	// Every crosspoint the device holds is shown: the device pushes
	// them in one frame and none may be dropped on the way to the
	// operator.
	w.until(fmt.Sprintf("all %d crosspoints", len(state)), 60*time.Second, func(s string) bool {
		return strings.Count(s, "matrices.audio.state.main.") >= len(state)
	})

	setAndRestore(t, h, path, next, was)
	w.until("the new crosspoint", 30*time.Second, func(s string) bool { return shows(s, path, next) })
}

func TestAGainChangeReachesAWatchOfEverySender(t *testing.T) {
	h := host(t)
	sender := firstID(t, h, "/io/ip/senders/audio")
	channel := firstID(t, h, "/io/ip/senders/audio/"+sender+"/channels")
	path := "io.ip.senders.audio." + sender + ".channels." + channel + ".gain"
	var doc struct {
		Gain float64 `json:"gain"`
	}
	device(t, h, "/io/ip/senders/audio/"+sender+"/channels/"+channel, &doc)
	was := strconv.Itoa(int(doc.Gain))
	next := "-3"
	if was == next {
		next = "-4"
	}

	// No sender named: the channels sit two parameters deep and are
	// subscribed to sender by sender, never with two wildcards.
	w := startWatch(t, "", h, "--path", "io.ip.senders.audio")
	w.until("the subscription", 60*time.Second, func(s string) bool { return strings.Contains(s, pushed) })
	w.until("every sender's channels", 5*time.Minute, func(s string) bool { return strings.Contains(s, members) })

	setAndRestore(t, h, path, next, was)
	w.until("the new gain", 30*time.Second, func(s string) bool { return shows(s, path, next) })

	// And the device's REST API kept answering while that many
	// subscriptions were being made.
	var self map[string]any
	device(t, h, "/self", &self)
}

// The DM is generated from the device, never committed and never
// hand-written (the model is the device's own api.yml): the first watch
// of a slot collects it in the background, and the next one starts from
// it. This is ADR-0025 deliverable 4 by generator.
func TestTheDMIsCollectedOnceAndTheNextWatchStartsFromIt(t *testing.T) {
	h := host(t)
	// A cache of this test's own, so the first watch finds no DM.
	bin, root := ownCopy(t)
	var self struct {
		ProductName    string `json:"productName"`
		ProductVersion string `json:"productVersion"`
	}
	device(t, h, "/self", &self)
	identity := self.ProductName + "@" + self.ProductVersion

	first := startWatch(t, bin, h, "--slot", "0", "--path", "reference.status.ptp.locked")
	first.until("the subscription", 60*time.Second, func(s string) bool { return strings.Contains(s, pushed) })
	dm := filepath.Join(root, ".cache", "dm", "ccm", identity+".json")
	deadline := time.Now().Add(15 * time.Minute)
	for {
		if st, err := os.Stat(dm); err == nil && st.Size() > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("TIMEOUT: no DM at %s after 15 minutes:\n%s", dm, tail(first.text()))
		}
		time.Sleep(2 * time.Second)
	}
	first.stop()

	raw, err := os.ReadFile(dm)
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		Model   string            `json:"model"`
		SwRev   string            `json:"sw_rev"`
		Objects []json.RawMessage `json:"objects"`
	}
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("FAIL-real: the DM at %s is not a DM: %v", dm, err)
	}
	if stored.Model != self.ProductName || stored.SwRev != self.ProductVersion {
		t.Errorf("FAIL-real: the DM is keyed %s@%s, the device is %s", stored.Model, stored.SwRev, identity)
	}
	if n := len(stored.Objects); n < 1000 {
		t.Fatalf("FAIL-real: the DM holds %d object(s) — the walk stopped early", n)
	}

	second := startWatch(t, bin, h, "--slot", "0", "--path", "reference.status.ptp.locked")
	second.until("the DM being reused", 60*time.Second, func(s string) bool {
		return strings.Contains(s, "DM cache hit") && strings.Contains(s, identity)
	})
	second.until("the value", 60*time.Second, func(s string) bool {
		return shows(s, "reference.status.ptp.locked", "true") || shows(s, "reference.status.ptp.locked", "false")
	})
}

func TestExportReadsADeviceThatHasNoAPIRoot(t *testing.T) {
	h := host(t)
	out := t.TempDir()
	text := mustRun(t, 15*time.Minute, "consumer", "ccm", "export", h, "--out", out)
	var self struct {
		ProductName    string `json:"productName"`
		ProductVersion string `json:"productVersion"`
	}
	device(t, h, "/self", &self)
	dir := filepath.Join(out, self.ProductName+"@"+self.ProductVersion)
	raw, err := os.ReadFile(filepath.Join(dir, "dm-tree.json"))
	if err != nil {
		t.Fatalf("FAIL-real: no dm-tree.json under the device's own identity: %v\n%s", err, tail(text))
	}
	var tree map[string]json.RawMessage
	if err := json.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("FAIL-real: dm-tree.json: %v", err)
	}
	// Every sender the device lists is in the tree, as the device
	// serves it.
	var senders []string
	device(t, h, "/io/ip/senders/audio", &senders)
	for _, id := range []string{senders[0], senders[len(senders)-1]} {
		if _, ok := tree["/io/ip/senders/audio/"+id]; !ok {
			t.Errorf("FAIL-real: sender %s is on the device and not in the export", id)
		}
	}
	if len(tree) < len(senders) {
		t.Errorf("FAIL-real: %d resource(s) exported for a device with %d senders", len(tree), len(senders))
	}
	if _, err := os.Stat(filepath.Join(dir, "api.yml")); err != nil {
		t.Errorf("FAIL-real: the device's api.yml was not stored: %v", err)
	}
}

// A device that does not serve the event channel is polled — and says
// so. CCM_TEST_POLLED_HOST names one (CONVERT Hybrid 7.0.3).
func TestADeviceWithoutTheEventChannelIsPolled(t *testing.T) {
	h := strings.TrimSpace(os.Getenv("CCM_TEST_POLLED_HOST"))
	if h == "" {
		t.Skip("CCM_TEST_POLLED_HOST is not set — no device without the event channel to test against")
	}
	w := startWatch(t, "", h, "--path", "self")
	w.until("the watch saying it polls", 10*time.Minute, func(s string) bool { return strings.Contains(s, polled) })
	w.until("a value", 10*time.Minute, func(s string) bool { return has(s, "self.", "live") })
	if strings.Contains(w.text(), pushed) {
		t.Errorf("FAIL-real: a device that answers the upgrade with 404 was watched over a channel:\n%s", tail(w.text()))
	}
}
