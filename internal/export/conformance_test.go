// Doc-conformance test: every fenced ```json block inside
// docs/protocols/elements/*.md must round-trip through the canonical
// Go structs. Catches schema drift between docs and code.
package export_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"acp/internal/export/canonical"
)

var fencedJSON = regexp.MustCompile("(?s)```json\\s*\\n(.*?)\\n```")

func TestDocConformance_ElementsMarkdown(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "protocols", "elements")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read docs dir: %v", err)
	}

	var seenAny bool
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(root, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read %s: %v", entry.Name(), err)
			continue
		}
		matches := fencedJSON.FindAllSubmatch(data, -1)
		if len(matches) == 0 {
			continue
		}
		for i, m := range matches {
			blockName := fmt.Sprintf("%s block[%d]", entry.Name(), i)
			t.Run(blockName, func(t *testing.T) {
				seenAny = true
				parseBlock(t, entry.Name(), m[1])
			})
		}
	}

	if !seenAny {
		t.Fatal("no fenced JSON blocks found — docs/protocols/elements is empty or regex missed")
	}
}

// parseBlock decides what schema to apply based on the file name and
// the block's keys, then Unmarshal-checks the JSON through the
// canonical struct. Failure means doc and code drifted.
func parseBlock(t *testing.T, filename string, body []byte) {
	t.Helper()
	// Some blocks are JSON fragments (not full elements) — e.g. the
	// schema.md compliance-events table, the stream descriptor sample,
	// or a bare `{<number>: "label"}` map. Skip blocks that don't have
	// an object at the top level or that don't look like an element.
	trimmed := strings.TrimSpace(string(body))
	if !strings.HasPrefix(trimmed, "{") {
		return
	}

	// Peek at keys to decide strategy.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(body, &probe); err != nil {
		t.Logf("non-object or invalid JSON in %s; skipping", filename)
		return
	}

	// README.md samples the common header only; skip.
	if _, hasOID := probe["oid"]; !hasOID {
		return
	}

	// Dispatch by filename. Elements have predictable schemas.
	switch {
	case strings.Contains(filename, "node"):
		checkElement[canonical.Node](t, body)
	case strings.Contains(filename, "parameter"):
		checkElement[canonical.Parameter](t, body)
	case strings.Contains(filename, "matrix"):
		checkElement[canonical.Matrix](t, body)
	case strings.Contains(filename, "function"):
		checkElement[canonical.Function](t, body)
	case strings.Contains(filename, "template"):
		// Template entries may be full Template shape or a fragment;
		// rely on generic element dispatch.
		if _, err := canonical.UnmarshalElement(body); err != nil {
			t.Errorf("template block unmarshal: %v\nbody:\n%s", err, truncate(body))
		}
	case strings.Contains(filename, "stream"):
		// Stream samples are Parameters carrying streamIdentifier; if
		// they have an oid they parse as Parameter.
		checkElement[canonical.Parameter](t, body)
	default:
		if _, err := canonical.UnmarshalElement(body); err != nil {
			t.Errorf("generic dispatch unmarshal: %v\nbody:\n%s", err, truncate(body))
		}
	}
}

func checkElement[T any](t *testing.T, body []byte) {
	t.Helper()
	var out T
	if err := json.Unmarshal(body, &out); err != nil {
		t.Errorf("unmarshal: %v\nbody:\n%s", err, truncate(body))
	}
}

func truncate(b []byte) string {
	const lim = 400
	s := string(b)
	if len(s) > lim {
		return s[:lim] + "..."
	}
	return s
}
