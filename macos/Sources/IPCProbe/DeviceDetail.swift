import SwiftUI
import AppKit

struct DeviceDetail: View {
    /// The one selected camera, or nil when none or several are selected.
    var device: Device?
    /// Every selected camera, in table order.
    var selected: [Device] = []
    var onChangeNetwork: () -> Void
    var onChangeBatch: () -> Void = {}
    var onRescue: () -> Void = {}
    var onImportPlan: () -> Void = {}

    var body: some View {
        Group {
            if let d = device {
                details(d)
            } else if selected.count > 1 {
                several(selected)
            } else {
                VStack(spacing: 10) {
                    Spacer()
                    Text("Select a camera").foregroundStyle(.secondary)
                    Text("Select several to change them together.").font(.callout).foregroundStyle(.tertiary)
                    Spacer()
                    // The other way in: who gets which address, decided in a spreadsheet.
                    Button("Import Plan…", action: onImportPlan)
                        .help("Apply a plan from a spreadsheet saved as CSV: one camera per row, with its MAC and new address (⇧⌘I).")
                    Text("Addresses decided in a spreadsheet? Export the camera list, type them in, import it here.")
                        .font(.caption).foregroundStyle(.tertiary)
                        .multilineTextAlignment(.center)
                        .fixedSize(horizontal: false, vertical: true)
                        .padding(.horizontal, 20).padding(.bottom, 28)
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
                    if d.isForeign {
                        Text("Outside this Mac's subnet: the web page and video will not open until its address is changed.")
                            .font(.callout).foregroundStyle(.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                        Button("Bring into This Network…", action: onRescue)
                            .buttonStyle(.borderedProminent)
                        Button("Change network…", action: onChangeNetwork)
                    } else {
                        Button("Change network…", action: onChangeNetwork)
                            .buttonStyle(.borderedProminent)
                    }
                }
            }
            .padding(16)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .font(.system(.body, design: .monospaced))
    }

    /// Several cameras selected: what they are, and the way into the batch sheet.
    private func several(_ ds: [Device]) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("\(ds.count) cameras selected").font(.title2).bold()
                    Text("Change them together with one admin password.").foregroundStyle(.secondary)
                }
                section("Actions") {
                    Button("Change network of \(ds.count) cameras…", action: onChangeBatch)
                        .buttonStyle(.borderedProminent)
                    if !ds.foreign.isEmpty {
                        Button("Bring \(ds.foreign.count) into This Network…", action: onRescue)
                            .help("The selected cameras that are outside this Mac's subnet each get a free address in it.")
                    }
                }
                section("Selected, in table order") {
                    ForEach(ds) { d in
                        HStack(alignment: .top) {
                            Text(d.ip).frame(width: 120, alignment: .leading)
                            Text(d.name.isEmpty ? d.model : d.name).foregroundStyle(.secondary)
                        }
                        .font(.system(.callout, design: .monospaced))
                    }
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
