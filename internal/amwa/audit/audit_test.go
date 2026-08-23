package audit

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const plantDir = "testdata/plant"

// codes indexes findings by code so a test can assert on one without
// caring what else the fixture produces.
func codes(fs []Finding) map[string][]Finding {
	m := map[string][]Finding{}
	for _, f := range fs {
		m[f.Code] = append(m[f.Code], f)
	}
	return m
}

func loadPlant(t *testing.T) Result {
	t.Helper()
	hs, err := Load(plantDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return Run(hs, Options{})
}

// TestLoadPlant pins the shape the loader builds from a registry export:
// one root, its followed nodes as children, identity from device.json.
func TestLoadPlant(t *testing.T) {
	hs, err := Load(plantDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(hs) != 1 {
		t.Fatalf("want 1 root harvest, got %d", len(hs))
	}
	root := hs[0]
	if root.Role != "registry" {
		t.Errorf("root role = %q, want registry", root.Role)
	}
	if root.Target != "198.51.100.10:8080" {
		t.Errorf("root target = %q", root.Target)
	}
	if len(root.Children) != 2 {
		t.Fatalf("want 2 followed nodes, got %d", len(root.Children))
	}
	if got := len(root.All()); got != 3 {
		t.Errorf("All() = %d, want 3 (registry + 2 nodes)", got)
	}
	for _, c := range root.Children {
		if c.Role != "node" {
			t.Errorf("child %s role = %q, want node", c.Target, c.Role)
		}
		if c.ID == "" || c.Label == "" {
			t.Errorf("child %s lost its identity: id=%q label=%q", c.Target, c.ID, c.Label)
		}
	}
	// report.txt must survive the load — every status code lives there
	// and nowhere else.
	if len(root.Report) == 0 {
		t.Error("registry report.txt was not loaded")
	}
}

// TestLoadRejectsMissingAndFiles proves a mistyped path fails loudly
// rather than auditing as a clean plant.
func TestLoadRejectsMissingAndFiles(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("Load of a missing directory should fail")
	}

	f := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(f); err == nil {
		t.Error("Load of a plain file should fail")
	}

	empty := t.TempDir()
	if _, err := Load(empty); err != ErrNoHarvest {
		t.Errorf("Load of an empty directory: got %v, want ErrNoHarvest", err)
	}
}

// TestLoadRejectsBrokenTree proves a truncated capture is an error, not
// a silently smaller audit.
func TestLoadRejectsBrokenTree(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tree.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Error("a malformed tree.json must fail the load")
	}
}

// TestInventory checks that the audit reports what each device exposes,
// counted from the capture rather than from a version claim.
func TestInventory(t *testing.T) {
	res := loadPlant(t)
	if len(res.Inventory) != 3 {
		t.Fatalf("want 3 inventory rows, got %d", len(res.Inventory))
	}
	byTarget := map[string]Inventory{}
	for _, i := range res.Inventory {
		byTarget[i.Target] = i
	}

	reg := byTarget["198.51.100.10:8080"]
	// The registry lists 3 nodes at v1.1 and 1 at v1.3. The union is
	// the plant; taking the highest minor alone would report 1.
	if reg.Nodes != 3 {
		t.Errorf("registry node count = %d, want 3 (union across minors)", reg.Nodes)
	}
	if got := reg.APIs["query"]; strings.Join(got, ",") != "v1.1,v1.3" {
		t.Errorf("query versions = %v, want [v1.1 v1.3] in ascending order", got)
	}

	cam := byTarget["198.51.100.21:3212"]
	if cam.Senders != 2 || cam.Receivers != 1 {
		t.Errorf("cam-01 senders/receivers = %d/%d, want 2/1", cam.Senders, cam.Receivers)
	}
}

// TestVersionIsolationDetected is the check that caught six missing
// nodes on a real 45-node registry: a controller speaking only the
// highest minor sees a subset of the plant, and nothing errors.
func TestVersionIsolationDetected(t *testing.T) {
	res := loadPlant(t)
	fs := codes(res.Findings)["NMOS-QUERY-VERSION-ISOLATION"]
	if len(fs) != 1 {
		t.Fatalf("want 1 version-isolation finding, got %d", len(fs))
	}
	f := fs[0]
	if f.Severity != SevError {
		t.Errorf("severity = %s, want ERROR", f.Severity)
	}
	for _, want := range []string{"3", "v1.1", "1", "v1.3", "2 resource(s)"} {
		if !strings.Contains(f.Detail, want) {
			t.Errorf("detail %q missing %q", f.Detail, want)
		}
	}
}

// TestMulticastCollision proves the plant-wide check finds what neither
// device can see on its own.
func TestMulticastCollision(t *testing.T) {
	res := loadPlant(t)
	fs := codes(res.Findings)["NMOS-PLANT-MCAST-COLLISION"]
	if len(fs) != 1 {
		t.Fatalf("want 1 collision, got %d: %v", len(fs), fs)
	}
	if fs[0].Resource != "233.252.0.20:20000" {
		t.Errorf("collision group = %q", fs[0].Resource)
	}
	if fs[0].Severity != SevCritical {
		t.Errorf("severity = %s, want CRITICAL", fs[0].Severity)
	}
	// 233.252.0.30 is the mv-01 leg with rtp_enabled=false. A leg that
	// is not emitting cannot collide with anything.
	for _, f := range fs {
		if strings.Contains(f.Detail, "233.252.0.30") {
			t.Error("a leg with rtp_enabled=false was counted as an emitter")
		}
	}
}

// TestSenderWithNoDestination covers the failure that reads as healthy
// from every angle except the wire: master_enable true, activation
// succeeded, destination 0.0.0.0.
func TestSenderWithNoDestination(t *testing.T) {
	res := loadPlant(t)
	fs := codes(res.Findings)["NMOS-IS05-NO-DESTINATION"]
	if len(fs) != 1 {
		t.Fatalf("want 1 no-destination finding, got %d", len(fs))
	}
	if fs[0].Severity != SevCritical {
		t.Errorf("severity = %s, want CRITICAL", fs[0].Severity)
	}
	// One finding per device, naming the count and the senders — the
	// per-leg form produced 628 findings on one real 44-node capture.
	if !strings.Contains(fs[0].Detail, "55555555-5555-4555-8555-555555555555") {
		t.Errorf("the finding should name the sender: %q", fs[0].Detail)
	}
	if !strings.Contains(fs[0].Detail, "1 of 2 sender(s)") {
		t.Errorf("the finding should say how many of how many: %q", fs[0].Detail)
	}
}

// TestAllSendersIdleIsNotCritical is the distinction that makes the
// count worth reporting: the same observation on every sender of a
// device means the device is idle, not that it is broken.
func TestAllSendersIdleIsNotCritical(t *testing.T) {
	h := mkIdlePlant(t)
	fs := codes(checkIS05Active(h))["NMOS-IS05-NO-DESTINATION"]
	if len(fs) != 1 {
		t.Fatalf("want 1 grouped finding, got %d", len(fs))
	}
	if fs[0].Severity != SevInfo {
		t.Errorf("severity = %s, want INFO when every sender is idle", fs[0].Severity)
	}
	if !strings.Contains(fs[0].Hint, "idle") {
		t.Errorf("the hint should explain why this is not a fault: %q", fs[0].Hint)
	}
}

// TestSingleLegRedundancy: cam-01's iso sender is bound to two
// interfaces and stages one leg. The device is happy; the stream is
// unprotected.
func TestSingleLegRedundancy(t *testing.T) {
	res := loadPlant(t)
	fs := codes(res.Findings)["NMOS-2022-7-SINGLE-LEG"]
	if len(fs) != 1 {
		t.Fatalf("want 1 single-leg finding, got %d", len(fs))
	}
	if !strings.Contains(fs[0].Detail, "2 interfaces") {
		t.Errorf("detail should name the binding count: %q", fs[0].Detail)
	}
}

// TestDanglingFlowRef: cam-01's iso sender points at a flow the node
// does not publish, so a controller drops the sender entirely.
func TestDanglingFlowRef(t *testing.T) {
	res := loadPlant(t)
	fs := codes(res.Findings)["NMOS-IS04-DANGLING-REF"]
	if len(fs) == 0 {
		t.Fatal("want a dangling-reference finding")
	}
	found := false
	for _, f := range fs {
		if strings.Contains(f.Detail, "99999999-9999-4999-8999-999999999999") {
			found = true
		}
	}
	if !found {
		t.Errorf("the dangling flow_id was not reported: %v", fs)
	}
}

// TestCoreValidation: the registry lists a legacy gateway with neither
// a UUID nor a TAI version. Both are load-bearing for a controller.
func TestCoreValidation(t *testing.T) {
	res := loadPlant(t)
	c := codes(res.Findings)
	if len(c["NMOS-IS04-BAD-ID"]) != 1 {
		t.Errorf("want 1 bad-id finding, got %d", len(c["NMOS-IS04-BAD-ID"]))
	}
	if len(c["NMOS-IS04-BAD-VERSION"]) != 1 {
		t.Errorf("want 1 bad-version finding, got %d", len(c["NMOS-IS04-BAD-VERSION"]))
	}
}

// TestServerFaultsGrouped proves 2 identical 502s on the same endpoint
// shape produce 1 finding naming 2, not 2 findings. On a 176-sender
// device the ungrouped form is unreadable.
func TestServerFaultsGrouped(t *testing.T) {
	res := loadPlant(t)
	fs := codes(res.Findings)["NMOS-HTTP-SERVER-FAULT"]
	if len(fs) != 1 {
		t.Fatalf("want 1 grouped server-fault finding, got %d", len(fs))
	}
	if !strings.Contains(fs[0].Detail, "2 request(s)") {
		t.Errorf("faults were not counted: %q", fs[0].Detail)
	}
	if !strings.Contains(fs[0].Resource, "{id}") {
		t.Errorf("the endpoint shape should be UUID-collapsed: %q", fs[0].Resource)
	}
}

// TestUnreachableRegisteredNode: the registry advertises a node that
// answers nowhere. Controllers will keep offering it.
func TestUnreachableRegisteredNode(t *testing.T) {
	res := loadPlant(t)
	fs := codes(res.Findings)["NMOS-NODE-UNREACHABLE"]
	if len(fs) != 1 {
		t.Fatalf("want 1 unreachable-node finding, got %d", len(fs))
	}
	if !strings.Contains(fs[0].Detail, "legacy-gw") {
		t.Errorf("detail should name the node: %q", fs[0].Detail)
	}
}

// TestPagingFindings covers both defects a registry's cursor can have:
// re-serving rows, and never advancing.
func TestPagingFindings(t *testing.T) {
	res := loadPlant(t)
	c := codes(res.Findings)
	if len(c["NMOS-QUERY-PAGING-DUPES"]) != 1 {
		t.Errorf("want 1 paging-dupe finding, got %d", len(c["NMOS-QUERY-PAGING-DUPES"]))
	}
	if len(c["NMOS-QUERY-PAGING-STUCK"]) != 1 {
		t.Errorf("want 1 stuck-cursor finding, got %d", len(c["NMOS-QUERY-PAGING-STUCK"]))
	}
}

// TestBCPChecks covers the two profile rules a controller UI depends on.
func TestBCPChecks(t *testing.T) {
	res := loadPlant(t)
	c := codes(res.Findings)
	if len(c["NMOS-BCP002-HINT-ABSENT"]) == 0 {
		t.Error("want a group-hint finding — most fixture senders carry none")
	}
	if len(c["NMOS-BCP004-CAPS-EMPTY"]) == 0 {
		t.Error("want a caps finding — the fixture receiver declares no media_types")
	}
}

// TestControlAdvertisement: mv-01 serves /x-nmos/connection but its
// device advertises no sr-ctrl control, so a spec-following controller
// walking controls[] never finds the API.
func TestControlAdvertisement(t *testing.T) {
	res := loadPlant(t)
	fs := codes(res.Findings)["NMOS-IS04-CONTROL-MISSING"]
	if len(fs) != 1 {
		t.Fatalf("want 1 missing-control finding, got %d", len(fs))
	}
	if fs[0].Target != "198.51.100.22:3212" {
		t.Errorf("wrong device flagged: %s", fs[0].Target)
	}
}

// TestMinSeverityFilter proves --min-severity drops the inventory noise
// and keeps every real deviation.
func TestMinSeverityFilter(t *testing.T) {
	hs, err := Load(plantDir)
	if err != nil {
		t.Fatal(err)
	}
	all := Run(hs, Options{})
	warn := Run(hs, Options{MinSeverity: SevWarn})

	if len(warn.Findings) >= len(all.Findings) {
		t.Error("filtering to WARN should drop the INFO inventory lines")
	}
	for _, f := range warn.Findings {
		if f.Severity < SevWarn {
			t.Errorf("finding %s survived the filter at %s", f.Code, f.Severity)
		}
	}
	if warn.Counts["INFO"] != 0 {
		t.Errorf("INFO count = %d after filtering, want 0", warn.Counts["INFO"])
	}
}

// TestFindingsAreOrderedAndStable: two runs over the same bytes must
// produce byte-identical reports, or a diff between two exports is
// meaningless.
func TestFindingsAreOrderedAndStable(t *testing.T) {
	var a, b bytes.Buffer
	for i, w := range []*bytes.Buffer{&a, &b} {
		hs, err := Load(plantDir)
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if err := RenderText(w, Run(hs, Options{})); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	if a.String() != b.String() {
		t.Error("two audits of the same capture produced different reports")
	}

	res := loadPlant(t)
	last := SevCritical + 1
	for _, f := range res.Findings {
		if f.Severity > last {
			t.Fatalf("findings are not worst-first: %s after %s", f.Severity, last)
		}
		last = f.Severity
	}
}

// TestWorst reports the highest severity present, which is what a CLI
// turns into an exit code.
func TestWorst(t *testing.T) {
	res := loadPlant(t)
	got, any := res.Worst()
	if !any || got != SevCritical {
		t.Errorf("Worst() = %s,%v; want CRITICAL,true", got, any)
	}

	empty := Result{}
	if _, any := empty.Worst(); any {
		t.Error("an empty result should report no findings")
	}
}

// TestRenderJSONRoundTrip proves the machine formats carry the same
// findings the text report shows.
func TestRenderJSONRoundTrip(t *testing.T) {
	res := loadPlant(t)

	var jb bytes.Buffer
	if err := RenderJSON(&jb, res); err != nil {
		t.Fatal(err)
	}
	var back Result
	if err := json.Unmarshal(jb.Bytes(), &back); err != nil {
		t.Fatalf("audit JSON does not round-trip: %v", err)
	}
	if len(back.Findings) != len(res.Findings) {
		t.Errorf("round-trip lost findings: %d != %d", len(back.Findings), len(res.Findings))
	}
	if back.Findings[0].Severity.String() != res.Findings[0].Severity.String() {
		t.Error("severity did not survive the round trip")
	}

	var lb bytes.Buffer
	if err := RenderJSONL(&lb, res); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(lb.String(), "\n"), "\n")
	if len(lines) != len(res.Findings) {
		t.Errorf("JSONL wrote %d lines for %d findings", len(lines), len(res.Findings))
	}
	for i, ln := range lines {
		var f Finding
		if err := json.Unmarshal([]byte(ln), &f); err != nil {
			t.Fatalf("line %d is not a JSON object: %v", i, err)
		}
		if f.Code == "" {
			t.Errorf("line %d has no code", i)
		}
	}
}

// TestRenderTextContents pins the sections an operator opens the report
// for: the inventory first, then the deviations.
func TestRenderTextContents(t *testing.T) {
	var b bytes.Buffer
	if err := RenderText(&b, loadPlant(t)); err != nil {
		t.Fatal(err)
	}
	s := b.String()
	for _, want := range []string{
		"NMOS PLANT AUDIT", "INVENTORY", "API SURFACE", "FINDINGS", "SUMMARY",
		"cam-01", "mv-01", "site-registry-01",
		"NMOS-PLANT-MCAST-COLLISION", "query(v1.1,v1.3)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("report is missing %q", want)
		}
	}
	if strings.Index(s, "INVENTORY") > strings.Index(s, "FINDINGS") {
		t.Error("the inventory must come before the findings")
	}
}

// TestRenderTextNoFindings covers the clean-plant path.
func TestRenderTextNoFindings(t *testing.T) {
	var b bytes.Buffer
	if err := RenderText(&b, Result{Counts: map[string]int{}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "none at or above") {
		t.Error("an empty audit should say so explicitly")
	}
}

// failWriter fails after n successful writes, proving the renderer
// surfaces I/O errors instead of swallowing them.
type failWriter struct{ n int }

func (f *failWriter) Write(p []byte) (int, error) {
	if f.n <= 0 {
		return 0, os.ErrClosed
	}
	f.n--
	return len(p), nil
}

func TestRenderTextPropagatesWriteError(t *testing.T) {
	if err := RenderText(&failWriter{n: 2}, loadPlant(t)); err == nil {
		t.Error("RenderText should return the write error")
	}
	if err := RenderJSONL(&failWriter{n: 0}, loadPlant(t)); err == nil {
		t.Error("RenderJSONL should return the write error")
	}
}

func TestParseSeverity(t *testing.T) {
	for in, want := range map[string]Severity{
		"info": SevInfo, "INFO": SevInfo,
		"warn": SevWarn, "warning": SevWarn,
		"error": SevError, "err": SevError,
		"critical": SevCritical, " crit ": SevCritical,
	} {
		got, err := ParseSeverity(in)
		if err != nil || got != want {
			t.Errorf("ParseSeverity(%q) = %v,%v; want %v", in, got, err, want)
		}
	}
	if _, err := ParseSeverity("loud"); err == nil {
		t.Error("an unknown severity must be rejected")
	}
	if got := Severity(99).String(); got != "UNKNOWN" {
		t.Errorf("out-of-range severity = %q", got)
	}
}

func TestFindingString(t *testing.T) {
	f := Finding{Code: "X-1", Severity: SevWarn, Detail: "something", Resource: "sender/a"}
	s := f.String()
	for _, want := range []string{"WARN", "X-1", "something", "[sender/a]"} {
		if !strings.Contains(s, want) {
			t.Errorf("%q missing %q", s, want)
		}
	}
}

func TestVersionRankOrdersMinorsNumerically(t *testing.T) {
	// A plain string sort puts v1.10 before v1.9. A registry serving
	// ten minors would then be audited against the wrong "highest".
	got := sortedVersionsDesc([]string{"v1.9", "v1.10", "v1.2", "bogus"})
	want := []string{"v1.10", "v1.9", "v1.2", "bogus"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sortedVersionsDesc = %v, want %v", got, want)
		}
	}
}

func TestTrunc(t *testing.T) {
	if got := trunc("abcdef", 4); got != "abc…" {
		t.Errorf("trunc = %q", got)
	}
	if got := trunc("ab", 4); got != "ab" {
		t.Errorf("trunc should not pad: %q", got)
	}
	if got := trunc("abc", 1); got != "a" {
		t.Errorf("trunc at width 1 = %q", got)
	}
}

func TestHarvestName(t *testing.T) {
	if got := (&Harvest{Target: "h:1"}).Name(); got != "h:1" {
		t.Errorf("Name without a label should fall back to the target, got %q", got)
	}
	if got := (&Harvest{Target: "h:1", Label: "cam"}).Name(); got != "cam" {
		t.Errorf("Name = %q", got)
	}
}
