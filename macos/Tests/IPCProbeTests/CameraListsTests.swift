import Foundation
import Testing
@testable import IPCProbe

func camera(_ mac: String, ip: String = "192.168.1.88", name: String = "DVC") -> Device {
    Device(mac: mac, ip: ip, mask: "255.255.255.0", gateway: "192.168.1.1", dns1: "192.168.1.1", dns2: "8.8.8.8",
           name: name, serial: "S", firmware: "1.5", model: "DCN-TEST", vendor: "DVC", http: 80, rtsp: 554,
           build: "2026-01-01", src: nil)
}

@Suite struct CameraFlagTests {
    @Test func sharedAddressesCountsOnlyAddressesUsedMoreThanOnce() {
        let cams = [camera("02:00:00:00:00:01"), camera("02:00:00:00:00:02"), camera("02:00:00:00:00:03"),
                    camera("02:00:00:00:00:04", ip: "192.168.1.10")]
        #expect(cams.sharedAddresses == ["192.168.1.88": 3])
        #expect([Device]().sharedAddresses.isEmpty)
    }

    @Test func foreignTreatsAMissingFlagAsReachable() {
        var stranded = camera("02:00:00:00:00:01", ip: "10.0.0.5")
        stranded.foreign = true
        let old = camera("02:00:00:00:00:02")          // a helper that predates the flag sends none
        var home = camera("02:00:00:00:00:03")
        home.foreign = false
        #expect([stranded, old, home].foreign.map(\.mac) == ["02:00:00:00:00:01"])
    }

    @Test func decodesTheHelpersJSONWithAndWithoutTheFlag() throws {
        let json = """
        [{"mac":"02:00:00:00:00:01","ip":"10.0.0.5","mask":"255.0.0.0","gateway":"10.0.0.1","dns1":"10.0.0.1",
          "dns2":"8.8.8.8","name":"lost","serial":"S","firmware":"1.5","model":"M","vendor":"V","http":80,"rtsp":554,
          "build":"2026-01-01","src":"192.168.1.145","foreign":true},
         {"mac":"02:00:00:00:00:02","ip":"192.168.1.9","mask":"255.255.255.0","gateway":"192.168.1.1",
          "dns1":"192.168.1.1","dns2":"8.8.8.8","name":"old","serial":"S","firmware":"1.5","model":"M","vendor":"V",
          "http":80,"rtsp":554,"build":""}]
        """
        let devices = try JSONDecoder().decode([Device].self, from: Data(json.utf8))
        #expect(devices.map(\.isForeign) == [true, false])
    }
}

@Suite struct CameraFilterTests {
    let cams = [
        camera("58:5b:69:00:00:01", ip: "192.168.1.69", name: "Lobby North"),
        camera("58:5b:69:00:00:02", ip: "192.168.1.70", name: "lobby south"),
        camera("02:00:00:00:00:03", ip: "10.0.0.5", name: "Gate"),
    ]

    @Test func emptyQueryMatchesEverything() {
        #expect(cams.matching("").count == 3)
        #expect(cams.matching("   ").count == 3)
    }

    @Test func matchesNameIgnoringCase() {
        #expect(cams.matching("LOBBY").map(\.name) == ["Lobby North", "lobby south"])
        #expect(cams.matching("gate").map(\.name) == ["Gate"])
        #expect(cams.matching("nothing like this").isEmpty)
    }

    @Test func everyWordHasToMatchSomewhere() {
        #expect(cams.matching("lobby .70").map(\.name) == ["lobby south"])     // a name and part of an address
        #expect(cams.matching("lobby 10.0").isEmpty)
        #expect(cams.matching("dcn-test 10.").map(\.name) == ["Gate"])         // model and address
    }

    @Test func matchesAddressesAndMACsHoweverTheyAreTyped() {
        #expect(cams.matching("192.168.1.7").map(\.name) == ["lobby south"])
        #expect(cams.matching("58:5b:69").count == 2)
        #expect(cams.matching("58-5B-69-00-00-02").map(\.name) == ["lobby south"])
        #expect(cams.matching("585b69000001").map(\.name) == ["Lobby North"])
    }
}

@Suite struct CameraOrderTests {
    let cams = [
        camera("02:00:00:00:00:03", ip: "192.168.1.9", name: "cam-10"),
        camera("02:00:00:00:00:01", ip: "192.168.1.134", name: "cam-2"),
        camera("02:00:00:00:00:02", ip: "192.168.1.9", name: "cam-2"),
    ]

    @Test func byMAC() {
        #expect(cams.sorted(by: .mac).map(\.mac) == ["02:00:00:00:00:01", "02:00:00:00:00:02", "02:00:00:00:00:03"])
    }

    @Test func byAddressIsNumericAndTiesBreakOnMAC() {
        // .9 sorts before .134, and the two cameras sharing .9 keep a fixed order.
        #expect(cams.sorted(by: .address).map(\.mac) == ["02:00:00:00:00:02", "02:00:00:00:00:03", "02:00:00:00:00:01"])
    }

    @Test func byNameComparesNumbersAsNumbers() {
        // cam-2 (twice, MAC order) before cam-10.
        #expect(cams.sorted(by: .name).map(\.mac) == ["02:00:00:00:00:01", "02:00:00:00:00:02", "02:00:00:00:00:03"])
    }

    @Test func movingOneCameraUpAndDown() {
        let abc = [camera("02:00:00:00:00:0a"), camera("02:00:00:00:00:0b"), camera("02:00:00:00:00:0c")]
        let b = "02:00:00:00:00:0b"
        #expect(abc.moving(b, by: -1)?.map { $0.mac.suffix(1) } == ["b", "a", "c"])
        #expect(abc.moving(b, by: 1)?.map { $0.mac.suffix(1) } == ["a", "c", "b"])
        // Already at an end, nothing picked, or an unknown camera: nothing to do.
        #expect(abc.moving("02:00:00:00:00:0a", by: -1) == nil)
        #expect(abc.moving("02:00:00:00:00:0c", by: 1) == nil)
        #expect(abc.moving(nil, by: 1) == nil)
        #expect(abc.moving("02:00:00:00:00:ff", by: 1) == nil)
        #expect(abc.moving(b, by: 0) == nil)
    }

    @Test func factoryFreshCamerasFallBackToMACOrder() {
        let fresh = [camera("02:00:00:00:00:0b"), camera("02:00:00:00:00:0a"), camera("02:00:00:00:00:0c")]
        for order in [CameraOrder.mac, .name, .address] {
            #expect(fresh.sorted(by: order).map(\.mac) == ["02:00:00:00:00:0a", "02:00:00:00:00:0b", "02:00:00:00:00:0c"])
        }
    }
}
