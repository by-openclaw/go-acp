package is05

// IS-05 bulk — POST /bulk/senders and /bulk/receivers.
//
// A bulk request is an array of {id, params}, where params is the same
// partial body a single PATCH of that resource's /staged carries; the
// response is an array of per-id {id, code[, error, debug]}, because a
// bulk request can partially succeed and one status could not say
// which entry was refused.
//
// The response rule is AMWA's bulk-response-schema.json, embedded
// verbatim and used as the validator — its text is the same at v1.0.2,
// v1.1.2 and v1.2.0. Decode is tolerant: an entry outside the schema
// is REPORTED as a deviation, never a reason to lose the verdicts of
// the entries around it (the controller needs every one it can read).
// Encode is strict: a request this codec builds wrong is our bug.

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"

	"dhs/internal/amwa/codec/jsonschema"
)

// bulkResponseSchemaName is the file AMWA publishes the rule under.
const bulkResponseSchemaName = "bulk-response-schema.json"

//go:embed testdata/schemas/v1.1.2/bulk-response-schema.json
var bulkResponseSchema []byte

type bulkSchemaLoader struct{}

func (bulkSchemaLoader) Load(name string) ([]byte, error) {
	if name != bulkResponseSchemaName {
		return nil, fmt.Errorf("is05: no embedded schema %q", name)
	}
	return bulkResponseSchema, nil
}

var bulkSchemas = jsonschema.New(bulkSchemaLoader{})

// resourceIDPattern is the id pattern both bulk-*-post schemas state.
var resourceIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// BulkItem is one entry of a bulk POST: the target resource and the
// partial /staged body to merge into it. Params is a map for the
// reason a PATCH body is one — a PATCH is a merge, and a round trip
// through the full struct would send zero values for every member the
// caller never mentioned and silently clear them.
type BulkItem struct {
	ID     string         `json:"id"`
	Params map[string]any `json:"params"`
}

// BulkResult is the Device's verdict on one entry. Code is the HTTP
// status the single PATCH would have answered.
type BulkResult struct {
	ID    string `json:"id"`
	Code  int    `json:"code"`
	Error string `json:"error,omitempty"`
	Debug string `json:"debug,omitempty"`
}

// OK reports whether the entry was applied.
func (r BulkResult) OK() bool { return r.Code >= 200 && r.Code <= 299 }

// EncodeBulkRequest renders a bulk POST body. It refuses what the
// bulk-*-post schemas refuse and what no Device could answer
// meaningfully: an empty request, an id that is not a resource id, an
// entry without params, and the same id twice (two verdicts for one
// resource cannot be told apart in the response).
func EncodeBulkRequest(items []BulkItem) ([]byte, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("is05: bulk request carries no entries")
	}
	seen := make(map[string]bool, len(items))
	for i, it := range items {
		if !resourceIDPattern.MatchString(it.ID) {
			return nil, fmt.Errorf("is05: bulk entry %d: %q is not a resource id", i, it.ID)
		}
		if it.Params == nil {
			return nil, fmt.Errorf("is05: bulk entry %d (%s): params is required", i, it.ID)
		}
		if seen[it.ID] {
			return nil, fmt.Errorf("is05: bulk entry %d: %s appears twice", i, it.ID)
		}
		seen[it.ID] = true
	}
	return json.Marshal(items)
}

// DecodeBulkResponse parses a bulk response. The verdicts it could
// read come back even when the body deviates from AMWA's schema; each
// deviation is returned for the caller to report. Only a body that is
// not a JSON array at all is an error.
func DecodeBulkResponse(raw []byte) (results []BulkResult, deviations []string, err error) {
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, nil, fmt.Errorf("is05: bulk response is not an array: %w", err)
	}
	if verr := bulkSchemas.Validate(bulkResponseSchemaName, raw); verr != nil {
		deviations = append(deviations, verr.Error())
	}
	results = make([]BulkResult, 0, len(entries))
	for _, e := range entries {
		var fields map[string]any
		if json.Unmarshal(e, &fields) != nil {
			// Not an object: the schema deviation above already names
			// it, and there is no verdict in it to keep.
			continue
		}
		var r BulkResult
		r.ID, _ = fields["id"].(string)
		if n, ok := fields["code"].(float64); ok {
			r.Code = int(n)
		}
		r.Error, _ = fields["error"].(string)
		r.Debug, _ = fields["debug"].(string)
		results = append(results, r)
	}
	return results, deviations, nil
}
