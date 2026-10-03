"""Test writes for every object type MN SET's UI edits, straight on the FusioN REST API.

usage: python3 rw_test.py <fusion-ip> <fusion6-ui-dm.csv> <out.csv>

Per object type (resource kind + field), on ONE instance (idle channels only):
  same     the current value written back unchanged
  values   each value MN SET's UI offers for it, in the module's spelling, then restored
Every test restores the original document of that resource before the next one.
Fields that can cut the in-band management path get the same-value test only.
Not for production modules: it writes.
"""
import copy, csv, json, sys, urllib.request, urllib.error

IP, DM, OUT = sys.argv[1:4]
BASE = f"http://{IP}/emsfp/node/v1/"


def req(method, res, body=None):
    data = json.dumps(body).encode() if body is not None else None
    r = urllib.request.Request(BASE + res, data, {"Content-Type": "application/json"}, method=method)
    try:
        with urllib.request.urlopen(r, timeout=20) as resp:
            raw = resp.read()
            return resp.status, (json.loads(raw) if raw else None)
    except urllib.error.HTTPError as e:
        return e.code, None
    except Exception as e:
        return 0, str(e)


def get_in(d, path):
    for k in path.split("."):
        d = d[int(k)] if isinstance(d, list) else d[k]
    return d


def set_in(d, path, v):
    keys = path.split(".")
    p = get_in(d, ".".join(keys[:-1])) if len(keys) > 1 else d
    if isinstance(p, list):
        p[int(keys[-1])] = v
    else:
        p[keys[-1]] = v


# Writes that can remove the only (in-band) path to the module: same-value only.
CUTS_ACCESS = ("self/interfaces", "self/phy", "self/ipconfig", "self/static_route")
CUTS_FIELDS = {("self/system", "flex_port_mode"), ("self/system", "access_control.media.device_management"),
               ("self/diag/nmos", "control_network")}
# Idle instances only (no live stream): receive CH1/CH3, transmit CH5/CH7.
IDLE_CH = {"CH1", "CH3", "CH5", "CH7"}

rows = list(csv.DictReader(open(DM, encoding="utf-8")))
types = {}
for r in rows:
    if r["match"] != "matched" or r["module_field"].endswith("*"):
        continue
    kind = r["module_resource"].split("/")[0] + ("/" + r["module_resource"].split("/")[1]
                                                  if r["module_resource"].startswith(("self/", "refclk/", "port/")) and
                                                  r["module_resource"].count("/") >= 1 and not r["module_resource"].startswith("refclk/") else "")
    key = (kind, r["module_field"], r["essence"].split(" ")[0])
    cand = types.get(key)
    idle = (r["channel"] in IDLE_CH) or not r["channel"]
    if cand is None or (idle and not cand["_idle"]):
        types[key] = {**r, "_idle": idle, "_kind": kind}


def module_values(r):
    """MN SET's offered values in the module's spelling (the option's value attribute)."""
    out = []
    for opt in [o.strip() for o in r["ui_values"].split(" | ") if o.strip()]:
        v = opt.split("=", 1)[0].split(": ", 1)[-1] if "=" in opt else None
        if v not in (None, "", "?"):
            out.append(v)
    return out


results = []
for key, r in sorted(types.items()):
    res, field = r["module_resource"], r["module_field"]
    out = {"resource_kind": r["_kind"], "field": field, "essence": key[2], "tested_on": res,
           "channel": r["channel"], "ui_label": r["ui_label"], "ui_editable": r["ui_editable"],
           "ui_values": r["ui_values"], "same": "", "values": "", "restored": ""}
    if r["ui_editable"] != "yes":
        out["same"] = "not tested (read-only in MN SET)"
        results.append(out)
        continue
    if r["channel"] and not r["_idle"]:
        out["same"] = "not tested (no idle instance)"
        results.append(out)
        continue
    st, orig = req("GET", res)
    if st != 200 or not isinstance(orig, (dict, list)):
        out["same"] = f"GET failed {st}"
        results.append(out)
        continue
    try:
        cur = get_in(orig, field)
    except (KeyError, IndexError, ValueError, TypeError):
        out["same"] = "field not in document"
        results.append(out)
        continue
    doc = copy.deepcopy(orig)
    st, _ = req("PUT", res, doc)
    _, back = req("GET", res)
    out["same"] = f"{st} " + ("kept" if back is not None and get_in(back, field) == cur else "CHANGED")
    tested = []
    cuts = res.startswith(CUTS_ACCESS) or (res, field) in CUTS_FIELDS
    if cuts:
        out["values"] = "change not tested (cuts in-band access)"
    else:
        for v in module_values(r):
            d = copy.deepcopy(orig)
            typed = v if isinstance(cur, str) else (int(v) if v.lstrip("-").isdigit() else v)
            set_in(d, field, typed)
            st, _ = req("PUT", res, d)
            _, back = req("GET", res)
            got = get_in(back, field) if isinstance(back, (dict, list)) else "?"
            verdict = "ok" if st == 200 and str(got) == str(typed) else ("refused" if st >= 400 else f"stored {got}")
            tested.append(f"{v}:{st}:{verdict}")
            req("PUT", res, orig)
        out["values"] = " ; ".join(tested)
    st, _ = req("PUT", res, orig)
    _, back = req("GET", res)
    out["restored"] = "yes" if back is not None and get_in(back, field) == cur else f"NO ({st})"
    results.append(out)
    print(f"{r['_kind'][:14]:14} {field[:34]:34} same={out['same']:8} {out['values'][:70]}", flush=True)

with open(OUT, "w", newline="", encoding="utf-8") as f:
    w = csv.DictWriter(f, list(results[0].keys()))
    w.writeheader()
    w.writerows(results)
print(len(results), "object types ->", OUT)
