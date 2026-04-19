package main

import "fmt"

// ---------------------------------------------------------------- per-command help

func helpInfo() {
	fmt.Println(`acp info — read device info

IN   acp info 10.6.239.113
OUT  device       10.6.239.113:2071
     protocol     acp1 v1
     slots        31
     per-slot status:
       slot  0   present
       slot  1   present
       …

USAGE
  acp info <host> [flags]

DESCRIPTION
  Connects to the device, reads the rack controller's Frame Status
  object (group=frame, id=0), and prints the slot count plus the
  status of every slot. This is the typical first call after power-on
  or after changing a LAN cable, to confirm the device is reachable
  and see which cards are present.

FLAGS (in addition to global flags)
  (none)

EXAMPLES
  acp info 10.6.239.113
  acp info 10.6.239.113 --timeout 5s
  acp info 10.6.239.113 --verbose`)
}

func helpWalk() {
	fmt.Println(`acp walk — enumerate every object on a slot

IN   acp walk 10.6.239.113 --slot 0
OUT  slot 0:
        0  Card name       string   R--  "RRS18"
        1  User label      string   RW-  "Synapse Simulator"
        2  Card description string  R--  "Virtual Rack Controller"
        …
     (with --capture <dir> also writes raw.<transport>.jsonl +
      tree.json — raw.acp1.jsonl / raw.an2.jsonl / raw.s101.jsonl
      per protocol — plus glow.json for Ember+)

USAGE
  acp walk <host> --slot N [flags]

DESCRIPTION
  Reads the root object on the target slot to learn the number of
  objects per group, then issues one getObject per object, producing
  a typed inventory: identity, control, status, alarm. Section
  markers (device-specific grouping hints) are rendered as "── NAME ──".

  The walker caches the result per slot for the lifetime of the CLI
  process so subsequent get/set calls can resolve --label without
  re-walking.

FLAGS
  --slot N           slot number (required)
  --all              walk every present slot
  --path PATH        filter by tree path prefix (e.g. BOARD, PSU.1)
  --filter TEXT      case-insensitive filter on output lines (like findstr /i or grep -i)

EXAMPLES
  acp walk 10.6.239.113 --slot 0                                       # rack controller
  acp walk 10.6.239.113 --slot 1                                       # first card
  acp walk 10.41.40.195 --protocol acp2 --slot 0 --path BOARD          # only BOARD subtree
  acp walk 10.41.40.195 --protocol acp2 --slot 0 --path PSU.1          # only PSU unit 1
  acp walk 10.41.40.195 --protocol acp2 --slot 0 --path PSU --filter Temperature  # combine
  acp walk 10.41.40.195 --protocol acp2 --slot 0 --filter QSFP         # text search all`)
}

func helpGet() {
	fmt.Println(`acp get — read one object value

IN   acp get 10.6.239.113 --slot 1 --label "Card name"
OUT  value = "CDV08v06   "
     raw  = 434456303876303620202000
     kind = string  access = R--
     max length = 11 chars

USAGE
  acp get <host> --slot N (--label L | --group G --id I) [flags]

DESCRIPTION
  Reads one object value and decodes it into a typed form:
  integer, float, enum, string, ipaddr, alarm priority, or frame
  status. The corresponding metadata (range, step, default, unit,
  enum items, max string length, alarm messages) is printed below
  the value.

  Addressing:
    • --label L            search the walked tree for the label
                           (walks the slot automatically if needed)
    • --group G --id I     explicit addressing, no walk required
    • --label L --group G  disambiguate a label that exists in
                           multiple groups

FLAGS
  --slot N           slot number (required)
  --label L          object label (preferred — stable across firmware)
  --group G          object group: identity | control | status | alarm | frame
  --id I             object id within a group

EXAMPLES
  acp get 10.6.239.113 --slot 1 --label "Card name"
  acp get 10.6.239.113 --slot 1 --label GainA
  acp get 10.6.239.113 --slot 1 --group control --id 91
  acp get 10.6.239.113 --slot 0 --group frame --id 0`)
}

func helpSet() {
	fmt.Println(`acp set — write one object value

IN   acp set 10.41.40.195 --protocol acp2 --slot 1 --id 3 --value "ACP2-OK"
OUT  confirmed value = "ACP2-OK"
     raw       = 414350322d4f4b00
     (non-zero exit on access / value / timeout errors)

USAGE
  acp set <host> --slot N (--label L | --group G --id I) --value V [flags]
  acp set <host> --slot N ...                                --raw HEX [flags]

DESCRIPTION
  Writes one object value. The device enforces range, step, and
  access constraints — out-of-range writes are silently clamped by
  the device and the echoed reply shows the stored value.

  Typed --value forms, picked automatically by object kind:
    integer / long     "42", "-7"
    float              "-6.3", "50.0"
    byte               "100"
    enum               "On"  (item name, case-sensitive)
                       "1"   (numeric index)
    string             "CH1"
    ipaddr             "192.168.1.5"

  --raw is an escape hatch for advanced users: pass the exact wire
  bytes in hex, bypassing type coercion. Useful when the walker
  hasn't seen the object (no prior walk) or when debugging a quirky
  device.

FLAGS
  --slot N           slot number (required)
  --label L          object label (preferred)
  --group G          object group
  --id I             object id within a group
  --value V          typed value string
  --raw HEX          raw wire bytes (mutually exclusive with --value)

EXAMPLES
  acp set 10.6.239.113 --slot 1 --label GainA --value 50.0
  acp set 10.6.239.113 --slot 0 --label Broadcasts --value On
  acp set 10.6.239.113 --slot 1 --label mIP0 --value 192.168.1.250
  acp set 10.6.239.113 --slot 1 --label "#CVBS-Frmt" --value "PAL-N"
  acp set 10.6.239.113 --slot 1 --label GainA --raw 42c80000`)
}

func helpWatch() {
	fmt.Println(`acp watch — subscribe to live announcements

IN   acp watch 10.6.239.113 --slot 1
OUT  12:34:56.789  slot=1 group=control id=91 GainA        = 50.0
     12:34:57.102  slot=1 group=status  id=9  SPF_Status   = Online
     …  (runs until Ctrl-C; --capture FILE also writes every frame to JSONL)

USAGE
  acp watch <host> [filters] [flags]

DESCRIPTION
  Opens a UDP listener and prints every announcement the device
  broadcasts: value changes (control, status, alarm), frame-status
  transitions (card inserted, removed, booting, error), identity
  updates. Runs until Ctrl-C.

  REQUIREMENTS:
    • The rack controller's "Broadcasts" enable must be ON. When
      it is OFF the device sends no LAN announcements at all.
      Check via: acp get <host> --slot 0 --label Broadcasts
    • Port 2071 must be free on the local host (another acp
      process or a Synapse Cortex running on the same box will
      hold it and prevent binding).

  Filters compose: any combination of --slot, --group, --label,
  --id narrows the stream. No filter = everything.

FLAGS
  --slot N           only events from this slot (default: any)
  --group G          only events in this group (default: any)
  --label L          only events for this label (requires --slot)
  --id I             only events for this object id

EXAMPLES
  acp watch 10.6.239.113                              # everything
  acp watch 10.6.239.113 --slot 1                     # slot 1 only
  acp watch 10.6.239.113 --slot 1 --group control
  acp watch 10.6.239.113 --slot 1 --label GainA
  acp watch 10.6.239.113 --verbose                    # + debug lines`)
}

func helpListProtocols() {
	fmt.Println(`acp list-protocols — list available protocol plugins

IN   acp list-protocols
OUT  name        port   description
     acp1        2071   Axon Control Protocol v1
     acp2        2072   Axon Control Protocol v2 (AN2/TCP)
     emberplus   9000   Ember+ v2.50 (S101/TCP)

USAGE
  acp list-protocols

DESCRIPTION
  Prints every protocol plugin that was compiled into this binary,
  with its canonical name, default port, and one-line description.
  The name shown here is what you pass to --protocol on other
  commands.

EXAMPLES
  acp list-protocols`)
}

func helpExport() {
	fmt.Println(`acp export — dump a walked device to json / yaml / csv

IN   acp export 10.6.239.113 --format json --out device.json
OUT  exported 1 slots to device.json (json)
     (json/yaml lossless; csv flat one-row-per-object)

USAGE
  acp export <host> [--format F] [--out FILE] [flags]

DESCRIPTION
  Walks every present slot on the device (same as 'acp walk --all')
  and writes the result to a snapshot file. Three formats:

    json  lossless, stdlib encoding, pretty-printed
    yaml  lossless, hand-rolled emitter, 2-space indent
    csv   lossy, one row per object, header row, '|' for nested fields

  Format is picked from --format first, then the --out extension,
  defaulting to json. With no --out the snapshot streams to stdout.

FLAGS
  --format F         json | yaml | csv   (default: json or from extension)
  --out FILE         output file path    (default: stdout)
  --slot N           export only this slot (-1 = all present)
  --path PATH        filter by tree path prefix (e.g. BOARD, PSU.1)

EXAMPLES
  acp export 10.6.239.113 --format json --out device.json
  acp export 10.6.239.113 --format yaml --out device.yaml
  acp export 10.6.239.113 --format csv  --out device.csv
  acp export 10.41.40.195 --protocol acp2 --slot 0 --path BOARD --format yaml   # subtree only
  acp export 10.41.40.195 --protocol acp2 --slot 0 --path PSU.1 --format csv    # single PSU unit
  acp export 10.6.239.113 > device.json`)
}

func helpExtract() {
	fmt.Println(`acp extract — capture a per-product DM triple into the fixture layout

IN   acp extract 10.41.40.195 --protocol acp2 --slot 0 \
       --manufacturer Axon --product DDB08 --direction consumer --version 2.3 \
       --out tests/fixtures/products/axon/DDB08/acp2/consumer/v2.3/
OUT  walked 214 objects on slot 0
     capture: wrote tree.json to tests/fixtures/products/axon/DDB08/acp2/consumer/v2.3/
     extract complete: tests/fixtures/products/axon/DDB08/acp2/consumer/v2.3/{meta.json, wire.jsonl, tree.json}
       fingerprint: sha256:3fa1c4e8abcd...

USAGE
  acp extract <host> --protocol P --manufacturer M --product X --direction D --version V --out DIR [--slot N] [flags]

DESCRIPTION
  Walks the target device (all protocols) and produces the three-file
  DM fixture triple at --out:

    meta.json    locked schema — identity, fingerprint, capture_tool
    wire.jsonl   raw frames captured during the walk (renamed from
                 raw.<transport>.jsonl so the fixture layout stays
                 protocol-agnostic)
    tree.json    canonical export (post-resolution)

  capture_tool carries the name + version + git_tag + git_commit of
  the acp binary that did the capture. git_commit comes from
  runtime/debug.BuildInfo (default -buildvcs=true); version + git_tag
  come from ldflags on release builds, fall back to "devel" / a
  commit-derived string otherwise. A dirty worktree flags git_tag
  with "-dirty" — such captures should not be committed.

  dm_fingerprint is the SHA-256 of tree.json. Byte-identical firmware
  produces identical fingerprints across captures; a drift means
  either the firmware changed or the canonical encoder moved.

FLAGS
  --manufacturer M    vendor display name (preserve casing)      (required)
  --product P         product identifier as vendor writes it     (required)
  --direction D       consumer | provider | both                 (required)
  --version V         version as reported by the device          (required)
  --version-kind K    firmware | software | release (default firmware)
  --description S     free text from the identity block          (optional)
  --notes S           free text for the engineer                 (optional)
  --out DIR           destination directory                      (required)
  --slot N            slot to walk (Ember+ defaults to 0)

EXAMPLES
  acp extract 10.6.239.113 --protocol acp1 --slot 1 \
    --manufacturer Axon --product CDV08v06 --direction consumer --version 2.1 \
    --out tests/fixtures/products/axon/CDV08v06/acp1/consumer/v2.1/

  acp extract 127.0.0.1 --protocol emberplus --port 9092 \
    --manufacturer L-S-B --product TinyEmberPlus --direction consumer --version 1.0 \
    --out tests/fixtures/products/l-s-b/TinyEmberPlus/emberplus/consumer/v1.0/`)
}

func helpImport() {
	fmt.Println(`acp import — apply values from a snapshot file

IN   acp import 10.6.239.113 --file device.json --dry-run
OUT  would apply 21, skipped 38, failed 0
     skipped rows (dry-run detail):
       read_only (38):
         slot=0 id=7  kind=int    access=R-- path="status.Temp_Right"
         slot=0 id=28 kind=string access=R-- path="status.MIB_S16"
         …

USAGE
  acp import <host> --file SNAPSHOT [--slot N] [--id N ...| --path P ...] [--dry-run] [flags]

DESCRIPTION
  Reads a snapshot produced by 'acp export' (json / yaml / csv — format
  auto-detected from extension) and writes every writable object's
  value back to the device. Read-only objects are skipped; alarm
  priorities and frame status are also skipped.

  --dry-run lists what WOULD be written without touching the device,
  followed by a grouped-by-reason table of every row the importer
  chose not to attempt ("read_only" / "marker" / "unknown_kind") with
  slot, id, kind, access, and path printed so you know exactly which
  rows in your edited file will be ignored.

  Selective addressing (issue #45): narrow the apply set to specific
  targets with --id (object ID, per-protocol unambiguous) or --path
  (dotted hierarchical path). Both flags repeat. They are MUTUALLY
  EXCLUSIVE — pick one scheme per invocation. --label is deliberately
  not offered: labels collide thousands of times across sub-trees in
  Ember+ ("gain" per channel) and ACP2 ("Present" per PSU), so
  label-only matching would be ambiguous.

FLAGS
  --file PATH        snapshot file (json / yaml / csv)        (required)
  --slot N           apply only this slot (-1 = all, default)
  --id N             apply only this object ID. Repeat for multiple.
                     Mutually exclusive with --path.
  --path P           apply only this dotted path (e.g. "BOARD.Gain A").
                     Repeat for multiple. Mutually exclusive with --id.
  --dry-run          validate without writing; prints skip report

EXAMPLES
  acp import 10.6.239.113 --file device.json --dry-run
  acp import 10.6.239.113 --file device.json
  acp import 10.6.239.113 --file edited.csv                      # partial setup via edited file
  acp import 10.6.239.113 --file device.json --id 47431          # one specific object
  acp import 10.6.239.113 --file device.json --id 47431 --id 60001
  acp import 10.6.239.113 --file tree.json --path "router.inputs.ch2.gain"
  acp import 10.6.239.113 --file device.json --slot 1 --path "BOARD.Gain A" --dry-run`)
}

func helpDiscover() {
	fmt.Println(`acp discover — passive + active LAN scan for ACP1 devices

IN   acp discover --duration 5s
OUT  found 1 device(s) in 5s:
       10.6.239.113   00:08:f4:3c:12:ab   Synapse Simulator

USAGE
  acp discover [--duration 5s] [--active] [--scan-port 2071]

DESCRIPTION
  Finds ACP1 devices on the local subnet without needing to know
  their IP addresses upfront. Two modes run in parallel:

    PASSIVE  — listen on :2071 for UDP announcements. Catches any
               device whose "Broadcasts" setting is On.

    ACTIVE   — send one getValue(FrameStatus,0) request to the
               subnet broadcast address 255.255.255.255:2071.
               Every rack controller replies with a directed unicast
               message that the listener picks up. Active is ON by
               default.

  IMPORTANT: This ONLY works when your host is on the same subnet
  (same VLAN, same broadcast domain) as the devices. Subnet broadcasts
  do not cross routers. Running 'acp discover' across a pfSense /
  router boundary will return zero results even if the devices are
  reachable via unicast.

FLAGS
  --duration DUR     how long to collect results (default 5s)
  --active           enable the broadcast probe (default: true)
  --scan-port N      ACP port (default 2071)

EXAMPLES
  acp discover
  acp discover --duration 15s
  acp discover --active=false          # passive-only scan`)
}

func helpInvoke() {
	fmt.Println(`acp invoke — invoke an Ember+ function (RPC)

IN   acp invoke 127.0.0.1 --protocol emberplus --port 9092 --path router.functions.add --args 3,5
OUT  invocation: success
     result[0] = 8

USAGE
  acp invoke <host> --path <func.path> [--args val1,val2,...]

FLAGS
  --path PATH        dot-separated path to the function (e.g. router.functions.add)
  --args ARGS        comma-separated arguments (e.g. 3,5)

EXAMPLES
  acp invoke 10.6.239.113 --protocol emberplus --port 9092 --path router.functions.add --args 3,5
  acp invoke 10.6.239.113 --protocol emberplus --port 9092 --path router.functions.doNothing`)
}

func helpMatrix() {
	fmt.Println(`acp matrix — set matrix crosspoint connections (Ember+ only)

IN   acp matrix 127.0.0.1 --protocol emberplus --port 9092 \
         --path router.oneToN.matrix --target 1 --sources 1
OUT  matrix connected: t=1 ← [1] op=absolute disp=tally

USAGE
  acp matrix <host> --path <matrix.path> --target N --sources N[,N,...] [--op absolute|connect|disconnect]

FLAGS
  --path PATH        dot-separated path to the matrix (e.g. router.oneToN.matrix)
  --target N         target number
  --sources N[,N]    comma-separated source numbers
  --op OP            operation: absolute (default, replace all), connect (add), disconnect (remove)

EXAMPLES
  acp matrix 10.6.239.113 --protocol emberplus --port 9092 --path router.oneToN.matrix --target 1 --sources 1
  acp matrix 10.6.239.113 --protocol emberplus --port 9092 --path router.oneToN.matrix --target 1 --sources 1,2,3
  acp matrix 10.6.239.113 --protocol emberplus --port 9092 --path router.oneToN.matrix --target 2 --sources 5 --op connect`)
}

func helpDiag() {
	fmt.Println(`acp diag — run ACP2 diagnostic probes

IN   acp diag 10.41.40.195 --slot 0
OUT  probe: AN2 GetVersion           → ok (proto 1)
     probe: AN2 GetDeviceInfo        → ok (2 slots)
     probe: AN2 EnableProtocolEvents → ok
     probe: ACP2 GetVersion          → ok (proto 2)
     probe: ACP2 GetObject(slot,0)   → ok (children: 4)

USAGE
  acp diag <host> [--slot N]

DESCRIPTION
  Connects to the device, completes the AN2 handshake, then sends
  multiple ACP2 request variants to discover which format the device
  accepts. Reports success/failure for each probe.

EXAMPLES
  acp diag 10.41.40.195 --slot 0
  acp diag 10.41.40.195 --slot 1`)
}
