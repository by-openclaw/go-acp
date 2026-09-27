"""Block-level diff between an intended and a live Arista EOS running-config.

Used by every arista_* role through arista_session. A role renders only the
blocks it owns (a top-level line plus the lines indented under it) and names
them with regex patterns; this filter compares those blocks with the live
ones and returns the commands that make live match intended. Blocks outside
the role's patterns are never read as intended nor touched, so no role can
remove another role's configuration and nothing is ever replaced wholesale.
EOS runs the commands inside a configuration session and commits only the
session's net difference, atomically.

Secrets cannot be compared: EOS stores the OSPF key type-7 encrypted and the
SNMPv3 keys localized. For those lines only their presence is compared; the
plaintext form in the intended text is what gets written when needed.

Standard library only.
"""

import re

_SECRET_CHILD = [
    (re.compile(r"^(\s*ip ospf message-digest-key \d+ md5) \d+ \S+$"), r"\1 <KEY>"),
]
_SECRET_HEADER = [
    (re.compile(r"^(snmp-server user \S+ \S+ v3)( .*)?$"), r"\1 <KEY>"),
]

# Blocks with more than one level of indentation are rewritten whole inside
# the session; flat blocks are corrected line by line.
_NESTED = re.compile(r"^(router |dhcp server|management api )")
# Modes whose edits EOS only applies on an explicit `exit` (MST region: the
# region is pending until the mode is left). Never rewritten whole: removing
# the region even inside a session splits it on commit.
_EXIT_REQUIRED = re.compile(r"^spanning-tree mst configuration$")
# Interfaces that exist in hardware are reset, never deleted.
_DEFAULTABLE = re.compile(r"^interface (Ethernet|Port-Channel|Management)\S*$")


def _norm_child(line):
    for rx, rep in _SECRET_CHILD:
        if rx.match(line):
            return rx.sub(rep, line)
    return line


def _norm_header(line):
    for rx, rep in _SECRET_HEADER:
        if rx.match(line):
            return rx.sub(rep, line)
    return line


def parse_blocks(text):
    """Split running-config text into an ordered list of blocks.

    Separator lines (`!`, indented `!`) and `end` are dropped. `!!` comment
    lines are kept: they are configuration.
    """
    blocks, cur = [], None
    for raw in (text or "").replace("\r", "").split("\n"):
        line = raw.rstrip()
        stripped = line.strip()
        if not stripped or line == "end":
            continue
        if stripped.startswith("!") and not stripped.startswith("!!"):
            continue
        if not line.startswith(" "):
            cur = {"header": line, "key": _norm_header(line), "children": [], "norm": []}
            blocks.append(cur)
        elif cur is not None:
            cur["children"].append(line)
            cur["norm"].append(_norm_child(line))
    return blocks


def _selected(block, patterns, ignore):
    if block["header"] in ignore:
        return False
    return any(re.search(p, block["key"]) for p in patterns)


def _is_nested(block):
    return bool(_NESTED.match(block["key"])) or any(c.startswith("      ") for c in block["children"])


def _removal(header):
    if _DEFAULTABLE.match(header):
        return "default " + header
    if header.startswith("no "):
        return "default " + header[3:]
    m = re.match(r"^(snmp-server user \S+ \S+ v3)", header)
    if m:
        return "no " + m.group(1)
    return "no " + header


def _child_removal(child):
    c = child.strip()
    if re.match(r"^\d+ ", c):  # sequenced entry (ACL, prefix): remove by number
        return "no " + c.split()[0]
    if c.startswith("no "):
        return "default " + c[3:]
    return "no " + c


def plan(live_text, intended_text, patterns, ignore=None):
    """Return {changed, commands, report} moving live toward intended.

    patterns: regexes on block headers this role owns.
    ignore:   exact headers to skip on both sides (e.g. dormant breakout lanes).
    """
    ignore = set(ignore or [])
    live = [b for b in parse_blocks(live_text) if _selected(b, patterns, ignore)]
    want = [b for b in parse_blocks(intended_text) if _selected(b, patterns, ignore)]
    live_by = {b["key"]: b for b in live}
    want_by = {b["key"]: b for b in want}

    removals, writes, report = [], [], []

    # Removed first, so a single-line value that changes (old line out) is
    # gone before the new line is written.
    for b in live:
        if b["key"] not in want_by:
            removals.append(_removal(b["header"]))
            report.append("remove  " + b["key"])

    for b in want:
        have = live_by.get(b["key"])
        if have is None:
            writes.append(b["header"])
            writes.extend(b["children"])
            if _EXIT_REQUIRED.match(b["key"]):
                writes.append("exit")
            report.append("add     " + b["key"])
            continue
        if not _EXIT_REQUIRED.match(b["key"]) and (_is_nested(b) or _is_nested(have)):
            if have["norm"] == b["norm"]:
                continue
            writes.append(_removal(b["header"]))
            writes.append(b["header"])
            writes.extend(b["children"])
            report.append("rewrite " + b["key"])
            continue
        if sorted(have["norm"]) == sorted(b["norm"]):
            continue
        want_set, have_set = set(b["norm"]), set(have["norm"])
        writes.append(b["header"])
        for raw, n in zip(have["children"], have["norm"]):
            if n not in want_set:
                writes.append(_child_removal(raw))
        for raw, n in zip(b["children"], b["norm"]):
            if n not in have_set:
                writes.append(raw)
        if _EXIT_REQUIRED.match(b["key"]):
            writes.append("exit")
        report.append("change  " + b["key"])

    commands = removals + writes
    return {"changed": bool(commands), "commands": commands, "report": report}


def within(nets, supernet):
    """Subset of `nets` (CIDR strings) that lie inside `supernet`."""
    import ipaddress
    sup = ipaddress.ip_network(supernet, strict=False)
    return [n for n in nets if ipaddress.ip_network(n, strict=False).subnet_of(sup)]


def eos_ranges(nums):
    """[0, 10, 11, 20] -> '0,10-11,20' (how EOS prints number lists)."""
    vals = sorted({int(n) for n in nums})
    out, i = [], 0
    while i < len(vals):
        j = i
        while j + 1 < len(vals) and vals[j + 1] == vals[j] + 1:
            j += 1
        out.append(str(vals[i]) if i == j else "%d-%d" % (vals[i], vals[j]))
        i = j + 1
    return ",".join(out)


class FilterModule(object):
    def filters(self):
        return {"eos_plan": plan, "eos_blocks": parse_blocks,
                "ip_within": within, "eos_ranges": eos_ranges}
