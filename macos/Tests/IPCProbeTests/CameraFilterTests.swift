import Testing
@testable import IPCProbe

@Suite struct CameraFilterCriteriaTests {
    static func cam(_ n: Int, ip: String, mask: String = "255.255.255.0", name: String = "DVC", model: String = "DCN-A",
                    firmware: String = "1.5", foreign: Bool = false) -> Device {
        var d = camera(String(format: "02:00:00:00:00:%02x", n), ip: ip, name: name)
        d.mask = mask; d.model = model; d.firmware = firmware; d.foreign = foreign
        return d
    }

    let cams = [
        cam(1, ip: "192.168.1.69", name: "Lobby North", model: "DCN-A", firmware: "1.5"),
        cam(2, ip: "192.168.1.70", name: "Lobby South", model: "DCN-B", firmware: "1.4"),
        cam(3, ip: "192.168.1.88"),                                   // factory fresh, shares .88
        cam(4, ip: "192.168.1.88", model: "DCN-B"),
        cam(5, ip: "10.0.0.5", mask: "255.0.0.0", name: "Gate", firmware: "1.4", foreign: true),
    ]

    func shown(_ f: CameraFilter) -> [Int] { f.apply(to: cams).map { Int($0.mac.suffix(2), radix: 16)! } }

    @Test func anEmptyFilterShowsEverything() {
        #expect(CameraFilter().isEmpty)
        #expect(shown(CameraFilter()) == [1, 2, 3, 4, 5])
        #expect(CameraFilter(text: "   ").isEmpty)
        #expect(!CameraFilter(unnamed: true).isEmpty)
    }

    @Test func statusCriteria() {
        #expect(shown(CameraFilter(foreign: true)) == [5])
        #expect(shown(CameraFilter(sharedAddress: true)) == [3, 4])
        #expect(shown(CameraFilter(unnamed: true)) == [3, 4])
    }

    @Test func valuesOfOneKindAreAlternatives_KindsCombine() {
        #expect(shown(CameraFilter(models: ["DCN-B"])) == [2, 4])
        #expect(shown(CameraFilter(models: ["DCN-A", "DCN-B"])) == [1, 2, 3, 4, 5])
        #expect(shown(CameraFilter(models: ["DCN-B"], firmwares: ["1.4"])) == [2])       // model AND firmware
        #expect(shown(CameraFilter(firmwares: ["1.4"])) == [2, 5])
        #expect(shown(CameraFilter(subnets: ["10.0.0.0/8"])) == [5])
        #expect(shown(CameraFilter(subnets: ["192.168.1.0/24", "10.0.0.0/8"])) == [1, 2, 3, 4, 5])
    }

    @Test func typedWordsCombineWithPickedCriteria() {
        #expect(shown(CameraFilter(text: "lobby", models: ["DCN-B"])) == [2])
        #expect(shown(CameraFilter(text: "lobby", foreign: true)).isEmpty)
    }

    @Test func sharedAddressesAreCountedOverAllCameras() {
        // Typing ".88 :03" leaves one camera showing; it still counts as sharing its address with the hidden one.
        #expect(shown(CameraFilter(text: "00:03", sharedAddress: true)) == [3])
    }

    @Test func chipsListWhatIsPickedAndRemoveIt() {
        var f = CameraFilter(text: "lobby", foreign: true, models: ["DCN-B", "DCN-A"], subnets: ["10.0.0.0/8"])
        #expect(f.chips.map(\.label) == ["Outside this subnet", "Model DCN-A", "Model DCN-B", "Subnet 10.0.0.0/8"])
        f.remove(f.chips[2])
        #expect(f.models == ["DCN-A"])
        for chip in f.chips { f.remove(chip) }
        #expect(!f.hasCriteria && f.text == "lobby")       // the typed text is not a chip and stays
    }

    @Test func facetsCountWhatIsOnTheNetwork() {
        let f = cams.facets()
        #expect(f.foreign == 1 && f.sharedAddress == 2 && f.unnamed == 2)
        #expect(f.models == [.init(value: "DCN-A", count: 3), .init(value: "DCN-B", count: 2)])
        #expect(f.firmwares == [.init(value: "1.4", count: 2), .init(value: "1.5", count: 3)])
        #expect(f.subnets == [.init(value: "10.0.0.0/8", count: 1), .init(value: "192.168.1.0/24", count: 4)])
        // A picked value no camera has any more is still listed, so it can be switched off.
        #expect(cams.facets(keeping: CameraFilter(models: ["DCN-GONE"])).models.last == .init(value: "DCN-GONE", count: 0))
    }

    @Test func subnetOfACamera() {
        #expect(Self.cam(1, ip: "192.168.1.69").subnet == "192.168.1.0/24")
        #expect(Self.cam(1, ip: "10.20.30.40", mask: "255.255.0.0").subnet == "10.20.0.0/16")
        #expect(Self.cam(1, ip: "10.20.30.40", mask: "255.255.255.252").subnet == "10.20.30.40/30")
        #expect(Self.cam(1, ip: "10.20.30.40", mask: "255.0.255.0").subnet == nil)       // not a netmask
        #expect(Self.cam(1, ip: "nonsense").subnet == nil)
    }

    @Test func unnamedMeansEmptyOrJustTheBrand() {
        #expect(Self.cam(1, ip: "1.1.1.1", name: "DVC").isUnnamed)       // vendor in these fixtures is "DVC"
        #expect(Self.cam(1, ip: "1.1.1.1", name: "dvc ").isUnnamed)
        #expect(Self.cam(1, ip: "1.1.1.1", name: "").isUnnamed)
        #expect(!Self.cam(1, ip: "1.1.1.1", name: "Lobby").isUnnamed)
    }
}
