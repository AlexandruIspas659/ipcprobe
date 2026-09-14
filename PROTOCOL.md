# The MHED discovery/configuration protocol

Reverse-engineered from packet captures of TVT-made IP cameras (sold under many OEM brands, e.g. DVC). This is the
protocol behind the Windows "IPTool" / "IPC Manager" utility. It is decode-only documentation for interoperability;
no vendor code was disassembled.

Everything here is verified against real traffic. Bytes not listed are of unknown meaning and must be treated as
opaque (write zeros; ignore on read).

## Transport

| | |
|---|---|
| Protocol | UDP, port **23456** (both directions) |
| Magic | ASCII `MHED` = `4D 48 45 44` at offset 0 |
| Tool → devices | multicast **234.55.55.55:23456** (search, set-network) |
| Devices → tool | multicast **234.55.55.56:23456** (announcements, acks); device source port 9407 or 23456 |
| Multi-byte ints | little-endian unless noted |
| Auth / session | none. The admin password rides inside the set-network packet, base64-encoded (not encrypted) |

All four packets begin with an 8-byte header, then a uint16 command at offset 8:

```
0  "MHED"
4  uint16  version word   (0x0009 tool→device, 0x0008 device→tool)
6  uint16  0x0001         (constant)
8  uint16  command        (1 search, 2 announce, 3 set-network, 0x10 set-ack)
```

## cmd 1 — search (tool → 234.55.55.55), 140 bytes

Header with version `0x0009`, command `1`, then 130 zero bytes. Devices reply with cmd 2. A device also
re-announces on its own every ~2 s, so listening on 234.55.55.56 without probing still enumerates the segment.

## cmd 2 — announce (device → 234.55.55.56), 240 bytes

Version `0x0008`, command `2`. Fields (offset, length, meaning):

| Off | Len | Type | Field | Notes |
|----:|----:|------|-------|-------|
| 12  | 20  | cstr | device name | user-set label; default `DVC`. **Not** the vendor. |
| 32  | 6   | raw  | MAC | the stable identity; the IP changes, this does not |
| 40  | 4   | ipv4 | IP address | network byte order |
| 44  | 4   | ipv4 | netmask | |
| 48  | 4   | ipv4 | gateway | |
| 56  | 4   | u32  | firmware build date | decimal `YYYYMMDD`, e.g. `20231103`; 0/invalid = absent |
| 60  | 2   | u16  | HTTP port | 80 on cameras seen |
| 62  | 2   | u16  | RTSP port | 554 on cameras; 0 on the NVR |
| 112 | 4   | ipv4 | DNS1 | |
| 116 | 4   | ipv4 | DNS2 | |
| 140 | 16  | cstr | serial / device id | e.g. `IXXXXXXXXXXX` |
| 156 | 16  | cstr | firmware version | e.g. `1.5-1428327` |
| 196 | 16  | cstr | model | e.g. `DCN-BM8127AIN` |
| 212 | 16  | cstr | vendor | e.g. `DVC` |

`cstr` = NUL-padded ASCII, read up to the first NUL. Match devices by MAC (offset 32), never by IP.

## cmd 3 — set-network (tool → 234.55.55.55), 140 bytes

Version `0x0009`, command `3`. One fire-and-forget datagram; the device applies it if the password is right and
re-announces at the new address. Fields:

| Off | Len | Type | Field | Notes |
|----:|----:|------|-------|-------|
| 32  | 6   | raw  | target MAC | which device this is for |
| 40  | 4   | ipv4 | new IP | |
| 44  | 4   | ipv4 | new netmask | |
| 48  | 4   | ipv4 | new gateway | |
| 84  | 28  | b64  | admin password | base64 of the plaintext password, NUL-padded to 28 bytes (so ≤ 21 plaintext bytes) |
| 112 | 4   | ipv4 | DNS1 | |
| 116 | 4   | ipv4 | DNS2 | |

All other bytes are zero. The packet carries no HTTP/RTSP port and no device-name field — IPTool changes those over
the camera's own web UI, not through this protocol. A camera on a *foreign* subnet (unreachable by IP) still
receives this, because it is L2 multicast; that is the whole point of the tool.

## cmd 0x10 — set-network ack (device → 234.55.55.56), 140 bytes

Version `0x0008`, command `0x10`. Sent by the target device immediately after a cmd 3 it received, **from its old
IP**. It confirms **receipt only** — it is sent even when the password was wrong. It echoes the requester:

| Off | Len | Type | Field |
|----:|----:|------|-------|
| 12  | 4   | ipv4 | requester IP (the tool's source address) |
| 16  | 2   | u16  | requester UDP source port |

So a successful set = ack **and** a subsequent announcement at the new IP. Ack without the new announcement =
command received but not applied → almost always a wrong admin password.

## Observed behaviour / footguns

- macOS pops a Local Network permission prompt on the first multicast send; a GUI app needs
  `NSLocalNetworkUsageDescription` in its Info.plist or the prompt never appears and sends fail silently.
- Bind the send socket to a specific interface (`IP_MULTICAST_IF`) — Macs have several. Join 234.55.55.56 with
  `IP_ADD_MEMBERSHIP` per interface, `SO_REUSEPORT` before `bind(("", 23456))`.
- IPTool sends with IP TTL 5 from an ephemeral port; devices announce with TTL 1 from port 9407 (the NVR uses 23456,
  TTL 5). TTL ≥ 1 is fine since everything is on-segment.

## Test vectors

`testdata/vectors.json` holds sanitized, language-neutral vectors (synthetic MACs/serials/names, no passwords, all
non-documented bytes zeroed). Any implementation must reproduce each `expect` from the hex and rebuild the
set-network vector from `decodes_to`. `go test ./internal/mhed/` is the runnable check.
