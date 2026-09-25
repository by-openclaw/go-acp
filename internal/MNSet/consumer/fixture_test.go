package mnset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/consumer/alarm"
	"dhs/internal/manifest"
)

// ADR-0025 deliverable 4: the committed DM + manifest, captured once
// from the live FusioN6 (10.6.40.53) so everything below runs from the
// repo alone. The producer is parked for this connector (ADR-0025
// per-protocol scope), so the fixture's job is not to feed an emulator
// — it is to keep the three things that claim to describe this module
// honest about each other: the DM, the poll plan in the dictionary,
// and the alarm template.
//
// Re-capture with:
//
//	dhs consumer mnset walk 10.6.40.53 --slot 0
//	cp .cache/dm/mnset/FusioN6@0x68cd783f.json \
//	   internal/MNSet/testdata/integration-test/dm/mnset/

const fixtureRoot = "../testdata/integration-test"

// dmFile is the DM the manifest's only slot attaches.
type dmFile struct {
	Model    string            `json:"model"`
	SwRev    string            `json:"sw_rev"`
	Protocol string            `json:"protocol"`
	Objects  []consumer.Object `json:"objects"`
}

func loadFixture(t *testing.T) (*manifest.Manifest, *dmFile) {
	t.Helper()
	m, err := manifest.Load(filepath.Join(fixtureRoot, "manifest", "fusion6-test.json"))
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if len(m.Frames) != 1 || len(m.Frames[0].Slots) != 1 {
		t.Fatalf("the fixture is one module in one frame: %+v", m.Frames)
	}
	ref := m.Frames[0].Slots[0].DM
	data, err := os.ReadFile(filepath.Join(fixtureRoot, "dm", "mnset", ref+".json"))
	if err != nil {
		t.Fatalf("the manifest names a DM the repo does not have: %v", err)
	}
	var dm dmFile
	if err := json.Unmarshal(data, &dm); err != nil {
		t.Fatalf("DM parse: %v", err)
	}
	return m, &dm
}

func TestTheFixtureIsTheModuleWeWalked(t *testing.T) {
	m, dm := loadFixture(t)

	if m.Device.Protocol != Name {
		t.Errorf("manifest protocol = %q", m.Device.Protocol)
	}
	if got := m.Device.Endpoints[0].Port; got != DefaultPort {
		t.Errorf("endpoint port = %d, want the module's own %d", got, DefaultPort)
	}
	if dm.Model != "FusioN6" || dm.Protocol != Name || dm.SwRev == "" {
		t.Errorf("DM identity = %s@%s (%s)", dm.Model, dm.SwRev, dm.Protocol)
	}
	// The module publishes its whole tree; a fixture with a few hundred
	// objects would mean the walk was cut short when it was captured.
	if len(dm.Objects) < 7000 {
		t.Errorf("DM has %d objects — the FusioN6 walk is ~7 200", len(dm.Objects))
	}
	// Identity is what a template and a DM cache are keyed by (ADR-0022).
	if id := dm.Model + "@" + dm.SwRev; id != "FusioN6@0x68cd783f" {
		t.Errorf("identity = %q", id)
	}
}

func TestThePollPlanNamesLeavesTheModuleHas(t *testing.T) {
	// A pattern that matches nothing is a rule nobody will ever see
	// fire, and the only way to catch it without a device is here.
	_, dm := loadFixture(t)
	d, _, err := parseDictionary(fusion6Dictionary, videoFormatsJSON)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range d.Poll.Entries {
		matched := false
		for _, o := range dm.Objects {
			if matchPath(e.Match, o.Path) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("poll plan pattern %q matches no object on the module", e.Match)
		}
	}
}

func TestTheDictionaryDescribesLeavesTheModuleHas(t *testing.T) {
	// The same class of silent miss, one layer up: a metadata pattern
	// that matches nothing fills no unit and no description, and the
	// only symptom is a blank column nobody notices.
	_, dm := loadFixture(t)
	d, _, err := parseDictionary(fusion6Dictionary, videoFormatsJSON)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range d.Entries {
		matched := false
		for _, o := range dm.Objects {
			if matchPath(e.Match, o.Path) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("dictionary entry %q describes no object on the module", e.Match)
		}
	}
}

func TestTheAlarmTemplateNamesLeavesTheModuleHas(t *testing.T) {
	// Same check for the shipped rules: a path typo in a template is
	// silence at 03:00, and silence is indistinguishable from health.
	_, dm := loadFixture(t)
	data, err := os.ReadFile(filepath.Join("..", "alarm", "fusion6.alarm.json"))
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := alarm.Load(data)
	if err != nil {
		t.Fatalf("the shipped template must load: %v", err)
	}
	for _, r := range tpl.Rows {
		if r.Match == alarm.CatchAll {
			continue // it matches everything by construction
		}
		matched := false
		for _, o := range dm.Objects {
			if alarm.MatchPath(r.Match, o.Path) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("alarm rule %q matches no object on the module", r.Match)
		}
	}
}

func TestEveryDMObjectCarriesWhatAViewNeeds(t *testing.T) {
	// The DM is what a cold `watch` and an offline `tree` read, so the
	// fields they print have to be in it.
	_, dm := loadFixture(t)
	var (
		withUnit, withEnum, readOnly int
		paths                        = map[string]bool{}
	)
	for _, o := range dm.Objects {
		if len(o.Path) == 0 {
			t.Fatalf("an object with no path: %+v", o)
		}
		p := strings.Join(o.Path, ".")
		if paths[p] {
			t.Errorf("two objects share the path %q", p)
		}
		paths[p] = true
		if o.Unit != "" {
			withUnit++
		}
		if len(o.EnumItems) > 0 {
			withEnum++
		}
		if o.Access&2 == 0 {
			readOnly++
		}
	}
	// The dictionary's job is to fill these in; if it stopped doing so
	// the DM would still walk and every display would go blank.
	if withUnit == 0 || withEnum == 0 {
		t.Errorf("DM carries %d units and %d enumerations — the dictionary is not being applied", withUnit, withEnum)
	}
	if readOnly == 0 {
		t.Error("no read-only object: the access bits are not being set")
	}
}
