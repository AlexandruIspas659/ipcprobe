import AppKit
import UniformTypeIdentifiers

/// Turning what is on screen into something to paste into a spreadsheet or file with the site's documentation.
/// The columns match the helper's own `--csv` output (`cmd/ipcprobe/export.go`).
enum Export {
    static func deviceRows(_ devices: [Device]) -> [[String]] {
        let shared = devices.sharedAddresses
        var rows = [["MAC", "IP", "Mask", "Gateway", "DNS 1", "DNS 2", "Name", "Model", "Firmware", "Build",
                     "Serial", "Vendor", "HTTP", "RTSP", "Web URL", "RTSP URL", "Note"]]
        for d in devices {
            var notes: [String] = []
            if d.isForeign { notes.append("other subnet") }
            if let n = shared[d.ip] { notes.append("address shared by \(n)") }
            rows.append([d.mac, d.ip, d.mask, d.gateway, d.dns1, d.dns2, d.name, d.model, d.firmware, d.build,
                         d.serial, d.vendor, String(d.http), String(d.rtsp), d.webURL?.absoluteString ?? "",
                         d.rtspURL ?? "", notes.joined(separator: ", ")])
        }
        return rows
    }

    /// A plan waiting to be filled in: each camera's MAC, its name and model to recognise it by, and where it is
    /// now — then empty New IP / Mask / Gateway / DNS columns. Blank means keep, so the file handed back untouched
    /// changes nothing. Same columns as the helper's `list --template`.
    static func templateRows(_ devices: [Device]) -> [[String]] {
        [["MAC", "Name", "Model", "IP", "New IP", "Mask", "Gateway", "DNS 1", "DNS 2"]]
            + devices.map { [$0.mac, $0.name, $0.model, $0.ip, "", "", "", "", ""] }
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

    /// RFC 4180: a field containing the separator, a quote or a line break is quoted, with its quotes doubled.
    static func csv(_ rows: [[String]], separator: Character = ",") -> String {
        rows.map { row in
            row.map { field in
                field.contains(where: { $0 == separator || "\"\n\r".contains($0) })
                    ? "\"" + field.replacingOccurrences(of: "\"", with: "\"\"") + "\""
                    : field
            }.joined(separator: String(separator))
        }.joined(separator: "\n") + "\n"
    }

    /// The separator a spreadsheet on this Mac expects in a "CSV" file. Where the comma is the decimal sign — most
    /// of Europe — Excel reads and writes semicolons, and opens a comma-separated file as one long column. (The
    /// helper's `--plan` reads either, so a file saved here comes back in whichever way it was written.)
    static func spreadsheetSeparator(for locale: Locale = .current) -> Character {
        locale.decimalSeparator == "," ? ";" : ","
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

    /// "cameras-2026-11-23" — the save panel adds the extension of the chosen format.
    static func fileName(_ stem: String, on date: Date = Date()) -> String {
        "\(stem)-\(date.formatted(.iso8601.year().month().day()))"
    }

    /// The columns of a plan that are there to be typed into; a workbook tints them.
    static let planInputColumns = ["New IP", "Mask", "Gateway", "DNS 1", "DNS 2"]

    /// What the helper's `ipcprobe xlsx <file>` reads on stdin to write a workbook.
    struct WorkbookRequest: Codable, Equatable {
        var sheet: String
        var rows: [[String]]
        var input: [String]
    }

    static func workbookJSON(_ rows: [[String]], sheet: String, input: [String] = []) -> String {
        let data = (try? JSONEncoder().encode(WorkbookRequest(sheet: sheet, rows: rows, input: input))) ?? Data("{}".utf8)
        return String(decoding: data, as: UTF8.self)
    }

    // MARK: clipboard and files

    @MainActor static func copy(_ rows: [[String]]) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(tsv(rows), forType: .string)
    }

    /// Asks where to save, and in which format, and writes the rows.
    ///
    /// An Excel workbook is the default: it opens with sensible column widths and a header, and every cell is text,
    /// so no spreadsheet decides that an address is a number. The workbook is written by the helper
    /// (`ipcprobe xlsx`), which is where the format is implemented and tested. CSV remains for everything that is
    /// not a spreadsheet program. The choice is remembered.
    @MainActor static func save(_ rows: [[String]], name: String, sheet: String, input: [String] = []) {
        let workbook = UTType(filenameExtension: "xlsx") ?? .data
        let formats: [(title: String, type: UTType, key: String)] = [
            ("Excel workbook (.xlsx)", workbook, "xlsx"), ("CSV (.csv)", .commaSeparatedText, "csv"),
        ]
        let remembered = UserDefaults.standard.string(forKey: "exportFormat")
        let start = formats.firstIndex { $0.key == remembered } ?? 0

        let panel = NSSavePanel()
        panel.allowedContentTypes = [formats[start].type]
        panel.nameFieldStringValue = name
        panel.canCreateDirectories = true

        let popup = NSPopUpButton(frame: .zero, pullsDown: false)
        popup.addItems(withTitles: formats.map(\.title))
        popup.selectItem(at: start)
        let chooser = FormatChooser { index in panel.allowedContentTypes = [formats[index].type] }   // swaps the extension too
        popup.target = chooser
        popup.action = #selector(FormatChooser.changed(_:))
        let row = NSStackView(views: [NSTextField(labelWithString: "Format:"), popup])
        row.edgeInsets = NSEdgeInsets(top: 8, left: 12, bottom: 8, right: 12)
        panel.accessoryView = row

        let answer = withExtendedLifetime(chooser) { panel.runModal() }
        guard answer == .OK, let url = panel.url else { return }
        let format = formats[max(0, popup.indexOfSelectedItem)]
        UserDefaults.standard.set(format.key, forKey: "exportFormat")

        if format.key == "csv" {
            do {
                try csv(rows, separator: spreadsheetSeparator()).write(to: url, atomically: true, encoding: .utf8)
            } catch {
                NSAlert(error: error).runModal()
            }
            return
        }
        let request = workbookJSON(rows, sheet: sheet, input: input)
        Task { @MainActor in
            var failure: String? = nil
            do {
                let r = try await ProbeRunner.run(["xlsx", url.path], stdin: request)
                if r.status != 0 { failure = r.stderr.trimmingCharacters(in: .whitespacesAndNewlines) }
            } catch {
                failure = error.localizedDescription
            }
            if let failure {
                let alert = NSAlert()
                alert.messageText = "The workbook could not be saved."
                alert.informativeText = failure.isEmpty ? "ipcprobe reported an error." : failure
                alert.runModal()
            }
        }
    }
}

/// The save panel's format pop-up needs an object to call; this is it.
private final class FormatChooser: NSObject {
    let onChange: (Int) -> Void
    init(onChange: @escaping (Int) -> Void) { self.onChange = onChange }
    @objc func changed(_ sender: NSPopUpButton) { onChange(sender.indexOfSelectedItem) }
}
