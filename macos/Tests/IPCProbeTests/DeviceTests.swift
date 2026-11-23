import Testing
@testable import IPCProbe

@Test func ipNumberOrdersNumerically() {
    #expect(Device.ipNumber("192.168.1.9") < Device.ipNumber("192.168.1.134"))
    #expect(Device.ipNumber("not an address") == 0)
}

@Test func urlsOmitDefaultPorts() {
    var d = Device(mac: "02:00:00:00:00:01", ip: "192.168.1.10", mask: "255.255.255.0", gateway: "192.168.1.1",
                   dns1: "192.168.1.1", dns2: "8.8.8.8", name: "lobby", serial: "S", firmware: "1.5",
                   model: "DCN-TEST", vendor: "DVC", http: 80, rtsp: 554, build: "2026-01-01", src: nil)
    #expect(d.webURL?.absoluteString == "http://192.168.1.10/")
    #expect(d.rtspURL == "rtsp://192.168.1.10/")
    d.http = 8080
    d.rtsp = 0
    #expect(d.webURL?.absoluteString == "http://192.168.1.10:8080/")
    #expect(d.rtspURL == nil)
}
