#!/usr/bin/env python3
import json, sys
raw = open(sys.argv[1], 'rb').read()
if raw.startswith(b'\xef\xbb\xbf'):
    raw = raw[3:]
d = json.loads(raw.decode('utf-8'))
for r in d['results']:
    if r['state'] in ('Fail', 'Warning'):
        print(r['name'], r['state'], (r.get('detail') or '')[:240])
