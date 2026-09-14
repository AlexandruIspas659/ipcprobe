import SwiftUI

struct ContentView: View {
    @EnvironmentObject private var model: AppModel
    @State private var showSetSheet = false

    var body: some View {
        HSplitView {
            deviceTable
                .frame(minWidth: 560)
            DeviceDetail(device: model.selectedDevice, onChangeNetwork: { showSetSheet = true })
                .frame(minWidth: 300, idealWidth: 340, maxWidth: 420)
        }
        .toolbar { toolbarContent }
        .safeAreaInset(edge: .bottom) { statusBar }
        .sheet(isPresented: $showSetSheet) {
            if let d = model.selectedDevice {
                SetNetworkSheet(device: d)
                    .environmentObject(model)
            }
        }
    }

    private var deviceTable: some View {
        Table(model.devices, selection: $model.selection) {
            TableColumn("IP", value: \.ip).width(min: 110, ideal: 120)
            TableColumn("MAC", value: \.mac).width(min: 130, ideal: 140)
            TableColumn("Name", value: \.name).width(min: 100)
            TableColumn("Model", value: \.model).width(min: 110)
            TableColumn("Firmware", value: \.firmware).width(min: 90)
            TableColumn("Mask", value: \.mask).width(min: 110)
            TableColumn("Gateway", value: \.gateway).width(min: 110)
        }
        .font(.system(.body, design: .monospaced))
        .overlay {
            if model.devices.isEmpty && !model.isScanning {
                emptyState
            }
        }
    }

    private var emptyState: some View {
        VStack(spacing: 8) {
            Image(systemName: "video.slash").font(.largeTitle).foregroundStyle(.secondary)
            Text("No cameras found").font(.title3)
            Text("Cameras announce themselves by link-layer multicast, so this Mac must be on the cameras' own network segment (not over a VPN). If a scan shows nothing on a network that has cameras, check System Settings › Privacy & Security › Local Network and make sure ipcprobe is allowed.")
                .font(.callout).foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .frame(maxWidth: 440)
        }
        .padding()
    }

    @ToolbarContentBuilder
    private var toolbarContent: some ToolbarContent {
        ToolbarItemGroup(placement: .principal) {
            Picker("Interface", selection: $model.selectedInterface) {
                Text("All interfaces").tag(String?.none)
                ForEach(model.interfaces) { i in
                    Text(i.label).tag(String?.some(i.ip))
                }
            }
            .frame(minWidth: 220)
            .help("Which network interface to listen and send on. Only interfaces with an IPv4 address and multicast are listed; VPN tunnels never are.")

            Picker("Listen", selection: $model.timeout) {
                Text("2 s").tag(2.0)
                Text("5 s").tag(5.0)
                Text("10 s").tag(10.0)
            }
            .pickerStyle(.segmented)
            .help("How long to collect announcements. Cameras announce every few seconds; 5 s catches all of them.")
        }
        ToolbarItem(placement: .primaryAction) {
            Button {
                Task { await model.scan() }
            } label: {
                if model.isScanning {
                    ProgressView().controlSize(.small).frame(width: 16)
                } else {
                    Label("Rescan", systemImage: "arrow.clockwise")
                }
            }
            .disabled(model.isScanning)
            .keyboardShortcut("r", modifiers: .command)
        }
    }

    private var statusBar: some View {
        HStack(spacing: 12) {
            if let e = model.error {
                Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.yellow)
                Text(e).lineLimit(2).textSelection(.enabled)
            } else if model.isScanning {
                Text("Listening for \(Int(model.timeout)) s…")
            } else if let t = model.lastScan {
                Text("\(model.devices.count) device\(model.devices.count == 1 ? "" : "s") · scanned \(t.formatted(date: .omitted, time: .standard))")
            } else {
                Text("Ready")
            }
            Spacer()
            Text("helper \(model.helperVersion)").foregroundStyle(.secondary)
        }
        .font(.callout)
        .padding(.horizontal, 12)
        .padding(.vertical, 6)
        .background(.bar)
    }
}
