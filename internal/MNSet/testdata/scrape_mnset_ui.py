import re, json, glob, collections

out = {"labels": [], "enums": {}, "ids": [], "labels_with_units": []}
files = sorted(glob.glob('.cache/mnset-app/*.js'))
txt = {f: open(f, encoding='utf-8', errors='replace').read() for f in files}

# 1. static label texts from compiled Angular templates: ɵɵtext(n,"Something:")
labels = collections.OrderedDict()
pat_text = re.compile(r'\\u0275\\u0275text\(\d+,"([^"]{2,80})"\)')
for f, s in txt.items():
    for m in pat_text.finditer(s):
        t = m.group(1).strip()
        if t and t not in labels:
            labels[t] = f.split('/')[-1][:6]
out["labels"] = list(labels.keys())

# 2. enum-like arrays: [{value:X,label:"Y"},...] in any key order, incl. name:
pat_arr = re.compile(r'\[(\{(?:value|label|name|id):[^\]]{10,3000}\})\]')
pat_pair = re.compile(r'\{(?:value:([^,}]+),label:"([^"]+)"|label:"([^"]+)",value:([^,}]+)|name:"([^"]+)",value:([^,}]+)|value:([^,}]+),name:"([^"]+)")\}')
enums = {}
for f, s in txt.items():
    for m in pat_arr.finditer(s):
        pairs = pat_pair.findall(m.group(1))
        if len(pairs) >= 2:
            lst = []
            for p in pairs:
                v = p[0] or p[3] or p[5] or p[6]
                l = p[1] or p[2] or p[4] or p[7]
                lst.append({"value": v.strip('"'), "label": l})
            key = " | ".join(x["label"] for x in lst)[:80]
            enums[key] = lst
out["enums"] = enums

# 3. element ids used in forms (candidates to map onto DM field names)
ids = set()
pat_id = re.compile(r'"id","([a-z][a-z0-9_]{2,40})"')
for f, s in txt.items():
    ids.update(pat_id.findall(s))
out["ids"] = sorted(ids)

# 4. labels carrying a unit in parentheses
pat_unit = re.compile(r'\((ms|ns|us|µs|MHz|kHz|Hz|dB|°C|Gbps|Mbps|bytes|frames|lines|pixels|s|%)\)')
out["labels_with_units"] = [l for l in labels if pat_unit.search(l)]

json.dump(out, open('internal/MNSet/testdata/mnset-ui-scrape.json', 'w', encoding='utf-8'), indent=1, ensure_ascii=False)
print("labels:", len(out["labels"]), "| enum lists:", len(enums), "| form ids:", len(ids), "| labels with units:", len(out["labels_with_units"]))
print("units:", out["labels_with_units"][:25])
print("enum samples:")
for k, v in list(enums.items())[:15]:
    print("  ", k[:70], "->", [(x['value'], x['label']) for x in v][:6])
print("ids:", sorted(ids)[:80])
print("label samples:", out["labels"][:60])
