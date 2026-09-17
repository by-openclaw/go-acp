import json, urllib.request, sys
SRC="http://127.0.0.1:8235/x-nmos/query/v1.3"
DST="http://10.6.250.5:8080/x-nmos/registration/v1.3/resource"
order=[("nodes","node"),("devices","device"),("sources","source"),("flows","flow"),("senders","sender"),("receivers","receiver")]
for plural,singular in order:
    docs=json.load(urllib.request.urlopen(f"{SRC}/{plural}",timeout=8))
    ok=0; fail=0
    for d in docs:
        body=json.dumps({"type":singular,"data":d}).encode()
        req=urllib.request.Request(DST,data=body,headers={"Content-Type":"application/json"})
        try:
            r=urllib.request.urlopen(req,timeout=8); ok+=1
        except Exception as e:
            fail+=1
            if fail<=2: print(f"  {singular} FAIL: {e}", file=sys.stderr)
    print(f"{plural}: {ok} ok, {fail} fail")
