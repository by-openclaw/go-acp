package codec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// Envelope is the general response of every REST query (§4.3):
//
//	{"customerId":"KRATOS","system":"f89270fd…","query":"consumer/alarmPoints/system/idu",
//	 "timeMs":"1554224586888","parameters":[{…}]}
//
// customerId, system and query let an asynchronous client tell which
// question an answer belongs to; timeMs is milliseconds since the UNIX
// epoch, sent as a string in every example.
type Envelope struct {
	CustomerID string          `json:"customerId"`
	System     string          `json:"system"`
	Query      string          `json:"query"`
	TimeMs     Text            `json:"timeMs"`
	Parameters json.RawMessage `json:"parameters"`
}

// ErrNotEnvelope is returned when a body is JSON but not an envelope
// (no "query" and no "parameters").
var ErrNotEnvelope = errors.New("kratos: response is not an API envelope")

// DecodeEnvelope parses one REST answer.
func DecodeEnvelope(b []byte) (Envelope, error) {
	var e Envelope
	if err := json.Unmarshal(b, &e); err != nil {
		return Envelope{}, fmt.Errorf("kratos: decode envelope: %w", err)
	}
	if e.Query == "" && len(e.Parameters) == 0 {
		return Envelope{}, ErrNotEnvelope
	}
	return e, nil
}

// Rows decodes the parameters into dst, a pointer to a slice of row
// structs. The document shows parameters as an array (§4.3), but two
// answers carry a single object: msgExchangePurge returns the database
// driver's result object (§5.34.4), and the boundaries example opens
// its array without the bracket (§5.10.4). A single object is decoded
// as a one-row array; Single reports which form arrived.
func (e Envelope) Rows(dst any) (single bool, err error) {
	p := bytes.TrimSpace(e.Parameters)
	if len(p) == 0 || bytes.Equal(p, []byte("null")) {
		return false, nil
	}
	if p[0] == '{' {
		p = append(append([]byte{'['}, p...), ']')
		single = true
	}
	if err := json.Unmarshal(p, dst); err != nil {
		return single, fmt.Errorf("kratos: decode %s parameters: %w", e.Query, err)
	}
	return single, nil
}

// Time returns timeMs as milliseconds since the epoch, 0 when absent.
func (e Envelope) Time() int64 {
	n, _ := strconv.ParseInt(string(e.TimeMs), 10, 64)
	return n
}

// Text is a column value the device sends as a JSON string, a number or
// null — the same column is a string in one table and a number in the
// next ("Value":"180000", "LastUTS":0, "configLastUTS":null in one CVT
// row, §5.15.4). It keeps the text as sent: numbers verbatim, null as
// the empty string with Null set.
type Text string

// UnmarshalJSON accepts a string, a number, a boolean or null.
func (t *Text) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	switch {
	case bytes.Equal(b, []byte("null")):
		*t = ""
	case len(b) > 0 && b[0] == '"':
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*t = Text(s)
	default:
		*t = Text(b)
	}
	return nil
}

// String returns the text.
func (t Text) String() string { return string(t) }

// Int returns the text as an integer, ok false when it is not one.
func (t Text) Int() (int64, bool) {
	n, err := strconv.ParseInt(string(t), 10, 64)
	return n, err == nil
}
