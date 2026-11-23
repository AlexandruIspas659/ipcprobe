import Testing
@testable import IPCProbe

func camera(_ mac: String, ip: String = "192.168.1.88", name: String = "DVC") -> Device {
    Device(mac: mac, ip: ip, mask: "255.255.255.0", gateway: "192.168.1.1", dns1: "192.168.1.1", dns2: "8.8.8.8",
           name: name, serial: "S", firmware: "1.5", model: "DCN-TEST", vendor: "DVC", http: 80, rtsp: 554,
           build: "2026-01-01", src: nil)
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

    @Test func factoryFreshCamerasFallBackToMACOrder() {
        let fresh = [camera("02:00:00:00:00:0b"), camera("02:00:00:00:00:0a"), camera("02:00:00:00:00:0c")]
        for order in [CameraOrder.mac, .name, .address] {
            #expect(fresh.sorted(by: order).map(\.mac) == ["02:00:00:00:00:0a", "02:00:00:00:00:0b", "02:00:00:00:00:0c"])
        }
    }
}
