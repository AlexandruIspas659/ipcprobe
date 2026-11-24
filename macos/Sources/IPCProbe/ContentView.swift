import SwiftUI

struct ContentView: View {
    @EnvironmentObject private var model: AppModel
    /// Current width of the detail pane. The selection buttons sit above it at the trailing end of the toolbar and
    /// must not be wider than it, or they visibly hang over the divider when the window is small.
    @State private var detailWidth: CGFloat = 340
    @FocusState private var filterFocused: Bool

    var body: some View {
        HSplitView {
            VStack(spacing: 0) {
                rescueBanner
                filterBar
                deviceTable
            }
            .frame(minWidth: 560)
            DeviceDetail(device: model.selectedDevice, selected: model.selectedDevices,
                         onChangeNetwork: { model.changeNetwork() },
                         onChangeBatch: { model.changeNetwork() },
                         onRescue: { model.rescue() },
                         onImportPlan: { model.importPlan() })
                .frame(minWidth: 300, idealWidth: 340, maxWidth: 420)
                .background(GeometryReader { g in
                    Color.clear
                        .onAppear { detailWidth = g.size.width }
                        .onChange(of: g.size.width) { detailWidth = $0 }
                })
        }
        .toolbar { toolbarContent }
        .safeAreaInset(edge: .bottom) { statusBar }
        .sheet(item: $model.editing) { d in
            SetNetworkSheet(device: d)
                .environmentObject(model)
        }
        .sheet(item: $model.batch) { b in
            BatchSheet(devices: b.devices, mode: b.mode)
                .environmentObject(model)
        }
    }

    private var deviceTable: some View {
        // The selection is a Set, so the table is multi-select: ⌘-click or ⇧-click several cameras to change them
        // together. Columns declared with `value:` are sortable: clicking a header updates model.sortOrder, and the row
        // below re-sorts the list. IP and Gateway sort by their numeric value (so .9 comes before .134) while
        // still showing the dotted string.
        Table(model.visibleDevices, selection: $model.selection, sortOrder: $model.sortOrder) {
            TableColumn("IP", value: \.ipValue) { addressCell($0) }.width(min: 130, ideal: 145)
            TableColumn("MAC", value: \.mac).width(min: 130, ideal: 140)
            TableColumn("Name", value: \.name).width(min: 100)
            TableColumn("Model", value: \.model).width(min: 110)
            TableColumn("Firmware", value: \.firmware).width(min: 90)
            TableColumn("Mask", value: \.mask).width(min: 110)
            TableColumn("Gateway", value: \.gatewayValue) { Text($0.gateway) }.width(min: 110)
        }
        .font(.system(.body, design: .monospaced))
        .onChange(of: model.sortOrder) { newOrder in model.devices.sort(using: newOrder) }
        .overlay {
            if model.devices.isEmpty && !model.isScanning && model.lastScan != nil {
                emptyState
            } else if model.visibleDevices.isEmpty && !model.devices.isEmpty {
                VStack(spacing: 8) {
                    Image(systemName: "magnifyingglass").font(.largeTitle).foregroundStyle(.secondary)
                    Text("No camera matches the filter").font(.title3)
                    Button("Show All Cameras") { model.filter = CameraFilter() }
                }
                .padding()
            }
        }
    }

    /// The address, with a mark when it needs attention: the camera is outside this Mac's subnet, or other cameras
    /// announce the same address.
    private func addressCell(_ d: Device) -> some View {
        HStack(spacing: 5) {
            Text(d.ip)
            if d.isForeign {
                Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.orange)
                    .help("Outside this Mac's subnet: it can be configured from here, but not reached by IP (no web page, no video) until its address is changed.")
            }
            if let n = model.devices.sharedAddresses[d.ip] {
                Image(systemName: "square.on.square").foregroundStyle(.secondary)
                    .help("\(n) cameras announce this address. Give them addresses of their own before using them.")
            }
        }
    }

    /// Shown only while there are stranded cameras: says what is wrong and offers the way out.
    @ViewBuilder
    private var rescueBanner: some View {
        let stranded = model.devices.foreign.count
        if stranded > 0 {
            HStack(spacing: 10) {
                Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.orange)
                Text(stranded == 1
                     ? "1 camera is outside this Mac's subnet and cannot be reached by IP."
                     : "\(stranded) cameras are outside this Mac's subnet and cannot be reached by IP.")
                    .lineLimit(2)
                Spacer()
                Button("Bring into This Network…") { model.rescue() }
            }
            .font(.callout)
            .padding(.horizontal, 12).padding(.vertical, 7)
            .background(Color.orange.opacity(0.12))
            Divider()
        }
    }

    /// A slim field above the table. It lives here rather than in the toolbar, which has no room to spare at the
    /// window's minimum width.
    private var filterBar: some View {
        VStack(spacing: 0) {
            HStack(spacing: 6) {
                Image(systemName: "magnifyingglass").foregroundStyle(filterFocused ? Color.accentColor : Color.secondary)
                TextField("Filter by name, model, address or MAC", text: $model.filter.text)
                    .textFieldStyle(.plain)
                    .focused($filterFocused)
                    .onExitCommand { model.filter.text = ""; filterFocused = false }     // Esc clears what was typed
                if !model.filter.isEmpty {
                    Text("\(model.visibleDevices.count) of \(model.devices.count)").foregroundStyle(.secondary)
                    Button {
                        model.filter = CameraFilter()
                    } label: {
                        Image(systemName: "xmark.circle.fill").foregroundStyle(.secondary)
                    }
                    .buttonStyle(.plain)
                    .help("Show all cameras")
                }
                Divider().frame(height: 14)
                criteriaMenu
            }
            .font(.callout)
            // Drawn as a field, not as text on a bar, and ringed in the accent colour while the cursor is in it —
            // so that ⌘F (Cameras › Filter…) visibly lands somewhere.
            .padding(.horizontal, 8).padding(.vertical, 5)
            .background(RoundedRectangle(cornerRadius: 7).fill(Color(nsColor: .textBackgroundColor)))
            .overlay(RoundedRectangle(cornerRadius: 7)
                .strokeBorder(filterFocused ? Color.accentColor : Color.secondary.opacity(0.3), lineWidth: filterFocused ? 2 : 1))
            .contentShape(Rectangle())
            .onTapGesture { filterFocused = true }
            .padding(.horizontal, 10).padding(.vertical, 6)
            if model.filter.hasCriteria { chips }
            Divider()
        }
        .onChange(of: model.filterFocusRequests) { _ in filterFocused = true }
    }

    /// The criteria that cannot be typed: built from the cameras on the network, each with how many it would show.
    private var criteriaMenu: some View {
        let facets = model.devices.facets(keeping: model.filter)
        return Menu {
            Toggle("Outside this subnet (\(facets.foreign))", isOn: $model.filter.foreign)
            Toggle("Shares an address (\(facets.sharedAddress))", isOn: $model.filter.sharedAddress)
            Toggle("Not named yet (\(facets.unnamed))", isOn: $model.filter.unnamed)
            criteriaSection("Model", facets.models, \.models)
            criteriaSection("Firmware", facets.firmwares, \.firmwares)
            criteriaSection("Subnet", facets.subnets, \.subnets)
            Divider()
            Button("Clear All Filters") { model.filter = CameraFilter() }
                .disabled(model.filter.isEmpty)
        } label: {
            Label("Filter", systemImage: model.filter.hasCriteria ? "line.3.horizontal.decrease.circle.fill"
                                                                  : "line.3.horizontal.decrease.circle")
        }
        .menuStyle(.borderlessButton)
        .fixedSize()
        .help("Narrow the list by status, model, firmware or subnet. What you pick stays visible below the field.")
    }

    @ViewBuilder
    private func criteriaSection(_ title: String, _ values: [CameraFacets.Value],
                                 _ set: WritableKeyPath<CameraFilter, Set<String>>) -> some View {
        if !values.isEmpty {
            Section(title) {
                ForEach(values) { v in
                    Toggle("\(v.value) (\(v.count))", isOn: Binding(
                        get: { model.filter[keyPath: set].contains(v.value) },
                        set: { on in
                            if on { model.filter[keyPath: set].insert(v.value) } else { model.filter[keyPath: set].remove(v.value) }
                        }))
                }
            }
        }
    }

    /// The picked criteria, each removable. They stay on screen for as long as they apply: a list that is quietly
    /// shorter than the network is how a batch ends up skipping cameras.
    private var chips: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 6) {
                ForEach(model.filter.chips) { chip in
                    HStack(spacing: 4) {
                        Text(chip.label)
                        Button {
                            model.filter.remove(chip)
                        } label: {
                            Image(systemName: "xmark").font(.caption2.weight(.bold))
                        }
                        .buttonStyle(.plain)
                        .help("Remove this filter")
                    }
                    .font(.callout)
                    .padding(.horizontal, 8).padding(.vertical, 3)
                    .background(Capsule().fill(Color.accentColor.opacity(0.3)))
                }
            }
            .padding(.horizontal, 10)
        }
        .padding(.bottom, 6)
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
        // Leading, not centred (.principal): at the minimum window width a centred group runs into the buttons
        // at the trailing end. From the leading edge the two groups only meet if the window is far too narrow.
        ToolbarItemGroup(placement: .navigation) {
            // No explicit frame: the toolbar draws its hover capsule around the item's frame, and a frame wider
            // than the pop-up button itself left a highlighted area with a smaller control inside it.
            Picker("Interface", selection: $model.selectedInterface) {
                Text("All interfaces").tag(String?.none)
                ForEach(model.interfaces) { i in
                    Text(i.label).tag(String?.some(i.ip))
                }
            }
            .fixedSize()
            .help("Which network interface to listen and send on. Only interfaces with an IPv4 address and multicast are listed; VPN tunnels never are.")

            Picker("Listen", selection: $model.listen) {
                Text("Live").tag(AppModel.Listen.live)
                Text("2 s").tag(AppModel.Listen.seconds(2))
                Text("5 s").tag(AppModel.Listen.seconds(5))
                Text("10 s").tag(AppModel.Listen.seconds(10))
            }
            .pickerStyle(.segmented)
            .help("Live keeps listening and updates the list as cameras appear, change and go quiet. The others listen once, for that long, each time you rescan.")
        }
        ToolbarItemGroup(placement: .primaryAction) {
            Button {
                model.selectAll()
            } label: {
                toolbarLabel("Select All", "checklist")
            }
            .disabled(model.visibleDevices.isEmpty)
            .help("Select every camera shown (⌥⌘A) — with a filter, the ones that match. ⌘-click or ⇧-click rows to pick some of them.")

            Button {
                model.changeNetwork()
            } label: {
                // A fixed, short title: the count lives in the tooltip and the status bar, so the button does not
                // grow with the selection.
                toolbarLabel("Change…", "square.and.pencil")
            }
            .disabled(model.selection.isEmpty)
            .help(model.selection.count > 1
                  ? "Change the network configuration of the \(model.selection.count) selected cameras together (⌘E)."
                  : "Change the network configuration of the selected camera (⌘E).")

            Button {
                Task { await model.rescan() }
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

    /// Toolbars show icons only by default; these buttons carry their words while the detail pane below them is wide
    /// enough to hold the group, and fall back to icons (with tooltips) when it is not.
    @ViewBuilder
    private func toolbarLabel(_ title: String, _ systemImage: String) -> some View {
        if detailWidth < 330 {
            Label(title, systemImage: systemImage).labelStyle(.iconOnly)
        } else {
            Label(title, systemImage: systemImage).labelStyle(.titleAndIcon)
        }
    }

    /// "12 devices", "3 of 12 devices", with "· 2 selected" when several rows are selected.
    private var countText: String {
        let all = model.devices.count, shown = model.visibleDevices.count
        var text = model.filter.isEmpty ? "\(all) device\(all == 1 ? "" : "s")" : "\(shown) of \(all) devices"
        if model.selection.count > 1 { text += " · \(model.selection.count) selected" }
        return text
    }

    private var statusBar: some View {
        HStack(spacing: 12) {
            if let e = model.error {
                Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.yellow)
                Text(e).lineLimit(2).textSelection(.enabled)
            } else if model.isScanning {
                Text("Listening for \(Int(model.timeout)) s…")
            } else if model.listen == .live {
                Circle().fill(model.liveRunning ? Color.green : Color.secondary).frame(width: 8, height: 8)
                Text(model.lastScan == nil
                     ? "Live · listening…"
                     : "Live · \(countText)")
            } else if let t = model.lastScan {
                Text("\(countText) · scanned \(t.formatted(date: .omitted, time: .standard))")
            } else {
                Text("Ready")
            }
            Spacer()
            Text(model.versionLabel).foregroundStyle(.secondary)
                .help("App version; the bundled ipcprobe helper is shown too when it differs (development builds).")
        }
        .font(.callout)
        .padding(.horizontal, 12)
        .padding(.vertical, 6)
        .background(.bar)
    }
}
