import SwiftUI

/// The "change network" form. Mirrors `ipcprobe set`: the helper does all validation and the confirmation
/// (ack + re-announcement); this sheet collects the fields, hands the password over stdin, and shows progress.
struct SetNetworkSheet: View {
    let device: Device
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss

    @State private var ip = ""
    @State private var mask = ""
    @State private var gateway = ""
    @State private var dns1 = ""
    @State private var dns2 = ""
    @State private var password = ""
    @State private var force = false

    private enum Phase: Equatable { case editing, running, done(Int32) }
    @State private var phase: Phase = .editing
    @State private var log: [String] = []

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("Change network of \(device.name.isEmpty ? device.model : device.name)").font(.title3).bold()
            Text("\(device.mac) · currently \(device.ip)").foregroundStyle(.secondary)
                .font(.system(.callout, design: .monospaced))

            // A plain two-column grid rather than Form: Form's label column plus long labels made the content
            // wider than the sheet, and SwiftUI then cropped both edges.
            Grid(alignment: .leading, horizontalSpacing: 12, verticalSpacing: 8) {
                fieldRow("IP address", $ip)
                fieldRow("Subnet mask", $mask)
                fieldRow("Gateway", $gateway)
                fieldRow("DNS 1", $dns1, prompt: "optional — defaults to the gateway")
                fieldRow("DNS 2", $dns2, prompt: "optional — defaults to 8.8.8.8")
                GridRow {
                    Text("Admin password").gridColumnAlignment(.trailing)
                    SecureField("", text: $password, prompt: Text("the camera's admin password"))
                        .font(.system(.body, design: .monospaced))
                }
                GridRow {
                    Color.clear.frame(width: 0, height: 0)
                    Toggle("Send even if the camera is not currently visible (--force)", isOn: $force)
                }
            }
            .disabled(phase != .editing)
            .textFieldStyle(.roundedBorder)
            .onSubmit(apply)

            if !log.isEmpty {
                ScrollViewReader { proxy in
                    ScrollView {
                        VStack(alignment: .leading, spacing: 2) {
                            ForEach(Array(log.enumerated()), id: \.offset) { _, line in
                                Text(line).textSelection(.enabled)
                            }
                            Color.clear.frame(height: 1).id("end")
                        }
                        .font(.system(.caption, design: .monospaced))
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(8)
                    }
                    .frame(height: 130)
                    .background(Color(nsColor: .textBackgroundColor))
                    .clipShape(RoundedRectangle(cornerRadius: 6))
                    .onChange(of: log.count) { _ in proxy.scrollTo("end") }
                }
            }

            HStack {
                verdict
                Spacer()
                switch phase {
                case .editing:
                    Button("Cancel") { dismiss() }.keyboardShortcut(.cancelAction)
                    Button("Apply", action: apply)
                        .buttonStyle(.borderedProminent)
                        .keyboardShortcut(.defaultAction)
                        .disabled(!formValid)
                case .running:
                    ProgressView().controlSize(.small)
                    Text("Waiting for the camera…").foregroundStyle(.secondary)
                case .done:
                    Button("Close") {
                        dismiss()
                        Task { await model.scan() }
                    }
                    .keyboardShortcut(.defaultAction)
                }
            }
        }
        .padding(20)
        .frame(width: 520)
        .onAppear {
            ip = device.ip; mask = device.mask; gateway = device.gateway
            dns1 = device.dns1 == "0.0.0.0" ? "" : device.dns1
            dns2 = device.dns2 == "0.0.0.0" ? "" : device.dns2
        }
    }

    private func fieldRow(_ label: String, _ text: Binding<String>, prompt: String? = nil) -> some View {
        GridRow {
            Text(label).gridColumnAlignment(.trailing)
            TextField("", text: text, prompt: prompt.map { Text($0) })
                .font(.system(.body, design: .monospaced))
                .autocorrectionDisabled()
        }
    }

    @ViewBuilder
    private var verdict: some View {
        switch phase {
        case .done(0):
            Label("Confirmed — the camera announces at its new address.", systemImage: "checkmark.circle.fill")
                .foregroundStyle(.green)
        case .done(2):
            Label("Not confirmed — wrong password, or the camera is slow to re-announce. Rescan to check.",
                  systemImage: "questionmark.circle.fill").foregroundStyle(.orange)
        case .done:
            Label("Failed — see the log.", systemImage: "xmark.octagon.fill").foregroundStyle(.red)
        default:
            EmptyView()
        }
    }

    private var formValid: Bool {
        isIPv4(ip) && isIPv4(mask) && isIPv4(gateway) && (dns1.isEmpty || isIPv4(dns1)) && (dns2.isEmpty || isIPv4(dns2))
            && !password.isEmpty && password.utf8.count <= 21
    }

    private func isIPv4(_ s: String) -> Bool {
        let parts = s.split(separator: ".", omittingEmptySubsequences: false)
        return parts.count == 4 && parts.allSatisfy { p in
            if let n = Int(p), (0...255).contains(n), String(n) == p { return true }
            return false
        }
    }

    private func apply() {
        guard formValid, phase == .editing else { return }
        phase = .running
        log = []
        let pw = password
        password = ""          // the sheet keeps the password only as long as the helper needs it
        Task { @MainActor in
            let status = await model.setNetwork(mac: device.mac, ip: ip, mask: mask, gateway: gateway,
                                                dns1: dns1, dns2: dns2, password: pw, force: force) { line in
                log.append(line)
            }
            phase = .done(status)
        }
    }
}
