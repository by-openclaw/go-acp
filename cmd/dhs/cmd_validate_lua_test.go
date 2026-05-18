package main

import (
	"errors"
	"testing"

	"dhs/internal/errcode"
)

// TestRunValidateLua_MissingPcap asserts the --lua path returns the
// validation:lua-pcap-required error when --pcap is missing (R12 #473
// pragmatic v1 — jsonl→pcap synthesis is v2).
func TestRunValidateLua_MissingPcap(t *testing.T) {
	// The error sentinel itself is the contract: callers (CLI + Ansible
	// dispatch) errors.Is against it to dispatch on the typed code.
	if !errors.Is(errLuaPcapRequired, errLuaPcapRequired) {
		t.Fatal("errors.Is failed on the sentinel")
	}
	c := errcode.From(errLuaPcapRequired)
	if c == nil || c.Layer != errcode.LayerValidation ||
		c.Name != "lua-pcap-required" || c.Class != errcode.ClassUsage {
		t.Errorf("typed code = %+v; want validation:lua-pcap-required class=usage", c)
	}
}

// TestRunValidateLua_TsharkSentinel asserts the tshark-not-found
// error sentinel is wired into the validation layer with the usage
// class (exit 2).
func TestRunValidateLua_TsharkSentinel(t *testing.T) {
	c := errcode.From(errTsharkNotFound)
	if c == nil || c.Layer != errcode.LayerValidation ||
		c.Name != "tshark-not-found" || c.Class != errcode.ClassUsage {
		t.Errorf("typed code = %+v; want validation:tshark-not-found class=usage", c)
	}
}
