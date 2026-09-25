package mnset

import (
	"context"
	"strings"
	"testing"

	"dhs/internal/consumer"
)

// Labels are read off the module's own names; the path (the address a
// PUT needs) never changes.
func TestWalkLabelsLeavesByChannel(t *testing.T) {
	m := newModule(t)
	p := connected(t, m)
	objs, err := p.Walk(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]consumer.Object{}
	for _, o := range objs {
		byPath[strings.Join(o.Path, ".")] = o
	}
	want := map[string]string{
		"flows.fee338d3.network.1.dst_ip_addr":   "CH8 · rx ch8 flow 0 pri · network.1.dst_ip_addr",
		"flows.fee338d3-sec.network.dst_ip_addr": "CH8 · rx ch8 flow 0 sec · network.dst_ip_addr",
		"flows.orphan.network.dst_ip_addr":       "dst_ip_addr", // no name, no owner: plain leaf label kept
		"receivers.0.format":                     "CH8 · receiver 0 video · format",
		"sdi_output.out1.color_bar":              "CH8 · HDMI 4 · color_bar",
		"clean_switch.dev8.clean_switch.mode":    "CH8 · clean_switch · clean_switch.mode",
		"devices.0.label":                        "CH8 · device · label",
		"sdp.fee338d3":                           "CH8 · sdp",
		"self.ipconfig.hostname":                 "hostname", // system tree untouched
		"refclk.mode":                            "mode",
	}
	for path, label := range want {
		o, ok := byPath[path]
		if !ok {
			t.Errorf("%s: not walked", path)
			continue
		}
		if o.Label != label {
			t.Errorf("%s: label = %q, want %q", path, o.Label, label)
		}
	}
}

func TestLabelObjectsCorners(t *testing.T) {
	objs := []consumer.Object{
		{Path: []string{"flows"}, Label: "flows"}, // too short to name
		{Path: []string{"sdi_output", "x", "label"}, Label: "label", Value: consumer.Value{Kind: consumer.KindString, Str: "  "}},
		{Path: []string{"sdi_input", "y", "line_offset"}, Label: "line_offset"},
		{Path: []string{"clean_switch", "unknown-dev", "clean_switch", "mode"}, Label: "mode"},
	}
	labelObjects(objs)
	if objs[0].Label != "flows" {
		t.Errorf("short path relabelled: %q", objs[0].Label)
	}
	if objs[1].Label != "sdi_output · label" {
		t.Errorf("blank sdi label must fall back to the resource: %q", objs[1].Label)
	}
	if objs[2].Label != "sdi_input · line_offset" {
		t.Errorf("sdi_input: %q", objs[2].Label)
	}
	if objs[3].Label != "clean_switch · clean_switch.mode" {
		t.Errorf("clean_switch without a device: %q", objs[3].Label)
	}
	if shortChannel(" Device CH2 ") != "CH2" || shortChannel("Encoder") != "Encoder" {
		t.Error("shortChannel")
	}
}

func TestCagesAbsentWhenPortIsNotListedOrNotAListing(t *testing.T) {
	m := newModule(t)
	delete(m.docs, "port")
	p := connected(t, m)
	si, _ := p.GetSlotInfo(context.Background(), 0)
	if _, any := si.Identity["cage1"]; any {
		t.Errorf("port 404 must give no cages: %v", si.Identity)
	}
	m2 := newModule(t)
	m2.docs["port"] = `{"not":"a listing"}`
	p2 := connected(t, m2)
	si2, _ := p2.GetSlotInfo(context.Background(), 0)
	if _, any := si2.Identity["cage1"]; any {
		t.Errorf("port as a document must give no cages: %v", si2.Identity)
	}
}
