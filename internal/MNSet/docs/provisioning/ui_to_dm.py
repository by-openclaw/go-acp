"""Match MN SET's scraped UI controls to the FusioN's own objects -> the DM.

usage: python3 ui_to_dm.py <mnset-ui-scrape.csv> <fusion-snapshot-dir> <out.csv>

Each MN SET control is mapped to the module field it edits, by the control's id
(MN SET names its inputs after the module's JSON fields) and, for per-channel
views, by the channel chain the module itself publishes (devices -> receivers /
senders -> flow_id [RED, BLUE] -> flows; sdi_output / sdi_input / clean_switch
by device_id). The table below is the whole mapping; a control it does not name
is written as UNMATCHED so nothing is silently dropped.
"""
import csv, glob, json, os, sys

SCRAPE, SNAP, OUT = sys.argv[1:4]


def snap(res):
    p = os.path.join(SNAP, res.replace("/", "__") + ".json")
    return json.load(open(p)) if os.path.exists(p) else None


def dig(doc, path):
    for k in path.split("."):
        if isinstance(doc, list):
            doc = doc[int(k)]
        elif isinstance(doc, dict) and k in doc:
            doc = doc[k]
        else:
            return None
    return doc


# --- device-level controls: (view tab, ui id or label) -> (resource, field) --------------------------
DEVICE = {
    ("Device", "device_label"): ("self/ipconfig", "hostname"),
    ("Device", "primary_ip"): ("self/interfaces", "e1.static_ip"),
    ("Device", "primary_subnet"): ("self/interfaces", "e1.static_ip"),
    ("Device", "primary_gateway"): ("self/interfaces", "e1.static_gateway"),
    ("Device", "Secondary IP Address:"): ("self/interfaces", "e2.static_ip"),
    ("Device", "second_subnet"): ("self/interfaces", "e2.static_ip"),
    ("Device", "second_gateway"): ("self/interfaces", "e2.static_gateway"),
    ("Device", "Management IP Address:"): ("self/interfaces", "oob.static_ip"),
    ("Device", "mgmt_subnet"): ("self/interfaces", "oob.static_ip"),
    ("Device", "mgmt_gateway"): ("self/interfaces", "oob.static_gateway"),
    ("Device", "dhcp_enable"): ("self/ipconfig", "dhcp_enable"),
    ("Device", "ctl_vlan_id"): ("self/ipconfig", "ctl_vlan_id"),
    ("Device", "e1_dhcp"): ("self/interfaces", "e1.dhcp"),
    ("Device", "e1_vlan_id"): ("self/interfaces", "e1.vlan"),
    ("Device", "e2_dhcp"): ("self/interfaces", "e2.dhcp"),
    ("Device", "e2_vlan_id"): ("self/interfaces", "e2.vlan"),
    ("Device", "mgmt_dhcp"): ("self/interfaces", "oob.dhcp"),
    ("Device", "mgmt_vlan_id"): ("self/interfaces", "oob.vlan"),
    ("Device", "mdns_enable"): ("self/protocols", "mdns_enable"),
    ("Device", "sap_announcement_enable"): ("self/protocols", "sap_announcement_enable"),
    ("Device", "media_enable"): ("self/system", "access_control.media.device_management"),
    ("PTP", "Multicast"): ("refclk", "mode"),
    ("PTP", "Auto"): ("refclk", "manual_ctrl"),
    ("PTP", "Sources 1"): ("refclk", "selected_uuid"),
    ("PTP", "domain_num"): ("refclk/{ref1}", "domain_num"),
    ("PTP", "vlan_id"): ("refclk/{ref1}", "vlan_id"),
    ("PTP", "dscp"): ("refclk/{ref1}", "dscp"),
    ("PTP", "grand_master_id"): ("refclk/{ref1}", "grandmaster_id"),
    ("PTP", "domin_number_s2"): ("refclk/{ref2}", "domain_num"),
    ("PTP", "vlan_id2"): ("refclk/{ref2}", "vlan_id"),
    ("PTP", "dscp_s2"): ("refclk/{ref2}", "dscp"),
    ("PTP", "grand_master_id_s2"): ("refclk/{ref2}", "grandmaster_id"),
    ("Location", "rate"): ("lldp", "configuration.rate"),
    ("Location", "enable"): ("lldp", "configuration.enable_rx"),
    ("DNS", "Server address:"): ("self/diag/dns", "dns.server_address"),
    ("DNS", "Domain Name:"): ("self/diag/dns", "dns.domain_name"),
    ("DNS", "Host:"): ("self/diag/dns", "lookup.host"),
    ("DNS", "Name:"): ("self/diag/dns", "lookup.name"),
    ("DNS", "Address:"): ("self/diag/dns", "lookup.address"),
    ("NMOS", "control-network"): ("self/diag/nmos", "control_network"),
    ("NMOS", "registry_address"): ("self/diag/nmos", "registry_address"),
    ("NMOS", "registry_address_2"): ("self/diag/nmos", "registry_address_2"),
    ("NMOS", "uptime"): ("self/diag/nmos", "uptime"),
    ("NMOS", "connection_count"): ("self/diag/nmos", "connection_count"),
    ("NMOS", "Auto"): ("self/diag/nmos", "registry_mode"),
    ("NMOS", "dns_registry_service"): ("self/diag/nmos", "dns_registry_service"),
    ("NMOS", "enable"): ("self/diag/nmos", "mdns_mode"),
    ("NMOS", "dns_server_address"): ("self/diag/nmos", "dns_server_address"),
    ("NMOS", "manual_dns_server_address"): ("self/diag/nmos", "manual_dns_server_address"),
    ("Ports", "operatingBitRate"): ("sdi", "configuration.operating_bit_rate"),
    ("Ports", "rs-fec-3"): ("self/phy", "e1.fec.scheme"),
    ("Ports", "rs-fec-5"): ("self/phy", "e2.fec.scheme"),
    ("Ports", "bb"): ("port/4", "host_pinout"),
    ("Ports", "mgmt"): ("self/system", "flex_port_mode"),
    ("Syslog", "Server"): ("self/syslog", "config.server"),
    ("Syslog", "Port"): ("self/syslog", "config.port"),
    ("Syslog", "enable"): ("self/syslog", "config.enable"),
    ("Syslog", "ptpEvent"): ("self/syslog", "monitoring.common.ptp_event"),
}
for ev, grp in (("temp_event", "common"), ("northbound_api_event", "common"), ("rtp_timestamp_audio_event", "common"),
                ("fan_speed", "common"), ("sdi_event", "encap"), ("no_signal", "encap"), ("output_flywheel", "decap"),
                ("memory_pkt_error", "decap"), ("flow_impairment", "decap"), ("dash7_fifo_error", "decap"),
                ("frame_repeat", "decap"), ("frame_skipped", "decap")):
    DEVICE[("Syslog", ev)] = ("self/syslog", f"monitoring.{grp}.{ev}")
for n in range(1, 6):
    DEVICE[("Routes", f"destination_{n}}}")] = ("self/static_route", f"route_{n}.destination")
    DEVICE[("Routes", f"gateway_{n}")] = ("self/static_route", f"route_{n}.gateway")
for tab in ("Device", "PTP", "Location", "DNS", "NMOS", "Ports", "Syslog", "Routes"):
    DEVICE[(tab, "device_label")] = ("self/ipconfig", "hostname")      # the header repeated on every tab
for n in (2, 3, 4):
    DEVICE[("NMOS", f"manual_dns_server_address_{n}")] = ("self/diag/nmos", f"manual_dns_server_address_{n}")
# MN SET's own helpers, not module fields: kept visible, marked as such
MNSET_ONLY = {"tags", "auto_set_source_ip", "Use Name entry to set Label", "Tags:", "static_location_input", "Static entry"}
for n in range(1, 9):
    DEVICE[("NMOS", f"channel_{n}_enable")] = ("self/diag/nmos", f"device_visible.{n - 1}")

# --- per-channel flow controls: ui id -> field inside the flow document -------------------------------
FLOW = {
    "cdis": "cdis", "video_format": "format.format_code_*", "flow-colorimetry": "format.format_colorimetry",
    "flow-tranfer_characteristics": "format.format_tcs", "src_ip_addr": "network{n}.src_ip_addr",
    "src_udp_port": "network{n}.src_udp_port", "dst_ip_addr": "network{n}.dst_ip_addr",
    "dst_udp_port": "network{n}.dst_udp_port", "dst_mac": "network{n}.dst_mac", "vlan_tag": "network{n}.vlan_tag",
    "network_enable": "network{n}.enable", "rtp_pt": "network{n}.rtp_pt", "igmp_src_ip": "network{n}.igmp_src_ip",
    "sender_type": "network{n}.sender_type", "name": "name", "label": "label",
    "pkt_filter_src_ip": "network{n}.pkt_filter_src_ip", "pkt_filter_src_udp": "network{n}.pkt_filter_src_udp",
    "pkt_filter_src_mac": "network{n}.pkt_filter_src_mac", "pkt_filter_dst_ip": "network{n}.pkt_filter_dst_ip",
    "pkt_filter_dst_udp": "network{n}.pkt_filter_dst_udp", "pkt_filter_dst_mac": "network{n}.pkt_filter_dst_mac",
    "pkt_filter_vlan": "network{n}.pkt_filter_vlan", "aud_chan_cnt": "format.aud_chan_cnt",
    "aud-format": "format.aud_format", "aud_ptime_idx": "format.aud_ptime_idx", "anc_flow_profile": "format.anc_flow_profile",
}
FLOW_LABEL = {"Sampling:": "format.format_code_sampling", "Bit Depth:": "format.format_bit_depth", "Color Space:": "format.sampling_format"}
SDI_OUT = {"video_format": "input_signal_output_mode.loss_of_input", "vpid_selector": "vpid.source"}
REF_OUT = {"MicroSeconds units": "line_offset.offset_mode", "usec_offset": "line_offset.usec_offset",
           "v_offset": "line_offset.v_offset", "h_offset": "line_offset.h_offset",
           "frame_buffer": "line_offset.frame_buffer", "audio_delay": "line_offset.audio_delay"}
REF_IN = {"Enabled": "line_offset.frame_sync", "MicroSeconds units": "line_offset.offset_mode",
          "usec_offset": "line_offset.usec_offset", "v_offset": "line_offset.v_offset", "h_offset": "line_offset.h_offset",
          "sdi_in_genlock": "frame_sync_audio.sdi_in_genlock", "audio_delay_enc": "frame_sync_audio.audio_delay"}
CLEAN = {"Enable": "clean_switch.mode", "igmp_delay": "clean_switch.igmp_setup_delay", "select_type": "clean_switch.type"}

# --- the module's own chain, from the snapshot -----------------------------------------------------
devices = snap("devices")
ch_dev = {d["label"].replace("Device ", ""): d["id"] for d in devices}
flows_of = {}   # (CHn, leg, essence) -> flow id
ESS = {"video": "Video", "audio": "Audio", "ancillary": "Ancillary"}
for kind in ("receivers", "senders"):
    for r in snap(kind):
        ch = next((c for c, i in ch_dev.items() if i == r["device_id"]), "")
        n_audio = sum(1 for k in flows_of if k[0] == ch and k[2].startswith("Audio") and k[1] == "RED primary") + 1
        ess = ESS[r["format"]] + (f" {n_audio}" if r["format"] == "audio" else "")
        for leg, fid in zip(("RED primary", "BLUE secondary"), r["flow_id"]):
            flows_of[(ch, leg, ess)] = fid
by_dev = {}
for pat in ("sdi_output", "sdi_input", "clean_switch"):
    for f in glob.glob(os.path.join(SNAP, pat + "__*.json")):
        d = json.load(open(f))
        dev = d.get("device_id") or os.path.basename(f)[len(pat) + 2:-5]
        by_dev[(pat, dev)] = pat + "/" + os.path.basename(f)[len(pat) + 2:-5]
ref = snap("refclk")
refs = {"{ref1}": ref["uuid"][0], "{ref2}": ref["uuid"][1] if len(ref["uuid"]) > 1 else ""}

rows = []
for c in csv.DictReader(open(SCRAPE, encoding="utf-8")):
    parts = c["view"].split(" > ")
    key = c["id"] or c["label"]
    res = field = None
    ch = leg = ess = ""
    if parts[0] == "Device":
        tab = parts[1]
        hit = DEVICE.get((tab, c["id"])) or DEVICE.get((tab, c["label"]))
        if hit:
            res, field = hit
            for k, v in refs.items():
                res = res.replace(k, v)
    elif parts[0] == "Signals":
        ch = parts[1]
        if len(parts) == 4:                       # Flows: CHn > leg > essence
            leg, ess = parts[2], parts[3]
            fid = flows_of.get((ch, leg, ess))
            f = FLOW.get(c["id"]) or FLOW_LABEL.get(c["label"])
            if fid and f:
                doc = snap("flows/" + fid) or {}
                listy = isinstance(doc.get("network"), list)
                res, field = "flows/" + fid, f.replace("{n}", ".0" if listy else "")
        else:
            tab = parts[2]
            dev = ch_dev.get(ch, "")
            table = {"SDI": SDI_OUT, "Clean switch": CLEAN}.get(tab)
            if tab == "Reference":
                table = REF_IN if (("sdi_input", dev) in by_dev and ("sdi_output", dev) not in by_dev) else REF_OUT
            pat = {"SDI": "sdi_output", "Clean switch": "clean_switch"}.get(tab) or \
                  ("sdi_input" if table is REF_IN else "sdi_output")
            f = table.get(c["id"]) or table.get(c["label"]) if table else None
            if f and (pat, dev) in by_dev:
                res, field = by_dev[(pat, dev)], f
    current = ""
    if res and not field.endswith("*"):
        current = dig(snap(res), field)
    rows.append({
        "section": parts[0] + (" > " + parts[1] if parts[0] == "Device" else ""),
        "channel": ch, "leg": leg, "essence": ess, "ui_label": c["label"], "ui_control": c["tag"], "ui_id": c["id"],
        "ui_editable": "no" if c["disabled"] == "True" else "yes", "ui_values": c["options"],
        "ui_value": c["value"], "module_resource": res or "", "module_field": field or "",
        "module_value": "" if current is None else current,
        "match": "matched" if res else ("MN SET only (not on the module)" if (c["id"] in MNSET_ONLY or c["label"] in MNSET_ONLY
                                          or c["label"].startswith("Use Name entry")) else "UNMATCHED"),
    })

with open(OUT, "w", newline="", encoding="utf-8") as f:
    w = csv.DictWriter(f, list(rows[0].keys()))
    w.writeheader()
    w.writerows(rows)
m = sum(1 for r in rows if r["match"] == "matched")
print(f"{len(rows)} UI controls, {m} matched, {len(rows) - m} unmatched -> {OUT}")
