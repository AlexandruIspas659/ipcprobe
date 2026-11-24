import Foundation
import Testing
@testable import IPCProbe

@Suite struct LiveListTests {
    @Test func decodesAWatchLine() throws {
        // Exactly what `ipcprobe watch --json` prints: one compact object per line.
        let line = #"{"devices":[{"mac":"02:00:00:00:05:01","ip":"192.168.60.10","mask":"255.255.255.0","gateway":"192.168.60.254","dns1":"192.168.60.1","dns2":"8.8.8.8","name":"w-1","serial":"FAKESERIAL01","firmware":"1.5-0000000","model":"DCN-FAKE1","vendor":"FAKE","http":80,"rtsp":554,"build":"2026-01-01","src":"192.168.1.145","foreign":true}]}"#
        let snapshot = try JSONDecoder().decode(WatchSnapshot.self, from: Data(line.utf8))
        #expect(snapshot.devices.map(\.ip) == ["192.168.60.10"])
        #expect(snapshot.devices[0].isForeign)
        #expect(try JSONDecoder().decode(WatchSnapshot.self, from: Data(#"{"devices":[]}"#.utf8)).devices.isEmpty)
    }

    @Test func lineCollectorSplitsAcrossChunks() {
        // The pipe hands over bytes, not lines: a snapshot can arrive in pieces, or two at once.
        let c = LineCollector()
        #expect(c.append(Data(#"{"dev"#.utf8)).isEmpty)
        #expect(c.append(Data("ices\":[]}\n{\"devices\":[]}\n{".utf8)) == [#"{"devices":[]}"#, #"{"devices":[]}"#])
        #expect(c.append(Data("}".utf8), flush: true) == ["{}"])
    }

    @MainActor @Test func exportTakesTheSelectionOnlyWhenSeveralAreSelected() {
        let model = AppModel()
        model.devices = [camera("02:00:00:00:00:01", name: "lobby"), camera("02:00:00:00:00:02", name: "lobby"),
                         camera("02:00:00:00:00:03", name: "gate")]
        #expect(model.exportDevices.count == 3)
        model.selection = ["02:00:00:00:00:01"]                       // one selected: still the whole list
        #expect(model.exportDevices.count == 3)
        model.selection = ["02:00:00:00:00:03", "02:00:00:00:00:01"]
        #expect(model.exportDevices.map(\.mac) == ["02:00:00:00:00:01", "02:00:00:00:00:03"])     // in table order
        model.selection = []
        model.filter.text = "lobby"                                   // a filtered list exports what it shows
        #expect(model.exportDevices.count == 2)
    }

    @MainActor @Test func oneOffRunsListenAsLongAsTheModeSays() {
        let model = AppModel()
        #expect(model.listen == .live)
        #expect(model.timeout == 3)                 // live mode: a short look before a batch is enough
        model.listen = .seconds(10)                 // before startup: must not launch anything
        #expect(model.timeout == 10)
        #expect(!model.liveRunning)
    }
}
