import Foundation

/// Runs the bundled `ipcprobe` helper. The app has no network code of its own: discovery and configuration are
/// the Go binary's job, and this type is the only place the app touches it.
///
/// Finding the helper: inside the .app it lives at `Contents/MacOS/ipcprobe-cli`, which is what
/// `Bundle.main.url(forAuxiliaryExecutable:)` resolves. When the app is run outside a bundle (`swift run` during
/// development) that lookup fails, so `IPCPROBE_BIN` in the environment, then `ipcprobe` on `PATH`, are tried.
enum ProbeRunner {
    struct Output {
        var status: Int32
        var stdout: Data
        var stderr: String
    }

    struct HelperError: LocalizedError {
        var message: String
        var errorDescription: String? { message }
    }

    static func helperURL() throws -> URL {
        if let u = Bundle.main.url(forAuxiliaryExecutable: "ipcprobe-cli"), FileManager.default.isExecutableFile(atPath: u.path) {
            return u
        }
        if let p = ProcessInfo.processInfo.environment["IPCPROBE_BIN"], FileManager.default.isExecutableFile(atPath: p) {
            return URL(fileURLWithPath: p)
        }
        let path = ProcessInfo.processInfo.environment["PATH"] ?? "/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin"
        for dir in path.split(separator: ":") {
            let candidate = URL(fileURLWithPath: String(dir)).appendingPathComponent("ipcprobe")
            if FileManager.default.isExecutableFile(atPath: candidate.path) { return candidate }
        }
        throw HelperError(message: "The ipcprobe helper was not found in the app bundle (Contents/MacOS/ipcprobe-cli), in IPCPROBE_BIN, or on PATH.")
    }

    /// Runs the helper with `args`. `stdin`, if given, is written to the process and the pipe closed — this is how
    /// the admin password is passed: it never appears in the argument list, so it is never visible in `ps`.
    /// `onStderrLine` receives progress lines as they arrive (the helper reports progress on stderr, results on
    /// stdout).
    static func run(_ args: [String], stdin: String? = nil,
                    onStderrLine: (@Sendable (String) -> Void)? = nil) async throws -> Output {
        let exe = try helperURL()
        return try await withCheckedThrowingContinuation { cont in
            let p = Process()
            p.executableURL = exe
            p.arguments = args
            let outPipe = Pipe(), errPipe = Pipe(), inPipe = Pipe()
            p.standardOutput = outPipe
            p.standardError = errPipe
            p.standardInput = inPipe

            // Drain stderr incrementally so a chatty helper can never fill the pipe and stall.
            let collected = LineCollector()
            errPipe.fileHandleForReading.readabilityHandler = { fh in
                let d = fh.availableData
                if d.isEmpty { fh.readabilityHandler = nil; return }
                for line in collected.append(d) { onStderrLine?(line) }
            }

            p.terminationHandler = { proc in
                errPipe.fileHandleForReading.readabilityHandler = nil
                let tail = errPipe.fileHandleForReading.readDataToEndOfFile()
                for line in collected.append(tail, flush: true) { onStderrLine?(line) }
                let out = outPipe.fileHandleForReading.readDataToEndOfFile()
                cont.resume(returning: Output(status: proc.terminationStatus, stdout: out, stderr: collected.text))
            }

            do {
                try p.run()
            } catch {
                cont.resume(throwing: error)
                return
            }
            if let s = stdin {
                inPipe.fileHandleForWriting.write(Data((s + "\n").utf8))
            }
            try? inPipe.fileHandleForWriting.close()
        }
    }

    /// Decodes the helper's JSON stdout, turning a non-zero exit into an error carrying its stderr.
    static func runJSON<T: Decodable>(_ type: T.Type, _ args: [String]) async throws -> T {
        let r = try await run(args)
        guard r.status == 0 else {
            throw HelperError(message: r.stderr.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                              ? "ipcprobe exited with status \(r.status)" : r.stderr.trimmingCharacters(in: .whitespacesAndNewlines))
        }
        return try JSONDecoder().decode(T.self, from: r.stdout)
    }

    static func version() async -> String {
        guard let r = try? await run(["--version"]), r.status == 0,
              let s = String(data: r.stdout, encoding: .utf8) else { return "unavailable" }
        return s.trimmingCharacters(in: .whitespacesAndNewlines).replacingOccurrences(of: "ipcprobe ", with: "")
    }
}

/// Splits a byte stream into lines across chunk boundaries. Thread-safe: the readability handler and the
/// termination handler run on different queues.
final class LineCollector: @unchecked Sendable {
    private var buffer = Data()
    private(set) var text = ""
    private let lock = NSLock()

    func append(_ d: Data, flush: Bool = false) -> [String] {
        lock.lock(); defer { lock.unlock() }
        buffer.append(d)
        var lines: [String] = []
        while let nl = buffer.firstIndex(of: 0x0A) {
            let line = String(decoding: buffer[buffer.startIndex..<nl], as: UTF8.self)
            buffer.removeSubrange(buffer.startIndex...nl)
            lines.append(line)
        }
        if flush, !buffer.isEmpty {
            lines.append(String(decoding: buffer, as: UTF8.self))
            buffer.removeAll()
        }
        for l in lines { text += l + "\n" }
        return lines
    }
}
