package consumer

// The IS-12 half of the Controller: the endpoint comes from the
// Device's ncp control, objects are named by role path and resolved to
// oids from the Device's own block tree, and a set is read back.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is12"
	httpsession "dhs/internal/amwa/session/http"
)

// controlMembers is a model of four objects under the root block: the
// class manager, a block, and a monitor inside that block.
const controlMembers = `[
  {"description":null,"role":"ClassManager","oid":3,"constantOid":true,"classId":[1,3,2],"userLabel":null,"owner":1},
  {"description":null,"role":"receivers","oid":10,"constantOid":true,"classId":[1,1],"userLabel":null,"owner":1},
  {"description":null,"role":"rx1","oid":11,"constantOid":true,"classId":[1,2,2,1],"userLabel":"RX 1","owner":10}]`

// controlDevice is the control endpoint of one Device.
type controlDevice struct {
	mu      sync.Mutex
	members string            // what the root block answers GetMemberDescriptors with
	props   map[string]string // "<oid>/<level>p<index>" -> value, as JSON
	answers map[string]string // "<oid>/<level>m<index>" -> method result, replacing the default
	ignore  bool              // answer OK to a set and keep the old value
	dieAt   int               // hang up instead of answering this frame (1 is the first)
	notify  []string          // frames pushed after a subscription is taken
	frames  int
	invoked []string // the arguments of every method that is not one of MS-05-02's own
	asked   [][]int  // every subscription set received
}

func newControlDevice() *controlDevice {
	return &controlDevice{
		members: controlMembers,
		props:   map[string]string{"1/1p5": `"root"`, "1/1p1": `[1,1]`, "11/1p6": `"RX 1"`},
		answers: map[string]string{},
	}
}

func (d *controlDevice) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ws, err := httpsession.AcceptWebSocket(w, r)
		if err != nil {
			return
		}
		defer func() { _ = ws.Close() }()
		for {
			raw, err := ws.ReadText()
			if err != nil {
				return
			}
			for _, frame := range d.take(raw) {
				if frame == "" {
					return // hang up
				}
				_ = ws.SendText([]byte(frame))
			}
		}
	}
}

// take answers one frame; an empty answer hangs up.
func (d *controlDevice) take(raw []byte) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.frames++; d.frames == d.dieAt {
		return []string{""}
	}
	m, _ := is12.Decode(raw)
	if sub, ok := m.(is12.SubscriptionMessage); ok {
		d.asked = append(d.asked, sub.Subscriptions)
		list, _ := json.Marshal(sub.Subscriptions)
		return append([]string{`{"messageType":4,"subscriptions":` + string(list) + `}`}, d.notify...)
	}
	cmd := m.(is12.CommandMessage).Commands[0]
	respond := func(result string) []string {
		return []string{fmt.Sprintf(`{"messageType":1,"responses":[{"handle":%d,"result":%s}]}`, cmd.Handle, result)}
	}
	if result, ok := d.answers[fmt.Sprintf("%d/%dm%d", cmd.OID, cmd.MethodID.Level, cmd.MethodID.Index)]; ok {
		return respond(result)
	}
	var args struct {
		ID    is12.PropertyID `json:"id"`
		Value json.RawMessage `json:"value"`
	}
	_ = json.Unmarshal(cmd.Arguments, &args)
	prop := fmt.Sprintf("%d/%dp%d", cmd.OID, args.ID.Level, args.ID.Index)
	switch cmd.MethodID {
	case is12.MethodID{Level: 1, Index: 1}:
		if v, ok := d.props[prop]; ok {
			return respond(`{"status":200,"value":` + v + `}`)
		}
		return respond(`{"status":404,"errorMessage":"no such property"}`)
	case is12.MethodID{Level: 1, Index: 2}:
		if !d.ignore {
			d.props[prop] = string(args.Value)
		}
		return respond(`{"status":200}`)
	case is12.MethodID{Level: 2, Index: 1}:
		return respond(`{"status":200,"value":` + d.members + `}`)
	case is12.MethodID{Level: 3, Index: 1}:
		return respond(`{"status":200,"value":{"name":"NcReceiverMonitor"}}`)
	}
	d.invoked = append(d.invoked, string(cmd.Arguments))
	return respond(`{"status":200,"value":"done"}`)
}

// controlHarness is a catalogue with one Device that advertises IS-12,
// and that endpoint behind it.
func controlHarness(t *testing.T) (*harness, *controlDevice, string) {
	t.Helper()
	h := newHarness(t)
	d := newControlDevice()
	h.is12 = d.handler()
	dev := deviceWith(uuidN(1), "")
	dev.Controls = []is04.DeviceControl{{
		Href: "ws" + strings.TrimPrefix(strings.Replace(h.controlHref, "/x-nmos/connection/v1.1", "/x-nmos/ncp/v1.0", 1), "http"),
		Type: "urn:x-nmos:control:ncp/v1.0",
	}}
	h.cat.devices = []is04.Device{dev}
	return h, d, dev.ID
}

func TestControlListsTheModelByRolePath(t *testing.T) {
	h, _, dev := controlHarness(t)

	res, err := h.ctrl.Control(context.Background(), ControlRequest{DeviceID: dev})
	if err != nil || !strings.HasSuffix(res.Endpoint, "/x-nmos/ncp/v1.0") {
		t.Fatalf("listing = %+v, %v", res, err)
	}
	var got []string
	for _, o := range res.Objects {
		got = append(got, fmt.Sprintf("%s=%d", o.RolePath, o.OID))
	}
	if want := "root=1 root.ClassManager=3 root.receivers=10 root.receivers.rx1=11"; strings.Join(got, " ") != want {
		t.Errorf("objects = %v, want %s", got, want)
	}
	rx := res.Objects[3]
	if rx.UserLabel != "RX 1" || len(rx.ClassID) != 4 || len(res.Objects[0].ClassID) != 2 {
		t.Errorf("rx1 = %+v, root = %+v", rx, res.Objects[0])
	}
}

// A root block that will not say its role or class is still the root,
// under the name MS-05-02 gives it.
func TestControlNamesARootThatDoesNotSayItsRole(t *testing.T) {
	h, d, dev := controlHarness(t)
	delete(d.props, "1/1p5")
	delete(d.props, "1/1p1")

	res, err := h.ctrl.Control(context.Background(), ControlRequest{DeviceID: dev})
	if err != nil || res.Objects[0].RolePath != "root" || res.Objects[0].ClassID != nil || res.Objects[3].RolePath != "root.receivers.rx1" {
		t.Fatalf("listing = %+v, %v", res, err)
	}
}

// A member whose owners do not lead to the root block — one the Device
// did not list, or a loop — keeps its role as its name, and is reported.
func TestControlReportsAMemberOutsideTheTree(t *testing.T) {
	h, d, dev := controlHarness(t)
	d.members = `[
	  {"description":null,"role":"lost","oid":50,"constantOid":true,"classId":[1,2],"userLabel":null,"owner":999},
	  {"description":null,"role":"a","oid":60,"constantOid":true,"classId":[1,1],"userLabel":null,"owner":61},
	  {"description":null,"role":"b","oid":61,"constantOid":true,"classId":[1,1],"userLabel":null,"owner":60}]`

	res, err := h.ctrl.Control(context.Background(), ControlRequest{DeviceID: dev})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, o := range res.Objects {
		got = append(got, o.RolePath)
	}
	if strings.Join(got, " ") != "a b lost root" {
		t.Errorf("objects = %v", got)
	}
	evs := h.rep.Snapshot()
	if len(evs) != 3 || evs[0].Code != "nmos_is12_member_outside_the_tree" || !strings.Contains(evs[0].Detail, "object 50 (lost) under owner 999") {
		t.Errorf("events = %+v", evs)
	}
}

func TestControlDescribesGetsAndInvokes(t *testing.T) {
	h, d, dev := controlHarness(t)
	ctx := context.Background()

	res, err := h.ctrl.Control(ctx, ControlRequest{DeviceID: dev, RolePath: "root.receivers.rx1"})
	if err != nil || res.OID != 11 || !strings.Contains(string(res.Class), "NcReceiverMonitor") {
		t.Fatalf("describing = %+v, %v", res, err)
	}

	res, err = h.ctrl.Control(ctx, ControlRequest{DeviceID: dev, RolePath: "root.receivers.rx1", Get: "1p6"})
	if err != nil || string(res.Value) != `"RX 1"` {
		t.Errorf("get = %+v, %v", res, err)
	}

	res, err = h.ctrl.Control(ctx, ControlRequest{DeviceID: dev, RolePath: "root.receivers.rx1", Invoke: "4m1", Arguments: json.RawMessage(`{"reset":true}`)})
	if err != nil || string(res.Value) != `"done"` || len(d.invoked) != 1 || !strings.Contains(d.invoked[0], "reset") {
		t.Errorf("invoke = %+v, %v (the Device saw %v)", res, err, d.invoked)
	}
	if _, err = h.ctrl.Control(ctx, ControlRequest{DeviceID: dev, RolePath: "root.receivers.rx1", Invoke: "4m2"}); err != nil || d.invoked[1] != "" {
		t.Errorf("invoke with no arguments: %v (the Device saw %q)", err, d.invoked)
	}

	// A dry run of a method calls nothing.
	res, err = h.ctrl.Control(ctx, ControlRequest{DeviceID: dev, RolePath: "root.receivers.rx1", Invoke: "4m1", DryRun: true})
	if err != nil || !res.DryRun || len(d.invoked) != 2 {
		t.Errorf("dry-run invoke = %+v, %v (the Device saw %d call(s))", res, err, len(d.invoked))
	}
}

func TestControlSetsAndReadsBack(t *testing.T) {
	h, d, dev := controlHarness(t)
	ctx := context.Background()
	set := ControlRequest{DeviceID: dev, RolePath: "root.receivers.rx1", Set: "1p6", SetValue: json.RawMessage(`"Studio A"`)}

	// A dry run reads what the Device holds and writes nothing.
	dry := set
	dry.DryRun = true
	res, err := h.ctrl.Control(ctx, dry)
	if err != nil || string(res.Value) != `"RX 1"` || d.props["11/1p6"] != `"RX 1"` {
		t.Fatalf("dry-run set = %+v, %v (the Device holds %s)", res, err, d.props["11/1p6"])
	}

	res, err = h.ctrl.Control(ctx, set)
	if err != nil || string(res.Value) != `"Studio A"` {
		t.Fatalf("set = %+v, %v", res, err)
	}
	if evs := h.rep.Snapshot(); len(evs) != 0 {
		t.Errorf("an applied set fires nothing, got %+v", evs)
	}

	// A Device that answers OK and keeps the old value is reported.
	d.ignore = true
	set.SetValue = json.RawMessage(`"Studio B"`)
	if _, err := h.ctrl.Control(ctx, set); err != nil {
		t.Fatal(err)
	}
	evs := h.rep.Snapshot()
	if len(evs) != 1 || evs[0].Code != "nmos_is12_set_not_applied" || !strings.Contains(evs[0].Detail, `= "Studio B" and holds "Studio A"`) {
		t.Errorf("events = %+v", evs)
	}
}

// A watch subscribes to the object named — to every object when none
// is — and hands each change over under the object's role path.
func TestControlWatchHandsOverPropertyChanges(t *testing.T) {
	h, d, dev := controlHarness(t)
	d.notify = []string{`{"messageType":2,"notifications":[{"oid":11,"eventId":{"level":1,"index":1},
	  "eventData":{"propertyId":{"level":1,"index":6},"changeType":0,"value":"Studio C","sequenceItemIndex":null}}]}`}

	for _, tc := range []struct {
		rolePath string
		want     []int
	}{{"root.receivers.rx1", []int{11}}, {"", []int{1, 3, 10, 11}}} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		var got []ControlChange
		res, err := h.ctrl.Control(ctx, ControlRequest{DeviceID: dev, RolePath: tc.rolePath, Watch: true,
			OnChange: func(c ControlChange) { got = append(got, c); cancel() }})
		cancel()
		if err != nil || res.Changes != 1 || len(got) != 1 {
			t.Fatalf("watch of %q = %+v, %v (changes %+v)", tc.rolePath, res, err, got)
		}
		if c := got[0]; c.RolePath != "root.receivers.rx1" || c.OID != 11 || c.Property != "1p6" || string(c.Value) != `"Studio C"` {
			t.Errorf("change = %+v", c)
		}
		d.mu.Lock()
		asked := d.asked[len(d.asked)-1]
		d.mu.Unlock()
		if fmt.Sprint(asked) != fmt.Sprint(tc.want) {
			t.Errorf("watch of %q subscribed to %v, want %v", tc.rolePath, asked, tc.want)
		}
	}
}

// What the catalogue or the Device does not have, said by name.
func TestControlOnWhatIsNotThere(t *testing.T) {
	h, d, dev := controlHarness(t)
	ctx := context.Background()

	if _, err := h.ctrl.Control(ctx, ControlRequest{}); err == nil || !strings.Contains(err.Error(), "a device id is required") {
		t.Errorf("a request that names no device: %v", err)
	}
	if _, err := h.ctrl.Control(ctx, ControlRequest{DeviceID: uuidN(9)}); err == nil || !strings.Contains(err.Error(), "no device "+uuidN(9)) {
		t.Errorf("an unknown device: %v", err)
	}
	if _, err := h.ctrl.Control(ctx, ControlRequest{DeviceID: dev, RolePath: "root.gain"}); err == nil || !strings.Contains(err.Error(), "has no object root.gain") {
		t.Errorf("an unknown object: %v", err)
	}

	// A model with no class manager lists and reads; it cannot describe.
	d.members = strings.Replace(controlMembers, "[1,3,2]", "[1,3]", 1)
	if _, err := h.ctrl.Control(ctx, ControlRequest{DeviceID: dev, RolePath: "root.receivers"}); err == nil || !strings.Contains(err.Error(), "no class manager") {
		t.Errorf("describing without a class manager: %v", err)
	}

	// A Device with no ncp control has no IS-12 endpoint.
	h.cat.devices[0].Controls = []is04.DeviceControl{}
	if _, err := h.ctrl.Control(ctx, ControlRequest{DeviceID: dev}); err == nil || !strings.Contains(err.Error(), "advertises no urn:x-nmos:control:ncp control") {
		t.Errorf("a device without the control: %v", err)
	}
	// One that advertises an endpoint nothing answers at cannot be dialled.
	h.cat.devices[0].Controls = []is04.DeviceControl{{Href: "ws://127.0.0.1:1/x-nmos/ncp/v1.0", Type: "urn:x-nmos:control:ncp/v1.0"}}
	if _, err := h.ctrl.Control(ctx, ControlRequest{DeviceID: dev}); err == nil || !strings.Contains(err.Error(), "nmos/control") {
		t.Errorf("an endpoint nothing answers at: %v", err)
	}
}

// A Device that answers no is quoted, with its own words when it gives
// any.
func TestControlQuotesADeviceThatRefuses(t *testing.T) {
	const rx = "root.receivers.rx1"
	for _, tc := range []struct {
		name   string
		answer string // "<oid>/<method>" the Device refuses
		result string
		req    ControlRequest
		want   string
	}{
		{"the root block's members", "1/2m1", `{"status":500,"errorMessage":"busy"}`, ControlRequest{},
			"members of the root block: the Device answered 500: busy"},
		{"a get", "11/1m1", `{"status":404}`, ControlRequest{RolePath: rx, Get: "1p6"},
			"property 1p6 of " + rx + ": the Device answered 404"},
		{"a set", "11/1m2", `{"status":405,"errorMessage":"read only"}`, ControlRequest{RolePath: rx, Set: "1p6", SetValue: json.RawMessage(`1`)},
			"setting 1p6 on " + rx + ": the Device answered 405: read only"},
		{"a method", "11/4m1", `{"status":406}`, ControlRequest{RolePath: rx, Invoke: "4m1"},
			"method 4m1 on " + rx + ": the Device answered 406"},
		{"a class", "3/3m1", `{"status":404}`, ControlRequest{RolePath: rx},
			"class of " + rx + ": the Device answered 404"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, d, dev := controlHarness(t)
			d.answers[tc.answer] = tc.result
			tc.req.DeviceID = dev
			_, err := h.ctrl.Control(context.Background(), tc.req)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to say %q", err, tc.want)
			}
		})
	}
}

// A socket that dies is an error at whichever step it dies: the model
// takes three commands (role, class, members), the operation follows.
func TestControlOnASocketThatDies(t *testing.T) {
	const rx = "root.receivers.rx1"
	onChange := func(ControlChange) {}
	for _, tc := range []struct {
		name  string
		dieAt int
		req   ControlRequest
	}{
		{"reading the root's role", 1, ControlRequest{}},
		{"reading the root's class", 2, ControlRequest{}},
		{"reading the members", 3, ControlRequest{}},
		{"a get", 4, ControlRequest{RolePath: rx, Get: "1p6"}},
		{"a method", 4, ControlRequest{RolePath: rx, Invoke: "4m1"}},
		{"a class", 4, ControlRequest{RolePath: rx}},
		{"a dry-run set", 4, ControlRequest{RolePath: rx, Set: "1p6", SetValue: json.RawMessage(`1`), DryRun: true}},
		{"a set", 4, ControlRequest{RolePath: rx, Set: "1p6", SetValue: json.RawMessage(`1`)}},
		{"the read back of a set", 5, ControlRequest{RolePath: rx, Set: "1p6", SetValue: json.RawMessage(`1`)}},
		{"a subscription", 4, ControlRequest{RolePath: rx, Watch: true, OnChange: onChange}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, d, dev := controlHarness(t)
			d.dieAt = tc.dieAt
			tc.req.DeviceID = dev
			if _, err := h.ctrl.Control(context.Background(), tc.req); err == nil || !strings.Contains(err.Error(), "closed the socket") {
				t.Fatalf("err = %v, want the closed socket", err)
			}
		})
	}
}

func TestControlRefusesARequestItCannotCarryOut(t *testing.T) {
	onChange := func(ControlChange) {}
	for _, tc := range []struct {
		req  ControlRequest
		want string
	}{
		{ControlRequest{}, "a device id is required"},
		{ControlRequest{DeviceID: "d", RolePath: "root", Get: "1p1", Invoke: "1m1"}, "one request each"},
		{ControlRequest{DeviceID: "d", Get: "1p1"}, "name the object with a role path"},
		{ControlRequest{DeviceID: "d", Watch: true}, "somewhere to hand the changes"},
		{ControlRequest{DeviceID: "d", RolePath: "root", Set: "1p6"}, "setting 1p6 needs a value"},
		{ControlRequest{DeviceID: "d", RolePath: "root", Set: "1p6", SetValue: json.RawMessage(`{`)}, "the value for 1p6 is not JSON"},
		{ControlRequest{DeviceID: "d", RolePath: "root", Invoke: "1m1", Arguments: json.RawMessage(`{`)}, "the arguments for 1m1 are not JSON"},
		{ControlRequest{DeviceID: "d", RolePath: "root", Get: "label"}, `"label" is not a property id`},
		{ControlRequest{DeviceID: "d", RolePath: "root", Get: "1m1"}, `"1m1" is not a property id`},
		{ControlRequest{DeviceID: "d", RolePath: "root", Set: "0p1", SetValue: json.RawMessage(`1`)}, `"0p1" is not a property id`},
		{ControlRequest{DeviceID: "d", RolePath: "root", Invoke: "3mx"}, `"3mx" is not a method id`},
	} {
		if err := checkControlRequest(tc.req); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: err = %v, want it to say %q", tc.req, err, tc.want)
		}
	}
	if err := checkControlRequest(ControlRequest{DeviceID: "d", Watch: true, OnChange: onChange}); err != nil {
		t.Errorf("a watch of the whole model: %v", err)
	}
}

func TestClassIsItsBaseOrDerivedFromIt(t *testing.T) {
	for _, tc := range []struct {
		id   []int32
		want bool
	}{{[]int32{1, 3, 2}, true}, {[]int32{1, 3, 2, 0, 7}, true}, {[]int32{1, 3}, false}, {[]int32{1, 3, 1}, false}} {
		if got := isClass(tc.id, classManager); got != tc.want {
			t.Errorf("isClass(%v) = %t, want %t", tc.id, got, tc.want)
		}
	}
}
