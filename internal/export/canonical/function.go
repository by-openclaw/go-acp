package canonical

import "encoding/json"

// TupleItem is one element of Function.Arguments or Function.Result.
// Types use the same vocabulary as Parameter.Type (see parameter.go).
type TupleItem struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Function is a callable element. See docs/protocols/elements/function.md.
type Function struct {
	Header

	Arguments []TupleItem `json:"arguments"`
	Result    []TupleItem `json:"result"`
}

// Kind implements Element.
func (*Function) Kind() string { return "function" }

// UnmarshalJSON handles children[] interface dispatch. Functions
// typically have no children but the schema permits it.
func (f *Function) UnmarshalJSON(data []byte) error {
	type alias struct {
		Number      int               `json:"number"`
		Identifier  string            `json:"identifier"`
		Path        string            `json:"path"`
		OID         string            `json:"oid"`
		Description *string           `json:"description"`
		IsOnline    bool              `json:"isOnline"`
		Access      string            `json:"access"`
		Children    []json.RawMessage `json:"children"`
		Arguments   []TupleItem       `json:"arguments"`
		Result      []TupleItem       `json:"result"`
	}
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	kids, err := unmarshalChildren(raw.Children)
	if err != nil {
		return err
	}
	f.Header = Header{
		Number:      raw.Number,
		Identifier:  raw.Identifier,
		Path:        raw.Path,
		OID:         raw.OID,
		Description: raw.Description,
		IsOnline:    raw.IsOnline,
		Access:      raw.Access,
		Children:    kids,
	}
	f.Arguments = raw.Arguments
	f.Result = raw.Result
	return nil
}
