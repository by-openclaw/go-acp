package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is04"
	v13 "dhs/internal/amwa/codec/is04/v13"
)

func TestNewClientRejectsBadInput(t *testing.T) {
	cases := []struct {
		name, base string
		codec      is04.Codec
		wantErr    string
	}{
		{"nil codec", "http://h", nil, "codec must not be nil"},
		{"empty base", "", v13.New(), "absolute URL"},
		{"bare host", "10.0.0.1", v13.New(), "absolute URL"},
		{"with path", "http://h/foo", v13.New(), "must not include a path"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewClient(tc.base, tc.codec)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want contains %q", err, tc.wantErr)
			}
		})
	}
}

func TestNewClientStripsTrailingSlash(t *testing.T) {
	c, err := NewClient("http://10.0.0.1:8235/", v13.New())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c.Base != "http://10.0.0.1:8235" {
		t.Fatalf("Base = %q", c.Base)
	}
	if c.Codec.APIVer() != "v1.3" {
		t.Fatalf("Codec.APIVer = %q", c.Codec.APIVer())
	}
}

func TestIndexRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/x-nmos/query/v1.3/" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`["nodes/","devices/","sources/","flows/","senders/","receivers/","subscriptions/"]`))
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL, v13.New())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	got, err := c.Index(context.Background())
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if len(got) != 7 {
		t.Fatalf("got %d entries, want 7", len(got))
	}
}

func TestListNodesAppliesLabelFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("label"); got != "lab" {
			t.Errorf("filter not propagated, got label=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{
		  "id":"f47ac10b-58cc-4372-a567-0e02b2c3d479",
		  "version":"1700000000:0","label":"lab","description":"x","tags":{},
		  "hostname":"h","href":"http://h/","caps":{},
		  "api":{"versions":["v1.3"],"endpoints":[{"host":"h","port":80,"protocol":"http"}]},
		  "services":[],"clocks":[],"interfaces":[]
		}]`))
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL, v13.New())
	nodes, err := c.ListNodes(context.Background(), map[string]string{"label": "lab"})
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Label != "lab" {
		t.Fatalf("nodes = %v", nodes)
	}
}

func TestDecodeListSurfacesError(t *testing.T) {
	bad := `{"id":"not-uuid","version":"x","label":"","description":"","tags":null}`
	raw := []json.RawMessage{json.RawMessage(bad)}
	c := v13.New()
	if _, err := decodeList(raw, "node", c.DecodeNode); err == nil {
		t.Fatalf("expected decode error on malformed element")
	}
}
