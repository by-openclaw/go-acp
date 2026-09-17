# VLAN600 consolidation — 2026-08-29

The whole NMOS lab moved from the DMZ (`10.100.0.0/24`, retired) onto
the plant's MGMT_CTRL VLAN 600 (`10.6.240.0/20`) — the TR-1001-style
design: control plane on management, media VRFs untouched, in-band
reachable via the fabric's existing VRF leaks. **No internet on the
fleet by design.**

## Address plan

| guest | old | new |
|---|---|---|
| dhs-debian (lxc 651) — registry :8235 + node :18080 | 10.100.0.101 | **10.6.250.101/20** |
| dhs-ubuntu (lxc 652) | 10.100.0.102 | **10.6.250.102/20** |
| dhs-rocky (lxc 653) | 10.100.0.103 | **10.6.250.103/20** |
| dhs-tools (lxc 655) — go+gcc race host | 10.100.0.104 | **10.6.250.104/20** |
| dhs.win11 (qemu 654) | 10.100.0.105 | bridge flipped; static set in guest |
| Cerebrum staging (qemu 601) | 10.100.0.5 | bridge flipped; static set in guest |

GW `10.6.255.254` (fabric VRRP VIP, vrf MGMT_CTRL) — chosen over the
pfSense leg so in-band traffic rides the fabric leak natively.
DNS `10.6.240.1`. Applied via the Proxmox API (bridge `vmbrMGMT`,
static `ip=`/`gw=`, `nameserver`/`searchdomain` options); stale
metric-100 defaults from the DMZ era removed in-guest.

## What the move bought (all verified on migration day)

- **Neuron mgmt (10.6.255.102) is on-link** — no firewall hop, no
  reflector; native mDNS sees `_nmos-node` directly on the segment.
- **In-band reachable via the fabric VRF leak**: from VLAN600, RED
  `10.6.40.0/24` and BLUE `10.7.40.0/24` answer (SVIs `.254` + the
  Neuron's media addresses) — the leak policies
  (`RM-MGMT-TO-RED/BLUE` ↔ `RM-RED/BLUE-TO-MGMT`, prefix
  `10.6.240.0/20`) cover the fleet with zero fabric changes. The DMZ
  never could: it wasn't in any leak prefix-list.
- pfSense is out of the control path entirely (it now only routes
  operator/OOB access into the VLAN).

## Migration-day repoints

- Registry + node on `.101` restarted with
  `--advertise-host 10.6.250.101:…`.
- Neuron **Registry Addr Override** → `10.6.250.101:8235` (device UI).
- Cerebrum Query Server entry → `10.6.250.101:8235`, Active ticked.

## pfSense de-scoping checklist (DMZ era artifacts to remove)

- MGMT/DMZ pass rules added for the old topology: `8080/8081 → .5`,
  `8235 → .101`, `10641 → .101`, `udp/5353 → 224.0.0.251`.
- **Avahi package (reflector)** — obsolete: everything shares one L2
  now, and the reflector was observed re-announcing stale records and
  leaking NAS adverts.
- Unbound `by-systems.arpa` NMOS DNS-SD records pointing at
  `10.100.0.x` — stale, removed. Unicast DNS-SD was then re-authored
  on dnsmasq on `.101` (`/etc/dnsmasq.d/nmos-lab.conf`, records →
  `10.6.250.101`) and verified end-to-end same day: a node in
  `--unicast --resolver 10.6.250.101 --domain by-systems.arpa` mode
  logged `registry discovered (unicast DNS-SD)` and registered. With
  that, all three discovery modes — Manual, mDNS, sd-DNS — are proven
  against the registry on the VLAN600 topology.
- Keep: MGMT interface, anti-lockout, DHCP pool (serves `lxc 202`;
  static fleet addresses live in `10.6.250.x`, clear of observed
  leases).

## Consequences to remember

- **No internet on the fleet**: no package installs, no git
  fetch/push from lab hosts. The repo work happens on the operator
  workstation (OOB `10.6.239.x`, which still has internet and reaches
  VLAN600 via pfSense). `.104`'s Go toolchain + module cache were
  installed pre-migration; `go test -race` still runs offline.
- NTP for the fleet: fabric + pfSense (`10.6.240.1`).
- Old `10.100.0.x` addresses linger in older doc pages and lab zone
  files — historical only.
