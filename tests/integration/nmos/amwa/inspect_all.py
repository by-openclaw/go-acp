#!/usr/bin/env python3
"""Round-23 cross-suite regression audit. Read every IS-04-0X v1.Y JSON
under /root/amwa-test/results/ and emit a per-file table of Fail +
Warning + Not Implemented entries with their detail (first 140 chars)."""
import json, glob, sys, os
paths = sorted(glob.glob(os.path.join(sys.argv[1] if len(sys.argv) > 1 else '/root/amwa-test/results', 'is04-0*.json')))
for path in paths:
    raw = open(path, 'rb').read()
    if raw.startswith(b'\xef\xbb\xbf'):
        raw = raw[3:]
    try:
        d = json.loads(raw.decode('utf-8'))
    except Exception as e:
        print(f"=== {os.path.basename(path)} === DECODE ERROR: {e}")
        continue
    interesting = [r for r in d.get('results', []) if r['state'] in ('Fail','Warning','Not Implemented')]
    if not interesting:
        print(f"=== {os.path.basename(path)} === clean")
        continue
    print(f"=== {os.path.basename(path)} ===")
    for r in interesting:
        detail = (r.get('detail') or '').replace('\n', ' ')[:160]
        print(f"  {r['state']:<18} {r['name']:<14} {detail}")
