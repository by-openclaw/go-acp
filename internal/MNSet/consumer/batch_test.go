package mnset

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"strings"
	"testing"

	"dhs/internal/consumer"
)

// Two fields of one document, one PUT: the module takes a static address
// and its gateway together where it refuses each alone (400 for a
// static_ip outside the static_gateway's subnet, FusioN6, 2026-10-03).
func TestSetValuesWritesOneDocumentOnce(t *testing.T) {
	m := newModule(t)
	m.docs["self"] = `["information/","ipconfig/","syslog/","interfaces/"]`
	m.docs["self/interfaces"] = `{"e1":{"static_ip":"192.168.39.230/24","static_gateway":"192.168.39.1","dhcp":true}}`
	p := connected(t, m)
	ctx := context.Background()

	puts := m.count("PUT self/interfaces")
	got, err := p.SetValues(ctx,
		[]consumer.ValueRequest{{Path: "self.interfaces.e1.static_ip"}, {Path: "self.interfaces.e1.static_gateway"}, {Path: "refclk.delay_req"}},
		[]consumer.Value{{Str: "10.6.40.150/24"}, {Str: "10.6.40.254"}, {Str: "-2"}})
	if err != nil {
		t.Fatal(err)
	}
	if m.count("PUT self/interfaces") != puts+1 {
		t.Errorf("self/interfaces PUT %d time(s) for two fields", m.count("PUT self/interfaces")-puts)
	}
	var doc map[string]map[string]any
	if err := json.Unmarshal([]byte(m.puts["self/interfaces"]), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["e1"]["static_ip"] != "10.6.40.150/24" || doc["e1"]["static_gateway"] != "10.6.40.254" || doc["e1"]["dhcp"] != true {
		t.Errorf("the one PUT carried %v", doc["e1"])
	}
	// Answers are the module's, in the order asked.
	if len(got) != 3 || got[0].Str != "10.6.40.150/24" || got[1].Str != "10.6.40.254" || got[2].Int != -2 {
		t.Errorf("confirmed = %+v", got)
	}

	// Nothing to write is not an error, and lengths must agree.
	if out, err := p.SetValues(ctx, nil, nil); err != nil || out != nil {
		t.Errorf("empty batch: %v, %v", out, err)
	}
	if _, err := p.SetValues(ctx, []consumer.ValueRequest{{Path: "refclk.delay_req"}}, nil); err == nil {
		t.Error("mismatched lengths accepted")
	}
}

// Every refusal a single write has, before anything is sent, for the
// whole batch — plus what the module does after the PUT.
func TestSetValuesRefusesTheWholeBatch(t *testing.T) {
	m := newModule(t)
	p := connected(t, m)
	ctx := context.Background()
	ok := consumer.ValueRequest{Path: "refclk.delay_req"}
	// A text document that may be written is still not a field.
	writable["sdp"] = true
	t.Cleanup(func() { delete(writable, "sdp") })
	cases := []struct {
		path, want string
		val        consumer.Value
	}{
		{"self.information.serial_number", "read-only", consumer.Value{Str: "x"}},
		{"sdp.fee338d3", "text document", consumer.Value{Str: "x"}},
		{"flows.fee338d3.network.9.dst_ip_addr", "not-found", consumer.Value{Str: "x"}},
		{"self.syslog.config.enable", "not 0/1", consumer.Value{Str: "maybe"}},
		{"self.syslog.config", "node takes a JSON object", consumer.Value{Str: "x"}},
		{"nosuch.field", "not listed under /", consumer.Value{Str: "x"}},
	}
	for _, c := range cases {
		puts := m.count("PUT refclk")
		_, err := p.SetValues(ctx, []consumer.ValueRequest{ok, {Path: c.path}}, []consumer.Value{{Str: "1"}, c.val})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("SetValues(…, %q) err = %v, want %q", c.path, err, c.want)
		}
		if m.count("PUT refclk") != puts {
			t.Errorf("%q: the good field was written although the batch was refused", c.path)
		}
	}
	// A slot the frame does not have.
	if _, err := p.SetValues(ctx, []consumer.ValueRequest{{Slot: 9, Path: "refclk.delay_req"}}, []consumer.Value{{Str: "1"}}); err == nil {
		t.Error("an unknown slot was accepted")
	}

	// The module refuses the PUT: its message is surfaced.
	m.fail["refclk"] = 400
	if _, err := p.SetValues(ctx, []consumer.ValueRequest{ok}, []consumer.Value{{Str: "1"}}); err == nil || !strings.Contains(err.Error(), "answered 400") {
		t.Errorf("PUT refusal err = %v", err)
	}
	delete(m.fail, "refclk")

	// PUT accepted, document gone on read-back.
	m.drop["refclk"] = true
	if _, err := p.SetValues(ctx, []consumer.ValueRequest{ok}, []consumer.Value{{Str: "1"}}); err == nil || !strings.Contains(err.Error(), "read-back failed") {
		t.Errorf("read-back err = %v", err)
	}
	delete(m.drop, "refclk")

	// PUT accepted, the field gone from the document that came back.
	writable["shrink"] = true
	t.Cleanup(func() { delete(writable, "shrink") })
	m.docs[""] = `["shrink/","refclk/"]`
	m.docs["shrink"] = `{"a":1}`
	inner := m.ts.Config.Handler
	m.ts.Config.Handler = stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.Method == stdhttp.MethodPut && strings.HasSuffix(r.URL.Path, "/shrink") {
			m.mu.Lock()
			m.docs["shrink"] = `{"b":2}`
			m.mu.Unlock()
			_, _ = w.Write([]byte(`{}`))
			return
		}
		inner.ServeHTTP(w, r)
	})
	if _, err := p.SetValues(ctx, []consumer.ValueRequest{{Path: "shrink.a"}}, []consumer.Value{{Str: "5"}}); err == nil || !strings.Contains(err.Error(), "gone on read-back") {
		t.Errorf("gone on read-back err = %v", err)
	}
}
