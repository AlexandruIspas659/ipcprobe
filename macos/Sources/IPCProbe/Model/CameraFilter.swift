import Foundation

/// Everything the filter bar can narrow the list by: what is typed in the field, plus criteria picked from the
/// Filter menu. The typed words and the picked criteria all have to hold; within one kind of criterion (two models,
/// say) any of the picked values will do.
struct CameraFilter: Equatable {
    var text = ""
    var foreign = false             // outside this Mac's subnet
    var sharedAddress = false       // announces an address another camera announces too
    var unnamed = false             // still carries the default name
    var models: Set<String> = []
    var firmwares: Set<String> = []
    var subnets: Set<String> = []   // "192.168.1.0/24"

    var hasCriteria: Bool {
        foreign || sharedAddress || unnamed || !models.isEmpty || !firmwares.isEmpty || !subnets.isEmpty
    }

    var isEmpty: Bool { !hasCriteria && text.allSatisfy(\.isWhitespace) }

    func apply(to devices: [Device]) -> [Device] {
        guard !isEmpty else { return devices }
        let shared = devices.sharedAddresses        // counted over every camera, not just the ones still showing
        return devices.matching(text).filter { d in
            (!foreign || d.isForeign)
                && (!sharedAddress || shared[d.ip] != nil)
                && (!unnamed || d.isUnnamed)
                && (models.isEmpty || models.contains(d.model))
                && (firmwares.isEmpty || firmwares.contains(d.firmware))
                && (subnets.isEmpty || d.subnet.map(subnets.contains) == true)
        }
    }

    /// One picked criterion, as shown — and removed — in the filter bar.
    struct Chip: Identifiable, Equatable {
        let id: String
        let label: String
    }

    var chips: [Chip] {
        var out: [Chip] = []
        if foreign { out.append(Chip(id: "foreign", label: "Outside this subnet")) }
        if sharedAddress { out.append(Chip(id: "shared", label: "Shares an address")) }
        if unnamed { out.append(Chip(id: "unnamed", label: "Not named yet")) }
        out += models.sorted().map { Chip(id: "model:\($0)", label: "Model \($0)") }
        out += firmwares.sorted().map { Chip(id: "firmware:\($0)", label: "Firmware \($0)") }
        out += subnets.sorted().map { Chip(id: "subnet:\($0)", label: "Subnet \($0)") }
        return out
    }

    mutating func remove(_ chip: Chip) {
        switch chip.id {
        case "foreign": foreign = false
        case "shared": sharedAddress = false
        case "unnamed": unnamed = false
        default:
            guard let colon = chip.id.firstIndex(of: ":") else { return }
            let value = String(chip.id[chip.id.index(after: colon)...])
            switch chip.id[..<colon] {
            case "model": models.remove(value)
            case "firmware": firmwares.remove(value)
            case "subnet": subnets.remove(value)
            default: break
            }
        }
    }
}

/// What the Filter menu offers: built from the cameras actually on the network, each choice with how many it
/// would show.
struct CameraFacets: Equatable {
    struct Value: Identifiable, Equatable {
        let value: String
        let count: Int
        var id: String { value }
    }

    var foreign = 0
    var sharedAddress = 0
    var unnamed = 0
    var models: [Value] = []
    var firmwares: [Value] = []
    var subnets: [Value] = []
}

extension Device {
    /// The network the camera's own address and mask put it on, e.g. "192.168.1.0/24"; nil if either is malformed.
    var subnet: String? {
        let a = Device.ipNumber(ip), m = Device.ipNumber(mask)
        guard ip.split(separator: ".").count == 4, mask.split(separator: ".").count == 4 else { return nil }
        guard m == 0 ? mask == "0.0.0.0" : (~m & (~m &+ 1)) == 0 else { return nil }      // ones then zeros only
        let n = a & m
        return "\(n >> 24).\((n >> 16) & 255).\((n >> 8) & 255).\(n & 255)/\(m.nonzeroBitCount)"
    }

    /// True while the camera still carries the name it shipped with: none, or just the brand ("DVC"). A guess that
    /// holds for the cameras seen so far — the protocol has no "was renamed" flag.
    var isUnnamed: Bool {
        let n = name.trimmingCharacters(in: .whitespaces)
        return n.isEmpty || n.caseInsensitiveCompare(vendor) == .orderedSame
    }
}

extension Array where Element == Device {
    /// The Filter menu's choices. Values picked in `selected` are listed even when no camera has them any more
    /// (count 0), so that a criterion that has stopped matching can still be seen and switched off.
    func facets(keeping selected: CameraFilter = CameraFilter()) -> CameraFacets {
        func tally(_ values: [String], _ picked: Set<String>, numeric: Bool = false) -> [CameraFacets.Value] {
            var counts = Dictionary(values.filter { !$0.isEmpty }.map { ($0, 1) }, uniquingKeysWith: +)
            for p in picked where counts[p] == nil { counts[p] = 0 }
            return counts.map { CameraFacets.Value(value: $0.key, count: $0.value) }
                .sorted { $0.value.localizedStandardCompare($1.value) == .orderedAscending }
        }
        let shared = sharedAddresses
        return CameraFacets(
            foreign: filter(\.isForeign).count,
            sharedAddress: filter { shared[$0.ip] != nil }.count,
            unnamed: filter(\.isUnnamed).count,
            models: tally(map(\.model), selected.models),
            firmwares: tally(map(\.firmware), selected.firmwares),
            subnets: tally(compactMap(\.subnet), selected.subnets))
    }
}
