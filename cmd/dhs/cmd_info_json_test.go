package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// The machine shape of `info` is what a play reads, so its fields are a
// contract rather than an implementation detail.
func TestInfoJSONCarriesTheSlotIdentity(t *testing.T) {
	// A slot number alone names a different node on a different topology, so
	// the address a node gives for itself is the only thing two readings of
	// one plant can be compared by. This shape used to drop it: the domain
	// type carried it, the runbooks told operators to read it, an Ansible
	// contract asserted on it, and it was never marshalled.
	out := deviceInfoJSON{
		Device: "10.6.250.105:2050",
		Slots:  1,
		SlotStatus: []slotInfoJSON{{
			Slot:   0,
			Status: "present",
			Online: true,
			Identity: map[string]string{
				"address": "0000-81-00:0FF",
				"name":    "XY Panel",
			},
		}},
	}

	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"identity"`) {
		t.Fatalf("the identity did not reach the JSON: %s", b)
	}

	var back deviceInfoJSON
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := back.SlotStatus[0].Identity["address"]; got != "0000-81-00:0FF" {
		t.Errorf("address round-tripped as %q", got)
	}
}

func TestInfoJSONOmitsAnIdentityNobodyKnows(t *testing.T) {
	// A plugin that does not know what a slot is says nothing rather than
	// an empty object: `identity: {}` reads as "it has none", which is a
	// different claim from "this protocol cannot tell you".
	b, err := json.Marshal(slotInfoJSON{Slot: 3, Online: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "identity") {
		t.Errorf("an unknown identity was emitted anyway: %s", b)
	}
}
