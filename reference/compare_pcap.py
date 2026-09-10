#!/usr/bin/env python3
"""
compare_pcap.py — byte-for-byte check of the packet iptool.py generates against a frame in a pcapng capture.

    python3 compare_pcap.py capture.pcapng --frame 2539 [--password <pw>]

Reads the UDP payload of the given frame (1-based, Wireshark numbering), decodes the MAC / IP / mask / gw / DNS
from it with the layout that matches, rebuilds the packet with iptool.build_set_packet() using those values and
the given password (or a dummy one), and prints a diff of every byte that differs. Standard library only.
"""
import argparse
import ipaddress
import struct
import sys

import ipcprobe as iptool


def read_pcapng_frames(path):
    """Yield (frame_no, linktype, packet_bytes) for every EPB/SPB in a pcapng file. Handles both endiannesses."""
    with open(path, "rb") as f:
        data = f.read()
    pos, n, endian, linktypes = 0, 0, "<", []
    while pos + 8 <= len(data):
        btype, = struct.unpack_from(endian + "I", data, pos)
        if btype == 0x0A0D0D0A:                                     # Section Header Block: fix endianness
            bom = data[pos + 8:pos + 12]
            endian = "<" if bom == b"\x4d\x3c\x2b\x1a" else ">"
            linktypes = []
        blen, = struct.unpack_from(endian + "I", data, pos + 4)
        if blen < 12:
            raise ValueError(f"corrupt block at {pos}")
        body = data[pos + 8:pos + blen - 4]
        if btype == 0x00000001:                                     # Interface Description Block
            linktypes.append(struct.unpack_from(endian + "H", body, 0)[0])
        elif btype == 0x00000006:                                   # Enhanced Packet Block
            iface, _ts_hi, _ts_lo, caplen, _origlen = struct.unpack_from(endian + "IIIII", body, 0)
            n += 1
            yield n, linktypes[iface] if iface < len(linktypes) else 1, body[20:20 + caplen]
        elif btype == 0x00000003:                                   # Simple Packet Block
            origlen, = struct.unpack_from(endian + "I", body, 0)
            n += 1
            yield n, linktypes[0] if linktypes else 1, body[4:4 + origlen]
        pos += blen


def udp_payload(linktype, frame):
    """Return (src, dst, sport, dport, ttl, payload) for an IPv4/UDP frame, else None."""
    if linktype == 1:                      # Ethernet
        if len(frame) < 14:
            return None
        ethertype = struct.unpack_from("!H", frame, 12)[0]
        off = 14
        if ethertype == 0x8100:            # 802.1Q
            ethertype = struct.unpack_from("!H", frame, 16)[0]
            off = 18
        if ethertype != 0x0800:
            return None
    elif linktype == 101:                  # raw IP
        off = 0
    elif linktype == 0:                    # BSD loopback (macOS)
        off = 4
    else:
        return None
    ip = frame[off:]
    if len(ip) < 20 or ip[0] >> 4 != 4 or ip[9] != 17:
        return None
    ihl = (ip[0] & 0xF) * 4
    total = struct.unpack_from("!H", ip, 2)[0]
    ttl = ip[8]
    src, dst = str(ipaddress.IPv4Address(ip[12:16])), str(ipaddress.IPv4Address(ip[16:20]))
    udp = ip[ihl:total]
    sport, dport, ulen = struct.unpack_from("!HHH", udp, 0)
    return src, dst, sport, dport, ttl, udp[8:ulen]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("pcap")
    ap.add_argument("--frame", type=int, default=2539, help="Wireshark frame number (1-based)")
    ap.add_argument("--password", default=None, help="admin password used in the capture (else fields are masked)")
    ap.add_argument("--list", action="store_true", help="list every MHED frame in the capture and exit")
    args = ap.parse_args()

    target = None
    for no, lt, frame in read_pcapng_frames(args.pcap):
        r = udp_payload(lt, frame)
        if not r:
            continue
        src, dst, sport, dport, ttl, payload = r
        if args.list and payload[:4] == b"MHED":
            cmd = struct.unpack_from("<H", payload, 8)[0]
            print(f"frame {no:5d}  {src}:{sport} -> {dst}:{dport}  ttl {ttl}  cmd {cmd}  len {len(payload)}")
        if no == args.frame:
            target = (src, dst, sport, dport, ttl, payload)
    if args.list:
        return 0
    if target is None:
        sys.exit(f"frame {args.frame} not found or not IPv4/UDP")

    src, dst, sport, dport, ttl, cap = target
    print(f"frame {args.frame}: {src}:{sport} -> {dst}:{dport}  ip ttl {ttl}  udp payload {len(cap)} bytes")
    if cap[:4] != b"MHED":
        sys.exit("not an MHED packet")
    ver, one, cmd = struct.unpack_from("<HHH", cap, 4)
    print(f"header: ver 0x{ver:04x} one 0x{one:04x} cmd 0x{cmd:04x}")
    if cmd != iptool.CMD_SET_NET:
        sys.exit("not a set-network (cmd 3) packet")

    if len(cap) != iptool.PKT_LEN_TX:
        print(f"warning: payload is {len(cap)} bytes, expected {iptool.PKT_LEN_TX}")
    ip4 = lambda off: ipaddress.IPv4Address(cap[off:off + 4])
    mac = cap[iptool.OFF_SET_MAC:iptool.OFF_SET_MAC + 6]
    ip, mask, gw = ip4(iptool.OFF_SET_IP), ip4(iptool.OFF_SET_MASK), ip4(iptool.OFF_SET_GW)
    dns1, dns2 = ip4(iptool.OFF_SET_DNS1), ip4(iptool.OFF_SET_DNS2)
    pw_lo, pw_hi = iptool.OFF_SET_PASSWORD, iptool.OFF_SET_PASSWORD + iptool.LEN_SET_PASSWORD
    pw_stripped = cap[pw_lo:pw_hi].rstrip(b"\0")
    b64_like = bool(pw_stripped) and all(c in b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/="
                                         for c in pw_stripped)
    print(f"fields: mac {iptool.format_mac(mac)} ip {ip} mask {mask} gw {gw} dns {dns1},{dns2} "
          f"password field {len(pw_stripped)} chars, base64-looking: {b64_like}")

    ours = iptool.build_set_packet(mac, ip, mask, gw, dns1, dns2, args.password or "x")
    diffs = [i for i in range(max(len(cap), len(ours))) if (cap[i:i + 1] or None) != (ours[i:i + 1] or None)]
    pw_diffs = [i for i in diffs if pw_lo <= i < pw_hi]
    other = [i for i in diffs if not (pw_lo <= i < pw_hi)]
    print(f"{len(cap)} captured vs {len(ours)} generated: {len(diffs)} differing byte(s), "
          f"{len(pw_diffs)} inside the password field, {len(other)} elsewhere")
    for i in other:
        print(f"  offset {i:3d}: capture {cap[i:i+1].hex() or '--'}  ours {ours[i:i+1].hex() or '--'}")
    if args.password and pw_diffs:
        print("  password field differs even with --password given -> encoding assumption is wrong")
    if not diffs:
        verdict = "EXACT MATCH"
    elif not other and not args.password:
        verdict = "MATCH (only the password field differs)"
    else:
        verdict = "MISMATCH"
    print("RESULT:", verdict)
    return 0 if not other else 1


if __name__ == "__main__":
    sys.exit(main())
