# CLAUDE.md — LLDP (support library, not a connector)

`internal/lldp` is NOT a protocol connector and does not follow the
connector playbook (no consumer/provider roles, no CLI verbs, no
runbook, no DM). It is a **support library** with one job: answer
`Source` — the LLDP neighbours a host sees — so the NMOS Node provider
can fill IS-04 v1.3 `interfaces[].attached_network_device` (what this
Node "received in LLDP"). See `internal/amwa/provider/node.go`
(`IS04NodeConfig.LLDP`) for the only consumer of it.

- `capture_linux.go` — the real capture: an AF_PACKET socket bound to
  Ethertype 0x88CC, stdlib `syscall` only (no libpcap, no cgo), needs
  `CAP_NET_RAW` (granted by the `dhs_capture` Ansible role, never run as
  root). This is the one raw socket in the tree; it is allowlisted in
  `internal/transport/architecture_test.go` because `transport` models
  TCP/UDP/TLS sessions and has no layer-2 frame listener yet.
- `capture_other.go` — every other OS: `Source` reports "unsupported",
  which is the correct value for a field the Node cannot know.
- `source.go` — the `Source` interface + `NewCache` (bounds how often a
  slow source is consulted); a device that reports its own LLDP over an
  API satisfies the same interface with no privileges.
- `tlv.go` / `neighbor.go` — IEEE 802.1AB TLV decoding into `Neighbor`.

Gold-template status: DI-free by design (pure functions + one injected
`Source`), 100% unit coverage is the bar like every shared package; the
Linux capture path is exercised by the `dhs_capture` Ansible validation,
not by unit tests (it needs a real interface and CAP_NET_RAW).

What NOT to do: do not turn this into a connector (no "lldp consumer
walk") — a controller reads LLDP through the Node's IS-04 API, which is
the spec's design.
