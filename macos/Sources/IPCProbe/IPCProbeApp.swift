import SwiftUI

@main
struct IPCProbeApp: App {
    @StateObject private var model = AppModel()

    init() {
        Snapshots.runIfAsked()      // `--snapshots <dir>`: render the screens to PNG and exit (development aid)
    }

    var body: some Scene {
        WindowGroup("ipcprobe") {
            ContentView()
                .environmentObject(model)
                .frame(minWidth: 900, minHeight: 480)
                .task { await model.startup() }
        }
        .commands {
            CommandGroup(after: .toolbar) {
                Button("Rescan") { Task { await model.rescan() } }
                    .keyboardShortcut("r", modifiers: .command)
                    .disabled(model.isScanning)
            }
            CommandMenu("Cameras") {
                Button("Filter…") { model.filterFocusRequests += 1 }
                    .keyboardShortcut("f", modifiers: .command)
                Button("Select All Cameras") { model.selectAll() }
                    .keyboardShortcut("a", modifiers: [.command, .option])
                    .disabled(model.visibleDevices.isEmpty)
                Button(model.selection.count > 1 ? "Change Network of \(model.selection.count) Cameras…" : "Change Network…") {
                    model.changeNetwork()
                }
                .keyboardShortcut("e", modifiers: .command)
                .disabled(model.selection.isEmpty)
                Divider()
                Button("Copy Camera List") { Export.copy(Export.deviceRows(model.devices)) }
                    .keyboardShortcut("c", modifiers: [.command, .shift])
                    .disabled(model.devices.isEmpty)
                Button("Export Camera List as CSV…") {
                    Export.save(Export.deviceRows(model.devices), suggestedName: Export.fileName("cameras"))
                }
                .keyboardShortcut("e", modifiers: [.command, .shift])
                .disabled(model.devices.isEmpty)
                Divider()
                Button("Bring into This Network…") { model.rescue() }
                    .keyboardShortcut("b", modifiers: [.command, .shift])
                    .disabled(model.rescueCandidates.isEmpty)
            }
        }
    }
}
