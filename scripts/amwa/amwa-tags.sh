#!/bin/bash
curl -s 'https://hub.docker.com/v2/repositories/amwa/nmos-testing/tags?page_size=30' \
  | python3 -c "import json,sys
d=json.load(sys.stdin)
for r in d.get('results',[]):
    print(r['name'], '->', r['last_updated'][:10])"
