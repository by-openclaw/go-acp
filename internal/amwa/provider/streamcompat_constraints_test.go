package provider

// IS-11 constraint re-negotiation surface: applying Active Constraints
// to a Sender must move its Inputs — version bump + a different (but
// still valid) Effective EDID — exactly what IS-11-01 test_02_03_05_*
// observes.

import (
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"dhs/internal/amwa/codec/is11"
)

func scReadEffective(t *testing.T, ts *httptest.Server, id string) []byte {
	t.Helper()
	resp, err := stdhttp.Get(ts.URL + scBase + "/inputs/" + id + "/edid/effective/")
	if err != nil {
		t.Fatalf("GET effective: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("effective EDID = %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	blob, _ := io.ReadAll(resp.Body)
	return blob
}

func TestIS11ConstraintsMoveInputs(t *testing.T) {
	ts, _ := scServer(t, false)

	var before is11.Input
	scGet(t, ts, scBase+"/inputs/"+scInput+"/properties/", &before)
	edidBefore := scReadEffective(t, ts, scInput)

	body := []byte(`{"constraint_sets":[{"urn:x-nmos:cap:format:grain_rate":{"enum":[{"numerator":25,"denominator":1}]}}]}`)
	resp := scDo(t, ts, "PUT", scBase+"/senders/"+scSender+"/constraints/active/", body, "application/json")
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("PUT constraints = %d", resp.StatusCode)
	}

	var after is11.Input
	scGet(t, ts, scBase+"/inputs/"+scInput+"/properties/", &after)
	if after.Version == before.Version {
		t.Error("input version did not change on constraints PUT")
	}
	edidAfter := scReadEffective(t, ts, scInput)
	if string(edidAfter) == string(edidBefore) {
		t.Error("effective EDID did not change on constraints PUT")
	}
	// Still a structurally valid EDID block: header + checksum.
	if len(edidAfter) != 128 || edidAfter[0] != 0x00 || edidAfter[1] != 0xFF {
		t.Errorf("varied EDID lost its structure: len=%d", len(edidAfter))
	}
	var sum byte
	for _, v := range edidAfter {
		sum += v
	}
	if sum != 0 {
		t.Errorf("varied EDID checksum broken (block sum %d)", sum)
	}

	// DELETE moves them again.
	resp = scDo(t, ts, "DELETE", scBase+"/senders/"+scSender+"/constraints/active/", nil, "")
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("DELETE constraints = %d", resp.StatusCode)
	}
	edidReset := scReadEffective(t, ts, scInput)
	if string(edidReset) == string(edidAfter) {
		t.Error("effective EDID did not move on constraints DELETE")
	}
}
