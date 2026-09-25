package consumer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dhsc "dhs/internal/consumer"
	dhsalarm "dhs/internal/consumer/alarm"
	"dhs/internal/manifest"
	"dhs/internal/snmp/codec"
	"dhs/internal/snmp/mib"
)

// ADR-0025 deliverable 4: the committed DM + manifest, walked once from
// the ATEME Kyrion DR5000 at 10.6.255.114 so everything below runs from
// the repo alone, with no agent.
//
// The fixture's job is to keep the three things that claim to describe
// this device honest about each other: the model the agent published,
// the compiled MIB tables that name it, and the alarm template written
// against those names. A MIB recompile that renames an object, or a
// typo in a rule, is silence at 03:00 — and silence is
// indistinguishable from health.
//
// Re-capture with:
//
//	SNMP_COMMUNITY=public dhs consumer snmp walk 10.6.255.114 --slot 0
//	cp .cache/dm/snmp/DR5000@1.3.1.1.json \
//	   internal/snmp/testdata/integration-test/dm/snmp/
//
// It takes about a quarter of an hour: the 4096-row programme table
// under Status.TsDescriptor is nearly the whole model. See
// docs/testbed.md.

const fixtureRoot = "../testdata/integration-test"

// dmFile is what the walk writes to the DM cache.
type dmFile struct {
	Model    string        `json:"model"`
	SwRev    string        `json:"sw_rev"`
	Protocol string        `json:"protocol"`
	Objects  []dhsc.Object `json:"objects"`
}

func loadFixture(t *testing.T) (*manifest.Manifest, *dmFile) {
	t.Helper()
	m, err := manifest.Load(filepath.Join(fixtureRoot, "manifest", "dr5000-test.json"))
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if len(m.Frames) != 1 || len(m.Frames[0].Slots) != 1 {
		t.Fatalf("an agent is one box in one frame: %+v", m.Frames)
	}
	ref := m.Frames[0].Slots[0].DM
	data, err := os.ReadFile(filepath.Join(fixtureRoot, "dm", "snmp", ref+".json"))
	if err != nil {
		t.Fatalf("the manifest names a DM the repo does not have: %v", err)
	}
	var dm dmFile
	if err := json.Unmarshal(data, &dm); err != nil {
		t.Fatalf("DM parse: %v", err)
	}
	return m, &dm
}

func TestTheFixtureIsTheAgentWeWalked(t *testing.T) {
	m, dm := loadFixture(t)

	if m.Device.Protocol != Name {
		t.Errorf("manifest protocol = %q", m.Device.Protocol)
	}
	if got := m.Device.Endpoints[0].Port; got != DefaultPort {
		t.Errorf("endpoint port = %d, want the agent's own %d", got, DefaultPort)
	}
	if m.Device.Endpoints[0].Transport != "udp" {
		t.Errorf("transport = %q — SNMP is a datagram protocol", m.Device.Endpoints[0].Transport)
	}
	// Identity is what a template and a DM cache are keyed by (ADR-0022):
	// the model the unit reports and the firmware it runs.
	if id := dm.Model + "@" + dm.SwRev; id != "DR5000@1.3.1.1" {
		t.Errorf("identity = %q", id)
	}
	if dm.Protocol != Name {
		t.Errorf("DM protocol = %q", dm.Protocol)
	}
	// The fixture is every branch this agent declares, with each group
	// capped — see the generator in ../integration/dmfixture_test.go
	// for why the two transport-stream tables are not in it. A fixture
	// with a few hundred objects would mean a branch went missing and
	// every check below would pass by not looking.
	if len(dm.Objects) < 2000 {
		t.Errorf("DM has %d objects — the fixture is ~2 800", len(dm.Objects))
	}

	// Every branch, not just the ones somebody happened to walk. The
	// first attempt at this fixture came from a single walk that died
	// partway through a 4096-row table, and silently had no Software,
	// no Hardware, no Network and no Status.Input at all.
	want := []string{
		"system",
		"ateme.dr5000.Unit",
		"ateme.dr5000.Software",
		"ateme.dr5000.Hardware",
		"ateme.dr5000.Network",
		"ateme.dr5000.Status.Input",
		"ateme.dr5000.Status.Decode",
		"ateme.dr5000.Status.TsDescriptor",
		"ateme.dr5000.Channel",
	}
	for _, branch := range want {
		found := false
		for _, o := range dm.Objects {
			if strings.HasPrefix(strings.Join(o.Path, ".")+".", branch+".") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("the fixture has nothing under %s", branch)
		}
	}
}

func TestNoTableBroughtItsBulkIntoTheRepository(t *testing.T) {
	// The cap is what makes a 94 000-object device committable. If it
	// stops being applied the fixture grows by two orders of magnitude
	// and nobody notices until the clone does.
	const cap = 32
	_, dm := loadFixture(t)
	rows := make(map[string]int, len(dm.Objects))
	for _, o := range dm.Objects {
		if len(o.Path) > 1 {
			rows[strings.Join(o.Path[:len(o.Path)-1], ".")]++
		}
	}
	for parent, n := range rows {
		if n > cap {
			t.Errorf("%s contributes %d objects, over the cap of %d", parent, n, cap)
		}
	}
}

func TestTheAlarmTemplateNamesObjectsTheAgentHas(t *testing.T) {
	// A path typo in a shipped template fires nothing, ever, and looks
	// exactly like a healthy device.
	_, dm := loadFixture(t)
	data, err := os.ReadFile(filepath.Join("..", "alarm", "DR5000@1.3.1.1.json"))
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := dhsalarm.Load(data)
	if err != nil {
		t.Fatalf("the shipped template must load: %v", err)
	}
	for _, r := range tpl.Rows {
		if r.Match == dhsalarm.CatchAll {
			continue // it matches everything by construction
		}
		matched := false
		for _, o := range dm.Objects {
			if dhsalarm.MatchPath(r.Match, o.Path) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("alarm rule %q matches no object on this agent", r.Match)
		}
	}
}

func TestEveryObjectTheAgentPublishedIsNamedByTheCompiledMIB(t *testing.T) {
	// The paths an operator types, an alarm rule matches and a dashboard
	// labels all come from the compiled tables. If a recompile drops or
	// renames a module, the walk still succeeds — it just starts
	// answering in numbers, and every rule written against a name stops
	// matching. That has to fail here, offline, and not in the field.
	_, dm := loadFixture(t)
	var unnamed, checked int
	for _, o := range dm.Objects {
		if o.OID == "" {
			t.Fatalf("an object with no OID: %+v", o)
		}
		oid, err := codec.ParseOID(o.OID)
		if err != nil {
			t.Fatalf("the DM carries an OID that does not parse (%q): %v", o.OID, err)
		}
		checked++
		if name := mib.Name(oid); name == oid.String() {
			if unnamed < 5 {
				t.Errorf("no compiled name for %s (path %s)", o.OID, strings.Join(o.Path, "."))
			}
			unnamed++
		}
	}
	if unnamed > 0 {
		t.Errorf("%d of %d objects have no name in the compiled tables", unnamed, checked)
	}
}

func TestEveryDMObjectCarriesWhatAViewNeeds(t *testing.T) {
	// The DM is what a cold `watch`, an offline `tree` and an alarm
	// template read. The fields they print have to be in it.
	_, dm := loadFixture(t)
	var (
		withEnum, readOnly int
		paths              = make(map[string]bool, len(dm.Objects))
	)
	for _, o := range dm.Objects {
		if len(o.Path) == 0 {
			t.Fatalf("an object with no path: %+v", o)
		}
		p := strings.Join(o.Path, ".")
		if paths[p] {
			t.Errorf("two objects share the path %q — one of them is unreachable", p)
		}
		paths[p] = true
		if len(o.EnumItems) > 0 {
			withEnum++
		}
		if o.Access&2 == 0 {
			readOnly++
		}
	}
	// An enumeration is what turns 1 into "true" and 2 into "hdsdi" —
	// the words the manual prints and the rules are written against.
	if withEnum == 0 {
		t.Error("no enumerated object: the MIB's enumerations are not being applied")
	}
	if readOnly == 0 {
		t.Error("no read-only object: the access bits are not being set")
	}
}
