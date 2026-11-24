import Foundation
import Testing
@testable import IPCProbe

@Suite struct ExportTests {
    @Test func csvQuotesOnlyWhatNeedsIt() {
        let text = Export.csv([["Name", "Note"], ["lobby, \"north\"", "plain"], ["two\nlines", ""]])
        #expect(text == "Name,Note\n\"lobby, \"\"north\"\"\",plain\n\"two\nlines\",\n")
    }

    @Test func csvWithSemicolonsQuotesSemicolonsNotCommas() {
        let text = Export.csv([["Name", "Note"], ["lobby, north", "a;b"]], separator: ";")
        #expect(text == "Name;Note\nlobby, north;\"a;b\"\n")
    }

    @Test func separatorFollowsTheDecimalSign() {
        #expect(Export.spreadsheetSeparator(for: Locale(identifier: "ro_RO")) == ";")
        #expect(Export.spreadsheetSeparator(for: Locale(identifier: "de_DE")) == ";")
        #expect(Export.spreadsheetSeparator(for: Locale(identifier: "en_US")) == ",")
        #expect(Export.spreadsheetSeparator(for: Locale(identifier: "en_GB")) == ",")
    }

    @Test func tsvFlattensWhatItCannotQuote() {
        #expect(Export.tsv([["a\tb", "c\nd"], ["e", "f"]]) == "a b\tc d\ne\tf\n")
    }

    @Test func deviceRowsCarryTheNotes() {
        var stranded = camera("02:00:00:00:00:01", ip: "10.0.0.5", name: "lost")
        stranded.foreign = true
        let rows = Export.deviceRows([stranded, camera("02:00:00:00:00:02", ip: "10.0.0.5"), camera("02:00:00:00:00:03")])
        #expect(rows.count == 4)
        #expect(Set(rows.map(\.count)) == [17])          // every row as wide as the header
        #expect(rows.map { $0[16] } == ["Note", "other subnet, address shared by 2", "address shared by 2", ""])
        #expect(rows[1][1] == "10.0.0.5" && rows[1][12] == "80" && rows[1][13] == "554")
        #expect(rows[0][14] == "Web URL" && rows[1][14] == "http://10.0.0.5/" && rows[1][15] == "rtsp://10.0.0.5/")
    }

    @Test func batchRowsShowWhereEachCameraEndsUp() {
        let cur = BatchConfig(ip: "192.168.1.88", mask: "255.255.255.0", gateway: "192.168.1.1", dns1: "192.168.1.1", dns2: "8.8.8.8")
        var tgt = cur
        tgt.ip = "192.168.1.100"
        let items = [
            BatchItem(mac: "02:00:00:00:00:01", name: "lobby", model: "M", current: cur, target: tgt, changes: ["ip"],
                      status: "confirmed", acked: true, warnings: ["gateway is a guess"], error: nil),
            BatchItem(mac: "02:00:00:00:00:02", name: "", model: "", current: nil, target: nil, changes: [],
                      status: "not-seen", acked: false, warnings: [], error: "not seen in discovery"),
        ]
        let rows = Export.batchRows(items)
        #expect(rows[1] == ["02:00:00:00:00:01", "lobby", "M", "192.168.1.88", "192.168.1.100", "255.255.255.0",
                            "192.168.1.1", "192.168.1.1", "8.8.8.8", "confirmed", "warning: gateway is a guess"])
        #expect(rows[2] == ["02:00:00:00:00:02", "", "", "", "", "", "", "", "", "not-seen", "not seen in discovery"])
    }

    @Test func templateHasTheCamerasAndEmptyColumnsToFill() {
        let rows = Export.templateRows([camera("02:00:00:00:00:01", ip: "192.168.1.88", name: "DVC")])
        #expect(rows[0] == ["MAC", "Name", "Model", "IP", "New IP", "Mask", "Gateway", "DNS 1", "DNS 2"])
        #expect(rows[1] == ["02:00:00:00:00:01", "DVC", "DCN-TEST", "192.168.1.88", "", "", "", "", ""])
    }

    @Test func workbookRequestIsWhatTheHelperReads() throws {
        // `ipcprobe xlsx <file>` decodes {"sheet", "rows", "input"} from stdin.
        let rows = Export.templateRows([camera("02:00:00:00:00:01", ip: "192.168.1.88", name: "say \"hi\"")])
        let json = Export.workbookJSON(rows, sheet: "Plan", input: Export.planInputColumns)
        let back = try JSONDecoder().decode(Export.WorkbookRequest.self, from: Data(json.utf8))
        #expect(back == Export.WorkbookRequest(sheet: "Plan", rows: rows, input: ["New IP", "Mask", "Gateway", "DNS 1", "DNS 2"]))
        #expect(!json.contains("\n"))            // one line: the helper reads a single JSON value
        #expect(Set(Export.planInputColumns).isSubset(of: Set(rows[0])))    // the tinted columns exist in the template
    }

    @Test func fileNameCarriesTheDate() {
        let day = Date(timeIntervalSince1970: 1_795_000_000)   // 2026-11-18 UTC
        #expect(Export.fileName("cameras", on: day).hasPrefix("cameras-2026-11-"))
        #expect(!Export.fileName("cameras", on: day).contains("."))      // the save panel adds the extension
    }
}
