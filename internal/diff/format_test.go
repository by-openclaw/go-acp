package diff

import (
	"bytes"
	"errors"
	"testing"

	"dhs/internal/export/canonical"
)

var errSink = errors.New("sink closed")

// failingWriter accepts writes until the nth call, which fails with
// errSink; models a pipe or terminal that goes away mid-report.
type failingWriter struct {
	failOn int
	calls  int
}

func (w *failingWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls == w.failOn {
		return 0, errSink
	}
	return len(p), nil
}

// mixedReport is one Breaking + one Added entry — enough to exercise
// every section-level line both formatters emit.
func mixedReport() *Report {
	before := makeExport(makeParam("1.2", "b", canonical.ParamReal, func(p *canonical.Parameter) {
		p.Access = canonical.AccessReadWrite
	}))
	after := makeExport(
		makeParam("1.2", "b", canonical.ParamReal, func(p *canonical.Parameter) {
			p.Access = canonical.AccessRead
		}),
		makeParam("1.3", "c", canonical.ParamReal),
	)
	return Diff(before, after)
}

// TestReport_WriteText — the terminal report: a count line greppable
// by scripts, then one section per non-empty category, risk first.
func TestReport_WriteText(t *testing.T) {
	var buf bytes.Buffer
	if err := mixedReport().WriteText(&buf); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	want := "2 changes: Breaking=1 Changed=0 Added=1 Removed=0\n\n" +
		"## Breaking (1)\n" +
		"  - parameter \"b\" access changed readWrite → read\n\n" +
		"## Added (1)\n" +
		"  - parameter \"c\" added\n\n"
	if buf.String() != want {
		t.Errorf("WriteText output:\n%s\nwant:\n%s", buf.String(), want)
	}
}

// TestReport_WriteText_SinkErrors — every write is checked; whichever
// line the sink drops, the caller learns about it.
func TestReport_WriteText_SinkErrors(t *testing.T) {
	cases := []struct {
		name   string
		report *Report
		failOn int
	}{
		{"no-changes line", Diff(nil, nil), 1},
		{"summary line", mixedReport(), 1},
		{"section heading", mixedReport(), 2},
		{"bullet", mixedReport(), 3},
		{"section trailer", mixedReport(), 4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.report.WriteText(&failingWriter{failOn: c.failOn})
			if !errors.Is(err, errSink) {
				t.Errorf("want errSink from write %d, got %v", c.failOn, err)
			}
		})
	}
}

// TestReport_WriteChangelog_Shape — Keep-a-Changelog block per
// ADR-0020: version header, one ### per non-empty category, bullets.
func TestReport_WriteChangelog_Shape(t *testing.T) {
	var buf bytes.Buffer
	if err := mixedReport().WriteChangelog(&buf, "2.4", "2026-05-15"); err != nil {
		t.Fatalf("WriteChangelog: %v", err)
	}
	want := "## [2.4] — 2026-05-15\n\n" +
		"### Breaking\n" +
		"- parameter \"b\" access changed readWrite → read\n\n" +
		"### Added\n" +
		"- parameter \"c\" added\n\n"
	if buf.String() != want {
		t.Errorf("WriteChangelog output:\n%s\nwant:\n%s", buf.String(), want)
	}
}

// TestReport_WriteChangelog_SinkErrors — same guarantee as WriteText,
// including the "No schema changes." placeholder path.
func TestReport_WriteChangelog_SinkErrors(t *testing.T) {
	cases := []struct {
		name   string
		report *Report
		failOn int
	}{
		{"version header", mixedReport(), 1},
		{"section heading", mixedReport(), 2},
		{"bullet", mixedReport(), 3},
		{"section trailer", mixedReport(), 4},
		{"no-changes placeholder", Diff(nil, nil), 2},
		{"no-changes trailer", Diff(nil, nil), 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.report.WriteChangelog(&failingWriter{failOn: c.failOn}, "1.0", "2026-01-01")
			if !errors.Is(err, errSink) {
				t.Errorf("want errSink from write %d, got %v", c.failOn, err)
			}
		})
	}
}
