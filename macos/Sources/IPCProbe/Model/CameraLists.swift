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
