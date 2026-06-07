# NMOS multi-OS install runbook

Verified install + verification recipes for dhs NMOS Node/Registry, per
target OS. Used both for production install and the multi-distro LXC
test rig (#197) gating the Avahi/Bonjour DNS-SD backends (#194/#195/#196).

> **Service-per-device rule.** Every dhs connector runs as one managed
> service per one physical device — never multiplexed. See
> `internal/amwa/docs/ha.md` for the lease/handover protocol.

## Per-OS package matrix

| OS | DNS-SD daemon | Install command | Service name | Notes |
|---|---|---|---|---|
| Debian 12 | Avahi 0.8 | `apt-get install -y avahi-daemon avahi-utils libnss-mdns dbus` | `avahi-daemon` | systemd 252 — LXC needs `nesting=1` |
| Ubuntu 24.04 | Avahi 0.8 | `apt-get install -y avahi-daemon avahi-utils libnss-mdns dbus` | `avahi-daemon` | systemd 255 — LXC needs `nesting=1` |
| Rocky 9 / RHEL 9 | Avahi 0.8 | `dnf install -y avahi avahi-tools dbus` | `avahi-daemon` | `wireshark-cli` for tshark; `nss-mdns` is in EPEL only |
| Windows 11 / Server | Bonjour (Apple) | install **Bonjour Print Services** + **Bonjour SDK for Windows** | `Bonjour Service` | runtime DLL `C:\Windows\System32\dnssd.dll`; SDK at `C:\Program Files\Bonjour SDK\` |
| macOS | Bonjour (built-in) | none — `mDNSResponder` ships with the OS | `mDNSResponder` (launchd) | always present, no install |
| any (slim container, no daemon) | dhs stdlib floor | none | n/a | always works; no kernel-callback perf |

## Debian 12 / Ubuntu 24.04 — full install

```sh
export DEBIAN_FRONTEND=noninteractive
echo "wireshark-common wireshark-common/install-setuid boolean true" | debconf-set-selections
apt-get update -qq
apt-get install -y -qq avahi-daemon avahi-utils libnss-mdns dbus tshark iputils-ping curl ca-certificates
systemctl enable --now avahi-daemon
systemctl is-active avahi-daemon          # → active
avahi-daemon --version | head -1          # → avahi-daemon 0.8
```

## Rocky 9 / RHEL 9 — full install

```sh
dnf install -y -q avahi avahi-tools dbus wireshark-cli iputils curl ca-certificates
systemctl enable --now avahi-daemon
systemctl is-active avahi-daemon          # → active
avahi-daemon --version | head -1          # → avahi-daemon 0.8
```

Optional EPEL extras (`nss-mdns` for `.local` resolver integration):
```sh
dnf install -y -q epel-release
dnf install -y -q nss-mdns
```

## Windows 11 / Windows Server — full install

```powershell
# 1. Bonjour Service runtime (~5 MB, free from Apple)
#    Provides:  C:\Windows\System32\dnssd.dll
#               Bonjour Service (mDNSResponder)
Start-Process -Wait -FilePath "<path-to>\BonjourPSSetup.exe" -ArgumentList '/quiet'

# 2. Bonjour SDK for Windows (~9 MB, free from Apple, free Apple ID required to download)
#    Provides:  C:\Program Files\Bonjour SDK\Include\dns_sd.h
#               C:\Program Files\Bonjour SDK\Lib\x64\dnssd.lib
Start-Process -Wait -FilePath "<path-to>\bonjoursdksetup.exe" -ArgumentList '/quiet'

# 3. Verify
Get-Service "Bonjour Service" | Select-Object Status, Name
Test-Path "C:\Windows\System32\dnssd.dll"
Test-Path "C:\Program Files\Bonjour SDK\Include\dns_sd.h"
Test-Path "C:\Program Files\Bonjour SDK\Lib\x64\dnssd.lib"
```

Both installers archived in-tree at
`internal/amwa/assets/{BonjourPSSetup.exe, bonjoursdksetup.exe}`.

LSA Protection blocks `mdnsNSP.dll` on modern Windows 11 — **harmless
for dhs**. The NSP only hooks `getaddrinfo` for `.local` resolution
from arbitrary apps; dhs talks to `mDNSResponder` directly via
`dnssd.dll` and is unaffected. Click Cancel + tick "Don't show again"
on the dialog if it appears.

## macOS — no install

Bonjour is always present. dhs links against `<dns_sd.h>` from the
SDK that ships with Xcode Command Line Tools. To verify:

```sh
xcrun --sdk macosx --show-sdk-path
ls $(xcrun --sdk macosx --show-sdk-path)/usr/include/dns_sd.h
launchctl print system/com.apple.mDNSResponder | head -5
```

## Verification — per-host mDNS smoke test

Every host must see the same Registries / peers on the LAN. Run on each
host:

| OS | Browse command | Expected |
|---|---|---|
| Linux (any) | `avahi-browse -a -r -t -p \| grep _nmos` | one row per Registry advertising `_nmos-register._tcp` and/or `_nmos-registration._tcp` |
| Windows | `dns-sd -B _nmos-register._tcp` | live Add/Remove rows as Registries come/go |
| macOS | `dns-sd -B _nmos-register._tcp` | same |

Successful cross-host result on the dhs DMZ VLAN (LXC rig 2026-05-02):

```
=;eth0;IPv4;Cerebrum;_nmos-register._tcp;local;...;10.100.0.5;8080;
   "api_auth=false" "pri=0" "api_proto=http" "api_ver=v1.1,v1.2,v1.3"
=;eth0;IPv4;Cerebrum;_nmos-registration._tcp;local;...;10.100.0.5;8080;
   "api_auth=false" "pri=0" "api_proto=http" "api_ver=v1.1,v1.2,v1.3"
```

Both modern (`_nmos-register._tcp`, IS-04 v1.2+) and legacy
(`_nmos-registration._tcp`, IS-04 v1.0/v1.1) names visible — confirms
the dual-name watcher (#193) browses what real-peer Registries publish.

## Known quirks

| OS | Quirk | Workaround |
|---|---|---|
| Debian/Ubuntu unprivileged LXC | systemd 252+ needs cgroup v2 unified | enable `nesting=1` on the CT |
| Rocky 9 in LXC | sshd not auto-enabled in some templates | `pct exec <CTID> -- bash -c 'dnf install -y openssh-server && systemctl enable --now sshd'` |
| Windows 11 LSA Protection | blocks `mdnsNSP.dll` from loading into LSASS | ignore — dhs doesn't use the NSP |
| Docker Desktop Win/macOS | VM layer breaks mDNS multicast to LAN | dev only — never use for prod; use bare-metal, Docker on Linux, or k3s/K8s `hostNetwork: true` |
| K8s default CNI | drops multicast | DaemonSet with `hostNetwork: true`, OR use `unicast-registry` mode via real DNS-SD records |
| Slim container w/o daemon | Avahi/Bonjour absent | dhs falls back to stdlib mDNS automatically — sub-optimal latency but functional |

## Multicast on the LAN

mDNS is L2 multicast on `224.0.0.251:5353`. On a flat broadcast LAN
(typical for SMPTE 2110 / AES67 / PTP plants) it just works between any
two hosts. Cross-VLAN requires either a multicast reflector
(Avahi-reflector, mDNS reflector on pfSense) or PIM routing — usually
already in place wherever PTP / 2110 is deployed.

If multicast is genuinely blocked (cloud VPCs, IT-managed enterprise
LANs that disable multicast by policy), use the unicast DNS-SD mode:
populate the `_nmos-register._tcp` SRV/PTR/TXT records on a real DNS
server (Unbound on pfSense in this fleet — see `dns-sd-unbound.md`).

## Linking the architecture

| Doc | What |
|---|---|
| [architecture.md](architecture.md) | Top-level NMOS Node + Registry layout |
| [ha.md](ha.md) | One-service-per-device rule + lease/handover |
| [dns-sd-unbound.md](dns-sd-unbound.md) | Unicast DNS-SD records for pfSense Unbound |
| [cerebrum-interop.md](cerebrum-interop.md) | Cerebrum v1.1/v1.2/v1.3 advertising shape (matches what we observe live on `10.100.0.5`) |

## Test rig recipe (Proxmox LXC — issue #197)

For repeatable cross-distro mDNS validation:

| Distro | CTID | IP | systemd | Avahi | Tshark |
|---|---|---|---|---|---|
| Debian 12 | 200 | 10.100.0.102 | 252 | 0.8 | 4.0.17 |
| Ubuntu 24.04 | 201 | 10.100.0.103 | 255 | 0.8 | 4.2.2 |
| Rocky 9.4 | 202 | 10.100.0.104 | 252 | 0.8 | 3.4.10 |

Per-CT settings:

```
--unprivileged 1
--features nesting=1     # required for systemd 250+
--cores 1 --memory 1024 --swap 512
--rootfs local-lvm:4
--net0 name=eth0,bridge=vmbr0,tag=<DMZ_VLAN_ID>,ip=dhcp
```

SSH access from Win11 dev box (key `~/.ssh/by-rune_lxc`):

```powershell
$key = "$env:USERPROFILE\.ssh\by-rune_lxc"
ssh -i $key root@10.100.0.102   # debian
ssh -i $key root@10.100.0.103   # ubuntu
ssh -i $key root@10.100.0.104   # rocky
```
