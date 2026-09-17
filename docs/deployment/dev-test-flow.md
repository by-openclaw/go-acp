# Dev & Test Flow — VLAN600 fleet

The single, authoritative flow for developing `dhs` and deploying/testing
it against the lab fleet. If any other doc disagrees with this one on
addresses, roles, or the deploy path, this doc + the Ansible inventory
(`ansible/inventory/`) win (ADR-0015).

## Where things run

| Role | Host | IP (VLAN600) | Notes |
| --- | --- | --- | --- |
| **Desk** (dev) | this workstation | — | Windows, **has internet + GitHub**. All code + git + PRs happen here. Cannot run Ansible directly (no native Windows control node). |
| **Control node** | `dhs-debian` | `10.6.250.101` | Runs **every** Ansible play. Also hosts the AMWA plant registry (`:8235`). |
| Build + tooling | `dhs-tools` | `10.6.250.104` | Go build host + AMWA NMOS Testing tool (Docker) + tshark. **Only host that needs internet.** |
| Runtime nodes / clients | `dhs-ubuntu`, `dhs-rocky` | `10.6.250.102`, `.103` | dhs producer/consumer targets. |
| Windows parity | `dhs-win11` | `10.6.250.105` | ADR-0016 Windows row (guest static unconfirmed post-migration). |
| External peer | Cerebrum (EVS) | — | Real controller; interop target, not part of the converge set. |

Fabric: MGMT_CTRL `10.6.240.0/20`, gateway `10.6.255.254`, **no internet on
the fleet** (except the one host below). The retired `10.100.0.0/24` plan
is dead — do not use it.

## Golden rules

1. **Code is built and PR'd on the desk.** The fleet never `git pull`s from
   GitHub (no internet).
2. **Provisioning is Ansible only, run ON the control node.** No PowerShell
   or `.sh` provisioning drivers (ADR-0025 §5).
3. **Code reaches the fleet as a git bundle** (desk → control node over SSH).

## Access (desk → fleet)

SSH aliases live in the desk `~/.ssh/config`. Linux LXC log in as `root`
(key `by-rune_lxc`); `dhs-win11` as `by-rune`.

```bash
ssh dhs-debian     # control node + registry  (10.6.250.101, root)
ssh dhs-tools      # build + tooling           (10.6.250.104, root)
```

## Dev flow (desk)

```bash
go vet ./internal/amwa/... && go test -count=1 ./internal/amwa/... ./cmd/...
git checkout -b <type>/<slug> main
# edit, commit (Fable5 co-author footer)
git push -u origin <branch>
gh pr create ...
gh pr checks <n> --watch          # CI green
gh pr merge <n> --squash --delete-branch
gh run watch <main-run-id> --exit-status   # main green
```

ADR-0014 cycle: issue → branch → tests → PR → CI green → `@yboujraf`
approval → merge → watch main.

## Deploy to the AMWA plant

The plant (registry + 22 nodes) is the `dhs_amwa_plant` Ansible role,
built on `dhs-tools` from committed source and served on `dhs-debian`.

```bash
# 1. Desk — bundle main and ship it to the control node
git bundle create %TEMP%\plantfull.bundle main
scp %TEMP%\plantfull.bundle dhs-debian:/tmp/plantfull.bundle

# 2. Control node — sync the plant checkout to main (origin = the bundle)
ssh dhs-debian 'cd /root/acp-plant && git fetch origin && git checkout main && git reset --hard origin/main'

# 3. Control node — converge (idempotent; re-run = changed=0)
ssh dhs-debian 'cd /root/acp-plant/ansible && ansible-playbook playbooks/amwa-plant.yml'
```

Facts: plant checkout = `/root/acp-plant`; registry systemd unit
`dhs-nmos-registry` runs `/opt/dhs-amwa-plant/dhs registry nmos serve
--bind :8235`. A redeploy **restarts the registry**, so after it comes
back **reconnect Cerebrum once** (Cerebrum 2.8.17 does not auto-refresh
its Query-API subscription).

## Verify (from the desk, over HTTP)

```bash
# nodes registered + heartbeat freshness (GC at 12 s)
curl http://10.6.250.101:8235/x-nmos/query/v1.3/nodes
curl http://10.6.250.101:8235/x-nmos/registration/v1.3/health/nodes/<id>
# active Query-WS subscriptions (0 = nothing is feeding a controller)
curl http://10.6.250.101:8235/x-nmos/query/v1.3/subscriptions
```

## Internet policy (VLAN600)

Only **`dhs-tools` (10.6.250.104)** needs outbound internet — Go module
fetch during build + AMWA Testing-tool container image pulls. Allow
**TCP 443** and **DNS via the fleet resolver** (never direct `1.1.1.1`,
which bypasses the DoT / pfBlockerNG chain). Every other host — registry,
runtime nodes, win11 — needs none.
