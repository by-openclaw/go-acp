"""FusioN6 provisioning objects — the base DM and the per-module sheet.

Reads one or more typed `dhs consumer mnset export --format csv` files
(one per module; every FusioN6 shares the same DM) and the model
dictionary, and writes two CSVs readable in VS Code:

  fusion6-objects.csv   one row per object PATTERN: what it means, its
                        values/range, access, where the fact comes from.
  fusion6-modules.csv   one row per module x concrete object, with the
                        current value and an empty target_value to fill.

Only the objects needed to stand a module up are kept: MGMT, the RED /
BLUE media network + PTP, the channels (receivers, senders, SDI I/O,
audio map, licences) and NMOS registration.

    python objects.py ../../consumer/dm/fusion6.json <export.csv> [<export.csv> ...]

Stdlib only.
"""
import csv
import json
import re
import sys

# Section, then the object patterns (dictionary grammar: "*" = one
# segment, a leading "**." = any leading segments) that make it up.
SECTIONS = [
    ("1 MGMT", [
        "self.information.base_type", "self.information.type", "self.information.serial_number",
        "self.information.current_version", "self.information.encap_count", "self.information.decap_count",
        "self.information.input_media.*", "self.information.output_media.*",
        "self.ipconfig.hostname", "self.ipconfig.local_mac", "self.ipconfig.dhcp_enable",
        "self.system.flex_port_mode", "self.system.access_control.media.device_management",
        "self.interfaces.oob.dhcp", "self.interfaces.oob.static_ip", "self.interfaces.oob.static_gateway",
        "self.syslog.config.server", "self.syslog.config.port", "self.syslog.config.enable",
        "self.syslog.monitoring.*.*",
        "self.protocols.mdns_enable", "self.protocols.sap_announcement_enable",
        "self.license.feature.*",
    ]),
    ("2 NETWORK (RED e1 / BLUE e2, PTP)", [
        "self.interfaces.e1.dhcp", "self.interfaces.e1.static_ip", "self.interfaces.e1.static_gateway",
        "self.interfaces.e1.current_ip", "self.interfaces.e1.vlan",
        "self.interfaces.e2.dhcp", "self.interfaces.e2.static_ip", "self.interfaces.e2.static_gateway",
        "self.interfaces.e2.current_ip", "self.interfaces.e2.vlan",
        "self.phy.*.fec.scheme", "self.information.current_link_speed.*",
        "self.system.smpte_network.2022-7.class", "self.system.igmp.version",
        "self.static_route.*.destination", "self.static_route.*.gateway",
        "refclk.delay_req", "refclk.announceReceiptTimeout", "refclk.locked_interface", "refclk.status",
    ]),
    ("3 CHANNELS", [
        "flows.*.name", "flows.*.format.format_type", "flows.*.cdis",
        "flows.*.network.enable", "flows.*.network.*.enable",
        "flows.*.network.dst_ip_addr", "flows.*.network.*.dst_ip_addr",
        "flows.*.network.dst_udp_port", "flows.*.network.*.dst_udp_port",
        "flows.*.network.src_ip_addr", "flows.*.network.*.src_ip_addr",
        "flows.*.network.src_udp_port", "flows.*.network.rtp_pt", "flows.*.network.*.rtp_pt",
        "flows.*.network.ttl", "flows.*.network.dscp", "flows.*.network.ssrc",
        "flows.*.format.range", "flows.*.format.format_tcs", "flows.*.format.format_colorimetry",
        "flows.*.format.aud_format", "flows.*.format.aud_chan_cnt", "flows.*.format.aud_ptime_idx",
        "flows.*.format.anc_flow_profile",
        "sdi_output.*.label", "sdi_output.*.input_signal_output_mode.loss_of_input",
        "sdi_output.*.vpid.source", "sdi_output.*.vpid.override_value",
        "sdi_output.*.line_offset.offset_mode", "sdi_output.*.line_offset.audio_delay",
        "sdi_output.*.sdi_aud_chans_cfg.*",
        "sdi_input.*.label", "sdi_input.*.frame_sync_audio.sdi_in_genlock", "sdi_input.*.line_offset.frame_sync",
        "clean_switch.*.clean_switch.mode", "clean_switch.*.clean_switch.type", "clean_switch.*.clean_switch.timeout_option",
    ]),
    ("4 NMOS", [
        "flows.*.label",
        "self.diag.nmos.registry_mode", "self.diag.nmos.registry_address", "self.diag.nmos.registry_address_2",
        "self.diag.nmos.mdns_mode", "self.diag.nmos.control_network",
        "self.diag.nmos.manual_dns_server_address", "self.diag.nmos.manual_dns_server_address_2",
        "self.diag.nmos.device_visible.*", "self.diag.nmos.status", "self.diag.nmos.current_registry",
        "receivers.*.label", "receivers.*.format", "senders.*.label", "senders.*.format",
    ]),
]

UUIDISH = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|[0-9a-f]{8}|\d+")
# "CH2 · rx ch2 flow 0 pri · network.dst_ip_addr"
FLOW_LABEL = re.compile(r"^(CH\d+) · (rx|tx) ch\d+ flow (\d+) (pri|sec)")
ESSENCE = {"0": "video", "1": "audio 1", "2": "audio 2", "3": "audio 3", "4": "audio 4", "5": "anc"}


def match(pattern, path):
    pat, segs = pattern.split("."), path.split(".")
    if pat[0] == "**":
        pat = pat[1:]
        if len(segs) < len(pat):
            return False
        segs = segs[len(segs) - len(pat):]
    return len(pat) == len(segs) and all(p == "*" or p == s for p, s in zip(pat, segs))


def dict_type(entries, path):
    t = {}
    for e in entries:
        if match(e["match"], path):
            for k in ("kind", "format", "min", "max", "values", "labels", "access", "note", "source", "enum"):
                if k in e:
                    t[k] = e[k]
    if "enum" in t and "values" not in t:
        t["values"], t["labels"] = sorted(t["enum"]), t["enum"]
    return t


def values_or_range(t, row):
    if t.get("values"):
        labels = t.get("labels", {})
        return " | ".join(v + ("=" + labels[v] if v in labels else "") for v in t["values"])
    lo, hi = t.get("min", row.get("min", "")), t.get("max", row.get("max", ""))
    if lo != "" or hi != "":
        return f"{lo}..{hi}"
    return t.get("format", "")


def section_of(path):
    for name, pats in SECTIONS:
        for p in pats:
            if match(p, path):
                return name, p
    return None, None


def main():
    dm = json.load(open(sys.argv[1], encoding="utf-8"))
    entries = dm["entries"]
    patterns, modules = {}, []
    for export in sys.argv[2:]:
        rows = list(csv.DictReader(open(export, encoding="utf-8")))
        ident = {r["path"]: r["value"] for r in rows if r["path"].startswith("self.")}
        mod = {
            "serial": ident.get("self.information.serial_number", "?"),
            "hostname": ident.get("self.ipconfig.hostname", "?"),
            "control_ip": rows[0]["ip"] if rows else "?",
        }
        for r in rows:
            sect, pat = section_of(r["path"])
            if not sect:
                continue
            t = dict_type(entries, r["path"])
            kind = r["kind"]
            access = "RW" if "W" in r["access"] else "R"
            patterns.setdefault(pat, {
                "section": sect, "object": pat, "kind": kind, "access": access,
                "values_or_range": values_or_range(t, r), "unit": r.get("unit", ""),
                "example_value": r["value"], "note": t.get("note", ""), "source": t.get("source", ""),
            })
            lab = r["label"]
            m = FLOW_LABEL.match(lab)
            chan = m.group(1) if m else (lab.split(" · ")[0] if lab.startswith("CH") else "")
            modules.append({
                **mod, "section": sect,
                "channel": chan,
                "direction": m.group(2) if m else "",
                "essence": ESSENCE.get(m.group(3), m.group(3)) if m else "",
                "leg": {"pri": "RED", "sec": "BLUE"}.get(m.group(4), "") if m else "",
                "object": r["path"], "kind": kind, "access": access,
                "current_value": r["value"], "target_value": "",
                "values_or_range": values_or_range(t, r),
            })
    order = {name: i for i, (name, _) in enumerate(SECTIONS)}
    pat_order = {p: i for _, pats in SECTIONS for i, p in enumerate(pats)}
    with open("fusion6-objects.csv", "w", newline="", encoding="utf-8") as f:
        cols = ["section", "object", "kind", "access", "values_or_range", "unit", "example_value", "note", "source"]
        w = csv.DictWriter(f, cols)
        w.writeheader()
        for p in sorted(patterns.values(), key=lambda x: (order[x["section"]], pat_order[x["object"]])):
            w.writerow(p)
    with open("fusion6-modules.csv", "w", newline="", encoding="utf-8") as f:
        cols = ["serial", "hostname", "control_ip", "section", "channel", "direction", "essence", "leg",
                "object", "kind", "access", "current_value", "target_value", "values_or_range"]
        w = csv.DictWriter(f, cols)
        w.writeheader()
        for m in sorted(modules, key=lambda x: (x["serial"], order[x["section"]], x["channel"], x["direction"],
                                                x["essence"], x["leg"], x["object"])):
            w.writerow(m)
    print(f"objects: {len(patterns)} patterns · modules sheet: {len(modules)} rows")


if __name__ == "__main__":
    main()
