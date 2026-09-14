# ipcprobe.app — the macOS front end

A small SwiftUI app around the same `ipcprobe` binary the CLI ships. It has **no network code of its own**: every
scan and every configuration change is the bundled helper (`Contents/MacOS/ipcprobe-cli`) run with `--json`, so the
app can never disagree with the CLI about the protocol, and the CLI's tests cover the app's behaviour on the wire.

```
Sources/IPCProbe/
  IPCProbeApp.swift        entry point, window, ⌘R
  ContentView.swift        toolbar (interface, listen time, rescan), device table, status bar
  DeviceDetail.swift       every field of the selected camera; open web UI / copy RTSP URL / change network
  SetNetworkSheet.swift    the change-network form; shows the helper's progress and verdict
  Model/Device.swift       Codable mirrors of the helper's JSON (mhed.Device, interfaces)
  Model/ProbeRunner.swift  runs the helper: finds it, pipes the password over stdin, streams stderr
  Model/AppModel.swift     app state: interfaces, devices, scan, set
Resources/Info.plist       bundle metadata; NSLocalNetworkUsageDescription is what macOS shows in its prompt
build-app.sh               assembles the .app (helper + Swift binary), ad-hoc signs it, wraps it in a .dmg
```

## How it talks to the helper

| App action | Helper command | Notes |
|---|---|---|
| interface picker | `ipcprobe interfaces --json` | same selection rules as `--iface` |
| Rescan | `ipcprobe list --json --timeout N [--iface IP]` | stdout = JSON array of devices |
| Change network | `ipcprobe set --mac … --ip … --mask … --gw … [--dns1 --dns2 --iface --force]` | **password on stdin**, never on the command line; stderr lines are shown live; exit 0 confirmed / 2 not confirmed / 1 setup error |

The helper reads the password silently when stdin is not a terminal (see `cmd/ipcprobe/password.go`), so it never
appears in `ps`, in logs, or on disk. The sheet clears its own copy the moment the helper has been started.

## Building

Requirements: Xcode (or the Command Line Tools — `swift build` and `codesign` are enough) and Go for the helper.

```
macos/build-app.sh                      # → macos/dist/ipcprobe.app and ipcprobe_<version>_macOS.dmg
macos/build-app.sh --helper ~/Downloads/ipcprobe   # bundle a prebuilt helper (what CI does with the release asset)
```

For development without assembling a bundle:

```
cd macos
IPCPROBE_BIN=$(which ipcprobe) swift run IPCProbe   # or `go build -o /tmp/ipcprobe ../cmd/ipcprobe` first
```

Outside a bundle the helper is located through `IPCPROBE_BIN`, then `PATH`. Inside the bundle it is named
`ipcprobe-cli` because the app's own executable is `IPCProbe`, and on the default (case-insensitive) APFS those two
names would collide.

## Local Network permission

macOS gates multicast behind the *Local Network* privacy permission. The prompt is attributed to the **app**, not to
the helper: a child process launched with `Process` is attributed to the app that launched it (the "responsible
process"), which is why the helper lives inside the bundle rather than being called from Homebrew's copy. On the
first scan macOS asks; if you deny, every scan will come back empty until you allow it in *System Settings ›
Privacy & Security › Local Network*. The empty-state text in the window says the same.

## Signing

The app is signed **ad hoc** (`codesign -s -`). That is enough for the system to give it a stable identity for the
permission above and to run it on the Mac that built it. A *downloaded* copy is quarantined and Gatekeeper reports
an unidentified developer: right-click › Open the first time, or `xattr -dr com.apple.quarantine ipcprobe.app`.
Proper Developer ID signing + notarization needs an Apple Developer membership; the build script is the only place
that would change (`--sign "Developer ID Application: …"` and a `notarytool submit` step).
