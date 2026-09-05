#!/usr/bin/env bash
# ADR-0005 rule 2, which the ADR has promised since it was written and which
# has never existed:
#
#   "CI gate: tools/check-deps.sh parses go.mod, fails if any direct dep
#    absent from 0005-deps.json."
#
# Rule 1 says the manifest is the only authoritative list and that go.mod's
# direct deps must match it EXACTLY. Without this script nothing checked, and
# the two drifted apart in both directions — a direct dependency that was
# never approved, and approved entries for libraries nothing imports.
#
# The two directions are NOT symmetric, and that asymmetry is deliberate.
#
# An unapproved direct dependency is a hard failure: rule 1 says the manifest
# is the only authoritative list.
#
# An approved entry nothing imports is only a NOTICE, because pre-approval is
# legitimate here — vault/api is pre-approved by ADR-0010, go-plugin by
# ADR-0009, golang-jwt by ADR-0003, all for work not yet written. Failing on
# those would put this script in conflict with three other ADRs. It still
# reports them, because an entry whose plan was abandoned should eventually
# go, and coder/websocket is exactly that case: superseded by the hand-rolled
# internal/transport/ws, which needs no dependency at all.
#
# stdlib-only by the same spirit as the rest of the repo: this is grep, sed
# and sort. No jq, so it runs on a bare CI image and on a fleet host.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
manifest="$root/docs/adr/0005-deps.json"
gomod="$root/go.mod"

[ -f "$manifest" ] || { echo "::error::missing $manifest (ADR-0005 calls it authoritative)"; exit 1; }
[ -f "$gomod" ] || { echo "::error::missing $gomod"; exit 1; }

# Direct requires only: go.mod puts indirect ones in their own block and
# marks each with a trailing "// indirect" comment.
direct="$(awk '
  /^require \(/ { inblock=1; next }
  inblock && /^\)/ { inblock=0; next }
  inblock && /\/\/ indirect/ { next }
  inblock && NF >= 2 { print $1 }
  /^require [^(]/ && $0 !~ /\/\/ indirect/ { print $2 }
' "$gomod" | sort -u)"

approved="$(grep -oE '"module"[[:space:]]*:[[:space:]]*"[^"]+"' "$manifest" \
  | sed -E 's/.*"module"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/' | sort -u)"

fail=0

# Every direct dependency must be approved.
while IFS= read -r dep; do
  [ -n "$dep" ] || continue
  if ! printf '%s\n' "$approved" | grep -qxF "$dep"; then
    echo "::error::$dep is a DIRECT dependency of go.mod but is not in docs/adr/0005-deps.json"
    echo "         ADR-0005 rule 1: the manifest is the only authoritative list."
    echo "         Either add it (issue + PR + @yboujraf approval, rule 3) or remove the import."
    fail=1
  fi
done <<< "$direct"

# Approved but not imported: reported, never fatal. See the header.
unused=0
while IFS= read -r dep; do
  [ -n "$dep" ] || continue
  if ! printf '%s\n' "$direct" | grep -qxF "$dep"; then
    echo "::notice::$dep is approved but nothing imports it — pre-approval, or a plan that ended?"
    unused=$((unused + 1))
  fi
done <<< "$approved"

if [ "$fail" -eq 0 ]; then
  echo "deps: every direct require in go.mod is approved in docs/adr/0005-deps.json"
  [ "$unused" -eq 0 ] || echo "deps: $unused approved entr(y|ies) not currently imported (see notices)"
fi
exit "$fail"
