package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"dhs/internal/rrcs/codec"
	rrcs "dhs/internal/rrcs/consumer"
)

// rrcsDesiredCard is the settings of one client card.
//
//	"cards": [{"card": "net.1.node.93.card.3", "name": "MADI CONVERT",
//	           "madi": {"frame": 64, "channels": "01-08", "up": "optical", "down": "optical"}}]
//
// A MADI card (§8.10.4.34) carries one block of eight channels of the
// stream. RRCS 9.0.RR1 reads the block and the interfaces counting from 1
// and writes them counting from 0 (oracle, 2026-11-07: Director "01 - 08,
// Optical, Optical" reads Channels 1, UpInterface 1, DownInterface 1; a
// write of Channels 2 read back 3 and moved the card to 17 - 24). The
// frame length reads as the count (56 or 64) and writes 0 = 56, 1 = 64.
// The audio comes in on the Up port (oracle: nothing on Down).
type rrcsDesiredCard struct {
	Card string           `json:"card"` // net.N.node.N.card.BAY, as list cards prints it
	Name *string          `json:"name,omitempty"`
	Madi *rrcsDesiredMadi `json:"madi,omitempty"`
}

type rrcsDesiredMadi struct {
	Frame    int    `json:"frame,omitempty"`    // 56 or 64
	Channels string `json:"channels,omitempty"` // 01-08, 09-16 … 57-64 (57-64 needs frame 64)
	Up       string `json:"up,omitempty"`       // optical | electrical
	Down     string `json:"down,omitempty"`     // optical | electrical
}

// rrcsMadiBlocks are the blocks a MADI card takes, in write order.
var rrcsMadiBlocks = []string{"01-08", "09-16", "17-24", "25-32", "33-40", "41-48", "49-56", "57-64"}

var rrcsMadiInterfaces = map[string]int{"optical": 0, "electrical": 1}

// rrcsDesiredPanel is the settings of one panel: any property the panel
// reports through GetObjectProperty, by its name, sent back as a portex
// edit (§8.10.4.6).
//
//	"panels": [{"panel": "net.1.node.93.port.8", "settings": {"MinSpeakerVol": 0, "SpeakerDimLevel": 5}}]
type rrcsDesiredPanel struct {
	Panel    string         `json:"panel"`
	Settings map[string]any `json:"settings"`
}

// rrcsCardOfPath reads net.N.node.N.card.BAY.
func rrcsCardOfPath(path string) (node, bay int, ok bool) {
	parts := strings.Split(path, ".")
	if len(parts) != 6 || parts[0] != "net" || parts[2] != "node" || parts[4] != "card" {
		return 0, 0, false
	}
	n, e1 := strconv.Atoi(parts[3])
	b, e2 := strconv.Atoi(parts[5])
	return n, b, e1 == nil && e2 == nil
}

// rrcsPlanCard compares one card with what RRCS reports for it and
// returns the members of the edit, with the diff.
func rrcsPlanCard(have map[string]any, w rrcsDesiredCard) (edit []codec.Member, madi []codec.Member, from, to []string, problems []string) {
	if w.Name != nil && jStr(have, "LongName") != *w.Name {
		edit = append(edit, codec.Member{Name: "LongName", Value: codec.String(*w.Name)})
		from, to = append(from, "name "+jStr(have, "LongName")), append(to, "name "+*w.Name)
	}
	if w.Madi == nil {
		return
	}
	if jStr(have, "ClientCardTypeString") != "MADI" {
		problems = append(problems, "madi settings on a "+jStr(have, "ClientCardTypeString")+" card")
		return
	}
	d := w.Madi
	if d.Frame != 0 {
		if d.Frame != 56 && d.Frame != 64 {
			problems = append(problems, "frame: want 56 or 64")
		} else if jInt(have, "FrameLength") != d.Frame {
			madi = append(madi, codec.Member{Name: "FrameLength", Value: codec.Int(int32(map[int]int{56: 0, 64: 1}[d.Frame]))})
			from, to = append(from, "frame "+strconv.Itoa(jInt(have, "FrameLength"))), append(to, "frame "+strconv.Itoa(d.Frame))
		}
	}
	if d.Channels != "" {
		idx := -1
		for i, b := range rrcsMadiBlocks {
			if b == strings.ReplaceAll(d.Channels, " ", "") {
				idx = i
			}
		}
		switch {
		case idx < 0:
			problems = append(problems, "channels: want 01-08, 09-16 … 57-64")
		case idx == 7 && d.Frame != 64 && jInt(have, "FrameLength") != 64:
			problems = append(problems, "channels 57-64 need frame 64")
		case jInt(have, "Channels") != idx+1:
			madi = append(madi, codec.Member{Name: "Channels", Value: codec.Int(int32(idx))})
			now := "?"
			if c := jInt(have, "Channels"); c >= 1 && c <= len(rrcsMadiBlocks) {
				now = rrcsMadiBlocks[c-1]
			}
			from, to = append(from, "channels "+now), append(to, "channels "+rrcsMadiBlocks[idx])
		}
	}
	for _, side := range []struct{ member, want string }{{"UpInterface", d.Up}, {"DownInterface", d.Down}} {
		if side.want == "" {
			continue
		}
		v, known := rrcsMadiInterfaces[strings.ToLower(side.want)]
		if !known {
			problems = append(problems, strings.ToLower(side.member[:len(side.member)-9])+": want optical or electrical")
			continue
		}
		if jInt(have, side.member) != v+1 {
			madi = append(madi, codec.Member{Name: side.member, Value: codec.Int(int32(v))})
			from, to = append(from, fmt.Sprintf("%s %d", side.member, jInt(have, side.member)-1)), append(to, side.member+" "+side.want)
		}
	}
	return
}

// rrcsClientCards reads GetAllClientCards as maps.
func rrcsClientCards(ctx context.Context, client *rrcs.Client) ([]map[string]any, error) {
	reply, err := client.Call(ctx, "GetAllClientCards")
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case []any:
			for _, e := range t {
				walk(e)
			}
		case map[string]any:
			if _, isCard := t["Bay"]; isCard {
				out = append(out, t)
				return
			}
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(rrcsPlain(reply.Payload()))
	return out, nil
}

// rrcsEnsureCards brings each card to its settings: read, one edit per
// card, read again; what still differs was accepted and not taken.
func rrcsEnsureCards(ctx context.Context, cf *rrcsFlags, client *rrcs.Client, cards []rrcsDesiredCard, check bool) (diff []rrcsDiffEntry, failures []rrcsEnsureFailure) {
	if len(cards) == 0 {
		return nil, nil
	}
	find := func(all []map[string]any, node, bay int) map[string]any {
		for _, c := range all {
			if jInt(c, "Node") == node && jInt(c, "Bay") == bay {
				return c
			}
		}
		return nil
	}
	all, err := rrcsClientCards(ctx, client)
	if err != nil {
		return nil, []rrcsEnsureFailure{{Field: "cards", Reason: err.Error()}}
	}
	sent := false
	for _, w := range cards {
		node, bay, ok := rrcsCardOfPath(w.Card)
		have := find(all, node, bay)
		if !ok || have == nil {
			failures = append(failures, rrcsEnsureFailure{Field: w.Card, Reason: "no client card there"})
			continue
		}
		edit, madi, from, to, problems := rrcsPlanCard(have, w)
		for _, p := range problems {
			failures = append(failures, rrcsEnsureFailure{Field: w.Card, Reason: p})
		}
		if len(edit)+len(madi) == 0 {
			continue
		}
		diff = append(diff, rrcsDiffEntry{Field: w.Card, From: strings.Join(from, ", "), To: strings.Join(to, ", ")})
		params := append([]codec.Member{
			{Name: "Node", Value: codec.Int(int32(node))},
			{Name: "Slot", Value: codec.Int(int32(bay))}}, edit...)
		if len(madi) > 0 {
			params = append(params, codec.Member{Name: "Madi", Value: codec.Struct(madi...)})
		}
		step := rrcsCfgStep{field: w.Card, changeType: "edit", objectType: "client-card", params: params}
		if check {
			cf.say("rrcs ensure: would send %s %s", rrcsChangeMethod, rrcsCompact(step.request()))
			continue
		}
		cf.say("rrcs ensure: %s edit client-card %s", rrcsChangeMethod, w.Card)
		if _, err := client.Call(ctx, rrcsChangeMethod, step.request()); err != nil {
			failures = append(failures, rrcsEnsureFailure{Field: w.Card, Reason: err.Error()})
			continue
		}
		sent = true
	}
	if !sent {
		return diff, failures
	}
	again, err := rrcsClientCards(ctx, client)
	if err != nil {
		return diff, append(failures, rrcsEnsureFailure{Field: "cards", Reason: err.Error()})
	}
	for _, w := range cards {
		node, bay, _ := rrcsCardOfPath(w.Card)
		if have := find(again, node, bay); have != nil {
			if e, m, _, _, _ := rrcsPlanCard(have, w); len(e)+len(m) > 0 {
				failures = append(failures, rrcsEnsureFailure{Field: w.Card, Reason: "not_taken: RRCS accepted the edit and the card still differs"})
			}
		}
	}
	return diff, failures
}

// rrcsPanelProps reads one port's properties with GetPort: the answer
// that carries a panel's own settings (MinSpeakerVol, SpeakerDimLevel…);
// GetObjectProperty does not.
func rrcsPanelProps(ctx context.Context, client *rrcs.Client, net, node, port int) (map[string]any, error) {
	reply, err := client.Call(ctx, "GetPort", codec.Int(int32(net)), codec.Int(int32(node)), codec.Int(int32(port)),
		codec.Bool(false), codec.Int(-1))
	if err != nil {
		return nil, err
	}
	v := rrcsPlain(reply.Payload())
	for {
		list, isList := v.([]any)
		if !isList || len(list) == 0 {
			break
		}
		v = list[len(list)-1]
	}
	top, _ := v.(map[string]any)
	// The answer groups some properties in blocks (PortMadi, PortAes67…);
	// a panel's own settings sit at the top or in a block, and the portex
	// edit takes them flat (oracle: MinSpeakerVol sent flat was taken).
	props := map[string]any{}
	for k, val := range top {
		if block, isBlock := val.(map[string]any); isBlock {
			for bk, bv := range block {
				if _, set := props[bk]; !set {
					props[bk] = bv
				}
			}
			continue
		}
		props[k] = val
	}
	return props, nil
}

// rrcsPanelValue is a wanted setting as the wire carries it.
func rrcsPanelValue(v any) (codec.Value, string, bool) {
	switch t := v.(type) {
	case bool:
		return codec.Bool(t), strconv.FormatBool(t), true
	case float64:
		if t == float64(int32(t)) {
			return codec.Int(int32(t)), strconv.Itoa(int(t)), true
		}
	case string:
		return codec.String(t), t, true
	}
	return codec.Value{}, "", false
}

// rrcsEnsurePanels brings the settings of panels to the file.
func rrcsEnsurePanels(ctx context.Context, cf *rrcsFlags, client *rrcs.Client, panels []rrcsDesiredPanel, check bool) (diff []rrcsDiffEntry, failures []rrcsEnsureFailure) {
	if len(panels) == 0 {
		return nil, nil
	}
	snap, _, err := rrcsCollect(ctx, client, rrcsCollectOpts{Only: []string{"GetAllPorts"}})
	if err != nil {
		return nil, []rrcsEnsureFailure{{Field: "panels", Reason: err.Error()}}
	}
	m, err := rrcsModelOf(snap)
	if err != nil {
		return nil, []rrcsEnsureFailure{{Field: "panels", Reason: err.Error()}}
	}
	for _, w := range panels {
		node, port, _, ok := rrcsPortOfPath(w.Panel)
		p := m.Find(node, port, false)
		if !ok || p == nil {
			failures = append(failures, rrcsEnsureFailure{Field: w.Panel, Reason: "no port there"})
			continue
		}
		plan := func() (edit []codec.Member, from, to []string, bad []string, err error) {
			have, err := rrcsPanelProps(ctx, client, p.Net, node, port)
			if err != nil {
				return nil, nil, nil, nil, err
			}
			names := make([]string, 0, len(w.Settings))
			for n := range w.Settings {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				now, known := have[n]
				if !known {
					bad = append(bad, n+": not a property this port reports")
					continue
				}
				val, text, ok := rrcsPanelValue(w.Settings[n])
				if !ok {
					bad = append(bad, n+": want a whole number, true/false or text")
					continue
				}
				if fmt.Sprint(now) == text {
					continue
				}
				edit = append(edit, codec.Member{Name: n, Value: val})
				from, to = append(from, fmt.Sprintf("%s %v", n, now)), append(to, n+" "+text)
			}
			return
		}
		edit, from, to, bad, err := plan()
		if err != nil {
			failures = append(failures, rrcsEnsureFailure{Field: w.Panel, Reason: err.Error()})
			continue
		}
		for _, b := range bad {
			failures = append(failures, rrcsEnsureFailure{Field: w.Panel, Reason: b})
		}
		if len(edit) == 0 {
			continue
		}
		diff = append(diff, rrcsDiffEntry{Field: w.Panel, From: strings.Join(from, ", "), To: strings.Join(to, ", ")})
		params := append([]codec.Member{{Name: "PortAddress", Value: codec.Struct(
			codec.Member{Name: "IsInput", Value: codec.Bool(false)},
			codec.Member{Name: "Node", Value: codec.Int(int32(node))},
			codec.Member{Name: "Port", Value: codec.Int(int32(port))})}}, edit...)
		step := rrcsCfgStep{field: w.Panel, changeType: "edit", objectType: "portex", params: params}
		if check {
			cf.say("rrcs ensure: would send %s %s", rrcsChangeMethod, rrcsCompact(step.request()))
			continue
		}
		cf.say("rrcs ensure: %s edit portex %s", rrcsChangeMethod, w.Panel)
		if _, err := client.Call(ctx, rrcsChangeMethod, step.request()); err != nil {
			failures = append(failures, rrcsEnsureFailure{Field: w.Panel, Reason: err.Error()})
			continue
		}
		if again, _, _, _, err := plan(); err == nil && len(again) > 0 {
			failures = append(failures, rrcsEnsureFailure{Field: w.Panel, Reason: "not_taken: RRCS accepted the edit and the panel still differs"})
		}
	}
	return diff, failures
}

// rrcsPlain is a reply as encoding/json gives it back: numbers as
// float64, the form jInt and jStr read.
func rrcsPlain(v codec.Value) any {
	raw, err := json.Marshal(rrcs.JSON(v))
	if err != nil {
		return nil
	}
	var out any
	_ = json.Unmarshal(raw, &out)
	return out
}
