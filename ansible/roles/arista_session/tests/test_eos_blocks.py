"""Unit tests for the arista_session block-diff engine (stdlib unittest).

Run: python3 -m unittest discover -s ansible/roles/arista_session/tests
"""

import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "filter_plugins"))
import eos_blocks as eb  # noqa: E402

LIVE = """\
! Command: show running-config
ptp domain 77
ptp priority1 248
!
interface Vlan120
   description P2P-RED
   mtu 9214
   ip ospf message-digest-key 1 md5 7 AbCdEf==
!
interface profile RED-CLIENT-PORT
   command switchport
   command switchport access vlan 640
!
router pim sparse-mode
   vrf RED
      ipv4
         rp address 10.6.110.1 239.0.0.0/10
!
spanning-tree mst configuration
   name FAB
   instance 10 vlan  110,120
!
snmp-server user dhs-ro DHS-RO v3 localized f5717f auth sha256 aaaa priv aes bbbb
username admin privilege 15 secret sha512 xyz
end
"""


class ParseTest(unittest.TestCase):
    def test_blocks_and_separators(self):
        b = eb.parse_blocks(LIVE)
        headers = [x["header"] for x in b]
        self.assertIn("interface Vlan120", headers)
        self.assertNotIn("end", headers)
        self.assertFalse(any(h.startswith("!") for h in headers))

    def test_comments_are_config(self):
        b = eb.parse_blocks("router general\n   !! why\n   vrf RED\n")
        self.assertEqual(b[0]["children"], ["   !! why", "   vrf RED"])

    def test_secret_normalisation(self):
        b = {x["header"]: x for x in eb.parse_blocks(LIVE)}
        self.assertIn("   ip ospf message-digest-key 1 md5 <KEY>", b["interface Vlan120"]["norm"])
        self.assertTrue(any(k == "snmp-server user dhs-ro DHS-RO v3 <KEY>" for k in
                            (x["key"] for x in eb.parse_blocks(LIVE))))


class PlanTest(unittest.TestCase):
    def test_in_sync_ignores_secret_encoding_and_order(self):
        want = """\
interface Vlan120
   mtu 9214
   description P2P-RED
   ip ospf message-digest-key 1 md5 0 plaintext
"""
        p = eb.plan(LIVE, want, [r"^interface Vlan120$"])
        self.assertFalse(p["changed"], p)

    def test_unowned_blocks_never_touched(self):
        p = eb.plan(LIVE, "ptp domain 77\nptp priority1 248\n", [r"^ptp "])
        self.assertFalse(p["changed"])
        self.assertFalse(any("username" in c for c in p["commands"]))

    def test_single_line_value_change_removes_first(self):
        p = eb.plan(LIVE, "ptp domain 77\nptp priority1 200\n", [r"^ptp "])
        self.assertEqual(p["commands"], ["no ptp priority1 248", "ptp priority1 200"])

    def test_flat_block_line_level(self):
        want = "interface profile RED-CLIENT-PORT\n   command switchport\n   command switchport access vlan 620\n"
        p = eb.plan(LIVE, want, [r"^interface profile "])
        self.assertEqual(p["commands"], [
            "interface profile RED-CLIENT-PORT",
            "no command switchport access vlan 640",
            "   command switchport access vlan 620",
        ])

    def test_nested_block_rewritten_whole(self):
        want = "router pim sparse-mode\n   !! why\n   vrf RED\n      ipv4\n         rp address 10.6.110.1 239.0.0.0/10\n"
        p = eb.plan(LIVE, want, [r"^router pim"])
        self.assertEqual(p["commands"][:2], ["no router pim sparse-mode", "router pim sparse-mode"])

    def test_mst_region_never_removed_and_exited(self):
        # Regression 2026-09-27: a whole-block rewrite of the MST region staged
        # its removal. It must be corrected line by line and end with `exit`.
        want = "spanning-tree mst configuration\n   name FAB\n   instance 10 vlan  110,120,640\n"
        p = eb.plan(LIVE, want, [r"^spanning-tree mst configuration$"])
        self.assertNotIn("no spanning-tree mst configuration", p["commands"])
        self.assertEqual(p["commands"][-1], "exit")

    def test_ignore_list(self):
        live = "interface Ethernet15/2\n   profile X\n"
        p = eb.plan(live, "", [r"^interface Ethernet"], ["interface Ethernet15/2"])
        self.assertFalse(p["changed"])

    def test_removal_forms(self):
        self.assertEqual(eb._removal("interface Ethernet9/1"), "default interface Ethernet9/1")
        self.assertEqual(eb._removal("no ip igmp snooping vlan 640 querier"), "default ip igmp snooping vlan 640 querier")
        self.assertEqual(eb._removal("snmp-server user dhs-ro DHS-RO v3 localized x"), "no snmp-server user dhs-ro DHS-RO v3")
        self.assertEqual(eb._child_removal("   10 deny 239.0.0.0/16"), "no 10")


class HelperTest(unittest.TestCase):
    def test_eos_ranges(self):
        self.assertEqual(eb.eos_ranges([0, 10, 11, 20]), "0,10-11,20")
        self.assertEqual(eb.eos_ranges([600, 110, 120]), "110,120,600")

    def test_within(self):
        self.assertEqual(eb.within(["239.0.0.0/16"], "239.0.0.0/10"), ["239.0.0.0/16"])
        self.assertEqual(eb.within(["239.0.0.0/16"], "239.64.0.0/10"), [])


if __name__ == "__main__":
    unittest.main()
