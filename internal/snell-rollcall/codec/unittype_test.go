package codec

import (
	"sort"
	"strings"
	"testing"
)

// TestUnitTypes_Generated checks the table was generated from the vendor
// database rather than stubbed, and that it holds the invariant the lookup
// depends on: sorted by id, with no duplicates.
func TestUnitTypes_Generated(t *testing.T) {
	if n := UnitTypeCount(); n < 700 {
		t.Fatalf("the table holds %d types; the vendor database has around 770", n)
	}

	if !sort.SliceIsSorted(unitTypes[:], func(i, j int) bool {
		return unitTypes[i].ID < unitTypes[j].ID
	}) {
		t.Fatal("the table is not sorted by id, so the binary search is wrong")
	}
	for i := 1; i < len(unitTypes); i++ {
		if unitTypes[i].ID == unitTypes[i-1].ID {
			t.Fatalf("id %d appears twice", unitTypes[i].ID)
		}
	}

	// Every entry carries at least an enum, or it is a hole in the data.
	for _, ty := range unitTypes {
		if ty.Enum == "" {
			t.Errorf("id %d has no vendor enum", ty.ID)
		}
	}
}

// TestLookupUnitType covers the ids this connector actually reasons about,
// with values taken from the vendor database.
func TestLookupUnitType(t *testing.T) {
	tests := []struct {
		id       uint16
		enum     string
		name     string
		category UnitCategory
	}{
		{1, "RD1_ADC", "IQD1ADC", CategoryIQModularInfrastructureProducts},
		{483, "ID_RC32_ROUTING_IPSH_CLIENT", "RC32 Rout. IPSh Cli", CategoryControlMonitoringProducts},
		{623, "ID_5915_CHANNEL", "5915 Embedded Audio", CategoryRoutingProducts},
		{636, "ID_ROUTER_MATRIX", "Router Matrix", CategoryRoutingProducts},
	}
	for _, tc := range tests {
		got, ok := LookupUnitType(tc.id)
		if !ok {
			t.Errorf("id %d not found", tc.id)
			continue
		}
		if got.Enum != tc.enum || got.Name != tc.name || got.Category != tc.category {
			t.Errorf("id %d = %+v, want enum %q name %q category %v",
				tc.id, got, tc.enum, tc.name, tc.category)
		}
	}
}

// TestLookupUnitType_Unknown covers ids the database does not hold. The vendor
// allocates new ones with every product, so an unnamed device is expected and
// must still be usable.
func TestLookupUnitType_Unknown(t *testing.T) {
	for _, id := range []uint16{0, 9999, 0xFFFF} {
		if _, ok := LookupUnitType(id); ok {
			t.Errorf("id %d should not be in the database", id)
		}
		if got := UnitTypeName(id); got == "" {
			t.Errorf("UnitTypeName(%d) returned an empty label", id)
		}
	}
	if got := UnitTypeName(9999); !strings.Contains(got, "9999") {
		t.Errorf("UnitTypeName(9999) = %q, want it to name the number", got)
	}
}

// TestLookupUnitType_EveryEntryIsReachable checks the binary search against
// the whole table, which is the only way to catch an ordering mistake that
// happens to leave the first and last entries findable.
func TestLookupUnitType_EveryEntryIsReachable(t *testing.T) {
	for _, want := range unitTypes {
		got, ok := LookupUnitType(want.ID)
		if !ok {
			t.Fatalf("id %d is in the table but not findable", want.ID)
		}
		if got != want {
			t.Fatalf("id %d found %+v, want %+v", want.ID, got, want)
		}
	}
}

func TestUnitTypeName(t *testing.T) {
	if got := UnitTypeName(636); got != "Router Matrix" {
		t.Errorf("UnitTypeName(636) = %q", got)
	}
	// A label is always produced, whatever the entry holds.
	for _, ty := range unitTypes {
		if UnitTypeName(ty.ID) == "" {
			t.Fatalf("id %d produced an empty label", ty.ID)
		}
	}
}

// TestUnitType_Label covers the fallbacks. Every entry in the database as it
// stands has a name, so these paths cannot be reached through the table; they
// exist because the table is vendor data we regenerate, and the file's Vistek
// section already contains entries with no name at all.
func TestUnitType_Label(t *testing.T) {
	tests := []struct {
		name string
		in   UnitType
		want string
	}{
		{"named", UnitType{ID: 636, Name: "Router Matrix", Description: "x"}, "Router Matrix"},
		{"description only", UnitType{ID: 78, Description: "V606 Rack Controller Module"},
			"V606 Rack Controller Module"},
		{"neither", UnitType{ID: 1234}, "unit type 1234"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.Label(); got != tc.want {
				t.Errorf("Label() = %q, want %q", got, tc.want)
			}
		})
	}

	// Every entry in the table today has a name, so Label is its name.
	for _, ty := range unitTypes {
		if ty.Label() != ty.Name {
			t.Fatalf("id %d labels as %q but is named %q", ty.ID, ty.Label(), ty.Name)
		}
	}
}

// TestUnitType_RouterMatrix records what the router-matrix id does and does
// not tell us. It is a reason to probe command 100; it is not proof that the
// routing interface is present, which the audit oracle demonstrated by
// reporting this id and rejecting that command.
func TestUnitType_RouterMatrix(t *testing.T) {
	if !IsRouterMatrix(TypeIDRouterMatrix) {
		t.Error("the router matrix id must be recognised")
	}
	if IsRouterMatrix(TypeIDRoutingIPShareClient) {
		t.Error("a routing client is not a router matrix")
	}

	ty, ok := LookupUnitType(TypeIDRouterMatrix)
	if !ok || ty.Category != CategoryRoutingProducts {
		t.Errorf("the router matrix is %+v, want a routing product", ty)
	}
}

func TestUnitTypesByCategory(t *testing.T) {
	routing := UnitTypesByCategory(CategoryRoutingProducts)
	if len(routing) == 0 {
		t.Fatal("no routing products in the table")
	}
	for _, ty := range routing {
		if ty.Category != CategoryRoutingProducts {
			t.Errorf("id %d is category %v", ty.ID, ty.Category)
		}
	}
	if !sort.SliceIsSorted(routing, func(i, j int) bool { return routing[i].ID < routing[j].ID }) {
		t.Error("results must keep the table's order")
	}

	// The category totals must account for every entry.
	total := 0
	for c := UnitCategory(0); c <= 8; c++ {
		total += len(UnitTypesByCategory(c))
	}
	if total != UnitTypeCount() {
		t.Errorf("categories account for %d of %d types", total, UnitTypeCount())
	}
}

func TestFindUnitTypes(t *testing.T) {
	got := FindUnitTypes("sirius 830")
	if len(got) == 0 {
		t.Fatal("Sirius 830 should be findable by name")
	}
	if got[0].ID != 606 {
		t.Errorf("found id %d, want 606", got[0].ID)
	}

	// The search covers the enum and the description as well as the name.
	if len(FindUnitTypes("ID_ROUTER_MATRIX")) == 0 {
		t.Error("a vendor enum should be searchable")
	}
	if len(FindUnitTypes("ROUTER MATRIX")) == 0 {
		t.Error("the search must be case-insensitive")
	}

	if got := FindUnitTypes(""); got != nil {
		t.Errorf("an empty query returned %d results, want none", len(got))
	}
	if got := FindUnitTypes("   "); got != nil {
		t.Errorf("a blank query returned %d results, want none", len(got))
	}
	if got := FindUnitTypes("no such product anywhere"); got != nil {
		t.Errorf("an unmatched query returned %d results", len(got))
	}
}

// TestUnitType_VistekCollision records a genuine collision in the vendor's own
// database: three ids appear in both its RollCall and Vistek sections naming
// different products. The vendor's generator takes the RollCall section, so we
// do too, and this test pins that choice rather than leaving it implicit.
func TestUnitType_VistekCollision(t *testing.T) {
	tests := []struct {
		id   uint16
		enum string
	}{
		{78, "ID_MDAVE_1"},
		{606, "ID_SIRIUS_830"},
		{608, "ID_SIRIUS_850"},
	}
	for _, tc := range tests {
		got, ok := LookupUnitType(tc.id)
		if !ok {
			t.Errorf("id %d is missing", tc.id)
			continue
		}
		if got.Enum != tc.enum {
			t.Errorf("id %d is %q, want the RollCall section's %q", tc.id, got.Enum, tc.enum)
		}
		if got.Category == CategoryVistekModularProducts {
			t.Errorf("id %d took the Vistek entry", tc.id)
		}
	}
}

func TestUnitCategory_String(t *testing.T) {
	tests := []struct {
		in   UnitCategory
		want string
	}{
		{CategoryUnknown, "unknown"},
		{CategoryRoutingProducts, "Routing Products"},
		{CategoryControlMonitoringProducts, "Control & Monitoring Products"},
		{99, "category(99)"},
	}
	for _, tc := range tests {
		if got := tc.in.String(); got != tc.want {
			t.Errorf("UnitCategory(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestUnitType_String(t *testing.T) {
	ty, _ := LookupUnitType(636)
	got := ty.String()
	for _, want := range []string{"636", "Router Matrix", "Sirius 800"} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q, want it to contain %q", got, want)
		}
	}

	if got := (UnitType{}).String(); got != "unknown" {
		t.Errorf("the zero value renders as %q", got)
	}
	// A type whose description repeats its name is not printed twice.
	same := UnitType{ID: 1, Name: "X", Description: "X"}
	if got := same.String(); strings.Count(got, "X") != 1 {
		t.Errorf("String() = %q, want the name once", got)
	}
}

// TestUnitType_IdentityIDs pins the two ids this connector reports and probes
// for, so a change to either is deliberate.
func TestUnitType_IdentityIDs(t *testing.T) {
	if TypeIDNone != 0 {
		t.Errorf("the null id is %d, want 0", TypeIDNone)
	}
	for _, id := range []uint16{TypeIDRouterMatrix, TypeIDRoutingIPShareClient} {
		if _, ok := LookupUnitType(id); !ok {
			t.Errorf("id %d is named in code but absent from the database", id)
		}
	}
}
