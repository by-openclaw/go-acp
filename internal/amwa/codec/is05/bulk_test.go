package is05

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const (
	bulkRx1 = "2c47bf5e-1b2c-4abc-9def-deadbeef0103"
	bulkRx2 = "2c47bf5e-1b2c-4abc-9def-deadbeef0006"
)

// The embedded response rule is AMWA's file, and AMWA states the same
// rule at every published minor — so one validator serves v1.0, v1.1
// and v1.2. (The v1.0.x file carries a `v1.0-` name prefix.)
func TestBulkResponseSchemaIsTheSameRuleAtEveryMinor(t *testing.T) {
	strip := func(b []byte) string { return strings.Join(strings.Fields(string(b)), "") }
	v10, err := os.ReadFile("testdata/schemas/v1.0.2/v1.0-bulk-response-schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if strip(v10) != strip(bulkResponseSchema) {
		t.Error("v1.0.2 states a different bulk response rule than the embedded v1.1.2 one")
	}
	var doc map[string]any
	if err := json.Unmarshal(bulkResponseSchema, &doc); err != nil || doc["title"] != "Bulk activation response" {
		t.Errorf("embedded schema = %v (%v), want AMWA's bulk activation response", doc["title"], err)
	}
}

// The request body is the array of {id, params} the bulk-*-post
// schemas describe, in the order given.
func TestEncodeBulkRequest(t *testing.T) {
	raw, err := EncodeBulkRequest([]BulkItem{
		{ID: bulkRx1, Params: map[string]any{"master_enable": true, "sender_id": bulkRx2,
			"activation": map[string]any{"mode": "activate_immediate"}}},
		{ID: bulkRx2, Params: map[string]any{"master_enable": false, "sender_id": nil}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"id":"` + bulkRx1 + `","params":{"activation":{"mode":"activate_immediate"},"master_enable":true,"sender_id":"` + bulkRx2 + `"}},` +
		`{"id":"` + bulkRx2 + `","params":{"master_enable":false,"sender_id":null}}]`
	if string(raw) != want {
		t.Errorf("request =\n%s\nwant\n%s", raw, want)
	}

	refused := []struct {
		name  string
		items []BulkItem
		want  string
	}{
		{"an empty request", nil, "no entries"},
		{"an id that is not a resource id", []BulkItem{{ID: "receiver-1", Params: map[string]any{}}}, "not a resource id"},
		{"an upper-case id", []BulkItem{{ID: strings.ToUpper(bulkRx1), Params: map[string]any{}}}, "not a resource id"},
		{"an entry without params", []BulkItem{{ID: bulkRx1}}, "params is required"},
		{"the same id twice", []BulkItem{{ID: bulkRx1, Params: map[string]any{}}, {ID: bulkRx1, Params: map[string]any{}}}, "appears twice"},
	}
	for _, tc := range refused {
		if _, err := EncodeBulkRequest(tc.items); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want one mentioning %q", tc.name, err, tc.want)
		}
	}
}

// A response is read entry by entry: a partial success stays a partial
// success, and an entry outside AMWA's schema is reported without
// costing the verdicts around it.
func TestDecodeBulkResponse(t *testing.T) {
	results, deviations, err := DecodeBulkResponse([]byte(`[
		{"id":"` + bulkRx1 + `","code":200},
		{"id":"` + bulkRx2 + `","code":423,"error":"Resource is locked","debug":null}
	]`))
	if err != nil || len(deviations) != 0 {
		t.Fatalf("a conforming response: err=%v deviations=%v", err, deviations)
	}
	if len(results) != 2 || !results[0].OK() || results[1].OK() ||
		results[1].Code != 423 || results[1].Error != "Resource is locked" || results[1].Debug != "" {
		t.Errorf("results = %+v", results)
	}

	// Deviations: a code outside 200-299 / 400-599, a missing code, an
	// id that is not a uuid, an entry that is not an object. The
	// readable verdicts all come back.
	results, deviations, err = DecodeBulkResponse([]byte(`[
		{"id":"` + bulkRx1 + `","code":302},
		{"id":"` + bulkRx2 + `"},
		{"id":"not-a-uuid","code":200,"debug":"trace"},
		"nonsense"
	]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(deviations) != 1 {
		t.Fatalf("deviations = %v, want the schema's report", deviations)
	}
	for _, want := range []string{"anyOf", "code", "pattern", "type"} {
		if !strings.Contains(deviations[0], want) {
			t.Errorf("the deviation report does not mention %q: %s", want, deviations[0])
		}
	}
	if len(results) != 3 || results[0].Code != 302 || results[0].OK() || results[1].Code != 0 ||
		results[2].ID != "not-a-uuid" || results[2].Debug != "trace" {
		t.Errorf("results = %+v, want the three readable entries", results)
	}

	if _, _, err := DecodeBulkResponse([]byte(`{"code":500}`)); err == nil {
		t.Error("a response that is not an array must be an error")
	}
}

// The embedded loader serves exactly one file.
func TestBulkSchemaLoaderRefusesOtherNames(t *testing.T) {
	if _, err := (bulkSchemaLoader{}).Load("receiver-stage-schema.json"); err == nil {
		t.Error("a schema that is not embedded must not load")
	}
}
