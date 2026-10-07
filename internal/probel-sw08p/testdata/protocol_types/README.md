# protocol_types — one folder per command, real traffic

Per ADR-0025 deliverable 6. Each folder is named after the command file it
belongs to (`consumer/cmd_rxNNN_*.go`) and holds a small slice of real
traffic showing that command:

    <cmd>/frames.jsonl     the wire trace the consumer wrote (--capture, ADR-0021)
    <cmd>/capture.pcapng   the same frames as a capture
    <cmd>/tshark.tree      dhs_probel_sw08p.lua's rendering of that capture

## Where they came from

One real device: the lab's **EVS Neuron CONVERT** (`10.6.255.102:7800`, one
matrix of 4 176 x 4 176), driven by the released `dhs v0.40.4` on
2026-10-07 through `ansible/playbooks/probel-sw08p-fixtures.yml`. Nothing
is synthetic and no byte was edited. A trace keeps SW-P-08 frames, not
packets, so each capture was built with Wireshark's `text2pcap`: the
SW-P-08 bytes are the device's and the consumer's own, the IP/TCP headers
around them are stand-ins (consumer on port 50000, matrix on 2008).

The slices are cut to the first frames of an exchange: a fixture is only
reviewable if a person can read all of it. The tally dump and the name
tables go on for hundreds of frames on this matrix; the first reply is here.

Regenerate a tree after changing the dissector:

    tshark -r <cmd>/capture.pcapng \
      -X lua_script:internal/probel-sw08p/wireshark/dhs_probel_sw08p.lua \
      -V -O dhs_probel_sw08p > <cmd>/tshark.tree

## What is here

A Neuron speaks the **extended** form of the crosspoint and protect
commands (its sources do not fit the general form's 10 bits), so those
folders show the extended command bytes.

| Folder | On the wire | What it shows |
|---|---|---|
| `rx_001_crosspoint_interrogate/` | rx 129 → tx 131 | a destination asked, its source answered — both extended |
| `rx_002_crosspoint_connect/` | rx 130 → tx 132 | a connect and the matrix's "connected" (destination 4175 onto source 256; put back after the capture) |
| `rx_010_protect_interrogate/` | rx 138 → tx 139 | the protect state of a destination |
| `rx_021_crosspoint_tally_dump/` | rx 021 → tx 023 | the dump request and the first word-form dump message |
| `rx_100_all_source_names/` | rx 100 → tx 106 | the request and the first of the "one or more" response messages |
| `rx_101_single_source_name/` | rx 101 → tx 106 | one source name |
| `rx_102_all_dest_assoc_names/` | rx 102 → tx 107 | the request and the first response message |
| `rx_103_single_dest_assoc_name/` | rx 103 → tx 107 | one destination name |
| `rx_117_update_name_request/` | rx 117, link ACK | a name update: by the spec the matrix sends no reply (§3.1.26) |

**Acknowledged and not answered.** SW-P-08 has no "unsupported" reply: a
matrix that does not implement a command takes the frame (DLE ACK) and
sends nothing. That is what a Neuron does for the commands below, and what
a client has to recognise — the consumer reports it as "not served by this
matrix". Each folder holds the request and the lone ACK.

| Folder | On the wire |
|---|---|
| `rx_008_dual_controller_status/` | rx 008, ACK |
| `rx_012_protect_connect/` | rx 138 → tx 139 (the verb reads the state first), then rx 140, ACK |
| `rx_017_protect_device_name_request/` | rx 017, ACK |
| `rx_019_protect_tally_dump_request/` | rx 019, ACK |
| `rx_029_master_protect_connect/` | rx 029, ACK |
| `rx_114_all_source_assoc_names/` | rx 114, ACK |
| `rx_115_single_source_assoc_name/` | rx 115, ACK |
| `rx_120_crosspoint_connect_on_go_salvo/` | rx 248 (extended), ACK |

## What is not here, and why

- **The general (short) form** of interrogate, connect and tally (rx 001 /
  002, tx 003 / 004). A Neuron answers a general interrogate only when the
  routed source fits ten bits, and on the CONVERT no route written by the
  tests does. Our own producer speaks it, but that would be our bytes on
  both ends rather than a matrix's.
- **Replies to the commands a Neuron does not serve** (tx 009, 013, 018,
  020, 116, 122 / 123, and the salvo family rx 121 / 124): no real matrix
  here answers them.
- **rx 007 maintenance** (reset, clear protects) and **rx 014 protect
  disconnect**: not sent to a matrix in use — the first acts on every
  route, the second has nothing to disconnect when no protect can be set.
- **rx 112 tie-line interrogate**: no tie-lines on this matrix.

## What checks them

- `codec/protocol_types_fixture_test.go` (unit, runs in CI): every frame
  of every trace unpacks — framing, byte count, checksum — and packs back
  to the bytes that were on the wire.
- `integration/protocol_types_test.go` (`-tags integration`, needs a
  tshark that loads Lua): every capture, re-rendered by the current
  dissector, is the committed tree.
