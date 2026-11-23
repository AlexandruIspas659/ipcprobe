import Foundation

/// One camera, exactly as `ipcprobe list --json` emits it (the JSON tags on `mhed.Device` in Go are the contract).
struct Device: Codable, Identifiable, Hashable {
    var mac: String
    var ip: String
    var mask: String
    var gateway: String
    var dns1: String
    var dns2: String
    var name: String
    var serial: String
    var firmware: String
    var model: String
    var vendor: String
    var http: Int
    var rtsp: Int
    var build: String
    var src: String?

    /// A camera is identified by its MAC: the IP is the thing we change.
    var id: String { mac }

    /// A dotted IPv4 as a number, so sorting orders 192.168.1.9 before 192.168.1.134 (a string sort would not).
    /// A malformed address sorts as 0.
    static func ipNumber(_ s: String) -> UInt32 {
        let parts = s.split(separator: ".", omittingEmptySubsequences: false)
        guard parts.count == 4 else { return 0 }
        var n: UInt32 = 0
        for p in parts {
            guard let b = UInt8(p) else { return 0 }
            n = (n << 8) | UInt32(b)
        }
        return n
    }

    var ipValue: UInt32 { Device.ipNumber(ip) }
    var gatewayValue: UInt32 { Device.ipNumber(gateway) }

    var webURL: URL? {
        guard http != 0 else { return nil }
        return URL(string: "http://\(ip)\(http == 80 ? "" : ":\(http)")/")
    }

    var rtspURL: String? {
        guard rtsp != 0 else { return nil }
        return "rtsp://\(ip)\(rtsp == 554 ? "" : ":\(rtsp)")/"
    }
}

/// One usable interface, as `ipcprobe interfaces --json` emits it.
struct NetInterface: Codable, Identifiable, Hashable {
    var name: String
    var ip: String
    var net: String

    /// `--iface` accepts a name or an address; the address is unambiguous when a name carries several.
    var id: String { ip }
    var label: String { "\(name)  \(ip)" }
}

/// One network configuration, as `ipcprobe apply --json` emits it (`batch.Config` in Go).
struct BatchConfig: Codable, Hashable {
    var ip: String
    var mask: String
    var gateway: String
    var dns1: String
    var dns2: String
}

/// One camera of a batch: what it has, what it should get, and how that went (`batch.Item` in Go).
/// `status` is one of planned, unchanged, not-seen, invalid, confirmed, not-confirmed, not-sent.
struct BatchItem: Codable, Hashable {
    var mac: String
    var name: String
    var model: String
    var current: BatchConfig?
    var target: BatchConfig?
    var changes: [String]
    var status: String
    var acked: Bool
    var warnings: [String]
    var error: String?
}

/// What `ipcprobe apply --json` prints: the plan (with `--dry-run`) or its outcome.
struct BatchOutput: Codable {
    var dryRun: Bool
    var devices: [BatchItem]

    enum CodingKeys: String, CodingKey {
        case dryRun = "dry_run"
        case devices
    }
}
