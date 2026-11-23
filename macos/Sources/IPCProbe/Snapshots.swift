import AppKit
import SwiftUI

/// A development aid, not a feature: `IPCProbe --snapshots <directory>` renders the app's screens with made-up
/// cameras into PNG files and exits, without showing a window or touching the network. It exists so that a change
/// to a view can be looked at without clicking through the app — including by tools that cannot see the screen.
///
///     swift run IPCProbe --snapshots /tmp/shots
@MainActor
enum Snapshots {
    static func runIfAsked() {
        let args = CommandLine.arguments
        guard let i = args.firstIndex(of: "--snapshots"), i + 1 < args.count else { return }
        NSApplication.shared.setActivationPolicy(.prohibited)      // no Dock icon, no stolen focus
        let dir = URL(fileURLWithPath: args[i + 1], isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)

        let model = AppModel()
        model.devices = Fixtures.devices
        model.lastScan = Date()

        shoot("main", ContentView().environmentObject(model), size: CGSize(width: 900, height: 520), to: dir)
        model.selection = [Fixtures.devices[0].mac]
        shoot("main-one-selected", ContentView().environmentObject(model), size: CGSize(width: 900, height: 520), to: dir)
        model.selection = Set(Fixtures.devices.prefix(4).map(\.mac))
        shoot("main-several-selected", ContentView().environmentObject(model), size: CGSize(width: 900, height: 520), to: dir)
        model.selection = []
        model.filter = "lobby"
        shoot("main-filtered", ContentView().environmentObject(model), size: CGSize(width: 900, height: 520), to: dir)
        model.filter = ""

        let four = Array(Fixtures.devices.prefix(4))
        shoot("batch-empty", BatchSheet(devices: four).environmentObject(model), to: dir)
        shoot("batch-preview", BatchSheet(devices: four, snapshot: .init(items: Fixtures.plan(four), previewed: true))
            .environmentObject(model), to: dir)
        // The way it really happens: the list is on screen first, the plan arrives afterwards.
        shoot("batch-preview-late", BatchSheet(devices: four, snapshot: .init(items: Fixtures.plan(four), previewed: true, late: true))
            .environmentObject(model), to: dir)
        shoot("batch-stale", BatchSheet(devices: four, snapshot: .init(items: Fixtures.plan(four), previewed: false))
            .environmentObject(model), to: dir)
        shoot("batch-done", BatchSheet(devices: four, snapshot: .init(items: Fixtures.outcome(four), previewed: true, done: 2))
            .environmentObject(model), to: dir)
        shoot("batch-many", BatchSheet(devices: Fixtures.many, snapshot: .init(items: Fixtures.plan(Fixtures.many), previewed: true, moveRow: 2))
            .environmentObject(model), to: dir)
        shoot("rescue", BatchSheet(devices: Fixtures.devices.foreign, mode: .rescue).environmentObject(model), to: dir)
        shoot("set", SetNetworkSheet(device: Fixtures.devices[0]).environmentObject(model), to: dir)
        exit(0)
    }

    /// Renders a view in an off-screen window and writes it as PNG. `size` nil means the view's own fitting size.
    private static func shoot<V: View>(_ name: String, _ view: V, size: CGSize? = nil, to dir: URL) {
        // Off screen nothing paints the window's background, and white-on-nothing text would vanish.
        let host = NSHostingView(rootView: view.background(Color(nsColor: .windowBackgroundColor)))
        let size = size ?? host.fittingSize
        let window = NSWindow(contentRect: NSRect(origin: .zero, size: size), styleMask: [.titled],
                              backing: .buffered, defer: false)
        window.contentView = host
        window.setFrameOrigin(NSPoint(x: -30000, y: -30000))      // exists, lays out, is never seen
        window.orderBack(nil)
        RunLoop.main.run(until: Date().addingTimeInterval(1.2))   // lists fill in asynchronously; "late" states arrive
        host.layoutSubtreeIfNeeded()
        if let rep = host.bitmapImageRepForCachingDisplay(in: host.bounds) {
            host.cacheDisplay(in: host.bounds, to: rep)
            try? rep.representation(using: .png, properties: [:])?.write(to: dir.appendingPathComponent("\(name).png"))
        }
        window.close()
    }
}

/// Made-up cameras and batch results for `Snapshots`.
enum Fixtures {
    static func cam(_ n: Int, ip: String, name: String, foreign: Bool = false) -> Device {
        Device(mac: String(format: "02:00:00:00:00:%02x", n), ip: ip, mask: foreign ? "255.0.0.0" : "255.255.255.0",
               gateway: foreign ? "10.0.0.1" : "192.168.1.1", dns1: foreign ? "10.0.0.1" : "192.168.1.1", dns2: "8.8.8.8",
               name: name, serial: String(format: "TESTSERIAL%02d", n), firmware: "1.5-1428327", model: "DCN-BM8127AIN",
               vendor: "DVC", http: 80, rtsp: 554, build: "2023-11-03", src: ip, foreign: foreign)
    }

    static let devices: [Device] = [
        cam(1, ip: "192.168.1.69", name: "Lobby North"), cam(2, ip: "192.168.1.70", name: "Lobby South"),
        cam(3, ip: "192.168.1.88", name: "DVC"), cam(4, ip: "192.168.1.88", name: "DVC"),
        cam(5, ip: "10.0.0.5", name: "Gate", foreign: true), cam(6, ip: "10.0.0.6", name: "Parking", foreign: true),
    ]

    static let many: [Device] = (1...30).map { cam($0, ip: "192.168.1.88", name: "DVC") }

    private static func config(_ d: Device) -> BatchConfig {
        BatchConfig(ip: d.ip, mask: d.mask, gateway: d.gateway, dns1: d.dns1, dns2: d.dns2)
    }

    /// A previewed plan: consecutive addresses from .100, with a warning on the second camera, one camera that
    /// already has its address and one that cannot be applied.
    static func plan(_ ds: [Device]) -> [BatchItem] {
        ds.enumerated().map { i, d in
            var target = config(d)
            target.ip = "192.168.1.\(100 + i)"
            var item = BatchItem(mac: d.mac, name: d.name, model: d.model, current: config(d), target: target,
                                 changes: ["ip"], status: "planned", acked: false, warnings: [], error: nil)
            if i == 1 {
                item.warnings = ["192.168.1.101 is in use by a4:83:e7:9f:a4:ea, which is not a camera this tool can see"]
            }
            if i == 2, ds.count <= 6 {
                item.target = config(d); item.changes = []; item.status = "unchanged"
            }
            if i == 3, ds.count <= 6 {
                item.status = "invalid"; item.error = "192.168.1.103 is in use by 02:00:00:00:00:09"
            }
            return item
        }
    }

    /// The same batch after a run that went half well.
    static func outcome(_ ds: [Device]) -> [BatchItem] {
        var items = plan(ds)
        let statuses = ["confirmed", "not-confirmed", "unchanged", "not-sent"]
        for i in items.indices {
            items[i].status = statuses[i % statuses.count]
            items[i].acked = items[i].status != "not-sent"
            items[i].error = items[i].status == "not-confirmed"
                ? "received the command but did not re-announce the new configuration: wrong admin password, or slow to come back"
                : (items[i].status == "not-sent" ? "not sent: the first camera did not confirm" : nil)
        }
        return items
    }
}
