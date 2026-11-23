import Foundation

/// The app's own list logic — ordering, and nothing that touches the network. Kept apart from the views so it can
/// be unit-tested (`macos/Tests`).

/// A way to put cameras in order, for the batch sheet's Order menu. The order matters there: consecutive addresses
/// are handed out top to bottom, and the first camera is the one tried on its own.
enum CameraOrder {
    case mac, name, address
}

extension Array where Element == Device {
    /// How many cameras announce each address that more than one of them announces. Cameras fresh out of the box
    /// all come up on the same factory address; until they are renumbered only this tool can tell them apart.
    var sharedAddresses: [String: Int] {
        Dictionary(map { ($0.ip, 1) }, uniquingKeysWith: +).filter { $0.value > 1 }
    }

    /// The cameras that cannot be reached by IP from this Mac.
    var foreign: [Device] { filter(\.isForeign) }

    /// The cameras matching a filter typed by the user. Every word of the query has to occur somewhere in the
    /// camera's name, model, addresses, MAC, serial, firmware or vendor; case does not matter, and a MAC can be
    /// typed with colons, with dashes or with nothing in between. An empty query matches everything.
    func matching(_ query: String) -> [Device] {
        let words = query.lowercased().split(whereSeparator: \.isWhitespace).map(String.init)
        guard !words.isEmpty else { return self }
        return filter { d in
            let bareMAC = d.mac.replacingOccurrences(of: ":", with: "")
            let haystack = [d.name, d.model, d.ip, d.mask, d.gateway, d.mac, bareMAC, d.serial, d.firmware, d.vendor]
                .joined(separator: "\n").lowercased()
            return words.allSatisfy { word in
                if haystack.contains(word) { return true }
                // A MAC typed with dashes, or only partly with separators: compare the bare digits.
                guard word.contains(where: { $0 == "-" || $0 == ":" }) else { return false }
                return bareMAC.contains(word.filter { $0 != "-" && $0 != ":" })
            }
        }
    }

    /// Sorted by the given key, with the MAC as the tie-breaker so the result is stable — factory-fresh cameras
    /// share both their name and their address.
    func sorted(by order: CameraOrder) -> [Device] {
        sorted { a, b in
            switch order {
            case .mac:
                break
            case .name:
                // "cam-2" before "cam-10": compare the way Finder does, not character by character.
                let c = a.name.localizedStandardCompare(b.name)
                if c != .orderedSame { return c == .orderedAscending }
            case .address:
                if a.ipValue != b.ipValue { return a.ipValue < b.ipValue }
            }
            return a.mac < b.mac
        }
    }
}
