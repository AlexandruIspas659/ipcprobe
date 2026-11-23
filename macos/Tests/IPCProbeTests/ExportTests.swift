import Foundation
import Testing
@testable import IPCProbe

@Suite struct ExportTests {
    @Test func csvQuotesOnlyWhatNeedsIt() {
        let text = Export.csv([["Name", "Note"], ["lobby, \"north\"", "plain"], ["two\nlines", ""]])
        #expect(text == "Name,Note\n\"lobby, \"\"north\"\"\",plain\n\"two\nlines\",\n")
    }

    @Test func tsvFlattensWhatItCannotQuote() {
        #expect(Export.tsv([["a\tb", "c\nd"], ["e", "f"]]) == "a b\tc d\ne\tf\n")
    }

    @Test func deviceRowsCarryTheNotes() {
        var stranded = camera("02:00:00:00:00:01", ip: "10.0.0.5", name: "lost")
        stranded.foreign = true
        let rows = Export.deviceRows([stranded, camera("02:00:00:00:00:02", ip: "10.0.0.5"), camera("02:00:00:00:00:03")])
        #expect(rows.count == 4)
        #expect(Set(rows.map(\.count)) == [15])          // every row as wide as the header
        #expect(rows.map { $0[14] } == ["Note", "other subnet, address shared by 2", "address shared by 2", ""])
        #expect(rows[1][1] == "10.0.0.5" && rows[1][12] == "80" && rows[1][13] == "554")
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

    @Test func fileNameCarriesTheDate() {
        let day = Date(timeIntervalSince1970: 1_795_000_000)   // 2026-11-18 UTC
        #expect(Export.fileName("cameras", on: day).hasPrefix("cameras-2026-11-"))
        #expect(Export.fileName("cameras", on: day).hasSuffix(".csv"))
    }
}
