package virtuoso

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	dhsc "dhs/internal/consumer"
	"dhs/internal/plugin"
	"dhs/internal/transport"
	"dhs/internal/virtuoso/codec"
)

func TestMeta(t *testing.T) {
	m := (&Factory{}).Meta()
	if m.Name != "virtuoso" || m.DefaultPort != 80 {
		t.Fatalf("%+v", m)
	}
	p := (&Factory{}).New(plugin.Deps{}).(*Plugin)
	if !p.PathNative() {
		t.Error("PathNative")
	}
	if p.MinOpTimeout() < 9*answerWait {
		t.Errorf("floor %v cannot cover nine modules", p.MinOpTimeout())
	}
}

func TestOptionsFromEnvironment(t *testing.T) {
	t.Setenv(EnvUser, "u")
	t.Setenv(EnvPass, "p")
	t.Setenv(EnvTLS, "")
	t.Setenv(EnvInsecure, "1")
	p := (&Factory{}).New(plugin.Deps{}).(*Plugin)
	o := p.options(DefaultPort)
	if o.User != "u" || o.Pass != "p" || o.TLS || !o.Insecure {
		t.Fatalf("%+v", o)
	}
	if !p.options(SecurePort).TLS {
		t.Error("port 443 implies TLS")
	}
	t.Setenv(EnvTLS, "1")
	if !p.options(DefaultPort).TLS {
		t.Error(EnvTLS)
	}
}

func TestConnectDefaultPortRefused(t *testing.T) {
	// A closed loopback port: the connect fails fast and says where.
	p := built(t, Options{})
	err := p.Connect(context.Background(), "127.0.0.1", 0)
	if err == nil {
		t.Skip("something listens on port 80 here")
	}
	if !strings.Contains(err.Error(), "127.0.0.1:80") {
		t.Fatalf("%v", err)
	}
}

func TestConnectTLSPosture(t *testing.T) {
	f := newFake(t)
	p := built(t, Options{TLS: true, Insecure: true})
	host, port := f.host()
	if err := p.Connect(context.Background(), host, port); err == nil {
		t.Fatal("HTTPS against an HTTP fake succeeded")
	}
}

func TestLoginGivesTheSessionItsCookie(t *testing.T) {
	f := newFake(t)
	p := connected(t, f)
	if f.logins != 1 {
		t.Fatalf("logins %d", f.logins)
	}
	info, err := p.GetDeviceInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.NumSlots != 9 || info.DtdVersion != "0.0.1" {
		t.Fatalf("%+v", info)
	}
	if err := p.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if f.logouts != 1 {
		t.Fatalf("logouts %d", f.logouts)
	}
	if _, err := p.GetDeviceInfo(context.Background()); !errors.Is(err, dhsc.ErrNotConnected) {
		t.Fatalf("after disconnect: %v", err)
	}
}

func TestConnectErrors(t *testing.T) {
	t.Run("bad password", func(t *testing.T) {
		f := newFake(t)
		p := built(t, Options{User: testUser, Pass: "wrong"})
		host, port := f.host()
		err := p.Connect(context.Background(), host, port)
		if !errors.Is(err, ErrLoginRefused) {
			t.Fatalf("%v", err)
		}
		if strings.Contains(err.Error(), "wrong") || strings.Contains(err.Error(), testUser) {
			t.Fatalf("credentials in an error: %v", err)
		}
	})
	t.Run("no credentials", func(t *testing.T) {
		f := newFake(t)
		p := built(t, Options{})
		host, port := f.host()
		if err := p.Connect(context.Background(), host, port); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("no cookie", func(t *testing.T) {
		f := newFake(t)
		f.noCookie = true
		p := built(t, Options{User: testUser, Pass: testPass})
		host, port := f.host()
		if err := p.Connect(context.Background(), host, port); !errors.Is(err, ErrNoSession) {
			t.Fatalf("%v", err)
		}
		if p.ComplianceProfile().Snapshot()[NoSessionCookie] != 1 {
			t.Fatal("no compliance event")
		}
	})
	t.Run("login status", func(t *testing.T) {
		f := newFake(t)
		f.statusOf[codec.LoginTarget] = http.StatusInternalServerError
		p := built(t, Options{User: testUser, Pass: testPass})
		host, port := f.host()
		var he *HTTPError
		if err := p.Connect(context.Background(), host, port); !errors.As(err, &he) || he.Status != 500 {
			t.Fatalf("%v", err)
		}
	})
	t.Run("login answers failed", func(t *testing.T) {
		f := newFake(t)
		f.info = `{"status":"failed","errors":["locked"]}`
		p := built(t, Options{User: testUser, Pass: testPass})
		host, port := f.host()
		var se *codec.StatusError
		if err := p.Connect(context.Background(), host, port); !errors.As(err, &se) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("redirect names the fix", func(t *testing.T) {
		f := newFake(t)
		f.statusOf[codec.ProductInfoTarget(0)] = http.StatusMovedPermanently
		p := built(t, Options{User: testUser, Pass: testPass})
		host, port := f.host()
		err := p.Connect(context.Background(), host, port)
		if err == nil || !strings.Contains(err.Error(), EnvTLS) {
			t.Fatalf("%v", err)
		}
		if f.logouts != 1 {
			t.Fatalf("a failed connect leaves its session behind: %d logouts", f.logouts)
		}
	})
}

func TestReconnectLogsOutTheOldSession(t *testing.T) {
	f := newFake(t)
	p := connected(t, f)
	host, port := f.host()
	if err := p.Connect(context.Background(), host, port); err != nil {
		t.Fatal(err)
	}
	if f.logouts != 1 || f.logins != 2 {
		t.Fatalf("logins %d logouts %d", f.logins, f.logouts)
	}
}

func TestNoLoginNoLogout(t *testing.T) {
	f := newFake(t)
	f.mu.Lock()
	f.sessions["open"] = true
	f.mu.Unlock()
	p := built(t, Options{})
	newREST(f.srv.URL, nilClient(), Options{}, p.deps.Logger, p.ComplianceProfile()).logout()
	if f.count("POST") != 0 {
		t.Fatalf("%v", f.requests())
	}
}

func TestSlotInfo(t *testing.T) {
	f := newFake(t)
	p := connected(t, f)
	tests := []struct {
		slot   int
		status dhsc.SlotStatus
		online bool
		reason string
	}{
		{0, dhsc.SlotPresent, true, ""},
		{1, dhsc.SlotPresent, true, ""},
		{2, dhsc.SlotNoCard, false, "no answer"}, // connection dropped
		{3, dhsc.SlotNoCard, false, "HTTP 404"},
		{4, dhsc.SlotNoCard, false, "HTTP 502"},
	}
	for _, tt := range tests {
		si, err := p.GetSlotInfo(context.Background(), tt.slot)
		if err != nil {
			t.Fatalf("slot %d must not be an error: %v", tt.slot, err)
		}
		if si.Status != tt.status || si.IsOnline != tt.online || si.Identity["reason"] != tt.reason {
			t.Errorf("slot %d: %+v", tt.slot, si)
		}
	}
	si, _ := p.GetSlotInfo(context.Background(), 1)
	if si.Identity["prodname"] != "UDC-IP-H25" || si.Identity["serialfull"] != "TST0000.0011" {
		t.Errorf("identity %v", si.Identity)
	}
	if _, err := p.GetSlotInfo(context.Background(), 9); err == nil {
		t.Error("slot 9 accepted")
	}
}

func TestSlotInfoAbsentOnAForeignPage(t *testing.T) {
	f := newFake(t)
	f.plainBody[codec.ProductInfoTarget(5)] = "<html>empty slot</html>"
	p := connected(t, f)
	si, err := p.GetSlotInfo(context.Background(), 5)
	if err != nil || si.Status != dhsc.SlotNoCard || si.Identity["reason"] != "answer is not the API" {
		t.Fatalf("%+v %v", si, err)
	}
	if p.ComplianceProfile().Snapshot()[AnswerNotJSON] != 1 {
		t.Error("no compliance event")
	}
}

func TestSlotStatusOtherThanOKIsAnError(t *testing.T) {
	f := newFake(t)
	p := connected(t, f)
	f.mu.Lock()
	f.info = `{"status":"failed","errors":[{"status":-1,"source":"card","detail":"booting"}]}`
	f.mu.Unlock()
	_, err := p.GetSlotInfo(context.Background(), 1)
	var se *codec.StatusError
	if !errors.As(err, &se) || !strings.Contains(err.Error(), "card: booting") {
		t.Fatalf("%v", err)
	}
}

func TestManagerWithoutAnswerIsAnError(t *testing.T) {
	f := newFake(t)
	p := connected(t, f)
	f.statusOf[codec.ProductInfoTarget(0)] = http.StatusBadGateway
	if _, err := p.GetSlotInfo(context.Background(), 0); err == nil {
		t.Fatal("slot 0 is never absent")
	}
}

func TestIdentityProbe(t *testing.T) {
	f := newFake(t)
	p := connected(t, f)
	id, err := p.IdentityProbe(context.Background(), 1)
	if err != nil || id != "UDC-IP-H25@0.0.2" {
		t.Fatalf("%q %v", id, err)
	}
	if _, err := p.IdentityProbe(context.Background(), 3); err == nil {
		t.Error("absent card has an identity")
	}
	if _, err := p.IdentityProbe(context.Background(), -1); err == nil {
		t.Error("slot -1 accepted")
	}
	f.mu.Lock()
	dev := f.docs[1]["dev"].(map[string]any)["product_info"].(map[string]any)
	delete(dev, "swver")
	dev["prodname"] = "A/B"
	f.mu.Unlock()
	if _, err := p.IdentityProbe(context.Background(), 1); err == nil {
		t.Error("identity without a version")
	}
	f.mu.Lock()
	dev["swver"] = "1"
	f.mu.Unlock()
	if id, _ := p.IdentityProbe(context.Background(), 1); id != "A-B@1" {
		t.Errorf("%q", id)
	}
}

func TestNotConnected(t *testing.T) {
	p := built(t, Options{})
	ctx := context.Background()
	if _, err := p.GetSlotInfo(ctx, 0); !errors.Is(err, dhsc.ErrNotConnected) {
		t.Error(err)
	}
	if _, err := p.IdentityProbe(ctx, 0); !errors.Is(err, dhsc.ErrNotConnected) {
		t.Error(err)
	}
	if _, err := p.Walk(ctx, 0); !errors.Is(err, dhsc.ErrNotConnected) {
		t.Error(err)
	}
	if _, err := p.GetValue(ctx, dhsc.ValueRequest{Path: "a/b"}); !errors.Is(err, dhsc.ErrNotConnected) {
		t.Error(err)
	}
	if _, err := p.SetValue(ctx, dhsc.ValueRequest{Path: "a/b"}, dhsc.Value{}); !errors.Is(err, dhsc.ErrNotConnected) {
		t.Error(err)
	}
	if err := p.Disconnect(); err != nil {
		t.Error(err)
	}
}

func TestWalkManager(t *testing.T) {
	f := newFake(t)
	p := connected(t, f)
	objs, err := p.Walk(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]dhsc.Object{}
	for i, o := range objs {
		if o.ID != i || o.Slot != 0 {
			t.Fatalf("%+v", o)
		}
		by[strings.Join(o.Path, "/")] = o
	}

	check := func(path string, kind dhsc.ValueKind, access uint8) dhsc.Object {
		t.Helper()
		o, ok := by[path]
		if !ok {
			t.Fatalf("%s not walked", path)
		}
		if o.Kind != kind || o.Access != access {
			t.Fatalf("%s: kind %v access %d", path, o.Kind, o.Access)
		}
		return o
	}
	if o := check("dev/product_info/devname", dhsc.KindString, 3); o.Value.Str != "TEST-MI-01" || o.Label != "devname" || o.Group != "dev/product_info" {
		t.Errorf("%+v", o)
	}
	if o := check("dev/temperature", dhsc.KindFloat, 1); o.Value.Float != 41.5 || o.Unit != "C" || o.Min != float64(-40) || o.Max != 125.5 {
		t.Errorf("%+v", o)
	}
	check("dev/product_info/model", dhsc.KindUint, 1)
	check("network/snmp/port_id", dhsc.KindInt, 3)
	check("network/ipinterface_coll/0/enable", dhsc.KindBool, 3)
	if o := check("network/snmp/trap_type", dhsc.KindEnum, 3); o.Value.Str != "INFORM" || o.Value.Enum != 1 || len(o.EnumItems) != 2 || o.Meta["enumValues"] != "1,2" || o.Def != float64(2) {
		t.Errorf("%+v", o)
	}
	if o := check("dev/system/do_reset", dhsc.KindRaw, 0); o.Meta["rpc"] != true || o.Meta["args"] != "delay,details,dev" {
		t.Errorf("%+v", o)
	}
	if o := check("network/ipinterface_coll/0/gid", dhsc.KindUint, 1); o.Meta["key"] != true {
		t.Errorf("%+v", o)
	}
	if o := check("alarms/log_note", dhsc.KindString, 1); o.Meta["noValue"] != true || o.Value.Kind != dhsc.KindUnknown {
		t.Errorf("%+v", o)
	}
	if o := by["network/routev4_static_coll/0.0.0.0:0:10/gw"]; o.Value.Str != "192.0.2.1" || o.Meta["format"] != "ipv4" {
		t.Errorf("%+v", o)
	}

	// Cached for the session: no second read of the device.
	gets := f.count("GET /api/data")
	if _, err := p.Walk(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if f.count("GET /api/data") != gets {
		t.Error("second walk read the device")
	}
	for _, r := range f.requests() {
		if !strings.HasPrefix(r, "GET ") && r != "POST /api/login" {
			t.Fatalf("a read path sent %s", r)
		}
	}
}

func TestWalkCardUndeclaredValue(t *testing.T) {
	f := newFake(t)
	p := connected(t, f)
	objs, err := p.Walk(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	var stray, clear bool
	for _, o := range objs {
		if o.Slot != 1 {
			t.Fatalf("%+v", o)
		}
		switch o.Label {
		case "severity":
			stray = o.Meta["undeclared"] == true && o.Access == 1 && o.Value.Uint == 3
		case "clear_stats":
			clear = o.Meta["rpc"] == true && o.Meta["args"] == nil
		}
	}
	if !stray || !clear {
		t.Fatalf("stray %v clear %v", stray, clear)
	}
	if p.ComplianceProfile().Snapshot()[DataNotInSchema] != 1 {
		t.Error("no compliance event")
	}
}

func TestWalkDeadSlots(t *testing.T) {
	f := newFake(t)
	p := connected(t, f)
	for _, slot := range []int{2, 3, 4} {
		if _, err := p.Walk(context.Background(), slot); !errors.Is(err, ErrSlotAbsent) {
			t.Errorf("slot %d: %v", slot, err)
		}
	}
	if _, err := p.Walk(context.Background(), 9); err == nil {
		t.Error("slot 9 accepted")
	}
}

func TestWalkBadAnswers(t *testing.T) {
	t.Run("data fails on a card", func(t *testing.T) {
		f := newFake(t)
		f.statusOf["/slot/1/api/data"] = http.StatusBadGateway
		p := connected(t, f)
		if _, err := p.Walk(context.Background(), 1); !errors.Is(err, ErrSlotAbsent) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("manager schema refused", func(t *testing.T) {
		f := newFake(t)
		f.statusOf["/api/schema.json"] = http.StatusForbidden
		p := connected(t, f)
		var he *HTTPError
		if _, err := p.Walk(context.Background(), 0); !errors.As(err, &he) || he.Status != 403 {
			t.Fatalf("%v", err)
		}
	})
	t.Run("schema not json", func(t *testing.T) {
		f := newFake(t)
		f.plainBody["/api/schema.json"] = "<html>"
		p := connected(t, f)
		if _, err := p.Walk(context.Background(), 0); !errors.Is(err, codec.ErrNotJSON) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("data is not an object", func(t *testing.T) {
		f := newFake(t)
		f.plainBody["/api/data"] = `{"data":[1],"info":{"status":"ok"}}`
		p := connected(t, f)
		if _, err := p.Walk(context.Background(), 0); !errors.Is(err, codec.ErrNotJSON) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("unresolved reference", func(t *testing.T) {
		f := newFake(t)
		f.schemas[0] = []byte(`{"properties":{"a":{"$ref":"#/definitions/gone"}}}`)
		f.plainBody["/api/data"] = `{"data":{},"info":{"status":"ok"}}`
		p := connected(t, f)
		if _, err := p.Walk(context.Background(), 0); err != nil {
			t.Fatal(err)
		}
		if p.ComplianceProfile().Snapshot()[UnresolvedRef] != 1 {
			t.Fatal("no compliance event")
		}
	})
}

func TestGetWithoutWalk(t *testing.T) {
	f := newFake(t)
	p := connected(t, f)
	tests := []struct {
		slot int
		path string
		want dhsc.Value
	}{
		{0, "dev/product_info/devname", dhsc.Value{Kind: dhsc.KindString, Str: "TEST-MI-01"}},
		{0, "dev.product_info.prodname", dhsc.Value{Kind: dhsc.KindString, Str: "Virtuoso MI"}},
		{0, "network/ipinterface_coll/0/enable", dhsc.Value{Kind: dhsc.KindBool, Bool: true}},
		{0, "network/snmp/port_id", dhsc.Value{Kind: dhsc.KindInt, Int: -1}},
		{0, "network/snmp/trap_type", dhsc.Value{Kind: dhsc.KindUint, Uint: 2}}, // no schema read: a plain number
		{0, "dev/temperature", dhsc.Value{Kind: dhsc.KindFloat, Float: 41.5}},
		{0, "network/routev4_static_coll/0.0.0.0:0:10/gw", dhsc.Value{Kind: dhsc.KindString, Str: "192.0.2.1"}},
		{1, "elements/sdiinput_coll/90000/sync", dhsc.Value{Kind: dhsc.KindBool, Bool: true}},
	}
	for _, tt := range tests {
		got, err := p.GetValue(context.Background(), dhsc.ValueRequest{Slot: tt.slot, Path: tt.path})
		if err != nil {
			t.Fatalf("%s: %v", tt.path, err)
		}
		if got.Kind != tt.want.Kind || got.Str != tt.want.Str || got.Bool != tt.want.Bool ||
			got.Int != tt.want.Int || got.Uint != tt.want.Uint || got.Float != tt.want.Float {
			t.Errorf("%s: %+v", tt.path, got)
		}
	}
	if n := f.count("GET /api/schema"); n != 0 {
		t.Errorf("get read the schema %d times", n)
	}
	if n := f.count("GET /api/data "); n != 0 {
		t.Errorf("get read the whole tree %d times", n)
	}
}

func TestGetObjectAnswersItsJSON(t *testing.T) {
	f := newFake(t)
	p := connected(t, f)
	v, err := p.GetValue(context.Background(), dhsc.ValueRequest{Path: "alarms"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind != dhsc.KindString || !strings.Contains(v.Str, `"count":3`) {
		t.Fatalf("%+v", v)
	}
}

func TestGetEnumLabelAfterWalk(t *testing.T) {
	f := newFake(t)
	p := connected(t, f)
	if _, err := p.Walk(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	v, err := p.GetValue(context.Background(), dhsc.ValueRequest{Path: "network/snmp/trap_type"})
	if err != nil || v.Kind != dhsc.KindEnum || v.Str != "INFORM" {
		t.Fatalf("%+v %v", v, err)
	}
	f.mu.Lock()
	f.docs[0]["network"].(map[string]any)["snmp"].(map[string]any)["trap_type"] = jsonNumber("9")
	f.mu.Unlock()
	v, err = p.GetValue(context.Background(), dhsc.ValueRequest{Path: "network/snmp/trap_type"})
	if err != nil || v.Kind != dhsc.KindUint || v.Uint != 9 {
		t.Fatalf("%+v %v", v, err)
	}
	if p.ComplianceProfile().Snapshot()[ValueOutsideEnum] != 1 {
		t.Error("no compliance event")
	}
}

func TestGetObjectAfterWalkStaysJSON(t *testing.T) {
	f := newFake(t)
	p := connected(t, f)
	if _, err := p.Walk(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	v, err := p.GetValue(context.Background(), dhsc.ValueRequest{Path: "alarms"})
	if err != nil || !strings.Contains(v.Str, `"toplevel":1`) {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestGetByLabel(t *testing.T) {
	f := newFake(t)
	p := connected(t, f)
	ctx := context.Background()
	if _, err := p.GetValue(ctx, dhsc.ValueRequest{Label: "devname"}); !errors.Is(err, dhsc.ErrUnknownLabel) {
		t.Fatalf("no walk yet: %v", err)
	}
	if _, err := p.Walk(ctx, 0); err != nil {
		t.Fatal(err)
	}
	v, err := p.GetValue(ctx, dhsc.ValueRequest{Label: "devname"})
	if err != nil || v.Str != "TEST-MI-01" {
		t.Fatalf("%+v %v", v, err)
	}
	if _, err := p.GetValue(ctx, dhsc.ValueRequest{Label: "enable"}); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("%v", err)
	}
	v, err = p.GetValue(ctx, dhsc.ValueRequest{Label: "mtu", Group: "network/ipinterface_coll/1"})
	if err != nil || v.Uint != 9000 {
		t.Fatalf("%+v %v", v, err)
	}
	v, err = p.GetValue(ctx, dhsc.ValueRequest{Label: "mtu", Group: "1"})
	if err != nil || v.Uint != 9000 {
		t.Fatalf("group suffix: %+v %v", v, err)
	}
}

func TestGetErrors(t *testing.T) {
	f := newFake(t)
	p := connected(t, f)
	ctx := context.Background()
	if _, err := p.GetValue(ctx, dhsc.ValueRequest{}); err == nil {
		t.Error("no path accepted")
	}
	if _, err := p.GetValue(ctx, dhsc.ValueRequest{Slot: 9, Path: "a"}); err == nil {
		t.Error("slot 9 accepted")
	}
	if _, err := p.GetValue(ctx, dhsc.ValueRequest{Path: "dev/nope"}); !errors.Is(err, dhsc.ErrObjectNotFound) {
		t.Errorf("404: %v", err)
	}
	f.mu.Lock()
	f.plainBody["/api/data/dev/thing"] = `{"data":{"dev":{}},"info":{"status":"ok"}}`
	f.plainBody["/api/data/dev/bad"] = `{"data":{"dev":{"bad":tru}},"info":{"status":"ok"}}`
	f.mu.Unlock()
	if _, err := p.GetValue(ctx, dhsc.ValueRequest{Path: "dev/thing"}); !errors.Is(err, dhsc.ErrObjectNotFound) {
		t.Errorf("not in answer: %v", err)
	}
	if p.ComplianceProfile().Snapshot()[NodeNotInAnswer] != 1 {
		t.Error("no compliance event")
	}
	if _, err := p.GetValue(ctx, dhsc.ValueRequest{Path: "dev/bad"}); !errors.Is(err, codec.ErrNotJSON) {
		t.Errorf("broken body: %v", err)
	}
	f.mu.Lock()
	f.info = `{"status":"failed","errors":["nope"]}`
	f.mu.Unlock()
	if _, err := p.GetValue(ctx, dhsc.ValueRequest{Path: "dev/temperature"}); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("status failed: %v", err)
	}
}

func TestValueOfNullAndList(t *testing.T) {
	p := built(t, Options{})
	if v := p.value(nil, nil); v.Kind != dhsc.KindString || v.Str != "" {
		t.Errorf("%+v", v)
	}
	if v := p.value([]any{"a"}, nil); v.Str != `["a"]` {
		t.Errorf("%+v", v)
	}
}

func TestInfoSurfaced(t *testing.T) {
	f := newFake(t)
	var log syncBuffer
	p := built(t, Options{User: testUser, Pass: testPass})
	p.deps.Logger = slog.New(slog.NewTextHandler(&log, nil))
	host, port := f.host()
	if err := p.Connect(context.Background(), host, port); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Disconnect() })

	read := func(info string) error {
		f.mu.Lock()
		f.info = info
		f.mu.Unlock()
		_, err := p.GetValue(context.Background(), dhsc.ValueRequest{Path: "dev/temperature"})
		return err
	}
	if err := read(`{"status":"ok","errors":[],"warnings":[{"status":-1,"source":"psu","detail":"fan slow"}],"notes":["saved"]}`); err != nil {
		t.Fatal(err)
	}
	if err := read(`{"status":"ok","errors":["stray"],"warnings":[],"notes":[]}`); err != nil {
		t.Fatal(err)
	}
	if err := read("none"); err != nil {
		t.Fatal(err)
	}
	if err := read(`{"status":"weird"}`); err == nil {
		t.Fatal("a status other than ok must be an error")
	}
	if err := read(`{"status":"failed"}`); err == nil {
		t.Fatal("failed must be an error")
	}
	got := log.String()
	for _, want := range []string{"psu: fan slow", "saved", "stray"} {
		if !strings.Contains(got, want) {
			t.Errorf("log lacks %q:\n%s", want, got)
		}
	}
	snap := p.ComplianceProfile().Snapshot()
	for _, ev := range []string{ErrorsWithOK, NoInfoObject, StatusUnknown, FailedWithoutErrors} {
		if snap[ev] != 1 {
			t.Errorf("%s: %d", ev, snap[ev])
		}
	}
	f.mu.Lock()
	f.info = ""
	f.mu.Unlock()
	if err := p.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "virtuoso compliance") {
		t.Error("no compliance summary at Disconnect")
	}
	if strings.Contains(log.String(), testPass) {
		t.Fatal("password in the log")
	}
}

func TestExpiredCookieLogsInOnce(t *testing.T) {
	f := newFake(t)
	p := connected(t, f)
	f.expireSessions()
	v, err := p.GetValue(context.Background(), dhsc.ValueRequest{Path: "dev/temperature"})
	if err != nil || v.Float != 41.5 {
		t.Fatalf("%+v %v", v, err)
	}
	if f.logins != 2 {
		t.Fatalf("logins %d", f.logins)
	}
	f.expireSessions()
	if _, err := p.Walk(context.Background(), 1); err != nil {
		t.Fatalf("schema path: %v", err)
	}
	if f.logins != 3 {
		t.Fatalf("logins %d", f.logins)
	}
}

func TestReloginFails(t *testing.T) {
	t.Run("refused", func(t *testing.T) {
		f := newFake(t)
		p := connected(t, f)
		f.expireSessions()
		r, _ := p.session()
		r.pass = "changed"
		if _, err := p.GetValue(context.Background(), dhsc.ValueRequest{Path: "dev/temperature"}); !errors.Is(err, ErrLoginRefused) {
			t.Fatalf("%v", err)
		}
		if _, err := p.GetSlotInfo(context.Background(), 1); !errors.Is(err, ErrLoginRefused) {
			t.Fatalf("a refused login is not an empty slot: %v", err)
		}
		if _, err := p.Walk(context.Background(), 1); !errors.Is(err, ErrLoginRefused) {
			t.Fatalf("schema path: %v", err)
		}
	})
	t.Run("401 after a fresh login", func(t *testing.T) {
		f := newFake(t)
		p := connected(t, f)
		f.statusOf["/api/data/dev/temperature"] = http.StatusUnauthorized
		if _, err := p.GetValue(context.Background(), dhsc.ValueRequest{Path: "dev/temperature"}); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("%v", err)
		}
		if f.logins != 2 {
			t.Fatalf("logged in %d times, once more is the limit", f.logins)
		}
	})
	t.Run("dead between login and retry", func(t *testing.T) {
		f := newFake(t)
		p := connected(t, f)
		f.expireSessions()
		f.mu.Lock()
		f.hangUp = "/api/data/dev/temperature"
		f.mu.Unlock()
		var te *dhsc.TransportError
		if _, err := p.GetValue(context.Background(), dhsc.ValueRequest{Path: "dev/temperature"}); !errors.As(err, &te) {
			t.Fatalf("%v", err)
		}
		if f.logins != 2 {
			t.Fatalf("logins %d", f.logins)
		}
	})
}

func TestTransportErrorKeepsItsMeaning(t *testing.T) {
	f := newFake(t)
	p := connected(t, f)
	r, _ := p.session()
	f.srv.Close()
	_, _, err := r.fetch(context.Background(), "GET", "/api/data")
	var te *dhsc.TransportError
	if !errors.As(err, &te) {
		t.Fatalf("%v", err)
	}
	if err := r.loginLocked(context.Background()); !errors.As(err, &te) {
		t.Fatalf("login: %v", err)
	}
}

func TestDetach(t *testing.T) {
	t.Run("a deadline does not reach the request", func(t *testing.T) {
		parent, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()
		ctx, done := detach(parent)
		defer done()
		time.Sleep(5 * time.Millisecond)
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
	})
	t.Run("a cancel does", func(t *testing.T) {
		parent, cancel := context.WithCancel(context.Background())
		ctx, done := detach(parent)
		defer done()
		cancel()
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
			t.Fatal("cancel did not arrive")
		}
	})
}

func TestExpiredDeadlineDoesNotCutAnAnswer(t *testing.T) {
	f := newFake(t)
	p := connected(t, f)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if _, err := p.GetValue(ctx, dhsc.ValueRequest{Path: "dev/temperature"}); err != nil {
		t.Fatalf("a whole-command deadline cut the read: %v", err)
	}
}

func TestCancelIsNotAbsent(t *testing.T) {
	f := newFake(t)
	p := connected(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.GetSlotInfo(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("%v", err)
	}
}

func TestOneRequestAtATime(t *testing.T) {
	f := newFake(t)
	p := connected(t, f)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = p.GetValue(context.Background(), dhsc.ValueRequest{Path: "dev/temperature"})
		}()
	}
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = p.Walk(context.Background(), 0) }()
	go func() { defer wg.Done(); _, _ = p.Walk(context.Background(), 1) }()
	wg.Wait()
	if f.maxFlight != 1 {
		t.Fatalf("%d requests in flight at once", f.maxFlight)
	}
}

func TestSetPatchesAndReadsBack(t *testing.T) {
	f := newFake(t)
	p := connected(t, f)
	ctx := context.Background()
	tests := []struct {
		name  string
		slot  int
		path  string
		val   dhsc.Value
		patch string
		check func(dhsc.Value) bool
	}{
		{"string with a space", 0, "network/routev4_static_coll/0.0.0.0:0:10/label", dhsc.Value{Str: "lab network"},
			"PATCH /api/data/network/routev4_static_coll/0.0.0.0:0:10?label=lab%20network",
			func(v dhsc.Value) bool { return v.Str == "lab network" }},
		{"bool", 1, "elements/ipoutputts_coll/500000/dst_coll/0/enable", dhsc.Value{Str: "false"},
			"PATCH /slot/1/api/data/elements/ipoutputts_coll/500000/dst_coll/0?enable=false",
			func(v dhsc.Value) bool { return v.Kind == dhsc.KindBool && !v.Bool }},
		{"int", 1, "elements/ipoutputts_coll/500000/dst_coll/0/ip/dstudpp", dhsc.Value{Kind: dhsc.KindUint, Uint: 5004},
			"PATCH /slot/1/api/data/elements/ipoutputts_coll/500000/dst_coll/0/ip?dstudpp=5004",
			func(v dhsc.Value) bool { return v.Uint == 5004 }},
		{"enum label", 0, "network/snmp/trap_type", dhsc.Value{Str: "TRAP"},
			"PATCH /api/data/network/snmp?trap_type=1",
			func(v dhsc.Value) bool { return v.Kind == dhsc.KindEnum && v.Str == "TRAP" }},
		{"float", 1, "elements/ipoutputts_coll/500000/dst_coll/0/gain", dhsc.Value{Kind: dhsc.KindFloat, Float: -1.5},
			"PATCH /slot/1/api/data/elements/ipoutputts_coll/500000/dst_coll/0?gain=-1.5",
			func(v dhsc.Value) bool { return v.Float == -1.5 }},
		{"signed", 0, "network/snmp/port_id", dhsc.Value{Kind: dhsc.KindInt, Int: -1},
			"PATCH /api/data/network/snmp?port_id=-1",
			func(v dhsc.Value) bool { return v.Int == -1 }},
		{"typed bool", 0, "network/ipinterface_coll/1/enable", dhsc.Value{Kind: dhsc.KindBool, Bool: true},
			"PATCH /api/data/network/ipinterface_coll/1?enable=true",
			func(v dhsc.Value) bool { return v.Bool }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := p.SetValue(ctx, dhsc.ValueRequest{Slot: tt.slot, Path: tt.path}, tt.val)
			if err != nil {
				t.Fatal(err)
			}
			if !tt.check(got) {
				t.Errorf("read back %+v", got)
			}
			reqs := f.requests()
			if reqs[len(reqs)-2] != tt.patch {
				t.Errorf("sent %s", reqs[len(reqs)-2])
			}
			if !strings.HasPrefix(reqs[len(reqs)-1], "GET ") {
				t.Errorf("no read-back: %s", reqs[len(reqs)-1])
			}
		})
	}
}

func TestSetRefusedWithNothingSent(t *testing.T) {
	f := newFake(t)
	p := connected(t, f)
	ctx := context.Background()
	tests := []struct {
		name string
		path string
		val  string
		want error
	}{
		{"read-only", "dev/product_info/uuid", "x", nil},
		{"rpc", "dev/system/do_reset", "1", nil},
		{"inside an rpc", "dev/system/do_reset/delay", "1000", nil},
		{"object", "dev/product_info", "x", nil},
		{"key", "network/routev4_static_coll/0.0.0.0:0:10/dest", "192.0.2.9", nil},
		{"unknown", "dev/nope", "x", dhsc.ErrObjectNotFound},
		{"low", "network/ipinterface_coll/0/mtu", "100", dhsc.ErrOutOfRangeLow},
		{"high", "network/ipinterface_coll/0/mtu", "9001", dhsc.ErrOutOfRangeHigh},
		{"enum", "network/snmp/trap_type", "NOPE", dhsc.ErrInvalidEnumLabel},
		{"type", "network/ipinterface_coll/0/mtu", "abc", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := p.SetValue(ctx, dhsc.ValueRequest{Path: tt.path}, dhsc.Value{Str: tt.val})
			if err == nil {
				t.Fatal("accepted")
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("%v", err)
			}
			var ve *dhsc.ValidationError
			if tt.want == nil && !errors.As(err, &ve) {
				t.Fatalf("%v", err)
			}
		})
	}
	if _, err := p.SetValue(ctx, dhsc.ValueRequest{Path: "dev/product_info/devname"}, dhsc.Value{Kind: dhsc.KindRaw, Raw: []byte{1}}); err == nil {
		t.Error("--raw accepted")
	}
	if f.count("PATCH") != 0 {
		t.Fatalf("%v", f.requests())
	}
}

func TestSetFailures(t *testing.T) {
	t.Run("device refuses", func(t *testing.T) {
		f := newFake(t)
		p := connected(t, f)
		if _, err := p.Walk(context.Background(), 0); err != nil {
			t.Fatal(err)
		}
		f.mu.Lock()
		f.info = `{"status":"failed","errors":["level too low"]}`
		f.mu.Unlock()
		_, err := p.SetValue(context.Background(), dhsc.ValueRequest{Path: "dev/product_info/devname"}, dhsc.Value{Str: "x"})
		if !errors.Is(err, dhsc.ErrWriteRejected) || !strings.Contains(err.Error(), "level too low") {
			t.Fatalf("%v", err)
		}
	})
	t.Run("status ok but the value did not move", func(t *testing.T) {
		f := newFake(t)
		f.ignoreWrites = true
		p := connected(t, f)
		got, err := p.SetValue(context.Background(), dhsc.ValueRequest{Path: "dev/product_info/devname"}, dhsc.Value{Str: "x"})
		if !errors.Is(err, dhsc.ErrWriteCoerced) {
			t.Fatalf("%v", err)
		}
		if got.Str != "TEST-MI-01" {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("read-back fails", func(t *testing.T) {
		f := newFake(t)
		f.plainBody["/api/data/dev/product_info/devname"] = `{"data":{},"info":{"status":"ok"}}`
		p := connected(t, f)
		_, err := p.SetValue(context.Background(), dhsc.ValueRequest{Path: "dev/product_info/devname"}, dhsc.Value{Str: "x"})
		if err == nil || !strings.Contains(err.Error(), "read-back failed") {
			t.Fatalf("%v", err)
		}
	})
	t.Run("absent card", func(t *testing.T) {
		f := newFake(t)
		p := connected(t, f)
		_, err := p.SetValue(context.Background(), dhsc.ValueRequest{Slot: 3, Path: "a/b"}, dhsc.Value{Str: "x"})
		if !errors.Is(err, ErrSlotAbsent) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("unresolved schema", func(t *testing.T) {
		f := newFake(t)
		f.schemas[0] = []byte(`{"properties":{"a":{"$ref":"#/definitions/gone"}}}`)
		p := connected(t, f)
		_, err := p.SetValue(context.Background(), dhsc.ValueRequest{Path: "a"}, dhsc.Value{Str: "x"})
		if !errors.Is(err, codec.ErrUnresolvedRef) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("bad path", func(t *testing.T) {
		f := newFake(t)
		p := connected(t, f)
		if _, err := p.SetValue(context.Background(), dhsc.ValueRequest{}, dhsc.Value{Str: "x"}); err == nil {
			t.Fatal("no path accepted")
		}
	})
}

func TestSetForgetsTheWalkedTree(t *testing.T) {
	f := newFake(t)
	p := connected(t, f)
	ctx := context.Background()
	if _, err := p.Walk(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := p.SetValue(ctx, dhsc.ValueRequest{Path: "dev/product_info/devname"}, dhsc.Value{Str: "OTHER"}); err != nil {
		t.Fatal(err)
	}
	objs, err := p.Walk(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range objs {
		if o.Label == "devname" && o.Value.Str != "OTHER" {
			t.Fatalf("stale tree: %+v", o)
		}
	}
}

func TestWatchReportsAChange(t *testing.T) {
	f := newFake(t)
	p := connected(t, f)
	p.SetPollInterval(10 * time.Millisecond)
	events := make(chan dhsc.Event, 16)
	req := dhsc.ValueRequest{Slot: -1, Path: "alarms/count"}
	if err := p.Subscribe(req, func(e dhsc.Event) { events <- e }); err != nil {
		t.Fatal(err)
	}
	first := <-events
	if first.Path != "alarms/count" || first.Value.Uint != 3 || first.Slot != 0 {
		t.Fatalf("%+v", first)
	}
	f.mu.Lock()
	f.docs[0]["alarms"].(map[string]any)["count"] = jsonNumber("4")
	f.mu.Unlock()
	select {
	case e := <-events:
		if e.Value.Uint != 4 {
			t.Fatalf("%+v", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no change event")
	}
	if err := p.Unsubscribe(req); err != nil {
		t.Fatal(err)
	}
}

func TestPollProfile(t *testing.T) {
	f := newFake(t)
	p := connected(t, f)
	ctx := context.Background()
	prof, err := p.pollProfileFor(ctx, dhsc.ValueRequest{Slot: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range prof.Entries {
		if e.Slot != 1 || strings.Contains(e.Path, "clear_stats") {
			t.Fatalf("%+v", e)
		}
	}
	all := len(prof.Entries)

	prof, err = p.pollProfileFor(ctx, dhsc.ValueRequest{Slot: 1, Path: "elements/sdiinput_coll,dev.product_info.swver"})
	if err != nil {
		t.Fatal(err)
	}
	if len(prof.Entries) >= all || len(prof.Entries) < 2 {
		t.Fatalf("narrowed to %d of %d", len(prof.Entries), all)
	}

	p.gap = time.Second
	p.SetPollInterval(time.Millisecond)
	prof, err = p.pollProfileFor(ctx, dhsc.ValueRequest{Slot: 1})
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Duration(all) * time.Second; time.Duration(prof.Defaults.Interval) != want {
		t.Fatalf("interval %v, want %v: the gap must set the floor", time.Duration(prof.Defaults.Interval), want)
	}

	if _, err := p.pollProfileFor(ctx, dhsc.ValueRequest{Slot: 1, Path: "nope"}); !errors.Is(err, dhsc.ErrObjectNotFound) {
		t.Fatalf("%v", err)
	}
	if _, err := p.pollProfileFor(ctx, dhsc.ValueRequest{Slot: 3}); !errors.Is(err, ErrSlotAbsent) {
		t.Fatalf("%v", err)
	}
}

func TestPacedReadHonoursCancel(t *testing.T) {
	f := newFake(t)
	p := connected(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.gap = time.Hour
	if _, err := (paced{p}).GetValue(ctx, dhsc.ValueRequest{Path: "dev/temperature"}); err == nil {
		t.Fatal("no error")
	}
}

func TestDisconnectStopsWatches(t *testing.T) {
	f := newFake(t)
	p := connected(t, f)
	if err := p.Subscribe(dhsc.ValueRequest{Path: "alarms/count"}, func(dhsc.Event) {}); err != nil {
		t.Fatal(err)
	}
	if err := p.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if p.poller.Active() != 0 {
		t.Fatal("a watch outlived its session")
	}
}

func TestWalkAfterDisconnectRace(t *testing.T) {
	// The session can end while a slow walk is in the air; nothing panics
	// and the result is not stored on a closed plugin.
	f := newFake(t)
	p := connected(t, f)
	r, _ := p.session()
	if _, err := p.schemaFor(context.Background(), r, 0); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	p.schemas, p.trees = nil, nil
	p.mu.Unlock()
	if _, err := p.schemaFor(context.Background(), r, 1); err != nil {
		t.Fatal(err)
	}
	if p.loadedSchema(1) != nil {
		t.Fatal("stored on a closed session")
	}
}

func TestCaptureHoldsNoCredentials(t *testing.T) {
	f := newFake(t)
	path := filepath.Join(t.TempDir(), "raw.jsonl")
	rec, err := transport.NewRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	p := built(t, Options{User: testUser, Pass: testPass})
	p.SetRecorder(rec)
	host, port := f.host()
	if err := p.Connect(context.Background(), host, port); err != nil {
		t.Fatal(err)
	}
	f.expireSessions() // forces the re-login path through the recorder too
	if _, err := p.GetValue(context.Background(), dhsc.ValueRequest{Path: "dev/temperature"}); err != nil {
		t.Fatal(err)
	}
	if err := p.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("nothing captured")
	}
	if bytes.Contains(raw, []byte(testPass)) || bytes.Contains(raw, []byte("Authorization")) {
		t.Fatal("credentials in the capture")
	}
	if bytes.Contains(raw, []byte("/api/login")) {
		t.Fatal("the login exchange was captured")
	}
}

func TestSnippetCuts(t *testing.T) {
	if got := snippet(bytes.Repeat([]byte("a"), 200)); len(got) > 170 {
		t.Fatalf("%d bytes", len(got))
	}
}

func TestHTTPErrorText(t *testing.T) {
	e := &HTTPError{Method: "GET", Target: "/api/data", Status: 500, Text: "boom"}
	if got := e.Error(); !strings.Contains(got, "HTTP 500 boom") || strings.Contains(got, EnvTLS) {
		t.Fatalf("%q", got)
	}
}

func TestNewObjectNumberEdges(t *testing.T) {
	if got := number(nil); got != nil {
		t.Errorf("%v", got)
	}
	big := jsonNumber("1.5")
	if got := number(&big); got != 1.5 {
		t.Errorf("%v", got)
	}
	bad := jsonNumber("x")
	if got := number(&bad); got != nil {
		t.Errorf("%v", got)
	}
	if defaultOf([]byte(`{`)) != nil || defaultOf(nil) != nil {
		t.Error("default")
	}
}

func TestValueTextDefaults(t *testing.T) {
	if s, err := valueText(dhsc.Value{Kind: dhsc.KindEnum, Str: "A"}); err != nil || s != "A" {
		t.Fatalf("%q %v", s, err)
	}
}
