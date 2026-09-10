#!/usr/bin/env python3
"""Unit tests for the reference implementation. Run: python3 test_ipcprobe.py

Frame-level regression is driven by ../testdata/vectors.json (sanitized, synthetic identifiers) via check_vectors.py;
this file adds targeted unit tests for the builders and validators using synthetic inputs only. No real device
identifiers appear anywhere in this repository.
"""
import base64
import ipaddress
import json
import os
import struct
import unittest

import ipcprobe

HERE = os.path.dirname(os.path.abspath(__file__))
VECTORS = json.load(open(os.path.join(HERE, "..", "testdata", "vectors.json")))
MAC = bytes.fromhex("020000000001")


def A(s):
    return ipaddress.IPv4Address(s)


def vec(name_sub):
    return next(v for v in VECTORS["vectors"] if name_sub in v["name"])


class TestVectors(unittest.TestCase):
    """Every vector in testdata/vectors.json must round-trip through the reference."""

    def test_announce_vectors(self):
        for v in [x for x in VECTORS["vectors"] if x["cmd"] == ipcprobe.CMD_ANNOUNCE]:
            self.assertEqual(ipcprobe.parse_announce(bytes.fromhex(v["hex"])), v["expect"], v["name"])

    def test_ack_vector(self):
        v = vec("ack")
        ip, port = ipcprobe.parse_set_ack(bytes.fromhex(v["hex"]))
        self.assertEqual((ip, port), (v["expect_ack"]["requester_ip"], v["expect_ack"]["requester_port"]))

    def test_search_vector(self):
        self.assertEqual(ipcprobe.build_search_packet(), bytes.fromhex(vec("search")["hex"]))

    def test_set_vector_rebuilds(self):
        v = vec("set-network")
        d = v["decodes_to"]
        built = ipcprobe.build_set_packet(ipcprobe.parse_mac(d["mac"]), A(d["ip"]), A(d["mask"]), A(d["gateway"]),
                                          A(d["dns1"]), A(d["dns2"]), d["password"])
        self.assertEqual(built, bytes.fromhex(v["hex"]))


class TestSearch(unittest.TestCase):
    def test_layout(self):
        p = ipcprobe.build_search_packet()
        self.assertEqual(len(p), 140)
        self.assertEqual(p[:10], bytes.fromhex("4D484544090001000100"))
        self.assertEqual(p[10:], b"\0" * 130)


class TestSet(unittest.TestCase):
    def build(self, password="admin123"):
        return ipcprobe.build_set_packet(MAC, A("192.168.1.68"), A("255.255.255.0"), A("192.168.1.1"),
                                         A("192.168.1.1"), A("8.8.8.8"), password)

    def test_layout(self):
        p = self.build()
        self.assertEqual(len(p), 140)
        self.assertEqual(p[0:4], b"MHED")
        self.assertEqual(struct.unpack_from("<HHH", p, 4), (9, 1, 3))
        self.assertEqual(p[10:32], b"\0" * 22)
        self.assertEqual(p[32:38], MAC)
        self.assertEqual(p[38:40], b"\0\0")
        self.assertEqual(p[40:44], bytes.fromhex("C0A80144"))
        self.assertEqual(p[44:48], bytes.fromhex("FFFFFF00"))
        self.assertEqual(p[48:52], bytes.fromhex("C0A80101"))
        self.assertEqual(p[52:84], b"\0" * 32)
        self.assertEqual(p[84:112], base64.b64encode(b"admin123").ljust(28, b"\0"))
        self.assertEqual(p[112:116], bytes.fromhex("C0A80101"))
        self.assertEqual(p[116:120], bytes.fromhex("08080808"))
        self.assertEqual(p[120:140], b"\0" * 20)

    def test_empty_password_is_all_zero_field(self):
        self.assertEqual(self.build("")[84:112], b"\0" * 28)

    def test_password_limit(self):
        self.build("x" * 21)                       # 21 bytes -> 28 base64 chars, fits exactly
        with self.assertRaises(ValueError):
            self.build("x" * 22)                   # 22 bytes -> 32 chars, too long


class TestParsePrimitives(unittest.TestCase):
    def test_build_date(self):
        pkt = bytearray(240)
        pkt[0:10] = ipcprobe.MAGIC + struct.pack("<HHH", ipcprobe.HDR_VER_RX, ipcprobe.HDR_ONE, ipcprobe.CMD_ANNOUNCE)
        struct.pack_into("<I", pkt, ipcprobe.OFF_ANN_BUILD, 20231103)
        self.assertEqual(ipcprobe.parse_announce(bytes(pkt))["build"], "2023-11-03")
        struct.pack_into("<I", pkt, ipcprobe.OFF_ANN_BUILD, 0)
        self.assertEqual(ipcprobe.parse_announce(bytes(pkt))["build"], "")

    def test_rejects_other(self):
        self.assertIsNone(ipcprobe.parse_announce(ipcprobe.build_search_packet()))
        self.assertIsNone(ipcprobe.parse_announce(b"junk"))
        self.assertIsNone(ipcprobe.parse_set_ack(b"MHED" + b"\0" * 20))

    def test_mac_parsing(self):
        for s in ("02:00:00:00:00:01", "02-00-00-00-00-01", "020000000001", "02.00.00.00.00.01"):
            self.assertEqual(ipcprobe.parse_mac(s), MAC)
        for bad in ("02:00:00:00:00", "zz:00:00:00:00:01", "02:00:00:00:00:01:00"):
            with self.assertRaises(Exception):
                ipcprobe.parse_mac(bad)


if __name__ == "__main__":
    unittest.main(verbosity=2)
