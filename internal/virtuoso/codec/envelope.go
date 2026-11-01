package codec

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// The two values the guide gives info.status ("HTTP response body").
const (
	StatusOK     = "ok"
	StatusFailed = "failed"
)

// ErrNotJSON is an answer that is not a JSON envelope. The guide says the
// API "may return XML or even HTML on GET requests".
var ErrNotJSON = errors.New("virtuoso: answer is not a JSON document")

// Message is one entry of info.errors, info.warnings or info.notes. The
// guide shows {"status":-1,"source":"","detail":"…"}; any other JSON is
// kept as text so that a message is never lost to its shape.
type Message struct {
	Status string
	Source string
	Detail string
}

// UnmarshalJSON accepts the documented object, a bare string, or anything else.
func (m *Message) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		m.Detail = s
		return nil
	}
	var o struct {
		Status Text   `json:"status"`
		Source string `json:"source"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		m.Detail = string(b)
		return nil
	}
	m.Status, m.Source, m.Detail = string(o.Status), o.Source, o.Detail
	return nil
}

// String is the message as an operator reads it.
func (m Message) String() string {
	if m.Source != "" {
		return m.Source + ": " + m.Detail
	}
	return m.Detail
}

// Info is the "info" object of an answer.
type Info struct {
	Status   string    `json:"status"`
	Errors   []Message `json:"errors"`
	Warnings []Message `json:"warnings"`
	Notes    []Message `json:"notes"`
}

// Envelope is one decoded answer. HasInfo is false on an answer without
// the "info" object the guide says is always there.
type Envelope struct {
	Data    json.RawMessage
	Info    Info
	HasInfo bool
}

// StatusError is an answer whose info.status is not "ok".
type StatusError struct {
	Status string
	Errors []Message
}

func (e *StatusError) Error() string {
	texts := make([]string, 0, len(e.Errors))
	for _, m := range e.Errors {
		texts = append(texts, m.String())
	}
	if len(texts) == 0 {
		return fmt.Sprintf("virtuoso: device status %q, no error text", e.Status)
	}
	return fmt.Sprintf("virtuoso: device status %q: %s", e.Status, strings.Join(texts, "; "))
}

// DecodeEnvelope decodes one answer body.
func DecodeEnvelope(raw []byte) (Envelope, error) {
	var w struct {
		Data json.RawMessage `json:"data"`
		Info *Info           `json:"info"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return Envelope{}, fmt.Errorf("%w: %v", ErrNotJSON, err)
	}
	env := Envelope{Data: w.Data}
	if w.Info != nil {
		env.Info, env.HasInfo = *w.Info, true
	}
	return env, nil
}

// Err is non-nil when the device reported a status other than "ok".
func (e Envelope) Err() error {
	if !e.HasInfo || e.Info.Status == StatusOK {
		return nil
	}
	return &StatusError{Status: e.Info.Status, Errors: e.Info.Errors}
}

// Lookup descends the data document along path, which is 1:1 with the URL
// path of the request.
func (e Envelope) Lookup(path []string) (json.RawMessage, bool) {
	cur := e.Data
	for _, seg := range path {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(cur, &m); err != nil {
			return nil, false
		}
		next, ok := m[seg]
		if !ok {
			return nil, false
		}
		cur = next
	}
	return cur, len(cur) > 0
}

// Text is a column that arrives as a string or as a number.
type Text string

// UnmarshalJSON keeps a number's own digits, and null as "".
func (t *Text) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*t = Text(s)
		return nil
	}
	*t = Text(b)
	return nil
}

// ProductInfo is /data/dev/product_info. Module identity on the manager
// and on a card alike.
type ProductInfo struct {
	Devname     Text `json:"devname"`
	InventoryID Text `json:"inventory_id"`
	Location    Text `json:"location"`
	Contact     Text `json:"contact"`
	UUID        Text `json:"uuid"`
	Model       Text `json:"model"`
	Prodname    Text `json:"prodname"`
	Serial      Text `json:"serial"`
	SerialFull  Text `json:"serialfull"`
	SwBuildTime Text `json:"swbuildtime"`
	SwVer       Text `json:"swver"`
	BoardSerial Text `json:"board_serial"`
}

// Fields is the identity as a non-empty name → value map.
func (p ProductInfo) Fields() map[string]string {
	all := map[string]Text{
		"devname": p.Devname, "inventory_id": p.InventoryID, "location": p.Location,
		"contact": p.Contact, "uuid": p.UUID, "model": p.Model, "prodname": p.Prodname,
		"serial": p.Serial, "serialfull": p.SerialFull, "swbuildtime": p.SwBuildTime,
		"swver": p.SwVer, "board_serial": p.BoardSerial,
	}
	out := make(map[string]string, len(all))
	for k, v := range all {
		if v != "" {
			out[k] = string(v)
		}
	}
	return out
}

// DecodeProductInfo reads the identity out of its answer.
func DecodeProductInfo(env Envelope) (ProductInfo, error) {
	raw, ok := env.Lookup(ProductInfoPath)
	if !ok {
		return ProductInfo{}, errors.New("virtuoso: answer holds no dev/product_info")
	}
	var p ProductInfo
	if err := json.Unmarshal(raw, &p); err != nil {
		return ProductInfo{}, fmt.Errorf("virtuoso: product_info: %w", err)
	}
	return p, nil
}
