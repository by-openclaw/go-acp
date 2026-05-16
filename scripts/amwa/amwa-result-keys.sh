#!/bin/bash
python3 << 'EOF'
import json
d = json.load(open('/root/amwa-test/results/is04-01-v1.3.json'))
print('Top keys:', list(d.keys()))
print()
for k in d:
    v = d[k]
    if isinstance(v, list):
        print(f'  {k}: list len={len(v)}; first={v[0] if v else None!r:.200}')
    elif isinstance(v, dict):
        print(f'  {k}: dict keys={list(v.keys())[:8]}')
    else:
        print(f'  {k}: {str(v)[:100]}')
EOF
