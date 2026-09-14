import Foundation
import SwiftUI

/// All app state. One instance, owned by the App, injected as an environment object.
@MainActor
final class AppModel: ObservableObject {
    @Published var interfaces: [NetInterface] = []
    /// `nil` means "all interfaces" — the helper's default when `--iface` is omitted.
    @Published var selectedInterface: String? = nil
    @Published var timeout: Double = 5

    @Published var devices: [Device] = []
    @Published var selection: String? = nil
    @Published var isScanning = false
    @Published var lastScan: Date? = nil
    @Published var error: String? = nil
    @Published var helperVersion = "…"

    var selectedDevice: Device? { devices.first { $0.mac == selection } }

    func startup() async {
        helperVersion = await ProbeRunner.version()
        await refreshInterfaces()
        await scan()
    }

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

    func scan() async {
        guard !isScanning else { return }
        isScanning = true
        error = nil
        defer { isScanning = false }
        var args = ["list", "--json", "--timeout", String(timeout)]
        if let i = selectedInterface { args += ["--iface", i] }
        do {
            let found = try await ProbeRunner.runJSON([Device].self, args)
            devices = found.sorted { ipKey($0.ip).lexicographicallyPrecedes(ipKey($1.ip)) }
            lastScan = Date()
            if let sel = selection, !devices.contains(where: { $0.mac == sel }) { selection = nil }
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

    private func ipKey(_ s: String) -> [Int] { s.split(separator: ".").map { Int($0) ?? 0 } }
}
