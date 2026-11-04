# Runbook — export a device to CSV, edit, import it back

Two sample plays do it for any connector, from the control node
(dhs-debian, repo at `/root/acp-plant`):

| Play | What it does |
|---|---|
| `ansible/playbooks/csv-export.yml` | reads the device, writes the CSV where dhs keeps it, copies it into the site |
| `ansible/playbooks/csv-import.yml` | dry run → apply what differs → dry run again (must be 0) |

## 1. Where the files are

| Place | Path |
|---|---|
| dhs host | `<root>/snapshots/<proto>/<ip>/` (ADR-0028) |
| repo (what you edit and commit) | `ansible/sites/<site>/snapshots/<proto>/<ip>/` |
| device models + manifests | `<root>/.cache/dm/<proto>/<Model@SwRev>.json`, `<root>/.cache/manifest/<proto>/<ip>.json` |

`<root>` is the install dir: `/usr/local/` for `/usr/local/bin/dhs`.
For a dedicated folder add `-e dhs_data_dir=/srv/dhs` (the play passes
`--data-dir`; by hand: `dhs --data-dir /srv/dhs …` or `DHS_DATA_DIR`).

The folder is always the device's **IP**: a target given by hostname is
resolved, and the name is kept in the manifest as `fqdn`. A port is never
part of it. Probel SW-P-08 matrix N > 0 sits in `<ip>/matrix-N/`.

### A device with several controllers

One device, one folder, whatever IP you address it by. The manifest
lists every controller IP; the first one walked is the key.

| How a controller IP joins its device | When |
|---|---|
| on its own | the connector reads a serial number that a device on file has |
| `dhs --primary 10.6.255.102 consumer acp2 walk 10.6.255.122 --all` | no serial (or to be explicit) |

After that, `export` / `import` / logs / captures through any controller
IP use the device's folder (`acp2: 10.6.255.122 is a controller of
10.6.255.102`). UDP devices (acp1, snmp) keep one controller each.

## 2. Export

```bash
cd /root/acp-plant/ansible
ansible-playbook -i inventory/hosts.ini playbooks/csv-export.yml -e proto=acp2 -e target=10.6.255.102
```

| Connector | Files | Extra |
|---|---|---|
| acp1, acp2, emberplus, snmp, mnset, rollcall, … | `params.csv` | — |
| probel-sw08p | `xpoint.csv src.csv dst.csv protect.csv matrix.csv` | `-e matrix=N` |
| cerebrum-nb | `xpoint.csv src.csv dst.csv level.csv lock.csv cat-*.csv` | `-e router=0.0.0.0` (RouteMaster, default), `-e '{"extra":["--port","40009"]}'`; `DHS_CEREBRUM_USER` / `DHS_CEREBRUM_PASS` from Vault in the environment |

Export only reads. The play reports `changed` when a file in the site
differs from the previous export.

## 3. Edit

- Keep only the rows you want to converge: a row absent from the file is
  never touched; a facet file absent from the folder is out of scope.
- `params.csv`: change the `value` column. `min` / `max` are the device's
  range as exported — leave them as they are.
- Commit the edited files; the site is the record of what was asked.

### What a value cell must be

A row that does not match its definition is **not sent**, named in the
report (`invalid …`), and the import ends with exit 2; the rows that are
right still go out. Nothing is guessed.

| Kind | Accepted | Refused (examples) |
|---|---|---|
| bool | `true` `false` `1` `0` (any case) | `2`, `ture`, `t`, empty |
| int / uint / float | a number; uint never negative | `4.5` in an int, `1,5`, empty (never written as 0) |
| enum | `value` = an item's value, `value_name` = that item; or `value_name` alone, spelled exactly | a value that is no item, a name that disagrees with the value, `on` for `On` (the report names the item meant) |
| ip | dotted IPv4 (`0.0.0.0` included); the report gives its class: private / public unicast, multicast (SSM for 232/8), broadcast, link-local, loopback, unspecified | `10.6.250.300`, `010.6.250.1`, an IPv6 address in an IPv4 field |
| string | up to `max_len` characters | longer than `max_len` |

`enum_items` carries the device's own value of each item when it numbers
them itself (ACP2 options, an Ember+ enumMap): `710=Off|711=1 s|715=60 s`,
and `value` is that number (`715`), never a position. A device whose
items are positions keeps the plain list `Off|On`.

A value the device already holds is left alone, whatever it is.

## 4. Import

```bash
ansible-playbook -i inventory/hosts.ini playbooks/csv-import.yml -e proto=acp2 -e target=10.6.255.102 --check   # dry run only
ansible-playbook -i inventory/hosts.ini playbooks/csv-import.yml -e proto=acp2 -e target=10.6.255.102           # apply
ansible-playbook -i inventory/hosts.ini playbooks/csv-import.yml -e proto=acp2 -e target=10.6.255.102           # → changed=0
```

| Step | Result |
|---|---|
| 1. dry run | prints `would apply N, unchanged N, skipped N, failed N` (matrix connectors: `would_change`) |
| 2. apply | only when N > 0; the target is named twice (`--write-to`, ADR-0037 P5) |
| 3. dry run again | must say 0, otherwise the play fails: the device did not take what it acknowledged |

Options: `-e from=10.6.250.40/matrix-1` (another folder of the site),
`-e select=--src` (probel-sw08p facet: `--xpoints` default, `--src`,
`--dst`, `--protect`), `-e site=<site>`, `-e dhs_bin=/root/dhs-branch/dhs`
(pre-PR test binary).

## 5. When it stops

| Message | Meaning | Do |
|---|---|---|
| `clamped … 19000 above MAX 18000, 18000 used instead` + exit 2 | a value outside the range in the file | fix the value; nothing was sent in the dry run |
| `invalid … ` + exit 2 | the row does not match its definition (table above) | fix the cell; that row was not sent |
| `failed N` in the dry run | the device refuses N rows | read the listed rows; nothing was sent |
| step 3 fails | written, acknowledged, not applied | a device fault: report it with the play output |
| `the site limits refuse N rows` | the plant's own limits (ADR-0036) | change the file or the limits |

Measured 2026-11-04 on dhs-debian (acp2 emulator, branch binary): export
43 300 rows, a one-row file applied, second run `changed=0`; a value of
19000 against MAX 18000 stopped at the dry run, nothing sent.
