package hyperdeck

import "testing"

func TestParseCommand(t *testing.T) {
	name, params := parseCommand("remote: enable: true override: false")
	if name != "remote" {
		t.Fatalf("name = %q", name)
	}
	if params["enable"] != "true" || params["override"] != "false" {
		t.Fatalf("params = %#v", params)
	}
}

func TestParseCommandMultiWordKey(t *testing.T) {
	name, params := parseCommand("notify: device info: true")
	if name != "notify" {
		t.Fatalf("name = %q", name)
	}
	if params["device info"] != "true" {
		t.Fatalf("params = %#v", params)
	}
}
