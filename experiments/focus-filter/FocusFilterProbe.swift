import AppIntents
import AppKit
import SwiftUI

enum Evidence {
    static var directory: URL {
        get throws {
            guard let path = Bundle.main.object(forInfoDictionaryKey: "ProbeEvidenceDirectory") as? String,
                  path.hasPrefix("/"), !path.isEmpty else {
                throw CocoaError(.fileNoSuchFile)
            }
            return URL(fileURLWithPath: path, isDirectory: true)
        }
    }

    static func write(_ kind: String, active: Bool? = nil, error: Error? = nil) throws {
        let folder = try directory
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true,
                                                attributes: [.posixPermissions: 0o700])
        var event: [String: Any] = [
            "schemaVersion": 1, "kind": kind, "at": ISO8601DateFormatter().string(from: Date()),
            "pid": ProcessInfo.processInfo.processIdentifier,
            "bundleID": Bundle.main.bundleIdentifier ?? "unknown",
            "executable": Bundle.main.executableURL?.path ?? "unknown",
            "runID": Bundle.main.object(forInfoDictionaryKey: "ProbeRunID") as? String ?? "unknown"
        ]
        if let active { event["active"] = active }
        if let error { event["error"] = String(describing: error) }
        let file = folder.appendingPathComponent("\(kind)-\(UUID().uuidString).json")
        try JSONSerialization.data(withJSONObject: event, options: [.sortedKeys, .prettyPrinted])
            .write(to: file, options: .atomic)
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: file.path)
    }

    static func recent() -> String {
        do {
            let files = try FileManager.default.contentsOfDirectory(at: directory,
                includingPropertiesForKeys: [.contentModificationDateKey])
                .filter { $0.pathExtension == "json" }
                .sorted { $0.lastPathComponent < $1.lastPathComponent }
            let events = files.compactMap { file -> [String: Any]? in
                guard let data = try? Data(contentsOf: file) else { return nil }
                return (try? JSONSerialization.jsonObject(with: data)) as? [String: Any]
            }.sorted { ($0["at"] as? String ?? "") < ($1["at"] as? String ?? "") }
            return events.suffix(10).map {
                "\($0["at"] ?? "")  \($0["kind"] ?? "")  active=\($0["active"] ?? "—")\($0["error"].map { "  \($0)" } ?? "")"
            }.joined(separator: "\n")
        } catch { return "证据读取失败：\(error)" }
    }
}

struct ProbeFocusFilter: SetFocusFilterIntent {
    static var title: LocalizedStringResource = "专注力联动技术验证"
    static var description = IntentDescription("仅记录合成测试标记，不启动专注会话，也不切换系统模式。")

    @Parameter(title: "启用测试标记", default: false)
    var active: Bool

    var displayRepresentation: DisplayRepresentation {
        DisplayRepresentation(title: "测试标记：\(active ? "开" : "关")")
    }

    func perform() async throws -> some IntentResult {
        try Evidence.write("perform", active: active)
        return .result()
    }
}

struct ProbeView: View {
    @State private var events = "等待读取"
    @State private var readStatus = "尚未读取当前过滤配置"
    private let timer = Timer.publish(every: 1, on: .main, in: .common).autoconnect()

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("Focus Filter 技术验证").font(.title2)
            Text("独立样例 · 不修改正式应用或系统模式")
                .foregroundStyle(.secondary)
            Text("在系统设置的专注模式中添加本样例过滤条件，并开启测试标记。下方分别记录系统 perform 回调与只读查询。")
            HStack {
                Button("读取当前过滤配置") { Task { await readCurrent() } }
                Spacer()
                Button("退出样例") { NSApplication.shared.terminate(nil) }
            }
            Text(readStatus).font(.callout).foregroundStyle(.secondary)
            ScrollView {
                Text(events).font(.system(.caption, design: .monospaced))
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .textSelection(.enabled)
            }
            .padding(10)
            .background(.quaternary.opacity(0.35), in: RoundedRectangle(cornerRadius: 8))
        }
        .padding(22)
        .frame(minWidth: 580, minHeight: 360)
        .task {
            do { try Evidence.write("launch") }
            catch { readStatus = "证据保存失败：\(error)" }
            events = Evidence.recent()
        }
        .onReceive(timer) { _ in events = Evidence.recent() }
    }

    private func readCurrent() async {
        do {
            let filter = try await ProbeFocusFilter.current
            try Evidence.write("readCurrent", active: filter.active)
            readStatus = "本应用过滤配置标记：\(filter.active ? "开" : "关")；不代表全局 Focus 开关。"
        } catch {
            readStatus = "未能读取当前配置：\(error)"
            do { try Evidence.write("readCurrent", error: error) }
            catch { readStatus += "；证据保存失败：\(error)" }
        }
        events = Evidence.recent()
    }
}

@main
struct FocusFilterProbeApp: App {
    var body: some Scene {
        WindowGroup("Focus Filter 技术验证") { ProbeView() }
            .defaultSize(width: 600, height: 390)
    }
}
