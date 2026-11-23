import AppKit
import Foundation
import SwiftUI

/// All app state. One instance, owned by the App, injected as an environment object.
@MainActor
final class AppModel: ObservableObject {
    @Published var interfaces: [NetInterface] = []
    /// `nil` means "all interfaces" — the helper's default when `--iface` is omitted.
    @Published var selectedInterface: String? = nil {
        didSet { if selectedInterface != oldValue, started, listen == .live { startLive() } }
    }

    /// How the list is kept: `live` runs `ipcprobe watch` and follows it; `seconds` is a one-off `ipcprobe list`
    /// that listens that long each time Rescan is pressed.
    enum Listen: Hashable { case live, seconds(Double) }
    @Published var listen: Listen = .live {
        didSet { if listen != oldValue, started { listenChanged() } }
    }

    /// How long one-off helper runs (a manual scan, the discovery before a batch) listen.
    var timeout: Double {
        if case .seconds(let s) = listen { return s }
        return 3
    }

    /// True while the `watch` helper behind the live list is running.
    @Published var liveRunning = false
    private var liveStream: HelperStream?
    /// Identifies the current stream, so that lines and exits from one that has been replaced are ignored.
    private var liveToken = UUID()
    /// Set once startup has run; until then, changing a setting must not start anything.
    private var started = false

    @Published var devices: [Device] = []
    @Published var sortOrder: [KeyPathComparator<Device>] = [KeyPathComparator(\.ipValue, order: .forward)]
    /// MACs of the selected rows. One selected camera shows its details; several offer the batch sheet.
    @Published var selection: Set<String> = []
    @Published var isScanning = false
    @Published var lastScan: Date? = nil
    @Published var error: String? = nil
    @Published var helperVersion = "…"

    /// The selected cameras in the order the table shows them — the order a batch hands out addresses in.
    var selectedDevices: [Device] { devices.filter { selection.contains($0.mac) } }
    var selectedDevice: Device? { selection.count == 1 ? selectedDevices.first : nil }

    /// What a batch sheet is for: a change the user describes, or bringing stranded cameras into this Mac's subnet
    /// with addresses the helper picks (`apply --rescue`).
    enum BatchMode { case change, rescue }

    /// The cameras a batch sheet was opened for — a snapshot, so a rescan behind the sheet cannot change them.
    struct BatchSelection: Identifiable {
        let id = UUID()
        let devices: [Device]
        var mode: BatchMode = .change
    }

    /// The sheet that is up, if any. Each carries its own cameras: a sheet presented from a bare flag reads the
    /// selection too late (or not at all), which is how a batch sheet once opened for "0 cameras".
    @Published var editing: Device? = nil
    @Published var batch: BatchSelection? = nil

    /// Opens the change-network sheet for the selection: the single-camera form for one, the batch form for several.
    /// The detail pane's buttons, the toolbar button and the Cameras menu all come through here.
    func changeNetwork() {
        guard editing == nil, batch == nil else { return }
        let chosen = selectedDevices
        if chosen.count == 1 {
            editing = chosen[0]
        } else if chosen.count > 1 {
            batch = BatchSelection(devices: chosen)
        }
    }

    func selectAll() { selection = Set(devices.map(\.mac)) }

    /// The stranded cameras a rescue would act on: the selected ones if any of the selection is stranded, otherwise
    /// every stranded camera in the list.
    var rescueCandidates: [Device] {
        let chosen = selectedDevices.foreign
        return chosen.isEmpty ? devices.foreign : chosen
    }

    /// Opens the batch sheet in rescue mode for `rescueCandidates`.
    func rescue() {
        guard editing == nil, batch == nil else { return }
        let stranded = rescueCandidates
        if !stranded.isEmpty { batch = BatchSelection(devices: stranded, mode: .rescue) }
    }

    /// The app's own version, from Info.plist ("dev" when run outside a bundle with `swift run`).
    var appVersion: String {
        (Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String).flatMap { $0.isEmpty ? nil : $0 } ?? "dev"
    }

    /// What the status bar shows. App and helper come from the same release, so normally one number; the helper is
    /// named only when it differs, which happens in development builds.
    var versionLabel: String {
        if helperVersion == "…" { return "v\(appVersion)" }
        return helperVersion == appVersion ? "v\(appVersion)" : "v\(appVersion) · helper \(helperVersion)"
    }

    init() {
        // The watch helper would notice the app is gone the next time it writes, but that can take seconds.
        NotificationCenter.default.addObserver(forName: NSApplication.willTerminateNotification, object: nil,
                                               queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.stopLive() }
        }
    }

    func startup() async {
        helperVersion = await ProbeRunner.version()
        await refreshInterfaces()
        started = true
        await rescan()
    }

    /// The Rescan button: start the live list afresh, or listen once more.
    func rescan() async {
        if listen == .live { startLive() } else { await scanOnce() }
    }

    /// Bring the list up to date after something may have changed (a sheet closing). The live list already is.
    func scan() async {
        if listen == .live {
            if !liveRunning { startLive() }
        } else {
            await scanOnce()
        }
    }

    private func listenChanged() {
        if listen == .live {
            startLive()
        } else {
            stopLive()
            Task { await scanOnce() }
        }
    }

    /// Replaces the list with what the helper reports, keeping the user's sort column and whatever of the
    /// selection is still there.
    private func show(_ found: [Device]) {
        devices = found.sorted(using: sortOrder)
        lastScan = Date()
        selection.formIntersection(devices.map(\.mac))
    }

    // MARK: live list

    func startLive() {
        stopLive()
        error = nil
        let token = UUID()
        liveToken = token
        var args = ["watch", "--json"]
        if let i = selectedInterface { args += ["--iface", i] }
        do {
            liveStream = try ProbeRunner.stream(args, onLine: { [weak self] line in
                guard let snapshot = try? JSONDecoder().decode(WatchSnapshot.self, from: Data(line.utf8)) else { return }
                Task { @MainActor in
                    guard let self, self.liveToken == token else { return }
                    self.show(snapshot.devices)
                }
            }, onExit: { [weak self] status, said in
                Task { @MainActor in
                    guard let self, self.liveToken == token else { return }   // replaced or stopped on purpose
                    self.liveRunning = false
                    self.liveStream = nil
                    self.error = "Live updates stopped" + (said.isEmpty ? " (ipcprobe exited with status \(status))." : ": \(said)")
                }
            })
            liveRunning = true
        } catch {
            liveRunning = false
            self.error = error.localizedDescription
        }
    }

    func stopLive() {
        liveToken = UUID()          // whatever the old stream still says is no longer ours
        liveStream?.stop()
        liveStream = nil
        liveRunning = false
    }

    // MARK: one-off scan

    func refreshInterfaces() async {
        do {
            interfaces = try await ProbeRunner.runJSON([NetInterface].self, ["interfaces", "--json"])
            if let sel = selectedInterface, !interfaces.contains(where: { $0.ip == sel }) {
                selectedInterface = nil
            }
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func scanOnce() async {
        guard !isScanning else { return }
        isScanning = true
        error = nil
        defer { isScanning = false }
        var args = ["list", "--json", "--timeout", String(timeout)]
        if let i = selectedInterface { args += ["--iface", i] }
        do {
            show(try await ProbeRunner.runJSON([Device].self, args))
        } catch {
            self.error = error.localizedDescription
        }
    }

    /// Runs `ipcprobe set`, passing the password on stdin. Progress lines are appended to `log`; the returned
    /// status is the helper's exit code: 0 confirmed, 2 not confirmed (wrong password or slow camera), 1 setup error.
    func setNetwork(mac: String, ip: String, mask: String, gateway: String, dns1: String, dns2: String,
                    password: String, force: Bool, log: @escaping @MainActor (String) -> Void) async -> Int32 {
        var args = ["set", "--mac", mac, "--ip", ip, "--mask", mask, "--gw", gateway]
        if !dns1.isEmpty { args += ["--dns1", dns1] }
        if !dns2.isEmpty { args += ["--dns2", dns2] }
        if let i = selectedInterface { args += ["--iface", i] }
        if force { args.append("--force") }
        do {
            let r = try await ProbeRunner.run(args, stdin: password) { line in
                Task { @MainActor in log(line) }
            }
            if let out = String(data: r.stdout, encoding: .utf8), !out.isEmpty {
                log(out.trimmingCharacters(in: .whitespacesAndNewlines))
            }
            return r.status
        } catch {
            log("error: \(error.localizedDescription)")
            return 1
        }
    }

    /// The result of one `ipcprobe apply` run: the helper's exit code, its decoded JSON if it printed any, and
    /// otherwise what it said on stderr.
    struct BatchRun {
        var status: Int32
        var output: BatchOutput?
        var message: String?
    }

    /// Runs `ipcprobe apply --json` for the given cameras. `options` are the helper's own flags (`--start-ip`,
    /// `--gw`, `--dry-run`, `--yes`, …). The password, when given, goes over stdin. Progress lines are passed to
    /// `log`. Exit codes: 0 everything confirmed (or a clean dry run), 2 not everything, 1 setup error.
    func applyBatch(macs: [String], options: [String], password: String?,
                    log: @escaping @MainActor (String) -> Void) async -> BatchRun {
        var args = ["apply", "--mac", macs.joined(separator: ","), "--json", "--timeout", String(timeout)]
        if let i = selectedInterface { args += ["--iface", i] }
        args += options
        do {
            let r = try await ProbeRunner.run(args, stdin: password) { line in
                Task { @MainActor in log(line) }
            }
            if let out = try? JSONDecoder().decode(BatchOutput.self, from: r.stdout) {
                return BatchRun(status: r.status, output: out, message: nil)
            }
            let said = r.stderr.trimmingCharacters(in: .whitespacesAndNewlines)
            return BatchRun(status: r.status == 0 ? 1 : r.status, output: nil,
                            message: said.isEmpty ? "ipcprobe exited with status \(r.status)" : said)
        } catch {
            return BatchRun(status: 1, output: nil, message: error.localizedDescription)
        }
    }
}
