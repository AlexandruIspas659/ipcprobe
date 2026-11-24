import SwiftUI
import AppKit

/// The right-hand pane: what is selected, and what can be done with it.
///
/// Three states — one camera, several, none — built from the same few parts: a header with an icon tile, status
/// badges, a row of quick-action tiles, and cards of label / value rows. Labels are in the system font and only the
/// values (addresses, serials) are monospaced, so the pane reads as an inspector rather than a terminal.
struct DeviceDetail: View {
    @EnvironmentObject private var model: AppModel
    /// The value whose copy button was just pressed; it shows a tick for a moment.
    @State private var copied: String? = nil

    var body: some View {
        Group {
            if let d = model.selectedDevice {
                ScrollView { one(d).padding(16) }
            } else if model.selectedDevices.count > 1 {
                ScrollView { several(model.selectedDevices).padding(16) }
            } else {
                none
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color(nsColor: .windowBackgroundColor))
    }

    // MARK: one camera

    private func one(_ d: Device) -> some View {
        let sharing = model.devices.sharedAddresses[d.ip]
        return VStack(alignment: .leading, spacing: 16) {
            header(icon: "video.fill", tint: d.isForeign ? .orange : .accentColor,
                   title: d.name.isEmpty ? d.model : d.name,
                   subtitle: [d.model, d.vendor].filter { !$0.isEmpty }.joined(separator: " · "))

            if let n = sharing {
                badge("Shares its address with \(n - 1) other camera\(n == 2 ? "" : "s")", "square.on.square", .yellow)
            }

            if d.isForeign {
                callout("Outside this Mac's subnet",
                        "It can be configured from here, but its web page and video will not open until it has an address on this network.",
                        button: "Bring into This Network…") { model.rescue() }
            }

            HStack(spacing: 8) {
                tile("Web Page", "safari", disabled: d.webURL == nil || d.isForeign,
                     help: d.isForeign ? "Not reachable by IP from this Mac yet." : "Open the camera's own web page in the browser.") {
                    if let u = d.webURL { NSWorkspace.shared.open(u) }
                }
                tile(copied == d.rtspURL ? "Copied" : "Copy RTSP", copied == d.rtspURL ? "checkmark" : "play.rectangle",
                     disabled: d.rtspURL == nil, help: "Copy the address of the video stream, for a recorder or a player.") {
                    if let r = d.rtspURL { copy(r) }
                }
                tile("Change Network", "network", prominent: !d.isForeign,
                     help: "Give the camera a different address, mask, gateway or DNS (⌘E).") {
                    model.changeNetwork()
                }
            }

            card("Network") {
                row("IP address", d.ip, copyable: true)
                row("Subnet mask", d.mask)
                row("Gateway", d.gateway)
                row("DNS", [d.dns1, d.dns2].filter { !$0.isEmpty && $0 != "0.0.0.0" }.joined(separator: ", "))
                row("MAC address", d.mac, copyable: true, last: true)
            }

            card("Device") {
                row("Model", d.model)
                row("Firmware", d.firmware)
                if !d.build.isEmpty { row("Built", d.build) }
                row("Serial", d.serial, copyable: true)
                row("Web port", d.http == 0 ? "" : String(d.http))
                row("RTSP port", d.rtsp == 0 ? "" : String(d.rtsp), last: d.src == nil || d.src == d.ip)
                // Normally a camera announces from its own address; when it does not, that is worth seeing.
                if let s = d.src, s != d.ip { row("Heard from", s, last: true) }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    // MARK: several cameras

    private func several(_ ds: [Device]) -> some View {
        let stranded = ds.foreign.count
        let subnets = Set(ds.compactMap(\.subnet)).count, models = Set(ds.map(\.model)).count
        return VStack(alignment: .leading, spacing: 16) {
            header(icon: "rectangle.stack.fill", tint: .accentColor, title: "\(ds.count) cameras selected",
                   subtitle: "\(models) model\(models == 1 ? "" : "s") · \(subnets) subnet\(subnets == 1 ? "" : "s")")

            if stranded > 0 {
                callout("\(stranded) outside this Mac's subnet",
                        "\(stranded == 1 ? "It" : "They") cannot be reached by IP until \(stranded == 1 ? "it has an address" : "they have addresses") on this network.",
                        button: "Bring \(stranded) into This Network…") { model.rescue() }
            }

            HStack(spacing: 8) {
                tile(copied == "list" ? "Copied" : "Copy List", copied == "list" ? "checkmark" : "doc.on.doc",
                     help: "Copy the selected cameras as a table, to paste into a spreadsheet (⇧⌘C).") {
                    Export.copy(Export.deviceRows(ds))
                    flash("list")
                }
                tile("Export", "square.and.arrow.up", help: "Save the selected cameras as an Excel workbook or a CSV file (⇧⌘E).") {
                    Export.save(Export.deviceRows(ds), name: Export.fileName("cameras"), sheet: "Cameras")
                }
                tile("Change Network", "network", prominent: true,
                     help: "Change all \(ds.count) together with one admin password (⌘E).") {
                    model.changeNetwork()
                }
            }

            card("Selected, in table order") {
                ForEach(Array(ds.enumerated()), id: \.element.mac) { i, d in
                    VStack(spacing: 0) {
                        HStack(alignment: .firstTextBaseline, spacing: 8) {
                            Text("\(i + 1)").foregroundStyle(.tertiary).frame(width: 20, alignment: .trailing)
                            Text(d.name.isEmpty ? d.model : d.name).lineLimit(1)
                            if d.isForeign {
                                Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.orange).font(.caption)
                            }
                            Spacer(minLength: 8)
                            Text(d.ip).font(.system(.callout, design: .monospaced)).foregroundStyle(.secondary)
                        }
                        .font(.callout)
                        .padding(.horizontal, 12).padding(.vertical, 7)
                        if i < ds.count - 1 { Divider().padding(.leading, 12) }
                    }
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    // MARK: nothing selected

    private var none: some View {
        VStack(spacing: 0) {
            Spacer()
            Image(systemName: "video").font(.system(size: 34, weight: .light)).foregroundStyle(.tertiary)
            Text("No camera selected").font(.title3.weight(.medium)).padding(.top, 12)
            Text("Select one to see its details, or several to change them together.")
                .font(.callout).foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.top, 4).padding(.horizontal, 28)
            Spacer()
            // The other way in: who gets which address, decided in a spreadsheet.
            VStack(alignment: .leading, spacing: 8) {
                Label("Addresses decided in a spreadsheet?", systemImage: "tablecells").font(.callout.weight(.medium))
                Text("Save a template with these cameras in it, type the new addresses in, and import it.")
                    .font(.callout).foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                HStack(spacing: 8) {
                    Button("Save Template…") { model.savePlanTemplate() }
                        .disabled(model.exportDevices.isEmpty)
                        .help("Save a spreadsheet listing the cameras shown, with empty columns for their new settings (⇧⌘T).")
                    Button("Import Plan…") { model.importPlan() }
                        .help("Apply a filled-in plan: one camera per row, with its MAC and new address (⇧⌘I).")
                }
            }
            .padding(12)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: 10).fill(Color.primary.opacity(0.06)))
            .padding(16)
        }
    }

    // MARK: parts

    private func header(icon: String, tint: Color, title: String, subtitle: String) -> some View {
        HStack(spacing: 12) {
            Image(systemName: icon)
                .font(.system(size: 19, weight: .medium))
                .foregroundStyle(.white)
                .frame(width: 44, height: 44)
                .background(RoundedRectangle(cornerRadius: 11).fill(tint.gradient))
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.title3.weight(.semibold)).lineLimit(2).textSelection(.enabled)
                Text(subtitle).font(.callout).foregroundStyle(.secondary).lineLimit(1)
            }
            Spacer(minLength: 0)
        }
    }

    private func badge(_ text: String, _ icon: String, _ color: Color) -> some View {
        Label(text, systemImage: icon)
            .font(.caption.weight(.medium))
            .padding(.horizontal, 8).padding(.vertical, 3)
            .foregroundStyle(color)
            .background(Capsule().fill(color.opacity(0.16)))
    }

    /// Something that needs doing, with the button that does it.
    private func callout(_ title: String, _ text: String, button: String, action: @escaping () -> Void) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Label(title, systemImage: "exclamationmark.triangle.fill")
                .font(.callout.weight(.semibold)).foregroundStyle(.orange)
            Text(text).font(.callout).fixedSize(horizontal: false, vertical: true)
            Button(button, action: action).buttonStyle(.borderedProminent).tint(.orange)
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 10).fill(Color.orange.opacity(0.13)))
    }

    /// A quick action: an icon over a short caption, as wide as its share of the row.
    private func tile(_ title: String, _ icon: String, prominent: Bool = false, disabled: Bool = false,
                      help: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            VStack(spacing: 5) {
                Image(systemName: icon).font(.system(size: 17, weight: .medium)).frame(height: 20)
                Text(title).font(.caption.weight(.medium)).lineLimit(1).minimumScaleFactor(0.75)
            }
            .padding(.vertical, 9).padding(.horizontal, 4)
            .frame(maxWidth: .infinity)
            .foregroundStyle(prominent ? Color.white : Color.accentColor)
            .background(RoundedRectangle(cornerRadius: 10)
                .fill(prominent ? AnyShapeStyle(Color.accentColor) : AnyShapeStyle(Color.primary.opacity(0.07))))
            .contentShape(RoundedRectangle(cornerRadius: 10))
        }
        .buttonStyle(.plain)
        .disabled(disabled)
        .opacity(disabled ? 0.4 : 1)
        .help(help)
    }

    private func card<Content: View>(_ title: String, @ViewBuilder _ content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title.uppercased()).font(.caption.weight(.semibold)).foregroundStyle(.secondary).padding(.leading, 4)
            VStack(spacing: 0) { content() }
                .background(RoundedRectangle(cornerRadius: 10).fill(Color.primary.opacity(0.06)))
        }
    }

    private func row(_ label: String, _ value: String, copyable: Bool = false, last: Bool = false) -> some View {
        VStack(spacing: 0) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(label).foregroundStyle(.secondary)
                Spacer(minLength: 8)
                Text(value.isEmpty ? "—" : value)
                    .font(.system(.callout, design: .monospaced))
                    .multilineTextAlignment(.trailing)
                    .textSelection(.enabled)
                if copyable && !value.isEmpty {
                    Button {
                        copy(value)
                    } label: {
                        Image(systemName: copied == value ? "checkmark" : "doc.on.doc")
                            .font(.caption)
                            .foregroundStyle(copied == value ? Color.green : Color.secondary)
                            .frame(width: 14)
                    }
                    .buttonStyle(.plain)
                    .help("Copy \(label.lowercased())")
                }
            }
            .font(.callout)
            .padding(.horizontal, 12).padding(.vertical, 6)
            if !last { Divider().padding(.leading, 12) }
        }
    }

    private func copy(_ s: String) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(s, forType: .string)
        flash(s)
    }

    /// Shows the tick on whatever was just copied, briefly.
    private func flash(_ what: String) {
        copied = what
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.2) {
            if copied == what { copied = nil }
        }
    }
}
