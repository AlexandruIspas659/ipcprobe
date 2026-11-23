import SwiftUI

@main
struct IPCProbeApp: App {
    @StateObject private var model = AppModel()

    var body: some Scene {
        WindowGroup("ipcprobe") {
            ContentView()
                .environmentObject(model)
                .frame(minWidth: 900, minHeight: 480)
                .task { await model.startup() }
        }
        .commands {
            CommandGroup(after: .toolbar) {
                Button("Rescan") { Task { await model.scan() } }
                    .keyboardShortcut("r", modifiers: .command)
                    .disabled(model.isScanning)
            }
            CommandMenu("Cameras") {
                Button("Select All Cameras") { model.selectAll() }
                    .keyboardShortcut("a", modifiers: [.command, .option])
                    .disabled(model.devices.isEmpty)
                Button(model.selection.count > 1 ? "Change Network of \(model.selection.count) Cameras…" : "Change Network…") {
                    model.changeNetwork()
                }
                .keyboardShortcut("e", modifiers: .command)
                .disabled(model.selection.isEmpty)
                Divider()
                Button("Bring into This Network…") { model.rescue() }
                    .keyboardShortcut("b", modifiers: [.command, .shift])
                    .disabled(model.rescueCandidates.isEmpty)
            }
        }
    }
}
