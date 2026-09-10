# Menu, 16-bit generation (SP_GETFUNC 8 / SP_BLOCKHEADER 39 / SP_GETNEXTPKT 35 / SP_RETFUNC 9 — FUNC_STR)

The older generation's control surface, from a real Snell IQ card.

## Spec

RollCall Rev 14 section 11.4.1. `FUNC_STR` is one menu line: index, style, a
**16-bit** command, min, max, step, then fixed-width text. There is no
`SP_GETMENUCOUNT` in this generation — the client asks for the whole menu with
`SP_GETFUNC` and the unit answers with a block header, then one line per
`SP_GETNEXTPKT`. The same block-transfer shape as `../device_map/`.

The 32-bit generation carries the same information in `MENUITEM_STR` with a
32-bit command and NUL-terminated text: see `../menu_32bit/`.

## What these bytes show

    GETFUNC     0000-00-00:2 -> 0000-0C-01:4
    BLOCKHEADER 0000-0C-01:4 -> 0000-00-00:2   167 items of GETFUNC
    GETNEXTPKT  0000-00-00:2 -> 0000-0C-01:4   item 0 of GETFUNC
    RETFUNC     0000-0C-01:4 -> 0000-00-00:2   line 0 cmd=0 List+hidden "Menu"
    GETNEXTPKT  0000-00-00:2 -> 0000-0C-01:4   item 1 of GETFUNC
    RETFUNC     0000-0C-01:4 -> 0000-00-00:2   line 1 cmd=0 VLevel+hidden+disabled "RETURN"

`0000-0C-01` is an IQDBE00 Nodal card, reached as port 1 of the IQ frame's
gateway (unit `0x0C`). A card of 167 lines costs 167 round trips in this
generation, one line per request — which is why the whole walk of one card
takes seconds on real hardware, and why a proxy that adds a hop per request
makes it take noticeably longer.

Both sessions in the full capture were opened without `SV_LONGSTR` (service
masks `0x0080` and `0x0003`). The IQ frame does not advertise it, so every
client speaks this generation to it.

## Where these bytes came from

The RollCall bytes are the card's own: they are the first six frames after
`SP_GETFUNC` in
[`../../fixtures/iq-frame-IQDBE00/wire.jsonl`](../../fixtures/iq-frame-IQDBE00/wire.jsonl),
recorded by our client walking the frame at `10.6.255.113` on 2026-09-09.

The Ethernet, IP and TCP headers around them are **not** the device's. That
recording is a JSONL wire trace (ADR-0021), which keeps RollCall frames and not
the packets they travelled in, so this pcap was made from it with Wireshark's
`text2pcap`:

    text2pcap -D -T 50000,2050 -4 10.6.250.200,10.6.255.113 trace.txt out.pcapng

Addresses, ports and sequence numbers are therefore stand-ins. Everything
inside the TCP payload is exactly what the card sent.
