# docs/testbed.md — Test fleet inventory and access

> **Dev/test/deploy flow:** see
> [`docs/deployment/dev-test-flow.md`](deployment/dev-test-flow.md) —
> the authoritative how-to (desk → git bundle → control node → Ansible).
> This file is the fleet **inventory + access reference**.

Tracked, repo-local source of truth for the test fleet on **VLAN 600 /
MGMT_CTRL** (`10.6.240.0/20`, Proxmox bridge `vmbrMGMT`, fabric gateway
`10.6.255.254`), live since the **2026-08-29 migration**. Management
addresses are `10.6.250.101`–`.105`. The Ansible inventory
(`ansible/inventory/`) carries the SAME facts per host (`ansible_host`,
`pve_vmid`, `nics`, `os_label`) — when one changes, both change in the
same PR — and the inventory wins on any disagreement.

> **Retired:** the earlier `10.100.0.0/24` VLAN 100 (`vmbrAPPS`, gw
> `10.100.0.1`) plan is **dead** — do not use those addresses. Older
> `10.6.239.x` office addresses for emulators are unrelated to the fleet.

## Fleet

| Inventory name | Proxmox | Guest OS | mgmt (`ansible_host`) | Role |
| --- | --- | --- | --- | --- |
| `dhs-debian` | LXC 651 | Debian 12 | `10.6.250.101` | **Ansible control node** + AMWA plant registry (`:8235`); dhs producer host; Docker |
| `dhs-ubuntu` | LXC 652 | Ubuntu 24.04 | `10.6.250.102` | dhs producer host; binary-test target |
| `dhs-rocky` | LXC 653 | Rocky 9.4 | `10.6.250.103` | dhs producer host; binary-test target |
| `dhs-tools` | LXC 655 | Ubuntu | `10.6.250.104` | tooling: Go build host, AMWA NMOS Testing tool (`scripts/amwa/`), tshark; **only host needing internet** |
| `win11` | VM 654 | Windows 11 Pro | `10.6.250.105` | Windows producer-parity row (ADR-0016); guest name `dhs-win11` (guest static unconfirmed post-migration) |
| `cerebrum` | VM `vm-cerebrum-stg-01` | Windows 11 | (VLAN600 IP — confirm) | external reference peer (EVS Cerebrum staging) — real-peer integration target, not part of the converge set |

All LXCs are unprivileged with `nesting=1`. MAC addresses per NIC live
in `ansible/inventory/host_vars/<name>.yml` (`nics:`).

Addressing is **static, from the inventory**: one management NIC per
host — `eth0` = `ansible_host` on `10.6.250.101`–`.105`, fabric gateway
`10.6.255.254`, no overlaps. LXCs get it in the Proxmox CT config
(`netX: …,ip=<addr>/20[,gw=…]`, `nameserver`, `searchdomain`, written
via the API, applied by a CT reboot); the Windows VM gets it guest-side.
No DHCP dependency. The inventory is the source of truth, not an
overlay. **The `dhs_netaddr` role (#786) still carries the retired
`10.100.0.x` plan — do NOT run it until it is reworked for VLAN 600**
(the live addresses above were set during the migration, not by that
role).

A second management NIC (`eth1` = eth0 + 10, `.111`–`.113`, no default
route) existed until 2026-08-23 and is **parked, not deleted** — re-adding
it is a `host_vars` edit. It was retired because both NICs sat on the same
L2, so `<host>.local` resolved to whichever answered first: `dhs-debian.local`
returned `.111` instead of `.101` (#822). IPv6 is **out of scope for this
fleet** — dual stack lives on the new Proxmox node.
`ansible/playbooks/fleet-verify.yml` asserts live IP == inventory IP per
NIC and fails loudly on drift. Before 2026-08-23 the fleet ran on DHCP
leases (`.101–.108`, win11 drifted `.106`→`.107`) — never reuse those
old numbers from memory; this table is the truth.

Guest hostnames equal the inventory name (`fleet_hostname`:
`dhs-debian`, `dhs-ubuntu`, `dhs-rocky`, `dhs-tools`, `dhs-win11`),
converged by the `dhs_hostname` role (#783) in BOTH layers — the guest
and the Proxmox CT config (Proxmox rewrites `/etc/hostname` from its
config at every CT start, so a guest-only rename would revert). Unique
names matter: three LXCs used to answer `dhs`, which collides in Avahi.

## AMWA plant — node identity (stable UUIDs)

The AMWA lab plant (`dhs_amwa_plant` role) uses **deterministic** UUIDs so
a redeploy or heartbeat re-registration **updates** a resource, never
creates a duplicate. Any duplicate label seen in a consumer (e.g.
Cerebrum's registry) is therefore a **stale or foreign registration**
that ages out on the heartbeat GC — not a UUID-churn bug. Keep this table
as the test oracle for "duplicate vs mismatch vs real".

Scale nodes `dhs-scale-NN` (NN = `00`–`19`), from
`templates/scale-node.json.j2` with index `i` = NN — one device / source /
flow / sender / receiver each:

| Resource | UUID (`i` = 00–19) |
| --- | --- |
| Node | `aa000000-0000-4000-8000-0000000000`*i* |
| Device | `bb000000-0000-4000-8000-0000000000`*i* |
| Source | `cc000000-0000-4000-8000-0000000000`*i* |
| Flow | `dd000000-0000-4000-8000-0000000000`*i* |
| Sender | `ee000000-0000-4000-8000-0000000000`*i* |
| Receiver | `ff000000-0000-4000-8000-0000000000`*i* |

Fixed nodes:

| Node | UUID |
| --- | --- |
| `dhs-test-node` (fixture `amwa-test-node.json`) | `2c47bf5e-1b2c-4abc-9def-deadbeef0001` |
| Neuron `bm-n-nnbrg-c01` (real EVS device) | `b7011c4e-5f39-5a1a-a6eb-a8036b0a5fd9` |

Fleet total: **22 nodes / 237 senders / 231 receivers** (208 senders are
the Neuron's). Feeding these into Cerebrum's own registry is a separate
bridge — see the mirror in [`dev-test-flow.md`](deployment/dev-test-flow.md).

**Clean-test rule:** run the AMWA conformance suite **or** the registry
mirror against the registry, not both at once — the conformance tool
registers its own mock nodes (foreign UUIDs) which a running mirror would
forward and show as transient extras. Only one mirror at a time.

## Producer port plan (every Linux LXC + win11)

| Connector | Wire | Port |
| --- | --- | --- |
| ACP1 | UDP + TCP | `2071` |
| ACP2 | TCP (AN2) | `2072` |
| Ember+ | TCP (S101) | `9000` |
| Probel SW-P-08 | TCP (DLE) | `2008` |
| Probel SW-P-02 | TCP | `2002` |
| AMWA NMOS Node | HTTP | `8080` (CLI default) / `18080` (fleet plan) |
| AMWA NMOS Registry (Registration/Query) | HTTP + WS | `8235` |
| AMWA NMOS IS-07 events | WS | `8090` |
| AMWA NMOS IS-09 System | HTTP | `10641` |
| Cerebrum NB | TCP | `40007` |
| mDNS (Avahi / Bonjour) | UDP | `5353` |
| metrics (`--metrics-addr`) | HTTP | `9100` |
| SSH / WinRM (mgmt) | TCP | `22` / `5985` |

All producers run concurrently on the same host; one port per
connector. The same producer binary is driven by `dhs consumer` and by
external peers (Cerebrum @ `.5`, AMWA Testing tool in Docker on
`dhs-tools` `.104`) so wire behaviour can be compared across drivers.
Host firewalls are managed by the `dhs_firewall` role (#785): each host opens exactly the rule groups of the connectors it declares in `dhs_connectors` (host_vars) plus ssh/metrics(/winrm); other `dhs-*` rules are removed; Windows connector rules are scoped to `C:\dhs\dhs.exe`; the `nmos` group includes mDNS and requires `dhs_mdns`.

## Control node

`dhs-debian` (`10.6.250.101`) runs every Ansible play — Linux hosts over
SSH as `root`, the Windows VM over SSH as `by-rune` (key auth). The fleet
has **no internet**, so code arrives as a **git bundle** from the desk
(see [`dev-test-flow.md`](deployment/dev-test-flow.md)), not `git pull`.
The AMWA plant checkout lives at **`/root/acp-plant`** (its git `origin`
is the shipped bundle `/tmp/plantfull.bundle`); the plant registry runs
as the `dhs-nmos-registry` systemd unit
(`/opt/dhs-amwa-plant/dhs registry nmos serve --bind :8235`). Never
drive the fleet from a Windows workstation (PowerShell) — ADR-0025 §5.

**Secondary runner** (#938): `dhs-tools` (`10.6.250.104`) carries its
own pipx `ansible-core` and repo mirror (`/root/acp-runner`, cloned
from the primary's `/root/acp-plant`), converged by
`playbooks/amwa-runner.yml`. It exists for exactly one class of play:
ones that reboot the primary control node itself
(`amwa-reboot-resilience.yml`, `amwa-reboot-gate.yml`) — a play cannot
survive rebooting the host it runs on. Everything else stays on the
primary.

Monitoring a run (#790): every play logs to `/tmp/ansible-fleet.log`
on the control node (`tail -f` it) and prints per-task timings
(`profile_tasks`); `ps -eo pid,lstart,etimes,args | grep ansible-playbook`
shows which pass is running and for how long. Windows tasks run with
`ansible_shell_type: powershell` against an sshd `DefaultShell` of
PowerShell (set by `dhs_access`, sshd restarted when it changes) — a
fresh VM needs `playbooks/win-shell-bootstrap.yml` once (play-scoped cmd
shell; never a global `-e ansible_shell_type`, it would hit tasks delegated
to the control node). Facts are cached for an hour (`/tmp/ansible-facts`).
Ad-hoc `ssh by-rune@win11 '<cmd>'` now lands in PowerShell, not cmd.

## Access — actor keys, not per-host keys

Exactly four ed25519 identities are authorized on every host, managed
by the `dhs_access` role (#782); anything else is removed:

| Actor | Key comment | Purpose |
| --- | --- | --- |
| by-rune (agent, desk-03) | `by-rune-dhs-lxc` | agent SSH to every host |
| control node `.101` | `root@dhs` | Ansible → fleet |
| codeowner | (owner's ed25519) | direct SSH |
| secondary runner `.104` | `runner-dhs-tools` | plays that reboot the primary control node (#938) |

Linux: `/root/.ssh/authorized_keys` (login user is `root`; `by-rune` is
rejected by sshd on the LXCs). Windows: `by-rune` is in the local
Administrators group, so sshd reads
`C:\ProgramData\ssh\administrators_authorized_keys` (strict ACL:
SYSTEM + Administrators only) — the per-profile `authorized_keys` is
ignored for admin users, which is why earlier key installs "did
nothing". No GPG keys and no per-host keypairs are generated (commit
signing belongs to the parked hardening topic).

## mDNS daemons

Avahi 0.8 is installed, active and pinned by the `dhs_mdns` role on all
four LXCs (Docker hosts exclude `docker0` from Avahi). On `win11` the
role installs Apple Bonjour unattended: it stages `BonjourPSSetup.exe`
at `C:\dhs\installers\`, carves the embedded CAB, expands it and
installs the core `Bonjour64.msi` with `msiexec /qn` (the bootstrapper's
own silent mode only installs the Print-Services MSI, which fails
without the core — #797), then keeps "Bonjour Service" running /
automatic. dhs uses the stdlib mDNS fallback on Windows until the
Bonjour backend (#195) lands.

## Host baseline (`dhs_host` role, #800)

Every fleet host gets the same dhs baseline, per OS, from the
`dhs_host` role (first role in `fleet-converge.yml`):

| | Linux (LXCs) | win11 |
| --- | --- | --- |
| binary | `/usr/local/bin/dhs` from the pinned GitHub release `dhs_version` (tar.gz, sha256 checked against `SHA256SUMS.txt`) | `C:\dhs\dhs.exe` (zip, same checksum rule) |
| PATH / env | `/etc/profile.d/dhs.sh` (PATH, `DHS_DATA_DIR=/var/lib/dhs`) | machine `PATH += C:\dhs`, `DHS_DATA_DIR=C:\ProgramData\dhs\data` |
| directories | `/etc/dhs` (trees/packs), `/var/lib/dhs` (data), `/var/log/dhs` | `C:\dhs`, `C:\ProgramData\dhs\{data,logs}` |
| packages | tshark/wireshark-cli, curl, jq, ca-certificates, unzip, tar | — |

Time (#804): `win11` syncs w32time to the fabric gateway
`10.6.255.254` (set by `dhs_host`); the LXCs cannot set
their own clock — they inherit the Proxmox node's, which was measured
~9 min ahead on 2026-08-23 → `ansible/playbooks/pve-time.yml` (#810,
group `pve` = pve01 over SSH as root; chrony + `makestep`; platform twin
`ansible-platform#168`);
`fleet-verify.yml` prints each host's offset vs the control node.

IPv6: this fleet is **IPv4-only** (owner). VLAN 600 offers no
RA/DHCPv6 — so NICs
hold link-local IPv6 only and nothing autoconfigures. Dual stack belongs
to the NEW Proxmox node, not here; do not reintroduce an IPv6 scheme for
this fleet without the owner asking.

Roll the fleet to a new release by bumping `dhs_version` and converging
(run-twice = 0 changes). Layering: hypervisor = `infra-terraform-proxmox`
(import of the dhs guests tracked there), OS baseline/hardening =
`ansible-platform` (sshd 22222 / `by-systems`; applies when hardening
un-parks — the inventory then switches port/user), dhs application layer
= this repo's roles.

### win11 Ansible latency (#790, #812, #815)

A no-op `fleet-converge.yml -l win11` pass, measured 2026-08-23 from the
control node:

| Configuration | Duration |
| --- | ---: |
| SSH transport, one `win_firewall_rule` task per rule | 481 s |
| PSRP over WinRM-HTTPS, client-certificate auth (#812) | 305 s |
| + `dhs_firewall` single-pass reconcile, bulk query + in-memory join (#790) | 236 s |
| + Defender path exclusions, VM 654 at 8 GB (#815) | 139-156 s |

What each lever fixed. Over SSH every task spawns a fresh PowerShell on
the target - connection reuse (`ControlPersist`) was already on, so the
cost is process spawn, not connect; PSRP keeps one runspace for the
whole play. Per-rule `Get-NetFirewallRule` + `Get-NetFirewall*Filter`
cost ~3 s each, so ~30 rules were 111 s of the pass - one bulk query
joined in memory removes it. Defender scanned every module written to
`ansible-tmp-*`; the exclusions (`dhs_host_defender_exclusions`) are
narrow by design - `C:\dhs`, `C:\ProgramData\dhs`, and the
`ansible-tmp-*` pattern only, never a blanket `%TEMP%` or a system-wide
exclusion, and real-time protection stays on. A memory change on the VM
needs a hypervisor stop/start: `win_reboot` restarts the guest only, the
QEMU process survives and keeps the old `maxmem`.

The floor is now per-task, not per-role - the twelve slowest tasks are
all 4.0-5.7 s of pure module round-trip. The next lever is task-count
consolidation (one idempotent script per role's Windows path, as
`dhs_firewall` already does), not transport.

## Producer launch and stop

### Linux LXC (Debian / Ubuntu / Rocky)

Launch the producers nohup-detached, writing logs to
`/var/log/dhs-<proto>.log`. Example for ACP2:

```sh
nohup /usr/local/bin/dhs producer acp2 serve \
  --tree /etc/dhs/tree.json \
  --port 2072 --host 0.0.0.0 --log-level info \
  > /var/log/dhs-acp2.log 2>&1 &
```

Hard-stop by walking `/proc/*/exe` symlinks pointing to
`/usr/local/bin/dhs` and killing each PID. Helper script at
`bin/stop-dhs.sh`.

### win11

OpenSSH Server on the VM, `dhs.exe` under `C:\dhs\dhs.exe` (see
`ansible/inventory/group_vars/windows.yml`). Producer launch via
`Start-Process -WindowStyle Hidden` with stdout/stderr redirected to
`C:\ProgramData\dhs\logs\`.

## OS updates (reboot-if-required, then continue)

`ansible/playbooks/fleet-update.yml` (#799) patches the fleet the way
production will: Windows Update on `win11` through `win_updates`
(security/critical/rollups/updates) with `reboot: true` — Ansible
performs the reboot only when Windows requires it, waits for SSH to
return on the same path and **continues** with the post-update tasks;
`apt`/`dnf` upgrades on the LXCs with `reboot` only when the OS flags it
(`/var/run/reboot-required`, `needs-restarting -r`). The control node is
never rebooted mid-play (reported instead — reboot it between runs).
Second run = 0 updates / 0 changes.

## Caveats

- `amwa/nmos-testing:latest` upstream regressed to the "Controller
  Testing Façade" on port 5001 after 2026-04-30. Pin to a pre-Façade
  tag or run from source (#173).
- desk-03 (the codeowner's workstation) sits on the OOB VLAN and cannot
  reach the DMZ via mDNS. `win11` (`.105`) on the DMZ provides the
  Windows mDNS live test surface instead.
- Cerebrum drops `v1.0` from `api.versions` on registration — real-peer
  fact, not our bug.
- The Synapse ACP1 emulator referenced by `ansible/inventory/group_vars/all.yml`
  (`10.6.239.113`) is on the office network, not the DMZ.

## Open work

Tracked in epic #780: static addressing (#786, `dhs_netaddr`), unique
hostnames (#783), actor-key convergence (#782), mDNS (#784, #797),
firewall (#785), host baseline (#800), OS updates + reboot (#799),
time sync + IPv6 (#804), win11 Ansible latency (#790, #812, #815).
Post-migration follow-ups: confirm Cerebrum's VLAN 600 address; rework
`dhs_netaddr` for VLAN 600 before re-enabling it.
