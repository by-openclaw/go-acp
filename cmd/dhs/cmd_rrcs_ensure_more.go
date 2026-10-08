package main

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"dhs/internal/rrcs/codec"
	rrcs "dhs/internal/rrcs/consumer"
)

// More of the desired state: the IFBs, and what a port holds outside its
// configuration object — label, alias and the two gains, each with a
// request of its own (§8.3, §8.4, §8.5).
//
//	"ifbs": [{"number": 7, "label": "SPORT", "name": "IFB Sport",
//	          "input": "net.1.node.61.port.1040", "output": "net.1.node.61.port.1041",
//	          "mix_minus": "", "dim_level": 4}],
//	"ports": [{"port": "net.1.node.61.port.1040", "label": "CODIP01A", "alias": "CODEC 1",
//	           "input_gain": "0", "output_gain": "-3.5"}]
//
// An IFB is named by its number; it cannot be created or deleted through
// RRCS (§8.10.1). A port of an IFB is a path, or "" for none.

// rrcsDesiredIFB is one IFB.
type rrcsDesiredIFB struct {
	Number   int     `json:"number"`
	Label    *string `json:"label,omitempty"`
	Name     *string `json:"name,omitempty"`
	Input    *string `json:"input,omitempty"`
	Output   *string `json:"output,omitempty"`
	MixMinus *string `json:"mix_minus,omitempty"`
	DimLevel *int    `json:"dim_level,omitempty"` // 0 = 0 dB … 6 = -24 dB, 7 = mute (§8.10.4.4)
}

// rrcsDesiredPort is the label, alias and gains of one port.
type rrcsDesiredPort struct {
	Port       string  `json:"port"`
	Label      *string `json:"label,omitempty"`
	Alias      *string `json:"alias,omitempty"`
	InputGain  string  `json:"input_gain,omitempty"`  // dB in steps of 0.5, -18 to 18, or "mute"
	OutputGain string  `json:"output_gain,omitempty"` // the same
}

// rrcsPlanIFBs compares the wanted IFBs with the model.
func rrcsPlanIFBs(m *rrcsModel, want *rrcsDesired) (steps []rrcsCfgStep, problems []rrcsEnsureFailure) {
	for _, w := range want.IFBs {
		field := "ifb.number." + strconv.Itoa(w.Number)
		var have *rrcsObject
		for _, o := range m.Objects["ifb"] {
			if jInt(o.Raw, "Number") == w.Number {
				have = o
			}
		}
		if have == nil {
			problems = append(problems, rrcsEnsureFailure{Field: field, Reason: "no IFB with this number; an IFB cannot be created through RRCS"})
			continue
		}
		edit := []codec.Member{}
		var from, to []string
		if w.Label != nil && jStr(have.Raw, "Label") != *w.Label {
			edit = append(edit, codec.Member{Name: "Label", Value: codec.String(*w.Label)})
			from, to = append(from, "label "+jStr(have.Raw, "Label")), append(to, "label "+*w.Label)
		}
		if w.Name != nil && jStr(have.Raw, "LongName") != *w.Name {
			edit = append(edit, codec.Member{Name: "LongName", Value: codec.String(*w.Name)})
			from, to = append(from, "name "+jStr(have.Raw, "LongName")), append(to, "name "+*w.Name)
		}
		bad := false
		for _, leg := range []struct {
			member string
			wanted *string
		}{{"Input", w.Input}, {"Output", w.Output}, {"MixMinus", w.MixMinus}} {
			if leg.wanted == nil {
				continue
			}
			node, port, isInput := 0, 0, false
			if *leg.wanted != "" {
				var ok bool
				if node, port, isInput, ok = rrcsPortOfPath(*leg.wanted); !ok {
					problems = append(problems, rrcsEnsureFailure{Field: field, Reason: leg.member + " " + *leg.wanted + " is not a port path"})
					bad = true
					continue
				}
			}
			now := jMap(have.Raw[leg.member])
			if jInt(now, "Node") == node && jInt(now, "Port") == port {
				continue
			}
			// §8.10.4.4: node and port 0 set the leg to nothing.
			edit = append(edit, codec.Member{Name: leg.member, Value: codec.Struct(
				codec.Member{Name: "IsInput", Value: codec.Bool(isInput)},
				codec.Member{Name: "Node", Value: codec.Int(int32(node))},
				codec.Member{Name: "Port", Value: codec.Int(int32(port))})})
			from = append(from, fmt.Sprintf("%s %d.%d", strings.ToLower(leg.member), jInt(now, "Node"), jInt(now, "Port")))
			to = append(to, fmt.Sprintf("%s %d.%d", strings.ToLower(leg.member), node, port))
		}
		if w.DimLevel != nil && jInt(have.Raw, "DimLevel") != *w.DimLevel {
			if *w.DimLevel < 0 || *w.DimLevel > 7 {
				problems = append(problems, rrcsEnsureFailure{Field: field, Reason: "dim_level is 0 (0 dB) to 6 (-24 dB), or 7 (mute)"})
				bad = true
			} else {
				edit = append(edit, codec.Member{Name: "DimLevel", Value: codec.Int(int32(*w.DimLevel))})
				from, to = append(from, "dim "+strconv.Itoa(jInt(have.Raw, "DimLevel"))), append(to, "dim "+strconv.Itoa(*w.DimLevel))
			}
		}
		if bad || len(edit) == 0 {
			continue
		}
		// IFBNumber addresses the IFB; it does not change the number.
		params := append([]codec.Member{{Name: "IFBNumber", Value: codec.Int(int32(w.Number))}}, edit...)
		steps = append(steps, rrcsCfgStep{field: field, from: strings.Join(from, ", "), to: strings.Join(to, ", "),
			changeType: "edit", objectType: "ifb", params: params})
	}
	return steps, problems
}

// rrcsGainRaw reads a gain of the file: "mute", or dB in half steps
// (§8.5: -36..36 in half dB, -128 mute).
func rrcsGainRaw(text string) (int, error) {
	text = strings.TrimSpace(text)
	if strings.EqualFold(text, "mute") {
		return -128, nil
	}
	db, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(text, "dB")), 64)
	raw := int(db * 2)
	if err != nil || float64(raw) != db*2 || raw < -36 || raw > 36 {
		return 0, fmt.Errorf("gain %q: want mute or -18 to 18 dB in steps of 0.5", text)
	}
	return raw, nil
}

func rrcsGainText(raw int) string {
	if raw == -128 {
		return "mute"
	}
	return strconv.FormatFloat(float64(raw)/2, 'f', 1, 64) + " dB"
}

// rrcsEnsurePorts brings label, alias and gains of ports to the file.
// Each is read, written when it differs, and read again.
func rrcsEnsurePorts(ctx context.Context, cf *rrcsFlags, client *rrcs.Client, ports []rrcsDesiredPort, check bool) (diff []rrcsDiffEntry, failures []rrcsEnsureFailure) {
	fail := func(field, reason string) {
		failures = append(failures, rrcsEnsureFailure{Field: field, Reason: reason})
	}
	// one brings one value to its target. read gives the value as text.
	one := func(field, wanted string, read func() (string, error), write func() error) {
		now, err := read()
		if err != nil {
			fail(field, err.Error())
			return
		}
		if now == wanted {
			return
		}
		if check {
			diff = append(diff, rrcsDiffEntry{Field: field, From: now, To: wanted})
			return
		}
		cf.say("rrcs ensure: %s = %s", field, wanted)
		if err := write(); err != nil {
			fail(field, err.Error())
			return
		}
		after, err := read()
		switch {
		case err != nil:
			fail(field, "written, but the read back failed: "+err.Error())
		case after != wanted:
			fail(field, "not_taken: RRCS accepted the request and still reports "+after)
		default:
			diff = append(diff, rrcsDiffEntry{Field: field, From: now, To: wanted})
		}
	}
	text := func(method string, args ...codec.Value) func() (string, error) {
		return func() (string, error) {
			reply, err := client.Call(ctx, method, args...)
			if err != nil {
				return "", err
			}
			if p := reply.Payload(); len(p.Items) > 0 {
				return p.Items[0].Str, nil
			}
			return "", nil
		}
	}
	gain := func(method string, args ...codec.Value) func() (string, error) {
		return func() (string, error) {
			reply, err := client.Call(ctx, method, args...)
			if err != nil {
				return "", err
			}
			if p := reply.Payload(); len(p.Items) > 0 && p.Items[0].Kind == codec.KindInt {
				return rrcsGainText(int(p.Items[0].Int)), nil
			}
			return "", fmt.Errorf("%s: no gain in the answer", method)
		}
	}
	send := func(method string, args ...codec.Value) func() error {
		return func() error {
			_, err := client.Call(ctx, method, args...)
			return err
		}
	}
	for _, w := range ports {
		node, port, isInput, ok := rrcsPortOfPath(w.Port)
		if !ok || len(strings.Split(w.Port, ".")) > 7 {
			fail(w.Port, "not a port path")
			continue
		}
		net, n, p, in := codec.Int(rrcsNetOfPath(w.Port)), codec.Int(int32(node)), codec.Int(int32(port)), codec.Bool(isInput)
		if w.Label != nil {
			// §8.4: node, port, label, is input.
			one(w.Port+".Label", *w.Label, text("GetPortLabel", n, p, in), send("SetPortLabel", n, p, codec.String(*w.Label), in))
		}
		if w.Alias != nil {
			// §8.3: net, node, port, alias, is input.
			one(w.Port+".Alias", *w.Alias, text("GetPortAlias", net, n, p, in), send("SetPortAlias", net, n, p, codec.String(*w.Alias), in))
		}
		for _, g := range []struct{ name, wanted, get, set string }{
			{"InputGain", w.InputGain, "GetInputGain", "SetInputGain"},
			{"OutputGain", w.OutputGain, "GetOutputGain", "SetOutputGain"},
		} {
			if g.wanted == "" {
				continue
			}
			raw, err := rrcsGainRaw(g.wanted)
			if err != nil {
				fail(w.Port+"."+g.name, err.Error())
				continue
			}
			// §8.5: net, node, port, gain.
			one(w.Port+"."+g.name, rrcsGainText(raw), gain(g.get, net, n, p), send(g.set, net, n, p, codec.Int(int32(raw))))
		}
	}
	return diff, failures
}

// logChanges writes what a converging verb did to the sinks (file,
// syslog): one record per value changed, with what it was and what it
// is, and one per value that could not be brought to its target
// (docs/logging.md; msg=config_change, msg=config_failed). A dry run is
// logged as such, so the log tells a plan from an act.
func (c *rrcsFlags) logChanges(ctx context.Context, verb, target string, dryRun bool, diff []rrcsDiffEntry, failures []rrcsEnsureFailure) {
	if c.sink == nil {
		return
	}
	mode := "applied"
	if dryRun {
		mode = "dry_run"
	}
	for _, d := range diff {
		c.sink.Log(ctx, slog.LevelInfo, "config_change", slog.String("proto", rrcsProto), slog.String("verb", verb),
			slog.String("target", target), slog.String("mode", mode), slog.String("path", d.Field),
			slog.String("from", fmt.Sprint(d.From)), slog.String("to", fmt.Sprint(d.To)))
	}
	for _, f := range failures {
		c.sink.Log(ctx, slog.LevelWarn, "config_failed", slog.String("proto", rrcsProto), slog.String("verb", verb),
			slog.String("target", target), slog.String("mode", mode), slog.String("path", f.Field), slog.String("reason", f.Reason))
	}
}
