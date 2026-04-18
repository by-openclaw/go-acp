package main

import "fmt"

// ---------------------------------------------------------------- per-command help

func helpInfo() {
	fmt.Println(`acp info — read device info

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

func helpImport() {
	fmt.Println(`acp import — apply values from a snapshot file

USAGE
  acp import <host> --file SNAPSHOT [--dry-run] [flags]

DESCRIPTION
  Reads a snapshot produced by 'acp export' and writes every writable
  object's value back to the device. Read-only objects are skipped;
  alarm priorities and frame status are also skipped (they have
  dedicated paths). YAML and CSV import are not supported — use JSON.

  --dry-run lists what WOULD be written without touching the device.
  Run it first to preview the effect.

FLAGS
  --file PATH        snapshot file (json only)             (required)
  --dry-run          validate without writing

EXAMPLES
  acp import 10.6.239.113 --file device.json --dry-run
  acp import 10.6.239.113 --file device.json`)
}

func helpDiscover() {
	fmt.Println(`acp discover — passive + active LAN scan for ACP1 devices

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

func helpMatrix() {
	fmt.Println(`acp matrix — set matrix crosspoint connections (Ember+ only)

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
