import SwiftUI
import AppKit

struct DeviceDetail: View {
    var device: Device?
    var onChangeNetwork: () -> Void

    var body: some View {
        Group {
            if let d = device {
                details(d)
            } else {
                VStack {
                    Spacer()
                    Text("Select a camera").foregroundStyle(.secondary)
                    Spacer()
                }
                .frame(maxWidth: .infinity)
            }
        }
        .background(Color(nsColor: .controlBackgroundColor))
    }

    private func details(_ d: Device) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(d.name.isEmpty ? d.model : d.name).font(.title2).bold()
                    Text(d.model + (d.vendor.isEmpty ? "" : " · \(d.vendor)")).foregroundStyle(.secondary)
                }

                section("Network") {
                    row("IP", d.ip); row("Mask", d.mask); row("Gateway", d.gateway)
                    row("DNS", [d.dns1, d.dns2].filter { !$0.isEmpty && $0 != "0.0.0.0" }.joined(separator: ", "))
                    row("MAC", d.mac)
                    if let s = d.src { row("Heard from", s) }
                }

                section("Device") {
                    row("Firmware", d.firmware); row("Build", d.build); row("Serial", d.serial)
                    row("HTTP port", String(d.http)); row("RTSP port", String(d.rtsp))
                }

                section("Actions") {
                    HStack {
                        if let u = d.webURL {
                            Button("Open web UI") { NSWorkspace.shared.open(u) }
                        }
                        if let r = d.rtspURL {
                            Button("Copy RTSP URL") { copy(r) }
                        }
                    }
                    Button("Change network…", action: onChangeNetwork)
                        .buttonStyle(.borderedProminent)
                }
            }
            .padding(16)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .font(.system(.body, design: .monospaced))
    }

    @ViewBuilder
    private func section<Content: View>(_ title: String, @ViewBuilder _ content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title.uppercased()).font(.caption).foregroundStyle(.secondary)
            content()
        }
    }

    private func row(_ k: String, _ v: String) -> some View {
        HStack(alignment: .top) {
            Text(k).foregroundStyle(.secondary).frame(width: 90, alignment: .leading)
            Text(v.isEmpty ? "—" : v).textSelection(.enabled)
        }
        .font(.system(.callout, design: .monospaced))
    }

    private func copy(_ s: String) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(s, forType: .string)
    }
}
