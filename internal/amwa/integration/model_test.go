//go:build integration

package amwa_integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// The verbs that read and change a Device's model and its audio channel
// map — control (IS-12), config (IS-14), map (IS-08), compat (IS-11) —
// against the nmos-cpp reference node. nmos-cpp serves one model over
// both IS-12 and IS-14, so what one verb does is read back through the
// other API, straight from the node: the oracle. Every change is undone.

// modelNode is the reference node these tests drive, its Device, and one
// of the Device's controls by URN prefix.
func modelNode(t *testing.T) (node, device string, control func(prefix string) string) {
	t.Helper()
	node = env(t, "NMOS_TEST_MUTATE_NODE", "a Node whose model may be changed (the reference node)")
	var devices []resource
	oracle(t, node+"/x-nmos/node/v1.3/devices", &devices)
	if len(devices) == 0 {
		t.Skip("this Node has no Device")
	}
	d := devices[0]
	return node, d.ID, func(prefix string) string {
		best, bestType := "", ""
		for _, c := range d.Controls {
			if strings.HasPrefix(c.Type, prefix) && c.Type > bestType {
				best, bestType = strings.TrimRight(c.Href, "/"), c.Type
			}
		}
		if best == "" {
			t.Skipf("the Device advertises no %s control", prefix)
		}
		return best
	}
}

// is14Value reads one property of one object from the node's own
// Configuration API.
func is14Value(t *testing.T, config, rolePath, property string) string {
	t.Helper()
	var res struct {
		Status int             `json:"status"`
		Value  json.RawMessage `json:"value"`
	}
	oracle(t, config+"/rolePaths/"+rolePath+"/properties/"+property+"/value/", &res)
	if res.Status != 200 {
		t.Fatalf("FAIL-real: the node answers %d for %s.%s", res.Status, rolePath, property)
	}
	return string(res.Value)
}

// is14Set writes one property through the node's own Configuration API:
// a change the CLI did not make.
func is14Set(t *testing.T, config, rolePath, property, value string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, config+"/rolePaths/"+rolePath+"/properties/"+property+"/value/",
		strings.NewReader(`{"value":`+value+`}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := peer.Do(req)
	if err != nil {
		t.Fatalf("TIMEOUT: the node did not answer the PUT: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		t.Fatalf("FAIL-real: the node answered the PUT of %s.%s with %s", rolePath, property, resp.Status)
	}
}

const (
	modelObject   = "root.ExampleControl" // nmos-cpp's example worker
	userLabel     = "1p6"
	labelFromTest = `"dhs integration"`
)

// The model the control verb lists over IS-12 is the model the node
// lists over IS-14: the same objects, by the same role paths.
func TestControlListsTheModelTheNodeServes(t *testing.T) {
	node, device, control := modelNode(t)
	config := control("urn:x-nmos:control:configuration/")
	control("urn:x-nmos:control:ncp/")

	var listed []string
	oracle(t, config+"/rolePaths/", &listed)
	want := map[string]bool{}
	for _, p := range listed {
		want[strings.TrimRight(p, "/")] = true
	}

	out := mustRun(t, time.Minute, "consumer", "nmos", "control", "--node", node, "--device", device)
	got := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line) // oid <n> class <id> <role path> [label…]
		if len(f) >= 5 && f[0] == "oid" && f[2] == "class" {
			got[f[4]] = true
		}
	}
	var missing, extra []string
	for p := range want {
		if !got[p] {
			missing = append(missing, p)
		}
	}
	for p := range got {
		// IS-14 lists the root block's members; IS-12 names the root too.
		if !want[p] && strings.Contains(p, ".") {
			extra = append(extra, p)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("FAIL-real: the control verb lists %d objects, the node's IS-14 %d; not listed %v, not the node's %v\n%s",
			len(got), len(want), head(missing), head(extra), tail(out))
	}
	t.Logf("PASS: %d objects over IS-12, the role paths the node's own IS-14 lists", len(want))
}

// A property set over IS-12 is what the node then serves over IS-14;
// set back, it is what it was.
func TestControlSetIsWhatTheNodeHolds(t *testing.T) {
	node, device, control := modelNode(t)
	config := control("urn:x-nmos:control:configuration/")
	before := is14Value(t, config, modelObject, userLabel)
	t.Cleanup(func() { is14Set(t, config, modelObject, userLabel, before) })

	args := []string{"consumer", "nmos", "control", "--node", node, "--device", device, "--role-path", modelObject}
	if out := mustRun(t, time.Minute, append(args, "--get", userLabel)...); !strings.Contains(out, userLabel+" = "+before) {
		t.Fatalf("FAIL-real: the verb read %q, the node's IS-14 says %s", strings.TrimSpace(out), before)
	}
	mustRun(t, time.Minute, append(args, "--set", userLabel+"="+labelFromTest)...)
	if got := is14Value(t, config, modelObject, userLabel); got != labelFromTest {
		t.Fatalf("FAIL-real: after the set over IS-12 the node's IS-14 serves %s, want %s", got, labelFromTest)
	}
	mustRun(t, time.Minute, append(args, "--set", userLabel+"="+before)...)
	if got := is14Value(t, config, modelObject, userLabel); got != before {
		t.Fatalf("FAIL-real: set back, the node's IS-14 serves %s, want %s", got, before)
	}
	t.Logf("PASS: %s.%s read, set to %s and back to %s over IS-12, each as the node's own IS-14 serves it", modelObject, userLabel, labelFromTest, before)
}

// A watch over IS-12 prints a change somebody else makes — here through
// the node's own IS-14.
func TestControlWatchPrintsAChangeMadeElsewhere(t *testing.T) {
	node, device, control := modelNode(t)
	config := control("urn:x-nmos:control:configuration/")
	before := is14Value(t, config, modelObject, userLabel)
	t.Cleanup(func() { is14Set(t, config, modelObject, userLabel, before) })

	watch := spawn(t, "consumer", "nmos", "control", "--node", node, "--device", device,
		"--role-path", modelObject, "--watch", "--duration", "20s")
	// The subscription is in place once the model has been read; the
	// verb says nothing until a change comes, so give it that time.
	time.Sleep(3 * time.Second)
	is14Set(t, config, modelObject, userLabel, labelFromTest)
	want := "changed  " + modelObject + "." + userLabel + " = " + labelFromTest
	if !eventually(func() bool { return strings.Contains(watch.logs.String(), want) }, 10*time.Second) {
		t.Fatalf("FAIL-real: the node's label was changed and the watch did not print %q:\n%s", want, tail(watch.logs.String()))
	}
	t.Logf("PASS: a change made through the node's IS-14 was printed by the IS-12 watch")
}

// The config verb reads and sets over IS-14 what the node serves, and
// the data set it backs up is one the node validates for a restore.
func TestConfigReadsSetsAndBacksUpTheModel(t *testing.T) {
	node, device, control := modelNode(t)
	config := control("urn:x-nmos:control:configuration/")
	before := is14Value(t, config, modelObject, userLabel)
	t.Cleanup(func() { is14Set(t, config, modelObject, userLabel, before) })

	args := []string{"consumer", "nmos", "config", "--node", node, "--device", device, "--role-path", modelObject}
	if out := mustRun(t, time.Minute, append(args, "--get", userLabel)...); !strings.Contains(out, userLabel+" = "+before) {
		t.Fatalf("FAIL-real: the verb read %q, the node says %s", strings.TrimSpace(out), before)
	}
	mustRun(t, time.Minute, append(args, "--set", userLabel+"="+labelFromTest)...)
	if got := is14Value(t, config, modelObject, userLabel); got != labelFromTest {
		t.Fatalf("FAIL-real: after the set the node serves %s, want %s", got, labelFromTest)
	}
	mustRun(t, time.Minute, append(args, "--set", userLabel+"="+before)...)
	if got := is14Value(t, config, modelObject, userLabel); got != before {
		t.Fatalf("FAIL-real: set back, the node serves %s, want %s", got, before)
	}

	// Backup, then ask the node what a restore of it would do.
	file := filepath.Join(t.TempDir(), "backup.json")
	mustRun(t, time.Minute, append(args, "--backup", file)...)
	raw, err := os.ReadFile(file)
	var holder struct {
		Values []json.RawMessage `json:"values"`
	}
	if err != nil || json.Unmarshal(raw, &holder) != nil || len(holder.Values) == 0 {
		t.Fatalf("FAIL-real: the backup is not a data set with objects: %v\n%s", err, tail(string(raw)))
	}
	out := mustRun(t, time.Minute, append(args, "--restore", file, "--validate-only")...)
	if !strings.Contains(out, "VALIDATED (nothing applied)") || !strings.Contains(out, "200  "+modelObject) {
		t.Fatalf("FAIL-real: the node did not validate its own backup:\n%s", tail(out))
	}
	if got := is14Value(t, config, modelObject, userLabel); got != before {
		t.Fatalf("FAIL-real: a validation changed the model: the node serves %s, want %s", got, before)
	}
	t.Logf("PASS: %s.%s read, set and set back; a backup of %d object(s) validated by the node for a restore, nothing applied",
		modelObject, userLabel, len(holder.Values))
}

// channelMap is the node's own /map/active.
type channelMap struct {
	Map map[string]map[string]struct {
		Input        *string `json:"input"`
		ChannelIndex *int    `json:"channel_index"`
	} `json:"map"`
}

// The map verb routes one channel, the node's own active map says so,
// and the route is taken out again.
func TestMapRoutesAChannelAndUnroutesIt(t *testing.T) {
	node, device, control := modelNode(t)
	cm := control("urn:x-nmos:control:cm-ctrl/")
	active := func() channelMap {
		var m channelMap
		oracle(t, cm+"/map/active", &m)
		return m
	}

	// An output channel that is unrouted now, and an input to give it.
	var inputs []string
	oracle(t, cm+"/inputs/", &inputs)
	output, input := "", ""
	before := active()
	for name, channels := range before.Map {
		if c, ok := channels["0"]; ok && c.Input == nil && (output == "" || name < output) {
			output = name
		}
	}
	for _, in := range inputs {
		in = strings.TrimRight(in, "/")
		var caps struct {
			Routable []*string `json:"routable_inputs"`
		}
		if output != "" {
			oracle(t, cm+"/outputs/"+output+"/caps", &caps)
		}
		ok := caps.Routable == nil
		for _, r := range caps.Routable {
			ok = ok || (r != nil && *r == in)
		}
		if ok && (input == "" || in < input) {
			input = in
		}
	}
	if output == "" || input == "" {
		t.Skip("this Device has no unrouted output channel with an input it can take")
	}
	args := []string{"consumer", "nmos", "map", "--node", node, "--device", device}
	unroute := append(append([]string{}, args...), "--route", output+":0=")
	t.Cleanup(func() { _, _, _ = run(t, time.Minute, unroute...) })

	out := mustRun(t, time.Minute, args...)
	if !strings.Contains(out, "output "+output) {
		t.Fatalf("FAIL-real: the verb's listing does not show output %s:\n%s", output, tail(out))
	}
	mustRun(t, time.Minute, append(append([]string{}, args...), "--route", fmt.Sprintf("%s:0=%s:0", output, input))...)
	if c := active().Map[output]["0"]; c.Input == nil || *c.Input != input || c.ChannelIndex == nil || *c.ChannelIndex != 0 {
		t.Fatalf("FAIL-real: after the route the node's active map has %s:0 as %+v, want %s:0", output, c, input)
	}
	mustRun(t, time.Minute, unroute...)
	if c := active().Map[output]["0"]; c.Input != nil {
		t.Fatalf("FAIL-real: unrouted, the node's active map still has %s:0 on %s", output, *c.Input)
	}
	t.Logf("PASS: %s:0 routed from %s:0 and unrouted, as read from the node's own active map", output, input)
}

// A Device with no IS-11 is told apart from one that answered: the verb
// names the control it did not find.
func TestCompatOnADeviceWithoutIS11IsRefusedByName(t *testing.T) {
	node, _, _ := modelNode(t)
	var devices []resource
	oracle(t, node+"/x-nmos/node/v1.3/devices", &devices)
	for _, c := range devices[0].Controls {
		if strings.HasPrefix(c.Type, "urn:x-nmos:control:stream-compat") {
			t.Skip("this Device has IS-11; the refusal is not what it would answer")
		}
	}
	_, receiver := rtpPair(t, node)
	out, errOut, err := run(t, time.Minute, "consumer", "nmos", "compat", "--node", node, "--receiver", receiver.ID)
	if err == nil {
		t.Fatalf("FAIL-real: compat on a Device without IS-11 reported a state:\n%s", tail(out))
	}
	if !bytes.Contains([]byte(out+errOut), []byte("advertises no urn:x-nmos:control:stream-compat control")) {
		t.Fatalf("FAIL-real: the refusal does not name the control:\n%s", tail(out+errOut))
	}
	t.Log("FAIL-expected: no IS-11 on this Device, refused by the control's name")
}
