"""Score an AMWA result set by COVERAGE, not by failure count.

The tool's "Fail" tally counts only tests that ran. Anything skipped —
Test Disabled, Could Not Test, Not Implemented — contributes zero to it,
so a suite that executes a third of itself still reports "0 Fail".
Leading with that number overstates conformance, which is exactly what
happened here.

Executed  = Pass + Fail + Warning        (a verdict was actually reached)
Skipped   = Test Disabled + Could Not Test + Not Implemented + Manual
N/A       = Not Applicable               (legitimately does not apply)
Coverage  = Executed / (Total - N/A)
"""
import glob, json, collections, os, sys

EXECUTED = {"Pass", "Fail", "Warning"}
NA = {"Not Applicable"}

d = sys.argv[1]
label = sys.argv[2] if len(sys.argv) > 2 else d

tot = collections.Counter()
for f in sorted(glob.glob(os.path.join(d, "*.json"))):
    try:
        doc = json.load(open(f))
    except Exception:
        continue
    for r in doc.get("results", []):
        tot[r.get("state", "?")] += 1

total = sum(tot.values())
executed = sum(v for k, v in tot.items() if k in EXECUTED)
na = sum(v for k, v in tot.items() if k in NA)
skipped = total - executed - na
applicable = total - na
cov = (100.0 * executed / applicable) if applicable else 0.0

print("==== %s ====" % label)
print("  total results      %4d" % total)
print("  not applicable     %4d   (legitimately out of scope)" % na)
print("  applicable         %4d" % applicable)
print("  EXECUTED           %4d   pass=%d fail=%d warn=%d"
      % (executed, tot.get("Pass", 0), tot.get("Fail", 0), tot.get("Warning", 0)))
print("  SKIPPED            %4d   disabled=%d couldnottest=%d notimpl=%d manual=%d"
      % (skipped, tot.get("Test Disabled", 0), tot.get("Could Not Test", 0),
         tot.get("Not Implemented", 0), tot.get("Manual", 0)))
print()
print("  COVERAGE           %5.1f%%  of applicable tests actually reached a verdict" % cov)
print("  of those executed, %5.1f%% passed"
      % (100.0 * tot.get("Pass", 0) / executed if executed else 0.0))
