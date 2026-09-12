package codec

import "testing"

// Every identity below is what a real device said about itself: the IQ frame
// at 10.6.255.113 answered its port list with these type ids and versions, and
// the committed DM for its Nodal card is filed as IQDBE00@5.0.cs5.

func TestDMKeyNamesACardAsItsDMIsFiled(t *testing.T) {
	got := DMKey(562, Version{Major: 5, Minor: 0, Alpha: ' ', CmdSet: 5})
	if got != "IQDBE00@5.0.cs5" {
		t.Errorf("DMKey = %q, want the name the committed DM carries", got)
	}
}

func TestParseDMKeyReadsBackWhatTheFrameReported(t *testing.T) {
	for _, c := range []struct {
		key string
		id  uint16
		v   Version
	}{
		{"IQDBE00@5.0.cs5", 562, Version{Major: 5, Minor: 0, Alpha: ' ', CmdSet: 5}},
		{"IQMUX42@8.5.cs17", 389, Version{Major: 8, Minor: 5, Alpha: ' ', CmdSet: 17}},
		{"IQH3UM4-S@5.25.cs21", 429, Version{Major: 5, Minor: 25, Alpha: ' ', CmdSet: 21}},
	} {
		id, v, ok := ParseDMKey(c.key)
		if !ok || id != c.id || v != c.v {
			t.Errorf("ParseDMKey(%q) = %d %+v %v, want %d %+v", c.key, id, v, ok, c.id, c.v)
		}
	}
}

func TestAnUnlistedTypeIsFiledAndReadByItsNumber(t *testing.T) {
	// The vendor allocates ids with every product, so a card our table has
	// never heard of is still filed — by its number, which identifies the
	// model as well as a name would.
	key := DMKey(0xFFFE, Version{Major: 1, Alpha: ' ', CmdSet: 1})
	if key != "unit-type-65534@1.0.cs1" {
		t.Fatalf("DMKey = %q", key)
	}
	id, _, ok := ParseDMKey(key)
	if !ok || id != 0xFFFE {
		t.Errorf("ParseDMKey(%q) = %d %v", key, id, ok)
	}
}

func TestEveryTypeInTheTableRoundTripsOrIsRefused(t *testing.T) {
	// The filename-safe form throws punctuation away, so two names can reduce
	// to one token. Those have to be refused rather than resolved to whichever
	// came first in the table; every other type must come back as itself.
	tokens := map[string]int{}
	for _, ty := range unitTypes {
		tokens[dmToken(ty.Label())]++
	}

	v := Version{Major: 1, Minor: 2, Alpha: ' ', CmdSet: 3}
	refused := 0
	for _, ty := range unitTypes {
		key := DMKey(ty.ID, v)
		id, got, ok := ParseDMKey(key)
		token := dmToken(ty.Label())
		if token == "" || tokens[token] > 1 {
			if ok {
				t.Errorf("%q names %d types and was read as %d", key, tokens[token], id)
			}
			refused++
			continue
		}
		if !ok || id != ty.ID || got != v {
			t.Errorf("ParseDMKey(%q) = %d %+v %v, want %d", key, id, got, ok, ty.ID)
		}
	}
	t.Logf("%d of %d types share a filename-safe name with another and are refused",
		refused, len(unitTypes))
}

func TestParseDMKeyRefusesWhatDMKeyCannotHaveWritten(t *testing.T) {
	for _, key := range []string{
		"",
		"IQDBE00",             // no revision at all
		"@5.0.cs5",            // no model
		"IQDBE00@",            // no revision after the separator
		"IQDBE00@5.0",         // too few parts
		"IQDBE00@5.0.5",       // no command-set marker
		"IQDBE00@x.0.cs5",     // not numbers
		"IQDBE00@5.x.cs5",     //
		"IQDBE00@5.0.csx",     //
		"IQDBE00@5.0.cs256",   // wider than the byte it travels in
		"IQDBE00@05.0.cs5",    // the same number, but not how DMKey writes it
		"NOT-A-CARD@5.0.cs5",  // a name no type has
		"unit-type-x@1.0.cs1", // a number that is not one
		"unit-type-0065534@1.0.cs1",
		"unit-type-562@5.0.cs5", // a listed type is filed by its name, never its number
	} {
		if _, _, ok := ParseDMKey(key); ok {
			t.Errorf("ParseDMKey(%q) accepted a key DMKey never writes", key)
		}
	}
}

func TestATokenKeepsOnlyWhatAFilenameCanCarry(t *testing.T) {
	// The vendor table names carry spaces, dots and slashes, and the key
	// becomes a path under .cache/dm. These cases moved here from the
	// consumer along with the function they pin.
	for _, tc := range []struct{ in, want string }{
		{"4929 AES O/P card", "4929-AES-O-P-card"},
		{"RC32 Rout. IPSh Cli", "RC32-Rout--IPSh-Cli"},
		{"IQH3UM4-S", "IQH3UM4-S"},
		{"plain", "plain"},
		{"--edges--", "edges"},
		{"/weird_name./", "weird_name"},
		{"", ""},
	} {
		if got := dmToken(tc.in); got != tc.want {
			t.Errorf("dmToken(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
