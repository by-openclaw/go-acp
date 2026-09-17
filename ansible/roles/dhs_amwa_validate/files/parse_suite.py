#!/usr/bin/env python3
"""Gate one AMWA suite result against its baseline.

Usage: parse_suite.py <result.json> <suite_id> <baseline_pass> <baseline_cnt>

Prints a JSON verdict on the LAST line (the Ansible task reads it):
  {"suite": ..., "ok": bool, "pass": N, "fail": N, "cnt": N,
   "warnings": N, "problems": [ ... EVS-style rows ... ]}

Gate rules (the baselines-as-code contract in defaults/main.yml):
  - any Fail/Error row       -> not ok
  - pass  < baseline_pass    -> not ok (regression)
  - cnt   > baseline_cnt     -> not ok (a lost fixture surfaces as CNT
                                before it surfaces as Fail)
  - pass  > baseline_pass    -> ok, but reported so the number gets
                                raised in git.

Every non-pass row is reported with the tool's own test name, its
state, the reason, and the AMWA spec link the tool attaches — the
per-test view EVS asked for.
"""

import json
import sys


def main() -> int:
    path, suite, base_pass, base_cnt = (
        sys.argv[1],
        sys.argv[2],
        int(sys.argv[3]),
        int(sys.argv[4]),
    )
    try:
        with open(path, encoding="utf-8") as f:
            r = json.load(f)
    except (OSError, json.JSONDecodeError) as e:
        print(json.dumps({"suite": suite, "ok": False, "error": f"unreadable result: {e}"}))
        return 1
    if not isinstance(r, dict):
        print(json.dumps({"suite": suite, "ok": False, "error": f"tool error: {str(r)[:200]}"}))
        return 1

    res = r.get("results", [])
    counts: dict[str, int] = {}
    problems = []
    for t in res:
        state = t.get("state", "?")
        counts[state] = counts.get(state, 0) + 1
        if state not in ("Pass", "Not Applicable", "Test Disabled"):
            problems.append(
                {
                    "test": t.get("name", "?"),
                    "state": state,
                    "reason": str(t.get("detail", ""))[:240],
                    "amwa_link": t.get("link") or t.get("url") or "",
                }
            )

    n_pass = counts.get("Pass", 0)
    n_fail = counts.get("Fail", 0) + counts.get("Error", 0)
    n_cnt = counts.get("Could Not Test", 0)
    n_warn = counts.get("Warning", 0)

    ok = n_fail == 0 and n_pass >= base_pass and n_cnt <= base_cnt
    verdict = {
        "suite": suite,
        "ok": ok,
        "pass": n_pass,
        "fail": n_fail,
        "cnt": n_cnt,
        "warnings": n_warn,
        "baseline_pass": base_pass,
        "baseline_cnt": base_cnt,
        "above_baseline": max(0, n_pass - base_pass),
        "states": counts,
        "problems": problems,
    }
    print(json.dumps(verdict))
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
