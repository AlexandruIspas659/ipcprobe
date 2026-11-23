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
    /// `.rescue` is the same sheet with less to fill in: the helper picks the addresses and the mask
    /// (`apply --rescue`), and the gateway unless one is given.
    var mode: AppModel.BatchMode = .change
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

    /// The cameras in the order they will be handled. Starts as the table's order; the Order menu and dragging
    /// rows change it. It matters twice: consecutive addresses go top to bottom, and the first camera is the one
    /// tried on its own.
    @State private var ordered: [Device] = []

    private enum Phase: Equatable { case editing, previewing, running, done(Int32) }
    @State private var phase: Phase = .editing
    @State private var items: [BatchItem] = []
    /// What the rows in `items` were previewed for: the options and the camera order. Apply is only offered while
    /// the form still says the same.
    @State private var previewed: [String]? = nil
    @State private var log: [String] = []
    @State private var problem: String? = nil

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text(title).font(.title3).bold()
            Text(mode == .rescue
                 ? "Each camera gets a free address in this Mac's subnet, with its mask. Fill in a field only to override what the cameras already on this network use."
                 : "Empty fields keep each camera's current value.")
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)

            form
            orderBar
            rows
            if !log.isEmpty { logView }

            HStack {
                verdict
                Spacer()
                buttons
            }
        }
        .padding(20)
        .frame(width: 800)
        .onAppear {
            ordered = devices
            // Cameras fresh out of the box all sit on one address: numbering them is almost certainly the job.
            let addresses = devices.map(\.ip)
            assignIPs = Set(addresses).count < addresses.count
        }
    }

    private var title: String {
        let n = devices.count
        switch mode {
        case .change: return "Change network of \(n) cameras"
        case .rescue: return n == 1 ? "Bring 1 camera into this network" : "Bring \(n) cameras into this network"
        }
    }

    // MARK: form

    private var form: some View {
        Grid(alignment: .leading, horizontalSpacing: 12, verticalSpacing: 8) {
            if mode == .rescue {
                fieldRow("Gateway", $gateway, prompt: "the one the cameras on this network use")
                fieldRow("DNS 1", $dns1, prompt: "the gateway")
                fieldRow("DNS 2", $dns2, prompt: "keep each camera's current")
            } else {
                changeFields
            }
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

    @ViewBuilder
    private var changeFields: some View {
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
    }

    private func fieldRow(_ label: String, _ text: Binding<String>,
                          prompt: String = "keep each camera's current") -> some View {
        GridRow {
            Text(label).gridColumnAlignment(.trailing)
            TextField("", text: text, prompt: Text(prompt))
                .font(.system(.body, design: .monospaced))
                .autocorrectionDisabled()
        }
    }

    // MARK: per-camera rows

    private var orderBar: some View {
        HStack(spacing: 10) {
            Menu("Order") {
                Button("As in the Table") { ordered = devices }
                Button("Reverse") { ordered.reverse() }
                Divider()
                Button("By MAC Address") { ordered = ordered.sorted(by: .mac) }
                Button("By Name") { ordered = ordered.sorted(by: .name) }
                Button("By Current Address") { ordered = ordered.sorted(by: .address) }
            }
            .fixedSize()
            Text(assignIPs || mode == .rescue ? "Addresses are handed out top to bottom. Drag rows to reorder."
                           : "The first camera is the one tried on its own. Drag rows to reorder.")
                .font(.callout).foregroundStyle(.secondary)
            Spacer()
        }
        .disabled(phase != .editing)
    }

    private enum Column {
        static let number: CGFloat = 24, camera: CGFloat = 165, other: CGFloat = 150, status: CGFloat = 160
    }

    private var rows: some View {
        VStack(spacing: 0) {
            HStack(spacing: 10) {
                Text("#").frame(width: Column.number, alignment: .trailing)
                Text("Camera").frame(width: Column.camera, alignment: .leading)
                Text("Address").frame(maxWidth: .infinity, alignment: .leading)
                Text("Other changes").frame(width: Column.other, alignment: .leading)
                Text("Status").frame(width: Column.status, alignment: .leading)
            }
            .font(.caption).foregroundStyle(.secondary)
            .padding(.horizontal, 16).padding(.vertical, 5)
            Divider()
            List {
                ForEach(Array(ordered.enumerated()), id: \.element.mac) { index, d in
                    row(index + 1, d, itemsByMAC[d.mac])
                }
                .onMove { from, to in ordered.move(fromOffsets: from, toOffset: to) }
            }
            .listStyle(.plain)
            .moveDisabled(phase != .editing)
            // A preview that no longer matches the form or the order is shown faded until it is refreshed.
            .opacity(previewIsCurrent || items.isEmpty || phase != .editing ? 1 : 0.45)
        }
        .font(.system(.callout, design: .monospaced))
        .frame(height: 220)
        .background(Color(nsColor: .textBackgroundColor))
        .clipShape(RoundedRectangle(cornerRadius: 6))
    }

    private var itemsByMAC: [String: BatchItem] {
        Dictionary(items.map { ($0.mac, $0) }, uniquingKeysWith: { first, _ in first })
    }

    private func row(_ number: Int, _ d: Device, _ item: BatchItem?) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(alignment: .top, spacing: 10) {
                Text("\(number)").foregroundStyle(.secondary).frame(width: Column.number, alignment: .trailing)
                cameraCell(name: d.name.isEmpty ? d.model : d.name, mac: d.mac)
                    .frame(width: Column.camera, alignment: .leading)
                if let it = item {
                    Text(addressText(it)).frame(maxWidth: .infinity, alignment: .leading)
                    Text(otherChanges(it)).frame(width: Column.other, alignment: .leading)
                    statusCell(it).frame(width: Column.status, alignment: .leading)
                } else {
                    Text(d.ip).frame(maxWidth: .infinity, alignment: .leading)
                    Text("").frame(width: Column.other, alignment: .leading)
                    Text("not previewed").foregroundStyle(.secondary).frame(width: Column.status, alignment: .leading)
                }
            }
            if let it = item {
                ForEach(notes(it), id: \.self) { note in
                    Text(note).font(.caption).foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                        .padding(.leading, Column.number + 10)
                }
            }
        }
        .padding(.vertical, 2)
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
                Button("Try again") { phase = .editing; previewed = nil }
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
        if mode == .rescue {
            o.append("--rescue")
        } else if assignIPs {
            o += ["--start-ip", startIP]
            if step != "1" { o += ["--step", step] }
        }
        if !mask.isEmpty, mode == .change { o += ["--mask", mask] }
        if !gateway.isEmpty { o += ["--gw", gateway] }
        if !dns1.isEmpty { o += ["--dns1", dns1] }
        if !dns2.isEmpty { o += ["--dns2", dns2] }
        return o
    }

    private var formValid: Bool {
        let optional = [mask, gateway, dns1, dns2]
        guard optional.allSatisfy({ $0.isEmpty || isIPv4($0) }) else { return false }
        if mode == .rescue { return true }      // nothing is required: the helper works the plan out
        if assignIPs {
            guard isIPv4(startIP), let n = Int(step), n >= 1 else { return false }
            return true
        }
        return optional.contains { !$0.isEmpty }
    }

    private var passwordValid: Bool { !password.isEmpty && password.utf8.count <= 21 }
    /// The form and the camera order as one value, to tell whether a preview is still about the same thing.
    private var signature: [String] { options + ["--mac"] + ordered.map(\.mac) }
    private var previewIsCurrent: Bool { previewed == signature }

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
        let opts = options, macs = ordered.map(\.mac), sig = signature
        phase = .previewing
        problem = nil
        log = []
        Task { @MainActor in
            let run = await model.applyBatch(macs: macs, options: opts + ["--dry-run"], password: nil) { _ in }
            if let out = run.output {
                items = out.devices
                previewed = sig
            } else {
                items = []
                previewed = nil
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
            let run = await model.applyBatch(macs: ordered.map(\.mac), options: opts, password: pw) { line in
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
