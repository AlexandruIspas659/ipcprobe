import SwiftUI

/// The "change several cameras" form. Mirrors `ipcprobe apply`: the helper works out each camera's target, checks
/// for address clashes, sends, and confirms; this sheet collects the fields, shows the helper's own preview
/// (`--dry-run`), and only then lets the change go out.
///
/// Whatever is left empty is kept from each camera's current configuration, which is what makes one form serve both
/// uses: tick "assign consecutive addresses" to number a row of new cameras, or leave it off and fill in just a
/// gateway or DNS server to change those in place.
struct BatchSheet: View {
    let devices: [Device]
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss

    @State private var assignIPs = false
    @State private var startIP = ""
    @State private var step = "1"
    @State private var mask = ""
    @State private var gateway = ""
    @State private var dns1 = ""
    @State private var dns2 = ""
    @State private var password = ""
    @State private var canary = true

    private enum Phase: Equatable { case editing, previewing, running, done(Int32) }
    @State private var phase: Phase = .editing
    @State private var items: [BatchItem] = []
    /// The options the rows in `items` were previewed with. Apply is only offered while the form still says the same.
    @State private var previewedOptions: [String]? = nil
    @State private var log: [String] = []
    @State private var problem: String? = nil

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("Change network of \(devices.count) cameras").font(.title3).bold()
            Text("Empty fields keep each camera's current value.").foregroundStyle(.secondary)

            form
            rows
            if !log.isEmpty { logView }

            HStack {
                verdict
                Spacer()
                buttons
            }
        }
        .padding(20)
        .frame(width: 760)
        .onAppear {
            // Cameras fresh out of the box all sit on one address: numbering them is almost certainly the job.
            let addresses = devices.map(\.ip)
            assignIPs = Set(addresses).count < addresses.count
        }
    }

    // MARK: form

    private var form: some View {
        Grid(alignment: .leading, horizontalSpacing: 12, verticalSpacing: 8) {
            GridRow {
                Text("Addresses").gridColumnAlignment(.trailing)
                HStack(spacing: 8) {
                    Toggle("Assign consecutive addresses, starting at", isOn: $assignIPs)
                    TextField("", text: $startIP, prompt: Text("first address"))
                        .font(.system(.body, design: .monospaced))
                        .frame(width: 150)
                        .disabled(!assignIPs)
                    Text("step").foregroundStyle(.secondary)
                    TextField("", text: $step)
                        .font(.system(.body, design: .monospaced))
                        .frame(width: 44)
                        .disabled(!assignIPs)
                }
            }
            fieldRow("Subnet mask", $mask)
            fieldRow("Gateway", $gateway)
            fieldRow("DNS 1", $dns1)
            fieldRow("DNS 2", $dns2)
            GridRow {
                Text("Admin password").gridColumnAlignment(.trailing)
                SecureField("", text: $password, prompt: Text("the same password is used for every camera"))
                    .font(.system(.body, design: .monospaced))
            }
            GridRow {
                Color.clear.frame(width: 0, height: 0)
                Toggle("Try the first camera on its own, and stop if it does not confirm", isOn: $canary)
                    .help("A wrong password then reaches one camera instead of all of them.")
            }
        }
        .disabled(phase == .previewing || phase == .running)
        .textFieldStyle(.roundedBorder)
    }

    private func fieldRow(_ label: String, _ text: Binding<String>) -> some View {
        GridRow {
            Text(label).gridColumnAlignment(.trailing)
            TextField("", text: text, prompt: Text("keep each camera's current"))
                .font(.system(.body, design: .monospaced))
                .autocorrectionDisabled()
        }
    }

    // MARK: per-camera rows

    private var rows: some View {
        ScrollView {
            Grid(alignment: .leading, horizontalSpacing: 14, verticalSpacing: 5) {
                GridRow {
                    Text("Camera"); Text("Address"); Text("Other changes"); Text("Status")
                }
                .font(.caption).foregroundStyle(.secondary)
                Divider()
                if items.isEmpty {
                    ForEach(devices) { d in
                        GridRow {
                            cameraCell(name: d.name.isEmpty ? d.model : d.name, mac: d.mac)
                            Text(d.ip)
                            Text("")
                            Text("Preview to see the changes").foregroundStyle(.secondary)
                        }
                    }
                } else {
                    ForEach(Array(items.enumerated()), id: \.offset) { _, it in
                        GridRow(alignment: .top) {
                            cameraCell(name: it.name.isEmpty ? it.model : it.name, mac: it.mac)
                            Text(addressText(it))
                            Text(otherChanges(it))
                            statusCell(it)
                        }
                        ForEach(notes(it), id: \.self) { note in
                            GridRow {
                                Color.clear.frame(width: 0, height: 0)
                                Text(note).font(.caption).foregroundStyle(.secondary)
                                    .fixedSize(horizontal: false, vertical: true)
                                    .gridCellColumns(3)
                            }
                        }
                    }
                }
            }
            .font(.system(.callout, design: .monospaced))
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(8)
            .opacity(previewIsCurrent || items.isEmpty || phase != .editing ? 1 : 0.45)
        }
        .frame(height: 190)
        .background(Color(nsColor: .textBackgroundColor))
        .clipShape(RoundedRectangle(cornerRadius: 6))
    }

    private func cameraCell(name: String, mac: String) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(name.isEmpty ? "—" : name)
            Text(mac).font(.caption).foregroundStyle(.secondary)
        }
    }

    private func addressText(_ it: BatchItem) -> String {
        guard let cur = it.current else { return "?" }
        guard let tgt = it.target, tgt.ip != cur.ip else { return cur.ip }
        return "\(cur.ip) → \(tgt.ip)"
    }

    private func otherChanges(_ it: BatchItem) -> String {
        guard let tgt = it.target else { return "" }
        let value: [String: String] = ["mask": tgt.mask, "gateway": tgt.gateway, "dns1": tgt.dns1, "dns2": tgt.dns2]
        let parts = it.changes.filter { $0 != "ip" }.map { "\($0) → \(value[$0] ?? "?")" }
        return parts.isEmpty ? "—" : parts.joined(separator: "\n")
    }

    private func notes(_ it: BatchItem) -> [String] {
        it.warnings.map { "Warning: \($0)" } + (it.error.map { [$0] } ?? [])
    }

    @ViewBuilder
    private func statusCell(_ it: BatchItem) -> some View {
        switch it.status {
        case "planned":
            Label("will change", systemImage: "arrow.right.circle").foregroundStyle(.blue)
        case "unchanged":
            Label("already set", systemImage: "equal.circle").foregroundStyle(.secondary)
        case "confirmed":
            Label("confirmed", systemImage: "checkmark.circle.fill").foregroundStyle(.green)
        case "not-confirmed":
            Label(it.acked ? "not confirmed (acked)" : "not confirmed", systemImage: "questionmark.circle.fill")
                .foregroundStyle(.orange)
        case "not-sent":
            Label("not sent", systemImage: "minus.circle").foregroundStyle(.secondary)
        case "not-seen":
            Label("not visible", systemImage: "eye.slash").foregroundStyle(.orange)
        default:
            Label("cannot apply", systemImage: "xmark.octagon.fill").foregroundStyle(.red)
        }
    }

    // MARK: log, verdict, buttons

    private var logView: some View {
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
            .frame(height: 110)
            .background(Color(nsColor: .textBackgroundColor))
            .clipShape(RoundedRectangle(cornerRadius: 6))
            .onChange(of: log.count) { _ in proxy.scrollTo("end") }
        }
    }

    @ViewBuilder
    private var verdict: some View {
        if let p = problem {
            Label(p, systemImage: "xmark.octagon.fill").foregroundStyle(.red).lineLimit(3)
        } else if case .done(let status) = phase {
            let confirmed = count("confirmed"), total = items.count - count("unchanged")
            if status == 0 {
                Label("Confirmed — \(confirmed) camera\(confirmed == 1 ? "" : "s") announce the new configuration.",
                      systemImage: "checkmark.circle.fill").foregroundStyle(.green)
            } else {
                Label("\(confirmed) of \(total) confirmed — see each camera's status.",
                      systemImage: "questionmark.circle.fill").foregroundStyle(.orange)
            }
        } else if phase == .editing, previewIsCurrent {
            if count("invalid") > 0 {
                Label("Nothing can be sent until the cameras marked in red are sorted out.",
                      systemImage: "xmark.octagon.fill").foregroundStyle(.red)
            } else if count("planned") == 0 {
                Label("Nothing to change.", systemImage: "equal.circle").foregroundStyle(.secondary)
            } else {
                Text("\(count("planned")) camera\(count("planned") == 1 ? "" : "s") will be changed.")
                    .foregroundStyle(.secondary)
            }
        }
    }

    @ViewBuilder
    private var buttons: some View {
        switch phase {
        case .editing:
            Button("Cancel") { dismiss() }.keyboardShortcut(.cancelAction)
            if canApply {
                Button("Preview", action: preview)
                Button("Apply to \(count("planned")) camera\(count("planned") == 1 ? "" : "s")", action: apply)
                    .buttonStyle(.borderedProminent)
                    .keyboardShortcut(.defaultAction)
                    .disabled(!passwordValid)
            } else {
                Button("Preview", action: preview)
                    .buttonStyle(.borderedProminent)
                    .keyboardShortcut(.defaultAction)
                    .disabled(!formValid)
            }
        case .previewing:
            ProgressView().controlSize(.small)
            Text("Looking at the cameras…").foregroundStyle(.secondary)
        case .running:
            ProgressView().controlSize(.small)
            Text("Waiting for the cameras…").foregroundStyle(.secondary)
        case .done(let status):
            if status != 0 {
                Button("Try again") { phase = .editing; previewedOptions = nil }
            }
            Button("Close") {
                dismiss()
                Task { await model.scan() }
            }
            .keyboardShortcut(.defaultAction)
        }
    }

    // MARK: state

    private func count(_ status: String) -> Int { items.filter { $0.status == status }.count }

    /// The helper flags the form currently describes (without `--dry-run` / `--yes`).
    private var options: [String] {
        var o: [String] = []
        if assignIPs {
            o += ["--start-ip", startIP]
            if step != "1" { o += ["--step", step] }
        }
        if !mask.isEmpty { o += ["--mask", mask] }
        if !gateway.isEmpty { o += ["--gw", gateway] }
        if !dns1.isEmpty { o += ["--dns1", dns1] }
        if !dns2.isEmpty { o += ["--dns2", dns2] }
        return o
    }

    private var formValid: Bool {
        let optional = [mask, gateway, dns1, dns2]
        guard optional.allSatisfy({ $0.isEmpty || isIPv4($0) }) else { return false }
        if assignIPs {
            guard isIPv4(startIP), let n = Int(step), n >= 1 else { return false }
            return true
        }
        return optional.contains { !$0.isEmpty }
    }

    private var passwordValid: Bool { !password.isEmpty && password.utf8.count <= 21 }
    private var previewIsCurrent: Bool { previewedOptions == options }

    /// Apply is offered only for a preview of exactly what the form says now, with something to do and nothing wrong.
    private var canApply: Bool {
        formValid && previewIsCurrent && count("planned") > 0 && count("invalid") == 0
    }

    private func isIPv4(_ s: String) -> Bool {
        let parts = s.split(separator: ".", omittingEmptySubsequences: false)
        return parts.count == 4 && parts.allSatisfy { p in
            if let n = Int(p), (0...255).contains(n), String(n) == p { return true }
            return false
        }
    }

    private func preview() {
        guard formValid, phase == .editing else { return }
        let opts = options
        phase = .previewing
        problem = nil
        log = []
        Task { @MainActor in
            let run = await model.applyBatch(macs: devices.map(\.mac), options: opts + ["--dry-run"], password: nil) { _ in }
            if let out = run.output {
                items = out.devices
                previewedOptions = opts
            } else {
                items = []
                previewedOptions = nil
                problem = run.message
            }
            phase = .editing
        }
    }

    private func apply() {
        guard canApply, passwordValid, phase == .editing else { return }
        let opts = options + ["--yes"] + (canary ? [] : ["--no-canary"])
        let pw = password
        password = ""          // the sheet keeps the password only as long as the helper needs it
        phase = .running
        problem = nil
        log = []
        Task { @MainActor in
            let run = await model.applyBatch(macs: devices.map(\.mac), options: opts, password: pw) { line in
                log.append(line)
            }
            if let out = run.output {
                items = out.devices
            } else {
                problem = run.message
            }
            phase = .done(run.status)
        }
    }
}
