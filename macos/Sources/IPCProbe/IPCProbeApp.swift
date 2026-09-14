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
        }
    }
}
