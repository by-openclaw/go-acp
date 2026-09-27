# Arista ST 2110 fabrics — configuration as code

Issue #1174. Roles `arista_*`, playbook `playbooks/arista-fabric.yml`, site data
in `sites/<site>/`. The switches are only changed through this playbook.

## How it works

Each role owns a set of **blocks** (a top-level config line plus the lines
indented under it) and renders them from site data. `arista_session` (the
engine) compares those blocks with the live running-config and stages only the
difference in an EOS **configuration session**:

| Mode | What happens |
|---|---|
| default (diff only) | session staged, its diff printed, session **aborted** |
| `--check` | same as default |
| `-e arista_apply=true` | flash backup → `commit timer 00:10:00` → health checks (OSPF FULL, PIM neighbours per plane, PTP grandmaster) → confirm → `write memory` → after-copy on flash |

A failed health check stops the play before confirming: the session rolls
back by itself when the timer expires. Blocks a role does not own are never
read as intended nor touched; nothing is ever replaced wholesale.

## Roles

| Role | Owns |
|---|---|
| `arista_session` | engine only (eAPI, plan, session, health) |
| `arista_mgmt` | management VLAN + gateway (VRRP), management loopback, NTP, SNMPv3, eAPI, gNMI, default routes |
| `arista_vrf` | VRFs, `ip routing`, **in-band control** leaks (management ⇄ planes), transit VLAN |
| `arista_underlay` | loopbacks, switch-to-switch point-to-point VLANs, OSPF (BFD, MD5) |
| `arista_media` | per plane: media VLAN, gateway (IGMP querier, PIM DR), DHCP scope |
| `arista_multicast` | IGMP snooping, `router multicast`, `router pim`, RP range ACL |
| `arista_ptp` | PTP globals, grandmaster-feed VLAN |
| `arista_stp` | MST region, root/backup priority |
| `arista_ports` | port profiles, port map, LAGs, parking VLAN — only the ports listed |

A block has exactly one owner; the owner reads other concerns' settings (e.g.
`arista_underlay` writes PIM on the plane links it owns).

## Data

- `sites/<site>/site.yml` — planes, management, underlay, media, multicast
  plan, PTP, MST, profiles, switches. Addressing is derived (red = 10.6.x,
  blue = 10.7.x, host = network + switch id); any value can be overridden.
- `sites/<site>/ports/<switch>.yml` — port map, parked ports, LAGs.
- `secrets/arista.json` — eAPI, OSPF key, SNMPv3 (gitignored; template
  `secrets/arista.example.json`; Vault later).

A new deployment (PoC, customer, bench staging before shipping) is a new
`sites/<name>/` directory and an inventory; the roles do not change.

## Runbook

From the control node (dhs-debian), in `ansible/`:

```bash
# 1. See what would change (safe, default)
ansible-playbook playbooks/arista-fabric.yml -e site=lab
# 2. Narrow it: one concern / one switch
ansible-playbook playbooks/arista-fabric.yml -e site=lab --tags ptp --limit fabric-2
# 3. Apply
ansible-playbook playbooks/arista-fabric.yml -e site=lab -e arista_apply=true
# 4. Prove idempotency: must report changed=0
ansible-playbook playbooks/arista-fabric.yml -e site=lab -e arista_apply=true
```

**Change something** — edit `sites/<site>/…`, run 1, read the session diff,
open a PR, apply after review.

**Roll back** — every applied session leaves `flash:dhs-<role>-<ts>-before.cfg`
and `-after.cfg` on the switch. Revert the data in git and re-apply, or on the
switch: `configure replace flash:dhs-<role>-<ts>-before.cfg` then
`write memory`. A session not yet confirmed rolls back by itself.

## Known EOS behaviours handled by the engine

| Behaviour | Handling |
|---|---|
| OSPF key stored type-7, SNMPv3 keys localized | compared by presence only; plaintext written when needed |
| Profile lines kept in insertion order | flat blocks compared order-insensitively |
| Sub-mode edits (`!!` comments) recorded when the mode is left | staging ends with `end`, re-enters the session to read the diff |
| MST region applied only on `exit`; rewriting it stages its removal | MST corrected line by line, followed by `exit` (regression test) |
| `!!` comments not kept in MST region mode | no comment there; documented in the template |
| Dormant breakout lanes (config without hardware) | per-switch `ignore_blocks` |

Tests: `python3 -m unittest discover -s roles/arista_session/tests`.
