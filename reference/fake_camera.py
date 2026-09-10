#!/usr/bin/env python3
"""fake_camera.py — loopback test double for ipcprobe (no real camera needed).

Announces a synthetic camera every 0.5 s on 234.55.55.56 and applies cmd-3 set-network packets addressed to its
MAC, replying with a cmd-0x10 ack like the real firmware does. Lets you exercise `list` / `show` / `set` end to end.

Run in one terminal:   python3 fake_camera.py
In another:            python3 ipcprobe.py list --wide
                       python3 ipcprobe.py set --mac 02:00:00:00:00:01 --ip 192.168.1.68 \
                               --mask 255.255.255.0 --gw 192.168.1.1 --password x

NB: ipcprobe.py sets IP_MULTICAST_LOOP=0, so its own probes/sets do not reach a listener on the same host. To test
`set` against this double on one machine, run a copy with that line flipped to 1. Announcements from this double
reach ipcprobe either way, so `list` / `show` work as-is.
"""
import os
import socket
import struct
import sys
import time
import threading

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import ipcprobe

MAC = bytes.fromhex("020000000001")

# a minimal synthetic 240-byte announcement (matches the vectors)
ann = bytearray(240)
ann[0:10] = ipcprobe.MAGIC + struct.pack("<HHH", ipcprobe.HDR_VER_RX, ipcprobe.HDR_ONE, ipcprobe.CMD_ANNOUNCE)
ann[12:32] = b"lobby".ljust(20, b"\0")
ann[32:38] = MAC
ann[40:44] = socket.inet_aton("192.168.1.69")
ann[44:48] = socket.inet_aton("255.255.255.0")
ann[48:52] = socket.inet_aton("192.168.1.1")
struct.pack_into("<I", ann, 56, 20231103)          # build date
struct.pack_into("<H", ann, 60, 80)                # http
struct.pack_into("<H", ann, 62, 554)               # rtsp
ann[112:116] = socket.inet_aton("192.168.1.1")     # dns1
ann[116:120] = socket.inet_aton("8.8.8.8")         # dns2
ann[140:156] = b"TESTSERIAL01".ljust(16, b"\0")
ann[156:172] = b"1.5-1428327".ljust(16, b"\0")
ann[196:212] = b"DCN-BM8127AIN".ljust(16, b"\0")
ann[212:228] = b"DVC".ljust(16, b"\0")
state = {"ann": ann}

rx = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
rx.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
if hasattr(socket, "SO_REUSEPORT"):
    rx.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEPORT, 1)
rx.bind(("", ipcprobe.PORT))
rx.setsockopt(socket.IPPROTO_IP, socket.IP_ADD_MEMBERSHIP,
              socket.inet_aton(ipcprobe.GROUP_TX) + socket.inet_aton("0.0.0.0"))
tx = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
tx.setsockopt(socket.IPPROTO_IP, socket.IP_MULTICAST_LOOP, 1)


def announcer():
    while True:
        tx.sendto(bytes(state["ann"]), (ipcprobe.GROUP_RX, ipcprobe.PORT))
        time.sleep(0.5)


threading.Thread(target=announcer, daemon=True).start()
print(f"fake camera {ipcprobe.format_mac(MAC)} announcing on {ipcprobe.GROUP_RX}:{ipcprobe.PORT} ...", flush=True)
while True:
    data, (sip, sport) = rx.recvfrom(2048)
    if ipcprobe.packet_cmd(data) != ipcprobe.CMD_SET_NET:
        continue
    if data[32:38] == MAC:
        print(f"fake camera: applying {socket.inet_ntoa(data[40:44])} "
              f"(password field {len(data[84:112].rstrip(chr(0).encode()))} b)", flush=True)
        ack = bytearray(240)
        ack[0:10] = ipcprobe.MAGIC + struct.pack("<HHH", ipcprobe.HDR_VER_RX, ipcprobe.HDR_ONE, ipcprobe.CMD_SET_ACK)
        ack[12:16] = socket.inet_aton(sip)
        struct.pack_into("<H", ack, 16, sport)
        tx.sendto(bytes(ack), (ipcprobe.GROUP_RX, ipcprobe.PORT))
        state["ann"][40:44] = data[40:44]
        state["ann"][44:48] = data[44:48]
        state["ann"][48:52] = data[48:52]
