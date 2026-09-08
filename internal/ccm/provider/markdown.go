package ccm

import (
	"bufio"
	"bytes"
	"html"
	"regexp"
	"strings"
)

// renderMarkdown turns the README subset this repo's docs use into an HTML
// fragment: ATX headings, paragraphs, fenced code blocks, bullet and numbered
// lists, pipe tables, and inline code / bold / italic / links. It is
// deliberately a subset — a landing page renders the connector's own tech
// doc, not arbitrary Markdown — and it is stdlib-only, because a rendered
// README is not worth a dependency in a device emulator (ADR-0005). All text
// is HTML-escaped; the only markup emitted is what the renderer produces.
func renderMarkdown(md []byte) []byte {
	var out bytes.Buffer
	var para []string
	var list []string
	listTag := ""
	var table [][]string
	inCode := false
	var code bytes.Buffer

	flushPara := func() {
		if len(para) > 0 {
			out.WriteString("<p>" + inline(strings.Join(para, " ")) + "</p>\n")
			para = nil
		}
	}
	flushList := func() {
		if len(list) > 0 {
			out.WriteString("<" + listTag + ">\n")
			for _, it := range list {
				out.WriteString("<li>" + inline(it) + "</li>\n")
			}
			out.WriteString("</" + listTag + ">\n")
			list = nil
			listTag = ""
		}
	}
	flushTable := func() {
		if len(table) == 0 {
			return
		}
		out.WriteString("<table>\n<thead><tr>")
		for _, c := range table[0] {
			out.WriteString("<th>" + inline(c) + "</th>")
		}
		out.WriteString("</tr></thead>\n<tbody>\n")
		for _, row := range table[1:] {
			out.WriteString("<tr>")
			for _, c := range row {
				out.WriteString("<td>" + inline(c) + "</td>")
			}
			out.WriteString("</tr>\n")
		}
		out.WriteString("</tbody>\n</table>\n")
		table = nil
	}
	flushAll := func() { flushPara(); flushList(); flushTable() }

	sc := bufio.NewScanner(bytes.NewReader(md))
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```") {
			if inCode {
				out.WriteString("<pre><code>" + html.EscapeString(code.String()) + "</code></pre>\n")
				code.Reset()
				inCode = false
			} else {
				flushAll()
				inCode = true
			}
			continue
		}
		if inCode {
			code.WriteString(line + "\n")
			continue
		}

		switch {
		case trimmed == "":
			flushAll()
		case strings.HasPrefix(trimmed, "#"):
			flushAll()
			// Count the hashes to find the text, then clamp the heading level
			// separately: "####### x" is an h6 whose text is "x", not "# x".
			hashes := len(trimmed) - len(strings.TrimLeft(trimmed, "#"))
			level := hashes
			if level > 6 {
				level = 6
			}
			text := strings.TrimSpace(trimmed[hashes:])
			tag := "h" + string(rune('0'+level))
			out.WriteString("<" + tag + ">" + inline(text) + "</" + tag + ">\n")
		case strings.HasPrefix(trimmed, "|"):
			flushPara()
			flushList()
			cells := splitTableRow(trimmed)
			if isTableSeparator(cells) {
				continue
			}
			table = append(table, cells)
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
			flushPara()
			flushTable()
			if listTag != "" && listTag != "ul" {
				flushList()
			}
			listTag = "ul"
			list = append(list, strings.TrimSpace(trimmed[2:]))
		case numberedItem.MatchString(trimmed):
			flushPara()
			flushTable()
			if listTag != "" && listTag != "ol" {
				flushList()
			}
			listTag = "ol"
			list = append(list, numberedItem.ReplaceAllString(trimmed, ""))
		default:
			flushList()
			flushTable()
			para = append(para, trimmed)
		}
	}
	if inCode {
		// An unterminated fence still renders what it holds rather than
		// swallowing the rest of the document.
		out.WriteString("<pre><code>" + html.EscapeString(code.String()) + "</code></pre>\n")
	}
	flushAll()
	return out.Bytes()
}

var (
	numberedItem = regexp.MustCompile(`^\d+\.\s+`)
	inlineCode   = regexp.MustCompile("`([^`]+)`")
	inlineBold   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	inlineEm     = regexp.MustCompile(`\*([^*]+)\*`)
	inlineLink   = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
)

// inline escapes text then applies the inline spans. Escaping first means a
// literal "<" in the doc can never become markup; the span regexes match on
// the escaped text, whose delimiters (` * [ ] ( )) are untouched by escaping.
func inline(s string) string {
	s = html.EscapeString(s)
	s = inlineCode.ReplaceAllString(s, "<code>$1</code>")
	s = inlineBold.ReplaceAllString(s, "<strong>$1</strong>")
	s = inlineEm.ReplaceAllString(s, "<em>$1</em>")
	s = inlineLink.ReplaceAllString(s, `<a href="$2">$1</a>`)
	return s
}

// splitTableRow splits "| a | b |" into its cells.
func splitTableRow(row string) []string {
	row = strings.TrimPrefix(strings.TrimSuffix(row, "|"), "|")
	parts := strings.Split(row, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// isTableSeparator reports the "|---|---|" row between header and body.
func isTableSeparator(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		if c == "" || strings.Trim(c, "-: ") != "" {
			return false
		}
	}
	return true
}
