"""Export EVERYTHING a FusioN publishes, with its current value, one CSV to filter.

usage: python3 fusion_export_all.py <fusion-red-ip> <out.csv>
Stdlib only; reads the module's own REST API (/emsfp/node/v1) by its own listings.

Columns
  level      fusion | io | cage
  group      MGMT, RED live 1, BLUE live 2, PTP RED/BLUE, NMOS, Syslog, CHn in/out, cage N, ...
  channel    CH1..CH8 when the object belongs to one
  direction  in (SDI -> 2110) | out (2110 -> SDI), from the module's devices record
  essence    video | audio | ancillary, from the receiver/sender record
  leg        RED | BLUE (flow_id[0] / flow_id[1] of the receiver/sender)
  resource   the REST resource the value comes from
  path       dotted path (resource + JSON field), as dhs addresses it
  value      the current value
  json_type  the module's JSON spelling (string / number / bool)
  write      what is known about writing that RESOURCE on this module:
               accepted  = a PUT was accepted in a test (2026-09-28/29)
               refused   = the module refused any PUT (400)
               untested  = never written
  field_note what a probe showed for that FIELD, when one did
"""
import csv, json, sys, urllib.request

IP, OUT = sys.argv[1], sys.argv[2]
BASE = f"http://{IP}/emsfp/node/v1/"


def get(res):
    with urllib.request.urlopen(BASE + res, timeout=15) as r:
        raw = r.read()
    try:
        return json.loads(raw)
    except ValueError:
        return raw.decode(errors="replace")


# Resource-level write knowledge (tests on FusioN6 fw 0x68cd783f).
WRITE = {
    "flows": "accepted", "sdi_output": "accepted", "sdi_input": "accepted", "self/syslog": "accepted",
    "self/diag/nmos": "accepted", "refclk/": "accepted", "self/system": "accepted",
    "receivers": "refused", "senders": "refused",
}
# Field-level probe findings.
FIELD = {
    "id": "write ignored", "version": "write ignored", "type": "write ignored",
    "network.pkt_filter_dst_ip": "fixed at 1 (0 refused)", "network.pkt_filter_src_mac": "write ignored",
    "network.pkt_filter_ssrc": "write ignored", "network.switch_state": "write ignored (status)",
    "network.dst_ip_addr": "bad octet wrapped mod 256; sets dst_mac as a side effect",
    "network.dscp": "module accepts 64 (RFC 2474: 0..63)", "network.rtp_pt": "module accepts 128 (RFC 3550: 0..127)",
    "format.format_type": "write ignored", "format.sampling_format": "refused (follows format codes)",
    "format.format_bit_depth": "refused (follows format codes)", "format.aud_chan_cnt": "module accepts 0 and 17",
    "label": "", "config_reset": "1 refused (400): factory reset is manual",
    "line_offset.frame_sync": "402 when frame_sync is unlicensed",
}


def write_state(resource):
    for k, v in WRITE.items():
        if resource == k.rstrip("/") or resource.startswith(k if k.endswith("/") else k + "/") or resource == k:
            return v
    return "untested"


rows = []


def flatten(resource, doc, ctx):
    def walk(node, path):
        if isinstance(node, dict):
            for k, v in node.items():
                walk(v, path + [k])
        elif isinstance(node, list):
            for i, v in enumerate(node):
                walk(v, path + [str(i)])
        else:
            field = ".".join(path)
            note = next((v for k, v in FIELD.items() if v and (field == k or field.endswith("." + k) and "." in k)), "")
            if not note and field in FIELD:
                note = FIELD[field]
            jt = "bool" if isinstance(node, bool) else "number" if isinstance(node, (int, float)) else \
                 "null" if node is None else "string"
            val = node if not isinstance(node, str) else node.replace("\r", "\\r").replace("\n", "\\n")
            rows.append({**ctx, "resource": resource,
                         "path": resource.replace("/", ".") + ("." + field if field else ""),
                         "value": val, "json_type": jt, "write": write_state(resource), "field_note": note})
    walk(doc, [])


# -- the chain: device -> channel/direction; receiver/sender -> essence/leg of each flow
devices = get("devices")
chan = {}
for d in devices:
    ch = d["label"].replace("Device ", "")
    chan[d["id"]] = (ch, "in" if "sdi" in d["inputs"] else "out")
flow_ctx = {}
for kind in ("receivers", "senders"):
    for r in get(kind):
        ch, dirn = chan.get(r["device_id"], ("", ""))
        for leg, fid in zip(("RED", "BLUE"), r.get("flow_id", [])):
            flow_ctx[fid] = {"channel": ch, "direction": dirn, "essence": r["format"], "leg": leg}

LIVE = {"e1": "RED live 1", "e2": "BLUE live 2", "oob": "MGMT (OOB)"}


def ctx_for(resource, doc):
    c = {"level": "fusion", "group": "", "channel": "", "direction": "", "essence": "", "leg": ""}
    top = resource.split("/")[0]
    if top == "flows":
        fid = resource.split("/")[1]
        c.update(level="io", **flow_ctx.get(fid, {}))
        c["group"] = f"{c['channel']} {c['direction']}".strip() or "flow (no receiver/sender)"
    elif top in ("sdi_output", "sdi_input", "sdi_audio", "clean_switch") and isinstance(doc, dict):
        ch, dirn = chan.get(doc.get("device_id", resource.split("/")[-1]), ("", ""))
        c.update(level="io", channel=ch, direction=dirn, group=f"{ch} {dirn} {top}".strip())
    elif top in ("receivers", "senders", "devices"):
        c.update(level="io", group="NMOS " + top)
    elif top == "port":
        n = resource.split("/")[1]
        c.update(level="cage", group=f"cage {n}" + {"3": " (RED optic)", "5": " (BLUE optic)"}.get(n, " (I/O module)"))
    elif top == "refclk":
        c["group"] = "PTP"
    elif resource.startswith("self/diag/nmos"):
        c["group"] = "NMOS"
    elif resource.startswith("self/syslog"):
        c["group"] = "Syslog"
    elif resource.startswith("self/interfaces") or resource.startswith("self/phy"):
        c["group"] = "RED / BLUE / OOB"
    elif resource.startswith("self/"):
        c["group"] = "MGMT " + resource.split("/")[1]
    else:
        c["group"] = top
    return c


def visit(resource, depth=0):
    try:
        doc = get(resource)
    except Exception as e:  # a listed resource the module will not serve: say so, keep going
        rows.append({"level": "fusion", "group": "unreadable", "channel": "", "direction": "", "essence": "", "leg": "",
                     "resource": resource.rstrip("/"), "path": resource.rstrip("/").replace("/", "."),
                     "value": "", "json_type": "", "write": "", "field_note": f"GET failed: {e}"})
        return
    if isinstance(doc, list) and doc and all(isinstance(x, str) and x.endswith("/") for x in doc):
        for item in doc:
            visit(resource + item.rstrip("/") if resource.endswith("/") or resource == "" else resource + "/" + item.rstrip("/"), depth + 1)
        return
    flatten(resource.rstrip("/"), doc, ctx_for(resource.rstrip("/"), doc))
    # refclk answers a document; its clock inputs are separate resources.
    if resource.rstrip("/") == "refclk" and isinstance(doc, dict):
        for i, u in enumerate(doc.get("uuid", [])):
            live = ("RED live 1", "BLUE live 2")[i] if i < 2 else f"input {i + 1}"
            sub = get("refclk/" + u)
            c = ctx_for("refclk", sub)
            c["group"] = f"PTP {live}"
            flatten("refclk/" + u, sub, c)


visit("")
# relabel the interface rows per live
for r in rows:
    for p, name in LIVE.items():
        if r["resource"] in ("self/interfaces", "self/phy") and (r["path"].split(".")[2:3] == [p]):
            r["group"] = name
cols = ["level", "group", "channel", "direction", "essence", "leg", "resource", "path", "value", "json_type", "write", "field_note"]
with open(OUT, "w", newline="", encoding="utf-8") as f:
    w = csv.DictWriter(f, cols)
    w.writeheader()
    w.writerows(rows)
print(len(rows), "values ->", OUT)
