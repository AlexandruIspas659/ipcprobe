# ipcprobe.app — the macOS front end

A small SwiftUI app around the same `ipcprobe` binary the CLI ships. It has **no network code of its own**: every
scan and every configuration change is the bundled helper (`Contents/MacOS/ipcprobe-cli`) run with `--json`, so the
app can never disagree with the CLI about the protocol, and the CLI's tests cover the app's behaviour on the wire.

```
Sources/IPCProbe/
  IPCProbeApp.swift        entry point, window, ⌘R
  ContentView.swift        toolbar (interface, live / listen time, select, change, rescan), rescue banner, filter, device table, status bar
  DeviceDetail.swift       every field of the selected camera; open web UI / copy RTSP URL / change network
  SetNetworkSheet.swift    the change-network form; shows the helper's progress and verdict
  BatchSheet.swift         the same for several selected cameras: preview, then apply
  Model/Device.swift       Codable mirrors of the helper's JSON (mhed.Device, interfaces)
  Model/ProbeRunner.swift  runs the helper: finds it, pipes the password over stdin, streams stderr; keeps `watch` running
  Model/AppModel.swift     app state: interfaces, devices, scan, set
Resources/Info.plist       bundle metadata; NSLocalNetworkUsageDescription is what macOS shows in its prompt
  Model/CameraLists.swift  list logic with no UI in it (ordering, filtering, shared addresses) — what the unit tests cover
  Model/Export.swift       the camera list and a batch's outcome as CSV / tab-separated text
Tests/IPCProbeTests/       unit tests for the app's own logic; nothing here touches the network
test.sh                    runs them (`swift test`, plus the flags the Command Line Tools need)
build-app.sh               assembles the .app (helper + Swift binary), ad-hoc signs it, wraps it in a .dmg
icon/                      the app icon: draw.py renders it, build_icns.py assembles AppIcon.icns
```

## How it talks to the helper

| App action | Helper command | Notes |
|---|---|---|
| interface picker | `ipcprobe interfaces --json` | same selection rules as `--iface` |
| Live list (the default) | `ipcprobe watch --json [--iface IP]`, kept running | one `{"devices":[…]}` line whenever the list changes; the table follows it. Rescan restarts it |
| Rescan, with Listen set to 2 / 5 / 10 s | `ipcprobe list --json --timeout N [--iface IP]` | stdout = JSON array of devices |
| Change network | `ipcprobe set --mac … --ip … --mask … --gw … [--dns1 --dns2 --iface --force]` | **password on stdin**, never on the command line; stderr lines are shown live; exit 0 confirmed / 2 not confirmed / 1 setup error |
| Batch: Preview | `ipcprobe apply --mac a,b,c --json --dry-run [--start-ip … --step … --mask … --gw … --dns1 … --dns2 …]` | stdout = `{dry_run, devices:[…]}`: each camera's current and target configuration, status, warnings; nothing is sent |
| Batch: Apply | the same flags with `--yes` instead of `--dry-run` (and `--no-canary` if the box is unticked) | **password on stdin**; stderr lines are shown live; stdout = the same JSON with each camera's outcome |
| Bring into This Network | `ipcprobe apply --mac a,b,c --rescue --json --dry-run [--gw … --dns1 … --dns2 …]`, then `--yes` | the helper picks a free address in this Mac's subnet for each stranded camera; same JSON, same sheet |

Cameras whose address is outside this Mac's subnet (`foreign` in `list --json`) carry an orange mark in the table,
and a banner above it offers to bring them in; cameras announcing the same address carry a mark too. With "All
interfaces" selected on a Mac that has several, the helper declines to guess which network is meant — pick the
cameras' interface in the toolbar first.

Select several rows (⌘-click, ⇧-click) to get the batch sheet. Cameras are passed in the order the table shows
them, and the sheet lets you change that: its Order menu sorts by MAC, name or current address or reverses the
list, and rows can be dragged. The order matters because consecutive addresses are handed out top to bottom and the
first camera is the one tried on its own. Apply is only offered for a preview of exactly what the form and the
order say; changing either means previewing again.

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
