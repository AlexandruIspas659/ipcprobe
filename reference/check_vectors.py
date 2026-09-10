#!/usr/bin/env python3
"""check_vectors.py — validate ../testdata/vectors.json against the reference implementation.

Run: python3 check_vectors.py
This is the cross-language contract: the Go port must pass the same vectors.json.
"""
import ipaddress
import json
import os
import sys

import ipcprobe

HERE = os.path.dirname(os.path.abspath(__file__))
VEC = os.path.join(HERE, "..", "testdata", "vectors.json")


def main():
    doc = json.load(open(VEC))
    ok = 0
    for v in doc["vectors"]:
        payload = bytes.fromhex(v["hex"])
        cmd = v["cmd"]
        if cmd == ipcprobe.CMD_ANNOUNCE:
            got = ipcprobe.parse_announce(payload)
            assert got == v["expect"], f"{v['name']}: {got} != {v['expect']}"
        elif cmd == ipcprobe.CMD_SET_ACK:
            ip, port = ipcprobe.parse_set_ack(payload)
            e = v["expect_ack"]
            assert (ip, port) == (e["requester_ip"], e["requester_port"]), v["name"]
        elif cmd == ipcprobe.CMD_SEARCH:
            assert payload == ipcprobe.build_search_packet(), v["name"]
        elif cmd == ipcprobe.CMD_SET_NET:
            d = v["decodes_to"]
            built = ipcprobe.build_set_packet(
                ipcprobe.parse_mac(d["mac"]),
                ipaddress.IPv4Address(d["ip"]), ipaddress.IPv4Address(d["mask"]),
                ipaddress.IPv4Address(d["gateway"]), ipaddress.IPv4Address(d["dns1"]),
                ipaddress.IPv4Address(d["dns2"]), d["password"])
            assert built == payload, f"{v['name']}: rebuilt packet != vector hex"
        else:
            raise SystemExit(f"unknown cmd {cmd} in vector {v['name']!r}")
        ok += 1
        print(f"  ok  {v['name']}")
    print(f"{ok}/{len(doc['vectors'])} vectors passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
