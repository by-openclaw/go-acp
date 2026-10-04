package control

// The IS-12 client: a command is paired with its response by handle,
// whatever else the Device sends in between; a Device that answers no,
// answers with something that is not IS-12, or does not answer, is
// reported as what it is.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is12"
	httpsession "dhs/internal/amwa/session/http"
)

// socket is the Device's end of one control connection.
type socket struct {
	ws *httpsession.WebSocket
}

func (s socket) send(frame string) { _ = s.ws.SendText([]byte(frame)) }
func (s socket) close()            { _ = s.ws.Close() }

// device is a scripted IS-12 Device: every frame the Controller sends
// is handed to script, which answers with whatever it likes. It returns
// the control href and the frames received so far.
func device(t *testing.T, script func(frame string, s socket)) (href string, received func() []string) {
	t.Helper()
	var mu sync.Mutex
	var frames []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			mu.Lock()
			frames = append(frames, string(raw))
			mu.Unlock()
			script(string(raw), socket{ws})
		}
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/x-nmos/ncp/v1.0", func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), frames...)
	}
}

func dial(t *testing.T, href string) *Client {
	t.Helper()
	c, err := Dial(context.Background(), href)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// command decodes the one command of a frame the Device received.
func command(t *testing.T, frame string) is12.Command {
	t.Helper()
	m, err := is12.Decode([]byte(frame))
	if err != nil {
		t.Fatalf("the Controller sent a frame that is not IS-12: %v\n%s", err, frame)
	}
	cmd, ok := m.(is12.CommandMessage)
	if !ok || len(cmd.Commands) != 1 {
		t.Fatalf("the Controller sent %T, want one command", m)
	}
	return cmd.Commands[0]
}

// answer is a command response for one handle.
func answer(handle int, result string) string {
	return `{"messageType":1,"responses":[{"handle":` + itoa(handle) + `,"result":` + result + `}]}`
}

func itoa(n int) string {
	raw, _ := json.Marshal(n)
	return string(raw)
}

const notification = `{"messageType":2,"notifications":[{"oid":7,"eventId":{"level":1,"index":1},
  "eventData":{"propertyId":{"level":1,"index":6},"changeType":0,"value":"new","sequenceItemIndex":null}}]}`

// The response is found by its handle among a notification, a response
// to something else and a frame of another kind; the notification is
// kept and delivered later.
func TestInvokePairsACommandWithItsResponse(t *testing.T) {
	href, received := device(t, func(frame string, s socket) {
		cmd := command(t, frame)
		s.send(notification)
		s.send(`{"messageType":4,"subscriptions":[]}`)
		s.send(answer(cmd.Handle+100, `{"status":500}`))
		s.send(answer(cmd.Handle, `{"status":200,"value":"ok"}`))
	})
	c := dial(t, href)
	if c.Href != href {
		t.Errorf("Href = %q, want %q", c.Href, href)
	}

	res, err := c.Invoke(context.Background(), 5, is12.MethodID{Level: 3, Index: 2}, map[string]int{"n": 1})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !OK(res) || string(res.Value) != `"ok"` {
		t.Errorf("result = %+v, want 200 with value \"ok\"", res)
	}
	first := command(t, received()[0])
	if first.Handle != 1 || first.OID != 5 || first.MethodID != (is12.MethodID{Level: 3, Index: 2}) || !sameJSON(first.Arguments, `{"n":1}`) {
		t.Errorf("the Device received %+v (arguments %s)", first, first.Arguments)
	}

	// The next command takes the next handle, and no arguments means none.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := c.Invoke(ctx, 5, is12.MethodID{Level: 3, Index: 2}, nil); err != nil {
		t.Fatalf("second Invoke: %v", err)
	}
	if second := command(t, received()[1]); second.Handle != 2 || second.Arguments != nil {
		t.Errorf("second command = %+v, want handle 2 and no arguments", second)
	}

	// Both commands met a notification on the way; both are delivered.
	done, stop := context.WithCancel(context.Background())
	stop()
	var got []is12.Notification
	if err := c.Notifications(done, func(n is12.Notification) { got = append(got, n) }); err != nil {
		t.Fatalf("Notifications: %v", err)
	}
	if len(got) != 2 || got[0].OID != 7 || string(got[0].EventData.Value) != `"new"` {
		t.Errorf("kept notifications = %+v, want the two the Device sent", got)
	}
}

func sameJSON(raw json.RawMessage, want string) bool {
	var a, b any
	if json.Unmarshal(raw, &a) != nil || json.Unmarshal([]byte(want), &b) != nil {
		return false
	}
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// A handle stays within what IS-12 allows on a socket that lives long.
func TestHandlesGoRound(t *testing.T) {
	href, received := device(t, func(frame string, s socket) {
		s.send(answer(command(t, frame).Handle, `{"status":200}`))
	})
	c := dial(t, href)
	c.handle = 65535
	if _, err := c.Invoke(context.Background(), 1, MethodGet, nil); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if h := command(t, received()[0]).Handle; h != 1 {
		t.Errorf("the handle after 65535 is %d, want 1", h)
	}
}

// Get, Set, Members and Class are the MS-05-02 methods, with the
// arguments MS-05-02 gives them.
func TestTheMS05MethodsAreWordedAsTheSpecDoes(t *testing.T) {
	href, received := device(t, func(frame string, s socket) {
		cmd := command(t, frame)
		value := `"v"`
		if cmd.MethodID == MethodGetMemberDescriptors {
			value = `[{"description":null,"role":"gain","oid":9,"constantOid":true,"classId":[1,2,1],"userLabel":"Gain","owner":1}]`
		}
		s.send(answer(cmd.Handle, `{"status":200,"value":`+value+`}`))
	})
	c := dial(t, href)
	ctx := context.Background()
	id := is12.PropertyID{Level: 1, Index: 6}

	if res, err := c.Get(ctx, 9, id); err != nil || string(res.Value) != `"v"` {
		t.Fatalf("Get: %+v, %v", res, err)
	}
	if _, err := c.Set(ctx, 9, id, json.RawMessage(`"label"`)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	members, res, err := c.Members(ctx, RootBlockOID, true)
	if err != nil || !OK(res) || len(members) != 1 || members[0].Role != "gain" || members[0].Oid != 9 || members[0].Owner != 1 {
		t.Fatalf("Members: %+v, %+v, %v", members, res, err)
	}
	if _, err := c.Class(ctx, 3, []int32{1, 2, 1}); err != nil {
		t.Fatalf("Class: %v", err)
	}

	want := []struct {
		oid    int
		method is12.MethodID
		args   string
	}{
		{9, MethodGet, `{"id":{"level":1,"index":6}}`},
		{9, MethodSet, `{"id":{"level":1,"index":6},"value":"label"}`},
		{1, MethodGetMemberDescriptors, `{"recurse":true}`},
		{3, MethodGetControlClass, `{"classId":[1,2,1],"includeInherited":true}`},
	}
	frames := received()
	for i, w := range want {
		cmd := command(t, frames[i])
		if cmd.OID != w.oid || cmd.MethodID != w.method || !sameJSON(cmd.Arguments, w.args) {
			t.Errorf("command %d = oid %d method %+v arguments %s, want oid %d method %+v arguments %s",
				i, cmd.OID, cmd.MethodID, cmd.Arguments, w.oid, w.method, w.args)
		}
	}
}

// A block that answers no, or with something that is not a member list,
// is not read as an empty block.
func TestMembersOfABlockThatDoesNotAnswerWithMembers(t *testing.T) {
	results := []string{`{"status":404,"errorMessage":"no such block"}`, `{"status":200,"value":5}`}
	href, _ := device(t, func(frame string, s socket) {
		cmd := command(t, frame)
		s.send(answer(cmd.Handle, results[cmd.Handle-1]))
	})
	c := dial(t, href)

	members, res, err := c.Members(context.Background(), 4, false)
	if err != nil || members != nil || res.Status != 404 || res.ErrorMessage != "no such block" {
		t.Errorf("a refused block: %+v, %+v, %v — want the Device's 404 and no error", members, res, err)
	}
	if _, _, err := c.Members(context.Background(), 4, false); err == nil || !strings.Contains(err.Error(), "not a list of member descriptors") {
		t.Errorf("a value that is no member list: %v", err)
	}
}

// What can go wrong with an answer, each said as what it is.
func TestAnAnswerThatIsNotOne(t *testing.T) {
	cases := []struct {
		name   string
		script func(frame string, s socket)
		want   string
	}{
		{"the Device refuses the message", func(_ string, s socket) {
			s.send(`{"messageType":5,"status":400,"errorMessage":"bad handle"}`)
		}, "refused the message with status 400: bad handle"},
		{"the Device answers with something else", func(_ string, s socket) { s.send(`{"hello":1}`) }, "not IS-12"},
		{"the Device hangs up", func(_ string, s socket) { s.close() }, "closed the socket before it answered"},
		{"the Device says nothing", func(string, socket) {}, "no answer from the Device"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			href, _ := device(t, tc.script)
			c := dial(t, href)
			c.Timeout = 200 * time.Millisecond
			_, err := c.Get(context.Background(), 1, is12.PropertyID{Level: 1, Index: 1})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to say %q", err, tc.want)
			}
		})
	}

	// The refusal is a typed error: a caller can tell it from a dead socket.
	href, _ := device(t, cases[0].script)
	_, err := dial(t, href).Get(context.Background(), 1, is12.PropertyID{Level: 1, Index: 1})
	var refused *ProtocolError
	if !errors.As(err, &refused) || refused.Status != 400 {
		t.Errorf("err = %v, want a ProtocolError carrying 400", err)
	}
}

// What can go wrong before anything is sent.
func TestACommandThatCannotBeSent(t *testing.T) {
	if _, err := Dial(context.Background(), "http://not-a-websocket"); err == nil {
		t.Error("a dial at an http URL succeeded")
	}

	href, received := device(t, func(string, socket) {})
	c := dial(t, href)
	if _, err := c.Invoke(context.Background(), 1, MethodGet, make(chan int)); err == nil || !strings.Contains(err.Error(), "arguments of 1m1") {
		t.Errorf("arguments that are not JSON: %v", err)
	}
	if _, err := c.Invoke(context.Background(), 0, MethodGet, nil); err == nil || !strings.Contains(err.Error(), "oid 0") {
		t.Errorf("an oid IS-12 does not allow: %v", err)
	}
	_ = c.Close()
	if _, err := c.Get(context.Background(), 1, is12.PropertyID{Level: 1, Index: 1}); err == nil || !strings.Contains(err.Error(), "send") {
		t.Errorf("a command on a closed socket: %v", err)
	}
	if _, _, err := c.Members(context.Background(), 1, true); err == nil {
		t.Error("Members on a closed socket succeeded")
	}
	if n := len(received()); n != 0 {
		t.Errorf("the Device received %d frame(s), want none", n)
	}
}

// A subscription names its whole set — none is an empty list, not a
// missing one — and returns what the Device took.
func TestSubscribeReturnsWhatTheDeviceTook(t *testing.T) {
	href, received := device(t, func(frame string, s socket) {
		var sub is12.SubscriptionMessage
		_ = json.Unmarshal([]byte(frame), &sub)
		raw, _ := json.Marshal(sub.Subscriptions)
		s.send(`{"messageType":4,"subscriptions":` + string(raw) + `}`)
	})
	c := dial(t, href)

	taken, err := c.Subscribe(context.Background(), []int{1, 9})
	if err != nil || len(taken) != 2 || taken[1] != 9 {
		t.Fatalf("Subscribe: %v, %v", taken, err)
	}
	if taken, err = c.Subscribe(context.Background(), nil); err != nil || len(taken) != 0 {
		t.Fatalf("Subscribe to nothing: %v, %v", taken, err)
	}
	if last := received()[1]; !strings.Contains(strings.Join(strings.Fields(last), ""), `"subscriptions":[]`) {
		t.Errorf("a subscription to nothing was sent as %s", last)
	}
}

// Notifications are handed over as they come, other frames are passed
// by, and the end of the context is the ordinary way out.
func TestNotificationsUntilTheContextEnds(t *testing.T) {
	href, _ := device(t, func(_ string, s socket) {
		s.send(`{"messageType":4,"subscriptions":[7]}`)
		s.send(`{"messageType":4,"subscriptions":[7]}`) // not a notification: passed by
		s.send(notification)
	})
	c := dial(t, href)
	if _, err := c.Subscribe(context.Background(), []int{7}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// The handler ends the context: the loop stops before its next read.
	ctx, cancel := context.WithCancel(context.Background())
	seen := 0
	if err := c.Notifications(ctx, func(n is12.Notification) { seen++; cancel() }); err != nil {
		t.Fatalf("Notifications: %v", err)
	}
	if seen != 1 {
		t.Errorf("saw %d notification(s), want 1", seen)
	}

	// The context ends while the read waits: the read is released.
	waiting, done := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer done()
	if err := c.Notifications(waiting, func(is12.Notification) { t.Error("a notification nobody sent") }); err != nil {
		t.Fatalf("Notifications released by its context: %v", err)
	}
}

// A socket that dies, or a frame that is not IS-12, ends the watch with
// the reason.
func TestNotificationsOnASocketThatFails(t *testing.T) {
	for name, tc := range map[string]struct {
		script func(frame string, s socket)
		want   string
	}{
		"the Device hangs up":             {func(_ string, s socket) { s.send(`{"messageType":4,"subscriptions":[]}`); s.close() }, "read"},
		"the Device sends something else": {func(_ string, s socket) { s.send(`{"messageType":4,"subscriptions":[]}`); s.send(`[]`) }, "not IS-12"},
	} {
		t.Run(name, func(t *testing.T) {
			href, _ := device(t, tc.script)
			c := dial(t, href)
			if _, err := c.Subscribe(context.Background(), nil); err != nil {
				t.Fatalf("Subscribe: %v", err)
			}
			err := c.Notifications(context.Background(), func(is12.Notification) {})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to say %q", err, tc.want)
			}
		})
	}
}
