# CLAUDE.md — LLDP (support library, not a connector)

`internal/lldp` is NOT a protocol connector and does not follow the
connector playbook (no consumer/provider roles, no runbook, no DM). It is
a **support library** with one job: answer `Source` — the LLDP neighbours
a host sees. Two callers:

- `dhs host info` (`cmd/dhs/cmd_host.go`, #1147) — which switch and port
  each interface of the machine dhs runs on is plugged into.
- the NMOS Node provider, for IS-04 v1.3
  `interfaces[].attached_network_device` (`IS04NodeConfig.LLDP` in
  `internal/amwa/provider/node.go`). Only correct when the Node IS this
  machine; a Node fronting another device takes that device's own LLDP.

- `capture_linux.go` — the real capture: an AF_PACKET socket bound to
  Ethertype 0x88CC, stdlib `syscall` only (no libpcap, no cgo), needs
  `CAP_NET_RAW` (granted by the `dhs_capture` Ansible role, never run as
  root). This is the one raw socket in the tree; it is allowlisted in
  `internal/transport/architecture_test.go` because `transport` models
  TCP/UDP/TLS sessions and has no layer-2 frame listener yet.
- `capture_darwin.go` — macOS: one `/dev/bpf*` per interface, a BPF
  filter for 0x88CC, stdlib `syscall` only. Needs root or access to
  `/dev/bpf*`.
- `capture_windows.go` — Windows: Npcap's `wpcap.dll`, loaded at run time
  from `System32\Npcap` with the standard library (no CGo). The operator
  installs Npcap; without it, `ErrCaptureUnsupported` says what to install.
- `capture_other.go` — every other OS: `Source` reports "unsupported",
  which is the correct value for a field the Node cannot know.
- `frame.go` — what the capture paths share: the Ethernet header strip
  (BPF and Npcap hand over whole frames), interface selection, and one
  listener per interface merged into one view, with `Until` to stop early.
- `source.go` — the `Source` interface + `NewCache` (bounds how often a
  slow source is consulted); a device that reports its own LLDP over an
  API satisfies the same interface with no privileges.
- `tlv.go` / `neighbor.go` — IEEE 802.1AB TLV decoding into `Neighbor`.

Gold-template status: DI-free by design (pure functions + one injected
`Source`), 100% unit coverage is the bar like every shared package; the
real capture paths are exercised live (`dhs host info` on the host, the
`dhs_capture` Ansible validation), not by unit tests — they need a real
interface and privileges. Unit tests drive each OS path through its seam
(`osCalls` / `bpfCalls` / `npcapCalls`) on that OS's CI runner.

What NOT to do: do not turn this into a connector (no "lldp consumer
walk") — a controller reads LLDP through the Node's IS-04 API, which is
the spec's design. `dhs host info` is a host command, not a connector.
