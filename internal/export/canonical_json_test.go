package export

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"dhs/internal/export/canonical"
)

var errSink = errors.New("sink closed")

// brokenSink is an io.Writer whose sink is gone — every write fails.
type brokenSink struct{}

func (brokenSink) Write([]byte) (int, error) { return 0, errSink }

// cancelOnRead cancels ctx from inside Read and then reports EOF —
// models a caller tearing the request down while the body is being
// drained, without any timing dependency.
type cancelOnRead struct {
	cancel context.CancelFunc
	data   io.Reader
}

func (c *cancelOnRead) Read(p []byte) (int, error) {
	c.cancel()
	return c.data.Read(p)
}

func canonicalTree() *canonical.Export {
	return &canonical.Export{Root: &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "root", Path: "root", OID: "1", Access: canonical.AccessRead,
		Children: []canonical.Element{&canonical.Parameter{
			Header: canonical.Header{Number: 1, Identifier: "gain", Path: "root.gain", OID: "1.1",
				Access: canonical.AccessReadWrite, Children: canonical.EmptyChildren()},
			Type: canonical.ParamReal, Value: float64(-6),
		}},
	}}}
}

// TestWriteCanonicalJSON_Contract — a written export is 2-space
// indented, newline-terminated, and reads back to the same tree.
func TestWriteCanonicalJSON_Contract(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteCanonicalJSON(context.Background(), &buf, canonicalTree()); err != nil {
		t.Fatalf("WriteCanonicalJSON: %v", err)
	}
	out := buf.String()
	if !strings.HasSuffix(out, "}\n") {
		t.Errorf("output must end with a newline, got %q", out[len(out)-4:])
	}
	if !strings.Contains(out, "\n  \"root\": {\n    \"number\": 1,") {
		t.Errorf("expected 2-space indentation:\n%s", out)
	}
	back, err := ReadCanonicalJSON(context.Background(), strings.NewReader(out))
	if err != nil {
		t.Fatalf("ReadCanonicalJSON: %v", err)
	}
	p, ok := back.Root.Common().Children[0].(*canonical.Parameter)
	if !ok || p.OID != "1.1" || p.Value != float64(-6) {
		t.Errorf("round-trip lost the parameter: %#v", back.Root.Common().Children[0])
	}
}

// TestWriteCanonicalJSON_Failures — every failure is wrapped so the
// caller can branch on the cause.
func TestWriteCanonicalJSON_Failures(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	unencodable := canonicalTree()
	unencodable.Root.Common().Children[0].(*canonical.Parameter).Value = make(chan int)

	cases := []struct {
		name    string
		ctx     context.Context
		w       io.Writer
		export  *canonical.Export
		wantIs  error
		wantMsg string
	}{
		{"nil export", context.Background(), &bytes.Buffer{}, nil, ErrNilExport, "export: nil"},
		{"already-canceled context", canceled, &bytes.Buffer{}, canonicalTree(), context.Canceled, "write canceled"},
		{"unencodable value", context.Background(), &bytes.Buffer{}, unencodable, nil, "json encode export"},
		{"broken sink", context.Background(), brokenSink{}, canonicalTree(), errSink, "write canonical"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := WriteCanonicalJSON(c.ctx, c.w, c.export)
			if err == nil {
				t.Fatal("want error")
			}
			if c.wantIs != nil && !errors.Is(err, c.wantIs) {
				t.Errorf("errors.Is(%v, %v) false", err, c.wantIs)
			}
			if !strings.Contains(err.Error(), c.wantMsg) {
				t.Errorf("error %q must mention %q", err, c.wantMsg)
			}
		})
	}
	t.Run("unencodable value names the type", func(t *testing.T) {
		var ute *json.UnsupportedTypeError
		if err := WriteCanonicalJSON(context.Background(), &bytes.Buffer{}, unencodable); !errors.As(err, &ute) {
			t.Errorf("want *json.UnsupportedTypeError, got %v", err)
		}
	})
}

// TestReadCanonicalJSON_Failures — cancellation is honoured both
// before and after the body is consumed; reader and parse errors are
// distinguishable by prefix.
func TestReadCanonicalJSON_Failures(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	errRead := errors.New("connection reset")

	midCtx, midCancel := context.WithCancel(context.Background())
	t.Cleanup(midCancel)

	cases := []struct {
		name    string
		ctx     context.Context
		r       io.Reader
		wantIs  error
		wantMsg string
	}{
		{"canceled before read", canceled, strings.NewReader("{}"), context.Canceled, "read canceled"},
		{"reader fails", context.Background(), iotest.ErrReader(errRead), errRead, "read canonical"},
		{"canceled while reading", midCtx, &cancelOnRead{cancel: midCancel, data: strings.NewReader("{}")}, context.Canceled, "read canceled"},
		{"not an export", context.Background(), strings.NewReader(`{"root": [1]}`), nil, "parse canonical"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ReadCanonicalJSON(c.ctx, c.r)
			if err == nil {
				t.Fatal("want error")
			}
			if c.wantIs != nil && !errors.Is(err, c.wantIs) {
				t.Errorf("errors.Is(%v, %v) false", err, c.wantIs)
			}
			if !strings.Contains(err.Error(), c.wantMsg) {
				t.Errorf("error %q must mention %q", err, c.wantMsg)
			}
		})
	}
}

// TestReadCanonicalJSON_Templates — a top-level templates[] survives
// the read with its embedded element dispatched to a concrete type.
func TestReadCanonicalJSON_Templates(t *testing.T) {
	doc := `{"root": {"number": 1, "identifier": "root", "path": "root", "oid": "1", "isOnline": true, "access": "read"},
	         "templates": [{"number": 1, "oid": "9.1", "identifier": "chan", "description": null,
	                        "template": {"number": 1, "identifier": "chan", "path": "chan", "oid": "9.1", "access": "read",
	                                     "children": [{"number": 1, "identifier": "gain", "path": "chan.gain", "oid": "9.1.1",
	                                                   "access": "readWrite", "type": "real", "value": 0}]}}]}`
	e, err := ReadCanonicalJSON(context.Background(), strings.NewReader(doc))
	if err != nil {
		t.Fatalf("ReadCanonicalJSON: %v", err)
	}
	if e.Root.Kind() != "node" || e.Root.Common().OID != "1" {
		t.Errorf("root: got kind=%s oid=%s", e.Root.Kind(), e.Root.Common().OID)
	}
	if len(e.Templates) != 1 || e.Templates[0].Template.Kind() != "node" {
		t.Fatalf("templates: %+v", e.Templates)
	}
	if kids := e.Templates[0].Template.Common().Children; len(kids) != 1 || kids[0].Kind() != "parameter" {
		t.Errorf("template children: %+v", kids)
	}
}

// TestUnmarshalCanonicalElement — a bare fragment parses without an
// Export envelope; a non-element is refused with the parse prefix.
func TestUnmarshalCanonicalElement(t *testing.T) {
	el, err := UnmarshalCanonicalElement([]byte(`{"number": 3, "identifier": "sw", "path": "sw", "oid": "1.3", "access": "readWrite", "targets": [], "sources": []}`))
	if err != nil {
		t.Fatalf("UnmarshalCanonicalElement: %v", err)
	}
	if el.Kind() != "matrix" || el.Common().OID != "1.3" {
		t.Errorf("got kind=%s oid=%s, want matrix 1.3", el.Kind(), el.Common().OID)
	}
	if _, err := UnmarshalCanonicalElement([]byte(`"just a string"`)); err == nil || !strings.HasPrefix(err.Error(), "parse element:") {
		t.Errorf("want parse element error, got %v", err)
	}
}

// TestFormat — the CLI/API spelling of each format and the file
// extension each one writes.
func TestFormat(t *testing.T) {
	cases := []struct {
		in   string
		want Format
		ok   bool
	}{
		{"", FormatJSON, true}, {"json", FormatJSON, true}, {"JSON", FormatJSON, true}, {"Json", FormatJSON, true},
		{"yaml", FormatYAML, true}, {"YAML", FormatYAML, true}, {"Yaml", FormatYAML, true}, {"yml", FormatYAML, true},
		{"csv", FormatCSV, true}, {"CSV", FormatCSV, true}, {"Csv", FormatCSV, true},
		{"xml", FormatJSON, false},
	}
	for _, c := range cases {
		t.Run("parse "+c.in, func(t *testing.T) {
			got, err := ParseFormat(c.in)
			if (err == nil) != c.ok {
				t.Fatalf("ParseFormat(%q) err=%v, want ok=%v", c.in, err, c.ok)
			}
			if got != c.want {
				t.Errorf("ParseFormat(%q) = %v, want %v", c.in, got, c.want)
			}
			if !c.ok && !strings.Contains(err.Error(), "use json, yaml, or csv") {
				t.Errorf("rejection must list the accepted values, got %q", err)
			}
		})
	}
	for f, ext := range map[Format]string{FormatJSON: "json", FormatYAML: "yaml", FormatCSV: "csv", Format(99): "json"} {
		if got := f.String(); got != ext {
			t.Errorf("Format(%d).String() = %q, want %q", f, got, ext)
		}
	}
}
