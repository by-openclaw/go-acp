package monitor

import (
	"testing"
	"time"
)

func TestLoadValidProfile(t *testing.T) {
	data := []byte(`{
	  "model": "RX1290",
	  "defaults": {"interval": "30s", "on_change": true},
	  "oids": [
	    {"oid": "1.3.6.1.4.1.1773.1.1.10", "interval": "1s"},
	    {"oid": "1.3.6.1.4.1.1773.1.3.200.1.11", "interval": "10s", "on_change": false}
	  ]
	}`)
	p, err := Load(data)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.Model != "RX1290" {
		t.Errorf("model = %q, want RX1290", p.Model)
	}
	if got := p.effInterval(p.Entries[0]); got != time.Second {
		t.Errorf("entry0 interval = %v, want 1s", got)
	}
	if got := p.effInterval(p.Entries[1]); got != 10*time.Second {
		t.Errorf("entry1 interval = %v, want 10s", got)
	}
	if !p.effOnChange(p.Entries[0]) {
		t.Errorf("entry0 on_change should inherit default true")
	}
	if p.effOnChange(p.Entries[1]) {
		t.Errorf("entry1 on_change should be its own false")
	}
	if ivs := p.Intervals(); len(ivs) != 2 || ivs[0] != time.Second || ivs[1] != 10*time.Second {
		t.Errorf("Intervals = %v, want [1s 10s]", ivs)
	}
}

func TestLoadRejectsDuplicateAddress(t *testing.T) {
	data := []byte(`{
	  "defaults": {"interval": "1s"},
	  "oids": [{"oid": "1.1.1"}, {"oid": "1.1.1"}]
	}`)
	if _, err := Load(data); err == nil {
		t.Fatal("expected duplicate-address error, got nil")
	}
}

func TestLoadRejectsNoInterval(t *testing.T) {
	data := []byte(`{"oids": [{"oid": "1.1.1"}]}`)
	if _, err := Load(data); err == nil {
		t.Fatal("expected no-interval error, got nil")
	}
}

func TestLoadRejectsMissingAddress(t *testing.T) {
	data := []byte(`{"defaults": {"interval": "1s"}, "oids": [{"interval": "2s"}]}`)
	if _, err := Load(data); err == nil {
		t.Fatal("expected missing-address error, got nil")
	}
}

func TestLoadRejectsBadDuration(t *testing.T) {
	data := []byte(`{"oids": [{"oid": "1.1.1", "interval": "banana"}]}`)
	if _, err := Load(data); err == nil {
		t.Fatal("expected bad-duration error, got nil")
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	data := []byte(`{"defaults": {"interval": "1s"}, "oids": [{"oid": "1.1.1"}], "bogus": 1}`)
	if _, err := Load(data); err == nil {
		t.Fatal("expected unknown-field error, got nil")
	}
}

func TestLoadRejectsEmpty(t *testing.T) {
	if _, err := Load([]byte(`{"defaults": {"interval": "1s"}, "oids": []}`)); err == nil {
		t.Fatal("expected empty-profile error, got nil")
	}
}

func TestJitterDeterministicAndBounded(t *testing.T) {
	const iv = time.Second
	a := jitter("some-address", iv)
	b := jitter("some-address", iv)
	if a != b {
		t.Errorf("jitter not deterministic: %v != %v", a, b)
	}
	if a < 0 || a >= iv {
		t.Errorf("jitter %v out of [0, %v)", a, iv)
	}
	if jitter("k", 0) != 0 {
		t.Errorf("jitter with zero interval must be 0")
	}
	// Different addresses should generally land on different offsets.
	if jitter("addr-one", iv) == jitter("addr-two", iv) {
		t.Log("note: two addresses collided on the same jitter offset (allowed but rare)")
	}
}
