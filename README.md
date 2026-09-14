# ipcprobe

A small, cross-platform CLI to discover and reconfigure TVT-made IP cameras (sold under many OEM brands, e.g. DVC)
on a LAN — the job the Windows-only **IPTool** / **IPC Manager** utility does, with no macOS equivalent until now.
It finds cameras by listening for their multicast announcements and can set a camera's IP / netmask / gateway / DNS
by MAC address, **even when the camera sits on a different subnet and can't be reached by IP** — which is the usual
reason you need a tool like this.

> Not affiliated with, or endorsed by, TVT or any camera brand. "IPTool", "IPC Manager", "TVT" and "DVC" are the
> marks of their respective owners and are used here only to describe compatibility. This project is a clean-room
> implementation of the on-the-wire protocol, observed from network captures. See [PROTOCOL.md](PROTOCOL.md).

## Status

- **Protocol:** fully documented in [PROTOCOL.md](PROTOCOL.md), verified byte-for-byte against real captures.
- **Implementation:** Go, standard library only (no external modules, builds offline). Conformance is pinned by
  `testdata/vectors.json`; `go test` also runs an end-to-end exchange against a software camera over real multicast
  sockets. Verified on a live fleet of five TVT/DVC cameras and an NVR: discovery, a live IP change, and rescuing a
  camera on a foreign subnet. See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Install

### Homebrew (macOS)

```
brew install --cask AlexandruIspas659/tap/ipcprobe
```

The cask clears macOS's quarantine flag on install, so there is no Gatekeeper "unidentified developer" prompt.
Upgrading from the 0.1.x formula: `brew uninstall ipcprobe` first, then install the cask.

### Prebuilt binaries

Every [release](https://github.com/AlexandruIspas659/ipcprobe/releases) ships `ipcprobe` for macOS (universal:
Apple Silicon + Intel), Linux (amd64, arm64) and Windows (amd64). Download, unpack, put it on your `PATH`.

### From source

```
go build -o ipcprobe ./cmd/ipcprobe   # standard library only; no network needed
```

## Usage

```
ipcprobe list [--iface en0] [--timeout 5] [--wide] [--json]
ipcprobe show --mac 58:5b:69:00:00:01 [--iface en0] [--json]
ipcprobe set  --mac 58:5b:69:00:00:01 --ip 192.168.1.68 --mask 255.255.255.0 --gw 192.168.1.1 \
              [--dns1 ...] [--dns2 ...] [--iface en0] [--password ...] [--force]
```

- `list` — discover every camera on the segment; `--wide` adds HTTP/RTSP port and firmware build date, `--json`
  emits machine-readable output.
- `show` — every field for one camera, including its `http://` and `rtsp://` URLs.
- `set` — change a camera's network config. Prompts for the admin password (never echoed, never written to disk or
  logs). It refuses to fire at a MAC it can't currently see (`--force` overrides), then confirms by waiting for the
  camera's ack and its re-announcement at the new IP; exit code 2 if that doesn't happen (e.g. wrong password).

On macOS, the wired camera interface is usually `en0` for built-in Ethernet or `en5`/`en6` for a USB‑C adapter
(`ifconfig` to check). The first run triggers the system's Local Network permission prompt — allow it.

## Security note

The set-network command is unauthenticated multicast and the admin password crosses the LAN base64-encoded only
(encoding, not encryption). Anyone sniffing the camera VLAN can read admin passwords and reconfigure devices. This
is a property of the camera firmware, not of this tool; the mitigation is to segregate the camera VLAN. ipcprobe
never stores or logs passwords.

## Layout

```
PROTOCOL.md            the wire protocol (the real deliverable)
docs/ARCHITECTURE.md   how the code works, layer by layer
docs/RELEASING.md      how a tag becomes binaries, a GitHub release and a Homebrew entry
cmd/ipcprobe/          the CLI
cmd/fakecam/           a software camera for testing without hardware (dev tool, not released)
cmd/mhedpcap/          list / decode / diff MHED frames in a Wireshark capture (dev tool, not released)
internal/mhed/         the protocol: packet build/parse, no I/O
internal/discovery/    interfaces, multicast sockets, the receive loop (+ end-to-end test)
internal/fakecam/      the device side of the protocol, used by cmd/fakecam and the tests
testdata/vectors.json  sanitized conformance vectors — the contract any implementation must pass
.goreleaser.yaml       multi-platform release build
```

## Development

```
go test ./...                              # unit + vectors + end-to-end (needs a multicast-capable interface)
go run ./cmd/fakecam --iface en0           # software camera; then `ipcprobe list` / `set` in another terminal
go run ./cmd/mhedpcap list capture.pcapng  # inspect a Wireshark capture; `show`/`diff --frame N` for one frame
```

## License

MIT — see [LICENSE](LICENSE).
