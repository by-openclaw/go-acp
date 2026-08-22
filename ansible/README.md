# dhs ACP1 — Ansible role + playbook

Idempotent ACP1 device configuration by wrapping the `dhs` CLI `ensure` verb in
an Ansible role. The same role runs on **Linux (SSH)** and **Windows (WinRM)**
clients, so you prove the multi-OS `dhs` binary (ADR-0016) and the `ensure`
idempotency contract behave identically on every platform in the Proxmox lab
(dhs-debian / dhs-ubuntu / dhs-rocky / dhs-tools / win11 — see
`docs/testbed.md`; every play runs from the control node dhs-debian).

This is the **authoritative idempotency layer**. The PowerShell scripts under
`scripts/acp1/` are the fast smoke layer; here a real `ansible-playbook` run
*is* the consumer, so "run twice → 0 changes" is proven, not simulated.

## Why wrap the CLI (not a custom module)

The CLI `ensure` already owns all the protocol logic — read current → predict the
device clamp → validate → set-if-different — and emits Ansible-friendly JSON
(`{changed|would_change, previous, current}`) plus the exit-code contract
(`0` ok / `1` runtime / `2` validation). The role just maps Ansible semantics
onto it, so there is one source of truth and no duplicated wire logic. A custom
`dhs_value` module is a later nicety, not a prerequisite.

| Ansible | → dhs CLI | Effect |
|---|---|---|
| `changed_when` reads the JSON | `ensure --json` `changed` field | `changed` reflects the device, not "the command ran" |
| `--check` mode | `ensure --check` (task `check_mode: false`) | `ansible-playbook --check` is a real dry-run |
| `failed_when: rc != 0` | exit `1`/`2` | runtime + validation failures fail the play; bad input is exit 2 |

## Layout

```
ansible/
  ansible.cfg
  requirements.yml          # ansible.windows collection
  inventory/
    hosts.ini               # linux (ssh) + windows (winrm) — set your IPs
    group_vars/{all,linux,windows}.yml
  roles/dhs_acp1/
    defaults/main.yml
    tasks/{main,linux,windows}.yml
    meta/main.yml
  playbooks/site.yml
```

## Prerequisites

Control node (Linux or WSL — Ansible is not a native-Windows control node):

```bash
pip install ansible pywinrm
ansible-galaxy collection install -r requirements.yml
```

Build the per-OS `dhs` binaries the role deploys (from the repo root):

```bash
GOOS=linux   GOARCH=amd64 go build -o bin/dhs-linux-amd64     ./cmd/dhs
GOOS=windows GOARCH=amd64 go build -o bin/dhs-windows-amd64.exe ./cmd/dhs
```

(PowerShell: `$env:GOOS='linux'; $env:GOARCH='amd64'; go build -o bin/dhs-linux-amd64 ./cmd/dhs`)

## Configure

- `inventory/hosts.ini` — set `ansible_host` for each LXC + each Windows host.
- `group_vars/all.yml` — `dhs_device` (the ACP1 emulator / real rack — the oracle,
  never our own provider) and `dhs_objects` (the desired state to converge).
- `group_vars/linux.yml` / `windows.yml` — connection user, binary paths. Put the
  WinRM password in `ansible-vault`, never plaintext.

## Windows hosts (WinRM) — one-time setup

Any Windows host (Windows 11 **or** Windows Server) is driven over WinRM the
standard way. Run the bootstrap once per host, in an **elevated** PowerShell:

```powershell
powershell -ExecutionPolicy Bypass -File windows\configure-winrm.ps1
```

It enables the WinRM listener + Negotiate/NTLM auth and — the part that's easy
to miss — sets `LocalAccountTokenFilterPolicy=1`. Without that, a **local**
admin's token is filtered on network logon and WinRM rejects auth (HTTP 401 /
SSPI `0x8009030d`) even with a correct password and a working interactive
login. After it runs, verify from the control node:

```bash
ansible <host> -i inventory/hosts.ini -m ansible.windows.win_ping   # -> pong
```

Supply the password with `ansible-vault` (or `--ask-pass`), never plaintext.
Verified: `dhs-win11` converges over WinRM with run-twice = 0 changed.

## Run

```bash
cd ansible
ansible-playbook playbooks/site.yml            # apply
ansible-playbook playbooks/site.yml --check    # dry-run (maps to ensure --check)
ansible-playbook playbooks/site.yml            # again → every host: ok, changed=0
```

The second apply reporting **0 changed on every OS** is the idempotency proof.
An out-of-range numeric (e.g. a `NetwPrefix` above its max) converges to the
device's clamped value and stays idempotent — the CLI predicts the clamp
client-side. A bad value (unknown enum, wrong type) fails the play with exit 2.

## Test (automated idempotency assertion)

`playbooks/site.yml` *converges* and leaves the run-twice check to your eye.
`playbooks/test-idempotency.yml` is the **automated test**: it runs `ensure`
twice against the device in one play and **asserts** the second pass reports
`changed=false` for every object — a real pass/fail, the Ansible test tier for
ACP1 (ADR-0007). It drives the local binary (`connection: local`), so it needs
no client fleet, and **skips cleanly** when no device is set.

```bash
# From the repo root — builds the linux binary, then runs the assertion:
make test-ansible ACP1_TEST_HOST=10.6.239.113

# Or directly:
cd ansible
ACP1_TEST_HOST=10.6.239.113 DHS_BIN=../bin/dhs-linux-amd64 \
  ansible-playbook -i inventory/hosts.ini playbooks/test-idempotency.yml
```

A non-idempotent object fails the `assert` task with the exact
`group/label` that changed on the second run. `dhs_objects` / `dhs_port`
come from `group_vars/all.yml`; the device host is `ACP1_TEST_HOST`-overridable.

## Status

Authored against the verified CLI contract (`--json` `changed`, `--check`,
exit `0/1/2` — all green vs the Synapse emulator). The live multi-OS run-twice
proof executes on your Proxmox control node; this host has no Ansible runtime.
