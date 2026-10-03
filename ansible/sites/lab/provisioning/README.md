# Provisioning — lab site

Base setup and multicast plan for every Neuron and FusioN of the lab,
as **dhs export-format CSV**, applied with **`dhs consumer <proto>
import`**, ensured by **Ansible** (run twice = 0 changes). Nothing here
talks to a device except dhs.

## Files

| file | role | written by |
|---|---|---|
| `site.csv` | plant-wide values: NTP main/backup, PTP domain and profile, dhs registry, syslog | hand |
| `devices.csv` | one row per device: type, mgmt IP, planes (`RED` or `RED+BLUE`), channels, NMOS label, static addressing, FEC per plane, multicast blocks | hand (blocks: `plan`) |
| `mcast-ranges.csv` | essence × plane → `/16` prefix + UDP port (video 239.20/30/40 RED, BLUE = RED + 64) | hand |
| `types/<type>.csv` | **the DM subset one device type is provisioned with, in the dhs export format** — `path`, `kind`, `access`, `min`, `max`, `enum_items`, … exactly as the device's own typed export carries them; `value` is the setting | hand, from a typed export |
| `mcast-plan.csv` | every sender leg with its group and port | `plan` |
| `values/<device>.csv` | the type's rows made concrete for one device from **its** export — ready for `dhs import` | `values` |

The type template is a dhs export with three conventions in `path` and
`value`, nothing else:

- `*` — every member of a collection: `io.ip.senders.audio.*.primaryLeg.ip`
- `[field=value]` — the member whose leaf `field` reads `value`:
  `network.macs.[name=Control Port Mac 1].fec`, `sdi_output.[label=SDI {ch}].color_bar`
- `{key}` — a value from `site.csv` or the device's `devices.csv` row
  (`{ptp_domain}`, `{red_ip}`, `{fec_blue}`, `{nmos_label}`, `{hostname}`;
  derived: `{dhcp_bool}`, `{red_cidr}`, `{blue_cidr}`); `{ch}` repeats the
  row per channel in `devices.csv`; `{mcast.ip}` / `{mcast.port}` take the
  sender leg's row of `mcast-plan.csv`

An empty `value` keeps the device's. A `plane` column (`RED`, `BLUE`,
empty) keeps a row off a device that does not have that plane. A value
outside the export's enum / min / max is refused by `values`, before any
file is written.

## Run

```
cd ansible
ansible-playbook -i inventory/hosts.ini playbooks/provision-plan.yml           # export → plan → values (commit the outputs)
ansible-playbook -i inventory/hosts.ini playbooks/provision-apply.yml --check  # what would change, nothing sent
ansible-playbook -i inventory/hosts.ini playbooks/provision-apply.yml          # import; verified by a second --check
ansible-playbook -i inventory/hosts.ini playbooks/provision-apply.yml          # → changed=0
```

`apply` is three dhs calls per device: `import --check` (what would
change), `import` (only the rows the device does not hold — dhs reads
before it writes), `import --check` again, which must answer
`would apply 0`. That second check is the verification; the connector
does not read back after a write.

## Devices today

| device | type | protocol | planes | what is provisioned |
|---|---|---|---|---|
| lab-convert-01 (.102) | CONVERT Hybrid 7.0.3 | ccm (`/api/v1`, PUT) | RED+BLUE | PTP, NTP, NMOS → dhs registry, both control ports (static kept while DHCP, FEC RS), every sender leg's group/port |
| lab-shuffle-01 (.103) | SHUFFLE 6.0.0 | ccm (`/api`, PATCH) | RED | same; 1544 audio senders on 239.30.x.x:30000 |
| lab-view-01 (.104) | NeuronView 1.13.2 | — | RED | no CCM on this firmware: in `devices.csv` for the plan, provisioned by hand |
| lab-fusion-01 (10.6.40.54) | FusioN6 | mnset | RED+BLUE | NMOS → dhs registry (both slots; one registry for every device), PTP domain on both clocks, syslog, interfaces, FEC none, hostname, SDI outputs CH2/4/6/8 only (our hardware) |

Multicast: one block of `/24`s per device and essence, given once by
`plan` and written back into `devices.csv`, so a device keeps its
addresses across runs; senders are numbered in the order of their names
(`Output Audio Stream 12` → the 12th address).
