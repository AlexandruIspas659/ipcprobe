import AppKit
import UniformTypeIdentifiers

/// Turning what is on screen into something to paste into a spreadsheet or file with the site's documentation.
/// The columns match the helper's own `--csv` output (`cmd/ipcprobe/export.go`).
enum Export {
    static func deviceRows(_ devices: [Device]) -> [[String]] {
        let shared = devices.sharedAddresses
        var rows = [["MAC", "IP", "Mask", "Gateway", "DNS 1", "DNS 2", "Name", "Model", "Firmware", "Build",
                     "Serial", "Vendor", "HTTP", "RTSP", "Note"]]
        for d in devices {
            var notes: [String] = []
            if d.isForeign { notes.append("other subnet") }
            if let n = shared[d.ip] { notes.append("address shared by \(n)") }
            rows.append([d.mac, d.ip, d.mask, d.gateway, d.dns1, d.dns2, d.name, d.model, d.firmware, d.build,
                         d.serial, d.vendor, String(d.http), String(d.rtsp), notes.joined(separator: ", ")])
        }
        return rows
    }

    /// One row per camera of a batch: where it was, where it is meant to be, how that went. The network columns
    /// hold the target configuration (the current one where there is no target), so after a successful run the
    /// sheet reads as the cameras' new settings.
    static func batchRows(_ items: [BatchItem]) -> [[String]] {
        var rows = [["MAC", "Name", "Model", "Old IP", "New IP", "Mask", "Gateway", "DNS 1", "DNS 2", "Status", "Note"]]
        for it in items {
            let cfg = it.target ?? it.current
            let notes = (it.error.map { [$0] } ?? []) + it.warnings.map { "warning: \($0)" }
            rows.append([it.mac, it.name, it.model, it.current?.ip ?? "", cfg?.ip ?? "", cfg?.mask ?? "",
                         cfg?.gateway ?? "", cfg?.dns1 ?? "", cfg?.dns2 ?? "", it.status,
                         notes.joined(separator: "; ")])
        }
        return rows
    }

    /// RFC 4180: a field containing a comma, a quote or a line break is quoted, with its quotes doubled.
    static func csv(_ rows: [[String]]) -> String {
        rows.map { row in
            row.map { field in
                field.contains(where: { ",\"\n\r".contains($0) })
                    ? "\"" + field.replacingOccurrences(of: "\"", with: "\"\"") + "\""
                    : field
            }.joined(separator: ",")
        }.joined(separator: "\n") + "\n"
    }

    /// Tab-separated, the form spreadsheets accept from the clipboard. There is no quoting in that form, so tabs
    /// and line breaks inside a field become spaces.
    static func tsv(_ rows: [[String]]) -> String {
        rows.map { row in
            row.map { $0.replacingOccurrences(of: "\t", with: " ").replacingOccurrences(of: "\r\n", with: " ")
                        .replacingOccurrences(of: "\n", with: " ").replacingOccurrences(of: "\r", with: " ") }
                .joined(separator: "\t")
        }.joined(separator: "\n") + "\n"
    }

    /// "cameras-2026-11-23.csv"
    static func fileName(_ stem: String, on date: Date = Date()) -> String {
        "\(stem)-\(date.formatted(.iso8601.year().month().day())).csv"
    }

    // MARK: clipboard and files

    @MainActor static func copy(_ rows: [[String]]) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(tsv(rows), forType: .string)
    }

    /// Asks where to save and writes the rows as CSV.
    @MainActor static func save(_ rows: [[String]], suggestedName: String) {
        let panel = NSSavePanel()
        panel.allowedContentTypes = [.commaSeparatedText]
        panel.nameFieldStringValue = suggestedName
        panel.canCreateDirectories = true
        guard panel.runModal() == .OK, let url = panel.url else { return }
        do {
            try csv(rows).write(to: url, atomically: true, encoding: .utf8)
        } catch {
            NSAlert(error: error).runModal()
        }
    }
}
