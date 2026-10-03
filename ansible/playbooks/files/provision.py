#!/usr/bin/env python3
"""Provisioning values for EVS Neuron and Riedel FusioN — stdlib only.

Driven by Ansible (provision-plan.yml, provision-apply.yml); runnable by
hand. Every byte that reaches a device goes through `dhs`: this script
reads dhs exports and writes dhs import files. It never opens a socket.

One site directory (ansible/sites/<site>/provisioning):

  site.csv            plant-wide values: NTP, PTP, registry, syslog
  devices.csv         one row per device: type, mgmt IP, planes, channels,
                      label, static addressing, FEC per plane, mcast blocks
  mcast-ranges.csv    essence x plane -> /16 prefix + UDP port
  types/<type>.csv    the rows to manage for one device type, IN THE DHS
                      EXPORT FORMAT (path, kind, access, min, max,
                      enum_items ... as the device's own typed export
                      carries them). value holds the setting: a literal,
                      {key} from site.csv / devices.csv, or {mcast.ip} /
                      {mcast.port} for a sender leg. In path, `*` is every
                      member of a collection and [field=value] the member
                      whose leaf `field` reads `value`; {ch} repeats the
                      row per channel of devices.csv.
  mcast-plan.csv      written by `plan`: every sender leg with its group
  values/<device>.csv written by `values`: the type's rows made concrete
                      for one device from ITS export — ready for
                      `dhs consumer <proto> import --file`

  plan    read each device's export, give every sender leg a group in its
          essence/plane range (one block of /24s per device, stable
          across runs), write mcast-plan.csv and the blocks back
  values  per device: type template x site x device x plan x export ->
          values/<device>.csv. A value outside the export's enum/min/max
          is refused here, before any file is written.

The last line on stdout is a JSON summary for Ansible's changed_when.
"""
import argparse
import csv
import ipaddress
import json
import os
import re
import sys

PER_BLOCK = 254  # .1 .. .254 per /24: no .0 / .255 in a group address
ESSENCES = ("video", "audio", "anc")
UUID = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}")

# Which dhs protocol speaks to each device type, and where its sender
# legs live in the export (path regex with named groups).
TYPES = {
    "neuron-convert-hybrid": {
        "proto": "ccm",
        "legs": r"^io\.ip\.senders\.(?P<essence>video|audio|data)\.(?P<uuid>[0-9a-f-]{36})\.legs\.(?P<leg>\d+)\.ip$",
    },
    "neuron-bridge": {
        "proto": "ccm",
        "legs": r"^io\.ip\.senders\.(?P<essence>video|audio|data)\.(?P<uuid>[0-9a-f-]{36})\.legs\.(?P<leg>\d+)\.ip$",
    },
    "neuron-shuffle": {
        "proto": "ccm",
        "legs": r"^io\.ip\.senders\.(?P<essence>audio)\.(?P<uuid>[0-9a-f-]{36})\.(?P<leg>primaryLeg|secondaryLeg)\.ip$",
    },
    "neuron-view": {"proto": "", "legs": None},  # no CCM on this firmware: nothing to write through dhs
    "fusion6": {"proto": "mnset", "legs": None},  # receive only on our hardware
}
LEG_PLANE = {"0": "RED", "1": "BLUE", "primaryLeg": "RED", "secondaryLeg": "BLUE"}
ESSENCE_OF = {"video": "video", "audio": "audio", "data": "anc"}


# ----------------------------------------------------------------- csv ----

def read_csv(path):
    with open(path, newline="", encoding="utf-8") as f:
        return list(csv.DictReader(f))


def write_csv(path, rows, cols):
    os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
    tmp = path + ".tmp"
    with open(tmp, "w", newline="", encoding="utf-8") as f:
        w = csv.DictWriter(f, cols, extrasaction="ignore")
        w.writeheader()
        for r in rows:
            w.writerow(r)
    os.replace(tmp, path)


def site_values(site_dir):
    return {r["key"]: r["value"] for r in read_csv(os.path.join(site_dir, "site.csv"))}


def devices(site_dir):
    return read_csv(os.path.join(site_dir, "devices.csv"))


def export_rows(export_dir, name):
    path = os.path.join(export_dir, name + ".csv")
    if not os.path.exists(path):
        return None
    return read_csv(path)


# ---------------------------------------------------------------- plan ----

def ranges(site_dir):
    out = {}
    for r in read_csv(os.path.join(site_dir, "mcast-ranges.csv")):
        out[(r["essence"], r["plane"])] = (ipaddress.ip_network(r["prefix"]), int(r["port"]))
    return out


def group(prefix, block, index):
    """The group address for sender `index` of a device holding /24 block
    `block` (and the following ones) inside `prefix`."""
    base = int(prefix.network_address) + (block + index // PER_BLOCK) * 256
    return str(ipaddress.ip_address(base + 1 + index % PER_BLOCK))


def plan(site_dir, export_dir):
    rng = ranges(site_dir)
    devs = devices(site_dir)
    plan_path = os.path.join(site_dir, "mcast-plan.csv")
    old = {(r["device"], r["sender"], r["leg"]): r for r in read_csv(plan_path)} if os.path.exists(plan_path) else {}
    rows, next_block, changed = [], {e: 0 for e in ESSENCES}, 0
    # Blocks already given stay given: a device keeps its addresses.
    for d in devs:
        for e in ESSENCES:
            b = d.get("block_" + e, "")
            if b != "":
                next_block[e] = max(next_block[e], int(b) + 1)
    for d in devs:
        if d.get("mcast_apply", "") != "yes" or not TYPES[d["type"]]["legs"]:
            continue
        exp = export_rows(export_dir, d["name"])
        if exp is None:
            print(f"{d['name']}: no export at {export_dir}/{d['name']}.csv — run the export first", file=sys.stderr)
            continue
        legs_re = re.compile(TYPES[d["type"]]["legs"])
        planes = [p.strip() for p in d["planes"].split("+")]
        senders = {}  # essence -> sender uuids, in the order of their names
        names = {r["path"]: r["value"] for r in exp if r["path"].endswith(".name")}
        legs = []
        for r in exp:
            m = legs_re.match(r["path"])
            if not m:
                continue
            e = ESSENCE_OF[m.group("essence")]
            senders.setdefault(e, [])
            if m.group("uuid") not in senders[e]:
                senders[e].append(m.group("uuid"))
            legs.append((e, m.group("uuid"), m.group("leg"), r["path"]))
        # "Output Audio Stream 12" gets the 12th address: the order an
        # operator reads on the device, not the order of uuids.
        for e, uuids in senders.items():
            uuids.sort(key=lambda u: natural(sender_name(names, u)))
        for e, uuids in senders.items():
            if d.get("block_" + e, "") == "":
                d["block_" + e] = str(next_block[e])
                next_block[e] += (len(uuids) + PER_BLOCK - 1) // PER_BLOCK
                changed += 1
        for e, uuid, leg, path in legs:
            plane = LEG_PLANE[leg]
            if plane not in planes:
                continue
            prefix, port = rng[(e, plane)]
            index = senders[e].index(uuid)
            row = {
                "device": d["name"], "sender": uuid, "essence": e, "plane": plane, "leg": leg,
                "ip": group(prefix, int(d["block_" + e]), index), "port": str(port),
                "path": path[: -len(".ip")],
            }
            prev = old.get((d["name"], uuid, leg))
            if prev is None or prev["ip"] != row["ip"] or prev["port"] != row["port"]:
                changed += 1
            rows.append(row)
    rows.sort(key=lambda r: (r["device"], r["essence"], r["plane"], r["path"]))
    write_csv(plan_path, rows, ["device", "sender", "essence", "plane", "leg", "ip", "port", "path"])
    write_csv(os.path.join(site_dir, "devices.csv"), devs, list(devs[0].keys()))
    return {"plan": len(rows), "changed": changed}


# -------------------------------------------------------------- values ----

SELECT = re.compile(r"\[([^=\]]+)=([^\]]*)\]")


def expand_path(tpl_path, exp_by_path, exp_index, channels):
    """Every concrete export path a template path names, as (path, row).
    `*` is every member below the prefix; [field=value] the member whose
    leaf reads value; {ch} each channel the device row lists."""
    chs = channels if "{ch}" in tpl_path else [None]
    out = []
    for ch in chs:
        p = tpl_path.replace("{ch}", ch) if ch is not None else tpl_path
        for concrete in _expand(p, exp_index):
            if concrete in exp_by_path:
                out.append((concrete, exp_by_path[concrete]))
    return out


def _expand(p, exp_index):
    segs = p.split(".")
    for i, seg in enumerate(segs):
        if seg == "*" or SELECT.fullmatch(seg):
            prefix = ".".join(segs[:i])
            members = exp_index.get(prefix, [])
            if seg != "*":
                field, value = SELECT.fullmatch(seg).groups()
                members = [m for m in members if exp_index["leaf"].get(f"{prefix}.{m}.{field}") == value]
            rest = ".".join(segs[i + 1:])
            found = []
            for m in members:
                found.extend(_expand(f"{prefix}.{m}" + ("." + rest if rest else ""), exp_index))
            return found
    return [p]


def index_export(rows):
    """prefix -> member ids (uuids, or indexes, directly below it), plus
    every leaf's value."""
    members, leaf = {}, {}
    for r in rows:
        leaf[r["path"]] = r["value"]
        segs = r["path"].split(".")
        for i, seg in enumerate(segs):
            if UUID.fullmatch(seg) or seg.isdigit():
                prefix = ".".join(segs[:i])
                lst = members.setdefault(prefix, [])
                if seg not in lst:
                    lst.append(seg)
    members["leaf"] = leaf
    return members


def check(row, value):
    """A value outside what the device's own export declares is refused
    here, not by the device later."""
    if row.get("enum_items"):
        # An item is "value" or "value=Label" (MN SET exports carry both).
        items = [i.split("=", 1)[0] for i in row["enum_items"].split("|")]
        if value not in items:
            return f"{value!r} is not one of {row['enum_items']}"
    if row.get("kind") in ("int", "float") and value != "":
        try:
            v = float(value)
        except ValueError:
            return f"{value!r} is not a number"
        if row.get("min") not in ("", None) and v < float(row["min"]):
            return f"{value} < min {row['min']}"
        if row.get("max") not in ("", None) and v > float(row["max"]):
            return f"{value} > max {row['max']}"
    if row.get("kind") == "bool" and value not in ("true", "false"):
        return f"{value!r} is not true/false"
    return None


def values(site_dir, export_dir, out_dir):
    site = site_values(site_dir)
    plan_path = os.path.join(site_dir, "mcast-plan.csv")
    plan_rows = read_csv(plan_path) if os.path.exists(plan_path) else []
    mcast = {(r["device"], r["path"]): r for r in plan_rows}
    summary = {"devices": {}, "refused": 0}
    for d in devices(site_dir):
        t = TYPES[d["type"]]
        if not t["proto"]:
            summary["devices"][d["name"]] = {"skipped": "no writable protocol on this firmware"}
            continue
        tpl_path = os.path.join(site_dir, "types", d["type"] + ".csv")
        exp = export_rows(export_dir, d["name"])
        if exp is None or not os.path.exists(tpl_path):
            summary["devices"][d["name"]] = {"skipped": "no export or no type template"}
            continue
        exp_by_path = {r["path"]: r for r in exp}
        exp_index = index_export(exp)
        channels = [c.strip() for c in d.get("channels", "").split(";") if c.strip()]
        keys = dict(site)
        keys.update({k: v for k, v in d.items() if v != ""})
        keys.setdefault("dhcp_bool", "true" if d.get("ip_mode", "DHCP").upper() == "DHCP" else "false")
        for plane, key in (("red", "red_ip"), ("blue", "blue_ip")):
            if d.get(key) and d.get(f"{plane}_netmask"):
                bits = ipaddress.ip_network(f"0.0.0.0/{d[f'{plane}_netmask']}").prefixlen
                keys[f"{plane}_cidr"] = f"{d[key]}/{bits}"
        rows, refused, missing = [], [], []
        cols = list(exp[0].keys())
        for trow in read_csv(tpl_path):
            if trow["value"] == "":
                continue  # empty = keep the device's value
            planes = [p.strip() for p in d["planes"].split("+")]
            if trow.get("plane") and trow["plane"] not in planes:
                continue
            targets = expand_path(trow["path"], exp_by_path, exp_index, channels)
            if not targets:
                missing.append(trow["path"])
                continue
            for path, erow in targets:
                v = trow["value"]
                if "{mcast." in v:
                    leg = mcast.get((d["name"], path[: path.rfind(".")]))
                    if leg is None:
                        missing.append(path)
                        continue
                    v = v.replace("{mcast.ip}", leg["ip"]).replace("{mcast.port}", leg["port"])
                try:
                    v = v.format(**keys)
                except KeyError as e:
                    refused.append(f"{path}: no value for {e}")
                    continue
                if v == "":
                    continue
                if "W" not in erow.get("access", ""):
                    refused.append(f"{path}: the device's export says read-only")
                    continue
                why = check(erow, v)
                if why:
                    refused.append(f"{path}: {why}")
                    continue
                rows.append(dict(erow, value=v))
        if refused:
            for r in refused:
                print(f"{d['name']}: REFUSED {r}", file=sys.stderr)
            summary["refused"] += len(refused)
        if missing:
            print(f"{d['name']}: not in the export, skipped: {', '.join(sorted(set(missing))[:8])}"
                  f"{' …' if len(set(missing)) > 8 else ''}", file=sys.stderr)
        out = os.path.join(out_dir, d["name"] + ".csv")
        before = read_csv(out) if os.path.exists(out) else None
        write_csv(out, rows, cols)
        summary["devices"][d["name"]] = {
            "proto": t["proto"], "rows": len(rows), "missing": len(set(missing)), "refused": len(refused),
            "changed": before is None or [(r["path"], r["value"]) for r in before] != [(r["path"], r["value"]) for r in rows],
        }
    return summary


def sender_name(names, uuid):
    for path, name in names.items():
        if f".{uuid}.name" in path and path.startswith("io.ip.senders."):
            return name
    return uuid


def natural(s):
    return [int(t) if t.isdigit() else t.lower() for t in re.split(r"(\d+)", s)]


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("verb", choices=["plan", "values"])
    ap.add_argument("--site", required=True, help="ansible/sites/<site>/provisioning")
    ap.add_argument("--exports", required=True, help="directory of dhs exports, <device>.csv")
    ap.add_argument("--out", help="where values/<device>.csv go (default: <site>/values)")
    a = ap.parse_args()
    if a.verb == "plan":
        s = plan(a.site, a.exports)
    else:
        s = values(a.site, a.exports, a.out or os.path.join(a.site, "values"))
    print(json.dumps(s))
    return 2 if s.get("refused") else 0


if __name__ == "__main__":
    sys.exit(main())