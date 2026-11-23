// swift-tools-version:5.9
// The macOS app. It is a thin SwiftUI front end over the Go binary: every network operation is done by running the
// bundled `ipcprobe` helper with `--json` and parsing its output. See macos/README.md.
import PackageDescription

let package = Package(
    name: "IPCProbe",
    platforms: [.macOS(.v13)],
    targets: [
        .executableTarget(
            name: "IPCProbe",
            path: "Sources/IPCProbe"
        ),
        // Unit tests for the app's own logic (ordering, filtering, export formatting). Nothing here touches the
        // network: that is the helper's job and is covered by the Go tests.
        .testTarget(
            name: "IPCProbeTests",
            dependencies: ["IPCProbe"],
            path: "Tests/IPCProbeTests"
        )
    ]
)
