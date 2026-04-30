# NMOS — host firewall recipes (Windows / Linux / macOS)

Per-OS recipes for opening the ports `dhs` and peer Registries need on the
**host** (workstation, server, container). Network-layer firewall (pfSense
ACLs between subnets) is a separate concern; this doc only covers the
host's local firewall.

> **Why we don't blanket-open mDNS in the DMZ.** mDNS chatter on UDP 5353
> reflects every `_*._tcp/udp` service the host runs. In a DMZ topology
> prefer **Mode B unicast DNS-SD** (per
> [`dns-sd-unbound.md`](dns-sd-unbound.md)) and skip the inbound 5353 rule
> entirely. The mDNS rule below is needed only for Mode A (full mDNS) on a
> trusted LAN.

---

## Port matrix by role

| Role | Binary invocation | Inbound TCP | Inbound UDP | Notes |
|---|---|---|---|---|
| **Node** | `dhs producer nmos serve` | **8080** (Node API HTTP — Phase 1 step #3) | 5353 (Mode A only) | Default port; override with `--bind`. |
| **Registry** | `dhs registry nmos serve` | **8235** (Registration + Query HTTP — Phase 1 step #4) | 5353 (Mode A only) | Default port; override with `--bind`. |
| **Controller** | `dhs consumer nmos discover` / `walk` | none — outbound only | 5353 (Mode A only — to receive mDNS responses) | No HTTP listener. |
| **Cerebrum host** (peer Registry) | EVS Cerebrum app | 8080 | 5353 (Mode A only) | Per [`cerebrum-interop.md`](cerebrum-interop.md) §4. |

Phase 1 step #1 (PR #149) ships discovery only — no HTTP REST yet. The
TCP rules below take effect once Phase 1 #3/#4 land; add them now so the
firewall doesn't become a surprise blocker later.

---

## Windows (PowerShell, run as Administrator)

```powershell
# --- Workstation running `dhs registry nmos serve` ---
New-NetFirewallRule -Name "DHS-NMOS-Registry-HTTP" `
    -DisplayName "dhs NMOS Registry (TCP 8235)" `
    -Direction Inbound -Protocol TCP -LocalPort 8235 -Action Allow

# --- Workstation running `dhs producer nmos serve` (Node API) ---
New-NetFirewallRule -Name "DHS-NMOS-Node-HTTP" `
    -DisplayName "dhs NMOS Node API (TCP 8080)" `
    -Direction Inbound -Protocol TCP -LocalPort 8080 -Action Allow

# --- mDNS — Mode A only; skip in Mode B / DMZ ---
New-NetFirewallRule -Name "DHS-NMOS-mDNS" `
    -DisplayName "mDNS (UDP 5353)" `
    -Direction Inbound -Protocol UDP -LocalPort 5353 -Action Allow

# --- Cerebrum host (Windows Server) — Hosted Registry mode ---
# These were the A4/A5 steps in the install runbook.
New-NetFirewallRule -Name "Cerebrum-NMOS-Reg-8080" `
    -DisplayName "Cerebrum NMOS Registry (TCP 8080)" `
    -Direction Inbound -Protocol TCP -LocalPort 8080 -Action Allow
New-NetFirewallRule -Name "Cerebrum-NMOS-mDNS" `
    -DisplayName "Cerebrum mDNS (UDP 5353)" `
    -Direction Inbound -Protocol UDP -LocalPort 5353 -Action Allow
```

Restrict to specific subnets with `-RemoteAddress`:

```powershell
... -RemoteAddress 10.100.0.0/16,10.6.224.0/20
```

Roll back: `Remove-NetFirewallRule -Name "DHS-NMOS-Registry-HTTP"`.

---

## Linux

### ufw (Ubuntu / Debian)

```bash
sudo ufw allow 8235/tcp  comment 'dhs NMOS Registry'
sudo ufw allow 8080/tcp  comment 'dhs NMOS Node API'
sudo ufw allow 5353/udp  comment 'mDNS (Mode A only)'
```

Subnet-scope: `sudo ufw allow from 10.100.0.0/16 to any port 8235 proto tcp`.

### firewalld (RHEL / Rocky / Fedora — incl. CI's rocky9 + rhel9-ubi targets)

```bash
sudo firewall-cmd --permanent --add-port=8235/tcp           # dhs Registry
sudo firewall-cmd --permanent --add-port=8080/tcp           # dhs Node API
sudo firewall-cmd --permanent --add-service=mdns            # Mode A only
sudo firewall-cmd --reload
```

Pin to a zone (e.g. `internal`): add `--zone=internal` to every command.

### iptables (raw — Alpine, embedded)

```bash
sudo iptables -A INPUT -p tcp --dport 8235 -j ACCEPT        # dhs Registry
sudo iptables -A INPUT -p tcp --dport 8080 -j ACCEPT        # dhs Node API
sudo iptables -A INPUT -p udp --dport 5353 -j ACCEPT        # mDNS (Mode A)
sudo iptables-save | sudo tee /etc/iptables/rules.v4        # persist
```

### nftables (Debian 11+, current default)

```bash
sudo nft add rule inet filter input tcp dport 8235 accept
sudo nft add rule inet filter input tcp dport 8080 accept
sudo nft add rule inet filter input udp dport 5353 accept
sudo nft list ruleset > /etc/nftables.conf                  # persist
```

---

## macOS

macOS Application Firewall is **per-app**, not per-port. On first bind
the OS prompts to allow incoming connections for `dhs.exe`. CLI overrides
when there's no GUI session:

```bash
# Add dhs as an allowed application
sudo /usr/libexec/ApplicationFirewall/socketfilterfw \
    --add /usr/local/bin/dhs
sudo /usr/libexec/ApplicationFirewall/socketfilterfw \
    --unblockapp /usr/local/bin/dhs

# Confirm
sudo /usr/libexec/ApplicationFirewall/socketfilterfw \
    --getappblocked /usr/local/bin/dhs
```

If you've explicitly enabled stealth mode (`--setstealthmode on`),
inbound mDNS responses will be dropped. Either disable stealth for the
NMOS workflow or stay on Mode B exclusively.

For container / lab deployments using `pf(4)` directly, add to
`/etc/pf.conf` and reload with `sudo pfctl -f /etc/pf.conf -e`:

```pf
pass in proto tcp from 10.100.0.0/16 to any port 8235 keep state
pass in proto tcp from 10.100.0.0/16 to any port 8080 keep state
pass in proto udp from any           to any port 5353 keep state
```

---

## Verification

| Check | Command |
|---|---|
| TCP listener bound | `netstat -an \| grep 8235` (Win/Linux) / `lsof -nP -iTCP:8235 -sTCP:LISTEN` (Linux/macOS) |
| Reachable from peer | `Test-NetConnection -ComputerName <host> -Port 8235` (Win) / `nc -zv <host> 8235` (Linux/macOS) |
| HTTP response | `curl http://<host>:8235/x-nmos/registration/` once Phase 1 #4 lands |
| mDNS observable | `dns-sd -B _nmos-register._tcp` (mDNSResponder, Win/macOS) / `avahi-browse -r _nmos-register._tcp` (Linux) |

Equivalent dhs end-to-end:

```
dhs.exe consumer nmos discover --mdns --service _nmos-register._tcp --timeout 5s
```

---

## Cross-references

- [`dns-sd-unbound.md`](dns-sd-unbound.md) — Mode B unicast DNS-SD via
  pfSense Unbound (preferred when subnets cross a DMZ).
- [`cerebrum-interop.md`](cerebrum-interop.md) §4 — Cerebrum's host
  prerequisites including the matching `urlacl` step.
- [`internal/amwa/CLAUDE.md`](../CLAUDE.md) Quirks #1 — four deployment
  modes (A/B/C/D) and when each requires UDP 5353.
- [`ha.md`](ha.md) — multi-Registry HA notes; ST 2022-7 dual-network
  deployments need the rules duplicated per NIC.
