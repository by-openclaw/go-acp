#!/usr/bin/env python3
"""Take acceptance probe — the timing of an ST 2110 take, measured.

For each take: a Cerebrum ROUTE through the dhs CLI (account from the
environment, never an argument), then
  * the receiver's IS-05 /active polled every 20 ms until the sender changes
    (requested_time and activation_time are TAI: -37 s to UTC);
  * optionally the receiver's own stream status (NeuronView REST
    inputs/streams/{uuid}/status: receiving, videoFormat) polled as fast as it
    answers, from 0.3 s before the take to 3 s after, to time the stream gap
    the receiver reports.
Alternates between two sources; the last take restores the first state.

Prints one JSON object per take, then {"summary": ...}. Exit 1 when a take
fails or the receiver does not follow.
"""
import argparse
import json
import ssl
import subprocess
import sys
import threading
import time
import urllib.request

TAI_UTC = 37
CTX = ssl._create_unverified_context()  # lab devices ship self-signed certificates


def getj(url, timeout=2.0):
    ctx = CTX if url.startswith("https") else None
    with urllib.request.urlopen(url, timeout=timeout, context=ctx) as r:
        return json.load(r)


def tai(ts):
    s, ns = ts.split(":")
    return int(s) + int(ns) / 1e9 - TAI_UTC


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dhs", default="/usr/local/bin/dhs")
    ap.add_argument("--cerebrum", required=True, help="host:port of Cerebrum Northbound")
    ap.add_argument("--dest", required=True, help="Cerebrum destination ID")
    ap.add_argument("--sources", required=True, help="two Cerebrum source IDs, comma-separated; the take alternates")
    ap.add_argument("--levels", default="1,2", help="levels to take, comma-separated")
    ap.add_argument("--receiver", required=True, help="the receiver's IS-05 /active URL")
    ap.add_argument("--status", default="", help="NeuronView inputs/streams URL (optional)")
    ap.add_argument("--stream", default="", help="stream label for --status, e.g. 'VIDEO STREAM 1'")
    ap.add_argument("--takes", type=int, default=20)
    ap.add_argument("--gap", type=float, default=6.0, help="seconds between takes")
    a = ap.parse_args()

    srcs = [s.strip() for s in a.sources.split(",")]
    levels = [l.strip() for l in a.levels.split(",") if l.strip()]
    status_url = ""
    if a.status:
        streams = getj(a.status)
        streams = streams if isinstance(streams, list) else streams.get("streams", [])
        uuid = next(s["uuid"] for s in streams if (s.get("label") or s.get("name")) == a.stream)
        status_url = f"{a.status}/{uuid}/status"

    first = getj(a.receiver)["sender_id"]
    prev = first
    print(json.dumps({"start": time.time(), "sender": first}), flush=True)
    rows = []
    for i in range(a.takes):
        # Alternate starting with the source NOT feeding the receiver now is
        # unknown here (Cerebrum IDs vs NMOS ids), so start with srcs[0] and
        # treat an already-converged first take as a no-op.
        src = srcs[i % 2]
        samples, stop = [], threading.Event()

        def poll():
            while not stop.is_set():
                t = time.time()
                try:
                    st = getj(status_url, 1)
                    samples.append((t, st.get("receiving"), st.get("videoFormat")))
                except Exception as e:  # keep polling through a hiccup
                    samples.append((t, None, str(e)[:40]))

        th = None
        if status_url:
            th = threading.Thread(target=poll, daemon=True)
            th.start()
            time.sleep(0.3)
        argv = [a.dhs, "consumer", "cerebrum-nb", "route", a.cerebrum, "--timeout", "10s", "--log", "off"]
        for l in levels:
            argv += ["--route", f"{a.dest}:{src}:{l}"]
        t0 = time.time()
        out = subprocess.run(argv, capture_output=True, text=True)
        t1 = time.time()
        noop = "changed=0" in out.stdout
        ok = out.returncode == 0
        seen, act_doc = None, None
        if ok and not noop:
            deadline = time.time() + 5
            while time.time() < deadline:
                act_doc = getj(a.receiver)
                if act_doc["sender_id"] != prev:
                    seen = time.time()
                    break
                time.sleep(0.02)
        if th:
            time.sleep(3)
            stop.set()
            th.join()
        act = (act_doc or {}).get("activation", {})
        gap = None
        start = None
        for t, rec, fmt in samples:
            bad = rec is False or fmt in ("NA", "", None)
            if bad and start is None:
                start = t
            if not bad and start is not None:
                gap = (t - start) * 1000
                break
        dt = [b[0] - x[0] for x, b in zip(samples, samples[1:])]
        row = dict(take=i + 1, src=src, ok=ok, noop=noop, followed=seen is not None or noop,
                   route_s=round(t1 - t0, 3),
                   ack_to_request_ms=round((tai(act["requested_time"]) - t1) * 1000) if act.get("requested_time") else None,
                   request_to_activation_ms=round((tai(act["activation_time"]) - tai(act["requested_time"])) * 1000) if act.get("activation_time") else None,
                   activation_to_seen_ms=round((seen - tai(act["activation_time"])) * 1000) if seen and act.get("activation_time") else None,
                   receiver_gap_ms=round(gap) if gap is not None else None,
                   status_poll_ms=round(1000 * sum(dt) / len(dt), 1) if dt else None,
                   groups=[t.get("multicast_ip") for t in (act_doc or {}).get("transport_params", [])],
                   err=out.stderr.strip()[-200:] if not ok else "")
        rows.append(row)
        print(json.dumps(row), flush=True)
        if act_doc:
            prev = act_doc["sender_id"]
        time.sleep(a.gap)

    real = [r for r in rows if not r["noop"]]
    acts = [r["request_to_activation_ms"] for r in real if r["request_to_activation_ms"] is not None]
    summary = dict(takes=len(rows), noop=len(rows) - len(real), failed=sum(not r["ok"] for r in rows),
                   not_followed=sum(not r["followed"] for r in rows),
                   activation_ms_min=min(acts) if acts else None, activation_ms_max=max(acts) if acts else None,
                   receiver_gap_seen=sum(r["receiver_gap_ms"] is not None for r in real),
                   final_sender=prev, restored=prev == first)
    print(json.dumps({"summary": summary}), flush=True)
    return 0 if summary["failed"] == 0 and summary["not_followed"] == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
