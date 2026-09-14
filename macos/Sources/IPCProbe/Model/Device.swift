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
