# docs/testbed.md — Test fleet inventory and access

Tracked, repo-local source of truth for the test fleet on the DMZ VLAN
(`10.100.0.0/24`, VLAN 100 on Proxmox bridge `vmbrAPPS`, gateway
pfSense `10.100.0.1`). The facts below were re-verified against the
Proxmox API (pve01, `10.6.224.5`) and the live guests on 2026-08-23;
the Ansible inventory (`ansible/inventory/`) carries the SAME facts per
host (`pve_vmid`, `nics`, `os_label`) — when one changes, both change
in the same PR (epic #780).

## Fleet

| Inventory name | Proxmox | Guest OS | eth0 mgmt-A (`ansible_host`) | eth1 mgmt-B | Role |
| --- | --- | --- | --- | --- | --- |
| `dhs-debian` | LXC 651 | Debian 12 | `10.100.0.101` | `10.100.0.111` | **Ansible control node**; dhs producer host; Docker |
| `dhs-ubuntu` | LXC 652 | Ubuntu 24.04 | `10.100.0.102` | `10.100.0.112` | dhs producer host |
| `dhs-rocky` | LXC 653 | Rocky 9.4 | `10.100.0.103` | `10.100.0.113` | dhs producer host |
| `dhs-tools` | LXC 655 | Ubuntu | `10.100.0.104` | — | tooling: Docker, AMWA NMOS Testing tool (`scripts/amwa/`), tshark |
| `win11` | VM 654 | Windows 11 Pro | `10.100.0.105` | — | Windows producer-parity row (ADR-0016); guest name `dhs-win11` |
| `cerebrum` | VM 601 `vm-cerebrum-stg-01` | Windows 11 | `10.100.0.5` | — | external reference peer (EVS Cerebrum staging) — real-peer integration target, not part of the converge set |

All LXCs are unprivileged with `nesting=1`. MAC addresses per NIC live
in `ansible/inventory/host_vars/<name>.yml` (`nics:`).

Addressing is **static, from the inventory** (`dhs_netaddr` role, #786):
one scheme across the fleet — eth0 = mgmt-A (`ansible_host`, default
gateway `10.100.0.1`), eth1 = eth0 + 10 = mgmt-B (no default route);
`.101–.105` / `.111–.113`, no overlaps, Cerebrum staging stays `.5`.
LXCs get it in the Proxmox CT config (`netX: …,ip=<addr>/24[,gw=…]`,
`nameserver`, `searchdomain`, written via the API, applied by a CT
reboot); the Windows VM gets it guest-side. No DHCP dependency (pfSense
only needs `.100–.120` excluded from its pool as hygiene).
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

`dhs-debian` (`.101`) runs every Ansible play — Linux hosts over SSH as
`root`, the Windows VM over SSH as `by-rune` (key auth; WinRM/NTLM +
password file is retired). It holds a clone of this (public) repo at
`~/acp` and its own ed25519 key (`root@dhs`), which is one of the three
fleet actor keys below. After a fresh clone run
`ansible-playbook playbooks/control-node.yml` (git-lfs + LFS pull of the
shipped assets, Ansible collections). Never drive the fleet from a
Windows workstation (PowerShell) — see ADR-0025 step 5.

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

Exactly three ed25519 identities are authorized on every host, managed
by the `dhs_access` role (#782); anything else is removed:

| Actor | Key comment | Purpose |
| --- | --- | --- |
| by-rune (agent, desk-03) | `by-rune-dhs-lxc` | agent SSH to every host |
| control node `.101` | `root@dhs` | Ansible → fleet |
| codeowner | (owner's ed25519) | direct SSH |

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

Time and IPv6 (#804): `win11` syncs w32time to the DMZ gateway
`10.100.0.1` + `pool.ntp.org` (set by `dhs_host`); the LXCs cannot set
their own clock — they inherit the Proxmox node's, which was measured
~9 min ahead on 2026-08-23 (pve01 NTP = `ansible-platform#168`);
`fleet-verify.yml` prints each host's offset vs the control node. IPv6:
the platform is dual-stack, but VLAN 100 currently offers no RA/DHCPv6,
so fleet NICs hold link-local IPv6 only; `fleet-verify.yml` reports
global IPv6 per host. The static IPv6 scheme (same shape as IPv4:
`<vlan-100 prefix>::101/::111` etc., set by `dhs_netaddr` in the Proxmox
`ip6=` field and guest-side on win11) lands as soon as the VLAN-100
prefix is known.

Roll the fleet to a new release by bumping `dhs_version` and converging
(run-twice = 0 changes). Layering: hypervisor = `infra-terraform-proxmox`
(import of the dhs guests tracked there), OS baseline/hardening =
`ansible-platform` (sshd 22222 / `by-systems`; applies when hardening
un-parks — the inventory then switches port/user), dhs application layer
= this repo's roles.

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
win11 Ansible latency (#790). pfSense hygiene (owner, any time): exclude
`10.100.0.100–120` from the DHCP pool.
