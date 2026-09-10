#!/usr/bin/env python3
"""
ipcprobe — native macOS/Linux replacement for the Windows "IPTool" (TVT IPC Manager)
utility used with DVC / TVT OEM IP cameras.

Standard library only (Python 3.9+).

    ipcprobe list [--iface <name|ip>] [--timeout 5] [--json]
    ipcprobe set  --mac 58:5b:69:00:00:01 --ip 192.168.1.68 --mask 255.255.255.0 --gw 192.168.1.1
                   [--dns1 192.168.1.1] [--dns2 8.8.8.8] [--iface <name|ip>] [--password ...] [--force]

Protocol (decoded from a real capture, see IPTOOL_HANDOFF.md; layout verified byte-for-byte against
capture.pcapng frame 2539 on 2026-09-09):
  UDP port 23456, magic "MHED", uint16 fields little-endian.
  tool  -> cameras : multicast 234.55.55.55:23456   (cmd 1 = search, cmd 3 = set network, 140 bytes)
  cameras -> tool  : multicast 234.55.55.56:23456   (cmd 2 = announce, 240 bytes;
                                                     cmd 0x10 = set-network ack, 140 bytes, echoes the
                                                     requester's IP + port; sent even for a wrong password)
  No session: set-network is one datagram; the camera acks receipt and, if the admin password was right,
  re-announces at the new address. A wrong password gives the ack and nothing else.
"""

import argparse
import base64
import getpass
import ipaddress
import json
import platform
import re
import socket
import struct
import subprocess
import sys
import time

# --------------------------------------------------------------------------- protocol constants

MAGIC = b"MHED"
PORT = 23456
GROUP_TX = "234.55.55.55"   # tool -> cameras
GROUP_RX = "234.55.55.56"   # cameras -> tool
MCAST_TTL = 5               # what IPTool uses (capture frame 2539); cameras are on the same L2 segment anyway

CMD_SEARCH = 0x0001
CMD_ANNOUNCE = 0x0002
CMD_SET_NET = 0x0003
CMD_SET_ACK = 0x0010        # camera -> 234.55.55.56 right after a cmd 3 it accepted

HDR_VER_TX = 0x0009         # header word at offset 4 in tool->camera packets
HDR_VER_RX = 0x0008         # header word at offset 4 in announcements
HDR_ONE = 0x0001            # word at offset 6, always 1

PKT_LEN_TX = 140
PKT_LEN_ANNOUNCE = 240

# set-network (cmd 3) field offsets — §3 of the handoff, confirmed against capture frame 2539
OFF_SET_MAC = 32
OFF_SET_IP = 40
OFF_SET_MASK = 44
OFF_SET_GW = 48
OFF_SET_PASSWORD = 84
LEN_SET_PASSWORD = 28
OFF_SET_DNS1 = 112
OFF_SET_DNS2 = 116

# announce (cmd 2) field offsets — §4 of the handoff, confirmed against the capture (all 6 devices).
# NB: the raw hex sample printed in handoff §4 has 10 spurious zero bytes at offsets 16..25; the real frame is
# 240 bytes and matches these offsets. Offset 12 is the user-set device NAME (a user label, or the
# default "DVC"), not the vendor; the vendor string is the one at 212.
OFF_ANN_NAME = 12
LEN_ANN_NAME = 20
OFF_ANN_MAC = 32
OFF_ANN_IP = 40
OFF_ANN_MASK = 44
OFF_ANN_GW = 48
OFF_ANN_BUILD = 56          # firmware build date as a uint32 LE decimal YYYYMMDD (e.g. 20231103); 0 = absent
OFF_ANN_HTTP = 60           # HTTP port, uint16 LE (80 on the cameras seen)
OFF_ANN_RTSP = 62           # RTSP port, uint16 LE (554 on the cameras; 0 on the NVR)
OFF_ANN_DNS1 = 112
OFF_ANN_DNS2 = 116
OFF_ANN_SERIAL = 140
OFF_ANN_FIRMWARE = 156
OFF_ANN_MODEL = 196
OFF_ANN_VENDOR = 212
LEN_ANN_VENDOR = 16

# set-ack (cmd 0x10) — echoes the requester's IPv4 (offset 12) and UDP source port (offset 16, LE)
OFF_ACK_IP = 12
OFF_ACK_PORT = 16


# --------------------------------------------------------------------------- packet building / parsing

def _header(cmd: int) -> bytes:
    return MAGIC + struct.pack("<HHH", HDR_VER_TX, HDR_ONE, cmd)


def build_search_packet() -> bytes:
    """cmd 1 — 140 bytes: MHED 09 00 01 00 01 00 + zeros."""
    pkt = bytearray(PKT_LEN_TX)
    pkt[0:10] = _header(CMD_SEARCH)
    return bytes(pkt)


def encode_password(password: str) -> bytes:
    """base64 of the plain admin password, NUL-padded to 28 bytes (this is encoding, not encryption)."""
    b64 = base64.b64encode(password.encode("utf-8"))
    if len(b64) > LEN_SET_PASSWORD:
        raise ValueError(
            f"password too long: base64 form is {len(b64)} bytes, the packet field holds {LEN_SET_PASSWORD} "
            f"(max {LEN_SET_PASSWORD // 4 * 3} raw bytes)"
        )
    return b64.ljust(LEN_SET_PASSWORD, b"\0")


def build_set_packet(mac: bytes, ip: ipaddress.IPv4Address, mask: ipaddress.IPv4Address,
                     gw: ipaddress.IPv4Address, dns1: ipaddress.IPv4Address, dns2: ipaddress.IPv4Address,
                     password: str) -> bytes:
    """cmd 3 — 140 bytes, layout per §3 of the handoff."""
    if len(mac) != 6:
        raise ValueError("MAC must be 6 bytes")
    pkt = bytearray(PKT_LEN_TX)
    pkt[0:10] = _header(CMD_SET_NET)
    pkt[OFF_SET_MAC:OFF_SET_MAC + 6] = mac
    pkt[OFF_SET_IP:OFF_SET_IP + 4] = ip.packed
    pkt[OFF_SET_MASK:OFF_SET_MASK + 4] = mask.packed
    pkt[OFF_SET_GW:OFF_SET_GW + 4] = gw.packed
    pkt[OFF_SET_PASSWORD:OFF_SET_PASSWORD + LEN_SET_PASSWORD] = encode_password(password)
    pkt[OFF_SET_DNS1:OFF_SET_DNS1 + 4] = dns1.packed
    pkt[OFF_SET_DNS2:OFF_SET_DNS2 + 4] = dns2.packed
    assert len(pkt) == PKT_LEN_TX
    return bytes(pkt)


def _cstr(buf: bytes, off: int, length: int) -> str:
    return buf[off:off + length].split(b"\0", 1)[0].decode("ascii", "replace")


def _ip4(buf: bytes, off: int) -> str:
    return str(ipaddress.IPv4Address(buf[off:off + 4]))


def packet_cmd(payload: bytes):
    """Command word of an MHED datagram, or None if it isn't one."""
    if len(payload) < 10 or payload[0:4] != MAGIC:
        return None
    return struct.unpack_from("<H", payload, 8)[0]


def _build_date(payload: bytes) -> str:
    """Firmware build date at OFF_ANN_BUILD, uint32 LE decimal YYYYMMDD -> 'YYYY-MM-DD', or '' if absent/invalid."""
    v = struct.unpack_from("<I", payload, OFF_ANN_BUILD)[0]
    y, m, d = v // 10000, (v // 100) % 100, v % 100
    if 2000 <= y <= 2099 and 1 <= m <= 12 and 1 <= d <= 31:
        return f"{y:04d}-{m:02d}-{d:02d}"
    return ""


def parse_announce(payload: bytes):
    """Parse a cmd-2 announcement. Returns a dict, or None if the datagram isn't one."""
    if packet_cmd(payload) != CMD_ANNOUNCE or len(payload) < PKT_LEN_ANNOUNCE:
        return None
    return {
        "mac": format_mac(payload[OFF_ANN_MAC:OFF_ANN_MAC + 6]),
        "ip": _ip4(payload, OFF_ANN_IP),
        "mask": _ip4(payload, OFF_ANN_MASK),
        "gateway": _ip4(payload, OFF_ANN_GW),
        "dns1": _ip4(payload, OFF_ANN_DNS1),
        "dns2": _ip4(payload, OFF_ANN_DNS2),
        "name": _cstr(payload, OFF_ANN_NAME, LEN_ANN_NAME),
        "serial": _cstr(payload, OFF_ANN_SERIAL, 16),
        "firmware": _cstr(payload, OFF_ANN_FIRMWARE, 16),
        "model": _cstr(payload, OFF_ANN_MODEL, 16),
        "vendor": _cstr(payload, OFF_ANN_VENDOR, LEN_ANN_VENDOR),
        "http": struct.unpack_from("<H", payload, OFF_ANN_HTTP)[0],
        "rtsp": struct.unpack_from("<H", payload, OFF_ANN_RTSP)[0],
        "build": _build_date(payload),
    }


def parse_set_ack(payload: bytes):
    """Parse a cmd-0x10 set-network ack. Returns (requester_ip, requester_port) or None."""
    if packet_cmd(payload) != CMD_SET_ACK or len(payload) < OFF_ACK_PORT + 2:
        return None
    return _ip4(payload, OFF_ACK_IP), struct.unpack_from("<H", payload, OFF_ACK_PORT)[0]


def parse_mac(text: str) -> bytes:
    hexstr = re.sub(r"[^0-9a-fA-F]", "", text)
    if len(hexstr) != 12 or not re.fullmatch(r"([0-9a-fA-F]{2}[:\-. ]?){5}[0-9a-fA-F]{2}", text.strip()):
        raise argparse.ArgumentTypeError(f"invalid MAC address: {text!r}")
    return bytes.fromhex(hexstr)


def format_mac(mac: bytes) -> str:
    return ":".join(f"{b:02x}" for b in mac)


# --------------------------------------------------------------------------- interfaces (macOS + Linux)

def _run(cmd):
    try:
        return subprocess.run(cmd, capture_output=True, text=True, timeout=5).stdout
    except (OSError, subprocess.SubprocessError):
        return ""


def list_ipv4_interfaces():
    """Return [(name, IPv4Interface)] for every non-loopback interface with an IPv4 address."""
    found = []
    if platform.system() == "Linux":
        out = _run(["ip", "-o", "-4", "addr", "show"])
        for line in out.splitlines():
            m = re.match(r"\s*\d+:\s+(\S+)\s+inet\s+(\d+\.\d+\.\d+\.\d+/\d+)", line)
            if m and m.group(1) != "lo":
                found.append((m.group(1), ipaddress.IPv4Interface(m.group(2))))
        if found:
            return found
    # macOS / BSD ifconfig (also net-tools ifconfig on Linux)
    out = _run(["ifconfig"])
    name = None
    for line in out.splitlines():
        m = re.match(r"^([A-Za-z0-9._-]+):?\s+flags=", line)
        if m:
            name = m.group(1)
            continue
        m = re.match(r"\s+inet\s+(\d+\.\d+\.\d+\.\d+)\s+netmask\s+(\S+)", line)
        if m and name and not name.startswith("lo"):
            addr, mask = m.group(1), m.group(2)
            if mask.startswith("0x"):
                mask = str(ipaddress.IPv4Address(int(mask, 16)))
            found.append((name, ipaddress.IPv4Interface(f"{addr}/{mask}")))
    if found:
        return found
    # last resort: whatever the kernel would route a multicast packet through
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    try:
        s.connect((GROUP_TX, PORT))
        return [("default", ipaddress.IPv4Interface(s.getsockname()[0] + "/32"))]
    finally:
        s.close()


def choose_interfaces(iface_arg, prefer_ips=()):
    """
    Interfaces to use, as [(name, IPv4Interface)].
    --iface <name|ip> restricts to that interface. Otherwise prefer interfaces whose subnet contains one of
    prefer_ips (the target's new IP / current IP); if none match, use every IPv4 interface.
    """
    all_ifaces = list_ipv4_interfaces()
    if not all_ifaces:
        raise SystemExit("error: no IPv4 interface found")
    if iface_arg:
        try:
            want_ip = ipaddress.IPv4Address(iface_arg)
            sel = [(n, i) for n, i in all_ifaces if i.ip == want_ip]
            if not sel:
                sel = [("iface", ipaddress.IPv4Interface(f"{want_ip}/32"))]
        except ipaddress.AddressValueError:
            sel = [(n, i) for n, i in all_ifaces if n == iface_arg]
            if not sel:
                names = ", ".join(n for n, _ in all_ifaces)
                raise SystemExit(f"error: interface {iface_arg!r} has no IPv4 address (have: {names})")
        return sel
    for ip in prefer_ips:
        sel = [(n, i) for n, i in all_ifaces if ipaddress.IPv4Address(ip) in i.network]
        if sel:
            return sel
    return all_ifaces


# --------------------------------------------------------------------------- socket / discovery

def open_socket(ifaces):
    """One UDP socket bound to *:23456, joined to 234.55.55.56 on each interface."""
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM, socket.IPPROTO_UDP)
    s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    if hasattr(socket, "SO_REUSEPORT"):
        s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEPORT, 1)
    s.bind(("", PORT))
    s.setsockopt(socket.IPPROTO_IP, socket.IP_MULTICAST_TTL, MCAST_TTL)
    s.setsockopt(socket.IPPROTO_IP, socket.IP_MULTICAST_LOOP, 0)
    for _name, iface in ifaces:
        mreq = socket.inet_aton(GROUP_RX) + socket.inet_aton(str(iface.ip))
        try:
            s.setsockopt(socket.IPPROTO_IP, socket.IP_ADD_MEMBERSHIP, mreq)
        except OSError as e:
            print(f"warning: cannot join {GROUP_RX} on {_name} ({iface.ip}): {e}", file=sys.stderr)
    return s


def send_multicast(sock, ifaces, payload):
    for _name, iface in ifaces:
        sock.setsockopt(socket.IPPROTO_IP, socket.IP_MULTICAST_IF, socket.inet_aton(str(iface.ip)))
        sock.sendto(payload, (GROUP_TX, PORT))


def discover(ifaces, timeout, sock=None, probe=True, acks=None):
    """
    Send a search probe, collect announcements for `timeout` seconds. Returns {mac: info}.
    If `acks` is a list, set-network acks (cmd 0x10) seen meanwhile are appended to it as (src_ip, requester).
    """
    own = sock is None
    if own:
        sock = open_socket(ifaces)
    try:
        if probe:
            send_multicast(sock, ifaces, build_search_packet())
        devices = {}
        deadline = time.monotonic() + timeout
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                break
            sock.settimeout(remaining)
            try:
                data, (src_ip, src_port) = sock.recvfrom(2048)
            except socket.timeout:
                break
            info = parse_announce(data)
            if info:
                info["src"] = src_ip
                devices[info["mac"]] = info      # de-dup by MAC, newest wins
            elif acks is not None:
                ack = parse_set_ack(data)
                if ack:
                    acks.append((src_ip, ack))
        return devices
    finally:
        if own:
            sock.close()


# --------------------------------------------------------------------------- commands

BASE_COLS = [("MAC", "mac"), ("IP", "ip"), ("Mask", "mask"), ("Gateway", "gateway"),
             ("Model", "model"), ("Firmware", "firmware"), ("Name", "name")]
WIDE_COLS = BASE_COLS + [("HTTP", "http"), ("RTSP", "rtsp"), ("Build", "build")]


def print_table(devices, wide=False):
    cols = WIDE_COLS if wide else BASE_COLS
    rows = [[str(d.get(k, "")) for _, k in cols]
            for d in sorted(devices.values(), key=lambda d: ipaddress.IPv4Address(d["ip"]))]
    widths = [max(len(h), *(len(r[i]) for r in rows)) if rows else len(h) for i, (h, _) in enumerate(cols)]
    print("  ".join(h.ljust(w) for (h, _), w in zip(cols, widths)))
    print("  ".join("-" * w for w in widths))
    for r in rows:
        print("  ".join(c.ljust(w) for c, w in zip(r, widths)))


def cmd_list(args):
    ifaces = choose_interfaces(args.iface)
    if not args.json:
        print(f"listening on {', '.join(f'{n} ({i.ip})' for n, i in ifaces)} for {args.timeout:g}s ...",
              file=sys.stderr)
    devices = discover(ifaces, args.timeout)
    if args.json:
        print(json.dumps(sorted(devices.values(), key=lambda d: d["mac"]), indent=2))
    else:
        print_table(devices, wide=args.wide)
        print(f"{len(devices)} device(s)", file=sys.stderr)
    return 0


def cmd_show(args):
    """Discover and print every field for one camera by MAC (a support-friendly detail view)."""
    mac_text = format_mac(args.mac)
    ifaces = choose_interfaces(args.iface)
    if not args.json:
        print(f"listening on {', '.join(f'{n} ({i.ip})' for n, i in ifaces)} for {args.timeout:g}s ...",
              file=sys.stderr)
    dev = discover(ifaces, args.timeout).get(mac_text)
    if not dev:
        if args.json:
            print("null")
        raise SystemExit(f"error: {mac_text} not seen in discovery")
    if args.json:
        print(json.dumps(dev, indent=2))
        return 0
    order = ["mac", "ip", "mask", "gateway", "dns1", "dns2", "name", "model", "firmware",
             "build", "serial", "vendor", "http", "rtsp", "src"]
    w = max(len(k) for k in order)
    for k in order:
        if k in dev:
            print(f"{k.rjust(w)} : {dev[k]}")
    for proto, port in (("http", dev.get("http")), ("rtsp", dev.get("rtsp"))):
        if port:
            scheme = "http" if proto == "http" else "rtsp"
            suffix = "" if port in (80, 554) else f":{port}"
            print(f"{('web' if proto=='http' else 'rtsp').rjust(w)} : {scheme}://{dev['ip']}{suffix}/")
    return 0


def cmd_set(args):
    mac_text = format_mac(args.mac)
    net = ipaddress.IPv4Network(f"{args.ip}/{args.mask}", strict=False)
    if net.prefixlen < 31 and args.ip in (net.network_address, net.broadcast_address):
        raise SystemExit(f"error: {args.ip} is the network or broadcast address of {net}")
    if args.gw not in net:
        print(f"warning: gateway {args.gw} is outside {net}", file=sys.stderr)

    password = args.password if args.password is not None else getpass.getpass("camera admin password: ")
    try:
        encode_password(password)
    except ValueError as e:
        raise SystemExit(f"error: {e}")

    # fresh discovery: refuse to fire at a MAC we can't see (unless --force)
    ifaces = choose_interfaces(args.iface, prefer_ips=[str(args.ip)])
    sock = open_socket(ifaces)
    try:
        print(f"discovering on {', '.join(f'{n} ({i.ip})' for n, i in ifaces)} ...", file=sys.stderr)
        before = discover(ifaces, args.timeout, sock=sock)
        cur = before.get(mac_text)
        if cur:
            print(f"found {mac_text}: {cur['model']} \"{cur['name']}\" fw {cur['firmware']} at "
                  f"{cur['ip']}/{cur['mask']} gw {cur['gateway']}", file=sys.stderr)
            if not args.iface:
                # prefer the interface whose subnet contains the new IP, else the camera's current IP, else all
                ifaces = choose_interfaces(None, prefer_ips=[str(args.ip), cur["ip"]])
        elif not args.force:
            raise SystemExit(f"error: {mac_text} not seen in discovery ({len(before)} other device(s) seen); "
                             f"use --force to send anyway")
        else:
            print(f"warning: {mac_text} not seen in discovery, sending anyway (--force)", file=sys.stderr)

        pkt = build_set_packet(args.mac, args.ip, args.mask, args.gw, args.dns1, args.dns2, password)
        if args.dump:   # password field masked — it is only base64, never let it into a terminal log
            p0, p1 = OFF_SET_PASSWORD, OFF_SET_PASSWORD + LEN_SET_PASSWORD
            print(pkt[:p0].hex() + "*" * (2 * LEN_SET_PASSWORD) + pkt[p1:].hex(), file=sys.stderr)
        send_multicast(sock, ifaces, pkt)
        print(f"sent set-network for {mac_text} -> {args.ip}/{args.mask} gw {args.gw} "
              f"dns {args.dns1},{args.dns2} via {', '.join(n for n, _ in ifaces)}", file=sys.stderr)

        # confirm: the camera acks (cmd 0x10, from its old IP) and then re-announces at the new IP
        deadline = time.monotonic() + args.confirm_timeout
        acked = False
        while time.monotonic() < deadline:
            acks = []
            seen = discover(ifaces, min(2.0, max(0.1, deadline - time.monotonic())), sock=sock, probe=True,
                            acks=acks)
            if acks and not acked:
                acked = True
                print(f"camera at {acks[0][0]} received the command (the ack does not validate the password)",
                      file=sys.stderr)
            dev = seen.get(mac_text)
            if dev and dev["ip"] == str(args.ip):
                print(f"confirmed: {mac_text} now announces at {dev['ip']}/{dev['mask']} gw {dev['gateway']}")
                return 0
        last = discover(ifaces, 0.5, sock=sock, probe=False).get(mac_text) or cur
        where = f"still at {last['ip']}" if last else "not announcing at all"
        if acked:
            hint = " — the camera received the command but did not apply it: most likely a wrong admin password"
        else:
            hint = " — no ack from the camera: command probably never reached it (interface / VLAN?)"
        print(f"NOT confirmed: {mac_text} {where} after {args.confirm_timeout:g}s{hint}", file=sys.stderr)
        return 2
    finally:
        sock.close()


# --------------------------------------------------------------------------- CLI

def ip_arg(text):
    try:
        return ipaddress.IPv4Address(text)
    except ipaddress.AddressValueError:
        raise argparse.ArgumentTypeError(f"invalid IPv4 address: {text!r}")


def mask_arg(text):
    ip = ip_arg(text)
    try:
        ipaddress.IPv4Network(f"0.0.0.0/{text}")
    except ValueError:
        raise argparse.ArgumentTypeError(f"invalid netmask: {text!r}")
    return ip


def main(argv=None):
    p = argparse.ArgumentParser(description="Discover DVC/TVT cameras and set their network config (MHED/UDP 23456).")
    sub = p.add_subparsers(dest="command", required=True)

    pl = sub.add_parser("list", help="discover cameras on the LAN")
    pl.add_argument("--iface", help="interface name (e.g. en5) or its IPv4 address; default: all IPv4 interfaces")
    pl.add_argument("--timeout", type=float, default=5.0, help="seconds to collect announcements (default 5)")
    pl.add_argument("--json", action="store_true", help="print JSON instead of a table")
    pl.add_argument("--wide", action="store_true", help="add HTTP/RTSP port and firmware build-date columns")
    pl.set_defaults(func=cmd_list)

    psh = sub.add_parser("show", help="print every field for one camera by MAC")
    psh.add_argument("--mac", required=True, type=parse_mac, help="target camera MAC, e.g. 58:5b:69:00:00:01")
    psh.add_argument("--iface", help="interface name (e.g. en5) or its IPv4 address; default: all IPv4 interfaces")
    psh.add_argument("--timeout", type=float, default=5.0, help="seconds to collect announcements (default 5)")
    psh.add_argument("--json", action="store_true", help="print JSON instead of a field list")
    psh.set_defaults(func=cmd_show)

    ps = sub.add_parser("set", help="set a camera's IP/mask/gateway/DNS by MAC")
    ps.add_argument("--mac", required=True, type=parse_mac, help="target camera MAC, e.g. 58:5b:69:00:00:01")
    ps.add_argument("--ip", required=True, type=ip_arg, help="new IPv4 address")
    ps.add_argument("--mask", required=True, type=mask_arg, help="netmask, e.g. 255.255.255.0")
    ps.add_argument("--gw", required=True, type=ip_arg, help="gateway")
    ps.add_argument("--dns1", type=ip_arg, default=None, help="DNS1 (default: gateway)")
    ps.add_argument("--dns2", type=ip_arg, default=ipaddress.IPv4Address("8.8.8.8"), help="DNS2 (default 8.8.8.8)")
    ps.add_argument("--iface", help="interface name (e.g. en5) or its IPv4 address; default: auto")
    ps.add_argument("--password", default=None, help="camera admin password (prompted if omitted)")
    ps.add_argument("--force", action="store_true", help="send even if the MAC was not seen in discovery")
    ps.add_argument("--timeout", type=float, default=3.0, help="pre-send discovery time in seconds (default 3)")
    ps.add_argument("--confirm-timeout", type=float, default=8.0,
                    help="seconds to wait for the camera to announce at the new IP (default 8)")
    ps.add_argument("--dump", action="store_true",
                    help="print the packet as hex on stderr before sending (password field masked)")
    ps.set_defaults(func=cmd_set)

    args = p.parse_args(argv)
    if args.command == "set" and args.dns1 is None:
        args.dns1 = args.gw
    try:
        return args.func(args)
    except KeyboardInterrupt:
        return 130
    except PermissionError as e:
        raise SystemExit(f"error: {e} (on macOS, allow Local Network access for Terminal/Python in "
                         f"System Settings > Privacy & Security > Local Network)")


if __name__ == "__main__":
    sys.exit(main())
