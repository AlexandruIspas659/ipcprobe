# Changelog

All notable changes to ipcprobe. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- **Batch changes.** `ipcprobe apply` changes several cameras at once with one admin password. Whatever is not
  given is kept from each camera's current configuration: `--start-ip` hands out consecutive addresses,
  `--mask` / `--gw` / `--dns1` / `--dns2` alone change those in place, and `--plan` takes an explicit JSON list
  (the shape of `list --json`). It previews the plan (`--dry-run`), refuses one that would put two devices on the
  same address, tries the first camera alone so a wrong password reaches one camera instead of all of them, and
  reports per camera whether the change was confirmed.
- macOS app: the device table is multi-select; several selected cameras open a batch sheet with a preview of
  every camera's old and new configuration before anything is sent. The sheet's Order menu and drag-to-reorder
  decide which camera gets which address. A new Cameras menu and two toolbar buttons give Select All (⌥⌘A) and
  Change Network (⌘E).
- **Rescue.** `list` now points out cameras that sit on another subnet than this computer (and cameras sharing an
  address), `list --json` carries a `foreign` flag, and `ipcprobe apply --rescue` brings such cameras into this
  computer's subnet in one go: each gets a free address there, with its mask and the gateway the cameras already
  on it use. `--all --rescue` takes every camera that needs it.
- `fakecam --count N [--same-ip]` runs several software cameras, for trying a batch without hardware.

### Fixed

- `set` could report "confirmed" for a change that had not happened. It only checked that the camera announced
  at the requested IP, so a change that kept the address (a new gateway or DNS server) was confirmed immediately,
  even with a wrong password. It now waits for the camera to announce the whole requested configuration.

- VPN tunnels are no longer offered as usable interfaces. macOS flags `utun` interfaces as multicast-capable, so
  they slipped through the filter and showed up in `ipcprobe interfaces`, in the app's interface picker, and in the
  "all interfaces" default. Point-to-point interfaces are now excluded outright; `--iface utunN` still explains why.

## [0.3.2] — 2026-09-22

macOS app only; the CLI is unchanged.

### Added

- The app has an icon.
- Sortable device table: click a column header to sort, again to reverse. IP and Gateway sort numerically (so
  `.9` comes before `.134`), and the chosen column survives a rescan.

### Fixed

- The change-network sheet no longer clips its fields; the defaults (gateway, `8.8.8.8`) show as placeholder text.
- The status bar shows the app's own version instead of the bundled helper's, which could look stale.
- The interface picker's toolbar highlight now hugs the control instead of a wider box.

## [0.3.1] — 2026-09-14

Release-tooling fix only; the CLI binaries are identical to 0.3.0.

### Fixed

- The `macos-app` release job failed before building anything: `build-app.sh` changes into `macos/` before using
  `--helper`, so the relative path the workflow passed no longer resolved. The script now resolves `--helper` to
  an absolute path first. The 0.3.0 release therefore has no `.dmg`; this one does.

## [0.3.0] — 2026-09-14

### Added

- **macOS app** (`macos/`): a SwiftUI front end — interface picker, device table, detail pane with "open web UI" /
  "copy RTSP URL", and a change-network sheet that shows the helper's progress and verdict. It bundles the same
  `ipcprobe` binary the CLI ships and drives it with `--json`; it has no network code of its own. Shipped as
  `ipcprobe_<version>_macOS.dmg` on each release, ad-hoc signed (not notarized — right-click › Open the first time).
  Verified on the live fleet: the Local Network permission is attributed to the app and flows to the bundled helper.
- `ipcprobe interfaces [--json]` — list the interfaces `--iface` would accept.

### Changed

- `set` reads the password silently when stdin is not a terminal (no prompt, no `stty`, no warning), so a wrapper
  can pipe it in without it ever appearing on a command line.

## [0.2.0] — 2026-09-14

### Added

- `cmd/fakecam` and `internal/fakecam`: a software camera that speaks the device side of the protocol, for
  testing without hardware. `go test` now includes an end-to-end discover → set → ack → re-announce exchange over
  real multicast sockets.
- `cmd/mhedpcap`: list, decode and diff MHED frames in a Wireshark capture (`.pcapng` or `.pcap`).
- `mhed.BuildAnnounce` / `mhed.BuildSetAck`, the inverses of the parsers; the announce vectors now round-trip
  byte for byte.
- `docs/ARCHITECTURE.md` and `docs/RELEASING.md`.

### Changed

- Homebrew now publishes a **cask** instead of a formula: `brew install --cask AlexandruIspas659/tap/ipcprobe`.
  The cask strips macOS's quarantine attribute at install time, so the unsigned binary runs from a terminal without a
  Gatekeeper prompt. Existing formula installs: `brew uninstall ipcprobe` then install the cask.
- `--iface` on a VPN / point-to-point interface now explains that the protocol is link-layer multicast and cannot
  cross a tunnel, instead of reporting "no IPv4 address".
- Default `--confirm-timeout` raised from 8 s to 15 s; the not-confirmed message no longer asserts a wrong password
  when the camera may simply be slow to re-announce.

### Removed

- The Python reference implementation (`reference/`). Its role — the thing the Go port was checked against — is
  over: `testdata/vectors.json` is the conformance contract and the Go tests enforce it. Its two useful tools
  (fake camera, capture diff) were ported to Go, above.

### Fixed

- `testdata/vectors.json`: the set-ack vector was 240 bytes; the real ack is 140.

## [0.1.1] — 2026-09-10

Release-tooling fix only; the binaries are identical to 0.1.0.

### Fixed

- The Homebrew formula is now published to `AlexandruIspas659/homebrew-tap` on release (the 0.1.0 workflow was
  missing the tap token, so `brew install AlexandruIspas659/tap/ipcprobe` did not work yet).
- Removed a stray archive that had been committed to the repository.

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

[0.3.1]: https://github.com/AlexandruIspas659/ipcprobe/releases/tag/v0.3.1
[0.3.0]: https://github.com/AlexandruIspas659/ipcprobe/releases/tag/v0.3.0
[0.2.0]: https://github.com/AlexandruIspas659/ipcprobe/releases/tag/v0.2.0
[0.1.1]: https://github.com/AlexandruIspas659/ipcprobe/releases/tag/v0.1.1
[0.1.0]: https://github.com/AlexandruIspas659/ipcprobe/releases/tag/v0.1.0
