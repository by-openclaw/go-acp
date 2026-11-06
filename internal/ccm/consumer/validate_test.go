package consumer

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	dhsc "dhs/internal/consumer"
)

// An import's pre-flight: what the device's document refuses, and an
// address field given something that is not an address, are reported
// before anything is sent — while a value the device already holds is
// left alone whatever it is.
func TestValidateValueBeforeAnImportWrites(t *testing.T) {
	const spec = "openapi: 3.1.1\npaths:\n  /tx:\n    get:\n      operationId: G\n    put:\n      operationId: S\n" +
		"      requestBody:\n        content:\n          application/json:\n            schema:\n              type: object\n              properties:\n" +
		"                ip:\n                  type: string\n" +
		"                dst:\n                  type: string\n" +
		"                mode:\n                  type: string\n                  enum:\n                    - Auto\n                    - Main\n" +
		"  /ro:\n    get:\n      operationId: R\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/self":
			_, _ = w.Write([]byte(`{"app":{"productName":"X","productVersion":"1"}}`))
		case "/docs/api.yml":
			_, _ = w.Write([]byte(spec))
		case "/tx":
			_, _ = w.Write([]byte(`{"ip":"239.30.0.1","dst":"","mode":"Backup"}`))
		case "/ro":
			_, _ = w.Write([]byte(`{"x":1}`))
		default:
			http.Error(w, "no", http.StatusNotFound)
		}
	}))
	defer srv.Close()
	restore := dialClient
	dialClient = func(string) *Client { return testClient(srv) }
	t.Cleanup(func() { dialClient = restore })
	p := testPluginConnected(t, srv)
	var _ dhsc.ValueValidator = p

	str := func(s string) dhsc.Value { return dhsc.Value{Kind: dhsc.KindString, Str: s} }
	cases := []struct {
		name, path, val string
		ok              bool
	}{
		{"an address for an address", "tx.ip", "239.30.0.2", true},
		{"a name where the device holds an address", "tx.ip", "CAM-1", false},
		{"IPv6 where the device holds IPv4", "tx.ip", "ff02::1", false},
		{"an empty field takes anything", "tx.dst", "CAM-1", true},
		{"a word of the enumeration", "tx.mode", "Main", true},
		{"a word outside it", "tx.mode", "main", false},
		{"what the device holds, even outside the list", "tx.mode", "Backup", true},
		{"a resource with no write", "ro.x", "2", false},
		{"a resource, not a value", "tx", "x", false},
		{"a path this device does not serve", "nowhere.x", "1", false},
	}
	for _, c := range cases {
		err := p.ValidateValue(context.Background(), dhsc.ValueRequest{Path: c.path}, str(c.val))
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v", c.name, err)
		}
		if err != nil && !errors.Is(err, dhsc.ErrValidationFailed) && !errors.Is(err, dhsc.ErrObjectNotFound) {
			t.Errorf("%s: %v is neither a validation failure nor not-found", c.name, err)
		}
	}
}

// Without a session there is nothing to check against.
func TestValidateValueNeedsAConnection(t *testing.T) {
	err := (&Plugin{}).ValidateValue(context.Background(), dhsc.ValueRequest{Path: "tx.ip"}, dhsc.Value{Kind: dhsc.KindString, Str: "1"})
	if err == nil {
		t.Error("an unconnected plugin validated a row")
	}
}
