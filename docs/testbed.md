# docs/testbed.md — Test fleet inventory and SSH access mesh

Tracked, repo-local source of truth for the test fleet on the DMZ VLAN
(`10.100.0.0/24`, routed via pfSense).

## Fleet

| Hostname | IP | Role | OS | Notes |
| --- | --- | --- | --- | --- |
| `dhs-debian` | `10.100.0.102` | dhs producer host | Debian 12 | OS-compat row, all four producers concurrently |
| `dhs-ubuntu` | `10.100.0.103` | dhs producer host + Ansible controller | Ubuntu 24.04 | Current ACP2 reconnect/keepalive test target |
| `dhs-rocky` | `10.100.0.104` | dhs producer host | Rocky 9.4 | OS-compat row |
| `dhs-tools` | `10.100.0.105` | tooling host | Ubuntu 24.04 | Docker, AMWA NMOS Testing tool, tshark, Wireshark dissector validator |
| `dhs-win11` | `10.100.0.106` | Windows producer host | Windows 11 Pro | Multi-OS matrix per ADR-0016; producer parity check for the Windows row |
| `cerebrum` | `10.100.0.5` | external reference peer | (vendor appliance) | NMOS Registry + Node — not part of the test fleet, used as a real-peer integration target |

The three `dhs-*` Linux producer hosts validate the OS-compat axis from
ADR-0016 without requiring vendor hardware. `dhs-tools` hosts the AMWA
Testing tool peer + isolated Docker bridge. `dhs-win11` covers the
Windows producer parity row required by ADR-0016.

## Producer port plan (every Linux LXC + dhs-win11)

| Connector | Wire | Port |
| --- | --- | --- |
| ACP1 | UDP + TCP | `2071` |
| ACP2 | TCP (AN2) | `2072` |
| Ember+ | TCP (S101) | `9000` |
| Probel SW-P-08 | TCP (DLE) | `2008` |
| AMWA NMOS Node | HTTP | `18080` |

All producers run concurrently on the same host; one port per
connector. The same producer binary is driven by `dhs consumer` and by
external peers (Cerebrum @ `.5`, AMWA Testing tool in Docker on `.105`)
so wire behaviour can be compared across drivers.

## SSH access mesh

The agent (Claude, git author `by-rune`) needs passwordless SSH to and
from every node in the fleet so it can launch / restart / probe
producers from any host.

### Key material

- Single ed25519 keypair, no passphrase: `id_dhs_testbed` (private),
  `id_dhs_testbed.pub` (public).
- Generated once on `BY-DESK-03`, then distributed to every node so the
  same `~/.ssh/id_dhs_testbed` exists on every Linux LXC and on the
  Windows host. This makes the mesh symmetric — any node can SSH any
  other node with the same key.
- The agent shell on `BY-DESK-03` continues to use the existing key
  `~/.ssh/by-rune_lxc` for outbound SSH (already present and working).
  The new `id_dhs_testbed` mesh is for fleet-internal SSH and replaces
  the desk-03-only path.

### Account

Login user is `root` on every Linux LXC (the `by-rune` user is
rejected by sshd config on the LXCs; only `root` accepts the key —
verified 2026-05-10). On `dhs-win11` the equivalent is the local
Administrator user with OpenSSH server enabled.

### Authorised destinations (mesh)

| Source → | dhs-debian | dhs-ubuntu | dhs-rocky | dhs-tools | dhs-win11 | desk-03 |
| --- | --- | --- | --- | --- | --- | --- |
| dhs-debian | self | yes | yes | yes | yes | (pull only) |
| dhs-ubuntu | yes | self | yes | yes | yes | (pull only) |
| dhs-rocky | yes | yes | self | yes | yes | (pull only) |
| dhs-tools | yes | yes | yes | self | yes | (pull only) |
| dhs-win11 | yes | yes | yes | yes | self | (pull only) |
| desk-03 | yes | yes | yes | yes | yes | self |

`desk-03` initiates SSH outbound. The fleet does not SSH back into
`desk-03` (the codeowner's workstation is on the OOB VLAN behind
pfSense and is not reachable from the DMZ).

### Distribution status

- desk-03 → fleet: working via `~/.ssh/by-rune_lxc` (legacy key).
- Fleet ↔ fleet: **NOT YET DISTRIBUTED.** The new `id_dhs_testbed`
  keypair has not been generated or pushed. Tracked as a separate
  infra task — see "Open work" below.

## Producer launch and stop

### Linux LXC (Debian / Ubuntu / Rocky)

Launch the four producers nohup-detached, writing logs to
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

### dhs-win11

OpenSSH server on the Windows host, `dhs.exe` placed under
`C:\Program Files\dhs\dhs.exe`. Producer launch via PowerShell
`Start-Process` with `-WindowStyle Hidden` and a redirect of
stdout/stderr to log files under `C:\ProgramData\dhs\logs\`.

## Caveats

- `amwa/nmos-testing:latest` upstream regressed to the "Controller
  Testing Façade" on port 5001 after 2026-04-30. Pin to a pre-Façade
  tag or run from source.
- Win11 desk-03 (the codeowner's workstation) sits on the OOB VLAN and
  cannot reach the DMZ via mDNS. `dhs-win11` (`.106`) on the DMZ
  provides the Bonjour live test surface instead.
- Cerebrum drops `v1.0` from `api.versions` on registration — real-peer
  fact, not our bug.

## Pending rig changes

- Add `eth1` second NIC per `dhs-*` Linux LXC for dual-controller
  redundancy testing per ADR-0022 (N-endpoints/Frame). Planned
  pairings: `.102/.108`, `.103/.109`, `.104/.107`.
- Bring `dhs-win11` (`.106`) online with the four producers running
  to complete the multi-OS coverage matrix.

## Open work

- Generate `id_dhs_testbed` keypair on `BY-DESK-03`.
- Distribute private + public to every node in the fleet (LXCs +
  Windows) and add the public to every `authorized_keys`.
- Verify the mesh: from each node, `ssh -i ~/.ssh/id_dhs_testbed
  <every-other-node>` succeeds without prompting.

These are infra actions that require codeowner-side execution (key
distribution touches credentials and is therefore not autonomous).
