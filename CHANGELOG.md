# Changelog

All notable changes to ipcprobe. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [0.1.0] — 2026-09-10

First public release.

### What it is

A cross-platform command-line replacement for the Windows-only **IPTool / IPC Manager** utility that ships with
TVT-made IP cameras (sold under many OEM brands, e.g. DVC). It discovers cameras on the LAN and can set a camera's
IP / netmask / gateway / DNS by MAC address — **including cameras sitting on a foreign subnet that you cannot reach
by IP**, which is the situation this tool exists for.

### Added

- `ipcprobe list` — discover every device on the segment (MAC, IP, mask, gateway, model, firmware, name);
  `--wide` adds HTTP/RTSP port and firmware build date; `--json` for scripting.
- `ipcprobe show --mac …` — every decoded field for one device, with its `http://` and `rtsp://` URLs.
- `ipcprobe set --mac … --ip … --mask … --gw …` — reconfigure a camera. Refuses to fire at a MAC it can't see
  (`--force` overrides), prompts for the admin password without echo, then confirms by waiting for the camera's
  ack **and** its re-announcement at the new address. Exit 0 = confirmed, 2 = not confirmed.
- `PROTOCOL.md` — full documentation of the MHED wire protocol (UDP 23456, multicast 234.55.55.55/.56), decoded
  from packet captures: search, announce, set-network and the set-ack that IPTool's own docs never mention.
- `testdata/vectors.json` — sanitized, language-neutral conformance vectors; the Go binary and the Python
  reference implementation in `reference/` both pass them.
- Standard-library only Go implementation: builds offline, single static binary, no runtime.

### Verified

Tested on a live fleet of five TVT/DVC cameras and one DRN-16162R NVR from macOS: discovery, a live IP change and
back, and a camera moved to 10.0.0.x and recovered while the host stayed on 192.168.1.x.

### Known limitations

- Only the fields the protocol carries can be changed: IP, mask, gateway, DNS1/2. HTTP/RTSP ports and the device
  name are read-only here (IPTool changes those through the camera's web UI, not this protocol).
- The camera's set-ack confirms receipt, not success; a wrong password looks like "received but never re-announced".
- macOS: the first multicast send triggers the Local Network permission prompt for your terminal — allow it.
- Security: the protocol itself is unauthenticated multicast and carries the admin password base64-encoded. Anyone
  sniffing the camera VLAN can read it. This is a property of the camera firmware, not of this tool — segregate
  the camera VLAN. ipcprobe never stores or logs passwords.

[0.1.0]: https://github.com/AlexandruIspas659/ipcprobe/releases/tag/v0.1.0
