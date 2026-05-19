package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRenderSubsRows_HappyPath pins the subs:list row rendering with
// two OIDs each having multiple subscribers; output must be sorted by
// OID and contain the per-row "close all" form.
func TestRenderSubsRows_HappyPath(t *testing.T) {
	data := json.RawMessage(`[
		{"oid":"1.2.3","subscribers":["peer-a","peer-b"]},
		{"oid":"1.2.1","subscribers":["peer-a"]}
	]`)
	got := renderSubsRows(data)
	if !strings.Contains(got, "1.2.1") || !strings.Contains(got, "1.2.3") {
		t.Errorf("OIDs missing from render: %s", got)
	}
	// Sorted ascending — 1.2.1 must appear before 1.2.3.
	if i, j := strings.Index(got, "1.2.1"), strings.Index(got, "1.2.3"); i < 0 || i > j {
		t.Errorf("subs not sorted: %s", got)
	}
	if !strings.Contains(got, `action="/subs/close"`) {
		t.Errorf("close form missing: %s", got)
	}
	if !strings.Contains(got, "peer-a, peer-b") {
		t.Errorf("subscribers list missing: %s", got)
	}
}

// TestRenderSubsRows_Empty pins the placeholder when there are no
// active subscriptions.
func TestRenderSubsRows_Empty(t *testing.T) {
	got := renderSubsRows(json.RawMessage(`[]`))
	if !strings.Contains(got, "no active subscriptions") {
		t.Errorf("empty-state row missing: %s", got)
	}
}

// TestRenderSubsRows_DecodeError ensures malformed payloads surface
// inline rather than panicking.
func TestRenderSubsRows_DecodeError(t *testing.T) {
	got := renderSubsRows(json.RawMessage(`not json`))
	if !strings.Contains(got, "decode error") {
		t.Errorf("decode-error row missing: %s", got)
	}
}

// TestHandleSessionsDisconnect_RejectsGET pins the method check —
// only POST is accepted; GET / HEAD / PUT must 405.
func TestHandleSessionsDisconnect_RejectsGET(t *testing.T) {
	w := &WebServer{Socket: "/nonexistent"}
	req := httptest.NewRequest(http.MethodGet, "/sessions/disconnect", nil)
	rr := httptest.NewRecorder()
	w.handleSessionsDisconnect(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d; want 405", rr.Code)
	}
	if rr.Header().Get("Allow") != "POST" {
		t.Errorf("Allow header = %q; want POST", rr.Header().Get("Allow"))
	}
}

// TestHandleSessionsDisconnect_RejectsMissingPeer pins the param check.
func TestHandleSessionsDisconnect_RejectsMissingPeer(t *testing.T) {
	w := &WebServer{Socket: "/nonexistent"}
	req := httptest.NewRequest(http.MethodPost, "/sessions/disconnect",
		strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	w.handleSessionsDisconnect(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("missing peer status = %d; want 400", rr.Code)
	}
}

// TestHandleSubsClose_RejectsBothEmpty pins the safety check that
// prevents an empty form (which would silently match every sub on
// the server) from being submitted.
func TestHandleSubsClose_RejectsBothEmpty(t *testing.T) {
	w := &WebServer{Socket: "/nonexistent"}
	req := httptest.NewRequest(http.MethodPost, "/subs/close",
		strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	w.handleSubsClose(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("empty form status = %d; want 400", rr.Code)
	}
}

// TestHandleSubsClose_RejectsGET pins the method check.
func TestHandleSubsClose_RejectsGET(t *testing.T) {
	w := &WebServer{Socket: "/nonexistent"}
	req := httptest.NewRequest(http.MethodGet, "/subs/close", nil)
	rr := httptest.NewRecorder()
	w.handleSubsClose(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d; want 405", rr.Code)
	}
}
