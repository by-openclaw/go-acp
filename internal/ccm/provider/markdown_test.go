package ccm

import (
	"strings"
	"testing"
)

func render(t *testing.T, md string) string {
	t.Helper()
	return string(renderMarkdown([]byte(md)))
}

func mustContain(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("rendered output lacks %q:\n%s", w, got)
		}
	}
}

// Headings map to h1..h6; deeper than six clamps to h6. Paragraph lines join.
func TestMarkdownHeadingsAndParagraphs(t *testing.T) {
	got := render(t, "# Title\n\n## Sub\n\n####### Deep\n\nline one\nline two\n")
	mustContain(t, got, "<h1>Title</h1>", "<h2>Sub</h2>", "<h6>Deep</h6>", "<p>line one line two</p>")
}

// Fenced code is emitted verbatim and escaped; an unterminated fence still
// renders what it holds instead of swallowing the rest of the document.
func TestMarkdownCodeFences(t *testing.T) {
	got := render(t, "```\ndhs producer ccm serve --bind :8443\n<not html>\n```\nafter\n")
	mustContain(t, got, "<pre><code>dhs producer ccm serve --bind :8443\n&lt;not html&gt;\n</code></pre>", "<p>after</p>")
	got = render(t, "```\nunterminated\n")
	mustContain(t, got, "<pre><code>unterminated\n</code></pre>")
}

// Bullet and numbered lists render as ul/ol; switching kinds closes the first.
func TestMarkdownLists(t *testing.T) {
	got := render(t, "- a\n* b\n1. one\n2. two\n")
	mustContain(t, got, "<ul>\n<li>a</li>\n<li>b</li>\n</ul>", "<ol>\n<li>one</li>\n<li>two</li>\n</ol>")
	// ol then ul flushes the ol first.
	got = render(t, "1. one\n- a\n")
	if strings.Index(got, "</ol>") > strings.Index(got, "<ul>") {
		t.Errorf("ol must close before ul opens:\n%s", got)
	}
}

// Pipe tables: the first row is the header, the |---| separator is dropped,
// cells are inline-rendered and escaped.
func TestMarkdownTables(t *testing.T) {
	got := render(t, "| Prefix | What |\n|---|---|\n| `/api/v1` | the **protocol** |\n")
	mustContain(t, got,
		"<thead><tr><th>Prefix</th><th>What</th></tr></thead>",
		"<td><code>/api/v1</code></td><td>the <strong>protocol</strong></td>")
	if strings.Contains(got, "---") {
		t.Errorf("separator row leaked into the table:\n%s", got)
	}
	// A pipe row with an empty cell is not a separator.
	if isTableSeparator([]string{"", "---"}) {
		t.Error("an empty cell must not read as a separator")
	}
	if isTableSeparator(nil) {
		t.Error("no cells is not a separator")
	}
}

// Inline spans: code, bold, italic, links — and raw HTML in the source is
// escaped, never emitted as markup.
func TestMarkdownInlineAndEscaping(t *testing.T) {
	got := render(t, "use `code`, **bold**, *em*, [link](/x-dhs/readme) and <script>x</script>\n")
	mustContain(t, got,
		"<code>code</code>", "<strong>bold</strong>", "<em>em</em>",
		`<a href="/x-dhs/readme">link</a>`, "&lt;script&gt;x&lt;/script&gt;")
	if strings.Contains(got, "<script>") {
		t.Errorf("raw HTML leaked:\n%s", got)
	}
}

// Blank lines end a paragraph, a list and a table so the next block starts
// clean; a heading right after a list closes the list.
func TestMarkdownBlockBoundaries(t *testing.T) {
	got := render(t, "- item\n# Head\n| a |\n|---|\n| b |\n\npara\n")
	mustContain(t, got, "</ul>\n<h1>Head</h1>", "<td>b</td>", "<p>para</p>")
}
