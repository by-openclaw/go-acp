package registers

// Values are transcribed from the AMWA capabilities register, never
// from an encoder. These pin the typed model and the introspection
// contract the CLI + bcp00401 depend on.

import "testing"

func TestLookupTypedConstraints(t *testing.T) {
	// component_depth is a bounded integer 8..16.
	p, ok := Lookup("urn:x-nmos:cap:format:component_depth")
	if !ok {
		t.Fatal("component_depth not in the register")
	}
	if p.Kind != KindInteger || p.Min == nil || *p.Min != 8 || p.Max == nil || *p.Max != 16 {
		t.Fatalf("component_depth constraint = %+v, want integer 8..16", p)
	}
	if p.Register != "capabilities" || p.RegisterVersion != "v1.0" {
		t.Errorf("provenance = %s %s, want capabilities v1.0", p.Register, p.RegisterVersion)
	}

	// color_sampling is an enum; a listed value is accepted, a bogus one not.
	cs, _ := Lookup("urn:x-nmos:cap:format:color_sampling")
	if cs.Kind != KindEnum {
		t.Fatalf("color_sampling kind = %s, want enum", cs.Kind)
	}
	if !cs.Accepts("YCbCr-4:2:2") {
		t.Error("color_sampling must accept YCbCr-4:2:2")
	}
	if cs.Accepts("YCbCr-9:9:9") {
		t.Error("color_sampling must reject a nonsense value")
	}

	// grain_rate is rational; sample_rate too.
	if gr, _ := Lookup("urn:x-nmos:cap:format:grain_rate"); gr.Kind != KindRational {
		t.Errorf("grain_rate kind = %s, want rational", gr.Kind)
	}

	// meta:enabled is boolean.
	if en, _ := Lookup("urn:x-nmos:cap:meta:enabled"); en.Kind != KindBoolean {
		t.Errorf("meta:enabled kind = %s, want boolean", en.Kind)
	}
}

func TestKnownAndUnknown(t *testing.T) {
	if !Known("urn:x-nmos:cap:transport:st2110_21_sender_type") {
		t.Error("a real transport cap URN must be Known")
	}
	if Known("urn:x-nmos:cap:format:teleportation") {
		t.Error("an invented cap URN must not be Known")
	}
}

func TestAllSortedAndProvenanced(t *testing.T) {
	all := All()
	if len(all) < 15 {
		t.Fatalf("register has %d params, expected the capabilities set", len(all))
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].URN > all[i].URN {
			t.Fatalf("All() not URN-sorted at %d: %s > %s", i, all[i-1].URN, all[i].URN)
		}
	}
	for _, p := range all {
		if p.Register == "" || p.RegisterVersion == "" {
			t.Errorf("%s lacks provenance", p.URN)
		}
	}
}
