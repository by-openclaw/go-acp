package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dhs/internal/consumer/compliance"
	"dhs/internal/rcp/codec"
)

// call is one request the fake server received.
type call struct {
	method, path, reqid, auth, body string
	contentLength                   int64
}

// fake is an RCP server that answers from a table of "METHOD path"
// entries and records what it was asked. The bodies below are the ones
// the lab Cerebrum (API 2.5.3) answered on 2026-10-09, or the API
// document's own examples where it says so.
type fake struct {
	t       *testing.T
	answers map[string]func(reqid string) (int, string)
	calls   []call
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.calls = append(f.calls, call{r.Method, r.URL.Path, r.Header.Get("reqid"), r.Header.Get("Authorization"), string(body), r.ContentLength})
	fn, ok := f.answers[r.Method+" "+r.URL.Path]
	if !ok {
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusTeapot)
		return
	}
	status, out := fn(r.Header.Get("reqid"))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, out)
}

func (f *fake) last() call { return f.calls[len(f.calls)-1] }

func ok(body string) func(string) (int, string) {
	return func(reqid string) (int, string) { return 200, `{"reqid":` + reqid + body + `}` }
}

func newSession(t *testing.T, answers map[string]func(string) (int, string)) (*Client, *fake, *compliance.Profile) {
	t.Helper()
	f := &fake{t: t, answers: answers}
	if _, has := answers["POST /v2/login"]; !has {
		answers["POST /v2/login"] = ok(`,"login":{"token":"tok-1","websocketPort":9081}`)
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	p := &compliance.Profile{}
	c := New(Options{Host: strings.TrimPrefix(srv.URL, "http://"), Profile: p})
	if err := c.Login(context.Background(), "u", "p"); err != nil {
		t.Fatalf("login: %v", err)
	}
	return c, f, p
}

func TestAPINeedsNoLoginAndReadsNumericVersion(t *testing.T) {
	f := &fake{t: t, answers: map[string]func(string) (int, string){
		"GET /v2/api": ok(`,"api":{"majorVersion":2,"minorVersion":5,"patchVersion":3}`),
	}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	c := New(Options{Host: strings.TrimPrefix(srv.URL, "http://")})

	v, err := c.API(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v.String() != "2.5.3" {
		t.Errorf("version = %s, want 2.5.3", v)
	}
	if got := f.last(); got.reqid != "1" || got.auth != "" {
		t.Errorf("request = %+v, want reqid 1 and no Authorization", got)
	}
}

func TestAPIReadsTheDocumentsStringVersion(t *testing.T) {
	var v codec.APIVersion
	if err := json.Unmarshal([]byte(`{"majorVersion":"1","minorVersion":"1"}`), &v); err != nil {
		t.Fatal(err)
	}
	if v.String() != "1.1.0" {
		t.Errorf("version = %s, want 1.1.0", v)
	}
}

func TestLoginSendsCredentialsAndKeepsTheToken(t *testing.T) {
	c, f, _ := newSession(t, map[string]func(string) (int, string){
		"GET /v2/login": ok(`,"login":{"username":"dhs-staging"}`),
	})
	if got := f.last(); got.body != `{"password":"p","username":"u"}` || got.auth != "" {
		t.Errorf("login request = %+v", got)
	}
	if c.WebsocketPort() != 9081 {
		t.Errorf("websocket port = %d, want 9081", c.WebsocketPort())
	}
	who, err := c.WhoAmI(context.Background())
	if err != nil || who != "dhs-staging" {
		t.Fatalf("whoami = %q, %v", who, err)
	}
	if got := f.last(); got.auth != "Bearer tok-1" || got.reqid != "2" {
		t.Errorf("whoami request = %+v, want the bearer token and reqid 2", got)
	}
}

func TestACallBeforeLoginIsRefusedLocally(t *testing.T) {
	c := New(Options{Host: "127.0.0.1:1"})
	if _, err := c.List(context.Background(), codec.Sources); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("err = %v, want ErrNotLoggedIn", err)
	}
}

func TestHeartbeatDeclaresAZeroLength(t *testing.T) {
	// The HTTP stack in front of Cerebrum answers 411 to a POST with no
	// Content-Length.
	c, f, _ := newSession(t, map[string]func(string) (int, string){"POST /v2/heartbeat": ok(``)})
	if err := c.Heartbeat(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.last(); got.contentLength != 0 || got.body != "" {
		t.Errorf("heartbeat request = %+v, want an empty body of declared length 0", got)
	}
}

func TestLogoutDropsTheToken(t *testing.T) {
	c, _, _ := newSession(t, map[string]func(string) (int, string){"DELETE /v2/login": ok(``)})
	if err := c.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.LoggedIn() {
		t.Error("still logged in after Logout")
	}
}

func TestListReturnsTheIDsAsStrings(t *testing.T) {
	c, _, _ := newSession(t, map[string]func(string) (int, string){
		"GET /v2/routemaster/sources":            ok(`,"ids":["1","2","3","4","5","6"]`),
		"GET /v2/routemaster/federation-sources": ok(`,"ids":[]`),
	})
	ids, err := c.List(context.Background(), codec.Sources)
	if err != nil || strings.Join(ids, ",") != "1,2,3,4,5,6" {
		t.Fatalf("ids = %v, %v", ids, err)
	}
	ids, err = c.List(context.Background(), codec.FederationSources)
	if err != nil || ids == nil || len(ids) != 0 {
		t.Fatalf("federation ids = %#v, %v — want an empty, non-nil list", ids, err)
	}
}

// What Cerebrum 2.5.3 answered for destination 1.
const realDestination1 = `{"id":1,"virtual":false,"federationUid":0,"tieLineInhibit":false,"tieLineGroup":0,"mnemonic":{"originalMnemonic":"Dest 1","alternateMnemonics":{}},"levels":{"level_1":{"id":1,"name":"Level 1","tags":[],"tagsInherited":true,"device":{"name":"Snell SW-P-08","typeId":-1073741824,"deviceLevel":1,"io":1,"inhibit":false,"ignore":false}}},"tags":[]}`

func TestGetAbsorbsThePluralKeyAndCountsIt(t *testing.T) {
	c, _, p := newSession(t, map[string]func(string) (int, string){
		"GET /v2/routemaster/destinations/1": ok(`,"destinations":` + realDestination1),
	})
	io, err := c.Get(context.Background(), codec.Destinations, "1")
	if err != nil {
		t.Fatal(err)
	}
	if io.ID != 1 || io.Mnemonic.Original != "Dest 1" {
		t.Errorf("io = %+v", io)
	}
	l := io.Levels["level_1"]
	if l.Device == nil || l.Device.Name != "Snell SW-P-08" || l.Device.TypeID != -1073741824 || l.Device.IO != 1 || !l.TagsInherited {
		t.Errorf("level_1 = %+v device %+v", l, l.Device)
	}
	if p.Snapshot()[SingleIOKey] != 1 {
		t.Errorf("deviations = %v, want %s counted once", p.Snapshot(), SingleIOKey)
	}
}

func TestGetReadsTheDocumentsKeyWithoutADeviation(t *testing.T) {
	c, _, p := newSession(t, map[string]func(string) (int, string){
		"GET /v2/routemaster/sources/7": ok(`,"source":{"id":7,"mnemonic":{"originalMnemonic":"SRC-0007"},"virtual":true}`),
	})
	io, err := c.Get(context.Background(), codec.Sources, "7")
	if err != nil || !io.Virtual || io.Mnemonic.Original != "SRC-0007" {
		t.Fatalf("io = %+v, %v", io, err)
	}
	if len(p.Snapshot()) != 0 {
		t.Errorf("deviations = %v, want none", p.Snapshot())
	}
}

func TestAnErrorAnswerCarriesStatusCodeAndMessage(t *testing.T) {
	c, _, _ := newSession(t, map[string]func(string) (int, string){
		"GET /v2/routemaster/sources/99": func(reqid string) (int, string) {
			return 404, `{"reqid":` + reqid + `,"error":{"code":1,"message":"Unknown source"}}`
		},
	})
	_, err := c.Get(context.Background(), codec.Sources, "99")
	var re *RequestError
	if !errors.As(err, &re) || re.Status != 404 || re.Code != 1 || re.Message != "Unknown source" {
		t.Fatalf("err = %v", err)
	}
}

func TestAnErrorThatIsNotTheEnvelopeIsCounted(t *testing.T) {
	c, _, p := newSession(t, map[string]func(string) (int, string){
		"POST /v2/heartbeat": func(string) (int, string) { return 411, `<HTML>Length Required</HTML>` },
	})
	err := c.Heartbeat(context.Background())
	var re *RequestError
	if !errors.As(err, &re) || re.Status != 411 || re.Message != "" {
		t.Fatalf("err = %v", err)
	}
	if p.Snapshot()[ErrorNotEnveloped] != 1 {
		t.Errorf("deviations = %v", p.Snapshot())
	}
}

func TestAnAnswerThatDoesNotEchoTheReqIDIsCounted(t *testing.T) {
	c, _, p := newSession(t, map[string]func(string) (int, string){
		"POST /v2/heartbeat": func(string) (int, string) { return 200, `{"reqid":4242}` },
	})
	if err := c.Heartbeat(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.Snapshot()[ReqIDNotEchoed] != 1 {
		t.Errorf("deviations = %v", p.Snapshot())
	}
}

func TestCreateUpdateDeleteSendTheDocumentsBodies(t *testing.T) {
	accepted := func(reqid string) (int, string) { return 202, `{"reqid":` + reqid + `}` }
	c, f, _ := newSession(t, map[string]func(string) (int, string){
		"POST /v2/routemaster/sources":         accepted,
		"PATCH /v2/routemaster/sources/7":      accepted,
		"DELETE /v2/routemaster/sources/7":     accepted,
		"POST /v2/routemaster/destinations":    accepted,
		"PATCH /v2/routemaster/destinations/3": accepted,
	})
	ctx := context.Background()
	two, yes, name, io1 := 2, true, "DHS-TEST-0001", 1

	if err := c.Create(ctx, codec.Sources, codec.Update{Count: &two, Mnemonic: &name}); err != nil {
		t.Fatal(err)
	}
	if got := f.last().body; got != `{"count":2,"mnemonic":"DHS-TEST-0001"}` {
		t.Errorf("create body = %s", got)
	}

	if err := c.Create(ctx, codec.Destinations, codec.Update{Count: &two, Virtual: &yes}); err != nil {
		t.Fatal(err)
	}
	if got := f.last().body; got != `{"count":2,"virtual":true}` {
		t.Errorf("virtual create body = %s", got)
	}

	none := []string{}
	u := codec.Update{Levels: map[string]codec.LevelUpdate{
		"level_1": {Tags: &none, Device: &codec.DeviceUpdate{IO: &io1}},
		"level_4": {Clear: &yes},
	}}
	if err := c.Update(ctx, codec.Sources, "7", u); err != nil {
		t.Fatal(err)
	}
	want := `{"levels":{"level_1":{"tags":[],"device":{"io":1}},"level_4":{"clear":true}}}`
	if got := f.last().body; got != want {
		t.Errorf("update body = %s\n          want %s", got, want)
	}

	group := 3
	if err := c.Update(ctx, codec.Destinations, "3", codec.Update{TieLineGroup: &group}); err != nil {
		t.Fatal(err)
	}
	if got := f.last().body; got != `{"tieLineGroup":3}` {
		t.Errorf("destination update body = %s", got)
	}

	if err := c.Delete(ctx, codec.Sources, "7"); err != nil {
		t.Fatal(err)
	}
	if got := f.last(); got.method != "DELETE" || got.body != "" {
		t.Errorf("delete request = %+v", got)
	}
}

func TestAWriteTheDocumentForbidsNeverReachesTheServer(t *testing.T) {
	c, f, _ := newSession(t, map[string]func(string) (int, string){})
	ctx := context.Background()
	sent := len(f.calls)
	yes, name, group, uid, one := true, "X", 1, int64(5), 1
	pipe := []string{"a|b"}
	sr := "Encap 1"

	cases := map[string]error{
		"empty update":                 c.Update(ctx, codec.Sources, "1", codec.Update{}),
		"count on update":              c.Update(ctx, codec.Sources, "1", codec.Update{Count: &one}),
		"tie line group on a source":   c.Update(ctx, codec.Sources, "1", codec.Update{TieLineGroup: &group}),
		"federation link on fed IO":    c.Update(ctx, codec.FederationSources, "1", codec.Update{FederationUID: &uid}),
		"virtual create with mnemonic": c.Create(ctx, codec.Sources, codec.Update{Virtual: &yes, Mnemonic: &name}),
		"clear with tags":              c.Update(ctx, codec.Sources, "1", codec.Update{Levels: map[string]codec.LevelUpdate{"level_1": {Clear: &yes, TagsInherit: &yes}}}),
		"tag with a pipe":              c.Update(ctx, codec.Sources, "1", codec.Update{Levels: map[string]codec.LevelUpdate{"level_1": {Tags: &pipe}}}),
		"disconnect on a source":       c.Update(ctx, codec.Sources, "1", codec.Update{Levels: map[string]codec.LevelUpdate{"level_1": {Device: &codec.DeviceUpdate{Disconnect: &yes}}}}),
		"ignore on a destination":      c.Update(ctx, codec.Destinations, "1", codec.Update{Levels: map[string]codec.LevelUpdate{"level_1": {Device: &codec.DeviceUpdate{Ignore: &yes}}}}),
		"io and sender together":       c.Update(ctx, codec.Sources, "1", codec.Update{Levels: map[string]codec.LevelUpdate{"level_1": {Device: &codec.DeviceUpdate{IO: &one, SenderReceiver: &sr}}}}),
		"no id":                        c.Delete(ctx, codec.Sources, " "),
	}
	for name, err := range cases {
		if err == nil {
			t.Errorf("%s: accepted, want a refusal", name)
		}
	}
	if len(f.calls) != sent {
		t.Errorf("%d request(s) reached the server", len(f.calls)-sent)
	}
}

func TestTheDocumentsUpdateExampleRoundTrips(t *testing.T) {
	// routeMasterDestinationUpdate, example of the API document 2.6.1.
	const example = `{"mnemonic":"DEST-0001","alternateMnemonics":{"AltMnemonic1":"Mon 1","AltMnemonic2":"Monitor 1"},"tieLineGroup":3,"tieLineInhibit":false,"federationUid":0,"virtual":false,"levels":{"level_1":{"tags":["Monitor"],"device":{"name":"Neuron 1 (01) CONV","typeId":1024,"deviceLevel":1,"io":1,"subChannel":"1","inhibit":false,"disconnect":false}},"level_2":{"tagsInherit":true,"device":{"senderReceiver":"Decap 1 ARX-1-1","subChannel":"1"}},"level_4":{"clear":true}}}`
	var u codec.Update
	d := json.NewDecoder(strings.NewReader(example))
	d.DisallowUnknownFields()
	if err := d.Decode(&u); err != nil {
		t.Fatalf("the document's example does not fit the type: %v", err)
	}
	if err := u.Validate(codec.Destinations, false); err != nil {
		t.Fatalf("the document's example is refused: %v", err)
	}
	out, err := json.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	_ = json.Unmarshal([]byte(example), &a)
	_ = json.Unmarshal(out, &b)
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Errorf("round trip changed the body:\n got %s\nwant %s", jb, ja)
	}
}

func TestParseCollection(t *testing.T) {
	for _, c := range codec.Collections {
		got, err := codec.ParseCollection(string(c))
		if err != nil || got != c {
			t.Errorf("%s: %v, %v", c, got, err)
		}
	}
	if _, err := codec.ParseCollection("levels"); err == nil {
		t.Error("levels accepted as a collection")
	}
}

// roundTrip is an http.RoundTripper from a function.
type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTheReqIDHeaderGoesOutInLowerCase(t *testing.T) {
	// Cerebrum 2.5.3 answers 400 "reqid missing from message headers" to
	// "Reqid", the canonical form.
	var keys []string
	hc := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		for k := range r.Header {
			keys = append(keys, k)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{},
			Body: io.NopCloser(strings.NewReader(`{"reqid":1,"api":{"majorVersion":2,"minorVersion":5}}`))}, nil
	})}
	c := New(Options{Host: "cerebrum", HTTP: hc})
	if _, err := c.API(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, k := range keys {
		if k == "reqid" {
			return
		}
	}
	t.Errorf("headers sent = %v, want a lower-case reqid", keys)
}
