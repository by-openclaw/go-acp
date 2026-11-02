package consumer

import (
	"context"
	"strconv"
	"strings"

	dhsc "dhs/internal/consumer"
)

// ValidateValue checks one write without touching the gateway
// (consumer.ValueValidator, ADR-0037 P6): the path names an object and a
// property, the property is one a method of its own or a configuration
// change writes, and the value has the type, the range and the named
// values the specification's tables print. The generic dry runs refuse
// here, before anything is sent.
//
// What needs the live object is not checked here: whether a stream is in
// NMOS mode (ADR-0035 refuses address fields then) is SetValue's
// business, after a read.
func (p *Plugin) ValidateValue(_ context.Context, req dhsc.ValueRequest, val dhsc.Value) error {
	if req.Path == "" {
		return &dhsc.ValidationError{Field: "path", Reason: "rrcs names a property by --path (object.Block.Field or object.Field)"}
	}
	object, block, field, ok := SplitPath(req.Path)
	if !ok {
		return &dhsc.ValidationError{Field: req.Path, Reason: "does not name an object and a property (object.Block.Field or object.Field)"}
	}
	wanted := wantedText(val)
	kind, _, _ := strings.Cut(object, ".")
	_, _, _, _, isPort := PortOfPath(object)
	switch {
	case kind == "logic" && block == "" && field == "State":
		switch strings.ToLower(wanted) {
		case "true", "false", "yes", "no", "1", "0", "on", "off":
			return nil
		}
		return &dhsc.ValidationError{Field: req.Path, Reason: wanted + " is not true or false"}
	case isPort && block == "" && (field == "InputGain" || field == "OutputGain"):
		n, err := strconv.Atoi(wanted)
		if err != nil {
			return &dhsc.ValidationError{Field: req.Path, Reason: wanted + " is not a whole number"}
		}
		if n != -128 && (n < -36 || n > 36) {
			return &dhsc.ValidationError{Field: req.Path, Reason: wanted + " is outside -36..36 half decibels (-128 = mute)"}
		}
		return nil
	case isPort && block == "" && (field == "Label" || field == "Alias"):
		if _, reason := TypedValue("portex", "", field, wanted, nil, false, nil); reason != "" {
			return &dhsc.ValidationError{Field: req.Path, Reason: reason}
		}
		return nil
	}
	if !Writable[block][field] {
		return &dhsc.ValidationError{Field: req.Path, Reason: "read only: not a property a configuration change or a method writes"}
	}
	if field == "Mode" && strings.HasPrefix(block, "PortAes67") && strings.HasSuffix(object, ".out") {
		return &dhsc.ValidationError{Field: req.Path, Reason: "an output's link carries Mode and Selection in one request: set, import or ensure carry it"}
	}
	specObject := "portex"
	if strings.Contains(object, ".card.") {
		specObject = "client-card"
	}
	if _, reason := TypedValue(specObject, block, field, wanted, nil, false, nil); reason != "" {
		return &dhsc.ValidationError{Field: req.Path, Reason: reason}
	}
	return nil
}

// wantedText is the value as the specification's tables read it.
func wantedText(val dhsc.Value) string {
	switch val.Kind {
	case dhsc.KindInt:
		return strconv.FormatInt(val.Int, 10)
	case dhsc.KindUint:
		return strconv.FormatUint(val.Uint, 10)
	case dhsc.KindBool:
		return strconv.FormatBool(val.Bool)
	case dhsc.KindFloat:
		return strconv.FormatFloat(val.Float, 'f', -1, 64)
	}
	return val.Str
}
