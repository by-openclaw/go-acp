#!/bin/bash
F="${1:?file required}"
python3 << EOF
import json
d = json.load(open('$F'))
for t in d.get('results', []):
    if t['state'] == 'Fail':
        print('=' * 60)
        print(t['name'])
        print('-' * 60)
        print(t['detail'])
EOF
