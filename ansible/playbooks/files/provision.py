#!/usr/bin/env python3
"""Plant provisioning for EVS Neuron and Riedel FusioN — stdlib only.

Driven by Ansible (provision-plan.yml, provision-apply.yml); runnable by hand.
All data lives in one site directory (ansible/sites/<site>/provisioning):

  site.csv           plant-wide values: NTP main/backup, PTP, registry, syslog
  devices.csv        one row per device: type, mgmt IP, planes (RED or
                     RED+BLUE), channels, label, static addressing, FEC per
                     plane, multicast blocks (filled by `plan`), mcast_apply
  mcast-ranges.csv   essence x plane -> /16 prefix + UDP port
  base-<type>.csv    the base setup of a device type, as a DM: resource,
                     field, value, type, format, enum, min, max, source.
                     {key} takes site.csv / devices.csv; {ch} repeats a row
                     per channel; a value outside enum/min/max is refused
                     before anything is sent
  mcast-plan.csv     written by `plan`: every sender with its RED/BLUE group

  plan   discover every sender on every device, give each a group in its
         essence range (one block of /24s per device, kept stable across
         runs), write mcast-plan.csv and the blocks back into devices.csv
  apply  per device: base-<type>.csv, then its mcast-plan.csv rows.
         Read -> compare -> write only what differs -> read back.
         --check reports would_change and sends nothing.

The last line on stdout is a JSON summary for Ansible's changed_when.
"""
import argparse
import concurrent.futures
import copy
import csv
import ipaddress
import json
import os
import re
import ssl
import sys
import urllib.error
import urllib.request

CTX = ssl._create_unverified_context()  # lab devices ship self-signed certs
TIMEOUT = 20
PER_BLOCK = 254  # .1 .. .254 per /24: no .0 / .255 in a group address
ESSENCES = ("video", "audio", "anc")

TYPES = {
    "neuron-convert-hybrid": {"base": "https://{ip}/api/v1", "senders": "neuron_legs"},
    "neuron-bridge": {"base": "https://{ip}/api/v1", "senders": "neuron_legs"},
    "neuron-shuffle": {"base": "https://{ip}/api", "senders": "neuron_shuffle"},
    "neuron-view": {"base": "https://{ip}/api/v1", "senders": "is05"},
    "fusion6": {"base": "http://{ip}/emsfp/node/v1", "senders": "fusion"},
}


# ---------------------------------------------------------------- HTTP ----

def http(method, url, body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data, {"Content-Type": "application/json"}, method=method)
    try:
        with urllib.request.urlopen(req, context=CTX, timeout=TIMEOUT) as r:
            raw = r.read()
            return r.status, (json.loads(raw) if raw.strip() else None)
    except urllib.error.HTTPError as e:
        raw = e.read().decode(errors="replace")
        try:
            return e.code, json.loads(raw)
        except ValueError:
            return e.code, raw[:300]


def get(url):
    st, body = http("GET", url)
    if st != 200:
        raise RuntimeError(f"GET {url} -> {st} {body}")
    return body


# ------------------------------------------------------------- helpers ----

def read_csv(path):
    with open(path, newline="", encoding="utf-8") as f:
        return list(csv.DictReader(f))


def write_csv(path, rows, cols):
    tmp = path + ".tmp"
    with open(tmp, "w", newline="", encoding="utf-8") as f:
        w = csv.DictWriter(f, cols, extrasaction="ignore")
        w.writeheader()
        w.writerows(rows)
    os.replace(tmp, path)


def natural(s):
    return [int(t) if t.isdigit() else t.lower() for t in re.split(r"(\d+)", s or "")]


def get_in(doc, path):
    for k in path.split("."):
        doc = doc[int(k)] if isinstance(doc, list) else doc[k]
    return doc


def set_in(doc, path, value):
    keys = path.split(".")
    parent = get_in(doc, ".".join(keys[:-1])) if len(keys) > 1 else doc
    if isinstance(parent, list):
        parent[int(keys[-1])] = value
    else:
        parent[keys[-1]] = value


def coerce(current, text):
    """Write the value in the device's own JSON type (bool / int / float / str)."""
    t = str(text).strip()
    if isinstance(current, bool):
        if t.lower() in ("true", "1", "yes", "on"):
            return True
        if t.lower() in ("false", "0", "no", "off"):
            return False
        raise ValueError(f"not a boolean: {text!r}")
    if isinstance(current, int):
        return int(t)
    if isinstance(current, float):
        return float(t)
    return t


def mcast_mac(group):
    """RFC 1112: 01:00:5e + the low 23 bits of the group address."""
    b = ipaddress.IPv4Address(group).packed
    return "01:00:5e:%02x:%02x:%02x" % (b[1] & 0x7F, b[2], b[3])


def api_base(dev):
    return TYPES[dev["type"]]["base"].format(ip=dev["mgmt_ip"])


def planes(dev):
    return set((dev.get("planes") or "RED+BLUE").upper().split("+"))


def channels(dev):
    return [c for c in (dev.get("channels") or "").replace(",", ";").split(";") if c.strip()]


def device_vars(site, dev):
    """site.csv values, overridden by the device row, plus derived values."""
    v = dict(site)
    v.update({k: x for k, x in dev.items() if x is not None})
    for p in ("red", "blue"):
        ip, mask = v.get(f"{p}_ip"), v.get(f"{p}_netmask")
        if ip and mask:
            v[f"{p}_cidr"] = f"{ip}/{ipaddress.IPv4Network('0.0.0.0/' + mask).prefixlen}"
    v["dhcp_bool"] = "true" if (v.get("ip_mode") or "").upper() == "DHCP" else "false"
    return v


def subst(value, var):
    """{key} -> value; empty when any key is unset, so the row is skipped."""
    missing = [k for k in re.findall(r"\{(\w+)\}", value) if not var.get(k)]
    if missing:
        return ""
    return re.sub(r"\{(\w+)\}", lambda m: var[m.group(1)], value)


def validate(row, value):
    """Check a value against the DM columns of its base row (enum, min, max, format)."""
    enum = row.get("enum") or ""
    if enum and "(as read" not in enum and value not in enum.split("|"):
        return f"{value!r} not in {enum}"
    for bound, op in (("min", lambda x, b: x < b), ("max", lambda x, b: x > b)):
        if row.get(bound):
            try:
                if op(float(value), float(row[bound])):
                    return f"{value} outside {row.get('min')}..{row.get('max')}"
            except ValueError:
                return f"{value!r} is not a number"
    fmt = row.get("format") or ""
    try:
        if fmt in ("ipv4", "ipv4 netmask"):
            ipaddress.IPv4Address(value)
        elif fmt == "cidr":
            ipaddress.IPv4Interface(value)
        elif fmt == "hostport":
            host, _, port = value.rpartition(":")
            ipaddress.IPv4Address(host)
            if not 0 <= int(port) <= 65535:
                raise ValueError(port)
    except ValueError:
        return f"{value!r} is not a valid {fmt}"
    if fmt.startswith("maxLength") and len(value) > int(fmt.split()[1]):
        return f"{value!r} longer than {fmt.split()[1]}"
    return None


# ------------------------------------------------------ sender discovery ----
# Each discoverer returns rows: essence, sender_id, sender_name, api (where the
# addresses live, for the reader of mcast-plan.csv).

def disc_neuron_legs(dev):
    base, out = api_base(dev), []
    for path, ess in (("video", "video"), ("audio", "audio"), ("data", "anc")):
        for s in get(f"{base}/io/ip/senders/{path}"):
            out.append({"essence": ess, "sender_id": s["uuid"], "sender_name": s["name"],
                        "api": f"io/ip/senders/{path}/{s['uuid']} legs.0 / legs.1"})
    return out


def disc_neuron_shuffle(dev):
    base = api_base(dev)
    ids = get(f"{base}/io/ip/senders/audio")
    with concurrent.futures.ThreadPoolExecutor(16) as ex:
        docs = list(ex.map(lambda u: (u, get(f"{base}/io/ip/senders/audio/{u}")), ids))
    return [{"essence": "audio", "sender_id": u, "sender_name": d["name"],
             "api": f"io/ip/senders/audio/{u} primaryLeg / secondaryLeg"} for u, d in docs]


def is05_root(dev):
    host = f"http://{dev['mgmt_ip']}:3000"
    node = sorted(get(f"{host}/x-nmos/node/"))[-1].strip("/")
    conn = sorted(get(f"{host}/x-nmos/connection/"))[-1].strip("/")
    return host, node, conn


def disc_is05(dev):
    host, node, conn = is05_root(dev)
    flows = {f["id"]: f for f in get(f"{host}/x-nmos/node/{node}/flows")}
    out = []
    for s in get(f"{host}/x-nmos/node/{node}/senders"):
        fmt = flows.get(s.get("flow_id"), {}).get("format", "")
        ess = {"video": "video", "audio": "audio", "data": "anc"}.get(fmt.rsplit(":", 1)[-1], "video")
        out.append({"essence": ess, "sender_id": s["id"], "sender_name": s["label"],
                    "api": f"x-nmos/connection/{conn}/single/senders/{s['id']}/staged"})
    return out


def disc_fusion(dev):
    base = api_base(dev)
    labels = {d["id"]: (d.get("label") or "").strip() for d in get(f"{base}/devices")}
    out, count = [], {}
    for s in get(f"{base}/senders"):
        ess = {"video": "video", "audio": "audio", "ancillary": "anc"}.get(s["format"], s["format"])
        chan = labels.get(s["device_id"], s["device_id"][:8]).replace("Device ", "")
        if channels(dev) and chan.replace("CH", "") not in channels(dev):
            continue  # a channel our hardware does not use
        count[(chan, ess)] = count.get((chan, ess), 0) + 1
        name = f"{chan} {ess} {count[(chan, ess)]}"
        out.append({"essence": ess, "sender_id": s["id"], "sender_name": name,
                    "api": f"flows/{s['flow_id'][0]} (RED) + flows/{s['flow_id'][1]} (BLUE)"})
    return out


DISCOVER = {"neuron_legs": disc_neuron_legs, "neuron_shuffle": disc_neuron_shuffle,
            "is05": disc_is05, "fusion": disc_fusion}

PLAN_COLS = ["device", "type", "essence", "index", "sender_name", "sender_id",
             "red_group", "red_port", "blue_group", "blue_port", "api"]


def cmd_plan(a):
    site = a.site_dir
    devices = read_csv(os.path.join(site, "devices.csv"))
    dev_cols = list(devices[0].keys())
    ranges = {(r["essence"], r["plane"]): r for r in read_csv(os.path.join(site, "mcast-ranges.csv"))}
    plan_path = os.path.join(site, "mcast-plan.csv")
    old = {(r["device"], r["sender_id"]): r for r in read_csv(plan_path)} if os.path.exists(plan_path) else {}

    d_ess = {}
    for d in devices:
        if d.get("type") not in TYPES:
            print(f"skip {d['name']}: unknown type {d.get('type')}", file=sys.stderr)
            continue
        try:
            d_ess[d["name"]] = DISCOVER[TYPES[d["type"]]["senders"]](d)
        except Exception as e:  # unreachable device: keep its old rows
            print(f"WARN {d['name']}: discovery failed: {e}", file=sys.stderr)
            d_ess[d["name"]] = None

    rows, changed = [], False
    for ess in ESSENCES:
        # blocks already owned, so a new device never lands on an old one
        used_end = 0
        for d in devices:
            if d.get(f"block_{ess}"):
                n_old = sum(1 for r in old.values() if r["device"] == d["name"] and r["essence"] == ess)
                used_end = max(used_end, int(d[f"block_{ess}"]) + max(1, -(-n_old // PER_BLOCK)))
        for d in devices:
            if d.get("type") not in TYPES:
                continue
            found = d_ess.get(d["name"])
            if found is None:
                rows += [r for r in old.values() if r["device"] == d["name"] and r["essence"] == ess]
                continue
            senders = sorted((s for s in found if s["essence"] == ess),
                             key=lambda s: (natural(s["sender_name"]), s["sender_id"]))
            if not senders:
                if d.get(f"block_{ess}"):  # no sender any more: give the block back
                    d[f"block_{ess}"] = ""
                    changed = True
                continue
            need = -(-len(senders) // PER_BLOCK)
            if not d.get(f"block_{ess}"):
                d[f"block_{ess}"] = str(used_end)
                used_end += need
                changed = True
            block = int(d[f"block_{ess}"])
            keep = {s["sender_id"]: int(old[(d["name"], s["sender_id"])]["index"])
                    for s in senders if (d["name"], s["sender_id"]) in old}
            taken, nxt = set(keep.values()), 0
            for s in senders:
                if s["sender_id"] in keep:
                    idx = keep[s["sender_id"]]
                else:
                    while nxt in taken:
                        nxt += 1
                    idx, nxt = nxt, nxt + 1
                    taken.add(idx)
                    changed = True
                third, fourth = block + idx // PER_BLOCK, idx % PER_BLOCK + 1
                if third > 255:
                    raise SystemExit(f"{d['name']} {ess}: range exhausted (block {block}, index {idx})")
                row = {"device": d["name"], "type": d["type"], "essence": ess, "index": idx,
                       "sender_name": s["sender_name"], "sender_id": s["sender_id"], "api": s["api"]}
                for plane in ("RED", "BLUE"):
                    if plane in planes(d):
                        net = ipaddress.IPv4Network(ranges[(ess, plane)]["prefix"]).network_address.packed
                        row[f"{plane.lower()}_group"] = f"{net[0]}.{net[1]}.{third}.{fourth}"
                        row[f"{plane.lower()}_port"] = ranges[(ess, plane)]["port"]
                rows.append(row)
    order = {d["name"]: i for i, d in enumerate(devices)}
    rows.sort(key=lambda r: (order[r["device"]], ESSENCES.index(r["essence"]), int(r["index"])))
    before = open(plan_path, encoding="utf-8").read() if os.path.exists(plan_path) else ""
    write_csv(plan_path, rows, PLAN_COLS)
    changed = changed or open(plan_path, encoding="utf-8").read() != before
    write_csv(os.path.join(site, "devices.csv"), devices, dev_cols)
    counts = {}
    for r in rows:
        counts.setdefault(r["device"], {}).setdefault(r["essence"], 0)
        counts[r["device"]][r["essence"]] += 1
    for name, c in counts.items():
        print(f"{name}: " + ", ".join(f"{k} {v}" for k, v in c.items()))
    print(json.dumps({"changed": changed, "senders": len(rows), "plan": plan_path}))


# ---------------------------------------------------------------- apply ----

class Result:
    def __init__(self, check):
        self.check, self.rows = check, []

    def add(self, dev, item, field, before, after, status):
        self.rows.append({"device": dev, "item": item, "field": field,
                          "before": before, "after": after, "status": status})
        if status != "ok":
            print(f"{status:12} {dev:16} {item[:60]:60} {field} {before} -> {after}", flush=True)

    def summary(self):
        s = {k: 0 for k in ("ok", "changed", "would_change", "failed")}
        for r in self.rows:
            s[r["status"]] = s.get(r["status"], 0) + 1
        return s


def write_doc(url, doc, check):
    """PUT the whole document (every API here accepts it); verify by reading back."""
    if check:
        return None
    st, body = http("PUT", url, doc)
    if st not in (200, 201, 202, 204):
        raise RuntimeError(f"PUT {url} -> {st} {body}")
    return get(url)


def apply_fields(res, dev, item, url, fields):
    """fields: [(path, desired_text)] -> one GET, one PUT if anything differs."""
    doc = get(url)
    new = copy.deepcopy(doc)
    diffs = []
    for path, text in fields:
        cur = get_in(doc, path)
        want = coerce(cur, text)
        set_in(new, path, want)
        if cur != want:
            diffs.append((path, cur, want))
    if not diffs:
        for path, text in fields:
            res.add(dev, item, path, get_in(doc, path), get_in(doc, path), "ok")
        return
    back = write_doc(url, new, res.check)
    for path, cur, want in diffs:
        if res.check:
            res.add(dev, item, path, cur, want, "would_change")
        else:
            got = get_in(back, path)
            res.add(dev, item, path, cur, got, "changed" if got == want else "failed")


def resolve(base, resource, select):
    """'name=Control Port Mac 1' on a collection -> that member's URL.
    'channel=2' (FusioN) -> the member whose device is labelled 'Device CH2'."""
    if not select:
        return f"{base}/{resource}"
    key, _, want = select.partition("=")
    devices = None
    if key == "channel":
        devices = {d["id"]: (d.get("label") or "").strip() for d in get(f"{base}/devices")}
    for m in get(f"{base}/{resource}"):
        mid = m.strip("/") if isinstance(m, str) else (m.get("uuid") or m.get("id"))
        body = get(f"{base}/{resource}/{mid}") if isinstance(m, str) else m
        if key == "channel":
            if devices.get(body.get("device_id")) == f"Device CH{want}":
                return f"{base}/{resource}/{mid}"
        elif str(body.get(key)) == want:
            return f"{base}/{resource}/{mid}"
    raise RuntimeError(f"{resource}: no member with {select}")


def base_rows(site, dev):
    """base-<type>.csv for one device: planes it has, {ch} expanded per channel."""
    rows = []
    for r in sorted(read_csv(os.path.join(site, f"base-{dev['type']}.csv")), key=lambda r: int(r["order"])):
        if r.get("plane") and r["plane"].upper() not in planes(dev):
            continue
        if "{ch}" in r["select"] + r["group"]:
            for ch in channels(dev):
                rows.append({k: v.replace("{ch}", ch) for k, v in r.items()})
        else:
            rows.append(r)
    return rows


def apply_base(res, site_dir, site, dev):
    var, base, groups = device_vars(site, dev), api_base(dev), {}
    for r in base_rows(site_dir, dev):
        value = subst(r["value"], var)
        if value == "":
            continue  # nothing set for this device: leave the device value
        item = f"{r['resource']}[{r['select']}]" if r["select"] else r["resource"]
        err = validate(r, value)
        if err:
            res.add(dev["name"], item, r["field"], "", f"invalid: {err}", "failed")
            continue
        groups.setdefault((r["resource"], r["select"]), []).append((r["field"], value))
    for (resource, select), fields in groups.items():
        item = f"{resource}[{select}]" if select else resource
        try:
            apply_fields(res, dev["name"], item, resolve(base, resource, select), fields)
        except Exception as e:
            res.add(dev["name"], item, ",".join(f for f, _ in fields), "", str(e)[:200], "failed")


def apply_mcast_one(res, dev, row):
    kind = TYPES[dev["type"]]["senders"]
    base, sid = api_base(dev), row["sender_id"]
    # leg 0 = RED, leg 1 = BLUE; a plane the device does not have stays untouched
    legs = [(i, row.get(f"{p}_group"), row.get(f"{p}_port"))
            for i, p in enumerate(("red", "blue")) if row.get(f"{p}_group")]
    item = f"{row['essence']} {row['sender_name']}"
    try:
        if kind == "neuron_legs":
            path = {"video": "video", "audio": "audio", "anc": "data"}[row["essence"]]
            apply_fields(res, dev["name"], item, f"{base}/io/ip/senders/{path}/{sid}",
                         [f for i, g, p in legs for f in ((f"legs.{i}.ip", g), (f"legs.{i}.port", p))])
        elif kind == "neuron_shuffle":
            name = {0: "primaryLeg", 1: "secondaryLeg"}
            apply_fields(res, dev["name"], item, f"{base}/io/ip/senders/audio/{sid}",
                         [f for i, g, p in legs for f in ((f"{name[i]}.ip", g), (f"{name[i]}.port", p))])
        elif kind == "fusion":
            s = get(f"{base}/senders/{sid}")
            for i, g, p in legs:
                apply_fields(res, dev["name"], f"{item} {('RED', 'BLUE')[i]}", f"{base}/flows/{s['flow_id'][i]}",
                             [("network.dst_ip_addr", g), ("network.dst_udp_port", p),
                              ("network.dst_mac", mcast_mac(g))])
        elif kind == "is05":
            host, _, conn = is05_root(dev)
            url = f"{host}/x-nmos/connection/{conn}/single/senders/{sid}/staged"
            tp = get(url)["transport_params"]
            diffs = [(i, (tp[i]["destination_ip"], tp[i]["destination_port"]), (g, int(p)))
                     for i, g, p in legs if i < len(tp)
                     and (tp[i]["destination_ip"], tp[i]["destination_port"]) != (g, int(p))]
            if not diffs:
                res.add(dev["name"], item, "transport_params", tp[0]["destination_ip"], tp[0]["destination_ip"], "ok")
                return
            if res.check:
                for i, cur, w in diffs:
                    res.add(dev["name"], item, f"transport_params.{i}", cur, w, "would_change")
                return
            # IS-05: an empty object leaves that leg as it is
            params = [{} for _ in tp]
            for i, g, p in legs:
                if i < len(tp):
                    params[i] = {"destination_ip": g, "destination_port": int(p)}
            body = {"transport_params": params, "activation": {"mode": "activate_immediate"}}
            st, out = http("PATCH", url, body)
            if st not in (200, 202):
                raise RuntimeError(f"PATCH staged -> {st} {out}")
            act = get(url.replace("/staged", "/active"))["transport_params"]
            for i, cur, w in diffs:
                got = (act[i]["destination_ip"], act[i]["destination_port"])
                res.add(dev["name"], item, f"transport_params.{i}", cur, got, "changed" if got == w else "failed")
    except Exception as e:
        res.add(dev["name"], item, "mcast", "", str(e)[:200], "failed")


def cmd_apply(a):
    site = a.site_dir
    site_vars = {r["key"]: r["value"] for r in read_csv(os.path.join(site, "site.csv"))}
    devices = [d for d in read_csv(os.path.join(site, "devices.csv"))
               if not a.device or d["name"] in a.device]
    res = Result(a.check)
    plan_path = os.path.join(site, "mcast-plan.csv")
    plan = read_csv(plan_path) if os.path.exists(plan_path) else []
    for dev in devices:
        if a.what in ("all", "base"):
            print(f"== {dev['name']} ({dev['type']}, {dev.get('planes')}): base", flush=True)
            apply_base(res, site, site_vars, dev)
        if a.what in ("all", "mcast"):
            rows = [r for r in plan if r["device"] == dev["name"]]
            if dev.get("mcast_apply", "yes").lower() != "yes":
                print(f"== {dev['name']}: mcast skipped (mcast_apply={dev.get('mcast_apply')}: {dev.get('note', '')})", flush=True)
                continue
            if a.limit:
                rows = [r for r in rows if int(r["index"]) < a.limit]
            print(f"== {dev['name']}: mcast, {len(rows)} senders", flush=True)
            with concurrent.futures.ThreadPoolExecutor(8) as ex:
                list(ex.map(lambda r: apply_mcast_one(res, dev, r), rows))
    report = os.path.join(site, "apply-report.csv")
    write_csv(report, res.rows, ["device", "item", "field", "before", "after", "status"])
    s = res.summary()
    print(json.dumps({"changed": s["changed"] > 0, "would_change": s["would_change"] > 0,
                      "counts": s, "check": a.check, "report": report}))
    return 1 if s["failed"] else 0


def main():
    p = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    sub = p.add_subparsers(dest="cmd", required=True)
    pl = sub.add_parser("plan")
    pl.add_argument("--site-dir", required=True)
    ap = sub.add_parser("apply")
    ap.add_argument("--site-dir", required=True)
    ap.add_argument("--check", action="store_true", help="report would_change, send nothing")
    ap.add_argument("--what", choices=("all", "base", "mcast"), default="all")
    ap.add_argument("--device", action="append", help="only this device (repeatable)")
    ap.add_argument("--limit", type=int, default=0, help="mcast: only the first N senders per essence")
    a = p.parse_args()
    if a.cmd == "plan":
        cmd_plan(a)
        return 0
    return cmd_apply(a)


if __name__ == "__main__":
    sys.exit(main())
