package codec

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
)

// Compliance labels. Each one is a place where the instrument did not
// answer as the reading in internal/prism/CLAUDE.md expects.
const (
	DevUnwrapped       = "prism_reply_unwrapped"     // JSON answer with no top-level "object"
	DevNotJSON         = "prism_reply_not_json"      // 2xx answer that is not JSON
	DevScalarForm      = "prism_scalar_form_differs" // scalar where the array was read, or the reverse
	DevWrongType       = "prism_reply_wrong_type"    // the value is not of the call's datatype
	DevErrorIn2xx      = "prism_error_in_2xx"        // 2xx answer that carries an error
	DevReadbackDiffers = "prism_readback_differs"    // the read-back is not the value written
)

// PostFailed reports a 2xx answer to a POST that carries an error. The
// page does not say what a POST answers, so nothing else is read from it.
func PostFailed(raw []byte) error {
	v, err := decodeJSON(bytes.TrimSpace(raw))
	if err != nil {
		return nil
	}
	top, _ := v.(map[string]any)
	if _, failed := top["error"]; failed {
		return errors.New(compact(raw))
	}
	return nil
}

// InputOf reads the mandatory "input" of an inputConfigure body.
func InputOf(body []byte) (string, error) {
	var b struct {
		Input *json.Number `json:"input"`
	}
	if json.Unmarshal(body, &b) != nil || b.Input == nil {
		return "", &Invalid{"inputConfigure", `the body needs "input": 0 .. 5`}
	}
	return b.Input.String(), nil
}

// ErrNoValue is a 2xx answer that holds no value.
var ErrNoValue = errors.New("no value in the answer")

// ErrNotAValue is a 2xx answer that cannot be a value of this call.
var ErrNotAValue = errors.New("the answer is not a value")

// Reply is one decoded GET answer.
type Reply struct {
	// Single is the one value: json.Number, string or bool. Nil when the
	// answer is a list of several, or an object.
	Single any
	// Text is the value as the operator reads it.
	Text string
	// Deviations are the labels this answer fired.
	Deviations []string
}

// DecodeReply reads one 2xx answer to a GET. An answer narrowed by
// ?field= is the member alone, so it is not held to the wrapper or to the
// call's datatype.
func (e Entry) DecodeReply(raw []byte, form ScalarForm, fielded bool) (Reply, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return Reply{}, ErrNoValue
	}
	v, err := decodeJSON(raw)
	if err != nil {
		r := Reply{Deviations: []string{DevNotJSON}}
		if e.Type != TypeString || raw[0] == '<' {
			return r, ErrNotAValue
		}
		r.Single, r.Text = string(raw), string(raw)
		return r, nil
	}

	var r Reply
	top, _ := v.(map[string]any)
	if body, wrapped := top["object"]; wrapped {
		v = body
	} else if _, failed := top["error"]; failed {
		r.Deviations = append(r.Deviations, DevErrorIn2xx)
		return r, errors.New(compact(raw))
	} else if !fielded {
		r.Deviations = append(r.Deviations, DevUnwrapped)
	}
	if v == nil {
		return r, ErrNoValue
	}
	if !fielded {
		r.Deviations = append(r.Deviations, e.shapeDeviations(v, form)...)
	}
	r.Single, r.Text = render(v)
	return r, nil
}

func decodeJSON(raw []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	if d.More() {
		return nil, ErrNotAValue
	}
	return v, nil
}

func (e Entry) shapeDeviations(v any, form ScalarForm) []string {
	switch e.Type {
	case TypeObject:
		switch v.(type) {
		case map[string]any, []any:
			return nil
		}
		return []string{DevWrongType}
	case TypeString:
		if _, ok := v.(string); ok {
			return nil
		}
		return []string{DevWrongType}
	}
	list, isList := v.([]any)
	var out []string
	if isList {
		if len(list) == 1 && form == ScalarBare && e.Type != TypeQuantity {
			out = append(out, DevScalarForm)
		}
	} else {
		if form == ScalarArray || e.Type == TypeQuantity {
			out = append(out, DevScalarForm)
		}
		list = []any{v}
	}
	for _, item := range list {
		if !e.itemFits(item) {
			return append(out, DevWrongType)
		}
	}
	return out
}

func (e Entry) itemFits(item any) bool {
	switch x := item.(type) {
	case json.Number:
		return e.Type != TypeQuantity
	case string:
		if e.Type == TypeQuantity {
			return true
		}
		return e.Type == TypeInts && slices.Contains(e.Options, x)
	}
	return false
}

func render(v any) (any, string) {
	if list, ok := v.([]any); ok && len(list) == 1 {
		v = list[0]
	}
	switch x := v.(type) {
	case json.Number:
		return x, x.String()
	case string:
		return x, x
	case bool:
		return x, strconv.FormatBool(x)
	}
	b, _ := json.Marshal(v) // v came from the JSON decoder
	return nil, string(b)
}

func compact(raw []byte) string {
	var b bytes.Buffer
	if json.Compact(&b, raw) != nil {
		return string(raw)
	}
	return b.String()
}
