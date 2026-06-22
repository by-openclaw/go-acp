// Package cerebrumfixtures holds the codec-generated 0v16 wire fixtures
// for the EVS Cerebrum Northbound API connector and the drift-guard that
// keeps them byte-identical to what the codec emits.
//
// Mirrors internal/probel-sw02p/integration/fixture_test.go: a single
// table-driven test regenerates every fixture from the codec encoders and
// asserts byte-equality against the committed file. Run with
// DHS_REGEN_FIXTURES=1 to (re)write the fixtures after an intentional
// codec change:
//
//	$env:DHS_REGEN_FIXTURES=1; go test ./internal/cerebrum-nb/testdata/fixtures/ -run TestFixtures
//	Remove-Item Env:DHS_REGEN_FIXTURES ; go test ./internal/cerebrum-nb/testdata/fixtures/ -run TestFixtures
//
// CONSTRAINT (ADR-0025 / session brief): every byte here is produced by a
// codec encoder — never hand-written, never from a live capture. There is
// no live Cerebrum peer (NB licence missing) and no emulator, so the codec
// is the ONLY authority for real 0v16 wire bytes. The RX-only frames
// (login_reply, RESULT, CONTINUE, WILDCARD_COMPLETE, *_change, datastore
// DATA) are produced by feeding a spec-verbatim 0v16 XML string (page-cited
// in codec_0v16_test.go) through codec.Decode and re-serialising the parsed
// AST via (*Element).String(), so the committed bytes are still the codec's
// own output, not a hand-typed document.
package cerebrumfixtures

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"dhs/internal/cerebrum-nb/codec"
)

// fixtureDir is where the .xml fixtures live (this package's own dir).
const fixtureDir = "."

// fixture is one named wire frame plus the codec call that produces it.
// gen() must be deterministic — same bytes every run — so the drift-guard
// is meaningful.
type fixture struct {
	// file is the committed fixture filename (under fixtureDir).
	file string
	// how documents which codec entry-point produced the bytes, for the
	// README + the brief's "how each frame was generated" report.
	how string
	// gen returns the exact wire bytes. TX frames come straight from an
	// Encode* call; RX frames are round-tripped through Decode + AST
	// re-serialisation (still codec output — see package doc).
	gen func() []byte
}

// rxRoundTrip decodes a spec-verbatim 0v16 XML string and re-serialises the
// parsed AST deterministically. The committed bytes are therefore the
// codec's own decode output (every element name, attribute name + value,
// and nesting comes from codec.Decode of the case-folded AST), not the
// hand-typed input string. The input strings are the page-cited 0v16
// worked examples already pinned byte-for-byte in
// codec/codec_0v16_test.go, so they are spec-authoritative, and Decode is
// the real RX path the consumer runs.
//
// We do NOT use (*codec.Element).String() here: it iterates the Attrs map
// in Go's randomised order, so it is not byte-stable across runs and would
// make the drift-guard meaningless. renderElement walks the same public
// codec.Element AST but emits attributes in sorted-key order, which is
// deterministic. (TX fixtures need no such treatment — the Encode* path
// uses an ordered AttrsBuilder and is already deterministic.)
func rxRoundTrip(t *testing.T, specXML string) []byte {
	t.Helper()
	f, err := codec.Decode([]byte(specXML))
	if err != nil {
		t.Fatalf("decode spec example: %v", err)
	}
	if f.Root == nil {
		t.Fatalf("decode produced nil root for %q", specXML)
	}
	var b strings.Builder
	renderElement(&b, f.Root)
	return []byte(b.String())
}

// renderElement serialises a decoded codec.Element with attributes in
// sorted-key order so the output is byte-deterministic. Only the public
// fields of codec.Element are read — the content is entirely the codec's
// decode product.
func renderElement(b *strings.Builder, e *codec.Element) {
	b.WriteByte('<')
	b.WriteString(e.Name)
	keys := make([]string, 0, len(e.Attrs))
	for k := range e.Attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteByte(' ')
		b.WriteString(k)
		b.WriteString(`="`)
		xmlEscapeAttr(b, e.Attrs[k])
		b.WriteByte('"')
	}
	if len(e.Children) == 0 && e.Text == "" {
		b.WriteString("/>")
		return
	}
	b.WriteByte('>')
	for _, c := range e.Children {
		renderElement(b, c)
	}
	if e.Text != "" {
		xmlEscapeText(b, e.Text)
	}
	b.WriteString("</")
	b.WriteString(e.Name)
	b.WriteByte('>')
}

// xmlEscapeAttr / xmlEscapeText mirror the codec's own escaping so the
// rendered bytes match what the wire codec would emit for the same values.
func xmlEscapeAttr(b *strings.Builder, s string) {
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		case '\n':
			b.WriteString("&#10;")
		case '\r':
			b.WriteString("&#13;")
		case '\t':
			b.WriteString("&#9;")
		default:
			b.WriteRune(r)
		}
	}
}

func xmlEscapeText(b *strings.Builder, s string) {
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		default:
			b.WriteRune(r)
		}
	}
}

// fixtures is the full set. Order is documentary only.
func fixtures(t *testing.T) []fixture {
	t.Helper()
	return []fixture{
		// ---- TX (direct codec encoders) -----------------------------
		{
			file: "tx_login.xml",
			how:  "codec.EncodeLogin(1, \"admin\", \"s3cr3t\") — §2.1 LOGIN",
			gen:  func() []byte { return codec.EncodeLogin(1, "admin", "s3cr3t") },
		},
		{
			file: "tx_device_config_add_generic.xml",
			how:  "codec.EncodeDeviceConfiguration(7, GENERIC ADD) — §4.5.1.1 p20",
			gen: func() []byte {
				return codec.EncodeDeviceConfiguration(7, &codec.DeviceConfiguration{
					Type:       codec.DeviceConfigAdd,
					IPAddress:  "10.10.10.1",
					DeviceType: codec.ConfigDeviceGeneric,
					Generic: &codec.GenericConfig{
						Device:  "Shotoku STAR Protocol",
						Version: "Latest",
						Name:    "Shotoku STAR Protocol",
						Protocol: &codec.ProtocolConfiguration{
							ConnectionType: codec.ConnTCP,
							PortNumber:     "8000",
							Timeout:        "5000",
							PollPeriod:     "3000",
						},
					},
				})
			},
		},
		{
			file: "tx_action_route.xml",
			how:  "codec.EncodeAction(2, &RoutingAction{TYPE:ROUTE}) — §4.1.1 routing change",
			gen: func() []byte {
				return codec.EncodeAction(2, &codec.RoutingAction{
					Type:        "ROUTE",
					DeviceName:  "MTX1",
					DeviceType:  codec.DeviceType("ROUTER"),
					SrceID:      "60",
					DestID:      "60",
					SrceLevelID: "1",
					DestLevelID: "1",
				})
			},
		},
		{
			file: "tx_action_salvo_run.xml",
			how:  "codec.EncodeAction(3, &SalvoAction{TYPE:RUN}) — §4.3 salvo change",
			gen: func() []byte {
				return codec.EncodeAction(3, &codec.SalvoAction{
					Type:     "RUN",
					Group:    "Multiviewer 1",
					Instance: "Line up",
				})
			},
		},
		{
			file: "tx_action_category_create.xml",
			how:  "codec.EncodeAction(4, &CategoryAction{TYPE:CREATE}) — §4.2 category change",
			gen: func() []byte {
				return codec.EncodeAction(4, &codec.CategoryAction{
					Type:        "CREATE",
					Category:    "EVS Sources",
					Description: "All EVS Sources",
				})
			},
		},
		{
			file: "tx_obtain_datastore.xml",
			how:  "codec.EncodeObtain(5, &DatastoreChange{Path:…}) — §5.5.1 p30 (emits PATH)",
			gen: func() []byte {
				return codec.EncodeObtain(5, []codec.SubItem{
					&codec.DatastoreChange{Path: `Global Data Files\Index.xml`, MTID: "5"},
				})
			},
		},

		// ---- RX (Decode → canonical AST re-serialisation) -----------
		// Input strings are the page-cited 0v16 worked examples already
		// pinned in codec/codec_0v16_test.go.
		{
			file: "rx_login_reply_datastores.xml",
			how:  "codec.Decode(§2.1 p7 LOGIN_REPLY+DATASTORES).Root.String()",
			gen: func() []byte {
				return rxRoundTrip(t, `<LOGIN_REPLY MTID="1" API_VER="0.1">`+
					`<DATASTORES>`+
					`<DATASTORE NAME="Global Data Files\Index.xml" AVAILABLE="1"/>`+
					`<DATASTORE NAME="Data Files\Device.xml" AVAILABLE="0"/>`+
					`</DATASTORES>`+
					`</LOGIN_REPLY>`)
			},
		},
		{
			file: "rx_device_config_result_accepted.xml",
			how:  "codec.Decode(§4.5.1.1 p20 DEVICE_CONFIGURATION RESULT).Root.String()",
			gen: func() []byte {
				return rxRoundTrip(t, `<DEVICE_CONFIGURATION TYPE="ADD" IP_ADDRESS="10.10.10.1" DEVICE_TYPE="GENERIC">`+
					`<RESULT VALUE="ACCEPTED"/>`+
					`</DEVICE_CONFIGURATION>`)
			},
		},
		{
			file: "rx_continue.xml",
			how:  "codec.Decode(§1.4 p5 <CONTINUE/>).Root.String()",
			gen:  func() []byte { return rxRoundTrip(t, `<CONTINUE/>`) },
		},
		{
			file: "rx_wildcard_complete.xml",
			how:  "codec.Decode(§1.6 p6 <WILDCARD_COMPLETE MTID/>).Root.String()",
			gen:  func() []byte { return rxRoundTrip(t, `<WILDCARD_COMPLETE MTID="1"/>`) },
		},
		{
			file: "rx_routing_change_route.xml",
			how:  "codec.Decode(§5.1.1 ROUTING_CHANGE TYPE=ROUTE).Root.String()",
			gen: func() []byte {
				return rxRoundTrip(t, `<ROUTING_CHANGE TYPE="ROUTE" DEVICE_NAME="MTX1" DEVICE_TYPE="ROUTER" DEST_ID="60" LEVEL_ID="1">`+
					`<ROUTE SOURCE_LEVEL_ID="1" SOURCE_ID="60" AVAILABLE="1"/>`+
					`</ROUTING_CHANGE>`)
			},
		},
		{
			file: "rx_device_change_value.xml",
			how:  "codec.Decode(§5.4.3 p29 DEVICE_CHANGE TYPE=VALUE).Root.String()",
			gen: func() []byte {
				return rxRoundTrip(t, `<DEVICE_CHANGE TYPE="VALUE" IP_ADDRESS="10.1.1.1" SUB_DEVICE="0" OBJECT="Status.Connected">`+
					`<OBJECT_VALUE OBJECT="Status.Connected" VALUE="1" AVAILABLE="1" DATA_TYPE="INTEGER" READABLE="1" WRITABLE="0" UNITS="" LABEL="" DEFAULT="0" ENUM_LIST="On,Off"/>`+
					`</DEVICE_CHANGE>`)
			},
		},
		{
			file: "rx_salvo_change_instance_details.xml",
			how:  "codec.Decode(§5.3.3 p28 SALVO_CHANGE INSTANCE_DETAILS).Root.String()",
			gen: func() []byte {
				return rxRoundTrip(t, `<SALVO_CHANGE TYPE="INSTANCE_DETAILS" GROUP="Multiviewer 1" INSTANCE="Line up">`+
					`<DETAILS AVAILABLE="1" DESCRIPTION="The layout for line up" ACTIVE="1" DATE="02/07/2017" TIME="17:33:22:12"/>`+
					`</SALVO_CHANGE>`)
			},
		},
		{
			file: "rx_category_change_details.xml",
			how:  "codec.Decode(§5.2.2 p28 CATEGORY_CHANGE CATEGORY_DETAILS).Root.String()",
			gen: func() []byte {
				return rxRoundTrip(t, `<CATEGORY_CHANGE TYPE="CATEGORY_DETAILS" CATEGORY="EVS">`+
					`<DETAILS AVAILABLE="1" LABEL="EVS Sources" DESCRIPTION="All EVS Sources"/>`+
					`<ITEMS>`+
					`<ITEM_1 TYPE="SALVO" VALUE="Line up" GROUP="Multiviewer 1"/>`+
					`</ITEMS>`+
					`</CATEGORY_CHANGE>`)
			},
		},
		{
			file: "rx_datastore_change_data.xml",
			how:  "codec.Decode(§5.5.1 p30 DATASTORE_CHANGE FILE+DATA).Root.String()",
			gen: func() []byte {
				return rxRoundTrip(t, `<DATASTORE_CHANGE TYPE="FILE" AVAILABLE="1">`+
					`<DATA><PRESETS><PRESET_1 NAME="Active"/></PRESETS></DATA>`+
					`</DATASTORE_CHANGE>`)
			},
		},
	}
}

// TestFixtures is the drift-guard. It regenerates every fixture from the
// codec and asserts the committed file is byte-identical. Mirrors
// probel-sw02p's TestMatrixTreeExportFixture.
func TestFixtures(t *testing.T) {
	regen := os.Getenv("DHS_REGEN_FIXTURES") == "1"
	for _, fx := range fixtures(t) {
		fx := fx
		t.Run(fx.file, func(t *testing.T) {
			want := fx.gen()
			path := filepath.Join(fixtureDir, fx.file)

			if regen {
				if err := os.WriteFile(path, want, 0o644); err != nil {
					t.Fatalf("write fixture %s: %v", path, err)
				}
				t.Logf("regenerated %s (%d bytes) via %s", path, len(want), fx.how)
				return
			}

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read committed fixture %s: %v "+
					"(regenerate with DHS_REGEN_FIXTURES=1)", path, err)
			}
			if string(got) != string(want) {
				t.Errorf("committed %s is stale — does not match codec output.\n"+
					" got %s\nwant %s\nRegenerate with DHS_REGEN_FIXTURES=1.",
					path, string(want), string(got))
			}
		})
	}
}
